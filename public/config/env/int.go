package env

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"

	cli "github.com/urfave/cli/v2"
)

// IntVar is the typed env-var handle for 64-bit signed integer values.
type IntVar struct {
	spec    Spec
	cacheMu sync.Mutex
	cached  bool
	value   int64
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
	inst.cacheMu.Lock()
	defer inst.cacheMu.Unlock()
	if inst.cached {
		return inst.value
	}
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" {
		inst.value = inst.parseDefault()
	} else {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil {
			inst.value = inst.parseDefault()
		} else {
			inst.value = parsed
		}
	}
	inst.cached = true
	return inst.value
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
	inst.cacheMu.Lock()
	defer inst.cacheMu.Unlock()
	inst.value = value
	inst.cached = true
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

func (inst *IntVar) SetForTest(t testing.TB, value string) {
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
