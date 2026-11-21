package env

import (
	"fmt"
	"os"
	"slices"
)

// CategorialStringVar is the typed env-var handle for string values
// restricted to a fixed, declared set. Get() returns Spec.Default when
// the env value is not in the allowed set, mirroring the env-side
// parse-failure fallback used by BoolVar/IntVar/DurationVar (see
// ADR-0009 §3 update 2026-05-17). The flag envcli.Flag derives honours the same fallback
// for env-supplied values; explicit `--flag=X` invocations with a
// value outside the set surface as a hard CLI error.
type CategorialStringVar struct {
	spec Spec
	// allowed is set once in NewCategorialString and never mutated;
	// membership checks therefore need no synchronisation.
	allowed []string
	res     resolved[string]
}

var _ VarI = (*CategorialStringVar)(nil)

// NewCategorialString registers spec with the restricted value set and
// returns a *CategorialStringVar. Allowed must be non-empty and must not
// contain the empty string; Spec.Default is either a member of allowed or
// empty. An empty Default declares "unset" as a legitimate resolved state:
// Get returns "" until a value in the set is supplied, so a variable whose
// absence means "idle" or "no selection" needs no sentinel member. Violations
// panic at registration as programmer errors.
func NewCategorialString(spec Spec, allowed []string) (v *CategorialStringVar) {
	mustValidate(spec)
	if len(allowed) == 0 {
		panic(fmt.Sprintf("env: NewCategorialString(%q) requires non-empty allowed values", spec.Name))
	}
	if slices.Contains(allowed, "") {
		panic(fmt.Sprintf("env: NewCategorialString(%q): the empty string cannot be an allowed value — empty means unset", spec.Name))
	}
	if spec.Default != "" && !slices.Contains(allowed, spec.Default) {
		panic(fmt.Sprintf("env: default %q for %q is not in allowed values %v",
			spec.Default, spec.Name, allowed))
	}
	spec.Origin = callerOrigin(2)
	spec.Type = TypeCategorialString
	allowedCopy := append([]string(nil), allowed...)
	spec.Allowed = allowedCopy
	v = &CategorialStringVar{spec: spec, allowed: allowedCopy}
	v = register(v).(*CategorialStringVar)
	return
}

// Spec returns the registered Spec. The returned value carries a
// defensive copy of Allowed so callers cannot mutate the registered
// membership set through Spec().Allowed[i].
func (inst *CategorialStringVar) Spec() (out Spec) {
	out = inst.spec
	out.Allowed = append([]string(nil), inst.allowed...)
	return
}

// Allowed returns the declared value set. The slice is a defensive copy;
// callers cannot mutate the registered spec.
func (inst *CategorialStringVar) Allowed() (out []string) {
	out = make([]string, len(inst.allowed))
	copy(out, inst.allowed)
	return
}

// IsAllowed reports whether value is in the declared set.
func (inst *CategorialStringVar) IsAllowed(value string) (ok bool) {
	return slices.Contains(inst.allowed, value)
}

// Get returns the resolved value. On first call: reads the env var; if
// non-empty and in the allowed set that becomes the cached value,
// otherwise Spec.Default. An out-of-set env value is treated as user
// error and silently falls back to the default (same convention as
// BoolVar/IntVar/DurationVar).
func (inst *CategorialStringVar) Get() (out string) {
	out, _ = inst.res.get(inst.resolveEnv)
	return
}

func (inst *CategorialStringVar) resolveEnv() (out string, src ValueSourceE) {
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" || !inst.IsAllowed(raw) {
		return inst.spec.Default, ValueSourceDefault
	}
	return raw, ValueSourceEnv
}

// Lookup returns the raw env var value and whether it is set and non-empty.
// It does not check membership in the allowed set; callers wanting that
// signal should use IsAllowed on the returned raw value.
func (inst *CategorialStringVar) Lookup() (raw string, set bool) {
	raw, set = os.LookupEnv(inst.spec.Name)
	if raw == "" {
		set = false
	}
	return
}

// RecordFlag records the value a command-line flag parsed
// (ValueSourceFlag); it shadows the environment and the default but not an
// Override. public/config/env/envcli calls it from a derived flag's Action.
func (inst *CategorialStringVar) RecordFlag(value string) {
	inst.res.setFlag(value)
}

// ClearCache forgets a recorded flag value and the cached environment
// read, so the next Get resolves from the environment again. The
// Override stays.
func (inst *CategorialStringVar) ClearCache() {
	inst.res.clearCache()
}

// Override pins the resolved value for this process (ValueSourceOverride):
// it shadows the flag, the environment and the default until ClearOverride,
// and is never written to the process environment, so child processes and
// `env list`'s CURRENT column do not see it. It is the seam a wrapper
// command uses to seed another component's variables before that component
// reads them in-process (ADR-0009, update 2026-08-28).
func (inst *CategorialStringVar) Override(value string) {
	inst.res.setOverride(value)
}

// ClearOverride removes the Override; resolution falls back to the flag,
// environment or default.
func (inst *CategorialStringVar) ClearOverride() {
	inst.res.clearOverride()
}

// ValueSource reports which tier Get's value comes from.
func (inst *CategorialStringVar) ValueSource() (src ValueSourceE) {
	_, src = inst.res.get(inst.resolveEnv)
	return
}
