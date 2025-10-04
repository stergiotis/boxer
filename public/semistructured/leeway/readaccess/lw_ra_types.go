package readaccess

import (
	"iter"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

type GoClassBuilder struct {
	builder *strings.Builder
	tech    *golang.TechnologySpecificCodeGenerator
}
type GeneratorDriver struct {
	builder          *GoClassBuilder
	validator        *common.TableValidator
	namingConvention common.NamingConventionI
	tech             common.TechnologySpecificGeneratorI
}

type InAttributeMembershipHighCardRefI interface {
	GetMembValueHighCardRef(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[uint64]
}
type InAttributeMembershipHighCardRefParametrizedI interface {
	GetMembValueHighCardRefParametrized(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[[]byte]
}
type InAttributeMembershipHighCardVerbatimI interface {
	GetMembValueHighCardVerbatim(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[[]byte]
}

type InAttributeMembershipLowCardRefI interface {
	GetMembValueLowCardRef(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[uint64]
}
type InAttributeMembershipLowCardRefParametrizedI interface {
	GetMembValueLowCardRefParametrized(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[[]byte]
}
type InAttributeMembershipLowCardVerbatimI interface {
	GetMembValueLowCardVerbatim(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[[]byte]
}

type InAttributeMembershipMixedLowCardRefI interface {
	GetMembValueMixedLowCardRef(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[uint64]
}
type InAttributeMembershipMixedLowCardVerbatimI interface {
	GetMembValueMixedLowCardVerbatim(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[[]byte]
}
type InAttributeMembershipMixedVerbatimHighCardParametersI interface {
	GetMembValueMixedVerbatimHighCardParameters(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[[]byte]
}
type InAttributeMembershipMixedRefHighCardParametersI interface {
	GetMembValueMixedRefHighCardParameters(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[[]byte]
}
