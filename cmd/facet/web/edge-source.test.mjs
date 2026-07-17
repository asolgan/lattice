// Unit vectors for the EDGE.5 W4 renderer swap's adaptation seam
// (edge-source.mjs) — the browser-native engine's JS API mapped to the
// renderer's feed-source interface. No live stack: a fake `api` stands in for
// the wasm host, exactly as internal/edge/browser/host_js_test.go fakes the
// shell. Node's built-in runner only (`node --test`), matching
// degraded_render.test.mjs and shell.test.mjs — no new dependency.

import { test } from "node:test";
import assert from "node:assert/strict";
import { dispatchFrame, edgeSource } from "./edge-source.mjs";

// collectingHandlers records every reducer call so a test can assert what the
// source delivered and in what shape.
function collectingHandlers() {
  const seen = [];
  return {
    seen,
    manifest: (fr) => seen.push(["manifest", fr]),
    outbox: (entry) => seen.push(["outbox", entry]),
    ready: (rev) => seen.push(["ready", rev]),
    revoked: (reason) => seen.push(["revoked", reason]),
    connectivity: (fr) => seen.push(["connectivity", fr]),
    open: () => seen.push(["open"]),
  };
}

test("dispatchFrame routes each frame kind to its handler", () => {
  const h = collectingHandlers();
  dispatchFrame(h, { kind: "manifest", key: "manifest.me", data: { x: 1 }, pending: true });
  dispatchFrame(h, { kind: "outbox", outbox: { requestId: "r1", state: "queued" } });
  dispatchFrame(h, { kind: "ready", revision: 42 });
  dispatchFrame(h, { kind: "revoked", reason: "token revoked" });
  dispatchFrame(h, { kind: "connectivity", connected: false });

  assert.deepEqual(h.seen, [
    ["manifest", { kind: "manifest", key: "manifest.me", data: { x: 1 }, pending: true }],
    ["outbox", { requestId: "r1", state: "queued" }],
    ["ready", 42],
    ["revoked", "token revoked"],
    ["connectivity", { kind: "connectivity", connected: false }],
  ]);
});

test("dispatchFrame ignores an unmodelled or malformed frame rather than throwing", () => {
  const h = collectingHandlers();
  dispatchFrame(h, { kind: "somethingNewer", foo: 1 });
  dispatchFrame(h, null);
  dispatchFrame(h, {});
  dispatchFrame(h, { kind: 123 });
  assert.equal(h.seen.length, 0);
});

test("dispatchFrame passes an empty string for a revoked frame with no reason", () => {
  const h = collectingHandlers();
  dispatchFrame(h, { kind: "revoked" });
  assert.deepEqual(h.seen, [["revoked", ""]]);
});

// fakeApi mimics the object latticeEdge.start() resolves to (host.go jsAPI).
function fakeApi(snapshotFrames) {
  const api = {
    frameCb: null,
    unsubCalled: false,
    stopCalled: false,
    enqueued: [],
    onFrame(cb) {
      api.frameCb = cb;
      return () => { api.unsubCalled = true; };
    },
    snapshot() {
      return Promise.resolve(snapshotFrames || []);
    },
    enqueue(req) {
      api.enqueued.push(req);
      return Promise.resolve({ requestId: "req-" + api.enqueued.length });
    },
    stop() {
      api.stopCalled = true;
      return Promise.resolve();
    },
  };
  return api;
}

test("edgeSource.start replays the snapshot burst through the reducer", async () => {
  const api = fakeApi([
    { kind: "connectivity", connected: true },
    { kind: "manifest", key: "manifest.me", data: { displayName: "Ada" } },
    { kind: "outbox", outbox: { requestId: "r0", state: "queued" } },
  ]);
  const h = collectingHandlers();
  const src = edgeSource(api);
  await src.start(h);

  assert.deepEqual(h.seen, [
    ["connectivity", { kind: "connectivity", connected: true }],
    ["manifest", { kind: "manifest", key: "manifest.me", data: { displayName: "Ada" } }],
    ["outbox", { requestId: "r0", state: "queued" }],
  ]);
});

test("edgeSource.start forwards subsequent live frames via onFrame", async () => {
  const api = fakeApi([]);
  const h = collectingHandlers();
  const src = edgeSource(api);
  await src.start(h);
  assert.equal(h.seen.length, 0);

  // A live delta lands after the snapshot.
  api.frameCb({ kind: "manifest", key: "manifest.task.abc", data: { title: "Laundry" }, pending: false });
  api.frameCb({ kind: "ready", revision: 7 });

  assert.deepEqual(h.seen, [
    ["manifest", { kind: "manifest", key: "manifest.task.abc", data: { title: "Laundry" }, pending: false }],
    ["ready", 7],
  ]);
});

test("edgeSource.enqueue forwards the request to the engine and returns its result", async () => {
  const api = fakeApi([]);
  const src = edgeSource(api);
  const req = { operationType: "RequestService", class: "write", payload: { a: 1 } };
  const body = await src.enqueue(req);
  assert.deepEqual(api.enqueued, [req]);
  assert.deepEqual(body, { requestId: "req-1" });
  assert.equal(body.error, undefined); // a resolved value with no error is an accept
});

test("edgeSource.close unsubscribes the frame listener and stops the engine", async () => {
  const api = fakeApi([]);
  const src = edgeSource(api);
  await src.start(collectingHandlers());
  await src.close();
  assert.equal(api.unsubCalled, true);
  assert.equal(api.stopCalled, true);
});
