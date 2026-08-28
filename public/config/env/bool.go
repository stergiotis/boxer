package env

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	cli "github.com/urfave/cli/v2"
)

// BoolVar is the typed env-var handle for boolean values.
type BoolVar struct {
	spec Spec
	res  resolved[bool]
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
	out, _ = inst.res.get(inst.resolveEnv)
	return
}

// resolveEnv reads the process environment: an unset, empty or unparsable
// value yields Spec.Default (ValueSourceDefault).
func (inst *BoolVar) resolveEnv() (out bool, src ValueSourceE) {
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" {
		return inst.parseDefault(), ValueSourceDefault
	}
	parsed, parseErr := strconv.ParseBool(raw)
	if parseErr != nil {
		return inst.parseDefault(), ValueSourceDefault
	}
	return parsed, ValueSourceEnv
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
	inst.res.setFlag(value)
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

// Override pins the resolved value for this process (ValueSourceOverride):
// it shadows the flag, the environment and the default until ClearOverride,
// and is never written to the process environment, so child processes and
// `env list`'s CURRENT column do not see it. It is the seam a wrapper
// command uses to seed another component's variables before that component
// reads them in-process (ADR-0009, update 2026-08-28).
func (inst *BoolVar) Override(value bool) {
	inst.res.setOverride(value)
}

// ClearOverride removes the Override; resolution falls back to the flag,
// environment or default.
func (inst *BoolVar) ClearOverride() {
	inst.res.clearOverride()
}

// ValueSource reports which tier Get's value comes from.
func (inst *BoolVar) ValueSource() (src ValueSourceE) {
	_, src = inst.res.get(inst.resolveEnv)
	return
}

func (inst *BoolVar) SetForTest(t testing.TB, value string) {
	t.Helper()
	inst.res.reset()
	t.Setenv(inst.spec.Name, value)
	t.Cleanup(func() {
		inst.res.reset()
	})
}
