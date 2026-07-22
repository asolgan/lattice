#!/usr/bin/env bash
# provision-demo-operator.sh — the hosted read-only Loupe's actor (F20): install
# the demo-operator grant package, mint a fresh identity holding ONLY the
# demoOperator role, wait until the platform actually authorizes it, and persist
# the key to loupe-demo-operator.json for demo-up.sh.
#
# The showcase world (and with it this identity) dies with every nightly reset,
# so demo-up.sh runs this on every bring-up. Idempotent per-world: a marker key
# that still authorizes is reused; a stale one (fresh world) is replaced. The
# minted email carries a per-attempt random suffix — email is an identity-index
# dedup key, so a fixed address would wedge every re-mint on the same world.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"
export PATH="/usr/local/go/bin:$PATH"
export HOME="${HOME:-/root}"

NATS_URL="${NATS_URL:-nats://localhost:4222}"
NKEY_CLI="deploy/nkeys/lattice.nk"
NKEY_PKG="deploy/nkeys/lattice-pkg.nk"
BOOTSTRAP_JSON="${BOOTSTRAP_JSON_PATH:-$REPO_ROOT/lattice.bootstrap.json}"
MARKER="$REPO_ROOT/loupe-demo-operator.json"

# authorizes <actorKey>: submits a class-less ctrl.weaver.read as the actor.
# Step-3 authorization precedes step-4 hydration, so AuthDenied means the
# grant is not live while ANY other outcome (the class-less envelope's
# HydrationFailed included) proves the capability doc authorized the read.
authorizes() {
	local out
	out=$(NATS_URL="$NATS_URL" NATS_NKEY="$NKEY_CLI" ./bin/lattice op submit \
		--operation-type ctrl.weaver.read --actor "$1" --output json --payload '{}' 2>&1 || true)
	! grep -q "AuthDenied" <<<"$out"
}

# corePending: how many Core KV CDC events the Refractor pipelines have yet to
# apply, summed across every consumer on the backing stream. Zero-ish means the
# auth plane reflects the graph; a large number means a projection for a
# just-assigned role has not been reached yet.
#
# A world wipe destroys the source stream and its durables, so the Refractor
# rebuilds them and rescans every key for every lens — tens of thousands of
# events, tens of minutes on this box. Waiting on THIS rather than on a fixed
# timeout is the difference between provisioning that works after a reset and
# provisioning that reliably gives up during one.
corePending() {
	curl -s "http://127.0.0.1:8222/jsz?consumers=1&limit=500" 2>/dev/null | python3 -c '
import json,sys
try:
    d=json.load(sys.stdin)
except Exception:
    print(-1); sys.exit(0)
t=0
for a in d.get("account_details",[]):
    for s in a.get("stream_detail",[]):
        if s["name"]!="KV_core-kv": continue
        for c in s.get("consumer_detail",[]): t+=c.get("num_pending",0)
print(t)' 2>/dev/null || echo -1
}

if [[ -f "$MARKER" ]]; then
	existing="$(jq -r '.operatorActorKey // empty' "$MARKER")"
	if [[ -n "$existing" ]] && authorizes "$existing"; then
		echo "==> Demo operator already provisioned + authorized: $existing"
		exit 0
	fi
	echo "==> Stale demo-operator marker (fresh world) — re-provisioning."
fi

echo "==> Installing demo-operator grant package (idempotent)..."
NATS_URL="$NATS_URL" NATS_NKEY="$NKEY_PKG" BOOTSTRAP_JSON_PATH="$BOOTSTRAP_JSON" \
	./bin/lattice-pkg install packages/demo-operator
make provision-readpath >/dev/null
echo "==> Read-path provisioned for demoOperatorReadGrants."

ADMIN_KEY="vtx.identity.$(jq -r '.primordialIDs.bootstrapIdentity' "$BOOTSTRAP_JSON")"
ROLE_KEY="$(go run ./scripts/print-role-id.go demo-operator demoOperator)"
suffix="$(od -An -N4 -tx4 /dev/urandom | tr -d ' ')"
OP_KEY="$(NATS_URL="$NATS_URL" NATS_NKEY="$NKEY_CLI" ./bin/lattice identity create-unclaimed \
	--actor "$ADMIN_KEY" --output json \
	--payload "{\"name\":\"Demo Operator\",\"email\":\"demo-operator-${suffix}@demo.lattice.local\"}" \
	| jq -r '.data.primaryKey')"
[[ -n "$OP_KEY" && "$OP_KEY" != "null" ]] || { echo "provision-demo-operator: identity mint failed" >&2; exit 1; }
echo "==> Demo operator identity: $OP_KEY"
NATS_URL="$NATS_URL" NATS_NKEY="$NKEY_CLI" ./bin/lattice op submit \
	--operation-type AssignRole --actor "$ADMIN_KEY" --output json \
	--payload "{\"actorKey\":\"$OP_KEY\",\"roleKey\":\"$ROLE_KEY\"}" \
	--context-hint-reads "$OP_KEY,$ROLE_KEY" >/dev/null
echo "==> demoOperator assigned; waiting for the capability projection..."

# Wait for the projection, not for a stopwatch. The deadline is generous
# because the honest bound is "however long the rescan takes", and progress is
# reported so a run that is merely slow is distinguishable from one that is
# stuck. While the pipelines still have a backlog the grant simply has not been
# reached yet, so a denial in that window is expected and not evidence of a
# gap; only a denial after the backlog drains means the projection was lost —
# and that is what `lattice lens reproject` exists to repair.
PROVISION_TIMEOUT_SECONDS="${PROVISION_TIMEOUT_SECONDS:-2700}"
deadline=$((SECONDS + PROVISION_TIMEOUT_SECONDS))
lastReport=0
until authorizes "$OP_KEY"; do
	if ((SECONDS >= deadline)); then
		pend="$(corePending)"
		echo "provision-demo-operator: grant never authorized within ${PROVISION_TIMEOUT_SECONDS}s (core-kv pending=${pend})." >&2
		if [[ "$pend" =~ ^[0-9]+$ ]] && ((pend < 20)); then
			echo "provision-demo-operator: the pipelines are drained, so this is a lost projection, not lag." >&2
			echo "provision-demo-operator: heal it with: ./bin/lattice lens reproject <capabilityRoles lensId> --actor-key $OP_KEY" >&2
		fi
		exit 1
	fi
	if ((SECONDS - lastReport >= 60)); then
		lastReport=$SECONDS
		echo "==> still waiting (${SECONDS}s elapsed, core-kv pending=$(corePending))..."
	fi
	sleep 5
done
printf '{"operatorActorKey":"%s"}\n' "$OP_KEY" >"$MARKER"
echo "==> Demo operator ready: $OP_KEY (persisted to $MARKER)"
