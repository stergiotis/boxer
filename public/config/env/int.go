package env

import (
	"fmt"
	"os"
	"strconv"
)

// IntVar is the typed env-var handle for 64-bit signed integer values.
type IntVar struct {
	spec Spec
	res  resolved[int64]
}

var _ VarI = (*IntVar)(nil)

func NewInt(spec Spec) (v *IntVar) {
	mustValidate(spec)
	spec.Origin = callerOrigin(2)
	spec.Type = TypeInt64
	v = &IntVar{spec: spec}
	v = register(v).(*IntVar)
	return
}

func (inst *IntVar) Spec() (out Spec) {
	return inst.spec
}

func (inst *IntVar) Get() (out int64) {
	out, _ = inst.res.get(inst.resolveEnv)
	return
}

// resolveEnv reads the process environment: an unset, empty or unparsable
// value yields Spec.Default (ValueSourceDefault).
func (inst *IntVar) resolveEnv() (out int64, src ValueSourceE) {
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" {
		return inst.DefaultValue(), ValueSourceDefault
	}
	parsed, parseErr := strconv.ParseInt(raw, 10, 64)
	if parseErr != nil {
		return inst.DefaultValue(), ValueSourceDefault
	}
	return parsed, ValueSourceEnv
}

// DefaultValue parses Spec.Default; the zero value when it is empty. A
// default that does not parse panics (a programmer error).
func (inst *IntVar) DefaultValue() (out int64) {
	if inst.spec.Default == "" {
		return 0
	}
	parsed, parseErr := strconv.ParseInt(inst.spec.Default, 10, 64)
	if parseErr != nil {
		panic(fmt.Sprintf("env: int default %q for %q cannot be parsed: %v",
			inst.spec.Default, inst.spec.Name, parseErr))
	}
	return parsed
}

func (inst *IntVar) Lookup() (raw string, set bool) {
	raw, set = os.LookupEnv(inst.spec.Name)
	if raw == "" {
		set = false
	}
	return
}

// RecordFlag records the value a command-line flag parsed
// (ValueSourceFlag); it shadows the environment and the default but not an
// Override. public/config/env/envcli calls it from a derived flag's Action.
func (inst *IntVar) RecordFlag(value int64) {
	inst.res.setFlag(value)
}

// ClearCache forgets a recorded flag value and the cached environment
// read, so the next Get resolves from the environment again. The
// Override stays.
func (inst *IntVar) ClearCache() {
	inst.res.clearCache()
}

// Override pins the resolved value for this process (ValueSourceOverride):
// it shadows the flag, the environment and the default until ClearOverride,
// and is never written to the process environment, so child processes and
// `env list`'s CURRENT column do not see it. It is the seam a wrapper
// command uses to seed another component's variables before that component
// reads them in-process (ADR-0009, update 2026-08-28).
func (inst *IntVar) Override(value int64) {
	inst.res.setOverride(value)
}

// ClearOverride removes the Override; resolution falls back to the flag,
// environment or default.
func (inst *IntVar) ClearOverride() {
	inst.res.clearOverride()
}

// ValueSource reports which tier Get's value comes from.
func (inst *IntVar) ValueSource() (src ValueSourceE) {
	_, src = inst.res.get(inst.resolveEnv)
	return
}
