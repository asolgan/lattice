package main

import (
	"strings"
	"testing"
)

// The Edge fleet logic tier (F19): gap classification, Interest Set phrasing,
// identity grouping, and the headline/retention/caveat lines — asserted against
// the shipped embedded asset via the goja harness.
//
// The recurring assertion: a gap the platform CANNOT determine must read as
// unknown, never as a clean "current". A device that never attached has no
// comparable position; saying "within retention window" there would tell an
// operator the device is caught up when the truth is that nothing was measured.

func edgeDeviceJS(over map[string]any) map[string]any {
	d := map[string]any{
		"identityId":  "AAAAAAAAAAAAAAAAAAAA",
		"identityKey": "vtx.identity.AAAAAAAAAAAAAAAAAAAA",
		"deviceId":    "phone",
		"gapped":      nil,
		"subscribed":  false,
	}
	for k, v := range over {
		d[k] = v
	}
	return d
}

func TestGapVerdict(t *testing.T) {
	vm := logicVM(t, "edge.js")

	// A nil/absent gapped field is UNKNOWN — the load-bearing rule.
	for _, d := range []any{
		edgeDeviceJS(nil),
		edgeDeviceJS(map[string]any{"gapped": nil, "subscribed": true, "ackFloor": int64(900)}),
		map[string]any{},
		nil,
	} {
		got, ok := call(t, vm, "gapVerdict", d).(map[string]any)
		if !ok {
			t.Fatalf("gapVerdict(%v) did not return an object", d)
		}
		if got["state"] != "unknown" {
			t.Errorf("gapVerdict(%v).state = %v, want unknown", d, got["state"])
		}
	}

	gapped := call(t, vm, "gapVerdict", edgeDeviceJS(map[string]any{
		"gapped": true, "behindBy": int64(100), "subscribed": true,
	})).(map[string]any)
	if gapped["state"] != "gapped" {
		t.Errorf("state = %v, want gapped", gapped["state"])
	}
	if numVal(t, gapped["behindBy"]) != 100 {
		t.Errorf("behindBy = %v, want 100", gapped["behindBy"])
	}

	current := call(t, vm, "gapVerdict", edgeDeviceJS(map[string]any{
		"gapped": false, "subscribed": true,
	})).(map[string]any)
	if current["state"] != "current" {
		t.Errorf("state = %v, want current", current["state"])
	}
	if numVal(t, current["behindBy"]) != 0 {
		t.Errorf("a current device is behind by 0, got %v", current["behindBy"])
	}
}

// An unknown verdict must name WHY it is unknown — bare "unknown" reads as a
// bug rather than a fact about the platform.
func TestGapLabelExplainsUnknown(t *testing.T) {
	vm := logicVM(t, "edge.js")

	unreadable := call(t, vm, "gapLabel", edgeDeviceJS(map[string]any{"subscribed": true}), false).(string)
	if !strings.Contains(unreadable, "unreadable") {
		t.Errorf("gapLabel with no stream = %q, want it to name the unreadable stream", unreadable)
	}

	never := call(t, vm, "gapLabel", edgeDeviceJS(nil), true).(string)
	if !strings.Contains(never, "never attached") {
		t.Errorf("gapLabel for a never-attached device = %q, want it to say so", never)
	}

	gapped := call(t, vm, "gapLabel", edgeDeviceJS(map[string]any{
		"gapped": true, "behindBy": int64(1), "subscribed": true,
	}), true).(string)
	if !strings.Contains(gapped, "1 message aged out") {
		t.Errorf("gapLabel singular = %q, want singular phrasing", gapped)
	}

	current := call(t, vm, "gapLabel", edgeDeviceJS(map[string]any{
		"gapped": false, "subscribed": true,
	}), true).(string)
	if !strings.Contains(current, "within retention") {
		t.Errorf("gapLabel current = %q", current)
	}
}

// "Absence is never a denial" — an empty Interest Set is a WIDER subscription,
// not a narrower one. Phrasing it as "no interests" would invert its meaning.
func TestInterestSummaryEmptyMeansEverything(t *testing.T) {
	vm := logicVM(t, "edge.js")

	for _, d := range []any{
		edgeDeviceJS(nil),
		edgeDeviceJS(map[string]any{"types": []any{}, "anchors": []any{}}),
		nil,
	} {
		got := call(t, vm, "interestSummary", d).(string)
		if !strings.Contains(got, "unfiltered") || !strings.Contains(got, "everything") {
			t.Errorf("interestSummary(%v) = %q, want it to read as unfiltered/everything", d, got)
		}
	}

	filtered := call(t, vm, "interestSummary", edgeDeviceJS(map[string]any{
		"types": []any{"lease", "task"},
	})).(string)
	if !strings.Contains(filtered, "2 types") || !strings.Contains(filtered, "lease, task") {
		t.Errorf("interestSummary filtered = %q", filtered)
	}

	both := call(t, vm, "interestSummary", edgeDeviceJS(map[string]any{
		"types": []any{"lease"}, "anchors": []any{"vtx.location.x"},
	})).(string)
	if !strings.Contains(both, "1 type") || !strings.Contains(both, "1 anchor") {
		t.Errorf("interestSummary both = %q", both)
	}
}

// Grouping preserves the server's gapped-first ordering: an identity with a
// gapped device must not sink below a healthy one just because it sorts later.
func TestGroupByIdentityPreservesOrder(t *testing.T) {
	vm := logicVM(t, "edge.js")

	devices := []any{
		edgeDeviceJS(map[string]any{"identityId": "ZZZ", "identityKey": "vtx.identity.ZZZ", "deviceId": "a", "gapped": true}),
		edgeDeviceJS(map[string]any{"identityId": "AAA", "identityKey": "vtx.identity.AAA", "deviceId": "b", "gapped": false}),
		edgeDeviceJS(map[string]any{"identityId": "ZZZ", "identityKey": "vtx.identity.ZZZ", "deviceId": "c", "gapped": false}),
	}
	groups, ok := call(t, vm, "groupByIdentity", devices).([]any)
	if !ok {
		t.Fatal("groupByIdentity did not return an array")
	}
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	first := groups[0].(map[string]any)
	if first["identityId"] != "ZZZ" {
		t.Errorf("first group = %v, want the gapped identity ZZZ first", first["identityId"])
	}
	if numVal(t, first["gapped"]) != 1 {
		t.Errorf("ZZZ gapped count = %v, want 1", first["gapped"])
	}
	if got := len(first["devices"].([]any)); got != 2 {
		t.Errorf("ZZZ device count = %d, want 2", got)
	}
	if numVal(t, groups[1].(map[string]any)["gapped"]) != 0 {
		t.Error("AAA must report 0 gapped")
	}
}

// The headline must not fold unmeasured devices into a healthy count.
func TestFleetHeadline(t *testing.T) {
	vm := logicVM(t, "edge.js")

	empty := call(t, vm, "fleetHeadline", map[string]any{"count": int64(0)}).(string)
	if !strings.Contains(empty, "No devices") {
		t.Errorf("empty headline = %q", empty)
	}

	// No readable stream ⇒ the headline says the gap state is unknown for ALL,
	// and never prints a "0 gapped" all-clear.
	noStream := call(t, vm, "fleetHeadline", map[string]any{
		"count": int64(3), "identities": int64(2), "streamKnown": false, "gapped": int64(0),
	}).(string)
	if !strings.Contains(noStream, "unknown") {
		t.Errorf("no-stream headline = %q, want it to report unknown", noStream)
	}
	if strings.Contains(noStream, "0 gapped") {
		t.Errorf("no-stream headline = %q, must not claim an all-clear", noStream)
	}

	known := call(t, vm, "fleetHeadline", map[string]any{
		"count": int64(3), "identities": int64(1), "streamKnown": true,
		"gapped": int64(1), "unsubscribed": int64(1), "stream": "SYNC",
	}).(string)
	for _, want := range []string{"3 devices", "1 identity", "1 gapped", "not attached"} {
		if !strings.Contains(known, want) {
			t.Errorf("headline %q missing %q", known, want)
		}
	}
}

func TestRetentionAndStaleLines(t *testing.T) {
	vm := logicVM(t, "edge.js")

	// No stream ⇒ no retention claim at all, rather than a 0–0 window.
	if got := call(t, vm, "retentionLine", map[string]any{"streamKnown": false}).(string); got != "" {
		t.Errorf("retentionLine without a stream = %q, want empty", got)
	}
	line := call(t, vm, "retentionLine", map[string]any{
		"streamKnown": true, "stream": "SYNC", "firstSeq": int64(500), "lastSeq": int64(600),
	}).(string)
	for _, want := range []string{"SYNC", "500", "600", "101 messages"} {
		if !strings.Contains(line, want) {
			t.Errorf("retentionLine %q missing %q", line, want)
		}
	}

	// The standing caveat only appears when there is a roster to caveat, and it
	// must state that this is registration, not liveness.
	if got := call(t, vm, "staleWarning", map[string]any{"count": int64(0)}).(string); got != "" {
		t.Errorf("staleWarning on an empty fleet = %q, want empty", got)
	}
	warn := call(t, vm, "staleWarning", map[string]any{"count": int64(2)}).(string)
	for _, want := range []string{"never expire", "not who is connected"} {
		if !strings.Contains(warn, want) {
			t.Errorf("staleWarning %q missing %q", warn, want)
		}
	}
}

// A red chip saying "0 messages aged out" reads as a contradiction, so the
// retention boundary names itself instead.
func TestGapLabelRetentionBoundary(t *testing.T) {
	vm := logicVM(t, "edge.js")
	got := call(t, vm, "gapLabel", edgeDeviceJS(map[string]any{
		"gapped": true, "behindBy": int64(0), "subscribed": true,
	}), true).(string)
	if !strings.Contains(got, "retention boundary") {
		t.Errorf("boundary label = %q, want it to name the boundary", got)
	}
	if strings.Contains(got, "0 message") {
		t.Errorf("boundary label = %q, must not claim 0 messages aged out", got)
	}
}

// An unreadable registration document must not be reported as the WIDEST
// possible subscription — that is a security-relevant claim from no evidence.
func TestInterestSummaryMalformedIsNotUnfiltered(t *testing.T) {
	vm := logicVM(t, "edge.js")
	got := call(t, vm, "interestSummary", edgeDeviceJS(map[string]any{"malformed": true})).(string)
	if strings.Contains(got, "unfiltered") || strings.Contains(got, "everything") {
		t.Errorf("malformed interest = %q, must not assert an unfiltered subscription", got)
	}
	if !strings.Contains(got, "unknown") {
		t.Errorf("malformed interest = %q, want it to read as unknown", got)
	}
}

// The headline must never print a gapped count that reads as an all-clear when
// devices went unmeasured.
func TestFleetHeadlineSurfacesUnknown(t *testing.T) {
	vm := logicVM(t, "edge.js")

	// Stream readable, but no device had a durable: every row is unmeasured, so
	// "0 gapped" would be an unearned all-clear.
	allUnknown := call(t, vm, "fleetHeadline", map[string]any{
		"count": int64(5), "identities": int64(2), "streamKnown": true,
		"gapped": int64(0), "unknown": int64(5),
	}).(string)
	if strings.Contains(allUnknown, "0 gapped") {
		t.Errorf("headline = %q, must not claim an all-clear when nothing was measured", allUnknown)
	}
	if !strings.Contains(allUnknown, "unknown for all") {
		t.Errorf("headline = %q, want it to report the fleet as unmeasured", allUnknown)
	}

	// Partially measured: the unmeasured remainder rides alongside the count.
	partial := call(t, vm, "fleetHeadline", map[string]any{
		"count": int64(5), "identities": int64(2), "streamKnown": true,
		"gapped": int64(1), "unknown": int64(2),
	}).(string)
	for _, want := range []string{"1 gapped", "2 unknown"} {
		if !strings.Contains(partial, want) {
			t.Errorf("headline %q missing %q", partial, want)
		}
	}
}

// An empty "gapped only" list is not an all-clear when rows were hidden because
// their gap state could not be determined.
func TestFilterEmptyMessage(t *testing.T) {
	vm := logicVM(t, "edge.js")

	clean := call(t, vm, "filterEmptyMessage", []any{
		edgeDeviceJS(map[string]any{"gapped": false}),
	}).(string)
	if clean != "(no gapped devices)" {
		t.Errorf("all-measured empty filter = %q", clean)
	}

	hidden := call(t, vm, "filterEmptyMessage", []any{
		edgeDeviceJS(map[string]any{"gapped": false}),
		edgeDeviceJS(nil),
		edgeDeviceJS(nil),
	}).(string)
	if !strings.Contains(hidden, "not an all-clear") || !strings.Contains(hidden, "2 device") {
		t.Errorf("filter with hidden unknowns = %q, want the unknown count stated", hidden)
	}
}

// An empty stream must not render as an inverted or zero-width range.
func TestRetentionLineEmptyStream(t *testing.T) {
	vm := logicVM(t, "edge.js")
	for _, f := range []map[string]any{
		{"streamKnown": true, "stream": "SYNC", "firstSeq": int64(101), "lastSeq": int64(100)},
		{"streamKnown": true, "stream": "SYNC", "firstSeq": int64(0), "lastSeq": int64(0)},
	} {
		got := call(t, vm, "retentionLine", f).(string)
		if !strings.Contains(got, "holds no messages") {
			t.Errorf("retentionLine(%v) = %q, want it to read as empty", f, got)
		}
		if strings.Contains(got, "–") {
			t.Errorf("retentionLine(%v) = %q, must not print a range", f, got)
		}
	}
}

// The hydration checkpoint is a Refractor pipeline sequence, not a SYNC
// position — the label must say so, or it reads as a second, contradictory
// sync cursor next to the ack floor.
func TestHydrationNoteNamesItsSequenceSpace(t *testing.T) {
	vm := logicVM(t, "edge.js")
	if got := call(t, vm, "hydrationNote", edgeDeviceJS(nil)); got != "" {
		t.Errorf("hydrationNote with no cursor = %v, want empty", got)
	}
	got := call(t, vm, "hydrationNote", edgeDeviceJS(map[string]any{"revisionCursor": int64(2487)})).(string)
	if !strings.Contains(got, "pipeline seq") || !strings.Contains(got, "2487") {
		t.Errorf("hydrationNote = %q, want it to name the pipeline sequence space", got)
	}
}
