package chrows

import (
	"bytes"
	"reflect"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// checkNulls refuses a column holding NULLs that no validity field records.
func checkNulls(b binding, arr arrow.Array) (err error) {
	if arr.NullN() > 0 && !b.nullable {
		err = eb.Build().Str("column", b.column).Int("nulls", arr.NullN()).Errorf("the column holds NULLs and has no `,valid` field to record them")
	}
	return
}

// setCell reads arr[row] into el, a settable value of the field's value
// type. A NULL leaves el at its zero value; the caller decided it may.
func setCell(el reflect.Value, b binding, arr arrow.Array, row int) (err error) {
	if arr.IsNull(row) {
		el.SetZero()
		return
	}
	if !b.list {
		return setScalar(el, b.scalar, arr, row, b.column)
	}
	l, ok := arr.(array.ListLike)
	if !ok {
		return eb.Build().Str("column", b.column).Stringer("type", arr.DataType()).Errorf("the column is not an Array")
	}
	inner := l.ListValues()
	start, end := l.ValueOffsets(row)
	s := reflect.MakeSlice(el.Type(), int(end-start), int(end-start))
	for j := range s.Len() {
		at := int(start) + j
		if inner.IsNull(at) {
			return eb.Build().Str("column", b.column).Int("row", row).Errorf("an Array element is NULL, which a slice cannot hold")
		}
		if err = setScalar(s.Index(j), b.scalar, inner, at, b.column); err != nil {
			return
		}
	}
	el.Set(s)
	return
}

// setScalar reads the non-NULL arr[row] into el. Text and bytes are
// copied, so el outlives the batch.
func setScalar(el reflect.Value, k scalarKindE, arr arrow.Array, row int, column string) (err error) {
	ok := false
	switch k {
	case scalarKindString:
		var s string
		if s, ok = String(arr, row); ok {
			el.SetString(strings.Clone(s))
		}
	case scalarKindBytes:
		var b []byte
		if b, ok = Bytes(arr, row); ok {
			el.SetBytes(bytes.Clone(b))
		}
	case scalarKindBool:
		var b bool
		if b, ok = Bool(arr, row); ok {
			el.SetBool(b)
		}
	case scalarKindInt:
		var i int64
		if i, ok = Int64(arr, row); ok {
			ok = !el.OverflowInt(i)
			el.SetInt(i)
		}
	case scalarKindUint:
		var u uint64
		if u, ok = Uint64(arr, row); ok {
			ok = !el.OverflowUint(u)
			el.SetUint(u)
		}
	case scalarKindFloat:
		var f float64
		if f, ok = Float64(arr, row); ok {
			el.SetFloat(f)
		}
	case scalarKindTime:
		var t time.Time
		if t, ok = Time(arr, row); ok {
			el.Set(reflect.ValueOf(t))
		}
	}
	if !ok {
		err = eb.Build().Str("column", column).Int("row", row).Str("value", arr.ValueStr(row)).Stringer("field", el.Type()).
			Errorf("the value does not fit the field")
	}
	return
}
