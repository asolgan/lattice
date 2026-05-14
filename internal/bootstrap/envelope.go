package bootstrap

import (
	"encoding/json"
	"time"
)

// BootstrapTime is the canonical createdAt/lastModifiedAt for all primordial
// entries.  Using a fixed timestamp makes bootstrap output deterministic and
// reproducible, which matters for the bypass test oracle in Story 1.10.
var BootstrapTime = time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC)

// Envelope is the universal document envelope per Contract #1 §1.3.
type Envelope struct {
	Key              string          `json:"key"`
	Class            string          `json:"class"`
	IsDeleted        bool            `json:"isDeleted"`
	CreatedAt        string          `json:"createdAt"`
	CreatedBy        string          `json:"createdBy"`
	CreatedByOp      string          `json:"createdByOp"`
	LastModifiedAt   string          `json:"lastModifiedAt"`
	LastModifiedBy   string          `json:"lastModifiedBy"`
	LastModifiedByOp string          `json:"lastModifiedByOp"`
	Data             json.RawMessage `json:"data"`
}

// AspectEnvelope extends Envelope for aspects (Contract #1 §1.3).
type AspectEnvelope struct {
	Envelope
	VertexKey string `json:"vertexKey"`
	LocalName string `json:"localName"`
}

// LinkEnvelope extends Envelope for links (Contract #1 §1.3).
type LinkEnvelope struct {
	Envelope
	YoungerVertex string `json:"youngerVertex"`
	OlderVertex   string `json:"olderVertex"`
	LocalName     string `json:"localName"`
}

func iso(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// MakeVertexEnvelope constructs a vertex envelope (Contract #1 §1.3).
// All provenance fields point to the primordial bootstrap identity + op.
func MakeVertexEnvelope(key, class string, data any) ([]byte, error) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	env := Envelope{
		Key:              key,
		Class:            class,
		IsDeleted:        false,
		CreatedAt:        iso(BootstrapTime),
		CreatedBy:        BootstrapIdentityKey,
		CreatedByOp:      BootstrapOpKey,
		LastModifiedAt:   iso(BootstrapTime),
		LastModifiedBy:   BootstrapIdentityKey,
		LastModifiedByOp: BootstrapOpKey,
		Data:             json.RawMessage(dataJSON),
	}
	return json.Marshal(env)
}

// MakeAspectEnvelope constructs an aspect envelope.
// vertexKey is the parent vertex (key segments 1-3).
// localName is key segment 4.
func MakeAspectEnvelope(key, vertexKey, localName, class string, data any) ([]byte, error) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	env := AspectEnvelope{
		Envelope: Envelope{
			Key:              key,
			Class:            class,
			IsDeleted:        false,
			CreatedAt:        iso(BootstrapTime),
			CreatedBy:        BootstrapIdentityKey,
			CreatedByOp:      BootstrapOpKey,
			LastModifiedAt:   iso(BootstrapTime),
			LastModifiedBy:   BootstrapIdentityKey,
			LastModifiedByOp: BootstrapOpKey,
			Data:             json.RawMessage(dataJSON),
		},
		VertexKey: vertexKey,
		LocalName: localName,
	}
	return json.Marshal(env)
}

// MakeLinkEnvelope constructs a link envelope.
// youngerVertex is key segments 1-3, olderVertex is segments 4-6 (after localName).
func MakeLinkEnvelope(key, youngerVertex, olderVertex, localName, class string, data any) ([]byte, error) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	env := LinkEnvelope{
		Envelope: Envelope{
			Key:              key,
			Class:            class,
			IsDeleted:        false,
			CreatedAt:        iso(BootstrapTime),
			CreatedBy:        BootstrapIdentityKey,
			CreatedByOp:      BootstrapOpKey,
			LastModifiedAt:   iso(BootstrapTime),
			LastModifiedBy:   BootstrapIdentityKey,
			LastModifiedByOp: BootstrapOpKey,
			Data:             json.RawMessage(dataJSON),
		},
		YoungerVertex: youngerVertex,
		OlderVertex:   olderVertex,
		LocalName:     localName,
	}
	return json.Marshal(env)
}

// MakeBootstrapOpEnvelope constructs the special bootstrap op tracker envelope.
// Per Contract #7 §7.2: self-referential provenance (the tracker IS the op record).
// Per Contract #4 §4.1: createdByOp/lastModifiedByOp both point to the tracker itself.
func MakeBootstrapOpEnvelope() ([]byte, error) {
	env := Envelope{
		Key:              BootstrapOpKey,
		Class:            "op.bootstrap",
		IsDeleted:        false,
		CreatedAt:        iso(BootstrapTime),
		CreatedBy:        BootstrapIdentityKey,
		CreatedByOp:      BootstrapOpKey, // self-referential per Contract #4
		LastModifiedAt:   iso(BootstrapTime),
		LastModifiedBy:   BootstrapIdentityKey,
		LastModifiedByOp: BootstrapOpKey, // self-referential per Contract #4
		Data: json.RawMessage(`{
  "status": "committed",
  "operationType": "PrimordialBootstrap",
  "requestId": "` + BootstrapOpID + `",
  "note": "Synthetic platform genesis op tracker. No TTL — permanent record."
}`),
	}
	return json.Marshal(env)
}
