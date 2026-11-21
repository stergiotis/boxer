package env

import (
	"fmt"
	"os"
	"time"
)

// DurationVar is the typed env-var handle for time.Duration values.
type DurationVar struct {
	spec Spec
	res  resolved[time.Duration]
}

var _ VarI = (*DurationVar)(nil)

func NewDuration(spec Spec) (v *DurationVar) {
	mustValidate(spec)
	spec.Origin = callerOrigin(2)
	spec.Type = TypeDuration
	v = &DurationVar{spec: spec}
	v = register(v).(*DurationVar)
	return
}

func (inst *DurationVar) Spec() (out Spec) {
	return inst.spec
}

func (inst *DurationVar) Get() (out time.Duration) {
	out, _ = inst.res.get(inst.resolveEnv)
	return
}

// resolveEnv reads the process environment: an unset, empty or unparsable
// value yields Spec.Default (ValueSourceDefault).
func (inst *DurationVar) resolveEnv() (out time.Duration, src ValueSourceE) {
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" {
		return inst.DefaultValue(), ValueSourceDefault
	}
	parsed, parseErr := time.ParseDuration(raw)
	if parseErr != nil {
		return inst.DefaultValue(), ValueSourceDefault
	}
	return parsed, ValueSourceEnv
}

// DefaultValue parses Spec.Default; the zero value when it is empty. A
// default that does not parse panics (a programmer error).
func (inst *DurationVar) DefaultValue() (out time.Duration) {
	if inst.spec.Default == "" {
		return 0
	}
	parsed, parseErr := time.ParseDuration(inst.spec.Default)
	if parseErr != nil {
		panic(fmt.Sprintf("env: duration default %q for %q cannot be parsed: %v",
			inst.spec.Default, inst.spec.Name, parseErr))
	}
	return parsed
}

func (inst *DurationVar) Lookup() (raw string, set bool) {
	raw, set = os.LookupEnv(inst.spec.Name)
	if raw == "" {
		set = false
	}
	return
}

// RecordFlag records the value a command-line flag parsed
// (ValueSourceFlag); it shadows the environment and the default but not an
// Override. public/config/env/envcli calls it from a derived flag's Action.
func (inst *DurationVar) RecordFlag(value time.Duration) {
	inst.res.setFlag(value)
}

// ClearCache forgets a recorded flag value and the cached environment
// read, so the next Get resolves from the environment again. The
// Override stays.
func (inst *DurationVar) ClearCache() {
	inst.res.clearCache()
}

// Override pins the resolved value for this process (ValueSourceOverride):
// it shadows the flag, the environment and the default until ClearOverride,
// and is never written to the process environment, so child processes and
// `env list`'s CURRENT column do not see it. It is the seam a wrapper
// command uses to seed another component's variables before that component
// reads them in-process (ADR-0009, update 2026-08-28).
func (inst *DurationVar) Override(value time.Duration) {
	inst.res.setOverride(value)
}

// ClearOverride removes the Override; resolution falls back to the flag,
// environment or default.
func (inst *DurationVar) ClearOverride() {
	inst.res.clearOverride()
}

// ValueSource reports which tier Get's value comes from.
func (inst *DurationVar) ValueSource() (src ValueSourceE) {
	_, src = inst.res.get(inst.resolveEnv)
	return
}
