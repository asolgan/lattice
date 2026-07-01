// Contract #6 §6.14 conformance test for the base ALL-ACCESS read-grant
// PRODUCER lens (capabilityReadWildcardGrants, D1 design §3.4 M5) — the
// wildcard sibling of capabilityReadGrants. It runs the LITERAL bootstrap
// cypher through the same `full` auth-plane engine selected at activation and
// asserts the projected grant rows: exactly one per PROTECTED (kernel-seeded,
// root-equivalent) identity, carrying the reserved WildcardAnchor ("*") —
// never for an ordinary actor. This is exactly the grant the Postgres-RLS
// wildcard OR-clause (internal/refractor/adapter.BuildProtectedTableDDL)
// matches — a root-equivalent actor reads every row of every protected table.
package full_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/asolgan/lattice/internal/bootstrap"
	"github.com/asolgan/lattice/internal/refractor/adapter"
	"github.com/asolgan/lattice/internal/refractor/ruleengine"
	"github.com/asolgan/lattice/internal/refractor/ruleengine/full"
)

func TestCapabilityReadWildcardGrantsLens_ProjectsOnlyProtectedIdentities(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}

	adjKV, coreKV := contractStartKVs(t)

	// TWO protected (root-equivalent, e.g. admin + a service actor) identities
	// and one ordinary identity — proves the WHERE admits every protected
	// identity (not just a coincidental single match) while still excluding
	// the ordinary one.
	adminKey := contractPutVertex(t, coreKV, "identity", "admin", map[string]any{"protected": true})
	loomKey := contractPutVertex(t, coreKV, "identity", "loom", map[string]any{"protected": true})
	_ = contractPutVertex(t, coreKV, "identity", "alice", map[string]any{"name": "alice"})

	body := bootstrap.CapabilityReadWildcardGrantsLensDefinition().CypherRule
	eng := full.New()
	cr, err := eng.Parse(body)
	require.NoError(t, err, "literal capabilityReadWildcardGrants cypher must parse on the full engine")

	out, err := eng.ExecuteWith(context.Background(), cr,
		ruleengine.EventContext{Parameters: map[string]any{}}, adjKV, coreKV)
	require.NoError(t, err, "literal capabilityReadWildcardGrants cypher must execute")
	require.Len(t, out, 2, "one wildcard grant row per protected identity — never the ordinary one")

	byActor := map[string]map[string]any{}
	for _, r := range out {
		actor, ok := r.Values["actor_id"].(string)
		require.True(t, ok, "actor_id must be a string")
		byActor[actor] = r.Values
	}

	for _, k := range []string{adminKey, loomKey} {
		id := nanoFromVertexKey(t, k)
		row, ok := byActor[id]
		require.Truef(t, ok, "missing wildcard grant for %s (bare NanoID %s); got actors %v", k, id, byActor)
		require.Equal(t, id, row["actor_id"], "actor_id is the protected identity's bare NanoID")
		require.Equal(t, adapter.WildcardAnchor, row["anchor_id"], "anchor_id is the reserved WildcardAnchor")
		require.Equal(t, "cap-read.root", row["grant_source"], "grant_source is the wildcard producer's own disjoint slice id")
	}
}
