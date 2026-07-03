// Pure pulse-feed logic: event→row shaping, the capped ring buffer, the
// poll-diff derivation (state transitions + rule updates between /api/systemmap
// refreshes), and the rows/min rate. No DOM, no fetch, no timers — the goja
// harness drives these tables.

// FEED_CAP bounds the ring buffer (design §8.2).
var FEED_CAP = 200;

// feedTime extracts the HH:MM:SS display time from an RFC3339 timestamp
// (UTC, matching every other timestamp the console renders). A malformed
// timestamp renders empty, never throws.
function feedTime(ts) {
  if (typeof ts !== "string") return "";
  var i = ts.indexOf("T");
  if (i < 0 || ts.length < i + 9) return "";
  return ts.slice(i + 1, i + 9);
}

// shapeEventRow shapes one SSE message (the parsed feedEvent JSON) into a feed
// row. requestId resolves to the op tracker vertex (vtx.op.<requestId>) — the
// Processor's idempotency tracker key shape.
function shapeEventRow(ev) {
  var e = ev || {};
  return {
    kind: "event",
    time: feedTime(e.timestamp),
    eventType: e.eventType || "",
    targetKey: e.targetKey || "",
    opKey: e.requestId ? "vtx.op." + e.requestId : "",
    dropped: e.dropped || 0,
  };
}

// pushRows prepends incoming rows (newest first) and caps the buffer.
function pushRows(rows, incoming, cap) {
  return incoming.concat(rows).slice(0, cap);
}

// deriveTransitions diffs two /api/systemmap node lists (previous poll →
// current) into derived feed rows: status transitions on lens / component /
// client nodes, and lens rule updates (activeSequence advancing — the NATS
// sequence of the active RULE VERSION; it moves on rule activation/update,
// not on row projection). Nodes new to this poll have no previous truth and
// derive nothing. Rows are poll-derived (≤ the poll interval of lag), which is
// why the renderer marks them "~".
function deriveTransitions(prevNodes, nodes) {
  var out = [];
  if (!prevNodes || !prevNodes.length || !nodes) return out;
  var prev = {};
  var i;
  for (i = 0; i < prevNodes.length; i++) prev[prevNodes[i].id] = prevNodes[i];
  for (i = 0; i < nodes.length; i++) {
    var n = nodes[i];
    if (n.kind !== "lens" && n.kind !== "component" && n.kind !== "client") continue;
    var p = prev[n.id];
    if (!p) continue;
    var href = n.kind === "lens" ? "#/lens/" + n.id : "#/component/" + n.id;
    if (p.status !== n.status) {
      out.push({
        kind: "derived",
        text: (n.label || n.id) + " " + (p.status || "?") + " → " + (n.status || "?"),
        href: href,
      });
    }
    // Any sequence movement derives, including a lens's FIRST rule activation
    // (0/absent → N) — both sides falsy means no rule truth on either poll.
    if (n.kind === "lens" && (p.activeSequence || n.activeSequence) &&
        p.activeSequence !== n.activeSequence) {
      out.push({
        kind: "derived",
        text: (n.label || n.id) + " rule updated (seq " + (p.activeSequence || 0) + " → " + (n.activeSequence || 0) + ")",
        href: href,
      });
    }
  }
  return out;
}

// rowsPerMin counts arrival timestamps (ms) within the trailing minute.
function rowsPerMin(times, nowMs) {
  var c = 0;
  for (var i = 0; i < times.length; i++) {
    if (nowMs - times[i] <= 60000) c++;
  }
  return c;
}

// pruneTimes drops arrival timestamps older than the trailing minute so the
// list never grows unbounded on a chatty stream.
function pruneTimes(times, nowMs) {
  var out = [];
  for (var i = 0; i < times.length; i++) {
    if (nowMs - times[i] <= 60000) out.push(times[i]);
  }
  return out;
}

// ledClass maps the stream state to the LED color class (§8.4): connected is
// green, an EventSource retry is yellow, a server-reported error is red, and
// off/idle is dim.
function ledClass(status) {
  if (status === "live") return "green";
  if (status === "retry") return "yellow";
  if (status === "error") return "red";
  return "dim";
}

export { FEED_CAP, feedTime, shapeEventRow, pushRows, deriveTransitions, rowsPerMin, pruneTimes, ledClass };
