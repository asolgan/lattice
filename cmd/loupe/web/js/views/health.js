// Health view: per-component heartbeat cards + the flattened alert lines.

import { $, el, api, setStatus } from "../api.js";
import { issueClass } from "../logic/status.js";

const state = { loaded: false };

function enter() {
  if (state.loaded) return;
  state.loaded = true;
  loadHealth();
}

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
    const title = el("div", "card-key", c.name || c.key);
    if (c.group && c.group !== c.name) title.appendChild(el("span", "card-group", c.group));
    card.appendChild(title);
    if (c.detail) card.appendChild(el("div", "card-sub", c.detail));
    const meta = el("div", "card-meta");
    meta.appendChild(el("span", "card-status", c.status));
    meta.appendChild(el("span", null, c.freshness));
    card.appendChild(meta);
    if (c.issues && c.issues.length) {
      const box = el("div", "card-issues");
      c.issues.forEach((i) => box.appendChild(el("div", issueClass(i), i)));
      card.appendChild(box);
    }
    cards.appendChild(card);
  });
  if (!body.components || !body.components.length) {
    cards.appendChild(el("div", "muted", "(no health entries)"));
  }
  (body.alerts || []).forEach((a) => alerts.appendChild(el("div", "alert-line", a)));
}

function init() {
  $("#health-load").addEventListener("click", loadHealth);
}

export { init, enter };
