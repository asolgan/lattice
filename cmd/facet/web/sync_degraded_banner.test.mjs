// Pins the sync-degraded banner wiring: the helper app.js's connectivity
// handler drives exists, toggles the element index.html declares, and leaves
// the reconnect banner's own element alone (the two axes render
// independently; the handler's offline-wins precedence lives in the
// non-exposed feedHandlers const, so it is exercised by the Go host tests'
// frame semantics rather than here). Same vm harness as
// degraded_render.test.mjs: app.js is a plain browser script, so
// vm.runInContext hoists its function declarations onto the sandbox.

import { test } from "node:test";
import assert from "node:assert/strict";
import vm from "node:vm";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const appSrc = fs.readFileSync(path.join(__dirname, "app.js"), "utf8");
const indexHtml = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");

function loadAppWithDom() {
  const els = new Map();
  const el = (id) => {
    if (!els.has(id)) els.set(id, { id, hidden: true, textContent: "" });
    return els.get(id);
  };
  const sandbox = {
    console,
    document: { addEventListener() {}, getElementById: el },
  };
  vm.createContext(sandbox);
  vm.runInContext(appSrc, sandbox, { filename: "app.js" });
  return { sandbox, el };
}

test("index.html declares the sync-degraded banner the helper targets", () => {
  assert.match(indexHtml, /id="sync-degraded-banner"/);
  assert.match(indexHtml, /Showing your last synced world/);
});

test("setSyncDegradedBanner toggles exactly the sync-degraded element", () => {
  const { sandbox, el } = loadAppWithDom();
  assert.equal(typeof sandbox.setSyncDegradedBanner, "function");

  sandbox.setSyncDegradedBanner(true);
  assert.equal(el("sync-degraded-banner").hidden, false);
  assert.equal(el("reconnect-banner").hidden, true, "the offline banner is a separate axis");

  sandbox.setSyncDegradedBanner(false);
  assert.equal(el("sync-degraded-banner").hidden, true);
});

test("reconnect banner helpers are untouched by the degraded axis", () => {
  const { sandbox, el } = loadAppWithDom();
  sandbox.setSyncDegradedBanner(true);
  sandbox.showReconnectBanner();
  assert.equal(el("reconnect-banner").hidden, false);
  sandbox.hideReconnectBanner();
  assert.equal(el("reconnect-banner").hidden, true);
  assert.equal(el("sync-degraded-banner").hidden, false, "hiding the offline banner leaves the degraded banner");
});
