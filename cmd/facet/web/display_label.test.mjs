// Unit vectors for the display-name floor rule (display-name-convention-
// design.md §2, N2): a bare NanoID is never a primary label. prettify is the
// last rung of the ladder ("<Type> · <short-id>"); anchorLabel composes the
// N1-projected location + container names; identityLabel renders the typed
// fallback instead of "Unnamed" until N3's sealed self-name arrives.
//
// Same harness as descriptor_autofill.test.mjs: app.js is a plain browser
// script, so vm.runInContext hoists its function declarations onto the sandbox.
// prettify / typeLabel / anchorLabel / identityLabel read only their arguments
// (identityLabel's no-key branch falls through to shortIdentityLabel, which
// reads the module-scoped whoamiIdentityID — "" at load, so "Resident").

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

test("prettify renders a typed label with a short id, never a bare NanoID", () => {
  const { prettify } = loadApp();
  assert.equal(prettify("vtx.leaseapp.AAAAAAAAAAAAAAAAAAAA"), "Lease application · AAAAAA");
  assert.equal(prettify("vtx.building.BBBBBBBBBBBBBBBBBBBB"), "Building · BBBBBB");
  // an unmapped type titleCases its own segment rather than dropping to raw
  assert.equal(prettify("vtx.widget.CCCCCCCCCCCCCCCCCCCC"), "Widget · CCCCCC");
  // too-short to be a Contract #1 key: passthrough, no crash
  assert.equal(prettify("manifest.me"), "manifest.me");
  assert.equal(prettify(""), "Unknown");
});

test("anchorLabel composes N1 location + container names, floors to typed", () => {
  const { anchorLabel } = loadApp();
  const key = "vtx.unit.UUUUUUUUUUUUUUUUUUUU";
  assert.equal(anchorLabel({ key, name: "Unit 1", containerName: "Riverside Building" }), "Unit 1 · Riverside Building");
  assert.equal(anchorLabel({ key, name: "Unit 1" }), "Unit 1");
  assert.equal(anchorLabel({ key, containerName: "Riverside Building" }), "Riverside Building");
  // no projected name yet: typed floor, never the raw key
  assert.equal(anchorLabel({ key }), "Unit · UUUUUU");
});

test("scopedLabel composes the class-4 relational label from the projected subject name", () => {
  const { scopedLabel } = loadApp();
  const leaseapp = "vtx.leaseapp.LLLLLLLLLLLLLLLLLLLL";
  // N2-tail: the lens projects the applied-for unit's name onto the task row
  assert.equal(scopedLabel(leaseapp, "Unit 1"), "Unit 1 lease");
  // a scoped type with no relational suffix mapping: the subject name alone
  assert.equal(scopedLabel("vtx.booking.BBBBBBBBBBBBBBBBBBBB", "Yoga Flow"), "Yoga Flow");
  // no projected subject name (non-leaseapp scope / unnamed unit): typed floor
  assert.equal(scopedLabel(leaseapp, null), "Lease application · LLLLLL");
  assert.equal(scopedLabel("", "Unit 1"), "");
});

test("identityLabel prefers the sealed self-name, else a typed fallback (never Unnamed)", () => {
  const { identityLabel } = loadApp();
  assert.equal(identityLabel({ displayName: "Sam Okafor" }), "Sam Okafor");
  assert.equal(identityLabel({ identityKey: "vtx.identity.IIIIIIIIIIIIIIIIIIII" }), "Resident · IIIIII");
  // no name, no key: still typed, not "Unnamed"
  assert.equal(identityLabel({}), "Resident");
});
