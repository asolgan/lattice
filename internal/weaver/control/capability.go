package control

import (
	"context"
	"log/slog"
)

// CapabilityChecker authorizes a control-plane operation against
// Lattice's Capability KV (Contract #6). This package ships a stub
// implementation that allow-all and logs every call — full integration
// is Epic 3 work. The interface lives here so the control service can
// be swapped to a real checker without touching handler bodies. Mirrors
// internal/refractor/control.CapabilityChecker.
type CapabilityChecker interface {
	// Authorize returns nil if the given actor may invoke op on the given
	// target. Returns a non-nil error when the operation must be denied.
	Authorize(ctx context.Context, actor, op, targetID string) error
}

// StubCapabilityChecker is the default implementation: allow every request
// and log it. Mirrors internal/refractor/control.StubCapabilityChecker.
type StubCapabilityChecker struct {
	Logger *slog.Logger
}

// NewStubCapabilityChecker constructs a permissive checker.
func NewStubCapabilityChecker(logger *slog.Logger) *StubCapabilityChecker {
	if logger == nil {
		logger = slog.Default()
	}
	return &StubCapabilityChecker{Logger: logger}
}

// Authorize always returns nil and logs the call.
func (s *StubCapabilityChecker) Authorize(ctx context.Context, actor, op, targetID string) error {
	s.Logger.Info("weaver control capability stub: ALLOW", "actor", actor, "op", op, "targetId", targetID)
	return nil
}
