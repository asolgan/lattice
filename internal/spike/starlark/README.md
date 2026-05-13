# Story 1.2 — Starlark Execution Spike: Findings Report

**Date:** 2026-05-13  
**Library:** `go.starlark.net v0.0.0-20260326113308-fadfc96def35`  
**Platform:** darwin/arm64 (Apple M-series, development machine)  
**Go:** 1.26.1  
**Model:** Claude Sonnet (claude-sonnet-4-6)

---

## GO / NO-GO RECOMMENDATION

**GO — with one known stub to complete in Story 1.6.**

All three acceptance criteria were met on verified, running code:

- Sandbox correctness: all four forbidden operations (HTTP, filesystem, env, time) are rejected by the interpreter at parse or execution time with clear errors. No forbidden operation succeeded.
- API ergonomics: the `ScriptContext → RunScript → ScriptResult` pipeline works end-to-end. The realistic CreateIdentity script executed cleanly and returned a Contract #3-conforming `{mutations, events}` dict. Story 1.6's implementing engineer has a usable prototype to wire into the Processor commit path.
- Performance: 1,000 sequential invocations (full compile + execute, no caching) produced mean=69µs, p95=126µs, p99=210µs on a dev Mac — roughly 500x better than the 100ms p99 NFR-P4 budget. Even accounting for the Mac-to-cloud gap, the order-of-magnitude case for Starlark is strong.

One known gap surfaces from the spike: Contract #3 §3.6 specifies `nanoid.new()` and `nanoid.short()` as Starlark stdlib builtins. These are not provided by `go.starlark.net` and must be implemented as custom Go-backed Starlark builtins in Story 1.6. The spike uses a deterministic placeholder (`op.requestId`) for the NanoID. This is expected — the brief and architecture both defer nanoid stdlib to Story 1.6.

---

## Area 1: Sandbox Correctness

### Background for readers new to Starlark

Starlark's sandbox is **not a blocklist** — it is an empty room. The interpreter knows only what you explicitly put in its global environment. By default, a Starlark script can do arithmetic, string manipulation, list/dict construction, conditionals, function definitions, and nothing else. There is no `import`, no `open`, no `os`, no `http`, no `time`, no `random`, no `socket`. None of these need to be blocked; they were never present.

The Go-side sandbox configuration in `runner.go` is the complete list of what scripts CAN do:

```go
globals := starlark.StringDict{
    "state": vertexMapToStarlark(ctx.Hydrated),  // pre-hydrated vertex dict
    "op":    operationEnvelopeToStarlark(ctx.Operation),  // operation envelope struct
    "ddl":   ddlMapToStarlark(ctx.DDLLookup),    // DDL meta-vertex dict
}
```

That is the entire sandbox surface. Three globals. No module loader. No I/O. No clock. This is not a configuration option that could be accidentally enabled — it is the complete set.

### Test results (verbatim error messages)

**Test 1: External HTTP call via load()**

Starlark's `load` statement is the module import mechanism. The `go.starlark.net` `Thread` struct has an optional `Load` function field. If left nil (the default), any `load(...)` call in a script raises:

```
starlark exec error: load not implemented by this application
```

The spike does not set `thread.Load`. No HTTP module can be loaded.

**Test 2: Filesystem read via open()**

The Starlark compiler itself rejects references to undefined names before execution. A script that calls `open("/etc/passwd")` is rejected at compile time:

```
starlark compile error: <script>:3:9: undefined: open
```

This is caught by the compiler — the script never executes.

**Test 3: os.Getenv equivalent**

Same compile-time rejection. `os` is not in the global environment:

```
starlark compile error: <script>:3:14: undefined: os
```

**Test 4: Non-deterministic time call**

Same compile-time rejection. `time` is not in the global environment:

```
starlark compile error: <script>:3:11: undefined: time
```

### Sandbox configuration that produces these results

The complete Go-side sandbox configuration (from `runner.go`) is:

```go
// Build the globals available to the script — this is the COMPLETE set.
globals := starlarklib.StringDict{
    "state": vertexMapToStarlark(ctx.Hydrated),
    "op":    operationEnvelopeToStarlark(ctx.Operation),
    "ddl":   ddlMapToStarlark(ctx.DDLLookup),
}

// Compile the script (catches undefined names, syntax errors)
_, prog, err := starlarklib.SourceProgram("<script>", src, globals.Has)

// Execute the compiled program
thread := &starlarklib.Thread{Name: "processor"}
// Note: thread.Load is NOT set — any load() call fails with
// "load not implemented by this application"
defined, err := prog.Init(thread, globals)
```

No additional configuration is required. The sandbox is correct by construction.

### Contract implication

NFR-S3 requires that scripts cannot reach outside the sandbox. This spike confirms `go.starlark.net` meets NFR-S3 requirements by default — the default configuration provides no I/O surface at all.

---

## Area 2: API Ergonomics

### ScriptContext prototype

The `ScriptContext` struct (in `types.go`) is the data contract between the Processor's commit step 4 (JIT Hydrate) and commit step 5 (Execute):

```go
// ScriptContext holds everything the Processor makes available to a Starlark script
// during commit step 5 (Execute).
type ScriptContext struct {
    Operation  OperationEnvelope       // per Contract #2 — available as `op` global
    Hydrated   map[string]VertexDoc    // JIT-hydrated vertices — available as `state` global
    DDLLookup  map[string]MetaVertex   // class definitions — available as `ddl` global
}

// ScriptResult is the parsed return value of a Starlark script execution.
// Conforms to Contract #3 §3.1: {"mutations": [...], "events": [...]}
type ScriptResult struct {
    Mutations []MutationOp  // per Contract #3 §3.2
    Events    []EventSpec   // per Contract #3 §3.4
}
```

### Realistic example script (CreateIdentity)

This script covers the three criteria from AC #3: one vertex hydration read, one conditional branch, one mutation proposal.

```python
def execute(state, op):
    """
    CreateIdentity handler — realistic example for spike validation.

    Inputs (from ScriptContext):
      state: dict of Core KV key -> vertex struct (pre-fetched at step 4)
      op:    OperationEnvelope struct (requestId, lane, operationType, actor, payload)

    Returns: {"mutations": [...], "events": [...]} per Contract #3 §3.1
    """

    # Read actor from hydrated state to demonstrate JIT hydration access.
    actor_doc = state.get(op.actor)
    if actor_doc == None:
        fail("actor not found in hydrated state: " + op.actor)

    # Read payload fields.
    name = op.payload.name
    email = op.payload.email

    # Conditional branch: validate email is non-empty.
    if len(email) == 0:
        fail("payload.email must not be empty")

    # Build the new identity key. In production this uses nanoid.new().
    # The spike uses a deterministic key derived from requestId for test stability.
    identity_id = op.requestId  # spike-only substitution; Story 1.6 uses nanoid.new()
    identity_key = "vtx.identity." + identity_id
    email_aspect_key = identity_key + ".email"

    # Declare state transitions per Contract #3 §3.2.
    mutations = [
        {
            "op": "create",
            "key": identity_key,
            "document": {
                "class": "identity",
                "isDeleted": False,
                "data": {"name": name}
            }
        },
        {
            "op": "create",
            "key": email_aspect_key,
            "document": {
                "class": "email",
                "vertexKey": identity_key,
                "localName": "email",
                "isDeleted": False,
                "data": {"value": email, "verified": False}
            }
        }
    ]

    # Declare business event per Contract #3 §3.4.
    events = [
        {
            "class": "identityCreated",
            "data": {
                "identityKey": identity_key,
                "createdBy": op.actor
            }
        }
    ]

    return {"mutations": mutations, "events": events}
```

### Observed output

When executed against a realistic `ScriptContext` (actor pre-hydrated, payload with name + email), the script produced:

```
Mutations produced: 2
  [0] op=create key=vtx.identity.Rm7q3pntwzkfbcxv5p9j
       class=identity isDeleted=false
  [1] op=create key=vtx.identity.Rm7q3pntwzkfbcxv5p9j.email
       class=email isDeleted=false

Events produced: 1
  [0] class=identityCreated data=map[createdBy:vtx.identity.St6mP3qBn4rT8wYxK7Vc identityKey:vtx.identity.Rm7q3pntwzkfbcxv5p9j]
```

Both mutations conform to Contract #3 §3.2 (op, key, document with class, isDeleted). The event conforms to Contract #3 §3.4 (class, data). Validation against the contract shape passed.

### Handoff readiness for Story 1.6

Story 1.6's implementing engineer can use the `ScriptContext`, `RunScript`, and `ScriptResult` types as-is. The remaining work for Story 1.6 that this spike does not implement:

1. **nanoid stdlib binding** — implement `nanoid.new()` and `nanoid.short()` as custom Go-backed Starlark builtins, seeded with `op.requestId` for replay determinism (per Contract #3 §3.6 and §3.8).
2. **Execution timeout** — set `thread.SetMaxSteps()` or use a context-deadline mechanism to enforce NFR-P4's execution time budget (StarlarkExecutionTimeout error code).
3. **Program caching** — compile the script source once and cache the `*starlark.Program`; the spike shows compilation is fast (~50µs), but caching is still the right production pattern.
4. **Wiring into commit path** — integrate `RunScript` at commit step 5 using the Processor's hydrated working set from step 4.

### Contract observation: nanoid stdlib

Contract #3 §3.6 specifies that `nanoid.new()` and `nanoid.short()` are provided to scripts by the Starlark stdlib. These are not provided by `go.starlark.net` itself — they are Lattice-specific custom builtins that Story 1.6 must implement via `starlark.NewBuiltin`. This is expected (the architecture explicitly defers stdlib design to Stream 1). No contract amendment is needed; this is a Story 1.6 implementation task, not a contradiction.

---

## Area 3: Order-of-Magnitude Performance

### Benchmark configuration

- **Script:** RealisticExampleScript (CreateIdentity) — one vertex read, one conditional branch, two mutations, one event
- **Mode:** Full compile + execute per iteration (worst case — no program caching)
- **Iterations:** 1,000 sequential invocations
- **Platform:** darwin/arm64 (Apple M-series development machine)
- **Go version:** 1.26.1

### Results

| Metric | Value |
|--------|-------|
| Total (1000 iters) | 69.5ms |
| Mean | 69µs |
| p95 | 126µs |
| p99 | 210µs |
| NFR-P4 budget (p99) | 100ms |
| Budget headroom | ~476x |

### Interpretation

The p99 of 210µs is roughly **476 times faster** than the 100ms NFR-P4 budget, measured on a development Mac in worst-case mode (full recompilation on every call). In a production Processor with program caching (compile once, execute many times), compilation cost is amortized and per-invocation latency drops further — likely to the 10-30µs range based on the benchmark's compile/execute split.

The Mac-to-cloud performance gap is typically 2-5x for CPU-bound work in Go. Even assuming 10x degradation on cloud hardware, p99 would land around 2ms — still 50x under budget.

**The `< 100ms p99` NFR-P4 target is achievable in principle with high confidence.**

No architecture adjustment is required. (The AC requires proposing mitigations only if p95 exceeds 100ms. Observed p95=126µs.)

### Production performance notes for Story 1.6

The performance case can be further strengthened by:
- **Program caching**: `starlark.SourceProgram()` compiles source to a `*Program`. Cache by script content hash (or script name + version). Re-execution of the same script skips compilation entirely.
- **Thread pooling**: `starlark.Thread` is lightweight. A pool of pre-allocated threads avoids garbage collection pressure under sustained load.
- These are optimizations, not requirements — the baseline (no caching) already has 476x budget headroom.

---

## Open Questions for Winston / Andrew

1. **nanoid stdlib API**: Contract #3 §3.6 says `nanoid.short()` returns an 8-char NanoID for "display codes." Should the Starlark stdlib expose `nanoid.short()` to scripts, or is it internal-only? If exposed, scripts could generate display codes themselves, which may or may not be desirable from a platform control standpoint.

2. **Script execution timeout enforcement**: The architecture specifies `StarlarkExecutionTimeout` as a step 5 error code (NFR-P4). `go.starlark.net` supports execution limits via `thread.SetMaxSteps(n)` (step count budget) rather than wall-clock time. Wall-clock enforcement requires running the script in a goroutine with a context timeout and cancellation. Story 1.6 should clarify which mechanism is preferred.

3. **Script loading/discovery**: The architecture mentions a `scripts/` directory (see `lattice-architecture.md` directory tree). Story 1.6 will need a script loader. The spike does not cover script discovery or hot-reload. This is a Story 1.6 concern but should be scoped before implementation begins.

---

## Files Produced

| File | Purpose |
|------|---------|
| `internal/spike/starlark/types.go` | Go type definitions: ScriptContext, ScriptResult, OperationEnvelope, VertexDoc, MutationOp, EventSpec, MetaVertex |
| `internal/spike/starlark/runner.go` | RunScript implementation; Starlark↔Go value conversion; sandbox setup |
| `internal/spike/starlark/sandbox_correctness.go` | Four forbidden-operation tests + two permitted-operation tests |
| `internal/spike/starlark/api_ergonomics.go` | RealisticExampleScript; buildAPIErgonomicsContext; validateScriptResult |
| `internal/spike/starlark/perf.go` | 1,000-iteration benchmark; mean/p95/p99 computation |
| `internal/spike/starlark/cmd/main.go` | Runnable harness entry point |

To run the spike:

```bash
go run ./internal/spike/starlark/cmd/
```

Expected output: all tests pass, clean exit.
