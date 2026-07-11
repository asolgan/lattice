package starlarksandbox

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"time"

	starlarklib "go.starlark.net/starlark"
)

func errBuiltin(msg string) error {
	return &starlarklib.EvalError{Msg: msg}
}

// CryptoBuiltins returns the pure crypto builtins as a StringDict, keyed by
// their exposed name (sha256, constant_time_equal). A caller composes this
// with any domain-specific additions (e.g. the Processor's
// crypto.sha256NanoID, which needs substrate — an internal dependency this
// leaf does not carry) before wrapping the result in a `crypto` module
// struct.
//
// Both builtins are pure (side-effect-free, deterministic): no I/O, no os
// access, no time, no randomness — output is fully determined by input.
func CryptoBuiltins() starlarklib.StringDict {
	sha256Fn := starlarklib.NewBuiltin("sha256", func(_ *starlarklib.Thread, _ *starlarklib.Builtin, args starlarklib.Tuple, kwargs []starlarklib.Tuple) (starlarklib.Value, error) {
		if len(args) != 1 || len(kwargs) != 0 {
			return nil, errBuiltin("crypto.sha256(s) takes exactly 1 positional argument")
		}
		s, ok := args[0].(starlarklib.String)
		if !ok {
			return nil, errBuiltin("crypto.sha256: argument must be a string, got " + args[0].Type())
		}
		sum := sha256.Sum256([]byte(string(s)))
		return starlarklib.String(hex.EncodeToString(sum[:])), nil
	})

	// constant_time_equal(a, b) → bool. Both operands MUST be fixed-length
	// (e.g. NanoIDs); length mismatch returns False immediately — timing
	// leaks the length of the shorter operand. Do NOT use with variable-
	// length secrets where length itself must stay confidential.
	constantTimeEqualFn := starlarklib.NewBuiltin("constant_time_equal", func(_ *starlarklib.Thread, _ *starlarklib.Builtin, args starlarklib.Tuple, kwargs []starlarklib.Tuple) (starlarklib.Value, error) {
		if len(args) != 2 || len(kwargs) != 0 {
			return nil, errBuiltin("crypto.constant_time_equal(a, b) takes exactly 2 positional arguments")
		}
		a, aOK := args[0].(starlarklib.String)
		b, bOK := args[1].(starlarklib.String)
		if !aOK {
			return nil, errBuiltin("crypto.constant_time_equal: first argument must be a string, got " + args[0].Type())
		}
		if !bOK {
			return nil, errBuiltin("crypto.constant_time_equal: second argument must be a string, got " + args[1].Type())
		}
		eq := subtle.ConstantTimeCompare([]byte(string(a)), []byte(string(b))) == 1
		return starlarklib.Bool(eq), nil
	})

	return starlarklib.StringDict{
		"sha256":              sha256Fn,
		"constant_time_equal": constantTimeEqualFn,
	}
}

// TimeBuiltins returns the pure time builtins as a StringDict: rfc3339_utc,
// rfc3339_add, weekday, seconds_of_day. All four are pure — deterministic,
// no wall-clock read — the output is a function of the input string(s)
// only; the host clock is never consulted.
func TimeBuiltins() starlarklib.StringDict {
	rfc3339UTCFn := starlarklib.NewBuiltin("rfc3339_utc", func(_ *starlarklib.Thread, _ *starlarklib.Builtin, args starlarklib.Tuple, kwargs []starlarklib.Tuple) (starlarklib.Value, error) {
		if len(args) != 1 || len(kwargs) != 0 {
			return nil, errBuiltin("time.rfc3339_utc(s) takes exactly 1 positional argument")
		}
		s, ok := args[0].(starlarklib.String)
		if !ok {
			return nil, errBuiltin("time.rfc3339_utc: argument must be a string, got " + args[0].Type())
		}
		t, err := time.Parse(time.RFC3339Nano, string(s))
		if err != nil {
			return nil, errBuiltin("InvalidArgument: not a valid RFC3339 timestamp: " + string(s))
		}
		return starlarklib.String(t.UTC().Format(time.RFC3339)), nil
	})

	rfc3339AddFn := starlarklib.NewBuiltin("rfc3339_add", func(_ *starlarklib.Thread, _ *starlarklib.Builtin, args starlarklib.Tuple, kwargs []starlarklib.Tuple) (starlarklib.Value, error) {
		if len(args) != 2 || len(kwargs) != 0 {
			return nil, errBuiltin("time.rfc3339_add(s, duration) takes exactly 2 positional arguments")
		}
		s, ok := args[0].(starlarklib.String)
		if !ok {
			return nil, errBuiltin("time.rfc3339_add: first argument must be a string, got " + args[0].Type())
		}
		durStr, ok := args[1].(starlarklib.String)
		if !ok {
			return nil, errBuiltin("time.rfc3339_add: second argument must be a string, got " + args[1].Type())
		}
		t, err := time.Parse(time.RFC3339Nano, string(s))
		if err != nil {
			return nil, errBuiltin("InvalidArgument: not a valid RFC3339 timestamp: " + string(s))
		}
		d, err := time.ParseDuration(string(durStr))
		if err != nil {
			return nil, errBuiltin("InvalidArgument: not a valid Go duration: " + string(durStr))
		}
		return starlarklib.String(t.Add(d).UTC().Format(time.RFC3339)), nil
	})

	weekdayFn := starlarklib.NewBuiltin("weekday", func(_ *starlarklib.Thread, _ *starlarklib.Builtin, args starlarklib.Tuple, kwargs []starlarklib.Tuple) (starlarklib.Value, error) {
		if len(args) != 1 || len(kwargs) != 0 {
			return nil, errBuiltin("time.weekday(s) takes exactly 1 positional argument")
		}
		s, ok := args[0].(starlarklib.String)
		if !ok {
			return nil, errBuiltin("time.weekday: argument must be a string, got " + args[0].Type())
		}
		t, err := time.Parse(time.RFC3339Nano, string(s))
		if err != nil {
			return nil, errBuiltin("InvalidArgument: not a valid RFC3339 timestamp: " + string(s))
		}
		return starlarklib.MakeInt(int(t.UTC().Weekday())), nil
	})

	secondsOfDayFn := starlarklib.NewBuiltin("seconds_of_day", func(_ *starlarklib.Thread, _ *starlarklib.Builtin, args starlarklib.Tuple, kwargs []starlarklib.Tuple) (starlarklib.Value, error) {
		if len(args) != 1 || len(kwargs) != 0 {
			return nil, errBuiltin("time.seconds_of_day(s) takes exactly 1 positional argument")
		}
		s, ok := args[0].(starlarklib.String)
		if !ok {
			return nil, errBuiltin("time.seconds_of_day: argument must be a string, got " + args[0].Type())
		}
		t, err := time.Parse(time.RFC3339Nano, string(s))
		if err != nil {
			return nil, errBuiltin("InvalidArgument: not a valid RFC3339 timestamp: " + string(s))
		}
		u := t.UTC()
		return starlarklib.MakeInt(u.Hour()*3600 + u.Minute()*60 + u.Second()), nil
	})

	return starlarklib.StringDict{
		"rfc3339_utc":    rfc3339UTCFn,
		"rfc3339_add":    rfc3339AddFn,
		"weekday":        weekdayFn,
		"seconds_of_day": secondsOfDayFn,
	}
}
