// Package bypass — Phase 1 Gate 3: Capability Lens adversarial test suite.
//
// Vector #16 — Root-designation forgery via a `data.protected` bit.
//
// Attack (the escalation root-designation-topology-reconverge, 2026-07-03,
// closed): before this design, core's own Capability Lens sites
// (CapabilityLensDefinition, CapabilityReadWildcardGrantsLensDefinition,
// SystemActorKeys) designated root by `identity.data.protected = true` — a
// literal inside the vertex's own `data`, set at CREATE time, which step-8's
// rejectProtectedMutations does NOT guard (it exempts create). A non-root
// actor forging `protected:true` on a freshly created identity would confer
// itself the fixed kernel root-grant set (write: CreateMetaVertex / ... /
// UpgradePackage at scope:any) AND the wildcard `*` read grant — an
// escalation that was UNCONDITIONAL on the read side (no routing gate at
// all stood between a forged identity and every RLS-protected row).
//
// Defense (Fork A, ratified 2026-07-02): root is designated SOLELY by the
// primordial `holdsRole -> operator` topology (Contract #7 §7.7) — a
// self-protecting mechanism (AssignRole / GrantPermission / CreateRole are
// themselves granted only to `operator` at scope:any, so an actor must
// already hold operator to grant it — root cannot be bootstrapped from
// nothing). `data.protected` is retired as a capability designator; it keeps
// only its unrelated anti-brick meaning (the step-8 update/tombstone guard).
//
// DEFENDED when: a forged `protected:true` identity with NO holdsRole link
// projects ZERO rows from BOTH the write anchor (CapabilityLensDefinition)
// and the read wildcard producer (CapabilityReadWildcardGrantsLensDefinition)
// — the create is allowed to succeed (Fork A drops the draft's create-guard
// as redundant, §3.2 of the design), but the forged vertex confers nothing.
// The positive baseline (a real holdsRole->operator holder DOES project both
// grants) proves the gate discriminates rather than degrading to "always
// empty."
//
// Report row:
//
//	Vector #16 | Root-designation forgery (protected bit) | DEFENDED | holdsRole->operator topology gate (Contract #7 §7.7; root-designation-topology-reconverge)
package bypass

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/asolgan/lattice/internal/bootstrap"
	"github.com/asolgan/lattice/internal/jsstore"
	"github.com/asolgan/lattice/internal/refractor/adjacency"
	"github.com/asolgan/lattice/internal/refractor/ruleengine"
	"github.com/asolgan/lattice/internal/refractor/ruleengine/full"
	"github.com/asolgan/lattice/internal/substrate"
)

// v16StartKVs provisions a self-contained embedded-NATS adjacency + core KV
// pair — mirrors the ruleengine `full` package's own contract-test harness,
// kept local here so this Gate-3 vector runs standalone (no dependency on
// setupCapAdvHarness's core-kv, which carries no adjacency bucket).
func v16StartKVs(t *testing.T) (adjKV, coreKV *substrate.KV) {
	t.Helper()
	opts := &natsserver.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: jsstore.Dir(t)}
	s := natstest.RunServer(opts)
	t.Cleanup(s.Shutdown)
	nc, err := nats.Connect(s.ClientURL())
	if err != nil {
		t.Fatalf("v16: connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("v16: jetstream: %v", err)
	}
	conn, err := substrate.Wrap(nc)
	if err != nil {
		t.Fatalf("v16: wrap: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "v16-adj"}); err != nil {
		t.Fatalf("v16: create adj bucket: %v", err)
	}
	if _, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "v16-core"}); err != nil {
		t.Fatalf("v16: create core bucket: %v", err)
	}
	adjKV, err = conn.OpenKV(ctx, "v16-adj")
	if err != nil {
		t.Fatalf("v16: open adj: %v", err)
	}
	coreKV, err = conn.OpenKV(ctx, "v16-core")
	if err != nil {
		t.Fatalf("v16: open core: %v", err)
	}
	return adjKV, coreKV
}

// v16PutVertex writes a plain vertex whose root `data` carries extra (e.g. the
// forged `protected:true` bit). Mirrors the seeding convention the ruleengine
// contract tests use — no bootstrap envelope, just the raw shape the full
// engine reads (key/class/data).
func v16PutVertex(t *testing.T, kv *substrate.KV, typ, id string, extra map[string]any) string {
	t.Helper()
	key := substrate.VertexKey(typ, id)
	body := map[string]any{"key": key, "class": typ, "data": extra}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("v16: marshal vertex %s: %v", key, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := kv.Put(ctx, key, raw); err != nil {
		t.Fatalf("v16: put vertex %s: %v", key, err)
	}
	return key
}

// v16PutAspect writes an aspect entry — the shape node.<aspect>.data.<field>
// chained property access resolves against (internal/refractor/ruleengine/full
// resolveProperty).
func v16PutAspect(t *testing.T, kv *substrate.KV, vtxKey, localName string, data map[string]any) {
	t.Helper()
	aspectKey := substrate.AspectKey(vtxKey, localName)
	body := map[string]any{"key": aspectKey, "class": localName, "data": data}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("v16: marshal aspect %s: %v", aspectKey, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := kv.Put(ctx, aspectKey, raw); err != nil {
		t.Fatalf("v16: put aspect %s: %v", aspectKey, err)
	}
}

// v16PutHoldsRoleEdge builds the holdsRole adjacency both directions between
// an identity and a role.
func v16PutHoldsRoleEdge(t *testing.T, adjKV *substrate.KV, identityID, roleID string) {
	t.Helper()
	ctx := context.Background()
	edgeID := "holdsRole:" + identityID + ":" + roleID
	if err := adjacency.Build(ctx, adjKV, adjacency.CoreKVEvent{
		CoreKvKey: edgeID, EdgeID: edgeID, Name: "holdsRole",
		Direction: "outbound", NodeID: identityID, OtherNodeID: roleID, OtherType: "role",
	}); err != nil {
		t.Fatalf("v16: build outbound holdsRole: %v", err)
	}
	if err := adjacency.Build(ctx, adjKV, adjacency.CoreKVEvent{
		CoreKvKey: edgeID, EdgeID: edgeID, Name: "holdsRole",
		Direction: "inbound", NodeID: roleID, OtherNodeID: identityID, OtherType: "identity",
	}); err != nil {
		t.Fatalf("v16: build inbound holdsRole: %v", err)
	}
}

// v16RunCypher parses and executes body (a literal bootstrap cypher rule)
// against the given actor key, returning the raw projection rows.
func v16RunCypher(t *testing.T, body string, actorKey string, adjKV, coreKV *substrate.KV) []ruleengine.ProjectionResult {
	t.Helper()
	eng := full.New()
	cr, err := eng.Parse(body)
	if err != nil {
		t.Fatalf("v16: parse cypher: %v", err)
	}
	params := map[string]any{"actorKey": actorKey}
	out, err := eng.ExecuteWith(context.Background(), cr, ruleengine.EventContext{Parameters: params}, adjKV, coreKV)
	if err != nil {
		t.Fatalf("v16: execute cypher: %v", err)
	}
	return out
}

// v16RunWildcardCypher executes the full-graph (unscoped) wildcard-grant
// producer and returns rows carrying the given actor's bare NanoID.
func v16RunWildcardCypher(t *testing.T, body, actorID string, adjKV, coreKV *substrate.KV) []ruleengine.ProjectionResult {
	t.Helper()
	eng := full.New()
	cr, err := eng.Parse(body)
	if err != nil {
		t.Fatalf("v16: parse wildcard cypher: %v", err)
	}
	out, err := eng.ExecuteWith(context.Background(), cr, ruleengine.EventContext{Parameters: map[string]any{}}, adjKV, coreKV)
	if err != nil {
		t.Fatalf("v16: execute wildcard cypher: %v", err)
	}
	var forActor []ruleengine.ProjectionResult
	for _, r := range out {
		if a, _ := r.Values["actor_id"].(string); a == actorID {
			forActor = append(forActor, r)
		}
	}
	return forActor
}

const (
	v16ForgedID  = "V16ForgedNodeAAAAAAA" // 20 chars — forged protected:true, no holdsRole
	v16LegitID   = "V16LegitNodeAAAAAAAA" // 20 chars — real holdsRole->operator holder (positive)
	v16RoleLegit = "V16GrantNodeAAAAAAAA" // 20 chars — operator role for the legit holder
)

// TestCapAdv_V16_ForgedProtected_NoWriteAnchor is the DEFENDED half on the
// write path: a `protected:true` identity with NO holdsRole link projects
// ZERO rows from the write-anchor CapabilityLensDefinition cypher.
func TestCapAdv_V16_ForgedProtected_NoWriteAnchor(t *testing.T) {
	adjKV, coreKV := v16StartKVs(t)

	forgedKey := v16PutVertex(t, coreKV, "identity", v16ForgedID, map[string]any{"protected": true})

	rows := v16RunCypher(t, bootstrap.CapabilityLensDefinition().CypherRule, forgedKey, adjKV, coreKV)
	if len(rows) != 0 {
		t.Fatalf("v16: EXPOSED — a forged protected:true identity with no holdsRole link projected %d write-anchor row(s); root-designation forgery succeeded", len(rows))
	}
	t.Logf("v16: DEFENDED — forged protected:true identity projects NO write-anchor row ✓")
}

// TestCapAdv_V16_ForgedProtected_NoReadWildcardGrant is the DEFENDED half on
// the read path (the historically unconditional escalation): the same forged
// identity projects ZERO rows from the wildcard read-grant producer.
func TestCapAdv_V16_ForgedProtected_NoReadWildcardGrant(t *testing.T) {
	adjKV, coreKV := v16StartKVs(t)

	v16PutVertex(t, coreKV, "identity", v16ForgedID, map[string]any{"protected": true})

	rows := v16RunWildcardCypher(t, bootstrap.CapabilityReadWildcardGrantsLensDefinition().CypherRule, v16ForgedID, adjKV, coreKV)
	if len(rows) != 0 {
		t.Fatalf("v16: EXPOSED — a forged protected:true identity projected %d wildcard read-grant row(s) (anchor_id=%s); unconditional read escalation succeeded", len(rows), v16ForgedID)
	}
	t.Logf("v16: DEFENDED — forged protected:true identity projects NO wildcard read grant ✓")
}

// TestCapAdv_V16_RealOperatorHolder_GetsBothGrants is the positive baseline:
// an identity holding the primordial `operator` role via a real holdsRole
// link projects BOTH the write anchor and the wildcard read grant — proving
// the gate discriminates on topology rather than degrading to "always deny."
func TestCapAdv_V16_RealOperatorHolder_GetsBothGrants(t *testing.T) {
	adjKV, coreKV := v16StartKVs(t)

	legitKey := v16PutVertex(t, coreKV, "identity", v16LegitID, nil)
	roleKey := v16PutVertex(t, coreKV, "role", v16RoleLegit, nil)
	v16PutAspect(t, coreKV, roleKey, "canonicalName", map[string]any{"value": "operator"})
	v16PutHoldsRoleEdge(t, adjKV, v16LegitID, v16RoleLegit)

	writeRows := v16RunCypher(t, bootstrap.CapabilityLensDefinition().CypherRule, legitKey, adjKV, coreKV)
	if len(writeRows) != 1 {
		t.Fatalf("v16 baseline: expected exactly 1 write-anchor row for a real operator holder, got %d", len(writeRows))
	}
	readRows := v16RunWildcardCypher(t, bootstrap.CapabilityReadWildcardGrantsLensDefinition().CypherRule, v16LegitID, adjKV, coreKV)
	if len(readRows) != 1 {
		t.Fatalf("v16 baseline: expected exactly 1 wildcard read-grant row for a real operator holder, got %d", len(readRows))
	}
	t.Logf("v16 baseline: real holdsRole->operator holder projects both the write anchor and the wildcard read grant ✓")
}
