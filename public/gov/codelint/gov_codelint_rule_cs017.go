package codelint

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// A goroutine an app starts is counted into its instance's lifetime scope
// (ADR-0322; the keelson-app skill's W5): through `app.LifetimeOf(ctx)` and
// `scope.Go`, through a task on `task.ForApp`, or through a child
// `lifetime.New(parent)`. The host retires the scope at close and drains it
// before it closes the bus client; a bare `go` statement is invisible to
// that drain and to the Running tab, so its bus calls can land on a closed
// client and its work outlives the window unseen.
//
// The rule flags a `go` statement in a package under the apps tree unless
// the goroutine is visibly owned. The owned shapes are the ones the apps use:
//
//   - The nil-scope fallback. The statement runs only when a scope is nil:
//     inside `if s == nil { … }`, in the else of `if s != nil`, or after an
//     `if s != nil { …; return }`. s is a `*lifetime.Scope`, or a value
//     whose type has a scope's `Go(func(context.Context)) bool` method (an
//     app's own wrapper over its scope). A host without a scope has no
//     drain to count into. A fallback that also runs when `Go` refused —
//     `if s != nil && s.Go(f) { return }; go f(…)` — starts work after the
//     scope retired, and is flagged.
//   - A task body. The goroutine's function calls `task.Run` or
//     `task.RunToCompletion`, so a task from `task.ForApp` holds the scope
//     while the work runs. The function is a literal, or a function or
//     method of the same package.
//   - Entered work. The goroutine's literal defers a `leave` obtained from
//     `(*lifetime.Scope).Enter`, directly or through a same-package function
//     that returns that call.
//   - A helper bounded by its spawner. The spawning function defers the
//     goroutine's stop — `defer close(ch)` or `defer cancel()` — on a value
//     the goroutine uses, or on one assigned with it (`hctx, cancel :=
//     context.WithCancel(ctx)`). The goroutine ends with the call that
//     started it, which is itself work some scope or task counts.
//   - A command. A `main` package under the apps tree is a process of its
//     own with no mount context, so it has no instance scope to count into.
//   - A process-wide goroutine. The enclosing function's doc comment carries
//     a `// lifetime: <reason>` line saying why the goroutine belongs to the
//     process rather than to an instance (a sampler singleton, a once-per-
//     process install).
//
// A goroutine started inside the function given to `scope.Go` is not
// exempt: Go counts the function's own run, not a goroutine it starts.
const (
	appsTreePkgPath    = "github.com/stergiotis/boxer/apps"
	lifetimePkgPath    = "github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	lifetimeScopeType  = "Scope"
	lifetimeEnterFunc  = "Enter"
	taskRunFunc        = "Run"
	taskRunToCompFunc  = "RunToCompletion"
	cs017RemedySummary = "start it with app.LifetimeOf(ctx).Go, or as a task on task.ForApp, so the host's drain and the Running tab see it (ADR-0322)"
)

// lifetimeAnnotationPattern matches a `// lifetime: <reason>` doc-comment
// line. A reason is required.
var lifetimeAnnotationPattern = regexp.MustCompile(`^//\s*lifetime:\s*\S`)

// RuleCS017 — a `go` statement in an app package that no lifetime scope or
// task owns. Packages outside the apps tree are out of scope: the runtime
// and widget packages manage their own goroutines. Commands and test files
// are exempt.
type RuleCS017 struct {
	prefixes []string
}

// NewRuleCS017 is the rule over this repository's apps tree.
func NewRuleCS017() (inst *RuleCS017) {
	inst = NewRuleCS017ForPrefixes(appsTreePkgPath)
	return
}

// NewRuleCS017ForPrefixes is the rule over the packages at or below the
// given import-path prefixes, for a consuming repository whose apps live
// elsewhere and for the rule's own testdata.
func NewRuleCS017ForPrefixes(prefixes ...string) (inst *RuleCS017) {
	inst = &RuleCS017{prefixes: prefixes}
	return
}

func (inst *RuleCS017) Id() (id string) {
	id = "CS017"
	return
}

func (inst *RuleCS017) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityWarn
	return
}

func (inst *RuleCS017) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs017",
		Doc:  "CS017: a goroutine an app starts is owned by its instance's lifetime scope — " + cs017RemedySummary,
		Run:  inst.run,
	}
	return
}

// inScope reports whether a package path lies at or below one of the rule's
// prefixes.
func (inst *RuleCS017) inScope(path string) bool {
	for _, p := range inst.prefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

func (inst *RuleCS017) run(pass *analysis.Pass) (res any, err error) {
	if pass.Pkg == nil || pass.Pkg.Name() == "main" || !inst.inScope(pass.Pkg.Path()) {
		return
	}
	c := &cs017Checker{pass: pass, decls: make(map[types.Object]*ast.FuncDecl)}
	for _, file := range pass.Files {
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if obj := pass.TypesInfo.Defs[fd.Name]; obj != nil {
				c.decls[obj] = fd
			}
		}
	}
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		var stack []ast.Node
		ast.Inspect(file, func(n ast.Node) (cont bool) {
			if n == nil {
				stack = stack[:len(stack)-1]
				return
			}
			stack = append(stack, n)
			cont = true
			g, ok := n.(*ast.GoStmt)
			if !ok || c.owned(g, stack) {
				return
			}
			pass.Report(analysis.Diagnostic{
				Pos:     g.Pos(),
				End:     g.End(),
				Message: "CS017: go statement in an app outside the instance's lifetime scope — " + cs017RemedySummary,
			})
			return
		})
	}
	return
}

type cs017Checker struct {
	pass  *analysis.Pass
	decls map[types.Object]*ast.FuncDecl
}

// owned reports whether the go statement at the top of stack has one of the
// owned shapes listed in the file comment.
func (inst *cs017Checker) owned(g *ast.GoStmt, stack []ast.Node) bool {
	encl, outer := enclosingFuncs(stack)
	if outer != nil && hasLifetimeAnnotation(outer.Doc) {
		return true
	}
	if inst.underNilScope(stack) {
		return true
	}
	body := inst.goBody(g)
	if body != nil && inst.callsTaskRun(body) {
		return true
	}
	if lit, ok := g.Call.Fun.(*ast.FuncLit); ok && inst.defersEnteredLeave(lit.Body) {
		return true
	}
	if encl != nil && inst.boundedBySpawner(g, encl) {
		return true
	}
	return false
}

// enclosingFuncs returns the body of the innermost function (declaration or
// literal) around the top of stack, and the outermost declaration.
func enclosingFuncs(stack []ast.Node) (encl *ast.BlockStmt, outer *ast.FuncDecl) {
	for i := len(stack) - 2; i >= 0; i-- {
		switch f := stack[i].(type) {
		case *ast.FuncLit:
			if encl == nil {
				encl = f.Body
			}
		case *ast.FuncDecl:
			if encl == nil {
				encl = f.Body
			}
			outer = f
		}
	}
	return
}

func hasLifetimeAnnotation(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, c := range doc.List {
		if lifetimeAnnotationPattern.MatchString(c.Text) {
			return true
		}
	}
	return false
}

// underNilScope reports whether the statement at the top of stack runs only
// when a scope is nil, looking outwards to the innermost function boundary.
func (inst *cs017Checker) underNilScope(stack []ast.Node) bool {
	for i := len(stack) - 1; i > 0; i-- {
		child, parent := stack[i], stack[i-1]
		switch p := parent.(type) {
		case *ast.FuncLit, *ast.FuncDecl:
			return false
		case *ast.IfStmt:
			if child == p.Body && inst.nilScopeCond(p.Cond, token.EQL) {
				return true
			}
			if child == p.Else && inst.nilScopeCond(p.Cond, token.NEQ) {
				return true
			}
		case *ast.BlockStmt:
			for _, s := range p.List {
				if s == child {
					break
				}
				if inst.returnsWhenScopeSet(s) {
					return true
				}
			}
		}
	}
	return false
}

// returnsWhenScopeSet reports whether s is `if x != nil { …; return }` with
// no else, x a scope.
func (inst *cs017Checker) returnsWhenScopeSet(s ast.Stmt) bool {
	is, ok := s.(*ast.IfStmt)
	if !ok || is.Else != nil || is.Init != nil || len(is.Body.List) == 0 {
		return false
	}
	if _, ok = is.Body.List[len(is.Body.List)-1].(*ast.ReturnStmt); !ok {
		return false
	}
	return inst.nilScopeCond(is.Cond, token.NEQ)
}

// nilScopeCond reports whether cond is `x <op> nil` (either order) with x a
// scope.
func (inst *cs017Checker) nilScopeCond(cond ast.Expr, op token.Token) bool {
	be, ok := ast.Unparen(cond).(*ast.BinaryExpr)
	if !ok || be.Op != op {
		return false
	}
	info := inst.pass.TypesInfo
	isNil := func(e ast.Expr) bool {
		tv, ok := info.Types[e]
		return ok && tv.IsNil()
	}
	switch {
	case isNil(be.Y):
		return isScopeLike(info.TypeOf(be.X))
	case isNil(be.X):
		return isScopeLike(info.TypeOf(be.Y))
	}
	return false
}

// isScopeLike reports whether t is a `*lifetime.Scope`, or has a method
// `Go(func(context.Context)) bool` as a scope has.
func isScopeLike(t types.Type) bool {
	if t == nil {
		return false
	}
	if isLifetimeScopePtr(t) {
		return true
	}
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "Go")
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig := fn.Signature()
	if sig.Params().Len() != 1 || sig.Results().Len() != 1 {
		return false
	}
	if b, ok := sig.Results().At(0).Type().Underlying().(*types.Basic); !ok || b.Kind() != types.Bool {
		return false
	}
	f, ok := sig.Params().At(0).Type().Underlying().(*types.Signature)
	if !ok || f.Params().Len() != 1 || f.Results().Len() != 0 {
		return false
	}
	named, ok := f.Params().At(0).Type().(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "context" && named.Obj().Name() == "Context"
}

func isLifetimeScopePtr(t types.Type) bool {
	p, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := p.Elem().(*types.Named)
	return ok && named.Obj().Name() == lifetimeScopeType && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == lifetimePkgPath
}

// goBody is the body the goroutine runs: the literal's, or a same-package
// function's or method's.
func (inst *cs017Checker) goBody(g *ast.GoStmt) (body *ast.BlockStmt) {
	switch f := ast.Unparen(g.Call.Fun).(type) {
	case *ast.FuncLit:
		return f.Body
	case *ast.Ident:
		return inst.declBody(inst.pass.TypesInfo.Uses[f])
	case *ast.SelectorExpr:
		return inst.declBody(inst.pass.TypesInfo.Uses[f.Sel])
	}
	return nil
}

func (inst *cs017Checker) declBody(obj types.Object) *ast.BlockStmt {
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil
	}
	if fd, ok := inst.decls[fn.Origin()]; ok {
		return fd.Body
	}
	return nil
}

// callsTaskRun reports whether body calls task.Run or task.RunToCompletion
// outside any nested function literal.
func (inst *cs017Checker) callsTaskRun(body *ast.BlockStmt) (found bool) {
	inspectShallow(body, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return
		}
		fn := calleeFunc(inst.pass.TypesInfo, call)
		if fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == taskPkgPath && (fn.Name() == taskRunFunc || fn.Name() == taskRunToCompFunc) {
			found = true
		}
	})
	return
}

// defersEnteredLeave reports whether body defers `leave()` with leave
// obtained from a scope's Enter.
func (inst *cs017Checker) defersEnteredLeave(body *ast.BlockStmt) (found bool) {
	info := inst.pass.TypesInfo
	inspectShallow(body, func(n ast.Node) {
		d, ok := n.(*ast.DeferStmt)
		if !ok || found || len(d.Call.Args) != 0 {
			return
		}
		id, ok := d.Call.Fun.(*ast.Ident)
		if !ok {
			return
		}
		v, ok := info.Uses[id].(*types.Var)
		if !ok {
			return
		}
		if src := inst.definingCall(v); src != nil && inst.isEnterCall(src) {
			found = true
		}
	})
	return
}

// definingCall is the call on the right of the assignment that defines v,
// when v is defined by `a, b := call(…)` or `a := call(…)` in this package.
func (inst *cs017Checker) definingCall(v *types.Var) (call *ast.CallExpr) {
	as := inst.definingAssign(v)
	if as == nil || len(as.Rhs) != 1 {
		return nil
	}
	call, _ = ast.Unparen(as.Rhs[0]).(*ast.CallExpr)
	return
}

// definingAssign is the `:=` statement that defines v, searched in the file
// that holds v's declaration.
func (inst *cs017Checker) definingAssign(v *types.Var) (as *ast.AssignStmt) {
	info := inst.pass.TypesInfo
	for _, file := range inst.pass.Files {
		if v.Pos() < file.Pos() || v.Pos() > file.End() {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if as != nil {
				return false
			}
			a, ok := n.(*ast.AssignStmt)
			if !ok || a.Tok != token.DEFINE {
				return true
			}
			for _, l := range a.Lhs {
				if id, ok := l.(*ast.Ident); ok && info.Defs[id] == v {
					as = a
					return false
				}
			}
			return true
		})
		return
	}
	return
}

// isEnterCall reports whether call is (*lifetime.Scope).Enter, or a call to
// a same-package function whose body is `return <scope>.Enter()`.
func (inst *cs017Checker) isEnterCall(call *ast.CallExpr) bool {
	fn := calleeFunc(inst.pass.TypesInfo, call)
	if fn == nil {
		return false
	}
	if isScopeEnter(fn) {
		return true
	}
	fd, ok := inst.decls[fn.Origin()]
	if !ok || len(fd.Body.List) != 1 {
		return false
	}
	ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	inner, ok := ast.Unparen(ret.Results[0]).(*ast.CallExpr)
	if !ok {
		return false
	}
	innerFn := calleeFunc(inst.pass.TypesInfo, inner)
	return innerFn != nil && isScopeEnter(innerFn)
}

func isScopeEnter(fn *types.Func) bool {
	if fn.Name() != lifetimeEnterFunc {
		return false
	}
	recv := fn.Signature().Recv()
	return recv != nil && isLifetimeScopePtr(recv.Type())
}

// boundedBySpawner reports whether encl, the function that holds g, defers
// a stop the goroutine sees: `defer close(x)` or `defer x()` with x, or a
// variable assigned with x, used by the go statement.
func (inst *cs017Checker) boundedBySpawner(g *ast.GoStmt, encl *ast.BlockStmt) (found bool) {
	info := inst.pass.TypesInfo
	used := make(map[types.Object]struct{})
	ast.Inspect(g.Call, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := info.Uses[id].(*types.Var); ok {
				used[v] = struct{}{}
			}
		}
		return true
	})
	inspectShallow(encl, func(n ast.Node) {
		d, ok := n.(*ast.DeferStmt)
		if !ok || found {
			return
		}
		var stop *ast.Ident
		if b, ok := info.Uses[identOf(d.Call.Fun)].(*types.Builtin); ok && b.Name() == "close" && len(d.Call.Args) == 1 {
			stop = identOf(d.Call.Args[0])
		} else if len(d.Call.Args) == 0 {
			stop = identOf(d.Call.Fun)
		}
		if stop == nil {
			return
		}
		v, ok := info.Uses[stop].(*types.Var)
		if !ok {
			return
		}
		if _, ok := used[v]; ok {
			found = true
			return
		}
		as := inst.definingAssign(v)
		if as == nil {
			return
		}
		for _, l := range as.Lhs {
			if id, ok := l.(*ast.Ident); ok {
				if _, ok := used[info.Defs[id]]; ok {
					found = true
					return
				}
			}
		}
	})
	return
}

func identOf(e ast.Expr) (id *ast.Ident) {
	id, _ = ast.Unparen(e).(*ast.Ident)
	return
}

// calleeFunc is the function or method a call invokes, nil for a call of a
// func value, a conversion or a builtin.
func calleeFunc(info *types.Info, call *ast.CallExpr) (fn *types.Func) {
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		fn, _ = info.Uses[f].(*types.Func)
	case *ast.SelectorExpr:
		fn, _ = info.Uses[f.Sel].(*types.Func)
	case *ast.IndexExpr:
		if id := identOf(f.X); id != nil {
			fn, _ = info.Uses[id].(*types.Func)
		}
	}
	return
}

// inspectShallow visits the nodes of body, not descending into nested
// function literals.
func inspectShallow(body *ast.BlockStmt, visit func(n ast.Node)) {
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		visit(n)
		return true
	})
}
