package processor

import (
	"context"
	"log/slog"
)

// AuthMode selects the Story-3.3-swappable Authorizer.
type AuthMode string

const (
	// AuthModeStub is the Story 1.5 default — always-allow.
	AuthModeStub AuthMode = "stub"
	// AuthModeCapability is reserved for Story 3.3 (Capability KV auth).
	AuthModeCapability AuthMode = "capability"
)

// Decision is the outcome of an Authorizer.Authorize call.
type Decision struct {
	Authorized bool
	// Stub is true when the decision came from StubAuthorizer (helps
	// downstream logging and bypass-test assertions distinguish a real
	// allow from a stubbed allow).
	Stub bool
	// Reason carries a short human-readable explanation. Empty when
	// Authorized=true.
	Reason string
	// Code is set when Authorized=false. Maps to Contract #2 §2.6 reply
	// error codes (LaneUnauthorized, AuthDenied, AuthContextMismatch).
	Code ErrorCode
}

// Authorizer is the step-3 interface. Story 1.5 ships StubAuthorizer;
// Story 3.3 replaces it with a Capability-KV-backed implementation behind
// the same interface so no commit-path wiring changes.
type Authorizer interface {
	Authorize(ctx context.Context, env *OperationEnvelope) (Decision, error)
}

// StubAuthorizer always returns Authorized=true, Stub=true. It is the
// platform default until Story 3.3 lights up real Capability KV auth.
type StubAuthorizer struct {
	logger *slog.Logger
}

// NewStubAuthorizer constructs the stub. Pass a logger so warnings are
// emitted on each Authorize call (auditability — operators must be able
// to see when their cluster is running the stub).
func NewStubAuthorizer(logger *slog.Logger) *StubAuthorizer {
	if logger == nil {
		logger = slog.Default()
	}
	return &StubAuthorizer{logger: logger}
}

// Authorize implements Authorizer.
func (s *StubAuthorizer) Authorize(_ context.Context, env *OperationEnvelope) (Decision, error) {
	s.logger.Warn("STUB AUTH: allow-all (Story 1.5; replaced by Capability KV in Story 3.3)",
		"requestId", env.RequestID,
		"actor", env.Actor,
		"operationType", env.OperationType,
		"lane", string(env.Lane),
	)
	return Decision{Authorized: true, Stub: true}, nil
}

// SelectAuthorizer returns the Authorizer implementation matching mode.
// `capability` mode is reserved — selecting it before Story 3.3 lands
// returns an error so misconfigured deployments fail loudly at startup
// rather than silently degrading to stub.
func SelectAuthorizer(mode AuthMode, logger *slog.Logger) (Authorizer, error) {
	switch mode {
	case "", AuthModeStub:
		return NewStubAuthorizer(logger), nil
	case AuthModeCapability:
		return nil, errCapabilityModeNotYetAvailable
	default:
		return nil, errUnknownAuthMode(mode)
	}
}
