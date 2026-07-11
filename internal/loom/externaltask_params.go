package loom

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// subjectParamPrefix is the token namespace for an externalTask param value that
// resolves against the step's subject vertex. A param string value beginning
// with this prefix is a §10.5 path template the instanceOp DDL resolves at
// submit time from the op's hydrated working set; any other value (a literal
// string, number, bool, or nested object) is passed through verbatim.
const subjectParamPrefix = "subject."

// inferExternalTaskReads computes the instanceOp's ContextHint.Reads +
// ContextHint.EgressReads for an externalTask step (Contract #10 §10.5
// subject-templated params, Mechanism 2; the egressReads split is the
// sensitive-param-egress design §3.4).
//
// The subject root (subjectKey) is always returned in reads — the instanceOp's
// no-orphan vertex_alive check needs it. In addition, every params value
// shaped subject.<aspect>.data.<field> contributes the known aspect key
// subjectKey + "." + <aspect> to egressReads (NOT reads): the aspect is read
// for external egress, so a sensitive aspect hydrates as a ref rather than
// plaintext (§3.1 egressReads disposition) — the instanceOp DDL cannot tell
// which templated aspects are sensitive, so ALL template-inferred aspect
// reads classify as egressReads uniformly; a non-sensitive aspect hydrates
// identically either way. A subject.data.<field> token reads only the subject
// root (already in reads).
//
// Mechanism 2 (Andrew's directive): Loom DECLARES the read-set by pure string
// parsing and performs NO Core KV read; resolution happens Processor-side in the
// instanceOp DDL from the JIT-hydrated, OCC-snapshot working set. params is the
// opaque step.Params, unchanged on the wire (the engine never substitutes a
// value). Both returned sets are deterministically ordered (aspect keys
// sorted by param-key order) so the outbox envelope is byte-stable.
//
// A subject.* value that is not a well-formed §10.5 path is a malformed template
// failed loudly here at submit — never dispatched with an unresolvable token.
func inferExternalTaskReads(subjectKey string, params json.RawMessage) (reads []string, egressReads []string, err error) {
	reads = []string{subjectKey}
	if len(params) == 0 {
		return reads, nil, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(params, &m); err != nil {
		// params is not a JSON object (e.g. a bare array/scalar) — no string-keyed
		// subject.* tokens to infer; the opaque value still passes through to the
		// instanceOp unchanged.
		return reads, nil, nil
	}

	// Iterate in sorted param-key order so the appended aspect reads are
	// deterministic regardless of JSON map iteration order.
	paramKeys := make([]string, 0, len(m))
	for k := range m {
		paramKeys = append(paramKeys, k)
	}
	sort.Strings(paramKeys)

	seen := map[string]bool{subjectKey: true}
	for _, k := range paramKeys {
		var s string
		if err := json.Unmarshal(m[k], &s); err != nil {
			continue // a non-string value is a literal — no read to declare
		}
		if !strings.HasPrefix(s, subjectParamPrefix) {
			continue // a literal string — passed through verbatim
		}
		gp, perr := parseGuardPath(s)
		if perr != nil {
			return nil, nil, fmt.Errorf("loom: externalTask param %q: malformed subject template %q: %w", k, s, perr)
		}
		if gp.aspect == "" {
			continue // subject.data.<field> reads the subject root (already declared)
		}
		akey := subjectKey + "." + gp.aspect
		if !seen[akey] {
			seen[akey] = true
			egressReads = append(egressReads, akey)
		}
	}
	return reads, egressReads, nil
}
