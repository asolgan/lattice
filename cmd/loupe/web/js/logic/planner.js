// Pure Weaver planner-diagnostics shaping: the contraction trajectory roll-up,
// the shadow-comparison rows, the admission-control pacing line, the effect
// mismatch counter, and the planner-issue split. No DOM, no fetch — goja-tested
// via cmd/loupe/web_logic_planner_test.go (strip-export load, ES6-conservative).
//
// Every reader here follows the console's honesty convention: a diagnostic the
// heartbeat does not carry reads as unknown (null), never as a fabricated zero.
// That distinction is load-bearing for this panel — the Weaver omits
// `plannerShadow`, `contractionTrajectory` and the admission counters entirely
// when nothing has been recorded, so "absent" means "no comparisons yet", not
// "no divergence".

// contractionRoll classifies the per-target contraction trajectories into the
// panel's exception-first buckets. Returns null when the heartbeat carries no
// trajectory map (no target has been sampled yet). `diverging` is the alarming
// bucket — a target whose open-gap count only ever grows is remediating slower
// than it is falling behind.
function contractionRoll(m) {
  var t = m && m.contractionTrajectory;
  if (!t || typeof t !== "object" || Array.isArray(t)) return null;
  var ids = Object.keys(t);
  if (!ids.length) return null;
  var roll = { diverging: [], shrinking: [], steady: 0, total: 0 };
  ids.sort();
  for (var i = 0; i < ids.length; i++) {
    var v = t[ids[i]];
    roll.total++;
    if (v === "diverging") roll.diverging.push(ids[i]);
    else if (v === "shrinking") roll.shrinking.push(ids[i]);
    else roll.steady++;
  }
  return roll;
}

// divergenceRate is diverge / (agree + diverge) — null when the target has
// recorded no comparisons at all, so a freshly-registered shadow target reads
// "—" rather than a green 0%.
function divergenceRate(agree, diverge) {
  var total = agree + diverge;
  if (total <= 0) return null;
  return diverge / total;
}

// shadowRows flattens the heartbeat's `plannerShadow` block into per-target
// rows, worst-first (most divergences, then highest rate, then target id).
// Returns null when the block is absent — the planner has ranked nothing, which
// is the normal state for a deployment with no mode:"shadow" target.
//
// These counters are DIAGNOSTIC ONLY: the shadow comparison never altered what
// was dispatched (internal/weaver/planner_shadow.go). The caller must label
// them as such — a high diverge count is a signal to investigate the candidate
// ranking, not an incident.
function shadowRows(m) {
  var s = m && m.plannerShadow;
  if (!s || typeof s !== "object" || Array.isArray(s)) return null;
  var ids = Object.keys(s);
  if (!ids.length) return null;
  var rows = [];
  for (var i = 0; i < ids.length; i++) {
    var t = s[ids[i]] || {};
    var agree = typeof t.agree === "number" ? t.agree : 0;
    var diverge = typeof t.diverge === "number" ? t.diverge : 0;
    var recent = [];
    var src = Array.isArray(t.recentDivergences) ? t.recentDivergences : [];
    for (var j = 0; j < src.length; j++) {
      var d = src[j] || {};
      recent.push({
        gapColumn: String(d.gapColumn || "?"),
        entityId: String(d.entityId || "?"),
        pickedRef: String(d.pickedRef || "(none)"),
        actualRef: String(d.actualRef || "(none)"),
        at: String(d.at || ""),
      });
    }
    // Newest divergence first — the ring is appended in occurrence order.
    recent.reverse();
    rows.push({
      targetId: ids[i],
      agree: agree,
      diverge: diverge,
      rate: divergenceRate(agree, diverge),
      recent: recent,
    });
  }
  rows.sort(function (a, b) {
    if (a.diverge !== b.diverge) return b.diverge - a.diverge;
    var ra = a.rate === null ? -1 : a.rate;
    var rb = b.rate === null ? -1 : b.rate;
    if (ra !== rb) return rb - ra;
    return a.targetId < b.targetId ? -1 : a.targetId > b.targetId ? 1 : 0;
  });
  return rows;
}

// admissionState derives the token-bucket pacing line. Returns null when the
// heartbeat carries neither counter — the Weaver omits both until a target
// declares an `admission` block, so absent means "no target is governed", not
// "nothing was deferred". A deferred share at or above 20% yellows: admission
// is meant to pace a burst, and steady heavy deferral means the declared rate
// is below what the target actually needs.
function admissionState(m) {
  if (!m) return null;
  var admitted = typeof m.admissionAdmitted === "number" ? m.admissionAdmitted : null;
  var deferred = typeof m.admissionDeferred === "number" ? m.admissionDeferred : null;
  if (admitted === null && deferred === null) return null;
  admitted = admitted === null ? 0 : admitted;
  deferred = deferred === null ? 0 : deferred;
  var total = admitted + deferred;
  var rate = total > 0 ? deferred / total : null;
  return {
    admitted: admitted,
    deferred: deferred,
    rate: rate,
    cls: rate !== null && rate >= 0.2 ? "warn" : "ok",
  };
}

// effectMismatchCount reads the scan counter. Absent means the mismatch scan
// itself failed this tick (the Weaver logs and skips the key) — reported as
// null so the panel can say "scan unavailable" instead of a clean zero.
function effectMismatchCount(m) {
  return m && typeof m.effectMismatches === "number" ? m.effectMismatches : null;
}

// plannerIssues splits an instance's STRUCTURED heartbeat issues (doc.issues —
// which carry code/severity/since, unlike the server's flattened display
// strings) into the panel's planner-owned buckets. Messages are passed through
// verbatim: the Weaver authors them for exactly this operator surface, and
// parsing the target pair back out of the oscillation text would be brittle.
function plannerIssues(doc) {
  var out = { oscillation: [], effectMismatch: [] };
  var raw = doc && doc.issues;
  if (!Array.isArray(raw)) return out;
  for (var i = 0; i < raw.length; i++) {
    var it = raw[i] || {};
    var row = {
      code: String(it.code || ""),
      severity: String(it.severity || ""),
      message: String(it.message || ""),
      since: String(it.since || ""),
    };
    if (row.code === "TargetOscillation") out.oscillation.push(row);
    else if (row.code === "LensEffectMismatch") out.effectMismatch.push(row);
  }
  return out;
}

// plannerPanel assembles one instance's whole planner section. `active` is
// false when the instance reports no planner signal whatsoever — the panel then
// renders a single quiet "nothing recorded" line instead of five empty boxes
// (the expected shape on a kernel-only stack with no planned/shadow target).
function plannerPanel(inst) {
  var doc = (inst && inst.doc) || {};
  var m = doc.metrics || {};
  var issues = plannerIssues(doc);
  var panel = {
    instance: String((inst && inst.instance) || ""),
    contraction: contractionRoll(m),
    shadow: shadowRows(m),
    admission: admissionState(m),
    effectMismatches: effectMismatchCount(m),
    oscillation: issues.oscillation,
    effectMismatchIssues: issues.effectMismatch,
  };
  panel.active = !!(panel.contraction || panel.shadow || panel.admission ||
    panel.oscillation.length || panel.effectMismatchIssues.length ||
    (typeof panel.effectMismatches === "number" && panel.effectMismatches > 0));
  return panel;
}

// plannerPanels builds one panel per live instance. The shadow, contraction and
// admission counters are per-PROCESS in-memory state that resets on restart
// (internal/weaver/planner_shadow.go), so they are never merged across
// instances — a summed fleet-wide "diverge" would be a number no process
// actually holds.
function plannerPanels(instances) {
  var out = [];
  var list = instances || [];
  for (var i = 0; i < list.length; i++) out.push(plannerPanel(list[i]));
  return out;
}

export { contractionRoll, divergenceRate, shadowRows, admissionState, effectMismatchCount, plannerIssues, plannerPanel, plannerPanels };
