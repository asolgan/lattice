// Regression test for the descriptor form's self-anchored auto-fill: café
// OpenTab's leaseAppKey (edge-showcase-app-design.md §3.6) is filled read-only
// from the manifest the signed-in identity already carries (the Sign-lease
// task's scopedTo) instead of asking the visitor to paste a raw vertex key.
//
// Same harness as degraded_render.test.mjs: app.js is a plain browser script,
// so vm.runInContext hoists its function declarations onto the sandbox
// (const/let stay lexical). vtxTypeForField / selfAnchoredKeys / renderField
// are all declarations and read only their arguments, so they exercise in
// isolation without a DOM or the module-scoped `state`.

import { test } from "node:test";
import assert from "node:assert/strict";
import vm from "node:vm";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const appSrc = fs.readFileSync(path.join(__dirname, "app.js"), "utf8");

function loadApp() {
  const sandbox = { console, document: { addEventListener() {} } };
  vm.createContext(sandbox);
  vm.runInContext(appSrc, sandbox, { filename: "app.js" });
  return sandbox;
}

test("vtxTypeForField maps <type>Key fields to their Contract #1 vertex type", () => {
  const { vtxTypeForField } = loadApp();
  assert.equal(vtxTypeForField("leaseAppKey"), "leaseapp");
  assert.equal(vtxTypeForField("tabKey"), "tab");
  assert.equal(vtxTypeForField("sessionKey"), "session");
  assert.equal(vtxTypeForField("amountCents"), undefined);
  assert.equal(vtxTypeForField(""), undefined);
});

test("selfAnchoredKeys indexes task scopedTo targets by vertex type", () => {
  const { selfAnchoredKeys } = loadApp();
  const idx = selfAnchoredKeys([
    { data: { scopedTo: "vtx.leaseapp.AAAAAAAAAAAAAAAAAAAA" } },
    { data: { scopedTo: "vtx.appointment.BBBBBBBBBBBBBBBBBBBB" } },
    { data: {} },                       // a task with no scopedTo is skipped
    { data: { scopedTo: "manifest.op.x" } }, // a non-vtx key is skipped
  ]);
  assert.deepEqual([...(idx.get("leaseapp") || [])], ["vtx.leaseapp.AAAAAAAAAAAAAAAAAAAA"]);
  assert.equal(idx.get("leaseapp").size, 1);
  assert.ok(idx.has("appointment"));
  assert.equal(idx.has("op"), false);
});

test("selfAnchoredKeys keeps two same-type keys so the field stays ambiguous", () => {
  const { selfAnchoredKeys } = loadApp();
  const idx = selfAnchoredKeys([
    { data: { scopedTo: "vtx.leaseapp.AAAAAAAAAAAAAAAAAAAA" } },
    { data: { scopedTo: "vtx.leaseapp.CCCCCCCCCCCCCCCCCCCC" } },
  ]);
  assert.equal(idx.get("leaseapp").size, 2); // renderDescriptorForm fills only when size === 1
});

test("renderField auto-anchored shows a read-only summary carrying the real key", () => {
  const { renderField } = loadApp();
  const key = "vtx.leaseapp.AAAAAAAAAAAAAAAAAAAA";
  const html = renderField("leaseAppKey", { type: "string" }, "your lease", true, key, false, true);
  assert.match(html, /class="static-field"/);
  assert.match(html, /type="hidden" name="leaseAppKey" value="vtx\.leaseapp\.AAAAAAAAAAAAAAAAAAAA"/);
  assert.doesNotMatch(html, /type="text"/); // never a paste-a-vertex-key input
  assert.match(html, /Lease application · AAAAAA/); // prettify() typed summary, not the raw key
});

test("renderField without the anchored flag renders the ordinary editable input", () => {
  const { renderField } = loadApp();
  const html = renderField("leaseAppKey", { type: "string" }, "your lease", true, "", false, false);
  assert.match(html, /type="text" name="leaseAppKey"/);
  assert.doesNotMatch(html, /static-field/);
});
