package registry

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/compiletimeflags"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

func (inst RegisteredNaturalKeyDml) MustAddParents(parents ...RegisteredNaturalKey) (r RegisteredNaturalKeyDml) {
	var err error
	r, err = inst.AddParents(parents...)
	if err != nil {
		log.Panic().Err(err).Msg("unable to add parents")
	}
	return
}
func (inst RegisteredNaturalKeyDml) MustAddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (r RegisteredNaturalKeyDml) {
	var err error
	r, err = inst.AddParentsVirtual(parents...)
	if err != nil {
		log.Panic().Err(err).Msg("unable to add virtual parents")
	}
	return
}
func (inst RegisteredNaturalKeyDml) AddParents(parents ...RegisteredNaturalKey) (r RegisteredNaturalKeyDml, err error) {
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
func (inst RegisteredNaturalKeyDml) AddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (r RegisteredNaturalKeyDml, err error) {
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
func (inst RegisteredNaturalKeyDml) MustAddRestriction(sectionName naming.StylableName, membershipSpec common.MembershipSpecE, cardinality CardinalitySpecE) RegisteredNaturalKey {
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
func (inst RegisteredNaturalKeyDml) SetVirtual() RegisteredNaturalKeyVirtualDml {
	inst.w.flags = inst.w.flags.SetVirtual()
	inst.w.register(inst.w)
	return RegisteredNaturalKeyVirtualDml{
		w: inst.w,
	}
}
func (inst RegisteredNaturalKeyDml) SetFinal() RegisteredNaturalKeyFinalDml {
	inst.w.flags = inst.w.flags.SetFinal()
	return RegisteredNaturalKeyFinalDml{
		w: inst.w,
	}
}
func (inst RegisteredNaturalKeyDml) SetDeprecated() RegisteredNaturalKeyDml {
	inst.w.flags = inst.w.flags.SetDeprecated()
	inst.w.register(inst.w)
	return inst
}
func (inst RegisteredNaturalKeyDml) ClearDeprecated() RegisteredNaturalKeyDml {
	inst.w.flags = inst.w.flags.ClearDeprecated()
	inst.w.register(inst.w)
	return inst
}
func (inst RegisteredNaturalKeyDml) End() RegisteredNaturalKey {
	return inst.w
}
