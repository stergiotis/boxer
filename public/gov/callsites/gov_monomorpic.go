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

// StaticPolySubtypeE refines the StaticPolymorphic case with granular types.
type StaticPolySubtypeE uint8

const (
	SubtypeNone StaticPolySubtypeE = 0

	// --- Stenciled / "Good" Cases ---
	SubtypeBasic      StaticPolySubtypeE = 1 // int, float, bool, complex
	SubtypeString     StaticPolySubtypeE = 2 // string
	SubtypeSliceBasic StaticPolySubtypeE = 3 // []int, []string, etc.
	SubtypeFunc       StaticPolySubtypeE = 4 // func() (User defined as "good")

	// --- Shape / Dictionary Cases ---
	SubtypePointer      StaticPolySubtypeE = 5  // *T
	SubtypeStruct       StaticPolySubtypeE = 6  // struct { ... }
	SubtypeInterface    StaticPolySubtypeE = 7  // interface { ... }
	SubtypeMap          StaticPolySubtypeE = 8  // map[K]V
	SubtypeChan         StaticPolySubtypeE = 9  // chan T
	SubtypeArray        StaticPolySubtypeE = 10 // [N]T
	SubtypeSliceGeneric StaticPolySubtypeE = 11 // []interface{}, []*T
	SubtypeTypeParam    StaticPolySubtypeE = 12 // T (Unresolved inside generic func)
)

func (inst StaticPolySubtypeE) String() string {
	switch inst {
	case SubtypeBasic:
		return "Basic"
	case SubtypeString:
		return "String"
	case SubtypeSliceBasic:
		return "SliceBasic"
	case SubtypeFunc:
		return "Func"
	case SubtypePointer:
		return "Pointer"
	case SubtypeStruct:
		return "Struct"
	case SubtypeInterface:
		return "Interface"
	case SubtypeMap:
		return "Map"
	case SubtypeChan:
		return "Chan"
	case SubtypeArray:
		return "Array"
	case SubtypeSliceGeneric:
		return "SliceGeneric"
	case SubtypeTypeParam:
		return "TypeParam"
	default:
		return "Unknown"
	}
}

type CallSite struct {
	File   string
	Line   int
	Func   string
	Type   CallTypeE
	Origin OriginE

	// TypeArgs contains the classification of every type argument.
	// Populated only if Type == CallTypeStaticPolymorphic.
	TypeArgs []StaticPolySubtypeE
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

	// 1. Identify the Call Identifier
	var callIdent *ast.Ident
	switch t := fun.(type) {
	case *ast.Ident:
		callIdent = t
	case *ast.SelectorExpr:
		callIdent = t.Sel
	case *ast.IndexExpr: // Func[T]
		if ident, ok := t.X.(*ast.Ident); ok {
			callIdent = ident
		} else if sel, ok := t.X.(*ast.SelectorExpr); ok {
			callIdent = sel.Sel
		}
	case *ast.IndexListExpr: // Func[A, B]
		if ident, ok := t.X.(*ast.Ident); ok {
			callIdent = ident
		} else if sel, ok := t.X.(*ast.SelectorExpr); ok {
			callIdent = sel.Sel
		}
	default:
		site.Func = "indirect"
		site.Type = CallTypeDynamicPolymorphic
		site.Origin = OriginLocal
		return
	}

	if callIdent == nil {
		site.Func = "unknown"
		site.Type = CallTypeDynamicPolymorphic
		return
	}

	// 2. Resolve Object
	obj := info.Uses[callIdent]
	if obj == nil {
		site.Func = callIdent.Name
		site.Type = CallTypeMonomorphic
		site.Origin = OriginStdLib
		return
	}

	site.Func = obj.Name()
	site.Origin = inst.determineOrigin(obj.Pkg(), currentPkg)

	// Check: Dynamic (Func Variable)
	if _, isFunc := obj.(*types.Func); !isFunc {
		site.Type = CallTypeDynamicPolymorphic
		return
	}

	// Check: Dynamic (Interface Method)
	if selExpr, ok := fun.(*ast.SelectorExpr); ok {
		if sel, ok := info.Selections[selExpr]; ok {
			if types.IsInterface(sel.Recv()) {
				site.Type = CallTypeDynamicPolymorphic
				return
			}
		}
	}

	// 3. Check for Static Polymorphism (Generics)

	// Case A: Generic Function Instantiation (Explicit or Inferred)
	// info.Instances contains the TypeArgs for the call
	if instance, isInstance := info.Instances[callIdent]; isInstance {
		site.Type = CallTypeStaticPolymorphic
		site.TypeArgs = inst.classifyTypeArgs(instance.TypeArgs)
		return
	}

	// Case B: Method on Generic Receiver
	// e.g. func (g G[T]) Method() called as g.Method()
	if selExpr, ok := fun.(*ast.SelectorExpr); ok {
		if sel, ok := info.Selections[selExpr]; ok {
			// We check if the receiver type is a Named type with type arguments
			if named, ok := sel.Recv().(*types.Named); ok && named.TypeArgs().Len() > 0 {
				site.Type = CallTypeStaticPolymorphic
				site.TypeArgs = inst.classifyTypeArgs(named.TypeArgs())
				return
			}
			// Special case: Receiver is a TypeParam itself (t.Method())
			if _, ok := sel.Recv().(*types.TypeParam); ok {
				site.Type = CallTypeStaticPolymorphic
				site.TypeArgs = []StaticPolySubtypeE{SubtypeTypeParam}
				return
			}
		}
	}

	site.Type = CallTypeMonomorphic
	return
}

// classifyTypeArgs maps a list of Go types to our granular Subtype enum.
func (inst *AnalyzerService) classifyTypeArgs(args *types.TypeList) []StaticPolySubtypeE {
	var out []StaticPolySubtypeE
	n := args.Len()

	out = make([]StaticPolySubtypeE, 0, n)

	for i := 0; i < n; i++ {
		t := args.At(i)
		out = append(out, inst.classifyType(t))
	}
	return out
}

// classifyType inspects a single type and returns its specific Subtype category.
func (inst *AnalyzerService) classifyType(t types.Type) StaticPolySubtypeE {
	// Unwrap Named types to get underlying structure for shape analysis
	// (unless it's a TypeParam)
	if _, isParam := t.(*types.TypeParam); isParam {
		return SubtypeTypeParam
	}
	if named, ok := t.(*types.Named); ok {
		t = named.Underlying()
	}

	switch u := t.(type) {
	case *types.Basic:
		kind := u.Kind()
		if kind == types.String {
			return SubtypeString
		}
		if kind >= types.Bool && kind <= types.Complex128 {
			return SubtypeBasic // Int, Float, Bool, Complex
		}
		// UnsafePointer etc. fall here, treated as Basic or Pointer?
		// UnsafePointer is kind=26. Let's treat as Pointer conceptually for safety.
		if kind == types.UnsafePointer {
			return SubtypePointer
		}
		return SubtypeBasic

	case *types.Slice:
		// Check element type for "Slice of Primitives" optimization
		elemSub := inst.classifyType(u.Elem())
		if elemSub == SubtypeBasic || elemSub == SubtypeString {
			return SubtypeSliceBasic
		}
		return SubtypeSliceGeneric

	case *types.Pointer:
		return SubtypePointer

	case *types.Struct:
		return SubtypeStruct

	case *types.Interface:
		return SubtypeInterface

	case *types.Map:
		return SubtypeMap

	case *types.Chan:
		return SubtypeChan

	case *types.Array:
		return SubtypeArray

	case *types.Signature:
		return SubtypeFunc

	case *types.TypeParam:
		return SubtypeTypeParam

	default:
		return SubtypeNone
	}
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
