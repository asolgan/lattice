// Command gen-dev-nkeys mints the per-component NATS NKey seeds and renders the
// Lattice transport-authorization config (deploy/nats-server.conf) that enforces
// the NATS account-level write restriction (Path A: static config + per-component
// NKey users).
//
// The permission matrix itself lives in internal/natsperm.Matrix (the single
// source of truth for each component's publish allow/deny set); this tool is a
// thin renderer — it mints/reuses the seed files (deploy/nkeys/<component>.nk)
// and writes the server config that references their public keys via
// natsperm.RenderConf. Run it after editing the matrix (e.g. adding a
// component):
//
//	go run ./deploy/gen-dev-nkeys
//
// An existing seed file is REUSED, not rotated — the run is idempotent per
// component, so adding one new entry does not churn every other component's
// dev identity. Delete a component's deploy/nkeys/<name>.nk first to force a
// deliberate rotation of just that seed.
//
// The seeds it writes are DEV-ONLY, committed like POSTGRES_PASSWORD: lattice_dev;
// production injects real seeds via mounted secrets / Vault and never commits them.
//
// The rendered config + committed seeds are exercised end-to-end by
// internal/natsperm (the offline conformance proof of the matrix).
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nats-io/nkeys"

	"github.com/asolgan/lattice/internal/natsperm"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-dev-nkeys:", err)
		os.Exit(1)
	}
}

func run() error {
	deployDir, err := deployRoot()
	if err != nil {
		return err
	}
	nkeysDir := filepath.Join(deployDir, "nkeys")
	if err := os.MkdirAll(nkeysDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", nkeysDir, err)
	}

	pubKeys := make(map[string]string, len(natsperm.Matrix))
	for _, c := range natsperm.Matrix {
		seedPath := filepath.Join(nkeysDir, c.Name+".nk")

		// Idempotent by component: an existing seed file is REUSED, not
		// rotated. Minting a fresh keypair for every component on every run
		// (the original behavior) rotates every OTHER component's dev
		// identity as a side effect of adding one new component — a
		// disruptive, unreviewable diff. Delete the seed file to force a
		// deliberate rotation for that one component.
		if existing, err := os.ReadFile(seedPath); err == nil {
			kp, err := nkeys.FromSeed(bytes.TrimSpace(existing))
			if err != nil {
				return fmt.Errorf("parse existing seed %s: %w", seedPath, err)
			}
			pub, err := kp.PublicKey()
			if err != nil {
				return fmt.Errorf("public key for existing %s: %w", c.Name, err)
			}
			pubKeys[c.Name] = pub
			continue
		}

		kp, err := nkeys.CreateUser()
		if err != nil {
			return fmt.Errorf("create nkey for %s: %w", c.Name, err)
		}
		seed, err := kp.Seed()
		if err != nil {
			return fmt.Errorf("seed for %s: %w", c.Name, err)
		}
		pub, err := kp.PublicKey()
		if err != nil {
			return fmt.Errorf("public key for %s: %w", c.Name, err)
		}
		if err := os.WriteFile(seedPath, append(seed, '\n'), 0o600); err != nil {
			return fmt.Errorf("write seed %s: %w", seedPath, err)
		}
		pubKeys[c.Name] = pub
	}

	conf := natsperm.RenderConf(pubKeys)
	confPath := filepath.Join(deployDir, "nats-server.conf")
	if err := os.WriteFile(confPath, []byte(conf), 0o644); err != nil {
		return fmt.Errorf("write conf %s: %w", confPath, err)
	}
	fmt.Printf("wrote %d seeds to %s and %s\n", len(natsperm.Matrix), nkeysDir, confPath)
	return nil
}

// deployRoot locates the deploy/ directory relative to this source file so the
// tool works regardless of the caller's working directory.
func deployRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// Walk up to the repo root (the dir containing go.mod), then into deploy/.
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "deploy"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod above %s", wd)
		}
		dir = parent
	}
}
