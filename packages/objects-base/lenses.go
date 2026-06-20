package objectsbase

import "github.com/asolgan/lattice/internal/pkgmgr"

// Lenses returns the package's Lens declarations: the single `objectLiveness`
// actorAggregate convergence lens (Contract #10 §10.2) — the v1b GC's orphan
// DETECTION. It anchors on each object vertex and projects a `missing_owner`
// gap flag (= zero live links) + `violating` + the object's `linkEpoch` (the
// link-set version the reclaim op CASes against). Weaver's `objectLiveness`
// target dispatches `directOp(TombstoneObject)` over the orphaned rows.
//
// One row per anchor (the §0.C guard fails closed on a multi-row anchor): the
// `OPTIONAL MATCH (o)-[r]->(owner)` fan is collapsed by `count(owner)`, so any
// number of links produces exactly one row. Dead-target awareness is free —
// a tombstoned owner does not bind (the full engine's `fetchNode` returns nil
// for a soft-deleted vertex and the traversal skips it), so `count(owner)`
// excludes it; a dangling link to a dead owner reprojects the object as
// orphaned without any extra consumer (§20 C-b). The object reprojects on any
// link create/tombstone via the actorAggregate adjacency fan-out (AnchorType
// `object`).
func Lenses() []pkgmgr.LensSpec {
	return []pkgmgr.LensSpec{
		{
			CanonicalName:  "objectLiveness",
			Class:          "meta.lens",
			Adapter:        "nats-kv",
			Bucket:         "weaver-targets",
			Engine:         "full",
			Spec:           objectLivenessSpec,
			ProjectionKind: "actorAggregate",
			Output: &pkgmgr.OutputDescriptorSpec{
				AnchorType:       "object",
				OutputKeyPattern: "objectLiveness.{actorSuffix}",
				BodyColumns:      []string{"violating", "missing_owner", "entityKey", "linkEpoch", "storeName"},
				EmptyBehavior:    "delete",
				KeyColumn:        "entityId",
			},
		},
	}
}

// objectLivenessSpec is the one-row-per-anchor orphan-detection cypher.
//
//   - `count(owner.key)` counts only BOUND neighbours, so it is the
//     dead-target-aware live-link count: a tombstoned owner (nil from fetchNode)
//     and a tombstoned link (absent from adjacency via removeEdge) both drop
//     out. It counts the owner's KEY, not the node, because an unbound OPTIONAL
//     node is a non-Go-nil null sentinel that `count` would tally as 1 — counting
//     the scalar `.key` (Go-nil when unbound) is the lease-lens idiom. `count(r)`
//     would be WRONG — an owner-only tombstone leaves the link's adjacency edge
//     in place, so counting edges would keep a dead-target object alive.
//   - The single OPTIONAL MATCH carries NO filtering WHERE, so a zero-link object
//     is null-restored to one row with owner=null (count 0 → orphaned) rather
//     than dropped — the documented full-engine grouping behaviour.
//   - `linkEpoch` is the object's root-data link-set version (`o.data.linkEpoch`,
//     a vertex root-data field the full engine resolves directly); Weaver
//     templates it into the reclaim op's expectedEpoch so a concurrent re-link
//     (which bumps the epoch) aborts the tombstone (§20).
//   - `= null` is not used here, but per the lease-lens convention any null test
//     would be `= null`, never the unsupported `IS NULL`.
const objectLivenessSpec = `
MATCH (o:object {key: $actorKey})
OPTIONAL MATCH (o)-[r]->(owner)
WITH
  o.key AS entityKey,
  o.data.linkEpoch AS linkEpoch,
  o.content.data.storeName AS storeName,
  count(owner.key) AS liveOwners
RETURN
  entityKey AS actorKey,
  entityKey,
  linkEpoch,
  storeName,
  (liveOwners = 0) AS missing_owner,
  (liveOwners = 0) AS violating
`
