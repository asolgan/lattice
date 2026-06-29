package bootstrap

// Proves the bootstrap lens-seeder's postgres support (D1.3): a postgres
// LensDefinition serializes to the same on-wire LensSpec shape Refractor's
// CoreKVSource activates (the bootstrap analog of pkgmgr.lensSpecBody), and the
// nats-kv path is unchanged.

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// decodeTargetConfig pulls the embedded targetConfig (a json.RawMessage in the
// spec body) into a map for field assertions.
func decodeTargetConfig(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	raw, ok := body["targetConfig"].(json.RawMessage)
	require.True(t, ok, "targetConfig must be a json.RawMessage")
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(raw, &cfg))
	return cfg
}

func TestMakeLensSpecBody_PostgresGrantLens(t *testing.T) {
	def := CapabilityReadGrantsLensDefinition()
	body, err := makeLensSpecBody("test-grants-id", def)
	require.NoError(t, err)

	require.Equal(t, "test-grants-id", body["id"])
	require.Equal(t, "capabilityReadGrants", body["canonicalName"])
	require.Equal(t, "postgres", body["targetType"], "a postgres LensDefinition must serialize targetType=postgres")
	require.Equal(t, "full", body["engine"])
	// A plain grant lens carries no actor-aggregate projection plan.
	require.NotContains(t, body, "projectionKind")
	require.NotContains(t, body, "output")
	require.NotContains(t, body, "outputSchema")

	cfg := decodeTargetConfig(t, body)
	require.Equal(t, true, cfg["grantTable"], "grantTable posture must be serialized")
	require.Equal(t, "", cfg["dsn"], "DSN is left empty — Refractor resolves REFRACTOR_PG_DSN at activation")
	// A GrantTable lens omits `key` so Refractor applies the platform grant
	// composite (actor_id, anchor_id, grant_source); a non-grant postgres lens
	// would default to ["key"].
	require.NotContains(t, cfg, "key", "a GrantTable lens must omit key")
	require.NotContains(t, cfg, "protected")
	require.NotContains(t, cfg, "columns")
}

func TestMakeLensSpecBody_NatsKvUnchanged(t *testing.T) {
	// Regression: the existing actor-aggregate NATS-KV base read lens still
	// serializes to nats_kv with its projection plan + bucket, untouched by the
	// postgres branch.
	def := CapabilityReadLensDefinition()
	body, err := makeLensSpecBody("test-capread-id", def)
	require.NoError(t, err)

	require.Equal(t, "nats_kv", body["targetType"])
	require.Equal(t, "actorAggregate", body["projectionKind"])
	require.Contains(t, body, "output")
	cfg := decodeTargetConfig(t, body)
	require.Equal(t, CapabilityKVBucket, cfg["bucket"])
	require.NotContains(t, cfg, "grantTable")
}

// TestCapabilityReadGrantsLensDefinition_Shape pins the lens's declared posture
// (the producer RLS trusts) so a careless edit can't silently change the
// grant_source, drop the GrantTable flag, or repoint the adapter.
func TestCapabilityReadGrantsLensDefinition_Shape(t *testing.T) {
	def := CapabilityReadGrantsLensDefinition()
	require.Equal(t, "capabilityReadGrants", def.CanonicalName)
	require.Equal(t, "postgres", def.Adapter)
	require.True(t, def.GrantTable, "the base read-grant producer must be a GrantTable lens")
	require.False(t, def.Protected)
	require.Empty(t, def.Columns)
	require.Contains(t, def.CypherRule, "nanoIdFromKey(identity.key)")
	require.Contains(t, def.CypherRule, "'cap-read'")
	require.Contains(t, def.CypherRule, "grant_source")
}
