package loom

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// A pattern spec may supply its own human-readable patternId, distinct from the
// source vertex's NanoID. When it does, the loaded Pattern must still carry the
// real vtx.meta.<NanoID> as MetaKey — the key a dispatched step op stamps as
// authContext.target. The human patternId must never become the meta-vertex
// reference (a forbidden vtx.meta.<canonicalName> shape per Contract #1).
func TestPatternSourceMetaKeyIsVertexKeyNotPatternID(t *testing.T) {
	src := newPatternSource(nil, "core-kv", "loom-test", nil)

	var loaded *Pattern
	src.setLoadCallback(func(p *Pattern) { loaded = p })

	const vertexID = "abc123NanoID" // the vtx.meta.<id> suffix
	specBody, err := json.Marshal(Pattern{
		PatternID:   "humanReadableName", // spec-supplied, deliberately != vertexID
		SubjectType: "identity",
		Steps:       []Step{{Kind: StepKindSystemOp, Operation: "DoThing"}},
	})
	require.NoError(t, err)

	src.dispatchSpec(vertexID, specBody)

	require.NotNil(t, loaded, "a valid spec should load")
	require.Equal(t, "humanReadableName", loaded.PatternID, "spec-supplied patternId is preserved")
	require.Equal(t, "vtx.meta."+vertexID, loaded.MetaKey,
		"MetaKey must be the real vertex key, never vtx.meta.<humanName>")
}

// When the spec omits patternId, it falls back to the vertex id and MetaKey is
// the matching vertex key — the common case must be unchanged.
func TestPatternSourceMetaKeyFallbackWhenPatternIDOmitted(t *testing.T) {
	src := newPatternSource(nil, "core-kv", "loom-test", nil)

	var loaded *Pattern
	src.setLoadCallback(func(p *Pattern) { loaded = p })

	const vertexID = "xyz789NanoID"
	specBody, err := json.Marshal(Pattern{
		SubjectType: "identity",
		Steps:       []Step{{Kind: StepKindSystemOp, Operation: "DoThing"}},
	})
	require.NoError(t, err)

	src.dispatchSpec(vertexID, specBody)

	require.NotNil(t, loaded, "a valid spec should load")
	require.Equal(t, vertexID, loaded.PatternID, "patternId falls back to the vertex id")
	require.Equal(t, "vtx.meta."+vertexID, loaded.MetaKey)
}
