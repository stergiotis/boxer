// Code generated; Leeway readaccess (github.com/stergiotis/boxer/public/semistructured/leeway/readaccess.test) DO NOT EDIT.

package example

import (
	///////////////////////////////////////////////////////////////////
	// code generator
	// readaccess.(*GeneratorDriver).GenerateGoClasses
	// ./public/semistructured/leeway/readaccess/lw_ra_generator_hl.go:67

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
	"slices"
	///////////////////////////////////////////////////////////////////
	// code generator
	// readaccess.(*GeneratorDriver).GenerateGoClasses
	// ./public/semistructured/leeway/readaccess/lw_ra_generator_hl.go:82
)

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeMembershipPacks
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:208

type MembershipPackTestTableShared1 struct {
	ValueLowCardRef                                 *array.List
	ValueLowCardRefElements                         *array.Uint64
	AccelLowCardRef                                 *runtime.RandomAccessTwoLevelLookupAccel[runtime.MembershipLowCardRefIdx, runtime.AttributeIdx, int, int64]
	ColumnIndexLowCardRef                           uint32
	ColumnIndexLowCardRefAccel                      uint32
	ValueMixedLowCardVerbatim                       *array.List
	ValueMixedLowCardVerbatimElements               *array.Binary
	AccelMixedLowCardVerbatim                       *runtime.RandomAccessTwoLevelLookupAccel[runtime.MembershipMixedLowCardVerbatimIdx, runtime.AttributeIdx, int, int64]
	ColumnIndexMixedLowCardVerbatim                 uint32
	ColumnIndexMixedLowCardVerbatimAccel            uint32
	ValueMixedVerbatimHighCardParameters            *array.List
	ValueMixedVerbatimHighCardParametersElements    *array.Binary
	AccelMixedVerbatimHighCardParameters            *runtime.RandomAccessTwoLevelLookupAccel[runtime.MembershipMixedVerbatimHighCardParametersIdx, runtime.AttributeIdx, int, int64]
	ColumnIndexMixedVerbatimHighCardParameters      uint32
	ColumnIndexMixedVerbatimHighCardParametersAccel uint32
}

func NewMembershipPackTestTableShared1Geo() (inst *MembershipPackTestTableShared1) {
	inst = &MembershipPackTestTableShared1{}
	inst.AccelLowCardRef = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.MembershipLowCardRefIdx, runtime.AttributeIdx, int, int64](128)
	inst.AccelMixedLowCardVerbatim = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.MembershipMixedLowCardVerbatimIdx, runtime.AttributeIdx, int, int64](128)
	inst.AccelMixedVerbatimHighCardParameters = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.MembershipMixedVerbatimHighCardParametersIdx, runtime.AttributeIdx, int, int64](128)
	inst.ColumnIndexLowCardRef = 7
	inst.ColumnIndexLowCardRefAccel = 10
	inst.ColumnIndexMixedLowCardVerbatim = 8
	inst.ColumnIndexMixedLowCardVerbatimAccel = 11
	inst.ColumnIndexMixedVerbatimHighCardParameters = 9
	inst.ColumnIndexMixedVerbatimHighCardParametersAccel = 11
	return
}

func (inst *MembershipPackTestTableShared1) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
		inst.ColumnIndexLowCardRef,
		inst.ColumnIndexLowCardRefAccel,
		inst.ColumnIndexMixedLowCardVerbatim,
		inst.ColumnIndexMixedLowCardVerbatimAccel,
		inst.ColumnIndexMixedVerbatimHighCardParameters,
		inst.ColumnIndexMixedVerbatimHighCardParametersAccel,
	}
	return
}

func (inst *MembershipPackTestTableShared1) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
		"MembershipPackTestTableShared1.ColumnIndexLowCardRef",
		"MembershipPackTestTableShared1.ColumnIndexLowCardRefAccel",
		"MembershipPackTestTableShared1.ColumnIndexMixedLowCardVerbatim",
		"MembershipPackTestTableShared1.ColumnIndexMixedLowCardVerbatimAccel",
		"MembershipPackTestTableShared1.ColumnIndexMixedVerbatimHighCardParameters",
		"MembershipPackTestTableShared1.ColumnIndexMixedVerbatimHighCardParametersAccel",
	}
	return
}

func (inst *MembershipPackTestTableShared1) SetColumnIndices(indices []uint32) (rest []uint32) {
	inst.ColumnIndexLowCardRef = indices[0]
	inst.ColumnIndexLowCardRefAccel = indices[1]
	inst.ColumnIndexMixedLowCardVerbatim = indices[2]
	inst.ColumnIndexMixedLowCardVerbatimAccel = indices[3]
	inst.ColumnIndexMixedVerbatimHighCardParameters = indices[4]
	inst.ColumnIndexMixedVerbatimHighCardParametersAccel = indices[5]

	rest = indices[6:]
	return
}

var _ runtime.ColumnIndexHandlingI = (*MembershipPackTestTableShared1)(nil)

func NewMembershipPackTestTableShared1Text() (inst *MembershipPackTestTableShared1) {
	inst = &MembershipPackTestTableShared1{}
	inst.AccelLowCardRef = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.MembershipLowCardRefIdx, runtime.AttributeIdx, int, int64](128)
	inst.AccelMixedLowCardVerbatim = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.MembershipMixedLowCardVerbatimIdx, runtime.AttributeIdx, int, int64](128)
	inst.AccelMixedVerbatimHighCardParameters = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.MembershipMixedVerbatimHighCardParametersIdx, runtime.AttributeIdx, int, int64](128)
	inst.ColumnIndexLowCardRef = 15
	inst.ColumnIndexLowCardRefAccel = 20
	inst.ColumnIndexMixedLowCardVerbatim = 16
	inst.ColumnIndexMixedLowCardVerbatimAccel = 21
	inst.ColumnIndexMixedVerbatimHighCardParameters = 17
	inst.ColumnIndexMixedVerbatimHighCardParametersAccel = 21
	return
}

func (inst *MembershipPackTestTableShared1) Release() {
	runtime.ReleaseIfNotNil(inst.ValueLowCardRef)
	runtime.ReleaseIfNotNil(inst.ValueLowCardRefElements)
	runtime.ReleaseIfNotNil(inst.ValueMixedLowCardVerbatim)
	runtime.ReleaseIfNotNil(inst.ValueMixedLowCardVerbatimElements)
	runtime.ReleaseIfNotNil(inst.ValueMixedVerbatimHighCardParameters)
	runtime.ReleaseIfNotNil(inst.ValueMixedVerbatimHighCardParametersElements)
}

func (inst *MembershipPackTestTableShared1) Reset() {
	//inst.Release()
	inst.ValueLowCardRef = nil
	inst.ValueLowCardRefElements = nil
	inst.ValueMixedLowCardVerbatim = nil
	inst.ValueMixedLowCardVerbatimElements = nil
	inst.ValueMixedVerbatimHighCardParameters = nil
	inst.ValueMixedVerbatimHighCardParametersElements = nil
}

func (inst *MembershipPackTestTableShared1) LoadFromRecord(rec arrow.Record) (err error) {
	{
		err = runtime.LoadNonScalarValueFieldFromRecord(int(inst.ColumnIndexLowCardRef), arrow.UINT64, rec, &inst.ValueLowCardRef, &inst.ValueLowCardRefElements, array.NewUint64Data)
		if err != nil {
			return
		}
	}
	{
		err = runtime.LoadAccelFieldFromRecord(int(inst.ColumnIndexLowCardRefAccel), rec, inst.AccelLowCardRef)
		if err != nil {
			return
		}
	}
	{
		err = runtime.LoadNonScalarValueFieldFromRecord(int(inst.ColumnIndexMixedLowCardVerbatim), arrow.BINARY, rec, &inst.ValueMixedLowCardVerbatim, &inst.ValueMixedLowCardVerbatimElements, array.NewBinaryData)
		if err != nil {
			return
		}
	}
	{
		err = runtime.LoadAccelFieldFromRecord(int(inst.ColumnIndexMixedLowCardVerbatimAccel), rec, inst.AccelMixedLowCardVerbatim)
		if err != nil {
			return
		}
	}
	{
		err = runtime.LoadNonScalarValueFieldFromRecord(int(inst.ColumnIndexMixedVerbatimHighCardParameters), arrow.BINARY, rec, &inst.ValueMixedVerbatimHighCardParameters, &inst.ValueMixedVerbatimHighCardParametersElements, array.NewBinaryData)
		if err != nil {
			return
		}
	}
	{
		err = runtime.LoadAccelFieldFromRecord(int(inst.ColumnIndexMixedLowCardVerbatimAccel), rec, inst.AccelMixedVerbatimHighCardParameters)
		if err != nil {
			return
		}
	}
	return
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionInnerClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:579

type ReadAccessTestTablePlainEntityIdScalar struct {
	ValueId       *array.Uint64
	ColumnIndexId uint32
}

type ReadAccessTestTablePlainEntityTimestampHomogenousArray struct {
	ValueProc         *array.List
	ColumnIndexProc   uint32
	ValueProcElements *array.Date32
}

type ReadAccessTestTablePlainEntityTimestampScalar struct {
	ValueTs       *array.Date32
	ColumnIndexTs uint32
}

type ReadAccessTestTableTaggedGeoScalar struct {
	ValueLat            *array.List
	ColumnIndexLat      uint32
	ValueLatElements    *array.Float32
	ValueLng            *array.List
	ColumnIndexLng      uint32
	ValueLngElements    *array.Float32
	ValueH3Res1         *array.List
	ColumnIndexH3Res1   uint32
	ValueH3Res1Elements *array.Uint64
	ValueH3Res2         *array.List
	ColumnIndexH3Res2   uint32
	ValueH3Res2Elements *array.Uint64
}

type ReadAccessTestTableTaggedTextHomogenousArray struct {
	ValueWords         *array.List
	ColumnIndexWords   uint32
	ValueWordsElements *array.String
}

type ReadAccessTestTableTaggedTextHomogenousArraySupport struct {
	Accel       *runtime.RandomAccessTwoLevelLookupAccel[runtime.AttributeIdx, runtime.HomogenousArrayIdx, int, int64]
	ColumnIndex uint32
}

type ReadAccessTestTableTaggedTextScalar struct {
	ValueText         *array.List
	ColumnIndexText   uint32
	ValueTextElements *array.String
}

type ReadAccessTestTableTaggedTextSet struct {
	ValueBagOfWords         *array.List
	ColumnIndexBagOfWords   uint32
	ValueBagOfWordsElements *array.String
}

type ReadAccessTestTableTaggedTextSetSupport struct {
	Accel       *runtime.RandomAccessTwoLevelLookupAccel[runtime.AttributeIdx, runtime.SetIdx, int, int64]
	ColumnIndex uint32
}

func NewReadAccessTestTablePlainEntityIdScalar() (inst *ReadAccessTestTablePlainEntityIdScalar) {
	inst = &ReadAccessTestTablePlainEntityIdScalar{}
	inst.ColumnIndexId = 0
	return
}

func (inst *ReadAccessTestTablePlainEntityIdScalar) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
		inst.ColumnIndexId,
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityIdScalar) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
		"ReadAccessTestTablePlainEntityIdScalar.ColumnIndexId",
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityIdScalar) SetColumnIndices(indices []uint32) (rest []uint32) {
	inst.ColumnIndexId = indices[0]

	rest = indices[1:]
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTablePlainEntityIdScalar)(nil)

func NewReadAccessTestTablePlainEntityTimestampHomogenousArray() (inst *ReadAccessTestTablePlainEntityTimestampHomogenousArray) {
	inst = &ReadAccessTestTablePlainEntityTimestampHomogenousArray{}
	inst.ColumnIndexProc = 2
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestampHomogenousArray) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
		inst.ColumnIndexProc,
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestampHomogenousArray) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
		"ReadAccessTestTablePlainEntityTimestampHomogenousArray.ColumnIndexProc",
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestampHomogenousArray) SetColumnIndices(indices []uint32) (rest []uint32) {
	inst.ColumnIndexProc = indices[0]

	rest = indices[1:]
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTablePlainEntityTimestampHomogenousArray)(nil)

func NewReadAccessTestTablePlainEntityTimestampScalar() (inst *ReadAccessTestTablePlainEntityTimestampScalar) {
	inst = &ReadAccessTestTablePlainEntityTimestampScalar{}
	inst.ColumnIndexTs = 1
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestampScalar) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
		inst.ColumnIndexTs,
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestampScalar) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
		"ReadAccessTestTablePlainEntityTimestampScalar.ColumnIndexTs",
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestampScalar) SetColumnIndices(indices []uint32) (rest []uint32) {
	inst.ColumnIndexTs = indices[0]

	rest = indices[1:]
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTablePlainEntityTimestampScalar)(nil)

func NewReadAccessTestTableTaggedGeoScalar() (inst *ReadAccessTestTableTaggedGeoScalar) {
	inst = &ReadAccessTestTableTaggedGeoScalar{}
	inst.ColumnIndexLat = 3
	inst.ColumnIndexLng = 4
	inst.ColumnIndexH3Res1 = 5
	inst.ColumnIndexH3Res2 = 6
	return
}

func (inst *ReadAccessTestTableTaggedGeoScalar) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
		inst.ColumnIndexLat,
		inst.ColumnIndexLng,
		inst.ColumnIndexH3Res1,
		inst.ColumnIndexH3Res2,
	}
	return
}

func (inst *ReadAccessTestTableTaggedGeoScalar) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
		"ReadAccessTestTableTaggedGeoScalar.ColumnIndexLat",
		"ReadAccessTestTableTaggedGeoScalar.ColumnIndexLng",
		"ReadAccessTestTableTaggedGeoScalar.ColumnIndexH3Res1",
		"ReadAccessTestTableTaggedGeoScalar.ColumnIndexH3Res2",
	}
	return
}

func (inst *ReadAccessTestTableTaggedGeoScalar) SetColumnIndices(indices []uint32) (rest []uint32) {
	inst.ColumnIndexLat = indices[0]
	inst.ColumnIndexLng = indices[1]
	inst.ColumnIndexH3Res1 = indices[2]
	inst.ColumnIndexH3Res2 = indices[3]

	rest = indices[4:]
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTableTaggedGeoScalar)(nil)

func NewReadAccessTestTableTaggedTextHomogenousArray() (inst *ReadAccessTestTableTaggedTextHomogenousArray) {
	inst = &ReadAccessTestTableTaggedTextHomogenousArray{}
	inst.ColumnIndexWords = 13
	return
}

func (inst *ReadAccessTestTableTaggedTextHomogenousArray) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
		inst.ColumnIndexWords,
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextHomogenousArray) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
		"ReadAccessTestTableTaggedTextHomogenousArray.ColumnIndexWords",
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextHomogenousArray) SetColumnIndices(indices []uint32) (rest []uint32) {
	inst.ColumnIndexWords = indices[0]

	rest = indices[1:]
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTableTaggedTextHomogenousArray)(nil)

func NewReadAccessTestTableTaggedTextHomogenousArraySupport() (inst *ReadAccessTestTableTaggedTextHomogenousArraySupport) {
	inst = &ReadAccessTestTableTaggedTextHomogenousArraySupport{}
	inst.ColumnIndex = 18
	inst.Accel = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.AttributeIdx, runtime.HomogenousArrayIdx, int, int64](128)
	return
}

func (inst *ReadAccessTestTableTaggedTextHomogenousArraySupport) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
		inst.ColumnIndex,
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextHomogenousArraySupport) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
		"ReadAccessTestTableTaggedTextHomogenousArraySupport.ColumnIndex",
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextHomogenousArraySupport) SetColumnIndices(indices []uint32) (rest []uint32) {
	inst.ColumnIndex = indices[0]

	rest = indices[1:]
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTableTaggedTextHomogenousArraySupport)(nil)

func NewReadAccessTestTableTaggedTextScalar() (inst *ReadAccessTestTableTaggedTextScalar) {
	inst = &ReadAccessTestTableTaggedTextScalar{}
	inst.ColumnIndexText = 12
	return
}

func (inst *ReadAccessTestTableTaggedTextScalar) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
		inst.ColumnIndexText,
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextScalar) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
		"ReadAccessTestTableTaggedTextScalar.ColumnIndexText",
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextScalar) SetColumnIndices(indices []uint32) (rest []uint32) {
	inst.ColumnIndexText = indices[0]

	rest = indices[1:]
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTableTaggedTextScalar)(nil)

func NewReadAccessTestTableTaggedTextSet() (inst *ReadAccessTestTableTaggedTextSet) {
	inst = &ReadAccessTestTableTaggedTextSet{}
	inst.ColumnIndexBagOfWords = 14
	return
}

func (inst *ReadAccessTestTableTaggedTextSet) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
		inst.ColumnIndexBagOfWords,
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextSet) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
		"ReadAccessTestTableTaggedTextSet.ColumnIndexBagOfWords",
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextSet) SetColumnIndices(indices []uint32) (rest []uint32) {
	inst.ColumnIndexBagOfWords = indices[0]

	rest = indices[1:]
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTableTaggedTextSet)(nil)

func NewReadAccessTestTableTaggedTextSetSupport() (inst *ReadAccessTestTableTaggedTextSetSupport) {
	inst = &ReadAccessTestTableTaggedTextSetSupport{}
	inst.ColumnIndex = 19
	inst.Accel = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.AttributeIdx, runtime.SetIdx, int, int64](128)
	return
}

func (inst *ReadAccessTestTableTaggedTextSetSupport) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
		inst.ColumnIndex,
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextSetSupport) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
		"ReadAccessTestTableTaggedTextSetSupport.ColumnIndex",
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextSetSupport) SetColumnIndices(indices []uint32) (rest []uint32) {
	inst.ColumnIndex = indices[0]

	rest = indices[1:]
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTableTaggedTextSetSupport)(nil)

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionInnerClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:798

func (inst *ReadAccessTestTablePlainEntityIdScalar) Reset() {
	inst.ValueId = nil
}

func (inst *ReadAccessTestTablePlainEntityTimestampHomogenousArray) Reset() {
	inst.ValueProc = nil
	inst.ValueProcElements = nil
}

func (inst *ReadAccessTestTablePlainEntityTimestampScalar) Reset() {
	inst.ValueTs = nil
}

func (inst *ReadAccessTestTableTaggedGeoScalar) Reset() {
	inst.ValueLat = nil
	inst.ValueLatElements = nil
	inst.ValueLng = nil
	inst.ValueLngElements = nil
	inst.ValueH3Res1 = nil
	inst.ValueH3Res1Elements = nil
	inst.ValueH3Res2 = nil
	inst.ValueH3Res2Elements = nil
}

func (inst *ReadAccessTestTableTaggedTextHomogenousArray) Reset() {
	inst.ValueWords = nil
	inst.ValueWordsElements = nil
}

func (inst *ReadAccessTestTableTaggedTextScalar) Reset() {
	inst.ValueText = nil
	inst.ValueTextElements = nil
}

func (inst *ReadAccessTestTableTaggedTextSet) Reset() {
	inst.ValueBagOfWords = nil
	inst.ValueBagOfWordsElements = nil
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionInnerClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:856

var _ runtime.ReleasableI = (*ReadAccessTestTablePlainEntityIdScalar)(nil)

func (inst *ReadAccessTestTablePlainEntityIdScalar) Release() {
	runtime.ReleaseIfNotNil(inst.ValueId)
}

var _ runtime.ReleasableI = (*ReadAccessTestTablePlainEntityTimestampHomogenousArray)(nil)

func (inst *ReadAccessTestTablePlainEntityTimestampHomogenousArray) Release() {
	runtime.ReleaseIfNotNil(inst.ValueProc)
	runtime.ReleaseIfNotNil(inst.ValueProcElements)
}

var _ runtime.ReleasableI = (*ReadAccessTestTablePlainEntityTimestampScalar)(nil)

func (inst *ReadAccessTestTablePlainEntityTimestampScalar) Release() {
	runtime.ReleaseIfNotNil(inst.ValueTs)
}

var _ runtime.ReleasableI = (*ReadAccessTestTableTaggedGeoScalar)(nil)

func (inst *ReadAccessTestTableTaggedGeoScalar) Release() {
	runtime.ReleaseIfNotNil(inst.ValueLat)
	runtime.ReleaseIfNotNil(inst.ValueLatElements)
	runtime.ReleaseIfNotNil(inst.ValueLng)
	runtime.ReleaseIfNotNil(inst.ValueLngElements)
	runtime.ReleaseIfNotNil(inst.ValueH3Res1)
	runtime.ReleaseIfNotNil(inst.ValueH3Res1Elements)
	runtime.ReleaseIfNotNil(inst.ValueH3Res2)
	runtime.ReleaseIfNotNil(inst.ValueH3Res2Elements)
}

var _ runtime.ReleasableI = (*ReadAccessTestTableTaggedTextHomogenousArray)(nil)

func (inst *ReadAccessTestTableTaggedTextHomogenousArray) Release() {
	runtime.ReleaseIfNotNil(inst.ValueWords)
	runtime.ReleaseIfNotNil(inst.ValueWordsElements)
}

var _ runtime.ReleasableI = (*ReadAccessTestTableTaggedTextHomogenousArraySupport)(nil)

func (inst *ReadAccessTestTableTaggedTextHomogenousArraySupport) Release() {
	// nothing to release
}

var _ runtime.ReleasableI = (*ReadAccessTestTableTaggedTextScalar)(nil)

func (inst *ReadAccessTestTableTaggedTextScalar) Release() {
	runtime.ReleaseIfNotNil(inst.ValueText)
	runtime.ReleaseIfNotNil(inst.ValueTextElements)
}

var _ runtime.ReleasableI = (*ReadAccessTestTableTaggedTextSet)(nil)

func (inst *ReadAccessTestTableTaggedTextSet) Release() {
	runtime.ReleaseIfNotNil(inst.ValueBagOfWords)
	runtime.ReleaseIfNotNil(inst.ValueBagOfWordsElements)
}

var _ runtime.ReleasableI = (*ReadAccessTestTableTaggedTextSetSupport)(nil)

func (inst *ReadAccessTestTableTaggedTextSetSupport) Release() {
	// nothing to release
}

func (inst *ReadAccessTestTablePlainEntityIdScalar) Len() (l int) {
	if inst.ValueId != nil {
		l = inst.ValueId.Len()
	}
	return
}
func (inst *ReadAccessTestTablePlainEntityTimestampScalar) Len() (l int) {
	if inst.ValueTs != nil {
		l = inst.ValueTs.Len()
	}
	return
}
func (inst *ReadAccessTestTablePlainEntityTimestampHomogenousArray) Len() (l int) {
	if inst.ValueProc != nil {
		l = inst.ValueProc.Len()
	}
	return
}
func (inst *ReadAccessTestTableTaggedGeoScalar) Len() (l int) {
	if inst.ValueLat != nil {
		l = inst.ValueLat.Len()
	}
	return
}
func (inst *ReadAccessTestTableTaggedTextScalar) Len() (l int) {
	if inst.ValueText != nil {
		l = inst.ValueText.Len()
	}
	return
}
func (inst *ReadAccessTestTableTaggedTextHomogenousArray) Len() (l int) {
	if inst.ValueWords != nil {
		l = inst.ValueWords.Len()
	}
	return
}
func (inst *ReadAccessTestTableTaggedTextSet) Len() (l int) {
	if inst.ValueBagOfWords != nil {
		l = inst.ValueBagOfWords.Len()
	}
	return
}
func (inst *ReadAccessTestTableTaggedTextHomogenousArraySupport) Len() (l int) {
	if inst.Accel != nil {
		l = inst.Accel.Len()
	}
	return
}
func (inst *ReadAccessTestTableTaggedTextSetSupport) Len() (l int) {
	if inst.Accel != nil {
		l = inst.Accel.Len()
	}
	return
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionInnerClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:971

func (inst *ReadAccessTestTablePlainEntityIdScalar) LoadFromRecord(rec arrow.Record) (err error) {
	{
		err = runtime.LoadScalarValueFieldFromRecord(int(inst.ColumnIndexId), arrow.UINT64, rec, &inst.ValueId, array.NewUint64Data)
		if err != nil {
			return
		}
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestampHomogenousArray) LoadFromRecord(rec arrow.Record) (err error) {
	{
		err = runtime.LoadNonScalarValueFieldFromRecord(int(inst.ColumnIndexProc), arrow.DATE32, rec, &inst.ValueProc, &inst.ValueProcElements, array.NewDate32Data)
		if err != nil {
			return
		}
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestampScalar) LoadFromRecord(rec arrow.Record) (err error) {
	{
		err = runtime.LoadScalarValueFieldFromRecord(int(inst.ColumnIndexTs), arrow.DATE32, rec, &inst.ValueTs, array.NewDate32Data)
		if err != nil {
			return
		}
	}
	return
}

func (inst *ReadAccessTestTableTaggedGeoScalar) LoadFromRecord(rec arrow.Record) (err error) {
	{
		err = runtime.LoadNonScalarValueFieldFromRecord(int(inst.ColumnIndexLat), arrow.FLOAT32, rec, &inst.ValueLat, &inst.ValueLatElements, array.NewFloat32Data)
		if err != nil {
			return
		}
	}
	{
		err = runtime.LoadNonScalarValueFieldFromRecord(int(inst.ColumnIndexLng), arrow.FLOAT32, rec, &inst.ValueLng, &inst.ValueLngElements, array.NewFloat32Data)
		if err != nil {
			return
		}
	}
	{
		err = runtime.LoadNonScalarValueFieldFromRecord(int(inst.ColumnIndexH3Res1), arrow.UINT64, rec, &inst.ValueH3Res1, &inst.ValueH3Res1Elements, array.NewUint64Data)
		if err != nil {
			return
		}
	}
	{
		err = runtime.LoadNonScalarValueFieldFromRecord(int(inst.ColumnIndexH3Res2), arrow.UINT64, rec, &inst.ValueH3Res2, &inst.ValueH3Res2Elements, array.NewUint64Data)
		if err != nil {
			return
		}
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextHomogenousArray) LoadFromRecord(rec arrow.Record) (err error) {
	{
		err = runtime.LoadNonScalarValueFieldFromRecord(int(inst.ColumnIndexWords), arrow.STRING, rec, &inst.ValueWords, &inst.ValueWordsElements, array.NewStringData)
		if err != nil {
			return
		}
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextHomogenousArraySupport) LoadFromRecord(rec arrow.Record) (err error) {
	{
		err = runtime.LoadAccelFieldFromRecord(int(inst.ColumnIndex), rec, inst.Accel)
		if err != nil {
			return
		}
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextScalar) LoadFromRecord(rec arrow.Record) (err error) {
	{
		err = runtime.LoadNonScalarValueFieldFromRecord(int(inst.ColumnIndexText), arrow.STRING, rec, &inst.ValueText, &inst.ValueTextElements, array.NewStringData)
		if err != nil {
			return
		}
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextSet) LoadFromRecord(rec arrow.Record) (err error) {
	{
		err = runtime.LoadNonScalarValueFieldFromRecord(int(inst.ColumnIndexBagOfWords), arrow.STRING, rec, &inst.ValueBagOfWords, &inst.ValueBagOfWordsElements, array.NewStringData)
		if err != nil {
			return
		}
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextSetSupport) LoadFromRecord(rec arrow.Record) (err error) {
	{
		err = runtime.LoadAccelFieldFromRecord(int(inst.ColumnIndex), rec, inst.Accel)
		if err != nil {
			return
		}
	}
	return
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:1126

type ReadAccessTestTablePlainEntityId struct {
	ValueScalar *ReadAccessTestTablePlainEntityIdScalar
}

type ReadAccessTestTablePlainEntityTimestamp struct {
	ValueHomogenousArray *ReadAccessTestTablePlainEntityTimestampHomogenousArray
	ValueScalar          *ReadAccessTestTablePlainEntityTimestampScalar
}

func NewReadAccessTestTablePlainEntityId() (inst *ReadAccessTestTablePlainEntityId) {
	inst = &ReadAccessTestTablePlainEntityId{}
	inst.ValueScalar = NewReadAccessTestTablePlainEntityIdScalar()
	return
}

func NewReadAccessTestTablePlainEntityTimestamp() (inst *ReadAccessTestTablePlainEntityTimestamp) {
	inst = &ReadAccessTestTablePlainEntityTimestamp{}
	inst.ValueHomogenousArray = NewReadAccessTestTablePlainEntityTimestampHomogenousArray()
	inst.ValueScalar = NewReadAccessTestTablePlainEntityTimestampScalar()
	return
}

func (inst *ReadAccessTestTablePlainEntityId) SetColumnIndices(indices []uint32) (restIndices []uint32) {
	restIndices = indices
	restIndices = slices.Concat(restIndices, inst.ValueScalar.SetColumnIndices(restIndices))
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestamp) SetColumnIndices(indices []uint32) (restIndices []uint32) {
	restIndices = indices
	restIndices = slices.Concat(restIndices, inst.ValueHomogenousArray.SetColumnIndices(restIndices))
	restIndices = slices.Concat(restIndices, inst.ValueScalar.SetColumnIndices(restIndices))
	return
}

func (inst *ReadAccessTestTablePlainEntityId) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = slices.Concat(columnIndices, inst.ValueScalar.GetColumnIndices())
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestamp) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = slices.Concat(columnIndices, inst.ValueHomogenousArray.GetColumnIndices())
	columnIndices = slices.Concat(columnIndices, inst.ValueScalar.GetColumnIndices())
	return
}

func (inst *ReadAccessTestTablePlainEntityId) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = slices.Concat(fieldNames, inst.ValueScalar.GetColumnIndexFieldNames())
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTablePlainEntityId)(nil)

func (inst *ReadAccessTestTablePlainEntityTimestamp) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = slices.Concat(fieldNames, inst.ValueHomogenousArray.GetColumnIndexFieldNames())
	fieldNames = slices.Concat(fieldNames, inst.ValueScalar.GetColumnIndexFieldNames())
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTablePlainEntityTimestamp)(nil)

func (inst *ReadAccessTestTablePlainEntityId) Release() {
	runtime.ReleaseIfNotNil(inst.ValueScalar)
}

func (inst *ReadAccessTestTablePlainEntityTimestamp) Release() {
	runtime.ReleaseIfNotNil(inst.ValueHomogenousArray)
	runtime.ReleaseIfNotNil(inst.ValueScalar)
}

func (inst *ReadAccessTestTablePlainEntityId) LoadFromRecord(rec arrow.Record) (err error) {
	err = inst.ValueScalar.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Str("fieldName", "ValueScalar").Errorf("unable to load from record: %w", err)
		return
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestamp) LoadFromRecord(rec arrow.Record) (err error) {
	err = inst.ValueHomogenousArray.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Str("fieldName", "ValueHomogenousArray").Errorf("unable to load from record: %w", err)
		return
	}
	err = inst.ValueScalar.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Str("fieldName", "ValueScalar").Errorf("unable to load from record: %w", err)
		return
	}
	return
}

type ReadAccessTestTableTaggedGeo struct {
	ValueScalar *ReadAccessTestTableTaggedGeoScalar
	Membership  *MembershipPackTestTableShared1
}

type ReadAccessTestTableTaggedText struct {
	ValueScalar            *ReadAccessTestTableTaggedTextScalar
	ValueHomogenousArray   *ReadAccessTestTableTaggedTextHomogenousArray
	ValueSet               *ReadAccessTestTableTaggedTextSet
	SupportHomogenousArray *ReadAccessTestTableTaggedTextHomogenousArraySupport
	SupportSet             *ReadAccessTestTableTaggedTextSetSupport
	Membership             *MembershipPackTestTableShared1
}

func NewReadAccessTestTableTaggedGeo() (inst *ReadAccessTestTableTaggedGeo) {
	inst = &ReadAccessTestTableTaggedGeo{}
	inst.ValueScalar = NewReadAccessTestTableTaggedGeoScalar()
	inst.Membership = NewMembershipPackTestTableShared1Geo()
	return
}

func NewReadAccessTestTableTaggedText() (inst *ReadAccessTestTableTaggedText) {
	inst = &ReadAccessTestTableTaggedText{}
	inst.ValueScalar = NewReadAccessTestTableTaggedTextScalar()
	inst.ValueHomogenousArray = NewReadAccessTestTableTaggedTextHomogenousArray()
	inst.ValueSet = NewReadAccessTestTableTaggedTextSet()
	inst.SupportHomogenousArray = NewReadAccessTestTableTaggedTextHomogenousArraySupport()
	inst.SupportSet = NewReadAccessTestTableTaggedTextSetSupport()
	inst.Membership = NewMembershipPackTestTableShared1Text()
	return
}

func (inst *ReadAccessTestTableTaggedGeo) SetColumnIndices(indices []uint32) (restIndices []uint32) {
	restIndices = indices
	if inst.ValueScalar != nil {
		restIndices = inst.ValueScalar.SetColumnIndices(restIndices)
	}
	if inst.Membership != nil {
		restIndices = inst.Membership.SetColumnIndices(restIndices)
	}
	return
}

func (inst *ReadAccessTestTableTaggedText) SetColumnIndices(indices []uint32) (restIndices []uint32) {
	restIndices = indices
	if inst.ValueScalar != nil {
		restIndices = inst.ValueScalar.SetColumnIndices(restIndices)
	}
	if inst.ValueHomogenousArray != nil {
		restIndices = inst.ValueHomogenousArray.SetColumnIndices(restIndices)
	}
	if inst.ValueSet != nil {
		restIndices = inst.ValueSet.SetColumnIndices(restIndices)
	}
	if inst.SupportHomogenousArray != nil {
		restIndices = inst.SupportHomogenousArray.SetColumnIndices(restIndices)
	}
	if inst.SupportSet != nil {
		restIndices = inst.SupportSet.SetColumnIndices(restIndices)
	}
	if inst.Membership != nil {
		restIndices = inst.Membership.SetColumnIndices(restIndices)
	}
	return
}

func (inst *ReadAccessTestTableTaggedGeo) GetColumnIndices() (columnIndices []uint32) {
	if inst.ValueScalar != nil {
		columnIndices = slices.Concat(columnIndices, inst.ValueScalar.GetColumnIndices())
	}
	if inst.Membership != nil {
		columnIndices = slices.Concat(columnIndices, inst.Membership.GetColumnIndices())
	}
	return
}

func (inst *ReadAccessTestTableTaggedText) GetColumnIndices() (columnIndices []uint32) {
	if inst.ValueScalar != nil {
		columnIndices = slices.Concat(columnIndices, inst.ValueScalar.GetColumnIndices())
	}
	if inst.ValueHomogenousArray != nil {
		columnIndices = slices.Concat(columnIndices, inst.ValueHomogenousArray.GetColumnIndices())
	}
	if inst.ValueSet != nil {
		columnIndices = slices.Concat(columnIndices, inst.ValueSet.GetColumnIndices())
	}
	if inst.SupportHomogenousArray != nil {
		columnIndices = slices.Concat(columnIndices, inst.SupportHomogenousArray.GetColumnIndices())
	}
	if inst.SupportSet != nil {
		columnIndices = slices.Concat(columnIndices, inst.SupportSet.GetColumnIndices())
	}
	if inst.Membership != nil {
		columnIndices = slices.Concat(columnIndices, inst.Membership.GetColumnIndices())
	}
	return
}

func (inst *ReadAccessTestTableTaggedGeo) GetColumnIndexFieldNames() (columnIndexFieldNames []string) {
	if inst.ValueScalar != nil {
		columnIndexFieldNames = slices.Concat(columnIndexFieldNames, inst.ValueScalar.GetColumnIndexFieldNames())
	}
	if inst.Membership != nil {
		columnIndexFieldNames = slices.Concat(columnIndexFieldNames, inst.Membership.GetColumnIndexFieldNames())
	}
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTableTaggedGeo)(nil)

func (inst *ReadAccessTestTableTaggedText) GetColumnIndexFieldNames() (columnIndexFieldNames []string) {
	if inst.ValueScalar != nil {
		columnIndexFieldNames = slices.Concat(columnIndexFieldNames, inst.ValueScalar.GetColumnIndexFieldNames())
	}
	if inst.ValueHomogenousArray != nil {
		columnIndexFieldNames = slices.Concat(columnIndexFieldNames, inst.ValueHomogenousArray.GetColumnIndexFieldNames())
	}
	if inst.ValueSet != nil {
		columnIndexFieldNames = slices.Concat(columnIndexFieldNames, inst.ValueSet.GetColumnIndexFieldNames())
	}
	if inst.SupportHomogenousArray != nil {
		columnIndexFieldNames = slices.Concat(columnIndexFieldNames, inst.SupportHomogenousArray.GetColumnIndexFieldNames())
	}
	if inst.SupportSet != nil {
		columnIndexFieldNames = slices.Concat(columnIndexFieldNames, inst.SupportSet.GetColumnIndexFieldNames())
	}
	if inst.Membership != nil {
		columnIndexFieldNames = slices.Concat(columnIndexFieldNames, inst.Membership.GetColumnIndexFieldNames())
	}
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTableTaggedText)(nil)

func (inst *ReadAccessTestTableTaggedGeo) Release() {
	runtime.ReleaseIfNotNil(inst.ValueScalar)
	runtime.ReleaseIfNotNil(inst.Membership)
}

func (inst *ReadAccessTestTableTaggedText) Release() {
	runtime.ReleaseIfNotNil(inst.ValueScalar)
	runtime.ReleaseIfNotNil(inst.ValueHomogenousArray)
	runtime.ReleaseIfNotNil(inst.ValueSet)
	runtime.ReleaseIfNotNil(inst.SupportHomogenousArray)
	runtime.ReleaseIfNotNil(inst.SupportSet)
	runtime.ReleaseIfNotNil(inst.Membership)
}

func (inst *ReadAccessTestTableTaggedGeo) LoadFromRecord(rec arrow.Record) (err error) {
	err = inst.ValueScalar.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Str("innerClassName", "ReadAccessTestTableTaggedGeoScalar").Errorf("unable to load from record: %w", err)
		return
	}
	err = inst.Membership.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Str("innerClassName", "MembershipPackTestTableShared1").Errorf("unable to load from record: %w", err)
		return
	}
	return
}

func (inst *ReadAccessTestTableTaggedText) LoadFromRecord(rec arrow.Record) (err error) {
	err = inst.ValueScalar.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Str("innerClassName", "ReadAccessTestTableTaggedTextScalar").Errorf("unable to load from record: %w", err)
		return
	}
	err = inst.ValueHomogenousArray.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Str("innerClassName", "ReadAccessTestTableTaggedTextHomogenousArray").Errorf("unable to load from record: %w", err)
		return
	}
	err = inst.ValueSet.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Str("innerClassName", "ReadAccessTestTableTaggedTextSet").Errorf("unable to load from record: %w", err)
		return
	}
	err = inst.SupportHomogenousArray.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Str("innerClassName", "ReadAccessTestTableTaggedTextHomogenousArraySupport").Errorf("unable to load from record: %w", err)
		return
	}
	err = inst.SupportSet.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Str("innerClassName", "ReadAccessTestTableTaggedTextSetSupport").Errorf("unable to load from record: %w", err)
		return
	}
	err = inst.Membership.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Str("innerClassName", "MembershipPackTestTableShared1").Errorf("unable to load from record: %w", err)
		return
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityId) GetNumberOfEntities() (nEntities int) {
	if inst.ValueScalar != nil {
		nEntities = inst.ValueScalar.Len()
	}
	return
}
func (inst *ReadAccessTestTablePlainEntityTimestamp) GetNumberOfEntities() (nEntities int) {
	if inst.ValueHomogenousArray != nil {
		nEntities = inst.ValueHomogenousArray.Len()
	}
	return
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeEntityClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:1848

type ReadAccessTestTable struct {
	EntityId        *ReadAccessTestTablePlainEntityId
	EntityTimestamp *ReadAccessTestTablePlainEntityTimestamp
	Geo             *ReadAccessTestTableTaggedGeo
	Text            *ReadAccessTestTableTaggedText
}

func NewReadAccessTestTable() (inst *ReadAccessTestTable) {
	inst = &ReadAccessTestTable{}
	inst.EntityId = NewReadAccessTestTablePlainEntityId()
	inst.EntityTimestamp = NewReadAccessTestTablePlainEntityTimestamp()
	inst.Geo = NewReadAccessTestTableTaggedGeo()
	inst.Text = NewReadAccessTestTableTaggedText()
	return
}

func (inst *ReadAccessTestTable) Release() {
	runtime.ReleaseIfNotNil(inst.EntityId)
	runtime.ReleaseIfNotNil(inst.EntityTimestamp)
	runtime.ReleaseIfNotNil(inst.Geo)
	runtime.ReleaseIfNotNil(inst.Text)
}

func (inst *ReadAccessTestTable) LoadFromRecord(rec arrow.Record) (err error) {
	if inst.EntityId != nil {
		err = inst.EntityId.LoadFromRecord(rec)
		if err != nil {
			err = eb.Build().Str("tableName", "test-table").Str("fieldName", "EntityId").Errorf("unable to load from record: %w", err)
			return
		}
	}
	if inst.EntityTimestamp != nil {
		err = inst.EntityTimestamp.LoadFromRecord(rec)
		if err != nil {
			err = eb.Build().Str("tableName", "test-table").Str("fieldName", "EntityTimestamp").Errorf("unable to load from record: %w", err)
			return
		}
	}
	if inst.Geo != nil {
		err = inst.Geo.LoadFromRecord(rec)
		if err != nil {
			err = eb.Build().Str("tableName", "test-table").Str("fieldName", "Geo").Errorf("unable to load from record: %w", err)
			return
		}
	}
	if inst.Text != nil {
		err = inst.Text.LoadFromRecord(rec)
		if err != nil {
			err = eb.Build().Str("tableName", "test-table").Str("fieldName", "Text").Errorf("unable to load from record: %w", err)
			return
		}
	}
	return
}

func (inst *ReadAccessTestTable) SetColumnIndices(indices []uint32) (rest []uint32) {
	rest = indices
	if inst.EntityId != nil {
		rest = inst.EntityId.SetColumnIndices(rest)
	}
	if inst.EntityTimestamp != nil {
		rest = inst.EntityTimestamp.SetColumnIndices(rest)
	}
	if inst.Geo != nil {
		rest = inst.Geo.SetColumnIndices(rest)
	}
	if inst.Text != nil {
		rest = inst.Text.SetColumnIndices(rest)
	}
	return
}

func (inst *ReadAccessTestTable) GetColumnIndices() (columnIndices []uint32) {
	if inst.EntityId != nil {
		columnIndices = slices.Concat(columnIndices, inst.EntityId.GetColumnIndices())
	}
	if inst.EntityTimestamp != nil {
		columnIndices = slices.Concat(columnIndices, inst.EntityTimestamp.GetColumnIndices())
	}
	if inst.Geo != nil {
		columnIndices = slices.Concat(columnIndices, inst.Geo.GetColumnIndices())
	}
	if inst.Text != nil {
		columnIndices = slices.Concat(columnIndices, inst.Text.GetColumnIndices())
	}
	return
}

func (inst *ReadAccessTestTable) GetColumnIndexFieldNames() (fieldNames []string) {
	if inst.EntityId != nil {
		fieldNames = slices.Concat(fieldNames, inst.EntityId.GetColumnIndexFieldNames())
	}
	if inst.EntityTimestamp != nil {
		fieldNames = slices.Concat(fieldNames, inst.EntityTimestamp.GetColumnIndexFieldNames())
	}
	if inst.Geo != nil {
		fieldNames = slices.Concat(fieldNames, inst.Geo.GetColumnIndexFieldNames())
	}
	if inst.Text != nil {
		fieldNames = slices.Concat(fieldNames, inst.Text.GetColumnIndexFieldNames())
	}
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTable)(nil)

func (inst *ReadAccessTestTable) GetNumberOfEntities() (nEntities int) {
	if inst.EntityId != nil {
		nEntities = inst.EntityId.Len()
	}
	return
}
