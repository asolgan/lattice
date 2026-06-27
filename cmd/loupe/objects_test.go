package main

import "testing"

// TestObjectDisposition pins the admin-UI anti-XSS boundary: only the
// raster-image allow-list renders inline with its declared type; every other
// type is forced to a neutral octet-stream attachment so an uploaded active
// document (svg / html / pdf) can never execute as same-origin script.
func TestObjectDisposition(t *testing.T) {
	tests := []struct {
		contentType string
		wantType    string
		wantDisp    string
	}{
		// Allow-list → inline, declared type preserved.
		{"image/jpeg", "image/jpeg", "inline"},
		{"image/png", "image/png", "inline"},
		{"image/gif", "image/gif", "inline"},
		{"image/webp", "image/webp", "inline"},
		// Active / scriptable documents → forced neutral attachment.
		{"image/svg+xml", "application/octet-stream", "attachment"},
		{"text/html", "application/octet-stream", "attachment"},
		{"application/pdf", "application/octet-stream", "attachment"},
		{"application/xhtml+xml", "application/octet-stream", "attachment"},
		// Already-neutral / unknown / empty → attachment.
		{"application/octet-stream", "application/octet-stream", "attachment"},
		{"", "application/octet-stream", "attachment"},
		// The allow-list is case-sensitive: a mixed-case spoof is NOT inlined.
		{"image/JPEG", "application/octet-stream", "attachment"},
		{"IMAGE/PNG", "application/octet-stream", "attachment"},
		// A parameterized image type does not match the bare allow-list key.
		{"image/png; charset=utf-8", "application/octet-stream", "attachment"},
	}
	for _, tt := range tests {
		gotType, gotDisp := objectDisposition(tt.contentType)
		if gotType != tt.wantType || gotDisp != tt.wantDisp {
			t.Errorf("objectDisposition(%q) = (%q, %q), want (%q, %q)",
				tt.contentType, gotType, gotDisp, tt.wantType, tt.wantDisp)
		}
	}
}

// TestObjectLinkKey checks the deterministic reconstruction of
// lnk.object.<oid>.<linkName>.<tgtType>.<tgtId> from an object id and a full
// vtx.<type>.<id> target, plus the malformed-target error paths.
func TestObjectLinkKey(t *testing.T) {
	t.Run("well-formed target", func(t *testing.T) {
		got, err := objectLinkKey("OID1", "vtx.identity.I1", "profilePhoto")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "lnk.object.OID1.profilePhoto.identity.I1"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("malformed targets error", func(t *testing.T) {
		bad := []string{
			"vtx.identity",            // 2 segments
			"vtx.identity.I1.profile", // 4 segments
			"lnk.identity.I1",         // wrong prefix
			"identity.I1.x",           // no vtx prefix
			"",                        // empty
		}
		for _, target := range bad {
			if _, err := objectLinkKey("OID1", target, "profilePhoto"); err == nil {
				t.Errorf("objectLinkKey(_, %q, _) = nil error, want error", target)
			}
		}
	})
}

// TestJoin0 confirms the NUL separator keeps disjoint field tuples distinct —
// ("ab","c") and ("a","bc") must not collide, which a plain concatenation would.
func TestJoin0(t *testing.T) {
	if got := join0("a", "b", "c"); got != "a\x00b\x00c" {
		t.Errorf("join0 = %q, want a\\x00b\\x00c", got)
	}
	if join0("ab", "c") == join0("a", "bc") {
		t.Error("join0 collided on disjoint tuples ('ab','c') vs ('a','bc')")
	}
	if got := join0("solo"); got != "solo" {
		t.Errorf("join0(single) = %q, want solo", got)
	}
}
