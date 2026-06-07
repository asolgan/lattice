package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/asolgan/lattice/internal/substrate"
)

// Operation types the engine submits for the lifecycle event-only ops
// (Contract #10 §10.9). The trigger op (StartLoomPattern) is submitted by the
// caller, never by the engine.
const (
	opCompletePattern = "CompletePattern"
	opFailPattern     = "FailPattern"
)

// triggerDurable is the fixed always-on trigger consumer's durable name
// (Contract #10 §10.9). It is independent of completionDomains.
const triggerDurable = "loom-trigger"

// triggerSubject is the single subject the trigger consumer filters on.
const triggerSubject = "events.loom.patternStarted"

// Config parameterizes the engine. Bucket/stream names default to the
// platform-standard values; callers (cmd/loom, tests) override only what they
// need.
type Config struct {
	// CoreKVBucket backs the pattern source (vtx.meta.> CDC). Default "core-kv".
	CoreKVBucket string
	// LoomStateBucket holds the per-instance cursors + token index. Default "loom-state".
	LoomStateBucket string
	// EventsStream is the core-events stream the trigger + per-domain completion
	// consumers attach to. Default "core-events".
	EventsStream string
	// ActorKey is the identity:loom service-actor vertex key the Actuator
	// submits under (vtx.identity.<id>, provisioned by Story 7.3).
	ActorKey string
	// Lane is the ops lane systemOps + lifecycle ops are submitted on. Default "system".
	Lane string
	// StepTimeout is the per-step deadline: a step whose committed event is not
	// seen within this window trips the step-deadline-exceeded handler (the
	// off-stream failed/rejected backstop, §10.6). Must be >= 1s (NATS per-key
	// TTL floor). Default 60s.
	StepTimeout time.Duration
	// Instance distinguishes this engine process; it suffixes the per-boot
	// pattern-source durable so each boot replays the installed pattern set.
	// Auto-generated when empty.
	Instance string
	// Logger is the diagnostics sink. Defaults to slog.Default().
	Logger *slog.Logger
}

func (c *Config) withDefaults() {
	if c.CoreKVBucket == "" {
		c.CoreKVBucket = "core-kv"
	}
	if c.LoomStateBucket == "" {
		c.LoomStateBucket = "loom-state"
	}
	if c.EventsStream == "" {
		c.EventsStream = "core-events"
	}
	if c.Lane == "" {
		c.Lane = "system"
	}
	if c.StepTimeout <= 0 {
		c.StepTimeout = 60 * time.Second
	}
	if c.StepTimeout < time.Second {
		// NATS per-key TTL floor: loom-state is provisioned LimitMarkerTTL >= 1s,
		// so a sub-second deadline would not arm a marker and the off-stream
		// failed terminal would never fire. Clamp up rather than silently degrade.
		c.StepTimeout = time.Second
	}
	if c.Instance == "" {
		if id, err := substrate.NewNanoID(); err == nil {
			c.Instance = id
		} else {
			c.Instance = "loom"
		}
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// Engine is the Loom orchestration engine: pattern loader (Sensorium's
// definition source), the fixed trigger consumer, per-domain completion
// consumers (Sensorium), Transition Engine, and Actuator. It holds NO in-memory
// correlation index — completions correlate by a durable token.<token> GET on
// loom-state (Contract #10 §10.6), so any replica resolves any token.
type Engine struct {
	cfg    Config
	conn   *substrate.Conn
	logger *slog.Logger
	source *patternSource
	state  *stateStore
	relay  *relay

	mu sync.Mutex
	// domainConsumers tracks which domains already have a running per-domain
	// completion consumer (durable loom-<domain>), so reconcile is additive and
	// idempotent.
	domainConsumers map[string]context.CancelFunc

	ctx context.Context
}

// NewEngine constructs an Engine over conn.
func NewEngine(conn *substrate.Conn, cfg Config) *Engine {
	cfg.withDefaults()
	e := &Engine{
		cfg:             cfg,
		conn:            conn,
		logger:          cfg.Logger,
		state:           newStateStore(conn, cfg.LoomStateBucket),
		relay:           newRelay(conn, cfg.LoomStateBucket, cfg.Logger),
		domainConsumers: make(map[string]context.CancelFunc),
	}
	e.source = newPatternSource(conn, cfg.CoreKVBucket, cfg.Instance, cfg.Logger)
	return e
}

// Start runs the engine until ctx is cancelled. It (1) starts the fixed trigger
// consumer on events.loom.patternStarted; (2) starts the pattern source whose
// load/update callbacks reconcile the per-domain completion consumers; (3)
// blocks on ctx. There is no startup index rebuild and no watch-suspend gate
// (Crash-safety invariant 3 removed, §10.6): a redelivered completion resolves
// against the durable token.<token> pointer regardless of engine age.
func (e *Engine) Start(ctx context.Context) error {
	e.ctx = ctx

	go e.runTriggerConsumer(ctx)
	go e.relay.run(ctx)
	go e.runDeadlineWatcher(ctx)

	e.source.setLoadCallback(func(p *Pattern) { e.reconcileConsumers() })
	e.source.setUpdateCallback(func(_, _ *Pattern) { e.reconcileConsumers() })
	if err := e.source.start(ctx); err != nil {
		return fmt.Errorf("loom: start pattern source: %w", err)
	}

	e.logger.Info("loom engine started",
		"coreKV", e.cfg.CoreKVBucket, "loomState", e.cfg.LoomStateBucket, "lane", e.cfg.Lane)
	<-ctx.Done()
	return nil
}

// --- Trigger consumer (Contract #10 §10.9) ---------------------------------

// runTriggerConsumer drives the fixed always-on durable consumer on
// events.loom.patternStarted. It is independent of completionDomains. On each
// event it creates the instance cursor and submits step 0; idempotent on the
// instanceId (cursor already present → skip).
func (e *Engine) runTriggerConsumer(ctx context.Context) {
	err := e.conn.RunDurableConsumer(ctx, substrate.DurableConsumerConfig{
		Stream:        e.cfg.EventsStream,
		FilterSubject: triggerSubject,
		Durable:       triggerDurable,
		Logger:        e.logger,
	}, e.handleTrigger)
	if err != nil && ctx.Err() == nil {
		e.logger.Error("loom trigger consumer exited", "err", err)
	}
}

// triggerBody is the patternStarted event Loom reads (Contract #10 §10.9:
// instanceId = the StartLoomPattern requestId). The business fields ride the
// Event's `payload` object (the outbox publishes the full Event envelope); they
// are read from the body, never from the subject.
type triggerBody struct {
	Payload struct {
		InstanceID string `json:"instanceId"`
		PatternRef string `json:"patternRef"`
		SubjectKey string `json:"subjectKey"`
	} `json:"payload"`
}

func (e *Engine) handleTrigger(ctx context.Context, msg substrate.Message) substrate.Decision {
	if len(msg.Body) == 0 {
		return substrate.Ack
	}
	var tb triggerBody
	if err := json.Unmarshal(msg.Body, &tb); err != nil {
		e.logger.Warn("loom: patternStarted body unparseable; dropping", "err", err)
		return substrate.Ack
	}
	t := tb.Payload
	if t.InstanceID == "" || t.PatternRef == "" || t.SubjectKey == "" {
		e.logger.Warn("loom: patternStarted body incomplete; dropping",
			"instanceId", t.InstanceID, "patternRef", t.PatternRef, "subjectKey", t.SubjectKey)
		return substrate.Ack
	}

	// Idempotency on instanceId: a redelivered trigger finds the cursor present
	// and skips (Contract #10 §10.9).
	existing, err := e.state.getInstance(ctx, t.InstanceID)
	if err != nil {
		e.logger.Error("loom: trigger instance read failed; nak", "instanceId", t.InstanceID, "err", err)
		return substrate.Nak
	}
	if existing != nil {
		return substrate.Ack
	}

	patternID := patternIDFromRef(t.PatternRef)
	pattern, ok := e.source.get(patternID)
	if !ok {
		// The pattern is not loaded yet (the CDC source replays asynchronously).
		// Nak so the trigger is redelivered once the pattern registers.
		e.logger.Warn("loom: patternStarted for unloaded pattern; nak for redelivery",
			"patternRef", t.PatternRef, "instanceId", t.InstanceID)
		return substrate.Nak
	}

	inst := &Instance{
		InstanceID: t.InstanceID,
		PatternRef: t.PatternRef,
		SubjectKey: t.SubjectKey,
		Cursor:     0,
		Status:     StatusRunning,
	}
	if err := e.state.createInstance(ctx, inst); err != nil {
		e.logger.Error("loom: create instance failed; nak", "instanceId", t.InstanceID, "err", err)
		return substrate.Nak
	}
	if err := e.submitStep(ctx, inst, pattern, ""); err != nil {
		e.logger.Error("loom: submit step 0 failed; nak", "instanceId", t.InstanceID, "err", err)
		return substrate.Nak
	}
	e.logger.Info("loom instance started", "instanceId", t.InstanceID, "patternId", patternID)
	return substrate.Ack
}

// --- Per-domain completion consumers (D2) ----------------------------------

// reconcileConsumers rebuilds the binding registry from the current pattern set
// and starts a durable per-domain completion consumer (loom-<domain>) for any
// referenced domain not already running. Additive: a newly-referenced domain
// spins up a consumer live, without an engine restart (D2, AC #2).
func (e *Engine) reconcileConsumers() {
	domains := bindingRegistry(e.source.snapshot())
	e.mu.Lock()
	defer e.mu.Unlock()
	for d := range domains {
		if _, running := e.domainConsumers[d]; running {
			continue
		}
		cctx, cancel := context.WithCancel(e.ctx)
		e.domainConsumers[d] = cancel
		domain := d
		go e.runDomainConsumer(cctx, domain)
		e.logger.Info("loom domain consumer reconciled", "domain", domain, "durable", "loom-"+domain)
	}
}

// runDomainConsumer drives one durable completion consumer on core-events,
// filtered to events.<domain>.>, durable loom-<domain>. The handler is
// idempotent (at-least-once): it reads Event.requestId from the body, resolves
// the durable token.<requestId> pointer, and advances the matching instance —
// dropping any completion whose pointer is gone (already advanced).
func (e *Engine) runDomainConsumer(ctx context.Context, domain string) {
	err := e.conn.RunDurableConsumer(ctx, substrate.DurableConsumerConfig{
		Stream:        e.cfg.EventsStream,
		FilterSubject: "events." + domain + ".>",
		Durable:       "loom-" + domain,
		Logger:        e.logger,
	}, e.handleCompletion)
	if err != nil && ctx.Err() == nil {
		e.logger.Error("loom domain consumer exited", "domain", domain, "err", err)
	}
}

// eventBody is the minimal view of a core-events message Loom reads. requestId
// is the Event envelope's top-level field (the outbox publishes the full Event
// per Contract #3 §3.4) — read from the body, never from the subject.
type eventBody struct {
	RequestID string `json:"requestId"`
}

// handleCompletion correlates a committed business event to its instance by a
// direct token.<requestId> GET on loom-state and advances the cursor. There is
// no in-memory index; the pointer's presence is the correlation + idempotency
// guard (Contract #10 §10.6).
func (e *Engine) handleCompletion(ctx context.Context, msg substrate.Message) substrate.Decision {
	if len(msg.Body) == 0 {
		return substrate.Ack
	}
	var ev eventBody
	if err := json.Unmarshal(msg.Body, &ev); err != nil {
		// A core-events body Loom cannot parse is not its concern; ack + skip.
		return substrate.Ack
	}
	if ev.RequestID == "" {
		return substrate.Ack
	}

	instanceID, live, err := e.state.resolveToken(ctx, ev.RequestID)
	if err != nil {
		e.logger.Error("loom: token resolve failed; nak", "requestId", ev.RequestID, "err", err)
		return substrate.Nak
	}
	if !live {
		// Not a token Loom is awaiting (another component's event, or a
		// redelivered completion for an already-advanced instance). Drop.
		return substrate.Ack
	}

	if err := e.advance(ctx, instanceID, ev.RequestID); err != nil {
		e.logger.Error("loom advance failed; nak for redelivery",
			"instanceId", instanceID, "requestId", ev.RequestID, "err", err)
		return substrate.Nak
	}
	return substrate.Ack
}

// --- Transition Engine -----------------------------------------------------

// advance moves an instance to its next step on a committed terminal. It
// re-reads loom-state and verifies the pendingToken still matches (idempotent:
// a redelivery whose token no longer matches clears the stale pointer and
// drops). On exhaustion it submits CompletePattern (event-only).
func (e *Engine) advance(ctx context.Context, instanceID, token string) error {
	inst, err := e.state.getInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	if inst == nil || inst.Status != StatusRunning || inst.PendingToken != token {
		// Already advanced (redelivery) — clear any stale pointer and drop.
		return e.state.deleteToken(ctx, token)
	}

	pattern, ok := e.source.get(patternIDFromRef(inst.PatternRef))
	if !ok {
		return fmt.Errorf("pattern %q not loaded", inst.PatternRef)
	}

	inst.Cursor++
	if inst.Cursor >= len(pattern.Steps) {
		return e.complete(ctx, inst, pattern, token)
	}
	return e.submitStep(ctx, inst, pattern, token)
}

// submitStep write-aheads the next step in a single AtomicBatch (update
// instance.<id>, write token.<newToken>, delete the prior token.<oldToken>,
// write the outbox.<token> op record, arm deadline.<instanceId>). The relay
// publishes the op off that batch — submission is part of the atomic fact, not a
// dual write (Crash-safety invariant 1, §10.6). oldToken == "" for step 0.
func (e *Engine) submitStep(ctx context.Context, inst *Instance, pattern *Pattern, oldToken string) error {
	step := pattern.Steps[inst.Cursor]
	token := deriveRequestID(inst.InstanceID, inst.Cursor)
	inst.PendingToken = token

	target := "vtx.meta." + pattern.PatternID
	payload := map[string]any{"subjectKey": inst.SubjectKey}
	ob, err := buildOutbox(token, step.Operation, payload, target, e.cfg.Lane, e.cfg.ActorKey)
	if err != nil {
		return err
	}
	if err := e.state.transition(ctx, inst, token, oldToken, ob, e.cfg.StepTimeout); err != nil {
		return err
	}
	e.logger.Info("loom step write-ahead",
		"instanceId", inst.InstanceID, "cursor", inst.Cursor,
		"operation", step.Operation, "requestId", token)
	return nil
}

// complete flips the instance to status=complete (the operational terminal) and
// writes the CompletePattern lifecycle op into the outbox — all in one
// AtomicBatch that also deletes the last pending pointer and disarms the
// deadline. The relay publishes CompletePattern, whose commit emits
// events.loom.patternCompleted through the Processor → outbox → core-events
// (never a direct publish, P2). Because the announcement rides the durable
// outbox in the same atomic fact as the terminal, it is delivered exactly like a
// step op — not best-effort — so a nested parent waiting on it is safe.
func (e *Engine) complete(ctx context.Context, inst *Instance, pattern *Pattern, oldToken string) error {
	inst.Status = StatusComplete
	inst.PendingToken = ""
	requestID := deriveRequestID(inst.InstanceID, lifecycleCursor)
	ob, err := buildOutbox(requestID, opCompletePattern,
		map[string]any{"instanceId": inst.InstanceID}, "", e.cfg.Lane, e.cfg.ActorKey)
	if err != nil {
		return err
	}
	if err := e.state.transition(ctx, inst, "", oldToken, ob, 0); err != nil {
		return err
	}
	e.logger.Info("loom pattern complete", "instanceId", inst.InstanceID, "patternId", pattern.PatternID)
	return nil
}

// fail flips the instance to status=failed (the off-stream rejected/timeout
// terminal, §10.6) and writes the FailPattern lifecycle op into the outbox in
// the same AtomicBatch (which also deletes the pending pointer and disarms the
// deadline). Delivery of the announcement is durable, like complete.
func (e *Engine) fail(ctx context.Context, inst *Instance, oldToken, reason string) error {
	inst.Status = StatusFailed
	inst.PendingToken = ""
	inst.RetryCount++
	requestID := deriveRequestID(inst.InstanceID, lifecycleCursor)
	payload := map[string]any{"instanceId": inst.InstanceID}
	if reason != "" {
		payload["reason"] = reason
	}
	ob, err := buildOutbox(requestID, opFailPattern, payload, "", e.cfg.Lane, e.cfg.ActorKey)
	if err != nil {
		return err
	}
	if err := e.state.transition(ctx, inst, "", oldToken, ob, 0); err != nil {
		return err
	}
	e.logger.Warn("loom instance failed",
		"instanceId", inst.InstanceID, "cursor", inst.Cursor, "reason", reason)
	return nil
}

// lifecycleCursor is the deterministic cursor sentinel used to derive the
// requestId of the terminal lifecycle op (CompletePattern/FailPattern). It is
// distinct from any step cursor (steps are 0..len-1), so the lifecycle op's
// requestId never collides with a step's; redelivery of the operational
// terminal collapses on the Contract #4 tracker.
const lifecycleCursor = -1

// --- Step-deadline-exceeded handler (Contract #10 §10.6) -------------------

// deadlineDurable is the deadline watcher's durable consumer name.
const deadlineDurable = "loom-deadline"

// runDeadlineWatcher drives a durable consumer on the loom-state backing stream
// filtered to deadline.>, so a deadline.<instanceId> TTL expiry (a
// KeyValuePurge/MaxAge marker — an empty-body message) trips the
// step-deadline-exceeded handler. The handler self-guards on status, so the
// explicit deletes that disarm the deadline on a normal advance/terminal (also
// empty-body) resolve to a harmless no-op.
func (e *Engine) runDeadlineWatcher(ctx context.Context) {
	subjPrefix := "$KV." + e.cfg.LoomStateBucket + "."
	err := e.conn.RunDurableConsumer(ctx, substrate.DurableConsumerConfig{
		Stream:        "KV_" + e.cfg.LoomStateBucket,
		FilterSubject: subjPrefix + deadlinePrefix + ">",
		Durable:       deadlineDurable,
		Logger:        e.logger,
	}, func(ctx context.Context, msg substrate.Message) substrate.Decision {
		return e.handleDeadline(ctx, subjPrefix, msg)
	})
	if err != nil && ctx.Err() == nil {
		e.logger.Error("loom deadline watcher exited", "err", err)
	}
}

// handleDeadline reacts to a deadline.<instanceId> delete/expiry marker (empty
// body). A value write (the re-arm PUT) carries a body and is ignored.
func (e *Engine) handleDeadline(ctx context.Context, subjPrefix string, msg substrate.Message) substrate.Decision {
	if len(msg.Body) != 0 {
		// A re-arm PUT, not an expiry/delete — nothing to do.
		return substrate.Ack
	}
	instanceID := strings.TrimPrefix(strings.TrimPrefix(msg.Subject, subjPrefix), deadlinePrefix)
	if instanceID == "" {
		return substrate.Ack
	}
	if err := e.onDeadline(ctx, instanceID); err != nil {
		e.logger.Error("loom: deadline handler failed; nak", "instanceId", instanceID, "err", err)
		return substrate.Nak
	}
	return substrate.Ack
}

// onDeadline runs the read-before-act probe for an instance whose step deadline
// fired (Contract #10 §10.6): GET the Contract #4 op tracker for the pending
// token — present → the op committed but its event was missed → advance + alert;
// absent but the outbox record still present → the relay has not delivered →
// re-arm; absent and no outbox record → rejected → fail. Every branch re-reads
// instance state and is CAS-on-running (the advance/fail paths verify the
// pending token), so a redelivered marker / second replica is a no-op.
func (e *Engine) onDeadline(ctx context.Context, instanceID string) error {
	inst, err := e.state.getInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	if inst == nil || inst.Status != StatusRunning {
		// Already terminal, or a stale marker (e.g. the disarm delete from a
		// normal advance/terminal). No-op.
		return nil
	}
	token := inst.PendingToken
	if token == "" {
		return nil
	}

	committed, err := e.trackerExists(ctx, token)
	if err != nil {
		return err
	}
	if committed {
		// The op committed; its completion event was missed (mis-declared
		// completionDomains / lost). Advance off the durable tracker (§10.6).
		e.logger.Warn("loom: completion recovered via deadline probe; check completionDomains",
			"instanceId", instanceID, "requestId", token, "patternRef", inst.PatternRef)
		return e.advance(ctx, instanceID, token)
	}

	outboxPending, err := e.state.outboxExists(ctx, token)
	if err != nil {
		return err
	}
	if outboxPending {
		// The relay has not delivered the op yet — extend the deadline rather
		// than fail.
		e.logger.Info("loom: deadline fired before relay delivered; re-arming",
			"instanceId", instanceID, "requestId", token)
		return e.state.rearmDeadline(ctx, instanceID, e.cfg.StepTimeout)
	}

	// Tracker absent and the op was relayed (no outbox record) → rejected/lost.
	return e.fail(ctx, inst, token, fmt.Sprintf("step %d deadline exceeded; op rejected or lost", inst.Cursor))
}

// trackerExists reports whether the Contract #4 op tracker vtx.op.<requestId>
// exists in Core KV (a read — Loom never writes Core KV). A committed op writes
// the tracker; a rejected op (denied before commit step 8) writes none.
func (e *Engine) trackerExists(ctx context.Context, requestID string) (bool, error) {
	_, err := e.conn.KVGet(ctx, e.cfg.CoreKVBucket, "vtx.op."+requestID)
	if err != nil {
		if errors.Is(err, substrate.ErrKeyNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("loom: probe tracker %q: %w", requestID, err)
	}
	return true, nil
}
