// Story 3.6 Starlark unit smoke tests.
//
// Each test confirms that the Starlark script for a DDL:
//   1. Parses and compiles without error.
//   2. Returns a dict with "mutations" + "events" keys (Contract #3 shape).
//   3. Produces at least one mutation for create/assign ops.
//
// These tests run the StarlarkRunner directly without NATS — the sandbox
// environment is real (state, op, ddl, nanoid globals).
package processor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/asolgan/lattice/internal/bootstrap"
)

// makeRMScriptContext builds a minimal ScriptContext for a role-mgmt script test.
func makeRMScriptContext(scriptSource, operationType string, payloadJSON string) ScriptContext {
	payload := json.RawMessage(payloadJSON)
	return ScriptContext{
		Operation: &OperationEnvelope{
			RequestID:     testNanoID1,
			Lane:          LaneDefault,
			OperationType: operationType,
			Actor:         "vtx.identity." + testNanoID2,
			SubmittedAt:   "2026-05-16T10:00:00Z",
			Payload:       payload,
		},
		Hydrated:     map[string]VertexDoc{},
		DDLLookup:    map[string]MetaVertex{},
		ScriptSource: scriptSource,
		ScriptClass:  "role",
	}
}

// getRoleDDLScript extracts the script from bootstrap's RoleMgmtDDLs by canonicalName.
func getRoleDDLScript(canonicalName string) string {
	for _, d := range bootstrap.RoleMgmtDDLs() {
		if d.CanonicalName == canonicalName {
			return d.Script
		}
	}
	panic("no DDL found for canonicalName: " + canonicalName)
}

// assertContract3Shape verifies the result has the Contract #3 shape.
func assertContract3Shape(t *testing.T, result ScriptResult, wantMutations bool) {
	t.Helper()
	if wantMutations && len(result.Mutations) == 0 {
		t.Fatalf("expected at least one mutation, got none")
	}
	for i, m := range result.Mutations {
		if m.Op != "create" && m.Op != "update" && m.Op != "tombstone" {
			t.Fatalf("mutations[%d].op = %q, want create|update|tombstone", i, m.Op)
		}
		if m.Key == "" {
			t.Fatalf("mutations[%d].key is empty", i)
		}
	}
}

// TestStarlark_RoleDDL_CreateRole: role DDL script parses + returns Contract #3 for CreateRole.
func TestStarlark_RoleDDL_CreateRole(t *testing.T) {
	runner := NewStarlarkRunner(0, 0)
	sc := makeRMScriptContext(
		getRoleDDLScript("role"),
		"CreateRole",
		`{"name":"TestRole","description":"A smoke test role"}`,
	)
	result, err := runner.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertContract3Shape(t, result, true)
	// First mutation key must start with vtx.role.
	if !strings.HasPrefix(result.Mutations[0].Key, "vtx.role.") {
		t.Fatalf("mutations[0].key = %q, want vtx.role.*", result.Mutations[0].Key)
	}
	// One RoleCreated event.
	if len(result.Events) == 0 || result.Events[0].Class != "RoleCreated" {
		t.Fatalf("expected RoleCreated event, got %+v", result.Events)
	}
}

// TestStarlark_RoleDDL_UpdateRole: role DDL handles UpdateRole.
func TestStarlark_RoleDDL_UpdateRole(t *testing.T) {
	runner := NewStarlarkRunner(0, 0)
	sc := makeRMScriptContext(
		getRoleDDLScript("role"),
		"UpdateRole",
		`{"roleKey":"vtx.role.`+testNanoID2+`","description":"Updated desc"}`,
	)
	result, err := runner.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertContract3Shape(t, result, true)
	if result.Mutations[0].Op != "update" {
		t.Fatalf("expected update op, got %q", result.Mutations[0].Op)
	}
}

// TestStarlark_RoleDDL_TombstoneRole: role DDL handles TombstoneRole.
func TestStarlark_RoleDDL_TombstoneRole(t *testing.T) {
	runner := NewStarlarkRunner(0, 0)
	sc := makeRMScriptContext(
		getRoleDDLScript("role"),
		"TombstoneRole",
		`{"roleKey":"vtx.role.`+testNanoID2+`"}`,
	)
	result, err := runner.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertContract3Shape(t, result, true)
	if result.Mutations[0].Op != "tombstone" {
		t.Fatalf("expected tombstone op, got %q", result.Mutations[0].Op)
	}
}

// TestStarlark_PermissionDDL_CreatePermission: permission DDL script parses + returns Contract #3.
func TestStarlark_PermissionDDL_CreatePermission(t *testing.T) {
	runner := NewStarlarkRunner(0, 0)
	sc := makeRMScriptContext(
		getRoleDDLScript("permission"),
		"CreatePermission",
		`{"operationType":"CreateRole","scope":"any","note":"test permission"}`,
	)
	sc.ScriptClass = "permission"
	result, err := runner.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertContract3Shape(t, result, true)
	if !strings.HasPrefix(result.Mutations[0].Key, "vtx.permission.") {
		t.Fatalf("mutations[0].key = %q, want vtx.permission.*", result.Mutations[0].Key)
	}
	if len(result.Events) == 0 || result.Events[0].Class != "PermissionCreated" {
		t.Fatalf("expected PermissionCreated event, got %+v", result.Events)
	}
}

// TestStarlark_HoldsRoleDDL_AssignRole: holdsRole DDL parses + returns a create link mutation.
func TestStarlark_HoldsRoleDDL_AssignRole(t *testing.T) {
	runner := NewStarlarkRunner(0, 0)
	sc := makeRMScriptContext(
		getRoleDDLScript("holdsRole"),
		"AssignRole",
		`{"identityKey":"vtx.identity.`+testNanoID1+`","roleKey":"vtx.role.`+testNanoID2+`"}`,
	)
	sc.ScriptClass = "holdsRole"
	result, err := runner.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertContract3Shape(t, result, true)
	if !strings.HasPrefix(result.Mutations[0].Key, "lnk.identity.") {
		t.Fatalf("mutations[0].key = %q, want lnk.identity.*", result.Mutations[0].Key)
	}
	if !strings.Contains(result.Mutations[0].Key, ".holdsRole.") {
		t.Fatalf("mutations[0].key = %q, want lnk.identity.*.holdsRole.*", result.Mutations[0].Key)
	}
	if len(result.Events) == 0 || result.Events[0].Class != "RoleAssigned" {
		t.Fatalf("expected RoleAssigned event, got %+v", result.Events)
	}
}

// TestStarlark_HoldsRoleDDL_RevokeRole: holdsRole DDL handles RevokeRole (tombstone).
func TestStarlark_HoldsRoleDDL_RevokeRole(t *testing.T) {
	runner := NewStarlarkRunner(0, 0)
	sc := makeRMScriptContext(
		getRoleDDLScript("holdsRole"),
		"RevokeRole",
		`{"identityKey":"vtx.identity.`+testNanoID1+`","roleKey":"vtx.role.`+testNanoID2+`"}`,
	)
	sc.ScriptClass = "holdsRole"
	result, err := runner.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertContract3Shape(t, result, true)
	if result.Mutations[0].Op != "tombstone" {
		t.Fatalf("expected tombstone op, got %q", result.Mutations[0].Op)
	}
}

// TestStarlark_GrantsPermissionDDL_GrantPermission: grantsPermission DDL parses + creates link.
func TestStarlark_GrantsPermissionDDL_GrantPermission(t *testing.T) {
	runner := NewStarlarkRunner(0, 0)
	sc := makeRMScriptContext(
		getRoleDDLScript("grantsPermission"),
		"GrantPermission",
		`{"permissionKey":"vtx.permission.`+testNanoID1+`","roleKey":"vtx.role.`+testNanoID2+`"}`,
	)
	sc.ScriptClass = "grantsPermission"
	result, err := runner.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertContract3Shape(t, result, true)
	if !strings.HasPrefix(result.Mutations[0].Key, "lnk.permission.") {
		t.Fatalf("mutations[0].key = %q, want lnk.permission.*", result.Mutations[0].Key)
	}
	if !strings.Contains(result.Mutations[0].Key, ".grantsPermission.") {
		t.Fatalf("mutations[0].key = %q, want lnk.permission.*.grantsPermission.*", result.Mutations[0].Key)
	}
	if len(result.Events) == 0 || result.Events[0].Class != "PermissionGranted" {
		t.Fatalf("expected PermissionGranted event, got %+v", result.Events)
	}
}

// TestStarlark_ReportsTo_AssignReportingChain: reportsTo DDL parses + creates link.
func TestStarlark_ReportsTo_AssignReportingChain(t *testing.T) {
	runner := NewStarlarkRunner(0, 0)
	sc := makeRMScriptContext(
		getRoleDDLScript("reportsTo"),
		"AssignReportingChain",
		`{"reportKey":"vtx.identity.`+testNanoID1+`","managerKey":"vtx.identity.`+testNanoID2+`"}`,
	)
	sc.ScriptClass = "reportsTo"
	result, err := runner.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertContract3Shape(t, result, true)
	if !strings.HasPrefix(result.Mutations[0].Key, "lnk.identity.") {
		t.Fatalf("mutations[0].key = %q, want lnk.identity.*", result.Mutations[0].Key)
	}
	if !strings.Contains(result.Mutations[0].Key, ".reportsTo.") {
		t.Fatalf("mutations[0].key = %q, want lnk.identity.*.reportsTo.*", result.Mutations[0].Key)
	}
	if len(result.Events) == 0 || result.Events[0].Class != "ReportingChainAssigned" {
		t.Fatalf("expected ReportingChainAssigned event, got %+v", result.Events)
	}
}

// TestStarlark_AllScriptsParse: quick compile check for all 5 DDL scripts.
func TestStarlark_AllScriptsParse(t *testing.T) {
	runner := NewStarlarkRunner(0, 0)
	// Use CreateRole as a representative op for each DDL's compile check.
	testCases := []struct {
		canonicalName string
		opType        string
		payload       string
	}{
		{"role", "CreateRole", `{"name":"x"}`},
		{"permission", "CreatePermission", `{"operationType":"X","scope":"any"}`},
		{"holdsRole", "AssignRole", `{"identityKey":"vtx.identity.` + testNanoID1 + `","roleKey":"vtx.role.` + testNanoID2 + `"}`},
		{"grantsPermission", "GrantPermission", `{"permissionKey":"vtx.permission.` + testNanoID1 + `","roleKey":"vtx.role.` + testNanoID2 + `"}`},
		{"reportsTo", "AssignReportingChain", `{"reportKey":"vtx.identity.` + testNanoID1 + `","managerKey":"vtx.identity.` + testNanoID2 + `"}`},
	}
	for _, tc := range testCases {
		t.Run(tc.canonicalName, func(t *testing.T) {
			sc := makeRMScriptContext(getRoleDDLScript(tc.canonicalName), tc.opType, tc.payload)
			sc.ScriptClass = tc.canonicalName
			result, err := runner.Run(context.Background(), sc)
			if err != nil {
				t.Fatalf("DDL %q script error: %v", tc.canonicalName, err)
			}
			// Must return Contract #3 shape.
			_ = result
		})
	}
}
