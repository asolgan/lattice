package main

import (
	"testing"
	"time"
)

func TestValidateJWKSURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		devMode bool
		wantErr bool
	}{
		{"https prod ok", "https://idp.example.com/.well-known/jwks.json", false, false},
		{"http prod rejected", "http://idp.example.com/jwks.json", false, true},
		{"http dev allowed", "http://localhost:9999/jwks.json", true, false},
		{"https dev allowed", "https://idp.example.com/jwks.json", true, false},
		{"malformed url", "://not-a-url", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateJWKSURL(tc.url, tc.devMode)
			if tc.wantErr && err == nil {
				t.Errorf("validateJWKSURL(%q, dev=%v) = nil, want error", tc.url, tc.devMode)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateJWKSURL(%q, dev=%v) = %v, want nil", tc.url, tc.devMode, err)
			}
		})
	}
}

func TestParsePollInterval(t *testing.T) {
	if got, err := parsePollInterval(""); err != nil || got != 0 {
		t.Errorf("parsePollInterval(\"\") = (%v, %v), want (0, nil)", got, err)
	}
	if got, err := parsePollInterval("2m"); err != nil || got != 2*time.Minute {
		t.Errorf("parsePollInterval(\"2m\") = (%v, %v), want (2m, nil)", got, err)
	}
	if _, err := parsePollInterval("not-a-duration"); err == nil {
		t.Error("parsePollInterval(\"not-a-duration\") = nil error, want an error")
	}
}
