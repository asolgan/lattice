package bootstrap

// LensDefinition holds the data payload for a Capability Lens meta-vertex.
// The Lens vertex has class "meta.lens" per Contract #6 §6.13.
// Aspects: canonicalName, cypherRule, targetBucket, outputSchema, and — for an
// actor-aggregate lens — projectionKind + the §6.13 Output descriptor.
type LensDefinition struct {
	CanonicalName string
	CypherRule    string
	TargetBucket  string
	OutputSchema  string

	// ProjectionKind opts the lens into the declarative actor-aggregate
	// projection plan ("actorAggregate"); empty for an operation-aggregate lens.
	ProjectionKind string

	// Output is the §6.13 Output descriptor for an actor-aggregate lens; nil
	// otherwise.
	Output *OutputDescriptorSpec

	// Adapter is the projection-output adapter — "nats-kv" (the default, empty)
	// or "postgres". A postgres primordial lens carries the read-path posture
	// below; the bootstrap seeder resolves an empty DSN from REFRACTOR_PG_DSN at
	// activation, exactly as the package declaration surface does (Contract #6
	// §6.14, D1; the pkgmgr.LensSpec analog).
	Adapter string

	// Table is the Postgres table (postgres adapter only). A GrantTable lens
	// leaves it empty (Refractor defaults to actor_read_grants); a Protected
	// lens names its provisioned table.
	Table string

	// GrantTable marks a postgres lens as a cap-read.* grant projector: its rows
	// are written to the shared actor_read_grants table through the seq-guarded
	// grant writer (Table defaults to actor_read_grants, key to the platform
	// composite actor_id/anchor_id/grant_source). The base read-auth producer.
	GrantTable bool

	// Protected marks a postgres lens as a read-path-authorized business read
	// model (RLS table provisioned from Columns). Mutually exclusive with
	// GrantTable. Unused by the primordial lenses today (the base grant lens is
	// a GrantTable); reserved for symmetry with pkgmgr.LensSpec.
	Protected bool

	// Columns declares the business columns of a Protected postgres table
	// (name + verbatim Postgres type). Ignored for a GrantTable / nats-kv lens.
	Columns []PostgresColumn
}

// PostgresColumn declares one provisioned column of a Protected read-model
// table. Mirrors pkgmgr.PostgresColumn / the Refractor-side on-wire shape.
type PostgresColumn struct {
	Name string
	Type string
}

// OutputDescriptorSpec mirrors the on-wire §6.13 Output descriptor a primordial
// actor-aggregate lens seeds. It is encoded into the `output` aspect + the spec
// body so Refractor's CoreKVSource compiles a ProjectionPlan from it. Field
// shape matches the Refractor-side lens.OutputDescriptorSpec.
type OutputDescriptorSpec struct {
	AnchorType         string   `json:"anchorType"`
	OutputKeyPattern   string   `json:"outputKeyPattern"`
	BodyColumns        []string `json:"bodyColumns"`
	EmptyBehavior      string   `json:"emptyBehavior"`
	RealnessFilter     string   `json:"realnessFilter,omitempty"`
	Freshness          string   `json:"freshness,omitempty"`
	ActorField         string   `json:"actorField,omitempty"`
	Lanes              []string `json:"lanes,omitempty"`
	StaticEmptyColumns []string `json:"staticEmptyColumns,omitempty"`
}

// CapabilityLensDefinition returns the primary Capability Lens definition —
// the primordial-identity anchor. Contract #7 §7.2 item 5 — vtx.meta.<NanoID>
// with class "meta.lens"; Contract #6 §6.1 decomposition note.
//
// Core projects root-equivalent platform grants for the kernel-seeded system
// identities only — the primordial admin and the Loom + Weaver + Bridge service
// actors (`internal/bootstrap/primordial.go`). These actors ARE core: protected,
// kernel-seeded, and fixed, so their root-grant set is hard-coded here rather
// than derived through the rbac role/permission graph. That keeps the
// kernel authorizable even when no rbac package is installed and removes every
// rbac (role/permission/holdsRole/grantedBy) and service/location
// (containedIn/availableAt/unavailableAt/permitsOperation) reference from
// core's bootstrap cypher — those vocabularies are owned by their packages
// (rbac-domain projects ordinary actors' role-derived grants to the disjoint
// cap.roles.<actor> key; a future service package projects service access).
func CapabilityLensDefinition() LensDefinition {
	return LensDefinition{
		CanonicalName: "capability",
		TargetBucket:  "capability",
		// Actor-aggregate: the compiled ProjectionPlan drives the §6.2 envelope.
		// The cap.<actor> doc carries `lanes` and an always-empty
		// `ephemeralGrants` (live grants live in the disjoint cap.ephemeral.<actor>
		// doc; §6.2/§6.3 require the field present here). emptyBehavior:delete is
		// the actor-disappearance tombstone.
		ProjectionKind: "actorAggregate",
		Output: &OutputDescriptorSpec{
			AnchorType:       "identity",
			OutputKeyPattern: "cap.{actorSuffix}",
			BodyColumns:      []string{"platformPermissions"},
			EmptyBehavior:    "delete",
			Freshness:        "auto",
			// Per-lane submission grant (Contract #2 §2.3). The protected
			// kernel-seeded system actors (admin + Loom + Weaver + Bridge +
			// object-store-manager) carry the full root-grant set: `meta`
			// (serialized DDL — installs/lens DDL), `system` (engine result/
			// dispatch ops), `urgent`, and `default`. This matches their
			// uniform root platformPermissions above; per-actor lane scoping is
			// a future refinement. Ordinary actors get only `default` from the
			// rbac cap.roles.<actor> lens.
			Lanes:              []string{"default", "meta", "urgent", "system"},
			StaticEmptyColumns: []string{"ephemeralGrants", "serviceAccess", "roles"},
		},
		// The anchor projects only the protected (kernel-seeded) system
		// identities; the WHERE filters out every ordinary actor (zero rows →
		// no cap.<actor> doc for them — they read cap.roles.<actor>). Each
		// system identity receives the fixed kernel root-grant set: the
		// scope:"any" meta + package-install permissions the operator role
		// carries. The grant set is a literal here, NOT a graph walk, so core
		// references no rbac vocabulary.
		CypherRule: `
MATCH (identity:identity {key: $actorKey})
WHERE identity.data.protected = true
RETURN
  identity.key AS actorKey,
  [
    {operationType: 'CreateMetaVertex', scope: 'any'},
    {operationType: 'UpdateMetaVertex', scope: 'any'},
    {operationType: 'TombstoneMetaVertex', scope: 'any'},
    {operationType: 'InstallPackage', scope: 'any'},
    {operationType: 'UninstallPackage', scope: 'any'},
    {operationType: 'UpgradePackage', scope: 'any'}
  ] AS platformPermissions
`,
		// outputSchema: JSON Schema for the Capability KV document per Contract #6 §6.2.
		OutputSchema: `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["key","actor","version","projectedAt","projectedFromRevisions","lanes",
               "platformPermissions","serviceAccess","roles"],
  "properties": {
    "key":                   {"type": "string"},
    "actor":                 {"type": "string"},
    "version":               {"type": "string"},
    "projectedAt":           {"type": "string", "format": "date-time"},
    "projectedFromRevisions":{"type": "object", "additionalProperties": {"type": "integer"}},
    "lanes":                 {"type": "array",  "items": {"type": "string"}},
    "platformPermissions":   {"type": "array",  "items": {
      "type": "object",
      "required": ["operationType","scope"],
      "properties": {
        "operationType": {"type": "string"},
        "scope":         {"type": "string", "enum": ["any","self","specific","owned"]}
      }
    }},
    "serviceAccess":  {"type": "array"},
    "roles":          {"type": "array", "items": {"type": "string"}}
  }
}`,
	}
}

// CapabilityReadLensDefinition returns the base read-path authorization lens —
// the core slice of the §6.14 cap-read.* family (D1). Contract #7 §7.2 item 5
// (vtx.meta.<NanoID> with class "meta.lens"); Contract #6 §6.14.
//
// Read auth mirrors write auth (§6.1's contract-contribution model): core owns
// the bucket + the key conventions and projects only the BASE read scope every
// actor carries independent of any package — its **self** anchor (an actor may
// always read its own vertex). Each package ships its own cap-read.<domain>
// actor-aggregate lens for the relationships it owns (rbac-domain →
// cap-read.roles, loftspace → cap-read.residence, …); the actor's effective
// readable set is the union over all cap-read.*.<actor> slices (§6.14). This
// base lens references no package vocabulary, exactly as the write-path base
// capability lens does.
//
// Scope note (D1.1): this increment projects the self anchor for every actor.
// The primordial root-read scope for kernel-seeded identities (the read analog
// of the write base's scope:"any" grant — the privileged all-access anchor)
// lands with the D1 enforcement seam, which defines the wildcard-anchor
// representation the RLS/read boundary matches against (design §3.3, M5).
//
// readableAnchors carries each entry as {anchorType, anchorId, via} (§6.14). The
// anchorId is the resource's bare NanoID — the §6.14 opaque-match-token
// representation (Andrew, 2026-06-29) — extracted from the vertex key by the
// auth-plane engine's fail-closed nanoIdFromKey function; this is distinct from
// §6.5 serviceAccess.service, which keeps the full key because there it is a
// dereferenceable read-hint address. emptyBehavior:delete is the
// actor-disappearance tombstone — the self anchor is always present, so the key
// drops only when the identity vertex itself disappears.
func CapabilityReadLensDefinition() LensDefinition {
	return LensDefinition{
		CanonicalName:  "capabilityRead",
		TargetBucket:   "capability",
		ProjectionKind: "actorAggregate",
		Output: &OutputDescriptorSpec{
			AnchorType:       "identity",
			OutputKeyPattern: "cap-read.{actorSuffix}",
			BodyColumns:      []string{"readableAnchors"},
			EmptyBehavior:    "delete",
			Freshness:        "auto",
			Lanes:            []string{"default"},
		},
		CypherRule: `
MATCH (identity:identity {key: $actorKey})
RETURN
  identity.key AS actorKey,
  [
    {anchorType: 'identity', anchorId: nanoIdFromKey(identity.key), via: ['self']}
  ] AS readableAnchors
`,
		// outputSchema: JSON Schema for the cap-read.<actor> document per
		// Contract #6 §6.14 (read-path mirror of the §6.2 envelope; the body
		// carries readableAnchors instead of the write-path grant columns).
		OutputSchema: `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["key","actor","version","projectedAt","projectedFromRevisions","lanes",
               "readableAnchors"],
  "properties": {
    "key":                   {"type": "string"},
    "actor":                 {"type": "string"},
    "version":               {"type": "string"},
    "projectedAt":           {"type": "string", "format": "date-time"},
    "projectedFromRevisions":{"type": "object", "additionalProperties": {"type": "integer"}},
    "lanes":                 {"type": "array",  "items": {"type": "string"}},
    "readableAnchors":       {"type": "array",  "items": {
      "type": "object",
      "required": ["anchorType","anchorId","via"],
      "properties": {
        "anchorType": {"type": "string"},
        "anchorId":   {"type": "string"},
        "via":        {"type": "array", "items": {"type": "string"}}
      }
    }}
  }
}`,
	}
}

// CapabilityReadGrantsLensDefinition returns the base read-grant PRODUCER —
// the Postgres GrantTable projection of the cap-read self-anchor (Contract #6
// §6.14, D1.3). It is the Postgres twin of CapabilityReadLensDefinition: that
// lens projects the readableAnchors[] document to the NATS-KV capability bucket
// (the Path-B transitional/audit shape, §6.14), while THIS lens projects the
// same self-anchor as a flat (actor_id, anchor_id, grant_source) grant row into
// the shared Postgres actor_read_grants table — the source of truth the
// Postgres-RLS enforcement boundary (Path A, the ratified end-state) reads.
//
// Without this producer the actor_read_grants table (provisioned by any
// protected lens's activation) is created EMPTY, so RLS matches nothing and
// every protected read is denied — the gap that stalled D1.3. With it, each
// actor holds its self-grant (an actor may always read its own vertex), which
// is exactly what the applicant-self milestone's RLS matches (A sees only A's
// applications).
//
// The two projections are SEPARATE lenses (Contract #6 §6.14 architectural
// note: one RETURN per lens; a second output shape is a second lens — the same
// rule the §6.1 write-path decomposition follows, and the precedent every
// package cap-read.<domain> grant lens will mirror).
//
// Shape: a PLAIN one-row-per-identity projection (not actorAggregate — a grant
// row is flat, no readableAnchors aggregate). GrantTable:true makes Refractor
// default the table to actor_read_grants and the key to the platform composite
// (actor_id, anchor_id, grant_source); the lens need only RETURN those three.
// grant_source = 'cap-read' is the base slice's source id (each lens owns and
// retracts only its own grant_source rows; package slices use cap-read.<domain>
// so they never collide). nanoIdFromKey is fail-closed (the §6.14 bare-NanoID
// anchor representation) — a malformed key yields no grant (deny), never a wrong
// one.
//
// RETRACTION. On an identity tombstone the self-grant is auto-revoked: the
// full-engine anchor-tombstone path (AnchorDeleteResult) resolves every key
// column read-free against the tombstoned anchor — including this lens's
// nanoIdFromKey(identity.key) function-call columns — and emits the
// (actor_id, anchor_id, grant_source) composite Delete the GrantWriterAdapter
// maps to RevokeGrant (the §6.14 seq-guarded soft-tombstone, stamped at the
// tombstone CDC message's stream seq so a stale lower-seq re-upsert cannot
// resurrect it). A lingering self-grant would be INERT even without this —
// self-only (actor == anchor), and a deactivated identity is denied at the JWT
// boundary (D1.2 auth + revocation) before RLS — but retracting it keeps the
// grant table bounded and forecloses a re-activated-NanoID collision.
//
// DSN is resolved from REFRACTOR_PG_DSN at activation (Adapter:"postgres" with
// no DSN → the bootstrap seeder leaves it empty and Refractor's translateSpec
// fills it from the env), so the kernel declares posture, not a connection
// string — mirroring the package declaration surface.
func CapabilityReadGrantsLensDefinition() LensDefinition {
	return LensDefinition{
		CanonicalName: "capabilityReadGrants",
		Adapter:       "postgres",
		GrantTable:    true,
		CypherRule: `
MATCH (identity:identity)
RETURN
  nanoIdFromKey(identity.key) AS actor_id,
  nanoIdFromKey(identity.key) AS anchor_id,
  'cap-read'                  AS grant_source
`,
	}
}

// CapabilityReadWildcardGrantsLensDefinition returns the base ALL-ACCESS
// read-grant PRODUCER — the read analog of the write path's kernel
// root-grant set (Contract #6 §6.14, D1 design §3.4 M5). It is
// CapabilityReadGrantsLensDefinition's wildcard sibling: instead of each
// actor's self-anchor, it grants the WildcardAnchor ("*",
// internal/refractor/adapter.WildcardAnchor) to the same fixed set of
// protected (kernel-seeded, root-equivalent) identities the write-path
// CapabilityLensDefinition already special-cases — the primordial admin and
// the Loom/Weaver/Bridge/object-store-manager service actors.
//
// Root-equivalence is identified the SAME way the write-side anchor does:
// `identity.data.protected = true`, a literal predicate over the vertex's own
// field, NOT a graph walk through rbac vocabulary (holdsRole/role) — core
// references no package vocabulary, mirroring CapabilityLensDefinition's own
// note that the grant set is "a literal here, NOT a graph walk." This is a
// PLAIN (not actorAggregate) full-graph projection, like
// CapabilityReadGrantsLensDefinition: one row per matching identity, not
// scoped by an actor key.
//
// Without this producer, an all-access read (e.g. a clinic staff/admin
// worklist spanning every patient) has no anchor to grant against — the
// per-row authz_anchors convention (a patient/provider/landlord's own
// NanoID) cannot express "every row," which is exactly the gap the D1 design
// flagged as deferred to "an Andrew posture call" (M5). M5's answer is a
// grant, never an RLS bypass: the wildcard row still flows through the same
// §6.14 set-membership policy (internal/refractor/adapter.rls.go), so an
// all-access read stays attributable and revocable like any other grant.
//
// grant_source = 'cap-read.root' (disjoint from the self-anchor producer's
// 'cap-read' and from any package's 'cap-read.<domain>' — each producer
// retracts only its own grant_source rows, §6.14).
func CapabilityReadWildcardGrantsLensDefinition() LensDefinition {
	return LensDefinition{
		CanonicalName: "capabilityReadWildcardGrants",
		Adapter:       "postgres",
		GrantTable:    true,
		CypherRule: `
MATCH (identity:identity)
WHERE identity.data.protected = true
RETURN
  nanoIdFromKey(identity.key) AS actor_id,
  '*'                         AS anchor_id,
  'cap-read.root'             AS grant_source
`,
	}
}
