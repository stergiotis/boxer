package codevol

import (
	"runtime/debug"
	"sort"
	"strings"
)

// Party classifies a module relative to the program being measured. It is the
// axis the "our code vs third-party code" question is actually asked on.
type Party string

const (
	// PartyFirst is the main module — the code this repository owns.
	PartyFirst Party = "first"
	// PartyThird is any other module in the build.
	PartyThird Party = "third"
	// PartyStdlib is the standard library, which carries no module.
	PartyStdlib Party = "stdlib"
)

// ModuleInfo is one Go module linked into the running binary, as recorded by
// the toolchain at link time.
type ModuleInfo struct {
	Path string
	// Version is the module version, or "(devel)" for a main module built
	// outside a tagged checkout.
	Version string
	// Sum is the go.sum checksum. Empty for the main module and for any
	// module resolved through a replace directive.
	Sum string
	// ReplacedBy is "path@version" when a replace directive redirected this
	// module, empty otherwise. A replaced module's code is not what its
	// Path/Version claims, so a supply-chain reading must not ignore it.
	ReplacedBy string
	IsMain     bool
	Party      Party
}

// Modules reports every module linked into the running binary.
//
// This is the cheapest and most portable evidence available about a Go
// program's composition: it needs no toolchain, no source and no module
// cache, because the linker recorded it in the binary. ok is false only when
// the binary carries no build information at all, which the toolchain omits
// for some non-module builds.
//
// One caveat worth knowing before trusting a small answer: a `go test` binary
// carries the main module but an **empty dependency list**, so under `go test`
// this returns one row. Only a `go build` binary carries the full list. The
// integration lane covers the built-binary case for that reason.
func Modules() (mods []ModuleInfo, ok bool) {
	var bi *debug.BuildInfo
	bi, ok = debug.ReadBuildInfo()
	if !ok {
		return
	}
	return modulesFrom(bi), true
}

// modulesFrom is Modules' pure core, so tests (and any caller inspecting a
// binary other than itself, via debug/buildinfo) can reach the same mapping.
func modulesFrom(bi *debug.BuildInfo) (mods []ModuleInfo) {
	mods = make([]ModuleInfo, 0, len(bi.Deps)+1)
	if bi.Main.Path != "" {
		mods = append(mods, ModuleInfo{
			Path:    bi.Main.Path,
			Version: bi.Main.Version,
			IsMain:  true,
			Party:   PartyFirst,
		})
	}
	for _, d := range bi.Deps {
		if d == nil {
			continue
		}
		m := ModuleInfo{
			Path:    d.Path,
			Version: d.Version,
			Sum:     d.Sum,
			Party:   PartyThird,
		}
		// A replace redirects to a different module or a local directory; the
		// replacement carries the code that actually shipped, so report the
		// original path (the import paths still use it) and name what stood
		// in for it.
		if d.Replace != nil {
			m.ReplacedBy = d.Replace.Path
			if d.Replace.Version != "" {
				m.ReplacedBy += "@" + d.Replace.Version
			}
		}
		mods = append(mods, m)
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].Path < mods[j].Path })
	return
}

// ModuleIndex resolves an import path to the module that owns it. It is built
// from the in-binary module list, so the resolution is exact for every module
// the linker recorded — no toolchain, no guessing.
type ModuleIndex struct {
	// paths is sorted by descending length so the first prefix hit is the
	// longest one. Nested modules (a/b inside a) resolve to the inner module,
	// which is what the toolchain does too.
	paths []string
	party map[string]Party
}

// NewModuleIndex builds an index over mods.
func NewModuleIndex(mods []ModuleInfo) (idx *ModuleIndex) {
	idx = &ModuleIndex{
		paths: make([]string, 0, len(mods)),
		party: make(map[string]Party, len(mods)),
	}
	for _, m := range mods {
		idx.paths = append(idx.paths, m.Path)
		idx.party[m.Path] = m.Party
	}
	sort.Slice(idx.paths, func(i, j int) bool { return len(idx.paths[i]) > len(idx.paths[j]) })
	return
}

// Lookup returns the module owning importPath and its party. A path matching
// no module is standard library: the stdlib is the one body of code in a Go
// binary that has no module, so "unmatched" and "stdlib" are the same
// statement rather than a failure to resolve.
func (idx *ModuleIndex) Lookup(importPath string) (modulePath string, party Party) {
	if idx != nil {
		for _, p := range idx.paths {
			if importPath == p || strings.HasPrefix(importPath, p+"/") {
				return p, idx.party[p]
			}
		}
	}
	return "std", PartyStdlib
}
