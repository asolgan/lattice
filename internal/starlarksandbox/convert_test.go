package starlarksandbox

// These tests pin the Go <-> Starlark value marshalling boundary
// (GoValueToStarlark / StarlarkValueToGo / GoMapToStarlarkDict /
// StarlarkDictToGoMap). This is the seam that governs what a DDL script
// reads from a vertex/op payload and what it writes back: a silent
// regression here corrupts script-visible data on the platform's sole
// writer, so the type mappings — especially the JSON-integer preservation
// trick — are pinned directly rather than left to incidental coverage.
// (Migrated verbatim from internal/processor/starlark_runner_test.go —
// this is the seam's new home.)

import (
	"math/big"
	"testing"

	starlarklib "go.starlark.net/starlark"
)

// jsonInteger names the subtle invariant: Go decodes every JSON number as
// float64, but a script comparing `x == 5` or indexing must see an integer
// for whole-valued numbers, so an integral float64 marshals to a Starlark
// Int while a fractional one stays a Float.
func TestGoValueToStarlark_JSONIntegerPreservation(t *testing.T) {
	cases := []struct {
		name    string
		in      interface{}
		wantStr string // Starlark String() rendering
		wantTyp string // Starlark Type()
	}{
		{"integral float decodes as int", float64(5), "5", "int"},
		{"negative integral float as int", float64(-42), "-42", "int"},
		{"zero float as int", float64(0), "0", "int"},
		{"fractional float stays float", 5.5, "5.5", "float"},
		{"native int", 7, "7", "int"},
		{"native int64", int64(99), "99", "int"},
		{"string passthrough", "hi", `"hi"`, "string"},
		{"bool passthrough", true, "True", "bool"},
		{"nil becomes none", nil, "None", "NoneType"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := GoValueToStarlark(tc.in)
			if got.String() != tc.wantStr {
				t.Errorf("String() = %q, want %q", got.String(), tc.wantStr)
			}
			if got.Type() != tc.wantTyp {
				t.Errorf("Type() = %q, want %q", got.Type(), tc.wantTyp)
			}
		})
	}
}

// A typed Go slice/struct is NOT structurally converted — only the
// json.Unmarshal-native []interface{} / map[string]interface{} shapes are.
// Anything else falls through to the Sprintf string fallback, which keeps a
// script from silently consuming a half-converted value.
func TestGoValueToStarlark_NonNativeFallsBackToString(t *testing.T) {
	// A []string is not []interface{}, so it is stringified, not listified.
	got := GoValueToStarlark([]string{"a", "b"})
	if got.Type() != "string" {
		t.Fatalf("typed slice Type() = %q, want string", got.Type())
	}
	if got.String() != `"[a b]"` {
		t.Errorf("typed slice String() = %q, want %q", got.String(), `"[a b]"`)
	}
	// A genuine JSON array round-trips as a list.
	list := GoValueToStarlark([]interface{}{"a", float64(2)})
	if list.Type() != "list" {
		t.Fatalf("[]interface{} Type() = %q, want list", list.Type())
	}
	if list.String() != `["a", 2]` {
		t.Errorf("list String() = %q, want %q", list.String(), `["a", 2]`)
	}
}

func TestStarlarkValueToGo_TypeMapping(t *testing.T) {
	if got := StarlarkValueToGo(starlarklib.None); got != nil {
		t.Errorf("None -> %#v, want nil", got)
	}
	if got := StarlarkValueToGo(starlarklib.String("x")); got != "x" {
		t.Errorf("String -> %#v, want \"x\"", got)
	}
	if got := StarlarkValueToGo(starlarklib.Bool(true)); got != true {
		t.Errorf("Bool -> %#v, want true", got)
	}
	// An Int that fits int64 returns a Go int64.
	if got := StarlarkValueToGo(starlarklib.MakeInt64(123)); got != int64(123) {
		t.Errorf("Int -> %#v (%T), want int64(123)", got, got)
	}
	// A Float returns a Go float64 verbatim — no integer collapse on the way out.
	if got := StarlarkValueToGo(starlarklib.Float(4.0)); got != float64(4.0) {
		t.Errorf("Float -> %#v (%T), want float64(4)", got, got)
	}
}

// An Int wider than int64 cannot be represented as a Go int64, so it
// degrades to its decimal string rather than silently truncating to a
// wrong number.
func TestStarlarkValueToGo_BigIntOverflowToString(t *testing.T) {
	big2pow64 := new(big.Int).Lsh(big.NewInt(1), 64) // 2^64, one past int64/uint64 range
	got := StarlarkValueToGo(starlarklib.MakeBigInt(big2pow64))
	want := big2pow64.String()
	if got != want {
		t.Errorf("overflow Int -> %#v, want string %q", got, want)
	}
}

// A Starlark value with no structural Go analogue (here a tuple) is
// rendered to its String() form rather than dropped or panicking.
func TestStarlarkValueToGo_UnknownTypeToString(t *testing.T) {
	tup := starlarklib.Tuple{starlarklib.MakeInt(1), starlarklib.String("a")}
	got := StarlarkValueToGo(tup)
	if got != tup.String() {
		t.Errorf("tuple -> %#v, want %q", got, tup.String())
	}
}

// StarlarkDictToGoMap only carries string-keyed entries; a non-string key
// (legal in Starlark, illegal as a JSON object key) is skipped, not
// coerced.
func TestStarlarkDictToGoMap_SkipsNonStringKeys(t *testing.T) {
	d := new(starlarklib.Dict)
	if err := d.SetKey(starlarklib.String("ok"), starlarklib.String("v")); err != nil {
		t.Fatal(err)
	}
	if err := d.SetKey(starlarklib.MakeInt(7), starlarklib.String("dropped")); err != nil {
		t.Fatal(err)
	}
	out := StarlarkDictToGoMap(d)
	if len(out) != 1 {
		t.Fatalf("map has %d entries, want 1 (int key skipped): %#v", len(out), out)
	}
	if out["ok"] != "v" {
		t.Errorf("out[ok] = %#v, want \"v\"", out["ok"])
	}
}

// The end-to-end shape a DDL script actually sees: a json.Unmarshal-style
// generic map goes Go -> Starlark -> Go. Strings/bools/nested structure
// survive unchanged; whole-valued JSON numbers come back as int64 (the
// integer preservation trick), fractional ones as float64. Pinning the
// round-trip guards every script that reads a value and writes it back.
func TestMarshalRoundTrip_GenericMap(t *testing.T) {
	in := map[string]interface{}{
		"name":     "alice",
		"active":   true,
		"count":    float64(3),    // whole JSON number
		"ratio":    float64(1.25), // fractional JSON number
		"nested":   map[string]interface{}{"k": "v"},
		"tags":     []interface{}{"a", float64(2)},
		"optional": nil,
	}

	dict := GoMapToStarlarkDict(in)
	out := StarlarkDictToGoMap(dict)

	if out["name"] != "alice" {
		t.Errorf("name = %#v", out["name"])
	}
	if out["active"] != true {
		t.Errorf("active = %#v", out["active"])
	}
	if out["count"] != int64(3) {
		t.Errorf("count = %#v (%T), want int64(3)", out["count"], out["count"])
	}
	if out["ratio"] != float64(1.25) {
		t.Errorf("ratio = %#v (%T), want float64(1.25)", out["ratio"], out["ratio"])
	}
	if out["optional"] != nil {
		t.Errorf("optional = %#v, want nil", out["optional"])
	}
	nested, ok := out["nested"].(map[string]interface{})
	if !ok || nested["k"] != "v" {
		t.Errorf("nested = %#v, want map{k:v}", out["nested"])
	}
	tags, ok := out["tags"].([]interface{})
	if !ok || len(tags) != 2 || tags[0] != "a" || tags[1] != int64(2) {
		t.Errorf("tags = %#v, want [a, int64(2)]", out["tags"])
	}
}
