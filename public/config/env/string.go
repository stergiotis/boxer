package env

import (
	"os"
	"testing"

	cli "github.com/urfave/cli/v2"
)

// StringVar is the typed env-var handle for string values.
type StringVar struct {
	spec Spec
	res  resolved[string]
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

// Get returns the resolved value: an Override, else the value the flag
// Action cached, else the env var when set and non-empty, else Spec.Default
// (the tiers of ValueSourceE). The environment read is cached on first call.
func (inst *StringVar) Get() (out string) {
	out, _ = inst.res.get(inst.resolveEnv)
	return
}

func (inst *StringVar) resolveEnv() (out string, src ValueSourceE) {
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" {
		return inst.spec.Default, ValueSourceDefault
	}
	return raw, ValueSourceEnv
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
	inst.res.setFlag(value)
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
// see it. An empty parsed value is not a value (Spec: empty means unset):
// urfave/cli hands one over for a set-but-empty environment variable or a
// bare `--flag=`, and caching it would shadow Get's own empty-means-default
// reading; the Action then leaves the cache alone and the user action
// does not run.
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
			if parsed == "" {
				inst.res.clearCache()
				return
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

// Override pins the resolved value for this process (ValueSourceOverride):
// it shadows the flag, the environment and the default until ClearOverride,
// and is never written to the process environment, so child processes and
// `env list`'s CURRENT column do not see it. It is the seam a wrapper
// command uses to seed another component's variables before that component
// reads them in-process (ADR-0009, update 2026-08-28).
func (inst *StringVar) Override(value string) {
	inst.res.setOverride(value)
}

// ClearOverride removes the Override; resolution falls back to the flag,
// environment or default.
func (inst *StringVar) ClearOverride() {
	inst.res.clearOverride()
}

// ValueSource reports which tier Get's value comes from.
func (inst *StringVar) ValueSource() (src ValueSourceE) {
	_, src = inst.res.get(inst.resolveEnv)
	return
}

// SetForTest sets the env var via t.Setenv and resets the cache. The
// cache is reset again on t.Cleanup so subsequent tests start fresh.
func (inst *StringVar) SetForTest(t testing.TB, value string) {
	t.Helper()
	inst.res.reset()
	t.Setenv(inst.spec.Name, value)
	t.Cleanup(func() {
		inst.res.reset()
	})
}
