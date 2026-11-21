package env

import (
	"os"
)

// StringVar is the typed env-var handle for string values.
type StringVar struct {
	spec Spec
	res  resolved[string]
	// file is the NAME_FILE companion of a Sensitive variable; nil
	// otherwise.
	file *secretFile
}

var _ VarI = (*StringVar)(nil)

// NewString registers spec and returns a *StringVar. Intended for
// package-level var declarations; declaring an identical Spec again
// returns the first handle, and a conflicting one is reported by
// RegistrationErrors. A Sensitive spec also registers NAME_FILE
// (SecretFileSuffix), read when NAME is unset.
func NewString(spec Spec) (v *StringVar) {
	mustValidate(spec)
	spec.Origin = callerOrigin(2)
	spec.Type = TypeString
	fresh := &StringVar{spec: spec}
	v = register(fresh).(*StringVar)
	if v == fresh && spec.Sensitive {
		v.file = newSecretFile(spec)
	}
	return
}

func (inst *StringVar) Spec() (out Spec) {
	return inst.spec
}

// Get returns the resolved value: an Override, else the value the flag
// Action cached, else the env var when set and non-empty, else — for a
// Sensitive variable — the contents of the file NAME_FILE names, else
// Spec.Default (the tiers of ValueSourceE). The environment read is cached
// on first call.
func (inst *StringVar) Get() (out string) {
	out, _ = inst.res.get(inst.resolveEnv)
	return
}

func (inst *StringVar) resolveEnv() (out string, src ValueSourceE) {
	raw, ok := os.LookupEnv(inst.spec.Name)
	if ok && raw != "" {
		return raw, ValueSourceEnv
	}
	if v, fromFile := inst.file.read(); fromFile {
		return v, ValueSourceFile
	}
	return inst.spec.Default, ValueSourceDefault
}

// Lookup returns the raw env var value and whether it is set and non-empty.
func (inst *StringVar) Lookup() (raw string, set bool) {
	raw, set = os.LookupEnv(inst.spec.Name)
	if raw == "" {
		set = false
	}
	return
}

// RecordFlag records the value a command-line flag parsed
// (ValueSourceFlag); it shadows the environment and the default but not an
// Override. public/config/env/envcli calls it from a derived flag's Action.
func (inst *StringVar) RecordFlag(value string) {
	inst.res.setFlag(value)
}

// ClearCache forgets a recorded flag value and the cached environment
// read, so the next Get resolves from the environment again. The
// Override stays.
func (inst *StringVar) ClearCache() {
	inst.res.clearCache()
}

// Override pins the resolved value for this process (ValueSourceOverride):
// it shadows the flag, the environment and the default until ClearOverride,
// and is never written to the process environment, so child processes and
// `env list`'s CURRENT column do not see it. It is the seam a wrapper
// command uses to seed another component's variables before that component
// reads them in-process (ADR-0009, update 2026-08-28).
func (inst *StringVar) Override(value string) {
	inst.res.setOverride(value)
}

// ClearOverride removes the Override; resolution falls back to the flag,
// environment or default.
func (inst *StringVar) ClearOverride() {
	inst.res.clearOverride()
}

// ValueSource reports which tier Get's value comes from.
func (inst *StringVar) ValueSource() (src ValueSourceE) {
	_, src = inst.res.get(inst.resolveEnv)
	return
}
