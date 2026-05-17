package env

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	cli "github.com/urfave/cli/v2"
)

// PathVar is the typed env-var handle for filesystem path values. Get
// expands a leading "~" or "~/" to the user's home directory; absolute
// or relative paths are returned unchanged.
type PathVar struct {
	spec    Spec
	cacheMu sync.Mutex
	cached  bool
	value   string
}

var _ VarI = (*PathVar)(nil)

func NewPath(spec Spec) (v *PathVar) {
	mustValidate(spec)
	spec.Origin = callerOrigin(2)
	v = &PathVar{spec: spec}
	register(v)
	return
}

func (inst *PathVar) Spec() (out Spec) {
	return inst.spec
}

func (inst *PathVar) Get() (out string) {
	inst.cacheMu.Lock()
	defer inst.cacheMu.Unlock()
	if inst.cached {
		return inst.value
	}
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" {
		inst.value = expandHome(inst.spec.Default)
	} else {
		inst.value = expandHome(raw)
	}
	inst.cached = true
	return inst.value
}

func (inst *PathVar) Lookup() (raw string, set bool) {
	raw, set = os.LookupEnv(inst.spec.Name)
	if raw == "" {
		set = false
	}
	return
}

func (inst *PathVar) setCached(value string) {
	inst.cacheMu.Lock()
	defer inst.cacheMu.Unlock()
	inst.value = expandHome(value)
	inst.cached = true
}

func (inst *PathVar) AsCliFlag(opts ...FlagOption) (out cli.Flag) {
	fo := resolveFlagOptions(inst.spec, opts)
	return &cli.PathFlag{
		Name:     fo.cliFlagName,
		Usage:    inst.spec.Description,
		Category: string(inst.spec.Category),
		EnvVars:  []string{inst.spec.Name},
		Value:    inst.spec.Default,
		Action: func(_ *cli.Context, parsed string) (err error) {
			inst.setCached(parsed)
			return
		},
	}
}

func (inst *PathVar) SetForTest(t testing.TB, value string) {
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

// expandHome rewrites a leading "~" or "~/" to the user's home directory.
// If os.UserHomeDir fails, the input is returned unchanged.
func expandHome(path string) (out string) {
	if path == "" {
		return path
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
