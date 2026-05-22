//go:build llm_generated_opus47

// Package env is the boxer-wide environment variable registry.
//
// Each variable is declared once as a package-level value via NewString /
// NewBool / NewInt / NewDuration / NewPath. The declaration registers a
// Spec process-globally, and the returned typed *Var carries the
// resolved value, caching, CLI-flag derivation, and test helpers. See
// ADR-0009 for design rationale.
package env

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
)

// CategoryE tags a Spec for filtering and documentation grouping. The
// type is an open string per ADR-0009 §2 so that downstream consumers
// (pebble2impl, third) can introduce additional tags without changes to
// this package.
type CategoryE string

const (
	CategoryObservability   CategoryE = "observability"
	CategoryDev             CategoryE = "dev"
	CategoryDocgen          CategoryE = "docgen"
	CategoryLLM             CategoryE = "llm"
	CategoryDatabase        CategoryE = "database"
	CategorySystem          CategoryE = "system"
	CategoryTestIntegration CategoryE = "test-integration"
)

// TypeE classifies a Spec by the Go-side typed handle that owns it.
// Filled at registration time by NewString / NewBool / NewInt /
// NewDuration / NewPath; callers must not set it. The doc generator
// (`boxer env gen-docs`) uses it to render the "Type" column in env-vars.md.
type TypeE string

const (
	TypeString           TypeE = "string"
	TypeBool             TypeE = "bool"
	TypeInt64            TypeE = "int64"
	TypeDuration         TypeE = "duration"
	TypePath             TypeE = "path"
	TypeCategorialString TypeE = "categorial-string"
)

// Origin identifies the declaring site of a Spec. It is auto-derived at
// registration time from the call stack; callers must not set it.
type Origin struct {
	Module  string
	Package string
}

// Spec is the declarative metadata for one environment variable. All
// caller-supplied fields are immutable after registration; Origin,
// Type, and Allowed are filled in by the NewXxx constructor.
type Spec struct {
	Name        string
	Default     string
	Description string
	Category    CategoryE
	Sensitive   bool
	CliFlagName string
	Origin      Origin
	Type        TypeE
	// Allowed is populated only for TypeCategorialString specs and
	// lists the values Get() will accept. Empty for all other Types.
	Allowed []string
}

// FlagOption customises the cli.Flag returned by Var.AsCliFlag.
type FlagOption func(opts *flagOptions)

// flagOptions carries the user-supplied customisations applied to
// AsCliFlag's output. actionFn is typed per-Var via the WithXxxAction
// helpers; each typed AsCliFlag type-asserts it to the matching
// signature and chains it after the spec-derived cache write.
type flagOptions struct {
	cliFlagName string
	actionFn    any
}

// WithCliFlagName overrides Spec.CliFlagName for the produced cli.Flag.
// Intended for the Configer composition path: the caller's
// NameTransformFunc is applied to the spec's CliFlagName and the result
// is passed in here.
func WithCliFlagName(name string) (opt FlagOption) {
	return func(opts *flagOptions) {
		opts.cliFlagName = name
	}
}

func resolveFlagOptions(spec Spec, opts []FlagOption) (out flagOptions) {
	out.cliFlagName = spec.CliFlagName
	for _, opt := range opts {
		opt(&out)
	}
	return
}

// VarI is the common surface shared by every typed Var. Useful for
// registry walks where the exact value type does not matter; the
// runtime `env list` subcommand consumes Spec() for metadata and
// Lookup() for the live env value.
type VarI interface {
	Spec() Spec
	Lookup() (raw string, set bool)
}

// LookupVar returns the registered VarI for name. ok is false when no
// spec with this Name has been registered.
func LookupVar(name string) (v VarI, ok bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	v, ok = registry[name]
	return
}

var (
	registryMu sync.RWMutex
	registry   = map[string]VarI{}
)

func register(v VarI) {
	s := v.Spec()
	registryMu.Lock()
	defer registryMu.Unlock()
	existing, dup := registry[s.Name]
	if dup {
		panic(fmt.Sprintf(
			"env: duplicate registration of %q (first at %s, again at %s)",
			s.Name, existing.Spec().Origin.Package, s.Origin.Package,
		))
	}
	registry[s.Name] = v
}

// All returns every Spec registered process-wide. Order is unspecified.
func All() (out []Spec) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out = make([]Spec, 0, len(registry))
	for _, v := range registry {
		out = append(out, v.Spec())
	}
	return
}

// ByCategory returns Specs whose Category equals c.
func ByCategory(c CategoryE) (out []Spec) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out = make([]Spec, 0, len(registry))
	for _, v := range registry {
		s := v.Spec()
		if s.Category == c {
			out = append(out, s)
		}
	}
	return
}

// ByOrigin returns Specs whose Origin.Module equals modulePath.
func ByOrigin(modulePath string) (out []Spec) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out = make([]Spec, 0, len(registry))
	for _, v := range registry {
		s := v.Spec()
		if s.Origin.Module == modulePath {
			out = append(out, s)
		}
	}
	return
}

// ByPrefix returns Specs whose Name starts with prefix.
func ByPrefix(prefix string) (out []Spec) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out = make([]Spec, 0, len(registry))
	for _, v := range registry {
		s := v.Spec()
		if strings.HasPrefix(s.Name, prefix) {
			out = append(out, s)
		}
	}
	return
}

// resetRegistryForTest clears the registry. Tests that re-register vars
// (e.g. duplicate-detection coverage) must call this first.
func resetRegistryForTest() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = map[string]VarI{}
}

// callerOrigin extracts the calling package and module from the call
// stack. skip is the number of frames above callerOrigin: typically 2
// (callerOrigin -> NewXxx -> the var declaration).
func callerOrigin(skip int32) (out Origin) {
	pc, _, _, ok := runtime.Caller(int(skip))
	if !ok {
		return
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return
	}
	out.Package = packageFromFuncName(fn.Name())
	out.Module = moduleFromPackage(out.Package)
	return
}

// packageFromFuncName extracts the package import path from a fully
// qualified Go symbol name. The package path ends at the first '.' that
// follows the last '/'.
//
// Examples:
//
//	"github.com/stergiotis/boxer/public/observability/logging.init"        -> "github.com/stergiotis/boxer/public/observability/logging"
//	"github.com/stergiotis/boxer/public/observability/logging.glob..func1" -> "github.com/stergiotis/boxer/public/observability/logging"
//	"main.init"                                                            -> "main"
func packageFromFuncName(name string) (pkg string) {
	slash := strings.LastIndex(name, "/")
	if slash < 0 {
		dot := strings.Index(name, ".")
		if dot >= 0 {
			return name[:dot]
		}
		return name
	}
	dot := strings.Index(name[slash:], ".")
	if dot >= 0 {
		return name[:slash+dot]
	}
	return name
}

// moduleFromPackage returns the host/owner/repo prefix of pkg using a
// 3-segment heuristic. Covers github.com/x/y, gitlab.com/x/y, etc.
// Shorter paths (e.g. "main") return unchanged.
func moduleFromPackage(pkg string) (mod string) {
	parts := strings.SplitN(pkg, "/", 4)
	if len(parts) >= 3 {
		return strings.Join(parts[:3], "/")
	}
	return pkg
}

// mustValidate panics if spec is missing required fields. Called at
// registration time; misuse is a programmer error.
func mustValidate(spec Spec) {
	if spec.Name == "" {
		panic("env: Spec.Name is required")
	}
	if spec.Description == "" {
		panic(fmt.Sprintf("env: Spec.Description is required (Name=%q)", spec.Name))
	}
}
