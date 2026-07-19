// Pure decision logic for the Edge fleet view (F19). No DOM, no fetch —
// goja-tested via cmd/loupe/web_logic_edge_test.go (strip-export load).
//
// The load-bearing rule throughout: a question the platform cannot answer must
// render as UNKNOWN, never as a clean all-clear. `gapped: null` means "no cursor to compare" or "no readable
// stream" — it never means "not gapped". Edge nodes structurally cannot
// self-report health (their per-identity permission set admits only their own
// sync subject and control RPCs), so every signal here is inferred
// server-side, and the view says which inference it made.

// gapVerdict classifies one device's sync-gap state.
//   "gapped"  — the SYNC stream's retention floor has overtaken this device's
//               ack floor; the deltas between them aged out and a warm resume
//               would silently miss them.
//   "current" — the device's ack floor is still inside the retention window.
//   "unknown" — unanswerable (the device has no SYNC durable, or the stream
//               could not be read).
function gapVerdict(device) {
  var d = device || {};
  if (d.gapped === null || d.gapped === undefined) {
    return { state: "unknown", behindBy: 0 };
  }
  return { state: d.gapped ? "gapped" : "current", behindBy: d.gapped ? d.behindBy || 0 : 0 };
}

// gapLabel is the human line for a verdict. An unknown verdict names WHY it is
// unknown, because "unknown" alone reads as a bug rather than a fact.
function gapLabel(device, streamKnown) {
  var d = device || {};
  var v = gapVerdict(d);
  if (v.state === "gapped") {
    // behindBy 0 is the retention boundary: the device's position predates the
    // window but nothing between it and the floor was actually lost. Saying
    // "0 messages aged out" alongside a red chip reads as a contradiction, so
    // the boundary case names itself.
    if (!v.behindBy) return "gapped · at the retention boundary";
    return "gapped · " + v.behindBy + " message" + (v.behindBy === 1 ? "" : "s") + " aged out";
  }
  if (v.state === "current") return "within retention window";
  if (!streamKnown) return "gap unknown — SYNC stream unreadable";
  if (!d.subscribed) return "gap unknown — never attached to the stream";
  return "gap unknown";
}

// hydrationNote describes the device's last hydration checkpoint. This is the
// Refractor pipeline's own progress sequence, NOT a SYNC stream position — the
// two are different sequence spaces, so the label names which one it is rather
// than letting it read as a second, contradictory sync position.
function hydrationNote(device) {
  var d = device || {};
  if (!d.revisionCursor) return "";
  return "last hydrated at pipeline seq " + d.revisionCursor;
}

// interestSummary describes a device's Interest Set. An EMPTY filter is a
// wider subscription, not a narrower one — personalinterest's own rule is
// "absence is never a denial": no declared types and no declared anchors
// admits everything the identity is authorized for. Rendering that as "no
// interests" would invert its meaning.
function interestSummary(device) {
  var d = device || {};
  var types = d.types || [];
  var anchors = d.anchors || [];
  if (d.malformed) {
    // An unparseable registration document tells us nothing about its filter.
    // Falling through would assert the WIDEST possible subscription about a
    // document nobody could read — a security-relevant claim from no evidence.
    return "interest set unknown — registration document unreadable";
  }
  if (!types.length && !anchors.length) {
    return "unfiltered — receives everything this identity is authorized for";
  }
  var parts = [];
  if (types.length) parts.push(types.length + " type" + (types.length === 1 ? "" : "s") + ": " + types.join(", "));
  if (anchors.length) parts.push(anchors.length + " anchor" + (anchors.length === 1 ? "" : "s"));
  return parts.join(" · ");
}

// groupByIdentity collapses the flat device list into per-identity groups,
// preserving the server's gapped-first ordering: a group sorts to the position
// of its first device, so an identity with a gapped device stays at the top.
function groupByIdentity(devices) {
  var list = devices || [];
  var order = [];
  var byID = {};
  for (var i = 0; i < list.length; i++) {
    var d = list[i];
    var id = d.identityId || "";
    if (!byID[id]) {
      byID[id] = { identityId: id, identityKey: d.identityKey || "", devices: [], gapped: 0 };
      order.push(byID[id]);
    }
    byID[id].devices.push(d);
    if (d.gapped === true) byID[id].gapped++;
  }
  return order;
}

// fleetHeadline is the one-line status summary above the roster. It reports
// what is KNOWN and separately what is unmeasured, rather than folding the
// unmeasured into a healthy count.
function fleetHeadline(fleet) {
  var f = fleet || {};
  var count = f.count || 0;
  if (!count) return "No devices registered.";
  var parts = [
    count + " device" + (count === 1 ? "" : "s") +
      " across " + (f.identities || 0) + " identit" + ((f.identities || 0) === 1 ? "y" : "ies"),
  ];
  if (!f.streamKnown) {
    parts.push("gap state unknown for all (no readable SYNC stream)");
    return parts.join(" · ");
  }
  var unknown = f.unknown || 0;
  // "0 gapped" alongside N unmeasured devices reads as an all-clear it has not
  // earned, so an all-unknown fleet never prints a gapped count at all, and a
  // partially-measured one always carries the unmeasured remainder.
  if (unknown >= count) {
    parts.push("gap state unknown for all " + count);
    return parts.join(" · ");
  }
  parts.push((f.gapped || 0) + " gapped");
  if (unknown) parts.push(unknown + " unknown");
  var unsub = f.unsubscribed || 0;
  if (unsub) parts.push(unsub + " not attached to " + (f.stream || "the stream"));
  return parts.join(" · ");
}

// retentionLine describes the window every gap verdict is measured against.
// Returns "" when there is no stream to describe.
function retentionLine(fleet) {
  var f = fleet || {};
  if (!f.streamKnown) return "";
  var first = f.firstSeq || 0;
  var last = f.lastSeq || 0;
  var name = f.stream || "?";
  // An empty stream reports firstSeq = lastSeq + 1 in NATS, and a brand-new one
  // reports 0/0. Printing either as a range ("101–100", "0–0") reads as
  // corruption, so an empty window says it is empty instead.
  if (last < first || last === 0) {
    return "Stream " + name + " holds no messages, so no device can be behind its retention window yet.";
  }
  var held = last - first + 1;
  return "Stream " + name + " retains sequences " + first + "–" + last +
    " (" + held + " message" + (held === 1 ? "" : "s") + "). A device is gapped once its position falls below " + first + ".";
}

// filterEmptyMessage is what the roster says when the "gapped only" filter
// leaves nothing to show. An empty filtered list is NOT an all-clear when some
// devices' gap state could not be determined — those were hidden by the filter,
// not cleared by it, so the count of hidden unknowns is stated.
function filterEmptyMessage(devices) {
  var list = devices || [];
  var unknown = 0;
  for (var i = 0; i < list.length; i++) {
    var g = list[i].gapped;
    if (g === null || g === undefined) unknown++;
  }
  if (!unknown) return "(no gapped devices)";
  return "(no gapped devices — but " + unknown + " device" + (unknown === 1 ? "'s" : "s'") +
    " gap state could not be determined, so this is not an all-clear)";
}

// staleWarning is the standing caveat this roster always carries: registration
// is durable and nothing garbage-collects it, so a device that vanished
// without a clean deregister keeps its row forever. This is a REGISTRATION
// roster, never a liveness view — no connection state is observable to any
// component today.
function staleWarning(fleet) {
  var f = fleet || {};
  if (!(f.count || 0)) return "";
  return "Registrations are durable and never expire — a device that disappeared without deregistering still lists here. " +
    "This is who is registered, not who is connected; edge nodes cannot self-report and no connection state is observable.";
}

export { gapVerdict, gapLabel, hydrationNote, interestSummary, groupByIdentity, fleetHeadline, retentionLine, filterEmptyMessage, staleWarning };
