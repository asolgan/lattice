package starlark_spike

import (
	"fmt"
	"strings"

	starlarklib "go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// RunScript compiles and executes a Starlark script in a sandboxed environment.
//
// The sandbox:
//   - Provides the script with `op` (operation envelope), `state` (hydrated vertices),
//     and `ddl` (class definitions) as global bindings.
//   - Does NOT provide any I/O, os, http, time, or random modules.
//   - Uses go.starlark.net's default universe which contains only safe built-ins:
//     print, len, range, type, str, int, float, bool, list, dict, tuple, set, etc.
//
// The script MUST define an `execute(state, op)` function that returns a dict
// with shape {"mutations": [...], "events": [...]}.
//
// Returns a ScriptResult with parsed mutations and events, or an error.
func RunScript(src string, ctx ScriptContext) (*ScriptResult, error) {
	// Build the globals available to the script.
	// This is the COMPLETE set of what a script can access.
	// Anything not in this dict is unavailable — that is the sandbox.
	globals := starlarklib.StringDict{
		// "state" is the hydrated vertex map: key -> struct with .class, .isDeleted, .data
		"state": vertexMapToStarlark(ctx.Hydrated),
		// "op" is the operation envelope as a struct
		"op": operationEnvelopeToStarlark(ctx.Operation),
		// "ddl" is the DDL lookup map: class name -> struct with .canonicalName, .permittedCommands
		"ddl": ddlMapToStarlark(ctx.DDLLookup),
	}

	// Compile the script source. Compilation catches syntax errors.
	_, prog, err := starlarklib.SourceProgram("<script>", src, globals.Has)
	if err != nil {
		return nil, fmt.Errorf("starlark compile error: %w", err)
	}

	// Execute the script. This defines the `execute` function (and any helpers).
	thread := &starlarklib.Thread{Name: "processor"}
	defined, err := prog.Init(thread, globals)
	if err != nil {
		return nil, fmt.Errorf("starlark exec error: %w", err)
	}

	// Call the execute function with (state, op).
	executeFn, ok := defined["execute"]
	if !ok {
		return nil, fmt.Errorf("starlark script must define an 'execute' function")
	}

	result, err := starlarklib.Call(thread, executeFn, starlarklib.Tuple{
		globals["state"],
		globals["op"],
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("starlark execute error: %w", err)
	}

	// Parse the return value into ScriptResult.
	return parseScriptResult(result)
}

// parseScriptResult converts a Starlark return value to a ScriptResult.
// The script must return a dict with "mutations" and "events" keys (Contract #3 §3.1).
func parseScriptResult(val starlarklib.Value) (*ScriptResult, error) {
	resultDict, ok := val.(*starlarklib.Dict)
	if !ok {
		return nil, fmt.Errorf("StarlarkExecutionFailed: InvalidReturnShape — script must return a dict, got %s", val.Type())
	}

	mutations, err := parseMutations(resultDict)
	if err != nil {
		return nil, err
	}

	events, err := parseEvents(resultDict)
	if err != nil {
		return nil, err
	}

	return &ScriptResult{
		Mutations: mutations,
		Events:    events,
	}, nil
}

func parseMutations(d *starlarklib.Dict) ([]MutationOp, error) {
	raw, found, err := d.Get(starlarklib.String("mutations"))
	if err != nil || !found {
		return nil, nil // empty is valid per Contract #3 §3.1
	}

	mutList, ok := raw.(*starlarklib.List)
	if !ok {
		return nil, fmt.Errorf("StarlarkExecutionFailed: InvalidReturnShape — 'mutations' must be a list")
	}

	var ops []MutationOp
	for i := 0; i < mutList.Len(); i++ {
		item := mutList.Index(i)
		m, err := parseSingleMutation(item)
		if err != nil {
			return nil, fmt.Errorf("mutation[%d]: %w", i, err)
		}
		ops = append(ops, m)
	}
	return ops, nil
}

func parseSingleMutation(val starlarklib.Value) (MutationOp, error) {
	d, ok := val.(*starlarklib.Dict)
	if !ok {
		return MutationOp{}, fmt.Errorf("StarlarkExecutionFailed: InvalidReturnShape — mutation entry must be a dict")
	}

	op, err := getDictString(d, "op")
	if err != nil {
		return MutationOp{}, err
	}
	if op != "create" && op != "update" && op != "tombstone" {
		return MutationOp{}, fmt.Errorf("StarlarkExecutionFailed: InvalidReturnShape — mutation.op must be 'create', 'update', or 'tombstone', got %q", op)
	}

	key, err := getDictString(d, "key")
	if err != nil {
		return MutationOp{}, err
	}

	mut := MutationOp{Op: op, Key: key}

	// Extract document for create/update ops.
	if op == "create" || op == "update" {
		docRaw, found, _ := d.Get(starlarklib.String("document"))
		if found {
			mut.Document = starlarkValueToGo(docRaw).(map[string]interface{})
		}
	}

	return mut, nil
}

func parseEvents(d *starlarklib.Dict) ([]EventSpec, error) {
	raw, found, err := d.Get(starlarklib.String("events"))
	if err != nil || !found {
		return nil, nil // empty is valid
	}

	evList, ok := raw.(*starlarklib.List)
	if !ok {
		return nil, fmt.Errorf("StarlarkExecutionFailed: InvalidReturnShape — 'events' must be a list")
	}

	var specs []EventSpec
	for i := 0; i < evList.Len(); i++ {
		item := evList.Index(i)
		ev, err := parseSingleEvent(item)
		if err != nil {
			return nil, fmt.Errorf("event[%d]: %w", i, err)
		}
		specs = append(specs, ev)
	}
	return specs, nil
}

func parseSingleEvent(val starlarklib.Value) (EventSpec, error) {
	d, ok := val.(*starlarklib.Dict)
	if !ok {
		return EventSpec{}, fmt.Errorf("StarlarkExecutionFailed: InvalidReturnShape — event entry must be a dict")
	}

	class, err := getDictString(d, "class")
	if err != nil {
		return EventSpec{}, err
	}

	ev := EventSpec{Class: class, Data: map[string]interface{}{}}
	dataRaw, found, _ := d.Get(starlarklib.String("data"))
	if found {
		if dataDict, ok := dataRaw.(*starlarklib.Dict); ok {
			ev.Data = starlarkDictToGoMap(dataDict)
		}
	}

	return ev, nil
}

// ---- Starlark value conversion helpers ----

// vertexMapToStarlark converts a map of VertexDoc to a Starlark dict.
// Each vertex becomes a struct with fields: key, class, isDeleted, data.
func vertexMapToStarlark(m map[string]VertexDoc) *starlarklib.Dict {
	d := new(starlarklib.Dict)
	for k, v := range m {
		sv := starlarkstruct.FromStringDict(starlarkstruct.Default, starlarklib.StringDict{
			"key":       starlarklib.String(v.Key),
			"class":     starlarklib.String(v.Class),
			"isDeleted": starlarklib.Bool(v.IsDeleted),
			"data":      goMapToStarlarkDict(v.Data),
		})
		d.SetKey(starlarklib.String(k), sv)
	}
	return d
}

// operationEnvelopeToStarlark converts an OperationEnvelope to a Starlark struct.
// Fields: requestId, lane, operationType, actor, submittedAt, payload.
func operationEnvelopeToStarlark(op OperationEnvelope) *starlarkstruct.Struct {
	payload := starlarklib.StringDict{}
	if payloadMap, ok := op.Payload.(map[string]interface{}); ok {
		for k, v := range payloadMap {
			payload[k] = goValueToStarlark(v)
		}
	}

	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlarklib.StringDict{
		"requestId":     starlarklib.String(op.RequestID),
		"lane":          starlarklib.String(op.Lane),
		"operationType": starlarklib.String(op.OperationType),
		"actor":         starlarklib.String(op.Actor),
		"submittedAt":   starlarklib.String(op.SubmittedAt),
		"payload":       starlarkstruct.FromStringDict(starlarkstruct.Default, payload),
	})
}

// ddlMapToStarlark converts a map of MetaVertex to a Starlark dict.
func ddlMapToStarlark(m map[string]MetaVertex) *starlarklib.Dict {
	d := new(starlarklib.Dict)
	for k, v := range m {
		permCmds := starlarklib.NewList(nil)
		for _, cmd := range v.PermittedCommands {
			permCmds.Append(starlarklib.String(cmd))
		}
		sv := starlarkstruct.FromStringDict(starlarkstruct.Default, starlarklib.StringDict{
			"canonicalName":     starlarklib.String(v.CanonicalName),
			"permittedCommands": permCmds,
		})
		d.SetKey(starlarklib.String(k), sv)
	}
	return d
}

func goMapToStarlarkDict(m map[string]interface{}) *starlarklib.Dict {
	d := new(starlarklib.Dict)
	for k, v := range m {
		d.SetKey(starlarklib.String(k), goValueToStarlark(v))
	}
	return d
}

func goValueToStarlark(v interface{}) starlarklib.Value {
	switch x := v.(type) {
	case string:
		return starlarklib.String(x)
	case bool:
		return starlarklib.Bool(x)
	case int:
		return starlarklib.MakeInt(x)
	case int64:
		return starlarklib.MakeInt64(x)
	case float64:
		return starlarklib.Float(x)
	case map[string]interface{}:
		return goMapToStarlarkDict(x)
	case []interface{}:
		l := starlarklib.NewList(nil)
		for _, item := range x {
			l.Append(goValueToStarlark(item))
		}
		return l
	case nil:
		return starlarklib.None
	default:
		return starlarklib.String(fmt.Sprintf("%v", x))
	}
}

func starlarkValueToGo(v starlarklib.Value) interface{} {
	switch x := v.(type) {
	case starlarklib.String:
		return string(x)
	case starlarklib.Bool:
		return bool(x)
	case starlarklib.Int:
		i, _ := x.Int64()
		return i
	case starlarklib.Float:
		return float64(x)
	case *starlarklib.Dict:
		return starlarkDictToGoMap(x)
	case *starlarklib.List:
		result := make([]interface{}, x.Len())
		for i := 0; i < x.Len(); i++ {
			result[i] = starlarkValueToGo(x.Index(i))
		}
		return result
	case starlarklib.NoneType:
		return nil
	default:
		return x.String()
	}
}

func starlarkDictToGoMap(d *starlarklib.Dict) map[string]interface{} {
	result := make(map[string]interface{})
	for _, item := range d.Items() {
		k := string(item[0].(starlarklib.String))
		result[k] = starlarkValueToGo(item[1])
	}
	return result
}

func getDictString(d *starlarklib.Dict, key string) (string, error) {
	val, found, err := d.Get(starlarklib.String(key))
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("StarlarkExecutionFailed: InvalidReturnShape — missing required field %q", key)
	}
	s, ok := val.(starlarklib.String)
	if !ok {
		return "", fmt.Errorf("StarlarkExecutionFailed: InvalidReturnShape — field %q must be a string", key)
	}
	return strings.TrimSpace(string(s)), nil
}
