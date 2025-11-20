package registry

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/compiletimeflags"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

func (inst RegisteredNaturalKeyVirtualDml) MustAddParents(parents ...RegisteredNaturalKey) (r RegisteredNaturalKeyVirtualDml) {
	var err error
	r, err = inst.AddParents(parents...)
	if err != nil {
		log.Panic().Err(err).Msg("unable to add parents")
	}
	return
}
func (inst RegisteredNaturalKeyVirtualDml) MustAddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (r RegisteredNaturalKeyVirtualDml) {
	var err error
	r, err = inst.AddParentsVirtual(parents...)
	if err != nil {
		log.Panic().Err(err).Msg("unable to add virtual parents")
	}
	return
}
func (inst RegisteredNaturalKeyVirtualDml) AddParents(parents ...RegisteredNaturalKey) (r RegisteredNaturalKeyVirtualDml, err error) {
	r = inst
	l := len(parents)
	if l == 0 {
		return
	}
	ps := inst.w.parents
	ps.Grow(len(parents))
	for _, parent := range parents {
		flags := parent.GetFlags()
		if compiletimeflags.ExtraChecks && flags.HasFinal() {
			err = eb.Build().Stringer("nk", inst.w.naturalKey).Stringer("parent", parent.naturalKey).Errorf("can not inherit from final parent")
			return
		}
		ps.UpsertBatch(parent.id, parent)
		parent.children.UpsertBatch(inst.w.id, inst.w)
	}
	r.w.register(r.w)
	return
}
func (inst RegisteredNaturalKeyVirtualDml) AddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (r RegisteredNaturalKeyVirtualDml, err error) {
	r = inst
	l := len(parents)
	if l == 0 {
		return
	}
	ps := inst.w.parentsVirtual
	for _, parent := range parents {
		flags := parent.GetFlags()
		if compiletimeflags.ExtraChecks && flags.HasFinal() {
			err = eb.Build().Stringer("nk", inst.w.naturalKey).Stringer("parent", parent.w.naturalKey).Errorf("can not inherit from final parent")
			return
		}
		ps.UpsertBatch(parent.w.id, parent)
		parent.w.children.UpsertBatch(inst.w.id, inst.w)
	}
	r.w.register(r.w)
	return
}
func (inst RegisteredNaturalKeyVirtualDml) MustAddRestriction(sectionName naming.StylableName, membershipSpec common.MembershipSpecE, cardinality CardinalitySpecE) RegisteredNaturalKey {
	inst.w.allowedColumnsSectionNames = append(inst.w.allowedColumnsSectionNames, sectionName)
	inst.w.allowedColumnsSectionMembership = append(inst.w.allowedColumnsSectionMembership, membershipSpec)
	for m := range membershipSpec.Iterate() {
		switch m {
		case common.MembershipSpecLowCardRef, common.MembershipSpecHighCardRef, common.MembershipSpecMixedLowCardRefHighCardParameters, common.MembershipSpecLowCardRefParametrized, common.MembershipSpecHighCardRefParametrized:
			break
		default:
			log.Panic().Stringer("m", m).Msg("found disallowed membership spec for natural key vdd")
		}
	}
	inst.w.allowedCardinality = append(inst.w.allowedCardinality, cardinality)
	return inst.w.register(inst.w)
}
func (inst RegisteredNaturalKeyVirtualDml) ClearVirtual() RegisteredNaturalKeyDml {
	inst.w.flags = inst.w.flags.ClearVirtual()
	inst.w.register(inst.w)
	return RegisteredNaturalKeyDml{
		w: inst.w,
	}
}
func (inst RegisteredNaturalKeyVirtualDml) SetDeprecated() RegisteredNaturalKeyVirtualDml {
	inst.w.flags = inst.w.flags.SetDeprecated()
	inst.w.register(inst.w)
	return inst
}
func (inst RegisteredNaturalKeyVirtualDml) ClearDeprecated() RegisteredNaturalKeyVirtualDml {
	inst.w.flags = inst.w.flags.ClearDeprecated()
	inst.w.register(inst.w)
	return inst
}
func (inst RegisteredNaturalKeyVirtualDml) End() RegisteredNaturalKeyVirtual {
	return RegisteredNaturalKeyVirtual{
		w: inst.w,
	}
}
