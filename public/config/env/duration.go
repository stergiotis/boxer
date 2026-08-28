package env

import (
	"fmt"
	"os"
	"testing"
	"time"

	cli "github.com/urfave/cli/v2"
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
	register(v)
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
		return inst.parseDefault(), ValueSourceDefault
	}
	parsed, parseErr := time.ParseDuration(raw)
	if parseErr != nil {
		return inst.parseDefault(), ValueSourceDefault
	}
	return parsed, ValueSourceEnv
}

func (inst *DurationVar) parseDefault() (out time.Duration) {
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

func (inst *DurationVar) setCached(value time.Duration) {
	inst.res.setFlag(value)
}

// WithDurationAction attaches a caller-supplied Action func to the
// cli.DurationFlag returned by AsCliFlag. The user action runs first;
// on success the parsed value is written to the cache.
func WithDurationAction(fn func(ctx *cli.Context, parsed time.Duration) error) (opt FlagOption) {
	return func(o *flagOptions) {
		o.actionFn = fn
	}
}

func (inst *DurationVar) AsCliFlag(opts ...FlagOption) (out cli.Flag) {
	fo := resolveFlagOptions(inst.spec, opts)
	userAction, _ := fo.actionFn.(func(*cli.Context, time.Duration) error)
	defaultValue := time.Duration(0)
	if inst.spec.Default != "" {
		defaultValue = inst.parseDefault()
	}
	return &cli.DurationFlag{
		Name:     fo.cliFlagName,
		Usage:    inst.spec.Description,
		Category: string(inst.spec.Category),
		EnvVars:  []string{inst.spec.Name},
		Value:    defaultValue,
		Action: func(ctx *cli.Context, parsed time.Duration) (err error) {
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

func (inst *DurationVar) SetForTest(t testing.TB, value string) {
	t.Helper()
	inst.res.reset()
	t.Setenv(inst.spec.Name, value)
	t.Cleanup(func() {
		inst.res.reset()
	})
}
