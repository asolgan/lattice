package weaver

import (
	"context"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/asolgan/lattice/internal/substrate"
)

// newStateTestStore starts an embedded NATS server with a TTL-capable
// weaver-state bucket and returns a markStore against it.
func newStateTestStore(t *testing.T, ctx context.Context) *markStore {
	t.Helper()
	opts := &natsserver.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir()}
	srv := natstest.RunServer(opts)
	t.Cleanup(srv.Shutdown)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)
	conn, err := substrate.Wrap(nc)
	if err != nil {
		t.Fatalf("substrate wrap: %v", err)
	}
	js := conn.JetStream()
	if _, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "weaver-state", LimitMarkerTTL: time.Second}); err != nil {
		t.Fatalf("create weaver-state: %v", err)
	}
	return newMarkStore(conn, "weaver-state", time.Minute, "unit-"+testNanoID(t))
}

// TestSetDisabled_RoundTrip verifies setDisabled(true)/isDisabled and
// setDisabled(false)/isDisabled round-trip (AC #3).
func TestSetDisabled_RoundTrip(t *testing.T) {
	ctx := context.Background()
	m := newStateTestStore(t, ctx)

	disabled, err := m.isDisabled(ctx, "t1")
	if err != nil {
		t.Fatalf("isDisabled (initial): %v", err)
	}
	if disabled {
		t.Fatalf("isDisabled (initial) = true, want false")
	}

	if err := m.setDisabled(ctx, "t1", true); err != nil {
		t.Fatalf("setDisabled(true): %v", err)
	}
	disabled, err = m.isDisabled(ctx, "t1")
	if err != nil {
		t.Fatalf("isDisabled (after disable): %v", err)
	}
	if !disabled {
		t.Fatalf("isDisabled (after disable) = false, want true")
	}

	if err := m.setDisabled(ctx, "t1", false); err != nil {
		t.Fatalf("setDisabled(false): %v", err)
	}
	disabled, err = m.isDisabled(ctx, "t1")
	if err != nil {
		t.Fatalf("isDisabled (after enable): %v", err)
	}
	if disabled {
		t.Fatalf("isDisabled (after enable) = true, want false")
	}
}

// TestSetDisabled_IdempotentClear verifies that setDisabled(false) on a
// target that was never disabled is a no-op success (missing-key-is-success,
// mirroring delete's posture).
func TestSetDisabled_IdempotentClear(t *testing.T) {
	ctx := context.Background()
	m := newStateTestStore(t, ctx)

	if err := m.setDisabled(ctx, "never-disabled", false); err != nil {
		t.Fatalf("setDisabled(false) on never-disabled target: %v", err)
	}
	if err := m.setDisabled(ctx, "never-disabled", false); err != nil {
		t.Fatalf("setDisabled(false) twice: %v", err)
	}
	disabled, err := m.isDisabled(ctx, "never-disabled")
	if err != nil {
		t.Fatalf("isDisabled: %v", err)
	}
	if disabled {
		t.Fatalf("isDisabled = true, want false")
	}
}

// TestControlKey_NoCollisionWithMark verifies the reserved-key shape:
// controlKey(targetID) has exactly ONE dot after targetID's segment (a
// single "__control" tail), while a real mark key markKey(targetID, entityID,
// gapColumn) always has TWO dots — so the two key shapes can never collide,
// regardless of entityID/gapColumn values.
func TestControlKey_NoCollisionWithMark(t *testing.T) {
	ck := controlKey("t1")
	if got, want := strings.Count(ck, "."), 1; got != want {
		t.Fatalf("controlKey(%q) = %q has %d dots, want %d", "t1", ck, got, want)
	}
	if !strings.HasSuffix(ck, controlKeySuffix) {
		t.Fatalf("controlKey(%q) = %q does not have suffix %q", "t1", ck, controlKeySuffix)
	}

	mk := markKey("t1", "someEntityID12345678", "missing_foo")
	if got, want := strings.Count(mk, "."), 2; got != want {
		t.Fatalf("markKey(...) = %q has %d dots, want %d", mk, got, want)
	}
	if mk == ck {
		t.Fatalf("markKey and controlKey collided: %q", mk)
	}
}

// TestControlKeySuffix_NotProducibleByNanoID verifies the structural
// safety claim underpinning AC #3's reserved-key shape: "__control" can
// never be produced by substrate.NewNanoID(), because substrate.Alphabet
// contains no underscore. If this ever changes (alphabet gains "_"), this
// test fails loudly — a structural finding to escalate, per Task 1's note,
// though entityIDs are sourced from the projecting Lens, not NewNanoID(),
// so a colliding entityID would itself be a pathological Lens bug
// independent of this story.
func TestControlKeySuffix_NotProducibleByNanoID(t *testing.T) {
	if strings.Contains(substrate.Alphabet, "_") {
		t.Fatalf("substrate.Alphabet contains '_' — __control marker keys may now collide with NanoID-derived entityIDs; escalate as a structural finding")
	}
	if !strings.Contains(controlKeySuffix, "_") {
		t.Fatalf("controlKeySuffix %q does not contain '_' — reserved-shape assumption invalid", controlKeySuffix)
	}
}

// TestDeleteByTargetPrefix_OnlyMatchesOwnTarget verifies that
// deleteByTargetPrefix(ctx, "t1") deletes only keys with prefix "t1." and
// does NOT delete keys belonging to "t10" — proving "t1." is never a prefix
// match for "t10." (the trailing "." in the prefix makes this safe by
// construction; this test confirms it).
func TestDeleteByTargetPrefix_OnlyMatchesOwnTarget(t *testing.T) {
	ctx := context.Background()
	m := newStateTestStore(t, ctx)

	// t1's marks.
	if _, _, err := m.create(ctx, "t1", "entityAAAAAAAAAAAAAA", "missing_a", "vtx.entity.entityAAAAAAAAAAAAAA", "MarkExpired"); err != nil {
		t.Fatalf("create t1 mark: %v", err)
	}
	if err := m.setDisabled(ctx, "t1", true); err != nil {
		t.Fatalf("setDisabled t1: %v", err)
	}

	// t10's marks — must survive deleteByTargetPrefix(ctx, "t1").
	if _, _, err := m.create(ctx, "t10", "entityBBBBBBBBBBBBBB", "missing_b", "vtx.entity.entityBBBBBBBBBBBBBB", "MarkExpired"); err != nil {
		t.Fatalf("create t10 mark: %v", err)
	}
	if err := m.setDisabled(ctx, "t10", true); err != nil {
		t.Fatalf("setDisabled t10: %v", err)
	}

	deleted, err := m.deleteByTargetPrefix(ctx, "t1")
	if err != nil {
		t.Fatalf("deleteByTargetPrefix(t1): %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleteByTargetPrefix(t1) deleted %d keys, want 2 (1 mark + 1 __control)", deleted)
	}

	// t1's mark and control marker are gone.
	if _, _, found, err := m.get(ctx, "t1", "entityAAAAAAAAAAAAAA", "missing_a"); err != nil {
		t.Fatalf("get t1 mark: %v", err)
	} else if found {
		t.Fatalf("t1 mark still present after deleteByTargetPrefix(t1)")
	}
	if disabled, err := m.isDisabled(ctx, "t1"); err != nil {
		t.Fatalf("isDisabled t1: %v", err)
	} else if disabled {
		t.Fatalf("t1 __control marker still present after deleteByTargetPrefix(t1)")
	}

	// t10's mark and control marker survive untouched.
	if _, _, found, err := m.get(ctx, "t10", "entityBBBBBBBBBBBBBB", "missing_b"); err != nil {
		t.Fatalf("get t10 mark: %v", err)
	} else if !found {
		t.Fatalf("t10 mark deleted by deleteByTargetPrefix(t1) — prefix overlap bug")
	}
	if disabled, err := m.isDisabled(ctx, "t10"); err != nil {
		t.Fatalf("isDisabled t10: %v", err)
	} else if !disabled {
		t.Fatalf("t10 __control marker deleted by deleteByTargetPrefix(t1) — prefix overlap bug")
	}
}

// TestDeleteByTargetPrefix_NoKeys verifies deleteByTargetPrefix on a target
// with no weaver-state keys returns (0, nil) — not an error.
func TestDeleteByTargetPrefix_NoKeys(t *testing.T) {
	ctx := context.Background()
	m := newStateTestStore(t, ctx)

	deleted, err := m.deleteByTargetPrefix(ctx, "ghost-target")
	if err != nil {
		t.Fatalf("deleteByTargetPrefix(ghost-target): %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleteByTargetPrefix(ghost-target) deleted %d keys, want 0", deleted)
	}
}
