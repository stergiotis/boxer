package runtime

import (
	"iter"

	"golang.org/x/exp/constraints"
)

type ReleasableI interface {
	Release()
}

type (
	AttributeIdx                                 int
	HomogenousArrayIdx                           int
	SetIdx                                       int
	MembershipHighCardRefIdx                     int
	MembershipHighCardRefParameterizedIdx        int
	MembershipHighCardVerbatimIdx                int
	MembershipLowCardRefIdx                      int
	MembershipLowCardRefParameterizedIdx         int
	MembershipLowCardVerbatimIdx                 int
	MembershipMixedLowCardRefIdx                 int
	MembershipMixedRefHighCardParametersIdx      int
	MembershipMixedLowCardVerbatimIdx            int
	MembershipMixedVerbatimHighCardParametersIdx int
)

type IndexConstraintI interface {
	constraints.Integer | constraints.Unsigned
}
type RandomAccessLookupAccel[F IndexConstraintI, B IndexConstraintI] struct {
	forwardBeginIncl []F
	forwardEndExcl   []F
	backward         []B
	len              int
}
type ValueOffsetI[I IndexConstraintI, I2 IndexConstraintI] interface {
	ValueOffsets(i I) (beginIncl I2, endExcl I2)
}
type RandomAccessTwoLevelLookupAccel[F IndexConstraintI, B IndexConstraintI, I IndexConstraintI, I2 IndexConstraintI] struct {
	accel  *RandomAccessLookupAccel[F, B]
	row    I
	cards  []uint64
	ranger ValueOffsetI[I, I2]
}
type RowIdx int
type RandomAccessLookupAccelI[F IndexConstraintI, B IndexConstraintI] interface {
	LookupForward(i B) (beginIncl F, endExcl F)
	LookupForwardRange(i B) (r Range[F])
	LookupForwardIndexedRange(i B) (r IndexedRange[F, B])
	LookupBackward(i F) (index B)
	GetCardinality(i B) (card uint64)
	IterateAllFwdIndexedRange() iter.Seq[IndexedRange[F, B]]
	IterateAllFwdRange() iter.Seq[Range[F]]
	LoadCardinalities(cards []uint64)
}

var _ RandomAccessLookupAccelI[int, uint] = (*RandomAccessTwoLevelLookupAccel[int, uint, RowIdx, int64])(nil)
var _ RandomAccessLookupAccelI[int, uint] = (*RandomAccessLookupAccel[int, uint])(nil)

type Range[T IndexConstraintI] struct {
	BeginIncl T
	EndExcl   T
}
type IndexedRange[R IndexConstraintI, I IndexConstraintI] struct {
	BeginIncl R
	EndExcl   R
	Index     I
	Length    int
}
type RangeI[T IndexConstraintI] interface {
	ToRange() (r Range[T])
	IsEmpty() bool
	CalcCardinality() (card uint64)
}

var _ RangeI[int] = IndexedRange[int, uint]{}
var _ RangeI[int] = Range[int]{}

type ColumnIndexHandlingI interface {
	SetColumnIndices(indices []uint32) (restIndices []uint32)
	GetColumnIndices() (columnIndices []uint32)
	GetColumnIndexFieldNames() (columnIndexFieldNames []string)
}
