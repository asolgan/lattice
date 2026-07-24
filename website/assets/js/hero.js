/* Hero canvas: drifting vertices weave into a lattice; a lens sweeps across,
   refracting the region it covers into a persona hue. */
(function () {
  const canvas = document.getElementById("lattice-canvas");
  if (!canvas) return;
  const ctx = canvas.getContext("2d");
  const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  const HUES = ["#59d8ff", "#a78bfa", "#fb7ba2", "#e8b45c"];
  const LINK_DIST = 130;
  let nodes = [], W = 0, H = 0, dpr = 1, raf = null, t = 0;

  function resize() {
    const rect = canvas.parentElement.getBoundingClientRect();
    dpr = Math.min(window.devicePixelRatio || 1, 2);
    W = rect.width; H = rect.height;
    canvas.width = W * dpr; canvas.height = H * dpr;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    const count = Math.min(90, Math.floor((W * H) / 16000));
    nodes = Array.from({ length: count }, () => ({
      x: Math.random() * W, y: Math.random() * H,
      vx: (Math.random() - 0.5) * 0.22, vy: (Math.random() - 0.5) * 0.22,
      r: 1.2 + Math.random() * 1.6,
    }));
  }

  function hexA(hex, a) {
    const n = parseInt(hex.slice(1), 16);
    return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${a})`;
  }

  function frame() {
    t += 0.0035;
    ctx.clearRect(0, 0, W, H);

    // lens position: slow lissajous sweep
    const lx = W * (0.5 + 0.38 * Math.sin(t * 1.0));
    const ly = H * (0.5 + 0.32 * Math.sin(t * 1.7 + 1.3));
    const lr = Math.min(W, H) * 0.28;
    const hue = HUES[Math.floor((t * 2) % HUES.length)];

    for (const n of nodes) {
      n.x += n.vx; n.y += n.vy;
      if (n.x < -10) n.x = W + 10; if (n.x > W + 10) n.x = -10;
      if (n.y < -10) n.y = H + 10; if (n.y > H + 10) n.y = -10;
    }

    for (let i = 0; i < nodes.length; i++) {
      for (let j = i + 1; j < nodes.length; j++) {
        const a = nodes[i], b = nodes[j];
        const dx = a.x - b.x, dy = a.y - b.y;
        const d2 = dx * dx + dy * dy;
        if (d2 > LINK_DIST * LINK_DIST) continue;
        const d = Math.sqrt(d2);
        const mx = (a.x + b.x) / 2, my = (a.y + b.y) / 2;
        const inLens = (mx - lx) ** 2 + (my - ly) ** 2 < lr * lr;
        const base = (1 - d / LINK_DIST) * (inLens ? 0.5 : 0.14);
        ctx.strokeStyle = inLens ? hexA(hue, base) : `rgba(148,163,216,${base})`;
        ctx.lineWidth = inLens ? 1.1 : 0.7;
        ctx.beginPath(); ctx.moveTo(a.x, a.y); ctx.lineTo(b.x, b.y); ctx.stroke();
      }
    }

    for (const n of nodes) {
      const inLens = (n.x - lx) ** 2 + (n.y - ly) ** 2 < lr * lr;
      ctx.fillStyle = inLens ? hexA(hue, 0.9) : "rgba(148,163,216,0.45)";
      ctx.beginPath(); ctx.arc(n.x, n.y, inLens ? n.r + 0.6 : n.r, 0, Math.PI * 2); ctx.fill();
    }

    // the lens ring itself
    ctx.strokeStyle = hexA(hue, 0.22);
    ctx.lineWidth = 1;
    ctx.beginPath(); ctx.arc(lx, ly, lr, 0, Math.PI * 2); ctx.stroke();

    raf = requestAnimationFrame(frame);
  }

  function start() {
    if (raf) cancelAnimationFrame(raf);
    resize();
    if (reduced) { t = 2.0; frame(); cancelAnimationFrame(raf); return; }
    raf = requestAnimationFrame(frame);
  }

  document.addEventListener("visibilitychange", () => {
    if (document.hidden) { if (raf) cancelAnimationFrame(raf); raf = null; }
    else if (!reduced) raf = requestAnimationFrame(frame);
  });
  window.addEventListener("resize", start);
  start();
})();
