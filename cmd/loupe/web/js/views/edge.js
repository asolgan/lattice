// Edge view: the Personal Lens / Edge Lattice device roster (F19). One card
// per registered device, grouped by identity, gapped-first.
//
// All decision logic lives in logic/edge.js (goja-tested); this file is the
// DOM binding. Every identity is a keyLink into the Graph explorer (design
// §1.2 — no dead ends).

import { $, el, api, setStatus } from "../api.js";
import { keyLinkEl } from "../render.js";
import {
  gapVerdict,
  gapLabel,
  hydrationNote,
  interestSummary,
  groupByIdentity,
  fleetHeadline,
  retentionLine,
  filterEmptyMessage,
  staleWarning,
} from "../logic/edge.js";

const state = { loaded: false, generation: 0 };

function enter() {
  if (state.loaded) return;
  state.loaded = true;
  loadFleet();
}

async function loadFleet() {
  // Two overlapping loads (a double-clicked Refresh, or Refresh racing the
  // filter's change handler) would otherwise repaint in completion order, so a
  // slow earlier response could overwrite a fast later one. Only the newest
  // request is allowed to touch the DOM.
  const generation = ++state.generation;
  setStatus("edge-status-msg", "loading…");
  const body = await api("/api/edge/fleet");
  if (generation !== state.generation) return;

  const host = $("#edge-groups");
  host.innerHTML = "";
  $("#edge-notes").innerHTML = "";
  $("#edge-retention").textContent = "";
  if (body.error) {
    setStatus("edge-status-msg", body.error, true);
    return;
  }
  setStatus("edge-status-msg", fleetHeadline(body));

  // Notes are the server's reasons a verdict could not be produced — an
  // absent measurement is stated, never quietly rendered as healthy.
  const notes = (body.notes || []).slice();
  const stale = staleWarning(body);
  if (stale) notes.push(stale);
  notes.forEach((n) => $("#edge-notes").appendChild(el("div", "small muted", n)));

  const line = retentionLine(body);
  if (line) $("#edge-retention").textContent = line;

  const all = body.devices || [];
  const gappedOnly = $("#edge-gapped-only").checked;
  const devices = gappedOnly ? all.filter((d) => d.gapped === true) : all;

  if (!devices.length) {
    host.appendChild(el("div", "muted", gappedOnly ? filterEmptyMessage(all) : "(no devices registered)"));
    return;
  }

  groupByIdentity(devices).forEach((group) => {
    const section = el("div", "card edge-identity" + (group.gapped ? " red" : ""));
    const head = el("div", "card-key");
    head.appendChild(keyLinkEl(group.identityKey));
    // Under the filter this count is a subset, so it is labelled as one rather
    // than reading as the identity's whole device list.
    const n = group.devices.length;
    head.appendChild(
      el("span", "card-group", gappedOnly ? n + " shown" : n + " device" + (n === 1 ? "" : "s"))
    );
    if (group.gapped) head.appendChild(el("span", "card-group badge-stuck", group.gapped + " gapped"));
    section.appendChild(head);
    group.devices.forEach((d) => section.appendChild(deviceRow(d, body.streamKnown)));
    host.appendChild(section);
  });
}

// deviceRow renders one device. The gap chip is the triage signal; the meta
// line below shows the working, so an operator who distrusts the chip can see
// what it was computed from.
function deviceRow(d, streamKnown) {
  const v = gapVerdict(d);
  const row = el("div", "edge-device");

  const title = el("div", "card-sub");
  title.appendChild(el("span", "edge-device-id", d.deviceId || "(unnamed device)"));
  const chipClass =
    v.state === "gapped" ? "badge-gapped" : v.state === "current" ? "badge-available" : "badge-unknown";
  title.appendChild(el("span", chipClass, gapLabel(d, streamKnown)));
  if (d.malformed) title.appendChild(el("span", "badge-gapped", "unreadable registration"));
  row.appendChild(title);

  row.appendChild(el("div", "small muted", interestSummary(d)));

  const meta = el("div", "card-meta small");
  if (!streamKnown) {
    // Attachment is read through the stream, so with no readable stream it is
    // unknown — not absent. Claiming "no SYNC consumer" here would contradict
    // the headline, which correctly reports the whole fleet as unmeasured.
    meta.appendChild(el("span", "muted", "attachment unknown"));
  } else if (d.subscribed) {
    meta.appendChild(el("span", null, "attached · acked through " + (d.ackFloor || 0)));
    if (d.pending) meta.appendChild(el("span", null, d.pending + " pending"));
  } else {
    meta.appendChild(el("span", "muted", "no SYNC consumer"));
  }
  const hydration = hydrationNote(d);
  if (hydration) meta.appendChild(el("span", "muted", hydration));
  if (d.registeredAt) meta.appendChild(el("span", "muted", "registered " + d.registeredAt));
  row.appendChild(meta);
  return row;
}

function init() {
  $("#edge-load").addEventListener("click", loadFleet);
  $("#edge-gapped-only").addEventListener("change", loadFleet);
}

export { init, enter };
