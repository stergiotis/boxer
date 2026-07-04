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
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
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
//   - Recognised flag tokens (`unit`, `explode`, channel flags, `const=`,
//     `ct=<canonical>`).
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

	// Collect every top-level struct type. One is the DTO; the others may
	// only be tuple element structs referenced by the DTO's slice-of-struct
	// fields (ADR-0103) — anything else keeps the one-DTO-per-file error.
	structDecls := map[string]*ast.StructType{}
	var structOrder []string
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
			structDecls[ts.Name.Name] = st
			structOrder = append(structOrder, ts.Name.Name)
		}
	}
	if len(structOrder) == 0 {
		err = eb.Build().Str("input", inputPath).Errorf("no struct type found in input")
		return
	}
	var structType *ast.StructType
	var kindType string
	if len(structOrder) == 1 {
		kindType = structOrder[0]
		structType = structDecls[kindType]
	} else {
		// With several structs the DTO is the (single) one carrying the `_`
		// entity-level kind field; the rest must be tuple element types.
		for _, name := range structOrder {
			if !structHasKindField(structDecls[name]) {
				continue
			}
			if structType != nil {
				err = eb.Build().Str("input", inputPath).Errorf("more than one struct carries a `_` kind field; only one DTO per file")
				return
			}
			kindType = name
			structType = structDecls[name]
		}
		if structType == nil {
			err = eb.Build().Str("input", inputPath).Errorf("several structs declared but none carries the `_` kind field — with tuple element structs present, the DTO must declare `kind:`")
			return
		}
	}

	// Per-field validation + plan assembly is shared with the reflect
	// front-end (marshallreflect.buildPlan) via goplan.PlanBuilder; this loop
	// only handles the go/ast-specific concerns (tag extraction, the
	// multi-name/anonymous-field check, type classification).
	b := goplan.NewPlanBuilder(inputPath, file.Name.Name, kindType)
	usedTupleStructs := map[string]bool{}

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

		// Dynamic-membership tuple field (ADR-0103): `[]X` where X is a
		// struct declared in this file. The element struct's fields are
		// classified individually; validation lives in the shared builder.
		if elemName, elemStruct, isTuple := tupleElemStruct(field.Type, structDecls, kindType); isTuple {
			usedTupleStructs[elemName] = true
			var elems []goplan.TupleElem
			elems, err = buildAstTupleElems(elemStruct)
			if err != nil {
				err = eb.Build().Str("field", goFieldName).Str("elemStruct", elemName).Errorf("%w", err)
				return
			}
			if err = b.AddTupleSliceField(goFieldName, lwTag, elemName, elems); err != nil {
				return
			}
			continue
		}

		var shape goplan.FieldShape
		shape, err = classifyType(field.Type)
		if err != nil {
			err = eb.Build().Str("field", goFieldName).Errorf("classify field type: %w", err)
			return
		}

		if err = b.AddField(goFieldName, lwTag, shape); err != nil {
			return
		}
	}

	// Every non-DTO struct in the file must have been consumed as a tuple
	// element type — otherwise the file layout is ambiguous (the historical
	// one-DTO-per-file rule, relaxed exactly for tuple elements).
	for _, name := range structOrder {
		if name == kindType || usedTupleStructs[name] {
			continue
		}
		err = eb.Build().Str("input", inputPath).Str("struct", name).Errorf("struct is neither the DTO nor referenced as a tuple element type; only one DTO per file")
		return
	}

	return b.Finish()
}

// structHasKindField reports whether the struct declares a `_` field
// carrying a `kind:"…"` tag — the marker identifying the DTO struct when
// a file also declares tuple element structs.
func structHasKindField(st *ast.StructType) bool {
	for _, field := range st.Fields.List {
		if len(field.Names) != 1 || field.Names[0].Name != "_" || field.Tag == nil {
			continue
		}
		if reflect.StructTag(stripQuotes(field.Tag.Value)).Get("kind") != "" {
			return true
		}
	}
	return false
}

// tupleElemStruct reports whether expr is `[]X` for a struct X declared in
// the same file (excluding the DTO struct itself — a self-referential DTO
// slice is not a tuple).
func tupleElemStruct(expr ast.Expr, structDecls map[string]*ast.StructType, kindType string) (name string, st *ast.StructType, ok bool) {
	at, isArr := expr.(*ast.ArrayType)
	if !isArr || at.Len != nil {
		return
	}
	id, isIdent := at.Elt.(*ast.Ident)
	if !isIdent || id.Name == kindType {
		return
	}
	st, ok = structDecls[id.Name]
	name = id.Name
	return
}

// buildAstTupleElems walks a tuple element struct's fields and classifies
// each with the shared go/ast classifier; the validation rules live in
// goplan.PlanBuilder.AddTupleSliceField, shared with the reflect
// front-end.
func buildAstTupleElems(st *ast.StructType) (elems []goplan.TupleElem, err error) {
	for _, field := range st.Fields.List {
		if len(field.Names) != 1 {
			err = eb.Build().Errorf("multi-name or anonymous tuple element field forbidden — declare one field per line")
			return
		}
		name := field.Names[0].Name
		if name == "_" {
			err = eb.Build().Errorf("`_` fields are not supported inside a tuple element struct — entity metadata belongs on the DTO")
			return
		}
		if !ast.IsExported(name) {
			// The reflect front-end cannot read or set unexported fields, so
			// the go/ast front-end rejects them too (front-end parity).
			err = eb.Build().Str("elemField", name).Errorf("unexported tuple element field; tagged fields must be exported")
			return
		}
		if field.Tag == nil {
			err = eb.Build().Str("elemField", name).Errorf("tuple element field missing `lw:` tag")
			return
		}
		lw := reflect.StructTag(stripQuotes(field.Tag.Value)).Get("lw")
		if lw == "" {
			err = eb.Build().Str("elemField", name).Errorf("tuple element field missing `lw:` tag")
			return
		}
		var shape goplan.FieldShape
		shape, err = classifyType(field.Type)
		if err != nil {
			err = eb.Build().Str("elemField", name).Errorf("classify tuple element field type: %w", err)
			return
		}
		elems = append(elems, goplan.TupleElem{GoFieldName: name, LWTag: lw, Shape: shape})
	}
	return
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
// shared goplan.FieldShape (consumed by goplan.PlanBuilder).
// It is canonical-native: the shape's value type is a leeway Canonical
// (the Go-facing GoType / IsSlice / IsRoaring are derived from it by
// PlanBuilder). Rejects forbidden shapes: `Option[[]T]` (except
// Option[[]byte]), `[]Option[T]`, arbitrary pointers (other than
// `*roaring.Bitmap`), and nested generics other than `option.Option`.
func classifyType(expr ast.Expr) (shape goplan.FieldShape, err error) {
	// option.Option[T] at the top level (functional.option in boxer).
	if idx, ok := expr.(*ast.IndexExpr); ok {
		sel, sok := idx.X.(*ast.SelectorExpr)
		if !sok || sel.Sel.Name != "Option" {
			err = eb.Build().Errorf("only option.Option[T] generic wrapper is supported")
			return
		}
		shape.IsOption = true
		// Reject Option[[]T] — slice inside Option is forbidden except
		// Option[[]byte] (the ZeroToOne scalar blob lane). `[]uint8` is
		// the same Go type as `[]byte` and classifies identically (the
		// reflect front-end cannot see the spelling — ADR-0101 OQ2).
		if at, isArr := idx.Index.(*ast.ArrayType); isArr && at.Len == nil {
			id, isIdent := at.Elt.(*ast.Ident)
			if !isIdent || (id.Name != "byte" && id.Name != "uint8") {
				err = eb.Build().Errorf("option.Option[[]T] is forbidden — use []T for multi-element membership (option.Option[[]byte] is allowed as a scalar blob)")
				return
			}
			shape.Canonical, err = goplan.ScalarCanonicalForGoType("[]byte")
			return
		}
		var inner string
		inner, err = renderInner(idx.Index)
		if err != nil {
			return
		}
		shape.Canonical, err = goplan.ScalarCanonicalForGoType(inner)
		return
	}
	// []T at the top level.
	if at, ok := expr.(*ast.ArrayType); ok && at.Len == nil {
		// Reject []Option[T] — Option inside slice is forbidden.
		if _, isIdx := at.Elt.(*ast.IndexExpr); isIdx {
			err = eb.Build().Errorf("[]Option[T] is forbidden — Option[T] is only allowed as a scalar field")
			return
		}
		// Special case: top-level `[]byte` / `[]uint8` is a SCALAR
		// variable-length blob, not a slice of uint8 — the two spellings
		// are one Go type, and the reflect front-end cannot tell them
		// apart, so both classify to the blob lane in both front-ends
		// (the previous "[]uint8 spells the u8-array lane" textual
		// convention is retired — ADR-0101 OQ2). The u8 homogenous-array
		// lane is selected explicitly via `,ct=u8h`. `[][]byte` keeps
		// multi-blob semantics because the recursion only collapses to
		// `*ast.Ident` when the top level is exactly `[]byte`/`[]uint8`.
		if id, isIdent := at.Elt.(*ast.Ident); isIdent && (id.Name == "byte" || id.Name == "uint8") {
			shape.Canonical, err = goplan.ScalarCanonicalForGoType("[]byte")
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
		scalar, err = goplan.ScalarCanonicalForGoType(elem)
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
				shape.Canonical = canonicaltypes.PromoteScalarPrim(goplan.RoaringElemCanonical(), canonicaltypes.ScalarModifierSet)
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
	shape.Canonical, err = goplan.ScalarCanonicalForGoType(scalar)
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
