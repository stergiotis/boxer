// Package env is the boxer-wide environment variable registry.
//
// Each variable is declared once as a package-level value via NewString /
// NewBool / NewInt / NewDuration / NewPath / NewCategorialString. The
// declaration registers a Spec process-globally, and the returned typed
// *Var carries the resolved value and its caching. A command-line flag is
// derived from a Var by public/config/env/envcli, and a test sets one with
// public/config/env/envtest; neither is linked by a package that only
// reads variables. See ADR-0009 for design rationale.
//
// # Resolution
//
// A Var resolves from four tiers, highest precedence first: an in-process
// Override, the value its derived CLI flag parsed, the process environment,
// and Spec.Default ([ValueSourceE]). An empty value is never a value: a
// variable that is set but empty, and a flag given as a bare `--flag=`,
// both read as unset and fall through to the next tier. Get caches the
// environment read on first call; ValueSource reports the tier in use.
//
// # Seeding another component's variables in-process
//
// A wrapper command that hosts another component in the same process
// (a downstream `gui` around boxer's carousel is the first) seeds that
// component's variables by calling Override on each *Var before the
// component reads them, typically from its own flag values. Nothing is
// written to the process environment — Lookup, `env list`'s CURRENT column
// and child processes keep seeing the shell's values — and ClearOverride
// hands resolution back. Exporting values with os.Setenv to reach an
// in-process reader is the pattern Override replaces.
package env

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// CategoryE tags a Spec for filtering and documentation grouping. The
// type is an open string per ADR-0009 §2 so that downstream consumers
// (downstream modules, third parties) can introduce additional tags without changes to
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
// NewDuration / NewPath / NewCategorialString; callers must not set
// it. The doc generator (`boxer env gen-docs`) uses it to render the
// "Type" column in env-vars.md.
type TypeE string

const (
	TypeString           TypeE = "string"
	TypeBool             TypeE = "bool"
	TypeInt64            TypeE = "int64"
	TypeDuration         TypeE = "duration"
	TypePath             TypeE = "path"
	TypeCategorialString TypeE = "categorial-string"
	TypeFloat64          TypeE = "float64"
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
//
// Empty means unset, on every path: a variable present in the environment
// with an empty value, or a derived flag given as `--flag=`, resolves as if
// absent — Default applies (or the empty state, for a categorial variable
// declared without a Default). Consequently no variable can carry the empty
// string as a distinct value.
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

// register adds v to the registry and returns the variable that holds the
// name. Declaring an identical Spec twice — the same metadata from any
// origin — returns the first declaration, so both handles share one
// resolution state. A conflicting declaration keeps the first, is recorded
// for RegistrationErrors, and returns v unregistered: a binary that links
// both declarations still starts, and its host reports the conflict.
func register(v VarI) (held VarI) {
	s := v.Spec()
	registryMu.Lock()
	defer registryMu.Unlock()
	existing, dup := registry[s.Name]
	if !dup {
		registry[s.Name] = v
		return v
	}
	if joined, ok := joinCompanion(existing, v); ok {
		return joined
	}
	first := existing.Spec()
	if sameDeclaration(first, s) {
		return existing
	}
	conflicts = append(conflicts, fmt.Errorf( //boxer:lint disable=CS001 reason="eh imports config/env; eh.Errorf would close an import cycle"
		"env: conflicting declarations of %q (first at %s, again at %s)%s: %w",
		s.Name, first.Origin.Package, s.Origin.Package, companionNote(existing, v), ErrConflictingDeclaration))
	return v
}

// ErrConflictingDeclaration is wrapped by every error RegistrationErrors
// returns.
var ErrConflictingDeclaration = errors.New("env: variable declared twice with different specs")

var conflicts []error

// RegistrationErrors returns one error per conflicting duplicate
// declaration seen so far (ErrConflictingDeclaration), joined; nil when
// there is none. A host calls it after init — logging.Apply does — since a
// package-level declaration has no caller to return an error to.
func RegistrationErrors() (err error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return errors.Join(conflicts...)
}

// sameDeclaration reports whether two specs declare the same variable,
// ignoring where they were declared.
func sameDeclaration(a Spec, b Spec) (same bool) {
	return a.Name == b.Name && a.Default == b.Default && a.Description == b.Description &&
		a.Category == b.Category && a.Sensitive == b.Sensitive && a.CliFlagName == b.CliFlagName &&
		a.Type == b.Type && slices.Equal(a.Allowed, b.Allowed)
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
	conflicts = nil
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
		before, _, ok := strings.Cut(name, ".")
		if ok {
			return before
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
