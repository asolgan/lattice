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
// identities only — the primordial admin and the Loom + Weaver service actors
// (`internal/bootstrap/primordial.go`). These actors ARE core: protected,
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
			AnchorType:         "identity",
			OutputKeyPattern:   "cap.{actorSuffix}",
			BodyColumns:        []string{"platformPermissions"},
			EmptyBehavior:      "delete",
			Freshness:          "auto",
			Lanes:              []string{"default"},
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
    {operationType: 'UninstallPackage', scope: 'any'}
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

