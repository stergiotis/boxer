package registry

import (
	"fmt"
	"iter"

	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/naturalkey"
)

type RegisteredItemLineageI interface {
	GetModuleInfo() string
	GetOrigin() string
}
type RegisteredItemRestrictionsI interface {
	GetNumberOfRestrictions() (n int)
	IterateRestrictionIndices() iter.Seq[int]
	GetRestrictionCardinality(idx int) CardinalitySpecE
	GetRestrictionSectionName(idx int) naming.StylableName
	GetRestrictionSectionMembership(idx int) common.MembershipSpecE
}
type RegisteredItemIdentifierI interface {
	GetId() identifier.TaggedId
	GetTagValue() identifier.TagValue
	GetNaturalKey() naming.StylableName
}
type RegisteredItemI interface {
	RegisteredItemLineageI
	RegisteredItemRestrictionsI
	RegisteredItemIdentifierI
}
type RegisteredItemDmlUseI[R1 any, R2 any] interface {
	MustAddParents(parents ...RegisteredNaturalKey) R1
	MustAddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) R1
	AddParents(parents ...RegisteredNaturalKey) (R1, error)
	AddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (R1, error)

	MustAddRestriction(sectionName naming.StylableName, membershipSpec common.MembershipSpecE, card CardinalitySpecE) RegisteredNaturalKey
	SetDeprecated() R1
	ClearDeprecated() R1

	End() R2
}

type CardinalitySpecE uint8

const (
	CardinalityZeroToOne  CardinalitySpecE = 0
	CardinalityExactlyOne CardinalitySpecE = 1
	CardinalityOneOrMore  CardinalitySpecE = 2
	CardinalityArbitrary  CardinalitySpecE = 3
)

type RegisteredNaturalKey struct {
	id              identifier.TaggedId
	origin          string
	moduleInfo      string
	naturalKey      naming.StylableName
	parents         *containers.BinarySearchGrowingKV[identifier.TaggedId, RegisteredNaturalKey]
	parentsVirtual  *containers.BinarySearchGrowingKV[identifier.TaggedId, RegisteredNaturalKeyVirtual]
	children        *containers.BinarySearchGrowingKV[identifier.TaggedId, RegisteredNaturalKey]
	childrenVirtual *containers.BinarySearchGrowingKV[identifier.TaggedId, RegisteredNaturalKeyVirtual]

	allowedColumnsSectionNames      []naming.StylableName
	allowedColumnsSectionMembership []common.MembershipSpecE
	allowedCardinality              []CardinalitySpecE
	flags                           RegisteredValueFlagsE

	register func(key RegisteredNaturalKey) RegisteredNaturalKey
}
type RegisteredNaturalKeyVirtual struct {
	w RegisteredNaturalKey
}

var _ RegisteredItemDmlUseI[RegisteredNaturalKeyDml, RegisteredNaturalKey] = RegisteredNaturalKeyDml{}

type RegisteredNaturalKeyFinal struct {
	w RegisteredNaturalKey
}

var _ RegisteredItemDmlUseI[RegisteredNaturalKeyFinalDml, RegisteredNaturalKeyFinal] = RegisteredNaturalKeyFinalDml{}

type RegisteredNaturalKeyDml struct {
	w RegisteredNaturalKey
}

var _ RegisteredItemDmlUseI[RegisteredNaturalKeyDml, RegisteredNaturalKey] = RegisteredNaturalKeyDml{}

type RegisteredNaturalKeyVirtualDml struct {
	w RegisteredNaturalKey
}

var _ RegisteredItemDmlUseI[RegisteredNaturalKeyVirtualDml, RegisteredNaturalKeyVirtual] = RegisteredNaturalKeyVirtualDml{}

type RegisteredNaturalKeyFinalDml struct {
	w RegisteredNaturalKey
}

var _ RegisteredItemDmlUseI[RegisteredNaturalKeyFinalDml, RegisteredNaturalKeyFinal] = RegisteredNaturalKeyFinalDml{}

var _ RegisteredItemI = RegisteredNaturalKey{}

type RegisteredTagValue struct {
	tv         identifier.TagValue
	origin     string
	moduleInfo string
	naturalKey naming.StylableName
	flags      RegisteredValueFlagsE
	register   func(r RegisteredTagValue) RegisteredTagValue
}

type HumanReadableNaturalKeyRegistry[C contract.ContractI] struct {
	tv             identifier.TagValue
	tag            identifier.IdTag
	untaggedOffset identifier.UntaggedId
	lookup         *containers.BinarySearchGrowingKV[naming.StylableName, RegisteredNaturalKey]
	namingStyle    naming.NamingStyleE
	contr          C
	memEnc         *naturalkey.Encoder
	format         naturalkey.SerializationFormatE
}
type RegisteredValueFlagsE uint8

var _ fmt.Stringer = RegisteredValueFlagsE(0)

type MembershipValueRegistry[C contract.ContractI] struct {
	offset      identifier.TagValue
	lookupTg    *containers.BinarySearchGrowingKV[identifier.IdTag, RegisteredTagValue]
	lookupNk    *containers.BinarySearchGrowingKV[naming.StylableName, RegisteredTagValue]
	namingStyle naming.NamingStyleE
	contr       C
	memEnc      *naturalkey.Encoder
	format      naturalkey.SerializationFormatE
}
