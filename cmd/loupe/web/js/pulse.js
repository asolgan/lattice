// The live-pulse client: one EventSource on /api/events/stream for the whole
// console, opened at boot and kept open across views (the op view's
// follow-through and the map feed share it). Owns the capped row ring buffer
// and the topbar LED; views subscribe for row/status callbacks and render
// their own DOM. The feed is a tail, not history — nothing persists, a refresh
// loses the rows by design.

import { $ } from "./api.js";
import { FEED_CAP, shapeEventRow, pushRows, rowsPerMin, pruneTimes, ledClass } from "./logic/feed.js";

const pulse = {
  es: null,
  status: "off",   // off | live | retry | error
  reason: "",      // the server-reported error, when status === "error"
  rows: [],        // newest first, capped at FEED_CAP
  times: [],       // arrival timestamps (ms) for the rows/min rate
  paused: false,   // pause stops appending; the stream stays open
  retryTimer: null, // manual reconnect timer (terminal failures only)
  listeners: new Set(),
};

// Manual-reconnect delays: a server-reported terminal error (capacity, NATS
// subscribe failure) backs off longer than a fatal transport response (a
// non-SSE reply, which EventSource never retries natively).
const RETRY_TERMINAL_MS = 15000;
const RETRY_FATAL_MS = 8000;

// subscribe registers a callback for feed changes; returns the unsubscribe.
// evt is {type:"row", row} per appended row (event or derived) or
// {type:"status"} on stream-state changes.
function subscribe(fn) {
  pulse.listeners.add(fn);
  return () => pulse.listeners.delete(fn);
}

function notify(evt) {
  pulse.listeners.forEach((fn) => { try { fn(evt); } catch (_) { /* a broken listener never kills the feed */ } });
}

function setStreamStatus(status, reason) {
  if (pulse.status === status && pulse.reason === (reason || "")) return;
  pulse.status = status;
  pulse.reason = reason || "";
  renderLED();
  notify({ type: "status" });
}

// append pushes rows through the shared ring buffer (unless paused) and
// notifies subscribers. Arrival times always count toward the rate — a paused
// feed still reports how busy the stream is.
function append(row) {
  pulse.times = pruneTimes(pulse.times, Date.now());
  pulse.times.push(Date.now());
  if (pulse.paused) return;
  pulse.rows = pushRows(pulse.rows, [row], FEED_CAP);
  notify({ type: "row", row });
}

// addDerived appends poll-derived rows (map poll diffs) stamped with the
// arrival time — they ride the same buffer as stream rows.
function addDerived(rows) {
  rows.forEach((r) => {
    r.time = new Date().toISOString().slice(11, 19);
    append(r);
  });
}

function setPaused(v) {
  pulse.paused = v;
  notify({ type: "status" });
}

function clearRows() {
  pulse.rows = [];
  notify({ type: "status" });
}

function connect() {
  if (pulse.es) return;
  const es = new EventSource("/api/events/stream");
  pulse.es = es;
  // "live" is gated on the server's hello event (a working tail), never on
  // the bare 200 handshake — a refused/failed connect must not blink green.
  es.addEventListener("hello", () => setStreamStatus("live"));
  // The server's named error event (subscribe failed / too many clients /
  // consumer death) — named streamError because the built-in transport event
  // owns type "error". These are terminal: the server closes after sending.
  // Close (stopping EventSource's ~3s native retry hammer) and reconnect on a
  // slow backoff so a freed slot / recovered NATS is picked up eventually.
  es.addEventListener("streamError", (e) => {
    let reason = "";
    try { reason = JSON.parse(e.data).error || ""; } catch (_) { reason = String(e.data); }
    setStreamStatus("error", reason);
    reconnectLater(RETRY_TERMINAL_MS);
  });
  es.onerror = () => {
    if (es !== pulse.es) return;
    if (es.readyState === EventSource.CLOSED) {
      // A fatal response (e.g. the 502 JSON while NATS is down at startup):
      // EventSource never retries natively — schedule our own reconnect, and
      // never label a dead-forever stream "retrying" without one.
      if (pulse.status !== "error") setStreamStatus("retry");
      reconnectLater(RETRY_FATAL_MS);
      return;
    }
    // A dropped connection: EventSource retries natively; rows are retained.
    // A server-reported error stays sticky through the retry cycle.
    if (pulse.status !== "error") setStreamStatus("retry");
  };
  es.onmessage = (e) => {
    let ev;
    try { ev = JSON.parse(e.data); } catch (_) { return; }
    if (pulse.status !== "live") setStreamStatus("live");
    append(shapeEventRow(ev));
  };
}

// reconnectLater tears the source down and schedules a fresh connect —
// EventSource's native retry only covers dropped connections, not terminal
// refusals or non-SSE responses.
function reconnectLater(ms) {
  if (pulse.es) { pulse.es.close(); pulse.es = null; }
  if (pulse.retryTimer) return;
  pulse.retryTimer = setTimeout(() => {
    pulse.retryTimer = null;
    connect();
  }, ms);
}

// renderLED mirrors the stream state on the topbar dot — visible from every
// view; clicking it lands on the map (the anchor's href).
function renderLED() {
  const led = $("#topbar-led");
  if (!led) return;
  led.className = "pulse-led " + ledClass(pulse.status);
  led.title = pulse.status === "error"
    ? "event stream: " + pulse.reason
    : "event stream: " + pulse.status;
}

function status() { return pulse.status; }
function reason() { return pulse.reason; }
function rows() { return pulse.rows; }
function paused() { return pulse.paused; }
function ratePerMin() { return rowsPerMin(pulse.times, Date.now()); }

function init() {
  connect();
  renderLED();
}

export { init, subscribe, addDerived, setPaused, clearRows, status, reason, rows, paused, ratePerMin };
