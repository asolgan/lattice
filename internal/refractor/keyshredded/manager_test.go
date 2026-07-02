package keyshredded

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/asolgan/lattice/internal/refractor/control"
	"github.com/asolgan/lattice/internal/refractor/failure"
	"github.com/asolgan/lattice/internal/substrate"
)

// fakeNullifier is a control.RowNullifier test double. Delete returns err
// (nil for success) and records every call.
type fakeNullifier struct {
	err   error
	calls []map[string]any
}

func (f *fakeNullifier) Delete(_ context.Context, keys map[string]any, _ uint64) error {
	f.calls = append(f.calls, keys)
	return f.err
}

// fakePauser is a control.Pauser test double recording whether Pause was called.
type fakePauser struct {
	paused bool
}

func (f *fakePauser) Pause(_ context.Context) { f.paused = true }

func newTestManager(t *testing.T, svc *control.Service, targets []NullifyTarget) *Manager {
	t.Helper()
	return New(Config{Control: svc, Targets: targets})
}

func keyShreddedMsg(t *testing.T, identityKey string) substrate.Message {
	t.Helper()
	body := []byte(`{"payload":{"identityKey":"` + identityKey + `"}}`)
	return substrate.Message{Body: body}
}

func TestHandleKeyShredded_NoTargets_AcksAndCounts(t *testing.T) {
	svc := control.NewService()
	m := newTestManager(t, svc, nil)

	decision := m.handleKeyShredded(context.Background(), keyShreddedMsg(t, "vtx.identity.AAAAAAAAAAAAAAAAAAAA"))

	require.Equal(t, substrate.Ack, decision)
	require.Equal(t, uint64(1), m.HandledTotal())
}

func TestHandleKeyShredded_TargetSucceeds_DeletesAndAcks(t *testing.T) {
	svc := control.NewService()
	nullifier := &fakeNullifier{}
	svc.RegisterRowNullifier("lens-a", nullifier)
	m := newTestManager(t, svc, []NullifyTarget{{RuleID: "lens-a", KeyField: "identityKey"}})

	decision := m.handleKeyShredded(context.Background(), keyShreddedMsg(t, "vtx.identity.AAAAAAAAAAAAAAAAAAAA"))

	require.Equal(t, substrate.Ack, decision)
	require.Equal(t, uint64(1), m.HandledTotal())
	require.Len(t, nullifier.calls, 1)
	require.Equal(t, "vtx.identity.AAAAAAAAAAAAAAAAAAAA", nullifier.calls[0]["identityKey"])
}

func TestHandleKeyShredded_MultipleTargets_AllAttempted(t *testing.T) {
	svc := control.NewService()
	nullifierA := &fakeNullifier{}
	nullifierB := &fakeNullifier{}
	svc.RegisterRowNullifier("lens-a", nullifierA)
	svc.RegisterRowNullifier("lens-b", nullifierB)
	m := newTestManager(t, svc, []NullifyTarget{
		{RuleID: "lens-a", KeyField: "identityKey"},
		{RuleID: "lens-b", KeyField: "identityKey"},
	})

	decision := m.handleKeyShredded(context.Background(), keyShreddedMsg(t, "vtx.identity.AAAAAAAAAAAAAAAAAAAA"))

	require.Equal(t, substrate.Ack, decision)
	require.Len(t, nullifierA.calls, 1)
	require.Len(t, nullifierB.calls, 1)
}

// TestHandleKeyShredded_TargetNotRegistered_NaksForRedelivery covers the
// still-starting-up case: a configured target whose lens hasn't registered
// yet is treated as transient (redeliver), not privacy-critical.
func TestHandleKeyShredded_TargetNotRegistered_NaksForRedelivery(t *testing.T) {
	svc := control.NewService() // lens-a never registered
	m := newTestManager(t, svc, []NullifyTarget{{RuleID: "lens-a", KeyField: "identityKey"}})

	decision := m.handleKeyShredded(context.Background(), keyShreddedMsg(t, "vtx.identity.AAAAAAAAAAAAAAAAAAAA"))

	require.Equal(t, substrate.NakWithDelay, decision)
	require.Equal(t, uint64(0), m.HandledTotal(), "not-yet-registered must not count as handled")
}

// TestHandleKeyShredded_TargetNeverRegisters_GivesUpAfterMaxDeliveries proves
// a permanently-misconfigured RuleID (a typo'd/decommissioned target) stops
// nak-looping once NumDelivered reaches maxNotRegisteredDeliveries, instead
// of retrying forever.
func TestHandleKeyShredded_TargetNeverRegisters_GivesUpAfterMaxDeliveries(t *testing.T) {
	svc := control.NewService() // lens-a never registered
	m := newTestManager(t, svc, []NullifyTarget{{RuleID: "lens-a", KeyField: "identityKey"}})

	msg := keyShreddedMsg(t, "vtx.identity.AAAAAAAAAAAAAAAAAAAA")
	msg.NumDelivered = maxNotRegisteredDeliveries

	decision := m.handleKeyShredded(context.Background(), msg)

	require.Equal(t, substrate.Ack, decision, "must give up (Ack) rather than nak forever once the threshold is reached")
	require.Equal(t, uint64(1), m.HandledTotal())
}

// TestNew_NilControl_Panics proves a misconfigured Manager fails at
// construction (fail fast) rather than mid-stream on the first real event.
func TestNew_NilControl_Panics(t *testing.T) {
	require.Panics(t, func() {
		New(Config{Control: nil})
	})
}

// TestHandleKeyShredded_NullifyFails_RaisesPrivacyCriticalPausesNoRetry is the
// failure-tier proof (vault-crypto-shredding-design.md §6 "a forced
// nullification failure raises the privacy-critical tier — lens halts, no
// retry, alert emitted"): a real Delete failure must pause the affected lens
// and Ack (never retry) rather than Nak.
func TestHandleKeyShredded_NullifyFails_RaisesPrivacyCriticalPausesNoRetry(t *testing.T) {
	svc := control.NewService()
	boom := errors.New("adapter: boom")
	nullifier := &fakeNullifier{err: boom}
	pauser := &fakePauser{}
	svc.RegisterRowNullifier("lens-a", nullifier)
	svc.RegisterPauser("lens-a", pauser)
	m := newTestManager(t, svc, []NullifyTarget{{RuleID: "lens-a", KeyField: "identityKey"}})

	decision := m.handleKeyShredded(context.Background(), keyShreddedMsg(t, "vtx.identity.AAAAAAAAAAAAAAAAAAAA"))

	require.Equal(t, substrate.Ack, decision, "a privacy-critical failure must never be retried")
	require.True(t, pauser.paused, "the affected lens must be paused")
	require.Equal(t, uint64(1), m.HandledTotal())
}

// TestHandleKeyShredded_OneTargetFailsAnotherSucceeds_BothAttempted proves a
// privacy-critical failure on one target does not skip the remaining ones.
func TestHandleKeyShredded_OneTargetFailsAnotherSucceeds_BothAttempted(t *testing.T) {
	svc := control.NewService()
	failing := &fakeNullifier{err: errors.New("boom")}
	ok := &fakeNullifier{}
	pauser := &fakePauser{}
	svc.RegisterRowNullifier("lens-fail", failing)
	svc.RegisterPauser("lens-fail", pauser)
	svc.RegisterRowNullifier("lens-ok", ok)
	m := newTestManager(t, svc, []NullifyTarget{
		{RuleID: "lens-fail", KeyField: "identityKey"},
		{RuleID: "lens-ok", KeyField: "identityKey"},
	})

	decision := m.handleKeyShredded(context.Background(), keyShreddedMsg(t, "vtx.identity.AAAAAAAAAAAAAAAAAAAA"))

	require.Equal(t, substrate.Ack, decision)
	require.True(t, pauser.paused)
	require.Len(t, failing.calls, 1)
	require.Len(t, ok.calls, 1, "the second target must still be attempted after the first fails")
}

func TestHandleKeyShredded_EmptyBody_Acks(t *testing.T) {
	svc := control.NewService()
	m := newTestManager(t, svc, nil)

	decision := m.handleKeyShredded(context.Background(), substrate.Message{})

	require.Equal(t, substrate.Ack, decision)
}

func TestHandleKeyShredded_UnparseableBody_Terms(t *testing.T) {
	svc := control.NewService()
	m := newTestManager(t, svc, nil)

	decision := m.handleKeyShredded(context.Background(), substrate.Message{Body: []byte("not json")})

	require.Equal(t, substrate.Term, decision)
}

func TestHandleKeyShredded_MissingIdentityKey_Terms(t *testing.T) {
	svc := control.NewService()
	m := newTestManager(t, svc, nil)

	decision := m.handleKeyShredded(context.Background(), substrate.Message{Body: []byte(`{"payload":{}}`)})

	require.Equal(t, substrate.Term, decision)
}

// TestFailurePrivacyCritical_Classify proves the new failure.PrivacyCritical
// tier round-trips through failure.Classify (mirrors the pattern each of the
// other three tiers already covers).
func TestFailurePrivacyCritical_Classify(t *testing.T) {
	err := failure.PrivacyCritical(errors.New("row nullify failed"))
	require.Equal(t, failure.CatPrivacyCritical, failure.Classify(err))
}
