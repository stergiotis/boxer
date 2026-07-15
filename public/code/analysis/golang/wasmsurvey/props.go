package wasmsurvey

import (
	"context"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/code/analysis/golang/propsfile"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/packageprops"
)

// This file implements the `props` command group (ADR-0080): generate seeds a
// per-package PackageProps declaration from the survey's verdict (idempotent-
// create, never clobbering a curated file), harvest reads the committed
// declarations into an overview table without re-running the survey, and
// verify reconciles declaration against the freshly computed verdict and gates
// regressions. The declaration vocabulary lives in public/packageprops.

const (
	propsFileName   = "package_props.go"
	propsImportPath = "github.com/stergiotis/boxer/public/packageprops"
)

// tierToState maps a survey verdict tier to the declared wasm state.
func tierToState(t Tier) (s packageprops.WASMState) {
	switch t {
	case TierGreen:
		return packageprops.WASMCompiles
	case TierRed:
		return packageprops.WASMBlocked
	default:
		return packageprops.WASMUnknown
	}
}

// heuristicKind guesses a package's Kind from its leaf directory name, used only
// to seed a fresh declaration. demo/example directory names are high-precision;
// integration tests are file-level (`*_integration_test.go`) in this repo, not
// package-level, so this never returns KindIntegrationTest. GenerateProps only
// consults it when no curated Kind already exists, so a hand-set value always
// wins on re-seed.
func heuristicKind(pr PackageReport) (k packageprops.Kind) {
	switch filepath.Base(pr.Dir) {
	case "demo", "demos":
		return packageprops.KindDemo
	case "example", "examples":
		return packageprops.KindExample
	}
	return packageprops.KindUnspecified
}

// stateFor returns a package's verdict state for one target.
func stateFor(pr PackageReport, target TargetID) (s packageprops.WASMState) {
	for _, v := range pr.Targets {
		if v.Target == target {
			return tierToState(v.Tier())
		}
	}
	return packageprops.WASMUnknown
}

// inScope reports whether importPath is under one of the prefixes (empty ⇒ all).
func inScope(importPath string, prefixes []string) (b bool) {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if importPath == p || strings.HasPrefix(importPath, p+"/") {
			return true
		}
	}
	return false
}

// patternsToPrefixes converts go list patterns ("./public/math/...") to
// import-path prefixes ("<module>/public/math"). A whole-module pattern
// ("./...") yields nil (matches everything).
func patternsToPrefixes(rootModule string, patterns []string) (prefixes []string) {
	for _, pat := range patterns {
		p := strings.TrimPrefix(pat, "./")
		p = strings.TrimSuffix(p, "...")
		p = strings.Trim(p, "/")
		if p == "" || p == "." {
			return nil // matches everything
		}
		prefixes = append(prefixes, rootModule+"/"+p)
	}
	return
}

// GenerateResult summarizes a generate run.
type GenerateResult struct {
	Created      int
	Overwritten  int      // rewritten because overwrite was set
	Skipped      int      // already had a props file (idempotent-create)
	WrittenPaths []string // created + overwritten
}

// GenerateProps runs the survey for opts and seeds a package_props.go in each
// in-scope package (scope derived from opts.Patterns). With overwrite false it
// is idempotent-create — it never clobbers an existing (curated) declaration
// (ADR-0080 SD3); with overwrite true it re-seeds every in-scope file (used for
// the initial rollout or to refresh after a verdict change).
func GenerateProps(ctx context.Context, opts Options, overwrite bool) (res GenerateResult, err error) {
	var survey Survey
	survey, err = Run(ctx, opts)
	if err != nil {
		return
	}
	prefixes := patternsToPrefixes(survey.RootModule, opts.Patterns)
	for _, pr := range survey.Packages {
		if pr.Dir == "" || pr.Name == "" || !inScope(pr.ImportPath, prefixes) {
			continue
		}
		if pr.ImportPath == propsImportPath {
			continue // packageprops cannot declare props referencing itself (import cycle)
		}
		path := filepath.Join(pr.Dir, propsFileName)
		_, statErr := os.Stat(path)
		exists := statErr == nil
		if exists && !overwrite {
			res.Skipped++
			continue // never clobber a curated declaration
		}
		// Read the existing declaration so a re-seed overlays only the fields
		// this survey owns. Everything else — a curated Kind, the capability
		// survey's verdicts — is preserved by Merge rather than by a per-field
		// special case here (ADR-0120 SD7).
		var base packageprops.Props
		if exists {
			if p, e := propsfile.Parse(path); e == nil {
				base = p
			}
		}
		// Kind is human-owned, not survey-computable: a curated value wins, and
		// the directory-name heuristic only seeds a declaration that has none
		// (ADR-0080 §SD3 hybrid lifecycle).
		kind := base.Kind
		if kind == packageprops.KindUnspecified {
			kind = heuristicKind(pr)
		}
		merged := propsfile.Merge(base, packageprops.Props{
			WASMWASI:         stateFor(pr, TargetWASI),
			WASMJS:           stateFor(pr, TargetJS),
			WASMFreestanding: stateFor(pr, TargetWasmUnknown),
			Kind:             kind,
		}, propsfile.FieldsWASM|propsfile.FieldsKind)
		var src []byte
		src, err = propsfile.Render(pr.Name, pr.ImportPath, merged)
		if err != nil {
			return
		}
		if err = os.WriteFile(path, src, 0o644); err != nil {
			err = eb.Build().Str("path", path).Errorf("write props file: %w", err)
			return
		}
		if exists {
			res.Overwritten++
		} else {
			res.Created++
		}
		res.WrittenPaths = append(res.WrittenPaths, path)
	}
	return
}

// renderHarvestGo emits the harvested rows as a gofmt-clean Go file declaring
// `var Table = packageprops.Table{...}` in pkgName — the whole-repo static
// snapshot for embedding into a binary that does not link every package
// (ADR-0080 `props harvest --emit go`).
func renderHarvestGo(rows []HarvestRow, pkgName string) (src []byte, err error) {
	var b strings.Builder
	b.WriteString("// Code generated by `wasmsurvey props harvest --emit go`; DO NOT EDIT.\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkgName)
	fmt.Fprintf(&b, "import %q\n\n", propsImportPath)
	b.WriteString("// Table is every package's declared PackageProps, harvested from source.\n")
	b.WriteString("var Table = packageprops.Table{\n")
	for _, r := range rows {
		// One row per line, sharing propsfile's field rendering so the table and
		// the per-package declarations cannot disagree about a field.
		fmt.Fprintf(&b, "{ImportPath: %q, Props: packageprops.Props{%s}},\n",
			r.ImportPath, strings.Join(propsfile.Fields(r.Props), ", "))
	}
	b.WriteString("}\n")
	src, err = format.Source([]byte(b.String()))
	if err != nil {
		err = eb.Build().Errorf("format harvest go: %w", err)
	}
	return
}

// HarvestRow is one package's declared props, read from its package_props.go.
type HarvestRow struct {
	ImportPath string
	packageprops.Props
}

// HarvestProps walks root for package_props.go files and parses their
// PackageProps declarations into rows, sorted by import path. It does not run
// the survey — it is the cheap, toolchain-free overview of what is declared.
func HarvestProps(root string, rootModule string) (rows []HarvestRow, err error) {
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "node_modules" || strings.HasPrefix(name, ".wasmsurvey") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != propsFileName {
			return nil
		}
		props, e := propsfile.Parse(path)
		if e != nil {
			return nil // skip unparseable files rather than abort the whole harvest
		}
		rel, e := filepath.Rel(root, filepath.Dir(path))
		if e != nil {
			return nil
		}
		rows = append(rows, HarvestRow{
			ImportPath: rootModule + "/" + filepath.ToSlash(rel),
			Props:      props,
		})
		return nil
	})
	if err != nil {
		err = eb.Build().Str("root", root).Errorf("harvest props: %w", err)
		return
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ImportPath < rows[j].ImportPath })
	return
}

// Mismatch is one declared-vs-computed disagreement found by verify.
type Mismatch struct {
	ImportPath   string
	Target       TargetID
	Declared     packageprops.WASMState
	Computed     packageprops.WASMState
	IsRegression bool // declared Compiles, computed Blocked — the CI-failing case
}

// VerifyProps reconciles declared PackageProps (harvested) against freshly
// computed verdicts (the survey for opts) and returns the mismatches. A
// regression — a package declared WASMCompiles that the survey now finds
// WASMBlocked — is the sound, CI-failing signal (ADR-0078: static-red is a
// sound lower bound, so this gates without TinyGo).
func VerifyProps(ctx context.Context, opts Options, root string) (mismatches []Mismatch, err error) {
	var survey Survey
	survey, err = Run(ctx, opts)
	if err != nil {
		return
	}
	declared, err := HarvestProps(root, survey.RootModule)
	if err != nil {
		return
	}
	computed := make(map[string]PackageReport, len(survey.Packages))
	for _, pr := range survey.Packages {
		computed[pr.ImportPath] = pr
	}
	prefixes := patternsToPrefixes(survey.RootModule, opts.Patterns)
	for _, row := range declared {
		if !inScope(row.ImportPath, prefixes) {
			continue
		}
		pr, ok := computed[row.ImportPath]
		if !ok {
			continue // declared but not in this survey's scope/closure
		}
		// Only the WASM* verdicts are reconciled: the survey computes them, so a
		// declaration can regress against reality. Kind has no computable oracle
		// (it is pure curated intent), so verify never flags it.
		for _, td := range []struct {
			target   TargetID
			declared packageprops.WASMState
		}{
			{TargetWASI, row.WASMWASI},
			{TargetJS, row.WASMJS},
			{TargetWasmUnknown, row.WASMFreestanding},
		} {
			comp := stateFor(pr, td.target)
			if td.declared == packageprops.WASMUnknown {
				continue // nothing asserted
			}
			if comp != td.declared {
				mismatches = append(mismatches, Mismatch{
					ImportPath:   row.ImportPath,
					Target:       td.target,
					Declared:     td.declared,
					Computed:     comp,
					IsRegression: td.declared == packageprops.WASMCompiles && comp == packageprops.WASMBlocked,
				})
			}
		}
	}
	return
}
