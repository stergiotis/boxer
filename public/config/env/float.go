package env

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	cli "github.com/urfave/cli/v2"
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
	register(v)
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
		return inst.parseDefault(), ValueSourceDefault
	}
	parsed, parseErr := strconv.ParseFloat(raw, 64)
	if parseErr != nil {
		return inst.parseDefault(), ValueSourceDefault
	}
	return parsed, ValueSourceEnv
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
	inst.res.setFlag(value)
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

func (inst *FloatVar) SetForTest(t testing.TB, value string) {
	t.Helper()
	inst.res.reset()
	t.Setenv(inst.spec.Name, value)
	t.Cleanup(func() {
		inst.res.reset()
	})
}
