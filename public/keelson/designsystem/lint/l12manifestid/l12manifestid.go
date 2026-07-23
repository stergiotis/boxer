// Package l12manifestid implements lint rule L12 — flag `app.AppIdT`
// string literals (in `app.Manifest{Id: …}` composites and in typed
// const / var declarations) whose value does not match the enclosing
// package's import path, does not have it as a `/`-terminated prefix,
// and is not on the AllowedSpecialIds allowlist.
//
// Rule rationale: ADR-0026 §SD12 makes Manifest.Id a public-stability
// surface and equates it with the Go import path. Three downstream
// consumers rely on this:
//
//   - `keelson/security/capslock.packageForManifest` (check.go:182)
//     looks up the Id verbatim as a Go package path in the capslock
//     JSON report. Drift = silent loss of capslock cross-check
//     coverage (evaluateAll skips with `continue` on map miss).
//   - The `--launch` CLI surface and `windowhost.Open(id)` both accept
//     the Id literally; typos build clean and only surface at dispatch.
//   - Audit rows (factsstore MembRuntimeApp) reference the Id as the
//     stable cross-process identifier.
//
// AllowedSpecialIds exempts dotted runtime-service names (the ADR-0026
// broker fleet: capbroker, persist, fsbroker, clipboard, sysmetrics, …)
// that intentionally use NATS-aligned form rather than a Go import path.
//
// Suppress per call site with `// designlint:ignore=L12 (reason)` on
// the same or preceding line.
package l12manifestid

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/internal/ignoreann"
)

// AppPackagePath is the canonical import path of runtime/app. A `var`
// (not `const`) so analysistest fixtures can swap it for a stand-in
// path that lives under testdata/src/.
var AppPackagePath = "github.com/stergiotis/boxer/public/keelson/runtime/app"

// AllowedSpecialIds enumerates the AppIdT literals that intentionally
// diverge from "import path" form — runtime services that take a dotted
// NATS-aligned name. Extending this is a deliberate exception; document
// in the new entry's PR.
var AllowedSpecialIds = map[string]bool{
	"runtime.broker":           true,
	"runtime.persist":          true,
	"runtime.fs":               true,
	"runtime.chlocal":          true,
	"runtime.clipboard":        true,
	"runtime.sysmetrics":       true,
	"runtime.adhoc":            true,
	"runtime.windowhost":       true,
	"runtime.introspect.query": true,
	"runtime.introspect.topo":  true,
}

// Analyzer is the L12 default analyzer used by the designlint binary.
var Analyzer = &analysis.Analyzer{
	Name:     "l12manifestid",
	Doc:      "L12: app.AppIdT literal must match the enclosing package's import path (or have it as a /-terminated prefix), or be on AllowedSpecialIds (project_adr_0026_app_runtime).",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (result any, err error) {
	appIdType := lookupAppIdType(pass)
	if appIdType == nil {
		return
	}
	manifestType := lookupNamedType(pass, "Manifest")

	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	pkgPath := pass.Pkg.Path()
	ignoreByFile := make(map[*ast.File]*ignoreann.Index)
	suppressed := func(file *ast.File, pos token.Pos) (ok bool) {
		idx, found := ignoreByFile[file]
		if !found {
			idx = ignoreann.Build(pass.Fset, file)
			ignoreByFile[file] = idx
		}
		ok = idx.Suppressed(pos, "L12")
		return
	}

	nodeFilter := []ast.Node{(*ast.KeyValueExpr)(nil), (*ast.ValueSpec)(nil)}
	insp.WithStack(nodeFilter, func(n ast.Node, push bool, stack []ast.Node) (proceed bool) {
		proceed = true
		if !push {
			return
		}
		file := fileOf(stack)
		if isTestFile(pass.Fset, file) {
			return
		}
		switch v := n.(type) {
		case *ast.KeyValueExpr:
			// Narrow to `Manifest{Id: …}` and reject other AppIdT-valued
			// KeyValueExpr shapes such as `map[uint64]app.AppIdT{1: "…"}`,
			// where the literal deliberately points at *another* package's
			// Id (e.g., the carousel's legacyCodeToId cross-reference table).
			if !isManifestIdField(pass, v, stack, manifestType) {
				return
			}
			literal, val, ok := constString(pass, v.Value)
			if !ok || suppressed(file, literal.Pos()) {
				return
			}
			reportIfBad(pass, literal, val, pkgPath)
		case *ast.ValueSpec:
			if v.Type == nil {
				return
			}
			declType := pass.TypesInfo.TypeOf(v.Type)
			if declType == nil || !typeIs(declType, appIdType) {
				return
			}
			for _, expr := range v.Values {
				literal, val, ok := constString(pass, expr)
				if !ok || suppressed(file, literal.Pos()) {
					continue
				}
				reportIfBad(pass, literal, val, pkgPath)
			}
		}
		return
	})
	return
}

func reportIfBad(pass *analysis.Pass, node ast.Expr, val string, pkgPath string) {
	if AllowedSpecialIds[val] {
		return
	}
	if val == pkgPath {
		return
	}
	if strings.HasPrefix(val, pkgPath+"/") {
		return
	}
	pass.ReportRangef(node,
		"L12: AppIdT literal %q does not match package import path %q (and is not on AllowedSpecialIds); annotate with // designlint:ignore=L12 (reason) if intentional",
		val, pkgPath)
}

// constString unwraps `expr` to its constant string value when one exists.
// Handles bare BasicLits, AppIdT("…") conversions, and references to typed
// string constants. node is returned for diagnostic positioning.
func constString(pass *analysis.Pass, expr ast.Expr) (node ast.Expr, val string, ok bool) {
	tv, found := pass.TypesInfo.Types[expr]
	if !found || tv.Value == nil {
		return
	}
	if tv.Value.Kind() != constant.String {
		return
	}
	val = constant.StringVal(tv.Value)
	node = expr
	ok = true
	return
}

func lookupAppIdType(pass *analysis.Pass) (t *types.Named) {
	t = lookupNamedType(pass, "AppIdT")
	return
}

func lookupNamedType(pass *analysis.Pass, name string) (t *types.Named) {
	var pkg *types.Package
	for _, imp := range pass.Pkg.Imports() {
		if imp.Path() == AppPackagePath {
			pkg = imp
			break
		}
	}
	if pkg == nil && pass.Pkg.Path() == AppPackagePath {
		pkg = pass.Pkg
	}
	if pkg == nil {
		return
	}
	obj := pkg.Scope().Lookup(name)
	if obj == nil {
		return
	}
	named, ok := obj.Type().(*types.Named)
	if !ok {
		return
	}
	t = named
	return
}

// isManifestIdField reports whether kv is the `Id: …` field of a literal
// of type app.Manifest. Two conditions: the immediately enclosing
// CompositeLit's static type is the manifestType, AND the key is an
// identifier named "Id". The type-check is sufficient on its own (Manifest
// has exactly one AppIdT field) but the name guard documents intent and
// keeps future Manifest field additions from accidentally widening scope.
func isManifestIdField(pass *analysis.Pass, kv *ast.KeyValueExpr, stack []ast.Node, manifestType *types.Named) (ok bool) {
	if manifestType == nil {
		return
	}
	k, isIdent := kv.Key.(*ast.Ident)
	if !isIdent || k.Name != "Id" {
		return
	}
	for i := len(stack) - 1; i >= 0; i-- {
		cl, isCl := stack[i].(*ast.CompositeLit)
		if !isCl {
			continue
		}
		t := pass.TypesInfo.TypeOf(cl)
		if t == nil {
			return
		}
		ok = typeIs(unptr(t), manifestType)
		return
	}
	return
}

func unptr(t types.Type) (out types.Type) {
	out = t
	if ptr, isPtr := t.(*types.Pointer); isPtr {
		out = ptr.Elem()
	}
	return
}

func typeIs(a types.Type, b *types.Named) (ok bool) {
	named, isNamed := a.(*types.Named)
	if !isNamed {
		return
	}
	ok = named == b
	return
}

// isTestFile reports whether the source file is a `_test.go` source. Test
// code routinely uses ad-hoc literal Ids (`"test.app"`, `"a"`, `"play"`)
// to exercise registry / broker logic; forcing those to match the package
// path would be churn for no signal.
func isTestFile(fset *token.FileSet, file *ast.File) (ok bool) {
	if file == nil {
		return
	}
	tf := fset.File(file.Pos())
	if tf == nil {
		return
	}
	ok = strings.HasSuffix(tf.Name(), "_test.go")
	return
}

func fileOf(stack []ast.Node) (file *ast.File) {
	for i := len(stack) - 1; i >= 0; i-- {
		if f, isFile := stack[i].(*ast.File); isFile {
			file = f
			return
		}
	}
	return
}
