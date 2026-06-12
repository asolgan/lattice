package weaver

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/asolgan/lattice/internal/substrate"
)

// laneConsumerPrefix prefixes a lane-1 durable name: weaver-target-<targetId>.
const laneConsumerPrefix = "weaver-target-"

// Config parameterizes the engine. Bucket/stream names default to the
// platform-standard values; callers (cmd/weaver, tests) override only what
// they need.
type Config struct {
	// CoreKVBucket backs the registry source (vtx.meta.> CDC). Default "core-kv".
	CoreKVBucket string
	// WeaverTargetsBucket is the shared target-Lens projection bucket lane-1
	// consumes (Contract #10 §10.2). Default "weaver-targets".
	WeaverTargetsBucket string
	// WeaverStateBucket holds the §10.3 in-flight marks. Default "weaver-state".
	WeaverStateBucket string
	// HealthKVBucket holds the Contract #5 heartbeat (health.weaver.<instance>)
	// and the per-consumer pause-state entries. Default "health-kv" — matches
	// internal/bootstrap.HealthKVBucket; cmd/weaver may override from there.
	HealthKVBucket string
	// ActorKey is the identity:weaver service-actor vertex key the Actuator
	// submits under (vtx.identity.<id>, the primordial weaver service actor).
	ActorKey string
	// Lane is the ops lane remediation ops are submitted on (the ops.<lane>
	// subject token — a single dot-free token, validated at Start). Default
	// "system".
	Lane string
	// HeartbeatEvery is the Contract #5 heartbeat cadence. The 10s default is
	// the §5.6/NFR-O1 production cadence; a shorter value lets a test observe
	// heartbeat-driven state without waiting out production timing. Values <= 0
	// take the default.
	HeartbeatEvery time.Duration
	// Instance distinguishes this engine process; it suffixes the per-boot
	// registry-source durable so each boot replays the installed target set,
	// and it is the key segment for this process's Contract #5 heartbeat
	// (health.weaver.<instance>) and per-consumer pause-state entries
	// (health.weaver.<instance>.consumer.<name>). MUST be unique per Weaver
	// process sharing a health-kv bucket, and MUST be a single dot-free token
	// (validated at Start — a dot would fragment the health key space and break
	// the durable name). Defaults to "<hostname>-<pid>-<NanoID>" (sanitized)
	// when empty.
	Instance string
	// Logger is the diagnostics sink. Defaults to slog.Default().
	Logger *slog.Logger
}

// instanceSegmentReplacer sanitizes a hostname for use as a KV key segment
// (Contract #5 health.weaver.<instance> /
// health.weaver.<instance>.consumer.<name>): '.' would be read as a
// key-segment separator and is replaced with '-'.
var instanceSegmentReplacer = strings.NewReplacer(".", "-")

// defaultInstance returns a host/pid-attributable, per-construction-unique
// instance id ("<hostname>-<pid>-<NanoID>", sanitized for KV key segments)
// used when Config.Instance is empty. The hostname+pid prefix makes an
// auto-generated health.weaver.<instance> document attributable to the
// process that wrote it (Contract #5); the NanoID suffix preserves per-boot
// uniqueness for the registry-source durable (multiple Engine constructions in
// one process must not collide on the same durable name).
func defaultInstance() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "weaver"
	}
	host = instanceSegmentReplacer.Replace(host)
	suffix, err := substrate.NewNanoID()
	if err != nil {
		suffix = strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return host + "-" + strconv.Itoa(os.Getpid()) + "-" + suffix
}

func (c *Config) withDefaults() {
	if c.CoreKVBucket == "" {
		c.CoreKVBucket = "core-kv"
	}
	if c.WeaverTargetsBucket == "" {
		c.WeaverTargetsBucket = "weaver-targets"
	}
	if c.WeaverStateBucket == "" {
		c.WeaverStateBucket = "weaver-state"
	}
	if c.HealthKVBucket == "" {
		// Literal default mirrors internal/bootstrap.HealthKVBucket; kept literal
		// (like the other bucket defaults) so internal/weaver does not import
		// internal/bootstrap.
		c.HealthKVBucket = "health-kv"
	}
	if c.Lane == "" {
		c.Lane = "system"
	}
	if c.Instance == "" {
		c.Instance = defaultInstance()
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// Engine is the Weaver convergence engine: the meta.weaverTarget registry
// source (Sensorium's definition source), per-target lane-1 KV-CDC consumers
// (Sensorium), Evaluator L1/L2 + Strategist, and the fire-and-forget Actuator.
// Durable dispatch state lives ONLY in weaver-state (the §10.3 mark — the
// in-flight check is a KV read, never an in-memory map); the engine's
// in-memory caches hold derived/registry state rebuilt by CDC replay (the
// registry source, the consumer-state cache).
type Engine struct {
	cfg              Config
	conn             *substrate.Conn
	logger           *slog.Logger
	source           *targetSource
	marks            *markStore
	act              *actuator
	supervisor       *substrate.ConsumerSupervisor
	states           *consumerStateCache
	issues           *issueCache
	rowSubjectPrefix string

	mu sync.Mutex
	// targets is the last-applied desired lane-1 consumer set (targetId →
	// applied spec fingerprint), diffed on each reconcile against the live
	// registry.
	targets map[string]specFingerprint

	ctx context.Context
}

// specFingerprint is the subset of a ConsumerSpec's config that, if it
// changes, requires the durable to be recreated (Reset). Hooks (Handler/
// Classify/Probe/Health) are intentionally excluded — they are refreshed via
// UpdateSpec without recreating the durable.
type specFingerprint struct {
	stream        string
	filterSubject string
	deliverPolicy substrate.DeliverPolicy
	deliverGroup  string
}

func fingerprintOf(spec substrate.ConsumerSpec) specFingerprint {
	return specFingerprint{
		stream:        spec.Stream,
		filterSubject: spec.FilterSubject,
		deliverPolicy: spec.DeliverPolicy,
		deliverGroup:  spec.DeliverGroup,
	}
}

// NewEngine constructs an Engine over conn.
func NewEngine(conn *substrate.Conn, cfg Config) *Engine {
	cfg.withDefaults()
	issues := newIssueCache()
	e := &Engine{
		cfg:              cfg,
		conn:             conn,
		logger:           cfg.Logger,
		marks:            newMarkStore(conn, cfg.WeaverStateBucket),
		act:              newActuator(conn, cfg.Lane, cfg.ActorKey, cfg.Logger),
		supervisor:       substrate.NewConsumerSupervisor(conn),
		states:           newConsumerStateCache(),
		issues:           issues,
		rowSubjectPrefix: "$KV." + cfg.WeaverTargetsBucket + ".",
		targets:          make(map[string]specFingerprint),
	}
	e.source = newTargetSource(conn, cfg.CoreKVBucket, cfg.Instance, issues, cfg.Logger)
	return e
}

// Start runs the engine until ctx is cancelled. It (1) validates the config
// tokens that feed KV keys, subjects, and durable names; (2) starts the
// Contract #5 heartbeater; (3) starts the registry source whose load/update
// callbacks reconcile the per-target lane-1 consumers; (4) seeds one reconcile
// (a restart must not depend solely on source callbacks to bring consumers
// up); (5) blocks on ctx.
func (e *Engine) Start(ctx context.Context) (err error) {
	if !singleTokenPattern.MatchString(e.cfg.Instance) {
		return fmt.Errorf("weaver: Instance %q must be a single dot-free token (it is a Contract #5 health key segment and a durable-name segment; must match %s)",
			e.cfg.Instance, singleTokenPattern.String())
	}
	if !singleTokenPattern.MatchString(e.cfg.Lane) {
		return fmt.Errorf("weaver: Lane %q must be a single dot-free subject token (ops are published to ops.<lane>; must match %s)",
			e.cfg.Lane, singleTokenPattern.String())
	}
	e.ctx = ctx

	defer func() {
		if err != nil {
			e.supervisor.Stop()
		}
	}()

	hb := newHeartbeater(e.conn, e.cfg.HealthKVBucket, e.cfg.Instance, e.cfg.HeartbeatEvery,
		e.states, e.issues, e.source, e.marks, e.logger)
	go hb.run(ctx)

	e.source.setLoadCallback(func(*Target) { e.reconcileConsumers() })
	e.source.setUpdateCallback(func(_, _ *Target) { e.reconcileConsumers() })
	if err := e.source.start(ctx); err != nil {
		return fmt.Errorf("weaver: start target source: %w", err)
	}
	e.reconcileConsumers()

	e.logger.Info("weaver engine started",
		"coreKV", e.cfg.CoreKVBucket, "targets", e.cfg.WeaverTargetsBucket,
		"state", e.cfg.WeaverStateBucket, "lane", e.cfg.Lane)
	<-ctx.Done()
	e.supervisor.Stop()
	return nil
}

// supervisedHandler adapts a Decision-returning handler to the supervisor's
// SupervisedHandler signature. The handler already encodes every outcome as a
// Decision, so the error channel is always nil and Classify is never exercised
// on this path (a nil Classify = always transient is the accepted posture).
func supervisedHandler(h func(context.Context, substrate.Message) substrate.Decision) substrate.SupervisedHandler {
	return func(ctx context.Context, msg substrate.Message) (substrate.Decision, error) {
		return h(ctx, msg), nil
	}
}

// targetSpec describes one lane-1 consumer: a per-target supervised KV-CDC
// durable on the weaver-targets backing stream, filtered to the target's key
// prefix, DeliverLastPerSubject (the Refractor CDC pattern — never a raw KV
// watcher).
func (e *Engine) targetSpec(targetID string) substrate.ConsumerSpec {
	name := laneConsumerPrefix + targetID
	return substrate.ConsumerSpec{
		Name:          name,
		Stream:        "KV_" + e.cfg.WeaverTargetsBucket,
		FilterSubject: e.rowSubjectPrefix + targetID + ".>",
		DeliverPolicy: substrate.DeliverLastPerSubject,
		Handler:       supervisedHandler(e.handleRow),
		Health:        newConsumerHealthSink(e.conn, e.cfg.HealthKVBucket, e.cfg.Instance, name, e.states),
		Logger:        e.logger,
	}
}

// reconcileConsumers diffs the desired lane-1 consumer set (the registered
// targets) against the last-applied set, driving the supervisor:
//
//   - a target newly registered → Add weaver-target-<targetId>;
//   - a target removed/revoked → Remove (the supervisor stops the pump AND
//     deletes the JetStream durable — an un-pumped server-side durable IS a
//     leak; re-add replays via DeliverLastPerSubject, safe because the mark
//     CAS + level-reconcile make the handler idempotent) and its health-sink
//     entry is deleted;
//   - a registered target whose desired spec differs from the running one →
//     UpdateSpec + Reset (delete-and-recreate), never silently unchanged.
//
// The per-target filter is name-derived and stable, so the Reset branch is
// mechanically reachable only if a future spec field changes; the diff is
// written generically so such a change is caught. The WHOLE pass runs under
// e.mu so concurrent passes serialize.
//
// A failed Add/Remove/Reset raises a Health KV issue (cleared on a later
// success for the same target) — the discrepancy never rides silently on the
// heartbeat. The retry is the next reconcile pass (the next registry event);
// there is no retry ticker here.
func (e *Engine) reconcileConsumers() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx == nil || e.ctx.Err() != nil {
		return
	}
	desired := make(map[string]struct{})
	for _, id := range e.source.targetIDs() {
		desired[id] = struct{}{}
	}
	for id := range desired {
		spec := e.targetSpec(id)
		fp := fingerprintOf(spec)
		applied, running := e.targets[id]
		if !running {
			if err := e.supervisor.Add(e.ctx, spec); err != nil {
				e.logger.Error("weaver target consumer add failed", "targetId", id, "err", err)
				e.issues.set(issueKeyConsumer(id), "error", "ConsumerReconcileError",
					"target "+id+": lane-1 consumer add failed: "+err.Error())
				continue
			}
			e.issues.clear(issueKeyConsumer(id))
			e.targets[id] = fp
			e.logger.Info("weaver target consumer added", "targetId", id, "durable", spec.Name)
			continue
		}
		if applied == fp {
			continue
		}
		if err := e.supervisor.UpdateSpec(spec.Name, func(s *substrate.ConsumerSpec) { *s = spec }); err != nil {
			e.logger.Error("weaver target consumer update-spec failed", "targetId", id, "err", err)
			e.issues.set(issueKeyConsumer(id), "error", "ConsumerReconcileError",
				"target "+id+": lane-1 consumer update-spec failed: "+err.Error())
			continue
		}
		if err := e.supervisor.Reset(e.ctx, spec.Name); err != nil {
			e.logger.Error("weaver target consumer reset failed", "targetId", id, "err", err)
			e.issues.set(issueKeyConsumer(id), "error", "ConsumerReconcileError",
				"target "+id+": lane-1 consumer reset failed: "+err.Error())
			continue
		}
		e.issues.clear(issueKeyConsumer(id))
		e.targets[id] = fp
		e.logger.Info("weaver target consumer reset", "targetId", id, "durable", spec.Name)
	}
	for id := range e.targets {
		if _, want := desired[id]; want {
			continue
		}
		name := laneConsumerPrefix + id
		if err := e.supervisor.Remove(e.ctx, name); err != nil {
			e.logger.Error("weaver target consumer remove failed", "targetId", id, "err", err)
			e.issues.set(issueKeyConsumer(id), "error", "ConsumerReconcileError",
				"target "+id+": lane-1 consumer remove failed (durable may leak until the next reconcile): "+err.Error())
			continue
		}
		e.issues.clear(issueKeyConsumer(id))
		delete(e.targets, id)
		sink := newConsumerHealthSink(e.conn, e.cfg.HealthKVBucket, e.cfg.Instance, name, e.states)
		if err := sink.delete(e.ctx); err != nil {
			e.logger.Error("weaver target consumer health-state cleanup failed", "targetId", id, "durable", name, "err", err)
		}
		e.logger.Info("weaver target consumer removed", "targetId", id, "durable", name)
	}
}

func issueKeyConsumer(targetID string) string { return "consumer:" + targetID }
