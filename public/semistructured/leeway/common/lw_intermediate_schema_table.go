package common

import (
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
)

// FIXME make this an et7 table
var SchemaTableArrowSchema = arrow.NewSchema([]arrow.Field{
	{Name: "Id", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "Scope", Type: &arrow.DictionaryType{ValueType: arrow.BinaryTypes.String, IndexType: arrow.PrimitiveTypes.Uint8}, Nullable: false},
	{Name: "ItemType", Type: &arrow.DictionaryType{ValueType: arrow.BinaryTypes.String, IndexType: arrow.PrimitiveTypes.Uint8}, Nullable: false},
	{Name: "SectionName", Type: &arrow.DictionaryType{ValueType: arrow.BinaryTypes.String, IndexType: arrow.PrimitiveTypes.Uint32}, Nullable: false},
	{Name: "LogicalColumnName", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "ColumnRole", Type: &arrow.DictionaryType{ValueType: arrow.BinaryTypes.String, IndexType: arrow.PrimitiveTypes.Uint8}, Nullable: false},
	{Name: "SubType", Type: &arrow.DictionaryType{ValueType: arrow.BinaryTypes.String, IndexType: arrow.PrimitiveTypes.Uint8}, Nullable: false},
	{Name: "UseAspects", Type: arrow.ListOfNonNullable(arrow.BinaryTypes.String), Nullable: false},
	{Name: "CanonicalType", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "EncodingHints", Type: arrow.ListOfNonNullable(arrow.BinaryTypes.String), Nullable: false},
}, nil)

func (inst *IntermediateTableRepresentation) LoadInArrowBuilder(id StylableName, builder *array.RecordBuilder) (err error) {
	idCol := builder.Field(0).(*array.StringBuilder)
	scopeCol := builder.Field(1).(*array.BinaryDictionaryBuilder)
	itemTypeCol := builder.Field(2).(*array.BinaryDictionaryBuilder)
	sectionCol := builder.Field(3).(*array.BinaryDictionaryBuilder)
	nameCol := builder.Field(4).(*array.StringBuilder)
	roleCol := builder.Field(5).(*array.BinaryDictionaryBuilder)
	subTypeCol := builder.Field(6).(*array.BinaryDictionaryBuilder)
	useAspectColList := builder.Field(7).(*array.ListBuilder)
	useAspectColValue := builder.Field(7).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	canonicalTypeCol := builder.Field(11).(*array.StringBuilder)
	encodingHintsColList := builder.Field(12).(*array.ListBuilder)
	encodingHintsColValue := builder.Field(12).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	addRow := func(scope IntermediateColumnScopeE, itemType PlainItemTypeE, section string, name string, role ColumnRoleE, subType IntermediateColumnSubTypeE, useAspect useaspects.AspectSet, canonicalType canonicalTypes.PrimitiveAstNodeI, encodingHints encodingaspects.AspectSet) (err error) {
		idCol.AppendString(id.Convert(DefaultNamingStyle).String())
		err = scopeCol.AppendString(scope.String())
		if err != nil {
			return
		}
		err = itemTypeCol.AppendString(itemType.String())
		if err != nil {
			return
		}
		err = sectionCol.AppendString(section)
		if err != nil {
			return
		}
		nameCol.AppendString(name)
		err = roleCol.AppendString(role.String())
		if err != nil {
			return
		}
		err = subTypeCol.AppendString(subType.String())
		if err != nil {
			return
		}
		if !useAspect.IsValid() {
			err = eh.Errorf("use aspect set is invalid")
			return
		}
		useAspectColList.Append(true)
		for _, asp := range useAspect.IterateAspects() {
			useAspectColValue.AppendString(asp.String())
		}
		if !canonicalType.IsValid() {
			err = eh.Errorf("canonical type is invalid")
			return
		}
		canonicalTypeCol.AppendString(canonicalType.String())
		if !encodingHints.IsValid() {
			err = eh.Errorf("encoding hint set is invalid")
			return
		}
		encodingHintsColList.Append(true)
		for _, hint := range encodingHints.IterateAspects() {
			encodingHintsColValue.AppendString(hint.String())
		}
		return
	}
	addColumnProps := func(cc IntermediateColumnContext, p *IntermediateColumnProps) (err error) {
		for i, name := range p.Names {
			err = addRow(cc.Scope, cc.PlainItemType, cc.SectionName.String(), name.String(), p.Roles[i], cc.SubType, cc.UseAspects, p.CanonicalType[i], p.EncodingHints[i])
			if err != nil {
				return
			}
		}
		return
	}
	for cc, cp := range inst.IterateColumnProps() {
		err = addColumnProps(cc, cp)
		if err != nil {
			return
		}
	}
	return
}
func (inst *IntermediateTableRepresentation) ToSchemaTable(id StylableName, out io.Writer) (err error) {
	builder := array.NewRecordBuilder(memory.DefaultAllocator, SchemaTableArrowSchema)
	defer builder.Release()

	err = inst.LoadInArrowBuilder(id, builder)
	if err != nil {
		err = eh.Errorf("unable to load intermediate representation in arrow builder: %w", err)
		return
	}

	rec := builder.NewRecord()
	defer rec.Release()

	tbl := array.NewTableFromRecords(SchemaTableArrowSchema, []arrow.Record{rec})
	defer tbl.Release()
	var w *ipc.FileWriter
	w, err = ipc.NewFileWriter(out, ipc.WithZstd(), ipc.WithAllocator(memory.DefaultAllocator), ipc.WithSchema(tbl.Schema()))
	if err != nil {
		err = eh.Errorf("unable to create file writer: %w", err)
	}
	defer w.Close()
	err = w.Write(rec)
	if err != nil {
		err = eh.Errorf("unable to write table: %w", err)
		return
	}
	return
}
