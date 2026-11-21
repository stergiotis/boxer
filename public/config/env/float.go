package env

import (
	"fmt"
	"os"
	"strconv"
)

// FloatVar is the typed env-var handle for 64-bit floating-point values.
type FloatVar struct {
	spec Spec
	res  resolved[float64]
}

var _ VarI = (*FloatVar)(nil)

func NewFloat(spec Spec) (v *FloatVar) {
	mustValidate(spec)
	spec.Origin = callerOrigin(2)
	spec.Type = TypeFloat64
	v = &FloatVar{spec: spec}
	v = register(v).(*FloatVar)
	return
}

func (inst *FloatVar) Spec() (out Spec) {
	return inst.spec
}

func (inst *FloatVar) Get() (out float64) {
	out, _ = inst.res.get(inst.resolveEnv)
	return
}

// resolveEnv reads the process environment: an unset, empty or unparsable
// value yields Spec.Default (ValueSourceDefault).
func (inst *FloatVar) resolveEnv() (out float64, src ValueSourceE) {
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" {
		return inst.DefaultValue(), ValueSourceDefault
	}
	parsed, parseErr := strconv.ParseFloat(raw, 64)
	if parseErr != nil {
		return inst.DefaultValue(), ValueSourceDefault
	}
	return parsed, ValueSourceEnv
}

// DefaultValue parses Spec.Default; the zero value when it is empty. A
// default that does not parse panics (a programmer error).
func (inst *FloatVar) DefaultValue() (out float64) {
	if inst.spec.Default == "" {
		return 0
	}
	parsed, parseErr := strconv.ParseFloat(inst.spec.Default, 64)
	if parseErr != nil {
		panic(fmt.Sprintf("env: float64 default %q for %q cannot be parsed: %v",
			inst.spec.Default, inst.spec.Name, parseErr))
	}
	return parsed
}

func (inst *FloatVar) Lookup() (raw string, set bool) {
	raw, set = os.LookupEnv(inst.spec.Name)
	if raw == "" {
		set = false
	}
	return
}

// RecordFlag records the value a command-line flag parsed
// (ValueSourceFlag); it shadows the environment and the default but not an
// Override. public/config/env/envcli calls it from a derived flag's Action.
func (inst *FloatVar) RecordFlag(value float64) {
	inst.res.setFlag(value)
}

// ClearCache forgets a recorded flag value and the cached environment
// read, so the next Get resolves from the environment again. The
// Override stays.
func (inst *FloatVar) ClearCache() {
	inst.res.clearCache()
}

// Override pins the resolved value for this process (ValueSourceOverride):
// it shadows the flag, the environment and the default until ClearOverride,
// and is never written to the process environment, so child processes and
// `env list`'s CURRENT column do not see it. It is the seam a wrapper
// command uses to seed another component's variables before that component
// reads them in-process (ADR-0009, update 2026-08-28).
func (inst *FloatVar) Override(value float64) {
	inst.res.setOverride(value)
}

// ClearOverride removes the Override; resolution falls back to the flag,
// environment or default.
func (inst *FloatVar) ClearOverride() {
	inst.res.clearOverride()
}

// ValueSource reports which tier Get's value comes from.
func (inst *FloatVar) ValueSource() (src ValueSourceE) {
	_, src = inst.res.get(inst.resolveEnv)
	return
}
