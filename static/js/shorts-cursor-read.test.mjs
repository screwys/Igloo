import assert from 'node:assert/strict';
import test from 'node:test';

import {
  readMomentsCursor,
  reconcileMomentsCursorRead,
  nextMomentsCursorTimestamp,
  observeMomentsCursorTimestamp,
  advanceMomentsCursorTimestamp,
  createMomentsCursorWriteQueue,
  momentsResumeVideoId,
} from './src/shorts/cursor.js';

function deferred() {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

test('a timed out history read remains non-authoritative when the response arrives late', async () => {
  const response = deferred();
  let timeout;

  const read = readMomentsCursor(() => response.promise, {
    timeoutMs: 1500,
    scheduleTimeout(callback) {
      timeout = callback;
      return 1;
    },
    cancelTimeout() {},
  });

  timeout();
  const result = await read;
  assert.deepEqual(result, { authoritative: false, cursor: null });

  response.resolve({ video_id: 'server-cursor' });
  await Promise.resolve();
  await Promise.resolve();

  assert.deepEqual(await read, result);
});

test('a failed history read is non-authoritative', async () => {
  const read = readMomentsCursor(() => Promise.reject(new Error('unavailable')), {
    timeoutMs: 1500,
    scheduleTimeout() {
      return 1;
    },
    cancelTimeout() {},
  });

  assert.deepEqual(await read, { authoritative: false, cursor: null });
});

test('a successful history response returns the authoritative cursor', async () => {
  const cursor = { video_id: 'server-cursor', updated_at_ms: 2000 };
  const read = readMomentsCursor(() => Promise.resolve(cursor), {
    timeoutMs: 1500,
    scheduleTimeout() {
      return 1;
    },
    cancelTimeout() {},
  });

  assert.deepEqual(await read, { authoritative: true, cursor });
});

test('an empty successful history response is authoritative', async () => {
  let timeout;
  const read = readMomentsCursor(() => Promise.resolve({ video_id: '' }), {
    timeoutMs: 1500,
    scheduleTimeout(callback) {
      timeout = callback;
      return 1;
    },
    cancelTimeout() {
      timeout = null;
    },
  });

  assert.deepEqual(await read, { authoritative: true, cursor: null });
  assert.equal(timeout, null);
});

test('an authoritative cursor replaces a locally newer resume', () => {
  const localResume = {
    videoId: 'local-cursor',
    page: 4,
    index: 9,
    ts: 9000,
    scope: 'all',
  };
  const serverRead = {
    authoritative: true,
    cursor: {
      video_id: 'server-cursor',
      page: 2,
      index: 3,
      sort_at_ms: 1500,
      updated_at_ms: 2000,
    },
  };

  const reconciled = reconcileMomentsCursorRead(serverRead, localResume, 'all');

  assert.deepEqual(reconciled, {
    apply: true,
    resume: {
      videoId: 'server-cursor',
      page: 2,
      index: 3,
      sortAtMs: 1500,
      ts: 2000,
      scope: 'all',
    },
  });
});

test('authoritative empty clears local resume while a failed read keeps it', () => {
  const localResume = {
    videoId: 'local-cursor',
    page: 4,
    index: 9,
    ts: 9000,
    scope: 'following',
  };

  assert.deepEqual(
    reconcileMomentsCursorRead(
      { authoritative: true, cursor: null },
      localResume,
      'following',
    ),
    { apply: true, resume: null },
  );
  assert.deepEqual(
    reconcileMomentsCursorRead(
      { authoritative: false, cursor: null },
      localResume,
      'following',
    ),
    { apply: false, resume: localResume },
  );
});

test('cursor timestamps advance past a fetched server cursor despite a behind or equal clock', () => {
  const currentResume = { ts: 10_000 };

  assert.equal(nextMomentsCursorTimestamp(9_500, currentResume), 10_001);
  assert.equal(nextMomentsCursorTimestamp(10_000, currentResume), 10_001);
  assert.equal(nextMomentsCursorTimestamp(11_000, currentResume), 11_000);
});

test('the in-memory cursor clock advances when browser storage cannot retain the server resume', () => {
  const clockByScope = new Map();
  observeMomentsCursorTimestamp(clockByScope, 'all', 10_000);

  assert.equal(
    advanceMomentsCursorTimestamp(clockByScope, 'all', 10_000, null),
    10_001,
  );
  assert.equal(
    advanceMomentsCursorTimestamp(clockByScope, 'all', 10_000, null),
    10_002,
  );
});

test('an older history response cannot lower an in-session cursor clock', () => {
  const clockByScope = new Map();
  observeMomentsCursorTimestamp(clockByScope, 'all', 10_001);
  observeMomentsCursorTimestamp(clockByScope, 'all', 10_000);

  assert.equal(
    advanceMomentsCursorTimestamp(clockByScope, 'all', 10_000, null),
    10_002,
  );
});

test('same-scope history waits for the pending cursor write to commit', async () => {
  const writes = createMomentsCursorWriteQueue();
  const commit = deferred();
  let serverCursor = 'server-old';
  let historyStarted = false;

  const pendingWrite = writes.enqueue('all', async () => {
    await commit.promise;
    serverCursor = 'client-new';
  });
  const history = writes.afterWrites('all', async () => {
    historyStarted = true;
    return serverCursor;
  });

  await Promise.resolve();
  await Promise.resolve();
  assert.equal(historyStarted, false);

  commit.resolve();
  await pendingWrite;
  assert.equal(await history, 'client-new');
});

test('rapid cursor writes retain only the latest request behind an in-flight write', async () => {
  const writes = createMomentsCursorWriteQueue();
  const firstCommit = deferred();
  const started = [];
  const pending = [
    writes.enqueue('all', async () => {
      started.push(1);
      await firstCommit.promise;
      return 1;
    }),
  ];

  for (let cursor = 2; cursor <= 50; cursor += 1) {
    pending.push(writes.enqueue('all', async () => {
      started.push(cursor);
      return cursor;
    }));
  }

  assert.deepEqual(started, [1]);
  firstCommit.resolve();
  assert.deepEqual(await Promise.all(pending), [
    1,
    ...Array.from({ length: 49 }, () => 50),
  ]);
  assert.deepEqual(started, [1, 50]);
});

test('page exit starts the retained latest cursor while the first write is still pending', async () => {
  const writes = createMomentsCursorWriteQueue();
  const firstCommit = deferred();
  const latestCommit = deferred();
  const started = [];
  const first = writes.enqueue('all', async () => {
    started.push('first');
    await firstCommit.promise;
  });
  const latest = writes.enqueue('all', async () => {
    started.push('latest');
    await latestCommit.promise;
  });

  writes.flushLatest();
  assert.deepEqual(started, ['first', 'latest']);

  latestCommit.resolve();
  firstCommit.resolve();
  await Promise.all([first, latest]);
});

test('following never falls back to the legacy unscoped resume after authoritative empty', () => {
  assert.equal(momentsResumeVideoId(null, 'legacy-all-cursor', 'following'), '');
  assert.equal(momentsResumeVideoId(null, 'legacy-all-cursor', 'stories'), '');
  assert.equal(momentsResumeVideoId(null, 'legacy-all-cursor', 'all'), 'legacy-all-cursor');
});

test('an authoritative read exposes server time for the web mutation clock', async () => {
  const read = await readMomentsCursor(
    () => Promise.resolve({ video_id: '', server_time_ms: 12_345 }),
    {
      timeoutMs: 1500,
      scheduleTimeout() {
        return 1;
      },
      cancelTimeout() {},
    },
  );

  assert.deepEqual(read, {
    authoritative: true,
    cursor: null,
    serverTimeMs: 12_345,
  });
});
