package codelint

import (
	"go/types"
	"iter"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// Linter aggregates rules and runs them against a loaded package set.
//
// Zero value is usable; rules are added via Register.
type Linter struct {
	rules []RuleI
}

func NewLinter() (inst *Linter) {
	inst = &Linter{}
	return
}

func (inst *Linter) Register(r RuleI) {
	inst.rules = append(inst.rules, r)
}

// Run executes every registered rule against the supplied packages and
// yields findings as they are produced. Suppression directives are
// applied before yielding.
func (inst *Linter) Run(pkgs []*packages.Package) iter.Seq2[Finding, error] {
	return func(yield func(Finding, error) bool) {
		for _, r := range inst.rules {
			analyzer := r.Analyzer()
			ruleId := r.Id()
			sev := r.DefaultSeverity()
			for _, pkg := range pkgs {
				if pkg.Types == nil || pkg.TypesInfo == nil {
					continue
				}
				diags, perr := runAnalyzer(analyzer, pkg)
				if perr != nil {
					if !yield(Finding{}, eb.Build().
						Str("rule", ruleId).
						Str("pkg", pkg.PkgPath).
						Errorf("codelint pass: %w", perr)) {
						return
					}
					continue
				}
				disables := collectAllDisables(pkg)
				for _, d := range diags {
					pos := pkg.Fset.Position(d.Pos)
					if IsGeneratedFile(pos.Filename) {
						continue
					}
					if disables[pos.Filename].has(pos.Line, ruleId) {
						continue
					}
					f := Finding{
						RuleId:   ruleId,
						Severity: sev,
						Path:     pos.Filename,
						Line:     int32(pos.Line),
						Col:      int32(pos.Column),
						Message:  d.Message,
					}
					if !yield(f, nil) {
						return
					}
				}
			}
		}
	}
}

// runAnalyzer builds a per-package analysis.Pass and invokes the analyzer.
// Analyzers with Requires are not yet supported — phase-1 rules don't need
// inspect.Analyzer, and adding dependency resolution is deferred until a
// rule actually needs it.
func runAnalyzer(a *analysis.Analyzer, pkg *packages.Package) (diags []analysis.Diagnostic, err error) {
	if len(a.Requires) > 0 {
		err = eb.Build().Str("analyzer", a.Name).Errorf("codelint: analyzer Requires not yet supported")
		return
	}
	var collected []analysis.Diagnostic
	pass := &analysis.Pass{
		Analyzer:   a,
		Fset:       pkg.Fset,
		Files:      pkg.Syntax,
		Pkg:        pkg.Types,
		TypesInfo:  pkg.TypesInfo,
		TypesSizes: types.SizesFor("gc", "amd64"),
		ResultOf:   map[*analysis.Analyzer]any{},
		Report: func(d analysis.Diagnostic) {
			collected = append(collected, d)
		},
	}
	_, err = a.Run(pass)
	if err != nil {
		return
	}
	diags = collected
	return
}

func collectAllDisables(pkg *packages.Package) (m map[string]*fileDisables) {
	m = make(map[string]*fileDisables, len(pkg.Syntax))
	for _, f := range pkg.Syntax {
		path := pkg.Fset.Position(f.Pos()).Filename
		m[path] = collectDisables(pkg.Fset, f)
	}
	return
}
