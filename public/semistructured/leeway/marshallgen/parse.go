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
)

// ParsePlan parses the DTO source file at inputPath and returns the
// resolved Plan. The DTO must contain exactly one top-level struct
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
//   - Flag-vs-Go-shape consistency (see FieldFlags doc).
//   - Plan completeness (kind name present, at least one plain column).
//   - In-DTO uniqueness of (membership, sub-column) pairs.
//   - Forbidden Go shapes: Option[[]T] (except Option[[]byte]),
//     []Option[T], multi-name fields, non-roaring pointer types, plain
//     fields with non-scalar shapes.
func ParsePlan(inputPath string) (plan *Plan, err error) {
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
	// front-end (marshallreflect.buildPlan) via PlanBuilder; this loop
	// only handles the go/ast-specific concerns (tag extraction, the
	// multi-name/anonymous-field check, type classification).
	b := NewPlanBuilder(inputPath, file.Name.Name, kindType)

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

		var shape FieldShape
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

// ValidatePlainColumnShape enforces the per-column type constraints
// for the four fact-row plain columns (id / ts / naturalKey /
// expiresAt). Plain columns map to physical row columns, not
// tagged-value sections — their Go type is fixed by the runtime.facts
// schema, not chosen by the DTO author. Exported for the sibling
// marshallreflect package.
func ValidatePlainColumnShape(column, goType string) (err error) {
	switch column {
	case "id":
		if goType != "uint64" {
			err = eb.Build().Str("column", column).Str("goType", goType).Errorf("plain column `id` must be uint64")
		}
	case "ts", "expiresAt":
		if goType != "time.Time" && goType != "int64" {
			err = eb.Build().Str("column", column).Str("goType", goType).Errorf("plain column `%s` must be time.Time or int64 (nanos)", column)
		}
	case "naturalKey":
		if goType != "[]byte" && goType != "string" {
			err = eb.Build().Str("column", column).Str("goType", goType).Errorf("plain column `naturalKey` must be []byte or string")
		}
	default:
		err = eb.Build().Str("column", column).Errorf("unknown plain column (allowed: id, ts, naturalKey, expiresAt)")
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

// ParsedLWTag is the structured result of parsing an `lw:` tag value.
// Returned by SplitLW; consumed by ParsePlan and the sibling
// marshallreflect.buildPlan.
type ParsedLWTag struct {
	Membership string
	Section    string
	Column     string
	Flags      FieldFlags
}

// setChannelFlag installs the parsed channel on the in-progress flag
// set, rejecting two channel flags on one tag. Tokens like `,verbatim`
// and `,lowCardVerbatim` both map to MembershipChannelLowCardVerbatim
// (per ADR-0008 D3 SD9); attempting either after a different channel
// already set raises the same "declared twice" error.
func setChannelFlag(flags *FieldFlags, ch MembershipChannel, token string) (err error) {
	if flags.Channel != MembershipChannelLowCardRef {
		err = eb.Build().Str("flag", token).Str("alreadySet", flags.Channel.String()).Errorf("channel flag declared twice on one tag")
		return
	}
	flags.Channel = ch
	return
}

// SplitLW parses a value of the form
//
//	<membership>[,<section>[:<column>]][,<flag>][,<flag>…]
//
// into its components. Empty segments are tolerated and yield zero
// values; unknown flag tokens are an error. Exported so the sibling
// marshallreflect package can reuse the grammar without duplicating
// the parser.
func SplitLW(tag string) (out ParsedLWTag, err error) {
	parts := strings.Split(tag, ",")
	out.Membership = strings.TrimSpace(parts[0])
	if len(parts) >= 2 {
		s := strings.TrimSpace(parts[1])
		if colonIdx := strings.IndexByte(s, ':'); colonIdx >= 0 {
			out.Section = s[:colonIdx]
			out.Column = s[colonIdx+1:]
		} else {
			out.Section = s
		}
	}
	if len(parts) < 3 {
		return
	}
	for _, raw := range parts[2:] {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		// Key=value flags (currently only `const=<value>`).
		if eq := strings.IndexByte(token, '='); eq > 0 {
			key := token[:eq]
			val := token[eq+1:]
			switch key {
			case "const":
				if out.Flags.HasConst {
					err = eb.Build().Str("flag", key).Errorf("flag declared twice")
					return
				}
				out.Flags.HasConst = true
				out.Flags.ConstValue = val
			default:
				err = eb.Build().Str("flag", key).Errorf("unknown key=value flag (recognised: const=<value>)")
				return
			}
			continue
		}
		switch token {
		case "unit":
			if out.Flags.Unit {
				err = eb.Build().Str("flag", token).Errorf("flag declared twice")
				return
			}
			out.Flags.Unit = true
		case "explode":
			if out.Flags.Explode {
				err = eb.Build().Str("flag", token).Errorf("flag declared twice")
				return
			}
			out.Flags.Explode = true
		case "verbatim", "lowCardVerbatim":
			// `,verbatim` retained as alias for `,lowCardVerbatim` per
			// ADR-0008 D3 SD9 — existing DTOs compile unchanged.
			if err = setChannelFlag(&out.Flags, MembershipChannelLowCardVerbatim, token); err != nil {
				return
			}
		case "highCardRef":
			if err = setChannelFlag(&out.Flags, MembershipChannelHighCardRef, token); err != nil {
				return
			}
		case "highCardVerbatim":
			if err = setChannelFlag(&out.Flags, MembershipChannelHighCardVerbatim, token); err != nil {
				return
			}
		case "lowCardRefParametrized", "highCardRefParametrized", "mixedLowCardRef", "mixedLowCardVerbatim":
			// ADR-0008 D3 stages these four "complex" channels for a
			// follow-up commit — the parametrized/mixed shapes require
			// a two-field DTO pairing the section value with a sibling
			// carrier, which is non-trivial. Parse-time rejection so
			// DTO authors get a clear signal rather than misleading
			// emit-time failures.
			err = eb.Build().Str("flag", token).Errorf("lw: channel flag %q is recognised but not yet implemented — see ADR-0008 D3 staged-rollout note", token)
			return
		default:
			err = eb.Build().Str("flag", token).Errorf("unknown flag token (recognised: unit, explode, verbatim / lowCardVerbatim, highCardRef, highCardVerbatim, const=<value>)")
			return
		}
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
// shared FieldShape (consumed by PlanBuilder). Rejects forbidden shapes:
// `Option[[]T]` (except Option[[]byte]), `[]Option[T]`, arbitrary
// pointers (other than `*roaring.Bitmap`), and nested generics other
// than `option.Option`.
func classifyType(expr ast.Expr) (shape FieldShape, err error) {
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
			shape.GoType = "[]byte"
			return
		}
		shape.GoType, err = renderInner(idx.Index)
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
			shape.GoType = "[]byte"
			return
		}
		shape.IsSlice = true
		shape.GoType, err = renderInner(at.Elt)
		return
	}
	// *roaring.Bitmap — the only allowed pointer-typed field.
	if se, ok := expr.(*ast.StarExpr); ok {
		sel, sok := se.X.(*ast.SelectorExpr)
		if sok {
			pkg, pkgOk := sel.X.(*ast.Ident)
			if pkgOk && pkg.Name == "roaring" && sel.Sel.Name == "Bitmap" {
				shape.IsRoaring = true
				shape.GoType = "*roaring.Bitmap"
				return
			}
		}
		err = eb.Build().Errorf("pointer types forbidden except *roaring.Bitmap — use option.Option[T] for ZeroToOne fields")
		return
	}
	// Plain scalar (T, time.Time, fixed-length [N]byte).
	shape.GoType, err = renderInner(expr)
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
