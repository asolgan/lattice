package substrate

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
)

// MaxBatchMessages is the maximum number of messages a single JetStream
// atomic batch may contain, per NATS 2.14 (ADR-50: "Each batch can have
// maximum 1000 messages"; the server abandons an over-limit batch with
// err_code 10199). Not server-configurable downward without resurfacing the
// raw error this guard prevents (Contract #3 §3.9.1 deployment invariant).
// The Processor's batch = business mutations + the idempotency tracker +
// (optional) the outbox aspect, so a single operation's business-mutation
// budget is MaxBatchMessages - 2 = 998.
const MaxBatchMessages = 1000

// ValueHeadroomBytes is reserved below the connection's negotiated
// max_payload for the message's batch/revision/TTL headers and the
// Processor's commit-time provenance injection (createdAt/By/ByOp,
// lastModified*). Deriving the per-value ceiling from the live negotiated
// max_payload (rather than a hardcoded 1 MiB) honors a production override
// automatically.
const ValueHeadroomBytes = 4 * 1024

// BatchOp describes a single write inside an atomic batch. Callers
// construct one BatchOp per Core KV mutation and pass the slice to
// AtomicBatch. The helper drives the raw NATS batch headers internally —
// callers never touch Nats-Batch-* directly.
//
// Op semantics:
//
//   - Create-if-absent: leave HasRevision false AND set Revision to 0 by
//     setting CreateOnly true. (Both forms are equivalent at the wire — a
//     revision condition of 0 means "key must not exist". CreateOnly is
//     provided as a more readable spelling for the common create-tracker
//     pattern.)
//
//   - Revision-conditioned update: set HasRevision true and Revision to
//     the expected current revision.
//
//   - Per-key TTL (used for op trackers per Contract #4 §4.3): set TTL to
//     a non-zero duration.
//
//   - Unconditional put: leave CreateOnly false, HasRevision false,
//     and Revision 0. (Note: at most one batch member can be unconditioned
//     against a given key; in practice the Processor always uses Create
//     or Update.)
type BatchOp struct {
	Bucket      string
	Key         string
	Value       []byte
	CreateOnly  bool
	HasRevision bool
	Revision    uint64
	TTL         time.Duration
	// Delete writes a NATS KV delete marker (KV-Operation: DEL) instead of a
	// value put, so a key can be removed within the same atomic batch as other
	// puts. Value is ignored when Delete is set; a subsequent read returns
	// ErrKeyNotFound. HasRevision/Revision still apply (a revision-conditioned
	// delete); CreateOnly is meaningless for a delete and is ignored.
	Delete bool
}

// BatchAck describes the server's atomic-commit acknowledgement for a
// successful AtomicBatch. Stream + Sequence identify the last message
// (the commit message); BatchID echoes the unique batch identifier
// substrate assigned; Count is the total messages in the batch.
//
// Revisions maps each op's key to the Core KV revision it committed at.
// An atomic batch commits all N messages as a contiguous stream block,
// and for a Core KV bucket an entry's revision equals its stream
// sequence; the per-key revision is therefore derived from the commit
// ack's last sequence and batch size. Revisions is nil when the
// contiguous-sequence invariant cannot be verified from the ack.
type BatchAck struct {
	Stream    string
	Sequence  uint64
	BatchID   string
	Count     uint64
	Revisions map[string]uint64
}

// AtomicBatch publishes ops as a single NATS JetStream atomic batch.
// Either every op is durably committed or none are. On failure the
// returned error wraps ErrAtomicBatchRejected.
//
// The atomic batch is implemented over the raw NATS protocol because the
// nats.go client does not expose a high-level PublishBatch API. This helper
// hides those mechanics from callers.
//
// Requirements:
//
//   - Every op's bucket must have AllowAtomicPublish enabled on its
//     underlying KV_<bucket> stream (Core KV is provisioned this way by
//     the bootstrap path).
//
//   - All ops in a single AtomicBatch call must target the SAME bucket.
//     Cross-bucket atomicity is not supported by NATS atomic batch;
//     pass one bucket per call.
//
//   - ctx bounds the round trip on the commit message and is checked
//     before each fire-and-forget publish. Callers wrap ctx with the
//     deadline appropriate to the operation's lane SLA.
func (c *Conn) AtomicBatch(ctx context.Context, ops []BatchOp) (*BatchAck, error) {
	if len(ops) == 0 {
		return nil, fmt.Errorf("substrate: AtomicBatch: empty op list")
	}
	if err := c.checkBatchSize(ops); err != nil {
		return nil, err
	}

	bucket := ops[0].Bucket
	for i, op := range ops {
		if op.Bucket != bucket {
			return nil, fmt.Errorf(
				"substrate: AtomicBatch: cross-bucket batch not supported (op[0]=%q op[%d]=%q)",
				bucket, i, op.Bucket)
		}
		if op.Key == "" {
			return nil, fmt.Errorf("substrate: AtomicBatch: op[%d] missing key", i)
		}
	}

	batchID, err := NewNanoID()
	if err != nil {
		return nil, fmt.Errorf("substrate: AtomicBatch: generate batch id: %w", err)
	}

	msgs := make([]*nats.Msg, len(ops))
	for i, op := range ops {
		m := nats.NewMsg(kvBucketSubject(op.Bucket, op.Key))
		m.Data = op.Value
		m.Header = nats.Header{}
		if op.Delete {
			// NATS KV delete marker: an empty body carrying the KV-Operation
			// header. The server removes the visible value; subsequent reads
			// return ErrKeyNotFound.
			m.Data = nil
			m.Header.Set("KV-Operation", "DEL")
		}
		if op.CreateOnly && !op.Delete {
			m.Header.Set("Nats-Expected-Last-Subject-Sequence", "0")
		} else if op.HasRevision {
			m.Header.Set("Nats-Expected-Last-Subject-Sequence",
				strconv.FormatUint(op.Revision, 10))
		}
		if op.TTL > 0 {
			m.Header.Set("Nats-TTL", op.TTL.String())
		}
		msgs[i] = m
	}

	ack, err := publishAtomicBatch(ctx, c.nc, batchID, msgs)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAtomicBatchRejected, err)
	}
	if ack.Error != nil {
		return nil, fmt.Errorf("%w: code=%d err_code=%d: %s",
			ErrAtomicBatchRejected, ack.Error.Code, ack.Error.ErrCode, ack.Error.Description)
	}
	return &BatchAck{
		Stream:    ack.Stream,
		Sequence:  ack.Sequence,
		BatchID:   batchID,
		Count:     ack.BatchSize,
		Revisions: deriveRevisions(ops, ack.Sequence, ack.BatchSize),
	}, nil
}

// deriveRevisions maps each op's key to its committed Core KV revision.
// An atomic batch lands as a contiguous block of stream sequences ending
// at lastSeq; the first member's sequence is lastSeq-batchSize+1, and a
// Core KV entry's revision equals its stream sequence. Revisions are only
// derived when the contiguous-sequence invariant holds for this ack;
// otherwise nil is returned and no revisions are fabricated. Duplicate
// keys resolve last-write-wins in op order.
func deriveRevisions(ops []BatchOp, lastSeq, batchSize uint64) map[string]uint64 {
	if batchSize != uint64(len(ops)) || lastSeq+1 < batchSize {
		return nil
	}
	firstSeq := lastSeq - batchSize + 1
	revisions := make(map[string]uint64, len(ops))
	for i, op := range ops {
		revisions[op.Key] = firstSeq + uint64(i)
	}
	return revisions
}

// checkBatchSize enforces the two NATS 2.14 atomic-batch bounds (Contract #3
// §3.9.1) before any message is built or published: the message-count
// ceiling and, per op, the per-value payload ceiling derived from the live
// negotiated max_payload. Delete ops carry no body and are exempt from the
// value check. Returns ErrBatchTooLarge / ErrValueTooLarge un-wrapped — this
// is a pre-flight guard, never a NATS-reported rejection.
func (c *Conn) checkBatchSize(ops []BatchOp) error {
	if len(ops) > MaxBatchMessages {
		return fmt.Errorf("%w: %d messages > %d", ErrBatchTooLarge, len(ops), MaxBatchMessages)
	}
	limit := c.valueSizeLimit()
	for i, op := range ops {
		if !op.Delete && len(op.Value) > limit {
			return fmt.Errorf("%w: op[%d] key=%q value=%d bytes > %d",
				ErrValueTooLarge, i, op.Key, len(op.Value), limit)
		}
	}
	return nil
}

// valueSizeLimit derives the per-message payload ceiling from the
// connection's server-negotiated max_payload, less ValueHeadroomBytes.
func (c *Conn) valueSizeLimit() int {
	return int(c.nc.MaxPayload()) - ValueHeadroomBytes
}

// kvBucketSubject returns the JetStream publish subject for a Core KV key.
// KV publish subjects follow the pattern: $KV.<bucket>.<key>
func kvBucketSubject(bucket, key string) string {
	return "$KV." + bucket + "." + key
}

// pubAckResponse mirrors the NATS PubAck JSON envelope returned by the
// server in response to the commit message.
type pubAckResponse struct {
	Stream    string  `json:"stream"`
	Sequence  uint64  `json:"seq"`
	Duplicate bool    `json:"duplicate"`
	BatchID   string  `json:"batch,omitempty"`
	BatchSize uint64  `json:"count,omitempty"`
	Error     *apiErr `json:"error,omitempty"`
}

type apiErr struct {
	Code        int    `json:"code"`
	ErrCode     uint16 `json:"err_code"`
	Description string `json:"description"`
}

// PublishOp describes a single message inside a non-conditional batch
// publish to JetStream. Unlike BatchOp, PublishOp
// targets arbitrary JetStream subjects (e.g. `events.identity.created`)
// rather than KV-bucket subjects, and it does not carry revision
// conditions — the batch is unconditional. Ordering within the batch is
// preserved by `Nats-Batch-Sequence` (1..N), and either the entire
// batch lands or none of it does.
//
// Note: the destination subjects must all belong to the SAME JetStream
// stream (the atomic-batch primitive is stream-scoped). For the
// Processor's event publish, all subjects share the `events.>` filter
// on the `core-events` stream.
type PublishOp struct {
	Subject string
	Data    []byte
	Header  map[string]string // optional extra headers
}

// PublishBatchAck mirrors BatchAck for a non-conditional batch publish.
type PublishBatchAck struct {
	Stream   string
	Sequence uint64
	BatchID  string
	Count    uint64
}

// PublishBatch publishes ops as a single JetStream atomic batch to
// arbitrary subjects (no revision conditions, no per-key TTL). All
// subjects must belong to the same JetStream stream — typically the
// `core-events` stream's `events.>` filter, published by the Processor's outbox consumer.
//
// Order is preserved via `Nats-Batch-Sequence` (1..N). On failure, no
// message is durably stored — semantics are all-or-nothing.
func (c *Conn) PublishBatch(ctx context.Context, ops []PublishOp) (*PublishBatchAck, error) {
	if len(ops) == 0 {
		return nil, fmt.Errorf("substrate: PublishBatch: empty op list")
	}
	if len(ops) > MaxBatchMessages {
		return nil, fmt.Errorf("%w: %d messages > %d", ErrBatchTooLarge, len(ops), MaxBatchMessages)
	}
	limit := c.valueSizeLimit()
	for i, op := range ops {
		if len(op.Data) > limit {
			return nil, fmt.Errorf("%w: op[%d] subject=%q value=%d bytes > %d",
				ErrValueTooLarge, i, op.Subject, len(op.Data), limit)
		}
	}
	for i, op := range ops {
		if op.Subject == "" {
			return nil, fmt.Errorf("substrate: PublishBatch: op[%d] missing subject", i)
		}
	}

	batchID, err := NewNanoID()
	if err != nil {
		return nil, fmt.Errorf("substrate: PublishBatch: generate batch id: %w", err)
	}

	msgs := make([]*nats.Msg, len(ops))
	for i, op := range ops {
		m := nats.NewMsg(op.Subject)
		m.Data = op.Data
		m.Header = nats.Header{}
		for k, v := range op.Header {
			m.Header.Set(k, v)
		}
		msgs[i] = m
	}

	ack, err := publishAtomicBatch(ctx, c.nc, batchID, msgs)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAtomicBatchRejected, err)
	}
	if ack.Error != nil {
		return nil, fmt.Errorf("%w: code=%d err_code=%d: %s",
			ErrAtomicBatchRejected, ack.Error.Code, ack.Error.ErrCode, ack.Error.Description)
	}
	return &PublishBatchAck{
		Stream:   ack.Stream,
		Sequence: ack.Sequence,
		BatchID:  batchID,
		Count:    ack.BatchSize,
	}, nil
}

// publishAtomicBatch is the raw-protocol atomic-batch publisher.
// All-but-last messages are fire-and-forget; the last carries
// Nats-Batch-Commit and is sent via RequestMsgWithContext so the server's
// commit ack can be parsed and so ctx cancellation/deadline bounds the
// round trip. nats.go has no PublishMsgWithContext, so each
// fire-and-forget send is gated on a ctx.Err() check.
func publishAtomicBatch(ctx context.Context, nc *nats.Conn, batchID string, messages []*nats.Msg) (*pubAckResponse, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("empty batch")
	}
	for i, m := range messages {
		if m.Header == nil {
			m.Header = nats.Header{}
		}
		seq := uint64(i + 1)
		m.Header.Set("Nats-Batch-Id", batchID)
		m.Header.Set("Nats-Batch-Sequence", strconv.FormatUint(seq, 10))

		if i < len(messages)-1 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("publish msg %d: %w", seq, err)
			}
			if err := nc.PublishMsg(m); err != nil {
				return nil, fmt.Errorf("publish msg %d: %w", seq, err)
			}
			continue
		}
		// Last message — commit and wait for ack.
		m.Header.Set("Nats-Batch-Commit", "1")
		resp, err := nc.RequestMsgWithContext(ctx, m)
		if err != nil {
			return nil, fmt.Errorf("request commit msg: %w", err)
		}
		var ack pubAckResponse
		if err := json.Unmarshal(resp.Data, &ack); err != nil {
			return nil, fmt.Errorf("unmarshal ack: %w (raw: %s)", err, string(resp.Data))
		}
		return &ack, nil
	}
	panic("substrate: publishAtomicBatch: unreachable")
}
