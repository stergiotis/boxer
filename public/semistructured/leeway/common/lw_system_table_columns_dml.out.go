// Code generated; Leeway DML (github.com/stergiotis/boxer/public/semistructured/leeway/test.test) DO NOT EDIT.

package common

import (
	"errors"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	_ "github.com/apache/arrow-go/v18/arrow/ipc"
	_ "github.com/apache/arrow-go/v18/arrow/math"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
	"slices"
)

///////////////////////////////////////////////////////////////////
// code generator
// gocodegen.GenerateArrowSchemaFactory
// ./public/semistructured/leeway/gocodegen/gocodegen_common.go:26

func CreateSchemaSystemTableColumns() (schema *arrow.Schema) {
	schema = arrow.NewSchema([]arrow.Field{
		/* 000 */ arrow.Field{Name: "id:tableHash:u64:2k:0:0:", Nullable: false, Type: arrow.PrimitiveTypes.Uint64},
		/* 001 */ arrow.Field{Name: "id:columnIndex:u64:2k:0:0:", Nullable: false, Type: arrow.PrimitiveTypes.Uint64},
		/* 002 */ arrow.Field{Name: "ro:tableName:s:k:0:0:", Nullable: false, Type: &arrow.StringType{}},
		/* 003 */ arrow.Field{Name: "tv:symbol:value:val:s:m:0:24:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 004 */ arrow.Field{Name: "tv:symbol:hr:hr:u64:2k:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 005 */ arrow.Field{Name: "tv:symbol:lr:lr:u64:2q:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 006 */ arrow.Field{Name: "tv:symbol:lmr:lmr:u64:2q:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 007 */ arrow.Field{Name: "tv:symbol:mrhp:mrhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 008 */ arrow.Field{Name: "tv:symbol:hrcard:hrcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 009 */ arrow.Field{Name: "tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 010 */ arrow.Field{Name: "tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 011 */ arrow.Field{Name: "tv:string:value:val:s:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 012 */ arrow.Field{Name: "tv:string:hr:hr:u64:2k:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 013 */ arrow.Field{Name: "tv:string:lr:lr:u64:2q:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 014 */ arrow.Field{Name: "tv:string:lmr:lmr:u64:2q:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 015 */ arrow.Field{Name: "tv:string:mrhp:mrhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 016 */ arrow.Field{Name: "tv:string:hrcard:hrcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 017 */ arrow.Field{Name: "tv:string:lrcard:lrcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 018 */ arrow.Field{Name: "tv:string:lmrcard:lmrcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 019 */ arrow.Field{Name: "tv:u64:value:val:u64:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 020 */ arrow.Field{Name: "tv:u64:hr:hr:u64:2k:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 021 */ arrow.Field{Name: "tv:u64:lr:lr:u64:2q:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 022 */ arrow.Field{Name: "tv:u64:lmr:lmr:u64:2q:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 023 */ arrow.Field{Name: "tv:u64:mrhp:mrhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 024 */ arrow.Field{Name: "tv:u64:hrcard:hrcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 025 */ arrow.Field{Name: "tv:u64:lrcard:lrcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 026 */ arrow.Field{Name: "tv:u64:lmrcard:lmrcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 027 */ arrow.Field{Name: "tv:text:value:val:s:g:9G59mUg:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 028 */ arrow.Field{Name: "tv:text:hr:hr:u64:2k:9G59mUg:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 029 */ arrow.Field{Name: "tv:text:lr:lr:u64:2q:9G59mUg:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 030 */ arrow.Field{Name: "tv:text:lmr:lmr:u64:2q:9G59mUg:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 031 */ arrow.Field{Name: "tv:text:mrhp:mrhp:y:g:9G59mUg:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 032 */ arrow.Field{Name: "tv:text:hrcard:hrcard:u64:4gw:9G59mUg:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 033 */ arrow.Field{Name: "tv:text:lrcard:lrcard:u64:4gw:9G59mUg:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 034 */ arrow.Field{Name: "tv:text:lmrcard:lmrcard:u64:4gw:9G59mUg:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
	}, nil)
	return
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityClassAndFactoryCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1184

type InEntitySystemTableColumns struct {
	errs            []error
	state           runtime.EntityStateE
	allocator       memory.Allocator
	builder         *array.RecordBuilder
	records         []arrow.RecordBatch
	section00Inst   *InEntitySystemTableColumnsSectionString
	section00State  runtime.EntityStateE
	section01Inst   *InEntitySystemTableColumnsSectionSymbol
	section01State  runtime.EntityStateE
	section02Inst   *InEntitySystemTableColumnsSectionText
	section02State  runtime.EntityStateE
	section03Inst   *InEntitySystemTableColumnsSectionU64
	section03State  runtime.EntityStateE
	plainTableHash0 uint64

	plainColumnIndex1 uint64

	plainTableName2       string
	scalarFieldBuilder000 *array.Uint64Builder

	scalarFieldBuilder001 *array.Uint64Builder

	scalarFieldBuilder002 *array.StringBuilder
}

func NewInEntitySystemTableColumns(allocator memory.Allocator, estimatedNumberOfRecords int) (inst *InEntitySystemTableColumns) {
	inst = &InEntitySystemTableColumns{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.allocator = allocator
	inst.records = make([]arrow.RecordBatch, 0, estimatedNumberOfRecords)
	schema := CreateSchemaSystemTableColumns()
	builder := array.NewRecordBuilder(allocator, schema)
	inst.builder = builder
	inst.initSections(builder)
	inst.scalarFieldBuilder000 = builder.Field(0).(*array.Uint64Builder)
	inst.scalarFieldBuilder001 = builder.Field(1).(*array.Uint64Builder)
	inst.scalarFieldBuilder002 = builder.Field(2).(*array.StringBuilder)

	return inst
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1298

func (inst *InEntitySystemTableColumns) SetId(tableHash0 uint64, columnIndex1 uint64) *InEntitySystemTableColumns {
	if inst.state != runtime.EntityStateInEntity {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.plainTableHash0 = tableHash0
	inst.plainColumnIndex1 = columnIndex1

	return inst
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1298

func (inst *InEntitySystemTableColumns) SetRouting(tableName2 string) *InEntitySystemTableColumns {
	if inst.state != runtime.EntityStateInEntity {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.plainTableName2 = tableName2

	return inst
}
func (inst *InEntitySystemTableColumns) appendPlainValues() {
	inst.scalarFieldBuilder000.Append(inst.plainTableHash0)

	inst.scalarFieldBuilder001.Append(inst.plainColumnIndex1)

	inst.scalarFieldBuilder002.Append(inst.plainTableName2)
}
func (inst *InEntitySystemTableColumns) resetPlainValues() {
	inst.plainTableHash0 = uint64(0)

	inst.plainColumnIndex1 = uint64(0)

	inst.plainTableName2 = ""
}
func (inst *InEntitySystemTableColumns) initSections(builder *array.RecordBuilder) {
	inst.section00Inst = NewInEntitySystemTableColumnsSectionString(builder, inst)
	inst.section01Inst = NewInEntitySystemTableColumnsSectionSymbol(builder, inst)
	inst.section02Inst = NewInEntitySystemTableColumnsSectionText(builder, inst)
	inst.section03Inst = NewInEntitySystemTableColumnsSectionU64(builder, inst)
}
func (inst *InEntitySystemTableColumns) beginSections() {
	inst.section00Inst.beginSection()
	inst.section01Inst.beginSection()
	inst.section02Inst.beginSection()
	inst.section03Inst.beginSection()
}
func (inst *InEntitySystemTableColumns) resetSections() {
	inst.section00Inst.resetSection()
	inst.section01Inst.resetSection()
	inst.section02Inst.resetSection()
	inst.section03Inst.resetSection()
}
func (inst *InEntitySystemTableColumns) CheckErrors() (err error) {
	err = eh.CheckErrors(inst.errs)
	err = errors.Join(err, inst.section00Inst.CheckErrors())
	err = errors.Join(err, inst.section01Inst.CheckErrors())
	err = errors.Join(err, inst.section02Inst.CheckErrors())
	err = errors.Join(err, inst.section03Inst.CheckErrors())

	return
}
func (inst *InEntitySystemTableColumns) GetSectionString() *InEntitySystemTableColumnsSectionString {
	return inst.section00Inst
}
func (inst *InEntitySystemTableColumns) GetSectionSymbol() *InEntitySystemTableColumnsSectionSymbol {
	return inst.section01Inst
}
func (inst *InEntitySystemTableColumns) GetSectionText() *InEntitySystemTableColumnsSectionText {
	return inst.section02Inst
}
func (inst *InEntitySystemTableColumns) GetSectionU64() *InEntitySystemTableColumnsSectionU64 {
	return inst.section03Inst
}
func (inst *InEntitySystemTableColumns) BeginEntity() *InEntitySystemTableColumns {
	switch inst.state {
	case runtime.EntityStateInitial:
		inst.state = runtime.EntityStateInEntity
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}

	inst.beginSections()
	return inst
}
func (inst *InEntitySystemTableColumns) validateEntity() {
	{
		state := inst.section00Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "string").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section01Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "symbol").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section02Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "text").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section03Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "u64").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}

	// FIXME check coSectionGroup consistency
	return
}
func (inst *InEntitySystemTableColumns) CommitEntity() (err error) {
	inst.validateEntity()
	err = inst.CheckErrors()
	if err != nil {
		err = eh.Errorf("unable to commit entity, found errors: %w", err)
		return
	}
	switch inst.state {
	case runtime.EntityStateInEntity:
		inst.state = runtime.EntityStateInitial
		break
	default:
		err = runtime.ErrInvalidStateTransition
		return
	}

	inst.appendPlainValues()
	inst.resetPlainValues()
	inst.resetSections()
	return
}
func (inst *InEntitySystemTableColumns) RollbackEntity() (err error) {
	switch inst.state {
	case runtime.EntityStateInEntity:
		inst.state = runtime.EntityStateInitial
		break
	default:
		err = runtime.ErrInvalidStateTransition
		return
	}

	inst.appendPlainValues() // arrow fields must all have one row
	inst.resetPlainValues()
	inst.resetSections()
	rec := inst.builder.NewRecord()
	if rec.NumRows() > 1 {
		inst.records = append(inst.records, rec.NewSlice(0, rec.NumRows()-1))
	} else {
		// FIXME find better way to truncate builder
		inst.builder.NewRecord().Release()
	}
	return
}

// TransferRecords The returned Records must be Release()'d after use.
func (inst *InEntitySystemTableColumns) TransferRecords(recordsIn []arrow.RecordBatch) (recordsOut []arrow.RecordBatch, err error) {
	if inst.state != runtime.EntityStateInitial {
		err = runtime.ErrInvalidStateTransition
		return
	}

	recordsOut = slices.Grow(recordsIn, len(inst.records)+1)
	copy(recordsOut, inst.records)
	clear(inst.records)
	inst.records = inst.records[:0]
	rec := inst.builder.NewRecord()
	if rec.NumRows() > 0 {
		recordsOut = append(recordsOut, rec)
	}
	return
}

func (inst *InEntitySystemTableColumns) GetSchema() (schema *arrow.Schema) {
	return inst.builder.Schema()
}

func (inst *InEntitySystemTableColumns) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntitySystemTableColumns) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntitySystemTableColumnsSectionString struct {
	errs                  []error
	inAttr                *InEntitySystemTableColumnsSectionStringInAttr
	state                 runtime.EntityStateE
	parent                *InEntitySystemTableColumns
	scalarFieldBuilder011 *array.StringBuilder
	scalarListBuilder011  *array.ListBuilder
}

func NewInEntitySystemTableColumnsSectionString(builder *array.RecordBuilder, parent *InEntitySystemTableColumns) (inst *InEntitySystemTableColumnsSectionString) {
	inst = &InEntitySystemTableColumnsSectionString{}
	inAttr := NewInEntitySystemTableColumnsSectionStringInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder011 = builder.Field(11).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder011 = builder.Field(11).(*array.ListBuilder)

	return inst
}
func (inst *InEntitySystemTableColumnsSectionString) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntitySystemTableColumnsSectionString) BeginAttribute(value11 string) *InEntitySystemTableColumnsSectionStringInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder011.Append(value11)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntitySystemTableColumnsSectionString) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntitySystemTableColumnsSectionString) EndSection() *InEntitySystemTableColumns {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntitySystemTableColumnsSectionString) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntitySystemTableColumnsSectionString) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntitySystemTableColumnsSectionString) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntitySystemTableColumnsSectionString) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntitySystemTableColumnsSectionStringInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntitySystemTableColumnsSectionString
	scalarFieldBuilder011            *array.StringBuilder
	scalarListBuilder011             *array.ListBuilder
	membershipFieldBuilder012        *array.Uint64Builder
	membershipListBuilder012         *array.ListBuilder
	membershipFieldBuilder013        *array.Uint64Builder
	membershipListBuilder013         *array.ListBuilder
	membershipFieldBuilder014        *array.Uint64Builder
	membershipListBuilder014         *array.ListBuilder
	membershipFieldBuilder015        *array.BinaryBuilder
	membershipListBuilder015         *array.ListBuilder
	membershipSupportFieldBuilder016 *array.Uint64Builder
	membershipSupportListBuilder016  *array.ListBuilder
	membershipSupportFieldBuilder017 *array.Uint64Builder
	membershipSupportListBuilder017  *array.ListBuilder
	membershipSupportFieldBuilder018 *array.Uint64Builder
	membershipSupportListBuilder018  *array.ListBuilder

	membershipContainerLength012 int

	membershipContainerLength013 int

	membershipContainerLength014 int

	membershipContainerLength015 int
}

func NewInEntitySystemTableColumnsSectionStringInAttr(builder *array.RecordBuilder, parent *InEntitySystemTableColumnsSectionString) (inst *InEntitySystemTableColumnsSectionStringInAttr) {
	inst = &InEntitySystemTableColumnsSectionStringInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder011 = builder.Field(11).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder011 = builder.Field(11).(*array.ListBuilder)
	inst.membershipFieldBuilder012 = builder.Field(12).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder012 = builder.Field(12).(*array.ListBuilder)
	inst.membershipFieldBuilder013 = builder.Field(13).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder013 = builder.Field(13).(*array.ListBuilder)
	inst.membershipFieldBuilder014 = builder.Field(14).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder014 = builder.Field(14).(*array.ListBuilder)
	inst.membershipFieldBuilder015 = builder.Field(15).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder015 = builder.Field(15).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder016 = builder.Field(16).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder016 = builder.Field(16).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder017 = builder.Field(17).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder017 = builder.Field(17).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder018 = builder.Field(18).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder018 = builder.Field(18).(*array.ListBuilder)

	return inst
}
func (inst *InEntitySystemTableColumnsSectionStringInAttr) beginAttribute() {
	inst.membershipListBuilder012.Append(true)
	inst.membershipListBuilder013.Append(true)
	inst.membershipListBuilder014.Append(true)
	inst.membershipListBuilder015.Append(true)
	inst.membershipContainerLength012 = 0
	inst.membershipContainerLength013 = 0
	inst.membershipContainerLength014 = 0
	inst.membershipContainerLength015 = 0
	inst.scalarListBuilder011.Append(true)
	inst.membershipSupportListBuilder016.Append(true)
	inst.membershipSupportListBuilder017.Append(true)
	inst.membershipSupportListBuilder018.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntitySystemTableColumnsSectionStringInAttr) AddMembershipHighCardRef(hr12 uint64) *InEntitySystemTableColumnsSectionStringInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder012.Append(hr12)
	inst.membershipContainerLength012++
	return inst
}
func (inst *InEntitySystemTableColumnsSectionStringInAttr) AddMembershipHighCardRefP(hr12 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder012.Append(hr12)
	inst.membershipContainerLength012++
	return
}
func (inst *InEntitySystemTableColumnsSectionStringInAttr) AddMembershipLowCardRef(lr13 uint64) *InEntitySystemTableColumnsSectionStringInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder013.Append(lr13)
	inst.membershipContainerLength013++
	return inst
}
func (inst *InEntitySystemTableColumnsSectionStringInAttr) AddMembershipLowCardRefP(lr13 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder013.Append(lr13)
	inst.membershipContainerLength013++
	return
}
func (inst *InEntitySystemTableColumnsSectionStringInAttr) AddMembershipMixedLowCardRef(lmr14 uint64, mrhp15 []byte) *InEntitySystemTableColumnsSectionStringInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder014.Append(lmr14)
	inst.membershipFieldBuilder015.Append(mrhp15)
	inst.membershipContainerLength014++
	inst.membershipContainerLength015++
	return inst
}
func (inst *InEntitySystemTableColumnsSectionStringInAttr) AddMembershipMixedLowCardRefP(lmr14 uint64, mrhp15 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder014.Append(lmr14)
	inst.membershipFieldBuilder015.Append(mrhp15)
	inst.membershipContainerLength014++
	inst.membershipContainerLength015++
	return
}
func (inst *InEntitySystemTableColumnsSectionStringInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength012
	inst.membershipContainerLength012 = 0
	inst.membershipSupportFieldBuilder016.Append(uint64(l))
	l = inst.membershipContainerLength013
	inst.membershipContainerLength013 = 0
	inst.membershipSupportFieldBuilder017.Append(uint64(l))
	l = inst.membershipContainerLength014
	inst.membershipContainerLength014 = 0
	inst.membershipSupportFieldBuilder018.Append(uint64(l))
}
func (inst *InEntitySystemTableColumnsSectionStringInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntitySystemTableColumnsSectionStringInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntitySystemTableColumnsSectionStringInAttr) EndSection() *InEntitySystemTableColumns {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntitySystemTableColumnsSectionStringInAttr) EndAttribute() *InEntitySystemTableColumnsSectionString {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}

func (inst *InEntitySystemTableColumnsSectionStringInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntitySystemTableColumnsSectionStringInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntitySystemTableColumnsSectionSymbol struct {
	errs                  []error
	inAttr                *InEntitySystemTableColumnsSectionSymbolInAttr
	state                 runtime.EntityStateE
	parent                *InEntitySystemTableColumns
	scalarFieldBuilder003 *array.StringBuilder
	scalarListBuilder003  *array.ListBuilder
}

func NewInEntitySystemTableColumnsSectionSymbol(builder *array.RecordBuilder, parent *InEntitySystemTableColumns) (inst *InEntitySystemTableColumnsSectionSymbol) {
	inst = &InEntitySystemTableColumnsSectionSymbol{}
	inAttr := NewInEntitySystemTableColumnsSectionSymbolInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder003 = builder.Field(3).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder003 = builder.Field(3).(*array.ListBuilder)

	return inst
}
func (inst *InEntitySystemTableColumnsSectionSymbol) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntitySystemTableColumnsSectionSymbol) BeginAttribute(value3 string) *InEntitySystemTableColumnsSectionSymbolInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder003.Append(value3)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntitySystemTableColumnsSectionSymbol) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntitySystemTableColumnsSectionSymbol) EndSection() *InEntitySystemTableColumns {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntitySystemTableColumnsSectionSymbol) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntitySystemTableColumnsSectionSymbol) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntitySystemTableColumnsSectionSymbol) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntitySystemTableColumnsSectionSymbol) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntitySystemTableColumnsSectionSymbolInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntitySystemTableColumnsSectionSymbol
	scalarFieldBuilder003            *array.StringBuilder
	scalarListBuilder003             *array.ListBuilder
	membershipFieldBuilder004        *array.Uint64Builder
	membershipListBuilder004         *array.ListBuilder
	membershipFieldBuilder005        *array.Uint64Builder
	membershipListBuilder005         *array.ListBuilder
	membershipFieldBuilder006        *array.Uint64Builder
	membershipListBuilder006         *array.ListBuilder
	membershipFieldBuilder007        *array.BinaryBuilder
	membershipListBuilder007         *array.ListBuilder
	membershipSupportFieldBuilder008 *array.Uint64Builder
	membershipSupportListBuilder008  *array.ListBuilder
	membershipSupportFieldBuilder009 *array.Uint64Builder
	membershipSupportListBuilder009  *array.ListBuilder
	membershipSupportFieldBuilder010 *array.Uint64Builder
	membershipSupportListBuilder010  *array.ListBuilder

	membershipContainerLength004 int

	membershipContainerLength005 int

	membershipContainerLength006 int

	membershipContainerLength007 int
}

func NewInEntitySystemTableColumnsSectionSymbolInAttr(builder *array.RecordBuilder, parent *InEntitySystemTableColumnsSectionSymbol) (inst *InEntitySystemTableColumnsSectionSymbolInAttr) {
	inst = &InEntitySystemTableColumnsSectionSymbolInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder003 = builder.Field(3).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder003 = builder.Field(3).(*array.ListBuilder)
	inst.membershipFieldBuilder004 = builder.Field(4).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder004 = builder.Field(4).(*array.ListBuilder)
	inst.membershipFieldBuilder005 = builder.Field(5).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder005 = builder.Field(5).(*array.ListBuilder)
	inst.membershipFieldBuilder006 = builder.Field(6).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder006 = builder.Field(6).(*array.ListBuilder)
	inst.membershipFieldBuilder007 = builder.Field(7).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder007 = builder.Field(7).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder008 = builder.Field(8).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder008 = builder.Field(8).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder009 = builder.Field(9).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder009 = builder.Field(9).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder010 = builder.Field(10).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder010 = builder.Field(10).(*array.ListBuilder)

	return inst
}
func (inst *InEntitySystemTableColumnsSectionSymbolInAttr) beginAttribute() {
	inst.membershipListBuilder004.Append(true)
	inst.membershipListBuilder005.Append(true)
	inst.membershipListBuilder006.Append(true)
	inst.membershipListBuilder007.Append(true)
	inst.membershipContainerLength004 = 0
	inst.membershipContainerLength005 = 0
	inst.membershipContainerLength006 = 0
	inst.membershipContainerLength007 = 0
	inst.scalarListBuilder003.Append(true)
	inst.membershipSupportListBuilder008.Append(true)
	inst.membershipSupportListBuilder009.Append(true)
	inst.membershipSupportListBuilder010.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntitySystemTableColumnsSectionSymbolInAttr) AddMembershipHighCardRef(hr4 uint64) *InEntitySystemTableColumnsSectionSymbolInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder004.Append(hr4)
	inst.membershipContainerLength004++
	return inst
}
func (inst *InEntitySystemTableColumnsSectionSymbolInAttr) AddMembershipHighCardRefP(hr4 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder004.Append(hr4)
	inst.membershipContainerLength004++
	return
}
func (inst *InEntitySystemTableColumnsSectionSymbolInAttr) AddMembershipLowCardRef(lr5 uint64) *InEntitySystemTableColumnsSectionSymbolInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder005.Append(lr5)
	inst.membershipContainerLength005++
	return inst
}
func (inst *InEntitySystemTableColumnsSectionSymbolInAttr) AddMembershipLowCardRefP(lr5 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder005.Append(lr5)
	inst.membershipContainerLength005++
	return
}
func (inst *InEntitySystemTableColumnsSectionSymbolInAttr) AddMembershipMixedLowCardRef(lmr6 uint64, mrhp7 []byte) *InEntitySystemTableColumnsSectionSymbolInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder006.Append(lmr6)
	inst.membershipFieldBuilder007.Append(mrhp7)
	inst.membershipContainerLength006++
	inst.membershipContainerLength007++
	return inst
}
func (inst *InEntitySystemTableColumnsSectionSymbolInAttr) AddMembershipMixedLowCardRefP(lmr6 uint64, mrhp7 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder006.Append(lmr6)
	inst.membershipFieldBuilder007.Append(mrhp7)
	inst.membershipContainerLength006++
	inst.membershipContainerLength007++
	return
}
func (inst *InEntitySystemTableColumnsSectionSymbolInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength004
	inst.membershipContainerLength004 = 0
	inst.membershipSupportFieldBuilder008.Append(uint64(l))
	l = inst.membershipContainerLength005
	inst.membershipContainerLength005 = 0
	inst.membershipSupportFieldBuilder009.Append(uint64(l))
	l = inst.membershipContainerLength006
	inst.membershipContainerLength006 = 0
	inst.membershipSupportFieldBuilder010.Append(uint64(l))
}
func (inst *InEntitySystemTableColumnsSectionSymbolInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntitySystemTableColumnsSectionSymbolInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntitySystemTableColumnsSectionSymbolInAttr) EndSection() *InEntitySystemTableColumns {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntitySystemTableColumnsSectionSymbolInAttr) EndAttribute() *InEntitySystemTableColumnsSectionSymbol {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}

func (inst *InEntitySystemTableColumnsSectionSymbolInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntitySystemTableColumnsSectionSymbolInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntitySystemTableColumnsSectionText struct {
	errs                  []error
	inAttr                *InEntitySystemTableColumnsSectionTextInAttr
	state                 runtime.EntityStateE
	parent                *InEntitySystemTableColumns
	scalarFieldBuilder027 *array.StringBuilder
	scalarListBuilder027  *array.ListBuilder
}

func NewInEntitySystemTableColumnsSectionText(builder *array.RecordBuilder, parent *InEntitySystemTableColumns) (inst *InEntitySystemTableColumnsSectionText) {
	inst = &InEntitySystemTableColumnsSectionText{}
	inAttr := NewInEntitySystemTableColumnsSectionTextInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder027 = builder.Field(27).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder027 = builder.Field(27).(*array.ListBuilder)

	return inst
}
func (inst *InEntitySystemTableColumnsSectionText) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntitySystemTableColumnsSectionText) BeginAttribute(value27 string) *InEntitySystemTableColumnsSectionTextInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder027.Append(value27)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntitySystemTableColumnsSectionText) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntitySystemTableColumnsSectionText) EndSection() *InEntitySystemTableColumns {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntitySystemTableColumnsSectionText) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntitySystemTableColumnsSectionText) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntitySystemTableColumnsSectionText) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntitySystemTableColumnsSectionText) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntitySystemTableColumnsSectionTextInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntitySystemTableColumnsSectionText
	scalarFieldBuilder027            *array.StringBuilder
	scalarListBuilder027             *array.ListBuilder
	membershipFieldBuilder028        *array.Uint64Builder
	membershipListBuilder028         *array.ListBuilder
	membershipFieldBuilder029        *array.Uint64Builder
	membershipListBuilder029         *array.ListBuilder
	membershipFieldBuilder030        *array.Uint64Builder
	membershipListBuilder030         *array.ListBuilder
	membershipFieldBuilder031        *array.BinaryBuilder
	membershipListBuilder031         *array.ListBuilder
	membershipSupportFieldBuilder032 *array.Uint64Builder
	membershipSupportListBuilder032  *array.ListBuilder
	membershipSupportFieldBuilder033 *array.Uint64Builder
	membershipSupportListBuilder033  *array.ListBuilder
	membershipSupportFieldBuilder034 *array.Uint64Builder
	membershipSupportListBuilder034  *array.ListBuilder

	membershipContainerLength028 int

	membershipContainerLength029 int

	membershipContainerLength030 int

	membershipContainerLength031 int
}

func NewInEntitySystemTableColumnsSectionTextInAttr(builder *array.RecordBuilder, parent *InEntitySystemTableColumnsSectionText) (inst *InEntitySystemTableColumnsSectionTextInAttr) {
	inst = &InEntitySystemTableColumnsSectionTextInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder027 = builder.Field(27).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder027 = builder.Field(27).(*array.ListBuilder)
	inst.membershipFieldBuilder028 = builder.Field(28).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder028 = builder.Field(28).(*array.ListBuilder)
	inst.membershipFieldBuilder029 = builder.Field(29).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder029 = builder.Field(29).(*array.ListBuilder)
	inst.membershipFieldBuilder030 = builder.Field(30).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder030 = builder.Field(30).(*array.ListBuilder)
	inst.membershipFieldBuilder031 = builder.Field(31).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder031 = builder.Field(31).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder032 = builder.Field(32).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder032 = builder.Field(32).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder033 = builder.Field(33).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder033 = builder.Field(33).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder034 = builder.Field(34).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder034 = builder.Field(34).(*array.ListBuilder)

	return inst
}
func (inst *InEntitySystemTableColumnsSectionTextInAttr) beginAttribute() {
	inst.membershipListBuilder028.Append(true)
	inst.membershipListBuilder029.Append(true)
	inst.membershipListBuilder030.Append(true)
	inst.membershipListBuilder031.Append(true)
	inst.membershipContainerLength028 = 0
	inst.membershipContainerLength029 = 0
	inst.membershipContainerLength030 = 0
	inst.membershipContainerLength031 = 0
	inst.scalarListBuilder027.Append(true)
	inst.membershipSupportListBuilder032.Append(true)
	inst.membershipSupportListBuilder033.Append(true)
	inst.membershipSupportListBuilder034.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntitySystemTableColumnsSectionTextInAttr) AddMembershipHighCardRef(hr28 uint64) *InEntitySystemTableColumnsSectionTextInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder028.Append(hr28)
	inst.membershipContainerLength028++
	return inst
}
func (inst *InEntitySystemTableColumnsSectionTextInAttr) AddMembershipHighCardRefP(hr28 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder028.Append(hr28)
	inst.membershipContainerLength028++
	return
}
func (inst *InEntitySystemTableColumnsSectionTextInAttr) AddMembershipLowCardRef(lr29 uint64) *InEntitySystemTableColumnsSectionTextInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder029.Append(lr29)
	inst.membershipContainerLength029++
	return inst
}
func (inst *InEntitySystemTableColumnsSectionTextInAttr) AddMembershipLowCardRefP(lr29 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder029.Append(lr29)
	inst.membershipContainerLength029++
	return
}
func (inst *InEntitySystemTableColumnsSectionTextInAttr) AddMembershipMixedLowCardRef(lmr30 uint64, mrhp31 []byte) *InEntitySystemTableColumnsSectionTextInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder030.Append(lmr30)
	inst.membershipFieldBuilder031.Append(mrhp31)
	inst.membershipContainerLength030++
	inst.membershipContainerLength031++
	return inst
}
func (inst *InEntitySystemTableColumnsSectionTextInAttr) AddMembershipMixedLowCardRefP(lmr30 uint64, mrhp31 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder030.Append(lmr30)
	inst.membershipFieldBuilder031.Append(mrhp31)
	inst.membershipContainerLength030++
	inst.membershipContainerLength031++
	return
}
func (inst *InEntitySystemTableColumnsSectionTextInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength028
	inst.membershipContainerLength028 = 0
	inst.membershipSupportFieldBuilder032.Append(uint64(l))
	l = inst.membershipContainerLength029
	inst.membershipContainerLength029 = 0
	inst.membershipSupportFieldBuilder033.Append(uint64(l))
	l = inst.membershipContainerLength030
	inst.membershipContainerLength030 = 0
	inst.membershipSupportFieldBuilder034.Append(uint64(l))
}
func (inst *InEntitySystemTableColumnsSectionTextInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntitySystemTableColumnsSectionTextInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntitySystemTableColumnsSectionTextInAttr) EndSection() *InEntitySystemTableColumns {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntitySystemTableColumnsSectionTextInAttr) EndAttribute() *InEntitySystemTableColumnsSectionText {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}

func (inst *InEntitySystemTableColumnsSectionTextInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntitySystemTableColumnsSectionTextInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntitySystemTableColumnsSectionU64 struct {
	errs                  []error
	inAttr                *InEntitySystemTableColumnsSectionU64InAttr
	state                 runtime.EntityStateE
	parent                *InEntitySystemTableColumns
	scalarFieldBuilder019 *array.Uint64Builder
	scalarListBuilder019  *array.ListBuilder
}

func NewInEntitySystemTableColumnsSectionU64(builder *array.RecordBuilder, parent *InEntitySystemTableColumns) (inst *InEntitySystemTableColumnsSectionU64) {
	inst = &InEntitySystemTableColumnsSectionU64{}
	inAttr := NewInEntitySystemTableColumnsSectionU64InAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder019 = builder.Field(19).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.scalarListBuilder019 = builder.Field(19).(*array.ListBuilder)

	return inst
}
func (inst *InEntitySystemTableColumnsSectionU64) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntitySystemTableColumnsSectionU64) BeginAttribute(value19 uint64) *InEntitySystemTableColumnsSectionU64InAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder019.Append(value19)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntitySystemTableColumnsSectionU64) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntitySystemTableColumnsSectionU64) EndSection() *InEntitySystemTableColumns {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntitySystemTableColumnsSectionU64) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntitySystemTableColumnsSectionU64) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntitySystemTableColumnsSectionU64) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntitySystemTableColumnsSectionU64) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntitySystemTableColumnsSectionU64InAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntitySystemTableColumnsSectionU64
	scalarFieldBuilder019            *array.Uint64Builder
	scalarListBuilder019             *array.ListBuilder
	membershipFieldBuilder020        *array.Uint64Builder
	membershipListBuilder020         *array.ListBuilder
	membershipFieldBuilder021        *array.Uint64Builder
	membershipListBuilder021         *array.ListBuilder
	membershipFieldBuilder022        *array.Uint64Builder
	membershipListBuilder022         *array.ListBuilder
	membershipFieldBuilder023        *array.BinaryBuilder
	membershipListBuilder023         *array.ListBuilder
	membershipSupportFieldBuilder024 *array.Uint64Builder
	membershipSupportListBuilder024  *array.ListBuilder
	membershipSupportFieldBuilder025 *array.Uint64Builder
	membershipSupportListBuilder025  *array.ListBuilder
	membershipSupportFieldBuilder026 *array.Uint64Builder
	membershipSupportListBuilder026  *array.ListBuilder

	membershipContainerLength020 int

	membershipContainerLength021 int

	membershipContainerLength022 int

	membershipContainerLength023 int
}

func NewInEntitySystemTableColumnsSectionU64InAttr(builder *array.RecordBuilder, parent *InEntitySystemTableColumnsSectionU64) (inst *InEntitySystemTableColumnsSectionU64InAttr) {
	inst = &InEntitySystemTableColumnsSectionU64InAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder019 = builder.Field(19).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.scalarListBuilder019 = builder.Field(19).(*array.ListBuilder)
	inst.membershipFieldBuilder020 = builder.Field(20).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder020 = builder.Field(20).(*array.ListBuilder)
	inst.membershipFieldBuilder021 = builder.Field(21).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder021 = builder.Field(21).(*array.ListBuilder)
	inst.membershipFieldBuilder022 = builder.Field(22).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder022 = builder.Field(22).(*array.ListBuilder)
	inst.membershipFieldBuilder023 = builder.Field(23).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder023 = builder.Field(23).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder024 = builder.Field(24).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder024 = builder.Field(24).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder025 = builder.Field(25).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder025 = builder.Field(25).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder026 = builder.Field(26).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder026 = builder.Field(26).(*array.ListBuilder)

	return inst
}
func (inst *InEntitySystemTableColumnsSectionU64InAttr) beginAttribute() {
	inst.membershipListBuilder020.Append(true)
	inst.membershipListBuilder021.Append(true)
	inst.membershipListBuilder022.Append(true)
	inst.membershipListBuilder023.Append(true)
	inst.membershipContainerLength020 = 0
	inst.membershipContainerLength021 = 0
	inst.membershipContainerLength022 = 0
	inst.membershipContainerLength023 = 0
	inst.scalarListBuilder019.Append(true)
	inst.membershipSupportListBuilder024.Append(true)
	inst.membershipSupportListBuilder025.Append(true)
	inst.membershipSupportListBuilder026.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntitySystemTableColumnsSectionU64InAttr) AddMembershipHighCardRef(hr20 uint64) *InEntitySystemTableColumnsSectionU64InAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder020.Append(hr20)
	inst.membershipContainerLength020++
	return inst
}
func (inst *InEntitySystemTableColumnsSectionU64InAttr) AddMembershipHighCardRefP(hr20 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder020.Append(hr20)
	inst.membershipContainerLength020++
	return
}
func (inst *InEntitySystemTableColumnsSectionU64InAttr) AddMembershipLowCardRef(lr21 uint64) *InEntitySystemTableColumnsSectionU64InAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder021.Append(lr21)
	inst.membershipContainerLength021++
	return inst
}
func (inst *InEntitySystemTableColumnsSectionU64InAttr) AddMembershipLowCardRefP(lr21 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder021.Append(lr21)
	inst.membershipContainerLength021++
	return
}
func (inst *InEntitySystemTableColumnsSectionU64InAttr) AddMembershipMixedLowCardRef(lmr22 uint64, mrhp23 []byte) *InEntitySystemTableColumnsSectionU64InAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder022.Append(lmr22)
	inst.membershipFieldBuilder023.Append(mrhp23)
	inst.membershipContainerLength022++
	inst.membershipContainerLength023++
	return inst
}
func (inst *InEntitySystemTableColumnsSectionU64InAttr) AddMembershipMixedLowCardRefP(lmr22 uint64, mrhp23 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder022.Append(lmr22)
	inst.membershipFieldBuilder023.Append(mrhp23)
	inst.membershipContainerLength022++
	inst.membershipContainerLength023++
	return
}
func (inst *InEntitySystemTableColumnsSectionU64InAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength020
	inst.membershipContainerLength020 = 0
	inst.membershipSupportFieldBuilder024.Append(uint64(l))
	l = inst.membershipContainerLength021
	inst.membershipContainerLength021 = 0
	inst.membershipSupportFieldBuilder025.Append(uint64(l))
	l = inst.membershipContainerLength022
	inst.membershipContainerLength022 = 0
	inst.membershipSupportFieldBuilder026.Append(uint64(l))
}
func (inst *InEntitySystemTableColumnsSectionU64InAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntitySystemTableColumnsSectionU64InAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntitySystemTableColumnsSectionU64InAttr) EndSection() *InEntitySystemTableColumns {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntitySystemTableColumnsSectionU64InAttr) EndAttribute() *InEntitySystemTableColumnsSectionU64 {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}

func (inst *InEntitySystemTableColumnsSectionU64InAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntitySystemTableColumnsSectionU64InAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}
