# deploy/demo — hosted live-demo deployment (Facet over the showcase world)

Runs the **stock dev-stack recipe** (docker compose NATS + Postgres, Makefile-launched host
processes) on one small VPS, with exactly one public surface: a reverse proxy (Caddy, TLS) in front
of Facet's loopback HTTP listener (`127.0.0.1:7810`). Everything else — NATS (4222/8222/9222),
Postgres (5432), Gateway (8080), Loupe (7777) — stays loopback-bound via `.env` host-IP port
bindings (`env.demo`) and is additionally expected to sit behind the provider's network firewall
(inbound 22/80/443 only). Note Docker publishes ports past host firewalls like ufw — use the
provider's network-level firewall, not ufw, as the outer wall.

Facet runs in **demo-persona posture** (`FACET_DEMO_PERSONAS`, see `cmd/facet/main.go`): the login
page offers one-tap persona cards, the dev-login minter refuses non-persona subjects, and
`/api/claim` is disabled. The world is the idempotent showcase dataset (`make seed-showcase`); a
nightly systemd timer tears the ephemeral stack down and reseeds it, rotating the persona ids.

## Files

- `demo-bootstrap.sh <host>` — one-time (idempotent) box setup: installs Docker + Go + jq + Caddy,
  writes `.env` from `env.demo`, installs the Caddyfile for `<host>`, runs `demo-up.sh`, enables the
  systemd boot service + nightly reset timer. Ubuntu 24.04, run as root from this directory.
- `demo-up.sh` — bring the full stack + Facet up against the current checkout: the `up-facet` chain
  (up-full → provisions → showcase installs → seed) with Facet started under `FACET_DEMO_PERSONAS`
  built from the seed's printed tenant ids. Safe to re-run.
- `demo-reset.sh` — the nightly reset: stop apps, `docker compose down` (the dev stack is ephemeral
  by design — no volumes — so this IS the wipe), then `demo-up.sh` again.
- `Caddyfile` — TLS + reverse proxy to Facet, SSE-safe (`flush_interval -1`). Reads `{$DEMO_HOST}`;
  the bootstrap installs it to `/etc/caddy/Caddyfile` with the host substituted.
- `env.demo` — compose `.env` template binding every published container port to `127.0.0.1`.
- `systemd/` — `lattice-demo.service` (runs `demo-up.sh` at boot), `lattice-demo-reset.service` +
  `lattice-demo-reset.timer` (nightly 09:10 UTC reset).

## Bring-up (after DNS `<host>` → the box, port 80/443 reachable)

```sh
git clone <repo-url> /opt/lattice
cd /opt/lattice/deploy/demo
./demo-bootstrap.sh demo.example.com
```

Verify: `https://<host>/login` shows the persona cards; sign in; request the laundry service; the
outbox entry confirms (DONE). The operator inspector is deliberately not exposed — reach Loupe via
`ssh -L 7777:127.0.0.1:7777 <box>`.

## Operational notes

- **Reset cadence**: `lattice-demo-reset.timer` (09:10 UTC). Manual reset: `systemctl start
  lattice-demo-reset.service`. Logs: `journalctl -u lattice-demo*` plus the stack's own `*.log`
  files in `/opt/lattice`.
- **Update**: `cd /opt/lattice && git pull && deploy/demo/demo-reset.sh` (rebuilds binaries, fresh
  world).
- **Sizing**: ~10 Go host processes + 2 containers ≈ well under 1 GB RSS; 2 vCPU / 4 GB is
  comfortable, builds included. All deps are pure Go — x86 and ARM both fine.
- **Demo blast radius**: visitors act only as the seeded consumer personas through the real Gateway
  capability authz; worst case is demo-world graffiti until the next reset. Before announcing the
  URL widely, put a rate-limiting proxy/WAF (e.g. Cloudflare) in front of `/api/*`.
