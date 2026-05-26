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
		err = eb.Build().Str("input", inputPath).Errorf("marshallgen: parse file: %w", err)
		return
	}

	plan = &Plan{
		InputPath:   inputPath,
		PackageName: file.Name.Name,
	}

	var structType *ast.StructType
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
				err = eb.Build().Str("input", inputPath).Errorf("marshallgen: more than one struct type declared; only one DTO per file")
				return
			}
			plan.KindType = ts.Name.Name
			structType = st
		}
	}
	if structType == nil {
		err = eb.Build().Str("input", inputPath).Errorf("marshallgen: no struct type found in input")
		return
	}

	usedMemberships := map[string]string{}
	usedPlainCols := map[string]string{}

	for _, field := range structType.Fields.List {
		if field.Tag == nil {
			err = eb.Build().Str("input", inputPath).Errorf("marshallgen: untagged DTO field; every field must carry `lw:` or be the `_` entity-level field")
			return
		}
		tag := stripQuotes(field.Tag.Value)
		st := reflect.StructTag(tag)

		// `_` entity-level field — carries `kind:"<name>"` and/or a
		// `lw:"<membership>,<section>,const=<value>"` constant
		// declaration. Multiple `_` fields are allowed (one per kind
		// directive and one per constant declaration).
		if len(field.Names) == 1 && field.Names[0].Name == "_" {
			kindTag := st.Get("kind")
			if kindTag != "" {
				if plan.KindName != "" {
					err = eb.Build().Str("input", inputPath).Errorf("marshallgen: multiple `_` fields carry `kind:` — only one entity-level kind name allowed per DTO")
					return
				}
				plan.KindName = kindTag
			}
			if st.Get("plain") != "" {
				err = eb.Build().Str("input", inputPath).Errorf("marshallgen: `_` field's `plain:` map is retired — declare plain columns per-field via `lw:\",<col>\"` (e.g. `Id uint64 \\`lw:\",id\"\\``)")
				return
			}
			lwUnderscoreTag := st.Get("lw")
			if lwUnderscoreTag == "" {
				continue
			}
			// Constant declaration on `_` field.
			var pt ParsedLWTag
			pt, err = SplitLW(lwUnderscoreTag)
			if err != nil {
				err = eb.Build().Str("tag", lwUnderscoreTag).Errorf("marshallgen: parse `_` lw tag: %w", err)
				return
			}
			if !pt.Flags.HasConst {
				err = eb.Build().Str("tag", lwUnderscoreTag).Errorf("marshallgen: `_` field's lw: tag must declare `,const=<value>` — bare memberships belong on Go fields")
				return
			}
			if pt.Membership == "" {
				err = eb.Build().Str("tag", lwUnderscoreTag).Errorf("marshallgen: const declaration requires non-empty membership name")
				return
			}
			if pt.Section == "" {
				err = eb.Build().Str("tag", lwUnderscoreTag).Errorf("marshallgen: const declaration requires a section name")
				return
			}
			if pt.Column != "" {
				err = eb.Build().Str("tag", lwUnderscoreTag).Errorf("marshallgen: const declaration cannot target a sub-column")
				return
			}
			if pt.Flags.Explode {
				err = eb.Build().Str("tag", lwUnderscoreTag).Errorf("marshallgen: const declaration cannot combine with `explode`")
				return
			}
			plan.Fields = append(plan.Fields, TaggedField{
				GoFieldName:  "", // synthetic — no Go field
				GoType:       "string",
				LWMembership: pt.Membership,
				LWSection:    pt.Section,
				Flags:        pt.Flags,
				IsConst:      true,
				ConstValue:   pt.Flags.ConstValue,
			})
			continue
		}

		lwTag := st.Get("lw")
		if lwTag == "" {
			err = eb.Build().Str("input", inputPath).Str("field", fieldNamesString(field)).Errorf("marshallgen: non-`_` field missing `lw:` tag")
			return
		}
		var pt ParsedLWTag
		pt, err = SplitLW(lwTag)
		if err != nil {
			err = eb.Build().Str("tag", lwTag).Errorf("marshallgen: parse lw tag: %w", err)
			return
		}
		membership, section, column, flags := pt.Membership, pt.Section, pt.Column, pt.Flags

		if len(field.Names) != 1 {
			err = eb.Build().Str("input", inputPath).Errorf("marshallgen: multi-name or anonymous struct field forbidden — declare one field per line")
			return
		}
		goFieldName := field.Names[0].Name

		shape, parseErr := classifyType(field.Type)
		if parseErr != nil {
			err = eb.Build().Str("field", goFieldName).Errorf("marshallgen: classify field type: %w", parseErr)
			return
		}

		// Empty membership ⇒ plain row column. The section slot names
		// the fact-row column (id / ts / naturalKey / expiresAt).
		// Shape is constrained per-column; flags are not allowed
		// (plain columns have no BeginAttribute call to switch).
		if membership == "" {
			if section == "" {
				err = eb.Build().Str("tag", lwTag).Errorf("marshallgen: empty membership AND empty section — plain field needs `lw:\",<col>\"` (id/ts/naturalKey/expiresAt)")
				return
			}
			if column != "" {
				err = eb.Build().Str("tag", lwTag).Errorf("marshallgen: plain field cannot carry sub-column (`:<col>`)")
				return
			}
			if flags.Unit || flags.Explode || flags.Channel != MembershipChannelLowCardRef {
				err = eb.Build().Str("field", goFieldName).Errorf("marshallgen: plain field cannot carry channel / `unit` / `explode` flags (flags apply to tagged-value attributes only)")
				return
			}
			if shape.IsOption || shape.IsRoaring || shape.IsSlice {
				// `[]byte` is the one slice shape allowed (naturalKey),
				// recognised because classifyType returns
				// IsSlice=false GoType="[]byte" for top-level `[]byte`.
				err = eb.Build().Str("field", goFieldName).Errorf("marshallgen: plain field must be a scalar T (no Option / no slice / no roaring; top-level `[]byte` for naturalKey is allowed)")
				return
			}
			if prev, dup := usedPlainCols[section]; dup {
				err = eb.Build().Str("column", section).Str("first", prev).Str("second", goFieldName).Errorf("marshallgen: plain column declared on two DTO fields")
				return
			}
			usedPlainCols[section] = goFieldName
			err = ValidatePlainColumnShape(section, shape.GoType)
			if err != nil {
				err = eb.Build().Str("field", goFieldName).Errorf("marshallgen: %w", err)
				return
			}
			plan.PlainCols = append(plan.PlainCols, PlainCol{
				Column:  section,
				GoField: goFieldName,
				GoType:  shape.GoType,
			})
			continue
		}

		// Tagged-value field. Slice element allowlist is shape-level
		// only (per-element identity conversion in the emitted code);
		// schema-specific section compatibility is the Go compiler's
		// job at the BuildEntities call site.
		if shape.IsSlice {
			switch shape.GoType {
			case "string",
				"uint8", "uint16", "uint32", "uint64",
				"int8", "int16", "int32", "int64",
				"float32", "float64", "bool":
				// OK — identity-conversion primitives.
			case "[]byte":
				// OK — [][]byte. Section choice is author's; the
				// generated AddToContainer call only compiles against
				// a section whose value column accepts []byte.
			default:
				err = eb.Build().Str("field", goFieldName).Str("elemType", shape.GoType).Errorf("marshallgen: slice element type not yet supported")
				return
			}
		}

		// In-DTO uniqueness: (membership, sub-column) is the key.
		// Two fields can share a membership iff they target distinct
		// sub-columns of a multi-column section (u32Range with
		// beginIncl + endExcl).
		dupKey := membership
		if column != "" {
			dupKey = membership + ":" + column
		}
		if prev, dup := usedMemberships[dupKey]; dup {
			err = eb.Build().Str("membership", membership).Str("column", column).Str("first", prev).Str("second", goFieldName).Errorf("marshallgen: membership+column appears on two DTO fields")
			return
		}
		usedMemberships[dupKey] = goFieldName

		// Flag × shape consistency.
		isMulti := shape.IsSlice || shape.IsRoaring
		if flags.Explode && !isMulti {
			err = eb.Build().Str("field", goFieldName).Str("flag", "explode").Errorf("marshallgen: `explode` requires a multi-element shape (`[]T`, `*roaring.Bitmap`, `[][]byte`)")
			return
		}
		if flags.Unit && isMulti && !flags.Explode {
			err = eb.Build().Str("field", goFieldName).Str("flag", "unit").Errorf("marshallgen: `unit` on a multi-element shape requires `explode` (otherwise the default container shape has no per-element call to switch)")
			return
		}
		if flags.HasConst {
			err = eb.Build().Str("field", goFieldName).Errorf("marshallgen: `,const=<value>` only valid on `_` blank-identifier fields (carries no Go-side data)")
			return
		}

		plan.Fields = append(plan.Fields, TaggedField{
			GoFieldName:  goFieldName,
			GoType:       shape.GoType,
			IsOption:     shape.IsOption,
			IsSlice:      shape.IsSlice,
			IsRoaring:    shape.IsRoaring,
			LWMembership: membership,
			LWSection:    section,
			LWColumn:     column,
			Flags:        flags,
		})
	}

	if plan.KindName == "" {
		err = eb.Build().Str("input", inputPath).Errorf("marshallgen: DTO struct is missing the `_` entity-level field with `kind:\"…\"`")
		return
	}

	// Per-section membership-channel uniformity check: all fields
	// targeting the same section must agree on Channel (the read-side
	// dispatch iterates a per-section channel; mixed channels would
	// require two separate decode passes). Generalised by ADR-0008 D3
	// from the original "all Verbatim or all Ref" bool.
	bySection := map[string]MembershipChannel{}
	bySectionFirst := map[string]string{}
	for _, f := range plan.Fields {
		seen, ok := bySection[f.LWSection]
		if !ok {
			bySection[f.LWSection] = f.Flags.Channel
			bySectionFirst[f.LWSection] = f.GoFieldName
			continue
		}
		if seen != f.Flags.Channel {
			err = eb.Build().Str("section", f.LWSection).Str("field", f.GoFieldName).Str("firstField", bySectionFirst[f.LWSection]).Str("firstChannel", seen.String()).Str("secondChannel", f.Flags.Channel.String()).Errorf("marshallgen: section mixes membership channels — pick one channel per section")
			return
		}
	}
	if len(plan.PlainCols) == 0 {
		err = eb.Build().Str("input", inputPath).Errorf("marshallgen: DTO declares no plain columns; at least `Id uint64 `+\"`lw:\\\",id\\\"`\"+` is required")
		return
	}
	if _, ok := usedPlainCols["id"]; !ok {
		err = eb.Build().Str("input", inputPath).Errorf("marshallgen: DTO missing required plain column `id` (`lw:\",id\"`)")
		return
	}

	return
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

// fieldShape is the parser-internal classification of a DTO field's Go
// type. classifyType returns one of these instead of a wide return
// tuple.
type fieldShape struct {
	GoType    string // inner element type, source-form (e.g. "uint64", "time.Time", "[4]byte", "[]byte")
	IsOption  bool   // option.Option[T] wrapper
	IsSlice   bool   // []T element-slice (top-level)
	IsRoaring bool   // *roaring.Bitmap
}

// classifyType walks an AST type expression and reports its shape.
// Rejects forbidden shapes: `Option[[]T]` (except Option[[]byte]),
// `[]Option[T]`, arbitrary pointers (other than `*roaring.Bitmap`),
// and nested generics other than `option.Option`.
func classifyType(expr ast.Expr) (shape fieldShape, err error) {
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
			err = eb.Build().Errorf("only fixed-length byte arrays are supported (e.g. [4]byte, [16]byte)")
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
