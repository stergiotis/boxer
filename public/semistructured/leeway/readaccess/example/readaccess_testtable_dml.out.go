// Code generated; Leeway DML (github.com/stergiotis/boxer/public/semistructured/leeway/readaccess.test) DO NOT EDIT.

package example

import (
	"errors"
	"slices"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	_ "github.com/apache/arrow-go/v18/arrow/ipc"
	_ "github.com/apache/arrow-go/v18/arrow/math"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
)

///////////////////////////////////////////////////////////////////
// code generator
// gocodegen.GenerateArrowSchemaFactory
// ./public/semistructured/leeway/gocodegen/gocodegen_common.go:26

func CreateSchemaTestTable() (schema *arrow.Schema) {
	schema = arrow.NewSchema([]arrow.Field{
		/* 000 */ arrow.Field{Name: "id:id:u64:2k:0:0:", Nullable: false, Type: arrow.PrimitiveTypes.Uint64},
		/* 001 */ arrow.Field{Name: "ts:ts:z32:2k:0:0:", Nullable: false, Type: &arrow.Date32Type{}},
		/* 002 */ arrow.Field{Name: "ts:proc:z32h:g:0:0:", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.Date32Type{})},
		/* 003 */ arrow.Field{Name: "tv:geo:lat:val:f32:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Float32)},
		/* 004 */ arrow.Field{Name: "tv:geo:lng:val:f32:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Float32)},
		/* 005 */ arrow.Field{Name: "tv:geo:h3_res1:val:u64:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 006 */ arrow.Field{Name: "tv:geo:h3_res2:val:u64:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 007 */ arrow.Field{Name: "tv:geo:lr:lr:u64:2q:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 008 */ arrow.Field{Name: "tv:geo:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 009 */ arrow.Field{Name: "tv:geo:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 010 */ arrow.Field{Name: "tv:geo:lrcard:lrcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 011 */ arrow.Field{Name: "tv:geo:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 012 */ arrow.Field{Name: "tv:text:text:val:s:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 013 */ arrow.Field{Name: "tv:text:word_length:val:u32h:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint32)},
		/* 014 */ arrow.Field{Name: "tv:text:words:val:sh:0:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 015 */ arrow.Field{Name: "tv:text:lr:lr:u64:2q:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 016 */ arrow.Field{Name: "tv:text:lmv:lmv:y:m:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 017 */ arrow.Field{Name: "tv:text:mvhp:mvhp:y:g:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 018 */ arrow.Field{Name: "tv:text:len:len:u64:28o:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 019 */ arrow.Field{Name: "tv:text:lrcard:lrcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 020 */ arrow.Field{Name: "tv:text:lmvcard:lmvcard:u64:4gw:0:0:0::", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
	}, nil)
	return
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityClassAndFactoryCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1174

type InEntityTestTable struct {
	errs           []error
	state          runtime.EntityStateE
	allocator      memory.Allocator
	builder        *array.RecordBuilder
	records        []arrow.Record
	section00Inst  *InEntityTestTableSectionGeo
	section00State runtime.EntityStateE
	section01Inst  *InEntityTestTableSectionText
	section01State runtime.EntityStateE
	plainId0       uint64

	plainTs1              time.Time
	plainProc2            []time.Time
	scalarFieldBuilder000 *array.Uint64Builder

	scalarFieldBuilder001          *array.Date32Builder
	homogenousArrayFieldBuilder002 *array.Date32Builder
	homogenousArrayListBuilder002  *array.ListBuilder
}

func NewInEntityTestTable(allocator memory.Allocator, estimatedNumberOfRecords int) (inst *InEntityTestTable) {
	inst = &InEntityTestTable{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.allocator = allocator
	inst.records = make([]arrow.Record, 0, estimatedNumberOfRecords)
	schema := CreateSchemaTestTable()
	builder := array.NewRecordBuilder(allocator, schema)
	inst.builder = builder
	inst.initSections(builder)
	inst.scalarFieldBuilder000 = builder.Field(0).(*array.Uint64Builder)
	inst.scalarFieldBuilder001 = builder.Field(1).(*array.Date32Builder)
	inst.homogenousArrayFieldBuilder002 = builder.Field(2).(*array.ListBuilder).ValueBuilder().(*array.Date32Builder)
	inst.homogenousArrayListBuilder002 = builder.Field(2).(*array.ListBuilder)

	return inst
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1288

func (inst *InEntityTestTable) SetId(id0 uint64) *InEntityTestTable {
	if inst.state != runtime.EntityStateInEntity {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.plainId0 = id0

	return inst
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1288

func (inst *InEntityTestTable) SetTimestamp(ts1 time.Time, proc2 []time.Time) *InEntityTestTable {
	if inst.state != runtime.EntityStateInEntity {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.plainTs1 = ts1
	inst.plainProc2 = proc2

	return inst
}
func (inst *InEntityTestTable) appendPlainValues() {
	inst.scalarFieldBuilder000.Append(inst.plainId0)

	inst.scalarFieldBuilder001.Append(arrow.Date32FromTime(inst.plainTs1))

	inst.homogenousArrayListBuilder002.Append(true)
	for _, v := range inst.plainProc2 {
		inst.homogenousArrayFieldBuilder002.Append(arrow.Date32FromTime(v))
	}
}
func (inst *InEntityTestTable) resetPlainValues() {
	inst.plainId0 = uint64(0)

	inst.plainTs1 = time.Time{}

	inst.plainProc2 = []time.Time(nil)
}
func (inst *InEntityTestTable) initSections(builder *array.RecordBuilder) {
	inst.section00Inst = NewInEntityTestTableSectionGeo(builder, inst)
	inst.section01Inst = NewInEntityTestTableSectionText(builder, inst)
}
func (inst *InEntityTestTable) beginSections() {
	inst.section00Inst.beginSection()
	inst.section01Inst.beginSection()
}
func (inst *InEntityTestTable) resetSections() {
	inst.section00Inst.resetSection()
	inst.section01Inst.resetSection()
}
func (inst *InEntityTestTable) CheckErrors() (err error) {
	err = eh.CheckErrors(inst.errs)
	err = errors.Join(err, inst.section00Inst.CheckErrors())
	err = errors.Join(err, inst.section01Inst.CheckErrors())

	return
}
func (inst *InEntityTestTable) GetSectionGeo() *InEntityTestTableSectionGeo {
	return inst.section00Inst
}
func (inst *InEntityTestTable) GetSectionText() *InEntityTestTableSectionText {
	return inst.section01Inst
}
func (inst *InEntityTestTable) BeginEntity() *InEntityTestTable {
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
func (inst *InEntityTestTable) validateEntity() {
	// FIXME check coSectionGroup consistency
	return
}
func (inst *InEntityTestTable) CommitEntity() (err error) {
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
func (inst *InEntityTestTable) RollbackEntity() (err error) {
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
func (inst *InEntityTestTable) TransferRecords(recordsIn []arrow.Record) (recordsOut []arrow.Record, err error) {
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

func (inst *InEntityTestTable) GetSchema() (schema *arrow.Schema) {
	return inst.builder.Schema()
}

func (inst *InEntityTestTable) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityTestTable) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityTestTableSectionGeo struct {
	errs                  []error
	inAttr                *InEntityTestTableSectionGeoInAttr
	state                 runtime.EntityStateE
	parent                *InEntityTestTable
	scalarFieldBuilder003 *array.Float32Builder
	scalarListBuilder003  *array.ListBuilder
	scalarFieldBuilder004 *array.Float32Builder
	scalarListBuilder004  *array.ListBuilder
	scalarFieldBuilder005 *array.Uint64Builder
	scalarListBuilder005  *array.ListBuilder
	scalarFieldBuilder006 *array.Uint64Builder
	scalarListBuilder006  *array.ListBuilder
}

func NewInEntityTestTableSectionGeo(builder *array.RecordBuilder, parent *InEntityTestTable) (inst *InEntityTestTableSectionGeo) {
	inst = &InEntityTestTableSectionGeo{}
	inAttr := NewInEntityTestTableSectionGeoInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder003 = builder.Field(3).(*array.ListBuilder).ValueBuilder().(*array.Float32Builder)
	inst.scalarListBuilder003 = builder.Field(3).(*array.ListBuilder)
	inst.scalarFieldBuilder004 = builder.Field(4).(*array.ListBuilder).ValueBuilder().(*array.Float32Builder)
	inst.scalarListBuilder004 = builder.Field(4).(*array.ListBuilder)
	inst.scalarFieldBuilder005 = builder.Field(5).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.scalarListBuilder005 = builder.Field(5).(*array.ListBuilder)
	inst.scalarFieldBuilder006 = builder.Field(6).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.scalarListBuilder006 = builder.Field(6).(*array.ListBuilder)

	return inst
}
func (inst *InEntityTestTableSectionGeo) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityTestTableSectionGeo) BeginAttribute(lat3 float32, lng4 float32, h3Res15 uint64, h3Res26 uint64) *InEntityTestTableSectionGeoInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder003.Append(lat3)
	inst.scalarFieldBuilder004.Append(lng4)
	inst.scalarFieldBuilder005.Append(h3Res15)
	inst.scalarFieldBuilder006.Append(h3Res26)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityTestTableSectionGeo) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityTestTableSectionGeo) EndSection() *InEntityTestTable {
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

func (inst *InEntityTestTableSectionGeo) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()

}

func (inst *InEntityTestTableSectionGeo) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityTestTableSectionGeo) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityTestTableSectionGeo) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityTestTableSectionGeoInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityTestTableSectionGeo
	scalarFieldBuilder003            *array.Float32Builder
	scalarListBuilder003             *array.ListBuilder
	scalarFieldBuilder004            *array.Float32Builder
	scalarListBuilder004             *array.ListBuilder
	scalarFieldBuilder005            *array.Uint64Builder
	scalarListBuilder005             *array.ListBuilder
	scalarFieldBuilder006            *array.Uint64Builder
	scalarListBuilder006             *array.ListBuilder
	membershipFieldBuilder007        *array.Uint64Builder
	membershipListBuilder007         *array.ListBuilder
	membershipFieldBuilder008        *array.BinaryBuilder
	membershipListBuilder008         *array.ListBuilder
	membershipFieldBuilder009        *array.BinaryBuilder
	membershipListBuilder009         *array.ListBuilder
	membershipSupportFieldBuilder010 *array.Uint64Builder
	membershipSupportListBuilder010  *array.ListBuilder
	membershipSupportFieldBuilder011 *array.Uint64Builder
	membershipSupportListBuilder011  *array.ListBuilder

	membershipContainerLength007 int

	membershipContainerLength008 int

	membershipContainerLength009 int
}

func NewInEntityTestTableSectionGeoInAttr(builder *array.RecordBuilder, parent *InEntityTestTableSectionGeo) (inst *InEntityTestTableSectionGeoInAttr) {
	inst = &InEntityTestTableSectionGeoInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder003 = builder.Field(3).(*array.ListBuilder).ValueBuilder().(*array.Float32Builder)
	inst.scalarListBuilder003 = builder.Field(3).(*array.ListBuilder)
	inst.scalarFieldBuilder004 = builder.Field(4).(*array.ListBuilder).ValueBuilder().(*array.Float32Builder)
	inst.scalarListBuilder004 = builder.Field(4).(*array.ListBuilder)
	inst.scalarFieldBuilder005 = builder.Field(5).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.scalarListBuilder005 = builder.Field(5).(*array.ListBuilder)
	inst.scalarFieldBuilder006 = builder.Field(6).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.scalarListBuilder006 = builder.Field(6).(*array.ListBuilder)
	inst.membershipFieldBuilder007 = builder.Field(7).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder007 = builder.Field(7).(*array.ListBuilder)
	inst.membershipFieldBuilder008 = builder.Field(8).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder008 = builder.Field(8).(*array.ListBuilder)
	inst.membershipFieldBuilder009 = builder.Field(9).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder009 = builder.Field(9).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder010 = builder.Field(10).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder010 = builder.Field(10).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder011 = builder.Field(11).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder011 = builder.Field(11).(*array.ListBuilder)

	return inst
}
func (inst *InEntityTestTableSectionGeoInAttr) beginAttribute() {
	inst.membershipListBuilder007.Append(true)
	inst.membershipListBuilder008.Append(true)
	inst.membershipListBuilder009.Append(true)
	inst.membershipContainerLength007 = 0
	inst.membershipContainerLength008 = 0
	inst.membershipContainerLength009 = 0
	inst.scalarListBuilder003.Append(true)
	inst.scalarListBuilder004.Append(true)
	inst.scalarListBuilder005.Append(true)
	inst.scalarListBuilder006.Append(true)
	inst.membershipSupportListBuilder010.Append(true)
	inst.membershipSupportListBuilder011.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityTestTableSectionGeoInAttr) AddMembershipLowCardRef(lr7 uint64) *InEntityTestTableSectionGeoInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder007.Append(lr7)
	inst.membershipContainerLength007++
	return inst
}
func (inst *InEntityTestTableSectionGeoInAttr) AddMembershipLowCardRefP(lr7 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder007.Append(lr7)
	inst.membershipContainerLength007++
	return
}
func (inst *InEntityTestTableSectionGeoInAttr) AddMembershipMixedLowCardVerbatim(lmv8 []byte, mvhp9 []byte) *InEntityTestTableSectionGeoInAttr {
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
func (inst *InEntityTestTableSectionGeoInAttr) AddMembershipMixedLowCardVerbatimP(lmv8 []byte, mvhp9 []byte) {
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
func (inst *InEntityTestTableSectionGeoInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength007
	inst.membershipContainerLength007 = 0
	inst.membershipSupportFieldBuilder010.Append(uint64(l))
	l = inst.membershipContainerLength008
	inst.membershipContainerLength008 = 0
	inst.membershipSupportFieldBuilder011.Append(uint64(l))
}
func (inst *InEntityTestTableSectionGeoInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityTestTableSectionGeoInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityTestTableSectionGeoInAttr) EndSection() *InEntityTestTable {
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
func (inst *InEntityTestTableSectionGeoInAttr) EndAttribute() *InEntityTestTableSectionGeo {
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

func (inst *InEntityTestTableSectionGeoInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityTestTableSectionGeoInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityTestTableSectionText struct {
	errs                           []error
	inAttr                         *InEntityTestTableSectionTextInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityTestTable
	scalarFieldBuilder012          *array.StringBuilder
	scalarListBuilder012           *array.ListBuilder
	homogenousArrayFieldBuilder013 *array.Uint32Builder
	homogenousArrayListBuilder013  *array.ListBuilder
	homogenousArrayFieldBuilder014 *array.StringBuilder
	homogenousArrayListBuilder014  *array.ListBuilder
}

func NewInEntityTestTableSectionText(builder *array.RecordBuilder, parent *InEntityTestTable) (inst *InEntityTestTableSectionText) {
	inst = &InEntityTestTableSectionText{}
	inAttr := NewInEntityTestTableSectionTextInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder012 = builder.Field(12).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder012 = builder.Field(12).(*array.ListBuilder)
	inst.homogenousArrayFieldBuilder013 = builder.Field(13).(*array.ListBuilder).ValueBuilder().(*array.Uint32Builder)
	inst.homogenousArrayListBuilder013 = builder.Field(13).(*array.ListBuilder)
	inst.homogenousArrayFieldBuilder014 = builder.Field(14).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.homogenousArrayListBuilder014 = builder.Field(14).(*array.ListBuilder)

	return inst
}
func (inst *InEntityTestTableSectionText) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityTestTableSectionText) BeginAttribute(text12 string) *InEntityTestTableSectionTextInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder012.Append(text12)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityTestTableSectionText) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityTestTableSectionText) EndSection() *InEntityTestTable {
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

func (inst *InEntityTestTableSectionText) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
	inst.homogenousArrayListBuilder013.Append(true)
	inst.homogenousArrayListBuilder014.Append(true)

}

func (inst *InEntityTestTableSectionText) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityTestTableSectionText) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityTestTableSectionText) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityTestTableSectionTextInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityTestTableSectionText
	scalarFieldBuilder012                 *array.StringBuilder
	scalarListBuilder012                  *array.ListBuilder
	homogenousArrayFieldBuilder013        *array.Uint32Builder
	homogenousArrayListBuilder013         *array.ListBuilder
	homogenousArrayFieldBuilder014        *array.StringBuilder
	homogenousArrayListBuilder014         *array.ListBuilder
	membershipFieldBuilder015             *array.Uint64Builder
	membershipListBuilder015              *array.ListBuilder
	membershipFieldBuilder016             *array.BinaryBuilder
	membershipListBuilder016              *array.ListBuilder
	membershipFieldBuilder017             *array.BinaryBuilder
	membershipListBuilder017              *array.ListBuilder
	homogenousArraySupportFieldBuilder018 *array.Uint64Builder
	homogenousArraySupportListBuilder018  *array.ListBuilder
	membershipSupportFieldBuilder019      *array.Uint64Builder
	membershipSupportListBuilder019       *array.ListBuilder
	membershipSupportFieldBuilder020      *array.Uint64Builder
	membershipSupportListBuilder020       *array.ListBuilder

	membershipContainerLength015 int

	membershipContainerLength016 int

	membershipContainerLength017 int

	homogenousArrayContainerLength013 int

	homogenousArrayContainerLength014 int
}

func NewInEntityTestTableSectionTextInAttr(builder *array.RecordBuilder, parent *InEntityTestTableSectionText) (inst *InEntityTestTableSectionTextInAttr) {
	inst = &InEntityTestTableSectionTextInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder012 = builder.Field(12).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder012 = builder.Field(12).(*array.ListBuilder)
	inst.homogenousArrayFieldBuilder013 = builder.Field(13).(*array.ListBuilder).ValueBuilder().(*array.Uint32Builder)
	inst.homogenousArrayListBuilder013 = builder.Field(13).(*array.ListBuilder)
	inst.homogenousArrayFieldBuilder014 = builder.Field(14).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.homogenousArrayListBuilder014 = builder.Field(14).(*array.ListBuilder)
	inst.membershipFieldBuilder015 = builder.Field(15).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder015 = builder.Field(15).(*array.ListBuilder)
	inst.membershipFieldBuilder016 = builder.Field(16).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder016 = builder.Field(16).(*array.ListBuilder)
	inst.membershipFieldBuilder017 = builder.Field(17).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder017 = builder.Field(17).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder018 = builder.Field(18).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder018 = builder.Field(18).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder019 = builder.Field(19).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder019 = builder.Field(19).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder020 = builder.Field(20).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder020 = builder.Field(20).(*array.ListBuilder)

	return inst
}
func (inst *InEntityTestTableSectionTextInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder013.Append(true)
	inst.homogenousArrayListBuilder014.Append(true)
	inst.membershipListBuilder015.Append(true)
	inst.membershipListBuilder016.Append(true)
	inst.membershipListBuilder017.Append(true)
	inst.homogenousArrayContainerLength013 = 0
	inst.homogenousArrayContainerLength014 = 0
	inst.membershipContainerLength015 = 0
	inst.membershipContainerLength016 = 0
	inst.membershipContainerLength017 = 0
	inst.scalarListBuilder012.Append(true)
	inst.homogenousArraySupportListBuilder018.Append(true)
	inst.membershipSupportListBuilder019.Append(true)
	inst.membershipSupportListBuilder020.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityTestTableSectionTextInAttr) AddToCoContainers(wordLength13 uint32, words14 string) *InEntityTestTableSectionTextInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder013.Append(wordLength13)
	inst.homogenousArrayContainerLength013++
	inst.homogenousArrayFieldBuilder014.Append(words14)
	inst.homogenousArrayContainerLength014++
	return inst
}
func (inst *InEntityTestTableSectionTextInAttr) AddMembershipLowCardRef(lr15 uint64) *InEntityTestTableSectionTextInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder015.Append(lr15)
	inst.membershipContainerLength015++
	return inst
}
func (inst *InEntityTestTableSectionTextInAttr) AddMembershipLowCardRefP(lr15 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder015.Append(lr15)
	inst.membershipContainerLength015++
	return
}
func (inst *InEntityTestTableSectionTextInAttr) AddMembershipMixedLowCardVerbatim(lmv16 []byte, mvhp17 []byte) *InEntityTestTableSectionTextInAttr {
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
func (inst *InEntityTestTableSectionTextInAttr) AddMembershipMixedLowCardVerbatimP(lmv16 []byte, mvhp17 []byte) {
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
func (inst *InEntityTestTableSectionTextInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength015
	inst.membershipContainerLength015 = 0
	inst.membershipSupportFieldBuilder019.Append(uint64(l))
	l = inst.membershipContainerLength016
	inst.membershipContainerLength016 = 0
	inst.membershipSupportFieldBuilder020.Append(uint64(l))
}
func (inst *InEntityTestTableSectionTextInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength013
	inst.homogenousArrayContainerLength013 = 0
	inst.homogenousArraySupportFieldBuilder018.Append(uint64(l))
}
func (inst *InEntityTestTableSectionTextInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityTestTableSectionTextInAttr) EndSection() *InEntityTestTable {
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
func (inst *InEntityTestTableSectionTextInAttr) EndAttribute() *InEntityTestTableSectionText {
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

func (inst *InEntityTestTableSectionTextInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityTestTableSectionTextInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}
