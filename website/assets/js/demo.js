/* "One graph, four lenses" — simulated walkthrough of package composition.
   Canned data; the key grammar, lens names and flows mirror the real stack. */
(function () {
  const root = document.getElementById("demo");
  if (!root) return;

  // ---------------------------------------------------------------- data
  const PKGS = {
    leasing:  { label: "Leasing" },
    clinic:   { label: "Clinic" },
    cafe:     { label: "Café" },
    wellness: { label: "Wellness" },
  };

  // pkg:"base" nodes are always present. x/y in a 660x600 viewBox.
  const NODES = [
    { id: "bldg",    label: "The Foundry",    key: "vtx.location.FNDRY01",   pkg: "base",     x: 330, y: 66,  person: false },
    { id: "u204",    label: "Unit 204",       key: "vtx.unit.U204",          pkg: "base",     x: 205, y: 158, person: false },
    { id: "u207",    label: "Unit 207",       key: "vtx.unit.U207",          pkg: "base",     x: 452, y: 148, person: false },
    { id: "kestrel", label: "Kestrel Mgmt",   key: "vtx.identity.KESTREL9",  pkg: "base",     x: 583, y: 70,  person: true  },
    { id: "maya",    label: "Maya",           key: "vtx.identity.MAYA4kP",   pkg: "base",     x: 110, y: 432, person: true  },

    { id: "app19",   label: "Application",    key: "vtx.leaseapp.APP19",     pkg: "leasing",  x: 318, y: 252, person: false },
    { id: "lease",   label: "Lease",          key: "vtx.lease.L204",         pkg: "leasing",  x: 212, y: 318, person: false },
    { id: "renewal", label: "Renewal R-88",   key: "vtx.renewal.R88",        pkg: "leasing",  x: 322, y: 388, person: false },

    { id: "clinic",  label: "Clinic",         key: "vtx.location.CLIN1",     pkg: "clinic",   x: 540, y: 232, person: false },
    { id: "drok",    label: "Dr. Okafor",     key: "vtx.provider.OKAFOR2",   pkg: "clinic",   x: 612, y: 330, person: true  },
    { id: "pat88",   label: "Patient record", key: "vtx.patient.P88",        pkg: "clinic",   x: 452, y: 372, person: false },
    { id: "appt",    label: "Appointment",    key: "vtx.appointment.A7761",  pkg: "clinic",   x: 545, y: 445, person: false },

    { id: "cafe",    label: "Café",           key: "vtx.location.CAFE1",     pkg: "cafe",     x: 330, y: 505, person: false },
    { id: "tab",     label: "House tab",      key: "vtx.account.ACC88",      pkg: "cafe",     x: 218, y: 545, person: false },

    { id: "studio",  label: "Studio",         key: "vtx.location.WELL1",     pkg: "wellness", x: 86,  y: 258, person: false },
    { id: "booking", label: "Class booking",  key: "vtx.booking.BK7",        pkg: "wellness", x: 122, y: 338, person: false },
  ];

  // Link naming reads "source relation target"; later-arriving vertex is the source.
  const LINKS = [
    { from: "u204",    to: "bldg",    rel: "partOf",        pkg: "base" },
    { from: "u207",    to: "bldg",    rel: "partOf",        pkg: "base" },
    { from: "kestrel", to: "u204",    rel: "manages",       pkg: "base" },
    { from: "kestrel", to: "u207",    rel: "manages",       pkg: "base" },

    { from: "app19",   to: "maya",    rel: "applicationFor", pkg: "leasing" },
    { from: "app19",   to: "u204",    rel: "appliesToUnit",  pkg: "leasing" },
    { from: "lease",   to: "u204",    rel: "forUnit",        pkg: "leasing" },
    { from: "lease",   to: "maya",    rel: "heldBy",         pkg: "leasing" },
    { from: "renewal", to: "lease",   rel: "forLease",       pkg: "leasing" },

    { from: "clinic",  to: "bldg",    rel: "partOf",         pkg: "clinic" },
    { from: "pat88",   to: "maya",    rel: "identifiedBy",   pkg: "clinic" },
    { from: "appt",    to: "drok",    rel: "withProvider",   pkg: "clinic" },
    { from: "appt",    to: "pat88",   rel: "forPatient",     pkg: "clinic" },
    { from: "drok",    to: "clinic",  rel: "practicesAt",    pkg: "clinic" },

    { from: "cafe",    to: "bldg",    rel: "partOf",         pkg: "cafe" },
    { from: "tab",     to: "maya",    rel: "heldBy",         pkg: "cafe" },

    { from: "studio",  to: "bldg",    rel: "partOf",         pkg: "wellness" },
    { from: "booking", to: "studio",  rel: "bookedAt",       pkg: "wellness" },
    { from: "booking", to: "maya",    rel: "bookedBy",       pkg: "wellness" },

    // cross-package links: only exist when BOTH packages are installed
    { from: "tab",     to: "lease",   rel: "billedWith",     pkg: "cafe",     needs: "leasing", emergent: true },
    { from: "booking", to: "lease",   rel: "residentRate",   pkg: "wellness", needs: "leasing", emergent: true },
  ];

  const PERSONAS = {
    resident: { label: "Maya — resident", color: "var(--c-consumer)" },
    front:    { label: "Front desk",      color: "var(--c-front)" },
    back:     { label: "Operations",      color: "var(--c-back)" },
    operator: { label: "Operator",        color: "var(--c-operator)" },
  };

  // visibility per persona: hi = their lens, mid = context, dim = outside the projection
  const VIS = {
    resident: { hi: ["maya", "app19", "lease", "renewal", "pat88", "appt", "tab", "booking"], mid: ["u204", "bldg", "clinic", "cafe", "studio", "drok"], },
    front:    { hi: ["app19", "appt", "drok", "tab", "booking", "maya"], mid: ["clinic", "cafe", "studio", "bldg", "u204", "pat88", "lease"], },
    back:     { hi: ["kestrel", "u204", "u207", "renewal", "bldg", "clinic", "cafe", "studio"], mid: ["lease", "app19", "drok", "appt", "booking", "tab"], },
    operator: { hi: NODES.map(n => n.id), mid: [] },
  };

  const CAPTIONS = {
    resident: "<b>Maya's lens</b> — her own subgraph, nothing else. One identity vertex across every service line.",
    front:    "<b>Front-of-house lens</b> — today's work, with full resident context surfaced before she asks.",
    back:     "<b>Back-of-house lens</b> — occupancy, renewals, utilization. Aggregates, not private records.",
    operator: "<b>Operator lens</b> — the raw graph, real key grammar. On the live stack this is Loupe, the console.",
  };

  const state = { pkgs: { leasing: true, clinic: false, cafe: false, wellness: false }, persona: "resident" };

  // ---------------------------------------------------------------- helpers
  const el = (sel) => root.querySelector(sel);
  const nodeById = Object.fromEntries(NODES.map(n => [n.id, n]));
  const on = (p) => state.pkgs[p];
  const activePkgCount = () => Object.values(state.pkgs).filter(Boolean).length;

  function linkKey(l) {
    const a = nodeById[l.from].key.split(".");
    const b = nodeById[l.to].key.split(".");
    return `lnk.${a[1]}.${a[2]}.${l.rel}.${b[1]}.${b[2]}`;
  }

  function visibleNodes() {
    return NODES.filter(n => n.pkg === "base" || on(n.pkg));
  }
  function visibleLinks() {
    return LINKS.filter(l => (l.pkg === "base" || on(l.pkg)) && (!l.needs || on(l.needs)));
  }

  // ---------------------------------------------------------------- graph
  function renderGraph() {
    const vis = VIS[state.persona];
    const pcol = PERSONAS[state.persona].color;
    const showKeys = state.persona === "operator";
    const sharedIdentity = on("leasing") && on("clinic");

    const links = visibleLinks().map(l => {
      const a = nodeById[l.from], b = nodeById[l.to];
      const emph = vis.hi.includes(l.from) && vis.hi.includes(l.to) ? "" : " dim";
      const cls = l.emergent ? "g-link emergent" : "g-link" + emph;
      const mx = (a.x + b.x) / 2, my = (a.y + b.y) / 2;
      const relLabel = l.emergent
        ? `<text x="${mx}" y="${my - 5}" text-anchor="middle" class="gkey" fill="var(--faint)" font-size="8.5" font-family="var(--mono)">${l.rel}</text>`
        : "";
      return `<line class="${cls}" x1="${a.x}" y1="${a.y}" x2="${b.x}" y2="${b.y}"><title>${linkKey(l)}</title></line>${relLabel}`;
    }).join("");

    const nodes = visibleNodes().map(n => {
      let emph = "dim";
      if (vis.hi.includes(n.id)) emph = "hi";
      else if (vis.mid.includes(n.id)) emph = "";
      const pulse = sharedIdentity && n.id === "maya" ? " pulse" : "";
      const r = n.person ? 11 : 8;
      const keyLabel = showKeys ? `<text class="gkey" x="${n.x}" y="${n.y + r + 22}" text-anchor="middle">${n.key}</text>` : "";
      return `<g class="g-node ${n.person ? "person " : ""}${emph}${pulse}" style="--pcol:${pcol}">
        <circle cx="${n.x}" cy="${n.y}" r="${r}"><title>${n.key}</title></circle>
        <text x="${n.x}" y="${n.y + r + 12}" text-anchor="middle">${n.label}</text>
        ${keyLabel}
      </g>`;
    }).join("");

    el(".graph-svg").innerHTML = links + nodes;
    el(".graph-cap").innerHTML = CAPTIONS[state.persona];
  }

  // ---------------------------------------------------------------- panel
  function card(t, d, m, hint) {
    return `<div class="pcard">
      <div class="t">${t}</div>
      ${d ? `<div class="d">${d}</div>` : ""}
      ${m ? `<div class="m">${m}</div>` : ""}
      ${hint ? `<div class="hint">${hint}</div>` : ""}
    </div>`;
  }

  function renderPanel() {
    const p = state.persona;
    let title = "", sub = "", cards = [];

    if (p === "resident") {
      title = "Maya's portal"; sub = "What the resident sees — reads served by lens projections, never the core store.";
      cards.push(card("Maya", "One profile across every service in the building.", "vtx.identity.MAYA4kP"));
      if (on("leasing")) cards.push(card(
        `My home — Unit 204 <span class="pill">lease</span>`,
        "$2,350/mo · lease ends Sep 30.",
        "lens: leaseApplicationsRead · renewalsRead (Postgres, row-level security)",
        `<span class="pill glow">Renewal R-88</span> &nbsp;Terms proposed — review &amp; sign. <span style="color:var(--faint)">(the lease-renewal flow that runs on the real stack)</span>`
      ));
      if (on("clinic")) cards.push(card(
        "Next appointment",
        "Thu 10:15 · Dr. Okafor · ground-floor clinic. Booked from the slot grid — double-booking is impossible by construction.",
        "lens: clinicAppointmentsRead (self-anchored)"
      ));
      if (on("cafe")) cards.push(card(
        `House tab`,
        "$23.40 open." + (on("leasing") ? " Settles on your monthly statement — the ledger serves both packages." : ""),
        "vtx.account.ACC88"
      ));
      if (on("wellness")) cards.push(card(
        `Mobility class`,
        "Sat 9:00 · booked." + (on("leasing") ? " Resident rate applied via your lease." : ""),
        "vtx.booking.BK7"
      ));
      if (["leasing", "cafe", "wellness"].filter(on).length >= 2) cards.push(card(
        "One statement",
        "Rent, tab, classes — one bill, because every line item hangs off the same identity vertex.",
        ""
      ));
    }

    if (p === "front") {
      title = "Front desk"; sub = "Full resident context, surfaced before anyone asks.";
      cards.push(card(
        "Maya is at the desk",
        [
          on("leasing") && "Resident of Unit 204 (renewal in progress)",
          on("clinic") && "Clinic visit Thursday 10:15",
          on("cafe") && "Open tab: $23.40",
          on("wellness") && "Booked: Sat mobility class",
        ].filter(Boolean).join(" · ") || "No services installed yet — toggle packages above.",
        "one lookup, one graph — no swivel-chair between systems"
      ));
      if (on("leasing")) cards.push(card("Applications to review (1)", "APP-19 · Maya → Unit 204 · background check clear.", "lens: landlordLeaseApplicationsRead"));
      if (on("clinic")) cards.push(card("Today's schedule", "6 appointments · Dr. Okafor · zero double-books (write-path slot claims).", "lens: clinicAppointments"));
      if (on("cafe")) cards.push(card(`Open tabs (3)`, "Table 2 · Maya · $23.40 — charge to residence?", ""));
      if (on("wellness")) cards.push(card(`Sat 9:00 roster`, "8 of 12 booked · 5 residents, 3 guests.", ""));
    }

    if (p === "back") {
      title = "Operations"; sub = "The building as a business — aggregates and queues, not private records.";
      if (on("leasing")) {
        cards.push(card("Renewals due (1)", "R-88 · Unit 204 · propose terms → guarantor → signature. The Weaver drives this toward 'renewed' as a goal, not a script.", "target lens: renewalsRead · goal-authored"));
        cards.push(card("Vacancy", "Unit 207 listed at $2,600 · 14 days on market.", "lens: availableListings"));
      }
      if (on("clinic")) cards.push(card("Provider utilization", "78% this week · Thursday is the bottleneck.", "lens: providerAppointmentsRead"));
      if (on("cafe")) cards.push(card(`Café`, "61% margin · restock Tuesday · residents = 70% of covers.", ""));
      if (on("wellness")) cards.push(card(`Studio`, "Sat 9:00 near capacity — consider a second class.", ""));
      if (activePkgCount() >= 3) cards.push(card("Portfolio pulse", "Occupancy 96% · service attach rate 2.4 packages per resident · churn risk: low.", "a view that only exists because the packages share one graph"));
      if (!cards.length) cards.push(card("Nothing installed", "Toggle packages above to give operations something to run.", ""));
    }

    if (p === "operator") {
      title = "Operator console"; sub = "On the real stack this is Loupe — inspector, lens explorer, Time Machine.";
      const lenses = [
        on("leasing") && "availableListings · leaseApplicationsRead · renewalsRead",
        on("clinic") && "clinicAppointments · clinicPatientsRead · providerAppointmentsRead",
        on("cafe") && "cafeLedgerHistory · leaseAccounts",
        on("wellness") && "classSchedule · classRosterRead",
      ].filter(Boolean).join("<br>");
      cards.push(card("Lens projections", lenses || "No packages installed.", "reads = lenses (P5) · writes = operations through the one Processor (P2)"));
      cards.push(card("Visible vertices", "", visibleNodes().map(n => n.key).join("<br>")));
      cards.push(card("Every mutation is attributed", "Who submitted it, which capability allowed it, what it changed — replayable end to end.", "Loupe Time Machine scrubs this history on the live stack"));
    }

    el(".panel-title").textContent = title;
    el(".panel-sub").textContent = sub;
    el(".panel-cards").innerHTML = cards.join("");
  }

  // ---------------------------------------------------------------- emergence
  function renderEmergence() {
    const items = [];
    if (on("leasing") && on("clinic")) items.push(`<li><b>Shared identity:</b> Maya's patient record points at the same vertex as her lease — one profile, no sync job. <span class="m">lnk.patient.P88.identifiedBy.identity.MAYA4kP</span></li>`);
    if (on("leasing") && on("cafe")) items.push(`<li><b>One bill:</b> café charges settle on the monthly statement — the ledger package serves both. <span class="m">lnk.account.ACC88.billedWith.lease.L204</span></li>`);
    if (on("leasing") && on("wellness")) items.push(`<li><b>Resident perk:</b> member rate applies automatically because the booking can see the lease. <span class="m">lnk.booking.BK7.residentRate.lease.L204</span></li>`);
    if (on("clinic") && on("wellness")) items.push(`<li><b>Care to wellness:</b> post-visit, the provider suggests a mobility class — bookable because both share the scheduling shape.</li>`);
    if (activePkgCount() === 4) items.push(`<li class="capstone"><b>Mixed-use mode:</b> residences, care, food &amp; beverage, wellness — one property, one graph. No integrations were written between these packages; they compose because they share the substrate.</li>`);

    const box = el(".emergence");
    if (!items.length) {
      box.innerHTML = `<div class="et">Composition</div><ul><li>Install a second package to see what composition buys — the packages don't know about each other; the graph connects them.</li></ul>`;
    } else {
      box.innerHTML = `<div class="et">More than the sum — what composition unlocked</div><ul>${items.join("")}</ul>`;
    }
  }

  // ---------------------------------------------------------------- ticker
  const tickerLines = [];
  function pushTick(lines) {
    for (const l of lines) tickerLines.push(l);
    while (tickerLines.length > 4) tickerLines.shift();
    el(".tk-lines").innerHTML = tickerLines
      .map((l, i) => `<div class="line${i === tickerLines.length - 1 ? " new" : ""}">${l}</div>`)
      .join("");
  }

  const TICKS = {
    leasing: [
      `<span class="op">op SetListing</span> → Processor → PUT vtx.unit.U204.listing · seq 4118`,
      `CDC → Refractor → <span class="lens">lens availableListings</span> ⇒ row upsert`,
      `<span class="op">op OpenRenewal</span> → PUT vtx.renewal.R88 · <span class="lens">renewalsRead</span> ⇒ tenant + landlord rows`,
    ],
    clinic: [
      `<span class="op">op CreateAppointment</span> → slot-claim aspects (provider + patient) · collision = rejected`,
      `CDC → Refractor → <span class="lens">lens clinicAppointments</span> ⇒ row upsert`,
    ],
    cafe: [
      `<span class="op">op DebitAccount</span> → PUT vtx.transaction.T90210 (append-only)`,
      `CDC → <span class="lens">lens ledgerHistory</span> ⇒ row upsert`,
    ],
    wellness: [
      `<span class="op">op CreateBooking</span> → PUT vtx.booking.BK7`,
      `CDC → <span class="lens">lens classSchedule</span> ⇒ row upsert`,
    ],
  };
  const UNTICKS = {
    leasing: [`<span class="op">op Tombstone…</span> lease records retired · lenses retract rows`],
    clinic: [`clinic package uninstalled · lens rows retracted`],
    cafe: [`café package uninstalled · ledger lenses retracted`],
    wellness: [`wellness package uninstalled · schedule lens retracted`],
  };

  // ---------------------------------------------------------------- wiring
  function renderAll() {
    root.dataset.persona = state.persona;
    renderGraph(); renderPanel(); renderEmergence();
  }

  el(".pkg-toggles").innerHTML = Object.entries(PKGS).map(([id, p]) =>
    `<button class="pkg-toggle${state.pkgs[id] ? " on" : ""}" data-pkg="${id}" type="button">
      <span class="dot"></span>${p.label}
    </button>`
  ).join("");

  el(".persona-tabs").innerHTML = Object.entries(PERSONAS).map(([id, p]) =>
    `<button class="persona-tab${state.persona === id ? " on" : ""}" data-persona-id="${id}" type="button">${p.label}</button>`
  ).join("");

  root.addEventListener("click", (e) => {
    const pkgBtn = e.target.closest(".pkg-toggle");
    if (pkgBtn) {
      const id = pkgBtn.dataset.pkg;
      state.pkgs[id] = !state.pkgs[id];
      pkgBtn.classList.toggle("on", state.pkgs[id]);
      pushTick(state.pkgs[id]
        ? [`<span class="op">install ${id} package</span> → DDL via ops.meta.> · entities + lenses + operations registered`, ...TICKS[id]]
        : UNTICKS[id]);
      renderAll();
      return;
    }
    const tab = e.target.closest(".persona-tab");
    if (tab) {
      state.persona = tab.dataset.personaId;
      root.querySelectorAll(".persona-tab").forEach(t => t.classList.toggle("on", t === tab));
      renderAll();
    }
  });

  pushTick([`bootstrap · kernel verified · <span class="op">install leasing package</span>`, ...TICKS.leasing.slice(0, 2)]);
  renderAll();
})();
