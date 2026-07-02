// Pure Submit-Op logic: reads derivation, field coercion, schema labels.
// No DOM, no fetch — goja-tested, so syntax stays ES6-conservative
// (no Object.values / Number.isNaN — ES5 spellings per the test-strategy
// design).

// deriveReads walks a payload and collects every key-shaped string value
// (vtx.* / lnk.*). A read-dependent op (Tombstone/Update/Assign/Grant…) must
// declare the keys it reads, and those keys are exactly the target references
// in its payload — so the form can supply ContextHint.Reads automatically.
function deriveReads(payload) {
  var out = [];
  function isKey(s) {
    return typeof s === "string" && (s.indexOf("vtx.") === 0 || s.indexOf("lnk.") === 0);
  }
  function walk(v) {
    if (isKey(v)) {
      out.push(v);
    } else if (Array.isArray(v)) {
      for (var i = 0; i < v.length; i++) walk(v[i]);
    } else if (v && typeof v === "object") {
      var ks = Object.keys(v);
      for (var j = 0; j < ks.length; j++) walk(v[ks[j]]);
    }
  }
  walk(payload);
  return out;
}

// coerceField coerces one op-form field's raw string per its JSON-Schema type.
// Returns {omit:true} when an empty optional field should be dropped from the
// payload, else {value}. Throws on a missing required field, a non-numeric
// number, or malformed JSON — the message is shown to the operator verbatim.
// (Booleans ride the checkbox state, not a raw string — handled by the form.)
function coerceField(name, type, raw, isRequired) {
  var s = (raw || "").trim();
  if (s === "") {
    if (isRequired) throw new Error(name + " is required");
    return { omit: true };
  }
  if (type === "integer" || type === "number") {
    var n = Number(s);
    if (n !== n) throw new Error(name + ": not a number");
    return { value: n };
  }
  if (type === "array" || type === "object") {
    try {
      return { value: JSON.parse(s) };
    } catch (e) {
      throw new Error(name + ": invalid JSON — " + e.message);
    }
  }
  return { value: s };
}

// schemaTypeLabel renders a JSON-Schema property's type for the field header.
function schemaTypeLabel(p) {
  if (p.enum) return "enum";
  return Array.isArray(p.type) ? p.type.join("|") : (p.type || "any");
}

export { deriveReads, coerceField, schemaTypeLabel };
