package processor

import "encoding/json"

// jsonUnmarshalLenient is a wrapper around json.Unmarshal used by
// best-effort partial extraction paths (e.g. recovering a requestId from
// an otherwise-malformed envelope). It exists as a one-line helper so
// the call site reads intentfully.
func jsonUnmarshalLenient(data []byte, out any) error {
	return json.Unmarshal(data, out)
}
