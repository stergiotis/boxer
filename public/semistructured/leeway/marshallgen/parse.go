//go:build llm_generated_opus47

package marshallgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// ParsePlan parses the DTO source file at inputPath and returns the
// resolved mappingplan.Plan. The DTO must contain exactly one top-level struct
// type declaration; that struct declares an `_` field carrying entity
// metadata (`kind:"<name>" plain:"<col=Field,…>"`) plus zero or more
// fields tagged with `lw:"<membership>[,<section>[:<column>]][,<flag>…]"`.
//
// ParsePlan does NOT consult any membership registry or schema
// descriptor. Membership-name typos, missing memberships, and
// section / Go-type incompatibilities surface at `go build` time of
// the generated wrapper output, where the FactsWrapper / NoOpWrapper
// reference `vdd.Memb<Name>` (facts) or compare against package-local
// consts (anchor), and where the generated BuildEntities /
// FillFromArrow are bound against a typed DML / RA at the call site.
//
// What ParsePlan does check:
//
//   - lw: tag grammar (comma layout, optional section, optional sub-
//     column, optional trailing flag tokens).
//   - Recognised flag tokens (`unit`, `explode`).
//   - Flag-vs-Go-shape consistency (see mappingplan.FieldFlags doc).
//   - mappingplan.Plan completeness (kind name present, at least one plain column).
//   - In-DTO uniqueness of (membership, sub-column) pairs.
//   - Forbidden Go shapes: Option[[]T] (except Option[[]byte]),
//     []Option[T], multi-name fields, non-roaring pointer types, plain
//     fields with non-scalar shapes.
func ParsePlan(inputPath string) (plan *mappingplan.Plan, err error) {
	fset := token.NewFileSet()
	var file *ast.File
	file, err = parser.ParseFile(fset, inputPath, nil, parser.ParseComments)
	if err != nil {
		err = eb.Build().Str("input", inputPath).Errorf("parse file: %w", err)
		return
	}

	var structType *ast.StructType
	var kindType string
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			if structType != nil {
				err = eb.Build().Str("input", inputPath).Errorf("more than one struct type declared; only one DTO per file")
				return
			}
			kindType = ts.Name.Name
			structType = st
		}
	}
	if structType == nil {
		err = eb.Build().Str("input", inputPath).Errorf("no struct type found in input")
		return
	}

	// Per-field validation + plan assembly is shared with the reflect
	// front-end (marshallreflect.buildPlan) via mappingplan.PlanBuilder; this loop
	// only handles the go/ast-specific concerns (tag extraction, the
	// multi-name/anonymous-field check, type classification).
	b := mappingplan.NewPlanBuilder(inputPath, file.Name.Name, kindType)

	for _, field := range structType.Fields.List {
		if field.Tag == nil {
			err = eb.Build().Str("input", inputPath).Errorf("untagged DTO field; every field must carry `lw:` or be the `_` entity-level field")
			return
		}
		st := reflect.StructTag(stripQuotes(field.Tag.Value))

		// `_` entity-level field — kind directive and/or a `,const=`
		// declaration; validated by the shared builder.
		if len(field.Names) == 1 && field.Names[0].Name == "_" {
			if err = b.AddUnderscoreField(st.Get("kind"), st.Get("plain"), st.Get("lw")); err != nil {
				return
			}
			continue
		}

		lwTag := st.Get("lw")
		if lwTag == "" {
			err = eb.Build().Str("input", inputPath).Str("field", fieldNamesString(field)).Errorf("non-`_` field missing `lw:` tag")
			return
		}
		// Multi-name / anonymous fields are a go/ast-only concern (reflect
		// fields always carry exactly one name).
		if len(field.Names) != 1 {
			err = eb.Build().Str("input", inputPath).Errorf("multi-name or anonymous struct field forbidden — declare one field per line")
			return
		}
		goFieldName := field.Names[0].Name

		var shape mappingplan.FieldShape
		shape, err = classifyType(field.Type)
		if err != nil {
			err = eb.Build().Str("field", goFieldName).Errorf("classify field type: %w", err)
			return
		}

		if err = b.AddField(goFieldName, lwTag, shape); err != nil {
			return
		}
	}

	return b.Finish()
}

func stripQuotes(s string) (out string) {
	out, err := strconv.Unquote(s)
	if err != nil {
		out = s
	}
	return
}

func fieldNamesString(field *ast.Field) (out string) {
	names := make([]string, 0, len(field.Names))
	for _, id := range field.Names {
		names = append(names, id.Name)
	}
	out = strings.Join(names, ",")
	return
}

// classifyType walks an AST type expression and reports its shape as a
// shared mappingplan.FieldShape (consumed by mappingplan.PlanBuilder).
// It is canonical-native: the shape's value type is a leeway Canonical
// (the Go-facing GoType / IsSlice / IsRoaring are derived from it by
// PlanBuilder). Rejects forbidden shapes: `Option[[]T]` (except
// Option[[]byte]), `[]Option[T]`, arbitrary pointers (other than
// `*roaring.Bitmap`), and nested generics other than `option.Option`.
func classifyType(expr ast.Expr) (shape mappingplan.FieldShape, err error) {
	// option.Option[T] at the top level (functional.option in boxer).
	if idx, ok := expr.(*ast.IndexExpr); ok {
		sel, sok := idx.X.(*ast.SelectorExpr)
		if !sok || sel.Sel.Name != "Option" {
			err = eb.Build().Errorf("only option.Option[T] generic wrapper is supported")
			return
		}
		shape.IsOption = true
		// Reject Option[[]T] — slice inside Option is forbidden except
		// Option[[]byte] (the ZeroToOne scalar blob lane). `byte` is
		// the blob spelling per boxer's coding standard; sized-integer
		// arrays spell themselves `[]uint8` and stay in the multi-
		// element lane.
		if at, isArr := idx.Index.(*ast.ArrayType); isArr && at.Len == nil {
			id, isIdent := at.Elt.(*ast.Ident)
			if !isIdent || id.Name != "byte" {
				err = eb.Build().Errorf("option.Option[[]T] is forbidden — use []T for multi-element membership (option.Option[[]byte] is allowed as a scalar blob)")
				return
			}
			shape.Canonical, err = mappingplan.ScalarCanonicalForGoType("[]byte")
			return
		}
		var inner string
		inner, err = renderInner(idx.Index)
		if err != nil {
			return
		}
		shape.Canonical, err = mappingplan.ScalarCanonicalForGoType(inner)
		return
	}
	// []T at the top level.
	if at, ok := expr.(*ast.ArrayType); ok && at.Len == nil {
		// Reject []Option[T] — Option inside slice is forbidden.
		if _, isIdx := at.Elt.(*ast.IndexExpr); isIdx {
			err = eb.Build().Errorf("[]Option[T] is forbidden — Option[T] is only allowed as a scalar field")
			return
		}
		// Special case: top-level `[]byte` is a SCALAR variable-length
		// blob, not a slice of uint8. The `byte` spelling reserves
		// itself for blobs; `[]uint8` stays in the multi-element u8
		// lane. `[][]byte` keeps multi-blob semantics because the
		// recursion only collapses to `*ast.Ident{byte}` when the top
		// level is exactly `[]byte`.
		if id, isIdent := at.Elt.(*ast.Ident); isIdent && id.Name == "byte" {
			shape.Canonical, err = mappingplan.ScalarCanonicalForGoType("[]byte")
			return
		}
		// []marshalltypes.X — a slice carrier, paired element-wise with an
		// exploded value field (one carrier per emitted attribute). Recognised
		// by the slice element being a marshalltypes selector, like the scalar
		// carrier branch below; PlanBuilder pairs it and checks the value is
		// `,explode`.
		if sel, isSel := at.Elt.(*ast.SelectorExpr); isSel {
			if pkg, pkgOk := sel.X.(*ast.Ident); pkgOk && pkg.Name == "marshalltypes" {
				shape.CarrierType = sel.Sel.Name
				shape.CarrierIsSlice = true
				return
			}
		}
		// A homogenous-array membership: classify the element to a scalar
		// canonical, then promote it with the HomogenousArray modifier.
		var elem string
		elem, err = renderInner(at.Elt)
		if err != nil {
			return
		}
		var scalar canonicaltypes.PrimitiveAstNodeI
		scalar, err = mappingplan.ScalarCanonicalForGoType(elem)
		if err != nil {
			return
		}
		shape.Canonical = canonicaltypes.PromoteScalarPrim(scalar, canonicaltypes.ScalarModifierHomogenousArray)
		return
	}
	// *roaring.Bitmap — the only allowed pointer-typed field. A roaring
	// bitmap is a Set of uint32 in the canonical model.
	if se, ok := expr.(*ast.StarExpr); ok {
		sel, sok := se.X.(*ast.SelectorExpr)
		if sok {
			pkg, pkgOk := sel.X.(*ast.Ident)
			if pkgOk && pkg.Name == "roaring" && sel.Sel.Name == "Bitmap" {
				shape.Canonical = canonicaltypes.PromoteScalarPrim(mappingplan.RoaringElemCanonical(), canonicaltypes.ScalarModifierSet)
				return
			}
		}
		err = eb.Build().Errorf("pointer types forbidden except *roaring.Bitmap — use option.Option[T] for ZeroToOne fields")
		return
	}
	// marshalltypes carrier (Cut-2) — a selector into the marshalltypes
	// package (e.g. marshalltypes.MixedLowCardRef). Recognised by the
	// source-level package identifier, like roaring / option above;
	// PlanBuilder pairs it with its value sibling.
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		if pkg, pkgOk := sel.X.(*ast.Ident); pkgOk && pkg.Name == "marshalltypes" {
			shape.CarrierType = sel.Sel.Name
			return
		}
	}
	// Plain scalar (T, time.Time, fixed-length [N]byte).
	var scalar string
	scalar, err = renderInner(expr)
	if err != nil {
		return
	}
	shape.Canonical, err = mappingplan.ScalarCanonicalForGoType(scalar)
	return
}

// renderInner produces the source-form of a plain (non-Option) Go type
// expression. Recognised: identifiers (`uint64`, `bool`, …), selectors
// (`time.Time`), fixed-size byte arrays (`[N]byte`), and the inner
// `[]byte` element used by the `[][]byte` recursion. Other shapes are
// forbidden.
func renderInner(expr ast.Expr) (s string, err error) {
	switch v := expr.(type) {
	case *ast.Ident:
		s = v.Name
	case *ast.SelectorExpr:
		x, ok := v.X.(*ast.Ident)
		if !ok {
			err = eb.Build().Errorf("nested selector type not supported")
			return
		}
		s = x.Name + "." + v.Sel.Name
	case *ast.ArrayType:
		if v.Len == nil {
			elt, ok := v.Elt.(*ast.Ident)
			if !ok || elt.Name != "byte" {
				err = eb.Build().Errorf("only []byte slices and fixed-length [N]byte arrays are supported")
				return
			}
			s = "[]byte"
			return
		}
		bl, ok := v.Len.(*ast.BasicLit)
		if !ok || bl.Kind != token.INT {
			err = eb.Build().Errorf("only fixed-length `[N]byte` arrays are supported (e.g. [4]byte, [16]byte)")
			return
		}
		elt, ok := v.Elt.(*ast.Ident)
		if !ok || elt.Name != "byte" {
			err = eb.Build().Errorf("only byte arrays are supported as fixed-length payloads")
			return
		}
		s = "[" + bl.Value + "]byte"
	case *ast.StarExpr:
		err = eb.Build().Errorf("pointer types forbidden — use option.Option[T] for ZeroToOne fields")
		return
	default:
		err = eb.Build().Errorf("unsupported type expression %T", expr)
		return
	}
	return
}
