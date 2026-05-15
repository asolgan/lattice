package full

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/asolgan/lattice/internal/refractor/adjacency"
	"github.com/asolgan/lattice/internal/refractor/ruleengine"
)

// startExecKVs spins up an in-memory NATS server with adj and core KV buckets.
// Tests use Materializer-style synthetic keys (no dots) because the current
// adjacency.AdjKey validator rejects Contract #1 vertex keys; the bridge
// between the two formats is a production wiring issue tracked separately
// (see closing summary residual risks).
func startExecKVs(t *testing.T) (adjKV, coreKV jetstream.KeyValue) {
	t.Helper()
	opts := &natsserver.Options{
		JetStream: true,
		StoreDir:  t.TempDir(),
		NoLog:     true,
		NoSigs:    true,
		Port:      natsserver.RANDOM_PORT,
	}
	s, err := natsserver.NewServer(opts)
	require.NoError(t, err)
	go s.Start()
	require.True(t, s.ReadyForConnections(5*time.Second))

	nc, err := nats.Connect(s.ClientURL())
	require.NoError(t, err)
	t.Cleanup(func() { nc.Close(); s.Shutdown() })

	js, err := jetstream.New(nc)
	require.NoError(t, err)

	ctx := context.Background()
	adjKV, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "adj-exec-test"})
	require.NoError(t, err)
	coreKV, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "core-exec-test"})
	require.NoError(t, err)
	return adjKV, coreKV
}

// putVertex writes a vertex to Core KV. Class encodes the cypher label.
func putVertex(t *testing.T, kv jetstream.KeyValue, key, class string, extra map[string]any) {
	t.Helper()
	props := map[string]any{"key": key, "class": class}
	for k, v := range extra {
		props[k] = v
	}
	data, err := json.Marshal(props)
	require.NoError(t, err)
	_, err = kv.Put(context.Background(), key, data)
	require.NoError(t, err)
}

// putEdge writes both inbound and outbound adjacency entries for one edge.
func putEdge(t *testing.T, adjKV jetstream.KeyValue, name, fromKey, toKey string) {
	t.Helper()
	edgeID := name + "_" + fromKey + "_" + toKey
	require.NoError(t, adjacency.Build(adjKV, adjacency.CoreKVEvent{
		CoreKvKey: "edge_" + edgeID, EdgeID: edgeID, Name: name,
		Direction: "outbound", NodeID: fromKey, OtherNodeID: toKey,
	}))
	require.NoError(t, adjacency.Build(adjKV, adjacency.CoreKVEvent{
		CoreKvKey: "edge_" + edgeID, EdgeID: edgeID, Name: name,
		Direction: "inbound", NodeID: toKey, OtherNodeID: fromKey,
	}))
}

// parseExec compiles body and runs ExecuteWith with the given params.
func parseExec(t *testing.T, body string, ec ruleengine.EventContext, adjKV, coreKV jetstream.KeyValue) []ruleengine.ProjectionResult {
	t.Helper()
	eng := New()
	cr, err := eng.Parse(body)
	require.NoError(t, err)
	out, err := eng.ExecuteWith(context.Background(), cr, ec, adjKV, coreKV)
	require.NoError(t, err)
	return out
}

// --- per-feature executor tests ---

func TestExec_SimpleMatchReturn(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	putVertex(t, coreKV, "alice", "identity", map[string]any{"name": "alice"})

	results := parseExec(t,
		`MATCH (i:identity {key: $k}) RETURN i.name AS name`,
		ruleengine.EventContext{Parameters: map[string]any{"k": "alice"}},
		adjKV, coreKV,
	)
	require.Len(t, results, 1)
	require.Equal(t, "alice", results[0].Values["name"])
}

func TestExec_SoftDeleteFiltered(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	putVertex(t, coreKV, "alice", "identity", map[string]any{"name": "alice", "isDeleted": true})

	results := parseExec(t,
		`MATCH (i:identity {key: $k}) RETURN i.name AS name`,
		ruleengine.EventContext{Parameters: map[string]any{"k": "alice"}},
		adjKV, coreKV,
	)
	require.Empty(t, results)
}

func TestExec_MissingParameter(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	eng := New()
	cr, err := eng.Parse(`MATCH (i:identity {key: $k}) RETURN i.name AS name`)
	require.NoError(t, err)
	_, err = eng.ExecuteWith(context.Background(), cr, ruleengine.EventContext{}, adjKV, coreKV)
	require.Error(t, err)
	var mpe *ruleengine.MissingParameterError
	require.True(t, errors.As(err, &mpe))
	require.Equal(t, "k", mpe.Name)
}

func TestExec_OutboundTraversal(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	putVertex(t, coreKV, "alice", "identity", nil)
	putVertex(t, coreKV, "admin", "role", map[string]any{"canonicalName": "admin"})
	putEdge(t, adjKV, "holdsRole", "alice", "admin")

	results := parseExec(t,
		`MATCH (i:identity {key: $k})-[:holdsRole]->(r:role) RETURN r.canonicalName AS role`,
		ruleengine.EventContext{Parameters: map[string]any{"k": "alice"}},
		adjKV, coreKV,
	)
	require.Len(t, results, 1)
	require.Equal(t, "admin", results[0].Values["role"])
}

func TestExec_InboundTraversal(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	putVertex(t, coreKV, "alice", "identity", nil)
	putVertex(t, coreKV, "bob", "identity", nil)
	// bob reportsTo alice → from alice's perspective: alice <-[:reportsTo]- bob
	putEdge(t, adjKV, "reportsTo", "bob", "alice")

	results := parseExec(t,
		`MATCH (i:identity {key: $k})<-[:reportsTo]-(r:identity) RETURN r.key AS reporter`,
		ruleengine.EventContext{Parameters: map[string]any{"k": "alice"}},
		adjKV, coreKV,
	)
	require.Len(t, results, 1)
	require.Equal(t, "bob", results[0].Values["reporter"])
}

func TestExec_OptionalMatchNullPreserving(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	putVertex(t, coreKV, "alice", "identity", map[string]any{"name": "alice"})

	results := parseExec(t,
		`MATCH (i:identity {key: $k}) OPTIONAL MATCH (i)-[:holdsRole]->(r:role) RETURN i.name AS name, r.canonicalName AS role`,
		ruleengine.EventContext{Parameters: map[string]any{"k": "alice"}},
		adjKV, coreKV,
	)
	require.Len(t, results, 1)
	require.Equal(t, "alice", results[0].Values["name"])
	require.Nil(t, results[0].Values["role"])
}

func TestExec_VarLengthTraversal(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	putVertex(t, coreKV, "alice", "identity", nil)
	putVertex(t, coreKV, "room", "location", nil)
	putVertex(t, coreKV, "building", "location", nil)
	putEdge(t, adjKV, "containedIn", "alice", "room")
	putEdge(t, adjKV, "containedIn", "room", "building")

	results := parseExec(t,
		`MATCH (i:identity {key: $k})-[:containedIn*0..]->(l) RETURN l.key AS lkey`,
		ruleengine.EventContext{Parameters: map[string]any{"k": "alice"}},
		adjKV, coreKV,
	)
	// Hop 0 (alice itself), room, building → 3.
	require.Len(t, results, 3)
}

func TestExec_AntiPatternWhere(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	putVertex(t, coreKV, "alice", "identity", nil)
	putVertex(t, coreKV, "svc1", "service", nil)
	putEdge(t, adjKV, "blocked", "alice", "svc1")

	// Returns identity only when NO blocked edge exists.
	results := parseExec(t,
		`MATCH (i:identity {key: $k}) WHERE NOT (i)-[:blocked]->(s:service) RETURN i.key AS k`,
		ruleengine.EventContext{Parameters: map[string]any{"k": "alice"}},
		adjKV, coreKV,
	)
	require.Empty(t, results)
}

func TestExec_WithCollectDistinct(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	putVertex(t, coreKV, "alice", "identity", nil)
	putVertex(t, coreKV, "admin", "role", map[string]any{"canonicalName": "admin"})
	putVertex(t, coreKV, "viewer", "role", map[string]any{"canonicalName": "viewer"})
	putEdge(t, adjKV, "holdsRole", "alice", "admin")
	putEdge(t, adjKV, "holdsRole", "alice", "viewer")

	results := parseExec(t,
		`MATCH (i:identity {key: $k})-[:holdsRole]->(r:role) RETURN i.key AS who, collect(DISTINCT r.canonicalName) AS roles`,
		ruleengine.EventContext{Parameters: map[string]any{"k": "alice"}},
		adjKV, coreKV,
	)
	require.Len(t, results, 1)
	roles, ok := results[0].Values["roles"].([]any)
	require.True(t, ok)
	require.Len(t, roles, 2)
}

func TestExec_MapLiteralAndListConcat(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	putVertex(t, coreKV, "alice", "identity", map[string]any{"name": "alice"})

	results := parseExec(t,
		`MATCH (i:identity {key: $k}) RETURN {name: i.name, key: i.key} AS info, collect(i.key) + collect(i.name) AS combined`,
		ruleengine.EventContext{Parameters: map[string]any{"k": "alice"}},
		adjKV, coreKV,
	)
	require.Len(t, results, 1)
	info, ok := results[0].Values["info"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "alice", info["name"])
	require.Equal(t, "alice", info["key"])
	combined, ok := results[0].Values["combined"].([]any)
	require.True(t, ok)
	require.Len(t, combined, 2)
}

func TestExec_PatternComprehension(t *testing.T) {
	if testing.Short() {
		t.Skip("requires NATS")
	}
	adjKV, coreKV := startExecKVs(t)
	putVertex(t, coreKV, "svc1", "service", nil)
	putVertex(t, coreKV, "opread", "operation", map[string]any{"operationType": "read"})
	putVertex(t, coreKV, "opwrite", "operation", map[string]any{"operationType": "write"})
	putEdge(t, adjKV, "permitsOperation", "svc1", "opread")
	putEdge(t, adjKV, "permitsOperation", "svc1", "opwrite")

	results := parseExec(t,
		`MATCH (s:service {key: $k}) RETURN s.key AS skey, [(s)-[:permitsOperation]->(op) | {operationType: op.operationType}] AS ops`,
		ruleengine.EventContext{Parameters: map[string]any{"k": "svc1"}},
		adjKV, coreKV,
	)
	require.Len(t, results, 1)
	ops, ok := results[0].Values["ops"].([]any)
	require.True(t, ok)
	require.Len(t, ops, 2)
}
