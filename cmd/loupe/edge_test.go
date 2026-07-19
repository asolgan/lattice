package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docSet builds the readDoc callback computeEdgeFleet takes, from a plain map.
// A key absent from the map reads as "gone" (raced a deregister).
func docSet(docs map[string]interestDoc) func(string) (interestDoc, bool, bool) {
	return func(k string) (interestDoc, bool, bool) {
		d, ok := docs[k]
		if !ok {
			return interestDoc{}, false, false
		}
		return d, true, true
	}
}

// consumerSet builds the readConsumer callback from durable-name → state.
func consumerSet(cs map[string]consumerState) func(string, string) (consumerState, bool) {
	return func(identityID, deviceID string) (consumerState, bool) {
		c, ok := cs[edgeSyncDurable(identityID, deviceID)]
		return c, ok
	}
}

func TestSplitInterestKey(t *testing.T) {
	id, dev, ok := splitInterestKey("AAAAAAAAAAAAAAAAAAAA.phone-1")
	require.True(t, ok)
	assert.Equal(t, "AAAAAAAAAAAAAAAAAAAA", id)
	assert.Equal(t, "phone-1", dev)

	// The device half may itself contain dots — only the identity half is a
	// subject token, so the split is on the FIRST dot, not the last.
	id, dev, ok = splitInterestKey("AAAAAAAAAAAAAAAAAAAA.chrome.macos.1")
	require.True(t, ok)
	assert.Equal(t, "AAAAAAAAAAAAAAAAAAAA", id)
	assert.Equal(t, "chrome.macos.1", dev)

	for _, bad := range []string{"", "nodot", ".leading", "trailing."} {
		_, _, ok := splitInterestKey(bad)
		assert.False(t, ok, "key %q must not split", bad)
	}
}

// The durable name must match internal/edge/sync's construction by value, or
// every device looks unattached and the whole fleet reads as unmeasurable.
func TestEdgeSyncDurableMatchesProducer(t *testing.T) {
	assert.Equal(t, "edge-sync-MQsmTTAgNkngkdEjQz9L-BHrdHRUWXPkLiukEvK9e",
		edgeSyncDurable("MQsmTTAgNkngkdEjQz9L", "BHrdHRUWXPkLiukEvK9e"))
}

// A device whose durable has consumed past the stream's retention floor is
// healthy; one the floor has overtaken is gapped, and BehindBy counts the
// messages actually lost.
func TestComputeEdgeFleet_GapFromAckFloor(t *testing.T) {
	const idA, idB = "AAAAAAAAAAAAAAAAAAAA", "BBBBBBBBBBBBBBBBBBBB"
	keys := []string{idA + ".phone", idB + ".laptop"}
	docs := map[string]interestDoc{
		idA + ".phone":  {Types: []string{"lease"}, RegisteredAt: "2026-07-19T00:00:00Z"},
		idB + ".laptop": {RegisteredAt: "2026-07-19T00:00:00Z"},
	}
	cons := map[string]consumerState{
		edgeSyncDurable(idA, "phone"):  {AckFloor: 400, Pending: 12}, // behind the floor
		edgeSyncDurable(idB, "laptop"): {AckFloor: 900, Pending: 0},  // inside the window
	}

	devices, gapped, unsubscribed, _ := computeEdgeFleet(keys, docSet(docs), consumerSet(cons), 500, true)
	require.Len(t, devices, 2)
	assert.Equal(t, 1, gapped)
	assert.Equal(t, 0, unsubscribed)

	// Gapped sorts first regardless of identity ordering.
	g := devices[0]
	assert.Equal(t, idA, g.IdentityID)
	assert.Equal(t, "vtx.identity."+idA, g.IdentityKey)
	require.NotNil(t, g.Gapped)
	assert.True(t, *g.Gapped)
	// Lost messages are (400, 500) exclusive — 401..499, i.e. 99, not 100.
	assert.Equal(t, uint64(99), g.BehindBy)
	assert.True(t, g.Subscribed)
	assert.Equal(t, uint64(12), g.Pending)
	assert.False(t, g.Unfiltered, "a declared type is a filter")

	ok := devices[1]
	require.NotNil(t, ok.Gapped)
	assert.False(t, *ok.Gapped)
	assert.Zero(t, ok.BehindBy)
	assert.True(t, ok.Unfiltered, "no types and no anchors admits everything")
}

// Gapped means data was actually lost — at least one message strictly between
// the device's ack floor and the oldest sequence still retained.
//
// This deliberately diverges from the platform's syncgap predicate
// (`cursor < firstSeq`), which also fires at the boundary where nothing was
// lost. That conservatism is correct for a device deciding whether to
// re-hydrate (cost: one redundant hydrate) and wrong for an operator's triage
// metric: the SYNC stream is MaxAge-limited, so a stack idle past the window
// ages to empty and reports firstSeq = lastSeq+1, at which point every
// caught-up device would satisfy the platform predicate and the whole fleet
// would render red with nothing wrong. These cases pin that divergence.
func TestComputeEdgeFleet_GappedMeansDataActuallyLost(t *testing.T) {
	const id = "AAAAAAAAAAAAAAAAAAAA"
	run := func(ackFloor, firstSeq uint64) edgeDevice {
		devices, _, _, _ := computeEdgeFleet(
			[]string{id + ".phone"},
			docSet(map[string]interestDoc{id + ".phone": {}}),
			consumerSet(map[string]consumerState{edgeSyncDurable(id, "phone"): {AckFloor: ackFloor}}),
			firstSeq, true)
		require.Len(t, devices, 1)
		return devices[0]
	}
	notGapped := func(t *testing.T, d edgeDevice, why string) {
		t.Helper()
		require.NotNil(t, d.Gapped, why)
		assert.False(t, *d.Gapped, why)
		assert.Zero(t, d.BehindBy)
	}

	// Fully caught up: the floor has not passed the device at all.
	notGapped(t, run(500, 500), "ack floor at the retention floor lost nothing")

	// The boundary: oldest retained is exactly the next message the device
	// wants. Nothing in between, so nothing lost — this is the case that would
	// otherwise turn an idle stack's entire fleet red.
	notGapped(t, run(499, 500), "ack floor one below the retention floor lost nothing")

	// An attached device that never acked, on a stream retaining from seq 1:
	// message 1 is still there, so it has missed nothing yet.
	notGapped(t, run(0, 1), "nothing has aged out of a stream retaining from 1")

	// An empty stream leaves nothing to be behind.
	notGapped(t, run(0, 0), "an empty stream cannot have discarded anything")

	// One message strictly between (seq 499) actually aged out.
	lostOne := run(498, 500)
	require.NotNil(t, lostOne.Gapped)
	assert.True(t, *lostOne.Gapped)
	assert.Equal(t, uint64(1), lostOne.BehindBy)

	// A never-acked device on a stream that has already discarded seq 1.
	lostFromZero := run(0, 2)
	require.NotNil(t, lostFromZero.Gapped)
	assert.True(t, *lostFromZero.Gapped)
	assert.Equal(t, uint64(1), lostFromZero.BehindBy)
}

// The unknown counter must account for every device whose gap state was not
// determined, so the headline can refuse to print an all-clear it did not earn.
func TestComputeEdgeFleet_UnknownCounted(t *testing.T) {
	const id = "AAAAAAAAAAAAAAAAAAAA"
	keys := []string{id + ".attached", id + ".never"}
	docs := map[string]interestDoc{id + ".attached": {}, id + ".never": {}}
	cons := map[string]consumerState{edgeSyncDurable(id, "attached"): {AckFloor: 900}}

	_, _, _, unknown := computeEdgeFleet(keys, docSet(docs), consumerSet(cons), 500, true)
	assert.Equal(t, 1, unknown, "only the unattached device is unmeasured")

	// No readable stream ⇒ nothing is measurable at all.
	_, _, _, allUnknown := computeEdgeFleet(keys, docSet(docs), consumerSet(cons), 0, false)
	assert.Equal(t, 2, allUnknown)
}

// The load-bearing honesty rule: an unanswerable gap question must read as
// unknown (nil), never as a clean false.
func TestComputeEdgeFleet_UnknownIsNeverFalse(t *testing.T) {
	const id = "AAAAAAAAAAAAAAAAAAAA"

	t.Run("stream unreadable", func(t *testing.T) {
		docs := map[string]interestDoc{id + ".phone": {RevisionCursor: 900}}
		cons := map[string]consumerState{edgeSyncDurable(id, "phone"): {AckFloor: 900}}
		devices, gapped, unsubscribed, _ := computeEdgeFleet([]string{id + ".phone"}, docSet(docs), consumerSet(cons), 0, false)
		require.Len(t, devices, 1)
		assert.Nil(t, devices[0].Gapped, "no readable stream ⇒ no verdict")
		assert.Zero(t, gapped)
		// Attachment is read THROUGH the stream, so with no stream it is
		// unknown rather than absent — counting it as unattached would assert
		// something never measured.
		assert.Zero(t, unsubscribed, "attachment is unmeasurable without the stream")
	})

	t.Run("registered but never attached", func(t *testing.T) {
		docs := map[string]interestDoc{id + ".phone": {RegisteredAt: "2026-07-19T00:00:00Z"}}
		devices, gapped, unsubscribed, _ := computeEdgeFleet([]string{id + ".phone"}, docSet(docs), consumerSet(nil), 500, true)
		require.Len(t, devices, 1)
		assert.Nil(t, devices[0].Gapped, "no durable ⇒ no comparable position")
		assert.False(t, devices[0].Subscribed)
		assert.Zero(t, gapped)
		assert.Equal(t, 1, unsubscribed)
	})
}

// revisionCursor is the Refractor pipeline's LastAppliedSeq — a position in the
// Core-KV change stream, NOT a SYNC sequence. Comparing it to the SYNC
// retention floor would manufacture gaps out of unrelated counters, so it must
// never produce a verdict however far apart the two numbers are.
func TestComputeEdgeFleet_RevisionCursorNeverProducesVerdict(t *testing.T) {
	const id = "AAAAAAAAAAAAAAAAAAAA"
	docs := map[string]interestDoc{id + ".phone": {RevisionCursor: 2487}}
	// 2487 sits far below a realistic SYNC floor of 8355 — the exact shape that
	// would read as "gapped by 5867" if the two sequence spaces were conflated.
	devices, gapped, _, _ := computeEdgeFleet([]string{id + ".phone"}, docSet(docs), consumerSet(nil), 8355, true)
	require.Len(t, devices, 1)
	assert.Nil(t, devices[0].Gapped, "a hydration checkpoint is not a stream position")
	assert.Zero(t, devices[0].BehindBy)
	assert.Zero(t, gapped)
	// It is still carried for display.
	assert.Equal(t, uint64(2487), devices[0].RevisionCursor)
}

// Rows that cannot be attributed to an identity are dropped, not rendered
// unattributed; a row whose doc raced a deregister drops; a row whose doc is
// unreadable survives, flagged, so a read fault cannot silently shorten the
// roster.
func TestComputeEdgeFleet_MalformedAndRacedRows(t *testing.T) {
	const id = "AAAAAAAAAAAAAAAAAAAA"
	keys := []string{"nodot", id + ".gone", id + ".broken"}
	readDoc := func(k string) (interestDoc, bool, bool) {
		switch k {
		case id + ".gone":
			return interestDoc{}, false, false // deregistered mid-page
		case id + ".broken":
			return interestDoc{}, true, false // present but unreadable
		}
		return interestDoc{}, false, false
	}
	devices, _, _, _ := computeEdgeFleet(keys, readDoc, consumerSet(nil), 500, true)
	require.Len(t, devices, 1, "only the unreadable-but-present row survives")
	assert.Equal(t, id+".broken", devices[0].Key)
	assert.True(t, devices[0].Malformed)
	assert.Nil(t, devices[0].Gapped)
	// An unreadable doc must not be reported as an unfiltered (widest)
	// subscription — that would be a security-relevant claim from no evidence.
	assert.False(t, devices[0].Unfiltered)
}

// The SYNC stream is discovered from the installed personal lens specs, never
// assumed — and an ambiguous or absent discovery yields a note, not a guess.
func TestPersonalSyncStream(t *testing.T) {
	spec := func(m map[string]lensSpecInfo) func(string) lensSpecInfo {
		return func(id string) lensSpecInfo { return m[id] }
	}
	// Health keys: bare NanoIDs are lens reporters; "health.*" keys are not.
	lensA, lensB := "AAAAAAAAAAAAAAAAAAAA", "BBBBBBBBBBBBBBBBBBBB"

	t.Run("one personal lens", func(t *testing.T) {
		got, note := personalSyncStream([]string{lensA, "health.refractor.r1"}, spec(map[string]lensSpecInfo{
			lensA: {TargetType: "nats_subject", Personal: true, Stream: "SYNC"},
		}))
		assert.Equal(t, "SYNC", got)
		assert.Empty(t, note)
	})

	t.Run("non-personal nats_subject lens is not the personal stream", func(t *testing.T) {
		got, note := personalSyncStream([]string{lensA}, spec(map[string]lensSpecInfo{
			lensA: {TargetType: "nats_subject", Personal: false, Stream: "BROADCAST"},
		}))
		assert.Empty(t, got)
		assert.Contains(t, note, "No Personal Lens")
	})

	t.Run("several personal lenses sharing one stream", func(t *testing.T) {
		got, note := personalSyncStream([]string{lensA, lensB}, spec(map[string]lensSpecInfo{
			lensA: {TargetType: "nats_subject", Personal: true, Stream: "SYNC"},
			lensB: {TargetType: "nats_subject", Personal: true, Stream: "SYNC"},
		}))
		assert.Equal(t, "SYNC", got)
		assert.Empty(t, note)
	})

	t.Run("ambiguous streams refuse a verdict", func(t *testing.T) {
		got, note := personalSyncStream([]string{lensA, lensB}, spec(map[string]lensSpecInfo{
			lensA: {TargetType: "nats_subject", Personal: true, Stream: "SYNC"},
			lensB: {TargetType: "nats_subject", Personal: true, Stream: "SYNC2"},
		}))
		assert.Empty(t, got, "ambiguity must not resolve to an arbitrary stream")
		assert.Contains(t, note, "more than one stream")
	})
}
