// Package projection compiles an actor-aggregate lens definition into a
// ProjectionPlan{Execution, Invalidation, Output} and drives the live pipeline
// from it. The plan turns per-actor projection behavior into data (lens-
// definition aspects) rather than core Go keyed on a lens canonical name: the
// Output descriptor's EnvelopeFn, BuildKey, and guard predicate replace the
// per-CanonicalName wrappers, so a brand-new package lens projects with no core
// edit. The compiled invalidation forest is the precise reverse-walk the plan
// also carries; the live fan-out uses the broad adjacency BFS (the sound
// superset).
package projection

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/asolgan/lattice/internal/refractor/lens"
	"github.com/asolgan/lattice/internal/refractor/ruleengine"
	"github.com/asolgan/lattice/internal/refractor/ruleengine/full"
	"github.com/asolgan/lattice/internal/refractor/ruleengine/simple"
)

// ActorAggregateKind is the projectionKind aspect value that opts a lens into
// the actor-aggregate projection plan compiler (Contract #6 §6.13).
const ActorAggregateKind = "actorAggregate"

// AuthPlaneBucket is the target bucket that classifies a lens as auth-plane: a
// lens projecting into capability-kv writes an authorization surface (cap.*,
// including the decomposed cap.roles.* / cap.svc.*), so an uncovered MATCH
// construct fails activation closed rather than falling back to broad BFS.
const AuthPlaneBucket = "capability-kv"

// ExecutionPlan is the Execution half of a ProjectionPlan: the per-actor full-
// engine evaluation of the lens for a bound $actorKey. It references the
// existing executor path; the compiler does not change how a row is produced.
type ExecutionPlan struct {
	// Engine is the resolved rule engine name (always "full" for an actor-
	// aggregate lens — the simple engine cannot express the delegation pattern).
	Engine string
	// CompiledRule is the engine-specific compiled artifact the executor
	// consumes via ExecuteWith.
	CompiledRule ruleengine.CompiledRule
	// AnchorType is the actor vertex type the lens projects against.
	AnchorType string
}

// Execute evaluates the lens for one bound actor against the live KV, returning
// the projected RETURN rows. It is the same per-actor eval path the live
// pipeline uses; the projection plan only references it.
func (e *ExecutionPlan) Execute(ctx context.Context, params map[string]any, adjKV, coreKV jetstream.KeyValue) ([]ruleengine.ProjectionResult, error) {
	eng := full.New()
	return eng.ExecuteWith(ctx, e.CompiledRule,
		ruleengine.EventContext{Parameters: params}, adjKV, coreKV)
}

// InvalidationPlan is the Invalidation half: the compiled per-branch reverse-
// traversal forest that derives affected anchors from a changed vertex / link /
// aspect. When the lens uses a construct the compiler cannot prove subset-safe
// on a non-auth lens, Forest is nil and FallbackToBFS is true: the live path
// must use the broad ActorEnumerator BFS instead. An auth-plane lens never
// reaches this state — it fails activation.
type InvalidationPlan struct {
	Forest        *simple.InvalidationForest
	FallbackToBFS bool
}

// AffectedAnchors runs the compiled reverse walk over the forest, unioning the
// per-branch affected-anchor keys. It returns an error if the plan is in BFS-
// fallback mode (the caller must use the broad enumerator instead).
func (p *InvalidationPlan) AffectedAnchors(ctx context.Context, entry simple.NodeEntry, adjKV jetstream.KeyValue) ([]string, error) {
	if p.FallbackToBFS || p.Forest == nil {
		return nil, fmt.Errorf("invalidation: plan is in BFS-fallback mode; use the broad enumerator")
	}
	return p.Forest.AffectedAnchors(ctx, entry, adjKV)
}

// ProjectionPlan is the compiled, data-driven representation of an actor-
// aggregate lens: how to evaluate it for one actor (Execution), how to derive
// the affected anchors from a CDC event (Invalidation), and how to shape and
// key the output document (Output).
type ProjectionPlan struct {
	CanonicalName string
	Execution     ExecutionPlan
	Invalidation  InvalidationPlan
	Output        OutputDescriptor
	// AuthPlane reports whether the lens projects into the capability-kv bucket
	// (an authorization surface). It governs the fail-closed-vs-warn fork.
	AuthPlane bool
}

// IsActorAggregate reports whether a lens rule opts into the actor-aggregate
// projection plan via projectionKind. Routing keys only off this aspect, never
// off the canonical name.
func IsActorAggregate(r *lens.Rule) bool {
	return r != nil && r.ProjectionKind == ActorAggregateKind
}

// RequiresGuard reports whether this plan's writes must run under the §6.2
// monotonic projection-write guard. It is true when the lens projects an
// authorization surface (AuthPlane, target bucket capability-kv) OR its empty
// behavior produces a §6.2 soft tombstone (emptyBehavior ∈ {delete, softDelete}).
// This is the sole gate on enabling the guard — derived from the compiled plan,
// never from a canonical-name list.
func (p *ProjectionPlan) RequiresGuard() bool {
	return p.AuthPlane || p.Output.RequiresGuardedTombstone()
}

// IsAuthPlane classifies a lens as auth-plane iff its target bucket is
// capability-kv. Derived from the bucket, never from a canonical-name list and
// never from an extra aspect (neither is in the ratified Contract #6 §6.13).
func IsAuthPlane(r *lens.Rule) bool {
	return r.Into.Target == "nats_kv" && r.Into.Bucket == AuthPlaneBucket
}

// CompileError is returned when an actor-aggregate lens cannot be compiled into
// a sound projection plan and the lens is auth-plane (fail closed). It names the
// uncovered construct so the activation log is actionable.
type CompileError struct {
	CanonicalName string
	Reason        string
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("projection: activation refused for auth-plane lens %q: %s", e.CanonicalName, e.Reason)
}

// Logger is the minimal logging surface Compile uses to emit the fallback-to-
// BFS warning for a non-auth lens. A nil Logger disables the warning.
type Logger interface {
	Warn(msg string, args ...any)
}

// Compile turns an actor-aggregate lens rule into a ProjectionPlan. It:
//
//   - validates and reads the Output descriptor (§6.13);
//   - analyzes MATCH construct coverage and applies the fail-closed activation
//     policy: an auth-plane lens with an uncovered construct returns a
//     *CompileError (refuse to register); a non-auth lens warns and records a
//     BFS-fallback plan;
//   - compiles the per-branch invalidation forest from the live cypher;
//   - references the per-actor execution path.
//
// Compile must only be called for a lens where IsActorAggregate(r) is true.
func Compile(r *lens.Rule, logger Logger) (*ProjectionPlan, error) {
	if !IsActorAggregate(r) {
		return nil, fmt.Errorf("projection: lens %q is not an actorAggregate (projectionKind=%q)", r.CanonicalName, r.ProjectionKind)
	}

	desc, err := ParseOutputDescriptor(r.Output)
	if err != nil {
		return nil, fmt.Errorf("projection: lens %q: %w", r.CanonicalName, err)
	}

	authPlane := IsAuthPlane(r)

	cov, covErr := simple.AnalyzeInvalidationCoverage(r.Match)
	if covErr != nil {
		// A parse failure is itself an uncovered construct. Fail closed on the
		// auth plane; otherwise it would have failed engine resolution already.
		if authPlane {
			return nil, &CompileError{CanonicalName: r.CanonicalName, Reason: cov.Reason}
		}
	}

	plan := &ProjectionPlan{
		CanonicalName: r.CanonicalName,
		Output:        desc,
		AuthPlane:     authPlane,
		Execution: ExecutionPlan{
			Engine:       r.ResolvedEngine,
			CompiledRule: r.CompiledRule,
			AnchorType:   desc.AnchorType,
		},
	}

	if !cov.Covered {
		if authPlane {
			return nil, &CompileError{CanonicalName: r.CanonicalName, Reason: cov.Reason}
		}
		if logger != nil {
			logger.Warn("projection: actor-aggregate lens uses an uncovered MATCH construct; falling back to broad BFS",
				"canonicalName", r.CanonicalName, "reason", cov.Reason, "bucket", r.Into.Bucket)
		}
		plan.Invalidation = InvalidationPlan{FallbackToBFS: true}
		return plan, nil
	}

	forest, err := simple.CompileInvalidationForest(r.Match)
	if err != nil {
		if authPlane {
			return nil, &CompileError{CanonicalName: r.CanonicalName, Reason: err.Error()}
		}
		if logger != nil {
			logger.Warn("projection: actor-aggregate invalidation compile failed; falling back to broad BFS",
				"canonicalName", r.CanonicalName, "err", err)
		}
		plan.Invalidation = InvalidationPlan{FallbackToBFS: true}
		return plan, nil
	}
	plan.Invalidation = InvalidationPlan{Forest: forest}
	return plan, nil
}
