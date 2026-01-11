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
	File   string
	Line   int
	Func   string
	Type   CallTypeE
	Origin OriginE
	Code   string
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

		// Convention: Scoping block for config
		{
			cfg = &packages.Config{
				Mode:    packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
				Context: ctx,
			}

			// FIX: Check if the pattern is a directory.
			// If it is, run the 'go' command from inside that directory to support
			// external modules and absolute paths.
			// Use os.Stat to check if it's a directory.
			if info, errStat := os.Stat(inst.Pattern); errStat == nil && info.IsDir() {
				cfg.Dir = inst.Pattern
				loadPattern = "." // Analyze the root of that directory
			} else {
				loadPattern = inst.Pattern
			}
		}

		pkgs, err = packages.Load(cfg, loadPattern)
		if err != nil {
			yield(CallSite{}, eh.Errorf("failed to load packages: %w", err))
			return
		}

		// Convention: Iterate packages
		for pkg, errIter := range inst.iteratePackages(pkgs) {
			if errIter != nil {
				if !yield(CallSite{}, eh.Errorf("package load error: %w", errIter)) {
					return
				}
				continue
			}

			// Convention: Iterate Call Sites inside the package
			for site, errAnalysis := range inst.scanPackage(pkg) {
				if errAnalysis != nil {
					if !yield(CallSite{}, eh.Errorf("analysis error: %w", errAnalysis)) {
						return
					}
					continue
				}

				if !yield(site, nil) {
					return
				}
			}
		}
	}
}

// iteratePackages yields valid packages.
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

// scanPackage yields every call site found in the package body.
func (inst *AnalyzerService) scanPackage(pkg *packages.Package) iter.Seq2[CallSite, error] {
	return func(yield func(CallSite, error) bool) {
		var fset *token.FileSet
		var info *types.Info

		fset = pkg.Fset
		info = pkg.TypesInfo

		for _, file := range pkg.Syntax {
			// Traverse AST
			ast.Inspect(file, func(n ast.Node) bool {
				if n == nil {
					return true
				}

				if call, ok := n.(*ast.CallExpr); ok {
					var site CallSite
					var errAnalyze error

					site, errAnalyze = inst.analyzeCall(call, fset, info, pkg.Types)
					if errAnalyze != nil {
						// In strict analysis, we might want to yield this error.
						// For this tool, we assume some nodes are not analyzable,
						// but strictly we should check.
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

	// Case 1: Method Call (x.Method())
	if selExpr, ok := fun.(*ast.SelectorExpr); ok {
		var selection *types.Selection
		var okSel bool

		// Retrieve the Selection object from type info
		selection, okSel = info.Selections[selExpr]
		if okSel {
			site.Func = selection.Obj().Name()
			site.Origin = inst.determineOrigin(selection.Obj().Pkg(), currentPkg)

			// Check if the receiver (the 'x' in x.Method) is an interface.
			// If it is, the dispatch happens at runtime (itab lookup).
			if types.IsInterface(selection.Recv()) {
				site.Type = CallTypeDynamicPolymorphic
				return
			}

			// Check for Generic Types (Statically Polymorphic)
			// e.g. Receiver is a Type Param or instantiated generic type
			if inst.isGenericType(selection.Recv()) {
				site.Type = CallTypeStaticPolymorphic
				return
			}

			// Otherwise, it's a concrete struct method -> Monomorphic
			site.Type = CallTypeMonomorphic
			return
		}
	}

	// Case 2: Function Call (Function(), funcVar(), or GenericFunc())
	var ident *ast.Ident
	switch t := fun.(type) {
	case *ast.Ident:
		ident = t
	case *ast.SelectorExpr:
		ident = t.Sel
	default:
		// Indirect calls: (func(){})(), slice[0](), etc.
		site.Func = "indirect"
		site.Type = CallTypeDynamicPolymorphic
		site.Origin = OriginLocal
		return
	}

	var obj types.Object
	obj = info.Uses[ident]

	if obj == nil {
		// Builtins (len, cap, etc.) are Monomorphic
		site.Func = ident.Name
		site.Type = CallTypeMonomorphic
		site.Origin = OriginStdLib
		return
	}

	site.Func = obj.Name()
	site.Origin = inst.determineOrigin(obj.Pkg(), currentPkg)

	// If the object is not a function (e.g., it's a var of type func), it's Dynamic.
	if _, isFunc := obj.(*types.Func); !isFunc {
		site.Type = CallTypeDynamicPolymorphic
		return
	}

	// If the function has Type Parameters, it is Statically Polymorphic.
	if inst.isGenericFunc(obj) {
		site.Type = CallTypeStaticPolymorphic
		return
	}

	// Standard function call
	site.Type = CallTypeMonomorphic
	return
}

func (inst *AnalyzerService) isGenericFunc(obj types.Object) bool {
	var sig *types.Signature
	var ok bool
	sig, ok = obj.Type().(*types.Signature)
	if !ok {
		return false
	}
	return sig.TypeParams().Len() > 0
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

//func main() {
//	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
//
//	cmd := &cli.Command{
//		Name:  "go-call-check",
//		Usage: "Categorizes all call sites (Mono/StaticPoly/DynamicPoly)",
//		Action: func(ctx context.Context, cmd *cli.Command) (err error) {
//			var pattern string
//			if cmd.NArg() > 0 {
//				pattern = cmd.Args().Get(0)
//			} else {
//				pattern = "."
//			}
//
//			svc := &AnalyzerService{Pattern: pattern}
//
//			// Convention: Iterate the results and handle logging here (The Consumer)
//			for site, errIter := range svc.Run(ctx) {
//				if errIter != nil {
//					// We log the error but continue if possible, or abort depending on severity.
//					// For this CLI, we log and continue.
//					log.Error().Err(errIter).Msg("analysis error")
//					continue
//				}
//
//				// Logging Logic moved here
//				var event *zerolog.Event
//				if site.Type == CallTypeDynamicPolymorphic {
//					event = log.Warn()
//				} else {
//					event = log.Info()
//				}
//
//				event.
//					Str("file", site.File).
//					Int("line", site.Line).
//					Str("callee", site.Func).
//					Str("type", site.Type.String()).
//					Str("origin", site.Origin.String()).
//					Msg("call site detected")
//			}
//			return
//		},
//	}
//
//	if err := cmd.Run(context.Background(), os.Args); err != nil {
//		log.Fatal().Err(err).Msg("fatal error")
//	}
//}
