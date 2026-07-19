// The hosted-demo visitor banner (F20). Reads the posture once at boot and
// renders it above the alert strip; the shaping decision lives in
// logic/demo.js, this module only puts it on the page.

import { $, api, el } from "../api.js";
import { demoBanner } from "../logic/demo.js";

async function init() {
  const host = $("#demobanner");
  if (!host) return;
  const banner = demoBanner(await api("/api/demo"));
  if (!banner) return;
  host.appendChild(el("strong", null, banner.title));
  host.appendChild(el("span", null, banner.text));
  host.classList.add("visible");
}

export { init };
