//go:build llm_generated_opus47

package env

import (
	"os"
	"sync"
	"testing"

	cli "github.com/urfave/cli/v2"
)

// StringVar is the typed env-var handle for string values.
type StringVar struct {
	spec    Spec
	cacheMu sync.Mutex
	cached  bool
	value   string
}

var _ VarI = (*StringVar)(nil)

// NewString registers spec and returns a *StringVar. Intended for
// package-level var declarations; calling twice with the same Spec.Name
// panics.
func NewString(spec Spec) (v *StringVar) {
	mustValidate(spec)
	spec.Origin = callerOrigin(2)
	spec.Type = TypeString
	v = &StringVar{spec: spec}
	register(v)
	return
}

func (inst *StringVar) Spec() (out Spec) {
	return inst.spec
}

// Get returns the resolved value. On first call: reads the env var; if
// non-empty that becomes the cached value, otherwise Spec.Default. The
// CLI Action installed by AsCliFlag also writes through this cache so
// callers after flag parsing see the parsed value.
func (inst *StringVar) Get() (out string) {
	inst.cacheMu.Lock()
	defer inst.cacheMu.Unlock()
	if inst.cached {
		return inst.value
	}
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" {
		inst.value = inst.spec.Default
	} else {
		inst.value = raw
	}
	inst.cached = true
	return inst.value
}

// Lookup returns the raw env var value and whether it is set and non-empty.
func (inst *StringVar) Lookup() (raw string, set bool) {
	raw, set = os.LookupEnv(inst.spec.Name)
	if raw == "" {
		set = false
	}
	return
}

func (inst *StringVar) setCached(value string) {
	inst.cacheMu.Lock()
	defer inst.cacheMu.Unlock()
	inst.value = value
	inst.cached = true
}

// WithStringAction attaches a caller-supplied Action func to the
// cli.StringFlag returned by AsCliFlag. The user action runs first; on
// success the parsed value is written to the cache so subsequent
// inst.Get() calls observe it.
func WithStringAction(fn func(ctx *cli.Context, parsed string) error) (opt FlagOption) {
	return func(o *flagOptions) {
		o.actionFn = fn
	}
}

// AsCliFlag returns a cli.StringFlag derived from the Spec. The Action
// runs an optional caller-supplied user action (via WithStringAction)
// and then writes the parsed value into the cache so post-parse reads
// see it.
func (inst *StringVar) AsCliFlag(opts ...FlagOption) (out cli.Flag) {
	fo := resolveFlagOptions(inst.spec, opts)
	userAction, _ := fo.actionFn.(func(*cli.Context, string) error)
	return &cli.StringFlag{
		Name:     fo.cliFlagName,
		Usage:    inst.spec.Description,
		Category: string(inst.spec.Category),
		EnvVars:  []string{inst.spec.Name},
		Value:    inst.spec.Default,
		Action: func(ctx *cli.Context, parsed string) (err error) {
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
func (inst *StringVar) SetForTest(t testing.TB, value string) {
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
