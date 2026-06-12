package wasmsurvey

import (
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

// stateToken is the packageprops identifier for a state, used in generated
// source and to compare against harvested tokens.
func stateToken(s packageprops.WASMState) (tok string) {
	switch s {
	case packageprops.WASMCompiles:
		return "WASMCompiles"
	case packageprops.WASMBlocked:
		return "WASMBlocked"
	default:
		return "WASMUnknown"
	}
}

// parseStateToken is the inverse of stateToken.
func parseStateToken(tok string) (s packageprops.WASMState) {
	switch tok {
	case "WASMCompiles":
		return packageprops.WASMCompiles
	case "WASMBlocked":
		return packageprops.WASMBlocked
	default:
		return packageprops.WASMUnknown
	}
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
		path := filepath.Join(pr.Dir, propsFileName)
		_, statErr := os.Stat(path)
		exists := statErr == nil
		if exists && !overwrite {
			res.Skipped++
			continue // never clobber a curated declaration
		}
		var src []byte
		src, err = renderPropsFile(pr)
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

// renderPropsFile emits the gofmt-clean source of a package's props file.
func renderPropsFile(pr PackageReport) (src []byte, err error) {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pr.Name)
	fmt.Fprintf(&b, "import %q\n\n", propsImportPath)
	b.WriteString("// PackageProps records this package's curated properties (ADR-0080).\n")
	b.WriteString("// Seeded by `wasmsurvey props generate`; curate by hand, then `wasmsurvey props verify`.\n")
	b.WriteString("var PackageProps = packageprops.Props{\n")
	fmt.Fprintf(&b, "WASMWASI: packageprops.%s,\n", stateToken(stateFor(pr, TargetWASI)))
	fmt.Fprintf(&b, "WASMJS: packageprops.%s,\n", stateToken(stateFor(pr, TargetJS)))
	fmt.Fprintf(&b, "WASMFreestanding: packageprops.%s,\n", stateToken(stateFor(pr, TargetWasmUnknown)))
	b.WriteString("}\n")
	src, err = format.Source([]byte(b.String()))
	if err != nil {
		err = eb.Build().Str("pkg", pr.ImportPath).Errorf("format props file: %w", err)
	}
	return
}

// HarvestRow is one package's declared props, read from its package_props.go.
type HarvestRow struct {
	ImportPath       string
	WASMWASI         packageprops.WASMState
	WASMJS           packageprops.WASMState
	WASMFreestanding packageprops.WASMState
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
		fields, e := parsePropsFile(path)
		if e != nil {
			return nil // skip unparseable files rather than abort the whole harvest
		}
		rel, e := filepath.Rel(root, filepath.Dir(path))
		if e != nil {
			return nil
		}
		rows = append(rows, HarvestRow{
			ImportPath:       rootModule + "/" + filepath.ToSlash(rel),
			WASMWASI:         parseStateToken(fields["WASMWASI"]),
			WASMJS:           parseStateToken(fields["WASMJS"]),
			WASMFreestanding: parseStateToken(fields["WASMFreestanding"]),
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

// parsePropsFile extracts the PackageProps composite-literal field→token map
// from a package_props.go via go/ast (no type checking, no build).
func parsePropsFile(path string) (fields map[string]string, err error) {
	fset := token.NewFileSet()
	var f *ast.File
	f, err = parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	fields = make(map[string]string, 4)
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != "PackageProps" || i >= len(vs.Values) {
					continue
				}
				cl, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, elt := range cl.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok {
						continue
					}
					fields[key.Name] = exprToken(kv.Value)
				}
			}
		}
	}
	return fields, nil
}

// exprToken renders a packageprops.WASMX selector (or a bare ident) as its
// trailing identifier ("WASMCompiles").
func exprToken(e ast.Expr) (tok string) {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return v.Sel.Name
	case *ast.Ident:
		return v.Name
	}
	return ""
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
