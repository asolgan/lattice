// Package natsperm holds the Lattice NATS transport-authorization permission
// matrix (the NATS account-level write restriction, Path A) and its
// conformance proof.
//
// Matrix + platform-bucket-registry-derived owner-allows/denies live here
// (matrix.go); deploy/gen-dev-nkeys is a thin renderer that mints per-
// component NKey seeds and writes deploy/nats-server.conf from Matrix. The
// package's tests load that exact production config + seeds into an embedded
// JetStream server and assert the load-bearing invariant — only the
// processor may write Core KV and only refractor may write capability-kv /
// the lens targets — end-to-end, before enforcement is ever wired into the
// live stack (natsperm-matrix-hygiene-design.md).
package natsperm
