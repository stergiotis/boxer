package marshallgen

import (
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
)

// The SoA <Kind>Columns storage, its AoS Append / Row adapters, and the
// plain-column (entity-header) helpers both the write and read emitters use.

func writeColumnsStruct(sb *strings.Builder, plan *mappingplan.Plan) {
	line(sb, 0, "// --- SoA columns + AoS Append adapter. ---")
	blank(sb)
	linef(sb, 0, "// %sColumns is the SoA storage for batches of %s rows.", plan.KindType, plan.KindType)
	line(sb, 0, "// All slices grow in lockstep — Len returns the row count.")
	linef(sb, 0, "type %sColumns struct {", plan.KindType)
	for _, p := range plan.PlainCols {
		linef(sb, 1, "%s []%s", p.GoField, p.GoType())
	}
	blank(sb)
	seenTuple := map[string]bool{}
	for _, f := range plan.Fields {
		if f.IsConst {
			continue // const fields have no Go-side storage
		}
		// A tuple / nested section's sub-column fields live inside the element
		// struct; the SoA column is the outer field, once per tuple. Its shape
		// follows the attributes-per-row cardinality: Many `[][]S`, One `[]S`,
		// Optional decomposed into `<F>Val []S` + `<F>Has []bool` (mirroring the
		// scalar-Option split above).
		if f.TupleField != "" {
			if !seenTuple[f.TupleField] {
				seenTuple[f.TupleField] = true
				switch f.TupleCardinality {
				case mappingplan.AttrCardinalityOne:
					linef(sb, 1, "%s []%s", f.TupleField, f.TupleStructType)
				case mappingplan.AttrCardinalityOptional:
					linef(sb, 1, "%sVal []%s", f.TupleField, f.TupleStructType)
					linef(sb, 1, "%sHas []bool", f.TupleField)
				default: // Many
					linef(sb, 1, "%s [][]%s", f.TupleField, f.TupleStructType)
				}
			}
			continue
		}
		switch {
		case f.IsOption:
			linef(sb, 1, "%sVal []%s", f.GoFieldName, f.GoType())
			linef(sb, 1, "%sHas []bool", f.GoFieldName)
		case f.IsSlice():
			linef(sb, 1, "%s [][]%s", f.GoFieldName, f.GoType())
		case f.IsRoaring():
			linef(sb, 1, "%s []*roaring.Bitmap", f.GoFieldName)
		default:
			linef(sb, 1, "%s []%s", f.GoFieldName, f.GoType())
		}
		// Cut-2 carrier sibling: its own SoA column, emits no attribute —
		// one scalar carrier per attribute, so []X in the SoA.
		if f.CarrierField != "" {
			linef(sb, 1, "%s []marshalltypes.%s", f.CarrierField, f.CarrierType)
		}
	}
	line(sb, 0, "}\n")
}

func writeLenAndAppend(sb *strings.Builder, plan *mappingplan.Plan) {
	linef(sb, 0, "// Len returns the number of rows currently in the batch.")
	linef(sb, 0, "func (c *%sColumns) Len() int { return len(c.%s) }", plan.KindType, plan.PlainCols[0].GoField)
	blank(sb)

	linef(sb, 0, "// Append pushes one AoS record into the SoA buffers.")
	linef(sb, 0, "//")
	linef(sb, 0, "// Aliasing: slice and pointer fields (`[]T`, `*roaring.Bitmap`) are")
	linef(sb, 0, "// stored by reference, not copied. Callers must not mutate")
	linef(sb, 0, "// row.<F> after Append unless they want Marshal to read the")
	linef(sb, 0, "// mutation. Scalar fields (T, Option[T]) are copied by value.")
	linef(sb, 0, "func (c *%sColumns) Append(row %s) {", plan.KindType, plan.KindType)
	for _, p := range plan.PlainCols {
		linef(sb, 1, "c.%s = append(c.%s, row.%s)", p.GoField, p.GoField, p.GoField)
	}
	seenTuple := map[string]bool{}
	for _, f := range plan.Fields {
		if f.IsConst {
			continue
		}
		if f.TupleField != "" {
			if !seenTuple[f.TupleField] {
				seenTuple[f.TupleField] = true
				if f.TupleCardinality == mappingplan.AttrCardinalityOptional {
					linef(sb, 1, "c.%sVal = append(c.%sVal, row.%s.Val)", f.TupleField, f.TupleField, f.TupleField)
					linef(sb, 1, "c.%sHas = append(c.%sHas, row.%s.Has)", f.TupleField, f.TupleField, f.TupleField)
				} else {
					linef(sb, 1, "c.%s = append(c.%s, row.%s)", f.TupleField, f.TupleField, f.TupleField)
				}
			}
			continue
		}
		if f.IsOption {
			linef(sb, 1, "c.%sVal = append(c.%sVal, row.%s.Val)", f.GoFieldName, f.GoFieldName, f.GoFieldName)
			linef(sb, 1, "c.%sHas = append(c.%sHas, row.%s.Has)", f.GoFieldName, f.GoFieldName, f.GoFieldName)
		} else {
			linef(sb, 1, "c.%s = append(c.%s, row.%s)", f.GoFieldName, f.GoFieldName, f.GoFieldName)
		}
		if f.CarrierField != "" {
			linef(sb, 1, "c.%s = append(c.%s, row.%s)", f.CarrierField, f.CarrierField, f.CarrierField)
		}
	}
	line(sb, 0, "}\n")
}

func writeRowExtract(sb *strings.Builder, plan *mappingplan.Plan) {
	linef(sb, 0, "// Row reconstructs entity i as an AoS %s record. Inverse of", plan.KindType)
	linef(sb, 0, "// Append: slice / pointer fields are shared by reference (no")
	linef(sb, 0, "// defensive copy); scalar fields and Option[T] are copied.")
	linef(sb, 0, "func (c *%sColumns) Row(i int) (row %s) {", plan.KindType, plan.KindType)
	for _, p := range plan.PlainCols {
		linef(sb, 1, "row.%s = c.%s[i]", p.GoField, p.GoField)
	}
	seenTuple := map[string]bool{}
	for _, f := range plan.Fields {
		if f.IsConst {
			continue
		}
		if f.TupleField != "" {
			if !seenTuple[f.TupleField] {
				seenTuple[f.TupleField] = true
				if f.TupleCardinality == mappingplan.AttrCardinalityOptional {
					linef(sb, 1, "row.%s.Val = c.%sVal[i]", f.TupleField, f.TupleField)
					linef(sb, 1, "row.%s.Has = c.%sHas[i]", f.TupleField, f.TupleField)
				} else {
					linef(sb, 1, "row.%s = c.%s[i]", f.TupleField, f.TupleField)
				}
			}
			continue
		}
		if f.IsOption {
			linef(sb, 1, "row.%s.Val = c.%sVal[i]", f.GoFieldName, f.GoFieldName)
			linef(sb, 1, "row.%s.Has = c.%sHas[i]", f.GoFieldName, f.GoFieldName)
		} else {
			linef(sb, 1, "row.%s = c.%s[i]", f.GoFieldName, f.GoFieldName)
		}
		if f.CarrierField != "" {
			linef(sb, 1, "row.%s = c.%s[i]", f.CarrierField, f.CarrierField)
		}
	}
	line(sb, 1, "return\n}\n")
}

func planIdCol(plan *mappingplan.Plan) *mappingplan.PlainCol {
	return goplan.FindPlainCol(plan, "id")
}

func planTsCol(plan *mappingplan.Plan) *mappingplan.PlainCol {
	return goplan.FindPlainCol(plan, "ts")
}

func planNaturalKeyCol(plan *mappingplan.Plan) *mappingplan.PlainCol {
	return goplan.FindPlainCol(plan, "naturalKey")
}

func planExpiresAtCol(plan *mappingplan.Plan) *mappingplan.PlainCol {
	return goplan.FindPlainCol(plan, "expiresAt")
}

// plainArrowParam renders the Arrow accessor parameter type for a plain
// column in FillFromArrow, e.g. "*array.Uint64". The Go type was
// validated as a supported plain type at parse time.
func plainArrowParam(p *mappingplan.PlainCol) string {
	at, _ := goplan.PlainArrowArrayType(p.GoType())
	return "*" + at
}

// writePlainRead emits the append of colVar.Value(i) into c.<GoField> in
// FillFromArrow, with the per-type read handling: defensive copy for
// []byte, time.Time reconstruction from Arrow nanos, and a copy into a
// fresh array for fixed-width [N]byte. Scalars pass straight through
// (strict 1:1 — the column Go type is the value type).
func writePlainRead(sb *strings.Builder, depth int, p *mappingplan.PlainCol, colVar string) {
	f := p.GoField
	switch goplan.CopyStrategy(p.GoType()) {
	case goplan.CopyTime:
		linef(sb, depth, "c.%s = append(c.%s, time.Unix(0, int64(%s.Value(i))).UTC())", f, f, colVar)
	case goplan.CopyBytes:
		line(sb, depth, "{")
		linef(sb, depth+1, "src := %s.Value(i)", colVar)
		line(sb, depth+1, "cp := make([]byte, len(src))")
		line(sb, depth+1, "copy(cp, src)")
		linef(sb, depth+1, "c.%s = append(c.%s, cp)", f, f)
		line(sb, depth, "}")
	case goplan.CopyFixedByte:
		line(sb, depth, "{")
		linef(sb, depth+1, "var v %s", p.GoType())
		linef(sb, depth+1, "copy(v[:], %s.Value(i))", colVar)
		linef(sb, depth+1, "c.%s = append(c.%s, v)", f, f)
		line(sb, depth, "}")
	default:
		linef(sb, depth, "c.%s = append(c.%s, %s.Value(i))", f, f, colVar)
	}
}
