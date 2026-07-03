// Pure Submit-Op reply/log logic: shaping an OperationReply into the
// structured accepted panel, the sessionStorage-backed session op log, and the
// follow-through row filter. No DOM, no fetch, no storage — the goja harness
// drives these tables.

// OPLOG_CAP bounds the session op log (design §10).
var OPLOG_CAP = 50;

// FOLLOW_WINDOW_MS is how long the follow-through section rides the pulse
// after an accepted reply (design §10: "~12s").
var FOLLOW_WINDOW_MS = 12000;

// shapeReply shapes a POST /api/op reply for rendering. structured is true for
// the non-error statuses (accepted, duplicate) — those render the structured
// panel; a rejected/failed/transport-error reply keeps the verbatim error
// rendering. keys is the committed key set (from revisions) ordered primaryKey
// first, then lexicographic, each {key, revision, primary}.
function shapeReply(reply) {
  var r = reply || {};
  var status = r.status || (r.error ? "error" : "");
  var structured = status === "accepted" || status === "duplicate";
  var keys = [];
  if (r.revisions) {
    var names = [];
    for (var k in r.revisions) names.push(k);
    names.sort();
    var i;
    for (i = 0; i < names.length; i++) {
      if (names[i] === r.primaryKey) { names.splice(i, 1); break; }
    }
    if (r.primaryKey && r.revisions[r.primaryKey] !== undefined) names.unshift(r.primaryKey);
    for (i = 0; i < names.length; i++) {
      keys.push({ key: names[i], revision: r.revisions[names[i]], primary: names[i] === r.primaryKey });
    }
  }
  return {
    structured: structured,
    status: status,
    statusLine: status + (r.decision ? " · " + r.decision : ""),
    committedAt: r.committedAt || r.originalCommittedAt || "",
    opTrackerKey: r.opTrackerKey || "",
    requestId: r.requestId || "",
    primaryKey: r.primaryKey || "",
    keys: keys,
  };
}

// logEntry builds one session-op-log row from a submit outcome. time is the
// caller-stamped HH:MM:SS display time (this module has no clock). A transport
// error ({error} body, no reply envelope) logs with status "error" and no
// links.
function logEntry(reply, operationType, time) {
  var r = reply || {};
  return {
    time: time || "",
    operationType: operationType || "",
    status: r.status || (r.error ? "error" : "?"),
    opTrackerKey: r.opTrackerKey || "",
    primaryKey: r.primaryKey || "",
  };
}

// pushLog prepends entry (newest first) and caps the log.
function pushLog(entries, entry, cap) {
  return [entry].concat(entries || []).slice(0, cap);
}

// followMatch decides whether a pulse feed row belongs in the follow-through
// section for opKey (the submitted op's tracker key, vtx.op.<requestId>):
// event rows must carry that opKey; poll-derived rows (lens/component
// transitions, §8.2) carry no requestId, so any arriving inside the window is
// shown — marked "~" by the renderer like every derived row.
function followMatch(row, opKey) {
  var r = row || {};
  if (r.kind === "event") return !!opKey && r.opKey === opKey;
  return r.kind === "derived";
}

export { OPLOG_CAP, FOLLOW_WINDOW_MS, shapeReply, logEntry, pushLog, followMatch };
