// cmd/edge — the Edge Lattice reference node (edge-lattice-full-design.md
// EDGE.1): a local-first device that mirrors a single identity's Personal-Lens
// slice into an embedded local store and keeps it fresh via the Sync
// Manager. Trusted-posture only (no JWT, no security filter — the same
// carve-out Loupe and Personal Lens PL.1/PL.2 use); EDGE.3 replaces
// EDGE_ACTOR_KEY with a real Gateway-verified identity.
//
// Environment:
//
//	EDGE_STORE_PATH    path to the local bbolt store file (default: ./edge.db)
//	NATS_URL           NATS server URL (default: nats://localhost:4222)
//	EDGE_IDENTITY_ID    the identity NanoID this node mirrors (required)
//	EDGE_DEVICE_ID      this device's id, distinguishes multiple nodes for
//	                    the same identity (required)
//	EDGE_ACTOR_KEY      the vtx.identity.<id> key stamped as the Lattice-Actor
//	                    header on personal.register/personal.hydrate control
//	                    requests (trusted posture; default: EDGE_IDENTITY_ID
//	                    vertex-keyed)
//
// Logs to stderr in slog text format. Blocks until SIGINT/SIGTERM.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/asolgan/lattice/internal/edge/store"
	"github.com/asolgan/lattice/internal/edge/sync"
	"github.com/asolgan/lattice/internal/substrate"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("edge node exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	storePath := envOrDefault("EDGE_STORE_PATH", "./edge.db")
	natsURL := envOrDefault("NATS_URL", nats.DefaultURL)
	identityID := os.Getenv("EDGE_IDENTITY_ID")
	deviceID := os.Getenv("EDGE_DEVICE_ID")
	if identityID == "" || deviceID == "" {
		return errors.New("EDGE_IDENTITY_ID and EDGE_DEVICE_ID must both be set")
	}
	actorKey := envOrDefault("EDGE_ACTOR_KEY", substrate.VertexKey("identity", identityID))

	st, err := store.Open(storePath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	logger.Info("local VAL store opened", "path", storePath)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	conn, err := substrate.Connect(ctx, substrate.ConnectOpts{
		URL:           natsURL,
		Name:          "edge-" + identityID + "-" + deviceID,
		MaxReconnects: -1,
		ReconnectWait: 2 * time.Second,
		NKeySeedFile:  os.Getenv("NATS_NKEY"),
		CredsFile:     os.Getenv("NATS_CREDS"),
	})
	if err != nil {
		return err
	}
	defer conn.Close()
	logger.Info("connected to NATS", "natsURL", natsURL)

	mgr, err := sync.New(conn, st, sync.Config{
		IdentityID:  identityID,
		DeviceID:    deviceID,
		ActorHeader: actorKey,
		Logger:      logger,
	})
	if err != nil {
		return err
	}

	logger.Info("edge sync manager starting", "identityId", identityID, "deviceId", deviceID)
	return mgr.Run(ctx)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
