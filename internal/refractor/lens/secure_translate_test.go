package lens

// LensSpec → Rule conversion coverage for the Secure-Lens secureColumns
// declaration (Contract #3 §3.10): decrypted PII may only land in a
// protected postgres model, every secure column must be a declared column,
// and any other posture fails closed at spec-load time.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func secureSpec(t *testing.T, cfg map[string]any) *LensSpec {
	base := map[string]any{
		"dsn":       "postgres://localhost/test",
		"table":     "read_secure_roster",
		"key":       []string{"identity_id"},
		"protected": true,
		"columns": []map[string]any{
			{"name": "name", "type": "text"},
			{"name": "identity_key", "type": "text"},
		},
		"secureColumns": []map[string]any{
			{"column": "name", "identityKeyColumn": "identity_key", "field": "value"},
		},
	}
	for k, v := range cfg {
		if v == nil {
			delete(base, k)
			continue
		}
		base[k] = v
	}
	return &LensSpec{
		ID:           "pg-secure",
		TargetType:   "postgres",
		CypherRule:   "MATCH (i:identity) RETURN i.key AS identity_id, i.key AS identity_key, i.name.data AS name",
		TargetConfig: mustJSON(t, base),
	}
}

func TestTranslateSpec_SecureColumns_Threaded(t *testing.T) {
	r, err := translateSpec(secureSpec(t, nil))
	require.NoError(t, err)
	require.Len(t, r.Into.SecureColumns, 1)
	assert.Equal(t, SecureColumn{Column: "name", IdentityKeyColumn: "identity_key", Field: "value"}, r.Into.SecureColumns[0])
	assert.True(t, r.Into.Protected)
}

func TestTranslateSpec_SecureColumns_RequireProtected(t *testing.T) {
	_, err := translateSpec(secureSpec(t, map[string]any{"protected": nil}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "protected")
}

func TestTranslateSpec_SecureColumns_PublicRejected(t *testing.T) {
	_, err := translateSpec(secureSpec(t, map[string]any{"protected": nil, "public": true}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "protected")
}

func TestTranslateSpec_SecureColumns_GrantTableRejected(t *testing.T) {
	_, err := translateSpec(secureSpec(t, map[string]any{"grantTable": true}))
	require.Error(t, err)
}

func TestTranslateSpec_SecureColumns_ActorAggregateRejected(t *testing.T) {
	spec := secureSpec(t, nil)
	spec.ProjectionKind = "actorAggregate"
	_, err := translateSpec(spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plain projection")
}

func TestTranslateSpec_SecureColumns_UndeclaredColumnRejected(t *testing.T) {
	_, err := translateSpec(secureSpec(t, map[string]any{
		"secureColumns": []map[string]any{
			{"column": "ssn", "identityKeyColumn": "identity_key"},
		},
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not among the declared")
}

func TestTranslateSpec_SecureColumns_MissingIdentityKeyColumnRejected(t *testing.T) {
	_, err := translateSpec(secureSpec(t, map[string]any{
		"secureColumns": []map[string]any{
			{"column": "name"},
		},
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "identityKeyColumn")
}

func TestTranslateSpec_SecureColumns_DuplicateRejected(t *testing.T) {
	_, err := translateSpec(secureSpec(t, map[string]any{
		"secureColumns": []map[string]any{
			{"column": "name", "identityKeyColumn": "identity_key"},
			{"column": "name", "identityKeyColumn": "identity_key", "field": "value"},
		},
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "twice")
}

func TestTranslateSpec_SecureColumns_ReservedRLSColumnRejected(t *testing.T) {
	_, err := translateSpec(secureSpec(t, map[string]any{
		"columns": []map[string]any{
			{"name": "authz_anchors", "type": "text[]"},
			{"name": "identity_key", "type": "text"},
		},
		"secureColumns": []map[string]any{
			{"column": "authz_anchors", "identityKeyColumn": "identity_key"},
		},
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "platform RLS column")

	_, err = translateSpec(secureSpec(t, map[string]any{
		"secureColumns": []map[string]any{
			{"column": "name", "identityKeyColumn": "projection_seq"},
		},
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "platform RLS column")
}

func TestTranslateSpec_SecureColumns_KeyColumnRejected(t *testing.T) {
	_, err := translateSpec(secureSpec(t, map[string]any{
		"columns": []map[string]any{
			{"name": "identity_id", "type": "text"},
			{"name": "identity_key", "type": "text"},
		},
		"secureColumns": []map[string]any{
			{"column": "identity_id", "identityKeyColumn": "identity_key"},
		},
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output-key column")
}

func TestTranslateSpec_SecureColumns_UndeclaredIdentityKeyColumnRejected(t *testing.T) {
	_, err := translateSpec(secureSpec(t, map[string]any{
		"columns": []map[string]any{
			{"name": "name", "type": "text"},
		},
		"secureColumns": []map[string]any{
			{"column": "name", "identityKeyColumn": "identity_key"},
		},
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "identityKeyColumn")
}

func TestTranslateSpec_SecureColumns_IdentityKeyAsKeyColumnAllowed(t *testing.T) {
	// The identity-key column may be an output-key column (it is not
	// rewritten by the decryptor, only read).
	r, err := translateSpec(secureSpec(t, map[string]any{
		"key": []string{"identity_key"},
		"columns": []map[string]any{
			{"name": "name", "type": "text"},
		},
		"secureColumns": []map[string]any{
			{"column": "name", "identityKeyColumn": "identity_key"},
		},
	}))
	require.NoError(t, err)
	require.Len(t, r.Into.SecureColumns, 1)
}

func TestTranslateSpec_SecureColumns_NATSKVRejected(t *testing.T) {
	spec := &LensSpec{
		ID:         "kv-secure",
		TargetType: "nats_kv",
		CypherRule: "MATCH (i:identity) RETURN i.key AS key, i.name.data AS name",
		TargetConfig: mustJSON(t, map[string]any{
			"bucket": "roster",
			"key":    []string{"key"},
			"secureColumns": []map[string]any{
				{"column": "name", "identityKeyColumn": "key"},
			},
		}),
	}
	_, err := translateSpec(spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RLS")
}
