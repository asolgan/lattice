package natsperm

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// opStatusSubject is the op-status RPC subject (internal/opstatus.Subject).
// Hardcoded here — like the other conformance tests — so the assertion is
// against the committed deploy/nats-server.conf, not a shared Go constant.
const opStatusSubject = "lattice.op.status"

// TestOpStatusReachability proves the transport gate for the op-status RPC
// (op-status-read-surface-design.md Fires 1-4). The responder does NO
// caller-level authorization, so this publish allow-list IS the boundary:
// the bridge (Fire 1), the Gateway (Fire 2), Loom (Fire 3), and the lattice
// CLI (Fire 4) may reach lattice.op.status, while an ordinary vertical app
// may not.
//
// The Processor hosts the responder in production (the sole sanctioned
// Core-KV reader). Here a processor-seed connection stands in as the
// responder: subscribe is unrestricted under the write-only-restriction
// model, and it replies over _INBOX.> (which the processor's publish
// allow-list carries — the same posture TestVaultDecryptReachability relies
// on).
func TestOpStatusReachability(t *testing.T) {
	url := startServerFromConf(t)

	resp := connectAs(t, url, "processor")
	sub, err := resp.NATS().Subscribe(opStatusSubject, func(m *nats.Msg) {
		_ = m.Respond([]byte(`{"found":false}`))
	})
	if err != nil {
		t.Fatalf("processor subscribe %q: %v", opStatusSubject, err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	if err := resp.NATS().Flush(); err != nil {
		t.Fatalf("flush responder: %v", err)
	}

	// The bridge is authorized to publish the request — it gets a reply promptly.
	bridge := connectAs(t, url, "bridge")
	reply, err := bridge.NATS().Request(opStatusSubject, []byte(`{"requestId":"x"}`), 3*time.Second)
	if err != nil {
		t.Fatalf("bridge request %q: want reply, got %v", opStatusSubject, err)
	}
	if len(reply.Data) == 0 {
		t.Fatalf("bridge request %q: empty reply", opStatusSubject)
	}

	// The Gateway is authorized too — GET /v1/operations/{requestId} (Fire 2)
	// backs onto this same RPC.
	gw := connectAs(t, url, "gateway")
	gwReply, err := gw.NATS().Request(opStatusSubject, []byte(`{"requestId":"x"}`), 3*time.Second)
	if err != nil {
		t.Fatalf("gateway request %q: want reply, got %v", opStatusSubject, err)
	}
	if len(gwReply.Data) == 0 {
		t.Fatalf("gateway request %q: empty reply", opStatusSubject)
	}

	// Loom is authorized too — the §10.6 deadline+probe's tracker read (Fire 3)
	// backs onto this same RPC.
	loom := connectAs(t, url, "loom")
	loomReply, err := loom.NATS().Request(opStatusSubject, []byte(`{"requestId":"x"}`), 3*time.Second)
	if err != nil {
		t.Fatalf("loom request %q: want reply, got %v", opStatusSubject, err)
	}
	if len(loomReply.Data) == 0 {
		t.Fatalf("loom request %q: empty reply", opStatusSubject)
	}

	// The lattice CLI is authorized too — `lattice op status` (Fire 4) backs
	// onto this same RPC, replacing its former raw Core-KV tracker KVGet.
	cli := connectAs(t, url, "lattice")
	cliReply, err := cli.NATS().Request(opStatusSubject, []byte(`{"requestId":"x"}`), 3*time.Second)
	if err != nil {
		t.Fatalf("lattice request %q: want reply, got %v", opStatusSubject, err)
	}
	if len(cliReply.Data) == 0 {
		t.Fatalf("lattice request %q: empty reply", opStatusSubject)
	}

	// An ordinary vertical app is NOT authorized: its publish is rejected at the
	// transport, so the request never reaches the responder and the call times
	// out (the denied-publish signal for a plain request — no reply ever comes).
	rogue := connectAs(t, url, "clinic-app")
	rctx, rcancel := context.WithTimeout(context.Background(), deniedTimeout)
	defer rcancel()
	if _, err := rogue.NATS().RequestWithContext(rctx, opStatusSubject, []byte(`{"requestId":"x"}`)); err == nil {
		t.Errorf("clinic-app request %q: want transport denial (timeout), got a reply", opStatusSubject)
	}
}
