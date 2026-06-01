// Package outbox implements the durable transactional-event-outbox consumer.
//
// The Processor persists each operation's faithful EventList as a sibling
// aspect (`vtx.op.<requestId>.events`) inside the step-8 atomic batch, so the
// events are durable iff the commit succeeds. This consumer reads those aspects
// from the Core KV stream and publishes the real events to `core-events`,
// acking only after a confirmed publish — then tombstones the aspect. A crash
// between commit and publish is recovered by redelivery from the durable
// offset; events are at-least-once and never reconstructed.
package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/asolgan/lattice/internal/processor"
	"github.com/asolgan/lattice/internal/substrate"
)

const (
	// ConsumerName is the durable consumer name on the Core KV stream.
	ConsumerName = "processor-outbox"
	reconnect    = 5 * time.Second
)

// Consumer drives the durable outbox consumer on the Core KV stream. It
// filters for outbox aspects (`$KV.<bucket>.vtx.op.*.events`), publishes the
// persisted EventList to `core-events`, tombstones the aspect, then acks.
type Consumer struct {
	conn         *substrate.Conn
	streamName   string
	filterSubj   string
	bucket       string
	subjectPrefx string // "$KV.<bucket>." — strip from msg.Subject() to recover the Core KV key
	publisher    *EventPublisherImpl
	logger       *slog.Logger
}

// New constructs the outbox Consumer for the given Core KV bucket.
func New(conn *substrate.Conn, coreKVBucket string, logger *slog.Logger) *Consumer {
	if conn == nil {
		panic("outbox: New requires Conn")
	}
	if coreKVBucket == "" {
		panic("outbox: New requires coreKVBucket")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Consumer{
		conn:         conn,
		streamName:   "KV_" + coreKVBucket,
		filterSubj:   "$KV." + coreKVBucket + ".vtx.op.*.events",
		bucket:       coreKVBucket,
		subjectPrefx: "$KV." + coreKVBucket + ".",
		publisher:    NewEventPublisher(conn, logger),
		logger:       logger,
	}
}

// Run creates the durable consumer (idempotent) and processes outbox aspects
// until ctx is cancelled. Run blocks until ctx is done.
func (c *Consumer) Run(ctx context.Context) error {
	cons, err := c.conn.JetStream().CreateOrUpdateConsumer(ctx, c.streamName, jetstream.ConsumerConfig{
		Durable:       ConsumerName,
		FilterSubject: c.filterSubj,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return fmt.Errorf("outbox: create consumer: %w", err)
	}
	c.loop(ctx, cons)
	return nil
}

// loop reopens the message iterator on transient errors until ctx is done.
func (c *Consumer) loop(ctx context.Context, cons jetstream.Consumer) {
	for {
		if ctx.Err() != nil {
			return
		}
		mc, err := cons.Messages()
		if err != nil {
			c.logger.Error("outbox: open messages iterator", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(reconnect):
			}
			continue
		}
		c.drain(ctx, mc)
	}
}

// drain reads messages from mc until ctx is cancelled or mc returns an error.
// A watcher stops the iterator when ctx is cancelled so the blocking Next()
// unblocks promptly for a clean shutdown.
func (c *Consumer) drain(ctx context.Context, mc jetstream.MessagesContext) {
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			mc.Stop()
		case <-stopped:
		}
	}()
	defer func() {
		close(stopped)
		mc.Stop()
	}()
	for {
		msg, err := mc.Next()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Error("outbox: receive error, will reconnect", "error", err)
			return
		}
		c.processMsg(ctx, msg)
	}
}

// processMsg handles a single outbox-aspect delivery: empty/tombstone/PURGE
// bodies are acked and skipped; otherwise the persisted EventList is published
// to `core-events`, the aspect is tombstoned, and the message is acked. Nak on
// publish failure so JetStream redelivers (events stay at-least-once).
func (c *Consumer) processMsg(ctx context.Context, msg jetstream.Msg) {
	// KV tombstone / PURGE / TTL-expiry markers have empty bodies — ack + skip.
	// This also covers our own post-publish tombstone on a full seq-0 replay.
	if len(msg.Data()) == 0 {
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("outbox: ack empty/tombstone", "error", ackErr)
		}
		return
	}

	// Recover the Core KV key from the JetStream subject ($KV.<bucket>.<key>).
	key := strings.TrimPrefix(msg.Subject(), c.subjectPrefx)

	aspect, err := processor.ParseOutboxAspect(msg.Data())
	if err != nil {
		// An unparseable outbox record is structurally invalid and an
		// event-loss risk; term it (poison message) and log loudly.
		c.logger.Error("outbox: unparseable aspect — terminating (event-loss risk)",
			"key", key, "error", err)
		if termErr := msg.Term(); termErr != nil {
			c.logger.Error("outbox: term failed", "error", termErr)
		}
		return
	}

	// A tombstoned aspect (isDeleted) carries no events to publish — ack + skip.
	if aspect.IsDeleted || len(aspect.Data.Events) == 0 {
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("outbox: ack deleted/empty aspect", "error", ackErr)
		}
		return
	}

	// Publish the faithful EventList. The publisher batches all events for the
	// operation into one ordered PublishBatch, preserving intra-op order.
	env := &processor.OperationEnvelope{RequestID: aspect.Data.RequestID}
	if err := c.publisher.Publish(ctx, env, aspect.Data.Events); err != nil {
		c.logger.Warn("outbox: publish failed; nak for redelivery",
			"key", key, "requestId", aspect.Data.RequestID, "error", err)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Error("outbox: nak failed", "error", nakErr)
		}
		return
	}

	// Tombstone the aspect (cleanup + replay-safety) before acking. A failure
	// here is tolerated — the events were published, and a redelivery would at
	// most re-publish once (consumers are idempotent).
	if delErr := c.conn.KVDelete(ctx, c.bucket, key); delErr != nil {
		c.logger.Warn("outbox: tombstone failed (events already published)",
			"key", key, "requestId", aspect.Data.RequestID, "error", delErr)
	}

	if ackErr := msg.Ack(); ackErr != nil {
		c.logger.Error("outbox: ack after publish failed", "error", ackErr)
	}
}
