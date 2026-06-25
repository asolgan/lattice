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
		{
			CanonicalName:  "objectAttachments",
			Class:          "meta.lens",
			Adapter:        "nats-kv",
			Bucket:         "weaver-targets",
			Engine:         "full",
			Spec:           objectAttachmentsSpec,
			ProjectionKind: "actorAggregate",
			Output: &pkgmgr.OutputDescriptorSpec{
				AnchorType:       "object",
				OutputKeyPattern: "objectAttachments.{actorSuffix}",
				BodyColumns:      []string{"entityKey", "storeName", "contentType", "size", "owners"},
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

// objectAttachmentsSpec is the per-object display read-model the vertical apps
// (LoftSpace's Documents tab) read in place of Core KV (P5): given an oid it
// resolves the byte-plane metadata (storeName / contentType / size) to stream a
// document, and given an owner it lists that owner's attached documents by
// filtering `owners`.
//
//   - One row per anchor object (the §0.C guard): the `OPTIONAL MATCH
//     (o)-[r]->(owner)` fan is collapsed by the single `collect`, so any number
//     of links produces one row. A zero-link object null-restores to one row
//     with `owners` carrying a degenerate `{ownerKey: null}` artifact (the
//     documented full-engine grouping behaviour, as in `myTasksSpec`); the app
//     drops null entries.
//   - The metadata columns are aspect-data reads off `.content` (the
//     `objectLiveness` storeName idiom), so they resolve directly in the full
//     engine. A tombstoned object does not bind (`fetchNode` returns nil for a
//     soft-deleted vertex), so it emits no row and `EmptyBehavior: delete`
//     reclaims its read-model key.
//   - The relationship NAME (the upload "slot" / linkName) is NOT projected —
//     the full engine cannot project `type(r)` — so `owners` carries only the
//     destination node key. Detach of a listed doc (which needs the linkName)
//     is therefore a documented follow-up.
const objectAttachmentsSpec = `
MATCH (o:object {key: $actorKey})
OPTIONAL MATCH (o)-[r]->(owner)
WITH
  o.key AS entityKey,
  o.content.data.storeName AS storeName,
  o.content.data.contentType AS contentType,
  o.content.data.size AS size,
  collect(DISTINCT { ownerKey: owner.key }) AS owners
RETURN
  entityKey AS actorKey,
  entityKey,
  storeName,
  contentType,
  size,
  owners
`
