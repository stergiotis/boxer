// Code generated; ET7 DML (github.com/stergiotis/boxer/public/semistructured/leeway/dml.test) DO NOT EDIT.

package example

import (
	"errors"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	_ "github.com/apache/arrow-go/v18/arrow/ipc"
	_ "github.com/apache/arrow-go/v18/arrow/math"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
	"slices"
	"time"
)

func createRecordBuilderTesttable(allocator memory.Allocator) (builder *array.RecordBuilder) {
	schema := arrow.NewSchema([]arrow.Field{
		/* 000 */ arrow.Field{Name: "id:id:u64:2k:0:0:", Nullable: false, Type: arrow.PrimitiveTypes.Uint64},
		/* 001 */ arrow.Field{Name: "ts:ts:z32:2k:0:0:", Nullable: false, Type: &arrow.Date32Type{}},
		/* 002 */ arrow.Field{Name: "tv:bool:value:val:b:0:0:0:0::", Nullable: false, Type: arrow.ListOf(&arrow.BooleanType{})},
		/* 003 */ arrow.Field{Name: "tv:bool:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOf(&arrow.BinaryType{})},
		/* 004 */ arrow.Field{Name: "tv:bool:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOf(&arrow.BinaryType{})},
		/* 005 */ arrow.Field{Name: "tv:bool:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOf(arrow.PrimitiveTypes.Uint64)},
		/* 006 */ arrow.Field{Name: "tv:string:value:val:s:g:0:0:0::", Nullable: false, Type: arrow.ListOf(&arrow.StringType{})},
		/* 007 */ arrow.Field{Name: "tv:string:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOf(&arrow.BinaryType{})},
		/* 008 */ arrow.Field{Name: "tv:string:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOf(&arrow.BinaryType{})},
		/* 009 */ arrow.Field{Name: "tv:string:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOf(arrow.PrimitiveTypes.Uint64)},
		/* 010 */ arrow.Field{Name: "tv:float64:value:val:f64:1:0:0:0::", Nullable: false, Type: arrow.ListOf(arrow.PrimitiveTypes.Float64)},
		/* 011 */ arrow.Field{Name: "tv:float64:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOf(&arrow.BinaryType{})},
		/* 012 */ arrow.Field{Name: "tv:float64:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOf(&arrow.BinaryType{})},
		/* 013 */ arrow.Field{Name: "tv:float64:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOf(arrow.PrimitiveTypes.Uint64)},
		/* 014 */ arrow.Field{Name: "tv:special:spc:val:s:0:0:0:0::", Nullable: false, Type: arrow.ListOf(&arrow.StringType{})},
		/* 015 */ arrow.Field{Name: "tv:special:ary1:val:u32h:0:0:0:0::", Nullable: false, Type: arrow.ListOf(arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint32))},
		/* 016 */ arrow.Field{Name: "tv:special:ary2:val:u32h:0:0:0:0::", Nullable: false, Type: arrow.ListOf(arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint32))},
		/* 017 */ arrow.Field{Name: "tv:special:lmr:lmr:u64:2q:0:0:0::", Nullable: false, Type: arrow.ListOf(arrow.PrimitiveTypes.Uint64)},
		/* 018 */ arrow.Field{Name: "tv:special:mrhp:mrhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOf(&arrow.BinaryType{})},
		/* 019 */ arrow.Field{Name: "tv:special:len:len:u64:28o:0:0:0::", Nullable: false, Type: arrow.ListOf(arrow.PrimitiveTypes.Uint64)},
		/* 020 */ arrow.Field{Name: "tv:special:lmrcard:lmrcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOf(arrow.PrimitiveTypes.Uint64)},
	}, nil)
	builder = array.NewRecordBuilder(allocator, schema)
	return
}

type InEntityTesttable struct {
	errs                  []error
	state                 runtime.EntityStateE
	allocator             memory.Allocator
	builder               *array.RecordBuilder
	records               []arrow.Record
	section00Inst         *InEntityTesttableSectionBool
	section00State        runtime.EntityStateE
	section01Inst         *InEntityTesttableSectionFloat64
	section01State        runtime.EntityStateE
	section02Inst         *InEntityTesttableSectionSpecial
	section02State        runtime.EntityStateE
	section03Inst         *InEntityTesttableSectionString
	section03State        runtime.EntityStateE
	scalarFieldBuilder002 *array.BooleanBuilder
	scalarListBuilder002  *array.ListBuilder

	membershipFieldBuilder003 *array.BinaryBuilder
	membershipListBuilder003  *array.ListBuilder

	membershipFieldBuilder004 *array.BinaryBuilder
	membershipListBuilder004  *array.ListBuilder

	membershipSupportFieldBuilder005 *array.Uint64Builder
	membershipSupportListBuilder005  *array.ListBuilder

	scalarFieldBuilder006 *array.StringBuilder
	scalarListBuilder006  *array.ListBuilder

	membershipFieldBuilder007 *array.BinaryBuilder
	membershipListBuilder007  *array.ListBuilder

	membershipFieldBuilder008 *array.BinaryBuilder
	membershipListBuilder008  *array.ListBuilder

	membershipSupportFieldBuilder009 *array.Uint64Builder
	membershipSupportListBuilder009  *array.ListBuilder

	scalarFieldBuilder010 *array.Float64Builder
	scalarListBuilder010  *array.ListBuilder

	membershipFieldBuilder011 *array.BinaryBuilder
	membershipListBuilder011  *array.ListBuilder

	membershipFieldBuilder012 *array.BinaryBuilder
	membershipListBuilder012  *array.ListBuilder

	membershipSupportFieldBuilder013 *array.Uint64Builder
	membershipSupportListBuilder013  *array.ListBuilder

	scalarFieldBuilder014 *array.StringBuilder
	scalarListBuilder014  *array.ListBuilder

	homogenousArrayFieldBuilder015 *array.Uint32Builder
	homogenousArrayListBuilder015  *array.ListBuilder

	homogenousArrayFieldBuilder016 *array.Uint32Builder
	homogenousArrayListBuilder016  *array.ListBuilder

	membershipFieldBuilder017 *array.Uint64Builder
	membershipListBuilder017  *array.ListBuilder

	membershipFieldBuilder018 *array.BinaryBuilder
	membershipListBuilder018  *array.ListBuilder

	homogenousArraySupportFieldBuilder019 *array.Uint64Builder
	homogenousArraySupportListBuilder019  *array.ListBuilder

	membershipSupportFieldBuilder020 *array.Uint64Builder
	membershipSupportListBuilder020  *array.ListBuilder
	plainId0                         uint64

	plainTs1              time.Time
	scalarFieldBuilder000 *array.Uint64Builder

	scalarFieldBuilder001 *array.Date32Builder
}

func NewInEntityTesttable(allocator memory.Allocator, estimatedNumberOfRecords int) (inst *InEntityTesttable) {
	inst = &InEntityTesttable{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.allocator = allocator
	inst.records = make([]arrow.Record, 0, estimatedNumberOfRecords)
	builder := createRecordBuilderTesttable(allocator)
	inst.builder = builder
	inst.initSections(builder)
	inst.scalarFieldBuilder000 = builder.Field(0).(*array.Uint64Builder)
	inst.scalarFieldBuilder001 = builder.Field(1).(*array.Date32Builder)

	return inst
}
func (inst *InEntityTesttable) SetId(id0 uint64) *InEntityTesttable {
	if inst.state != runtime.EntityStateInEntity {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.plainId0 = id0

	return inst
}
func (inst *InEntityTesttable) SetTimestamp(ts1 time.Time) *InEntityTesttable {
	if inst.state != runtime.EntityStateInEntity {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.plainTs1 = ts1

	return inst
}
func (inst *InEntityTesttable) appendPlainValues() {
	inst.scalarFieldBuilder000.Append(inst.plainId0)

	inst.scalarFieldBuilder001.Append(arrow.Date32FromTime(inst.plainTs1))
}
func (inst *InEntityTesttable) resetPlainValues() {
	inst.plainId0 = uint64(0)

	inst.plainTs1 = time.Time{}
}
func (inst *InEntityTesttable) initSections(builder *array.RecordBuilder) {
	inst.section00Inst = NewInEntityTesttableSectionBool(builder, inst)
	inst.section01Inst = NewInEntityTesttableSectionFloat64(builder, inst)
	inst.section02Inst = NewInEntityTesttableSectionSpecial(builder, inst)
	inst.section03Inst = NewInEntityTesttableSectionString(builder, inst)
}
func (inst *InEntityTesttable) beginSections() {
	inst.section00Inst.beginSection()
	inst.section01Inst.beginSection()
	inst.section02Inst.beginSection()
	inst.section03Inst.beginSection()
}
func (inst *InEntityTesttable) resetSections() {
	inst.section00Inst.resetSection()
	inst.section01Inst.resetSection()
	inst.section02Inst.resetSection()
	inst.section03Inst.resetSection()
}
func (inst *InEntityTesttable) CheckErrors() (err error) {
	if len(inst.errs) > 0 {
		err = errors.Join(inst.errs...)
	}
	err = errors.Join(err, inst.section00Inst.CheckErrors())
	err = errors.Join(err, inst.section01Inst.CheckErrors())
	err = errors.Join(err, inst.section02Inst.CheckErrors())
	err = errors.Join(err, inst.section03Inst.CheckErrors())

	return
}
func (inst *InEntityTesttable) GetSectionBool() *InEntityTesttableSectionBool {
	return inst.section00Inst
}
func (inst *InEntityTesttable) GetSectionFloat64() *InEntityTesttableSectionFloat64 {
	return inst.section01Inst
}
func (inst *InEntityTesttable) GetSectionSpecial() *InEntityTesttableSectionSpecial {
	return inst.section02Inst
}
func (inst *InEntityTesttable) GetSectionString() *InEntityTesttableSectionString {
	return inst.section03Inst
}
func (inst *InEntityTesttable) BeginEntity() *InEntityTesttable {
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
func (inst *InEntityTesttable) validateEntity() {
	// FIXME check coSectionGroup consistency
	return
}
func (inst *InEntityTesttable) CommitEntity() (err error) {
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
func (inst *InEntityTesttable) RollbackEntity() (err error) {
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
func (inst *InEntityTesttable) TransferRecords(recordsIn []arrow.Record) (recordsOut []arrow.Record, err error) {
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

func (inst *InEntityTesttable) GetSchema() (schema *arrow.Schema) {
	return inst.builder.Schema()
}

func (inst *InEntityTesttable) AppendError(err error) {
	l := len(inst.errs)
	if l == 0 {
		inst.errs = append(inst.errs, err)
		return
	}
	if inst.errs[l-1] != err {
		inst.errs = append(inst.errs, err)
	}
}
func (inst *InEntityTesttable) clearErrors() {
	clear(inst.errs)
	inst.errs = inst.errs[:0]
}

type InEntityTesttableSectionBool struct {
	errs                  []error
	inAttr                *InEntityTesttableSectionBoolInAttr
	state                 runtime.EntityStateE
	parent                *InEntityTesttable
	scalarFieldBuilder002 *array.BooleanBuilder
	scalarListBuilder002  *array.ListBuilder
}

func NewInEntityTesttableSectionBool(builder *array.RecordBuilder, parent *InEntityTesttable) (inst *InEntityTesttableSectionBool) {
	inst = &InEntityTesttableSectionBool{}
	inAttr := NewInEntityTesttableSectionBoolInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder002 = builder.Field(2).(*array.ListBuilder).ValueBuilder().(*array.BooleanBuilder)
	inst.scalarListBuilder002 = builder.Field(2).(*array.ListBuilder)

	return inst
}
func (inst *InEntityTesttableSectionBool) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityTesttableSectionBool) BeginAttribute(value2 bool) *InEntityTesttableSectionBoolInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder002.Append(value2)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityTesttableSectionBool) CheckErrors() (err error) {
	if len(inst.errs) > 0 || len(inst.inAttr.errs) > 0 {
		err = errors.Join(inst.errs...)
		err = errors.Join(err, errors.Join(inst.inAttr.errs...))
		return
	}
	return
}
func (inst *InEntityTesttableSectionBool) EndSection() *InEntityTesttable {
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

func (inst *InEntityTesttableSectionBool) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()

}

func (inst *InEntityTesttableSectionBool) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityTesttableSectionBool) AppendError(err error) {
	l := len(inst.errs)
	if l == 0 {
		inst.errs = append(inst.errs, err)
		return
	}
	if inst.errs[l-1] != err {
		inst.errs = append(inst.errs, err)
	}
}
func (inst *InEntityTesttableSectionBool) clearErrors() {
	clear(inst.errs)
	inst.errs = inst.errs[:0]
}

type InEntityTesttableSectionBoolInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityTesttableSectionBool
	scalarFieldBuilder002            *array.BooleanBuilder
	scalarListBuilder002             *array.ListBuilder
	membershipFieldBuilder003        *array.BinaryBuilder
	membershipListBuilder003         *array.ListBuilder
	membershipFieldBuilder004        *array.BinaryBuilder
	membershipListBuilder004         *array.ListBuilder
	membershipSupportFieldBuilder005 *array.Uint64Builder
	membershipSupportListBuilder005  *array.ListBuilder
}

func NewInEntityTesttableSectionBoolInAttr(builder *array.RecordBuilder, parent *InEntityTesttableSectionBool) (inst *InEntityTesttableSectionBoolInAttr) {
	inst = &InEntityTesttableSectionBoolInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder002 = builder.Field(2).(*array.ListBuilder).ValueBuilder().(*array.BooleanBuilder)
	inst.scalarListBuilder002 = builder.Field(2).(*array.ListBuilder)
	inst.membershipFieldBuilder003 = builder.Field(3).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder003 = builder.Field(3).(*array.ListBuilder)
	inst.membershipFieldBuilder004 = builder.Field(4).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder004 = builder.Field(4).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder005 = builder.Field(5).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder005 = builder.Field(5).(*array.ListBuilder)

	return inst
}
func (inst *InEntityTesttableSectionBoolInAttr) beginAttribute() {
	inst.membershipListBuilder003.Append(true)
	inst.membershipListBuilder004.Append(true)
	inst.scalarListBuilder002.Append(true)
	inst.membershipSupportListBuilder005.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityTesttableSectionBoolInAttr) AddMembershipMixedLowCardVerbatim(lmv3 []byte, mvhp4 []byte) *InEntityTesttableSectionBoolInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder003.Append(lmv3)
	inst.membershipFieldBuilder004.Append(mvhp4)

	return inst
}
func (inst *InEntityTesttableSectionBoolInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipListBuilder003.Len()
	inst.membershipSupportFieldBuilder005.Append(uint64(l))
}
func (inst *InEntityTesttableSectionBoolInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityTesttableSectionBoolInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityTesttableSectionBoolInAttr) EndSection() *InEntityTesttable {
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
func (inst *InEntityTesttableSectionBoolInAttr) EndAttribute() *InEntityTesttableSectionBool {
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

func (inst *InEntityTesttableSectionBoolInAttr) AppendError(err error) {
	l := len(inst.errs)
	if l == 0 {
		inst.errs = append(inst.errs, err)
		return
	}
	if inst.errs[l-1] != err {
		inst.errs = append(inst.errs, err)
	}
}
func (inst *InEntityTesttableSectionBoolInAttr) clearErrors() {
	clear(inst.errs)
	inst.errs = inst.errs[:0]
}

type InEntityTesttableSectionFloat64 struct {
	errs                  []error
	inAttr                *InEntityTesttableSectionFloat64InAttr
	state                 runtime.EntityStateE
	parent                *InEntityTesttable
	scalarFieldBuilder010 *array.Float64Builder
	scalarListBuilder010  *array.ListBuilder
}

func NewInEntityTesttableSectionFloat64(builder *array.RecordBuilder, parent *InEntityTesttable) (inst *InEntityTesttableSectionFloat64) {
	inst = &InEntityTesttableSectionFloat64{}
	inAttr := NewInEntityTesttableSectionFloat64InAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder010 = builder.Field(10).(*array.ListBuilder).ValueBuilder().(*array.Float64Builder)
	inst.scalarListBuilder010 = builder.Field(10).(*array.ListBuilder)

	return inst
}
func (inst *InEntityTesttableSectionFloat64) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityTesttableSectionFloat64) BeginAttribute(value10 float64) *InEntityTesttableSectionFloat64InAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder010.Append(value10)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityTesttableSectionFloat64) CheckErrors() (err error) {
	if len(inst.errs) > 0 || len(inst.inAttr.errs) > 0 {
		err = errors.Join(inst.errs...)
		err = errors.Join(err, errors.Join(inst.inAttr.errs...))
		return
	}
	return
}
func (inst *InEntityTesttableSectionFloat64) EndSection() *InEntityTesttable {
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

func (inst *InEntityTesttableSectionFloat64) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()

}

func (inst *InEntityTesttableSectionFloat64) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityTesttableSectionFloat64) AppendError(err error) {
	l := len(inst.errs)
	if l == 0 {
		inst.errs = append(inst.errs, err)
		return
	}
	if inst.errs[l-1] != err {
		inst.errs = append(inst.errs, err)
	}
}
func (inst *InEntityTesttableSectionFloat64) clearErrors() {
	clear(inst.errs)
	inst.errs = inst.errs[:0]
}

type InEntityTesttableSectionFloat64InAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityTesttableSectionFloat64
	scalarFieldBuilder010            *array.Float64Builder
	scalarListBuilder010             *array.ListBuilder
	membershipFieldBuilder011        *array.BinaryBuilder
	membershipListBuilder011         *array.ListBuilder
	membershipFieldBuilder012        *array.BinaryBuilder
	membershipListBuilder012         *array.ListBuilder
	membershipSupportFieldBuilder013 *array.Uint64Builder
	membershipSupportListBuilder013  *array.ListBuilder
}

func NewInEntityTesttableSectionFloat64InAttr(builder *array.RecordBuilder, parent *InEntityTesttableSectionFloat64) (inst *InEntityTesttableSectionFloat64InAttr) {
	inst = &InEntityTesttableSectionFloat64InAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder010 = builder.Field(10).(*array.ListBuilder).ValueBuilder().(*array.Float64Builder)
	inst.scalarListBuilder010 = builder.Field(10).(*array.ListBuilder)
	inst.membershipFieldBuilder011 = builder.Field(11).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder011 = builder.Field(11).(*array.ListBuilder)
	inst.membershipFieldBuilder012 = builder.Field(12).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder012 = builder.Field(12).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder013 = builder.Field(13).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder013 = builder.Field(13).(*array.ListBuilder)

	return inst
}
func (inst *InEntityTesttableSectionFloat64InAttr) beginAttribute() {
	inst.membershipListBuilder011.Append(true)
	inst.membershipListBuilder012.Append(true)
	inst.scalarListBuilder010.Append(true)
	inst.membershipSupportListBuilder013.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityTesttableSectionFloat64InAttr) AddMembershipMixedLowCardVerbatim(lmv11 []byte, mvhp12 []byte) *InEntityTesttableSectionFloat64InAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder011.Append(lmv11)
	inst.membershipFieldBuilder012.Append(mvhp12)

	return inst
}
func (inst *InEntityTesttableSectionFloat64InAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipListBuilder011.Len()
	inst.membershipSupportFieldBuilder013.Append(uint64(l))
}
func (inst *InEntityTesttableSectionFloat64InAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityTesttableSectionFloat64InAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityTesttableSectionFloat64InAttr) EndSection() *InEntityTesttable {
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
func (inst *InEntityTesttableSectionFloat64InAttr) EndAttribute() *InEntityTesttableSectionFloat64 {
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

func (inst *InEntityTesttableSectionFloat64InAttr) AppendError(err error) {
	l := len(inst.errs)
	if l == 0 {
		inst.errs = append(inst.errs, err)
		return
	}
	if inst.errs[l-1] != err {
		inst.errs = append(inst.errs, err)
	}
}
func (inst *InEntityTesttableSectionFloat64InAttr) clearErrors() {
	clear(inst.errs)
	inst.errs = inst.errs[:0]
}

type InEntityTesttableSectionSpecial struct {
	errs                           []error
	inAttr                         *InEntityTesttableSectionSpecialInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityTesttable
	scalarFieldBuilder014          *array.StringBuilder
	scalarListBuilder014           *array.ListBuilder
	homogenousArrayFieldBuilder015 *array.Uint32Builder
	homogenousArrayListBuilder015  *array.ListBuilder
	homogenousArrayFieldBuilder016 *array.Uint32Builder
	homogenousArrayListBuilder016  *array.ListBuilder
}

func NewInEntityTesttableSectionSpecial(builder *array.RecordBuilder, parent *InEntityTesttable) (inst *InEntityTesttableSectionSpecial) {
	inst = &InEntityTesttableSectionSpecial{}
	inAttr := NewInEntityTesttableSectionSpecialInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder014 = builder.Field(14).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder014 = builder.Field(14).(*array.ListBuilder)
	inst.homogenousArrayFieldBuilder015 = builder.Field(15).(*array.ListBuilder).ValueBuilder().(*array.Uint32Builder)
	inst.homogenousArrayListBuilder015 = builder.Field(15).(*array.ListBuilder)
	inst.homogenousArrayFieldBuilder016 = builder.Field(16).(*array.ListBuilder).ValueBuilder().(*array.Uint32Builder)
	inst.homogenousArrayListBuilder016 = builder.Field(16).(*array.ListBuilder)

	return inst
}
func (inst *InEntityTesttableSectionSpecial) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityTesttableSectionSpecial) BeginAttribute(spc14 string) *InEntityTesttableSectionSpecialInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder014.Append(spc14)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityTesttableSectionSpecial) CheckErrors() (err error) {
	if len(inst.errs) > 0 || len(inst.inAttr.errs) > 0 {
		err = errors.Join(inst.errs...)
		err = errors.Join(err, errors.Join(inst.inAttr.errs...))
		return
	}
	return
}
func (inst *InEntityTesttableSectionSpecial) EndSection() *InEntityTesttable {
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

func (inst *InEntityTesttableSectionSpecial) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
	inst.homogenousArrayListBuilder015.Append(true)
	inst.homogenousArrayListBuilder016.Append(true)

}

func (inst *InEntityTesttableSectionSpecial) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityTesttableSectionSpecial) AppendError(err error) {
	l := len(inst.errs)
	if l == 0 {
		inst.errs = append(inst.errs, err)
		return
	}
	if inst.errs[l-1] != err {
		inst.errs = append(inst.errs, err)
	}
}
func (inst *InEntityTesttableSectionSpecial) clearErrors() {
	clear(inst.errs)
	inst.errs = inst.errs[:0]
}

type InEntityTesttableSectionSpecialInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityTesttableSectionSpecial
	scalarFieldBuilder014                 *array.StringBuilder
	scalarListBuilder014                  *array.ListBuilder
	homogenousArrayFieldBuilder015        *array.Uint32Builder
	homogenousArrayListBuilder015         *array.ListBuilder
	homogenousArrayFieldBuilder016        *array.Uint32Builder
	homogenousArrayListBuilder016         *array.ListBuilder
	membershipFieldBuilder017             *array.Uint64Builder
	membershipListBuilder017              *array.ListBuilder
	membershipFieldBuilder018             *array.BinaryBuilder
	membershipListBuilder018              *array.ListBuilder
	homogenousArraySupportFieldBuilder019 *array.Uint64Builder
	homogenousArraySupportListBuilder019  *array.ListBuilder
	membershipSupportFieldBuilder020      *array.Uint64Builder
	membershipSupportListBuilder020       *array.ListBuilder
}

func NewInEntityTesttableSectionSpecialInAttr(builder *array.RecordBuilder, parent *InEntityTesttableSectionSpecial) (inst *InEntityTesttableSectionSpecialInAttr) {
	inst = &InEntityTesttableSectionSpecialInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder014 = builder.Field(14).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder014 = builder.Field(14).(*array.ListBuilder)
	inst.homogenousArrayFieldBuilder015 = builder.Field(15).(*array.ListBuilder).ValueBuilder().(*array.Uint32Builder)
	inst.homogenousArrayListBuilder015 = builder.Field(15).(*array.ListBuilder)
	inst.homogenousArrayFieldBuilder016 = builder.Field(16).(*array.ListBuilder).ValueBuilder().(*array.Uint32Builder)
	inst.homogenousArrayListBuilder016 = builder.Field(16).(*array.ListBuilder)
	inst.membershipFieldBuilder017 = builder.Field(17).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder017 = builder.Field(17).(*array.ListBuilder)
	inst.membershipFieldBuilder018 = builder.Field(18).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder018 = builder.Field(18).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder019 = builder.Field(19).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder019 = builder.Field(19).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder020 = builder.Field(20).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder020 = builder.Field(20).(*array.ListBuilder)

	return inst
}
func (inst *InEntityTesttableSectionSpecialInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder015.Append(true)
	inst.homogenousArrayListBuilder016.Append(true)
	inst.membershipListBuilder017.Append(true)
	inst.membershipListBuilder018.Append(true)
	inst.scalarListBuilder014.Append(true)
	inst.homogenousArraySupportListBuilder019.Append(true)
	inst.membershipSupportListBuilder020.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityTesttableSectionSpecialInAttr) AddToCoContainers(ary115 uint32, ary216 uint32) *InEntityTesttableSectionSpecialInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder015.Append(ary115)
	inst.homogenousArrayFieldBuilder016.Append(ary216)
	return inst
}
func (inst *InEntityTesttableSectionSpecialInAttr) AddMembershipMixedLowCardRef(lmr17 uint64, mrhp18 []byte) *InEntityTesttableSectionSpecialInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder017.Append(lmr17)
	inst.membershipFieldBuilder018.Append(mrhp18)

	return inst
}
func (inst *InEntityTesttableSectionSpecialInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipListBuilder017.Len()
	inst.membershipSupportFieldBuilder020.Append(uint64(l))
}
func (inst *InEntityTesttableSectionSpecialInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayListBuilder015.Len()
	inst.homogenousArraySupportFieldBuilder019.Append(uint64(l))
}
func (inst *InEntityTesttableSectionSpecialInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityTesttableSectionSpecialInAttr) EndSection() *InEntityTesttable {
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
func (inst *InEntityTesttableSectionSpecialInAttr) EndAttribute() *InEntityTesttableSectionSpecial {
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

func (inst *InEntityTesttableSectionSpecialInAttr) AppendError(err error) {
	l := len(inst.errs)
	if l == 0 {
		inst.errs = append(inst.errs, err)
		return
	}
	if inst.errs[l-1] != err {
		inst.errs = append(inst.errs, err)
	}
}
func (inst *InEntityTesttableSectionSpecialInAttr) clearErrors() {
	clear(inst.errs)
	inst.errs = inst.errs[:0]
}

type InEntityTesttableSectionString struct {
	errs                  []error
	inAttr                *InEntityTesttableSectionStringInAttr
	state                 runtime.EntityStateE
	parent                *InEntityTesttable
	scalarFieldBuilder006 *array.StringBuilder
	scalarListBuilder006  *array.ListBuilder
}

func NewInEntityTesttableSectionString(builder *array.RecordBuilder, parent *InEntityTesttable) (inst *InEntityTesttableSectionString) {
	inst = &InEntityTesttableSectionString{}
	inAttr := NewInEntityTesttableSectionStringInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder006 = builder.Field(6).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder006 = builder.Field(6).(*array.ListBuilder)

	return inst
}
func (inst *InEntityTesttableSectionString) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityTesttableSectionString) BeginAttribute(value6 string) *InEntityTesttableSectionStringInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder006.Append(value6)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityTesttableSectionString) CheckErrors() (err error) {
	if len(inst.errs) > 0 || len(inst.inAttr.errs) > 0 {
		err = errors.Join(inst.errs...)
		err = errors.Join(err, errors.Join(inst.inAttr.errs...))
		return
	}
	return
}
func (inst *InEntityTesttableSectionString) EndSection() *InEntityTesttable {
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

func (inst *InEntityTesttableSectionString) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()

}

func (inst *InEntityTesttableSectionString) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityTesttableSectionString) AppendError(err error) {
	l := len(inst.errs)
	if l == 0 {
		inst.errs = append(inst.errs, err)
		return
	}
	if inst.errs[l-1] != err {
		inst.errs = append(inst.errs, err)
	}
}
func (inst *InEntityTesttableSectionString) clearErrors() {
	clear(inst.errs)
	inst.errs = inst.errs[:0]
}

type InEntityTesttableSectionStringInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityTesttableSectionString
	scalarFieldBuilder006            *array.StringBuilder
	scalarListBuilder006             *array.ListBuilder
	membershipFieldBuilder007        *array.BinaryBuilder
	membershipListBuilder007         *array.ListBuilder
	membershipFieldBuilder008        *array.BinaryBuilder
	membershipListBuilder008         *array.ListBuilder
	membershipSupportFieldBuilder009 *array.Uint64Builder
	membershipSupportListBuilder009  *array.ListBuilder
}

func NewInEntityTesttableSectionStringInAttr(builder *array.RecordBuilder, parent *InEntityTesttableSectionString) (inst *InEntityTesttableSectionStringInAttr) {
	inst = &InEntityTesttableSectionStringInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder006 = builder.Field(6).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder006 = builder.Field(6).(*array.ListBuilder)
	inst.membershipFieldBuilder007 = builder.Field(7).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder007 = builder.Field(7).(*array.ListBuilder)
	inst.membershipFieldBuilder008 = builder.Field(8).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder008 = builder.Field(8).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder009 = builder.Field(9).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder009 = builder.Field(9).(*array.ListBuilder)

	return inst
}
func (inst *InEntityTesttableSectionStringInAttr) beginAttribute() {
	inst.membershipListBuilder007.Append(true)
	inst.membershipListBuilder008.Append(true)
	inst.scalarListBuilder006.Append(true)
	inst.membershipSupportListBuilder009.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityTesttableSectionStringInAttr) AddMembershipMixedLowCardVerbatim(lmv7 []byte, mvhp8 []byte) *InEntityTesttableSectionStringInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder007.Append(lmv7)
	inst.membershipFieldBuilder008.Append(mvhp8)

	return inst
}
func (inst *InEntityTesttableSectionStringInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipListBuilder007.Len()
	inst.membershipSupportFieldBuilder009.Append(uint64(l))
}
func (inst *InEntityTesttableSectionStringInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityTesttableSectionStringInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityTesttableSectionStringInAttr) EndSection() *InEntityTesttable {
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
func (inst *InEntityTesttableSectionStringInAttr) EndAttribute() *InEntityTesttableSectionString {
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

func (inst *InEntityTesttableSectionStringInAttr) AppendError(err error) {
	l := len(inst.errs)
	if l == 0 {
		inst.errs = append(inst.errs, err)
		return
	}
	if inst.errs[l-1] != err {
		inst.errs = append(inst.errs, err)
	}
}
func (inst *InEntityTesttableSectionStringInAttr) clearErrors() {
	clear(inst.errs)
	inst.errs = inst.errs[:0]
}
