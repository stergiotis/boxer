// Code generated; Leeway readaccess (github.com/stergiotis/boxer/public/semistructured/leeway/readaccess.test) DO NOT EDIT.

package example

import (
	///////////////////////////////////////////////////////////////////
	// code generator
	// readaccess.(*GeneratorDriver).GenerateGoClasses
	// ./public/semistructured/leeway/readaccess/lw_ra_generator_hl.go:67

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
	"iter"
	"slices"
	///////////////////////////////////////////////////////////////////
	// code generator
	// readaccess.(*GeneratorDriver).GenerateGoClasses
	// ./public/semistructured/leeway/readaccess/lw_ra_generator_hl.go:84
)

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeMembershipPacks
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:207

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
	inst.AccelLowCardRef = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.MembershipLowCardRefIdx, runtime.AttributeIdx, int, int64](runtime.AccelEstimatedInitialLength)
	inst.AccelMixedLowCardVerbatim = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.MembershipMixedLowCardVerbatimIdx, runtime.AttributeIdx, int, int64](runtime.AccelEstimatedInitialLength)
	inst.AccelMixedVerbatimHighCardParameters = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.MembershipMixedVerbatimHighCardParametersIdx, runtime.AttributeIdx, int, int64](runtime.AccelEstimatedInitialLength)
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
	inst.AccelLowCardRef = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.MembershipLowCardRefIdx, runtime.AttributeIdx, int, int64](runtime.AccelEstimatedInitialLength)
	inst.AccelMixedLowCardVerbatim = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.MembershipMixedLowCardVerbatimIdx, runtime.AttributeIdx, int, int64](runtime.AccelEstimatedInitialLength)
	inst.AccelMixedVerbatimHighCardParameters = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.MembershipMixedVerbatimHighCardParametersIdx, runtime.AttributeIdx, int, int64](runtime.AccelEstimatedInitialLength)
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
	inst.ValueLowCardRef = nil
	inst.ValueLowCardRefElements = nil
	inst.ValueMixedLowCardVerbatim = nil
	inst.ValueMixedLowCardVerbatimElements = nil
	inst.ValueMixedVerbatimHighCardParameters = nil
	inst.ValueMixedVerbatimHighCardParametersElements = nil
}

func (inst *MembershipPackTestTableShared1) LoadFromRecord(rec arrow.Record) (err error) {
	err = runtime.LoadNonScalarValueFieldFromRecord(inst.ColumnIndexLowCardRef, arrow.UINT64, rec, &inst.ValueLowCardRef, &inst.ValueLowCardRefElements, array.NewUint64Data)
	if err != nil {
		return
	}
	err = runtime.LoadAccelFieldFromRecord(inst.ColumnIndexLowCardRefAccel, rec, inst.AccelLowCardRef)
	if err != nil {
		return
	}
	err = runtime.LoadNonScalarValueFieldFromRecord(inst.ColumnIndexMixedLowCardVerbatim, arrow.BINARY, rec, &inst.ValueMixedLowCardVerbatim, &inst.ValueMixedLowCardVerbatimElements, array.NewBinaryData)
	if err != nil {
		return
	}
	err = runtime.LoadAccelFieldFromRecord(inst.ColumnIndexMixedLowCardVerbatimAccel, rec, inst.AccelMixedLowCardVerbatim)
	if err != nil {
		return
	}
	err = runtime.LoadNonScalarValueFieldFromRecord(inst.ColumnIndexMixedVerbatimHighCardParameters, arrow.BINARY, rec, &inst.ValueMixedVerbatimHighCardParameters, &inst.ValueMixedVerbatimHighCardParametersElements, array.NewBinaryData)
	if err != nil {
		return
	}
	err = runtime.LoadAccelFieldFromRecord(inst.ColumnIndexMixedLowCardVerbatimAccel, rec, inst.AccelMixedVerbatimHighCardParameters)
	if err != nil {
		return
	}
	return
}

func (inst *MembershipPackTestTableShared1) Len() (nEntities int) {
	if inst.ValueLowCardRef != nil {
		nEntities = inst.ValueLowCardRef.Len()
	}
	return
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionAttributeClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:609

type ReadAccessTestTablePlainEntityIdAttributes struct {
	ValueId       *array.Uint64
	ColumnIndexId uint32
}

type ReadAccessTestTablePlainEntityTimestampAttributes struct {
	ValueTs           *array.Date32
	ColumnIndexTs     uint32
	ValueProc         *array.List
	ColumnIndexProc   uint32
	ValueProcElements *array.Date32
}

type ReadAccessTestTableTaggedGeoAttributes struct {
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

type ReadAccessTestTableTaggedTextAttributes struct {
	ValueText                  *array.List
	ColumnIndexText            uint32
	ValueTextElements          *array.String
	ValueWords                 *array.List
	ColumnIndexWords           uint32
	ValueWordsElements         *array.String
	ValueBagOfWords            *array.List
	ColumnIndexBagOfWords      uint32
	ValueBagOfWordsElements    *array.String
	AccelHomogenousArray       *runtime.RandomAccessTwoLevelLookupAccel[runtime.HomogenousArrayIdx, runtime.AttributeIdx, int, int64]
	ColumnIndexHomogenousArray uint32
	AccelSet                   *runtime.RandomAccessTwoLevelLookupAccel[runtime.SetIdx, runtime.AttributeIdx, int, int64]
	ColumnIndexSet             uint32
}

func NewReadAccessTestTablePlainEntityIdAttributes() (inst *ReadAccessTestTablePlainEntityIdAttributes) {
	inst = &ReadAccessTestTablePlainEntityIdAttributes{}
	inst.ColumnIndexId = 0
	return
}

func (inst *ReadAccessTestTablePlainEntityIdAttributes) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
		inst.ColumnIndexId,
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityIdAttributes) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
		"ReadAccessTestTablePlainEntityIdAttributes.ColumnIndexId",
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityIdAttributes) SetColumnIndices(indices []uint32) (rest []uint32) {
	inst.ColumnIndexId = indices[0]

	rest = indices[1:]
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTablePlainEntityIdAttributes)(nil)

func NewReadAccessTestTablePlainEntityTimestampAttributes() (inst *ReadAccessTestTablePlainEntityTimestampAttributes) {
	inst = &ReadAccessTestTablePlainEntityTimestampAttributes{}
	inst.ColumnIndexTs = 1
	inst.ColumnIndexProc = 2
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
		inst.ColumnIndexTs,
		inst.ColumnIndexProc,
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
		"ReadAccessTestTablePlainEntityTimestampAttributes.ColumnIndexTs",
		"ReadAccessTestTablePlainEntityTimestampAttributes.ColumnIndexProc",
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) SetColumnIndices(indices []uint32) (rest []uint32) {
	inst.ColumnIndexTs = indices[0]
	inst.ColumnIndexProc = indices[1]

	rest = indices[2:]
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTablePlainEntityTimestampAttributes)(nil)

func NewReadAccessTestTableTaggedGeoAttributes() (inst *ReadAccessTestTableTaggedGeoAttributes) {
	inst = &ReadAccessTestTableTaggedGeoAttributes{}
	inst.ColumnIndexLat = 3
	inst.ColumnIndexLng = 4
	inst.ColumnIndexH3Res1 = 5
	inst.ColumnIndexH3Res2 = 6
	return
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
		inst.ColumnIndexLat,
		inst.ColumnIndexLng,
		inst.ColumnIndexH3Res1,
		inst.ColumnIndexH3Res2,
	}
	return
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
		"ReadAccessTestTableTaggedGeoAttributes.ColumnIndexLat",
		"ReadAccessTestTableTaggedGeoAttributes.ColumnIndexLng",
		"ReadAccessTestTableTaggedGeoAttributes.ColumnIndexH3Res1",
		"ReadAccessTestTableTaggedGeoAttributes.ColumnIndexH3Res2",
	}
	return
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) SetColumnIndices(indices []uint32) (rest []uint32) {
	inst.ColumnIndexLat = indices[0]
	inst.ColumnIndexLng = indices[1]
	inst.ColumnIndexH3Res1 = indices[2]
	inst.ColumnIndexH3Res2 = indices[3]

	rest = indices[4:]
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTableTaggedGeoAttributes)(nil)

func NewReadAccessTestTableTaggedTextAttributes() (inst *ReadAccessTestTableTaggedTextAttributes) {
	inst = &ReadAccessTestTableTaggedTextAttributes{}
	inst.ColumnIndexText = 12
	inst.ColumnIndexWords = 13
	inst.ColumnIndexBagOfWords = 14
	inst.ColumnIndexHomogenousArray = 18
	inst.ColumnIndexSet = 19
	inst.AccelHomogenousArray = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.HomogenousArrayIdx, runtime.AttributeIdx, int, int64](runtime.AccelEstimatedInitialLength)
	inst.AccelSet = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.SetIdx, runtime.AttributeIdx, int, int64](runtime.AccelEstimatedInitialLength)
	return
}

func (inst *ReadAccessTestTableTaggedTextAttributes) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
		inst.ColumnIndexText,
		inst.ColumnIndexWords,
		inst.ColumnIndexBagOfWords,
		inst.ColumnIndexHomogenousArray,
		inst.ColumnIndexSet,
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextAttributes) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
		"ReadAccessTestTableTaggedTextAttributes.ColumnIndexText",
		"ReadAccessTestTableTaggedTextAttributes.ColumnIndexWords",
		"ReadAccessTestTableTaggedTextAttributes.ColumnIndexBagOfWords",
		"ReadAccessTestTableTaggedTextAttributes.ColumnIndexHomogenousArray",
		"ReadAccessTestTableTaggedTextAttributes.ColumnIndexSet",
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextAttributes) SetColumnIndices(indices []uint32) (rest []uint32) {
	inst.ColumnIndexText = indices[0]
	inst.ColumnIndexWords = indices[1]
	inst.ColumnIndexBagOfWords = indices[2]
	inst.ColumnIndexHomogenousArray = indices[3]
	inst.ColumnIndexSet = indices[4]

	rest = indices[5:]
	return
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTableTaggedTextAttributes)(nil)

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionAttributeClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:833

func (inst *ReadAccessTestTablePlainEntityIdAttributes) Reset() {
	inst.ValueId = nil
}

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) Reset() {
	inst.ValueTs = nil
	inst.ValueProc = nil
	inst.ValueProcElements = nil
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) Reset() {
	inst.ValueLat = nil
	inst.ValueLatElements = nil
	inst.ValueLng = nil
	inst.ValueLngElements = nil
	inst.ValueH3Res1 = nil
	inst.ValueH3Res1Elements = nil
	inst.ValueH3Res2 = nil
	inst.ValueH3Res2Elements = nil
}

func (inst *ReadAccessTestTableTaggedTextAttributes) Reset() {
	inst.ValueText = nil
	inst.ValueTextElements = nil
	inst.ValueWords = nil
	inst.ValueWordsElements = nil
	inst.ValueBagOfWords = nil
	inst.ValueBagOfWordsElements = nil
	if inst.AccelHomogenousArray != nil {
		inst.AccelHomogenousArray.Reset()
	}
	if inst.AccelSet != nil {
		inst.AccelSet.Reset()
	}
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionAttributeClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:912

var _ runtime.ReleasableI = (*ReadAccessTestTablePlainEntityIdAttributes)(nil)

func (inst *ReadAccessTestTablePlainEntityIdAttributes) Release() {
	runtime.ReleaseIfNotNil(inst.ValueId)
}

var _ runtime.ReleasableI = (*ReadAccessTestTablePlainEntityTimestampAttributes)(nil)

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) Release() {
	runtime.ReleaseIfNotNil(inst.ValueTs)
	runtime.ReleaseIfNotNil(inst.ValueProc)
	runtime.ReleaseIfNotNil(inst.ValueProcElements)
}

var _ runtime.ReleasableI = (*ReadAccessTestTableTaggedGeoAttributes)(nil)

func (inst *ReadAccessTestTableTaggedGeoAttributes) Release() {
	runtime.ReleaseIfNotNil(inst.ValueLat)
	runtime.ReleaseIfNotNil(inst.ValueLatElements)
	runtime.ReleaseIfNotNil(inst.ValueLng)
	runtime.ReleaseIfNotNil(inst.ValueLngElements)
	runtime.ReleaseIfNotNil(inst.ValueH3Res1)
	runtime.ReleaseIfNotNil(inst.ValueH3Res1Elements)
	runtime.ReleaseIfNotNil(inst.ValueH3Res2)
	runtime.ReleaseIfNotNil(inst.ValueH3Res2Elements)
}

var _ runtime.ReleasableI = (*ReadAccessTestTableTaggedTextAttributes)(nil)

func (inst *ReadAccessTestTableTaggedTextAttributes) Release() {
	runtime.ReleaseIfNotNil(inst.ValueText)
	runtime.ReleaseIfNotNil(inst.ValueTextElements)
	runtime.ReleaseIfNotNil(inst.ValueWords)
	runtime.ReleaseIfNotNil(inst.ValueWordsElements)
	runtime.ReleaseIfNotNil(inst.ValueBagOfWords)
	runtime.ReleaseIfNotNil(inst.ValueBagOfWordsElements)
	runtime.ReleaseIfNotNil(inst.AccelHomogenousArray)
	runtime.ReleaseIfNotNil(inst.AccelSet)
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionAttributeClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:994

func (inst *ReadAccessTestTablePlainEntityIdAttributes) Len() (nEntities int) {
	if inst.ValueId != nil {
		nEntities = inst.ValueId.Len()
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) Len() (nEntities int) {
	if inst.ValueTs != nil {
		nEntities = inst.ValueTs.Len()
	}
	return
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) Len() (nEntities int) {
	if inst.ValueLat != nil {
		nEntities = inst.ValueLat.Len()
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextAttributes) Len() (nEntities int) {
	if inst.ValueText != nil {
		nEntities = inst.ValueText.Len()
	}
	return
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionAttributeClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:1048

func (inst *ReadAccessTestTablePlainEntityIdAttributes) LoadFromRecord(rec arrow.Record) (err error) {
	err = runtime.LoadScalarValueFieldFromRecord(inst.ColumnIndexId, arrow.UINT64, rec, &inst.ValueId, array.NewUint64Data)
	if err != nil {
		return
	}
	return
}

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) LoadFromRecord(rec arrow.Record) (err error) {
	err = runtime.LoadScalarValueFieldFromRecord(inst.ColumnIndexTs, arrow.DATE32, rec, &inst.ValueTs, array.NewDate32Data)
	if err != nil {
		return
	}
	err = runtime.LoadNonScalarValueFieldFromRecord(inst.ColumnIndexProc, arrow.DATE32, rec, &inst.ValueProc, &inst.ValueProcElements, array.NewDate32Data)
	if err != nil {
		return
	}
	return
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) LoadFromRecord(rec arrow.Record) (err error) {
	err = runtime.LoadNonScalarValueFieldFromRecord(inst.ColumnIndexLat, arrow.FLOAT32, rec, &inst.ValueLat, &inst.ValueLatElements, array.NewFloat32Data)
	if err != nil {
		return
	}
	err = runtime.LoadNonScalarValueFieldFromRecord(inst.ColumnIndexLng, arrow.FLOAT32, rec, &inst.ValueLng, &inst.ValueLngElements, array.NewFloat32Data)
	if err != nil {
		return
	}
	err = runtime.LoadNonScalarValueFieldFromRecord(inst.ColumnIndexH3Res1, arrow.UINT64, rec, &inst.ValueH3Res1, &inst.ValueH3Res1Elements, array.NewUint64Data)
	if err != nil {
		return
	}
	err = runtime.LoadNonScalarValueFieldFromRecord(inst.ColumnIndexH3Res2, arrow.UINT64, rec, &inst.ValueH3Res2, &inst.ValueH3Res2Elements, array.NewUint64Data)
	if err != nil {
		return
	}
	return
}

func (inst *ReadAccessTestTableTaggedTextAttributes) LoadFromRecord(rec arrow.Record) (err error) {
	err = runtime.LoadNonScalarValueFieldFromRecord(inst.ColumnIndexText, arrow.STRING, rec, &inst.ValueText, &inst.ValueTextElements, array.NewStringData)
	if err != nil {
		return
	}
	err = runtime.LoadNonScalarValueFieldFromRecord(inst.ColumnIndexWords, arrow.STRING, rec, &inst.ValueWords, &inst.ValueWordsElements, array.NewStringData)
	if err != nil {
		return
	}
	err = runtime.LoadNonScalarValueFieldFromRecord(inst.ColumnIndexBagOfWords, arrow.STRING, rec, &inst.ValueBagOfWords, &inst.ValueBagOfWordsElements, array.NewStringData)
	if err != nil {
		return
	}
	err = runtime.LoadAccelFieldFromRecord(inst.ColumnIndexHomogenousArray, rec, inst.AccelHomogenousArray)
	if err != nil {
		return
	}
	err = runtime.LoadAccelFieldFromRecord(inst.ColumnIndexSet, rec, inst.AccelSet)
	if err != nil {
		return
	}
	return
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) GetAttrValueLat(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) (scalarAttrValue float32) {
	b, e := inst.ValueLat.ValueOffsets(int(entityIdx))
	if int64(attrIdx) > (e - b) {
		log.Panic().Str("attribute", "Lat").Int("beginIncl", int(b)).Int("endExcl", int(e)).Int("attrIdx", int(attrIdx)).Msg("attribute index is out of range")
	}
	scalarAttrValue = inst.ValueLatElements.Value(int(b) + int(attrIdx))
	return
}
func (inst *ReadAccessTestTableTaggedGeoAttributes) GetAttrValueLng(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) (scalarAttrValue float32) {
	b, e := inst.ValueLng.ValueOffsets(int(entityIdx))
	if int64(attrIdx) > (e - b) {
		log.Panic().Str("attribute", "Lng").Int("beginIncl", int(b)).Int("endExcl", int(e)).Int("attrIdx", int(attrIdx)).Msg("attribute index is out of range")
	}
	scalarAttrValue = inst.ValueLngElements.Value(int(b) + int(attrIdx))
	return
}
func (inst *ReadAccessTestTableTaggedGeoAttributes) GetAttrValueH3Res1(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) (scalarAttrValue uint64) {
	b, e := inst.ValueH3Res1.ValueOffsets(int(entityIdx))
	if int64(attrIdx) > (e - b) {
		log.Panic().Str("attribute", "H3Res1").Int("beginIncl", int(b)).Int("endExcl", int(e)).Int("attrIdx", int(attrIdx)).Msg("attribute index is out of range")
	}
	scalarAttrValue = inst.ValueH3Res1Elements.Value(int(b) + int(attrIdx))
	return
}
func (inst *ReadAccessTestTableTaggedGeoAttributes) GetAttrValueH3Res2(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) (scalarAttrValue uint64) {
	b, e := inst.ValueH3Res2.ValueOffsets(int(entityIdx))
	if int64(attrIdx) > (e - b) {
		log.Panic().Str("attribute", "H3Res2").Int("beginIncl", int(b)).Int("endExcl", int(e)).Int("attrIdx", int(attrIdx)).Msg("attribute index is out of range")
	}
	scalarAttrValue = inst.ValueH3Res2Elements.Value(int(b) + int(attrIdx))
	return
}
func (inst *ReadAccessTestTableTaggedTextAttributes) GetAttrValueText(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) (scalarAttrValue string) {
	b, e := inst.ValueText.ValueOffsets(int(entityIdx))
	if int64(attrIdx) > (e - b) {
		log.Panic().Str("attribute", "Text").Int("beginIncl", int(b)).Int("endExcl", int(e)).Int("attrIdx", int(attrIdx)).Msg("attribute index is out of range")
	}
	scalarAttrValue = inst.ValueTextElements.Value(int(b) + int(attrIdx))
	return
}
func (inst *ReadAccessTestTableTaggedTextAttributes) GetAttrValueWords(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[string] {
	accel := inst.AccelHomogenousArray
	accel.SetCurrentEntityIdx(int(entityIdx))
	r := accel.LookupForwardRange(attrIdx)
	if !r.IsEmpty() {
		return func(yield func(string) bool) {
			vs := inst.ValueWordsElements
			for i := r.BeginIncl; i < r.EndExcl; i++ {
				if !yield(vs.Value(int(i))) {
					break
				}
			}
		}
	}
	return nil
}
func (inst *ReadAccessTestTableTaggedTextAttributes) GetAttrValueBagOfWords(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[string] {
	accel := inst.AccelSet
	accel.SetCurrentEntityIdx(int(entityIdx))
	r := accel.LookupForwardRange(attrIdx)
	if !r.IsEmpty() {
		return func(yield func(string) bool) {
			vs := inst.ValueBagOfWordsElements
			for i := r.BeginIncl; i < r.EndExcl; i++ {
				if !yield(vs.Value(int(i))) {
					break
				}
			}
		}
	}
	return nil
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:1298

type ReadAccessTestTableTaggedGeo struct {
	Attributes  *ReadAccessTestTableTaggedGeoAttributes
	Memberships *MembershipPackTestTableShared1
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTableTaggedGeo)(nil)

type ReadAccessTestTableTaggedText struct {
	Attributes  *ReadAccessTestTableTaggedTextAttributes
	Memberships *MembershipPackTestTableShared1
}

var _ runtime.ColumnIndexHandlingI = (*ReadAccessTestTableTaggedText)(nil)

func NewReadAccessTestTableTaggedGeo() (inst *ReadAccessTestTableTaggedGeo) {
	inst = &ReadAccessTestTableTaggedGeo{}
	inst.Attributes = NewReadAccessTestTableTaggedGeoAttributes()
	inst.Memberships = NewMembershipPackTestTableShared1Geo()
	return
}

func NewReadAccessTestTableTaggedText() (inst *ReadAccessTestTableTaggedText) {
	inst = &ReadAccessTestTableTaggedText{}
	inst.Attributes = NewReadAccessTestTableTaggedTextAttributes()
	inst.Memberships = NewMembershipPackTestTableShared1Text()
	return
}

func (inst *ReadAccessTestTableTaggedGeo) SetColumnIndices(indices []uint32) (restIndices []uint32) {
	restIndices = indices
	restIndices = inst.Attributes.SetColumnIndices(restIndices)
	restIndices = inst.Memberships.SetColumnIndices(restIndices)
	return
}

func (inst *ReadAccessTestTableTaggedText) SetColumnIndices(indices []uint32) (restIndices []uint32) {
	restIndices = indices
	restIndices = inst.Attributes.SetColumnIndices(restIndices)
	restIndices = inst.Memberships.SetColumnIndices(restIndices)
	return
}

func (inst *ReadAccessTestTableTaggedGeo) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = slices.Concat(columnIndices, inst.Attributes.GetColumnIndices())
	columnIndices = slices.Concat(columnIndices, inst.Memberships.GetColumnIndices())
	return
}

func (inst *ReadAccessTestTableTaggedText) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = slices.Concat(columnIndices, inst.Attributes.GetColumnIndices())
	columnIndices = slices.Concat(columnIndices, inst.Memberships.GetColumnIndices())
	return
}

func (inst *ReadAccessTestTableTaggedGeo) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = slices.Concat(fieldNames, inst.Attributes.GetColumnIndexFieldNames())
	fieldNames = slices.Concat(fieldNames, inst.Memberships.GetColumnIndexFieldNames())
	return
}

func (inst *ReadAccessTestTableTaggedText) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = slices.Concat(fieldNames, inst.Attributes.GetColumnIndexFieldNames())
	fieldNames = slices.Concat(fieldNames, inst.Memberships.GetColumnIndexFieldNames())
	return
}

func (inst *ReadAccessTestTableTaggedGeo) Release() {
	runtime.ReleaseIfNotNil(inst.Attributes)
	runtime.ReleaseIfNotNil(inst.Memberships)
}

func (inst *ReadAccessTestTableTaggedText) Release() {
	runtime.ReleaseIfNotNil(inst.Attributes)
	runtime.ReleaseIfNotNil(inst.Memberships)
}

func (inst *ReadAccessTestTableTaggedGeo) LoadFromRecord(rec arrow.Record) (err error) {
	err = inst.Attributes.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Errorf("unable to load from record: %w", err)
		return
	}
	err = inst.Memberships.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Errorf("unable to load from record: %w", err)
		return
	}
	return
}

func (inst *ReadAccessTestTableTaggedText) LoadFromRecord(rec arrow.Record) (err error) {
	err = inst.Attributes.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Errorf("unable to load from record: %w", err)
		return
	}
	err = inst.Memberships.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Errorf("unable to load from record: %w", err)
		return
	}
	return
}

func (inst *ReadAccessTestTableTaggedGeo) Len() (nEntities int) {
	nEntities = inst.Memberships.Len()
	return
}

func (inst *ReadAccessTestTableTaggedText) Len() (nEntities int) {
	nEntities = inst.Memberships.Len()
	return
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeEntityClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:1519

type ReadAccessTestTable struct {
	EntityId        *ReadAccessTestTablePlainEntityIdAttributes
	EntityTimestamp *ReadAccessTestTablePlainEntityTimestampAttributes
	Geo             *ReadAccessTestTableTaggedGeo
	Text            *ReadAccessTestTableTaggedText
}

func NewReadAccessTestTable() (inst *ReadAccessTestTable) {
	inst = &ReadAccessTestTable{}
	inst.EntityId = NewReadAccessTestTablePlainEntityIdAttributes()
	inst.EntityTimestamp = NewReadAccessTestTablePlainEntityTimestampAttributes()
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
