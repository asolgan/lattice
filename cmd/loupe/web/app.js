"use strict";

// Loupe UI — vanilla fetch, no framework. The Go server does all NATS I/O; this
// renders its JSON. Every /api/* response may carry {"error": ...}; renderError
// surfaces it inline rather than throwing.

function $(sel, root) { return (root || document).querySelector(sel); }
function $all(sel, root) { return Array.from((root || document).querySelectorAll(sel)); }

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function pretty(v) {
  try { return JSON.stringify(v, null, 2); }
  catch (_) { return String(v); }
}

// api GETs/POSTs JSON and returns the parsed body. A non-2xx with a JSON body is
// returned as-is (it carries {"error":...}); a transport failure is mapped to a
// synthetic {error} object so callers always get an object.
async function api(path, opts) {
  try {
    const res = await fetch(path, opts);
    const text = await res.text();
    let body;
    try { body = text ? JSON.parse(text) : {}; }
    catch (_) { body = { error: "non-JSON response: " + text.slice(0, 200) }; }
    return body;
  } catch (e) {
    return { error: "request failed: " + e.message };
  }
}

function setStatus(id, msg, isError) {
  const e = document.getElementById(id);
  if (!e) return;
  e.textContent = msg || "";
  e.className = "muted" + (isError ? " error-text" : "");
}

// ---- Tabs ----
$all(".tab").forEach((btn) => {
  btn.addEventListener("click", () => {
    $all(".tab").forEach((b) => b.classList.remove("active"));
    $all(".panel").forEach((p) => p.classList.remove("active"));
    btn.classList.add("active");
    document.getElementById("panel-" + btn.dataset.tab).classList.add("active");
    lazyLoad(btn.dataset.tab);
  });
});

const loaded = {};
function lazyLoad(tab) {
  if (loaded[tab]) return;
  loaded[tab] = true;
  if (tab === "health") loadHealth();
  if (tab === "control") loadControl();
  if (tab === "packages") loadPackages();
  if (tab === "files") loadFiles();
}

// ---- Core KV ----
let selectedKeyRow = null;

async function loadCoreKV() {
  const prefix = $("#corekv-prefix").value.trim();
  const limit = $("#corekv-limit").value.trim() || "500";
  setStatus("corekv-status", "loading…");
  const q = new URLSearchParams({ prefix, limit });
  const body = await api("/api/corekv?" + q.toString());
  const list = $("#corekv-keys");
  list.innerHTML = "";
  if (body.error) { setStatus("corekv-status", body.error, true); return; }
  setStatus("corekv-status", body.count + " keys" + (body.truncated ? " (capped at " + body.limit + ")" : ""));
  body.keys.forEach((k) => {
    const row = el("div", "key-row");
    row.appendChild(el("span", "ktext", k.key));
    row.appendChild(el("span", "badge " + k.class, k.class));
    row.addEventListener("click", () => {
      if (selectedKeyRow) selectedKeyRow.classList.remove("selected");
      row.classList.add("selected");
      selectedKeyRow = row;
      loadCoreKVEntry(k.key);
    });
    list.appendChild(row);
  });
}

async function loadCoreKVEntry(key) {
  $("#corekv-valuehead").textContent = key;
  $("#corekv-value").textContent = "loading…";
  const body = await api("/api/corekv/entry?key=" + encodeURIComponent(key));
  if (body.error) {
    $("#corekv-value").textContent = body.error;
    $("#corekv-value").className = "error-text";
    return;
  }
  $("#corekv-value").className = "";
  const head = $("#corekv-valuehead");
  head.textContent = key + " · r" + body.revision;
  if (body.isDeleted) {
    const flag = el("span", "deleted-flag", "isDeleted");
    head.appendChild(flag);
  }
  $("#corekv-value").textContent = body.envelope !== null ? pretty(body.envelope) : "(non-JSON value)";
}

$("#corekv-load").addEventListener("click", loadCoreKV);
$("#corekv-prefix").addEventListener("keydown", (e) => { if (e.key === "Enter") loadCoreKV(); });

// ---- Health ----
async function loadHealth() {
  setStatus("health-status", "loading…");
  const body = await api("/api/health");
  const cards = $("#health-cards");
  const alerts = $("#health-alerts");
  cards.innerHTML = "";
  alerts.innerHTML = "";
  const overall = $("#health-overall");
  if (body.error) {
    setStatus("health-status", body.error, true);
    overall.textContent = "";
    overall.className = "rollup";
    return;
  }
  setStatus("health-status", "");
  overall.textContent = body.overall;
  overall.className = "rollup " + body.overall;
  (body.components || []).forEach((c) => {
    const card = el("div", "card " + c.status);
    card.appendChild(el("div", "card-key", c.key));
    const meta = el("div", "card-meta");
    meta.appendChild(el("span", "card-status", c.status));
    meta.appendChild(el("span", null, c.freshness));
    card.appendChild(meta);
    if (c.issues && c.issues.length) {
      const box = el("div", "card-issues");
      c.issues.forEach((i) => box.appendChild(el("div", "card-issue", i)));
      card.appendChild(box);
    }
    cards.appendChild(card);
  });
  if (!body.components || !body.components.length) {
    cards.appendChild(el("div", "muted", "(no health entries)"));
  }
  (body.alerts || []).forEach((a) => alerts.appendChild(el("div", "alert-line", a)));
}
$("#health-load").addEventListener("click", loadHealth);

// ---- Control ----
async function loadControl() {
  setStatus("control-status", "loading…");
  await Promise.all([
    loadControlReads("weaver"),
    loadControlReads("loom"),
  ]);
  setStatus("control-status", "");
}

// loadControlReads fetches a component's read lists and renders them. Refractor
// has no list endpoint (per-lens only) so its column is action-only.
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
  rows.forEach((r) => {
    const item = el("div", "control-item");
    const id = idField ? r[idField] : (r.instanceId || r.targetId || r.name || r.id || "");
    const idSpan = el("span", "cid", id || "(no id)");
    if (id) idSpan.addEventListener("click", () => { $(".control-name", box.closest(".control-col")).value = id; });
    item.appendChild(idSpan);
    const state = r.state || r.status || (r.State || r.Status) || "";
    if (state) item.appendChild(el("span", "state-tag", String(state)));
    box.appendChild(item);
  });
}

// Wire every control column's action buttons.
$all(".control-col").forEach((col) => {
  const comp = col.dataset.comp;
  const nameInput = $(".control-name", col);
  const out = $(".control-out", col);
  $all(".control-action button", col).forEach((btn) => {
    btn.addEventListener("click", async () => {
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

// ---- Packages ----
async function loadPackages() {
  setStatus("packages-status", "loading…");
  const body = await api("/api/packages");
  const tbody = $("#packages-table tbody");
  tbody.innerHTML = "";
  if (body.error) { setStatus("packages-status", body.error, true); return; }
  setStatus("packages-status", body.count + " installed");
  (body.packages || []).forEach((p) => {
    const tr = el("tr");
    tr.appendChild(el("td", null, p.name));
    tr.appendChild(el("td", null, p.version));
    tr.appendChild(el("td", null, p.key));
    tbody.appendChild(tr);
  });
  if (!body.packages || !body.packages.length) {
    const tr = el("tr");
    const td = el("td", "muted", "(no packages installed)");
    td.colSpan = 3;
    tr.appendChild(td);
    tbody.appendChild(tr);
  }
}
$("#packages-load").addEventListener("click", loadPackages);

// ---- Submit Op ----
$("#op-submit").addEventListener("click", async () => {
  const operationType = $("#op-type").value.trim();
  const lane = $("#op-lane").value;
  const klass = $("#op-class").value.trim();
  const payloadText = $("#op-payload").value.trim() || "{}";
  const reply = $("#op-reply");

  if (!operationType) { setStatus("op-status", "operationType is required", true); return; }
  let payload;
  try { payload = JSON.parse(payloadText); }
  catch (e) { setStatus("op-status", "payload is not valid JSON: " + e.message, true); return; }

  setStatus("op-status", "submitting…");
  reply.textContent = "";
  reply.className = "";
  const req = { operationType, lane, payload };
  if (klass) req.class = klass;
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
});

// ---- Files (off-graph blob plane) ----

// uploadObject POSTs the multipart form to /api/objects. Uses fetch directly
// (not api()) because the body is FormData, not JSON.
async function uploadObject() {
  const target = $("#files-target").value.trim();
  const linkName = $("#files-linkname").value.trim();
  const replace = $("#files-replace").value.trim();
  const fileInput = $("#files-file");
  const reply = $("#files-upload-reply");
  if (!target || !linkName) { setStatus("files-upload-status", "target key and link name are required", true); return; }
  if (!fileInput.files || !fileInput.files.length) { setStatus("files-upload-status", "choose a file first", true); return; }

  const fd = new FormData();
  fd.append("file", fileInput.files[0]);
  fd.append("targetKey", target);
  fd.append("linkName", linkName);
  if (replace) fd.append("replaceObjectId", replace);

  setStatus("files-upload-status", "uploading…");
  reply.textContent = "";
  reply.className = "";
  let body;
  try {
    const res = await fetch("/api/objects", { method: "POST", body: fd });
    const text = await res.text();
    try { body = text ? JSON.parse(text) : {}; }
    catch (_) { body = { error: "non-JSON response: " + text.slice(0, 200) }; }
  } catch (e) {
    body = { error: "request failed: " + e.message };
  }
  reply.textContent = pretty(body);
  if (body.error || (body.status && body.status === "rejected")) {
    setStatus("files-upload-status", "failed", true);
    reply.className = "error-text";
    return;
  }
  setStatus("files-upload-status", "attached " + (body.oid || ""));
  reply.className = "ok-text";
  fileInput.value = "";
  loadFiles();
}

// loadFiles lists object→owner links (a lnk.object.* prefix scan) and renders a
// card per link: an inline thumbnail (for image objects), a download link, and
// a detach button. v1a has no object-listing lens, so this scans Core KV keys
// directly (a Loupe-only inspection path, P5 debug exception).
async function loadFiles() {
  setStatus("files-status", "loading…");
  const grid = $("#files-grid");
  grid.innerHTML = "";
  const body = await api("/api/corekv?prefix=lnk.object.&limit=500");
  if (body.error) { setStatus("files-status", body.error, true); return; }
  const links = (body.keys || []).filter((k) => k.class === "link");
  if (!links.length) { grid.appendChild(el("div", "muted", "(no attached objects)")); setStatus("files-status", "0 links"); return; }
  setStatus("files-status", links.length + " link(s)" + (body.truncated ? " (capped)" : ""));

  for (const k of links) {
    // lnk.object.<oid>.<linkName>.<tgtType>.<tgtId>
    const parts = k.key.split(".");
    if (parts.length !== 6) continue;
    const oid = parts[2], linkName = parts[3];
    const targetKey = "vtx." + parts[4] + "." + parts[5];

    const entry = await api("/api/corekv/entry?key=" + encodeURIComponent(k.key));
    if (entry.isDeleted) continue; // detached — skip

    const card = el("div", "file-card");
    const thumb = el("img", "file-thumb");
    thumb.src = "/api/objects/" + encodeURIComponent(oid);
    thumb.alt = oid;
    thumb.addEventListener("error", () => { thumb.replaceWith(el("div", "file-thumb file-thumb-none", "no preview")); });
    card.appendChild(thumb);

    const meta = el("div", "file-meta");
    meta.appendChild(el("div", "file-oid", oid));
    meta.appendChild(el("div", "muted small", linkName + " → " + targetKey));
    const actions = el("div", "file-actions");
    const dl = el("a", "file-link", "download");
    dl.href = "/api/objects/" + encodeURIComponent(oid);
    dl.setAttribute("download", "");
    actions.appendChild(dl);
    const detach = el("button", "file-detach", "detach");
    detach.addEventListener("click", () => detachObject(oid, targetKey, linkName));
    actions.appendChild(detach);
    meta.appendChild(actions);
    card.appendChild(meta);
    grid.appendChild(card);
  }
  if (!grid.children.length) grid.appendChild(el("div", "muted", "(no live attached objects)"));
}

async function detachObject(oid, targetKey, linkName) {
  setStatus("files-status", "detaching " + oid + "…");
  const q = new URLSearchParams({ targetKey, linkName });
  const body = await api("/api/objects/" + encodeURIComponent(oid) + "?" + q.toString(), { method: "DELETE" });
  if (body.error || body.status === "rejected") {
    setStatus("files-status", "detach failed: " + (body.error || pretty(body.error)), true);
    return;
  }
  loadFiles();
}

$("#files-upload-btn").addEventListener("click", uploadObject);
$("#files-load").addEventListener("click", loadFiles);

// Load the default (Core KV) tab on first paint.
loadCoreKV();
loaded.corekv = true;
