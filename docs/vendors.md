# Vendors — authoritative external sources

The canonical record of Lattice's load-bearing third-party dependencies, **where their authoritative
behavior is documented**, and **which version we pin**. Referenced from `CLAUDE.md` ("Authoritative
external sources").

**Rule.** When you need the authoritative behavior of a vendored dependency — semantics, version-gated
features, edge cases — cite the **upstream project's own docs / source / ADRs, version-matched to our
pinned version**. Never rely on a secondary blog or an unqualified web search. Web search is a last
resort and must be **corroborated against the upstream** (the project's docs site or its source/ADRs at
the pinned version) before you act on it.

Add a row when a new vendor's behavior becomes load-bearing.

| Vendor | Role in Lattice | Authoritative sources | Our pin |
|--------|-----------------|-----------------------|---------|
| **NATS / JetStream** | The **substrate**: KV (Core KV, Health KV, operational buckets), JetStream streams (`core-operations` / `core-events` / `core-schedules`), atomic batch (single-stream multi-key commit), per-key message TTL, message scheduling (`@at` / `@every`). | <https://nats.io> (docs) · <https://github.com/nats-io> (source). Design semantics live in the **ADRs** at `nats-io/nats-architecture-and-design` — e.g. **ADR-48** (per-key message TTL), **ADR-51** (message scheduling). Match every claim to **our pinned version's** docs/source. | **NATS 2.14** — `go.mod` `github.com/nats-io/nats-server/v2 v2.14.0`, client `github.com/nats-io/nats.go`; `docker-compose.yml` `nats:2.14-alpine`. |
| **golang-jwt/jwt** | Actor authentication, both read (D1) and write (Gateway) paths: `internal/gateway/auth` verifies IdP-signed JWTs (signature + standard-claim validation) for the read boundary AND `cmd/gateway`'s `POST /v1/operations` strip-and-stamp translator — one Verifier, two callers. Lattice holds only the IdP's **public** key (asymmetric RS*/ES* verify; never signs). | <https://github.com/golang-jwt/jwt> — upstream source + `MIGRATION_GUIDE.md`. Security-critical semantics to match at our pin: `Parser.WithValidMethods` (the alg allow-list — the alg-confusion/`none` guard) and the v5 error tree (`ErrTokenMalformed` / `ErrTokenSignatureInvalid` / `ErrTokenUnverifiable`, joined via `%w`). | **v5.2.1** — `go.mod` `github.com/golang-jwt/jwt/v5 v5.2.1`. |
| **External IdP (OIDC)** | The **actor signing authority** Lattice never owns (architecture F3): the deployment's OIDC provider (Auth0/Keycloak/Google/…) issues the JWTs `internal/gateway/auth` verifies. Load-bearing for **Contract #11** — the `(iss, sub)` pair is the input to the actor-key derivation (`opaque` mode), and OIDC's per-issuer `sub` uniqueness scope is *why* `iss` must be a derivation input + pinned per source. | **OIDC Core 1.0** — <https://openid.net/specs/openid-connect-core-1_0.html> (`sub`: *"locally unique and never reassigned… within the Issuer"*, ≤255 ASCII; §8 public vs pairwise subject types). Per-vendor: Google — <https://developers.google.com/identity/openid-connect/openid-connect> (*"use `sub`, never `email`"*; `iss` is **`https://accounts.google.com` OR `accounts.google.com`** — two forms, pin one). Match claims to the deployment's actual IdP. | No library pin — external integration dependency. Lattice holds only the IdP's **public** JWKS; the accepted JWT profile is frozen in Contract #11 §11.2. |
| **dop251/goja** | **Test-only** pure-Go ECMAScript interpreter running Loupe's pure FE logic (`cmd/loupe/web/js/logic/*.js`) under `go test ./cmd/loupe/...` (`web_logic_test.go`, strip-export load) — the FE regression net with no Node toolchain, no build step. | <https://github.com/dop251/goja> — README (capability list: ES5.1 + most-of-ES6, **no ES-module support**, no host objects; the reason logic files stay ES6-conservative and are loaded via the strip-export transform). | `go.mod` `github.com/dop251/goja` (pseudo-version; test-only dependency). |
| **antlr4-go/antlr** | The **only rule engine's** parser runtime: Refractor's full openCypher engine (`internal/refractor/ruleengine/full/`) lexes + parses lens cypher via the generated `cypher.CypherLexer` / `cypher.CypherParser`, walked into a `*CompiledRule`. Load-bearing — every lens definition is parsed through it. | <https://github.com/antlr4-go/antlr> — the official ANTLR4 Go runtime (source + `README`), version-matched to the pin. The **grammar** is vendored from <https://github.com/jtejido/go-opencypher> (openCypher `.g4` → generated `full/cypher/`); regenerate against that grammar, not by hand. | **v4.13.1** — `go.mod` `github.com/antlr4-go/antlr/v4 v4.13.1`. |
| **go.etcd.io/bbolt** | The Edge node's embedded **Local VAL Store** (`internal/edge/store`, edge-lattice-full-design.md §3.1): a single-file, transactional, pure-Go KV mirroring Core KV's key shape on-device. No cgo (unlike `mattn/go-sqlite3`) — the reason it's the Go-reference choice; SQLite/IndexedDB only enter with the browser node (EDGE.5). | <https://github.com/etcd-io/bbolt> — upstream source + README (single-writer/many-reader MVCC semantics, `Update`/`View` transaction API). | **v1.5.0** — `go.mod` `go.etcd.io/bbolt v1.5.0`. |

## Version-gated NATS features (why the pin matters)

Feature availability is version-gated; cite the version that introduced a feature and confirm it is
≤ our pin:

| Feature | Introduced | Notes |
|---------|-----------|-------|
| Per-key message TTL (ADR-48) | NATS 2.11 | Idempotency-tracker 24h TTL (Contract #4 §4.3). |
| Atomic batch (single-stream multi-key, revision-conditioned) | NATS 2.12 | The Processor step-8 commit; the reason all Core KV is one stream. |
| `@at` one-shot message schedules (ADR-51) | NATS 2.12 | The temporal lane's freshness timers + the bridge poll/timeout lane (Contract #10 §10.4). |
| `@every` / 6-field cron / timezone message schedules (ADR-51) | NATS 2.14 | Recurring schedules — the cron-killer (Contract #10 §10.4 "Recurring schedules"). |

**Platform floor: NATS 2.14** (the highest of the above). Pinned in `go.mod` + `docker-compose.yml`;
recorded in Contract #4 §4.3. Do not assume a lower floor — `@every`/cron need 2.14 and the platform
provides it.
