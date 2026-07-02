// Core KV view: the vertex list + detail pane. Detail selection is
// URL-carried (#/corekv/<key>), every rendered key resolves through
// keyTarget, and link rows are far-end-clickable (design §1.2 seed).

import { $, el, pretty, api, setStatus } from "../api.js";
import { shortId, keyTarget, isEntityKey } from "../logic/keys.js";
import { navigate } from "../router.js";

const state = { listLoaded: false, listSeq: 0 };
let selectedKeyRow = null;

// enter handles a #/corekv route: an explicit ?prefix= reloads the list with
// the filter box set (URL-carried filter state) — compared against the live
// box value, so a manual filter typed in between never masks the reload. The
// arg, when present, is the selected key whose detail loads alongside.
function enter(route) {
  if (route.params.prefix !== undefined && route.params.prefix !== $("#corekv-prefix").value) {
    $("#corekv-prefix").value = route.params.prefix;
    state.listLoaded = false;
  }
  if (!state.listLoaded) {
    state.listLoaded = true;
    loadCoreKV();
  }
  if (route.arg) loadVertexDetail(route.arg, route.params.aspect);
}

async function loadCoreKV() {
  const seq = ++state.listSeq;
  const prefix = $("#corekv-prefix").value.trim();
  const limit = $("#corekv-limit").value.trim() || "500";
  setStatus("corekv-status", "loading…");
  const q = new URLSearchParams({ prefix, limit });
  const body = await api("/api/vertices?" + q.toString());
  if (seq !== state.listSeq) return; // a newer list load superseded this one
  const list = $("#corekv-keys");
  list.innerHTML = "";
  selectedKeyRow = null;
  if (body.error) { setStatus("corekv-status", body.error, true); return; }
  setStatus("corekv-status", body.count + " vertices" + (body.truncated ? " (capped at " + body.limit + ")" : ""));
  (body.vertices || []).forEach((v) => {
    const row = el("div", "key-row vtx-row");
    row.dataset.key = v.key;
    row.appendChild(el("span", "badge vtype", v.type));
    const main = el("span", "ktext");
    main.appendChild(el("span", "vtx-label", v.label || shortId(v.key)));
    if (v.label) main.appendChild(el("span", "vtx-id", shortId(v.key)));
    row.appendChild(main);
    if (v.isDeleted) row.appendChild(el("span", "deleted-flag", "del"));
    row.addEventListener("click", () => navigate(keyTarget(v.key)));
    list.appendChild(row);
  });
  if (!body.vertices || !body.vertices.length) list.appendChild(el("div", "muted", "(no vertices)"));
  markSelected(currentDetailKey);
}

// markSelected highlights the list row for key, when it is in the list.
function markSelected(key) {
  if (selectedKeyRow) { selectedKeyRow.classList.remove("selected"); selectedKeyRow = null; }
  if (!key) return;
  const row = $('#corekv-keys .key-row[data-key="' + CSS.escape(key) + '"]');
  if (row) { row.classList.add("selected"); selectedKeyRow = row; }
}

// keyChip renders a key-shaped string as a clickable chip when it resolves,
// else plain text.
function keyChip(key, cls) {
  const target = isEntityKey(key) ? keyTarget(key) : null;
  if (!target) return el("span", cls, key);
  const a = el("a", (cls ? cls + " " : "") + "key-link", key);
  a.href = target;
  return a;
}

let currentDetailKey = null;

async function loadVertexDetail(key, openAspect) {
  currentDetailKey = key;
  markSelected(key);
  const head = $("#corekv-valuehead");
  const detail = $("#corekv-detail");
  head.textContent = key;
  detail.innerHTML = "";
  detail.appendChild(el("div", "muted small", "loading…"));
  const body = await api("/api/vertex?key=" + encodeURIComponent(key));
  if (currentDetailKey !== key) return; // a newer selection superseded this one
  detail.innerHTML = "";
  if (body.error) { detail.appendChild(el("div", "error-text", body.error)); return; }

  head.textContent = key + " · r" + body.revision;
  if (body.isDeleted) head.appendChild(el("span", "deleted-flag", "isDeleted"));

  // Provenance chips: who/what created + last modified this entity, every id
  // a link (createdBy → the actor's vertex, *ByOp → the op tracker).
  const env = (body.envelope && typeof body.envelope === "object") ? body.envelope : {};
  const prov = el("div", "prov-chips");
  const chip = (label, val, isKey) => {
    if (!val) return;
    const c = el("span", "prov-chip");
    c.appendChild(el("span", "prov-k", label));
    c.appendChild(isKey ? keyChip(val) : el("span", null, val));
    prov.appendChild(c);
  };
  chip("created by", env.createdBy, true);
  chip("via op", env.createdByOp, true);
  chip("at", env.createdAt, false);
  if (env.lastModifiedAt && env.lastModifiedAt !== env.createdAt) {
    chip("modified by", env.lastModifiedBy, true);
    chip("via op", env.lastModifiedByOp, true);
    chip("at", env.lastModifiedAt, false);
  }
  if (prov.children.length) detail.appendChild(prov);

  // Vertex document.
  detail.appendChild(el("div", "vtx-section-head", "document" + (body.class ? " · " + body.class : "")));
  const doc = el("pre", "vtx-doc");
  doc.textContent = body.envelope ? pretty(body.envelope) : "(non-JSON value)";
  detail.appendChild(doc);

  // Aspects.
  const aspects = body.aspects || [];
  detail.appendChild(el("div", "vtx-section-head", "aspects (" + aspects.length + ")"));
  if (!aspects.length) detail.appendChild(el("div", "muted small", "(none)"));
  let aspectFound = false;
  aspects.forEach((a) => {
    const row = expanderRow(a.localName, "aspect", a.key);
    detail.appendChild(row);
    if (openAspect && a.localName === openAspect) {
      aspectFound = true;
      $(".expander-head", row).click();
      row.scrollIntoView({ block: "nearest" });
    }
  });
  if (openAspect && !aspectFound) {
    detail.appendChild(el("div", "muted small", "(aspect “" + openAspect + "” not present on this vertex)"));
  }

  // Links (either direction): the far end is the row's primary click; the ⧉
  // expander still opens the link document in place.
  const links = body.links || [];
  detail.appendChild(el("div", "vtx-section-head", "links (" + links.length + ")"));
  if (!links.length) detail.appendChild(el("div", "muted small", "(none)"));
  links.forEach((l) => {
    const arrow = l.direction === "out" ? "→" : "←";
    const label = arrow + " " + l.relation + " " + l.otherType + " · " + shortId(l.otherKey);
    detail.appendChild(expanderRow(label, "link " + l.direction, l.key, l.otherKey));
  });
}

// expanderRow renders a collapsed row that lazy-loads the entry's document via
// /api/corekv/entry on toggle. When farKey is given (a link row), the row
// label's click navigates to the far-end vertex and a trailing ⧉ toggles the
// document instead.
function expanderRow(label, badge, key, farKey) {
  const wrap = el("div", "expander");
  const headEl = el("div", "expander-head");
  const arrow = el("span", "expander-arrow", "▸");
  const bodyEl = el("pre", "expander-body");
  bodyEl.style.display = "none";
  let docLoaded = false;

  const toggleDoc = async () => {
    const isOpen = bodyEl.style.display !== "none";
    bodyEl.style.display = isOpen ? "none" : "block";
    arrow.textContent = isOpen ? "▸" : "▾";
    if (!isOpen && !docLoaded) {
      docLoaded = true;
      bodyEl.textContent = "loading…";
      const e = await api("/api/corekv/entry?key=" + encodeURIComponent(key));
      bodyEl.className = "expander-body" + (e.error ? " error-text" : "");
      bodyEl.textContent = e.error ? e.error : (e.envelope ? pretty(e.envelope) : "(non-JSON value)");
    }
  };

  headEl.appendChild(arrow);
  const labelEl = el("span", "expander-label" + (farKey ? " far-link" : ""), label);
  headEl.appendChild(labelEl);
  if (badge) headEl.appendChild(el("span", "badge " + badge, badge));

  if (farKey) {
    labelEl.title = farKey;
    headEl.addEventListener("click", () => navigate(keyTarget(farKey)));
    const doc = el("span", "expander-doc-toggle", "⧉");
    doc.title = "link document (" + key + ")";
    doc.addEventListener("click", (e) => { e.stopPropagation(); toggleDoc(); });
    headEl.appendChild(doc);
  } else {
    headEl.addEventListener("click", toggleDoc);
  }

  wrap.appendChild(headEl);
  wrap.appendChild(bodyEl);
  return wrap;
}

function init() {
  $("#corekv-load").addEventListener("click", () => { state.listLoaded = true; loadCoreKV(); });
  $("#corekv-prefix").addEventListener("keydown", (e) => { if (e.key === "Enter") loadCoreKV(); });
}

export { init, enter };
