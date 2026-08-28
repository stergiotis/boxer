package env

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	cli "github.com/urfave/cli/v2"
)

// CategorialStringVar is the typed env-var handle for string values
// restricted to a fixed, declared set. Get() returns Spec.Default when
// the env value is not in the allowed set, mirroring the env-side
// parse-failure fallback used by BoolVar/IntVar/DurationVar (see
// ADR-0009 §3 update 2026-05-17). AsCliFlag honours the same fallback
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
	register(v)
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

func (inst *CategorialStringVar) setCached(value string) {
	inst.res.setFlag(value)
}

// AsCliFlag returns a cli.StringFlag derived from the Spec. The Usage
// string gains an "(one of: a|b|c)" suffix listing the allowed values.
// The Action's behaviour on an out-of-set value depends on the source
// — matching urfave/cli's existing env-vs-flag treatment for typed
// flags (BoolFlag / Int64Flag / DurationFlag): when the value came
// from the bound env var it silently falls back to Spec.Default and
// the chained user action runs on the default; when the value was
// supplied explicitly via `--flag=…` it surfaces as a CLI error.
func (inst *CategorialStringVar) AsCliFlag(opts ...FlagOption) (out cli.Flag) {
	fo := resolveFlagOptions(inst.spec, opts)
	userAction, _ := fo.actionFn.(func(*cli.Context, string) error)
	usage := inst.spec.Description
	usage += fmt.Sprintf(" (one of: %s)", strings.Join(inst.allowed, "|"))
	return &cli.StringFlag{
		Name:     fo.cliFlagName,
		Usage:    usage,
		Category: string(inst.spec.Category),
		EnvVars:  []string{inst.spec.Name},
		Value:    inst.spec.Default,
		Action: func(ctx *cli.Context, parsed string) (err error) {
			// Empty means unset; see StringVar.AsCliFlag.
			if parsed == "" {
				inst.res.clearCache()
				return
			}
			effective := parsed
			if !inst.IsAllowed(effective) {
				envRaw, envSet := os.LookupEnv(inst.spec.Name)
				if envSet && envRaw == parsed {
					// env-supplied invalid: silent fallback to Default
					effective = inst.spec.Default
				} else {
					// eh is unavailable here: public/observability/eh
					// imports this package to read its own formatting
					// switches, so eh.Errorf would close an import cycle.
					// The message carries the prefix the stack would
					// otherwise have supplied.
					return fmt.Errorf("env: %q is not in allowed values for --%s: %v", //boxer:lint disable=CS001 reason="eh imports config/env; eh.Errorf would close an import cycle"
						parsed, fo.cliFlagName, inst.allowed)
				}
			}
			if userAction != nil {
				err = userAction(ctx, effective)
				if err != nil {
					return
				}
			}
			inst.setCached(effective)
			return
		},
	}
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

// SetForTest sets the env var via t.Setenv and resets the cache. The
// cache is reset again on t.Cleanup so subsequent tests start fresh.
// Out-of-set values are allowed here; the next Get() will fall back to
// the default per the env-parse-failure convention.
func (inst *CategorialStringVar) SetForTest(t testing.TB, value string) {
	t.Helper()
	inst.res.reset()
	t.Setenv(inst.spec.Name, value)
	t.Cleanup(func() {
		inst.res.reset()
	})
}
