package substrate

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
)

// Schedule-publish header names (Contract #10 §10.4, ADR-51). Publishing to a
// stream provisioned with AllowMsgSchedules and these headers set arms a
// NATS-native scheduled message; the scheduler republishes the payload back
// into the same stream at the target subject when the schedule fires.
const (
	// ScheduleHeader carries the schedule spec. Phase 2 uses the one-shot
	// absolute form "@at <RFC3339>".
	ScheduleHeader = "Nats-Schedule"
	// ScheduleTargetHeader carries the republish target subject. The target
	// MUST lie within the scheduling stream's own subject space — the server
	// rejects an out-of-stream target at publish time.
	ScheduleTargetHeader = "Nats-Schedule-Target"
)

// Publish sends a single message to subject through JetStream and waits for the
// server's store ack (ctx-bounds the round trip). It is the fire-and-forget
// primitive for command submission where no reply is awaited — the durable
// record of intent lives in the caller's own store (e.g. Loom's loom-state
// outbox), and the outcome is observed off-stream (a committed event, or a
// timeout). header is optional.
//
// This is deliberately a thin wrapper over the JetStream publish so callers
// (e.g. internal/loom) can submit ops without importing nats.go / jetstream
// directly.
func (c *Conn) Publish(ctx context.Context, subject string, data []byte, header map[string]string) error {
	if subject == "" {
		return fmt.Errorf("substrate: Publish: subject required")
	}
	msg := &nats.Msg{Subject: subject, Data: data}
	if len(header) > 0 {
		msg.Header = nats.Header{}
		for k, v := range header {
			msg.Header.Set(k, v)
		}
	}
	if _, err := c.js.PublishMsg(ctx, msg); err != nil {
		return fmt.Errorf("substrate: publish %q: %w", subject, err)
	}
	return nil
}

// PublishCore sends a single message to subject over core NATS — no JetStream
// stream, no store ack. It is the fire-and-forget primitive for ephemeral
// fan-out (e.g. observability metrics) where no durable record is wanted and the
// subject is not stream-backed; use Publish (JetStream) for durable command
// submission. ctx is honoured for cancellation only — a core publish is a local
// buffer enqueue with no server round trip.
func (c *Conn) PublishCore(ctx context.Context, subject string, data []byte) error {
	if subject == "" {
		return fmt.Errorf("substrate: PublishCore: subject required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.nc.Publish(subject, data); err != nil {
		return fmt.Errorf("substrate: publish-core %q: %w", subject, err)
	}
	return nil
}
