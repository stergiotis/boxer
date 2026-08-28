package env

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	cli "github.com/urfave/cli/v2"
)

// IntVar is the typed env-var handle for 64-bit signed integer values.
type IntVar struct {
	spec Spec
	res  resolved[int64]
}

var _ VarI = (*IntVar)(nil)

func NewInt(spec Spec) (v *IntVar) {
	mustValidate(spec)
	spec.Origin = callerOrigin(2)
	spec.Type = TypeInt64
	v = &IntVar{spec: spec}
	register(v)
	return
}

func (inst *IntVar) Spec() (out Spec) {
	return inst.spec
}

func (inst *IntVar) Get() (out int64) {
	out, _ = inst.res.get(inst.resolveEnv)
	return
}

// resolveEnv reads the process environment: an unset, empty or unparsable
// value yields Spec.Default (ValueSourceDefault).
func (inst *IntVar) resolveEnv() (out int64, src ValueSourceE) {
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" {
		return inst.parseDefault(), ValueSourceDefault
	}
	parsed, parseErr := strconv.ParseInt(raw, 10, 64)
	if parseErr != nil {
		return inst.parseDefault(), ValueSourceDefault
	}
	return parsed, ValueSourceEnv
}

func (inst *IntVar) parseDefault() (out int64) {
	if inst.spec.Default == "" {
		return 0
	}
	parsed, parseErr := strconv.ParseInt(inst.spec.Default, 10, 64)
	if parseErr != nil {
		panic(fmt.Sprintf("env: int default %q for %q cannot be parsed: %v",
			inst.spec.Default, inst.spec.Name, parseErr))
	}
	return parsed
}

func (inst *IntVar) Lookup() (raw string, set bool) {
	raw, set = os.LookupEnv(inst.spec.Name)
	if raw == "" {
		set = false
	}
	return
}

func (inst *IntVar) setCached(value int64) {
	inst.res.setFlag(value)
}

// WithInt64Action attaches a caller-supplied Action func to the
// cli.Int64Flag returned by AsCliFlag. The user action runs first; on
// success the parsed value is written to the cache.
func WithInt64Action(fn func(ctx *cli.Context, parsed int64) error) (opt FlagOption) {
	return func(o *flagOptions) {
		o.actionFn = fn
	}
}

func (inst *IntVar) AsCliFlag(opts ...FlagOption) (out cli.Flag) {
	fo := resolveFlagOptions(inst.spec, opts)
	userAction, _ := fo.actionFn.(func(*cli.Context, int64) error)
	defaultValue := int64(0)
	if inst.spec.Default != "" {
		defaultValue = inst.parseDefault()
	}
	return &cli.Int64Flag{
		Name:     fo.cliFlagName,
		Usage:    inst.spec.Description,
		Category: string(inst.spec.Category),
		EnvVars:  []string{inst.spec.Name},
		Value:    defaultValue,
		Action: func(ctx *cli.Context, parsed int64) (err error) {
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
func (inst *IntVar) Override(value int64) {
	inst.res.setOverride(value)
}

// ClearOverride removes the Override; resolution falls back to the flag,
// environment or default.
func (inst *IntVar) ClearOverride() {
	inst.res.clearOverride()
}

// ValueSource reports which tier Get's value comes from.
func (inst *IntVar) ValueSource() (src ValueSourceE) {
	_, src = inst.res.get(inst.resolveEnv)
	return
}

func (inst *IntVar) SetForTest(t testing.TB, value string) {
	t.Helper()
	inst.res.reset()
	t.Setenv(inst.spec.Name, value)
	t.Cleanup(func() {
		inst.res.reset()
	})
}
