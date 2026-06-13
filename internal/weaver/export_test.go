package weaver

// DeriveTimerRequestID exposes the §10.4 deterministic fired-timer requestId
// derivation to the external weaver_test package, so the e2e suite can assert
// the exact requestId an observed MarkExpired op must carry.
var DeriveTimerRequestID = deriveTimerRequestID
