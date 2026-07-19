// The hosted-demo posture's render decision (F20,
// loupe-f20-demo-operator-ux.md). Pure — no DOM, no fetch.
//
// The banner is a disclaimer for a visitor on a public URL. Its copy promises
// only what this console actually enforces — writes and reveals refused — and
// not that the platform's grants are narrow, which is provisioned separately
// and which nothing on this page can verify.

// demoPostureOn reads the one field that decides everything else. Anything
// other than an explicit demoMode:true is "not a demo" — an absent, failed, or
// malformed read leaves the ordinary console untouched rather than fabricating
// a posture from a missing field. Suppression is cosmetic (the server's method
// rule is the enforcement), so failing this way costs nothing but honesty.
function demoPostureOn(payload) {
  return !!payload && payload.demoMode === true;
}

// demoControlOpHidden reports whether a control-plane op button should be
// hidden under the given posture: in demo mode the server refuses every control
// op except the ones that only inspect, which it names in readOnlyControlOps.
// Reading that classification off the server response rather than restating it
// here means the buttons shown and the ops permitted cannot drift apart.
//
// An absent or malformed list hides every op for that component — the same
// omission-denies posture the server's gate takes, so a shape change degrades
// to "too little shown", never to a button that only 403s.
function demoControlOpHidden(payload, comp, op) {
  if (!demoPostureOn(payload)) return false;
  var byComp = payload.readOnlyControlOps;
  if (!byComp || typeof byComp !== "object") return true;
  var ops = byComp[comp];
  if (!Array.isArray(ops)) return true;
  return ops.indexOf(op) === -1;
}

// demoBanner shapes /api/demo into the banner to render, or null for none.
function demoBanner(payload) {
  if (!demoPostureOn(payload)) return null;
  var notice = typeof payload.notice === "string" ? payload.notice.trim() : "";
  // The server's notice leads with "read-only demo:" so it stands alone as a
  // 403 body; the banner already carries that as its title, so drop the
  // duplicate lead-in rather than rendering the phrase twice in a row.
  var lead = /^read-only demo:\s*/i;
  if (lead.test(notice)) notice = notice.replace(lead, "");
  return {
    title: "Read-only demo",
    // The server's own denial message is the body when it sent one, so the
    // banner and the 403 a visitor triggers say the same thing.
    text: notice ||
      "This is a live Lattice operator console, in read-only demo mode. Write actions and PII reveals are refused.",
  };
}

export { demoBanner, demoPostureOn, demoControlOpHidden };
