package projection

// EmptyAction is the concrete write action a plan takes when an actor's
// projection is empty (no real rows after the realness filter). It is what the
// live writer (Story 12.4) dispatches; 12.3 produces and tests it.
type EmptyAction int

const (
	// ActionDelete removes the actor's output key. On a guarded NATS-KV adapter
	// this lands as the §6.2 soft tombstone carrying the projectionSeq
	// watermark; on an unguarded hard-delete adapter it physically removes the
	// key. This is the built-in lenses' behavior (ErrDeleteProjection).
	ActionDelete EmptyAction = iota
	// ActionSoftDelete writes the §6.2 soft tombstone explicitly. It reuses the
	// SAME natskv guarded-delete mechanism as ActionDelete on a guarded adapter
	// — a guarded Delete always writes {isDeleted:true, projectionSeq}. There is
	// no second tombstone path: softDelete is the descriptor's way of requesting
	// the tombstone regardless of the adapter's hard/soft deleteMode.
	ActionSoftDelete
	// ActionWriteEmptyDoc upserts an empty document for the actor (key stays
	// present with an empty body).
	ActionWriteEmptyDoc
	// ActionSkip declines the row, leaving any existing key untouched
	// (ErrSkipProjection).
	ActionSkip
)

// EmptyAction maps the descriptor's emptyBehavior onto the concrete write
// action the writer dispatches for an empty projection.
func (d OutputDescriptor) EmptyAction() EmptyAction {
	switch d.EmptyBehavior {
	case EmptySoftDelete:
		return ActionSoftDelete
	case EmptyDoc:
		return ActionWriteEmptyDoc
	case EmptySkip:
		return ActionSkip
	default: // EmptyDelete
		return ActionDelete
	}
}

// RequiresGuardedTombstone reports whether the empty behavior produces a §6.2
// soft tombstone via the guarded-delete mechanism. Both delete (on a guarded
// adapter) and softDelete reuse the one natskv guarded-delete path; the writer
// enables the guard and calls Delete — it never opens a second tombstone path.
func (d OutputDescriptor) RequiresGuardedTombstone() bool {
	a := d.EmptyAction()
	return a == ActionSoftDelete || a == ActionDelete
}
