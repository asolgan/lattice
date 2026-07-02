// Control view: the per-engine control columns (Refractor / Weaver / Loom).
// Loupe forwards control-plane bytes verbatim; the UI inspects loosely.

import { $, $all, el, pretty, api, setStatus } from "../api.js";

const state = { loaded: false };

function enter() {
  if (state.loaded) return;
  state.loaded = true;
  loadControl();
}

async function loadControl() {
  setStatus("control-status", "loading…");
  await Promise.all([
    loadControlReads("weaver"),
    loadControlReads("loom"),
  ]);
  setStatus("control-status", "");
}

// loadControlReads fetches a component's read lists and renders them.
// Refractor has no list endpoint (per-lens only) so its column is action-only.
async function loadControlReads(comp) {
  const col = $('.control-col[data-comp="' + comp + '"]');
  const listBox = $(".control-list", col);
  if (!listBox) return;
  const body = await api("/api/control/" + comp);
  listBox.innerHTML = "";
  if (body.error) { listBox.appendChild(el("div", "error-text", body.error)); return; }
  const reads = body.reads || {};
  Object.keys(reads).forEach((name) => {
    const reply = reads[name];
    listBox.appendChild(el("div", "muted small", name + ":"));
    renderControlList(comp, listBox, reply);
  });
}

// renderControlList renders a control plane's raw reply. Loupe forwards bytes
// verbatim, so the UI inspects loosely: render known list shapes (instances /
// targets / consumers) as clickable rows, else dump the JSON.
function renderControlList(comp, box, reply) {
  if (reply && reply.error) { box.appendChild(el("div", "error-text", reply.error)); return; }
  let rows = null;
  let idField = null;
  if (Array.isArray(reply)) rows = reply;
  else if (reply && Array.isArray(reply.instances)) { rows = reply.instances; idField = "instanceId"; }
  else if (reply && Array.isArray(reply.targets)) { rows = reply.targets; idField = "targetId"; }
  else if (reply && Array.isArray(reply.consumers)) { rows = reply.consumers; idField = "name"; }

  if (!rows) { box.appendChild(Object.assign(el("pre"), { textContent: pretty(reply) })); return; }
  if (!rows.length) { box.appendChild(el("div", "muted small", "(none)")); return; }
  // A row click fills the name field for its own namespace: a consumer row
  // (idField "name") fills the consumer field, an instance row ("instanceId")
  // the instance field; single-field columns (weaver targets) fall back.
  const fillKind = idField === "name" ? "consumer" : idField === "instanceId" ? "instance" : null;
  rows.forEach((r) => {
    const item = el("div", "control-item");
    const id = idField ? r[idField] : (r.instanceId || r.targetId || r.name || r.id || "");
    const idSpan = el("span", "cid", id || "(no id)");
    if (id) idSpan.addEventListener("click", () => {
      const col = box.closest(".control-col");
      const input = (fillKind && $('.control-name[data-fill="' + fillKind + '"]', col)) || $(".control-name", col);
      if (input) input.value = id;
    });
    item.appendChild(idSpan);
    const state = r.state || r.status || (r.State || r.Status) || "";
    if (state) item.appendChild(el("span", "state-tag", String(state)));
    box.appendChild(item);
  });
}

function init() {
  // Wire every control column's action buttons. A column may hold more than
  // one action group, each with its own name field (Loom separates the
  // instance namespace (inspect) from the consumer namespace (pause/resume));
  // a button reads the field in its own group, falling back to the column's
  // sole field.
  $all(".control-col").forEach((col) => {
    const comp = col.dataset.comp;
    const out = $(".control-out", col);
    $all(".control-action button", col).forEach((btn) => {
      btn.addEventListener("click", async () => {
        const nameInput = $(".control-name", btn.closest(".control-action")) || $(".control-name", col);
        const name = nameInput.value.trim();
        if (!name) { out.textContent = "enter a name/id first"; out.className = "control-out error-text"; return; }
        out.className = "control-out";
        out.textContent = comp + " " + btn.dataset.op + " " + name + " …";
        const body = await api("/api/control/" + comp + "/" + encodeURIComponent(name) + "/" + btn.dataset.op, { method: "POST" });
        out.textContent = pretty(body);
        out.className = "control-out" + (body.error ? " error-text" : "");
        // Refresh lists so a state change shows immediately.
        if (comp === "weaver" || comp === "loom") loadControlReads(comp);
      });
    });
  });
  $("#control-load").addEventListener("click", loadControl);
}

export { init, enter };
