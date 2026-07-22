package main

import (
	"context"
	"time"

	"github.com/operatinggraph/lattice/internal/healthkv"
	"github.com/operatinggraph/lattice/internal/substrate"
)

// healthProbe re-checks the Facet host's own dependencies each tick — the
// host↔NATS health connection, the identityCredentialsRead read model, and
// (in host-engine mode only) the in-process engine fleet's connectivity and
// sync-loop state — so a heartbeat can never merely echo a boot-time
// snapshot. This is the operator-facing half of the silent per-user sync
// wedge ce050a7 left unaddressed (facet-host-health-emission-design.md §1).
func (s *server) healthProbe(ctx context.Context, healthConn *substrate.Conn) healthkv.Snapshot {
	var issues []healthkv.Issue

	if healthConn == nil || !healthConn.NATS().IsConnected() {
		issues = append(issues, healthkv.Issue{
			Code:     "NatsUnreachable",
			Severity: "error",
			Message:  "host health connection is down; this heartbeat itself is degraded or absent",
		})
	}

	mode := "host-engine"
	metrics := map[string]any{"mode": mode}

	if s.browserEngine != nil {
		// Browser-native mode: engines live in-page, invisible to the host by
		// design (§7 non-goal) — report the host's own serving posture only,
		// never fabricated fleet counts.
		mode = "browser-native"
		metrics["mode"] = mode
	} else {
		snap := s.engines.healthSnapshot()
		metrics["engines_active"] = snap.Total
		metrics["engines_pinned"] = snap.Pinned
		metrics["engines_sync_degraded"] = snap.SyncDegraded
		metrics["engines_nats_disconnected"] = snap.NatsDisconnected

		if snap.SyncDegraded > 0 {
			issues = append(issues, healthkv.Issue{
				Code:     "EngineSyncDegraded",
				Severity: "warning",
				Message:  "one or more in-process engines are in sync-manager restart-backoff",
			})
		}
		if snap.NatsDisconnected > 0 {
			issues = append(issues, healthkv.Issue{
				Code:     "EngineNatsDisconnected",
				Severity: "warning",
				Message:  "one or more in-process engines' per-identity NATS connection is currently down",
			})
		}
	}

	if s.pgPool != nil {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := s.pgPool.Ping(pingCtx)
		cancel()
		if err != nil {
			issues = append(issues, healthkv.Issue{
				Code:     "ReadModelUnreachable",
				Severity: "warning",
				Message:  "identityCredentialsRead Postgres pool unreachable; /api/credentials will 502",
			})
		}
	}

	status := healthkv.StatusHealthy
	for _, iss := range issues {
		if iss.Severity == "error" {
			status = healthkv.StatusUnhealthy
			break
		}
		status = healthkv.StatusDegraded
	}

	return healthkv.Snapshot{Status: status, Issues: issues, Metrics: metrics}
}
