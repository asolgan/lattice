package substrate

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
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
