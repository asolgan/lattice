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
	// opCreateTask is the op a userTask step submits to assign its bound op to
	// the instance subject (Contract #10 §10.5).
	opCreateTask = "CreateTask"
)

// userTaskGrantTTL is the expiry horizon set on a userTask's task grant. A
// userTask wait is unbounded by design (§10.6), so the grant outlives any
// realistic human response window; the grant authorizes the user's bound op,
// whose commit auto-completes the task and advances the cursor.
const userTaskGrantTTL = 30 * 24 * time.Hour

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
	// submits under (vtx.identity.<id>, the primordial loom service actor).
	ActorKey string
	// Lane is the ops lane systemOps + lifecycle ops are submitted on. Default "system".
	Lane string
	// StepTimeout is the per-step deadline: a step whose committed event is not
	// seen within this window trips the step-deadline-exceeded handler (the
	// off-stream failed/rejected backstop, §10.6). Must be >= 1s (NATS per-key
	// TTL floor). Default 60s.
	StepTimeout time.Duration
	// CreateTaskTimeout is the bounded creation-deadline a userTask step arms
	// while it waits for its CreateTask to commit (the §10.6 deadline+probe
	// applied to the task-creation path). It backstops a CreateTask that is
	// rejected or lost — without it a userTask whose CreateTask never commits
	// parks forever. It is sized ≫ any CreateTask commit latency (NOT a human
	// response window): once the probe confirms the task vertex exists, the
	// deadline is disarmed and the wait for the human becomes unbounded
	// (§10.6). Must be >= 1s (NATS per-key TTL floor). Default 60s.
	CreateTaskTimeout time.Duration
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
	if c.CreateTaskTimeout <= 0 {
		c.CreateTaskTimeout = 60 * time.Second
	}
	if c.CreateTaskTimeout < time.Second {
		c.CreateTaskTimeout = time.Second
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
// spins up a consumer live, without an engine restart (D2).
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

// runDomainConsumer drives one durable completion consumer on core-events for a
// domain, durable loom-<domain>. It filters events.<domain>.> — every event
// class is `<domain>.<eventName>` (Contract #3 §3.4), subjected
// events.<domain>.<eventName>, so events.<domain>.> always matches. The handler
// is idempotent (at-least-once): it reads the body's correlation keys
// (requestId, taskKey), resolves the durable token pointer, and advances the
// matching instance — dropping any completion whose pointer is gone (already
// advanced).
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

// eventBody is the minimal view of a core-events message Loom reads. It carries
// the two structural correlation keys the contract defines (Contract #10 §10.6),
// both from the Event envelope body (read-from-body, never from the subject):
//   - requestId — the top-level field; the systemOp token (the op's own
//     requestId Loom chose).
//   - payload.taskKey — the userTask token (a vtx.task.<id> the
//     orchestration.taskCompleted event carries under the Event envelope's
//     payload object, Contract #3 §3.4; the top-level requestId on that event is
//     the user's bound-op requestId, which Loom does not know, so it cannot be
//     the correlation key).
//
// Loom stays domain-ignorant: it does not know which event is which, it tries
// both keys against the durable token store and the pointer decides.
type eventBody struct {
	RequestID string `json:"requestId"`
	Payload   struct {
		TaskKey string `json:"taskKey"`
	} `json:"payload"`
}

// handleCompletion correlates a committed business event to its instance by a
// direct token.<token> GET on loom-state and advances the cursor. It tries both
// structural correlation keys (requestId for systemOp, payload.taskKey for
// userTask); at most one resolves a live pointer (tokens are unique). There is
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

	for _, token := range correlationKeys(ev) {
		instanceID, live, err := e.state.resolveToken(ctx, token)
		if err != nil {
			e.logger.Error("loom: token resolve failed; nak", "token", token, "err", err)
			return substrate.Nak
		}
		if !live {
			continue
		}
		if err := e.advance(ctx, instanceID, token); err != nil {
			e.logger.Error("loom advance failed; nak for redelivery",
				"instanceId", instanceID, "token", token, "err", err)
			return substrate.Nak
		}
		return substrate.Ack
	}
	// Not a token Loom is awaiting (another component's event, or a redelivered
	// completion for an already-advanced instance). Drop.
	return substrate.Ack
}

// correlationKeys returns the distinct, non-empty structural correlation keys to
// try for a completion event, in order (systemOp requestId first, userTask
// taskKey second). Trying requestId before payload.taskKey is safe because both
// namespaces are unguessable NanoIDs: an orchestration.taskCompleted event's top-level requestId
// is the user's bound-op id, which cannot collide with a live Loom systemOp token
// (Loom's own op requestId), so the wrong key never resolves a live pointer.
func correlationKeys(ev eventBody) []string {
	keys := make([]string, 0, 2)
	if ev.RequestID != "" {
		keys = append(keys, ev.RequestID)
	}
	if ev.Payload.TaskKey != "" && ev.Payload.TaskKey != ev.RequestID {
		keys = append(keys, ev.Payload.TaskKey)
	}
	return keys
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
// write the outbox.<opRequestId> op record, arm or disarm deadline.<instanceId>).
// The relay publishes the op off that batch — submission is part of the atomic
// fact, not a dual write (Crash-safety invariant 1, §10.6). oldToken == "" for
// step 0. The step is dispatched by Kind: a systemOp submits its bound op
// directly with a bounded deadline; a userTask submits CreateTask and parks for
// a human (the human wait is unbounded; the bounded deadline backstops only the
// task creation, §10.6).
func (e *Engine) submitStep(ctx context.Context, inst *Instance, pattern *Pattern, oldToken string) error {
	step := pattern.Steps[inst.Cursor]
	if step.Kind == StepKindUserTask {
		return e.submitUserTask(ctx, inst, pattern, step, oldToken)
	}
	return e.submitSystemOp(ctx, inst, pattern, step, oldToken)
}

// submitSystemOp submits a step's bound op directly. The write-ahead token is
// the op's own requestId; the step arms the bounded deadline (the off-stream
// rejected/lost backstop, §10.6).
func (e *Engine) submitSystemOp(ctx context.Context, inst *Instance, pattern *Pattern, step Step, oldToken string) error {
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
		"kind", step.Kind, "operation", step.Operation, "requestId", token)
	return nil
}

// submitUserTask submits a CreateTask assigning the step's bound op to the
// instance subject (§10.5: assignedTo/scopedTo = the subject, forOperation = the
// bound op's meta-vertex). The write-ahead token is the taskKey — the
// completion-correlation handle the orchestration.taskCompleted event will carry — derived
// deterministically so a crash-retry re-supplies the SAME taskId and collapses
// on the Contract #4 tracker (no duplicate task). The CreateTask op's own
// requestId is a disjoint deterministic id (the submission idempotency handle).
//
// A bounded CreateTaskTimeout creation-deadline IS armed (the §10.6 deadline+
// probe applied to the task-creation path): waiting for the task vertex to be
// CREATED is a machine action with a tight latency bound, so a rejected/lost
// CreateTask must not park the token forever. The deadline backstops only the
// creation; once onDeadline's probe confirms the task vertex exists, it disarms
// the deadline and the wait for the human becomes unbounded (§10.6) — a
// human may take days, and false-failing that wait would be a correctness bug.
func (e *Engine) submitUserTask(ctx context.Context, inst *Instance, pattern *Pattern, step Step, oldToken string) error {
	forOperation, ok := e.source.opMetaKey(step.Operation)
	if !ok {
		return fmt.Errorf("loom: userTask step %d: no op meta-vertex for operation %q (forOperation unresolved)",
			inst.Cursor, step.Operation)
	}

	taskID := deriveTaskID(inst.InstanceID, inst.Cursor)
	taskKey := "vtx.task." + taskID
	token := taskKey
	inst.PendingToken = token

	opRequestID := deriveRequestID(inst.InstanceID, inst.Cursor)
	payload := map[string]any{
		"assignee":     inst.SubjectKey,
		"forOperation": forOperation,
		"scopedTo":     inst.SubjectKey,
		"expiresAt":    substrate.FormatTimestamp(time.Now().Add(userTaskGrantTTL)),
		"taskId":       taskID,
	}
	ob, err := buildOutbox(opRequestID, opCreateTask, payload, inst.SubjectKey, e.cfg.Lane, e.cfg.ActorKey)
	if err != nil {
		return err
	}
	if err := e.state.transition(ctx, inst, token, oldToken, ob, e.cfg.CreateTaskTimeout); err != nil {
		return err
	}
	e.logger.Info("loom userTask write-ahead",
		"instanceId", inst.InstanceID, "cursor", inst.Cursor,
		"operation", step.Operation, "forOperation", forOperation,
		"taskKey", taskKey, "createTaskRequestId", opRequestID)
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

// userTaskTokenPrefix is the key prefix of a userTask write-ahead token (the
// taskKey, vtx.task.<id>). A token with this prefix is an unbounded human wait,
// distinguishing it from a systemOp token (a bare requestId).
const userTaskTokenPrefix = "vtx.task."

// isUserTaskToken reports whether a pending token is a userTask taskKey.
func isUserTaskToken(token string) bool {
	return strings.HasPrefix(token, userTaskTokenPrefix)
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
//
// A userTask token (vtx.task.<id>) routes to onUserTaskDeadline: the deadline is
// bounded on the task-CREATION only, so the probe reads the task vertex and the
// CreateTask op's tracker/outbox to decide created-vs-rejected.
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
	if isUserTaskToken(token) {
		return e.onUserTaskDeadline(ctx, inst)
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

// onUserTaskDeadline runs the read-before-act probe for a userTask whose bounded
// creation-deadline fired (the §10.6 deadline+probe applied to task creation). It
// distinguishes "still waiting for the task to be CREATED" (a bounded machine
// action) from "the task exists, now waiting for the HUMAN" (unbounded):
//
//  1. GET the task vertex vtx.task.<taskId> from Core KV. Present → the
//     CreateTask committed and the flow is now in the legitimate unbounded human
//     wait → disarm the creation-deadline (the cursor/token are untouched) and
//     stop; the human may take days.
//  2. Absent → probe the CreateTask op like a systemOp deadline: its tracker
//     present → CreateTask committed but the task-vertex read raced/missed →
//     re-arm; else its outbox record still present → the relay has not delivered
//     → re-arm; else (no task, no tracker, no outbox) → CreateTask rejected/lost
//     → fail.
//
// Every branch re-reads instance state via the caller and is CAS-on-running (the
// fail path verifies the pending token), so a redelivered marker / second replica
// is a no-op.
func (e *Engine) onUserTaskDeadline(ctx context.Context, inst *Instance) error {
	taskID := deriveTaskID(inst.InstanceID, inst.Cursor)
	created, err := e.taskVertexExists(ctx, taskID)
	if err != nil {
		return err
	}
	if created {
		// The task vertex exists: the bounded creation wait is over and the
		// unbounded human wait begins. Disarm the deadline without touching the
		// cursor/token — the instance stays running until the human acts.
		e.logger.Info("loom: userTask created; disarming creation-deadline for unbounded human wait",
			"instanceId", inst.InstanceID, "cursor", inst.Cursor, "taskId", taskID)
		return e.state.disarmDeadline(ctx, inst.InstanceID)
	}

	opRequestID := deriveRequestID(inst.InstanceID, inst.Cursor)
	committed, err := e.trackerExists(ctx, opRequestID)
	if err != nil {
		return err
	}
	if committed {
		// CreateTask committed but the task-vertex read raced the commit; the next
		// probe will see the vertex. Extend the creation-deadline rather than fail.
		e.logger.Info("loom: CreateTask committed but task vertex not yet visible; re-arming",
			"instanceId", inst.InstanceID, "cursor", inst.Cursor, "createTaskRequestId", opRequestID)
		return e.state.rearmDeadline(ctx, inst.InstanceID, e.cfg.CreateTaskTimeout)
	}

	outboxPending, err := e.state.outboxExists(ctx, opRequestID)
	if err != nil {
		return err
	}
	if outboxPending {
		// The relay has not delivered the CreateTask yet — extend rather than fail.
		e.logger.Info("loom: creation-deadline fired before relay delivered CreateTask; re-arming",
			"instanceId", inst.InstanceID, "cursor", inst.Cursor, "createTaskRequestId", opRequestID)
		return e.state.rearmDeadline(ctx, inst.InstanceID, e.cfg.CreateTaskTimeout)
	}

	// No task vertex, no tracker, no outbox record → the CreateTask was rejected
	// or lost. Fail the instance rather than park the token forever (§10.6: never
	// a silent wedge).
	return e.fail(ctx, inst, inst.PendingToken,
		fmt.Sprintf("step %d CreateTask rejected", inst.Cursor))
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

// taskVertexExists reports whether the task vertex vtx.task.<taskId> exists in
// Core KV (a read — Loom never writes Core KV). A committed CreateTask mints it;
// a rejected CreateTask mints none. It is the signal that a userTask's bounded
// creation wait is over and the unbounded human wait may begin (§10.6).
func (e *Engine) taskVertexExists(ctx context.Context, taskID string) (bool, error) {
	_, err := e.conn.KVGet(ctx, e.cfg.CoreKVBucket, "vtx.task."+taskID)
	if err != nil {
		if errors.Is(err, substrate.ErrKeyNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("loom: probe task vertex %q: %w", taskID, err)
	}
	return true, nil
}
