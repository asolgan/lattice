package starlarksandbox

// The forbidden-operation battery, ported from internal/spike/starlark's
// sandbox_correctness.go spike into a real CI test over the shared Execute
// harness — the security gate this leaf exists to prove: every listed
// forbidden operation is rejected, and safe operations are not
// over-restricted.

import (
	"context"
	"testing"
)

func TestSandboxCorrectness_ForbiddenAndPermittedOps(t *testing.T) {
	tests := []struct {
		name       string
		script     string
		shouldFail bool
	}{
		{
			name: "forbidden: external module import via load()",
			// go.starlark.net's load statement is the module-import
			// mechanism; Execute's Thread carries no Load hook, so any
			// load() call fails.
			script: `
load("net/http", "get")
def execute():
    return 1
`,
			shouldFail: true,
		},
		{
			name: "forbidden: filesystem read via open()",
			// Starlark has no built-in open(); referencing it is an
			// unbound-name resolve error (SandboxViolation).
			script: `
def execute():
    f = open("/etc/passwd")
    return 1
`,
			shouldFail: true,
		},
		{
			name: "forbidden: os module access",
			script: `
def execute():
    secret = os.getenv("SECRET_KEY")
    return 1
`,
			shouldFail: true,
		},
		{
			name: "forbidden: non-deterministic time module",
			// No `time` global is supplied in this test's globals — only
			// the pure functions the leaf exposes when a caller wires
			// them in. Referencing a bare `time` name with none bound is
			// an unbound-name resolve error.
			script: `
def execute():
    now = time.now()
    return 1
`,
			shouldFail: true,
		},
		{
			name: "permitted: arithmetic and string operations",
			script: `
def execute():
    x = 1 + 2
    s = "hello " + "world"
    items = [i for i in range(3)]
    return x
`,
			shouldFail: false,
		},
		{
			name: "permitted: comprehensions and string methods",
			script: `
def execute():
    words = "a,b,c".split(",")
    upper = [w.upper() for w in words]
    return len(upper)
`,
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, sErr := Execute(context.Background(), tt.script, "execute", nil, nil, Budget{})
			failed := sErr != nil
			if failed != tt.shouldFail {
				t.Fatalf("script %q: failed=%v (err=%v), want shouldFail=%v", tt.name, failed, sErr, tt.shouldFail)
			}
			if failed && sErr.Code != SandboxViolation && sErr.Code != ScriptError {
				t.Fatalf("script %q: unexpected error code %q", tt.name, sErr.Code)
			}
		})
	}
}
