package wasmsurvey

import (
	"context"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
	"github.com/stergiotis/boxer/public/code/analysis/golang/godep/godepcollect"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/packageprops"
)

// This file holds the stability rules of `props verify` (ADR-0389). A Stable
// package is one whose exported API is covered by the compatibility promise in
// VERSIONING.md. The promise is only as good as what the package stands on, so
// a Stable package may not import a package of this module that makes a
// weaker promise or none, and it must say what it is in a package doc comment.
// Imports are the non-test imports of the native build under the repo's tags:
// a test of a Stable package may import whatever it needs.

// StabilityRuleE names one stability rule.
type StabilityRuleE uint8

const (
	// StabilityRuleImportsWeakerTier — a Stable package imports an
	// Experimental or Internal package of this module.
	StabilityRuleImportsWeakerTier StabilityRuleE = iota
	// StabilityRuleImportsNonLibrary — a Stable package imports a demo,
	// example, integration-test, test-support or tool package of this module.
	StabilityRuleImportsNonLibrary
	// StabilityRuleNoPackageDoc — a Stable package has no package doc comment.
	StabilityRuleNoPackageDoc
	// StabilityRuleNonLibraryKind — a demo, example or tool package is
	// declared Stable; its Go API is not library surface, so the tier
	// contradicts the role.
	StabilityRuleNonLibraryKind
	// StabilityRuleImportsUntiered — advisory, never fails the gate: a Stable
	// package imports a non-internal package of this module that declares no
	// tier. The import is allowed, but whatever it exposes is unpromised.
	StabilityRuleImportsUntiered
)

// String renders the rule as a short token for diagnostics.
func (inst StabilityRuleE) String() (s string) {
	switch inst {
	case StabilityRuleImportsWeakerTier:
		return "imports-weaker-tier"
	case StabilityRuleImportsNonLibrary:
		return "imports-non-library"
	case StabilityRuleNoPackageDoc:
		return "no-package-doc"
	case StabilityRuleNonLibraryKind:
		return "non-library-kind"
	case StabilityRuleImportsUntiered:
		return "imports-untiered"
	}
	return "unknown"
}

// Advisory reports whether a finding of this rule is informational only.
func (inst StabilityRuleE) Advisory() (b bool) {
	return inst == StabilityRuleImportsUntiered
}

// StabilityFinding is one rule hit on one Stable package.
type StabilityFinding struct {
	ImportPath string
	Rule       StabilityRuleE
	// Imported is the offending import for the import rules, empty otherwise.
	Imported string
	// ImportedProps is the imported package's declaration (zero when it has
	// none).
	ImportedProps packageprops.Props
}

// nonLibraryKind reports whether k marks a package that is not library code a
// Stable package may build on.
func nonLibraryKind(k packageprops.Kind) (b bool) {
	return k != packageprops.KindUnspecified
}

// CheckStability applies the stability rules to the declared rows. imports
// maps an import path of this module to its non-test imports; packages
// missing from it are checked for everything but their imports. rootModule
// scopes the import rules to this module's packages. Findings are sorted by
// package, then rule, then import.
func CheckStability(rows []HarvestRow, imports map[string][]string, rootModule string) (findings []StabilityFinding) {
	declared := make(map[string]HarvestRow, len(rows))
	for _, r := range rows {
		declared[r.ImportPath] = r
	}
	for _, r := range rows {
		if r.Stability != packageprops.StabilityStable {
			continue
		}
		switch r.Kind {
		case packageprops.KindDemo, packageprops.KindExample, packageprops.KindTool:
			findings = append(findings, StabilityFinding{ImportPath: r.ImportPath, Rule: StabilityRuleNonLibraryKind})
		}
		if r.Synopsis == "" {
			findings = append(findings, StabilityFinding{ImportPath: r.ImportPath, Rule: StabilityRuleNoPackageDoc})
		}
		for _, imp := range imports[r.ImportPath] {
			if imp != rootModule && !strings.HasPrefix(imp, rootModule+"/") {
				continue // external or stdlib: outside this module's tiers
			}
			if imp == propsImportPath {
				continue // the vocabulary every declaration imports; its tier is stated in ADR-0389
			}
			d, ok := declared[imp]
			var rule StabilityRuleE
			switch {
			case ok && nonLibraryKind(d.Kind):
				rule = StabilityRuleImportsNonLibrary
			case ok && (d.Stability == packageprops.StabilityExperimental || d.Stability == packageprops.StabilityInternal):
				rule = StabilityRuleImportsWeakerTier
			case ok && d.Stability == packageprops.StabilityStable:
				continue
			case isGoInternal(imp):
				continue // Go's internal rule already keeps it out of consumers' reach
			default:
				rule = StabilityRuleImportsUntiered
			}
			findings = append(findings, StabilityFinding{ImportPath: r.ImportPath, Rule: rule, Imported: imp, ImportedProps: d.Props()})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.ImportPath != b.ImportPath {
			return a.ImportPath < b.ImportPath
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Imported < b.Imported
	})
	return
}

// isGoInternal reports whether importPath has an `internal` path element.
func isGoInternal(importPath string) (b bool) {
	return strings.Contains(importPath, "/internal/") || strings.HasSuffix(importPath, "/internal")
}

// LoadModuleImports collects the non-test imports of every package matched by
// patterns under dir, for the native platform and the given tags, keyed by
// import path and restricted to packages of the main module. It reuses the
// godep collector (ADR-0064) the wasm survey drives, without the wasm target
// environment.
func LoadModuleImports(ctx context.Context, dir string, patterns []string, tags []string) (imports map[string][]string, rootModule string, err error) {
	var m godep.Manifest
	m, err = godepcollect.New(godepcollect.Config{Dir: dir, Patterns: patterns, Tags: tags}).Load(ctx)
	if err != nil {
		err = eb.Build().Str("dir", dir).Errorf("collect native import graph: %w", err)
		return
	}
	rootModule = m.Run.RootModulePath
	idx := m.BuildIndex()
	imports = make(map[string][]string, len(m.Packages))
	for i := range m.Packages {
		p := &m.Packages[i]
		if p.Class != godep.ClassInternal {
			continue
		}
		list := make([]string, 0, len(p.Imports))
		for _, id := range p.Imports {
			if n, ok := idx.Node(id); ok {
				list = append(list, n.ImportPath)
			}
		}
		sort.Strings(list)
		imports[p.ImportPath] = list
	}
	return
}
