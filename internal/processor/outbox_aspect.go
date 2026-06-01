package processor

import (
	"encoding/json"
	"fmt"
)

// OutboxAspectClass is the Contract #1 class of the transactional-outbox
// aspect that carries an operation's faithful EventList. It is platform
// infrastructure (like the op tracker) and has no registered DDL.
const OutboxAspectClass = "op-outbox"

// OutboxLocalName is the aspect localName under the op-tracker vertex.
const OutboxLocalName = "events"

// OutboxAspectKey returns the Core KV key for an operation's outbox aspect
// (`vtx.op.<requestId>.events`). It is a sibling aspect of the op tracker
// (`vtx.op.<requestId>`) written atomically with the commit.
func OutboxAspectKey(requestID string) string {
	return TrackerKey(requestID) + "." + OutboxLocalName
}

// OutboxData is the payload carried under the outbox aspect's `data` field:
// the operation's request id and the faithful EventList produced by
// BuildEventList (full events — eventId, payload, targetKey, timestamp).
type OutboxData struct {
	RequestID string    `json:"requestId"`
	Events    EventList `json:"events"`
}

// OutboxAspect is the Contract #1 aspect envelope that durably persists an
// operation's EventList as part of the step-8 atomic batch. The outbox
// consumer reads it, publishes the events to `core-events`, then tombstones
// the aspect. It carries NO per-key TTL so it outlives the 24h dedup tracker.
type OutboxAspect struct {
	Key              string     `json:"key"`
	Class            string     `json:"class"`
	IsDeleted        bool       `json:"isDeleted"`
	VertexKey        string     `json:"vertexKey"`
	LocalName        string     `json:"localName"`
	CreatedAt        string     `json:"createdAt"`
	CreatedBy        string     `json:"createdBy"`
	CreatedByOp      string     `json:"createdByOp"`
	LastModifiedAt   string     `json:"lastModifiedAt"`
	LastModifiedBy   string     `json:"lastModifiedBy"`
	LastModifiedByOp string     `json:"lastModifiedByOp"`
	Data             OutboxData `json:"data"`
}

// NewOutboxAspect builds the outbox aspect for an operation from its faithful
// EventList. stamp is the commit timestamp; actor is the operation's actor;
// trackerKey is the op-tracker vertex key (the createdByOp/lastModifiedByOp).
func NewOutboxAspect(requestID, actor, trackerKey, stamp string, events EventList) OutboxAspect {
	return OutboxAspect{
		Key:              OutboxAspectKey(requestID),
		Class:            OutboxAspectClass,
		IsDeleted:        false,
		VertexKey:        trackerKey,
		LocalName:        OutboxLocalName,
		CreatedAt:        stamp,
		CreatedBy:        actor,
		CreatedByOp:      trackerKey,
		LastModifiedAt:   stamp,
		LastModifiedBy:   actor,
		LastModifiedByOp: trackerKey,
		Data: OutboxData{
			RequestID: requestID,
			Events:    events,
		},
	}
}

// Marshal returns the JSON encoding of the outbox aspect (Core KV value).
func (a OutboxAspect) Marshal() ([]byte, error) { return json.Marshal(a) }

// ParseOutboxAspect decodes an outbox aspect read back from Core KV.
func ParseOutboxAspect(b []byte) (*OutboxAspect, error) {
	var a OutboxAspect
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, fmt.Errorf("outbox aspect: json decode: %w", err)
	}
	return &a, nil
}
