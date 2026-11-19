package vendortidy

import (
	"go/version"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ProblemKindE classifies what the workspace cannot satisfy.
type ProblemKindE uint8

const (
	// ProblemKindUnresolvedImport is an import neither a used module nor
	// vendor/ provides (ADR-0384 §SD5).
	ProblemKindUnresolvedImport ProblemKindE = iota + 1
	// ProblemKindWorkspaceOnly is a module reached only through a used
	// module's source, with no version in modules.txt to require it at.
	ProblemKindWorkspaceOnly
)

var AllProblemKinds = []ProblemKindE{ProblemKindUnresolvedImport, ProblemKindWorkspaceOnly}

func (inst ProblemKindE) String() string {
	switch inst {
	case ProblemKindUnresolvedImport:
		return "unresolved-import"
	case ProblemKindWorkspaceOnly:
		return "workspace-only"
	}
	return "unknown"
}

// Requirement is one computed go.mod requirement.
type Requirement struct {
	Path    string
	Version string
	// Replacement is the replace modules.txt records for the module; the
	// member's go.mod has to carry the same one.
	Replacement string
	Indirect    bool
	// Kept marks an existing requirement on a module that provides no
	// vendored package: whether tidy keeps it depends on dependency tests the
	// vendor tree does not hold, so it is kept as it was (ADR-0384 §SD3).
	Kept bool
}

// Problem is something the workspace cannot satisfy.
type Problem struct {
	Path     string
	Importer string
	Kind     ProblemKindE
}

// Result is what tidy would write into a member's go.mod, as far as the
// vendor tree can tell.
type Result struct {
	Module string
	// GoVersion is the go line: the module's own, raised to the highest go
	// version a required module declares (ADR-0384 §SD2).
	GoVersion string
	Requires  []Requirement
	Problems  []Problem
}

// HasUnresolved reports whether an import could not be resolved; such a
// result must not be written.
func (inst Result) HasUnresolved() bool {
	return slices.ContainsFunc(inst.Problems, func(p Problem) bool { return p.Kind == ProblemKindUnresolvedImport })
}

// Tidy computes the requirements of the used module in moduleDir
// (ADR-0384 §SD1–§SD3).
func (inst *Workspace) Tidy(moduleDir string) (res Result, err error) {
	main, ok := inst.ModuleAt(moduleDir)
	if !ok {
		err = eb.Build().Str("dir", moduleDir).Str("workspace", inst.root).Errorf("not a module used by the workspace")
		return
	}
	res.Module = main.Path

	var roots []string
	roots, err = packageDirs(main)
	if err != nil {
		return
	}

	direct := make(map[string]bool, 64)
	used := make(map[string]bool, 128)
	seen := make(map[string]bool, 1024)
	problems := make(map[string]Problem, 4)
	queue := make([]pkgRef, 0, 1024)

	visit := func(importPath string, importer string, fromMain bool) {
		if importPath == "C" || inst.isStd(importPath) {
			return
		}
		ref, found := inst.resolve(importPath)
		if !found {
			if _, dup := problems[importPath]; !dup {
				problems[importPath] = Problem{Path: importPath, Importer: importer, Kind: ProblemKindUnresolvedImport}
			}
			return
		}
		if ref.module != main.Path {
			used[ref.module] = true
			if fromMain {
				direct[ref.module] = true
			}
		}
		if !seen[importPath] {
			seen[importPath] = true
			queue = append(queue, ref)
		}
	}

	for _, d := range roots {
		var rel string
		rel, err = filepath.Rel(main.Dir, d)
		if err != nil {
			err = eb.Build().Str("dir", d).Errorf("relate package to module root: %w", err)
			return
		}
		ip := main.Path
		if rel != "." {
			ip = path.Join(main.Path, filepath.ToSlash(rel))
		}
		seen[ip] = true
		var imps []string
		imps, err = dirImports(d, true)
		if err != nil {
			return
		}
		for _, i := range imps {
			visit(i, ip, true)
		}
	}
	for _, t := range main.File.Tool {
		// tidy records a tool's module as indirect unless code imports it.
		visit(t.Path, "tool", false)
	}
	for len(queue) > 0 {
		r := queue[0]
		queue = queue[1:]
		if r.module == main.Path {
			// A member package reached through a dependency: its imports are
			// roots already and its tests do not count from here.
			continue
		}
		var imps []string
		imps, err = dirImports(r.dir, false)
		if err != nil {
			return
		}
		for _, i := range imps {
			visit(i, r.importPath, false)
		}
	}

	res.Requires = make([]Requirement, 0, len(used)+4)
	for m := range used {
		vm, found := inst.vendor.Selected(m)
		if !found || vm.Version == "" {
			problems["module "+m] = Problem{Path: m, Kind: ProblemKindWorkspaceOnly}
			continue
		}
		res.Requires = append(res.Requires, Requirement{Path: m, Version: vm.Version, Replacement: vm.Replacement, Indirect: !direct[m]})
	}
	for _, r := range main.File.Require {
		m := r.Mod.Path
		if used[m] || inst.vendor.IsProvidingPackages(m) {
			continue
		}
		vm, found := inst.vendor.Selected(m)
		if !found || !vm.Explicit || vm.Version == "" {
			continue
		}
		res.Requires = append(res.Requires, Requirement{Path: m, Version: vm.Version, Replacement: vm.Replacement, Indirect: r.Indirect, Kept: true})
	}
	slices.SortFunc(res.Requires, func(a, b Requirement) int { return strings.Compare(a.Path, b.Path) })

	if main.File.Go != nil {
		res.GoVersion = main.File.Go.Version
	}
	for _, r := range res.Requires {
		vm, _ := inst.vendor.Selected(r.Path)
		if vm.GoVersion != "" && (res.GoVersion == "" || version.Compare("go"+vm.GoVersion, "go"+res.GoVersion) > 0) {
			res.GoVersion = vm.GoVersion
		}
	}

	res.Problems = make([]Problem, 0, len(problems))
	for _, p := range problems {
		res.Problems = append(res.Problems, p)
	}
	slices.SortFunc(res.Problems, func(a, b Problem) int { return strings.Compare(a.Path, b.Path) })
	return
}
