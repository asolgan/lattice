// cmd/bootstrap — Primordial bootstrap binary for Story 1.3.
//
// Invoked by `make up` after NATS and Postgres containers are healthy.
// Provisions KV buckets + streams, writes all primordial Core KV entries,
// starts the refractor-stub (via subprocess or waits for it to write the
// readiness signal), then exits 0.
//
// Idempotent: if lattice.bootstrap.json already exists, bucket/stream
// provisioning still runs (to recover from partial failures) but primordial
// key seeding is skipped per Contract #7 §7.4.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/asolgan/lattice/internal/bootstrap"
)

const defaultBootstrapJSONPath = "./lattice.bootstrap.json"
const defaultReadyTimeoutSec = 30

// BootstrapConfig is persisted to lattice.bootstrap.json per Contract #7 §7.3.
// Fixed IDs are used for primordial entities (see internal/bootstrap/nanoid.go).
type BootstrapConfig struct {
	PlatformVersion      string    `json:"platformVersion"`
	BootstrapDate        time.Time `json:"bootstrapDate"`
	BootstrapIdentityKey string    `json:"bootstrapIdentityKey"`
	PlatformActorKey     string    `json:"platformActorKey"`
	MetaRootKey          string    `json:"metaRootKey"`
	CapabilityLensKey    string    `json:"capabilityLensKey"`
	CapabilityRoleIndex  string    `json:"capabilityRoleIndexKey"`
	BootstrapOpKey       string    `json:"bootstrapOpKey"`
	RoleKeys             map[string]string `json:"roleKeys"`
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	natsURL := envOrDefault("NATS_URL", nats.DefaultURL)
	bootstrapJSONPath := envOrDefault("BOOTSTRAP_JSON_PATH", defaultBootstrapJSONPath)
	timeoutSec := envIntOrDefault("BOOTSTRAP_READY_TIMEOUT_SEC", defaultReadyTimeoutSec)

	logger.Info("lattice bootstrap starting", "natsURL", natsURL, "bootstrapJSON", bootstrapJSONPath)

	// Connect to NATS with retry (containers may be slow to accept connections
	// even after healthcheck passes).
	nc, err := connectNATSWithRetry(natsURL, 20, 1*time.Second, logger)
	if err != nil {
		logger.Error("failed to connect to NATS", "error", err)
		os.Exit(1)
	}
	defer nc.Close()
	logger.Info("connected to NATS", "url", nc.ConnectedUrl())

	seeder, err := bootstrap.NewSeeder(nc, logger)
	if err != nil {
		logger.Error("failed to create seeder", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

	// Always provision buckets/streams — idempotent and recovers partial failures.
	logger.Info("provisioning KV buckets and streams")
	if err := seeder.ProvisionBuckets(ctx); err != nil {
		logger.Error("bucket provisioning failed", "error", err)
		os.Exit(1)
	}

	// Check if lattice.bootstrap.json already exists → skip seeding.
	alreadyBootstrapped := fileExists(bootstrapJSONPath)
	if alreadyBootstrapped {
		logger.Info("lattice.bootstrap.json found — skipping primordial seeding (idempotent re-run)")
	} else {
		logger.Info("seeding primordial Core KV entries")
		if err := seeder.SeedPrimordial(ctx); err != nil {
			logger.Error("primordial seeding failed", "error", err)
			os.Exit(1)
		}

		// Persist lattice.bootstrap.json.
		cfg := buildBootstrapConfig()
		if err := writeBootstrapJSON(bootstrapJSONPath, cfg); err != nil {
			logger.Error("failed to write lattice.bootstrap.json", "error", err)
			os.Exit(1)
		}
		logger.Info("lattice.bootstrap.json written", "path", bootstrapJSONPath)
	}

	// Wait for readiness gate: refractor-stub writes health.bootstrap.complete.
	logger.Info("waiting for readiness gate", "timeout", fmt.Sprintf("%ds", timeoutSec))
	readyCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	if err := bootstrap.WaitForBootstrapComplete(readyCtx, nc, logger); err != nil {
		logger.Error("readiness gate failed", "error", err,
			"suggestion", "check refractor-stub logs; try `make down && make up`")
		os.Exit(1)
	}

	logger.Info("Lattice ready — primordial bootstrap complete")
}

// buildBootstrapConfig assembles the config from the fixed primordial constants.
func buildBootstrapConfig() BootstrapConfig {
	return BootstrapConfig{
		PlatformVersion:      "1.0",
		BootstrapDate:        bootstrap.BootstrapTime,
		BootstrapIdentityKey: bootstrap.BootstrapIdentityKey,
		PlatformActorKey:     bootstrap.PlatformActorKey,
		MetaRootKey:          bootstrap.MetaRootKey,
		CapabilityLensKey:    bootstrap.CapabilityLensKey,
		CapabilityRoleIndex:  bootstrap.CapabilityRoleIndexLensKey,
		BootstrapOpKey:       bootstrap.BootstrapOpKey,
		RoleKeys: map[string]string{
			"consumer":         bootstrap.RoleConsumerKey,
			"frontOfHouse":     bootstrap.RoleFrontOfHouseKey,
			"backOfHouse":      bootstrap.RoleBackOfHouseKey,
			"operator":         bootstrap.RoleOperatorKey,
			"platformInternal": bootstrap.RolePlatformIntlKey,
		},
	}
}

func writeBootstrapJSON(path string, cfg BootstrapConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOrDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// connectNATSWithRetry retries NATS connection until maxAttempts or success.
func connectNATSWithRetry(url string, maxAttempts int, delay time.Duration, logger *slog.Logger) (*nats.Conn, error) {
	var lastErr error
	for i := 1; i <= maxAttempts; i++ {
		nc, err := nats.Connect(url,
			nats.MaxReconnects(5),
			nats.ReconnectWait(1*time.Second),
		)
		if err == nil {
			return nc, nil
		}
		lastErr = err
		logger.Info("NATS connect attempt failed, retrying", "attempt", i, "error", err)
		time.Sleep(delay)
	}
	return nil, fmt.Errorf("NATS connect failed after %d attempts: %w", maxAttempts, lastErr)
}
