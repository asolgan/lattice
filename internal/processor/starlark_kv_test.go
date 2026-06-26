package processor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Tests for the Contract #2 §2.5 lazy on-demand `kv.Read()` Starlark builtin
// (starlark_kv.go): the read-before-create idempotency seam.
//
// The unit tests drive a real StarlarkRunner with a fake ScriptKVReader and
// observe the read result through the script's returned events. The final test
// exercises the production connKVReader adapter against a real embedded Core KV.

// fakeKVReader is an in-memory ScriptKVReader. A key present in docs returns its
// doc; a key absent returns (nil, nil) (the absent/tombstoned signal); a non-nil
// err returns that error for every read. It records every key it was asked for
// so a test can prove the cache-first path skipped it.
type fakeKVReader struct {
	docs  map[string]*VertexDoc
	err   error
	calls []string
}

func (f *fakeKVReader) ReadVertex(_ context.Context, key string) (*VertexDoc, error) {
	f.calls = append(f.calls, key)
	if f.err != nil {
		return nil, f.err
	}
	if d, ok := f.docs[key]; ok {
		return d, nil
	}
	return nil, nil
}

// runKVScript runs source against sc with a default-budget runner, supplying a
// minimal operation envelope when the test left one unset.
func runKVScript(t *testing.T, sc ScriptContext, source string) (ScriptResult, error) {
	t.Helper()
	sc.ScriptSource = source
	if sc.Operation == nil {
		sc.Operation = &OperationEnvelope{
			RequestID: "req-kv-test", Lane: LaneDefault, OperationType: "X",
			Actor: "a", SubmittedAt: "t", Payload: []byte("{}"),
		}
	}
	return NewStarlarkRunner(0, 0).Run(context.Background(), sc)
}

// TestKVRead_AbsentReturnsNone — a read of a key the reader does not have yields
// None, so the script can branch into its create path. The load-bearing case
// for idempotent create: absence is graceful, not a fatal HydrationMiss.
func TestKVRead_AbsentReturnsNone(t *testing.T) {
	sc := ScriptContext{KVReader: &fakeKVReader{docs: map[string]*VertexDoc{}}}
	res, err := runKVScript(t, sc, `
def execute(state, op):
    v = kv.Read("vtx.task.missing")
    cls = "none" if v == None else "present"
    return {"mutations": [], "events": [{"class": cls}]}
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Events) != 1 || res.Events[0].Class != "none" {
		t.Fatalf("expected one 'none' event, got %+v", res.Events)
	}
}

// TestKVRead_PresentReturnsProjectedDoc — a present key returns a struct with the
// same shape as a `state` entry: .class, .isDeleted, .revision, .data[...].
func TestKVRead_PresentReturnsProjectedDoc(t *testing.T) {
	sc := ScriptContext{KVReader: &fakeKVReader{docs: map[string]*VertexDoc{
		"vtx.task.t1": {
			Key: "vtx.task.t1", Class: "task", IsDeleted: false, Revision: 7,
			Data: map[string]interface{}{"status": "open"},
		},
	}}}
	res, err := runKVScript(t, sc, `
def execute(state, op):
    v = kv.Read("vtx.task.t1")
    return {"mutations": [], "events": [{"class": "read", "data": {
        "cls": getattr(v, "class"), "del": v.isDeleted, "rev": v.revision, "status": v.data["status"],
    }}]}
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := res.Events[0].Data
	if d["cls"] != "task" {
		t.Errorf("class = %v, want task", d["cls"])
	}
	if d["del"] != false {
		t.Errorf("isDeleted = %v, want false", d["del"])
	}
	if d["rev"] != int64(7) {
		t.Errorf("revision = %v (%T), want int64(7)", d["rev"], d["rev"])
	}
	if d["status"] != "open" {
		t.Errorf("data.status = %v, want open", d["status"])
	}
}

// TestKVRead_CacheFirstSkipsReader — a key already in the hydrated working set
// (declared via contextHint.reads, pre-fetched at step 4) is served from the
// cache with NO reader round-trip (§2.5 "Starlark reads hit the cache"). Proven
// two ways: the reader is never called, and the value returned is the hydrated
// one, not the (deliberately divergent) value the reader holds.
func TestKVRead_CacheFirstSkipsReader(t *testing.T) {
	reader := &fakeKVReader{docs: map[string]*VertexDoc{
		"vtx.task.cached": {Key: "vtx.task.cached", Class: "from-reader-WRONG"},
	}}
	sc := ScriptContext{
		Hydrated: map[string]VertexDoc{
			"vtx.task.cached": {
				Key: "vtx.task.cached", Class: "task", Revision: 3,
				Data: map[string]interface{}{"v": "hydrated"},
			},
		},
		KVReader: reader,
	}
	res, err := runKVScript(t, sc, `
def execute(state, op):
    v = kv.Read("vtx.task.cached")
    return {"mutations": [], "events": [{"class": "read", "data": {"cls": getattr(v, "class"), "v": v.data["v"]}}]}
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reader.calls) != 0 {
		t.Fatalf("cache hit must skip the reader, but it was called for: %v", reader.calls)
	}
	d := res.Events[0].Data
	if d["cls"] != "task" || d["v"] != "hydrated" {
		t.Fatalf("expected hydrated value, got %+v", d)
	}
}

// TestKVRead_MixedCachedAndOnDemand — the two §2.5 paths coexist correctly in
// ONE execution: a contextHint-hydrated key is served from the cache (reader
// untouched), an undeclared present key falls through to the reader, and an
// undeclared absent key falls through and yields None. Guards against a
// cache-first short-circuit accidentally suppressing later on-demand reads.
func TestKVRead_MixedCachedAndOnDemand(t *testing.T) {
	reader := &fakeKVReader{docs: map[string]*VertexDoc{
		"vtx.task.live": {Key: "vtx.task.live", Class: "task", Revision: 5, Data: map[string]interface{}{"src": "reader"}},
	}}
	sc := ScriptContext{
		Hydrated: map[string]VertexDoc{
			"vtx.task.cached": {Key: "vtx.task.cached", Class: "task", Revision: 2, Data: map[string]interface{}{"src": "cache"}},
		},
		KVReader: reader,
	}
	res, err := runKVScript(t, sc, `
def execute(state, op):
    c = kv.Read("vtx.task.cached")
    l = kv.Read("vtx.task.live")
    m = kv.Read("vtx.task.missing")
    return {"mutations": [], "events": [{"class": "read", "data": {
        "cached": c.data["src"], "live": l.data["src"], "missing": m == None,
    }}]}
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := res.Events[0].Data
	if d["cached"] != "cache" {
		t.Errorf("cached key: got %v, want the hydrated value 'cache'", d["cached"])
	}
	if d["live"] != "reader" {
		t.Errorf("on-demand present key: got %v, want 'reader'", d["live"])
	}
	if d["missing"] != true {
		t.Errorf("on-demand absent key: got %v, want None", d["missing"])
	}
	// The reader is consulted for exactly the two undeclared keys — never the
	// hydrated one.
	if len(reader.calls) != 2 {
		t.Fatalf("reader calls = %v, want exactly the two undeclared keys", reader.calls)
	}
	for _, c := range reader.calls {
		if c == "vtx.task.cached" {
			t.Fatalf("reader was called for the cached key: %v", reader.calls)
		}
	}
}

// blockingKVReader blocks until its context is cancelled, then returns the
// context error — a stand-in for a hung Core KV read.
type blockingKVReader struct{}

func (blockingKVReader) ReadVertex(ctx context.Context, _ string) (*VertexDoc, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestKVRead_SlowReadHitsWallBudget — a hung on-demand read is bounded by the
// script wall budget and classified as ScriptTimeout. The elapsed-time guard
// also proves the wall-budget context is actually threaded into kv.Read: if it
// were not, the read would block on the (longer) parent ctx and overrun.
func TestKVRead_SlowReadHitsWallBudget(t *testing.T) {
	sc := ScriptContext{KVReader: blockingKVReader{}}
	sc.ScriptSource = `
def execute(state, op):
    v = kv.Read("vtx.task.slow")
    return {"mutations": [], "events": []}
`
	sc.Operation = &OperationEnvelope{
		RequestID: "req-slow", Lane: LaneDefault, OperationType: "X",
		Actor: "a", SubmittedAt: "t", Payload: []byte("{}"),
	}
	// Parent deadline well above the 50ms budget so a broken (un-threaded) ctx
	// would visibly overrun rather than hang forever.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	_, err := NewStarlarkRunner(50*time.Millisecond, 0).Run(ctx, sc)
	elapsed := time.Since(start)

	se, ok := err.(*ScriptError)
	if !ok {
		t.Fatalf("want *ScriptError, got %T: %v", err, err)
	}
	if se.Code != "ScriptTimeout" {
		t.Fatalf("Code = %q, want ScriptTimeout", se.Code)
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("kv.Read took %s — the 50ms wall-budget ctx is not threaded into the read", elapsed)
	}
}

// TestKVRead_LogicallyDeletedReturnsDocWithFlag — a logically-deleted vertex
// (isDeleted=true, still a live KV envelope) returns a non-nil doc carrying the
// flag, NOT None. Mirrors how `state` surfaces deletes; the script — not the
// primitive — decides whether a deleted record counts as "present".
func TestKVRead_LogicallyDeletedReturnsDocWithFlag(t *testing.T) {
	sc := ScriptContext{KVReader: &fakeKVReader{docs: map[string]*VertexDoc{
		"vtx.task.del": {Key: "vtx.task.del", Class: "task", IsDeleted: true, Revision: 9, Data: map[string]interface{}{}},
	}}}
	res, err := runKVScript(t, sc, `
def execute(state, op):
    v = kv.Read("vtx.task.del")
    if v == None:
        return {"mutations": [], "events": [{"class": "none"}]}
    return {"mutations": [], "events": [{"class": "present", "data": {"del": v.isDeleted}}]}
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Events[0].Class != "present" || res.Events[0].Data["del"] != true {
		t.Fatalf("logical delete must surface as a present doc with isDeleted=true, got %+v", res.Events[0])
	}
}

// TestKVRead_NoReaderWiredErrors — an on-demand read (cache miss) with no reader
// wired is a script error, not a silent None. Guards against a misconfigured
// pipeline masquerading as "absent" and wrongly triggering a create.
func TestKVRead_NoReaderWiredErrors(t *testing.T) {
	sc := ScriptContext{} // no KVReader, no Hydrated
	_, err := runKVScript(t, sc, `
def execute(state, op):
    v = kv.Read("vtx.task.x")
    return {"mutations": [], "events": [{"class": "x"}]}
`)
	if err == nil {
		t.Fatalf("expected error for on-demand read with no reader wired")
	}
	se, ok := err.(*ScriptError)
	if !ok {
		t.Fatalf("want *ScriptError, got %T: %v", err, err)
	}
	if !strings.Contains(se.Message, "no Core KV reader") {
		t.Fatalf("error message = %q, want it to mention the missing reader", se.Message)
	}
}

// TestKVRead_ReaderErrorPropagates — a substrate-level read failure (not a
// not-found) surfaces as a ScriptError rather than being swallowed as None.
func TestKVRead_ReaderErrorPropagates(t *testing.T) {
	sc := ScriptContext{KVReader: &fakeKVReader{err: errors.New("boom-substrate")}}
	_, err := runKVScript(t, sc, `
def execute(state, op):
    v = kv.Read("vtx.task.x")
    return {"mutations": [], "events": [{"class": "x"}]}
`)
	if err == nil {
		t.Fatalf("expected the reader error to propagate")
	}
	if !strings.Contains(err.Error(), "boom-substrate") {
		t.Fatalf("error = %q, want it to carry the underlying cause", err.Error())
	}
}

// TestKVRead_ArgValidation — arity/type/empty-key misuse fails fast. A fake
// reader is supplied so failures are argument validation, not a missing reader.
func TestKVRead_ArgValidation(t *testing.T) {
	cases := []struct{ name, body string }{
		{"no args", `kv.Read()`},
		{"too many args", `kv.Read("a", "b")`},
		{"keyword arg", `kv.Read(key="a")`},
		{"non-string", `kv.Read(42)`},
		{"empty string", `kv.Read("")`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := ScriptContext{KVReader: &fakeKVReader{docs: map[string]*VertexDoc{}}}
			_, err := runKVScript(t, sc, "def execute(state, op):\n    "+tc.body+"\n    return {\"mutations\": [], \"events\": []}")
			if err == nil {
				t.Fatalf("%s: expected an error", tc.name)
			}
		})
	}
}

// TestConnKVReader_AgainstCoreKV exercises the production connKVReader adapter
// against a real embedded Core KV: absent → (nil,nil); a live vertex → a parsed
// doc with the read revision; a logically-deleted vertex → a non-nil doc with
// isDeleted=true (NOT not-found — Conn.KVGet returns logical deletes normally).
func TestConnKVReader_AgainstCoreKV(t *testing.T) {
	url := startEmbeddedNATS(t)
	ctx, conn := acConnect(t, url)
	r := connKVReader{conn: conn, bucket: testCoreBucket}

	// Absent → (nil, nil).
	doc, err := r.ReadVertex(ctx, "vtx.task.nope")
	if err != nil || doc != nil {
		t.Fatalf("absent: got (doc=%v, err=%v), want (nil, nil)", doc, err)
	}

	// Live vertex → parsed doc, revision threaded.
	rev, err := conn.KVCreate(ctx, testCoreBucket, "vtx.task.live",
		[]byte(`{"class":"task","isDeleted":false,"data":{"status":"open"}}`))
	if err != nil {
		t.Fatalf("seed live: %v", err)
	}
	doc, err = r.ReadVertex(ctx, "vtx.task.live")
	if err != nil || doc == nil {
		t.Fatalf("live: got (doc=%v, err=%v), want a doc", doc, err)
	}
	if doc.Class != "task" || doc.IsDeleted || doc.Data["status"] != "open" || doc.Revision != rev {
		t.Fatalf("live doc mismatch: %+v (rev want %d)", doc, rev)
	}

	// Logically-deleted vertex → non-nil doc with isDeleted=true.
	if _, err := conn.KVCreate(ctx, testCoreBucket, "vtx.task.del",
		[]byte(`{"class":"task","isDeleted":true,"data":{}}`)); err != nil {
		t.Fatalf("seed deleted: %v", err)
	}
	doc, err = r.ReadVertex(ctx, "vtx.task.del")
	if err != nil || doc == nil {
		t.Fatalf("logically-deleted: got (doc=%v, err=%v), want a non-nil doc", doc, err)
	}
	if !doc.IsDeleted {
		t.Fatalf("logically-deleted: isDeleted = false, want true (must surface, not nil)")
	}
}
