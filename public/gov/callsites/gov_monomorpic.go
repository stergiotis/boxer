//go:build llm_generated_gemini3pro

package callsites

import (
	"context"
	"go/ast"
	"go/token"
	"go/types"
	"iter"
	"os"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// CallTypeE defines the nature of the call site.
type CallTypeE uint8

const (
	CallTypeMonomorphic        CallTypeE = 1
	CallTypeStaticPolymorphic  CallTypeE = 2
	CallTypeDynamicPolymorphic CallTypeE = 3
)

func (inst CallTypeE) String() string {
	switch inst {
	case CallTypeMonomorphic:
		return "Mono"
	case CallTypeStaticPolymorphic:
		return "StaticPoly"
	case CallTypeDynamicPolymorphic:
		return "DynPoly"
	default:
		return "Unknown"
	}
}

// StaticPolySubtypeE refines the StaticPolymorphic case.
type StaticPolySubtypeE uint8

const (
	StaticPolyNone      StaticPolySubtypeE = 0
	StaticPolyOptimized StaticPolySubtypeE = 1 // Primitives, Strings, Slices of Primitives (Stenciled)
	StaticPolyGeneric   StaticPolySubtypeE = 2 // Pointers, Interfaces, Complex Structs (Likely Dictionary/Shape)
)

func (inst StaticPolySubtypeE) String() string {
	switch inst {
	case StaticPolyOptimized:
		return "Optimized"
	case StaticPolyGeneric:
		return "Dictionary"
	default:
		return ""
	}
}

// OriginE defines where the callee lives.
type OriginE uint8

const (
	OriginLocal    OriginE = 1
	OriginStdLib   OriginE = 2
	Origin3rdParty OriginE = 3
)

func (inst OriginE) String() string {
	switch inst {
	case OriginLocal:
		return "Local"
	case OriginStdLib:
		return "StdLib"
	case Origin3rdParty:
		return "3rdParty"
	default:
		return "Unknown"
	}
}

type CallSite struct {
	File          string
	Line          int
	Func          string
	Type          CallTypeE
	StaticSubtype StaticPolySubtypeE // Only populated if Type == StaticPoly
	Origin        OriginE
}

// AnalyzerService controls the analysis logic.
type AnalyzerService struct {
	Pattern string
}

func (inst *AnalyzerService) Run(ctx context.Context) iter.Seq2[CallSite, error] {
	return func(yield func(CallSite, error) bool) {
		var cfg *packages.Config
		var pkgs []*packages.Package
		var err error
		var loadPattern string

		{
			cfg = &packages.Config{
				Mode:    packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
				Context: ctx,
			}
			if info, errStat := os.Stat(inst.Pattern); errStat == nil && info.IsDir() {
				cfg.Dir = inst.Pattern
				loadPattern = "."
			} else {
				loadPattern = inst.Pattern
			}
		}

		pkgs, err = packages.Load(cfg, loadPattern)
		if err != nil {
			yield(CallSite{}, eh.Errorf("failed to load packages: %w", err))
			return
		}

		for pkg, errIter := range inst.iteratePackages(pkgs) {
			if errIter != nil {
				if !yield(CallSite{}, eh.Errorf("package load error: %w", errIter)) {
					return
				}
				continue
			}

			for site, errAnalysis := range inst.scanPackage(pkg) {
				if errAnalysis != nil {
					// Convention: We swallow analysis errors for individual nodes but could log them.
					continue
				}
				if !yield(site, nil) {
					return
				}
			}
		}
	}
}

func (inst *AnalyzerService) iteratePackages(pkgs []*packages.Package) iter.Seq2[*packages.Package, error] {
	return func(yield func(*packages.Package, error) bool) {
		for _, pkg := range pkgs {
			if len(pkg.Errors) > 0 {
				for _, pkgErr := range pkg.Errors {
					if !yield(nil, pkgErr) {
						return
					}
				}
				continue
			}
			if !yield(pkg, nil) {
				return
			}
		}
	}
}

func (inst *AnalyzerService) scanPackage(pkg *packages.Package) iter.Seq2[CallSite, error] {
	return func(yield func(CallSite, error) bool) {
		fset := pkg.Fset
		info := pkg.TypesInfo

		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				if n == nil {
					return true
				}
				if call, ok := n.(*ast.CallExpr); ok {
					site, err := inst.analyzeCall(call, fset, info, pkg.Types)
					if err != nil {
						return true
					}
					if !yield(site, nil) {
						return false
					}
				}
				return true
			})
		}
	}
}

// analyzeCall determines the nature of a specific AST call expression.
func (inst *AnalyzerService) analyzeCall(call *ast.CallExpr, fset *token.FileSet, info *types.Info, currentPkg *types.Package) (site CallSite, err error) {
	var fun ast.Expr
	var pos token.Position

	fun = call.Fun
	pos = fset.Position(call.Pos())

	site = CallSite{
		File: pos.Filename,
		Line: pos.Line,
	}

	// ---------------------------------------------
	// Helper: Determine Identifier for Lookup
	// ---------------------------------------------
	var callIdent *ast.Ident

	switch t := fun.(type) {
	case *ast.Ident:
		callIdent = t
	case *ast.SelectorExpr:
		callIdent = t.Sel
	case *ast.IndexExpr: // Explicit generic: Func[T](...)
		if ident, ok := t.X.(*ast.Ident); ok {
			callIdent = ident
		} else if sel, ok := t.X.(*ast.SelectorExpr); ok {
			callIdent = sel.Sel
		}
	case *ast.IndexListExpr: // Explicit generic multi-param: Func[T, U](...)
		if ident, ok := t.X.(*ast.Ident); ok {
			callIdent = ident
		} else if sel, ok := t.X.(*ast.SelectorExpr); ok {
			callIdent = sel.Sel
		}
	default:
		// Indirect/Closure call
		site.Func = "indirect"
		site.Type = CallTypeDynamicPolymorphic
		site.Origin = OriginLocal
		return
	}

	// ---------------------------------------------
	// Resolution
	// ---------------------------------------------
	if callIdent == nil {
		// Should have been caught by default case, but safety first
		site.Func = "unknown"
		site.Type = CallTypeDynamicPolymorphic
		return
	}

	// Look up the object
	obj := info.Uses[callIdent]
	if obj == nil {
		// Builtins (len, make, etc.)
		site.Func = callIdent.Name
		site.Type = CallTypeMonomorphic
		site.Origin = OriginStdLib
		return
	}

	site.Func = obj.Name()
	site.Origin = inst.determineOrigin(obj.Pkg(), currentPkg)

	// Check 1: Is it a variable of type func? (Dynamic)
	if _, isFunc := obj.(*types.Func); !isFunc {
		site.Type = CallTypeDynamicPolymorphic
		return
	}

	// Check 2: Method Call via Interface?
	if selExpr, ok := fun.(*ast.SelectorExpr); ok {
		if sel, ok := info.Selections[selExpr]; ok {
			if types.IsInterface(sel.Recv()) {
				site.Type = CallTypeDynamicPolymorphic
				return
			}
		}
	}

	// Check 3: Is it a Generic Instantiation? (Static Polymorphic)
	// info.Instances contains data if the call uses Type Parameters (explicit or inferred)
	if instance, isInstance := info.Instances[callIdent]; isInstance {
		site.Type = CallTypeStaticPolymorphic
		site.StaticSubtype = inst.classifyTypeArgs(instance.TypeArgs)
		return
	}

	// Check 4: Is the receiver generic? (Method on generic type)
	// e.g., func (t T) Method()
	// This is also static polymorphism (dictionary dispatch on receiver).
	if selExpr, ok := fun.(*ast.SelectorExpr); ok {
		if sel, ok := info.Selections[selExpr]; ok {
			if inst.isGenericType(sel.Recv()) {
				site.Type = CallTypeStaticPolymorphic
				// For receivers, we treat it as Generic/Dictionary usually,
				// unless we analyzed the receiver's instantiation type deeply.
				// For simplicity, we flag as Generic (Dictionary) unless we inspect the variable's type.
				site.StaticSubtype = StaticPolyGeneric
				return
			}
		}
	}

	// Otherwise: Standard Monomorphic
	site.Type = CallTypeMonomorphic
	return
}

// classifyTypeArgs checks if all type arguments are "Optimized" (Primitives, etc.)
func (inst *AnalyzerService) classifyTypeArgs(args *types.TypeList) StaticPolySubtypeE {
	n := args.Len()
	for i := 0; i < n; i++ {
		t := args.At(i)
		if !inst.isOptimizedType(t) {
			// If ANY arg is complex/pointer/interface, the whole call likely uses dictionary/shape
			return StaticPolyGeneric
		}
	}
	return StaticPolyOptimized
}

// isOptimizedType checks if the type is Primitive, String, Slice of Primitive, or Function.
func (inst *AnalyzerService) isOptimizedType(t types.Type) bool {
	// Unwrap names (e.g., type MyInt int)
	if named, ok := t.(*types.Named); ok {
		t = named.Underlying()
	}

	switch u := t.(type) {
	case *types.Basic:
		// Primitives + String
		// Bool, Int..., Uint..., Float..., Complex..., String
		// UnsafePointer is usually BasicKind too, but let's stick to safe primitives + string
		kind := u.Kind()
		if kind == types.String || (kind >= types.Bool && kind <= types.Complex128) {
			return true
		}
		return false

	case *types.Slice:
		// Recursive check: Slice of Primitive
		return inst.isOptimizedType(u.Elem())

	case *types.Signature:
		// User specifically listed "functions" as allowed optimized types.
		return true

	default:
		// Pointers (*T), Structs, Interfaces, Maps, Channels, Arrays (unless primitive?)
		return false
	}
}

func (inst *AnalyzerService) isGenericType(tp types.Type) bool {
	if ptr, ok := tp.(*types.Pointer); ok {
		tp = ptr.Elem()
	}
	if _, ok := tp.(*types.TypeParam); ok {
		return true
	}
	if named, ok := tp.(*types.Named); ok {
		if named.TypeArgs().Len() > 0 {
			return true
		}
	}
	return false
}

func (inst *AnalyzerService) determineOrigin(pkg *types.Package, current *types.Package) OriginE {
	if pkg == nil {
		return OriginStdLib
	}
	if pkg.Path() == current.Path() {
		return OriginLocal
	}
	if strings.Contains(pkg.Path(), ".") {
		return Origin3rdParty
	}
	return OriginStdLib
}
