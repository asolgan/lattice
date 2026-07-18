# FacetSwiftUISpike

Second-renderer spike for the Facet edge showcase app
(`_bmad-output/implementation-artifacts/edge-showcase-app-design.md` §7 Fire 5): a native SwiftUI
client that hydrates from the exact same `manifest.*` feed the PWA renderer (`cmd/facet/web`) renders
from, over `cmd/facet`'s already-shipped browser-facing HTTP+SSE surface. No NATS/crypto client of its
own — the Go host owns auth and the wire connection; this is proof that any renderer speaking HTTP+SSE
can hydrate from it, not just a browser.

## Honest scope caveat

This machine has Xcode Command Line Tools but no full Xcode.app and no iOS Simulator SDK
(`xcrun --sdk iphonesimulator --show-sdk-path` fails). The package therefore targets **macOS 13**, not
iOS — the closest buildable/runnable proxy available here. It uses the identical SwiftUI framework and
declarative paradigm an iOS build would, and the manifest-consuming code
(`Sources/FacetManifestKit`) has zero platform-specific API in it, so the actual claim under test —
that the manifest/descriptor vocabulary is renderer-neutral, not that this exact bundle runs on an
iPhone — is proven. A literal iOS build (device or simulator) needs a machine with full Xcode installed
and is unstarted; that is the actual remaining gap before the design's FORK-1 freeze trigger is
completely satisfied, not a re-architecture.

## Layout

- `Sources/FacetManifestKit` — platform-agnostic: `JSONValue` (loosely-typed JSON, mirroring the Go
  host's `json.RawMessage` posture, encodable as well as decodable so it also builds write-request
  bodies), `ManifestFrame` (mirrors `cmd/facet/feed.go`'s `frame` struct, including the `outbox`
  write-lifecycle field), `SSEDecoder` (pure line-based SSE parser, unit-tested), `FeedClient`
  (dev-login + live SSE stream + `enqueue` writes against a running `cmd/facet` host).
- `Sources/FacetSwiftUISpike` — the SwiftUI app: `ManifestStore` (last-write-wins reducer over the
  frame stream, the same reducer shape `app.js`'s manifest handler uses; also owns the attached
  `FeedClient` and the live Outbox lifecycle dict), `ContentView` (renders Services/Catalog/Tasks/My
  Instances/Outbox sections straight off manifest row fields — no manifest-specific text anywhere in
  the view code; each Catalog row has an "Enqueue" button), `FacetSwiftUISpikeApp` (entry point).
- `Tests/FacetManifestKitTests` — XCTest coverage of `SSEDecoder`/`ManifestFrame`/`JSONValue`. **Could
  not run in this sandbox** (no XCTest module without full Xcode — see caveat above); will run under a
  normal Xcode toolchain. The same assertions were verified live via a throwaway `swift run` smoke
  check during this fire (not checked in) — see the Fire 5 §7 build note in the design doc for the
  transcript.

## Running it

```
cd clients/facet-swiftui-spike
swift build
FACET_BASE_URL=http://127.0.0.1:7810 FACET_IDENTITY_ID=<20-char-NanoID> swift run FacetSwiftUISpike
```

The identity id is a `make seed-showcase` tenant (`FACET_TENANT1_NANOID`/`FACET_TENANT2_NANOID` from its
output) — `up-facet` runs with `FACET_DEV_AUTH=1` and no boot identity, so a session must be established
via `POST /api/dev-login` first; `FeedClient.devLogin` does this before opening the feed.

## Write path

`FeedClient.enqueue(operationType:payload:reads:optionalReads:authContext:touchedKey:)` POSTs to
`/api/enqueue`, mirroring `cmd/facet/server.go`'s `enqueueRequest` field-for-field. Like the PWA, this
call is fire-and-forget: it returns a `requestId` immediately, and the write's outcome
(queued/submitting/confirmed/rejected) arrives back over the same SSE stream `stream()` already
consumes, as an `outbox` frame — `ManifestStore` tracks it in a `[String: OutboxEntry]` and
`ContentView` renders it under "Outbox". The Catalog section's "Enqueue" button submits an empty
(`{}`) payload — it proves the SwiftUI renderer can drive a real write through the same envelope path
a filled-in form would use, not that every op succeeds with no fields (an op with required
`inputSchema` fields comes back `rejected` with the Starlark-level `InvalidArgument`, which is itself
part of the round trip this button proves). Building the descriptor-form renderer that resolves
`inputSchema`/`dispatch.reads`/`dispatch.contextParams` into a real filled-in form — `app.js`'s
`renderOpForm` on the PWA side — is a separate, larger increment, not done here.

Live-verified (throwaway `swift run` smoke check during Fire 5 Inc 2, not checked in — see the design
doc's §7 build note for the transcript): `OpenTab{leaseAppKey}` against a real fixture lease
(`vtx.leaseapp.Z8ebXzStgUGerUpqeHEF`, the one §7.9's café self-service proof minted) with `reads`
declaring the lease vertex and `optionalReads` declaring the `cafeOpenTab` guard key + the
`applicationFor` link key round-tripped `queued → submitting → confirmed` over the live SSE stream, and
the new tab was independently confirmed via `cafe-app`'s own `/api/tabs` read API — a write initiated
by this Swift client actually reached the Gateway/Processor and landed in Core KV, not just a
client-side illusion.

## A note on `URLSession.bytes(for:)`

`FeedClient.stream()` uses a delegate-based `URLSessionDataTask`, not the newer
`URLSession.bytes(for:)` async-sequence API. `bytes(for:)` was tried first and, live against a running
`cmd/facet` host, never yielded a single line for this endpoint — a long-lived SSE connection that never
closes and pings every 20s (`curl` streamed it instantly; `AsyncBytes.lines` sat empty past a 10s
timeout in the same process). The delegate's `urlSession(_:dataTask:didReceive:)` callback fires as
bytes actually arrive and does not have this problem. Worth knowing if a future increment touches this
file.
