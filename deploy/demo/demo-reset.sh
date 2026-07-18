#!/usr/bin/env bash
# demo-reset.sh — the scheduled reseed: tear the ephemeral stack down (the dev
# compose keeps no volumes, so `make down` IS the data wipe) and bring the
# demo back up with a freshly seeded showcase world. Run nightly by
# lattice-demo-reset.timer; safe to run by hand.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"
export PATH="/usr/local/go/bin:$PATH"

echo "==> Stopping facet..."
pkill -f "bin/facet" 2>/dev/null || true

echo "==> Tearing the stack down (ephemeral — this wipes the demo world)..."
make down

# The per-identity local mirrors are keyed by tenant NanoIDs that die with the
# world — remove them so stale bbolt files don't accumulate across resets.
rm -rf ./facet-store

exec "$REPO_ROOT/deploy/demo/demo-up.sh"
