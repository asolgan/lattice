package pkgmgr

import (
	"encoding/json"
	"testing"

	"github.com/asolgan/lattice/internal/refractor/ruleengine/full"
)

// fullCypherParser adapts ruleengine/full.Engine to CypherParser. Living in a
// _test.go file (not pkgmgr's production code) is what avoids the import
// cycle CypherParser's doc explains — full's own test binary transitively
// imports pkgmgr, but pkgmgr's *test* binary importing full (prod) has no such
// path back, so this is safe here (and would be safe in any other package's
// production code too — just not pkgmgr's).
type fullCypherParser struct{}

func (fullCypherParser) Parse(ruleBody string) error {
	_, err := full.New().Parse(ruleBody)
	return err
}

func lensContent(t *testing.T, lc LensArtifactContent) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(lc)
	if err != nil {
		t.Fatalf("marshal lens content: %v", err)
	}
	return b
}

func grantContent(t *testing.T, gc GrantArtifactContent) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(gc)
	if err != nil {
		t.Fatalf("marshal grant content: %v", err)
	}
	return b
}

func TestValidateCapabilityArtifact_DisabledKind(t *testing.T) {
	// weaverTarget is not enabled until Fire 3 — lens and grant are the two
	// kinds this increment enables, so a still-disabled kind is needed here.
	report, err := ValidateCapabilityArtifact("weaverTarget", json.RawMessage(`{}`), fullCypherParser{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected invalid report for a disabled kind, got valid")
	}
	if len(report.Errors) != 1 {
		t.Fatalf("expected exactly one error, got %v", report.Errors)
	}
}

func TestValidateCapabilityArtifact_MalformedContent(t *testing.T) {
	_, err := ValidateCapabilityArtifact("lens", json.RawMessage(`not-json`), fullCypherParser{}, nil)
	if err == nil {
		t.Fatalf("expected a caller-contract error for malformed content")
	}
}

func TestValidateCapabilityArtifact_ValidLens(t *testing.T) {
	content := lensContent(t, LensArtifactContent{
		CanonicalName: "activeProvidersBySpecialty",
		Adapter:       "nats-kv",
		Bucket:        "active-providers",
		Spec:          "MATCH (p:provider) RETURN p.key AS key",
	})
	report, err := ValidateCapabilityArtifact("lens", content, fullCypherParser{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !report.Valid {
		t.Fatalf("expected a valid report, got errors: %v", report.Errors)
	}
}

func TestValidateCapabilityArtifact_UnparseableCypher(t *testing.T) {
	content := lensContent(t, LensArtifactContent{
		CanonicalName: "brokenLens",
		Adapter:       "nats-kv",
		Bucket:        "broken-lens",
		Spec:          "MATCH (p:provider RETURN p.key AS key", // missing close paren
	})
	report, err := ValidateCapabilityArtifact("lens", content, fullCypherParser{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report for unparseable cypher")
	}
}

func TestValidateCapabilityArtifact_MissingCanonicalName(t *testing.T) {
	content := lensContent(t, LensArtifactContent{
		Adapter: "nats-kv",
		Bucket:  "no-name",
		Spec:    "MATCH (p:provider) RETURN p.key AS key",
	})
	report, err := ValidateCapabilityArtifact("lens", content, fullCypherParser{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report for a missing canonicalName")
	}
}

func TestValidateCapabilityArtifact_CoreKVAdapterRejected(t *testing.T) {
	// P5: a lens may never target Core KV directly — validateLensAdapters
	// already rejects any Adapter other than "" / "nats-kv" / "postgres", so an
	// AI-authored artifact cannot smuggle a core-kv-shaped adapter through.
	content := lensContent(t, LensArtifactContent{
		CanonicalName: "sneakyLens",
		Adapter:       "core-kv",
		Spec:          "MATCH (p:provider) RETURN p.key AS key",
	})
	report, err := ValidateCapabilityArtifact("lens", content, fullCypherParser{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report for a core-kv-shaped adapter")
	}
}

func TestValidateCapabilityArtifact_ReservedBucketAliasRejected(t *testing.T) {
	// The reserved short alias guard (bucketguard.go) must apply identically to
	// an AI-authored lens — reused validateAll, not a weaker copy.
	content := lensContent(t, LensArtifactContent{
		CanonicalName: "phantomLens",
		Adapter:       "nats-kv",
		Bucket:        "capability",
		Spec:          "MATCH (p:provider) RETURN p.key AS key",
	})
	report, err := ValidateCapabilityArtifact("lens", content, fullCypherParser{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report for the reserved 'capability' bucket alias")
	}
}

func TestValidateCapabilityArtifact_OutOfScopeFieldRejected(t *testing.T) {
	// A raw content payload that smuggles a field this increment doesn't expose
	// (e.g. "protected") must be caught, not silently dropped by json.Unmarshal
	// and downgraded to a plain lens.
	content := json.RawMessage(`{"canonicalName":"sneakyProtected","adapter":"postgres","table":"sneaky","spec":"MATCH (p:provider) RETURN p.key AS key","protected":true}`)
	report, err := ValidateCapabilityArtifact("lens", content, fullCypherParser{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report for an out-of-scope 'protected' field")
	}
}

func TestValidateCapabilityArtifact_MissingBucketRejected(t *testing.T) {
	content := lensContent(t, LensArtifactContent{
		CanonicalName: "noBucketLens",
		Adapter:       "nats-kv",
		Spec:          "MATCH (p:provider) RETURN p.key AS key",
	})
	report, err := ValidateCapabilityArtifact("lens", content, fullCypherParser{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report for a nats-kv lens with no Bucket")
	}
}

func TestValidateCapabilityArtifact_ValidGrant(t *testing.T) {
	content := grantContent(t, GrantArtifactContent{
		OperationType: "RescheduleAppointment",
		Scope:         "self",
		GrantsTo:      []string{"front-desk"},
	})
	held := []HeldPermission{{OperationType: "RescheduleAppointment", Scope: "any"}}
	report, err := ValidateCapabilityArtifact("grant", content, fullCypherParser{}, held)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !report.Valid {
		t.Fatalf("expected a valid report, got errors: %v", report.Errors)
	}
}

func TestValidateCapabilityArtifact_GrantExactScopeMatch(t *testing.T) {
	content := grantContent(t, GrantArtifactContent{
		OperationType: "RescheduleAppointment",
		Scope:         "any",
		GrantsTo:      []string{"front-desk"},
	})
	held := []HeldPermission{{OperationType: "RescheduleAppointment", Scope: "any"}}
	report, err := ValidateCapabilityArtifact("grant", content, fullCypherParser{}, held)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !report.Valid {
		t.Fatalf("expected a valid report, got errors: %v", report.Errors)
	}
}

func TestValidateCapabilityArtifact_GrantExceedsRequesterScope_Rejected(t *testing.T) {
	// The privilege-escalation case the scope check exists to close: the
	// requester holds ONLY "self" for this operationType, but the artifact
	// requests granting "any" — broader than the requester's own authority.
	content := grantContent(t, GrantArtifactContent{
		OperationType: "RescheduleAppointment",
		Scope:         "any",
		GrantsTo:      []string{"front-desk"},
	})
	held := []HeldPermission{{OperationType: "RescheduleAppointment", Scope: "self"}}
	report, err := ValidateCapabilityArtifact("grant", content, fullCypherParser{}, held)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report for a grant exceeding the requester's held scope")
	}
}

func TestValidateCapabilityArtifact_GrantRequesterHoldsNothing_Rejected(t *testing.T) {
	// An operator routing an AI request for an operationType they don't hold at
	// all can never mint that grant, at any scope.
	content := grantContent(t, GrantArtifactContent{
		OperationType: "DeleteEverything",
		Scope:         "self",
		GrantsTo:      []string{"operator"},
	})
	report, err := ValidateCapabilityArtifact("grant", content, fullCypherParser{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report when the requester holds no matching permission")
	}
}

func TestValidateCapabilityArtifact_GrantDifferentOperationType_Rejected(t *testing.T) {
	// Holding broad authority over ONE operationType must never cover a grant
	// naming a DIFFERENT operationType.
	content := grantContent(t, GrantArtifactContent{
		OperationType: "DeleteEverything",
		Scope:         "self",
		GrantsTo:      []string{"operator"},
	})
	held := []HeldPermission{{OperationType: "RescheduleAppointment", Scope: "any"}}
	report, err := ValidateCapabilityArtifact("grant", content, fullCypherParser{}, held)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report for a grant naming an operationType the requester doesn't hold")
	}
}

func TestValidateCapabilityArtifact_GrantMissingOperationType_Rejected(t *testing.T) {
	content := grantContent(t, GrantArtifactContent{
		Scope:    "self",
		GrantsTo: []string{"front-desk"},
	})
	report, err := ValidateCapabilityArtifact("grant", content, fullCypherParser{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report for a missing operationType")
	}
}

func TestValidateCapabilityArtifact_GrantInvalidScope_Rejected(t *testing.T) {
	content := grantContent(t, GrantArtifactContent{
		OperationType: "RescheduleAppointment",
		Scope:         "everything",
		GrantsTo:      []string{"front-desk"},
	})
	held := []HeldPermission{{OperationType: "RescheduleAppointment", Scope: "any"}}
	report, err := ValidateCapabilityArtifact("grant", content, fullCypherParser{}, held)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report for a scope outside {any, self}")
	}
}

func TestValidateCapabilityArtifact_GrantEmptyGrantsTo_Rejected(t *testing.T) {
	content := grantContent(t, GrantArtifactContent{
		OperationType: "RescheduleAppointment",
		Scope:         "self",
	})
	held := []HeldPermission{{OperationType: "RescheduleAppointment", Scope: "any"}}
	report, err := ValidateCapabilityArtifact("grant", content, fullCypherParser{}, held)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report for an empty grantsTo")
	}
}

func TestValidateCapabilityArtifact_GrantWhitespaceRole_Rejected(t *testing.T) {
	content := grantContent(t, GrantArtifactContent{
		OperationType: "RescheduleAppointment",
		Scope:         "self",
		GrantsTo:      []string{"  "},
	})
	held := []HeldPermission{{OperationType: "RescheduleAppointment", Scope: "any"}}
	report, err := ValidateCapabilityArtifact("grant", content, fullCypherParser{}, held)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report for a whitespace-only role name")
	}
}

func TestValidateCapabilityArtifact_GrantDuplicateRole_Rejected(t *testing.T) {
	content := grantContent(t, GrantArtifactContent{
		OperationType: "RescheduleAppointment",
		Scope:         "self",
		GrantsTo:      []string{"front-desk", "front-desk"},
	})
	held := []HeldPermission{{OperationType: "RescheduleAppointment", Scope: "any"}}
	report, err := ValidateCapabilityArtifact("grant", content, fullCypherParser{}, held)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report for a duplicate role in grantsTo")
	}
}

func TestValidateCapabilityArtifact_KindCaseSensitive_Rejected(t *testing.T) {
	// The enabled-kind check is exact-string, case-sensitive — "Grant" must
	// never be silently treated as the enabled "grant" kind, on either this Go
	// allow-list or the independent Starlark ENABLED_KINDS gate it mirrors.
	report, err := ValidateCapabilityArtifact("Grant", json.RawMessage(`{}`), fullCypherParser{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected invalid report for a case-mismatched kind, got valid")
	}
}

func TestValidateCapabilityArtifact_GrantDuplicatePermission_Rejected(t *testing.T) {
	// validatePermissionIdentityUniqueness only fires within a Definition's own
	// Permissions slice — a single-grant artifact can never self-collide, so
	// this proves the shared validateAll pre-flight is genuinely wired in
	// (grantArtifactDefinition), not merely present as an unreachable import.
	def := grantArtifactDefinition(GrantArtifactContent{
		OperationType: "RescheduleAppointment",
		Scope:         "self",
		GrantsTo:      []string{"front-desk"},
	}, "", "")
	def.Permissions = append(def.Permissions, def.Permissions[0])
	if err := def.validateAll(); err == nil {
		t.Fatalf("expected validateAll to reject a duplicate (operationType, scope) permission pair")
	}
}

func TestValidateCapabilityArtifact_GrantOutOfScopeFieldRejected(t *testing.T) {
	// A raw content payload smuggling a field GrantArtifactContent doesn't
	// expose (e.g. "roles" instead of "grantsTo") must be caught, not silently
	// dropped by json.Unmarshal.
	content := json.RawMessage(`{"operationType":"RescheduleAppointment","scope":"self","grantsTo":["front-desk"],"roles":["operator"]}`)
	held := []HeldPermission{{OperationType: "RescheduleAppointment", Scope: "any"}}
	report, err := ValidateCapabilityArtifact("grant", content, fullCypherParser{}, held)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected an invalid report for an out-of-scope 'roles' field")
	}
}

func TestDefinitionForCapabilityArtifact_Grant(t *testing.T) {
	content := grantContent(t, GrantArtifactContent{
		OperationType: "RescheduleAppointment",
		Scope:         "self",
		GrantsTo:      []string{"front-desk"},
		Note:          "AI-authored grant",
	})
	def, err := DefinitionForCapabilityArtifact("grant", content, "ai-grant-pkg", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(def.Permissions) != 1 {
		t.Fatalf("expected exactly one Permission, got %d", len(def.Permissions))
	}
	p := def.Permissions[0]
	if p.OperationType != "RescheduleAppointment" || p.Scope != "self" || p.Note != "AI-authored grant" {
		t.Fatalf("materialized Permission = %+v, want operationType=RescheduleAppointment scope=self note=%q", p, "AI-authored grant")
	}
	if len(p.GrantsTo) != 1 || p.GrantsTo[0] != "front-desk" {
		t.Fatalf("materialized Permission.GrantsTo = %v, want [front-desk]", p.GrantsTo)
	}
}
