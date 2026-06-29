package bridge_test

import (
	"context"
	"testing"

	"github.com/asolgan/lattice/internal/bridge"
)

// TestFakeAugur_HappyPath: a benign Subject yields a Resolved, OutcomeCompleted
// dispatch carrying a VALID, in-scope assignTask proposal scoped to the escalated
// candidate (read from Params["entityId"]). The reasoning side-effect is recorded
// exactly once.
func TestFakeAugur_HappyPath(t *testing.T) {
	a := bridge.NewFakeAugur()
	entity := "vtx.leaseapp.applicant1"
	disp, err := a.Execute(context.Background(), bridge.Request{
		IdempotencyKey: "aug-1",
		Subject:        "aug-1",
		Params:         map[string]string{"entityId": entity},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if disp.Disposition != bridge.Resolved {
		t.Fatalf("FakeAugur is synchronous: Disposition = %v, want Resolved", disp.Disposition)
	}
	if disp.Result.Status != bridge.OutcomeCompleted {
		t.Fatalf("happy path Status = %q, want %q", disp.Result.Status, bridge.OutcomeCompleted)
	}
	p, err := bridge.DecodeAugurProposal(disp.Result.Detail)
	if err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	if p.Action != "assignTask" {
		t.Fatalf("happy proposal action = %q, want assignTask", p.Action)
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		t.Fatalf("happy proposal confidence out of range: %v", p.Confidence)
	}
	if got, _ := p.Params["scopedTo"].(string); got != entity {
		t.Fatalf("happy proposal must scope to the escalated candidate: scopedTo = %q, want %q", got, entity)
	}
	if got := a.SideEffects("aug-1"); got != 1 {
		t.Fatalf("one reasoning call performed: side effects = %d, want 1", got)
	}
}

// TestFakeAugur_IdempotentOnRepeatedKey is the cost-control proof: a repeat
// idempotencyKey returns the SAME proposal and performs NO second reasoning call
// (at most one billed model call per escalation episode, even under redelivery).
func TestFakeAugur_IdempotentOnRepeatedKey(t *testing.T) {
	a := bridge.NewFakeAugur()
	req := bridge.Request{IdempotencyKey: "aug-rep", Subject: "aug-rep", Params: map[string]string{"entityId": "vtx.leaseapp.x"}}
	first, err := a.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	for i := 0; i < 3; i++ {
		again, err := a.Execute(context.Background(), req)
		if err != nil {
			t.Fatalf("repeat Execute: %v", err)
		}
		if again.Result.Detail != first.Result.Detail {
			t.Fatalf("repeat key must replay the same proposal:\n first = %q\n again = %q", first.Result.Detail, again.Result.Detail)
		}
	}
	if got := a.SideEffects("aug-rep"); got != 1 {
		t.Fatalf("repeat key must perform exactly one reasoning call: side effects = %d, want 1", got)
	}
}

// TestFakeAugur_AdversarialTriggers: each crafted-malicious trigger Subject
// produces the shape the §5 record-time validator must DEFEND against. FakeAugur
// only PRODUCES these proposals — catching them is RecordProposal's job; this test
// pins that the fixtures the e2e/adversarial tests rely on are well-formed.
func TestFakeAugur_AdversarialTriggers(t *testing.T) {
	a := bridge.NewFakeAugur()
	entity := "vtx.leaseapp.candidate"

	// scope escape — a directOp targeting a DIFFERENT entity.
	scope, err := a.Execute(context.Background(), bridge.Request{IdempotencyKey: "k1", Subject: bridge.AugurScopeEscapeSubject, Params: map[string]string{"entityId": entity}})
	if err != nil {
		t.Fatalf("scope-escape Execute: %v", err)
	}
	ps, err := bridge.DecodeAugurProposal(scope.Result.Detail)
	if err != nil {
		t.Fatalf("decode scope-escape: %v", err)
	}
	if got, _ := ps.Params["scopedTo"].(string); got == entity || got == "" {
		t.Fatalf("scope-escape proposal must target a foreign entity, got scopedTo = %q", got)
	}

	// unknown action — outside {triggerLoom, assignTask, directOp}.
	unk, err := a.Execute(context.Background(), bridge.Request{IdempotencyKey: "k2", Subject: bridge.AugurUnknownActionSubject, Params: map[string]string{"entityId": entity}})
	if err != nil {
		t.Fatalf("unknown-action Execute: %v", err)
	}
	pu, _ := bridge.DecodeAugurProposal(unk.Result.Detail)
	for _, ok := range []string{"triggerLoom", "assignTask", "directOp"} {
		if pu.Action == ok {
			t.Fatalf("unknown-action proposal must NOT name an allowed action, got %q", pu.Action)
		}
	}

	// bad confidence — structurally valid action, confidence outside [0,1].
	bad, err := a.Execute(context.Background(), bridge.Request{IdempotencyKey: "k3", Subject: bridge.AugurBadConfidenceSubject, Params: map[string]string{"entityId": entity}})
	if err != nil {
		t.Fatalf("bad-confidence Execute: %v", err)
	}
	pb, _ := bridge.DecodeAugurProposal(bad.Result.Detail)
	if pb.Confidence >= 0 && pb.Confidence <= 1 {
		t.Fatalf("bad-confidence proposal must be out of [0,1], got %v", pb.Confidence)
	}
}

// TestFakeAugur_Refusal: a modeled model refusal is a terminal OutcomeFailed
// (err == nil — a definitive verdict the bridge must not retry), carries no
// proposal, and performs no reasoning side-effect.
func TestFakeAugur_Refusal(t *testing.T) {
	a := bridge.NewFakeAugur()
	disp, err := a.Execute(context.Background(), bridge.Request{IdempotencyKey: "ref", Subject: bridge.AugurRefusalSubject})
	if err != nil {
		t.Fatalf("a refusal is a terminal verdict, not a transient error: %v", err)
	}
	if disp.Result.Status != bridge.OutcomeFailed {
		t.Fatalf("refusal Status = %q, want %q", disp.Result.Status, bridge.OutcomeFailed)
	}
	if got := a.SideEffects("ref"); got != 0 {
		t.Fatalf("a refusal performs no reasoning side-effect: side effects = %d, want 0", got)
	}
}

// TestFakeAugur_SetProposalOverride: the injection seam returns an arbitrary
// proposal for a non-trigger Subject, while a trigger Subject still wins.
func TestFakeAugur_SetProposalOverride(t *testing.T) {
	a := bridge.NewFakeAugur()
	a.SetProposal(bridge.AugurProposal{Action: "triggerLoom", Confidence: 0.5, Params: map[string]any{"pattern": "p"}})
	disp, err := a.Execute(context.Background(), bridge.Request{IdempotencyKey: "ov", Subject: "ov"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	p, _ := bridge.DecodeAugurProposal(disp.Result.Detail)
	if p.Action != "triggerLoom" {
		t.Fatalf("override action = %q, want triggerLoom", p.Action)
	}

	// A trigger subject still selects its adversarial shape, not the override.
	disp2, err := a.Execute(context.Background(), bridge.Request{IdempotencyKey: "ov2", Subject: bridge.AugurUnknownActionSubject})
	if err != nil {
		t.Fatalf("Execute trigger: %v", err)
	}
	p2, _ := bridge.DecodeAugurProposal(disp2.Result.Detail)
	if p2.Action == "triggerLoom" {
		t.Fatalf("a trigger subject must win over the override")
	}
}

// TestFakeAugur_PollUnsupported: the synchronous adapter never returns Pending,
// so a routed Poll is a wiring bug — a clear error, not a silent zero Dispatch.
func TestFakeAugur_PollUnsupported(t *testing.T) {
	a := bridge.NewFakeAugur()
	if _, err := a.Poll(context.Background(), "ref"); err == nil {
		t.Fatalf("Poll on a synchronous adapter must error")
	}
}
