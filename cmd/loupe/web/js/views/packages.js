// Packages view: the installed-package table (GET /api/packages).

import { $, el, api, setStatus } from "../api.js";

const state = { loaded: false };

function enter() {
  if (state.loaded) return;
  state.loaded = true;
  loadPackages();
}

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

function init() {
  $("#packages-load").addEventListener("click", loadPackages);
}

export { init, enter };
