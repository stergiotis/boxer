//go:build llm_generated_opus47

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
// ADR-0009 §3 update 2026-05-17).
type CategorialStringVar struct {
	spec    Spec
	allowed []string
	cacheMu sync.Mutex
	cached  bool
	value   string
}

var _ VarI = (*CategorialStringVar)(nil)

// NewCategorialString registers spec with the restricted value set and
// returns a *CategorialStringVar. Allowed must be non-empty; if
// Spec.Default is non-empty it must be a member of allowed — both
// violations panic at registration as programmer errors.
func NewCategorialString(spec Spec, allowed []string) (v *CategorialStringVar) {
	mustValidate(spec)
	if len(allowed) == 0 {
		panic(fmt.Sprintf("env: NewCategorialString(%q) requires non-empty allowed values", spec.Name))
	}
	if spec.Default != "" && !slices.Contains(allowed, spec.Default) {
		panic(fmt.Sprintf("env: default %q for %q is not in allowed values %v",
			spec.Default, spec.Name, allowed))
	}
	spec.Origin = callerOrigin(2)
	spec.Type = TypeCategorialString
	spec.Allowed = append([]string(nil), allowed...)
	v = &CategorialStringVar{spec: spec, allowed: spec.Allowed}
	register(v)
	return
}

func (inst *CategorialStringVar) Spec() (out Spec) {
	return inst.spec
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
	if !ok || raw == "" || !inst.isAllowedLocked(raw) {
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

func (inst *CategorialStringVar) isAllowedLocked(value string) (ok bool) {
	return slices.Contains(inst.allowed, value)
}

// AsCliFlag returns a cli.StringFlag derived from the Spec. The Usage
// string gains an "(one of: a|b|c)" suffix listing the allowed values.
// The Action validates membership before chaining a caller-supplied
// user action (via WithStringAction) and writing the parsed value to
// the cache; a non-allowed value returns an error and the cache is
// left unchanged.
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
			if !inst.IsAllowed(parsed) {
				return fmt.Errorf("env: %q is not in allowed values for %s: %v",
					parsed, inst.spec.Name, inst.allowed)
			}
			if userAction != nil {
				err = userAction(ctx, parsed)
				if err != nil {
					return
				}
			}
			inst.setCached(parsed)
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

