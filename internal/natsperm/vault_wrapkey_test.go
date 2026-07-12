package natsperm

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// vaultWrapKeySubject / vaultUnwrapKeySubject are the blob-plane envelope-key
// RPC subjects (internal/vault.WrapKeySubject / UnwrapKeySubject).
// Hardcoded here — like vaultDecryptSubject — so the assertion is against the
// committed deploy/nats-server.conf, not a shared Go constant.
const (
	vaultWrapKeySubject   = "lattice.vault.wrapkey"
	vaultUnwrapKeySubject = "lattice.vault.unwrapkey"
)

// TestVaultWrapUnwrapKeyReachability proves the transport gate for the
// blob-plane envelope-key RPCs (object-store-crypto-shred-design.md §3.1
// Fire 2, §9 Fire 4 Increment 1), mirroring TestVaultDecryptReachability:
// Loupe and loftspace-app — the two named trusted plaintext consumers — may
// reach both subjects, while clinic-app (not a Fire 4 consumer) may not.
func TestVaultWrapUnwrapKeyReachability(t *testing.T) {
	url := startServerFromConf(t)

	resp := connectAs(t, url, "processor")
	for _, subj := range []string{vaultWrapKeySubject, vaultUnwrapKeySubject} {
		sub, err := resp.NATS().Subscribe(subj, func(m *nats.Msg) {
			_ = m.Respond([]byte(`{"key":"b2s="}`))
		})
		if err != nil {
			t.Fatalf("processor subscribe %q: %v", subj, err)
		}
		t.Cleanup(func() { _ = sub.Unsubscribe() })
	}
	if err := resp.NATS().Flush(); err != nil {
		t.Fatalf("flush responder: %v", err)
	}

	for _, trusted := range []string{"loupe", "loftspace-app"} {
		conn := connectAs(t, url, trusted)
		for _, subj := range []string{vaultWrapKeySubject, vaultUnwrapKeySubject} {
			reply, err := conn.NATS().Request(subj, []byte(`{"identityKey":"vtx.identity.x"}`), 3*time.Second)
			if err != nil {
				t.Fatalf("%s request %q: want reply, got %v", trusted, subj, err)
			}
			if len(reply.Data) == 0 {
				t.Fatalf("%s request %q: empty reply", trusted, subj)
			}
		}
	}

	// An ordinary vertical app not named as a Fire 4 consumer is NOT
	// authorized: its publish is rejected at the transport, so the request
	// never reaches the responder and the call times out (the
	// denied-publish signal for a plain request — no reply ever comes).
	rogue := connectAs(t, url, "clinic-app")
	for _, subj := range []string{vaultWrapKeySubject, vaultUnwrapKeySubject} {
		rctx, rcancel := context.WithTimeout(context.Background(), deniedTimeout)
		if _, err := rogue.NATS().RequestWithContext(rctx, subj, []byte(`{"identityKey":"vtx.identity.x"}`)); err == nil {
			t.Errorf("clinic-app request %q: want transport denial (timeout), got a reply", subj)
		}
		rcancel()
	}
}
