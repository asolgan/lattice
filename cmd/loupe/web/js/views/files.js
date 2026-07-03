// Files view: the off-graph blob plane — upload/attach + the attached-object
// grid.

import { $, el, pretty, api, setStatus } from "../api.js";
import { keyLinkEl } from "../render.js";

const state = { loaded: false, seq: 0 };

// enter loads once; ?target= pre-fills the attach target key (the vertex
// page's "attach file" affordance carries the key in the URL).
function enter(route) {
  const t = route && route.params && route.params.target;
  if (t) $("#files-target").value = t;
  if (state.loaded) return;
  state.loaded = true;
  loadFiles();
}

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

// loadFiles lists object→owner links (a lnk.object.* prefix scan) and renders
// a card per link: an inline thumbnail (for image objects), a download link,
// and a detach button. v1a has no object-listing lens, so this scans Core KV
// keys directly (a Loupe-only inspection path, P5 debug exception).
async function loadFiles() {
  const seq = ++state.seq; // supersede any in-flight load (the per-link loop is long)
  setStatus("files-status", "loading…");
  const grid = $("#files-grid");
  grid.innerHTML = "";
  const body = await api("/api/corekv?prefix=lnk.object.&limit=500");
  if (seq !== state.seq) return;
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
    if (seq !== state.seq) return; // a newer load owns the grid now
    if (entry.isDeleted) continue; // detached — skip

    const card = el("div", "file-card");
    const thumb = el("img", "file-thumb");
    thumb.src = "/api/objects/" + encodeURIComponent(oid);
    thumb.alt = oid;
    thumb.addEventListener("error", () => { thumb.replaceWith(el("div", "file-thumb file-thumb-none", "no preview")); });
    card.appendChild(thumb);

    const meta = el("div", "file-meta");
    meta.appendChild(el("div", "file-oid", oid));
    const owner = el("div", "muted small", linkName + " → ");
    owner.appendChild(keyLinkEl(targetKey));
    meta.appendChild(owner);
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

function init() {
  $("#files-upload-btn").addEventListener("click", uploadObject);
  $("#files-load").addEventListener("click", loadFiles);
}

export { init, enter };
