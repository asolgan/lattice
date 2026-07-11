package identitydomain

import "github.com/asolgan/lattice/internal/pkgmgr"

// IdentityIndexHintBucket is the package-owned NATS-KV read model the
// identityIndexHint lens projects into — the P5-clean query surface the
// Gateway's provision-time probe reads directly (whoami `?probe=1`,
// multi-credential-identity-linking-design.md §3.4), instead of routing a
// read-derived signal through an operation's synchronous reply (Contract #2
// §2.7's closed `response` schema permits only `primaryKey` and forbids any
// other script-returned data — verified at build time; see the design doc's
// §3.4 build-note).
const IdentityIndexHintBucket = "identity-index-hint"

// Lenses returns the package's Lens declarations.
func Lenses() []pkgmgr.LensSpec {
	return []pkgmgr.LensSpec{
		{
			CanonicalName: "identityIndexHint",
			Class:         "meta.lens",
			Adapter:       "nats-kv",
			Bucket:        IdentityIndexHintBucket,
			Engine:        "full",
			Spec:          identityIndexHintSpec,
		},
	}
}

// identityIndexHintSpec projects one row per live identityindex vertex,
// keyed by its own derived-hash key (the IntoKey default, `["key"]`) — the
// same existence + identityKey the dedup scripts already read in-graph
// (packages/identity-domain/ddls.go's CreateUnclaimedIdentity dedup check),
// now available P5-clean outside a write-path declared read. No PII: the
// index key is already a one-way hash (`sha256NanoID("email:"+email)`, etc)
// and the projected row carries only the identity key it resolves to.
const identityIndexHintSpec = `MATCH (n:identityindex)
RETURN n.key AS key,
       n.data.identityKey AS identityKey,
       n.data.contactType AS contactType`
