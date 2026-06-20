// cmd/object-store-manager — Lattice object-store-manager binary (the v1b GC
// Loop B byte-janitor).
//
// Connects to NATS and runs the object-store-manager: a durable consumer on
// events.object.tombstoned that reclaims a tombstoned object's bytes (after an
// authoritative core-kv re-check), plus a low-cadence never-attached reconcile.
// It submits NO ops (the graph tombstone is Weaver's directOp) — it only deletes
// bytes — so it needs no actor key. Shares only internal/substrate with the rest
// of the platform.
//
// Environment:
//
//	NATS_URL              NATS server URL (default: nats://localhost:4222)
//	OBJMGR_INSTANCE       instance id (default: auto-generated objmgr-<NanoID>)
//	OBJMGR_RECONCILE_EVERY  reconcile cadence as a Go duration (default: 1h)
//	OBJMGR_RECONCILE_GRACE  spare bytes younger than this Go duration (default: 25h, > the 24h tracker TTL)
//
// Logs to stderr in slog text format. Exits non-zero on startup failure;
// graceful shutdown on SIGINT/SIGTERM.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/asolgan/lattice/internal/bootstrap"
	"github.com/asolgan/lattice/internal/objectmanager"
	"github.com/asolgan/lattice/internal/substrate"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("object-store-manager exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	natsURL := envOrDefault("NATS_URL", nats.DefaultURL)

	instance := os.Getenv("OBJMGR_INSTANCE")
	if instance == "" {
		id, err := substrate.NewNanoID()
		if err != nil {
			return fmt.Errorf("generate instance id: %w", err)
		}
		instance = "objmgr-" + id
	}

	logger.Info("object-store-manager starting", "natsURL", natsURL, "instance", instance)

	conn, err := substrate.Connect(context.Background(), substrate.ConnectOpts{
		URL:           natsURL,
		Name:          "lattice-object-store-manager:" + instance,
		MaxReconnects: -1,
		ReconnectWait: 1 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("substrate connect: %w", err)
	}
	defer conn.Close()

	mgr := objectmanager.New(objectmanager.Config{
		Conn:              conn,
		CoreKVBucket:      bootstrap.CoreKVBucket,
		ObjectsBucket:     bootstrap.CoreObjectsBucket,
		EventsStream:      bootstrap.CoreEventsStreamName,
		Durable:           objectmanager.DefaultDurable,
		ReconcileInterval: envDuration("OBJMGR_RECONCILE_EVERY", time.Hour, logger),
		ReconcileGrace:    envDuration("OBJMGR_RECONCILE_GRACE", 25*time.Hour, logger),
		HealthKVBucket:    bootstrap.HealthKVBucket,
		Instance:          instance,
		Logger:            logger,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("signal received; shutting down", "signal", sig.String())
		cancel()
	}()

	logger.Info("object-store-manager ready", "instance", instance)
	if err := mgr.Run(ctx); err != nil && ctx.Err() == nil {
		return fmt.Errorf("manager: %w", err)
	}
	logger.Info("object-store-manager exited cleanly", "instance", instance)
	return nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration, logger *slog.Logger) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		logger.Warn("ignoring invalid duration env; using default", "key", key, "value", v, "default", def)
		return def
	}
	return d
}
