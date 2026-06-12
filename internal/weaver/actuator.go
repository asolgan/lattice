package weaver

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/asolgan/lattice/internal/substrate"
)

// opEnvelope is the wire format published to ops.<lane> (Contract #2 §2.1) —
// the envelope fields Weaver populates, serialized in the shape the
// Processor's consume path reads; weaver carries its own copy to keep the
// module boundary clean (no internal/processor import).
type opEnvelope struct {
	RequestID     string          `json:"requestId"`
	Lane          string          `json:"lane"`
	OperationType string          `json:"operationType"`
	Actor         string          `json:"actor"`
	SubmittedAt   string          `json:"submittedAt"`
	Payload       json.RawMessage `json:"payload"`
	AuthContext   *authContext    `json:"authContext,omitempty"`
}

type authContext struct {
	Target string `json:"target,omitempty"`
}

// actuator submits remediation ops. The submit is ONE fire-and-forget publish
// to ops.<lane> — no request-reply (a synchronous reply wait blocks the
// consumer and forces a raw NATS handle into the engine) and no command
// outbox: Weaver, unlike Loom, has no cursor advance to keep atomic with the
// submit. Its crash-recovery story is the §10.3 weaver-state mark +
// level-reconcile: a failed publish Naks the CDC message, the redelivery
// re-reads the existing mark, and the retry re-publishes the SAME
// deterministic requestId, which collapses on the Contract #4
// vtx.op.<requestId> tracker.
type actuator struct {
	conn   *substrate.Conn
	lane   string
	actor  string
	logger *slog.Logger
}

func newActuator(conn *substrate.Conn, lane, actor string, logger *slog.Logger) *actuator {
	if logger == nil {
		logger = slog.Default()
	}
	return &actuator{conn: conn, lane: lane, actor: actor, logger: logger}
}

// submit publishes one remediation op under Weaver's service-actor authority.
func (a *actuator) submit(ctx context.Context, requestID, operationType string, payload map[string]any, authTarget string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("weaver: marshal op payload: %w", err)
	}
	env := opEnvelope{
		RequestID:     requestID,
		Lane:          a.lane,
		OperationType: operationType,
		Actor:         a.actor,
		SubmittedAt:   substrate.FormatTimestamp(time.Now()),
		Payload:       body,
	}
	if authTarget != "" {
		env.AuthContext = &authContext{Target: authTarget}
	}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("weaver: marshal op envelope: %w", err)
	}
	if err := a.conn.Publish(ctx, "ops."+a.lane, data, nil); err != nil {
		return fmt.Errorf("weaver: publish op %s: %w", requestID, err)
	}
	a.logger.Info("weaver op submitted",
		"requestId", requestID, "operation", operationType, "lane", a.lane, "authTarget", authTarget)
	return nil
}

// deriveEpisodeRequestID returns a deterministic 20-char NanoID (over the
// canonical Lattice alphabet, Contract #1) for one dispatch episode. The
// episode tag is the mark's KV create revision: a re-fire of the SAME episode
// (publish-failure retry, CDC redelivery) reuses the same requestId and
// collapses on the Contract #4 vtx.op.<requestId> tracker; a legitimately
// re-opened gap (mark deleted, new CAS-create) gets a new revision → a new
// requestId → a real new dispatch.
func deriveEpisodeRequestID(targetID, entityID, gapColumn string, markRevision uint64) string {
	return deriveID("episode:", targetID+"\x00"+entityID+"\x00"+gapColumn, markRevision)
}

// deriveEpisodeTaskID returns the deterministic task NanoID an assignTask
// episode supplies to CreateTask (the verbatim taskId seam, Contract #10
// §10.6): a re-fire of the same episode re-supplies the same taskId, so the
// duplicate CreateTask collapses on the Contract #4 tracker — no duplicate
// task. It is namespaced disjoint from deriveEpisodeRequestID so the op's
// requestId and the task id never collide for the same episode.
func deriveEpisodeTaskID(targetID, entityID, gapColumn string, markRevision uint64) string {
	return deriveID("task:", targetID+"\x00"+entityID+"\x00"+gapColumn, markRevision)
}

// deriveID is the shared deterministic NanoID derivation: sha256 over the
// namespaced seed, expanded across the canonical alphabet by re-hashing.
func deriveID(namespace, seed string, revision uint64) string {
	var rev [8]byte
	binary.BigEndian.PutUint64(rev[:], revision)
	sum := sha256.Sum256(append([]byte(namespace+seed+":"), rev[:]...))
	id := make([]byte, substrate.NanoIDLength)
	digest := sum[:]
	di := 0
	for i := 0; i < substrate.NanoIDLength; i++ {
		if di >= len(digest) {
			next := sha256.Sum256(digest)
			digest = next[:]
			di = 0
		}
		id[i] = substrate.Alphabet[int(digest[di])%len(substrate.Alphabet)]
		di++
	}
	return string(id)
}
