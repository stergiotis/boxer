package env

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"

	cli "github.com/urfave/cli/v2"
)

// FloatVar is the typed env-var handle for 64-bit floating-point values.
type FloatVar struct {
	spec    Spec
	cacheMu sync.Mutex
	cached  bool
	value   float64
}

var _ VarI = (*FloatVar)(nil)

func NewFloat(spec Spec) (v *FloatVar) {
	mustValidate(spec)
	spec.Origin = callerOrigin(2)
	spec.Type = TypeFloat64
	v = &FloatVar{spec: spec}
	register(v)
	return
}

func (inst *FloatVar) Spec() (out Spec) {
	return inst.spec
}

func (inst *FloatVar) Get() (out float64) {
	inst.cacheMu.Lock()
	defer inst.cacheMu.Unlock()
	if inst.cached {
		return inst.value
	}
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" {
		inst.value = inst.parseDefault()
	} else {
		parsed, parseErr := strconv.ParseFloat(raw, 64)
		if parseErr != nil {
			inst.value = inst.parseDefault()
		} else {
			inst.value = parsed
		}
	}
	inst.cached = true
	return inst.value
}

func (inst *FloatVar) parseDefault() (out float64) {
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

func (inst *FloatVar) setCached(value float64) {
	inst.cacheMu.Lock()
	defer inst.cacheMu.Unlock()
	inst.value = value
	inst.cached = true
}

// WithFloat64Action attaches a caller-supplied Action func to the
// cli.Float64Flag returned by AsCliFlag. The user action runs first; on
// success the parsed value is written to the cache.
func WithFloat64Action(fn func(ctx *cli.Context, parsed float64) error) (opt FlagOption) {
	return func(o *flagOptions) {
		o.actionFn = fn
	}
}

func (inst *FloatVar) AsCliFlag(opts ...FlagOption) (out cli.Flag) {
	fo := resolveFlagOptions(inst.spec, opts)
	userAction, _ := fo.actionFn.(func(*cli.Context, float64) error)
	defaultValue := float64(0)
	if inst.spec.Default != "" {
		defaultValue = inst.parseDefault()
	}
	return &cli.Float64Flag{
		Name:     fo.cliFlagName,
		Usage:    inst.spec.Description,
		Category: string(inst.spec.Category),
		EnvVars:  []string{inst.spec.Name},
		Value:    defaultValue,
		Action: func(ctx *cli.Context, parsed float64) (err error) {
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

func (inst *FloatVar) SetForTest(t testing.TB, value string) {
	t.Helper()
	inst.cacheMu.Lock()
	inst.cached = false
	inst.value = 0
	inst.cacheMu.Unlock()
	t.Setenv(inst.spec.Name, value)
	t.Cleanup(func() {
		inst.cacheMu.Lock()
		inst.cached = false
		inst.value = 0
		inst.cacheMu.Unlock()
	})
}
