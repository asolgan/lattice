package processor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/operatinggraph/lattice/internal/substrate"
)

// TestAuthTargetValidated exhaustively covers the rule that decides whether a
// resolved grant validated env.AuthContext.Target — only a platform scope=self
// grant and a task/ephemeralGrant bind it; every other path (including a nil
// permission) is unvalidated and fails closed.
func TestAuthTargetValidated(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		rp   *ResolvedPermission
		want bool
	}{
		{"nil permission (stub path)", nil, false},
		{"platform scope=self", &ResolvedPermission{Path: "platform", PlatformPermission: &PlatformPermission{Scope: "self"}}, true},
		{"platform scope=any", &ResolvedPermission{Path: "platform", PlatformPermission: &PlatformPermission{Scope: "any"}}, false},
		{"platform scope=specific", &ResolvedPermission{Path: "platform", PlatformPermission: &PlatformPermission{Scope: "specific"}}, false},
		{"platform nil permission pointer", &ResolvedPermission{Path: "platform"}, false},
		{"task with ephemeralGrant", &ResolvedPermission{Path: "task", EphemeralGrant: &EphemeralGrant{}}, true},
		{"task without ephemeralGrant", &ResolvedPermission{Path: "task"}, false},
		{"service path", &ResolvedPermission{Path: "service", ServiceAccess: &ServiceAccessEntry{}}, false},
		{"unknown path", &ResolvedPermission{Path: "future"}, false},
		{"empty path", &ResolvedPermission{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := authTargetValidated(c.rp); got != c.want {
				t.Fatalf("authTargetValidated(%+v) = %v, want %v", c.rp, got, c.want)
			}
		})
	}
}

// fakeResolvedAuthorizer authorizes every envelope and reports a caller-chosen
// ResolvedPermission, so a test can drive the sanitize call site through every
// auth-provenance shape without seeding full Capability KV docs.
type fakeResolvedAuthorizer struct{ resolved *ResolvedPermission }

func (f fakeResolvedAuthorizer) Authorize(_ context.Context, _ *OperationEnvelope) (Decision, error) {
	return Decision{Authorized: true, Resolved: f.resolved}, nil
}

// targetRecordingExecutor captures the authContext.target the script would see
// (the sanitized value) and commits nothing, so the pipeline runs clean.
type targetRecordingExecutor struct{ seen *string }

func (e targetRecordingExecutor) Execute(_ context.Context, env *OperationEnvelope, _ HydratedState) (ScriptResult, error) {
	*e.seen = ""
	if env.AuthContext != nil {
		*e.seen = env.AuthContext.Target
	}
	return ScriptResult{}, nil
}

// driveWithAuthContext dispatches one envelope (keyed by a valid-NanoID
// requestID) carrying the given authContext, so the executor can observe the
// authContext.target left after the step-3 sanitize.
func driveWithAuthContext(t *testing.T, cp *CommitPath, requestID string, ac *AuthContext) {
	t.Helper()
	env := newTestEnvelope(requestID)
	env.AuthContext = ac
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	msg := substrate.Message{
		Subject:      "ops.default",
		Body:         b,
		ReplySubject: "",
		Header:       func(string) string { return "" },
	}
	if outcome, _ := cp.dispatch(context.Background(), msg); outcome != OutcomeAccepted {
		t.Fatalf("dispatch outcome = %v, want OutcomeAccepted", outcome)
	}
}

// TestSanitizeForgeableAuthContextTarget proves the platform fix at the commit
// path: a script never observes an authContext.target that the resolving grant
// did not validate. This is the root closure for the forgeable-target surface
// that cafe/wellness/maintenance/lease-signing key their self/workplace
// exemptions on (persona-worlds W1 Inc 2a).
func TestSanitizeForgeableAuthContextTarget(t *testing.T) {
	t.Parallel()
	conn := occConn(t)
	provisionHarness(t, context.Background(), conn)

	actor := "vtx.identity." + testNanoID2 // newTestEnvelope's actor
	forged := "vtx.identity." + testNanoID1 // a target the caller does not own

	run := func(t *testing.T, requestID string, rp *ResolvedPermission, ac *AuthContext) string {
		var seen string
		exec := targetRecordingExecutor{seen: &seen}
		cp := newOCCPipelineAuth(t, conn, fakeResolvedAuthorizer{resolved: rp},
			occFakeHydrator{}, exec, &occFakeCommitter{}, &Metrics{})
		driveWithAuthContext(t, cp, requestID, ac)
		return seen
	}

	t.Run("scope=any forged target is blanked", func(t *testing.T) {
		got := run(t, "Rt7wKvQ2yBn4rT8mPxCz",
			&ResolvedPermission{Path: "platform", PlatformPermission: &PlatformPermission{Scope: "any"}},
			&AuthContext{Target: forged})
		if got != "" {
			t.Fatalf("a forged scope=any target reached the script as %q; want blanked", got)
		}
	})

	t.Run("scope=self validated target is preserved", func(t *testing.T) {
		got := run(t, "Bn4rT8wYxKvQ2yHj5kPm",
			&ResolvedPermission{Path: "platform", PlatformPermission: &PlatformPermission{Scope: "self"}},
			&AuthContext{Target: actor})
		if got != actor {
			t.Fatalf("a genuine scope=self self-book saw target %q; want %q", got, actor)
		}
	})

	t.Run("task ephemeralGrant target is preserved", func(t *testing.T) {
		got := run(t, "CxzvQ2yBn7wKmPtR4j6S",
			&ResolvedPermission{Path: "task", EphemeralGrant: &EphemeralGrant{}},
			&AuthContext{Target: actor})
		if got != actor {
			t.Fatalf("a task-grant target saw %q; want %q (task path validates target)", got, actor)
		}
	})

	t.Run("service path forged target is blanked", func(t *testing.T) {
		got := run(t, "TqBn4rKvw8YxPm2yHj5c",
			&ResolvedPermission{Path: "service", ServiceAccess: &ServiceAccessEntry{}},
			&AuthContext{Target: forged})
		if got != "" {
			t.Fatalf("a forged service-path target reached the script as %q; want blanked", got)
		}
	})

	t.Run("stub authorizer (nil resolved) leaves target untouched", func(t *testing.T) {
		// The test-only StubAuthorizer resolves nil and makes no security claim;
		// sanitize must not fire, or unit tests relying on the stub would silently
		// lose the target they set.
		var seen string
		exec := targetRecordingExecutor{seen: &seen}
		cp := newOCCPipeline(t, conn, occFakeHydrator{}, exec, &occFakeCommitter{}, &Metrics{})
		driveWithAuthContext(t, cp, "YxKvw8Bn4rT2qPm5Hj6c", &AuthContext{Target: forged})
		if seen != forged {
			t.Fatalf("stub-path target = %q; want it preserved (%q)", seen, forged)
		}
	})
}
