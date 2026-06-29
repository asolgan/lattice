package processor

import (
	"context"
	"testing"
	"time"

	"github.com/asolgan/lattice/internal/substrate"
)

func TestLaneSpecs(t *testing.T) {
	handler := func(context.Context, substrate.Message) (substrate.Decision, error) {
		return substrate.Ack, nil
	}
	specs := LaneSpecs("core-operations", handler, 30*time.Second, nil)

	if len(specs) != 4 {
		t.Fatalf("got %d specs, want 4 (one per lane)", len(specs))
	}

	want := map[string]string{ // durable → ops.<lane> subject
		"processor-default": "ops.default",
		"processor-urgent":  "ops.urgent",
		"processor-system":  "ops.system",
		"processor-meta":    "ops.meta",
	}
	seen := map[string]bool{}
	for _, s := range specs {
		subj, ok := want[s.Name]
		if !ok {
			t.Fatalf("unexpected durable %q", s.Name)
		}
		seen[s.Name] = true
		if s.FilterSubject != subj {
			t.Fatalf("durable %q FilterSubject = %q, want %q", s.Name, s.FilterSubject, subj)
		}
		if len(s.FilterSubjects) != 0 {
			t.Fatalf("durable %q set FilterSubjects %v; want single FilterSubject only", s.Name, s.FilterSubjects)
		}
		if s.Stream != "core-operations" {
			t.Fatalf("durable %q Stream = %q, want core-operations", s.Name, s.Stream)
		}
		if s.AckWait != 30*time.Second {
			t.Fatalf("durable %q AckWait = %v, want 30s", s.Name, s.AckWait)
		}
		if s.DeliverPolicy != substrate.DeliverAll {
			t.Fatalf("durable %q DeliverPolicy = %v, want DeliverAll", s.Name, s.DeliverPolicy)
		}
		if s.Handler == nil {
			t.Fatalf("durable %q has nil Handler", s.Name)
		}
		// Only the meta lane is serialized (Contract #2 §3.7); all others leave
		// MaxAckPending at the JetStream default (0).
		wantMAP := 0
		if s.Name == "processor-meta" {
			wantMAP = 1
		}
		if s.MaxAckPending != wantMAP {
			t.Fatalf("durable %q MaxAckPending = %d, want %d", s.Name, s.MaxAckPending, wantMAP)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("missing lane durables; saw %v", seen)
	}
}

func TestLaneDurablesIsACopy(t *testing.T) {
	a := LaneDurables()
	if len(a) != 4 {
		t.Fatalf("LaneDurables len = %d, want 4", len(a))
	}
	a["default"] = "tampered"
	b := LaneDurables()
	if b["default"] != "processor-default" {
		t.Fatalf("LaneDurables returned a shared map; mutation leaked: %q", b["default"])
	}
}
