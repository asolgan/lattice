package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestIsLoopbackHost pins the load-bearing exposure check of an auth-less admin
// tool: only a genuine loopback bind may be treated as safe. A mis-classification
// that reports a wide or public bind as loopback would silently expose admin
// control + op-submission to the network with no warning.
func TestIsLoopbackHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		// Loopback literals — safe.
		{"127.0.0.1", true},
		{"127.0.0.5", true}, // the whole 127/8 block is loopback
		{"::1", true},
		{"localhost", true},
		{"LocalHost", true}, // hostname match is case-insensitive
		{"LOCALHOST", true},
		// Empty host = the bare ":7777" form = all interfaces = NOT loopback.
		{"", false},
		// Wide / public binds — never loopback.
		{"0.0.0.0", false},
		{"::", false},
		{"192.168.1.10", false},
		{"10.0.0.1", false},
		{"8.8.8.8", false},
		{"2001:db8::1", false},
		// A non-literal hostname (other than localhost) is not trusted as loopback.
		{"example.com", false},
		{"localhost.evil.com", false},
	}
	for _, tc := range tests {
		if got := isLoopbackHost(tc.host); got != tc.want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

// TestWarnIfNonLoopback pins the startup exposure warning: a non-loopback or
// unparseable LOUPE_ADDR must emit a loud WARN so an auth-less network-wide
// admin bind is never silent; a loopback bind must stay quiet.
func TestWarnIfNonLoopback(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		wantWarn bool
	}{
		{"loopback ipv4", "127.0.0.1:7777", false},
		{"loopback ipv6", "[::1]:7777", false},
		{"localhost", "localhost:7777", false},
		{"bare port (all interfaces)", ":7777", true},
		{"all interfaces explicit", "0.0.0.0:7777", true},
		{"public ip", "8.8.8.8:7777", true},
		{"lan ip", "192.168.1.10:7777", true},
		{"unparseable addr", "not-an-addr", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
			warnIfNonLoopback(logger, tc.addr)
			gotWarn := strings.Contains(buf.String(), "level=WARN")
			if gotWarn != tc.wantWarn {
				t.Errorf("warnIfNonLoopback(%q): warn=%v, want %v (log: %q)", tc.addr, gotWarn, tc.wantWarn, buf.String())
			}
		})
	}
}
