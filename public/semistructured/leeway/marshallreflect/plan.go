//go:build llm_generated_opus47

package marshallreflect

import (
	"reflect"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallgen"
)

// PlanFor returns the marshallgen.Plan derived from T's struct tags.
// Cached per reflect.Type via sync.Map — call once per type per
// process is the same cost as building the plan once at codegen time.
func PlanFor[T any]() (plan *marshallgen.Plan, err error) {
	rt := reflect.TypeOf((*T)(nil)).Elem()
	plan, err = planForType(rt)
	return
}

var planCache sync.Map // map[reflect.Type]planEntry

type planEntry struct {
	plan *marshallgen.Plan
	err  error
}

func planForType(rt reflect.Type) (plan *marshallgen.Plan, err error) {
	if cached, ok := planCache.Load(rt); ok {
		e := cached.(planEntry)
		plan = e.plan
		err = e.err
		return
	}
	plan, err = buildPlan(rt)
	planCache.Store(rt, planEntry{plan: plan, err: err})
	return
}

// buildPlan mirrors marshallgen.ParsePlan's per-field handling but
// against reflect.StructField inputs instead of ast.Field. The output
// is a marshallgen.Plan that downstream Marshal / Unmarshal helpers
// drive the same way the marshallgen-emitted code does (via the
// shared TaggedField vocabulary).
func buildPlan(rt reflect.Type) (plan *marshallgen.Plan, err error) {
	if rt.Kind() != reflect.Struct {
		err = eb.Build().Str("type", rt.String()).Errorf("marshallreflect: DTO must be a struct type")
		return
	}
	plan = &marshallgen.Plan{
		InputPath:   rt.PkgPath() + "/" + rt.Name(),
		PackageName: pkgLastSegment(rt.PkgPath()),
		KindType:    rt.Name(),
	}

	usedPlainCols := map[string]string{}
	usedMemberships := map[string]string{}

	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		st := f.Tag

		// `_` blank-identifier — entity-level metadata + optional const
		// declarations. Multiple `_` fields allowed; one carries kind:.
		if f.Name == "_" {
			err = parseUnderscoreField(plan, st)
			if err != nil {
				return
			}
			continue
		}

		lwTag := st.Get("lw")
		if lwTag == "" {
			err = eb.Build().Str("field", f.Name).Errorf("marshallreflect: non-`_` field missing `lw:` tag")
			return
		}
		var pt marshallgen.ParsedLWTag
		pt, err = marshallgen.SplitLW(lwTag)
		if err != nil {
			err = eb.Build().Str("tag", lwTag).Errorf("marshallreflect: parse lw tag: %w", err)
			return
		}
		membership, section, column, flags := pt.Membership, pt.Section, pt.Column, pt.Flags

		shape, classErr := classifyReflectType(f.Type)
		if classErr != nil {
			err = eb.Build().Str("field", f.Name).Errorf("marshallreflect: classify field type: %w", classErr)
			return
		}

		// Empty membership → plain row column.
		if membership == "" {
			if section == "" {
				err = eb.Build().Str("tag", lwTag).Errorf("marshallreflect: empty membership AND empty section — plain field needs `lw:\",<col>\"`")
				return
			}
			if column != "" {
				err = eb.Build().Str("tag", lwTag).Errorf("marshallreflect: plain field cannot carry sub-column")
				return
			}
			if flags.Unit || flags.Explode || flags.Verbatim || flags.HasConst {
				err = eb.Build().Str("field", f.Name).Errorf("marshallreflect: plain field cannot carry `unit` / `explode` / `verbatim` / `const` flags")
				return
			}
			if shape.IsOption || shape.IsRoaring || shape.IsSlice {
				err = eb.Build().Str("field", f.Name).Errorf("marshallreflect: plain field must be a scalar T (top-level `[]byte` for naturalKey is allowed)")
				return
			}
			if prev, dup := usedPlainCols[section]; dup {
				err = eb.Build().Str("column", section).Str("first", prev).Str("second", f.Name).Errorf("marshallreflect: plain column declared on two DTO fields")
				return
			}
			usedPlainCols[section] = f.Name
			err = marshallgen.ValidatePlainColumnShape(section, shape.GoType)
			if err != nil {
				err = eb.Build().Str("field", f.Name).Errorf("marshallreflect: %w", err)
				return
			}
			plan.PlainCols = append(plan.PlainCols, marshallgen.PlainCol{
				Column:  section,
				GoField: f.Name,
				GoType:  shape.GoType,
			})
			continue
		}

		// Tagged-value field. Slice element allowlist (same as marshallgen).
		if shape.IsSlice {
			switch shape.GoType {
			case "string",
				"uint8", "uint16", "uint32", "uint64",
				"int8", "int16", "int32", "int64",
				"float32", "float64", "bool",
				"[]byte":
				// OK
			default:
				err = eb.Build().Str("field", f.Name).Str("elemType", shape.GoType).Errorf("marshallreflect: slice element type not yet supported")
				return
			}
		}

		// In-DTO uniqueness: (membership, sub-column).
		dupKey := membership
		if column != "" {
			dupKey = membership + ":" + column
		}
		if prev, dup := usedMemberships[dupKey]; dup {
			err = eb.Build().Str("membership", membership).Str("first", prev).Str("second", f.Name).Errorf("marshallreflect: membership+column appears on two DTO fields")
			return
		}
		usedMemberships[dupKey] = f.Name

		// Flag × shape consistency.
		isMulti := shape.IsSlice || shape.IsRoaring
		if flags.Explode && !isMulti {
			err = eb.Build().Str("field", f.Name).Errorf("marshallreflect: `explode` requires a multi-element shape")
			return
		}
		if flags.Unit && isMulti && !flags.Explode {
			err = eb.Build().Str("field", f.Name).Errorf("marshallreflect: `unit` on a multi-element shape requires `explode`")
			return
		}
		if flags.HasConst {
			err = eb.Build().Str("field", f.Name).Errorf("marshallreflect: `,const=<value>` only valid on `_` blank-identifier fields")
			return
		}

		plan.Fields = append(plan.Fields, marshallgen.TaggedField{
			GoFieldName:  f.Name,
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
		err = eb.Build().Str("type", rt.String()).Errorf("marshallreflect: DTO struct missing `_` field with `kind:\"…\"`")
		return
	}
	if len(plan.PlainCols) == 0 {
		err = eb.Build().Str("type", rt.String()).Errorf("marshallreflect: DTO declares no plain columns; at least `Id uint64 `+\"`lw:\\\",id\\\"`\"+` required")
		return
	}
	if _, ok := usedPlainCols["id"]; !ok {
		err = eb.Build().Str("type", rt.String()).Errorf("marshallreflect: DTO missing required plain column `id`")
		return
	}

	// Per-section verbatim uniformity.
	bySection := map[string]bool{}
	for _, fld := range plan.Fields {
		seen, ok := bySection[fld.LWSection]
		if !ok {
			bySection[fld.LWSection] = fld.Flags.Verbatim
			continue
		}
		if seen != fld.Flags.Verbatim {
			err = eb.Build().Str("section", fld.LWSection).Errorf("marshallreflect: section mixes `,verbatim` and ref-channel fields")
			return
		}
	}
	return
}

func parseUnderscoreField(plan *marshallgen.Plan, st reflect.StructTag) (err error) {
	kindTag := st.Get("kind")
	if kindTag != "" {
		if plan.KindName != "" {
			err = eb.Build().Errorf("marshallreflect: multiple `_` fields carry `kind:`")
			return
		}
		plan.KindName = kindTag
	}
	if st.Get("plain") != "" {
		err = eb.Build().Errorf("marshallreflect: `_` field's `plain:` map is retired — declare plain columns per-field via `lw:\",<col>\"`")
		return
	}
	lwTag := st.Get("lw")
	if lwTag == "" {
		return
	}
	// Constant declaration.
	pt, parseErr := marshallgen.SplitLW(lwTag)
	if parseErr != nil {
		err = eb.Build().Str("tag", lwTag).Errorf("marshallreflect: parse `_` lw tag: %w", parseErr)
		return
	}
	if !pt.Flags.HasConst {
		err = eb.Build().Str("tag", lwTag).Errorf("marshallreflect: `_` field's lw: tag must declare `,const=<value>`")
		return
	}
	if pt.Membership == "" {
		err = eb.Build().Str("tag", lwTag).Errorf("marshallreflect: const declaration requires non-empty membership name")
		return
	}
	if pt.Section == "" {
		err = eb.Build().Str("tag", lwTag).Errorf("marshallreflect: const declaration requires a section name")
		return
	}
	if pt.Column != "" {
		err = eb.Build().Str("tag", lwTag).Errorf("marshallreflect: const declaration cannot target a sub-column")
		return
	}
	if pt.Flags.Explode {
		err = eb.Build().Str("tag", lwTag).Errorf("marshallreflect: const declaration cannot combine with `explode`")
		return
	}
	plan.Fields = append(plan.Fields, marshallgen.TaggedField{
		GoFieldName:  "",
		GoType:       "string",
		LWMembership: pt.Membership,
		LWSection:    pt.Section,
		Flags:        pt.Flags,
		IsConst:      true,
		ConstValue:   pt.Flags.ConstValue,
	})
	return
}

func pkgLastSegment(pkg string) string {
	if pkg == "" {
		return "main"
	}
	for i := len(pkg) - 1; i >= 0; i-- {
		if pkg[i] == '/' {
			return pkg[i+1:]
		}
	}
	return pkg
}
