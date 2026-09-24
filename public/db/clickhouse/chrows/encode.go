package chrows

import (
	"io"
	"reflect"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// arrowTypeOf is the Arrow type a scalar field encodes as: the Go type's
// own width, text as utf8, time.Time as a UTC nanosecond timestamp.
func arrowTypeOf(t reflect.Type, k scalarKindE) (dt arrow.DataType) {
	switch k {
	case scalarKindString:
		dt = arrow.BinaryTypes.String
	case scalarKindBytes:
		dt = arrow.BinaryTypes.Binary
	case scalarKindBool:
		dt = arrow.FixedWidthTypes.Boolean
	case scalarKindTime:
		dt = &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"}
	case scalarKindInt:
		switch t.Kind() {
		case reflect.Int8:
			dt = arrow.PrimitiveTypes.Int8
		case reflect.Int16:
			dt = arrow.PrimitiveTypes.Int16
		case reflect.Int32:
			dt = arrow.PrimitiveTypes.Int32
		default:
			dt = arrow.PrimitiveTypes.Int64
		}
	case scalarKindUint:
		switch t.Kind() {
		case reflect.Uint8:
			dt = arrow.PrimitiveTypes.Uint8
		case reflect.Uint16:
			dt = arrow.PrimitiveTypes.Uint16
		case reflect.Uint32:
			dt = arrow.PrimitiveTypes.Uint32
		default:
			dt = arrow.PrimitiveTypes.Uint64
		}
	case scalarKindFloat:
		if t.Kind() == reflect.Float32 {
			dt = arrow.PrimitiveTypes.Float32
		} else {
			dt = arrow.PrimitiveTypes.Float64
		}
	}
	return
}

// EncodeStream writes src, a struct (or a pointer to one) of column slices
// tagged as for [Decode], to w as one Arrow IPC stream batch, the shape a
// ClickHouse `FORMAT ArrowStream` body has. A `,valid` field marks NULLs;
// an `,optional` tag has no meaning here. The column slices must be of
// equal length.
func EncodeStream(w io.Writer, src any) (err error) {
	v := reflect.ValueOf(src)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	p, err := planOf(v.Type(), shapeColumns)
	if err != nil {
		return
	}
	valid := make(map[string]reflect.Value, 2)
	for _, fp := range p.fields {
		if fp.valid {
			valid[fp.column] = v.Field(fp.index)
		}
	}
	rows := -1
	fields := make([]arrow.Field, 0, len(p.fields))
	values := make([]fieldPlan, 0, len(p.fields))
	for _, fp := range p.fields {
		l := v.Field(fp.index).Len()
		if rows >= 0 && l != rows {
			return eb.Build().Str("column", fp.column).Int("len", l).Int("expected", rows).Errorf("the column slices differ in length")
		}
		rows = l
		if fp.valid {
			continue
		}
		var dt arrow.DataType
		if fp.list {
			dt = arrow.ListOf(arrowTypeOf(fp.value.Elem(), fp.scalar))
		} else {
			dt = arrowTypeOf(fp.value, fp.scalar)
		}
		_, nullable := valid[fp.column]
		fields = append(fields, arrow.Field{Name: fp.column, Type: dt, Nullable: nullable})
		values = append(values, fp)
	}
	schema := arrow.NewSchema(fields, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Reserve(rows)
	for i, fp := range values {
		col := v.Field(fp.index)
		vf, hasValid := valid[fp.column]
		bld := rb.Field(i)
		for r := range rows {
			if hasValid && !vf.Index(r).Bool() {
				bld.AppendNull()
				continue
			}
			el := col.Index(r)
			if fp.list {
				lb := bld.(*array.ListBuilder)
				lb.Append(true)
				vb := lb.ValueBuilder()
				for j := range el.Len() {
					appendScalar(vb, el.Index(j))
				}
				continue
			}
			appendScalar(bld, el)
		}
	}
	rec := rb.NewRecordBatch()
	defer rec.Release()
	wr := ipc.NewWriter(w, ipc.WithSchema(schema), ipc.WithAllocator(memory.DefaultAllocator))
	if err = wr.Write(rec); err != nil {
		_ = wr.Close()
		return eh.Errorf("write the Arrow batch: %w", err)
	}
	if err = wr.Close(); err != nil {
		return eh.Errorf("close the Arrow stream: %w", err)
	}
	return
}

func appendScalar(b array.Builder, el reflect.Value) {
	switch c := b.(type) {
	case *array.StringBuilder:
		c.Append(el.String())
	case *array.BinaryBuilder:
		c.Append(el.Bytes())
	case *array.BooleanBuilder:
		c.Append(el.Bool())
	case *array.TimestampBuilder:
		c.Append(arrow.Timestamp(el.Interface().(time.Time).UnixNano()))
	case *array.Int8Builder:
		c.Append(int8(el.Int()))
	case *array.Int16Builder:
		c.Append(int16(el.Int()))
	case *array.Int32Builder:
		c.Append(int32(el.Int()))
	case *array.Int64Builder:
		c.Append(el.Int())
	case *array.Uint8Builder:
		c.Append(uint8(el.Uint()))
	case *array.Uint16Builder:
		c.Append(uint16(el.Uint()))
	case *array.Uint32Builder:
		c.Append(uint32(el.Uint()))
	case *array.Uint64Builder:
		c.Append(el.Uint())
	case *array.Float32Builder:
		c.Append(float32(el.Float()))
	case *array.Float64Builder:
		c.Append(el.Float())
	}
}
