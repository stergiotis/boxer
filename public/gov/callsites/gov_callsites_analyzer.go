package callsites

import (
	"context"
	"go/ast"
	"go/token"
	"go/types"
	"iter"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// AnalyzerService loads packages and streams classified call sites.
// All fields are honored (ADR-0107 §SD5).
type AnalyzerService struct {
	// Patterns are package patterns for packages.Load; "./..." when empty.
	Patterns []string
	// Dir is the working directory for loading and adjudication; the
	// process working directory when empty.
	Dir string
	// BuildTags are passed as -tags to loading and adjudication. When empty
	// no -tags flag is emitted at all, leaving GOFLAGS untouched.
	BuildTags []string
	// BuildFlags are additional go command flags for packages.Load and the
	// adjudication build.
	BuildFlags []string
	// IncludeTests also loads and scans test variants of the matched
	// packages. Test files are outside `go build`, so their sites stay
	// Compiler.Checked == false.
	IncludeTests bool
	// Adjudicate joins compiler devirtualization/inlining decisions onto
	// the classified sites (ADR-0107 §SD1).
	Adjudicate bool
	// OnLoadStats, when set, receives coverage statistics once after a
	// successful load (ADR-0107 §SD5).
	OnLoadStats func(LoadStats)
}

// All streams every call site of the matched packages. The sequence yields
// exactly one non-nil error — a load or adjudication failure — and then
// stops; partial coverage is never silent (ADR-0107 §SD5). Terminating the
// consumer early is safe (§SD6).
func (inst *AnalyzerService) All(ctx context.Context) iter.Seq2[CallSite, error] {
	return func(yield func(CallSite, error) bool) {
		var pkgs []*packages.Package
		var decisions map[posKey]CompilerDecision
		var err error

		pkgs, err = inst.loadE(ctx)
		if err != nil {
			yield(CallSite{}, err)
			return
		}
		if inst.OnLoadStats != nil {
			inst.OnLoadStats(loadStats(pkgs))
		}
		if inst.Adjudicate {
			decisions, err = inst.adjudicateE(ctx, hasSingleMainRoot(pkgs))
			if err != nil {
				yield(CallSite{}, err)
				return
			}
		}

		// Overlapping patterns and test-augmented package variants repeat
		// files; scan each file once.
		seenFiles := make(map[string]struct{}, 256)
		for _, pkg := range pkgs {
			if ctx.Err() != nil {
				yield(CallSite{}, ctx.Err())
				return
			}
			if !inst.scanPackage(pkg, decisions, seenFiles, yield) {
				return
			}
		}
	}
}

func (inst *AnalyzerService) loadE(ctx context.Context) (pkgs []*packages.Package, err error) {
	patterns := inst.patterns()
	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles | // IgnoredFiles for LoadStats
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedImports |
			packages.NeedModule,
		Context: ctx,
		Dir:     inst.Dir,
		Tests:   inst.IncludeTests,
	}
	if len(inst.BuildTags) > 0 {
		cfg.BuildFlags = append(cfg.BuildFlags, "-tags="+strings.Join(inst.BuildTags, ","))
	}
	cfg.BuildFlags = append(cfg.BuildFlags, inst.BuildFlags...)

	pkgs, err = packages.Load(cfg, patterns...)
	if err != nil {
		err = eb.Build().Strs("patterns", patterns).Errorf("callsites load: %w", err)
		return
	}
	if len(pkgs) == 0 {
		err = eb.Build().Strs("patterns", patterns).Errorf("callsites load: no packages matched")
		return
	}
	for _, p := range pkgs {
		if len(p.Errors) == 0 {
			continue
		}
		err = eb.Build().
			Str("pkg", p.PkgPath).
			Int("errCount", len(p.Errors)).
			Str("firstErr", p.Errors[0].Msg).
			Errorf("callsites load: package has errors")
		return
	}
	return
}

func (inst *AnalyzerService) patterns() []string {
	if len(inst.Patterns) == 0 {
		return []string{"./..."}
	}
	return inst.Patterns
}

func loadStats(pkgs []*packages.Package) (stats LoadStats) {
	for _, p := range pkgs {
		if strings.Contains(p.ID, " [") || strings.HasSuffix(p.ID, ".test") {
			continue // test variants repeat the plain package's files
		}
		stats.Packages++
		for _, f := range p.IgnoredFiles {
			if strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") {
				stats.IgnoredFiles++
			}
		}
	}
	return
}

// hasSingleMainRoot reports whether the build targets resolve to exactly one
// main package — the one `go build` case that writes a binary into Dir.
// Test-variant roots (loaded when IncludeTests is set) are not build targets.
func hasSingleMainRoot(pkgs []*packages.Package) bool {
	buildRoots := 0
	mains := 0
	for _, p := range pkgs {
		if strings.Contains(p.ID, " [") || strings.HasSuffix(p.ID, ".test") {
			continue
		}
		buildRoots++
		if p.Name == "main" {
			mains++
		}
	}
	return buildRoots == 1 && mains == 1
}

// scanPackage walks one package and yields its call sites. It reports false
// once the consumer has terminated the sequence; the walk stops without
// calling yield again (ADR-0107 §SD6).
func (inst *AnalyzerService) scanPackage(pkg *packages.Package, decisions map[posKey]CompilerDecision, seenFiles map[string]struct{}, yield func(CallSite, error) bool) (cont bool) {
	cont = true
	fset := pkg.Fset
	info := pkg.TypesInfo
	modulePath := ""
	if pkg.Module != nil {
		modulePath = pkg.Module.Path
	}
	for _, file := range pkg.Syntax {
		fileName := fset.Position(file.Pos()).Filename
		if _, seen := seenFiles[fileName]; seen {
			continue
		}
		seenFiles[fileName] = struct{}{}
		checked := decisions != nil && !strings.HasSuffix(fileName, "_test.go")
		ast.Inspect(file, func(n ast.Node) bool {
			if !cont {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			site := inst.analyzeCall(call, fset, info, pkg.Types, modulePath)
			if checked {
				site.Compiler = decisions[posKey{file: site.File, line: site.Line, col: site.Col}]
				site.Compiler.Checked = true
			}
			if !yield(site, nil) {
				cont = false
				return false
			}
			return true
		})
		if !cont {
			return
		}
	}
	return
}

// analyzeCall classifies one call expression (ADR-0107 §SD2). It is
// infallible: unresolvable callees yield CallTypeUnknown, never a fabricated
// class (§SD6).
func (inst *AnalyzerService) analyzeCall(call *ast.CallExpr, fset *token.FileSet, info *types.Info, currentPkg *types.Package, modulePath string) (site CallSite) {
	pos := fset.Position(call.Lparen)
	site = CallSite{
		File: pos.Filename,
		Line: pos.Line,
		Col:  pos.Column,
	}

	// Conversions use call syntax but dispatch nothing: []byte(x), (*S)(p),
	// int64(n), MyType(v), G[int](v).
	if tv, ok := info.Types[call.Fun]; ok && tv.IsType() {
		site.Type = CallTypeConversion
		site.Func = types.TypeString(tv.Type, types.RelativeTo(currentPkg))
		return
	}

	fun := ast.Unparen(call.Fun)

	// An immediately-invoked func literal is a direct static call.
	if _, isLit := fun.(*ast.FuncLit); isLit {
		site.Type = CallTypeMonomorphic
		site.Origin = OriginLocal
		site.Func = "(func literal)"
		return
	}

	var callIdent *ast.Ident
	switch t := fun.(type) {
	case *ast.Ident:
		callIdent = t
	case *ast.SelectorExpr:
		callIdent = t.Sel
	case *ast.IndexExpr: // F[T](…), fns[i](…)
		switch x := ast.Unparen(t.X).(type) {
		case *ast.Ident:
			callIdent = x
		case *ast.SelectorExpr:
			callIdent = x.Sel
		}
	case *ast.IndexListExpr: // F[A, B](…)
		switch x := ast.Unparen(t.X).(type) {
		case *ast.Ident:
			callIdent = x
		case *ast.SelectorExpr:
			callIdent = x.Sel
		}
	}
	if callIdent == nil {
		// Calling the result of an expression: f()(), m[k](), x.(func())().
		site.Type = CallTypeDynamicPolymorphic
		site.Func = "(indirect)"
		return
	}

	obj := info.Uses[callIdent]
	if obj == nil {
		site.Type = CallTypeUnknown
		site.Func = callIdent.Name
		return
	}
	site.Func = obj.Name()
	site.Origin = classifyOrigin(obj.Pkg(), modulePath)

	var fn *types.Func
	switch o := obj.(type) {
	case *types.Builtin:
		site.Type = CallTypeBuiltin
		site.Origin = OriginStdLib
		return
	case *types.TypeName:
		// Conversion via a type name that info.Types did not already flag.
		site.Type = CallTypeConversion
		return
	case *types.Func:
		fn = o
		site.Func = o.FullName()
	default:
		// A func-typed var, field or parameter: dynamic dispatch.
		site.Type = CallTypeDynamicPolymorphic
		return
	}

	// Generic function instantiation, explicit or inferred (§SD2: TypeArgs
	// carries the callee's own instantiation).
	if instance, isInstance := info.Instances[callIdent]; isInstance {
		site.Type = CallTypeStaticPolymorphic
		site.TypeArgs = classifyTypeArgs(instance.TypeArgs, currentPkg)
		return
	}

	// Method calls: classify by receiver (§SD2: RecvTypeArgs carries the
	// receiver's instantiation).
	if selExpr, isSel := fun.(*ast.SelectorExpr); isSel {
		if sel, isSelection := info.Selections[selExpr]; isSelection {
			recv := types.Unalias(sel.Recv())
			if ptr, isPtr := recv.(*types.Pointer); isPtr {
				recv = types.Unalias(ptr.Elem())
			}
			// A type-parameter receiver dispatches through the dictionary;
			// checked before IsInterface, which is true for type parameters
			// (their underlying type is the constraint interface).
			if tp, isTypeParam := recv.(*types.TypeParam); isTypeParam {
				site.Type = CallTypeStaticPolymorphic
				site.RecvTypeArgs = []TypeArgInfo{newTypeArgInfo(tp, currentPkg)}
				return
			}
			// Interface dispatch — including instantiated generic
			// interfaces, hence checked before the Named case below.
			if types.IsInterface(recv) {
				site.Type = CallTypeDynamicPolymorphic
				return
			}
			if named, isNamed := recv.(*types.Named); isNamed && named.TypeArgs().Len() > 0 {
				site.Type = CallTypeStaticPolymorphic
				site.RecvTypeArgs = classifyTypeArgs(named.TypeArgs(), currentPkg)
				return
			}
			// A concrete receiver can still promote an embedded interface's
			// method; the declared receiver tells.
			if sig := fn.Signature(); sig.Recv() != nil && types.IsInterface(sig.Recv().Type()) {
				site.Type = CallTypeDynamicPolymorphic
				return
			}
		}
	}

	site.Type = CallTypeMonomorphic
	return
}

// classifyOrigin implements the module-based origin rule (ADR-0107 §SD4).
func classifyOrigin(pkg *types.Package, modulePath string) OriginE {
	if pkg == nil {
		// Universe scope: builtins, error.Error.
		return OriginStdLib
	}
	path := pkg.Path()
	if modulePath != "" && (path == modulePath || strings.HasPrefix(path, modulePath+"/")) {
		return OriginLocal
	}
	first, _, _ := strings.Cut(path, "/")
	if !strings.Contains(first, ".") {
		return OriginStdLib
	}
	return Origin3rdParty
}

func classifyTypeArgs(args *types.TypeList, currentPkg *types.Package) []TypeArgInfo {
	n := args.Len()
	if n == 0 {
		return nil
	}
	out := make([]TypeArgInfo, 0, n)
	for i := range n {
		out = append(out, newTypeArgInfo(args.At(i), currentPkg))
	}
	return out
}

func newTypeArgInfo(t types.Type, currentPkg *types.Package) TypeArgInfo {
	return TypeArgInfo{
		Type:  types.TypeString(t, types.RelativeTo(currentPkg)),
		Shape: classifyShape(t),
	}
}

// classifyShape maps a type argument onto the gcshape axis (ADR-0107 §SD3).
// Only the argument's top-level shape is classified; pointer collapse inside
// a composite ([]*T) is out of scope for the per-argument verdict.
func classifyShape(t types.Type) ShapeClassE {
	t = types.Unalias(t)
	if _, isTypeParam := t.(*types.TypeParam); isTypeParam {
		return ShapeClassTypeParam
	}
	switch u := t.Underlying().(type) {
	case *types.Interface:
		return ShapeClassInterface
	case *types.Pointer:
		return ShapeClassPointer
	case *types.Basic:
		if u.Kind() == types.UnsafePointer {
			return ShapeClassPointer
		}
		return ShapeClassStenciled
	case *types.Struct, *types.Array, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Tuple:
		return ShapeClassStenciled
	default:
		return ShapeClassUnknown
	}
}
