package pkgmgr

import "fmt"

// BuildInstallBatchForTest exposes the internal install-batch builder to the
// external pkgmgr_test package so a test can round-trip the emitted
// orchestration bodies through the engine parse structs (weaver.Target /
// loom.Pattern) — the regression that proves the seam emits exactly what the
// engines load, with no engine change. Test-only; not part of the public API.
func BuildInstallBatchForTest(def Definition) ([]InstallMutationForTest, []string, error) {
	inst := &Installer{}
	pkgKey := PackageVertexPrefix + deterministicNanoID(def.Name, def.Version, "package")

	ddlIDs := make([]string, len(def.DDLs))
	lensIDs := make([]string, len(def.Lenses))
	permIDs := make([]string, len(def.Permissions))
	roleIDs := make([]string, len(def.Roles))
	weaverTargetIDs := make([]string, len(def.WeaverTargets))
	loomPatternIDs := make([]string, len(def.LoomPatterns))
	opMetaIDs := make([]string, len(def.OpMetas))
	for idx, d := range def.DDLs {
		ddlIDs[idx] = deterministicNanoID(def.Name, def.Version, "ddl:"+d.CanonicalName)
	}
	for idx, l := range def.Lenses {
		lensIDs[idx] = deterministicNanoID(def.Name, def.Version, "lens:"+l.CanonicalName)
	}
	for idx, p := range def.Permissions {
		permIDs[idx] = deterministicNanoID(def.Name, def.Version,
			fmt.Sprintf("perm:%d:%s", idx, p.OperationType))
	}
	for idx, r := range def.Roles {
		roleIDs[idx] = deterministicNanoID(def.Name, def.Version, "role:"+r.CanonicalName)
	}
	for idx, t := range def.WeaverTargets {
		weaverTargetIDs[idx] = deterministicNanoID(def.Name, def.Version, "weaverTarget:"+t.TargetID)
	}
	for idx, p := range def.LoomPatterns {
		loomPatternIDs[idx] = deterministicNanoID(def.Name, def.Version, "loomPattern:"+p.PatternID)
	}
	for idx, o := range def.OpMetas {
		opMetaIDs[idx] = deterministicNanoID(def.Name, def.Version, "opMeta:"+o.OperationType)
	}

	ops, declared, err := inst.buildInstallBatch(def, pkgKey, ddlIDs, lensIDs, permIDs, roleIDs,
		weaverTargetIDs, loomPatternIDs, opMetaIDs)
	if err != nil {
		return nil, nil, err
	}
	out := make([]InstallMutationForTest, len(ops))
	for idx, op := range ops {
		out[idx] = InstallMutationForTest(op)
	}
	return out, declared, nil
}

// InstallMutationForTest mirrors the internal installMutation so the external
// test package can read emitted keys/documents.
type InstallMutationForTest struct {
	Op       string
	Key      string
	Document map[string]any
}

// DeterministicNanoIDForTest exposes the installer's deterministic NanoID
// minting so tests can recompute the id a given entity will be keyed under.
func DeterministicNanoIDForTest(name, version, tag string) string {
	return deterministicNanoID(name, version, tag)
}
