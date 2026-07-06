// Map-scrubber frame math (F13 §4.2's v1 tier — flow-liveness replay). Pure:
// positions (a flow list + a time window) in, frames out — the same
// "pure model, DOM paints" discipline as logic/hood.js. All timestamps are ms
// epoch numbers; the caller (map.js) converts /api/history/timeline's RFC3339
// strings once at the edge via Date.parse, never inside this module.

// liveAt returns the instanceIds of flows live at time t: started at or
// before t, and (if ended at all) not yet ended by t. A flow with an
// unparsable startedAt never counts as live (never a false positive from bad
// data); a flow with no endedAt is still running, so it counts as live for
// any t at or after its start — exactly "open on the right".
function liveAt(flows, t) {
  return (flows || []).filter((f) => {
    const s = Date.parse(f.startedAt);
    if (!isFinite(s) || s > t) return false;
    if (f.endedAt) {
      const e = Date.parse(f.endedAt);
      if (isFinite(e) && e <= t) return false;
    }
    return true;
  }).map((f) => f.instanceId);
}

// MAX_FRAMES bounds the sample count: a caller passing a step that's
// absurdly small relative to the window (a wrong-unit bug — seconds instead
// of ms — or a huge span) gets a clamped, still-useful track instead of
// freezing the tab in a synchronous loop.
const MAX_FRAMES = 2000;

// framesFromFlows samples liveAt across [from, to] every step ms — the
// scrubber's replay track. A non-positive step or an inverted/empty window
// yields no frames rather than looping forever or dividing by zero. Frame
// count is computed once as an integer (from the window span, not by
// accumulating `step` across iterations) so float rounding can't drift the
// sample count off the caller's intended N.
function framesFromFlows(flows, from, to, step) {
  if (!(step > 0) || !(to >= from)) return [];
  const n = Math.min(Math.floor((to - from) / step), MAX_FRAMES);
  const frames = [];
  for (let i = 0; i <= n; i++) {
    const t = from + i * step;
    const live = liveAt(flows, t);
    frames.push({ t: t, liveFlows: live, rollup: live.length });
  }
  return frames;
}

// clockLabel renders a frame's timestamp as the playhead clock (HH:MM:SS,
// UTC — matching the pulse feed's time format); an invalid t renders empty
// rather than "Invalid Date".
function clockLabel(t) {
  const d = new Date(t);
  if (isNaN(d.getTime())) return "";
  return d.toISOString().slice(11, 19);
}

// timelineWindow computes the default [from, to] the scrubber requests on
// entry: the trailing spanMs up to now. A non-positive spanMs collapses to a
// zero-width window at now (never negative-width).
function timelineWindow(nowMs, spanMs) {
  const span = spanMs > 0 ? spanMs : 0;
  return { from: nowMs - span, to: nowMs };
}

export { liveAt, framesFromFlows, clockLabel, timelineWindow };
