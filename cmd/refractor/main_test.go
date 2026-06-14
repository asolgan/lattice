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
