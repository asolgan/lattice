package pkgmgr

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// EnabledArtifactKinds is the artifact-kind allow-list for the capability-author
// package (ai-authored-capabilities-design.md §3.2). The kinds are ordered by the
// deterministic-validatability spine: "lens" (Fire 1) and "grant" (Fire 2 fast-
// follow) are enabled here — weaverTarget/loomPattern land with Fire 3, and
// vertexTypeDDL/opMeta (Starlark-bearing) are gated behind the separate verified-
// pure Starlark sandbox + ratification (§3.2 Fire 4). A kind outside this set is
// never valid, regardless of content.
var EnabledArtifactKinds = map[string]bool{
	"lens":  true,
	"grant": true,
}

// enabledArtifactKindsList returns EnabledArtifactKinds' keys sorted, for a
// deterministic "disabled kind" error message.
func enabledArtifactKindsList() []string {
	kinds := make([]string, 0, len(EnabledArtifactKinds))
	for k := range EnabledArtifactKinds {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return kinds
}

// CypherParser abstracts the static openCypher parse check the "lens" kind's §5
// validation needs. Injected rather than pkgmgr importing
// internal/refractor/ruleengine/full directly: that package's own test binary
// transitively imports pkgmgr (parse_test.go → packages/identity-hygiene →
// pkgmgr), so a direct production import here would be a cycle. A caller
// (tests today; the bridge's capabilityAuthor adapter in a later increment)
// supplies a full.New()-backed implementation.
type CypherParser interface {
	// Parse returns a non-nil error if ruleBody fails to statically parse.
	Parse(ruleBody string) error
}

// ArtifactValidationReport is the §5 record-time deterministic-validation
// verdict for one proposed capability artifact: Valid decides pending vs invalid
// (RecordCapabilityProposal never fails the op on a bad artifact — the proposal
// is always recorded, auditable — it just never becomes dispatchable). Errors is
// the human-readable per-check failure list (stored as the proposal's
// .validation.report for the reviewer).
type ArtifactValidationReport struct {
	Valid  bool
	Errors []string
}

// LensArtifactContent is the JSON shape of a "lens"-kind proposal's
// artifact.content — the constrained subset of pkgmgr.LensSpec an AI-authored
// lens proposal may carry in this increment: a plain nats-kv or postgres
// projection (no actor-aggregate Output, no Protected/SecureColumns/GrantTable
// postures — those need a richer scope-check this increment does not yet build;
// see the design's §3.2 phase-by-kind boundary). Field names are the wire shape
// the capabilityAuthor adapter's structured output (and this fire's tests) use.
type LensArtifactContent struct {
	CanonicalName string `json:"canonicalName"`
	Adapter       string `json:"adapter"`
	Bucket        string `json:"bucket"`
	Table         string `json:"table"`
	Spec          string `json:"spec"`
}

// GrantArtifactContent is the JSON shape of a "grant"-kind proposal's
// artifact.content — a single Contract #6 permission grant, mirroring
// pkgmgr.PermissionSpec field-for-field: an operationType gated at a scope,
// granted to one or more already-existing roles by canonical name. A "grant"
// artifact never declares a new Role (§3.2 keeps this increment to widening an
// existing role's permissions, not minting new roles) — GrantsTo entries must
// name a role the installer's live catalog already knows, checked at apply time
// exactly as a hand-authored package's GrantsTo is.
type GrantArtifactContent struct {
	OperationType string   `json:"operationType"`
	Scope         string   `json:"scope"`
	GrantsTo      []string `json:"grantsTo"`
	Note          string   `json:"note"`
}

// HeldPermission is one permission the requesting operator currently holds
// (operationType + scope), as read from their live capability projection
// (Contract #6 §6.x capabilityRoles). It is the caller-supplied basis for the
// "grant" kind's §5 scope check (ai-authored-capabilities-design.md §5, point
// 2): "a grant artifact's conferred authority must be a subset of the
// requesting operator's own held scope". The caller (the bridge at record
// time; the operator's Loupe/CLI submission path at approve time — same
// compute-client-side-then-submit-a-trusted-verdict split as the rest of §5)
// reads this projection fresh and passes it in; ValidateCapabilityArtifact
// never reads a live substrate itself, and takes the slice on faith — it is
// NOT bound to any actor identity here. The grant kind raises the stakes on
// that trust versus the lens kind: whoever builds the real caller (neither
// the bridge's capabilityAuthor adapter nor a Loupe/CLI review path exists
// yet — see package.go's "deliberately not yet built" list) MUST read this
// projection for the actual requesting/approving actor (op.actor), fresh,
// every time — a stale or wrong-actor slice defeats the entire scope check.
type HeldPermission struct {
	OperationType string
	Scope         string
}

// covers reports whether a held permission's scope authorizes granting the
// given requested scope. "any" is the broader posture — an operator holding
// "any" for an operationType may grant either "any" or "self"; an operator
// holding only "self" may grant "self" but never "any" (that would let a
// self-scoped operator mint a broader grant than their own).
func (h HeldPermission) covers(operationType, requestedScope string) bool {
	if h.OperationType != operationType {
		return false
	}
	if h.Scope == "any" {
		return true
	}
	return h.Scope == requestedScope
}

// requesterHolds reports whether held contains a permission covering
// (operationType, requestedScope) — the "subset of the requester's own held
// scope" test the grant kind's §5 scope check applies.
func requesterHolds(held []HeldPermission, operationType, requestedScope string) bool {
	for _, h := range held {
		if h.covers(operationType, requestedScope) {
			return true
		}
	}
	return false
}

// ValidateCapabilityArtifact runs the §5 record-time deterministic-validation
// boundary for one proposed artifact (ai-authored-capabilities-design.md §5,
// point 2): a kind outside EnabledArtifactKinds is always invalid; a "lens" kind
// is parsed with the caller-supplied openCypher parser (rejecting unparseable
// cypher) and run through the existing pkgmgr lens validators (validateLensAdapters
// / validateLensBuckets / validateLensReadPath — reused verbatim, not
// reimplemented) via a throwaway single-lens Definition. It never mutates
// anything and never touches a live substrate (no sandbox dry-run / delta
// preview yet — that lands with the bridge-adapter increment that calls this
// against a real Refractor sandbox); it is pure and unit-testable in isolation.
//
// err is non-nil only for a caller contract violation (malformed content JSON
// for an enabled kind) — never for a model-authored defect, which always comes
// back as a non-valid report (auditable, never dispatchable), per §5's "the
// proposal is ALWAYS stored; the verdict decides only pending vs invalid".
//
// requesterHeld is the requesting operator's currently-held permissions (read
// fresh by the caller from their live Contract #6 capability projection) — the
// basis for the "grant" kind's scope check (a grant may never exceed what the
// requester themselves holds). Ignored by every other kind; a caller validating
// a non-grant artifact may pass nil.
func ValidateCapabilityArtifact(kind string, content json.RawMessage, parser CypherParser, requesterHeld []HeldPermission) (ArtifactValidationReport, error) {
	if !EnabledArtifactKinds[kind] {
		return ArtifactValidationReport{
			Valid:  false,
			Errors: []string{fmt.Sprintf("artifact kind %q is not enabled (enabled: %v)", kind, enabledArtifactKindsList())},
		}, nil
	}

	switch kind {
	case "lens":
		var lc LensArtifactContent
		if err := json.Unmarshal(content, &lc); err != nil {
			return ArtifactValidationReport{}, fmt.Errorf("pkgmgr: capability materializer: malformed lens artifact content: %w", err)
		}
		// A known-fields check catches an artifact trying to smuggle a field this
		// increment's LensArtifactContent doesn't expose (e.g.
		// "protected"/"public"/"grantTable"/"columns"/"secureColumns" — the postures
		// explicitly out of scope, §3.2). Without this, json.Unmarshal above would
		// SILENTLY DROP the unrecognized field and materialize a plain lens anyway —
		// a scope-widening intent quietly downgraded rather than rejected. Treated
		// as a validation FAILURE (stored invalid, auditable), not a caller error:
		// the model may plausibly attempt an out-of-scope posture; §5 wants that
		// visible on the .validation.report, not silently swallowed.
		if extra := unknownLensFields(content); len(extra) > 0 {
			return ArtifactValidationReport{
				Valid: false,
				Errors: []string{fmt.Sprintf(
					"lens artifact content declares out-of-scope field(s) %v — this increment enables only canonicalName/adapter/bucket/table/spec (no protected/public/grantTable/columns/secureColumns postures)",
					extra)},
			}, nil
		}
		return validateLensArtifact(lc, parser), nil
	case "grant":
		var gc GrantArtifactContent
		if err := json.Unmarshal(content, &gc); err != nil {
			return ArtifactValidationReport{}, fmt.Errorf("pkgmgr: capability materializer: malformed grant artifact content: %w", err)
		}
		// Same scope-widening defense as the lens kind's unknownLensFields: a
		// field this increment's GrantArtifactContent doesn't expose would
		// otherwise be silently dropped by json.Unmarshal rather than rejected.
		if extra := unknownGrantFields(content); len(extra) > 0 {
			return ArtifactValidationReport{
				Valid: false,
				Errors: []string{fmt.Sprintf(
					"grant artifact content declares out-of-scope field(s) %v — this increment enables only operationType/scope/grantsTo/note",
					extra)},
			}, nil
		}
		return validateGrantArtifact(gc, requesterHeld), nil
	default:
		// Unreachable: EnabledArtifactKinds gates every case above.
		return ArtifactValidationReport{Valid: false, Errors: []string{"unhandled enabled kind " + kind}}, nil
	}
}

// knownLensFields are the JSON keys LensArtifactContent exposes for this
// increment's "lens" kind. Kept as an explicit set (rather than deriving it
// via reflection) so the allow-list is the obviously-correct source of truth
// unknownLensFields checks raw content against.
var knownLensFields = map[string]bool{
	"canonicalName": true,
	"adapter":       true,
	"bucket":        true,
	"table":         true,
	"spec":          true,
}

// unknownLensFields decodes content as a generic JSON object and returns any
// top-level key outside knownLensFields, sorted for a deterministic report. A
// non-object content (or malformed JSON) returns nil — json.Unmarshal into
// LensArtifactContent already caught that as a caller-contract error before
// this runs.
func unknownLensFields(content json.RawMessage) []string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(content, &raw); err != nil {
		return nil
	}
	var extra []string
	for k := range raw {
		if !knownLensFields[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	return extra
}

// knownGrantFields are the JSON keys GrantArtifactContent exposes for the
// "grant" kind. Mirrors knownLensFields' explicit-allow-list rationale.
var knownGrantFields = map[string]bool{
	"operationType": true,
	"scope":         true,
	"grantsTo":      true,
	"note":          true,
}

// unknownGrantFields decodes content as a generic JSON object and returns any
// top-level key outside knownGrantFields, sorted for a deterministic report.
// Mirrors unknownLensFields.
func unknownGrantFields(content json.RawMessage) []string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(content, &raw); err != nil {
		return nil
	}
	var extra []string
	for k := range raw {
		if !knownGrantFields[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	return extra
}

// validateGrantArtifact is the "grant" kind's deterministic check (design §3.2,
// §5): a well-formed operationType + scope + non-empty GrantsTo, the shared
// validatePermissionIdentityUniqueness/validateAll pre-flight every package
// (hand- or AI-authored) runs, and — the property that makes this kind safe to
// enable at all — the scope check: the artifact's (operationType, scope) must
// be a subset of what the requesting operator already holds. Without this
// check an operator with only narrow authority could route an AI request that
// mints a package granting arbitrarily broad authority to any role (including
// their own) — the exact privilege-escalation path §5 exists to close. Reused,
// not duplicated: an AI-authored grant can never pass a check a hand-authored
// one would fail.
func validateGrantArtifact(gc GrantArtifactContent, requesterHeld []HeldPermission) ArtifactValidationReport {
	var errs []string

	if gc.OperationType == "" {
		errs = append(errs, "operationType is required")
	}
	if gc.Scope != "any" && gc.Scope != "self" {
		errs = append(errs, fmt.Sprintf("scope must be \"any\" or \"self\", got %q", gc.Scope))
	}
	if len(gc.GrantsTo) == 0 {
		errs = append(errs, "grantsTo must name at least one role")
	}
	seenRoles := make(map[string]bool, len(gc.GrantsTo))
	for _, role := range gc.GrantsTo {
		if strings.TrimSpace(role) == "" {
			errs = append(errs, "grantsTo entries must be non-empty role names")
			continue
		}
		if seenRoles[role] {
			errs = append(errs, fmt.Sprintf("grantsTo names role %q more than once", role))
		}
		seenRoles[role] = true
	}

	wellFormed := gc.OperationType != "" && (gc.Scope == "any" || gc.Scope == "self") && len(gc.GrantsTo) > 0
	if wellFormed {
		def := grantArtifactDefinition(gc, "", "")
		if err := def.validateAll(); err != nil {
			errs = append(errs, err.Error())
		}
		// The scope check runs only once the artifact is otherwise well-formed —
		// an empty/invalid scope has nothing meaningful to compare against the
		// requester's held permissions.
		if !requesterHolds(requesterHeld, gc.OperationType, gc.Scope) {
			errs = append(errs, fmt.Sprintf(
				"requesting operator does not hold %q at scope %q or broader — a grant cannot exceed the requester's own held scope",
				gc.OperationType, gc.Scope))
		}
	}

	return ArtifactValidationReport{Valid: len(errs) == 0, Errors: errs}
}

// grantArtifactDefinition is the single shape both record-time validation
// (validateGrantArtifact, a throwaway unnamed Definition) and apply-time
// materialization (DefinitionForCapabilityArtifact) build from a
// GrantArtifactContent — mirrors lensArtifactDefinition's byte-for-byte
// validated-equals-materialized guarantee.
func grantArtifactDefinition(gc GrantArtifactContent, name, version string) Definition {
	return Definition{
		Name:    name,
		Version: version,
		Permissions: []PermissionSpec{{
			OperationType: gc.OperationType,
			Scope:         gc.Scope,
			GrantsTo:      gc.GrantsTo,
			Note:          gc.Note,
		}},
	}
}

// validateLensArtifact is the "lens" kind's deterministic check: the openCypher
// parser must accept the spec (statically, without executing it), and the
// materialized single-lens Definition must pass the same validateAll the human
// package-authoring path runs (bucket/adapter/read-path posture) — reused, not
// duplicated, so an AI-authored lens can never pass a check a hand-authored one
// would fail.
func validateLensArtifact(lc LensArtifactContent, parser CypherParser) ArtifactValidationReport {
	var errs []string

	if lc.CanonicalName == "" {
		errs = append(errs, "canonicalName is required")
	}
	if lc.Spec == "" {
		errs = append(errs, "spec is required")
	} else if err := parser.Parse(lc.Spec); err != nil {
		errs = append(errs, fmt.Sprintf("cypher spec does not parse: %v", err))
	}

	def := lensArtifactDefinition(lc, "", "")
	if err := def.validateAll(); err != nil {
		errs = append(errs, err.Error())
	}

	return ArtifactValidationReport{Valid: len(errs) == 0, Errors: errs}
}

// lensArtifactDefinition is the single shape both record-time validation
// (validateLensArtifact, a throwaway unnamed Definition) and apply-time
// materialization (DefinitionForCapabilityArtifact, a real named/versioned
// Definition — Fire 2, design §3.5) build from a LensArtifactContent — the
// reason an installed lens is guaranteed byte-for-byte identical to what §5
// validated.
func lensArtifactDefinition(lc LensArtifactContent, name, version string) Definition {
	return Definition{
		Name:    name,
		Version: version,
		Lenses: []LensSpec{{
			CanonicalName: lc.CanonicalName,
			Class:         "meta.lens",
			Adapter:       lc.Adapter,
			Bucket:        lc.Bucket,
			Table:         lc.Table,
			Engine:        "full",
			Spec:          lc.Spec,
		}},
	}
}

// DefinitionForCapabilityArtifact builds the pkgmgr.Definition an APPROVED
// proposal's artifact materializes to (design §3.5, the Fire 2 apply step) —
// named/versioned for a real package Install/Upgrade, unlike
// ValidateCapabilityArtifact's throwaway unnamed check. kind must already be
// one of EnabledArtifactKinds: by construction a proposal can only reach
// review.state=approved if RecordCapabilityProposal's §5 gate already
// accepted its kind, so an unrecognized kind here is a caller-contract
// violation (a proposal applied out of order), never a model-authored
// defect.
func DefinitionForCapabilityArtifact(kind string, content json.RawMessage, name, version string) (Definition, error) {
	if !EnabledArtifactKinds[kind] {
		return Definition{}, fmt.Errorf("pkgmgr: capability apply: artifact kind %q is not enabled", kind)
	}
	switch kind {
	case "lens":
		var lc LensArtifactContent
		if err := json.Unmarshal(content, &lc); err != nil {
			return Definition{}, fmt.Errorf("pkgmgr: capability apply: malformed lens artifact content: %w", err)
		}
		return lensArtifactDefinition(lc, name, version), nil
	case "grant":
		var gc GrantArtifactContent
		if err := json.Unmarshal(content, &gc); err != nil {
			return Definition{}, fmt.Errorf("pkgmgr: capability apply: malformed grant artifact content: %w", err)
		}
		return grantArtifactDefinition(gc, name, version), nil
	default:
		// Unreachable: EnabledArtifactKinds gates every case above.
		return Definition{}, fmt.Errorf("pkgmgr: capability apply: unhandled enabled kind %q", kind)
	}
}
