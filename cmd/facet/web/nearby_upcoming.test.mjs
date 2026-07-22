// Regression vectors for the Nearby past-session filter (verticals board:
// "Showcase demo session ages out"). A hosted-demo world that outlived its
// seed used to offer a class that had already happened as the only thing
// Nearby could book; a time-anchored entity is now offerable only while its
// startsAt is still in the future. StartsAt-less entities (a clinic provider,
// a café) are always current, and a present-but-unparseable startsAt stays
// visible rather than being silently dropped.
//
// Same harness as dispatch_target.test.mjs: app.js is a plain browser script,
// so vm.runInContext hoists its function declarations onto the sandbox and
// isUpcoming is callable directly.

import { test } from "node:test";
import assert from "node:assert/strict";
import vm from "node:vm";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const appSrc = fs.readFileSync(path.join(__dirname, "app.js"), "utf8");

function loadApp() {
  const sandbox = { console, document: { addEventListener() {} }, Date };
  vm.createContext(sandbox);
  vm.runInContext(appSrc, sandbox, { filename: "app.js" });
  return sandbox;
}

const ent = (startsAt) => ({ key: "manifest.ent.x", data: { entityType: "session", startsAt } });

test("a future session is upcoming", () => {
  const { isUpcoming } = loadApp();
  const future = new Date(Date.now() + 60 * 60 * 1000).toISOString();
  assert.equal(isUpcoming(ent(future)), true);
});

test("a past session is not upcoming", () => {
  const { isUpcoming } = loadApp();
  const past = new Date(Date.now() - 60 * 60 * 1000).toISOString();
  assert.equal(isUpcoming(ent(past)), false);
});

test("an entity with no startsAt is always current", () => {
  const { isUpcoming } = loadApp();
  assert.equal(isUpcoming(ent(undefined)), true);
  assert.equal(isUpcoming({ key: "manifest.ent.p", data: { entityType: "provider" } }), true);
});

test("a present-but-unparseable startsAt is left visible", () => {
  const { isUpcoming } = loadApp();
  assert.equal(isUpcoming(ent("not-a-date")), true);
});

test("a null/empty entity does not throw and reads as current", () => {
  const { isUpcoming } = loadApp();
  assert.equal(isUpcoming(null), true);
  assert.equal(isUpcoming({}), true);
});
