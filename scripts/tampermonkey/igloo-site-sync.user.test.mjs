import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import vm from "node:vm";

const script = fs.readFileSync(
  new URL("./igloo-site-sync.user.js", import.meta.url),
  "utf8",
);

function fakeElement() {
  const element = {
    style: {},
    dataset: {},
    classList: {
      add() {},
      remove() {},
      toggle() {},
      contains() {
        return false;
      },
    },
    appendChild(child) {
      return child;
    },
    insertAdjacentElement() {},
    remove() {},
    setAttribute() {},
    getAttribute() {
      return "";
    },
    addEventListener() {},
    querySelector() {
      return null;
    },
    querySelectorAll() {
      return [];
    },
    closest() {
      return null;
    },
  };
  return element;
}

function buildHarness({ prompts = [], followHandles = [] } = {}) {
  const values = new Map([
    ["igloo_sync_x_downloads", false],
    ["xsync_api_base", "http://127.0.0.1:5001"],
  ]);
  const requests = [];
  const menu = new Map();
  const promptCalls = [];
  const followButtons = followHandles.map((handle) => {
    const btn = fakeElement();
    btn.dataset.handle = handle;
    return btn;
  });
  const documentElement = fakeElement();
  const body = fakeElement();
  const head = fakeElement();

  const context = {
    console: {
      log() {},
      warn() {},
      error() {},
    },
    location: {
      hostname: "x.com",
      origin: "https://x.com",
      pathname: "/home",
    },
    window: {
      addEventListener() {},
    },
    unsafeWindow: {},
    document: {
      body,
      head,
      documentElement,
      addEventListener() {},
      getElementById() {
        return null;
      },
      querySelector() {
        return null;
      },
      querySelectorAll(selector) {
        if (selector === ".x-sync-btn[data-handle]") return followButtons;
        return [];
      },
      createElement() {
        return fakeElement();
      },
      createElementNS() {
        return fakeElement();
      },
    },
    MutationObserver: class {
      observe() {}
    },
    GM_getValue(key, fallback) {
      return values.has(key) ? values.get(key) : fallback;
    },
    GM_setValue(key, value) {
      values.set(key, value);
    },
    GM_registerMenuCommand(name, callback) {
      menu.set(name, callback);
    },
    GM_notification() {},
    GM_setClipboard() {},
    GM_download() {},
    GM_xmlhttpRequest(options) {
      requests.push(options.url);
      const response = responseFor(options.url);
      queueMicrotask(() => {
        options.onload({
          status: response.status,
          responseText: response.text,
        });
      });
    },
    prompt(message, fallback) {
      promptCalls.push([message, fallback]);
      return prompts.length ? prompts.shift() : null;
    },
    setTimeout(callback) {
      queueMicrotask(callback);
      return 1;
    },
    clearTimeout() {},
    setInterval() {
      return 1;
    },
    URL,
    queueMicrotask,
  };
  context.globalThis = context;

  return {
    context: vm.createContext(context),
    requests,
    values,
    menu,
    promptCalls,
    followButtons,
  };
}

function responseFor(url) {
  if (url === "http://127.0.0.1:5001/api/health") {
    return {
      status: 400,
      text: "Client sent an HTTP request to an HTTPS server.",
    };
  }
  if (url === "https://localhost:5001/api/health") {
    return { status: 200, text: JSON.stringify({ ok: true }) };
  }
  if (url === "https://localhost:5001/api/channels?platform=twitter") {
    return {
      status: 200,
      text: JSON.stringify({
        channels: [
          {
            channel_id: "twitter_alice",
            url: "",
          },
        ],
      }),
    };
  }
  if (url === "https://localhost:5001/api/feed/sources?platform=twitter") {
    return { status: 200, text: JSON.stringify({ sources: [] }) };
  }
  if (url === "https://localhost:5001/api/auth/login") {
    return {
      status: 200,
      text: JSON.stringify({
        access_token: "access-token",
        refresh_token: "refresh-token",
      }),
    };
  }
  return { status: 500, text: JSON.stringify({ error: "unexpected url" }) };
}

async function drainMicrotasks() {
  for (let i = 0; i < 8; i += 1) {
    await new Promise((resolve) => setImmediate(resolve));
  }
}

test("uses the HTTPS localhost API when the legacy HTTP default hits a TLS listener", async () => {
  const harness = buildHarness();
  vm.runInContext(script, harness.context, {
    filename: "igloo-site-sync.user.js",
  });

  await drainMicrotasks();

  assert.ok(
    harness.requests.includes("https://localhost:5001/api/health"),
    `expected HTTPS health probe, got ${harness.requests.join(", ")}`,
  );
  assert.ok(
    harness.requests.includes(
      "https://localhost:5001/api/channels?platform=twitter",
    ),
    `expected channels request over HTTPS, got ${harness.requests.join(", ")}`,
  );
});

test("recognizes followed X accounts from channel_id when the endpoint omits url", async () => {
  const harness = buildHarness({ followHandles: ["alice"] });
  vm.runInContext(script, harness.context, {
    filename: "igloo-site-sync.user.js",
  });

  await drainMicrotasks();

  assert.equal(harness.followButtons[0].textContent, "Following");
});

test("login menu prompts for API URL before credentials and removes manual bearer setup", async () => {
  const harness = buildHarness({
    prompts: ["https://localhost:5001", "admin", "secret"],
  });
  vm.runInContext(script, harness.context, {
    filename: "igloo-site-sync.user.js",
  });

  assert.equal(harness.menu.has("Set Dashboard Bearer Token"), false);
  const login = harness.menu.get("Login Dashboard (Store Token)");
  assert.equal(typeof login, "function");

  await login();
  await drainMicrotasks();

  assert.deepEqual(
    harness.promptCalls.map(([message]) => message),
    ["Dashboard API base URL", "Dashboard username", "Dashboard password"],
  );
  assert.equal(harness.values.get("xsync_api_base"), "https://localhost:5001");
  assert.equal(harness.values.get("xsync_auth_token"), "access-token");
  assert.ok(
    harness.requests.includes("https://localhost:5001/api/auth/login"),
    `expected login request over configured HTTPS base, got ${harness.requests.join(", ")}`,
  );
});

test("uses follow wording for visible subscription labels", () => {
  assert.doesNotMatch(
    script,
    /Save source|Saved source|Toggle Local Save|Local save/,
  );
});
