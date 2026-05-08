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

class TestElement {
  constructor(tagName, attrs = {}, children = []) {
    this.tagName = tagName.toUpperCase();
    this.attrs = { ...attrs };
    this.children = [];
    this.parentElement = null;
    this.dataset = {};
    this.style = {};
    this.textContent = attrs.textContent || "";
    this.src = attrs.src || "";
    for (const child of children) this.appendChild(child);
  }

  appendChild(child) {
    child.parentElement = this;
    this.children.push(child);
    return child;
  }

  getAttribute(name) {
    if (name === "src" && this.src) return this.src;
    return this.attrs[name] || "";
  }

  contains(target) {
    if (this === target) return true;
    return this.children.some((child) => child.contains(target));
  }

  closest(selector) {
    for (let node = this; node; node = node.parentElement) {
      if (matchesSelector(node, selector)) return node;
    }
    return null;
  }

  querySelector(selector) {
    return this.querySelectorAll(selector)[0] || null;
  }

  querySelectorAll(selector) {
    const selectors = selector.split(",").map((part) => part.trim());
    const results = [];
    const visit = (node) => {
      for (const child of node.children) {
        if (selectors.some((part) => matchesSelector(child, part))) {
          results.push(child);
        }
        visit(child);
      }
    };
    visit(this);
    return results;
  }
}

function el(tagName, attrs = {}, children = []) {
  return new TestElement(tagName, attrs, children);
}

function matchesSelector(node, selector) {
  if (selector === "time") return node.tagName === "TIME";
  if (selector === "video") return node.tagName === "VIDEO";
  if (selector === '[data-testid="videoPlayer"]') {
    return node.getAttribute("data-testid") === "videoPlayer";
  }
  if (selector === 'a[href*="/status/"]') {
    return (
      node.tagName === "A" &&
      node.getAttribute("href").includes("/status/")
    );
  }
  if (selector === 'a[href*="/status/"] time') {
    return node.tagName === "TIME" && !!node.closest('a[href*="/status/"]');
  }
  if (selector === 'img[src*="pbs.twimg.com/media"]') {
    return (
      node.tagName === "IMG" &&
      (node.src || node.getAttribute("src")).includes("pbs.twimg.com/media")
    );
  }
  if (selector === '[role="link"]') {
    return node.getAttribute("role") === "link";
  }
  return false;
}

function buildHarness({
  prompts = [],
  followHandles = [],
  localList = [],
  twitterChannels = [
    {
      channel_id: "twitter_alice",
      url: "",
    },
  ],
} = {}) {
  const values = new Map([
    ["igloo_sync_x_downloads", false],
    ["xsync_api_base", "http://127.0.0.1:5001"],
    ["xsync_local_list", localList],
  ]);
  const requests = [];
  const requestCalls = [];
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
      requestCalls.push({
        method: options.method,
        url: options.url,
        data: options.data,
      });
      const response = responseFor(options.url, { twitterChannels });
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
    requestCalls,
    values,
    menu,
    promptCalls,
    followButtons,
  };
}

function responseFor(url, { twitterChannels } = {}) {
  if (url === "http://127.0.0.1:5001/api/health/live") {
    return {
      status: 400,
      text: "Client sent an HTTP request to an HTTPS server.",
    };
  }
  if (url === "https://localhost:5001/api/health/live") {
    return { status: 200, text: JSON.stringify({ ok: true }) };
  }
  if (url === "https://localhost:5001/api/channels?platform=twitter") {
    return {
      status: 200,
      text: JSON.stringify({
        channels: twitterChannels,
      }),
    };
  }
  if (url === "https://localhost:5001/api/subscribe") {
    return {
      status: 201,
      text: JSON.stringify({
        success: true,
        channel_id: "twitter_bob",
      }),
    };
  }
  if (url === "https://localhost:5001/api/unsubscribe/twitter_bob") {
    return { status: 200, text: JSON.stringify({ success: true }) };
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

function runScript(harness, { exposeDebug = false } = {}) {
  const source = exposeDebug
    ? script.replace(
        /\}\)\(\);\s*$/,
        `globalThis.__iglooTest = {
  handleUnsave,
  collectTweetMediaItems: typeof collectTweetMediaItems === "function" ? collectTweetMediaItems : undefined,
  shouldShowMediaIndexPicker: typeof shouldShowMediaIndexPicker === "function" ? shouldShowMediaIndexPicker : undefined,
  normalizeSelectedMediaIndices: typeof normalizeSelectedMediaIndices === "function" ? normalizeSelectedMediaIndices : undefined,
};\n})();`,
      )
    : script;
  vm.runInContext(source, harness.context, {
    filename: "igloo-site-sync.user.js",
  });
}

test("uses the HTTPS localhost API when the legacy HTTP default hits a TLS listener", async () => {
  const harness = buildHarness();
  runScript(harness);

  await drainMicrotasks();

  assert.ok(
    harness.requests.includes("https://localhost:5001/api/health/live"),
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
  runScript(harness);

  await drainMicrotasks();

  assert.equal(harness.followButtons[0].textContent, "Following");
});

test("login menu prompts for API URL before credentials and removes manual bearer setup", async () => {
  const harness = buildHarness({
    prompts: ["https://localhost:5001", "admin", "secret"],
  });
  runScript(harness);

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

test("ghost-resubscribed X handles can be unfollowed immediately", async () => {
  const harness = buildHarness({
    localList: [{ handle: "bob", url: "https://x.com/bob" }],
    twitterChannels: [],
  });
  runScript(harness, { exposeDebug: true });

  await drainMicrotasks();

  assert.ok(
    harness.requestCalls.some(
      (call) =>
        call.method === "POST" &&
        call.url === "https://localhost:5001/api/subscribe",
    ),
    `expected ghost re-subscribe, got ${harness.requestCalls
      .map((call) => `${call.method} ${call.url}`)
      .join(", ")}`,
  );

  await harness.context.__iglooTest.handleUnsave("bob", null);
  await drainMicrotasks();

  assert.ok(
    harness.requestCalls.some(
      (call) =>
        call.method === "DELETE" &&
        call.url === "https://localhost:5001/api/unsubscribe/twitter_bob",
    ),
    `expected immediate unfollow DELETE, got ${harness.requestCalls
      .map((call) => `${call.method} ${call.url}`)
      .join(", ")}`,
  );
});

test("collects parent and quote media in parent-first order", () => {
  const harness = buildHarness();
  runScript(harness, { exposeDebug: true });

  const article = el("article", {}, [
    el("a", { href: "/parent/status/111" }, [el("time")]),
    el("a", { href: "/parent/status/111/photo/1" }, [
      el("img", {
        src: "https://pbs.twimg.com/media/main-one?format=jpg&name=small",
      }),
    ]),
    el("a", { href: "/parent/status/111/photo/2" }, [
      el("img", {
        src: "https://pbs.twimg.com/media/main-two?format=jpg&name=small",
      }),
    ]),
    el("div", { role: "link" }, [
      el("a", { href: "/quote/status/222" }, [el("time")]),
      el("a", { href: "/quote/status/222/photo/1" }, [
        el("img", {
          src: "https://pbs.twimg.com/media/quote-one?format=jpg&name=small",
        }),
      ]),
      el("a", { href: "/quote/status/222/photo/2" }, [
        el("img", {
          src: "https://pbs.twimg.com/media/quote-two?format=jpg&name=small",
        }),
      ]),
    ]),
  ]);

  const items = JSON.parse(
    JSON.stringify(harness.context.__iglooTest.collectTweetMediaItems(article)),
  );

  assert.deepEqual(
    items.map((item) => [item.kind, item.tweetId, item.url]),
    [
      [
        "image",
        "111",
        "https://pbs.twimg.com/media/main-one?format=jpg&name=orig",
      ],
      [
        "image",
        "111",
        "https://pbs.twimg.com/media/main-two?format=jpg&name=orig",
      ],
      [
        "image",
        "222",
        "https://pbs.twimg.com/media/quote-one?format=jpg&name=orig",
      ],
      [
        "image",
        "222",
        "https://pbs.twimg.com/media/quote-two?format=jpg&name=orig",
      ],
    ],
  );
});

test("uses the quote tweet URL for quote-only videos", () => {
  const harness = buildHarness();
  runScript(harness, { exposeDebug: true });

  const article = el("article", {}, [
    el("a", { href: "/parent/status/111" }, [el("time")]),
    el("div", { role: "link" }, [
      el("a", { href: "/quote/status/222" }, [el("time")]),
      el("div", { "data-testid": "videoPlayer" }, [el("video")]),
    ]),
  ]);

  const items = JSON.parse(
    JSON.stringify(harness.context.__iglooTest.collectTweetMediaItems(article)),
  );

  assert.deepEqual(items, [
    {
      kind: "video",
      tweetId: "222",
      tweetUrl: "https://x.com/quote/status/222",
      ext: ".mp4",
      index: 0,
    },
  ]);
});

test("shows the media picker even for a single media item", () => {
  const harness = buildHarness();
  runScript(harness, { exposeDebug: true });

  assert.equal(harness.context.__iglooTest.shouldShowMediaIndexPicker(1), true);
});

test("treats no selected media buttons as the default all-media selection", () => {
  const harness = buildHarness();
  runScript(harness, { exposeDebug: true });

  assert.equal(
    harness.context.__iglooTest.normalizeSelectedMediaIndices([], 4),
    null,
  );
});
