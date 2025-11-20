package registry

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/compiletimeflags"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

func (inst RegisteredNaturalKeyFinalDml) MustAddParents(parents ...RegisteredNaturalKey) (r RegisteredNaturalKeyFinalDml) {
	var err error
	r, err = inst.AddParents(parents...)
	if err != nil {
		log.Panic().Err(err).Msg("unable to add parents")
	}
	return
}
func (inst RegisteredNaturalKeyFinalDml) MustAddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (r RegisteredNaturalKeyFinalDml) {
	var err error
	r, err = inst.AddParentsVirtual(parents...)
	if err != nil {
		log.Panic().Err(err).Msg("unable to add virtual parents")
	}
	return
}
func (inst RegisteredNaturalKeyFinalDml) AddParents(parents ...RegisteredNaturalKey) (r RegisteredNaturalKeyFinalDml, err error) {
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
func (inst RegisteredNaturalKeyFinalDml) AddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (r RegisteredNaturalKeyFinalDml, err error) {
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
func (inst RegisteredNaturalKeyFinalDml) MustAddRestriction(sectionName naming.StylableName, membershipSpec common.MembershipSpecE, cardinality CardinalitySpecE) RegisteredNaturalKeyFinalDml {
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
	inst.w.register(inst.w)
	return inst
}
func (inst RegisteredNaturalKeyFinalDml) ClearFinal() RegisteredNaturalKeyDml {
	inst.w.flags = inst.w.flags.ClearVirtual()
	inst.w.register(inst.w)
	return RegisteredNaturalKeyDml{
		w: inst.w,
	}
}
func (inst RegisteredNaturalKeyFinalDml) SetDeprecated() RegisteredNaturalKeyFinalDml {
	inst.w.flags = inst.w.flags.SetDeprecated()
	inst.w.register(inst.w)
	return inst
}
func (inst RegisteredNaturalKeyFinalDml) ClearDeprecated() RegisteredNaturalKeyFinalDml {
	inst.w.flags = inst.w.flags.ClearDeprecated()
	inst.w.register(inst.w)
	return inst
}
func (inst RegisteredNaturalKeyFinalDml) End() RegisteredNaturalKeyFinal {
	return RegisteredNaturalKeyFinal{
		w: inst.w,
	}
}
