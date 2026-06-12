package weaver

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/asolgan/lattice/internal/substrate"
)

// consumerHealthEntry is Weaver's minimal per-consumer pause-state document,
// stored under health.weaver.<instance>.consumer.<name> in the health-kv
// bucket — a SEPARATE, smaller shape from the Contract #5 heartbeat document.
// It carries only the fields HealthSink.Load needs to restore pause state
// across a restart.
type consumerHealthEntry struct {
	Status      string `json:"status"`                // "active" | "paused"
	PauseReason string `json:"pauseReason,omitempty"` // "infra" | "structural" | "manual"
	LastError   string `json:"lastError,omitempty"`
}

// consumerHealthSink implements substrate.HealthSink for one managed consumer.
// Each consumer gets its own sink instance keyed by name (for lane-1 the
// durable weaver-target-<targetId> — the per-target health surface). Every
// supervisor transition is funnelled through this
// sink: it persists to health-kv AND updates the engine's in-memory
// consumer-state cache, which the Contract #5 heartbeater reads to populate
// metrics.consumers.
type consumerHealthSink struct {
	conn   *substrate.Conn
	bucket string
	key    string
	name   string
	states *consumerStateCache
}

func newConsumerHealthSink(conn *substrate.Conn, bucket, instance, name string, states *consumerStateCache) *consumerHealthSink {
	return &consumerHealthSink{
		conn:   conn,
		bucket: bucket,
		key:    "health.weaver." + instance + ".consumer." + name,
		name:   name,
		states: states,
	}
}

func (s *consumerHealthSink) SetActive(ctx context.Context) error {
	s.states.set(s.name, consumerState(false, ""))
	return s.put(ctx, consumerHealthEntry{Status: "active"})
}

func (s *consumerHealthSink) SetPaused(ctx context.Context, reason substrate.PauseReason, lastErr string) error {
	s.states.set(s.name, consumerState(true, reason))
	return s.put(ctx, consumerHealthEntry{
		Status:      "paused",
		PauseReason: string(reason),
		LastError:   lastErr,
	})
}

// Load restores the persisted pause state at supervisor Add time. A missing or
// malformed entry resolves to (StatusActive, "", nil) per the HealthSink
// contract. It also seeds the in-memory state cache with the restored state.
func (s *consumerHealthSink) Load(ctx context.Context) (substrate.HealthStatus, substrate.PauseReason, error) {
	entry, err := s.conn.KVGet(ctx, s.bucket, s.key)
	if err != nil {
		if errors.Is(err, substrate.ErrKeyNotFound) {
			s.states.set(s.name, consumerState(false, ""))
			return substrate.StatusActive, "", nil
		}
		return substrate.StatusActive, "", err
	}
	var doc consumerHealthEntry
	if err := json.Unmarshal(entry.Value, &doc); err != nil {
		s.states.set(s.name, consumerState(false, ""))
		return substrate.StatusActive, "", nil
	}
	if doc.Status != "paused" {
		s.states.set(s.name, consumerState(false, ""))
		return substrate.StatusActive, "", nil
	}
	reason := pauseReasonFromString(doc.PauseReason)
	s.states.set(s.name, consumerState(true, reason))
	return substrate.StatusPaused, reason, nil
}

// delete removes the persisted pause-state entry and the in-memory
// consumer-state cache entry for this consumer. Called when the consumer is
// torn down (supervisor.Remove) so a future re-add of the same name does not
// restore a stale pause and the heartbeat does not report a phantom consumer.
func (s *consumerHealthSink) delete(ctx context.Context) error {
	s.states.delete(s.name)
	if err := s.conn.KVDelete(ctx, s.bucket, s.key); err != nil && !errors.Is(err, substrate.ErrKeyNotFound) {
		return err
	}
	return nil
}

func (s *consumerHealthSink) put(ctx context.Context, entry consumerHealthEntry) error {
	body, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = s.conn.KVPut(ctx, s.bucket, s.key, body)
	return err
}

func pauseReasonFromString(s string) substrate.PauseReason {
	switch s {
	case string(substrate.PauseManual):
		return substrate.PauseManual
	case string(substrate.PauseStructural):
		return substrate.PauseStructural
	default:
		return substrate.PauseInfra
	}
}
