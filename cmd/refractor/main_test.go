package main

import (
	"testing"

	"github.com/asolgan/lattice/internal/refractor/lens"
	"github.com/asolgan/lattice/internal/refractor/projection"
)

// TestIsOperationRoleIndexLens asserts the role-index routing predicate fires
// only for a lens that is BOTH keyed solely by operationType AND targets the
// capability-kv bucket (Contract #6 §6.1). A package lens that happens to
// share the operationType key but projects into a different nats_kv bucket
// must not be force-rewritten into the cap.role-by-operation.<op> shape.
func TestIsOperationRoleIndexLens(t *testing.T) {
	tests := []struct {
		name string
		rule *lens.Rule
		want bool
	}{
		{
			name: "real role-index lens (operationType key + capability-kv bucket)",
			rule: &lens.Rule{
				Into: lens.IntoConfig{
					Target: "nats_kv",
					Bucket: projection.AuthPlaneBucket,
					Key:    lens.KeyField{"operationType"},
				},
			},
			want: true,
		},
		{
			name: "package lens with operationType key but a different bucket",
			rule: &lens.Rule{
				Into: lens.IntoConfig{
					Target: "nats_kv",
					Bucket: "some-other-bucket",
					Key:    lens.KeyField{"operationType"},
				},
			},
			want: false,
		},
		{
			name: "capability-kv lens keyed by something else",
			rule: &lens.Rule{
				Into: lens.IntoConfig{
					Target: "nats_kv",
					Bucket: projection.AuthPlaneBucket,
					Key:    lens.KeyField{"actorId"},
				},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isOperationRoleIndexLens(tc.rule); got != tc.want {
				t.Fatalf("isOperationRoleIndexLens() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestThreadsKeyColumns pins the exemption set both the activation path and
// the MATCH-update (hot-reload) path share. A Personal Lens is the case that
// matters most: its reserved "__actor" key field is injected by the envelope
// and is never a RETURN alias, so threading Into.Key at it fails validation
// and REFUSES the update — which silently pins the running pipeline to its
// old cypher until the process restarts, making every Personal Lens cypher
// edit look like it simply did not take.
func TestThreadsKeyColumns(t *testing.T) {
	tests := []struct {
		name string
		rule *lens.Rule
		want bool
	}{
		{
			name: "plain projection lens threads its key columns",
			rule: &lens.Rule{
				Into: lens.IntoConfig{Target: "nats_kv", Bucket: "weaver-targets", Key: lens.KeyField{"entityId"}},
			},
			want: true,
		},
		{
			name: "personal lens is exempt (__actor comes from the envelope)",
			rule: &lens.Rule{
				Into: lens.IntoConfig{Target: "nats_subject", Personal: true, Key: lens.KeyField{"__actor", "ns"}},
			},
			want: false,
		},
		{
			name: "operation-role-index lens is exempt",
			rule: &lens.Rule{
				Into: lens.IntoConfig{Target: "nats_kv", Bucket: projection.AuthPlaneBucket, Key: lens.KeyField{"operationType"}},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := threadsKeyColumns(tc.rule); got != tc.want {
				t.Fatalf("threadsKeyColumns() = %v, want %v", got, tc.want)
			}
		})
	}
}
