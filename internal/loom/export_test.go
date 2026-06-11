package loom

import (
	"context"

	"github.com/asolgan/lattice/internal/substrate"
)

// PauseForTest manually pauses a managed consumer via the supervisor. Test-only
// seam: Loom exposes no operator Pause/Resume control surface in production
// (that is a future control-plane story), but the supervisor API is callable for
// the pause-restore test.
func (e *Engine) PauseForTest(ctx context.Context, name string) {
	e.supervisor.Pause(ctx, name)
}

// ResetDomainForTest forces a config-divergence Reset of a per-domain consumer
// through the supervisor (UpdateSpec + Reset), exercising the reconcile diff's
// Reset branch — which production never reaches because the per-domain filter is
// name-derived and stable.
func (e *Engine) ResetDomainForTest(ctx context.Context, domain string) error {
	spec := e.domainSpec(domain)
	if err := e.supervisor.UpdateSpec(spec.Name, func(s *substrate.ConsumerSpec) { *s = spec }); err != nil {
		return err
	}
	return e.supervisor.Reset(ctx, spec.Name)
}
