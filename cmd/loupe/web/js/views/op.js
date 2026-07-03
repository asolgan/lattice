// Submit Op view: the op catalog picker, the schema-driven form, submit, the
// structured accepted-reply panel, the ~12s follow-through riding the shared
// pulse stream, and the sessionStorage-backed session op log. Field coercion +
// reads derivation are pure (logic/reads.js); reply shaping + log mechanics
// are pure (logic/oplog.js).

import { $, $all, el, pretty, api, setStatus } from "../api.js";
import { deriveReads, coerceField, schemaTypeLabel } from "../logic/reads.js";
import { OPLOG_CAP, FOLLOW_WINDOW_MS, shapeReply, logEntry, pushLog, followMatch } from "../logic/oplog.js";
import { keyLinkEl } from "../render.js";
import * as pulse from "../pulse.js";

const state = { loaded: false };

// opCatalog maps an operationType to its group (service), input schema, and
// description, built from GET /api/ops. The op picker drives a schema form.
let opCatalog = {};

// enter handles the #/op route; ?type= pre-fills the picker (the Tasks view's
// "Complete →" path — URL-carried, so a prefill link is shareable).
function enter(route) {
  if (!state.loaded) {
    state.loaded = true;
    loadOps();
    renderLog();
  }
  const t = route && route.params && route.params.type;
  if (t) prefillOp(t);
}

// leave stops an in-flight follow-through window — the subscription renders
// into this view's DOM, so it must not outlive the route.
function leave() {
  stopFollow();
}

// pendingPrefill holds a prefill requested before the catalog loaded; loadOps
// applies it once the picker has real options.
let pendingPrefill = null;

async function loadOps() {
  setStatus("op-catalog-status", "loading…");
  const sel = $("#op-select");
  const body = await api("/api/ops");
  if (body.error) { setStatus("op-catalog-status", body.error, true); return; }
  opCatalog = {};
  sel.innerHTML = '<option value="">— choose an operation —</option>';
  (body.groups || []).forEach((g) => {
    const og = document.createElement("optgroup");
    og.label = g.name + (g.commands.length > 1 ? " (" + g.commands.length + ")" : "");
    (g.commands || []).forEach((cmd) => {
      opCatalog[cmd] = { group: g.name, schema: g.inputSchema || null, description: g.description || "" };
      const opt = el("option", null, cmd);
      opt.value = cmd;
      og.appendChild(opt);
    });
    sel.appendChild(og);
  });
  setStatus("op-catalog-status", (body.count || 0) + " service(s), " + Object.keys(opCatalog).length + " ops");
  if (pendingPrefill) {
    const name = pendingPrefill;
    pendingPrefill = null;
    applyPrefill(name);
  }
}

// prefillOp pre-selects opName in the picker (or fills the operationType
// override when it is not a catalog command). Before the catalog has loaded
// the prefill is queued, so a catalog command never silently lands in the raw
// override field.
function prefillOp(opName) {
  if (!Object.keys(opCatalog).length) {
    pendingPrefill = opName;
    setStatus("op-status", "pre-filling…");
    return;
  }
  applyPrefill(opName);
}

function applyPrefill(opName) {
  const sel = $("#op-select");
  if (sel && Array.from(sel.options).some((o) => o.value === opName)) {
    sel.value = opName;
    sel.dispatchEvent(new Event("change"));
  } else {
    const type = $("#op-type");
    if (type) type.value = opName;
  }
  setStatus("op-status", "pre-filled — complete the fields and submit");
}

// renderOpForm builds one input per top-level property of a JSON-Schema object.
function renderOpForm(schema) {
  const host = $("#op-fields");
  host.innerHTML = "";
  if (!schema || schema.type !== "object" || !schema.properties) {
    host.appendChild(el("div", "muted small",
      "(no field schema for this op — use the raw payload under Advanced)"));
    return;
  }
  const required = new Set(schema.required || []);
  Object.keys(schema.properties).forEach((name) => {
    const p = schema.properties[name] || {};
    const isReq = required.has(name);
    const wrap = el("label", "op-field");
    const head = el("span", "op-field-name", name + (isReq ? " *" : ""));
    head.appendChild(el("span", "op-field-type", schemaTypeLabel(p)));
    wrap.appendChild(head);
    wrap.appendChild(buildInput(name, p, isReq));
    if (p.description) wrap.appendChild(el("span", "op-field-desc", p.description));
    host.appendChild(wrap);
  });
}

// buildInput maps a JSON-Schema property to a form control, tagging it with
// the field name + type so collectOpForm can coerce the value back.
function buildInput(name, p, isReq) {
  const type = Array.isArray(p.type) ? p.type[0] : p.type;
  let input;
  if (p.enum) {
    input = document.createElement("select");
    if (!isReq) input.appendChild(el("option", null, ""));
    p.enum.forEach((v) => { const o = el("option", null, String(v)); o.value = String(v); input.appendChild(o); });
  } else if (type === "boolean") {
    input = document.createElement("input"); input.type = "checkbox";
  } else if (type === "integer" || type === "number") {
    input = document.createElement("input"); input.type = "number";
  } else if (type === "array" || type === "object") {
    input = document.createElement("textarea"); input.rows = 3;
    input.placeholder = (type === "array" ? "[ … ]" : "{ … }") + " JSON";
  } else {
    input = document.createElement("input"); input.type = "text";
  }
  input.dataset.field = name;
  input.dataset.type = type || "string";
  if (isReq) input.dataset.required = "1";
  return input;
}

// collectOpForm reads the rendered fields into a payload object. Empty
// optional fields are omitted; numbers/booleans/JSON are coerced via the pure
// coerceField. Throws on a malformed JSON field or a missing required field.
function collectOpForm() {
  const out = {};
  $all("#op-fields [data-field]").forEach((inp) => {
    const name = inp.dataset.field, type = inp.dataset.type, req = inp.dataset.required;
    if (type === "boolean") {
      if (inp.checked) out[name] = true; else if (req) out[name] = false;
      return;
    }
    const r = coerceField(name, type, inp.value, !!req);
    if (!r.omit) out[name] = r.value;
  });
  return out;
}

async function submitOp() {
  const override = $("#op-type").value.trim();
  const operationType = override || $("#op-select").value;
  const lane = $("#op-lane").value;
  const klass = $("#op-class").value.trim();
  const rawPayload = $("#op-payload").value.trim();
  const reply = $("#op-reply");

  if (!operationType) { setStatus("op-status", "choose an operation (or set an override)", true); return; }

  let payload;
  if (rawPayload) {
    try { payload = JSON.parse(rawPayload); }
    catch (e) { setStatus("op-status", "raw payload is not valid JSON: " + e.message, true); return; }
  } else {
    try { payload = collectOpForm(); }
    catch (e) { setStatus("op-status", e.message, true); return; }
  }

  setStatus("op-status", "submitting…");
  stopFollow();
  reply.textContent = "";
  reply.className = "";
  $("#op-accepted").innerHTML = "";
  $("#op-follow").innerHTML = "";
  const manualReads = $("#op-reads").value.split(/[\s,]+/).map((s) => s.trim()).filter(Boolean);
  const reads = deriveReads(payload).concat(manualReads);
  const req = { operationType, lane, payload };
  if (klass) req.class = klass;
  if (reads.length) req.reads = reads;
  const body = await api("/api/op", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });

  appendLog(logEntry(body, operationType, new Date().toISOString().slice(11, 19)));

  const shaped = shapeReply(body);
  if (!shaped.structured) {
    // Rejected / transport error: the verbatim red rendering.
    reply.textContent = pretty(body);
    setStatus("op-status", shaped.status || "error", true);
    reply.className = "error-text";
    return;
  }
  setStatus("op-status", shaped.status);
  renderAccepted(shaped, body);
  startFollow(shaped.opTrackerKey);
}

// renderAccepted renders the structured reply panel (§10): status line,
// committed keys as links (primaryKey first + highlighted) with their
// revisions, the op-tracker chip, committedAt — the raw reply collapsed below.
function renderAccepted(shaped, body) {
  const host = $("#op-accepted");
  host.innerHTML = "";
  const panel = el("div", "opreply");

  const head = el("div", "opreply-head");
  head.appendChild(el("span", "opreply-status ok-text", shaped.statusLine));
  if (shaped.committedAt) head.appendChild(el("span", "muted small", shaped.committedAt));
  panel.appendChild(head);

  if (shaped.keys.length) {
    panel.appendChild(el("div", "vtx-section-head", "committed keys (" + shaped.keys.length + ")"));
    shaped.keys.forEach((k) => {
      const row = el("div", "opreply-key" + (k.primary ? " opreply-key-primary" : ""));
      row.appendChild(keyLinkEl(k.key));
      row.appendChild(el("span", "muted small", "rev " + k.revision));
      if (k.primary) row.appendChild(el("span", "state-tag", "primary"));
      panel.appendChild(row);
    });
  }

  if (shaped.opTrackerKey) {
    const meta = el("div", "opreply-meta prov-chips");
    const m = el("span", "prov-chip");
    m.appendChild(el("span", "prov-k", "op tracker"));
    m.appendChild(keyLinkEl(shaped.opTrackerKey));
    meta.appendChild(m);
    panel.appendChild(meta);
  }

  const raw = document.createElement("details");
  raw.appendChild(el("summary", "muted small", "raw reply"));
  const pre = document.createElement("pre");
  pre.textContent = pretty(body);
  raw.appendChild(pre);
  panel.appendChild(raw);

  host.appendChild(panel);
}

// --- follow-through: ~12s of the shared pulse, filtered to this op ---------

const follow = { unsub: null, timer: null };

function stopFollow() {
  if (follow.unsub) { follow.unsub(); follow.unsub = null; }
  if (follow.timer) { clearTimeout(follow.timer); follow.timer = null; }
}

// startFollow renders the "what happened next" section and rides the shared
// pulse stream for FOLLOW_WINDOW_MS, appending this op's emitted events
// (matched by tracker key) and any poll-derived transition rows. Degrades to
// an honest notice when the stream is not live — the committed-key links above
// work regardless.
function startFollow(opKey) {
  stopFollow();
  const host = $("#op-follow");
  host.innerHTML = "";
  host.appendChild(el("div", "vtx-section-head",
    "what happened next (~" + Math.round(FOLLOW_WINDOW_MS / 1000) + "s window)"));
  const list = el("div", "opfollow-rows");
  host.appendChild(list);

  if (pulse.status() !== "live") {
    list.appendChild(el("div", "muted small", "live follow-through unavailable (event stream disconnected)"));
    return;
  }
  const empty = el("div", "muted small", "listening…");
  list.appendChild(empty);
  let got = 0;

  follow.unsub = pulse.subscribe((evt) => {
    if (evt.type === "status") {
      if (pulse.status() !== "live") {
        stopFollow();
        empty.remove();
        list.appendChild(el("div", "muted small", "live follow-through unavailable (event stream disconnected)"));
      }
      return;
    }
    if (evt.type !== "row" || !followMatch(evt.row, opKey)) return;
    if (!got++) empty.remove();
    list.appendChild(followRowEl(evt.row));
  });
  follow.timer = setTimeout(() => {
    stopFollow();
    empty.remove();
    list.appendChild(el("div", "muted small",
      got ? "(window closed)" : "(no events in the window — the feed is a live tail, not history)"));
  }, FOLLOW_WINDOW_MS);
}

// followRowEl renders one follow-through row, mirroring the map feed's idiom:
// events verbatim, derived rows prefixed "~" (poll-lagged).
function followRowEl(row) {
  const r = el("div", "opfollow-row");
  r.appendChild(el("span", "muted small", row.time || ""));
  if (row.kind === "event") {
    r.appendChild(el("span", "opfollow-type", row.eventType));
    if (row.targetKey) r.appendChild(keyLinkEl(row.targetKey));
  } else {
    const a = el("a", "opfollow-derived", "~ " + row.text);
    if (row.href) a.href = row.href;
    r.appendChild(a);
  }
  return r;
}

// --- session op log: sessionStorage-backed, dies with the tab --------------

const OPLOG_STORE = "loupe.oplog";

function readLog() {
  try {
    const v = JSON.parse(sessionStorage.getItem(OPLOG_STORE) || "[]");
    return Array.isArray(v) ? v : [];
  } catch (_) { return []; }
}

function appendLog(entry) {
  const entries = pushLog(readLog(), entry, OPLOG_CAP);
  try { sessionStorage.setItem(OPLOG_STORE, JSON.stringify(entries)); }
  catch (_) { /* storage denied/full — the log is best-effort */ }
  renderLog();
}

function clearLog() {
  try { sessionStorage.removeItem(OPLOG_STORE); }
  catch (_) { /* best-effort */ }
  renderLog();
}

// renderLog renders the session op log rows: time · operationType · status
// chip · op-tracker link · primaryKey link. The log survives route changes and
// dies with the tab (deliberately — durable op history is the platform's, not
// Loupe's).
function renderLog() {
  const host = $("#op-log");
  host.innerHTML = "";
  const entries = readLog();
  if (!entries.length) {
    host.appendChild(el("div", "muted small", "(no ops submitted this session)"));
    return;
  }
  entries.forEach((e) => {
    const row = el("div", "oplog-row");
    row.appendChild(el("span", "muted small", e.time));
    row.appendChild(el("span", "oplog-type", e.operationType));
    row.appendChild(el("span",
      "state-tag " + (e.status === "accepted" || e.status === "duplicate" ? "oplog-ok" : "oplog-err"),
      e.status));
    if (e.opTrackerKey) row.appendChild(keyLinkEl(e.opTrackerKey));
    if (e.primaryKey) row.appendChild(keyLinkEl(e.primaryKey));
    host.appendChild(row);
  });
}

function init() {
  $("#op-select").addEventListener("change", () => {
    const entry = opCatalog[$("#op-select").value];
    $("#op-desc").textContent = entry ? (entry.group + (entry.description ? " — " + entry.description : "")) : "";
    renderOpForm(entry ? entry.schema : null);
    $("#op-payload").value = ""; // start from the form, not a stale raw payload
  });
  $("#op-reload").addEventListener("click", loadOps);
  $("#op-submit").addEventListener("click", submitOp);
  $("#oplog-clear").addEventListener("click", clearLog);
}

export { init, enter, leave };
