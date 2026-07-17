package wellnessdomain

import "testing"

// TestOpMetas_DispatchClassMatchesOwningDDL mirrors clinic-domain's guard of
// the same name: Dispatch.Class must be the owning vertexType DDL's
// CanonicalName (the Contract #2 §2.1 envelope `class` DDL-hint a real
// client submission uses), never the vertical name — clinic-domain's Fire 5
// Inc 1 shipped that exact mistake (cd8696d, corrected here).
func TestOpMetas_DispatchClassMatchesOwningDDL(t *testing.T) {
	classForOp := map[string]string{}
	for _, d := range DDLs() {
		if d.Class != "meta.ddl.vertexType" {
			continue
		}
		for _, op := range d.PermittedCommands {
			classForOp[op] = d.CanonicalName
		}
	}
	for _, m := range OpMetas() {
		if m.Dispatch == nil {
			continue
		}
		want := classForOp[m.OperationType]
		if want == "" {
			t.Fatalf("%s: no owning vertexType DDL found in PermittedCommands", m.OperationType)
		}
		if m.Dispatch.Class != want {
			t.Errorf("%s: Dispatch.Class = %q, want %q (owning DDL's CanonicalName)", m.OperationType, m.Dispatch.Class, want)
		}
	}
}
