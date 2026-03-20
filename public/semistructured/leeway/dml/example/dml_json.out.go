// Code generated; Leeway DML (github.com/stergiotis/boxer/public/semistructured/leeway/dml.test) DO NOT EDIT.

package example

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

func CreateSchemaJson() (schema *arrow.Schema) {
	schema = arrow.NewSchema([]arrow.Field{
		/* 000 */ arrow.Field{Name: "id:blake3hash:y:g:0:0:", Nullable: false, Type: &arrow.BinaryType{}},
		/* 001 */ arrow.Field{Name: "tv:strings:semantic-type:val:s:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 002 */ arrow.Field{Name: "tv:strings:short:val:s:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 003 */ arrow.Field{Name: "tv:strings:long:val:s:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 004 */ arrow.Field{Name: "tv:strings:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 005 */ arrow.Field{Name: "tv:strings:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 006 */ arrow.Field{Name: "tv:strings:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 007 */ arrow.Field{Name: "tv:bool:value:val:b:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BooleanType{})},
		/* 008 */ arrow.Field{Name: "tv:bool:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 009 */ arrow.Field{Name: "tv:bool:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 010 */ arrow.Field{Name: "tv:bool:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 011 */ arrow.Field{Name: "tv:undefined:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 012 */ arrow.Field{Name: "tv:undefined:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 013 */ arrow.Field{Name: "tv:undefined:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 014 */ arrow.Field{Name: "tv:null:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 015 */ arrow.Field{Name: "tv:null:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 016 */ arrow.Field{Name: "tv:null:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 017 */ arrow.Field{Name: "tv:string:value:val:s:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 018 */ arrow.Field{Name: "tv:string:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 019 */ arrow.Field{Name: "tv:string:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 020 */ arrow.Field{Name: "tv:string:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 021 */ arrow.Field{Name: "tv:symbol:value:val:s:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 022 */ arrow.Field{Name: "tv:symbol:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 023 */ arrow.Field{Name: "tv:symbol:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 024 */ arrow.Field{Name: "tv:symbol:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 025 */ arrow.Field{Name: "tv:float64:value:val:f64:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Float64)},
		/* 026 */ arrow.Field{Name: "tv:float64:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 027 */ arrow.Field{Name: "tv:float64:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 028 */ arrow.Field{Name: "tv:float64:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 029 */ arrow.Field{Name: "tv:int64:value:val:i64:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Int64)},
		/* 030 */ arrow.Field{Name: "tv:int64:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 031 */ arrow.Field{Name: "tv:int64:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 032 */ arrow.Field{Name: "tv:int64:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
	}, nil)
	return
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityClassAndFactoryCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1175

type InEntityJson struct {
	errs                  []error
	state                 runtime.EntityStateE
	allocator             memory.Allocator
	builder               *array.RecordBuilder
	records               []arrow.Record
	section00Inst         *InEntityJsonSectionBool
	section00State        runtime.EntityStateE
	section01Inst         *InEntityJsonSectionFloat64
	section01State        runtime.EntityStateE
	section02Inst         *InEntityJsonSectionInt64
	section02State        runtime.EntityStateE
	section03Inst         *InEntityJsonSectionNull
	section03State        runtime.EntityStateE
	section04Inst         *InEntityJsonSectionString
	section04State        runtime.EntityStateE
	section05Inst         *InEntityJsonSectionStrings
	section05State        runtime.EntityStateE
	section06Inst         *InEntityJsonSectionSymbol
	section06State        runtime.EntityStateE
	section07Inst         *InEntityJsonSectionUndefined
	section07State        runtime.EntityStateE
	plainBlake3hash0      []byte
	scalarFieldBuilder000 *array.BinaryBuilder
}

func NewInEntityJson(allocator memory.Allocator, estimatedNumberOfRecords int) (inst *InEntityJson) {
	inst = &InEntityJson{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.allocator = allocator
	inst.records = make([]arrow.Record, 0, estimatedNumberOfRecords)
	schema := CreateSchemaJson()
	builder := array.NewRecordBuilder(allocator, schema)
	inst.builder = builder
	inst.initSections(builder)
	inst.scalarFieldBuilder000 = builder.Field(0).(*array.BinaryBuilder)

	return inst
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1289

func (inst *InEntityJson) SetId(blake3hash0 []byte) *InEntityJson {
	if inst.state != runtime.EntityStateInEntity {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.plainBlake3hash0 = blake3hash0

	return inst
}
func (inst *InEntityJson) appendPlainValues() {
	inst.scalarFieldBuilder000.Append(inst.plainBlake3hash0)
}
func (inst *InEntityJson) resetPlainValues() {
	inst.plainBlake3hash0 = []byte(nil)
}
func (inst *InEntityJson) initSections(builder *array.RecordBuilder) {
	inst.section00Inst = NewInEntityJsonSectionBool(builder, inst)
	inst.section01Inst = NewInEntityJsonSectionFloat64(builder, inst)
	inst.section02Inst = NewInEntityJsonSectionInt64(builder, inst)
	inst.section03Inst = NewInEntityJsonSectionNull(builder, inst)
	inst.section04Inst = NewInEntityJsonSectionString(builder, inst)
	inst.section05Inst = NewInEntityJsonSectionStrings(builder, inst)
	inst.section06Inst = NewInEntityJsonSectionSymbol(builder, inst)
	inst.section07Inst = NewInEntityJsonSectionUndefined(builder, inst)
}
func (inst *InEntityJson) beginSections() {
	inst.section00Inst.beginSection()
	inst.section01Inst.beginSection()
	inst.section02Inst.beginSection()
	inst.section03Inst.beginSection()
	inst.section04Inst.beginSection()
	inst.section05Inst.beginSection()
	inst.section06Inst.beginSection()
	inst.section07Inst.beginSection()
}
func (inst *InEntityJson) resetSections() {
	inst.section00Inst.resetSection()
	inst.section01Inst.resetSection()
	inst.section02Inst.resetSection()
	inst.section03Inst.resetSection()
	inst.section04Inst.resetSection()
	inst.section05Inst.resetSection()
	inst.section06Inst.resetSection()
	inst.section07Inst.resetSection()
}
func (inst *InEntityJson) CheckErrors() (err error) {
	err = eh.CheckErrors(inst.errs)
	err = errors.Join(err, inst.section00Inst.CheckErrors())
	err = errors.Join(err, inst.section01Inst.CheckErrors())
	err = errors.Join(err, inst.section02Inst.CheckErrors())
	err = errors.Join(err, inst.section03Inst.CheckErrors())
	err = errors.Join(err, inst.section04Inst.CheckErrors())
	err = errors.Join(err, inst.section05Inst.CheckErrors())
	err = errors.Join(err, inst.section06Inst.CheckErrors())
	err = errors.Join(err, inst.section07Inst.CheckErrors())

	return
}
func (inst *InEntityJson) GetSectionBool() *InEntityJsonSectionBool {
	return inst.section00Inst
}
func (inst *InEntityJson) GetSectionFloat64() *InEntityJsonSectionFloat64 {
	return inst.section01Inst
}
func (inst *InEntityJson) GetSectionInt64() *InEntityJsonSectionInt64 {
	return inst.section02Inst
}
func (inst *InEntityJson) GetSectionNull() *InEntityJsonSectionNull {
	return inst.section03Inst
}
func (inst *InEntityJson) GetSectionString() *InEntityJsonSectionString {
	return inst.section04Inst
}
func (inst *InEntityJson) GetSectionStrings() *InEntityJsonSectionStrings {
	return inst.section05Inst
}
func (inst *InEntityJson) GetSectionSymbol() *InEntityJsonSectionSymbol {
	return inst.section06Inst
}
func (inst *InEntityJson) GetSectionUndefined() *InEntityJsonSectionUndefined {
	return inst.section07Inst
}
func (inst *InEntityJson) BeginEntity() *InEntityJson {
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
func (inst *InEntityJson) validateEntity() {
	{
		state := inst.section00Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "bool").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section01Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "float64").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section02Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "int64").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section03Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "null").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section04Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "string").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section05Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "strings").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section06Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "symbol").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section07Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "undefined").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}

	// FIXME check coSectionGroup consistency
	return
}
func (inst *InEntityJson) CommitEntity() (err error) {
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
func (inst *InEntityJson) RollbackEntity() (err error) {
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
func (inst *InEntityJson) TransferRecords(recordsIn []arrow.Record) (recordsOut []arrow.Record, err error) {
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

func (inst *InEntityJson) GetSchema() (schema *arrow.Schema) {
	return inst.builder.Schema()
}

func (inst *InEntityJson) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJson) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionBool struct {
	errs                  []error
	inAttr                *InEntityJsonSectionBoolInAttr
	state                 runtime.EntityStateE
	parent                *InEntityJson
	scalarFieldBuilder007 *array.BooleanBuilder
	scalarListBuilder007  *array.ListBuilder
}

func NewInEntityJsonSectionBool(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionBool) {
	inst = &InEntityJsonSectionBool{}
	inAttr := NewInEntityJsonSectionBoolInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder007 = builder.Field(7).(*array.ListBuilder).ValueBuilder().(*array.BooleanBuilder)
	inst.scalarListBuilder007 = builder.Field(7).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionBool) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityJsonSectionBool) BeginAttribute(value7 bool) *InEntityJsonSectionBoolInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder007.Append(value7)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityJsonSectionBool) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityJsonSectionBool) EndSection() *InEntityJson {
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

func (inst *InEntityJsonSectionBool) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityJsonSectionBool) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityJsonSectionBool) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionBool) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionBoolInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityJsonSectionBool
	scalarFieldBuilder007            *array.BooleanBuilder
	scalarListBuilder007             *array.ListBuilder
	membershipFieldBuilder008        *array.BinaryBuilder
	membershipListBuilder008         *array.ListBuilder
	membershipFieldBuilder009        *array.BinaryBuilder
	membershipListBuilder009         *array.ListBuilder
	membershipSupportFieldBuilder010 *array.Uint64Builder
	membershipSupportListBuilder010  *array.ListBuilder

	membershipContainerLength008 int

	membershipContainerLength009 int
}

func NewInEntityJsonSectionBoolInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionBool) (inst *InEntityJsonSectionBoolInAttr) {
	inst = &InEntityJsonSectionBoolInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder007 = builder.Field(7).(*array.ListBuilder).ValueBuilder().(*array.BooleanBuilder)
	inst.scalarListBuilder007 = builder.Field(7).(*array.ListBuilder)
	inst.membershipFieldBuilder008 = builder.Field(8).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder008 = builder.Field(8).(*array.ListBuilder)
	inst.membershipFieldBuilder009 = builder.Field(9).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder009 = builder.Field(9).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder010 = builder.Field(10).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder010 = builder.Field(10).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionBoolInAttr) beginAttribute() {
	inst.membershipListBuilder008.Append(true)
	inst.membershipListBuilder009.Append(true)
	inst.membershipContainerLength008 = 0
	inst.membershipContainerLength009 = 0
	inst.scalarListBuilder007.Append(true)
	inst.membershipSupportListBuilder010.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionBoolInAttr) AddMembershipMixedLowCardVerbatim(lmv8 []byte, mvhp9 []byte) *InEntityJsonSectionBoolInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder008.Append(lmv8)
	inst.membershipFieldBuilder009.Append(mvhp9)
	inst.membershipContainerLength008++
	inst.membershipContainerLength009++
	return inst
}
func (inst *InEntityJsonSectionBoolInAttr) AddMembershipMixedLowCardVerbatimP(lmv8 []byte, mvhp9 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder008.Append(lmv8)
	inst.membershipFieldBuilder009.Append(mvhp9)
	inst.membershipContainerLength008++
	inst.membershipContainerLength009++
	return
}
func (inst *InEntityJsonSectionBoolInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength008
	inst.membershipContainerLength008 = 0
	inst.membershipSupportFieldBuilder010.Append(uint64(l))
}
func (inst *InEntityJsonSectionBoolInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityJsonSectionBoolInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityJsonSectionBoolInAttr) EndSection() *InEntityJson {
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
func (inst *InEntityJsonSectionBoolInAttr) EndAttribute() *InEntityJsonSectionBool {
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

func (inst *InEntityJsonSectionBoolInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionBoolInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionFloat64 struct {
	errs                  []error
	inAttr                *InEntityJsonSectionFloat64InAttr
	state                 runtime.EntityStateE
	parent                *InEntityJson
	scalarFieldBuilder025 *array.Float64Builder
	scalarListBuilder025  *array.ListBuilder
}

func NewInEntityJsonSectionFloat64(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionFloat64) {
	inst = &InEntityJsonSectionFloat64{}
	inAttr := NewInEntityJsonSectionFloat64InAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder025 = builder.Field(25).(*array.ListBuilder).ValueBuilder().(*array.Float64Builder)
	inst.scalarListBuilder025 = builder.Field(25).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionFloat64) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityJsonSectionFloat64) BeginAttribute(value25 float64) *InEntityJsonSectionFloat64InAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder025.Append(value25)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityJsonSectionFloat64) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityJsonSectionFloat64) EndSection() *InEntityJson {
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

func (inst *InEntityJsonSectionFloat64) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityJsonSectionFloat64) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityJsonSectionFloat64) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionFloat64) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionFloat64InAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityJsonSectionFloat64
	scalarFieldBuilder025            *array.Float64Builder
	scalarListBuilder025             *array.ListBuilder
	membershipFieldBuilder026        *array.BinaryBuilder
	membershipListBuilder026         *array.ListBuilder
	membershipFieldBuilder027        *array.BinaryBuilder
	membershipListBuilder027         *array.ListBuilder
	membershipSupportFieldBuilder028 *array.Uint64Builder
	membershipSupportListBuilder028  *array.ListBuilder

	membershipContainerLength026 int

	membershipContainerLength027 int
}

func NewInEntityJsonSectionFloat64InAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionFloat64) (inst *InEntityJsonSectionFloat64InAttr) {
	inst = &InEntityJsonSectionFloat64InAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder025 = builder.Field(25).(*array.ListBuilder).ValueBuilder().(*array.Float64Builder)
	inst.scalarListBuilder025 = builder.Field(25).(*array.ListBuilder)
	inst.membershipFieldBuilder026 = builder.Field(26).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder026 = builder.Field(26).(*array.ListBuilder)
	inst.membershipFieldBuilder027 = builder.Field(27).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder027 = builder.Field(27).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder028 = builder.Field(28).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder028 = builder.Field(28).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionFloat64InAttr) beginAttribute() {
	inst.membershipListBuilder026.Append(true)
	inst.membershipListBuilder027.Append(true)
	inst.membershipContainerLength026 = 0
	inst.membershipContainerLength027 = 0
	inst.scalarListBuilder025.Append(true)
	inst.membershipSupportListBuilder028.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionFloat64InAttr) AddMembershipMixedLowCardVerbatim(lmv26 []byte, mvhp27 []byte) *InEntityJsonSectionFloat64InAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder026.Append(lmv26)
	inst.membershipFieldBuilder027.Append(mvhp27)
	inst.membershipContainerLength026++
	inst.membershipContainerLength027++
	return inst
}
func (inst *InEntityJsonSectionFloat64InAttr) AddMembershipMixedLowCardVerbatimP(lmv26 []byte, mvhp27 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder026.Append(lmv26)
	inst.membershipFieldBuilder027.Append(mvhp27)
	inst.membershipContainerLength026++
	inst.membershipContainerLength027++
	return
}
func (inst *InEntityJsonSectionFloat64InAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength026
	inst.membershipContainerLength026 = 0
	inst.membershipSupportFieldBuilder028.Append(uint64(l))
}
func (inst *InEntityJsonSectionFloat64InAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityJsonSectionFloat64InAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityJsonSectionFloat64InAttr) EndSection() *InEntityJson {
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
func (inst *InEntityJsonSectionFloat64InAttr) EndAttribute() *InEntityJsonSectionFloat64 {
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

func (inst *InEntityJsonSectionFloat64InAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionFloat64InAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionInt64 struct {
	errs                  []error
	inAttr                *InEntityJsonSectionInt64InAttr
	state                 runtime.EntityStateE
	parent                *InEntityJson
	scalarFieldBuilder029 *array.Int64Builder
	scalarListBuilder029  *array.ListBuilder
}

func NewInEntityJsonSectionInt64(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionInt64) {
	inst = &InEntityJsonSectionInt64{}
	inAttr := NewInEntityJsonSectionInt64InAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder029 = builder.Field(29).(*array.ListBuilder).ValueBuilder().(*array.Int64Builder)
	inst.scalarListBuilder029 = builder.Field(29).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionInt64) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityJsonSectionInt64) BeginAttribute(value29 int64) *InEntityJsonSectionInt64InAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder029.Append(value29)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityJsonSectionInt64) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityJsonSectionInt64) EndSection() *InEntityJson {
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

func (inst *InEntityJsonSectionInt64) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityJsonSectionInt64) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityJsonSectionInt64) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionInt64) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionInt64InAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityJsonSectionInt64
	scalarFieldBuilder029            *array.Int64Builder
	scalarListBuilder029             *array.ListBuilder
	membershipFieldBuilder030        *array.BinaryBuilder
	membershipListBuilder030         *array.ListBuilder
	membershipFieldBuilder031        *array.BinaryBuilder
	membershipListBuilder031         *array.ListBuilder
	membershipSupportFieldBuilder032 *array.Uint64Builder
	membershipSupportListBuilder032  *array.ListBuilder

	membershipContainerLength030 int

	membershipContainerLength031 int
}

func NewInEntityJsonSectionInt64InAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionInt64) (inst *InEntityJsonSectionInt64InAttr) {
	inst = &InEntityJsonSectionInt64InAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder029 = builder.Field(29).(*array.ListBuilder).ValueBuilder().(*array.Int64Builder)
	inst.scalarListBuilder029 = builder.Field(29).(*array.ListBuilder)
	inst.membershipFieldBuilder030 = builder.Field(30).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder030 = builder.Field(30).(*array.ListBuilder)
	inst.membershipFieldBuilder031 = builder.Field(31).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder031 = builder.Field(31).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder032 = builder.Field(32).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder032 = builder.Field(32).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionInt64InAttr) beginAttribute() {
	inst.membershipListBuilder030.Append(true)
	inst.membershipListBuilder031.Append(true)
	inst.membershipContainerLength030 = 0
	inst.membershipContainerLength031 = 0
	inst.scalarListBuilder029.Append(true)
	inst.membershipSupportListBuilder032.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionInt64InAttr) AddMembershipMixedLowCardVerbatim(lmv30 []byte, mvhp31 []byte) *InEntityJsonSectionInt64InAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder030.Append(lmv30)
	inst.membershipFieldBuilder031.Append(mvhp31)
	inst.membershipContainerLength030++
	inst.membershipContainerLength031++
	return inst
}
func (inst *InEntityJsonSectionInt64InAttr) AddMembershipMixedLowCardVerbatimP(lmv30 []byte, mvhp31 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder030.Append(lmv30)
	inst.membershipFieldBuilder031.Append(mvhp31)
	inst.membershipContainerLength030++
	inst.membershipContainerLength031++
	return
}
func (inst *InEntityJsonSectionInt64InAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength030
	inst.membershipContainerLength030 = 0
	inst.membershipSupportFieldBuilder032.Append(uint64(l))
}
func (inst *InEntityJsonSectionInt64InAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityJsonSectionInt64InAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityJsonSectionInt64InAttr) EndSection() *InEntityJson {
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
func (inst *InEntityJsonSectionInt64InAttr) EndAttribute() *InEntityJsonSectionInt64 {
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

func (inst *InEntityJsonSectionInt64InAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionInt64InAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionNull struct {
	errs   []error
	inAttr *InEntityJsonSectionNullInAttr
	state  runtime.EntityStateE
	parent *InEntityJson
}

func NewInEntityJsonSectionNull(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionNull) {
	inst = &InEntityJsonSectionNull{}
	inAttr := NewInEntityJsonSectionNullInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent

	return inst
}
func (inst *InEntityJsonSectionNull) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityJsonSectionNull) BeginAttribute() *InEntityJsonSectionNullInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityJsonSectionNull) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityJsonSectionNull) EndSection() *InEntityJson {
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

func (inst *InEntityJsonSectionNull) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityJsonSectionNull) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityJsonSectionNull) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionNull) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionNullInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityJsonSectionNull
	membershipFieldBuilder014        *array.BinaryBuilder
	membershipListBuilder014         *array.ListBuilder
	membershipFieldBuilder015        *array.BinaryBuilder
	membershipListBuilder015         *array.ListBuilder
	membershipSupportFieldBuilder016 *array.Uint64Builder
	membershipSupportListBuilder016  *array.ListBuilder

	membershipContainerLength014 int

	membershipContainerLength015 int
}

func NewInEntityJsonSectionNullInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionNull) (inst *InEntityJsonSectionNullInAttr) {
	inst = &InEntityJsonSectionNullInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.membershipFieldBuilder014 = builder.Field(14).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder014 = builder.Field(14).(*array.ListBuilder)
	inst.membershipFieldBuilder015 = builder.Field(15).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder015 = builder.Field(15).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder016 = builder.Field(16).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder016 = builder.Field(16).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionNullInAttr) beginAttribute() {
	inst.membershipListBuilder014.Append(true)
	inst.membershipListBuilder015.Append(true)
	inst.membershipContainerLength014 = 0
	inst.membershipContainerLength015 = 0
	inst.membershipSupportListBuilder016.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionNullInAttr) AddMembershipMixedLowCardVerbatim(lmv14 []byte, mvhp15 []byte) *InEntityJsonSectionNullInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder014.Append(lmv14)
	inst.membershipFieldBuilder015.Append(mvhp15)
	inst.membershipContainerLength014++
	inst.membershipContainerLength015++
	return inst
}
func (inst *InEntityJsonSectionNullInAttr) AddMembershipMixedLowCardVerbatimP(lmv14 []byte, mvhp15 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder014.Append(lmv14)
	inst.membershipFieldBuilder015.Append(mvhp15)
	inst.membershipContainerLength014++
	inst.membershipContainerLength015++
	return
}
func (inst *InEntityJsonSectionNullInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength014
	inst.membershipContainerLength014 = 0
	inst.membershipSupportFieldBuilder016.Append(uint64(l))
}
func (inst *InEntityJsonSectionNullInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityJsonSectionNullInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityJsonSectionNullInAttr) EndSection() *InEntityJson {
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
func (inst *InEntityJsonSectionNullInAttr) EndAttribute() *InEntityJsonSectionNull {
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

func (inst *InEntityJsonSectionNullInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionNullInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionString struct {
	errs                  []error
	inAttr                *InEntityJsonSectionStringInAttr
	state                 runtime.EntityStateE
	parent                *InEntityJson
	scalarFieldBuilder017 *array.StringBuilder
	scalarListBuilder017  *array.ListBuilder
}

func NewInEntityJsonSectionString(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionString) {
	inst = &InEntityJsonSectionString{}
	inAttr := NewInEntityJsonSectionStringInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder017 = builder.Field(17).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder017 = builder.Field(17).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionString) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityJsonSectionString) BeginAttribute(value17 string) *InEntityJsonSectionStringInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder017.Append(value17)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityJsonSectionString) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityJsonSectionString) EndSection() *InEntityJson {
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

func (inst *InEntityJsonSectionString) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityJsonSectionString) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityJsonSectionString) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionString) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionStringInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityJsonSectionString
	scalarFieldBuilder017            *array.StringBuilder
	scalarListBuilder017             *array.ListBuilder
	membershipFieldBuilder018        *array.BinaryBuilder
	membershipListBuilder018         *array.ListBuilder
	membershipFieldBuilder019        *array.BinaryBuilder
	membershipListBuilder019         *array.ListBuilder
	membershipSupportFieldBuilder020 *array.Uint64Builder
	membershipSupportListBuilder020  *array.ListBuilder

	membershipContainerLength018 int

	membershipContainerLength019 int
}

func NewInEntityJsonSectionStringInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionString) (inst *InEntityJsonSectionStringInAttr) {
	inst = &InEntityJsonSectionStringInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder017 = builder.Field(17).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder017 = builder.Field(17).(*array.ListBuilder)
	inst.membershipFieldBuilder018 = builder.Field(18).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder018 = builder.Field(18).(*array.ListBuilder)
	inst.membershipFieldBuilder019 = builder.Field(19).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder019 = builder.Field(19).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder020 = builder.Field(20).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder020 = builder.Field(20).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionStringInAttr) beginAttribute() {
	inst.membershipListBuilder018.Append(true)
	inst.membershipListBuilder019.Append(true)
	inst.membershipContainerLength018 = 0
	inst.membershipContainerLength019 = 0
	inst.scalarListBuilder017.Append(true)
	inst.membershipSupportListBuilder020.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionStringInAttr) AddMembershipMixedLowCardVerbatim(lmv18 []byte, mvhp19 []byte) *InEntityJsonSectionStringInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder018.Append(lmv18)
	inst.membershipFieldBuilder019.Append(mvhp19)
	inst.membershipContainerLength018++
	inst.membershipContainerLength019++
	return inst
}
func (inst *InEntityJsonSectionStringInAttr) AddMembershipMixedLowCardVerbatimP(lmv18 []byte, mvhp19 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder018.Append(lmv18)
	inst.membershipFieldBuilder019.Append(mvhp19)
	inst.membershipContainerLength018++
	inst.membershipContainerLength019++
	return
}
func (inst *InEntityJsonSectionStringInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength018
	inst.membershipContainerLength018 = 0
	inst.membershipSupportFieldBuilder020.Append(uint64(l))
}
func (inst *InEntityJsonSectionStringInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityJsonSectionStringInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityJsonSectionStringInAttr) EndSection() *InEntityJson {
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
func (inst *InEntityJsonSectionStringInAttr) EndAttribute() *InEntityJsonSectionString {
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

func (inst *InEntityJsonSectionStringInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionStringInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionStrings struct {
	errs                  []error
	inAttr                *InEntityJsonSectionStringsInAttr
	state                 runtime.EntityStateE
	parent                *InEntityJson
	scalarFieldBuilder001 *array.StringBuilder
	scalarListBuilder001  *array.ListBuilder
	scalarFieldBuilder002 *array.StringBuilder
	scalarListBuilder002  *array.ListBuilder
	scalarFieldBuilder003 *array.StringBuilder
	scalarListBuilder003  *array.ListBuilder
}

func NewInEntityJsonSectionStrings(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionStrings) {
	inst = &InEntityJsonSectionStrings{}
	inAttr := NewInEntityJsonSectionStringsInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder001 = builder.Field(1).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder001 = builder.Field(1).(*array.ListBuilder)
	inst.scalarFieldBuilder002 = builder.Field(2).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder002 = builder.Field(2).(*array.ListBuilder)
	inst.scalarFieldBuilder003 = builder.Field(3).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder003 = builder.Field(3).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionStrings) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityJsonSectionStrings) BeginAttribute(semanticType1 string, short2 string, long3 string) *InEntityJsonSectionStringsInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder001.Append(semanticType1)
	inst.scalarFieldBuilder002.Append(short2)
	inst.scalarFieldBuilder003.Append(long3)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityJsonSectionStrings) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityJsonSectionStrings) EndSection() *InEntityJson {
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

func (inst *InEntityJsonSectionStrings) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityJsonSectionStrings) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityJsonSectionStrings) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionStrings) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionStringsInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityJsonSectionStrings
	scalarFieldBuilder001            *array.StringBuilder
	scalarListBuilder001             *array.ListBuilder
	scalarFieldBuilder002            *array.StringBuilder
	scalarListBuilder002             *array.ListBuilder
	scalarFieldBuilder003            *array.StringBuilder
	scalarListBuilder003             *array.ListBuilder
	membershipFieldBuilder004        *array.BinaryBuilder
	membershipListBuilder004         *array.ListBuilder
	membershipFieldBuilder005        *array.BinaryBuilder
	membershipListBuilder005         *array.ListBuilder
	membershipSupportFieldBuilder006 *array.Uint64Builder
	membershipSupportListBuilder006  *array.ListBuilder

	membershipContainerLength004 int

	membershipContainerLength005 int
}

func NewInEntityJsonSectionStringsInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionStrings) (inst *InEntityJsonSectionStringsInAttr) {
	inst = &InEntityJsonSectionStringsInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder001 = builder.Field(1).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder001 = builder.Field(1).(*array.ListBuilder)
	inst.scalarFieldBuilder002 = builder.Field(2).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder002 = builder.Field(2).(*array.ListBuilder)
	inst.scalarFieldBuilder003 = builder.Field(3).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder003 = builder.Field(3).(*array.ListBuilder)
	inst.membershipFieldBuilder004 = builder.Field(4).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder004 = builder.Field(4).(*array.ListBuilder)
	inst.membershipFieldBuilder005 = builder.Field(5).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder005 = builder.Field(5).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder006 = builder.Field(6).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder006 = builder.Field(6).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionStringsInAttr) beginAttribute() {
	inst.membershipListBuilder004.Append(true)
	inst.membershipListBuilder005.Append(true)
	inst.membershipContainerLength004 = 0
	inst.membershipContainerLength005 = 0
	inst.scalarListBuilder001.Append(true)
	inst.scalarListBuilder002.Append(true)
	inst.scalarListBuilder003.Append(true)
	inst.membershipSupportListBuilder006.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionStringsInAttr) AddMembershipMixedLowCardVerbatim(lmv4 []byte, mvhp5 []byte) *InEntityJsonSectionStringsInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder004.Append(lmv4)
	inst.membershipFieldBuilder005.Append(mvhp5)
	inst.membershipContainerLength004++
	inst.membershipContainerLength005++
	return inst
}
func (inst *InEntityJsonSectionStringsInAttr) AddMembershipMixedLowCardVerbatimP(lmv4 []byte, mvhp5 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder004.Append(lmv4)
	inst.membershipFieldBuilder005.Append(mvhp5)
	inst.membershipContainerLength004++
	inst.membershipContainerLength005++
	return
}
func (inst *InEntityJsonSectionStringsInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength004
	inst.membershipContainerLength004 = 0
	inst.membershipSupportFieldBuilder006.Append(uint64(l))
}
func (inst *InEntityJsonSectionStringsInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityJsonSectionStringsInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityJsonSectionStringsInAttr) EndSection() *InEntityJson {
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
func (inst *InEntityJsonSectionStringsInAttr) EndAttribute() *InEntityJsonSectionStrings {
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

func (inst *InEntityJsonSectionStringsInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionStringsInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionSymbol struct {
	errs                  []error
	inAttr                *InEntityJsonSectionSymbolInAttr
	state                 runtime.EntityStateE
	parent                *InEntityJson
	scalarFieldBuilder021 *array.StringBuilder
	scalarListBuilder021  *array.ListBuilder
}

func NewInEntityJsonSectionSymbol(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionSymbol) {
	inst = &InEntityJsonSectionSymbol{}
	inAttr := NewInEntityJsonSectionSymbolInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder021 = builder.Field(21).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder021 = builder.Field(21).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionSymbol) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityJsonSectionSymbol) BeginAttribute(value21 string) *InEntityJsonSectionSymbolInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder021.Append(value21)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityJsonSectionSymbol) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityJsonSectionSymbol) EndSection() *InEntityJson {
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

func (inst *InEntityJsonSectionSymbol) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityJsonSectionSymbol) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityJsonSectionSymbol) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionSymbol) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionSymbolInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityJsonSectionSymbol
	scalarFieldBuilder021            *array.StringBuilder
	scalarListBuilder021             *array.ListBuilder
	membershipFieldBuilder022        *array.BinaryBuilder
	membershipListBuilder022         *array.ListBuilder
	membershipFieldBuilder023        *array.BinaryBuilder
	membershipListBuilder023         *array.ListBuilder
	membershipSupportFieldBuilder024 *array.Uint64Builder
	membershipSupportListBuilder024  *array.ListBuilder

	membershipContainerLength022 int

	membershipContainerLength023 int
}

func NewInEntityJsonSectionSymbolInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionSymbol) (inst *InEntityJsonSectionSymbolInAttr) {
	inst = &InEntityJsonSectionSymbolInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder021 = builder.Field(21).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder021 = builder.Field(21).(*array.ListBuilder)
	inst.membershipFieldBuilder022 = builder.Field(22).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder022 = builder.Field(22).(*array.ListBuilder)
	inst.membershipFieldBuilder023 = builder.Field(23).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder023 = builder.Field(23).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder024 = builder.Field(24).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder024 = builder.Field(24).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionSymbolInAttr) beginAttribute() {
	inst.membershipListBuilder022.Append(true)
	inst.membershipListBuilder023.Append(true)
	inst.membershipContainerLength022 = 0
	inst.membershipContainerLength023 = 0
	inst.scalarListBuilder021.Append(true)
	inst.membershipSupportListBuilder024.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionSymbolInAttr) AddMembershipMixedLowCardVerbatim(lmv22 []byte, mvhp23 []byte) *InEntityJsonSectionSymbolInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder022.Append(lmv22)
	inst.membershipFieldBuilder023.Append(mvhp23)
	inst.membershipContainerLength022++
	inst.membershipContainerLength023++
	return inst
}
func (inst *InEntityJsonSectionSymbolInAttr) AddMembershipMixedLowCardVerbatimP(lmv22 []byte, mvhp23 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder022.Append(lmv22)
	inst.membershipFieldBuilder023.Append(mvhp23)
	inst.membershipContainerLength022++
	inst.membershipContainerLength023++
	return
}
func (inst *InEntityJsonSectionSymbolInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength022
	inst.membershipContainerLength022 = 0
	inst.membershipSupportFieldBuilder024.Append(uint64(l))
}
func (inst *InEntityJsonSectionSymbolInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityJsonSectionSymbolInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityJsonSectionSymbolInAttr) EndSection() *InEntityJson {
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
func (inst *InEntityJsonSectionSymbolInAttr) EndAttribute() *InEntityJsonSectionSymbol {
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

func (inst *InEntityJsonSectionSymbolInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionSymbolInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionUndefined struct {
	errs   []error
	inAttr *InEntityJsonSectionUndefinedInAttr
	state  runtime.EntityStateE
	parent *InEntityJson
}

func NewInEntityJsonSectionUndefined(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionUndefined) {
	inst = &InEntityJsonSectionUndefined{}
	inAttr := NewInEntityJsonSectionUndefinedInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent

	return inst
}
func (inst *InEntityJsonSectionUndefined) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityJsonSectionUndefined) BeginAttribute() *InEntityJsonSectionUndefinedInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityJsonSectionUndefined) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityJsonSectionUndefined) EndSection() *InEntityJson {
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

func (inst *InEntityJsonSectionUndefined) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityJsonSectionUndefined) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityJsonSectionUndefined) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionUndefined) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityJsonSectionUndefinedInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityJsonSectionUndefined
	membershipFieldBuilder011        *array.BinaryBuilder
	membershipListBuilder011         *array.ListBuilder
	membershipFieldBuilder012        *array.BinaryBuilder
	membershipListBuilder012         *array.ListBuilder
	membershipSupportFieldBuilder013 *array.Uint64Builder
	membershipSupportListBuilder013  *array.ListBuilder

	membershipContainerLength011 int

	membershipContainerLength012 int
}

func NewInEntityJsonSectionUndefinedInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionUndefined) (inst *InEntityJsonSectionUndefinedInAttr) {
	inst = &InEntityJsonSectionUndefinedInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.membershipFieldBuilder011 = builder.Field(11).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder011 = builder.Field(11).(*array.ListBuilder)
	inst.membershipFieldBuilder012 = builder.Field(12).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder012 = builder.Field(12).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder013 = builder.Field(13).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder013 = builder.Field(13).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionUndefinedInAttr) beginAttribute() {
	inst.membershipListBuilder011.Append(true)
	inst.membershipListBuilder012.Append(true)
	inst.membershipContainerLength011 = 0
	inst.membershipContainerLength012 = 0
	inst.membershipSupportListBuilder013.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionUndefinedInAttr) AddMembershipMixedLowCardVerbatim(lmv11 []byte, mvhp12 []byte) *InEntityJsonSectionUndefinedInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder011.Append(lmv11)
	inst.membershipFieldBuilder012.Append(mvhp12)
	inst.membershipContainerLength011++
	inst.membershipContainerLength012++
	return inst
}
func (inst *InEntityJsonSectionUndefinedInAttr) AddMembershipMixedLowCardVerbatimP(lmv11 []byte, mvhp12 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder011.Append(lmv11)
	inst.membershipFieldBuilder012.Append(mvhp12)
	inst.membershipContainerLength011++
	inst.membershipContainerLength012++
	return
}
func (inst *InEntityJsonSectionUndefinedInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength011
	inst.membershipContainerLength011 = 0
	inst.membershipSupportFieldBuilder013.Append(uint64(l))
}
func (inst *InEntityJsonSectionUndefinedInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityJsonSectionUndefinedInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityJsonSectionUndefinedInAttr) EndSection() *InEntityJson {
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
func (inst *InEntityJsonSectionUndefinedInAttr) EndAttribute() *InEntityJsonSectionUndefined {
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

func (inst *InEntityJsonSectionUndefinedInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityJsonSectionUndefinedInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}
