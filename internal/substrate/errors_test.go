package substrate

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestIsConnectionError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"connection closed", nats.ErrConnectionClosed, true},
		{"connection draining", nats.ErrConnectionDraining, true},
		{"disconnected", nats.ErrDisconnected, true},
		{"no servers", nats.ErrNoServers, true},
		{"wrapped connection closed", fmt.Errorf("dial: %w", nats.ErrConnectionClosed), true},
		{"unrelated error", errors.New("boom"), false},
		{"revision conflict is not a connection error", jetstream.ErrKeyExists, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsConnectionError(tc.err); got != tc.want {
				t.Fatalf("IsConnectionError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsInvalidKeyError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"invalid key", jetstream.ErrInvalidKey, true},
		{"wrapped invalid key", fmt.Errorf("KVPut: %w", jetstream.ErrInvalidKey), true},
		{"unrelated error", errors.New("boom"), false},
		{"connection error is not an invalid-key error", nats.ErrConnectionClosed, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsInvalidKeyError(tc.err); got != tc.want {
				t.Fatalf("IsInvalidKeyError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
