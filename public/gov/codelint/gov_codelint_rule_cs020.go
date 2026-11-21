package codelint

import (
	_ "embed"
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// RuleCS020 — an exported function that roots a context or hides I/O
// behind a ctx-less signature (ADR-0395).
//
// CODINGSTANDARDS makes ctx mandatory and first on anything that does I/O,
// so the caller's cancellation, deadline and call identity reach the round
// trip. Two shapes break that without failing to compile:
//
//   - An exported function or method that calls context.Background() or
//     context.TODO(): whatever it does under that context cannot be
//     cancelled by its caller, and a call identity on the caller's ctx is
//     dropped.
//   - An exported function or method that takes no context.Context and calls
//     one of the I/O verbs that take one — a bus request, an app storage or
//     persist call, a facts or state store write, a generated store's Begin,
//     Delete or Ingest: the ctx it passes is not its caller's.
//
// Exempt: `package main`, `init`, test files, generated files (the driver
// drops their findings), and a site annotated with
// `//boxer:lint disable=CS020 reason="..."` — a documented root, such as
// app.Msg.Context for a message no requester waits on. Functions recorded in
// the rule's baseline (gov_codelint_rule_cs020_baseline.txt) are held: the
// baseline only shrinks.
type RuleCS020 struct {
	prefixes []string
	baseline map[string]struct{}
}

//go:embed gov_codelint_rule_cs020_baseline.txt
var cs020Baseline string

// cs020IoPackages are the packages whose ctx-first functions and methods
// count as I/O verbs for the second shape.
var cs020IoPackages = map[string]struct{}{
	"github.com/stergiotis/boxer/public/keelson/runtime/app":        {},
	"github.com/stergiotis/boxer/public/keelson/runtime/persist":    {},
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore": {},
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore": {},
}

// NewRuleCS020 is the rule over this repository's public tree, with the
// committed baseline.
func NewRuleCS020() (inst *RuleCS020) {
	inst = NewRuleCS020ForPrefixes("github.com/stergiotis/boxer/public")
	inst.baseline = parseCS020Baseline(cs020Baseline)
	return
}

// NewRuleCS020ForPrefixes is the rule over the packages at or below the
// given import-path prefixes, without a baseline.
func NewRuleCS020ForPrefixes(prefixes ...string) (inst *RuleCS020) {
	inst = &RuleCS020{prefixes: prefixes, baseline: map[string]struct{}{}}
	return
}

func parseCS020Baseline(text string) (keys map[string]struct{}) {
	keys = make(map[string]struct{}, 256)
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keys[line] = struct{}{}
	}
	return
}

func (inst *RuleCS020) Id() (id string) {
	id = "CS020"
	return
}

func (inst *RuleCS020) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityError
	return
}

func (inst *RuleCS020) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs020",
		Doc:  "CS020: an exported function takes its context from its caller — it neither roots one with context.Background/TODO nor hides an I/O verb behind a ctx-less signature",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS020) inScope(path string) bool {
	for _, p := range inst.prefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// cs020Key names a function for the baseline: "<import path>.<Func>" or
// "<import path>.<Recv>.<Method>".
func cs020Key(pkgPath string, fd *ast.FuncDecl) (key string) {
	key = pkgPath + "."
	if fd.Recv != nil && len(fd.Recv.List) == 1 {
		t := fd.Recv.List[0].Type
		if s, ok := t.(*ast.StarExpr); ok {
			t = s.X
		}
		switch x := t.(type) {
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		}
		if id, ok := t.(*ast.Ident); ok {
			key += id.Name + "."
		}
	}
	key += fd.Name.Name
	return
}

func (inst *RuleCS020) run(pass *analysis.Pass) (res any, err error) {
	if pass.Pkg == nil || pass.Pkg.Name() == "main" || !inst.inScope(pass.Pkg.Path()) {
		return
	}
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || !fd.Name.IsExported() || fd.Name.Name == "init" {
				continue
			}
			key := cs020Key(pass.Pkg.Path(), fd)
			if _, held := inst.baseline[key]; held {
				continue
			}
			takesCtx := inst.takesCtx(pass, fd)
			ast.Inspect(fd.Body, func(n ast.Node) (cont bool) {
				cont = true
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return
				}
				fn := calleeFunc(pass.TypesInfo, call)
				if fn == nil || fn.Pkg() == nil {
					return
				}
				if fn.Pkg().Path() == "context" && (fn.Name() == "Background" || fn.Name() == "TODO") {
					pass.Report(analysis.Diagnostic{
						Pos:     call.Pos(),
						End:     call.End(),
						Message: "CS020: " + key + " roots a context with context." + fn.Name() + "() — take ctx from the caller (or annotate a documented root)",
					})
					return
				}
				if !takesCtx && inst.isIoVerb(pass, fn) && !isRootCall(pass, call) {
					pass.Report(analysis.Diagnostic{
						Pos:     call.Pos(),
						End:     call.End(),
						Message: "CS020: " + key + " calls the I/O verb " + fn.Name() + " without taking a ctx — take ctx first and pass it on",
					})
				}
				return
			})
		}
	}
	return
}

// takesCtx says fd has a context.Context parameter.
func (inst *RuleCS020) takesCtx(pass *analysis.Pass, fd *ast.FuncDecl) (yes bool) {
	obj, _ := pass.TypesInfo.Defs[fd.Name].(*types.Func)
	if obj == nil {
		return
	}
	params := obj.Type().(*types.Signature).Params()
	for i := range params.Len() {
		if isContextType(params.At(i).Type()) {
			return true
		}
	}
	return
}

// isIoVerb says fn is one of the I/O verbs: ctx first, and either in one of
// the I/O packages or a generated store's write verb.
func (inst *RuleCS020) isIoVerb(pass *analysis.Pass, fn *types.Func) (yes bool) {
	sig := fn.Type().(*types.Signature)
	if sig.Params().Len() == 0 || !isContextType(sig.Params().At(0).Type()) {
		return
	}
	if _, ok := cs020IoPackages[fn.Pkg().Path()]; ok {
		return true
	}
	if sig.Recv() == nil {
		return
	}
	n := fn.Name()
	if n != "Begin" && n != "Delete" && !strings.HasPrefix(n, "Ingest") {
		return
	}
	return strings.HasSuffix(pass.Fset.Position(fn.Pos()).Filename, "_store.out.go")
}

func isContextType(t types.Type) (yes bool) {
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "context" && named.Obj().Name() == "Context"
}

// isRootCall says the call's first argument is itself a context.Background
// or context.TODO call, which the first shape reports already.
func isRootCall(pass *analysis.Pass, call *ast.CallExpr) (yes bool) {
	if len(call.Args) == 0 {
		return
	}
	inner, ok := ast.Unparen(call.Args[0]).(*ast.CallExpr)
	if !ok {
		return
	}
	fn := calleeFunc(pass.TypesInfo, inner)
	return fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == "context" && (fn.Name() == "Background" || fn.Name() == "TODO")
}
