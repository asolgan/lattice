// Pure status lookups shared by the map and (later fires) component/lens
// pages. This module grows into the full §4 status vocabulary in F4; today it
// holds the classifier tables the system map renders from. No DOM, no fetch.

// componentStatusClass / lensDotClass map a backend status string to the CSS
// class that drives its color. Unknown statuses fall back to a neutral dot.
var componentStatusClass = {
  green: "green", stale: "stale", absent: "absent", unknown: "unknown",
  degraded: "yellow", unhealthy: "red",
};

var lensDotClass = {
  active: "green", yellow: "yellow", paused: "yellow", rebuilding: "yellow", unknown: "dim",
};

var lensGlyph = { paused: "⏸", rebuilding: "⟳" };

// sysmapControlComponents: components with a Control column a map click
// drills into (plain object, not Set — logic files stay ES5-friendly).
var sysmapControlComponents = { refractor: true, weaver: true, loom: true };

// issueClass colors a flattened "[severity] code: message" issue line: an
// [error] line is red, everything else (warnings, stale notes) stays yellow.
function issueClass(text) {
  return /^\[error\]/.test(text) ? "card-issue bad" : "card-issue";
}

// sysmapTier derives a node's tier (0..4) from its kind + id, never hardcoded
// x/y — so the layout survives backend node-set changes.
function sysmapTier(node) {
  if (node.kind === "lens") return 4;
  if (node.kind === "infra") {
    return node.id === "core-operations" ? 0 : 2; // core-kv / core-events = spine
  }
  // component
  return node.id === "processor" ? 1 : 3;
}

export { componentStatusClass, lensDotClass, lensGlyph, sysmapControlComponents, issueClass, sysmapTier };
