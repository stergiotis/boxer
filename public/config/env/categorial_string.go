package env

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
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
	cacheMu sync.Mutex
	cached  bool
	value   string
}

var _ VarI = (*CategorialStringVar)(nil)

// NewCategorialString registers spec with the restricted value set and
// returns a *CategorialStringVar. Allowed must be non-empty;
// Spec.Default must be non-empty and a member of allowed. All three
// violations panic at registration as programmer errors.
func NewCategorialString(spec Spec, allowed []string) (v *CategorialStringVar) {
	mustValidate(spec)
	if len(allowed) == 0 {
		panic(fmt.Sprintf("env: NewCategorialString(%q) requires non-empty allowed values", spec.Name))
	}
	if spec.Default == "" {
		panic(fmt.Sprintf("env: NewCategorialString(%q) requires non-empty Default — categorial vars cannot return an out-of-set zero value", spec.Name))
	}
	if !slices.Contains(allowed, spec.Default) {
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
	inst.cacheMu.Lock()
	defer inst.cacheMu.Unlock()
	if inst.cached {
		return inst.value
	}
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" || !inst.IsAllowed(raw) {
		inst.value = inst.spec.Default
	} else {
		inst.value = raw
	}
	inst.cached = true
	return inst.value
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
	inst.cacheMu.Lock()
	defer inst.cacheMu.Unlock()
	inst.value = value
	inst.cached = true
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
			effective := parsed
			if !inst.IsAllowed(effective) {
				envRaw, envSet := os.LookupEnv(inst.spec.Name)
				if envSet && envRaw == parsed {
					// env-supplied invalid: silent fallback to Default
					effective = inst.spec.Default
				} else {
					return fmt.Errorf("env: %q is not in allowed values for --%s: %v",
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

// SetForTest sets the env var via t.Setenv and resets the cache. The
// cache is reset again on t.Cleanup so subsequent tests start fresh.
// Out-of-set values are allowed here; the next Get() will fall back to
// the default per the env-parse-failure convention.
func (inst *CategorialStringVar) SetForTest(t testing.TB, value string) {
	t.Helper()
	inst.cacheMu.Lock()
	inst.cached = false
	inst.value = ""
	inst.cacheMu.Unlock()
	t.Setenv(inst.spec.Name, value)
	t.Cleanup(func() {
		inst.cacheMu.Lock()
		inst.cached = false
		inst.value = ""
		inst.cacheMu.Unlock()
	})
}
