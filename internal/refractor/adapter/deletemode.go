package adapter

import "fmt"

// DeleteMode is a per-lens, construction-time property controlling how an
// adapter projects a Core KV deletion into its target store.
//
//   - DeleteModeHard (default): physically remove the row/key from the target
//     (`DELETE FROM` / `kv.Delete`). Lineage already lives in Core KV, so the
//     derived view reflects deletions as removals.
//   - DeleteModeSoft: retain a tombstone in the target (`UPDATE … SET
//     is_deleted=true` / `Put({isDeleted:true})`) for audit/forensic targets
//     that opt in.
//
// Mode is fixed at adapter construction time; the Adapter.Delete signature is
// unchanged — the adapter the pipeline calls already carries its mode.
type DeleteMode string

const (
	// DeleteModeHard physically removes the row/key. This is the default.
	DeleteModeHard DeleteMode = "hard"
	// DeleteModeSoft retains a tombstone in the target store.
	DeleteModeSoft DeleteMode = "soft"
)

// ParseDeleteMode maps a string to a DeleteMode. The empty string defaults to
// DeleteModeHard. "hard" and "soft" map to themselves. Any other value is an
// error. This is the single source of truth for the allowed delete-mode set.
func ParseDeleteMode(s string) (DeleteMode, error) {
	switch s {
	case "":
		return DeleteModeHard, nil
	case string(DeleteModeHard):
		return DeleteModeHard, nil
	case string(DeleteModeSoft):
		return DeleteModeSoft, nil
	default:
		return "", fmt.Errorf("invalid delete mode %q (expected \"hard\", \"soft\", or empty)", s)
	}
}
