package env

import (
	"fmt"
	"os"
	"strconv"
)

// BoolVar is the typed env-var handle for boolean values.
type BoolVar struct {
	spec Spec
	res  resolved[bool]
}

var _ VarI = (*BoolVar)(nil)

func NewBool(spec Spec) (v *BoolVar) {
	mustValidate(spec)
	spec.Origin = callerOrigin(2)
	spec.Type = TypeBool
	v = &BoolVar{spec: spec}
	v = register(v).(*BoolVar)
	return
}

func (inst *BoolVar) Spec() (out Spec) {
	return inst.spec
}

// Get returns the resolved value. Env-side parse failures fall back to
// the default; default-side parse failures panic (programmer error).
func (inst *BoolVar) Get() (out bool) {
	out, _ = inst.res.get(inst.resolveEnv)
	return
}

// resolveEnv reads the process environment: an unset, empty or unparsable
// value yields Spec.Default (ValueSourceDefault).
func (inst *BoolVar) resolveEnv() (out bool, src ValueSourceE) {
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" {
		return inst.DefaultValue(), ValueSourceDefault
	}
	parsed, parseErr := strconv.ParseBool(raw)
	if parseErr != nil {
		return inst.DefaultValue(), ValueSourceDefault
	}
	return parsed, ValueSourceEnv
}

// DefaultValue parses Spec.Default; the zero value when it is empty. A
// default that does not parse panics (a programmer error).
func (inst *BoolVar) DefaultValue() (out bool) {
	if inst.spec.Default == "" {
		return false
	}
	parsed, parseErr := strconv.ParseBool(inst.spec.Default)
	if parseErr != nil {
		panic(fmt.Sprintf("env: bool default %q for %q cannot be parsed: %v",
			inst.spec.Default, inst.spec.Name, parseErr))
	}
	return parsed
}

func (inst *BoolVar) Lookup() (raw string, set bool) {
	raw, set = os.LookupEnv(inst.spec.Name)
	if raw == "" {
		set = false
	}
	return
}

// RecordFlag records the value a command-line flag parsed
// (ValueSourceFlag); it shadows the environment and the default but not an
// Override. public/config/env/envcli calls it from a derived flag's Action.
func (inst *BoolVar) RecordFlag(value bool) {
	inst.res.setFlag(value)
}

// ClearCache forgets a recorded flag value and the cached environment
// read, so the next Get resolves from the environment again. The
// Override stays.
func (inst *BoolVar) ClearCache() {
	inst.res.clearCache()
}

// Override pins the resolved value for this process (ValueSourceOverride):
// it shadows the flag, the environment and the default until ClearOverride,
// and is never written to the process environment, so child processes and
// `env list`'s CURRENT column do not see it. It is the seam a wrapper
// command uses to seed another component's variables before that component
// reads them in-process (ADR-0009, update 2026-08-28).
func (inst *BoolVar) Override(value bool) {
	inst.res.setOverride(value)
}

// ClearOverride removes the Override; resolution falls back to the flag,
// environment or default.
func (inst *BoolVar) ClearOverride() {
	inst.res.clearOverride()
}

// ValueSource reports which tier Get's value comes from.
func (inst *BoolVar) ValueSource() (src ValueSourceE) {
	_, src = inst.res.get(inst.resolveEnv)
	return
}
