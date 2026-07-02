// Submit Op view: the op catalog picker, the schema-driven form, and submit.
// Field coercion + reads derivation are pure (logic/reads.js).

import { $, $all, el, pretty, api, setStatus } from "../api.js";
import { deriveReads, coerceField, schemaTypeLabel } from "../logic/reads.js";

const state = { loaded: false };

// opCatalog maps an operationType to its group (service), input schema, and
// description, built from GET /api/ops. The op picker drives a schema form.
let opCatalog = {};

function enter() {
  if (state.loaded) return;
  state.loaded = true;
  loadOps();
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
// override when it is not a catalog command) — the Tasks view's "Complete in
// Submit Op →" path. Before the catalog has loaded the prefill is queued, so
// a catalog command never silently lands in the raw override field.
function prefillOp(opName) {
  if (!Object.keys(opCatalog).length) {
    pendingPrefill = opName;
    setStatus("op-status", "pre-filling from task…");
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
  setStatus("op-status", "pre-filled from task — complete the fields and submit");
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
  reply.textContent = "";
  reply.className = "";
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
  reply.textContent = pretty(body);
  if (body.error) {
    setStatus("op-status", "error", true);
    reply.className = "error-text";
  } else {
    setStatus("op-status", body.status || "done");
    reply.className = body.status === "accepted" ? "ok-text" : "";
  }
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
}

export { init, enter, prefillOp };
