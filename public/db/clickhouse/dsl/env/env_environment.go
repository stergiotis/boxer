//go:build llm_generated_opus47

// Package env models the Chez-Scheme-style environment that surrounds a
// ClickHouse SELECT statement. An Environment holds settings, parameters, and
// the FORMAT clause as Go values — anything a pass might want to manipulate
// without round-tripping through SQL text.
//
// Decision and scope: see ADR-0006 (doc/adr/0006-nanopass-environment-and-first-class-pass.md).
//
// v1 scope. [Extract] handles the leading `SET key = value;` prelude (split into
// session settings vs. params via the `param_` name prefix) and populates
// [Param].Type by scanning `{name: Type}` slots in the body. The inline
// `... SETTINGS k=v` clause and the `... FORMAT FormatName` clause are not
// stripped from the body in v1 — passes that operate on them continue to
// rewrite the body's CST. SessionSettings and Params round-trip cleanly;
// StatementSettings and Format are populated only when a pass explicitly sets
// them (and [Integrate] will emit them on output).
package env

// Environment holds the SETTINGS / params / FORMAT context of a SELECT.
//
// Maps are nil-safe to read but must be allocated before write. [NewEnvironment]
// returns a zero-value Environment with all maps allocated.
type Environment struct {
	// SessionSettings hold values from leading `SET key = value;` lines whose
	// key does NOT start with `param_`.
	SessionSettings map[string]Setting

	// StatementSettings hold values from inline `... SETTINGS k=v` clauses.
	// Populated only when a pass explicitly writes here in v1.
	StatementSettings map[string]Setting

	// Params hold the unified view of named parameters: from `SET param_x = ...;`
	// lines and from `{x: Type}` slot occurrences in the body.
	Params map[string]Param

	// Format holds the value of a `FORMAT FormatName` clause. "" if absent.
	// Populated only when a pass explicitly writes here in v1.
	Format string
}

// Setting represents a single SETTINGS-style key/value pair.
type Setting struct {
	Name string
	// Raw is the verbatim SQL text of the value (e.g. `5`, `'foo'`, `[1,2]`).
	Raw string
	// Value is the Go-typed value when known; nil if the value has not been
	// (or cannot yet be) deserialised.
	Value any
}

// Param represents a named parameter under the unified Params view.
//
// Resolution states:
//   - Both Raw and Type populated → resolved; Value holds the deserialised value.
//   - Only Type populated (slot exists, no SET) → unresolved.
//   - Only Raw populated (SET exists, no slot) → resolved-without-slot-type;
//     Value remains nil until a consumer requests deserialisation against an
//     explicit type.
//   - Neither → not in the env.
type Param struct {
	Name string
	// Type is the ClickHouse type from a `{name: Type}` slot occurrence in the
	// body. Empty if no slot was seen.
	Type string
	// Raw is the verbatim SQL text from `SET param_name = <here>`. Empty if no
	// SET was seen.
	Raw string
	// Value is the deserialised Go value when both Raw and Type are populated;
	// nil otherwise.
	Value any
}

// NewEnvironment returns a fresh Environment with all maps allocated.
func NewEnvironment() *Environment {
	return &Environment{
		SessionSettings:   make(map[string]Setting, 4),
		StatementSettings: make(map[string]Setting, 4),
		Params:            make(map[string]Param, 8),
	}
}

// IsResolved reports whether the param has both a slot type and a SET value.
func (p Param) IsResolved() bool {
	return p.Type != "" && p.Raw != ""
}

// IsUnresolved reports whether the param is referenced from the body (slot
// type known) but has no SET value bound to it.
func (p Param) IsUnresolved() bool {
	return p.Type != "" && p.Raw == ""
}
