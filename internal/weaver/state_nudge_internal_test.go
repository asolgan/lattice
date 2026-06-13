package weaver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/asolgan/lattice/internal/substrate"
)

// TestCreateNudge_MintsClaimIDAtomically verifies the §10.3 invariant: the
// claim-minting mark create writes a non-empty, valid-NanoID claimId into the
// mark in the single KVCreateWithTTL that creates the key — so a nudge mark
// ALWAYS carries its claimId, impossible-by-construction.
func TestCreateNudge_MintsClaimIDAtomically(t *testing.T) {
	ctx := context.Background()
	m := newStateTestStore(t, ctx)

	claimID, _, exists, err := m.createNudge(ctx, "t1", "entityAAAAAAAAAAAAAA", "missing_check",
		"vtx.identity.entityAAAAAAAAAAAAAA", actionNudge)
	if err != nil {
		t.Fatalf("createNudge: %v", err)
	}
	if exists {
		t.Fatal("createNudge: unexpected exists=true on a fresh key")
	}
	if claimID == "" {
		t.Fatal("createNudge returned an empty claimId")
	}
	if !substrate.IsValidNanoID(claimID) {
		t.Fatalf("createNudge claimId %q is not a valid NanoID", claimID)
	}

	rec, _, found, err := m.get(ctx, "t1", "entityAAAAAAAAAAAAAA", "missing_check")
	if err != nil || !found {
		t.Fatalf("get nudge mark: found=%v err=%v", found, err)
	}
	if rec.ClaimID != claimID {
		t.Fatalf("mark.ClaimID = %q, want the minted %q (written in the same create)", rec.ClaimID, claimID)
	}
}

// TestCreateNudge_RaceLoserMintsNoClaim verifies that when the CAS-create loses
// the dispatch-OCC race (key already exists), createNudge returns exists=true
// and an empty claimId — no second claim is minted for a key already held.
func TestCreateNudge_RaceLoserMintsNoClaim(t *testing.T) {
	ctx := context.Background()
	m := newStateTestStore(t, ctx)

	if _, _, exists, err := m.createNudge(ctx, "t1", "entityAAAAAAAAAAAAAA", "missing_check",
		"vtx.identity.entityAAAAAAAAAAAAAA", actionNudge); err != nil || exists {
		t.Fatalf("first createNudge: exists=%v err=%v", exists, err)
	}
	claimID, _, exists, err := m.createNudge(ctx, "t1", "entityAAAAAAAAAAAAAA", "missing_check",
		"vtx.identity.entityAAAAAAAAAAAAAA", actionNudge)
	if err != nil {
		t.Fatalf("second createNudge: %v", err)
	}
	if !exists {
		t.Fatal("second createNudge: want exists=true (lost the CAS race)")
	}
	if claimID != "" {
		t.Fatalf("race-losing createNudge minted a claimId %q — must mint none", claimID)
	}
}

// TestNonNudgeCreate_CarriesNoClaimID is the 9.1/9.2 regression guard: a
// non-nudge mark NEVER carries a claimId, and the omitempty keeps the JSON
// identical to the pre-nudge shape (the claimId field is absent).
func TestNonNudgeCreate_CarriesNoClaimID(t *testing.T) {
	ctx := context.Background()
	m := newStateTestStore(t, ctx)

	if _, _, err := m.create(ctx, "t1", "entityAAAAAAAAAAAAAA", "missing_x",
		"vtx.entity.entityAAAAAAAAAAAAAA", actionAssignTask); err != nil {
		t.Fatalf("create: %v", err)
	}
	rec, _, found, err := m.get(ctx, "t1", "entityAAAAAAAAAAAAAA", "missing_x")
	if err != nil || !found {
		t.Fatalf("get mark: found=%v err=%v", found, err)
	}
	if rec.ClaimID != "" {
		t.Fatalf("non-nudge mark carries claimId %q — must be empty", rec.ClaimID)
	}

	// Prove the on-wire JSON omits the claimId key entirely (omitempty).
	entry, err := m.conn.KVGet(ctx, m.bucket, markKey("t1", "entityAAAAAAAAAAAAAA", "missing_x"))
	if err != nil {
		t.Fatalf("raw KVGet: %v", err)
	}
	if containsKey(entry.Value, "claimId") {
		t.Fatalf("non-nudge mark JSON contains a claimId key: %s", entry.Value)
	}
}

// TestReplaceCarryingClaim_ReusesExistingClaimID verifies the reclaim path for
// a nudge mark reuses the EXISTING mark's claimId (never mints a new one): the
// re-armed mark keeps the same join key so recovery resumes the SAME claim.
func TestReplaceCarryingClaim_ReusesExistingClaimID(t *testing.T) {
	ctx := context.Background()
	m := newStateTestStore(t, ctx)

	claimID, rev, _, err := m.createNudge(ctx, "t1", "entityAAAAAAAAAAAAAA", "missing_check",
		"vtx.identity.entityAAAAAAAAAAAAAA", actionNudge)
	if err != nil {
		t.Fatalf("createNudge: %v", err)
	}

	newRev, conflict, err := m.replaceCarryingClaim(ctx, "t1", "entityAAAAAAAAAAAAAA", "missing_check",
		"vtx.identity.entityAAAAAAAAAAAAAA", actionNudge, claimID, rev)
	if err != nil || conflict {
		t.Fatalf("replaceCarryingClaim: conflict=%v err=%v", conflict, err)
	}
	if newRev == rev {
		t.Fatal("replaceCarryingClaim did not advance the revision")
	}

	rec, _, found, err := m.get(ctx, "t1", "entityAAAAAAAAAAAAAA", "missing_check")
	if err != nil || !found {
		t.Fatalf("get reclaimed mark: found=%v err=%v", found, err)
	}
	if rec.ClaimID != claimID {
		t.Fatalf("reclaimed nudge mark.ClaimID = %q, want the original %q (no fresh id)", rec.ClaimID, claimID)
	}
}

// containsKey reports whether the JSON object body has a top-level key.
func containsKey(body []byte, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}
