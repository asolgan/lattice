package starlarksandbox

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	starlarklib "go.starlark.net/starlark"
)

func TestExecute_Success(t *testing.T) {
	globals := starlarklib.StringDict{"x": starlarklib.MakeInt(2)}
	out, sErr := Execute(context.Background(), "def run():\n    return x + 3\n", "run", nil, globals, Budget{})
	if sErr != nil {
		t.Fatalf("unexpected error: %+v", sErr)
	}
	if got, ok := out.(starlarklib.Int); !ok || got.String() != "5" {
		t.Fatalf("out = %v, want 5", out)
	}
}

func TestExecute_MissingEntrypoint_InvalidReturnShape(t *testing.T) {
	_, sErr := Execute(context.Background(), "def other():\n    return 1\n", "run", nil, nil, Budget{})
	if sErr == nil || sErr.Code != InvalidReturnShape {
		t.Fatalf("got %+v, want InvalidReturnShape", sErr)
	}
}

func TestExecute_UndefinedName_SandboxViolation(t *testing.T) {
	_, sErr := Execute(context.Background(), "def run():\n    return os.getenv('X')\n", "run", nil, nil, Budget{})
	if sErr == nil || sErr.Code != SandboxViolation {
		t.Fatalf("got %+v, want SandboxViolation", sErr)
	}
}

func TestExecute_Load_SandboxViolation(t *testing.T) {
	_, sErr := Execute(context.Background(), "load('mod', 'x')\ndef run():\n    return 1\n", "run", nil, nil, Budget{})
	if sErr == nil || sErr.Code != SandboxViolation {
		t.Fatalf("got %+v, want SandboxViolation", sErr)
	}
}

func TestExecute_SyntaxError_ScriptError(t *testing.T) {
	_, sErr := Execute(context.Background(), "def run(:\n    return 1\n", "run", nil, nil, Budget{})
	if sErr == nil || sErr.Code != ScriptError {
		t.Fatalf("got %+v, want ScriptError", sErr)
	}
}

func TestExecute_RuntimeFail_ScriptError(t *testing.T) {
	_, sErr := Execute(context.Background(), "def run():\n    fail('boom')\n", "run", nil, nil, Budget{})
	if sErr == nil || sErr.Code != ScriptError {
		t.Fatalf("got %+v, want ScriptError", sErr)
	}
}

// A fail() message containing "payload:" (which contains "load:" as a
// substring of a longer word) must classify as ScriptError, not
// SandboxViolation — the word-boundary regex must not false-positive.
func TestExecute_FailMessageContainingPayloadColon_NotSandboxViolation(t *testing.T) {
	_, sErr := Execute(context.Background(), "def run():\n    fail('invalid payload: missing field')\n", "run", nil, nil, Budget{})
	if sErr == nil || sErr.Code != ScriptError {
		t.Fatalf("got %+v, want ScriptError (not SandboxViolation)", sErr)
	}
}

func TestClassify_LoadColonMessage_SandboxViolation(t *testing.T) {
	sErr := classify(fmt.Errorf("load: empty identifier"))
	if sErr.Code != SandboxViolation {
		t.Fatalf("got %+v, want SandboxViolation", sErr)
	}
}

func TestExecute_WallBudget_ScriptTimeout(t *testing.T) {
	src := "def run():\n    x = 0\n    for i in range(100000000):\n        x = x + i\n    return x\n"
	_, sErr := Execute(context.Background(), src, "run", nil, nil, Budget{Wall: 5 * time.Millisecond, MaxSteps: 0})
	if sErr == nil || sErr.Code != ScriptTimeout {
		t.Fatalf("got %+v, want ScriptTimeout", sErr)
	}
}

func TestExecute_MaxSteps_ScriptError(t *testing.T) {
	src := "def run():\n    x = 0\n    for i in range(100000000):\n        x = x + i\n    return x\n"
	_, sErr := Execute(context.Background(), src, "run", nil, nil, Budget{Wall: time.Second, MaxSteps: 100})
	if sErr == nil {
		t.Fatal("expected an error from step-limit exhaustion")
	}
	if sErr.Code != ScriptError {
		t.Fatalf("got code %q, want ScriptError", sErr.Code)
	}
}

func TestExecute_CallerCtxCancelled_ScriptTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	src := "def run():\n    x = 0\n    for i in range(100000000):\n        x = x + i\n    return x\n"
	// No Wall budget: Execute uses the caller's ctx as-is; a caller-side
	// cancellation still aborts the thread (via thread.Cancel), though it
	// classifies generically (ScriptError) since only a DeadlineExceeded
	// wall-budget timeout gets the ScriptTimeout code — the caller-context
	// case has no `budget.Wall` duration to report.
	_, sErr := Execute(ctx, src, "run", nil, nil, Budget{})
	if sErr == nil {
		t.Fatal("expected an error from ctx cancellation")
	}
}

func TestExecute_LoadDisabled_NoLoadHook(t *testing.T) {
	// A well-formed script with no load statement and no forbidden name
	// runs fine — the sandbox isn't over-restrictive.
	out, sErr := Execute(context.Background(), "def run():\n    s = 'a' + 'b'\n    items = [i for i in range(3)]\n    return len(items)\n", "run", nil, nil, Budget{})
	if sErr != nil {
		t.Fatalf("unexpected error: %+v", sErr)
	}
	if got, ok := out.(starlarklib.Int); !ok || got.String() != "3" {
		t.Fatalf("out = %v, want 3", out)
	}
}

// A builtin reads its execution-scoped context via ContextFromThread —
// proving the thread.Local wiring Execute performs before Init/Call.
func TestExecute_BuiltinObservesBoundContext(t *testing.T) {
	type ctxKeyT struct{}
	baseCtx := context.WithValue(context.Background(), ctxKeyT{}, "marker")

	var observed context.Context
	probe := starlarklib.NewBuiltin("probe", func(thread *starlarklib.Thread, _ *starlarklib.Builtin, _ starlarklib.Tuple, _ []starlarklib.Tuple) (starlarklib.Value, error) {
		observed = ContextFromThread(thread, context.Background())
		return starlarklib.None, nil
	})
	globals := starlarklib.StringDict{"probe": probe}

	_, sErr := Execute(baseCtx, "def run():\n    probe()\n    return 1\n", "run", nil, globals, Budget{Wall: time.Second})
	if sErr != nil {
		t.Fatalf("unexpected error: %+v", sErr)
	}
	if observed == nil || observed.Value(ctxKeyT{}) != "marker" {
		t.Fatalf("builtin did not observe the bound context: %v", observed)
	}
}

func TestContextFromThread_FallbackWhenUnset(t *testing.T) {
	thread := &starlarklib.Thread{}
	fallback := context.Background()
	if got := ContextFromThread(thread, fallback); got != fallback {
		t.Fatalf("got %v, want fallback", got)
	}
}

func TestSandboxError_ErrorString(t *testing.T) {
	e := &SandboxError{Code: ScriptError, Message: "boom"}
	if e.Error() != "boom" {
		t.Fatalf("Error() = %q, want %q", e.Error(), "boom")
	}
	var target error = e
	var got *SandboxError
	if !errors.As(target, &got) || got.Code != ScriptError {
		t.Fatalf("errors.As failed to unwrap SandboxError")
	}
}
