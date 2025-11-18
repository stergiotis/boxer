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
		/* 001 */ arrow.Field{Name: "tv:bool:value:val:b:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BooleanType{})},
		/* 002 */ arrow.Field{Name: "tv:bool:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 003 */ arrow.Field{Name: "tv:bool:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 004 */ arrow.Field{Name: "tv:bool:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 005 */ arrow.Field{Name: "tv:undefined:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 006 */ arrow.Field{Name: "tv:undefined:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 007 */ arrow.Field{Name: "tv:undefined:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 008 */ arrow.Field{Name: "tv:null:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 009 */ arrow.Field{Name: "tv:null:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 010 */ arrow.Field{Name: "tv:null:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 011 */ arrow.Field{Name: "tv:string:value:val:s:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 012 */ arrow.Field{Name: "tv:string:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 013 */ arrow.Field{Name: "tv:string:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 014 */ arrow.Field{Name: "tv:string:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 015 */ arrow.Field{Name: "tv:symbol:value:val:s:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 016 */ arrow.Field{Name: "tv:symbol:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 017 */ arrow.Field{Name: "tv:symbol:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 018 */ arrow.Field{Name: "tv:symbol:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 019 */ arrow.Field{Name: "tv:float64:value:val:f64:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Float64)},
		/* 020 */ arrow.Field{Name: "tv:float64:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 021 */ arrow.Field{Name: "tv:float64:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 022 */ arrow.Field{Name: "tv:float64:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 023 */ arrow.Field{Name: "tv:int64:value:val:i64:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Int64)},
		/* 024 */ arrow.Field{Name: "tv:int64:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 025 */ arrow.Field{Name: "tv:int64:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 026 */ arrow.Field{Name: "tv:int64:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
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
	section05Inst         *InEntityJsonSectionSymbol
	section05State        runtime.EntityStateE
	section06Inst         *InEntityJsonSectionUndefined
	section06State        runtime.EntityStateE
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
	inst.section05Inst = NewInEntityJsonSectionSymbol(builder, inst)
	inst.section06Inst = NewInEntityJsonSectionUndefined(builder, inst)
}
func (inst *InEntityJson) beginSections() {
	inst.section00Inst.beginSection()
	inst.section01Inst.beginSection()
	inst.section02Inst.beginSection()
	inst.section03Inst.beginSection()
	inst.section04Inst.beginSection()
	inst.section05Inst.beginSection()
	inst.section06Inst.beginSection()
}
func (inst *InEntityJson) resetSections() {
	inst.section00Inst.resetSection()
	inst.section01Inst.resetSection()
	inst.section02Inst.resetSection()
	inst.section03Inst.resetSection()
	inst.section04Inst.resetSection()
	inst.section05Inst.resetSection()
	inst.section06Inst.resetSection()
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
func (inst *InEntityJson) GetSectionSymbol() *InEntityJsonSectionSymbol {
	return inst.section05Inst
}
func (inst *InEntityJson) GetSectionUndefined() *InEntityJsonSectionUndefined {
	return inst.section06Inst
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
			inst.AppendError(eb.Build().Str("section", "symbol").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section06Inst.state
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
	scalarFieldBuilder001 *array.BooleanBuilder
	scalarListBuilder001  *array.ListBuilder
}

func NewInEntityJsonSectionBool(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionBool) {
	inst = &InEntityJsonSectionBool{}
	inAttr := NewInEntityJsonSectionBoolInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder001 = builder.Field(1).(*array.ListBuilder).ValueBuilder().(*array.BooleanBuilder)
	inst.scalarListBuilder001 = builder.Field(1).(*array.ListBuilder)

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
func (inst *InEntityJsonSectionBool) BeginAttribute(value1 bool) *InEntityJsonSectionBoolInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder001.Append(value1)

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
	scalarFieldBuilder001            *array.BooleanBuilder
	scalarListBuilder001             *array.ListBuilder
	membershipFieldBuilder002        *array.BinaryBuilder
	membershipListBuilder002         *array.ListBuilder
	membershipFieldBuilder003        *array.BinaryBuilder
	membershipListBuilder003         *array.ListBuilder
	membershipSupportFieldBuilder004 *array.Uint64Builder
	membershipSupportListBuilder004  *array.ListBuilder

	membershipContainerLength002 int

	membershipContainerLength003 int
}

func NewInEntityJsonSectionBoolInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionBool) (inst *InEntityJsonSectionBoolInAttr) {
	inst = &InEntityJsonSectionBoolInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder001 = builder.Field(1).(*array.ListBuilder).ValueBuilder().(*array.BooleanBuilder)
	inst.scalarListBuilder001 = builder.Field(1).(*array.ListBuilder)
	inst.membershipFieldBuilder002 = builder.Field(2).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder002 = builder.Field(2).(*array.ListBuilder)
	inst.membershipFieldBuilder003 = builder.Field(3).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder003 = builder.Field(3).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder004 = builder.Field(4).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder004 = builder.Field(4).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionBoolInAttr) beginAttribute() {
	inst.membershipListBuilder002.Append(true)
	inst.membershipListBuilder003.Append(true)
	inst.membershipContainerLength002 = 0
	inst.membershipContainerLength003 = 0
	inst.scalarListBuilder001.Append(true)
	inst.membershipSupportListBuilder004.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionBoolInAttr) AddMembershipMixedLowCardVerbatim(lmv2 []byte, mvhp3 []byte) *InEntityJsonSectionBoolInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder002.Append(lmv2)
	inst.membershipFieldBuilder003.Append(mvhp3)
	inst.membershipContainerLength002++
	inst.membershipContainerLength003++
	return inst
}
func (inst *InEntityJsonSectionBoolInAttr) AddMembershipMixedLowCardVerbatimP(lmv2 []byte, mvhp3 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder002.Append(lmv2)
	inst.membershipFieldBuilder003.Append(mvhp3)
	inst.membershipContainerLength002++
	inst.membershipContainerLength003++
	return
}
func (inst *InEntityJsonSectionBoolInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength002
	inst.membershipContainerLength002 = 0
	inst.membershipSupportFieldBuilder004.Append(uint64(l))
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
	scalarFieldBuilder019 *array.Float64Builder
	scalarListBuilder019  *array.ListBuilder
}

func NewInEntityJsonSectionFloat64(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionFloat64) {
	inst = &InEntityJsonSectionFloat64{}
	inAttr := NewInEntityJsonSectionFloat64InAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder019 = builder.Field(19).(*array.ListBuilder).ValueBuilder().(*array.Float64Builder)
	inst.scalarListBuilder019 = builder.Field(19).(*array.ListBuilder)

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
func (inst *InEntityJsonSectionFloat64) BeginAttribute(value19 float64) *InEntityJsonSectionFloat64InAttr {
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
	scalarFieldBuilder019            *array.Float64Builder
	scalarListBuilder019             *array.ListBuilder
	membershipFieldBuilder020        *array.BinaryBuilder
	membershipListBuilder020         *array.ListBuilder
	membershipFieldBuilder021        *array.BinaryBuilder
	membershipListBuilder021         *array.ListBuilder
	membershipSupportFieldBuilder022 *array.Uint64Builder
	membershipSupportListBuilder022  *array.ListBuilder

	membershipContainerLength020 int

	membershipContainerLength021 int
}

func NewInEntityJsonSectionFloat64InAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionFloat64) (inst *InEntityJsonSectionFloat64InAttr) {
	inst = &InEntityJsonSectionFloat64InAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder019 = builder.Field(19).(*array.ListBuilder).ValueBuilder().(*array.Float64Builder)
	inst.scalarListBuilder019 = builder.Field(19).(*array.ListBuilder)
	inst.membershipFieldBuilder020 = builder.Field(20).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder020 = builder.Field(20).(*array.ListBuilder)
	inst.membershipFieldBuilder021 = builder.Field(21).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder021 = builder.Field(21).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder022 = builder.Field(22).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder022 = builder.Field(22).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionFloat64InAttr) beginAttribute() {
	inst.membershipListBuilder020.Append(true)
	inst.membershipListBuilder021.Append(true)
	inst.membershipContainerLength020 = 0
	inst.membershipContainerLength021 = 0
	inst.scalarListBuilder019.Append(true)
	inst.membershipSupportListBuilder022.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionFloat64InAttr) AddMembershipMixedLowCardVerbatim(lmv20 []byte, mvhp21 []byte) *InEntityJsonSectionFloat64InAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder020.Append(lmv20)
	inst.membershipFieldBuilder021.Append(mvhp21)
	inst.membershipContainerLength020++
	inst.membershipContainerLength021++
	return inst
}
func (inst *InEntityJsonSectionFloat64InAttr) AddMembershipMixedLowCardVerbatimP(lmv20 []byte, mvhp21 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder020.Append(lmv20)
	inst.membershipFieldBuilder021.Append(mvhp21)
	inst.membershipContainerLength020++
	inst.membershipContainerLength021++
	return
}
func (inst *InEntityJsonSectionFloat64InAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength020
	inst.membershipContainerLength020 = 0
	inst.membershipSupportFieldBuilder022.Append(uint64(l))
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
	scalarFieldBuilder023 *array.Int64Builder
	scalarListBuilder023  *array.ListBuilder
}

func NewInEntityJsonSectionInt64(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionInt64) {
	inst = &InEntityJsonSectionInt64{}
	inAttr := NewInEntityJsonSectionInt64InAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder023 = builder.Field(23).(*array.ListBuilder).ValueBuilder().(*array.Int64Builder)
	inst.scalarListBuilder023 = builder.Field(23).(*array.ListBuilder)

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
func (inst *InEntityJsonSectionInt64) BeginAttribute(value23 int64) *InEntityJsonSectionInt64InAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder023.Append(value23)

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
	scalarFieldBuilder023            *array.Int64Builder
	scalarListBuilder023             *array.ListBuilder
	membershipFieldBuilder024        *array.BinaryBuilder
	membershipListBuilder024         *array.ListBuilder
	membershipFieldBuilder025        *array.BinaryBuilder
	membershipListBuilder025         *array.ListBuilder
	membershipSupportFieldBuilder026 *array.Uint64Builder
	membershipSupportListBuilder026  *array.ListBuilder

	membershipContainerLength024 int

	membershipContainerLength025 int
}

func NewInEntityJsonSectionInt64InAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionInt64) (inst *InEntityJsonSectionInt64InAttr) {
	inst = &InEntityJsonSectionInt64InAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder023 = builder.Field(23).(*array.ListBuilder).ValueBuilder().(*array.Int64Builder)
	inst.scalarListBuilder023 = builder.Field(23).(*array.ListBuilder)
	inst.membershipFieldBuilder024 = builder.Field(24).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder024 = builder.Field(24).(*array.ListBuilder)
	inst.membershipFieldBuilder025 = builder.Field(25).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder025 = builder.Field(25).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder026 = builder.Field(26).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder026 = builder.Field(26).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionInt64InAttr) beginAttribute() {
	inst.membershipListBuilder024.Append(true)
	inst.membershipListBuilder025.Append(true)
	inst.membershipContainerLength024 = 0
	inst.membershipContainerLength025 = 0
	inst.scalarListBuilder023.Append(true)
	inst.membershipSupportListBuilder026.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionInt64InAttr) AddMembershipMixedLowCardVerbatim(lmv24 []byte, mvhp25 []byte) *InEntityJsonSectionInt64InAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder024.Append(lmv24)
	inst.membershipFieldBuilder025.Append(mvhp25)
	inst.membershipContainerLength024++
	inst.membershipContainerLength025++
	return inst
}
func (inst *InEntityJsonSectionInt64InAttr) AddMembershipMixedLowCardVerbatimP(lmv24 []byte, mvhp25 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder024.Append(lmv24)
	inst.membershipFieldBuilder025.Append(mvhp25)
	inst.membershipContainerLength024++
	inst.membershipContainerLength025++
	return
}
func (inst *InEntityJsonSectionInt64InAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength024
	inst.membershipContainerLength024 = 0
	inst.membershipSupportFieldBuilder026.Append(uint64(l))
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
	membershipFieldBuilder008        *array.BinaryBuilder
	membershipListBuilder008         *array.ListBuilder
	membershipFieldBuilder009        *array.BinaryBuilder
	membershipListBuilder009         *array.ListBuilder
	membershipSupportFieldBuilder010 *array.Uint64Builder
	membershipSupportListBuilder010  *array.ListBuilder

	membershipContainerLength008 int

	membershipContainerLength009 int
}

func NewInEntityJsonSectionNullInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionNull) (inst *InEntityJsonSectionNullInAttr) {
	inst = &InEntityJsonSectionNullInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.membershipFieldBuilder008 = builder.Field(8).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder008 = builder.Field(8).(*array.ListBuilder)
	inst.membershipFieldBuilder009 = builder.Field(9).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder009 = builder.Field(9).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder010 = builder.Field(10).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder010 = builder.Field(10).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionNullInAttr) beginAttribute() {
	inst.membershipListBuilder008.Append(true)
	inst.membershipListBuilder009.Append(true)
	inst.membershipContainerLength008 = 0
	inst.membershipContainerLength009 = 0
	inst.membershipSupportListBuilder010.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionNullInAttr) AddMembershipMixedLowCardVerbatim(lmv8 []byte, mvhp9 []byte) *InEntityJsonSectionNullInAttr {
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
func (inst *InEntityJsonSectionNullInAttr) AddMembershipMixedLowCardVerbatimP(lmv8 []byte, mvhp9 []byte) {
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
func (inst *InEntityJsonSectionNullInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength008
	inst.membershipContainerLength008 = 0
	inst.membershipSupportFieldBuilder010.Append(uint64(l))
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
	scalarFieldBuilder011 *array.StringBuilder
	scalarListBuilder011  *array.ListBuilder
}

func NewInEntityJsonSectionString(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionString) {
	inst = &InEntityJsonSectionString{}
	inAttr := NewInEntityJsonSectionStringInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder011 = builder.Field(11).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder011 = builder.Field(11).(*array.ListBuilder)

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
func (inst *InEntityJsonSectionString) BeginAttribute(value11 string) *InEntityJsonSectionStringInAttr {
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
	scalarFieldBuilder011            *array.StringBuilder
	scalarListBuilder011             *array.ListBuilder
	membershipFieldBuilder012        *array.BinaryBuilder
	membershipListBuilder012         *array.ListBuilder
	membershipFieldBuilder013        *array.BinaryBuilder
	membershipListBuilder013         *array.ListBuilder
	membershipSupportFieldBuilder014 *array.Uint64Builder
	membershipSupportListBuilder014  *array.ListBuilder

	membershipContainerLength012 int

	membershipContainerLength013 int
}

func NewInEntityJsonSectionStringInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionString) (inst *InEntityJsonSectionStringInAttr) {
	inst = &InEntityJsonSectionStringInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder011 = builder.Field(11).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder011 = builder.Field(11).(*array.ListBuilder)
	inst.membershipFieldBuilder012 = builder.Field(12).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder012 = builder.Field(12).(*array.ListBuilder)
	inst.membershipFieldBuilder013 = builder.Field(13).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder013 = builder.Field(13).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder014 = builder.Field(14).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder014 = builder.Field(14).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionStringInAttr) beginAttribute() {
	inst.membershipListBuilder012.Append(true)
	inst.membershipListBuilder013.Append(true)
	inst.membershipContainerLength012 = 0
	inst.membershipContainerLength013 = 0
	inst.scalarListBuilder011.Append(true)
	inst.membershipSupportListBuilder014.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionStringInAttr) AddMembershipMixedLowCardVerbatim(lmv12 []byte, mvhp13 []byte) *InEntityJsonSectionStringInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder012.Append(lmv12)
	inst.membershipFieldBuilder013.Append(mvhp13)
	inst.membershipContainerLength012++
	inst.membershipContainerLength013++
	return inst
}
func (inst *InEntityJsonSectionStringInAttr) AddMembershipMixedLowCardVerbatimP(lmv12 []byte, mvhp13 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder012.Append(lmv12)
	inst.membershipFieldBuilder013.Append(mvhp13)
	inst.membershipContainerLength012++
	inst.membershipContainerLength013++
	return
}
func (inst *InEntityJsonSectionStringInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength012
	inst.membershipContainerLength012 = 0
	inst.membershipSupportFieldBuilder014.Append(uint64(l))
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

type InEntityJsonSectionSymbol struct {
	errs                  []error
	inAttr                *InEntityJsonSectionSymbolInAttr
	state                 runtime.EntityStateE
	parent                *InEntityJson
	scalarFieldBuilder015 *array.StringBuilder
	scalarListBuilder015  *array.ListBuilder
}

func NewInEntityJsonSectionSymbol(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionSymbol) {
	inst = &InEntityJsonSectionSymbol{}
	inAttr := NewInEntityJsonSectionSymbolInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder015 = builder.Field(15).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder015 = builder.Field(15).(*array.ListBuilder)

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
func (inst *InEntityJsonSectionSymbol) BeginAttribute(value15 string) *InEntityJsonSectionSymbolInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder015.Append(value15)

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
	scalarFieldBuilder015            *array.StringBuilder
	scalarListBuilder015             *array.ListBuilder
	membershipFieldBuilder016        *array.BinaryBuilder
	membershipListBuilder016         *array.ListBuilder
	membershipFieldBuilder017        *array.BinaryBuilder
	membershipListBuilder017         *array.ListBuilder
	membershipSupportFieldBuilder018 *array.Uint64Builder
	membershipSupportListBuilder018  *array.ListBuilder

	membershipContainerLength016 int

	membershipContainerLength017 int
}

func NewInEntityJsonSectionSymbolInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionSymbol) (inst *InEntityJsonSectionSymbolInAttr) {
	inst = &InEntityJsonSectionSymbolInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder015 = builder.Field(15).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder015 = builder.Field(15).(*array.ListBuilder)
	inst.membershipFieldBuilder016 = builder.Field(16).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder016 = builder.Field(16).(*array.ListBuilder)
	inst.membershipFieldBuilder017 = builder.Field(17).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder017 = builder.Field(17).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder018 = builder.Field(18).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder018 = builder.Field(18).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionSymbolInAttr) beginAttribute() {
	inst.membershipListBuilder016.Append(true)
	inst.membershipListBuilder017.Append(true)
	inst.membershipContainerLength016 = 0
	inst.membershipContainerLength017 = 0
	inst.scalarListBuilder015.Append(true)
	inst.membershipSupportListBuilder018.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionSymbolInAttr) AddMembershipMixedLowCardVerbatim(lmv16 []byte, mvhp17 []byte) *InEntityJsonSectionSymbolInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder016.Append(lmv16)
	inst.membershipFieldBuilder017.Append(mvhp17)
	inst.membershipContainerLength016++
	inst.membershipContainerLength017++
	return inst
}
func (inst *InEntityJsonSectionSymbolInAttr) AddMembershipMixedLowCardVerbatimP(lmv16 []byte, mvhp17 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder016.Append(lmv16)
	inst.membershipFieldBuilder017.Append(mvhp17)
	inst.membershipContainerLength016++
	inst.membershipContainerLength017++
	return
}
func (inst *InEntityJsonSectionSymbolInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength016
	inst.membershipContainerLength016 = 0
	inst.membershipSupportFieldBuilder018.Append(uint64(l))
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
	membershipFieldBuilder005        *array.BinaryBuilder
	membershipListBuilder005         *array.ListBuilder
	membershipFieldBuilder006        *array.BinaryBuilder
	membershipListBuilder006         *array.ListBuilder
	membershipSupportFieldBuilder007 *array.Uint64Builder
	membershipSupportListBuilder007  *array.ListBuilder

	membershipContainerLength005 int

	membershipContainerLength006 int
}

func NewInEntityJsonSectionUndefinedInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionUndefined) (inst *InEntityJsonSectionUndefinedInAttr) {
	inst = &InEntityJsonSectionUndefinedInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.membershipFieldBuilder005 = builder.Field(5).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder005 = builder.Field(5).(*array.ListBuilder)
	inst.membershipFieldBuilder006 = builder.Field(6).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder006 = builder.Field(6).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder007 = builder.Field(7).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder007 = builder.Field(7).(*array.ListBuilder)

	return inst
}
func (inst *InEntityJsonSectionUndefinedInAttr) beginAttribute() {
	inst.membershipListBuilder005.Append(true)
	inst.membershipListBuilder006.Append(true)
	inst.membershipContainerLength005 = 0
	inst.membershipContainerLength006 = 0
	inst.membershipSupportListBuilder007.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityJsonSectionUndefinedInAttr) AddMembershipMixedLowCardVerbatim(lmv5 []byte, mvhp6 []byte) *InEntityJsonSectionUndefinedInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder005.Append(lmv5)
	inst.membershipFieldBuilder006.Append(mvhp6)
	inst.membershipContainerLength005++
	inst.membershipContainerLength006++
	return inst
}
func (inst *InEntityJsonSectionUndefinedInAttr) AddMembershipMixedLowCardVerbatimP(lmv5 []byte, mvhp6 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder005.Append(lmv5)
	inst.membershipFieldBuilder006.Append(mvhp6)
	inst.membershipContainerLength005++
	inst.membershipContainerLength006++
	return
}
func (inst *InEntityJsonSectionUndefinedInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength005
	inst.membershipContainerLength005 = 0
	inst.membershipSupportFieldBuilder007.Append(uint64(l))
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
