package propsfile

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/packageprops"
)

// This package owns the on-disk representation of a package_props.go
// declaration (ADR-0080) — parsing one, rendering one, and merging a survey's
// verdict into one. It is deliberately survey-agnostic: more than one survey
// contributes fields to the same file (the wasm survey owns the WASM* fields,
// the capability survey owns the Caps* fields, and Kind is human-curated), so
// the file machinery cannot live inside any one of them (ADR-0120 SD7).

const (
	// FileName is the per-package declaration file.
	FileName = "package_props.go"
	// ImportPath is the vocabulary package the declarations reference. A package
	// cannot declare props referencing itself, so it is excluded from surveys.
	ImportPath = "github.com/stergiotis/boxer/public/packageprops"
)

// FieldSet names the Props fields a survey computes, and therefore owns when it
// regenerates a declaration. Fields outside a survey's set are preserved from
// the existing file verbatim.
//
// This is what lets independent surveys share one file without clobbering each
// other, and it subsumes the older hand-written special case that preserved
// Kind across a wasm re-seed: preservation is now the default, and overwriting
// is the thing a survey must opt into.
type FieldSet uint8

const (
	FieldsWASM FieldSet = 1 << iota // WASMWASI, WASMJS, WASMFreestanding
	FieldsKind                      // Kind
	FieldsCaps                      // CapsDirect, CapsReachable
)

// Has reports whether f contains every field in want.
func (f FieldSet) Has(want FieldSet) (b bool) { return f&want == want }

// Merge returns base with only the owned fields replaced by computed's. Every
// other field of base survives untouched — including another survey's verdicts
// and curated values a survey has no oracle for.
func Merge(base, computed packageprops.Props, owned FieldSet) (out packageprops.Props) {
	out = base
	if owned.Has(FieldsWASM) {
		out.WASMWASI = computed.WASMWASI
		out.WASMJS = computed.WASMJS
		out.WASMFreestanding = computed.WASMFreestanding
	}
	if owned.Has(FieldsKind) {
		out.Kind = computed.Kind
	}
	if owned.Has(FieldsCaps) {
		out.CapsDirect = computed.CapsDirect
		out.CapsReachable = computed.CapsReachable
	}
	return
}

// Fields renders p as the `Name: value` elements of a packageprops.Props
// composite literal, in declaration order.
//
// Both the per-package declarations and the harvested static table are built
// from it, differing only in how they join the elements — so the two renderings
// cannot drift, which matters because a new Props field otherwise has to be
// remembered in two codegen sites.
//
// Fields are omitted at their zero value, so a declaration states only what a
// survey has actually established and the common case stays terse. The WASM*
// fields are the exception: they are always emitted, because they predate this
// rule and omitting them would rewrite every committed declaration for no gain.
func Fields(p packageprops.Props) (elems []string) {
	elems = append(elems,
		"WASMWASI: packageprops."+StateToken(p.WASMWASI),
		"WASMJS: packageprops."+StateToken(p.WASMJS),
		"WASMFreestanding: packageprops."+StateToken(p.WASMFreestanding),
	)
	if p.Kind != packageprops.KindUnspecified {
		elems = append(elems, "Kind: packageprops."+KindToken(p.Kind))
	}
	if p.CapsDirect != 0 {
		elems = append(elems, "CapsDirect: "+capsExpr(p.CapsDirect))
	}
	if p.CapsReachable != 0 {
		elems = append(elems, "CapsReachable: "+capsExpr(p.CapsReachable))
	}
	return
}

// Render emits the gofmt-clean source of a package's declaration.
func Render(pkgName, importPath string, p packageprops.Props) (src []byte, err error) {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkgName)
	fmt.Fprintf(&b, "import %q\n\n", ImportPath)
	b.WriteString("// PackageProps records this package's curated properties (ADR-0080).\n")
	b.WriteString("// Seeded by `wasmsurvey props generate` (WASM*) and `capsurvey generate`\n")
	b.WriteString("// (Caps*); curate by hand, then run the matching verify.\n")
	b.WriteString("var PackageProps = packageprops.Props{\n")
	for _, e := range Fields(p) {
		b.WriteString(e)
		b.WriteString(",\n")
	}
	b.WriteString("}\n\n")
	// Self-register from init so packageprops.All() enumerates this package when
	// it is linked into a binary (ADR-0080 registry surface).
	fmt.Fprintf(&b, "func init() { packageprops.Register(%q, PackageProps) }\n", importPath)
	src, err = format.Source([]byte(b.String()))
	if err != nil {
		err = eb.Build().Str("pkg", importPath).Errorf("format props file: %w", err)
	}
	return
}

// capsExpr renders a set as a packageprops.Caps(...) call. A numeric literal
// would be terser but unreadable and ungreppable; naming the capabilities keeps
// the declarations IDE-navigable and lets `grep -r CapabilityExec` answer which
// packages run external processes.
func capsExpr(s packageprops.CapabilitySet) (expr string) {
	cs := s.Capabilities()
	toks := make([]string, 0, len(cs))
	for _, c := range cs {
		toks = append(toks, "packageprops."+CapabilityToken(c))
	}
	return "packageprops.Caps(" + strings.Join(toks, ", ") + ")"
}

// Parse reads a declaration's PackageProps value. Fields absent from the file
// stay at their zero value, which asserts nothing.
//
// It parses the AST only — no build, no type check — so it works on a package
// that does not compile, and it matches on the name PackageProps without
// verifying the value's type.
func Parse(path string) (p packageprops.Props, err error) {
	fset := token.NewFileSet()
	var f *ast.File
	f, err = parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return
	}
	fields := propsFields(f)
	p.WASMWASI = ParseStateToken(identOf(fields["WASMWASI"]))
	p.WASMJS = ParseStateToken(identOf(fields["WASMJS"]))
	p.WASMFreestanding = ParseStateToken(identOf(fields["WASMFreestanding"]))
	p.Kind = ParseKindToken(identOf(fields["Kind"]))
	p.CapsDirect = parseCapsExpr(fields["CapsDirect"])
	p.CapsReachable = parseCapsExpr(fields["CapsReachable"])
	return
}

// propsFields extracts the PackageProps composite-literal field→expression map.
func propsFields(f *ast.File) (fields map[string]ast.Expr) {
	fields = make(map[string]ast.Expr, 6)
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
					if key, ok := kv.Key.(*ast.Ident); ok {
						fields[key.Name] = kv.Value
					}
				}
			}
		}
	}
	return
}

// identOf renders a packageprops.X selector (or a bare ident) as its trailing
// identifier ("WASMCompiles"). Anything else yields "", which every ParseXToken
// maps to the zero value.
func identOf(e ast.Expr) (tok string) {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return v.Sel.Name
	case *ast.Ident:
		return v.Name
	}
	return ""
}

// parseCapsExpr reads a packageprops.Caps(packageprops.CapabilityX, …) call
// back into a set. A shape it does not recognise yields the zero set, matching
// the token parsers' rule that an unreadable declaration asserts nothing rather
// than erroring.
func parseCapsExpr(e ast.Expr) (s packageprops.CapabilitySet) {
	call, ok := e.(*ast.CallExpr)
	if !ok || identOf(call.Fun) != "Caps" {
		return 0
	}
	for _, arg := range call.Args {
		if c, ok := ParseCapabilityToken(identOf(arg)); ok {
			s = s.With(c)
		}
	}
	return
}
