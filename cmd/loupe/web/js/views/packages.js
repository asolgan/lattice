// Packages view: the installed-package table (GET /api/packages) with per-row
// detail links (#/package/<key>) and the toolbar Install action (§9.1/§9.3 —
// the shared upload modal lives in views/package.js).

import { $, el, api, setStatus } from "../api.js";
import { openApplyModal, closeModal } from "./package.js";

const state = { loaded: false };

function enter() {
  if (state.loaded) return;
  state.loaded = true;
  loadPackages();
}

// leave closes a dangling install modal — its dry-run preview renders key
// links, so a route change with the modal open is one click away.
function leave() {
  closeModal();
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
    const nameTd = el("td");
    const nameLink = el("a", "key-link", p.name);
    nameLink.href = "#/package/" + p.key;
    nameTd.appendChild(nameLink);
    tr.appendChild(nameTd);
    tr.appendChild(el("td", null, p.version));
    tr.appendChild(el("td", "muted small", p.installedAt || ""));
    const keyTd = el("td");
    const keyLink = el("a", "key-link cid", p.key);
    keyLink.href = "#/package/" + p.key;
    keyTd.appendChild(keyLink);
    tr.appendChild(keyTd);
    const goTd = el("td");
    const go = el("a", "key-link small", "detail →");
    go.href = "#/package/" + p.key;
    goTd.appendChild(go);
    tr.appendChild(goTd);
    tbody.appendChild(tr);
  });
  if (!body.packages || !body.packages.length) {
    const tr = el("tr");
    const td = el("td", "muted", "(no packages installed)");
    td.colSpan = 5;
    tr.appendChild(td);
    tbody.appendChild(tr);
  }
}

function init() {
  $("#packages-load").addEventListener("click", loadPackages);
  $("#packages-install").addEventListener("click", () => {
    openApplyModal({
      title: "Install package from file",
      intro: "Select the package directory's manifest.yaml. The package's " +
        "definition is compiled into Loupe — the manifest names and " +
        "cross-checks it. Preview shows the exact delta before anything is submitted.",
      endpoint: "/api/packages/install",
      forceDefault: false,
      onDone: loadPackages,
    });
  });
}

export { init, enter, leave };
