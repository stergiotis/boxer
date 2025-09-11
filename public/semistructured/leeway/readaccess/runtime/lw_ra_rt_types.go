package runtime

import (
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"golang.org/x/exp/constraints"
)

type ReleasableI interface {
	Release()
}
type SourceOrientedValueI[T any] interface {
	GetValue(rowIndex int) T
	GetColumnIndex() int
	GetCanonicalType() canonicaltypes.PrimitiveAstNodeI
	GetPhysicalColumnName() string
}
type CompositeValueI[T any] interface {
	GetValue(rowIndex int) T
}

type PhysicalColumnLineage struct {
	PhysicalColumnIndex uint32

	/* Materialized from PhysicalColumnDesc */

	PhysicalColumnName             string
	PhysicalColumnNameComponents   []string
	PhysicalColumnNameExplanations []string
	PhysicalColumnComment          string
	CanonicalType                  canonicaltypes.PrimitiveAstNodeI
	EncodingHints                  encodingaspects.AspectSet
	TableRowConfig                 common.TableRowConfigE
	PlainItemType                  common.PlainItemTypeE
	SectionName                    string
	LeewayColumnName               naming.StylableName
}

type AttributeIdx int
type NonScalarIdx int
type MembershipHighCardRefIdx int
type MembershipHighCardRefParameterizedIdx int
type MembershipHighCardVerbatimIdx int
type MembershipLowCardRefIdx int
type MembershipLowCardRefParameterizedIdx int
type MembershipLowCardVerbatimIdx int
type MembershipMixedLowCardRefIdx int
type MembershipMixedLowCardVerbatimIdx int

type MembershipRandomAccesAccels struct {
	AccelMembHighCardRef             *RandomAccessLookupAccel[MembershipHighCardRefIdx, AttributeIdx]
	AccelMembHighCardRefParametrized *RandomAccessLookupAccel[MembershipHighCardRefParameterizedIdx, AttributeIdx]
	AccelMembHighCardVerbatim        *RandomAccessLookupAccel[MembershipHighCardVerbatimIdx, AttributeIdx]
	AccelMembLowCardRef              *RandomAccessLookupAccel[MembershipLowCardRefIdx, AttributeIdx]
	AccelMembLowCardRefParametrized  *RandomAccessLookupAccel[MembershipLowCardRefParameterizedIdx, AttributeIdx]
	AccelMembLowCardVerbatim         *RandomAccessLookupAccel[MembershipLowCardVerbatimIdx, AttributeIdx]
	AccelMembMixedLowCardRef         *RandomAccessLookupAccel[MembershipMixedLowCardRefIdx, AttributeIdx]
	AccelMembMixedHighCardRef        *RandomAccessLookupAccel[MembershipMixedLowCardVerbatimIdx, AttributeIdx]
}
type CoValuePackI interface {
}
type SourceOrientedScalarValue[T CoValuePackI] struct {
	Values          T
	Lineage         PhysicalColumnLineage
	MembAccels      *MembershipRandomAccesAccels
	NonScalarsAccel *RandomAccessLookupAccel[AttributeIdx, NonScalarIdx]
}
type SourceOrientedNonScalarValue[T CoValuePackI] struct {
	Values          T
	Cardinality     *array.Uint64
	Lineage         PhysicalColumnLineage
	MembAccels      *MembershipRandomAccesAccels
	AccelNonScalars *RandomAccessLookupAccel[AttributeIdx, NonScalarIdx]
}
type IndexConstraintI interface {
	constraints.Integer | constraints.Unsigned
}
type RandomAccessLookupAccel[F IndexConstraintI, B IndexConstraintI] struct {
	forwardBeginIncl []F
	forwardEndExcl   []F
	backward         []B
	len              int
}
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
