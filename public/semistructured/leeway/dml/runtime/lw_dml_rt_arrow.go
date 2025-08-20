//go:build disabled
package runtime

import (
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

type ValueAdder struct {
	pool          *memory.GoAllocator
	structBuilder *array.StructBuilder
	fieldIndex    int
	listBuilder *array.ListBuilder
	a11 *array.StringBuilder
	a17 *array.BooleanBuilder
	a12 *array.BinaryBuilder
	a16 *array.FixedSizeBinaryBuilder

	a15 *array.FixedSizeBinaryDictionaryBuilder
	a20 *array.BinaryDictionaryBuilder

	a1 *array.Uint8Builder
	a2 *array.Uint16Builder
	a3 *array.Uint32Builder
	a4 *array.Uint64Builder
	a212 *array.Uint8DictionaryBuilder
	a213 *array.Uint16DictionaryBuilder
	a214 *array.Uint32DictionaryBuilder
	a215 *array.Uint64DictionaryBuilder

	a5 *array.Int8Builder
	a6 *array.Int16Builder
	a7 *array.Int32Builder
	a8 *array.Int64Builder
	a216 *array.Int8DictionaryBuilder
	a219 *array.Int16DictionaryBuilder
	a217 *array.Int32DictionaryBuilder
	a218 *array.Int64DictionaryBuilder

	a922 *array.Float16Builder
	a934 *array.Float32Builder
	a10 *array.Float64Builder
	a211 *array.Float16DictionaryBuilder
	a234 *array.Float32DictionaryBuilder
	a4545 *array.Float64DictionaryBuilder

	a13 *array.Date32Builder
	a14 *array.Date64Builder
	a18 *array.Time32Builder
	a19 *array.Time64Builder
	a4353 *array.TimestampBuilder
	a3434 *array.Date32DictionaryBuilder
	a3435 *array.Date64DictionaryBuilder
	a2134 *array.Time32DictionaryBuilder
	a211034 *array.Time64DictionaryBuilder
	a214034 *array.TimestampDictionaryBuilder

}

func NewValueAdder(pool *memory.GoAllocator, structBuilder *array.StructBuilder, fieldIndex int) *ValueAdder {
	structBuilder.FieldBuilder(fieldIndex).Type()
	return &ValueAdder{
		pool:          pool,
		structBuilder: structBuilder,
		fieldIndex:    fieldIndex,
	}
}
func (inst *ValueAdder) Release() {

}
func (inst *ValueAdder) BeginList() {
	l := &array.ListBuilder{}
	l.NewArray().
	d := &array.Date32DictionaryBuilder{}
	d.Append()
	array.FromJSON()
	inst.listBuilder.ValueBuilder()
}
func (inst *ValueAdder) EndList() {

}
func (inst *ValueAdder) AddBool(v bool) {
func (inst *ValueAdder) AddBool(v bool) {
	t := array.ListBuilder{}
	t.ValueBuilder()
}
