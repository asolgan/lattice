// Regression vectors for the EDGE.5 W4 renderer swap on the *other* two halves
// of the seam: app.js's SSE feed source (the shipped Go-host path must keep
// behaving byte-for-byte after the refactor to a pluggable source) and boot.mjs's
// config gate + engine-assembly wiring. No live stack; Node's built-in runner.
//
// app.js is a plain browser script exposing function declarations to a vm
// sandbox (the degraded_render.test.mjs idiom); boot.mjs and edge-source.mjs are
// ESM imported directly.

import { test } from "node:test";
import assert from "node:assert/strict";
import vm from "node:vm";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { readBootConfig, assembleEdgeSource, resolveDeviceId, createTokenRefresher } from "./boot.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const appSrc = fs.readFileSync(path.join(__dirname, "app.js"), "utf8");

// fakeEventSource records listeners so a test can fire named SSE events and
// assert the source translated them to reducer handler calls.
function makeFakeEventSourceClass(instances) {
  return class FakeEventSource {
    constructor(url) {
      this.url = url;
      this.listeners = {};
      this.closed = false;
      instances.push(this);
    }
    addEventListener(name, fn) { this.listeners[name] = fn; }
    close() { this.closed = true; }
    fire(name, data) { this.listeners[name]({ data: JSON.stringify(data) }); }
  };
}

function loadApp(extra) {
  const sandbox = {
    console,
    document: { addEventListener() {} },
    setTimeout: () => 0,
    clearTimeout: () => {},
    queueMicrotask: (fn) => fn(),
    ...extra,
  };
  vm.createContext(sandbox);
  vm.runInContext(appSrc, sandbox, { filename: "app.js" });
  return sandbox;
}

// norm rebuilds vm-context objects (JSON parsed inside the sandbox carry the
// sandbox's Object.prototype) with this context's prototypes, so deepEqual
// compares by structure rather than tripping on prototype identity.
function norm(v) { return JSON.parse(JSON.stringify(v)); }

function collectingHandlers() {
  const seen = [];
  return {
    seen,
    manifest: (fr) => seen.push(["manifest", fr]),
    outbox: (entry) => seen.push(["outbox", entry]),
    ready: () => seen.push(["ready"]),
    revoked: (reason) => seen.push(["revoked", reason]),
    connectivity: (fr) => seen.push(["connectivity", fr]),
    open: () => seen.push(["open"]),
  };
}

test("sseSource.start translates every SSE event to its reducer handler", () => {
  const instances = [];
  const { sseSource } = loadApp({ EventSource: makeFakeEventSourceClass(instances) });
  const h = collectingHandlers();
  const src = sseSource();
  src.start(h);

  assert.equal(instances.length, 1);
  assert.equal(instances[0].url, "/api/feed");
  const es = instances[0];

  es.listeners.onopen ? es.listeners.onopen() : es.onopen();
  es.fire("manifest", { key: "manifest.me", data: { displayName: "Ada" }, pending: true });
  es.fire("outbox", { outbox: { requestId: "r1", state: "queued" } });
  es.listeners.ready();
  es.fire("revoked", { reason: "token revoked" });
  es.fire("connectivity", { connected: false });

  assert.deepEqual(norm(h.seen), [
    ["open"],
    ["manifest", { key: "manifest.me", data: { displayName: "Ada" }, pending: true }],
    ["outbox", { requestId: "r1", state: "queued" }],
    ["ready"],
    ["revoked", "token revoked"],
    ["connectivity", { connected: false }],
  ]);
});

test("sseSource.start unwraps the outbox frame to its entry (parity with the edge source)", () => {
  const instances = [];
  const { sseSource } = loadApp({ EventSource: makeFakeEventSourceClass(instances) });
  const h = collectingHandlers();
  sseSource().start(h);
  instances[0].fire("outbox", { outbox: { requestId: "r9", state: "confirmed" } });
  assert.deepEqual(norm(h.seen), [["outbox", { requestId: "r9", state: "confirmed" }]]);
});

test("sseSource.revoked survives a malformed payload with an empty reason", () => {
  const instances = [];
  const { sseSource } = loadApp({ EventSource: makeFakeEventSourceClass(instances) });
  const h = collectingHandlers();
  sseSource().start(h);
  instances[0].listeners.revoked({ data: "not json" });
  assert.deepEqual(norm(h.seen), [["revoked", ""]]);
});

test("sseSource.enqueue POSTs the request to /api/enqueue and returns the parsed body", async () => {
  const instances = [];
  const calls = [];
  const fetchFn = (url, init) => {
    calls.push([url, init]);
    return Promise.resolve({ json: () => Promise.resolve({ requestId: "z1" }) });
  };
  const { sseSource } = loadApp({ EventSource: makeFakeEventSourceClass(instances), fetch: fetchFn });
  const src = sseSource();
  const req = { operationType: "RequestService", payload: { a: 1 } };
  const body = await src.enqueue(req);

  assert.equal(calls.length, 1);
  assert.equal(calls[0][0], "/api/enqueue");
  assert.equal(calls[0][1].method, "POST");
  assert.deepEqual(JSON.parse(calls[0][1].body), req);
  assert.deepEqual(body, { requestId: "z1" });
});

test("sseSource.close closes the underlying EventSource", () => {
  const instances = [];
  const { sseSource } = loadApp({ EventSource: makeFakeEventSourceClass(instances) });
  const src = sseSource();
  src.start(collectingHandlers());
  assert.equal(instances[0].closed, false);
  src.close();
  assert.equal(instances[0].closed, true);
});

// ---------------------------------------------------------------- boot.mjs

test("readBootConfig returns null when the page is not configured for the in-page engine", () => {
  const prev = globalThis.window;
  try {
    globalThis.window = {}; // no __EDGE_BOOT__
    assert.equal(readBootConfig(), null);
    globalThis.window = { __EDGE_BOOT__: { identityId: "i" } }; // incomplete
    assert.equal(readBootConfig(), null);
  } finally {
    globalThis.window = prev;
  }
});

test("readBootConfig fills defaults for the served asset URLs", () => {
  const prev = globalThis.window;
  try {
    globalThis.window = {
      __EDGE_BOOT__: {
        identityId: "vtx.identity.abc",
        deviceId: "dev1",
        wsUrl: "ws://localhost:9222",
        gatewayUrl: "http://localhost:8080",
        token: "jwt",
      },
    };
    const cfg = readBootConfig();
    assert.equal(cfg.identityId, "vtx.identity.abc");
    assert.equal(cfg.wasmUrl, "/edge.wasm");
    assert.equal(cfg.wasmExecUrl, "/wasm_exec.js");
    assert.equal(cfg.shellUrl, "/shell/shell.mjs");
  } finally {
    globalThis.window = prev;
  }
});

test("readBootConfig accepts a config with no deviceId (browser-local, §3.5)", () => {
  const prev = globalThis.window;
  try {
    // The static host (inc 4) does NOT inject deviceId — it is resolved
    // browser-side in boot() — so its absence must not void an otherwise
    // complete config.
    globalThis.window = {
      __EDGE_BOOT__: {
        identityId: "vtx.identity.abc",
        wsUrl: "ws://localhost:9222",
        gatewayUrl: "http://localhost:8080",
        token: "jwt",
      },
    };
    const cfg = readBootConfig();
    assert.notEqual(cfg, null, "a config missing only deviceId is still valid");
    assert.equal(cfg.deviceId, undefined);
  } finally {
    globalThis.window = prev;
  }
});

// ---------------------------------------------------------- resolveDeviceId

function fakeStorage(seed) {
  const m = new Map(Object.entries(seed || {}));
  return {
    getItem: (k) => (m.has(k) ? m.get(k) : null),
    setItem: (k, v) => m.set(k, v),
    _map: m,
  };
}

const DEVICE_ID_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz123456789";

function isSafeDeviceId(s) {
  if (typeof s !== "string" || s.length !== 20) return false;
  for (const ch of s) if (!DEVICE_ID_ALPHABET.includes(ch)) return false;
  return true;
}

test("resolveDeviceId generates a durable-name-safe id and persists it", () => {
  const storage = fakeStorage();
  const id = resolveDeviceId(storage);
  assert.ok(isSafeDeviceId(id), `generated id ${id} must be a 20-char NanoID-alphabet string`);
  assert.equal(storage.getItem("facet.deviceId"), id, "the id is persisted");
  // A second call returns the SAME persisted id (warm resume across reloads).
  assert.equal(resolveDeviceId(storage), id);
});

test("resolveDeviceId returns a valid stored id unchanged", () => {
  const stored = "ABCDEFGHJKLMNPQRSTUV"; // 20 chars, all in-alphabet
  const storage = fakeStorage({ "facet.deviceId": stored });
  assert.equal(resolveDeviceId(storage), stored);
});

test("resolveDeviceId regenerates when the stored id is malformed", () => {
  // A dot is forbidden in a JetStream consumer name — a stored id carrying one
  // (corrupt, or an older scheme) must be replaced, not trusted.
  const storage = fakeStorage({ "facet.deviceId": "bad.id.with.dots.xx" });
  const id = resolveDeviceId(storage);
  assert.ok(isSafeDeviceId(id));
  assert.notEqual(id, "bad.id.with.dots.xx");
  assert.equal(storage.getItem("facet.deviceId"), id, "the replacement is persisted");
});

test("resolveDeviceId degrades to an ephemeral id when storage throws", () => {
  const throwing = {
    getItem: () => { throw new Error("blocked"); },
    setItem: () => { throw new Error("blocked"); },
  };
  const id = resolveDeviceId(throwing);
  assert.ok(isSafeDeviceId(id), "a boot must still get a usable id with storage unavailable");
});

test("assembleEdgeSource configures the shell, starts the engine, and wires the shell's deliver", async () => {
  const cfg = {
    identityId: "vtx.identity.abc",
    deviceId: "dev1",
    wsUrl: "ws://localhost:9222",
    gatewayUrl: "http://localhost:8080",
    token: "jwt",
    types: ["svc"],
    anchors: ["a1"],
    storeName: "custom-store",
  };

  let shellConfig = null;
  const shell = { deliver: null };
  const createShell = (c) => { shellConfig = c; return shell; };

  const api = { deliver: () => "delivered", onFrame: () => () => {}, snapshot: () => Promise.resolve([]), enqueue: () => Promise.resolve({}), stop: () => Promise.resolve() };
  let startArg = null;
  const latticeEdge = { start: (a) => { startArg = a; return Promise.resolve(api); } };

  const src = await assembleEdgeSource({ latticeEdge, createShell, cfg });

  // The shell got the transport config, with getToken as a live getter.
  assert.equal(shellConfig.url, "ws://localhost:9222");
  assert.equal(shellConfig.identityId, "vtx.identity.abc");
  assert.equal(shellConfig.deviceId, "dev1");
  assert.equal(typeof shellConfig.getToken, "function");
  assert.equal(shellConfig.getToken(), "jwt");

  // start() got the engine config, including the shell.
  assert.equal(startArg.shell, shell);
  assert.equal(startArg.gatewayUrl, "http://localhost:8080");
  assert.deepEqual(startArg.types, ["svc"]);
  assert.equal(startArg.storeName, "custom-store");

  // The shell's push target is the engine's deliver (or the durable Naks every
  // message and the mirror never advances).
  assert.equal(shell.deliver, api.deliver);

  // The result is a feed source.
  assert.equal(typeof src.start, "function");
  assert.equal(typeof src.enqueue, "function");
  assert.equal(typeof src.close, "function");
});

test("assembleEdgeSource wires a supplied getToken into the shell and the initial start() call, live", async () => {
  // The sliding-session fix (session.go's handleSessionRefresh + boot()'s
  // createTokenRefresher below): the shell must read a LIVE getter, not the
  // cfg.token snapshot the page was served with — otherwise a rotated token
  // never reaches the WS reconnect authenticator or the wasm host's Gateway
  // submitter (internal/edge/browser/host.go's shellGetTokenFunc reads the
  // SAME shell.getToken this proves gets wired).
  const cfg = { identityId: "vtx.identity.abc", deviceId: "dev1", wsUrl: "ws://x", gatewayUrl: "http://gw", token: "stale-snapshot" };
  let shellConfig = null;
  const createShell = (c) => { shellConfig = c; return { deliver: null }; };
  let startArg = null;
  const latticeEdge = { start: (a) => { startArg = a; return Promise.resolve({ deliver: () => {} }); } };

  let current = "live-token";
  await assembleEdgeSource({ latticeEdge, createShell, cfg, getToken: () => current });

  assert.equal(shellConfig.getToken(), "live-token", "the shell must read the live getter, not cfg.token");
  assert.equal(startArg.token, "live-token", "the initial start() call must use the live getter's current value");

  current = "rotated-token";
  assert.equal(shellConfig.getToken(), "rotated-token", "a later rotation must reach the shell with no reassembly");
});

// ------------------------------------------------------- createTokenRefresher

// fakeFetch resolves/rejects per a script of canned responses, one per call —
// lets a test drive multiple refresh() calls with different outcomes.
function fakeFetch(script) {
  let i = 0;
  const calls = [];
  return {
    calls,
    fn: async (reqPath, init) => {
      calls.push({ path: reqPath, init });
      const step = script[Math.min(i, script.length - 1)];
      i++;
      if (step.throws) throw step.throws;
      return { ok: step.ok !== false, status: step.status ?? (step.ok === false ? 401 : 200), json: async () => step.body };
    },
  };
}

test("createTokenRefresher.refresh() POSTs to /api/session/refresh and updates the token cell on success", async () => {
  const { calls, fn } = fakeFetch([{ body: { token: "fresh-token", expiresAt: "2026-01-01T00:00:00Z" } }]);
  const r = createTokenRefresher("initial-token", { fetchImpl: fn, logger: { warn() {} } });
  assert.equal(r.getToken(), "initial-token");

  const ok = await r.refresh();

  assert.equal(ok, true);
  assert.equal(r.getToken(), "fresh-token");
  assert.equal(calls.length, 1);
  assert.equal(calls[0].path, "/api/session/refresh");
  assert.equal(calls[0].init.method, "POST");
});

test("createTokenRefresher.refresh() keeps the last-known token on an HTTP error", async () => {
  const { fn } = fakeFetch([{ ok: false, status: 401 }]);
  const warnings = [];
  const r = createTokenRefresher("initial-token", { fetchImpl: fn, logger: { warn: (...a) => warnings.push(a) } });

  assert.equal(await r.refresh(), false);
  assert.equal(r.getToken(), "initial-token");
  assert.equal(warnings.length, 1);
});

test("createTokenRefresher.refresh() keeps the last-known token on a network failure", async () => {
  const { fn } = fakeFetch([{ throws: new TypeError("network down") }]);
  const r = createTokenRefresher("initial-token", { fetchImpl: fn, logger: { warn() {} } });

  assert.equal(await r.refresh(), false);
  assert.equal(r.getToken(), "initial-token");
});

test("createTokenRefresher.refresh() rejects a malformed response body without updating the cell", async () => {
  const { fn } = fakeFetch([{ body: { expiresAt: "2026-01-01T00:00:00Z" } }]); // no token field
  const r = createTokenRefresher("initial-token", { fetchImpl: fn, logger: { warn() {} } });

  assert.equal(await r.refresh(), false);
  assert.equal(r.getToken(), "initial-token");
});

test("createTokenRefresher.refresh() can be called repeatedly, each call re-reading the current cell", async () => {
  const { fn } = fakeFetch([{ body: { token: "token-2" } }, { ok: false, status: 401 }, { body: { token: "token-3" } }]);
  const r = createTokenRefresher("token-1", { fetchImpl: fn, logger: { warn() {} } });

  await r.refresh();
  assert.equal(r.getToken(), "token-2");
  await r.refresh(); // fails — stays on token-2
  assert.equal(r.getToken(), "token-2");
  await r.refresh();
  assert.equal(r.getToken(), "token-3");
});
