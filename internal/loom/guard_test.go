package loom

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestParseGuard_GrammarShapes exercises the §10.5 declarative grammar parser:
// valid atoms/composites parse, malformed shapes reject with errMalformedGuard,
// and the reserved Starlark pair rejects with errStarlarkReserved (distinct).
func TestParseGuard_GrammarShapes(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr error // nil = accept; else errors.Is target
	}{
		// --- valid atoms ---
		{"absent root", `{"absent":"subject.data.name"}`, nil},
		{"absent aspect", `{"absent":"subject.profile.data.name"}`, nil},
		{"present aspect", `{"present":"subject.profile.data.phone"}`, nil},
		{"equals string", `{"equals":{"path":"subject.data.status","value":"active"}}`, nil},
		{"equals number", `{"equals":{"path":"subject.data.count","value":3}}`, nil},
		{"equals bool", `{"equals":{"path":"subject.data.flag","value":false}}`, nil},
		{"equals explicit null", `{"equals":{"path":"subject.data.x","value":null}}`, nil},
		// --- valid composites ---
		{"allOf", `{"allOf":[{"absent":"subject.profile.data.name"},{"present":"subject.data.id"}]}`, nil},
		{"anyOf", `{"anyOf":[{"absent":"subject.data.a"},{"absent":"subject.data.b"}]}`, nil},
		{"not", `{"not":{"absent":"subject.data.name"}}`, nil},
		{"nested composite", `{"allOf":[{"not":{"present":"subject.data.x"}},{"anyOf":[{"absent":"subject.data.y"}]}]}`, nil},

		// --- malformed shapes ---
		{"unknown top key", `{"exists":"subject.data.name"}`, errMalformedGuard},
		{"multi-key object", `{"absent":"subject.data.a","present":"subject.data.b"}`, errMalformedGuard},
		{"zero-key object", `{}`, errMalformedGuard},
		{"bare string", `"subject.data.name"`, errMalformedGuard},
		{"empty allOf", `{"allOf":[]}`, errMalformedGuard},
		{"empty anyOf", `{"anyOf":[]}`, errMalformedGuard},
		{"allOf not array", `{"allOf":{"absent":"subject.data.x"}}`, errMalformedGuard},
		{"equals missing value", `{"equals":{"path":"subject.data.x"}}`, errMalformedGuard},
		{"equals missing path", `{"equals":{"value":"x"}}`, errMalformedGuard},
		{"equals unknown field", `{"equals":{"path":"subject.data.x","value":1,"extra":2}}`, errMalformedGuard},
		{"equals object comparand", `{"equals":{"path":"subject.data.x","value":{"nested":true}}}`, errMalformedGuard},
		{"equals array comparand", `{"equals":{"path":"subject.data.x","value":[1,2,3]}}`, errMalformedGuard},
		{"absent wrong type", `{"absent":123}`, errMalformedGuard},
		{"composite child malformed", `{"allOf":[{"exists":"subject.data.a"}]}`, errMalformedGuard},

		// --- bad path shapes ---
		{"no subject prefix", `{"absent":"profile.data.name"}`, errMalformedGuard},
		{"aspect without data", `{"absent":"subject.profile.name"}`, errMalformedGuard},
		{"too deep", `{"absent":"subject.profile.data.addr.city"}`, errMalformedGuard},
		{"root empty field", `{"absent":"subject.data."}`, errMalformedGuard},
		{"bare subject", `{"absent":"subject"}`, errMalformedGuard},

		// --- reserved starlark ---
		{"starlark full", `{"reads":["profile"],"starlark":"def guard(subject): return True"}`, errStarlarkReserved},
		{"starlark only key", `{"starlark":"def guard(s): return True"}`, errStarlarkReserved},
		{"reads only key", `{"reads":["profile"]}`, errStarlarkReserved},

		// --- duplicate keys (BH-1): encoding/json silently last-wins on a
		// repeated object key; reject at load time instead. ---
		{"duplicate atom key", `{"absent":"subject.data.a","absent":"subject.data.b"}`, errMalformedGuard},
		{"duplicate key inside nested composite", `{"allOf":[{"absent":"subject.data.a","absent":"subject.data.b"}]}`, errMalformedGuard},
		{"duplicate key inside equals body", `{"equals":{"path":"subject.data.a","path":"subject.data.b","value":1}}`, errMalformedGuard},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, err := parseGuard(json.RawMessage(tc.raw))
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("parseGuard(%s) err=%v, want accept", tc.raw, err)
				}
				if g == nil {
					t.Fatalf("parseGuard(%s) returned nil guard with no error", tc.raw)
				}
				return
			}
			if err == nil {
				t.Fatalf("parseGuard(%s) accepted, want %v", tc.raw, tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("parseGuard(%s) err=%v, want errors.Is %v", tc.raw, err, tc.wantErr)
			}
		})
	}
}

// TestParseGuardPath_Shapes pins the two legal path shapes and their (aspect,
// field) decomposition.
func TestParseGuardPath_Shapes(t *testing.T) {
	root, err := parseGuardPath("subject.data.name")
	if err != nil || root.aspect != "" || root.field != "name" {
		t.Fatalf("root path = %+v, err=%v; want {aspect:\"\", field:\"name\"}", root, err)
	}
	asp, err := parseGuardPath("subject.profile.data.phone")
	if err != nil || asp.aspect != "profile" || asp.field != "phone" {
		t.Fatalf("aspect path = %+v, err=%v; want {aspect:\"profile\", field:\"phone\"}", asp, err)
	}
}
