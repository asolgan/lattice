// Pure Gateway-page shaping: the auth-failure ratio, the JWKS key-set rows,
// the revocation-materializer status line, and the revoke-form input rules.
// No DOM, no fetch — goja-tested via cmd/loupe/web_logic_gateway_test.go
// (strip-export load, ES6-conservative).

// authFailureRate derives the security headline from the gateway heartbeat's
// cumulative counters: auth_failures_total / requests_total as a ratio.
// No traffic — or either counter missing/mistyped — renders "—" (pct null),
// never a fabricated 0% — the num() honesty convention. At or above 20%
// failing the panel yellows (a spike in forged/expired tokens is the thing
// an operator wants to see).
function authFailureRate(m) {
  if (!m || typeof m.requests_total !== "number" || typeof m.auth_failures_total !== "number") {
    return { pct: null, cls: "muted" };
  }
  if (m.requests_total <= 0) return { pct: null, cls: "muted" };
  var pct = m.auth_failures_total / m.requests_total;
  return { pct: pct, cls: pct >= 0.2 ? "warn" : "ok" };
}

// pctLabel renders an authFailureRate result for display: "—" when the ratio
// is unknown, else a percentage with one decimal; a nonzero ratio that would
// round to 0% shows "<0.1%" (never "0%" while failures exist).
function pctLabel(rate) {
  if (!rate || rate.pct === null || rate.pct === undefined) return "—";
  var rounded = Math.round(rate.pct * 1000) / 10;
  if (rounded === 0 && rate.pct > 0) return "<0.1%";
  return rounded + "%";
}

// jwksRows flattens the gateway heartbeat's `jwks` block into sorted table
// rows + the poll line. Returns null when the heartbeat does not carry a
// well-formed block — the panel renders its designed empty state ("not
// reported by this Gateway build") so a heartbeat without the block never
// fabricates key-set claims.
function jwksRows(doc) {
  var j = doc && doc.jwks;
  if (!j || typeof j !== "object" || Array.isArray(j)) return null;
  var keys = [];
  var src = Array.isArray(j.keys) ? j.keys : [];
  for (var i = 0; i < src.length; i++) {
    var k = src[i] || {};
    keys.push({
      kid: String(k.kid || "(no kid)"),
      source: String(k.source || "?"),
      alg: String(k.alg || "?"),
      addedAt: String(k.addedAt || ""),
    });
  }
  keys.sort(function (a, b) { return a.kid < b.kid ? -1 : a.kid > b.kid ? 1 : 0; });

  // The poll line: url-sourced sets show poll freshness + health; static
  // (dir/dev) sets say so — a restart is the only rotation for those.
  var poll = { line: "static key set (restart to rotate)", cls: "muted" };
  var lp = j.lastPoll;
  if (lp && typeof lp === "object" && lp.at) {
    poll = {
      line: "JWKS polled " + String(lp.at) + (lp.source ? " from " + String(lp.source) : ""),
      cls: lp.ok === false ? "warn" : "ok",
    };
    if (lp.ok === false) poll.line += " — last poll failed (serving last-known-good)";
  }

  var swaps = [];
  var sw = Array.isArray(j.swaps) ? j.swaps : [];
  for (var s = Math.max(0, sw.length - 10); s < sw.length; s++) {
    swaps.push(String(sw[s]));
  }
  swaps.reverse();
  return { keys: keys, poll: poll, swaps: swaps };
}

// revocationStatus shapes the heartbeat's `revocation` block (the kill-switch
// materializer's live state) into the revoke panel's status line. A missing
// block means the Gateway build predates the kill-switch — say so rather than
// showing zeros. A disconnected materializer is the §2.6 fail-safe half: the
// bucket may lag new revocations, so the line warns.
function revocationStatus(doc) {
  var r = doc && doc.revocation;
  if (!r || typeof r !== "object" || Array.isArray(r)) {
    return { line: "revocation state not reported by this Gateway build", cls: "muted", connected: false };
  }
  var connected = r.consumerConnected === true;
  var line = (connected ? "materializer connected" : "materializer DISCONNECTED — new revocations may lag") +
    " · " + (typeof r.revokedCount === "number" ? r.revokedCount : "?") + " revoked";
  if (r.lastSyncAt) line += " · last sync " + String(r.lastSyncAt);
  return { line: line, cls: connected ? "ok" : "warn", connected: connected };
}

// revokeActorValid gates the Revoke button: the kill-switch keys on full
// identity vertex keys (vtx.identity.<id>). The id segment is restricted to
// the NanoID alphabet — a looser string still commits (the op only checks the
// prefix), but the Gateway materializer's KVPut refuses a key outside the
// NATS KV charset and would redeliver that event forever, so the console
// must never submit one.
function revokeActorValid(v) {
  if (typeof v !== "string") return false;
  var segs = v.trim().split(".");
  return segs.length === 3 && segs[0] === "vtx" && segs[1] === "identity" &&
    /^[A-Za-z0-9_-]+$/.test(segs[2]);
}

// revokeConfirmReady enables the typed-confirm Revoke: the input must exactly
// match the actor key being revoked (same rule as the lens typed delete).
function revokeConfirmReady(input, actor) {
  return typeof input === "string" && typeof actor === "string" &&
    actor !== "" && input.trim() === actor;
}

export { authFailureRate, pctLabel, jwksRows, revocationStatus, revokeActorValid, revokeConfirmReady };
