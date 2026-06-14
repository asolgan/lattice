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

// CapabilityLensDefinition returns the primary Capability Lens definition.
// Contract #7 §7.2 item 5 — vtx.meta.<NanoID> with class "meta.lens".
// cypherRule body: Contract #6 §6.10 required behaviors.
// Rule is stored as TEXT; openCypher parsing arrives in Story 3.1.
// The rule body here is structurally valid cypher per the handoff brief decision #8.
func CapabilityLensDefinition() LensDefinition {
	return LensDefinition{
		CanonicalName: "capability",
		TargetBucket:  "capability",
		// Actor-aggregate: the compiled ProjectionPlan drives the §6.2 envelope.
		// The primary cap.<actor> doc carries `lanes` and an always-empty
		// `ephemeralGrants` (live grants live in the disjoint cap.ephemeral.<actor>
		// doc; §6.2/§6.3 require the field present here). emptyBehavior:delete is
		// the actor-disappearance tombstone (the doc is never deleted on an empty
		// permission set — it has no realness filter).
		ProjectionKind: "actorAggregate",
		Output: &OutputDescriptorSpec{
			AnchorType:         "identity",
			OutputKeyPattern:   "cap.{actorSuffix}",
			BodyColumns:        []string{"platformPermissions", "serviceAccess", "roles"},
			EmptyBehavior:      "delete",
			Freshness:          "auto",
			Lanes:              []string{"default"},
			StaticEmptyColumns: []string{"ephemeralGrants"},
		},
		// Cypher rule per Contract #6 §6.10 and brief decision #8.
		// Produces platformPermissions, serviceAccess, and roles.
		// Story 3.1 connects the openCypher engine; Story 3.2 activates live projection.
		//
		// This bootstrap god-cypher does NOT include ephemeralGrants. FR56
		// ephemeral grants are produced by the orchestration-base
		// `capabilityEphemeral` lens to the disjoint key
		// `cap.ephemeral.<actor>` (Contract #6 §6.6 amendment, Contract #10
		// §10.7). The `cap.<actor>` doc this cypher produces carries
		// roles/permissions/service access only — no `ephemeralGrants`
		// section.
		CypherRule: `
MATCH (identity:identity {key: $actorKey})

// --- platformPermissions ---
// Walk: identity → holdsRole → role <-grantedBy- permission
// Story 4.7 rename: grantsPermission(role→permission) became
// grantedBy(permission→role); the topology is identical, the traversal
// direction reverses.
OPTIONAL MATCH (identity)-[:holdsRole]->(role:role)<-[:grantedBy]-(perm:permission)

// --- serviceAccess ---
// Walk: identity → containedIn* → location → availableAt → service
// Exclusion: identity path → unavailableAt → service wins over availableAt
OPTIONAL MATCH (identity)-[:containedIn*0..]->(loc)
  -[:availableAt]->(svc)
WHERE NOT (identity)-[:containedIn*0..]->(loc)-[:unavailableAt]->(svc)

RETURN
  identity.key AS actorKey,
  collect(DISTINCT {
    operationType: perm.data.operationType,
    scope: perm.data.scope
  }) AS platformPermissions,
  collect(DISTINCT {
    service: svc.key,
    serviceClass: svc.class,
    resolvedVia: [loc.key],
    allowedOperations: [(svc)-[:permitsOperation]->(op) | {operationType: op.data.operationType}]
  }) AS serviceAccess,
  collect(DISTINCT role.key) AS roles
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

// CapabilityRoleIndexLensDefinition returns the secondary role-coverage index Lens.
// Contract #6 §6.1 — produces cap.role-by-operation.<operationType> keys.
// Story 3.2 activates live projection; Story 1.3 just seeds the definition.
func CapabilityRoleIndexLensDefinition() LensDefinition {
	return LensDefinition{
		CanonicalName: "capabilityRoleIndex",
		TargetBucket:  "capability",
		// Produces one entry per operationType listing roles that grant it.
		// Used by Processor denial-response (Story 3.4) to build FR22 rolesCarryingPermission.
		CypherRule: `
MATCH (role:role)<-[:grantedBy]-(perm:permission)
RETURN
  perm.data.operationType AS operationType,
  collect(DISTINCT role.canonicalName.data.value) AS roles,
  $projectedAt AS projectedAt
`,
		OutputSchema: `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["roles","projectedAt"],
  "properties": {
    "roles":       {"type": "array", "items": {"type": "string"}},
    "projectedAt": {"type": "string", "format": "date-time"}
  }
}`,
	}
}
