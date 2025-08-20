package common

import (
	"fmt"
	"iter"
	"math"
	"math/bits"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const InvalidEnumValueString = "<invalid>"

const (
	ColumnRoleUnspecific                      ColumnRoleE = ""
	ColumnRoleHighCardRef                     ColumnRoleE = "hr"
	ColumnRoleHighCardRefParametrized         ColumnRoleE = "hp"
	ColumnRoleHighCardVerbatim                ColumnRoleE = "hv"
	ColumnRoleLowCardRef                      ColumnRoleE = "lr"
	ColumnRoleLowCardRefParametrized          ColumnRoleE = "lp"
	ColumnRoleLowCardVerbatim                 ColumnRoleE = "lv"
	ColumnRoleMixedLowCardRef                 ColumnRoleE = "lmr"
	ColumnRoleMixedVerbatimHighCardParameters ColumnRoleE = "mvhp"
	ColumnRoleMixedRefHighCardParameters      ColumnRoleE = "mrhp"
	ColumnRoleMixedLowCardVerbatim            ColumnRoleE = "lmv"
	ColumnRoleValue                           ColumnRoleE = "val"
	ColumnRoleLength                          ColumnRoleE = "len"

	ColumnRoleHighCardRefCardinality             ColumnRoleE = ColumnRoleHighCardRef + ColumnRoleE("card")
	ColumnRoleHighCardRefParametrizedCardinality ColumnRoleE = ColumnRoleHighCardRefParametrized + ColumnRoleE("card")
	ColumnRoleHighCardVerbatimCardinality        ColumnRoleE = ColumnRoleHighCardVerbatim + ColumnRoleE("card")
	ColumnRoleLowCardRefCardinality              ColumnRoleE = ColumnRoleLowCardRef + ColumnRoleE("card")
	ColumnRoleLowCardRefParametrizedCardinality  ColumnRoleE = ColumnRoleLowCardRefParametrized + ColumnRoleE("card")
	ColumnRoleLowCardVerbatimCardinality         ColumnRoleE = ColumnRoleLowCardVerbatim + ColumnRoleE("card")
	ColumnRoleMixedLowCardRefCardinality         ColumnRoleE = ColumnRoleMixedLowCardRef + ColumnRoleE("card")
	ColumnRoleMixedLowCardVerbatimCardinality    ColumnRoleE = ColumnRoleMixedLowCardVerbatim + ColumnRoleE("card")

	ColumnRoleCardinality ColumnRoleE = "card"

	ColumnRoleCusumLength      ColumnRoleE = "cusumlen"
	ColumnRoleCusumCardinality ColumnRoleE = "cusumcard"
)

var AllColumnRoles = []ColumnRoleE{
	ColumnRoleUnspecific,
	ColumnRoleHighCardRef,
	ColumnRoleHighCardRefParametrized,
	ColumnRoleHighCardVerbatim,
	ColumnRoleLowCardRef,
	ColumnRoleLowCardRefParametrized,
	ColumnRoleLowCardVerbatim,
	ColumnRoleMixedLowCardRef,
	ColumnRoleMixedRefHighCardParameters,
	ColumnRoleMixedLowCardVerbatim,
	ColumnRoleValue,
	ColumnRoleLength,

	ColumnRoleHighCardRefCardinality,
	ColumnRoleHighCardRefParametrizedCardinality,
	ColumnRoleHighCardVerbatimCardinality,
	ColumnRoleLowCardRefCardinality,
	// NOTE: parametrization is high cardinality, ref is low-cardinality
	ColumnRoleLowCardRefParametrizedCardinality,
	ColumnRoleLowCardVerbatimCardinality,
	ColumnRoleMixedLowCardRefCardinality,
	ColumnRoleMixedLowCardVerbatimCardinality,

	ColumnRoleCardinality,
	ColumnRoleCusumLength,
	ColumnRoleCusumCardinality,
}

func ParseColumnRole(s string) (role ColumnRoleE, err error) {
	role = ColumnRoleE(s)
	i := slices.Index(AllColumnRoles, role)
	if i < 0 {
		err = eb.Build().Str("role", s).Errorf("unknown role")
		role = ColumnRoleUnspecific
		return
	}
	return
}

func (inst ColumnRoleE) String() string {
	switch inst {
	case ColumnRoleUnspecific,
		ColumnRoleHighCardRef,
		ColumnRoleHighCardRefParametrized,
		ColumnRoleHighCardVerbatim,
		ColumnRoleLowCardRef,
		ColumnRoleLowCardRefParametrized,
		ColumnRoleLowCardVerbatim,
		ColumnRoleMixedLowCardRef,
		ColumnRoleMixedVerbatimHighCardParameters,
		ColumnRoleMixedRefHighCardParameters,
		ColumnRoleMixedLowCardVerbatim,
		ColumnRoleValue,
		ColumnRoleLength,
		ColumnRoleHighCardRefCardinality,
		ColumnRoleHighCardRefParametrizedCardinality,
		ColumnRoleHighCardVerbatimCardinality,
		ColumnRoleLowCardRefCardinality,
		ColumnRoleLowCardRefParametrizedCardinality,
		ColumnRoleLowCardVerbatimCardinality,
		ColumnRoleMixedLowCardRefCardinality,
		ColumnRoleMixedLowCardVerbatimCardinality,
		ColumnRoleCardinality,
		ColumnRoleCusumLength,
		ColumnRoleCusumCardinality:
		return string(inst)
	}
	return InvalidEnumValueString
}

const (
	MembershipSpecNone                                   MembershipSpecE = 0b0000_0000
	MembershipSpecHighCardRef                            MembershipSpecE = 0b0000_0001
	MembershipSpecHighCardVerbatim                       MembershipSpecE = 0b0000_0010
	MembershipSpecHighCardRefParametrized                MembershipSpecE = 0b0000_0100
	MembershipSpecLowCardRef                             MembershipSpecE = 0b0001_0000
	MembershipSpecLowCardVerbatim                        MembershipSpecE = 0b0010_0000
	MembershipSpecLowCardRefParametrized                 MembershipSpecE = 0b0100_0000
	MembershipSpecMixedLowCardRefHighCardParameters      MembershipSpecE = 0b0000_1000
	MembershipSpecMixedLowCardVerbatimHighCardParameters MembershipSpecE = 0b1000_0000
)

var AllMembershipSpecs = []MembershipSpecE{
	MembershipSpecNone,
	MembershipSpecHighCardRef,
	MembershipSpecHighCardVerbatim,
	MembershipSpecHighCardRefParametrized,
	MembershipSpecLowCardRef,
	MembershipSpecLowCardVerbatim,
	MembershipSpecLowCardRefParametrized,
	MembershipSpecMixedLowCardRefHighCardParameters,
	MembershipSpecMixedLowCardVerbatimHighCardParameters,
}

func (inst MembershipSpecE) ContainsMixed() (mixed bool) {
	mixed = inst.HasMixedLowCardVerbatimHighCardParameters() || inst.HasMixedLowCardRefHighCardParameters()
	return
}

func (inst MembershipSpecE) String() string {
	if inst == MembershipSpecNone {
		return "none"
	}
	l := inst.Count()
	if l == 1 {
		switch inst {
		case MembershipSpecHighCardRef:
			return "high-card-ref"
		case MembershipSpecHighCardVerbatim:
			return "high-card-verbatim"
		case MembershipSpecHighCardRefParametrized:
			return "high-card-ref-parametrized"
		case MembershipSpecLowCardRef:
			return "low-card-ref"
		case MembershipSpecLowCardVerbatim:
			return "low-card-verbatim"
		case MembershipSpecLowCardRefParametrized:
			return "low-card-ref-parametrized"
		case MembershipSpecMixedLowCardRefHighCardParameters:
			return "low-card-ref-high-card-params"
		case MembershipSpecMixedLowCardVerbatimHighCardParameters:
			return "low-card-verbatim-high-card-params"
		}
	}
	s := strings.Builder{}
	i := 0
	for m := range inst.Iterate() {
		_, _ = s.WriteString(m.String())
		if i > 0 {
			_, _ = s.WriteString(" | ")
		}
		i++
	}
	return s.String()
}

func (inst MembershipSpecE) HasHighCardRefOnly() bool {
	return inst&MembershipSpecHighCardRef != 0
}
func (inst MembershipSpecE) HasLowCardRefOnly() bool {
	return inst&MembershipSpecLowCardRef != 0
}
func (inst MembershipSpecE) HasHighCardVerbatim() bool {
	return inst&MembershipSpecHighCardVerbatim != 0
}
func (inst MembershipSpecE) HasLowCardVerbatim() bool {
	return inst&MembershipSpecLowCardVerbatim != 0
}
func (inst MembershipSpecE) HasHighCardRefParametrized() bool {
	return inst&MembershipSpecHighCardRefParametrized != 0
}
func (inst MembershipSpecE) HasLowCardRefParametrized() bool {
	return inst&MembershipSpecLowCardRefParametrized != 0
}
func (inst MembershipSpecE) HasMixedLowCardRefHighCardParameters() bool {
	return inst&MembershipSpecMixedLowCardRefHighCardParameters != 0
}
func (inst MembershipSpecE) HasMixedLowCardVerbatimHighCardParameters() bool {
	return inst&MembershipSpecMixedLowCardVerbatimHighCardParameters != 0
}
func (inst MembershipSpecE) AddHighCardRefOnly() MembershipSpecE {
	return inst | MembershipSpecHighCardRef
}
func (inst MembershipSpecE) AddHighCardRefParametrized() MembershipSpecE {
	return inst | MembershipSpecHighCardRefParametrized
}
func (inst MembershipSpecE) AddHighCardVerbatim() MembershipSpecE {
	return inst | MembershipSpecHighCardVerbatim
}
func (inst MembershipSpecE) AddLowCardRefOnly() MembershipSpecE {
	return inst | MembershipSpecLowCardRef
}
func (inst MembershipSpecE) AddLowCardRefParametrized() MembershipSpecE {
	return inst | MembershipSpecLowCardRefParametrized
}
func (inst MembershipSpecE) AddLowCardVerbatim() MembershipSpecE {
	return inst | MembershipSpecLowCardVerbatim
}
func (inst MembershipSpecE) AddMixedLowCardRefHighCardParameters() MembershipSpecE {
	return inst | MembershipSpecMixedLowCardRefHighCardParameters
}
func (inst MembershipSpecE) AddMixedLowCardVerbatimHighCardParameters() MembershipSpecE {
	return inst | MembershipSpecMixedLowCardVerbatimHighCardParameters
}
func (inst MembershipSpecE) ClearHighCardRefOnly() MembershipSpecE {
	return inst & ^MembershipSpecHighCardRef
}
func (inst MembershipSpecE) ClearHighCardRefParametrized() MembershipSpecE {
	return inst & ^MembershipSpecHighCardRefParametrized
}
func (inst MembershipSpecE) ClearHighCardVerbatim() MembershipSpecE {
	return inst & ^MembershipSpecHighCardVerbatim
}
func (inst MembershipSpecE) ClearLowCardRefOnly() MembershipSpecE {
	return inst & ^MembershipSpecLowCardRef
}
func (inst MembershipSpecE) ClearLowCardRefParametrized() MembershipSpecE {
	return inst & ^MembershipSpecLowCardRefParametrized
}
func (inst MembershipSpecE) ClearLowCardVerbatim() MembershipSpecE {
	return inst & ^MembershipSpecLowCardVerbatim
}
func (inst MembershipSpecE) ClearMixedLowCardRefHighCardParameters() MembershipSpecE {
	return inst & ^MembershipSpecMixedLowCardRefHighCardParameters
}
func (inst MembershipSpecE) ClearMixedLowCardVerbatimHighCardParameters() MembershipSpecE {
	return inst & ^MembershipSpecMixedLowCardVerbatimHighCardParameters
}
func (inst MembershipSpecE) Count() int {
	return bits.OnesCount8(uint8(inst))
}
func (inst MembershipSpecE) Iterate() iter.Seq[MembershipSpecE] {
	return func(yield func(MembershipSpecE) bool) {
		for _, m := range AllMembershipSpecs {
			if (inst&m) != 0 && m != MembershipSpecNone {
				if !yield(m) {
					return
				}
			}
		}
	}
}

const (
	ImplementationStatusNotImplemented ImplementationStatusE = 0
	ImplementationStatusPartial        ImplementationStatusE = math.MaxUint8 >> 1
	ImplementationStatusFull           ImplementationStatusE = math.MaxUint8
)

var AllImplementationStatus = []ImplementationStatusE{
	ImplementationStatusNotImplemented,
	ImplementationStatusPartial,
	ImplementationStatusFull,
}

func (inst ImplementationStatusE) String() string {
	switch inst {
	case ImplementationStatusNotImplemented:
		return "not-implemented"
	case ImplementationStatusPartial:
		return "partially-implemented"
	case ImplementationStatusFull:
		return "fully-implemented"
	}
	return InvalidEnumValueString
}

const (
	TableRowConfigMultiAttributesPerRow TableRowConfigE = 0
)

var AllTableRowConfigs = []TableRowConfigE{
	TableRowConfigMultiAttributesPerRow,
}

func (inst TableRowConfigE) IsValid() bool {
	switch inst {
	case TableRowConfigMultiAttributesPerRow:
		return true
	}
	return false
}

func (inst TableRowConfigE) String() string {
	switch inst {
	case TableRowConfigMultiAttributesPerRow:
		return "multi-attributes-per-row"
	}
	return InvalidEnumValueString
}

const (
	PlainItemTypeNone            PlainItemTypeE = 0
	PlainItemTypeEntityId        PlainItemTypeE = 1
	PlainItemTypeEntityTimestamp PlainItemTypeE = 2
	PlainItemTypeEntityRouting   PlainItemTypeE = 3
	PlainItemTypeEntityLifecycle PlainItemTypeE = 4
	PlainItemTypeTransaction     PlainItemTypeE = 5
	PlainItemTypeOpaque          PlainItemTypeE = 6
)

var AllPlainItemTypes = []PlainItemTypeE{
	PlainItemTypeNone,
	PlainItemTypeEntityId,
	PlainItemTypeEntityTimestamp,
	PlainItemTypeEntityRouting,
	PlainItemTypeEntityLifecycle,
	PlainItemTypeTransaction,
	PlainItemTypeOpaque,
}

var MaxPlainItemTypeExcl = PlainItemTypeE(len(AllMembershipSpecs))

func (inst PlainItemTypeE) String() string {
	switch inst {
	case PlainItemTypeNone:
		return "none"
	case PlainItemTypeEntityId:
		return "entity-id"
	case PlainItemTypeEntityTimestamp:
		return "entity-timestamp"
	case PlainItemTypeEntityRouting:
		return "entity-routing"
	case PlainItemTypeEntityLifecycle:
		return "entity-lifecycle"
	case PlainItemTypeTransaction:
		return "transaction"
	case PlainItemTypeOpaque:
		return "opaque"
	}
	return InvalidEnumValueString
}

var AllIntermediateColumnScopes = []IntermediateColumnScopeE{
	IntermediateColumnScopeEntity,
	IntermediateColumnScopeTransaction,
	IntermediateColumnScopeOpaque,
	IntermediateColumnScopeTagged,
}

const (
	IntermediateColumnScopeEntity      IntermediateColumnScopeE = "entity"
	IntermediateColumnScopeTransaction IntermediateColumnScopeE = "transaction"
	IntermediateColumnScopeOpaque      IntermediateColumnScopeE = "opaque"
	IntermediateColumnScopeTagged      IntermediateColumnScopeE = "tagged"
)

var _ fmt.Stringer = IntermediateColumnScopeE("")

func (inst IntermediateColumnScopeE) String() string {
	if inst.IsValid() {
		return string(inst)
	}
	return InvalidEnumValueString
}
func (inst IntermediateColumnScopeE) IsValid() bool {
	switch inst {
	case IntermediateColumnScopeEntity,
		IntermediateColumnScopeTransaction,
		IntermediateColumnScopeOpaque,
		IntermediateColumnScopeTagged:
		return true
	}
	return false
}

var AllIntermediateColumnsSubTypes = []IntermediateColumnSubTypeE{
	IntermediateColumnsSubTypeScalar,
	IntermediateColumnsSubTypeHomogenousArray,
	IntermediateColumnsSubTypeHomogenousArraySupport,
	IntermediateColumnsSubTypeSet,
	IntermediateColumnsSubTypeSetSupport,
	IntermediateColumnsSubTypeMembership,
	IntermediateColumnsSubTypeMembershipSupport,
}

const (
	IntermediateColumnsSubTypeScalar                 IntermediateColumnSubTypeE = "scalar"
	IntermediateColumnsSubTypeHomogenousArray        IntermediateColumnSubTypeE = "homogenous-array"
	IntermediateColumnsSubTypeHomogenousArraySupport IntermediateColumnSubTypeE = "homogenous-array-support"
	IntermediateColumnsSubTypeSet                    IntermediateColumnSubTypeE = "set"
	IntermediateColumnsSubTypeSetSupport             IntermediateColumnSubTypeE = "set-support"
	IntermediateColumnsSubTypeMembership             IntermediateColumnSubTypeE = "membership"
	IntermediateColumnsSubTypeMembershipSupport      IntermediateColumnSubTypeE = "membership-support"
)

var _ fmt.Stringer = IntermediateColumnSubTypeE("")

func (inst IntermediateColumnSubTypeE) String() string {
	if inst.IsValid() {
		return string(inst)
	}
	return InvalidEnumValueString
}
func (inst IntermediateColumnSubTypeE) IsValid() bool {
	switch inst {
	case IntermediateColumnsSubTypeScalar,
		IntermediateColumnsSubTypeHomogenousArray,
		IntermediateColumnsSubTypeHomogenousArraySupport,
		IntermediateColumnsSubTypeSet,
		IntermediateColumnsSubTypeSetSupport,
		IntermediateColumnsSubTypeMembership,
		IntermediateColumnsSubTypeMembershipSupport:
		return true
	}
	return false
}
func (inst PlainItemTypeE) GetIntermediateColumnScope() IntermediateColumnScopeE {
	switch inst {
	case PlainItemTypeEntityId, PlainItemTypeEntityTimestamp, PlainItemTypeEntityRouting, PlainItemTypeEntityLifecycle:
		return IntermediateColumnScopeEntity
	case PlainItemTypeTransaction:
		return IntermediateColumnScopeTransaction
	case PlainItemTypeOpaque:
		return IntermediateColumnScopeOpaque
	}
	log.Panic().Uint8("value", uint8(inst)).Msg("unhandled plain item type")
	return IntermediateColumnScopeE("")
}

