package chrows

import (
	"reflect"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// TagKey is the struct tag that names a field's column.
//
// The same tags serve both destination shapes. A column struct ([Decode])
// holds one slice per column:
//
//	type events struct {
//		JobId     []string    `ch:"job_id"`
//		At        []time.Time `ch:"at"`
//		Note      []string    `ch:"note,optional"` // the column may be absent
//		NoteValid []bool      `ch:"note,valid"`    // false where note is NULL
//		Kinds     [][]string  `ch:"kinds"`         // Array(String)
//	}
//
// A row struct ([DecodeRows]) holds one value per column, and is the
// element of the slice the rows land in:
//
//	type event struct {
//		JobId     string    `ch:"job_id"`
//		Note      string    `ch:"note"`
//		NoteValid bool      `ch:"note,valid"`
//		Kinds     []string  `ch:"kinds"`
//	}
//
// A value is a string, []byte, bool, a sized or unsized integer, float32,
// float64 or time.Time — or a named type over one of those — or a slice of
// such values for an Array column. Untagged fields are not columns. A result
// column no field names is ignored.
const TagKey = "ch"

type scalarKindE uint8

const (
	scalarKindString scalarKindE = iota + 1
	scalarKindBytes
	scalarKindBool
	scalarKindInt
	scalarKindUint
	scalarKindFloat
	scalarKindTime
)

var timeType = reflect.TypeFor[time.Time]()

func scalarKindOf(t reflect.Type) (k scalarKindE, ok bool) {
	ok = true
	if t == timeType {
		k = scalarKindTime
		return
	}
	switch t.Kind() {
	case reflect.String:
		k = scalarKindString
	case reflect.Bool:
		k = scalarKindBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		k = scalarKindInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		k = scalarKindUint
	case reflect.Float32, reflect.Float64:
		k = scalarKindFloat
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			k = scalarKindBytes
		} else {
			ok = false
		}
	default:
		ok = false
	}
	return
}

// shapeE is what a tagged field holds: every row of its column, or one.
type shapeE uint8

const (
	shapeColumns shapeE = iota
	shapeRow
)

// fieldPlan is one tagged field as the struct declares it.
type fieldPlan struct {
	index    int
	column   string
	optional bool
	// valid marks a bool field (a []bool one in a column struct) carrying
	// the column's validity.
	valid bool
	// value is the Go type of one cell: the field's type in a row struct,
	// its element type in a column struct.
	value reflect.Type
	// scalar is the kind the cells are read as; for a list, the kind of
	// its elements.
	scalar scalarKindE
	// list is set when value is a slice read from an Array column. A
	// []byte value is a bytes cell; a [][]byte one is a list of bytes.
	list bool
}

// structPlan is a struct type's tagged fields, in declaration order.
type structPlan struct {
	typ    reflect.Type
	shape  shapeE
	fields []fieldPlan
}

func planOf(t reflect.Type, shape shapeE) (p structPlan, err error) {
	if t.Kind() != reflect.Struct {
		err = eb.Build().Stringer("type", t).Errorf("the destination is not a struct")
		return
	}
	p.typ, p.shape = t, shape
	seen := make(map[string]bool, t.NumField())
	for i := range t.NumField() {
		sf := t.Field(i)
		tag, has := sf.Tag.Lookup(TagKey)
		if !has || tag == "-" {
			continue
		}
		if !sf.IsExported() {
			err = eb.Build().Str("field", sf.Name).Errorf("a tagged field is unexported")
			return
		}
		name, opts, _ := strings.Cut(tag, ",")
		fp := fieldPlan{index: i, column: name}
		for o := range strings.SplitSeq(opts, ",") {
			switch o {
			case "":
			case "optional":
				fp.optional = true
			case "valid":
				fp.valid = true
			default:
				err = eb.Build().Str("field", sf.Name).Str("option", o).Errorf("unknown tag option")
				return
			}
		}
		if name == "" {
			err = eb.Build().Str("field", sf.Name).Errorf("the tag names no column")
			return
		}
		fp.value = sf.Type
		if shape == shapeColumns {
			if sf.Type.Kind() != reflect.Slice {
				err = eb.Build().Str("field", sf.Name).Stringer("type", sf.Type).Errorf("a column struct's field must be a slice")
				return
			}
			fp.value = sf.Type.Elem()
		}
		key := name
		if fp.valid {
			if fp.value.Kind() != reflect.Bool {
				err = eb.Build().Str("field", sf.Name).Errorf("a validity field must hold bool")
				return
			}
			key = name + ",valid"
		} else {
			k, ok := scalarKindOf(fp.value)
			if !ok && fp.value.Kind() == reflect.Slice {
				k, ok = scalarKindOf(fp.value.Elem())
				fp.list = ok
			}
			if !ok {
				err = eb.Build().Str("field", sf.Name).Stringer("type", sf.Type).Errorf("the field's type is not a column type")
				return
			}
			fp.scalar = k
		}
		if seen[key] {
			err = eb.Build().Str("column", name).Errorf("two fields name the same column")
			return
		}
		seen[key] = true
		p.fields = append(p.fields, fp)
	}
	if len(p.fields) == 0 {
		err = eb.Build().Stringer("type", t).Errorf("the struct has no `ch` tagged fields")
	}
	return
}

// accepts reports whether cells of dt can be read as k. It is checked once
// per column, so a mismatch is one error naming the column rather than one
// per row.
func accepts(k scalarKindE, dt arrow.DataType) (ok bool) {
	vt := ValueType(dt)
	switch k {
	case scalarKindString:
		ok = IsStringLike(vt)
	case scalarKindBytes:
		ok = IsStringLike(vt) || vt.ID() == arrow.FIXED_SIZE_BINARY
	case scalarKindBool:
		ok = vt.ID() == arrow.BOOL || IsInteger(vt)
	case scalarKindInt, scalarKindUint:
		ok = IsInteger(vt)
	case scalarKindFloat:
		ok = IsNumeric(vt) || IsDecimal(vt)
	case scalarKindTime:
		switch vt.ID() {
		case arrow.TIMESTAMP, arrow.DATE32, arrow.DATE64, arrow.UINT32:
			ok = true
		}
	}
	return
}

// binding is a field plan bound to one schema: the column it reads, or -1
// for an optional column the result lacks. nullable is set when the
// column's NULLs have a validity field to go to.
type binding struct {
	fieldPlan
	col      int
	nullable bool
}

func (inst structPlan) bind(schema *arrow.Schema) (bs []binding, err error) {
	bs = make([]binding, 0, len(inst.fields))
	hasValid := make(map[string]bool, 2)
	for _, fp := range inst.fields {
		if fp.valid {
			hasValid[fp.column] = true
		}
	}
	for _, fp := range inst.fields {
		b := binding{fieldPlan: fp, col: -1, nullable: hasValid[fp.column]}
		if idx := schema.FieldIndices(fp.column); len(idx) > 0 {
			b.col = idx[0]
		} else if !fp.optional && !fp.valid {
			names := fieldNames(schema)
			err = eb.Build().Str("column", fp.column).Strs("got", names).Errorf("the result lacks column %s; it has: %s", fp.column, strings.Join(names, ", ")) //boxer:lint disable=CS013 reason="shape 1: callers render Error() to a human (a panel, a CLI), and the column list is what diagnoses a mistyped tag"
			return
		}
		if b.col >= 0 && !fp.valid {
			dt := schema.Field(b.col).Type
			ok := false
			if fp.list {
				if l, isList := dt.(arrow.ListLikeType); isList {
					ok = accepts(fp.scalar, l.Elem())
				}
			} else {
				ok = accepts(fp.scalar, dt)
			}
			if !ok {
				err = eb.Build().Str("column", fp.column).Stringer("type", dt).Stringer("field", fp.value).Errorf("the column cannot be read into the field")
				return
			}
		}
		bs = append(bs, b)
	}
	return
}

func fieldNames(schema *arrow.Schema) (names []string) {
	names = make([]string, 0, schema.NumFields())
	for _, f := range schema.Fields() {
		names = append(names, f.Name)
	}
	return
}
