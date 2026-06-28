package pkgmgr

import (
	"testing"
)

func TestLensSpecBody_NatsKV(t *testing.T) {
	body := lensSpecBody("lens-id-1", LensSpec{
		CanonicalName: "myLens",
		Adapter:       "nats-kv",
		Bucket:        "my-bucket",
		Engine:        "full",
		Spec:          "MATCH (n) RETURN n.key AS key",
	})

	if got := body["targetType"]; got != "nats_kv" {
		t.Errorf("targetType: want nats_kv, got %q", got)
	}
	cfg, ok := body["targetConfig"].(map[string]any)
	if !ok {
		t.Fatalf("targetConfig: not a map")
	}
	if cfg["bucket"] != "my-bucket" {
		t.Errorf("targetConfig.bucket: want my-bucket, got %v", cfg["bucket"])
	}
	if _, hasKey := cfg["key"]; !hasKey {
		t.Error("targetConfig.key: missing")
	}
	if _, hasDSN := cfg["dsn"]; hasDSN {
		t.Error("targetConfig should not contain dsn for nats-kv")
	}
}

func TestLensSpecBody_NatsKV_EmptyAdapterDefaultsToNatsKV(t *testing.T) {
	body := lensSpecBody("lens-id-2", LensSpec{
		CanonicalName: "myLens",
		Adapter:       "",
		Bucket:        "my-bucket",
		Engine:        "full",
		Spec:          "MATCH (n) RETURN n.key AS key",
	})
	if got := body["targetType"]; got != "nats_kv" {
		t.Errorf("targetType: want nats_kv for empty Adapter, got %q", got)
	}
}

func TestLensSpecBody_Postgres(t *testing.T) {
	body := lensSpecBody("lens-id-3", LensSpec{
		CanonicalName: "myPgLens",
		Adapter:       "postgres",
		DSN:           "postgres://localhost/mydb",
		Table:         "my_projection",
		Engine:        "full",
		Spec:          "MATCH (n) RETURN n.key AS key",
		IntoKey:       []string{"key"},
	})

	if got := body["targetType"]; got != "postgres" {
		t.Errorf("targetType: want postgres, got %q", got)
	}
	cfg, ok := body["targetConfig"].(map[string]any)
	if !ok {
		t.Fatalf("targetConfig: not a map")
	}
	if cfg["dsn"] != "postgres://localhost/mydb" {
		t.Errorf("targetConfig.dsn: want postgres://localhost/mydb, got %v", cfg["dsn"])
	}
	if cfg["table"] != "my_projection" {
		t.Errorf("targetConfig.table: want my_projection, got %v", cfg["table"])
	}
	if _, hasBucket := cfg["bucket"]; hasBucket {
		t.Error("targetConfig should not contain bucket for postgres")
	}
	if _, hasTimeout := cfg["queryTimeout"]; hasTimeout {
		t.Error("queryTimeout should be absent when QueryTimeout is empty")
	}
}

func TestLensSpecBody_Postgres_WithQueryTimeout(t *testing.T) {
	body := lensSpecBody("lens-id-4", LensSpec{
		CanonicalName: "myPgLens",
		Adapter:       "postgres",
		DSN:           "postgres://localhost/mydb",
		Table:         "my_projection",
		Engine:        "full",
		Spec:          "MATCH (n) RETURN n.key AS key",
		QueryTimeout:  "10s",
	})
	cfg, ok := body["targetConfig"].(map[string]any)
	if !ok {
		t.Fatalf("targetConfig: not a map")
	}
	if cfg["queryTimeout"] != "10s" {
		t.Errorf("targetConfig.queryTimeout: want 10s, got %v", cfg["queryTimeout"])
	}
}

func TestLensSpecBody_IntoKey_DefaultsToKey(t *testing.T) {
	body := lensSpecBody("lens-id-5", LensSpec{
		CanonicalName: "myLens",
		Adapter:       "nats-kv",
		Bucket:        "bucket",
		Engine:        "full",
		Spec:          "MATCH (n) RETURN n.key AS key",
	})
	cfg := body["targetConfig"].(map[string]any)
	keys, ok := cfg["key"].([]string)
	if !ok {
		t.Fatalf("key: not []string, got %T", cfg["key"])
	}
	if len(keys) != 1 || keys[0] != "key" {
		t.Errorf("key: want [key], got %v", keys)
	}
}

// minimalDDL returns a DDLSpec satisfying buildInstallBatch's self-description
// gate, with the given canonicalName/class/sensitivity.
func minimalDDL(name, class string, sensitive bool) DDLSpec {
	return DDLSpec{
		CanonicalName:    name,
		Class:            class,
		Sensitive:        sensitive,
		Description:      name + " ddl",
		Script:           "def execute(state, op):\n    fail(\"noop\")\n",
		InputSchema:      `{"type":"object"}`,
		OutputSchema:     `{"type":"object"}`,
		FieldDescription: map[string]string{name: "the " + name},
		Examples:         []ExampleSpec{{Name: name, Payload: map[string]any{}, ExpectedOutcome: "ok"}},
	}
}

// findOp returns the install mutation for the given key, or false.
func findOp(ops []installMutation, key string) (installMutation, bool) {
	for _, op := range ops {
		if op.Key == key {
			return op, true
		}
	}
	return installMutation{}, false
}

// TestBuildInstallBatch_SensitiveAspectEmittedOnlyWhenTrue pins Item A: a DDL
// with Sensitive:true emits a `.sensitive` aspect carrying data.value=true; a
// default (Sensitive:false) DDL emits NO `.sensitive` aspect (opt-in
// regression pin — the read side, ddl_cache, treats absent as non-sensitive).
func TestBuildInstallBatch_SensitiveAspectEmittedOnlyWhenTrue(t *testing.T) {
	def := Definition{
		Name:    "sensitive-test-pkg",
		Version: "0.0.1",
		DDLs: []DDLSpec{
			minimalDDL("plainType", "meta.ddl.vertexType", false),
			minimalDDL("secretType", "meta.ddl.aspectType", true),
		},
	}

	inst := &Installer{}
	pkgKey := PackageVertexPrefix + EntityNanoIDForTest(def.Name, "package")
	ddlIDs := []string{
		EntityNanoIDForTest(def.Name, "ddl:plainType"),
		EntityNanoIDForTest(def.Name, "ddl:secretType"),
	}
	ops, _, err := inst.buildInstallBatch(def, pkgKey, ddlIDs, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildInstallBatch: %v", err)
	}

	plainKey := metaVertexPrefix + ddlIDs[0]
	secretKey := metaVertexPrefix + ddlIDs[1]

	// Sensitive DDL: `.sensitive` aspect present with data.value == true.
	sOp, ok := findOp(ops, secretKey+".sensitive")
	if !ok {
		t.Fatalf("sensitive DDL: no %s aspect emitted", secretKey+".sensitive")
	}
	if got := sOp.Document["class"]; got != "sensitive" {
		t.Errorf("sensitive aspect class = %v, want \"sensitive\"", got)
	}
	data, _ := sOp.Document["data"].(map[string]any)
	if v, _ := data["value"].(bool); !v {
		t.Errorf("sensitive aspect data.value = %v, want true", data["value"])
	}

	// Non-sensitive DDL: NO `.sensitive` aspect (the opt-in regression pin).
	if _, ok := findOp(ops, plainKey+".sensitive"); ok {
		t.Errorf("non-sensitive DDL emitted a %s aspect; want none (opt-in)", plainKey+".sensitive")
	}
}
