package app

import (
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
)

// Origin is where a registration came from, as the registry observed it
// (ADR-0339): the Go package whose code called Register or RegisterFactory,
// and the module that package was built from, resolved against the binary's
// own build information.
//
// It is observed rather than declared, so it sits on [Registration] and not
// on [Manifest]: an author cannot misstate it and it cannot go stale. It
// names the code that registered, which for an app minted from data — an
// applet from a book — is the code that minted it, not the author of the
// definition.
type Origin struct {
	// Package is the import path of the registering code; empty when the
	// call stack could not be read.
	Package string
	// Module is the module Package belongs to; empty when the binary carries
	// no build information or no module of it contains Package.
	Module string
	// ModuleVersion is the version the build selected. "(devel)" is a module
	// built from a local directory — a workspace member, a replace to a
	// directory, a main module without a VCS stamp — where the version says
	// nothing and the binary's revision (keelson.build) is the nearest
	// stand-in.
	ModuleVersion string
	// ModuleSum is the go.sum hash of the module as built; empty for local
	// directories.
	ModuleSum string
	// Replace is the replace target, "path" or "path@version"; empty when
	// the module is not replaced.
	Replace string
	// Main is true when Module is the binary's main module.
	Main bool
}

// registryPackage is this package's import path: the frames to skip when
// looking for the code that registered.
var registryPackage = reflect.TypeFor[Registry]().PkgPath()

// readBuildInfo is the binary's build information, read once; nil when the
// binary carries none.
var readBuildInfo = sync.OnceValue(func() (bi *debug.BuildInfo) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		bi = nil
	}
	return
})

// observeOrigin walks the stack up to the first frame outside this package —
// the caller of Register or RegisterFactory, whichever entry point it came
// through — and resolves its package against the build information.
func observeOrigin() (o Origin) {
	var pcs [32]uintptr
	n := runtime.Callers(2, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		pkg := packageOfFunc(f.Function)
		if pkg != "" && pkg != registryPackage {
			o = originOfPackage(pkg, readBuildInfo())
			return
		}
		if !more {
			return
		}
	}
}

// packageOfFunc extracts the import path from a fully qualified function
// name as runtime.Frame reports it:
//
//	"github.com/x/y/apps/a.init.0"         -> "github.com/x/y/apps/a"
//	"github.com/x/y/apps/a.(*App).Mount"   -> "github.com/x/y/apps/a"
//	"github.com/x/y/pkg.F[...]"                 -> "github.com/x/y/pkg"
//	"gopkg.in/yaml%2ev3.Unmarshal"              -> "gopkg.in/yaml.v3"
//	"main.main"                                 -> "main"
//
// The linker escapes a dot in the last path element as %2e, so the package
// ends at the first literal dot after the last slash.
func packageOfFunc(name string) (pkg string) {
	// A type argument list can itself hold slashes and dots.
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	slash := strings.LastIndexByte(name, '/')
	dot := strings.IndexByte(name[slash+1:], '.')
	if dot < 0 {
		pkg = name
		return
	}
	pkg = strings.ReplaceAll(name[:slash+1+dot], "%2e", ".")
	return
}

// originOfPackage resolves pkg to the module containing it: the longest
// module path in bi that is pkg or a path prefix of it. Package main belongs
// to the main module by definition.
func originOfPackage(pkg string, bi *debug.BuildInfo) (o Origin) {
	o.Package = pkg
	if bi == nil {
		return
	}
	var best *debug.Module
	main := false
	consider := func(m *debug.Module, isMain bool) {
		if m == nil || m.Path == "" {
			return
		}
		if pkg != m.Path && !strings.HasPrefix(pkg, m.Path+"/") {
			return
		}
		if best == nil || len(m.Path) > len(best.Path) {
			best = m
			main = isMain
		}
	}
	consider(&bi.Main, true)
	for _, d := range bi.Deps {
		consider(d, false)
	}
	if best == nil && pkg == "main" && bi.Main.Path != "" {
		best = &bi.Main
		main = true
	}
	if best == nil {
		return
	}
	o.Module = best.Path
	o.ModuleVersion = best.Version
	o.ModuleSum = best.Sum
	o.Main = main
	if r := best.Replace; r != nil {
		o.Replace = r.Path
		if r.Version != "" {
			o.Replace += "@" + r.Version
		}
		o.ModuleSum = r.Sum
	}
	return
}
