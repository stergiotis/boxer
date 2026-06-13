package env

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	cli "github.com/urfave/cli/v2"
)

// DurationVar is the typed env-var handle for time.Duration values.
type DurationVar struct {
	spec    Spec
	cacheMu sync.Mutex
	cached  bool
	value   time.Duration
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
	inst.cacheMu.Lock()
	defer inst.cacheMu.Unlock()
	if inst.cached {
		return inst.value
	}
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" {
		inst.value = inst.parseDefault()
	} else {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil {
			inst.value = inst.parseDefault()
		} else {
			inst.value = parsed
		}
	}
	inst.cached = true
	return inst.value
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
	inst.cacheMu.Lock()
	defer inst.cacheMu.Unlock()
	inst.value = value
	inst.cached = true
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

func (inst *DurationVar) SetForTest(t testing.TB, value string) {
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
