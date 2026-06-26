package processor

import (
	"context"
	"errors"

	starlarklib "go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/asolgan/lattice/internal/substrate"
)

// kvModule returns the Starlark `kv` global exposing a single builtin,
// `kv.Read(key)` — the Contract #2 §2.5 lazy on-demand Core KV read.
//
// §2.5 makes `contextHint.reads` a pre-fetch *optimisation, not a gate*:
//   - a key declared in contextHint.reads is hydrated at step 4 and served here
//     from the cache (state) with NO Core KV round-trip. NOTE this means a
//     declared key is read at the step-4 OCC snapshot; kv.Read CANNOT force a
//     fresher re-read of an already-hydrated key (and should not — echoing the
//     snapshot revision as expectedRevision is what makes the commit's OCC
//     check sound);
//   - a key NOT declared falls through to a single on-demand GET via sc.KVReader
//     ("incur latency", §2.5).
//
// Return value:
//   - present (live OR logically-deleted) → a struct projection identical in
//     shape to a `state` entry (key, class, isDeleted, data, revision, and the
//     aspect-only vertexKey/localName when set), so scripts read it the same way
//     they read `state[key]`. A logically-deleted vertex (isDeleted=true) is a
//     live KV envelope and reads as a PRESENT doc carrying the flag — NOT None;
//   - absent / hard-tombstoned (NATS delete/purge/TTL-expiry) → None.
//
// A script branching on existence must therefore test `v == None or
// v.isDeleted`, not just `v == None`, to treat a logically-deleted record as
// "needs (re)creating".
//
// This unlocks the read-before-create idempotency pattern that `contextHint` and
// `createIfAbsent` cannot express: a declared-but-absent contextHint key fails
// hydration *fatally* (HydrationMiss) before the script runs, so it cannot say
// "read this, tolerate absence." kv.Read tolerates absence (→ None), letting the
// script decide mutations AND events coherently in one branch.
//
// DETERMINISM: this is the ONE non-pure builtin. nanoid/crypto/time/json are all
// deterministic so a replayed (at-least-once) operation reproduces byte-identical
// output. kv.Read deliberately breaks that — it reads LIVE state, so two runs of
// the same requestId can observe different Core KV and branch differently. That
// is the POINT (Contract #10 §10.3 / design §4.3): the consumer reads current
// state to decide create-vs-no-op, and the Processor — not replay determinism —
// is the idempotency authority. The deterministic id + the CreateOnly backstop
// at commit (step 8) resolve the residual publish→commit race. Do not assume
// kv.Read is replay-stable.
//
// Latency note: each on-demand kv.Read is a NATS round-trip. getExecCtx returns
// the per-invocation wall-budget context, so a slow read counts against the
// script budget and surfaces as ScriptTimeout if it overruns. It is an
// intentional opt-in for the idempotency-read pattern — NOT a general
// scan/read-model hook (read models are lenses, P5).
func kvModule(getExecCtx func() context.Context, sc ScriptContext) *starlarkstruct.Struct {
	readFn := starlarklib.NewBuiltin("Read", func(_ *starlarklib.Thread, _ *starlarklib.Builtin, args starlarklib.Tuple, kwargs []starlarklib.Tuple) (starlarklib.Value, error) {
		if len(args) != 1 || len(kwargs) != 0 {
			return nil, errBuiltin("kv.Read(key) takes exactly 1 positional argument")
		}
		keyStr, ok := args[0].(starlarklib.String)
		if !ok {
			return nil, errBuiltin("kv.Read: argument must be a string, got " + args[0].Type())
		}
		key := string(keyStr)
		if key == "" {
			return nil, errBuiltin("kv.Read: key must be non-empty")
		}

		// Cache-first (§2.5). A hydrated entry is, by construction, always a
		// successful read: a contextHint key that was absent in Core KV fails
		// hydration (HydrationMiss) before execution begins, so reaching the
		// script with the key in sc.Hydrated guarantees it was present.
		if doc, ok := sc.Hydrated[key]; ok {
			return vertexDocToStarlark(doc), nil
		}

		// Lazy on-demand read for a key not declared in contextHint.reads.
		if sc.KVReader == nil {
			return nil, errBuiltin("kv.Read: no Core KV reader wired for on-demand read of " + key)
		}
		doc, err := sc.KVReader.ReadVertex(getExecCtx(), key)
		if err != nil {
			return nil, errBuiltin("kv.Read: " + err.Error())
		}
		if doc == nil {
			// Absent or hard-tombstoned — the script branches on None.
			return starlarklib.None, nil
		}
		return vertexDocToStarlark(*doc), nil
	})

	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlarklib.StringDict{
		"Read": readFn,
	})
}

// connKVReader adapts a substrate.Conn + Core bucket to ScriptKVReader. It is
// the production backing for kv.Read's on-demand path, wired by the Hydrator.
//
// A not-found (absent / hard-tombstoned) maps to (nil, nil) so kv.Read yields
// None; a logically-deleted vertex (isDeleted=true envelope still live, per
// Conn.KVGet) returns a non-nil doc carrying isDeleted so the script decides;
// every other error propagates. Single-key GET only — never a prefix scan.
type connKVReader struct {
	conn   *substrate.Conn
	bucket string
}

// ReadVertex implements ScriptKVReader.
func (r connKVReader) ReadVertex(ctx context.Context, key string) (*VertexDoc, error) {
	entry, err := r.conn.KVGet(ctx, r.bucket, key)
	if err != nil {
		if errors.Is(err, substrate.ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}
	doc, err := parseVertexDoc(entry.Value, key)
	if err != nil {
		return nil, err
	}
	doc.Revision = entry.Revision
	return &doc, nil
}
