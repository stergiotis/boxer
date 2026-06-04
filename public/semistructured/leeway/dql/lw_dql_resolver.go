package dql

import (
	"strconv"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// IdLookup resolves a leeway membership name to the uint64 id carried on the
// wire for the ref channels — the value matched against the hr/lr/lmr columns.
// It mirrors marshallreflect.LookupI, so any registry implementing that
// interface satisfies this one.
type IdLookup interface {
	LookupMembership(name string) (id uint64, err error)
}

// ColumnMatch is one physical predicate that identifies a membership: the
// column of role Role must contain Literal — has(col, Literal) to test
// presence, indexOf(col, Literal) to locate the attribute. Literal is a
// ready-to-embed ClickHouse literal (escaped string or decimal id).
type ColumnMatch struct {
	Role    common.ColumnRoleE
	Literal string
}

// ResolvedMembership is a logical membership resolved for SQL emission: the
// per-column matches that together identify it. Simple channels yield one
// match (the identity); the mixed channels would add a second (the high-card
// parameters) once mappingplan surfaces them (ADR-0008 Cut 2).
type ResolvedMembership struct {
	Spec    common.MembershipSpecE
	Matches []ColumnMatch
}

// Identity returns the identity match used to locate the attribute.
func (r ResolvedMembership) Identity() ColumnMatch { return r.Matches[0] }

// MembershipResolver turns a logical (name, channel) into its SQL-embeddable
// identity, with every ref id resolved to a literal at generation time — the
// emitted SQL carries constants and never calls dictGet.
type MembershipResolver interface {
	Resolve(name string, spec common.MembershipSpecE) (ResolvedMembership, error)
}

// LookupResolver is the default MembershipResolver: verbatim channels echo the
// membership name as an escaped string literal; ref channels resolve the name
// to its uint64 id via IdLookup and embed it as a decimal. Resolution is
// total — an unresolvable ref name is a generation error, not a runtime miss.
type LookupResolver struct {
	lookup IdLookup
}

func NewLookupResolver(lookup IdLookup) *LookupResolver {
	return &LookupResolver{lookup: lookup}
}

var _ MembershipResolver = (*LookupResolver)(nil)

func (r *LookupResolver) Resolve(name string, spec common.MembershipSpecE) (res ResolvedMembership, err error) {
	roles, rerr := membershipRoles(spec)
	if rerr != nil {
		err = eh.Errorf("unable to resolve membership %q: %w", name, rerr)
		return
	}
	res.Spec = spec
	var lit string
	if roles.verbatim {
		lit = marshalling.EscapeString(name)
	} else {
		if r.lookup == nil {
			err = eb.Build().Str("membership", name).Stringer("spec", spec).Errorf("ref channel needs an IdLookup but none was configured")
			return
		}
		var id uint64
		id, err = r.lookup.LookupMembership(name)
		if err != nil {
			err = eb.Build().Str("membership", name).Stringer("spec", spec).Errorf("unable to look up ref membership id: %w", err)
			return
		}
		lit = strconv.FormatUint(id, 10)
	}
	res.Matches = []ColumnMatch{{Role: roles.identity, Literal: lit}}
	return
}

// roleSet is the physical column roles a single membership channel occupies:
// the identity column, its per-attribute cardinality column, whether the
// identity is a verbatim name (vs a ref id), and the high-card parameter
// column for the mixed channels ("" if none).
type roleSet struct {
	identity common.ColumnRoleE
	card     common.ColumnRoleE
	verbatim bool
	param    common.ColumnRoleE
}

// membershipRoles maps one channel (exactly one MembershipSpec bit) to its
// roleSet. All eight channels are mapped so the generator is complete ahead of
// the mappingplan front-end, which emits only the four simple channels today.
func membershipRoles(spec common.MembershipSpecE) (rs roleSet, err error) {
	switch spec {
	case common.MembershipSpecLowCardRef:
		rs = roleSet{common.ColumnRoleLowCardRef, common.ColumnRoleLowCardRefCardinality, false, ""}
	case common.MembershipSpecLowCardVerbatim:
		rs = roleSet{common.ColumnRoleLowCardVerbatim, common.ColumnRoleLowCardVerbatimCardinality, true, ""}
	case common.MembershipSpecHighCardRef:
		rs = roleSet{common.ColumnRoleHighCardRef, common.ColumnRoleHighCardRefCardinality, false, ""}
	case common.MembershipSpecHighCardVerbatim:
		rs = roleSet{common.ColumnRoleHighCardVerbatim, common.ColumnRoleHighCardVerbatimCardinality, true, ""}
	case common.MembershipSpecMixedLowCardRefHighCardParameters:
		rs = roleSet{common.ColumnRoleMixedLowCardRef, common.ColumnRoleMixedLowCardRefCardinality, false, common.ColumnRoleMixedRefHighCardParameters}
	case common.MembershipSpecMixedLowCardVerbatimHighCardParameters:
		rs = roleSet{common.ColumnRoleMixedLowCardVerbatim, common.ColumnRoleMixedLowCardVerbatimCardinality, true, common.ColumnRoleMixedVerbatimHighCardParameters}
	case common.MembershipSpecLowCardRefParametrized:
		rs = roleSet{common.ColumnRoleLowCardRefParametrized, common.ColumnRoleLowCardRefParametrizedCardinality, false, ""}
	case common.MembershipSpecHighCardRefParametrized:
		rs = roleSet{common.ColumnRoleHighCardRefParametrized, common.ColumnRoleHighCardRefParametrizedCardinality, false, ""}
	default:
		err = eb.Build().Stringer("spec", spec).Errorf("membership spec is not a single supported channel: %w", common.ErrNotImplemented)
	}
	return
}

// channelSpec maps a mappingplan membership channel (the four simple channels
// the Plan front-end emits) to its common.MembershipSpec.
func channelSpec(ch mappingplan.MembershipChannel) (spec common.MembershipSpecE, err error) {
	switch ch {
	case mappingplan.MembershipChannelLowCardRef:
		spec = common.MembershipSpecLowCardRef
	case mappingplan.MembershipChannelLowCardVerbatim:
		spec = common.MembershipSpecLowCardVerbatim
	case mappingplan.MembershipChannelHighCardRef:
		spec = common.MembershipSpecHighCardRef
	case mappingplan.MembershipChannelHighCardVerbatim:
		spec = common.MembershipSpecHighCardVerbatim
	default:
		err = eb.Build().Stringer("channel", ch).Errorf("unsupported membership channel: %w", common.ErrNotImplemented)
	}
	return
}
