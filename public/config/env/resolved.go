package env

import "sync"

// ValueSourceE names where a Var's resolved value came from. The tiers are
// ordered by precedence — a higher tier shadows every lower one — which is
// the order [resolved.get] consults them: an in-process override, then the
// value the CLI flag parsed, then the process environment, then Spec.Default.
type ValueSourceE uint8

const (
	// ValueSourceDefault: nothing supplied a value; Spec.Default applies.
	ValueSourceDefault ValueSourceE = iota
	// ValueSourceEnv: the process environment carried a non-empty,
	// well-formed value.
	ValueSourceEnv
	// ValueSourceFlag: the cli.Flag derived by AsCliFlag parsed a value
	// (from the command line or, through urfave/cli, from the bound
	// environment variable).
	ValueSourceFlag
	// ValueSourceOverride: Override was called in this process.
	ValueSourceOverride
)

func (inst ValueSourceE) String() (s string) {
	switch inst {
	case ValueSourceDefault:
		return "default"
	case ValueSourceEnv:
		return "env"
	case ValueSourceFlag:
		return "flag"
	case ValueSourceOverride:
		return "override"
	default:
		return "unknown"
	}
}

// resolved is the per-Var resolution state every typed Var embeds: the
// value cached from the environment or the flag, and an optional
// in-process override that shadows both. One mutex guards all of it.
type resolved[T any] struct {
	mu         sync.Mutex
	cached     bool
	value      T
	source     ValueSourceE
	overridden bool
	override   T
}

// get resolves the value: the override when set, else the cached
// flag/environment value, else fromEnv (whose result is cached).
func (inst *resolved[T]) get(fromEnv func() (value T, src ValueSourceE)) (out T, src ValueSourceE) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.overridden {
		return inst.override, ValueSourceOverride
	}
	if !inst.cached {
		inst.value, inst.source = fromEnv()
		inst.cached = true
	}
	return inst.value, inst.source
}

// setFlag records the value a flag Action parsed.
func (inst *resolved[T]) setFlag(value T) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.value = value
	inst.source = ValueSourceFlag
	inst.cached = true
}

// clearCache forgets the cached flag/environment value so the next get
// resolves from the environment again. The override is untouched.
func (inst *resolved[T]) clearCache() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	var zero T
	inst.cached = false
	inst.value = zero
	inst.source = ValueSourceDefault
}

func (inst *resolved[T]) setOverride(value T) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.override = value
	inst.overridden = true
}

func (inst *resolved[T]) clearOverride() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	var zero T
	inst.override = zero
	inst.overridden = false
}

// reset forgets everything — cache and override — for SetForTest.
func (inst *resolved[T]) reset() {
	inst.clearCache()
	inst.clearOverride()
}
