package processor

import (
	"log/slog"
	"time"

	"github.com/asolgan/lattice/internal/substrate"
)

// LegacyDurable is the pre-per-lane single consumer name. A startup migration
// deletes it (substrate.Conn.DeleteStreamConsumer) so its un-acked messages
// redeliver to the per-lane durables (at-least-once + step-2 dedup make the
// one-time redelivery idempotent).
const LegacyDurable = "processor-main"

// laneOrder is the deterministic lane list. It mirrors Contract #2 §2.3
// (default / urgent / system / meta) and fixes iteration order for stable
// per-lane health output and spec construction.
var laneOrder = []string{"default", "urgent", "system", "meta"}

// laneDurable maps a lane name to its JetStream durable consumer name. Each
// lane gets its own durable bound to the `ops.<lane>` subject so per-lane
// backlog (Contract #5 §5.4 lane_lag) is separable and lanes drain
// independently (priority isolation — urgent never queues behind default).
var laneDurable = map[string]string{
	"default": "processor-default",
	"urgent":  "processor-urgent",
	"system":  "processor-system",
	"meta":    "processor-meta",
}

// LaneDurables returns a fresh lane→durable map for the four operation lanes,
// for wiring the health heartbeater's per-lane backlog reads. A copy is
// returned so callers cannot mutate the package's canonical mapping.
func LaneDurables() map[string]string {
	out := make(map[string]string, len(laneDurable))
	for lane, durable := range laneDurable {
		out[lane] = durable
	}
	return out
}

// LaneSpecs builds the four per-lane ConsumerSupervisor specs for the Processor
// commit path, one durable per lane bound to its `ops.<lane>` subject. All four
// share the single supervised handler (the commit path is concurrency-correct —
// step-8 OCC + the RWMutex-guarded DDL cache; see the per-lane-consumers design
// §5.2). The `meta` lane is pinned to MaxAckPending=1 (Contract #2 §3.7) so DDL
// mutations are serialized server-side as well as by its single sequential pump.
//
// FilterSubject is the exact two-segment `ops.<lane>` subject every publisher
// emits (submit.go / candidates.go), matching the legacy processor-main filter
// list. Per-lane intra-concurrency (a queue-group fan-out from
// LATTICE_PROCESSOR_LANES_<LANE>_CONSUMERS) is the next increment; meta stays
// clamped to one regardless.
func LaneSpecs(stream string, handler substrate.SupervisedHandler, ackWait time.Duration, logger *slog.Logger) []substrate.ConsumerSpec {
	specs := make([]substrate.ConsumerSpec, 0, len(laneOrder))
	for _, lane := range laneOrder {
		spec := substrate.ConsumerSpec{
			Name:          laneDurable[lane],
			Stream:        stream,
			FilterSubject: "ops." + lane,
			DeliverPolicy: substrate.DeliverAll,
			AckWait:       ackWait,
			Handler:       handler,
			Logger:        logger,
		}
		if lane == "meta" {
			// Serial by contract (§2.3 / §3.7): one in-flight DDL mutation at a
			// time, so a meta-vertex commit + its synchronous DDL-cache
			// invalidation never races a second concurrent DDL mutation.
			spec.MaxAckPending = 1
		}
		specs = append(specs, spec)
	}
	return specs
}
