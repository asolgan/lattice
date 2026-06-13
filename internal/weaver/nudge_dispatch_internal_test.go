package weaver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/asolgan/lattice/internal/substrate"
	"github.com/asolgan/lattice/internal/weaver/nudge"
)

// nudgeHarness drives the live lane-1 nudge dispatch path (fireEpisode →
// fireNudge/recoverNudge → the Two-Phase Nudge protocol) against an embedded
// NATS server, with a registered FakeStripe so the FR58 idempotency + crash
// proofs can be exercised deterministically. It seeds the registry directly (no
// CDC consumer) and constructs the §10.2 row + §10.3 mark stores the engine uses.
type nudgeHarness struct {
	engine *Engine
	conn   *substrate.Conn
	nc     *nats.Conn
	stripe *nudge.FakeStripe
	ops    *nats.Subscription
}

func newNudgeHarness(t *testing.T, ctx context.Context) *nudgeHarness {
	t.Helper()
	srvOpts := &natsserver.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir()}
	srv := natstest.RunServer(srvOpts)
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
	js := conn.JetStream()
	for _, b := range []string{"core-kv", "weaver-state", "weaver-claims"} {
		if _, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: b, LimitMarkerTTL: time.Second}); err != nil {
			t.Fatalf("create bucket %s: %v", b, err)
		}
	}
	if _, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "weaver-targets"}); err != nil {
		t.Fatalf("create weaver-targets: %v", err)
	}
	provisionOps(t, ctx, js)

	cfg := Config{
		ActorKey: "vtx.identity.WeaverServiceActor1abc",
		Instance: "nudge-" + testNanoID(t),
		Logger:   discardLogger(),
	}
	engine := NewEngine(conn, cfg)
	stripe := nudge.NewFakeStripe()
	if err := engine.RegisterAdapter("stripe", stripe); err != nil {
		t.Fatalf("register adapter: %v", err)
	}
	ops, err := nc.SubscribeSync("ops.system")
	if err != nil {
		t.Fatalf("subscribe ops: %v", err)
	}
	t.Cleanup(func() { _ = ops.Unsubscribe() })

	return &nudgeHarness{engine: engine, conn: conn, nc: nc, stripe: stripe, ops: ops}
}

func provisionOps(t *testing.T, ctx context.Context, js jetstream.JetStream) {
	t.Helper()
	// Bound to ops.system only (not ops.>), so a publish to any other ops.<lane>
	// subject fails fast (no stream accepts it) — the resolve-submit crash hook.
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: "core-operations", Subjects: []string{"ops.system"},
	}); err != nil {
		t.Fatalf("create ops stream: %v", err)
	}
}

// breakResolve repoints the engine's actuator at an unbound ops lane so the
// resolve-op publish fails immediately — the crash injection: e.act.submit
// returns an error, leaving the claim in state=executing (a crash between the
// adapter execute and the resolve op landing). restoreResolve undoes it.
func (h *nudgeHarness) breakResolve() {
	h.engine.act = newActuator(h.conn, "deadletter", h.engine.cfg.ActorKey, discardLogger())
}

func (h *nudgeHarness) restoreResolve() {
	h.engine.act = newActuator(h.conn, "system", h.engine.cfg.ActorKey, discardLogger())
}

func (h *nudgeHarness) seedNudgeTarget(targetID, adapter, operation string) {
	h.engine.source.mu.Lock()
	h.engine.source.targets[targetID] = &Target{
		TargetID: targetID,
		Gaps: map[string]GapAction{
			"missing_check": {Action: actionNudge, Adapter: adapter, Operation: operation, Subject: "row.entityKey"},
		},
	}
	h.engine.source.mu.Unlock()
}

func (h *nudgeHarness) row(targetID, entityID string) map[string]any {
	return map[string]any{
		"entityKey": "vtx.leaseApp." + entityID, "violating": true, "missing_check": true,
	}
}

// dispatch runs one lane-1 nudge dispatch through the live path for the given
// gap, mirroring dispatchGap → fireEpisode (a fresh delivery is redelivered=false).
func (h *nudgeHarness) dispatch(ctx context.Context, targetID, entityID string, rowRevision uint64, redelivered bool) substrate.Decision {
	r := h.row(targetID, entityID)
	pl, perr := buildPlan(h.engine.source, targetID, entityID, "missing_check",
		h.engine.source.targets[targetID].Gaps["missing_check"], r, rowRevision)
	if perr != nil {
		h.engine.logger.Error("buildPlan failed in harness", "err", perr.msg)
		return substrate.Ack
	}
	return h.engine.fireEpisode(ctx, targetID, entityID, "vtx.leaseApp."+entityID, "missing_check",
		actionNudge, pl, rowRevision, redelivered)
}

// putRow writes a §10.2 violating row to the weaver-targets bucket (the sweep
// reads it during reclaim). The mark store and the dispatch helper otherwise work
// off the in-memory plan; the sweep needs the durable row.
func (h *nudgeHarness) putRow(t *testing.T, ctx context.Context, targetID, entityID string) {
	t.Helper()
	body, err := json.Marshal(h.row(targetID, entityID))
	if err != nil {
		t.Fatalf("marshal row: %v", err)
	}
	if _, err := h.conn.KVPut(ctx, "weaver-targets", targetID+"."+entityID, body); err != nil {
		t.Fatalf("put row: %v", err)
	}
}

// expireMark rewinds a mark's leaseExpiresAt into the past so the sweep treats it
// as a presumed-dead episode to reclaim, preserving the claimId.
func (h *nudgeHarness) expireMark(t *testing.T, ctx context.Context, targetID, entityID string) {
	t.Helper()
	key := markKey(targetID, entityID, "missing_check")
	entry, err := h.conn.KVGet(ctx, "weaver-state", key)
	if err != nil {
		t.Fatalf("get mark: %v", err)
	}
	var rec mark
	if err := json.Unmarshal(entry.Value, &rec); err != nil {
		t.Fatalf("unmarshal mark: %v", err)
	}
	rec.LeaseExpiresAt = substrate.FormatTimestamp(time.Now().Add(-time.Minute))
	body, _ := json.Marshal(rec)
	if _, err := h.conn.KVPut(ctx, "weaver-state", key, body); err != nil {
		t.Fatalf("put expired mark: %v", err)
	}
}

func (h *nudgeHarness) claimID(t *testing.T, ctx context.Context, targetID, entityID string) string {
	t.Helper()
	rec, _, found, err := h.engine.marks.get(ctx, targetID, entityID, "missing_check")
	if err != nil || !found {
		t.Fatalf("get mark: found=%v err=%v", found, err)
	}
	return rec.ClaimID
}

func (h *nudgeHarness) claim(t *testing.T, ctx context.Context, claimID string) *nudge.Claim {
	t.Helper()
	rec, found, err := h.engine.claims.Get(ctx, claimID)
	if err != nil || !found {
		t.Fatalf("get claim %s: found=%v err=%v", claimID, found, err)
	}
	return rec
}

// drainResolveOps counts the resolve ops published to ops.system and asserts they
// all carry the SAME deterministic requestId (the Contract #4 collapse). Returns
// the distinct requestIds seen.
func (h *nudgeHarness) resolveOpRequestIDs(t *testing.T) []string {
	t.Helper()
	seen := map[string]struct{}{}
	for {
		msg, err := h.ops.NextMsg(300 * time.Millisecond)
		if err != nil {
			break
		}
		var env struct {
			RequestID     string `json:"requestId"`
			OperationType string `json:"operationType"`
		}
		if err := json.Unmarshal(msg.Data, &env); err != nil {
			t.Fatalf("unmarshal op: %v", err)
		}
		seen[env.RequestID] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out
}

// TestNudgeDispatch_FailedThenRetried_AtMostOneSideEffect is the FR58 idempotency
// proof (AC #6): a nudge dispatch whose external call FAILS the first attempt and
// is RETRIED on redelivery performs at most ONE side-effect (the claim is the
// idempotency boundary), and exactly one resolve op lands (the deterministic
// requestId collapses any duplicate on the Contract #4 tracker). The claim record
// walks claimed → executing → failed → executing → resolved with resolveRef set.
func TestNudgeDispatch_FailedThenRetried_AtMostOneSideEffect(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h := newNudgeHarness(t, ctx)

	const targetID = "fixtureNudgeFR58"
	h.seedNudgeTarget(targetID, "stripe", "ResolveCharge")
	entityID := testNanoID(t)

	// First delivery: the adapter is armed to fail → the claim lands failed, the
	// dispatch Acks (the reconciler/redelivery re-attempts), and NO charge occurred.
	h.stripe.FailNext()
	if dec := h.dispatch(ctx, targetID, entityID, 7, false); dec != substrate.Ack {
		t.Fatalf("failed-adapter dispatch decision = %v, want Ack (re-attempt is the reconciler's job)", dec)
	}
	claimID := h.claimID(t, ctx, targetID, entityID)
	if got := h.stripe.SideEffects(claimID); got != 0 {
		t.Fatalf("a failed external call must record NO side-effect, got %d", got)
	}
	if st := h.claim(t, ctx, claimID).State; st != nudge.StateFailed {
		t.Fatalf("claim state after the failed attempt = %q, want failed", st)
	}
	if !hasIssueCode(h.engine.issues.snapshot(), "NudgeAdapterFailed") {
		t.Fatal("a failed adapter must raise a Health issue (NudgeAdapterFailed)")
	}

	// Redelivery over the live mark: routes to recovery (read-before-act). The
	// adapter is no longer armed to fail, so the retry on the SAME idempotencyKey
	// succeeds — exactly one side-effect.
	if dec := h.dispatch(ctx, targetID, entityID, 7, true); dec != substrate.Ack {
		t.Fatalf("recovery dispatch decision = %v, want Ack", dec)
	}
	if got := h.stripe.SideEffects(claimID); got != 1 {
		t.Fatalf("after failure + retry: side effects = %d, want exactly 1 (no duplicate charge)", got)
	}
	resolved := h.claim(t, ctx, claimID)
	if resolved.State != nudge.StateResolved {
		t.Fatalf("claim state after recovery = %q, want resolved", resolved.State)
	}
	if resolved.ResolveRef != deriveResolveRequestID(claimID) {
		t.Fatalf("resolveRef = %q, want the deterministic resolve requestId %q", resolved.ResolveRef, deriveResolveRequestID(claimID))
	}

	// Exactly one resolve op, carrying the deterministic requestId.
	ids := h.resolveOpRequestIDs(t)
	if len(ids) != 1 {
		t.Fatalf("distinct resolve op requestIds = %v, want exactly 1", ids)
	}
	if ids[0] != deriveResolveRequestID(claimID) {
		t.Fatalf("resolve op requestId = %q, want the deterministic %q", ids[0], deriveResolveRequestID(claimID))
	}
}

// TestNudgeDispatch_CrashBetweenClaimAndResolve is the NFR-S11 crash proof (AC
// #7): a crash AFTER the adapter executes but BEFORE the resolve op lands (the
// resolve submit fails) leaves the claim visible in weaver-claims (executing); a
// plain lane-1 redelivery does NOT re-initiate a duplicate side-effect (the
// create-semantic claim routes to recovery, the adapter de-dups on the same
// idempotencyKey); and the recovery converges the claim to resolved with the
// side-effect count still at most 1.
func TestNudgeDispatch_CrashBetweenClaimAndResolve(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h := newNudgeHarness(t, ctx)

	const targetID = "fixtureNudgeCrash"
	h.seedNudgeTarget(targetID, "stripe", "ResolveCharge")
	entityID := testNanoID(t)

	// Crash injection: repoint the actuator at an unbound lane so the resolve-op
	// publish fails. The adapter still charges (one side-effect), then the resolve
	// submit errors and the claim is left in state=executing — the
	// crash-between-execute-and-resolve window. The dispatch Naks so a redelivery
	// re-drives it.
	h.breakResolve()
	if dec := h.dispatch(ctx, targetID, entityID, 11, false); dec != substrate.Nak {
		t.Fatalf("resolve-submit-failure dispatch decision = %v, want Nak (redelivery re-drives via recovery)", dec)
	}
	claimID := h.claimID(t, ctx, targetID, entityID)

	// (a) the claim is visible in weaver-claims, pre-resolved (NFR-S11).
	stuck := h.claim(t, ctx, claimID)
	if stuck.State != nudge.StateExecuting {
		t.Fatalf("claim state at the crash point = %q, want executing (visible claim before resolve)", stuck.State)
	}
	if got := h.stripe.SideEffects(claimID); got != 1 {
		t.Fatalf("the adapter charged once before the crash, got %d side-effects", got)
	}

	// Restore the resolve path (the process is back up).
	h.restoreResolve()

	// (b)/(c) a plain lane-1 redelivery routes to recovery (the live mark + the
	// create-semantic claim), re-executes on the SAME idempotencyKey (adapter
	// de-dups → no second charge), and resolves.
	if dec := h.dispatch(ctx, targetID, entityID, 11, true); dec != substrate.Ack {
		t.Fatalf("recovery dispatch decision = %v, want Ack", dec)
	}
	if got := h.stripe.SideEffects(claimID); got != 1 {
		t.Fatalf("after crash + recovery: side effects = %d, want still at most 1 (no duplicate charge)", got)
	}
	resolved := h.claim(t, ctx, claimID)
	if resolved.State != nudge.StateResolved {
		t.Fatalf("claim state after recovery = %q, want resolved", resolved.State)
	}
	if resolved.ResolveRef != deriveResolveRequestID(claimID) {
		t.Fatalf("resolveRef = %q, want %q", resolved.ResolveRef, deriveResolveRequestID(claimID))
	}
}

// TestNudgeRecovery_LandedResolveSkipsReExecute proves the read-before-act probe
// (AC #4): if the resolve op already committed in Core KV (the deterministic
// tracker vtx.op.<resolveRequestId> is present), recovery advances the claim to
// resolved WITHOUT re-executing the adapter — zero side-effects from recovery.
func TestNudgeRecovery_LandedResolveSkipsReExecute(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h := newNudgeHarness(t, ctx)

	const targetID = "fixtureNudgeLanded"
	h.seedNudgeTarget(targetID, "stripe", "ResolveCharge")
	entityID := testNanoID(t)

	// Crash before resolve: adapter charges once, claim left executing.
	h.breakResolve()
	if dec := h.dispatch(ctx, targetID, entityID, 5, false); dec != substrate.Nak {
		t.Fatalf("dispatch decision = %v, want Nak", dec)
	}
	claimID := h.claimID(t, ctx, targetID, entityID)
	if got := h.stripe.SideEffects(claimID); got != 1 {
		t.Fatalf("adapter charged once, got %d", got)
	}
	h.restoreResolve()

	// Simulate the Processor having committed the resolve op (the authoritative
	// Core KV business outcome): write the Contract #4 tracker for the resolve's
	// deterministic requestId.
	resolveReq := deriveResolveRequestID(claimID)
	if _, err := h.conn.KVPut(ctx, "core-kv", "vtx.op."+resolveReq,
		[]byte(`{"class":"op","data":{"status":"committed"}}`)); err != nil {
		t.Fatalf("seed resolve tracker: %v", err)
	}

	// Recovery: the probe finds the landed resolve and advances to resolved
	// without re-executing — the side-effect count stays at 1 (no recovery charge).
	if dec := h.dispatch(ctx, targetID, entityID, 5, true); dec != substrate.Ack {
		t.Fatalf("recovery dispatch decision = %v, want Ack", dec)
	}
	if got := h.stripe.SideEffects(claimID); got != 1 {
		t.Fatalf("a landed-resolve recovery must NOT re-charge: side effects = %d, want 1", got)
	}
	resolved := h.claim(t, ctx, claimID)
	if resolved.State != nudge.StateResolved {
		t.Fatalf("claim state = %q, want resolved (probe found the landed resolve)", resolved.State)
	}
	if resolved.ResolveRef != resolveReq {
		t.Fatalf("resolveRef = %q, want the landed resolve requestId %q", resolved.ResolveRef, resolveReq)
	}
}

// TestNudgeRecovery_TombstonedResolveNotLanded proves the Contract #4
// isDeleted:false guard in resolveProbe (Edge F7): Core KV returns a logically-
// deleted (isDeleted:true) tracker normally (err == nil), and §4.3 reserves that
// as an operator-driven retry signal. A tombstoned resolve tracker must therefore
// NOT count as a landed resolve — recovery re-executes on the same idempotencyKey
// (the adapter dedups → side-effect stays at 1) and re-resolves, rather than
// silently advancing the claim to resolved off the tombstone.
func TestNudgeRecovery_TombstonedResolveNotLanded(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h := newNudgeHarness(t, ctx)

	const targetID = "fixtureNudgeTombstone"
	h.seedNudgeTarget(targetID, "stripe", "ResolveCharge")
	entityID := testNanoID(t)

	// Crash before resolve: adapter charges once, claim left executing.
	h.breakResolve()
	if dec := h.dispatch(ctx, targetID, entityID, 8, false); dec != substrate.Nak {
		t.Fatalf("dispatch decision = %v, want Nak", dec)
	}
	claimID := h.claimID(t, ctx, targetID, entityID)
	if got := h.stripe.SideEffects(claimID); got != 1 {
		t.Fatalf("adapter charged once, got %d", got)
	}
	h.restoreResolve()

	// Seed the resolve tracker as a TOMBSTONE (isDeleted:true) — the §4.3
	// operator-driven retry signal, which must read as not-landed.
	resolveReq := deriveResolveRequestID(claimID)
	if _, err := h.conn.KVPut(ctx, "core-kv", "vtx.op."+resolveReq,
		[]byte(`{"class":"op","isDeleted":true,"data":{"status":"committed"}}`)); err != nil {
		t.Fatalf("seed tombstoned resolve tracker: %v", err)
	}

	// Recovery: the probe must NOT treat the tombstone as landed — it re-executes
	// (adapter dedups, no second charge) and re-resolves.
	if dec := h.dispatch(ctx, targetID, entityID, 8, true); dec != substrate.Ack {
		t.Fatalf("recovery dispatch decision = %v, want Ack", dec)
	}
	if got := h.stripe.SideEffects(claimID); got != 1 {
		t.Fatalf("recovery over a tombstoned tracker must re-drive without a duplicate side-effect: got %d, want 1", got)
	}
	resolved := h.claim(t, ctx, claimID)
	if resolved.State != nudge.StateResolved {
		t.Fatalf("claim state after recovery = %q, want resolved (re-resolved on the live requestId)", resolved.State)
	}
	// The re-resolve published its own resolve op (the tombstone was not trusted),
	// carrying the deterministic requestId.
	if resolved.ResolveRef != resolveReq {
		t.Fatalf("resolveRef = %q, want the deterministic resolve requestId %q", resolved.ResolveRef, resolveReq)
	}
}

// TestNudgeDispatch_FreshHappyPath proves the create-win path (AC #2): a fresh
// nudge dispatch CAS-creates the mark with a minted claimId, writes the claim
// (claimed) before executing, charges once, submits the resolve op, and resolves.
func TestNudgeDispatch_FreshHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h := newNudgeHarness(t, ctx)

	const targetID = "fixtureNudgeHappy"
	h.seedNudgeTarget(targetID, "stripe", "ResolveCharge")
	entityID := testNanoID(t)

	if dec := h.dispatch(ctx, targetID, entityID, 3, false); dec != substrate.Ack {
		t.Fatalf("fresh nudge dispatch decision = %v, want Ack", dec)
	}
	claimID := h.claimID(t, ctx, targetID, entityID)
	if !substrate.IsValidNanoID(claimID) {
		t.Fatalf("minted claimId %q is not a valid NanoID", claimID)
	}
	if got := h.stripe.SideEffects(claimID); got != 1 {
		t.Fatalf("happy path: side effects = %d, want 1", got)
	}
	resolved := h.claim(t, ctx, claimID)
	if resolved.State != nudge.StateResolved {
		t.Fatalf("claim state = %q, want resolved", resolved.State)
	}
	if resolved.IdempotencyKey != claimID {
		t.Fatalf("claim idempotencyKey = %q, want = claimId %q", resolved.IdempotencyKey, claimID)
	}
	if resolved.Subject != "vtx.leaseApp."+entityID {
		t.Fatalf("claim subject = %q, want the templated row.entityKey", resolved.Subject)
	}
	ids := h.resolveOpRequestIDs(t)
	if len(ids) != 1 || ids[0] != deriveResolveRequestID(claimID) {
		t.Fatalf("resolve op requestIds = %v, want exactly the deterministic %q", ids, deriveResolveRequestID(claimID))
	}
}

// TestBuildPlan_Nudge covers the §10.8 nudge plan resolution and its config/data
// routing (AC #1): a valid nudge resolves a nudgePlan (adapter/operation/subject/
// params templated from the row) with no planError — proving the loud-stub
// "nudge is not yet implemented" PlaybookConfigError is gone; a blank or
// row-templated adapter/operation is a config error; a templated-null subject is
// a data error (the same routing the other actions use).
func TestBuildPlan_Nudge(t *testing.T) {
	src := newTestSource(t)
	row := map[string]any{"entityKey": "vtx.leaseApp.abc", "applicantEmail": "a@example.com"}

	valid := GapAction{Action: actionNudge, Adapter: "stripe", Operation: "ResolveCharge",
		Subject: "row.entityKey", Params: map[string]string{"email": "row.applicantEmail"}}
	pl, perr := buildPlan(src, "t1", "ent", "missing_check", valid, row, 4)
	if perr != nil {
		t.Fatalf("valid nudge must not error: %v", perr.msg)
	}
	if pl.nudge == nil {
		t.Fatal("valid nudge must produce a nudgePlan carrier")
	}
	if pl.nudge.adapter != "stripe" || pl.nudge.operation != "ResolveCharge" {
		t.Fatalf("nudgePlan adapter/operation = %q/%q, want stripe/ResolveCharge", pl.nudge.adapter, pl.nudge.operation)
	}
	if pl.nudge.subject != "vtx.leaseApp.abc" {
		t.Fatalf("nudgePlan subject = %q, want the templated row.entityKey", pl.nudge.subject)
	}
	if pl.nudge.params["email"] != "a@example.com" {
		t.Fatalf("nudgePlan params[email] = %q, want the templated row value", pl.nudge.params["email"])
	}

	cases := []struct {
		name string
		ga   GapAction
		kind errKind
	}{
		{"blank adapter", GapAction{Action: actionNudge, Operation: "Op", Subject: "row.entityKey"}, errConfig},
		{"templated adapter", GapAction{Action: actionNudge, Adapter: "row.x", Operation: "Op", Subject: "row.entityKey"}, errConfig},
		{"blank operation", GapAction{Action: actionNudge, Adapter: "stripe", Subject: "row.entityKey"}, errConfig},
		{"templated operation", GapAction{Action: actionNudge, Adapter: "stripe", Operation: "row.x", Subject: "row.entityKey"}, errConfig},
		{"null subject", GapAction{Action: actionNudge, Adapter: "stripe", Operation: "Op", Subject: "row.missingCol"}, errData},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, perr := buildPlan(src, "t1", "ent", "missing_check", tc.ga, row, 4)
			if perr == nil {
				t.Fatal("want a planError")
			}
			if perr.kind != tc.kind {
				t.Fatalf("planError kind = %v, want %v (%q)", perr.kind, tc.kind, perr.msg)
			}
		})
	}
}

// TestNudgeDispatch_MissingAdapterAcksAndAlerts proves the missing-adapter
// posture (Blind HIGH / Edge F8): a nudge gap naming an adapter the registry does
// not hold is a config error, NOT a redelivery failure. The dispatch through
// fireEpisode Acks (redelivery can never fix a name the registry does not know, so
// a Nak would hot-loop lane-1) and raises a NudgeAdapterMissing Health issue.
func TestNudgeDispatch_MissingAdapterAcksAndAlerts(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h := newNudgeHarness(t, ctx)

	const targetID = "fixtureNudgeNoAdapter"
	// "ghost" is never registered (only "stripe" is, in newNudgeHarness).
	h.seedNudgeTarget(targetID, "ghost", "ResolveCharge")
	entityID := testNanoID(t)

	if dec := h.dispatch(ctx, targetID, entityID, 4, false); dec != substrate.Ack {
		t.Fatalf("missing-adapter dispatch decision = %v, want Ack (a config error must not hot-loop lane-1)", dec)
	}
	if !hasIssueCode(h.engine.issues.snapshot(), "NudgeAdapterMissing") {
		t.Fatal("a missing adapter must raise a NudgeAdapterMissing Health issue")
	}
	// No resolve op was published — the dispatch never reached an adapter.
	if ids := h.resolveOpRequestIDs(t); len(ids) != 0 {
		t.Fatalf("a missing-adapter dispatch must publish no op, got %v", ids)
	}
}

// TestNudgeRecovery_WedgedClaimRaisesHealth proves the executing-wedge alert
// (Blind MEDIUM / Edge F4): a recovery driven over a claim whose claimedAt is
// older than claimWedgeBound (the Contract #4 idempotency horizon) raises a
// persistent NudgeClaimWedged Health issue on its OWN issue key, so nudgeDecision's
// clear/raise on issueKeyNudge cannot clobber it. The check fires on the lane-1
// live-redelivery recovery path (recoverNudge), proving it is no longer confined
// to the sweep.
func TestNudgeRecovery_WedgedClaimRaisesHealth(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h := newNudgeHarness(t, ctx)

	const targetID = "fixtureNudgeWedged"
	h.seedNudgeTarget(targetID, "stripe", "ResolveCharge")
	entityID := testNanoID(t)

	// Fresh dispatch lands the mark + claim and resolves (happy path).
	if dec := h.dispatch(ctx, targetID, entityID, 6, false); dec != substrate.Ack {
		t.Fatalf("fresh dispatch decision = %v, want Ack", dec)
	}
	claimID := h.claimID(t, ctx, targetID, entityID)

	// Age the claim past the dedup horizon and rewind it out of the terminal
	// resolved state so the wedge check sees an unresolved, aged claim on recovery.
	rec := h.claim(t, ctx, claimID)
	rec.State = nudge.StateExecuting
	rec.ClaimedAt = substrate.FormatTimestamp(time.Now().Add(-claimWedgeBound - time.Hour))
	body, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal wedged claim: %v", err)
	}
	if _, err := h.conn.KVPut(ctx, "weaver-claims", claimID, body); err != nil {
		t.Fatalf("put wedged claim: %v", err)
	}

	// Redelivery over the live mark routes to recoverNudge — the wedge check runs
	// there. The recovery itself re-executes/re-resolves idempotently; the
	// assertion is purely that the wedge issue is now standing.
	if dec := h.dispatch(ctx, targetID, entityID, 6, true); dec != substrate.Ack {
		t.Fatalf("recovery dispatch decision = %v, want Ack", dec)
	}
	if !hasIssueCode(h.engine.issues.snapshot(), "NudgeClaimWedged") {
		t.Fatal("a recovery over a claim aged past claimWedgeBound must raise a NudgeClaimWedged Health issue")
	}
}

// TestNudgeSweepReclaim_RecoversReusingClaimID proves the reconciler-sweep
// recovery path (AC #4/#7): a nudge episode that crashed before resolve (claim
// executing, mark lease expired) is reclaimed by the sweep through
// replaceCarryingClaim (the SAME claimId is carried forward) + Nudger.Recover,
// converging the claim to resolved with the side-effect count still at most 1 and
// the mark surviving the reclaim (never deleted as corrupt).
func TestNudgeSweepReclaim_RecoversReusingClaimID(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h := newNudgeHarness(t, ctx)

	const targetID = "fixtureNudgeSweep"
	h.seedNudgeTarget(targetID, "stripe", "ResolveCharge")
	entityID := testNanoID(t)
	h.putRow(t, ctx, targetID, entityID)

	// Crash before resolve: adapter charged once, claim left executing.
	h.breakResolve()
	if dec := h.dispatch(ctx, targetID, entityID, 9, false); dec != substrate.Nak {
		t.Fatalf("dispatch decision = %v, want Nak", dec)
	}
	claimID := h.claimID(t, ctx, targetID, entityID)
	if got := h.stripe.SideEffects(claimID); got != 1 {
		t.Fatalf("adapter charged once before the crash, got %d", got)
	}
	h.restoreResolve()

	// The lease expires (the episode is presumed dead); the sweep reclaims it.
	h.expireMark(t, ctx, targetID, entityID)
	h.engine.sweep.pass(ctx)

	// The mark survives (re-armed carrying the claimId), the claim is resolved,
	// and the adapter still charged exactly once.
	markKeyStr := markKey(targetID, entityID, "missing_check")
	if _, err := h.conn.KVGet(ctx, "weaver-state", markKeyStr); err != nil {
		t.Fatalf("reclaimed nudge mark must survive the sweep: %v", err)
	}
	reclaimed := h.claimID(t, ctx, targetID, entityID)
	if reclaimed != claimID {
		t.Fatalf("reclaimed mark claimId = %q, want the original %q (carried forward, never re-minted)", reclaimed, claimID)
	}
	if got := h.stripe.SideEffects(claimID); got != 1 {
		t.Fatalf("after sweep recovery: side effects = %d, want still at most 1", got)
	}
	resolved := h.claim(t, ctx, claimID)
	if resolved.State != nudge.StateResolved {
		t.Fatalf("claim state after sweep recovery = %q, want resolved", resolved.State)
	}
}
