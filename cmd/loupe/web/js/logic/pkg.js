// Pure package-view logic: the upload manifest pick (the JS twin of the Go
// manifestFromUpload rule), the one-line apply-reply summary, and the
// uninstall-confirm summary. No DOM, no fetch — goja-tested via
// cmd/loupe/web_logic_test.go (strip-export load).

// manifestCandidate picks the manifest out of a selected file list by name:
// an exact manifest.yaml / manifest.yml wins (case-insensitive); a single
// file of any name is accepted; anything else is ambiguous (null).
function manifestCandidate(names) {
  var list = names || [];
  for (var i = 0; i < list.length; i++) {
    var n = String(list[i] || "").toLowerCase();
    if (n === "manifest.yaml" || n === "manifest.yml") return list[i];
  }
  if (list.length === 1) return list[0];
  return null;
}

// applySummaryLine renders an install/upgrade reply as one human line:
// "preview — upgrade 1.0.0 → 1.1.0 — 3 created · 2 updated".
function applySummaryLine(res) {
  if (!res) return "";
  var prefix = res.dryRun ? "preview — " : "";
  if (res.skipped) {
    return prefix + "skipped — " + (res.reason || "already installed at this version");
  }
  var counts = [];
  if (res.created) counts.push(res.created + " created");
  if (res.updated) counts.push(res.updated + " updated");
  if (res.tombstoned) counts.push(res.tombstoned + " tombstoned");
  var delta = counts.length ? counts.join(" · ") : "no changes";
  if (res.action === "upgrade") {
    return prefix + "upgrade " + (res.fromVersion || "?") + " → " + (res.toVersion || "?") + " — " + delta;
  }
  if (res.action === "install") {
    return prefix + "install" + (res.toVersion ? " v" + res.toVersion : "") + " — " + delta;
  }
  return prefix + (res.action || "apply") + " — " + delta;
}

// uninstallSummary tells the operator what an uninstall will tombstone: every
// declared key that still resolves, plus the manifest aspect and the package
// vertex itself (the server appends both), minus unresolved declared keys
// (the server skips those). The per-kind breakdown counts declared ITEMS
// (aspects fold into their parent), so it reads as a contents summary, not a
// key count.
function uninstallSummary(pkg) {
  var p = pkg || {};
  var declared = p.declaredCount || 0;
  var unresolved = p.unresolved || 0;
  var total = declared - unresolved + 2; // + the manifest aspect + the package vertex
  var parts = [];
  var list = p.sections || [];
  for (var i = 0; i < list.length; i++) {
    if (list[i].count) parts.push(list[i].count + " " + list[i].kind);
  }
  var line = "tombstones up to " + total + " key(s) incl. the manifest + package vertex";
  if (parts.length) line += " — " + parts.join(" · ");
  if (unresolved) line += "; " + unresolved + " unresolved skipped";
  return line;
}

export { manifestCandidate, applySummaryLine, uninstallSummary };
