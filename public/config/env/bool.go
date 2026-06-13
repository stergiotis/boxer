package env

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"

	cli "github.com/urfave/cli/v2"
)

// BoolVar is the typed env-var handle for boolean values.
type BoolVar struct {
	spec    Spec
	cacheMu sync.Mutex
	cached  bool
	value   bool
}

var _ VarI = (*BoolVar)(nil)

func NewBool(spec Spec) (v *BoolVar) {
	mustValidate(spec)
	spec.Origin = callerOrigin(2)
	spec.Type = TypeBool
	v = &BoolVar{spec: spec}
	register(v)
	return
}

func (inst *BoolVar) Spec() (out Spec) {
	return inst.spec
}

// Get returns the resolved value. Env-side parse failures fall back to
// the default; default-side parse failures panic (programmer error).
func (inst *BoolVar) Get() (out bool) {
	inst.cacheMu.Lock()
	defer inst.cacheMu.Unlock()
	if inst.cached {
		return inst.value
	}
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" {
		inst.value = inst.parseDefault()
	} else {
		parsed, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			inst.value = inst.parseDefault()
		} else {
			inst.value = parsed
		}
	}
	inst.cached = true
	return inst.value
}

func (inst *BoolVar) parseDefault() (out bool) {
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

func (inst *BoolVar) setCached(value bool) {
	inst.cacheMu.Lock()
	defer inst.cacheMu.Unlock()
	inst.value = value
	inst.cached = true
}

// WithBoolAction attaches a caller-supplied Action func to the
// cli.BoolFlag returned by AsCliFlag. The user action runs first; on
// success the parsed value is written to the cache.
func WithBoolAction(fn func(ctx *cli.Context, parsed bool) error) (opt FlagOption) {
	return func(o *flagOptions) {
		o.actionFn = fn
	}
}

func (inst *BoolVar) AsCliFlag(opts ...FlagOption) (out cli.Flag) {
	fo := resolveFlagOptions(inst.spec, opts)
	userAction, _ := fo.actionFn.(func(*cli.Context, bool) error)
	defaultValue := false
	if inst.spec.Default != "" {
		defaultValue = inst.parseDefault()
	}
	return &cli.BoolFlag{
		Name:     fo.cliFlagName,
		Usage:    inst.spec.Description,
		Category: string(inst.spec.Category),
		EnvVars:  []string{inst.spec.Name},
		Value:    defaultValue,
		Action: func(ctx *cli.Context, parsed bool) (err error) {
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

func (inst *BoolVar) SetForTest(t testing.TB, value string) {
	t.Helper()
	inst.cacheMu.Lock()
	inst.cached = false
	inst.value = false
	inst.cacheMu.Unlock()
	t.Setenv(inst.spec.Name, value)
	t.Cleanup(func() {
		inst.cacheMu.Lock()
		inst.cached = false
		inst.value = false
		inst.cacheMu.Unlock()
	})
}
