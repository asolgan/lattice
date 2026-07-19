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

// deviceIdKey is the localStorage slot the browser's own device id lives in.
// The device id is browser-local (edge-browser-node-design.md §3.5), NOT handed
// down by the static host: it is persisted so a reload reuses the SAME durable
// consumer (warm resume) instead of orphaning a fresh one per page load.
const deviceIdKey = "facet.deviceId";

// deviceIdAlphabet is the canonical Lattice NanoID charset (internal/substrate/
// keys/nanoid.go) — alphanumeric with no confusables and, critically, none of
// the '.', '*', '>' or whitespace a JetStream consumer name forbids (the id is
// spliced into the durable name `edge-sync-<identity>-<device>`).
const deviceIdAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz123456789";
const deviceIdLength = 20;

function isSafeDeviceId(s) {
  if (typeof s !== "string" || s.length !== deviceIdLength) return false;
  for (const ch of s) if (!deviceIdAlphabet.includes(ch)) return false;
  return true;
}

function randomDeviceId() {
  const bytes = new Uint8Array(deviceIdLength);
  const c = globalThis.crypto;
  if (c && typeof c.getRandomValues === "function") {
    c.getRandomValues(bytes);
  } else {
    // No WebCrypto (a non-secure context) — a device id needs uniqueness, not
    // cryptographic secrecy, so Math.random is an acceptable last resort.
    for (let i = 0; i < deviceIdLength; i++) bytes[i] = Math.floor(Math.random() * 256);
  }
  // The tiny modulo bias (256 % 58 ≠ 0) is irrelevant across a 58^20 space —
  // collisions remain astronomically unlikely.
  let out = "";
  for (let i = 0; i < deviceIdLength; i++) out += deviceIdAlphabet[bytes[i] % deviceIdAlphabet.length];
  return out;
}

// resolveDeviceId returns this browser's stable device id, generating and
// persisting one on first use. A stored id that fails the charset/length check
// (corrupt or from an older scheme) is regenerated. Storage being unavailable
// (private mode, disabled) degrades to an ephemeral per-load id rather than
// failing the boot.
export function resolveDeviceId(storage = globalThis.localStorage) {
  try {
    const existing = storage && storage.getItem(deviceIdKey);
    if (isSafeDeviceId(existing)) return existing;
  } catch { /* storage unavailable — fall through to an ephemeral id */ }
  const id = randomDeviceId();
  try {
    if (storage) storage.setItem(deviceIdKey, id);
  } catch { /* ignore: an ephemeral id still boots, it just won't resume warm */ }
  return id;
}

// readBootConfig returns the in-page-engine bootstrap, or null when the page is
// not configured for it (the shipped Go-host page). A malformed config is
// treated as absent rather than fatal — the SSE fallback still loads the app.
// deviceId is NOT required here: the static host does not inject it (§3.5), so
// an absent one is resolved browser-side in boot() via resolveDeviceId.
export function readBootConfig() {
  const c = globalThis.window && window.__EDGE_BOOT__;
  if (!c || typeof c !== "object") return null;
  if (!c.identityId || !c.wsUrl || !c.gatewayUrl || !c.token) return null;
  return {
    identityId: c.identityId,
    deviceId: c.deviceId || undefined,
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
//   getToken     () => string, the current bearer token — defaults to cfg.token
//                (a fixed snapshot) when omitted; boot() below instead passes a
//                createTokenRefresher getter so this reads the rotating value.
export async function assembleEdgeSource({ latticeEdge, createShell, cfg, getToken, logger = console }) {
  const currentToken = typeof getToken === "function" ? getToken : () => cfg.token;
  const shell = createShell({
    url: cfg.wsUrl,
    identityId: cfg.identityId,
    deviceId: cfg.deviceId,
    // getToken is a getter, not a fixed string, so nats.js re-supplies the
    // current token on every reconnect (the server drops the connection at
    // authz expiry) — and, via createShell's own getToken passthrough, so does
    // the wasm host's Gateway-write submitter (internal/edge/browser/host.go's
    // shellGetTokenFunc). currentToken is createTokenRefresher's live cell,
    // not the cfg.token snapshot: this is what makes a refreshed session
    // actually reach both transports rather than only the page's initial load.
    getToken: currentToken,
    logger,
  });

  const api = await latticeEdge.start({
    shell,
    identityId: cfg.identityId,
    deviceId: cfg.deviceId,
    gatewayUrl: cfg.gatewayUrl,
    token: currentToken(),
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

// sessionRefreshPath is cmd/facet's sliding-session renewal endpoint
// (session.go's handleSessionRefresh). Same-origin POST, so the HttpOnly
// session cookie rides automatically; the response body carries the fresh
// token's raw value for this in-page engine (the cookie alone cannot
// authenticate a WS CONNECT or a fetch Authorization header).
const sessionRefreshPath = "/api/session/refresh";

// refreshIntervalMs sits comfortably inside the session's TTL (devTokenTTL,
// cmd/facet/claim.go — 30 minutes today), so the common case renews with
// margin to spare and never needs the server's sessionRefreshGrace window at
// all. That window exists for the case THIS interval was delayed — a
// backgrounded tab's timers throttled by the browser, a laptop that slept —
// which the visibilitychange check below also targets.
const refreshIntervalMs = 20 * 60 * 1000;

// createTokenRefresher builds the sliding-session renewal loop: refresh()
// does one POST to sessionRefreshPath and, on success, updates the token
// cell getToken() reads; startLoop() wires the interval + tab-visibility
// triggers around it. Split from startLoop so a test can drive refresh()
// directly without a real timer or a DOM (mirrors assembleEdgeSource/boot's
// own split — the logic is unit-testable, the browser glue is not).
//
// A failed refresh (network hiccup, or the session is truly dead past even
// the server's grace window) logs and leaves the cell at its last known
// value — it does not invent a new failure signal. The existing paths
// (nats.js's reconnect, the wasm host's ErrCredentialRejected → revoked
// frame) already surface a credential that never recovers; this loop's job
// is only to make reaching those paths the rare exception instead of the
// 30-minute-in rule.
export function createTokenRefresher(initialToken, { fetchImpl = fetch, logger = console } = {}) {
  let current = initialToken;
  let lastRefreshAt = 0;

  async function refresh() {
    try {
      const res = await fetchImpl(sessionRefreshPath, { method: "POST" });
      if (!res.ok) throw new Error("HTTP " + res.status);
      const body = await res.json();
      if (!body || typeof body.token !== "string" || !body.token) {
        throw new Error("malformed refresh response");
      }
      current = body.token;
      lastRefreshAt = Date.now();
      return true;
    } catch (err) {
      logger.warn?.("facet: session refresh failed; keeping the current token", err?.message ?? err);
      return false;
    }
  }

  function startLoop() {
    if (typeof globalThis.setInterval === "function") {
      globalThis.setInterval(refresh, refreshIntervalMs);
    }
    if (globalThis.document?.addEventListener) {
      globalThis.document.addEventListener("visibilitychange", () => {
        if (globalThis.document.visibilityState === "visible" && Date.now() - lastRefreshAt > refreshIntervalMs) {
          refresh();
        }
      });
    }
  }

  return { getToken: () => current, refresh, startLoop };
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
  // Fill the browser-local device id the static host did not inject (§3.5).
  if (!cfg.deviceId) cfg.deviceId = resolveDeviceId();
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
  const refresher = createTokenRefresher(cfg.token);
  refresher.startLoop();
  return assembleEdgeSource({ latticeEdge: globalThis.latticeEdge, createShell, cfg, getToken: refresher.getToken });
}

const cfg = readBootConfig();
if (cfg) {
  // Set synchronously (the promise, not its result) so app.js's DOMContentLoaded
  // handler — which runs after this module's top level — sees it and awaits the
  // in-page source instead of opening the SSE feed.
  window.__facetBoot = boot(cfg);
}
