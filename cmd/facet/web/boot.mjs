// boot is Facet's browser-native engine boot (EDGE.5 W4 inc 3,
// edge-browser-node-design.md §3.4): it loads the wasm engine
// (internal/edge/browser via cmd/edge-wasm) + the JS transport shell
// (internal/edge/browser/shell), wires them together, and hands the renderer an
// edge-source so the PWA reads its own in-page Personal-Lens mirror instead of
// the Go host's SSE feed.
//
// It is config-gated. The page is configured for the in-page engine only when a
// window.__EDGE_BOOT__ object is present (the session-scoped bootstrap the
// static host injects — wired in inc 4 alongside Facet Fire 3's auth turn-on,
// which mints the WS token). Absent, this module is a cheap no-op and app.js
// falls back to the SSE source: the shipped Go host is unchanged. So this
// module never fetches the wasm/shell assets (nor requires them to be served)
// until a real config appears.

import { edgeSource } from "./edge-source.mjs";

// readBootConfig returns the in-page-engine bootstrap, or null when the page is
// not configured for it (the shipped Go-host page). A malformed config is
// treated as absent rather than fatal — the SSE fallback still loads the app.
export function readBootConfig() {
  const c = globalThis.window && window.__EDGE_BOOT__;
  if (!c || typeof c !== "object") return null;
  if (!c.identityId || !c.deviceId || !c.wsUrl || !c.gatewayUrl || !c.token) return null;
  return {
    identityId: c.identityId,
    deviceId: c.deviceId,
    wsUrl: c.wsUrl,
    gatewayUrl: c.gatewayUrl,
    token: c.token,
    types: Array.isArray(c.types) ? c.types : undefined,
    anchors: Array.isArray(c.anchors) ? c.anchors : undefined,
    storeName: c.storeName || undefined,
    wasmUrl: c.wasmUrl || "/edge.wasm",
    wasmExecUrl: c.wasmExecUrl || "/wasm_exec.js",
    shellUrl: c.shellUrl || "/shell/shell.mjs",
  };
}

// assembleEdgeSource wires an already-loaded engine + shell into an edge-source.
// It is separated from the wasm/DOM glue below so the wiring contract — how the
// shell is configured, how start() is called, and that the shell's push target
// is set to the engine's deliver — is unit-testable against fakes without a
// browser. The wasm load itself (boot) is the only untestable glue.
//
//   latticeEdge  the global the wasm main registers ({start(config) -> Promise<api>})
//   createShell  internal/edge/browser/shell's factory
//   cfg          a readBootConfig() result
export async function assembleEdgeSource({ latticeEdge, createShell, cfg, logger = console }) {
  const shell = createShell({
    url: cfg.wsUrl,
    identityId: cfg.identityId,
    deviceId: cfg.deviceId,
    // getToken is a getter, not a fixed string, so nats.js re-supplies the
    // current token on every reconnect (the server drops the connection at
    // authz expiry). In inc 3 the token is static; Fire 3's refresh endpoint
    // makes this return a rotating token without touching this seam.
    getToken: () => cfg.token,
    logger,
  });

  const api = await latticeEdge.start({
    shell,
    identityId: cfg.identityId,
    deviceId: cfg.deviceId,
    gatewayUrl: cfg.gatewayUrl,
    token: cfg.token,
    types: cfg.types,
    anchors: cfg.anchors,
    storeName: cfg.storeName,
  });

  // Wire the shell's push target: the shell's consume loop hands each landed
  // JetStream message to shell.deliver, which must be the engine's api.deliver
  // (internal/edge/browser/shell/shell.mjs — "Set by the page to the wasm
  // host's api.deliver once start() resolves"). Without this the durable feed
  // Naks every message and the mirror never advances.
  shell.deliver = api.deliver;

  return edgeSource(api);
}

// loadScript injects a classic script (wasm_exec.js defines globalThis.Go) and
// resolves once it has run. A module cannot `import` a classic script, so this
// is the standard injection.
function loadScript(src) {
  return new Promise((resolve, reject) => {
    const el = document.createElement("script");
    el.src = src;
    el.onload = () => resolve();
    el.onerror = () => reject(new Error("facet: failed to load " + src));
    document.head.appendChild(el);
  });
}

// boot performs the browser-only load: fetch + instantiate the wasm engine,
// dynamically import the shell (only now — keeping the no-op path from pulling
// the ~440 KB nats.js bundle), and assemble the source. Rejects on any failure
// so app.js's startFeed falls back to SSE rather than stranding the boot.
async function boot(cfg) {
  await loadScript(cfg.wasmExecUrl);
  const go = new globalThis.Go();
  const result = await WebAssembly.instantiateStreaming(fetch(cfg.wasmUrl), go.importObject);
  // Do NOT await go.run: the wasm main parks on select{} and never returns.
  // It runs synchronously up to that park, registering globalThis.latticeEdge
  // before it yields — guarded below in case a future runtime yields earlier.
  go.run(result.instance);
  for (let i = 0; i < 100 && !globalThis.latticeEdge; i++) {
    await new Promise((r) => setTimeout(r, 10));
  }
  if (!globalThis.latticeEdge) throw new Error("facet: wasm engine did not register latticeEdge");
  const { createShell } = await import(cfg.shellUrl);
  return assembleEdgeSource({ latticeEdge: globalThis.latticeEdge, createShell, cfg });
}

const cfg = readBootConfig();
if (cfg) {
  // Set synchronously (the promise, not its result) so app.js's DOMContentLoaded
  // handler — which runs after this module's top level — sees it and awaits the
  // in-page source instead of opening the SSE feed.
  window.__facetBoot = boot(cfg);
}
