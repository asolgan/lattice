package starlark_spike

import (
	"fmt"
)

// RealisticExampleScript is the representative Starlark script used for both
// API ergonomics validation and the performance benchmark.
//
// It implements a CreateIdentity operation:
//   - reads the actor vertex from hydrated state
//   - validates the payload has a non-empty email
//   - conditionally creates an identity vertex + email aspect
//   - emits an identityCreated event
//
// This covers: one vertex hydration read, one conditional branch, one mutation proposal.
// These are the three criteria specified in AC #3 (order-of-magnitude perf).
//
// Note: NanoID generation (nanoid.new()) is specified in Contract #3 §3.6 as a
// Starlark stdlib builtin seeded with the requestId. For this spike, we inject a
// deterministic ID via the Starlark globals so the script remains pure. Story 1.6
// will wire up the actual nanoid stdlib binding.
const RealisticExampleScript = `
def execute(state, op):
    """
    CreateIdentity handler — realistic example for spike validation.

    Inputs (from ScriptContext):
      state: dict of Core KV key -> vertex struct (pre-fetched at step 4)
      op:    OperationEnvelope struct (requestId, lane, operationType, actor, payload)

    Returns: {"mutations": [...], "events": [...]} per Contract #3 §3.1
    """

    # Read actor from hydrated state to demonstrate JIT hydration access.
    actor_doc = state.get(op.actor)
    if actor_doc == None:
        fail("actor not found in hydrated state: " + op.actor)

    # Read payload fields.
    name = op.payload.name
    email = op.payload.email

    # Conditional branch: validate email is non-empty.
    if len(email) == 0:
        fail("payload.email must not be empty")

    # Build the new identity key. In production this uses nanoid.new().
    # The spike uses a deterministic key derived from requestId for test stability.
    identity_id = op.requestId  # spike-only substitution; Story 1.6 uses nanoid.new()
    identity_key = "vtx.identity." + identity_id
    email_aspect_key = identity_key + ".email"

    # Declare state transitions per Contract #3 §3.2.
    mutations = [
        {
            "op": "create",
            "key": identity_key,
            "document": {
                "class": "identity",
                "isDeleted": False,
                "data": {"name": name}
            }
        },
        {
            "op": "create",
            "key": email_aspect_key,
            "document": {
                "class": "email",
                "vertexKey": identity_key,
                "localName": "email",
                "isDeleted": False,
                "data": {"value": email, "verified": False}
            }
        }
    ]

    # Declare business event per Contract #3 §3.4.
    events = [
        {
            "class": "identityCreated",
            "data": {
                "identityKey": identity_key,
                "createdBy": op.actor
            }
        }
    ]

    return {"mutations": mutations, "events": events}
`

// buildAPIErgonomicsContext builds a realistic ScriptContext for the ergonomics test.
// It simulates what the Processor would build after commit step 4 (JIT Hydrate).
func buildAPIErgonomicsContext() ScriptContext {
	actorKey := "vtx.identity.St6mP3qBn4rT8wYxK7Vc"

	return ScriptContext{
		Operation: OperationEnvelope{
			RequestID:     "Rm7q3pntwzkfbcxv5p9j",
			Lane:          "default",
			OperationType: "CreateIdentity",
			Actor:         actorKey,
			SubmittedAt:   "2026-05-13T10:00:00.000Z",
			Payload: map[string]interface{}{
				"name":  "Andrew Solgan",
				"email": "andrew@lattice.example",
			},
			ContextHint: &ContextHint{
				Reads: []string{actorKey},
			},
		},
		Hydrated: map[string]VertexDoc{
			actorKey: {
				Key:       actorKey,
				Class:     "identity",
				IsDeleted: false,
				Data:      map[string]interface{}{"name": "System Admin"},
			},
		},
		DDLLookup: map[string]MetaVertex{
			"identity": {
				Key:               "vtx.meta.identity",
				CanonicalName:     "identity",
				PermittedCommands: []string{"CreateIdentity", "ClaimIdentity"},
			},
		},
	}
}

// RunAPIErgonomicsTest executes the realistic example script and validates
// that the return value conforms to Contract #3.
func RunAPIErgonomicsTest() error {
	fmt.Println("=== API ERGONOMICS TEST ===")
	fmt.Println()
	fmt.Println("Script: RealisticExampleScript (CreateIdentity)")
	fmt.Println("Context: one actor vertex pre-hydrated; payload with name and email")
	fmt.Println()

	ctx := buildAPIErgonomicsContext()
	result, err := RunScript(RealisticExampleScript, ctx)
	if err != nil {
		return fmt.Errorf("script execution failed: %w", err)
	}

	// Validate the result conforms to Contract #3.
	if err := validateScriptResult(result, ctx); err != nil {
		return fmt.Errorf("result validation failed: %w", err)
	}

	fmt.Printf("Mutations produced: %d\n", len(result.Mutations))
	for i, m := range result.Mutations {
		fmt.Printf("  [%d] op=%s key=%s\n", i, m.Op, m.Key)
		if m.Document != nil {
			fmt.Printf("       class=%v isDeleted=%v\n", m.Document["class"], m.Document["isDeleted"])
		}
	}

	fmt.Printf("\nEvents produced: %d\n", len(result.Events))
	for i, ev := range result.Events {
		fmt.Printf("  [%d] class=%s data=%v\n", i, ev.Class, ev.Data)
	}

	fmt.Println()
	fmt.Println("API Ergonomics: PASSED — script ran, return value conforms to Contract #3")
	fmt.Println()
	return nil
}

// validateScriptResult checks that the ScriptResult conforms to Contract #3 §3.2 and §3.4.
func validateScriptResult(r *ScriptResult, ctx ScriptContext) error {
	if r == nil {
		return fmt.Errorf("result is nil")
	}

	// Validate each mutation.
	for i, m := range r.Mutations {
		if m.Op != "create" && m.Op != "update" && m.Op != "tombstone" {
			return fmt.Errorf("mutation[%d]: invalid op %q", i, m.Op)
		}
		if m.Key == "" {
			return fmt.Errorf("mutation[%d]: key must not be empty", i)
		}
		if m.Op == "create" || m.Op == "update" {
			if m.Document == nil {
				return fmt.Errorf("mutation[%d]: create/update requires document", i)
			}
			if _, ok := m.Document["class"]; !ok {
				return fmt.Errorf("mutation[%d]: document missing required 'class' field", i)
			}
		}
	}

	// Validate each event.
	for i, ev := range r.Events {
		if ev.Class == "" {
			return fmt.Errorf("event[%d]: class must not be empty", i)
		}
		if ev.Data == nil {
			return fmt.Errorf("event[%d]: data must not be nil (may be empty dict)", i)
		}
	}

	// Validate expected mutations for CreateIdentity.
	if len(r.Mutations) != 2 {
		return fmt.Errorf("expected 2 mutations (identity vertex + email aspect), got %d", len(r.Mutations))
	}
	if r.Mutations[0].Op != "create" {
		return fmt.Errorf("expected mutation[0].op=create, got %s", r.Mutations[0].Op)
	}
	if !startsWith(r.Mutations[0].Key, "vtx.identity.") {
		return fmt.Errorf("expected mutation[0].key to start with 'vtx.identity.', got %s", r.Mutations[0].Key)
	}
	if len(r.Events) != 1 {
		return fmt.Errorf("expected 1 event (identityCreated), got %d", len(r.Events))
	}
	if r.Events[0].Class != "identityCreated" {
		return fmt.Errorf("expected event.class=identityCreated, got %s", r.Events[0].Class)
	}

	return nil
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
