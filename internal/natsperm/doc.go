// Package natsperm holds the conformance proof for the Lattice NATS
// transport-authorization permission matrix (the NATS account-level write
// restriction, Path A).
//
// It is intentionally code-free: the matrix lives in deploy/nats-server.conf
// (rendered from deploy/gen-dev-nkeys) and the per-component dev NKey seeds in
// deploy/nkeys. The package's tests load that exact production config + seeds
// into an embedded JetStream server and assert the load-bearing invariant —
// only the processor may write Core KV and only refractor may write
// capability-kv / the lens targets — end-to-end, before enforcement is ever
// wired into the live stack.
package natsperm
