// Unit vectors for the maintenance half of the Work screen
// (facet-staff-worlds-design.md FORK-S1 A / §6 F5): the mirror-backed
// work-order list the offline tech works from.
//
// The property every vector here defends is the one that makes this section
// worth having at all — it is the OPPOSITE of the Protected pane beneath it.
// The pane goes unavailable offline; this list stays complete and stays
// actionable, and says so. A regression that made it degrade like the pane
// would look harmless and would take the whole feature with it.
//
// Same harness as staff_worklist.test.mjs — app.js is a plain browser script,
// so vm.runInContext hoists its function declarations onto the sandbox.

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

const woOpen = {
  key: "manifest.work.WO1",
  data: {
    workOrderKey: "vtx.workorder.WO1",
    summary: "Basement riser valve is weeping",
    priority: "urgent",
    reportedAt: "2026-07-21T09:00:00Z",
    placeKey: "vtx.unit.U1",
    placeName: "Unit 1",
    status: "open",
  },
};

const woResolved = {
  key: "manifest.work.WO2",
  data: {
    workOrderKey: "vtx.workorder.WO2",
    summary: "Lobby door sticks",
    priority: "normal",
    reportedAt: "2026-07-20T09:00:00Z",
    placeKey: "vtx.building.B1",
    placeName: "Riverside Building",
    status: "resolved",
    resolutionNotes: "Planed the frame.",
  },
};

const queuedTask = {
  key: "manifest.task.T1",
  data: { taskKey: "vtx.task.T1", scopedTo: "vtx.workorder.WO1", queuedRoleName: "backOfHouse" },
};

const myTask = {
  key: "manifest.task.T1",
  data: { taskKey: "vtx.task.T1", scopedTo: "vtx.workorder.WO1", assignee: "vtx.identity.ME" },
};

test("offline, the list still renders in full and says work will sync", () => {
  const { workOrdersHTML } = loadApp();
  const html = workOrdersHTML([woOpen], [queuedTask], false);
  assert.match(html, /Basement riser valve is weeping/);
  assert.match(html, /queued and syncs when you are back/i);
  // The pane's vocabulary must NOT leak into this half: "unavailable" here
  // would tell a tech their work list could not be read, which is false.
  assert.doesNotMatch(html, /unavailable/i);
});

test("a queued work order offers Claim; a claimed one offers Resolve", () => {
  const { workOrdersHTML } = loadApp();
  const claimable = workOrdersHTML([woOpen], [queuedTask], true);
  assert.match(claimable, /data-claim-task/);
  assert.doesNotMatch(claimable, /Resolve/);

  const mine = workOrdersHTML([woOpen], [myTask], true);
  assert.match(mine, /Resolve/);
  assert.doesNotMatch(mine, /data-claim-task/);
});

test("a work order with no task at all still lists, with no affordance", () => {
  // Work can exist at your building that nobody has queued a task for. It
  // belongs in "what work exists here" — silently dropping it would make the
  // list a lie about the building.
  const { workOrdersHTML } = loadApp();
  const html = workOrdersHTML([woOpen], [], true);
  assert.match(html, /Basement riser valve is weeping/);
  assert.doesNotMatch(html, /data-claim-task/);
  assert.doesNotMatch(html, /Resolve/);
});

test("a resolved order moves to its own group and offers nothing", () => {
  const { workOrdersHTML } = loadApp();
  const html = workOrdersHTML([woOpen, woResolved], [queuedTask], true);
  assert.match(html, /Resolved/);
  assert.match(html, /Planed the frame\./);
  // The open count in the heading counts only open work.
  assert.match(html, /Work orders \(1\)/);
});

test("urgent sorts above normal, then oldest first", () => {
  const { sortWorkOrders } = loadApp();
  const older = { data: { priority: "normal", reportedAt: "2026-07-01T00:00:00Z", summary: "old" } };
  const newer = { data: { priority: "normal", reportedAt: "2026-07-20T00:00:00Z", summary: "new" } };
  const urgent = { data: { priority: "urgent", reportedAt: "2026-07-21T00:00:00Z", summary: "urgent" } };
  // Joined rather than deepEqual'd: the array comes back from the vm realm,
  // so its prototype is not the host Array and a strict deep-equal fails on
  // identical contents.
  const got = sortWorkOrders([newer, older, urgent]).map((w) => w.data.summary).join("|");
  assert.equal(got, "urgent|old|new");
});

test("a pending claim shows on the row it was made from", () => {
  // The tech taps Claim underground, the intent queues, and the row must SAY
  // so — a tech who cannot tell whether their tap registered taps again.
  const { workOrdersHTML } = loadApp();
  const html = workOrdersHTML([woOpen], [{ ...queuedTask, pending: true }], false);
  assert.match(html, /pending-chip/);
});

test("a place with no projected name falls to a typed label, never a NanoID", () => {
  const { workOrdersHTML } = loadApp();
  const nameless = { key: "manifest.work.WO3", data: { ...woOpen.data, placeName: null, placeKey: "vtx.unit.LnRy1abc" } };
  const html = workOrdersHTML([nameless], [], true);
  assert.match(html, /Unit/);
  assert.doesNotMatch(html, /LnRy1abc/);
});

test("no work orders renders nothing at all, not an empty section", () => {
  // A front-desk actor has a workplace but no maintenance work; their Work
  // screen should be the Protected pane alone, with no vestigial heading.
  const { workOrdersHTML } = loadApp();
  assert.equal(workOrdersHTML([], [], true), "");
});
