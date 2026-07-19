// The hosted-demo posture's render decision (F20,
// loupe-f20-demo-operator-ux.md). Pure — no DOM, no fetch.
//
// The banner is a disclaimer for a visitor on a public URL. Its copy promises
// only what this console actually enforces — writes and reveals refused — and
// not that the platform's grants are narrow, which is provisioned separately
// and which nothing on this page can verify.

// demoBanner shapes /api/demo into the banner to render, or null for none.
// Anything other than an explicit demoMode:true reads as "not a demo" — an
// absent, failed, or malformed read leaves the ordinary console unbannered
// rather than fabricating a posture from a missing field.
function demoBanner(payload) {
  if (!payload || payload.demoMode !== true) return null;
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

export { demoBanner };
