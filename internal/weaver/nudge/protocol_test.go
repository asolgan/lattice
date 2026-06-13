package nudge_test

import (
	"context"
	"errors"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/asolgan/lattice/internal/substrate"
	"github.com/asolgan/lattice/internal/weaver/nudge"
)

// newClaimStore starts an embedded NATS server with a TTL-capable weaver-claims
// bucket and returns a ClaimStore against it.
func newClaimStore(t *testing.T, ctx context.Context) *nudge.ClaimStore {
	t.Helper()
	opts := &natsserver.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir()}
	srv := natstest.RunServer(opts)
	t.Cleanup(srv.Shutdown)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)
	conn, err := substrate.Wrap(nc)
	if err != nil {
		t.Fatalf("substrate wrap: %v", err)
	}
	if _, err := conn.JetStream().CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: "weaver-claims", LimitMarkerTTL: time.Second,
	}); err != nil {
		t.Fatalf("create weaver-claims: %v", err)
	}
	return nudge.NewClaimStore(conn, "weaver-claims", 90*24*time.Hour)
}

// failingAdapter always hard-fails — proves the failed-state transition.
type failingAdapter struct{}

func (failingAdapter) Execute(context.Context, nudge.Request) (nudge.Result, error) {
	return nudge.Result{}, errors.New("external system unavailable")
}

func dispatch(claimID string) nudge.Dispatch {
	return nudge.Dispatch{
		ClaimID:   claimID,
		Adapter:   "backgroundCheck",
		Operation: "ResolveCheck",
		Subject:   "vtx.identity.subjectABCDEF12",
		Params:    map[string]string{"reason": "lease-app"},
	}
}

// TestClaimStore_WriteRecordsClaimedShape verifies the frozen §10.3 record:
// state=claimed, idempotencyKey == claimId, claimedAt set.
func TestClaimStore_WriteRecordsClaimedShape(t *testing.T) {
	ctx := context.Background()
	cs := newClaimStore(t, ctx)

	rec, err := cs.Write(ctx, "claim-abc", "backgroundCheck", "ResolveCheck", "vtx.identity.x", map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if rec.State != nudge.StateClaimed {
		t.Errorf("state = %q, want %q", rec.State, nudge.StateClaimed)
	}
	if rec.IdempotencyKey != rec.ClaimID {
		t.Errorf("idempotencyKey %q != claimId %q (invariant)", rec.IdempotencyKey, rec.ClaimID)
	}
	if rec.ClaimedAt == "" {
		t.Error("claimedAt not set")
	}

	got, found, err := cs.Get(ctx, "claim-abc")
	if err != nil || !found {
		t.Fatalf("Get after Write: found=%v err=%v", found, err)
	}
	if got.State != nudge.StateClaimed {
		t.Errorf("persisted state = %q, want claimed", got.State)
	}
}

func TestClaimStore_WriteRejectsBlankClaimID(t *testing.T) {
	ctx := context.Background()
	cs := newClaimStore(t, ctx)
	if _, err := cs.Write(ctx, "", "a", "o", "s", nil); err == nil {
		t.Fatal("Write: want error for a blank claimId")
	}
}

func TestClaimStore_AdvanceIdempotentAndTerminalGuard(t *testing.T) {
	ctx := context.Background()
	cs := newClaimStore(t, ctx)
	if _, err := cs.Write(ctx, "c1", "a", "o", "s", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Advance(ctx, "c1", nudge.StateExecuting); err != nil {
		t.Fatalf("advance to executing: %v", err)
	}
	// Re-advancing to the same state is a no-op.
	if _, err := cs.Advance(ctx, "c1", nudge.StateExecuting); err != nil {
		t.Fatalf("re-advance to executing should be a no-op: %v", err)
	}
	if _, err := cs.Advance(ctx, "c1", nudge.StateFailed); err != nil {
		t.Fatalf("advance to failed: %v", err)
	}
	// A terminal claim cannot move to a different terminal state.
	if _, err := cs.Advance(ctx, "c1", nudge.StateResolved); err == nil {
		t.Fatal("advance failed→resolved: want error (terminal guard)")
	}
}

// TestNudger_RunAdvancesClaimedExecutingResolved is the full claim→execute→
// resolve happy path: the claim is written claimed BEFORE the adapter runs, the
// adapter acts once, and the claim lands resolved with resolveRef set.
func TestNudger_RunAdvancesClaimedExecutingResolved(t *testing.T) {
	ctx := context.Background()
	cs := newClaimStore(t, ctx)
	fake := nudge.NewFakeBackgroundCheck()

	// Wrap the adapter so we can assert the claim is in state=executing at the
	// moment the adapter is called — proving the claim was written (claimed) and
	// advanced before any external call (NFR-S11 visible-claim-before-execute).
	var stateAtExecute nudge.ClaimState
	probeFake := nudge.AdapterFunc(func(ctx context.Context, req nudge.Request) (nudge.Result, error) {
		rec, _, _ := cs.Get(ctx, req.IdempotencyKey)
		stateAtExecute = rec.State
		return fake.Execute(ctx, req)
	})
	reg := nudge.NewRegistry()
	if err := reg.Register("backgroundCheck", probeFake); err != nil {
		t.Fatal(err)
	}
	n := nudge.NewNudger(cs, reg)

	resolveCalls := 0
	resolve := func(ctx context.Context, claimID string, result nudge.Result) (string, error) {
		resolveCalls++
		return "req-" + claimID, nil
	}

	rec, err := n.Run(ctx, dispatch("claim-run-1"), resolve)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stateAtExecute != nudge.StateExecuting {
		t.Errorf("state at adapter Execute = %q, want executing (claim written + advanced before the call)", stateAtExecute)
	}
	if rec.State != nudge.StateResolved {
		t.Errorf("final state = %q, want resolved", rec.State)
	}
	if rec.ResolveRef != "req-claim-run-1" {
		t.Errorf("resolveRef = %q, want the resolve op requestId", rec.ResolveRef)
	}
	if rec.ResolvedAt == "" {
		t.Error("resolvedAt not set on resolved claim")
	}
	if fake.SideEffects("claim-run-1") != 1 {
		t.Errorf("adapter side effects = %d, want 1", fake.SideEffects("claim-run-1"))
	}
	if resolveCalls != 1 {
		t.Errorf("resolve op submitted %d times, want 1", resolveCalls)
	}
}

func TestNudger_RunMissingAdapterIsConfigError(t *testing.T) {
	ctx := context.Background()
	cs := newClaimStore(t, ctx)
	n := nudge.NewNudger(cs, nudge.NewRegistry())
	_, err := n.Run(ctx, dispatch("c-x"), func(context.Context, string, nudge.Result) (string, error) { return "", nil })
	if err == nil {
		t.Fatal("Run with no registered adapter: want a config error")
	}
}

func TestNudger_RunAdapterFailureLandsFailed(t *testing.T) {
	ctx := context.Background()
	cs := newClaimStore(t, ctx)
	reg := nudge.NewRegistry()
	if err := reg.Register("backgroundCheck", failingAdapter{}); err != nil {
		t.Fatal(err)
	}
	n := nudge.NewNudger(cs, reg)

	rec, err := n.Run(ctx, dispatch("claim-fail"), func(context.Context, string, nudge.Result) (string, error) {
		t.Fatal("resolve must not be called after an adapter failure")
		return "", nil
	})
	if err == nil {
		t.Fatal("Run with a failing adapter: want an error")
	}
	if rec == nil || rec.State != nudge.StateFailed {
		t.Fatalf("claim state after adapter failure = %v, want failed", rec)
	}
}

// TestNudger_RecoverReusesClaimIDNoSecondSideEffect is the 10.1 share of the
// FR58 idempotency proof: a recovery re-drive on the SAME claimId produces NO
// second adapter side-effect and NO second resolve op. The adapter de-dups on
// the reused idempotencyKey.
func TestNudger_RecoverReusesClaimIDNoSecondSideEffect(t *testing.T) {
	ctx := context.Background()
	cs := newClaimStore(t, ctx)
	reg := nudge.NewRegistry()
	fake := nudge.NewFakeBackgroundCheck()
	if err := reg.Register("backgroundCheck", fake); err != nil {
		t.Fatal(err)
	}
	n := nudge.NewNudger(cs, reg)

	d := dispatch("claim-recover-1")

	// First run succeeds end-to-end.
	resolveCalls := 0
	resolve := func(ctx context.Context, claimID string, result nudge.Result) (string, error) {
		resolveCalls++
		return "req-" + claimID, nil
	}
	if _, err := n.Run(ctx, d, resolve); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Recovery sees a Core KV resolve already landed (read-before-act): it must
	// NOT re-execute the adapter, just advance the record.
	probe := func(ctx context.Context, claimID string) (string, bool, error) {
		return "req-" + claimID, true, nil
	}
	rec, err := n.Recover(ctx, d, probe, func(context.Context, string, nudge.Result) (string, error) {
		t.Fatal("resolve must not be re-submitted when Core KV shows the resolve already landed")
		return "", nil
	})
	if err != nil {
		t.Fatalf("Recover (already-resolved): %v", err)
	}
	if rec.State != nudge.StateResolved {
		t.Errorf("recovered claim state = %q, want resolved", rec.State)
	}
	if got := fake.SideEffects("claim-recover-1"); got != 1 {
		t.Errorf("adapter side effects after recovery = %d, want 1 (no second side-effect)", got)
	}
	if resolveCalls != 1 {
		t.Errorf("resolve op submitted %d times total, want 1 (recovery re-submitted none)", resolveCalls)
	}
}

// TestNudger_RecoverExecutingReExecutesSameKey covers the executing-state retry
// where Core KV shows NO landed resolve: recovery re-executes on the SAME
// idempotencyKey, the adapter de-dups, and the claim resolves — still one
// side-effect.
func TestNudger_RecoverExecutingReExecutesSameKey(t *testing.T) {
	ctx := context.Background()
	cs := newClaimStore(t, ctx)
	reg := nudge.NewRegistry()
	fake := nudge.NewFakeBackgroundCheck()
	if err := reg.Register("backgroundCheck", fake); err != nil {
		t.Fatal(err)
	}
	n := nudge.NewNudger(cs, reg)
	d := dispatch("claim-stuck-1")

	// Simulate a crash mid-protocol: claim written + advanced to executing, the
	// adapter already ran once (side-effect performed), but the resolve never
	// landed.
	if _, err := cs.Write(ctx, d.ClaimID, d.Adapter, d.Operation, d.Subject, d.Params); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Advance(ctx, d.ClaimID, nudge.StateExecuting); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.Execute(ctx, nudge.Request{
		IdempotencyKey: d.ClaimID, Operation: d.Operation, Subject: d.Subject, Params: d.Params,
	}); err != nil {
		t.Fatal(err)
	}

	probeNoResolve := func(ctx context.Context, claimID string) (string, bool, error) {
		return "", false, nil
	}
	resolved := false
	rec, err := n.Recover(ctx, d, probeNoResolve, func(context.Context, string, nudge.Result) (string, error) {
		resolved = true
		return "req-recovered", nil
	})
	if err != nil {
		t.Fatalf("Recover (executing): %v", err)
	}
	if rec.State != nudge.StateResolved || rec.ResolveRef != "req-recovered" {
		t.Errorf("recovered claim = %+v, want resolved with resolveRef req-recovered", rec)
	}
	if !resolved {
		t.Error("resolve op was not submitted on the re-execute path")
	}
	if got := fake.SideEffects("claim-stuck-1"); got != 1 {
		t.Errorf("adapter side effects after executing-state recovery = %d, want 1 (idempotent re-execute)", got)
	}
}

func TestNudger_RecoverRejectsEmptyClaimID(t *testing.T) {
	ctx := context.Background()
	cs := newClaimStore(t, ctx)
	n := nudge.NewNudger(cs, nudge.NewRegistry())
	d := dispatch("")
	probe := func(context.Context, string) (string, bool, error) { return "", false, nil }
	if _, err := n.Recover(ctx, d, probe, func(context.Context, string, nudge.Result) (string, error) { return "", nil }); err == nil {
		t.Fatal("Recover with an empty claimId: want error (never mint a fresh id)")
	}
}

// TestNudger_RecoverRejectsNilProbe proves the landed-resolve check cannot be
// silently skipped: a nil probe is rejected the same way a blank claimId is. A
// nil probe would let recovery re-execute AND re-submit a resolve on a
// crash-between-execute-and-record — a duplicate-resolve hole (review H2/M2).
func TestNudger_RecoverRejectsNilProbe(t *testing.T) {
	ctx := context.Background()
	cs := newClaimStore(t, ctx)
	reg := nudge.NewRegistry()
	if err := reg.Register("backgroundCheck", nudge.NewFakeBackgroundCheck()); err != nil {
		t.Fatal(err)
	}
	n := nudge.NewNudger(cs, reg)
	d := dispatch("claim-nil-probe")
	if _, err := n.Recover(ctx, d, nil, func(context.Context, string, nudge.Result) (string, error) { return "", nil }); err == nil {
		t.Fatal("Recover with a nil probe: want error (the landed-resolve check must not be skipped)")
	}
}

// TestNudger_RunRejectsExistingClaim proves a fresh Run cannot clobber a live
// claim (review H1): a redelivery/retry routed to Run for a claimId that already
// has a claim record is rejected with ErrClaimExists, the existing claim is left
// untouched (NOT reset to claimed), and the adapter is NOT called. The caller
// must route an existing claim to Recover.
func TestNudger_RunRejectsExistingClaim(t *testing.T) {
	ctx := context.Background()
	cs := newClaimStore(t, ctx)
	reg := nudge.NewRegistry()
	fake := nudge.NewFakeBackgroundCheck()
	adapterCalls := 0
	guarded := nudge.AdapterFunc(func(ctx context.Context, req nudge.Request) (nudge.Result, error) {
		adapterCalls++
		return fake.Execute(ctx, req)
	})
	if err := reg.Register("backgroundCheck", guarded); err != nil {
		t.Fatal(err)
	}
	n := nudge.NewNudger(cs, reg)
	d := dispatch("claim-dup")

	// Pre-existing claim already past claimed (executing) — simulate an in-flight
	// or recovered claim a redelivery might race.
	if _, err := cs.Write(ctx, d.ClaimID, d.Adapter, d.Operation, d.Subject, d.Params); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Advance(ctx, d.ClaimID, nudge.StateExecuting); err != nil {
		t.Fatal(err)
	}

	_, err := n.Run(ctx, d, func(context.Context, string, nudge.Result) (string, error) {
		t.Fatal("resolve must not run when Run hits an existing claim")
		return "", nil
	})
	if err == nil || !errors.Is(err, nudge.ErrClaimExists) {
		t.Fatalf("Run on an existing claim: err = %v, want ErrClaimExists", err)
	}
	if adapterCalls != 0 {
		t.Errorf("adapter called %d times on a re-Run over a live claim, want 0", adapterCalls)
	}
	got, found, err := cs.Get(ctx, d.ClaimID)
	if err != nil || !found {
		t.Fatalf("Get: found=%v err=%v", found, err)
	}
	if got.State != nudge.StateExecuting {
		t.Errorf("existing claim state after re-Run = %q, want executing (not reset to claimed)", got.State)
	}
}

// panickingAdapter blows up inside Execute — the third-party hazard the framework
// must contain (review M3): a panic must become a normal execution error, not a
// propagated panic that crashes the dispatch goroutine and strands the claim in
// executing.
type panickingAdapter struct{}

func (panickingAdapter) Execute(context.Context, nudge.Request) (nudge.Result, error) {
	panic("adapter exploded")
}

// TestNudger_RunContainsAdapterPanic proves a panic in the adapter is recovered
// and converted to a returned error, and the claim lands in failed (the same
// re-drivable disposition a returned error produces) — not propagated.
func TestNudger_RunContainsAdapterPanic(t *testing.T) {
	ctx := context.Background()
	cs := newClaimStore(t, ctx)
	reg := nudge.NewRegistry()
	if err := reg.Register("backgroundCheck", panickingAdapter{}); err != nil {
		t.Fatal(err)
	}
	n := nudge.NewNudger(cs, reg)

	rec, err := n.Run(ctx, dispatch("claim-panic"), func(context.Context, string, nudge.Result) (string, error) {
		t.Fatal("resolve must not run after a contained panic")
		return "", nil
	})
	if err == nil {
		t.Fatal("Run with a panicking adapter: want a contained error, not a propagated panic")
	}
	if rec == nil || rec.State != nudge.StateFailed {
		t.Fatalf("claim state after contained panic = %v, want failed (re-drivable)", rec)
	}
}

// TestNudger_RecoverFailedReAttemptsSameKey proves a failed claim is NOT a
// permanent no-op (review M1): on recovery it re-executes on the SAME
// idempotencyKey, the adapter records exactly one side-effect (dedup of the prior
// partial attempt), and the claim then reaches resolved. Per §10.3 the gap keeps
// converging — recovery short-circuits ONLY on resolved.
func TestNudger_RecoverFailedReAttemptsSameKey(t *testing.T) {
	ctx := context.Background()
	cs := newClaimStore(t, ctx)
	reg := nudge.NewRegistry()
	fake := nudge.NewFakeBackgroundCheck()
	if err := reg.Register("backgroundCheck", fake); err != nil {
		t.Fatal(err)
	}
	n := nudge.NewNudger(cs, reg)
	d := dispatch("claim-failed-1")

	// Simulate a prior attempt that performed its side-effect then failed at the
	// resolve (left the claim failed). The adapter already recorded one side-effect
	// for this idempotencyKey.
	if _, err := cs.Write(ctx, d.ClaimID, d.Adapter, d.Operation, d.Subject, d.Params); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Advance(ctx, d.ClaimID, nudge.StateExecuting); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.Execute(ctx, nudge.Request{
		IdempotencyKey: d.ClaimID, Operation: d.Operation, Subject: d.Subject, Params: d.Params,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Advance(ctx, d.ClaimID, nudge.StateFailed); err != nil {
		t.Fatal(err)
	}

	probeNoResolve := func(context.Context, string) (string, bool, error) { return "", false, nil }
	resolved := false
	rec, err := n.Recover(ctx, d, probeNoResolve, func(context.Context, string, nudge.Result) (string, error) {
		resolved = true
		return "req-failed-reattempt", nil
	})
	if err != nil {
		t.Fatalf("Recover (failed): %v", err)
	}
	if rec.State != nudge.StateResolved || rec.ResolveRef != "req-failed-reattempt" {
		t.Errorf("recovered claim = %+v, want resolved with resolveRef req-failed-reattempt", rec)
	}
	if !resolved {
		t.Error("resolve op was not submitted on the failed-claim re-attempt")
	}
	if got := fake.SideEffects("claim-failed-1"); got != 1 {
		t.Errorf("adapter side effects after failed-claim recovery = %d, want 1 (dedup)", got)
	}
}
