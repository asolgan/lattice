package processor

import (
	"errors"
	"fmt"
)

// errCapabilityModeNotYetAvailable is returned by SelectAuthorizer when a
// deployment requests `capability` mode before Story 3.3 lands. Returned
// as a fixed sentinel so startup-time configuration probes can detect it
// with errors.Is.
var errCapabilityModeNotYetAvailable = errors.New(
	"processor: LATTICE_AUTH_MODE=capability is reserved for Story 3.3 (not yet implemented)")

// errUnknownAuthMode wraps the offending mode value.
func errUnknownAuthMode(m AuthMode) error {
	return fmt.Errorf("processor: unknown LATTICE_AUTH_MODE %q (expected 'stub' or 'capability')", m)
}
