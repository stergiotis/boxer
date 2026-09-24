package chrows

import (
	"bytes"
	"io"
	"reflect"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// destOf checks dst is a non-nil pointer to a struct and returns the
// struct.
func destOf(dst any) (v reflect.Value, err error) {
	pv := reflect.ValueOf(dst)
	if pv.Kind() != reflect.Pointer || pv.IsNil() {
		err = eh.Errorf("the destination is not a non-nil pointer to a struct")
		return
	}
	v = pv.Elem()
	return
}

// Decode appends the rows of rec to dst, a pointer to a struct of column
// slices (see [TagKey]), and returns how many it appended. The tagged
// slices must be of equal length on entry; they are on return.
//
// Values are copied out of the batch, so dst outlives rec. A NULL is an
// error unless the column has a `,valid` field, which then records it and
// the value slice takes the zero value. An integer that does not fit its
// field is an error, as is a column whose type the field cannot hold.
func Decode(dst any, rec arrow.RecordBatch) (n int, err error) {
	v, err := destOf(dst)
	if err != nil {
		return
	}
	p, err := planOf(v.Type(), shapeColumns)
	if err != nil {
		return
	}
	bs, err := p.bind(rec.Schema())
	if err != nil {
		return
	}
	return decodeColumns(v, bs, rec)
}

// DecodeStream reads an Arrow IPC stream — the body of a ClickHouse
// `FORMAT ArrowStream` — into dst as [Decode] does, batch by batch, and
// returns the rows appended. Compressed bodies (ClickHouse's default
// lz4_frame) are read as they come. A result of no rows still carries its
// schema, so a mistyped tag is reported on an empty result too.
func DecodeStream(dst any, r io.Reader) (n int, err error) {
	v, err := destOf(dst)
	if err != nil {
		return
	}
	p, err := planOf(v.Type(), shapeColumns)
	if err != nil {
		return
	}
	err = eachBatch(r, p, func(bs []binding, rec arrow.RecordBatch) (err error) {
		var m int
		m, err = decodeColumns(v, bs, rec)
		n += m
		return
	})
	return
}

// DecodeBytes is [DecodeStream] over a body already in memory.
func DecodeBytes(dst any, body []byte) (n int, err error) {
	return DecodeStream(dst, bytes.NewReader(body))
}

// eachBatch binds p to the stream's schema once and hands every batch to
// yield.
func eachBatch(r io.Reader, p structPlan, yield func(bs []binding, rec arrow.RecordBatch) error) (err error) {
	rdr, err := ipc.NewReader(r, ipc.WithAllocator(memory.DefaultAllocator))
	if err != nil {
		return eh.Errorf("open the Arrow stream: %w", err)
	}
	defer rdr.Release()
	bs, err := p.bind(rdr.Schema())
	if err != nil {
		return
	}
	for rdr.Next() {
		if err = yield(bs, rdr.RecordBatch()); err != nil {
			return
		}
	}
	if err = rdr.Err(); err != nil {
		return eh.Errorf("read the Arrow stream: %w", err)
	}
	return
}

func decodeColumns(v reflect.Value, bs []binding, rec arrow.RecordBatch) (n int, err error) {
	rows := int(rec.NumRows())
	base := -1
	for _, b := range bs {
		l := v.Field(b.index).Len()
		if base >= 0 && l != base {
			err = eb.Build().Str("column", b.column).Int("len", l).Int("expected", base).Errorf("the destination's column slices differ in length")
			return
		}
		base = l
	}
	for _, b := range bs {
		f := v.Field(b.index)
		f.Grow(rows)
		f.SetLen(base + rows)
		dst := f.Slice(base, base+rows)
		if b.col < 0 {
			// An absent optional column, or its validity: zero values,
			// cleared explicitly in case the backing array is reused.
			for r := range rows {
				dst.Index(r).SetZero()
			}
			continue
		}
		arr := rec.Column(b.col)
		if b.valid {
			for r := range rows {
				dst.Index(r).SetBool(arr.IsValid(r))
			}
			continue
		}
		if err = checkNulls(b, arr); err != nil {
			return
		}
		if !b.list && copyFast(dst, arr) {
			continue
		}
		for r := range rows {
			if err = setCell(dst.Index(r), b, arr, r); err != nil {
				return
			}
		}
	}
	n = rows
	return
}

// copyFast copies a NULL-free primitive column whose Go element type is
// the field's own, without per-cell reads.
func copyFast(dst reflect.Value, arr arrow.Array) (done bool) {
	if arr.NullN() > 0 {
		return
	}
	var src any
	switch c := arr.(type) {
	case *array.Int8:
		src = c.Int8Values()
	case *array.Int16:
		src = c.Int16Values()
	case *array.Int32:
		src = c.Int32Values()
	case *array.Int64:
		src = c.Int64Values()
	case *array.Uint8:
		src = c.Uint8Values()
	case *array.Uint16:
		src = c.Uint16Values()
	case *array.Uint32:
		src = c.Uint32Values()
	case *array.Uint64:
		src = c.Uint64Values()
	case *array.Float32:
		src = c.Float32Values()
	case *array.Float64:
		src = c.Float64Values()
	default:
		return
	}
	sv := reflect.ValueOf(src)
	if sv.Type().Elem() != dst.Type().Elem() {
		return
	}
	reflect.Copy(dst, sv)
	return true
}
