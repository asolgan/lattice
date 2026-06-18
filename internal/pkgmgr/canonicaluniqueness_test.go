package pkgmgr

import (
	"strings"
	"testing"
)

// ddlSpec returns a minimal valid-shaped DDLSpec carrying just the
// canonicalName the uniqueness check reads (the self-description fields are
// validated elsewhere and are irrelevant to this pure check).
func ddlSpec(name string) DDLSpec { return DDLSpec{CanonicalName: name} }

func lensSpec(name string) LensSpec { return LensSpec{CanonicalName: name} }

func opMetaSpec(op string) OpMetaSpec { return OpMetaSpec{OperationType: op} }

func TestValidateCanonicalNameUniqueness(t *testing.T) {
	tests := []struct {
		name    string
		def     Definition
		wantErr bool
		// substr, when wantErr, is a fragment the error must name (the
		// duplicated canonicalName).
		substr string
	}{
		{
			name: "clean def passes",
			def: Definition{
				DDLs:    []DDLSpec{ddlSpec("identity"), ddlSpec("ssn")},
				Lenses:  []LensSpec{lensSpec("duplicateCandidates")},
				OpMetas: []OpMetaSpec{opMetaSpec("CreateServiceInstance")},
			},
			wantErr: false,
		},
		{
			name:    "DDL+DDL dup",
			def:     Definition{DDLs: []DDLSpec{ddlSpec("ssn"), ddlSpec("ssn")}},
			wantErr: true,
			substr:  "ssn",
		},
		{
			name:    "Lens+Lens dup",
			def:     Definition{Lenses: []LensSpec{lensSpec("dupe"), lensSpec("dupe")}},
			wantErr: true,
			substr:  "dupe",
		},
		{
			name: "DDL+Lens dup",
			def: Definition{
				DDLs:   []DDLSpec{ddlSpec("ssn")},
				Lenses: []LensSpec{lensSpec("ssn")},
			},
			wantErr: true,
			substr:  "ssn",
		},
		{
			name: "op-meta collides with DDL",
			def: Definition{
				DDLs:    []DDLSpec{ddlSpec("shared")},
				OpMetas: []OpMetaSpec{opMetaSpec("shared")},
			},
			wantErr: true,
			substr:  "shared",
		},
		{
			name: "op-meta collides with op-meta",
			def: Definition{
				OpMetas: []OpMetaSpec{opMetaSpec("Op"), opMetaSpec("Op")},
			},
			wantErr: true,
			substr:  "Op",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.def.validateCanonicalNameUniqueness()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.substr) {
					t.Errorf("error should name duplicated canonicalName %q; got %q", tc.substr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected clean def to pass, got: %v", err)
			}
		})
	}
}

// TestValidateCanonicalNameUniqueness_NamesBothKinds asserts the DDL+Lens
// collision error names both colliding kinds, so an author can locate them.
func TestValidateCanonicalNameUniqueness_NamesBothKinds(t *testing.T) {
	def := Definition{
		DDLs:   []DDLSpec{ddlSpec("ssn")},
		Lenses: []LensSpec{lensSpec("ssn")},
	}
	err := def.validateCanonicalNameUniqueness()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "DDL") || !strings.Contains(err.Error(), "lens") {
		t.Errorf("error should name both colliding kinds (DDL and lens); got %q", err)
	}
}
