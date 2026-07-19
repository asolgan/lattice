// Regression vectors for typed dispatch-target resolution (edge-showcase-app
// -design.md §3.3). A `dispatch.targetField` is answered by matching the op's
// declared `dispatch.targetType` against the keys the context carries — NOT by
// keying off authContext, which says something else entirely (which wire-
// envelope field the client populates).
//
// The bug these pin: every `authContext:"self"` op with a targetField used to
// resolve to the actor's identity key, so wellness CreateBooking submitted a
// vtx.identity where the script required a vtx.session and the Processor
// rejected it live. Six of the seven shipped op metas had this shape.
//
// Same harness as descriptor_autofill.test.mjs: app.js is a plain browser
// script, so vm.runInContext hoists its function declarations onto the
// sandbox. resolveTargetKey / keyType are declarations; the resolver's
// me()/tasks() fallbacks read the module-scoped `state`, which starts empty —
// so an unresolvable target correctly comes back undefined here.

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

const SESSION = "vtx.session.AAAAAAAAAAAAAAAAAAAA";
const APPT = "vtx.appointment.BBBBBBBBBBBBBBBBBBBB";
const SERVICE = "vtx.service.CCCCCCCCCCCCCCCCCCCC";

test("keyType reads the Contract #1 type out of a vtx key", () => {
  const { keyType } = loadApp();
  assert.equal(keyType(SESSION), "session");
  assert.equal(keyType("manifest.op.x"), undefined);
  assert.equal(keyType(""), undefined);
  assert.equal(keyType(undefined), undefined);
});

// The live failure, pinned: "Book a class" off a service card has no session
// anywhere in context, so it must resolve to nothing — never to the actor.
test("a self-authContext op does not resolve its typed target to the identity", () => {
  const { resolveTargetKey } = loadApp();
  const createBooking = {
    dispatchAuthContext: "self",
    dispatchTargetField: "session",
    dispatchTargetType: "session",
  };
  assert.equal(resolveTargetKey(createBooking, { serviceKey: SERVICE }), undefined);
});

test("a typed target resolves from the entity in view", () => {
  const { resolveTargetKey } = loadApp();
  const createBooking = {
    dispatchAuthContext: "self",
    dispatchTargetField: "session",
    dispatchTargetType: "session",
  };
  assert.equal(resolveTargetKey(createBooking, { entityKey: SESSION }), SESSION);
});

// A task scopedTo the appointment is what makes clinic's Reschedule/Cancel
// submittable — the same ops that previously sent an identity key.
test("a typed target resolves from the task's scopedTo target", () => {
  const { resolveTargetKey } = loadApp();
  const reschedule = {
    dispatchAuthContext: "self",
    dispatchTargetField: "appointmentKey",
    dispatchTargetType: "appointment",
  };
  assert.equal(resolveTargetKey(reschedule, { taskKey: "manifest.task.x", scopedTo: APPT }), APPT);
});

test("a context key of the wrong type does not satisfy a typed target", () => {
  const { resolveTargetKey } = loadApp();
  const createBooking = {
    dispatchAuthContext: "self",
    dispatchTargetField: "session",
    dispatchTargetType: "session",
  };
  assert.equal(resolveTargetKey(createBooking, { scopedTo: APPT, serviceKey: SERVICE }), undefined);
});

// RequestService — the one shape the authContext mapping always got right,
// and the one that must keep working now that it is declared.
test("a service-typed target resolves from the service in context", () => {
  const { resolveTargetKey } = loadApp();
  const requestService = {
    dispatchAuthContext: "service",
    dispatchTargetField: "service",
    dispatchTargetType: "service",
  };
  assert.equal(resolveTargetKey(requestService, { serviceKey: SERVICE }), SERVICE);
});

test("an op meta with no declared targetType keeps the authContext fallback", () => {
  const { resolveTargetKey } = loadApp();
  const legacyService = { dispatchAuthContext: "service", dispatchTargetField: "service" };
  assert.equal(resolveTargetKey(legacyService, { serviceKey: SERVICE }), SERVICE);

  const legacyTask = { dispatchAuthContext: "task", dispatchTargetField: "target" };
  assert.equal(resolveTargetKey(legacyTask, { scopedTo: APPT }), APPT);

  // The fallback deliberately has no "self" arm: that arm was the bug.
  const legacySelf = { dispatchAuthContext: "self", dispatchTargetField: "session" };
  assert.equal(resolveTargetKey(legacySelf, { serviceKey: SERVICE }), undefined);
});

test("an op with no targetField resolves to nothing at all", () => {
  const { resolveTargetKey } = loadApp();
  const openTab = { dispatchAuthContext: "self", dispatchClass: "tab" };
  assert.equal(resolveTargetKey(openTab, { serviceKey: SERVICE }), undefined);
});
