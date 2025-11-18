package registry

import (
	"iter"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/naturalkey"
)

func compareNaturalKey(a naming.StylableName, b naming.StylableName) int {
	return strings.Compare(string(a), string(b))
}

func NewNaturalKeyRegistry[C contract.ContractI](tagValue identifier.TagValue, estSize int, namingStyle naming.NamingStyleE, untaggedOffset identifier.UntaggedId, contr C) (inst *HumanReadableNaturalKeyRegistry[C], err error) {
	lookup := containers.NewBinarySearchGrowingKV[naming.StylableName, RegisteredNaturalKey](estSize, compareNaturalKey)
	inst = &HumanReadableNaturalKeyRegistry[C]{
		tv:             tagValue,
		tag:            tagValue.GetTag(),
		untaggedOffset: untaggedOffset,
		lookup:         lookup,
		namingStyle:    namingStyle,
		contr:          contr,
		memEnc:         naturalkey.NewEncoder(),
	}
	return
}
func MustNewNaturalKeyRegistry[C contract.ContractI](tagValue identifier.TagValue, estSize int, namingStyle naming.NamingStyleE, untaggedOffset identifier.UntaggedId, contr C) (inst *HumanReadableNaturalKeyRegistry[C]) {
	var err error
	inst, err = NewNaturalKeyRegistry[C](tagValue,
		estSize,
		namingStyle,
		untaggedOffset,
		contr)
	if err != nil {
		log.Panic().Err(err).Msg("unable to create tag value registry")
	}
	return
}
func (inst *HumanReadableNaturalKeyRegistry[C]) Length() int {
	return inst.lookup.Len()
}

func (inst *HumanReadableNaturalKeyRegistry[C]) IterateAll() iter.Seq[RegisteredNaturalKey] {
	return inst.lookup.IterateValues()
}

func (inst *HumanReadableNaturalKeyRegistry[C]) MustRegister(nk naming.StylableName, parents ...RegisteredNaturalKey) (r RegisteredNaturalKey) {
	var err error
	r, err = inst.Register(nk, parents...)
	if err != nil {
		log.Panic().Err(err).Msg("unable to register natural key")
	}
	return
}

var ErrNotFound = eh.Errorf("item is not contained in registry")

func (inst *HumanReadableNaturalKeyRegistry[C]) Lookup(nk naming.StylableName) (r RegisteredNaturalKey, err error) {
	var has bool
	r, has = inst.lookup.Get(nk)
	if !has {
		r, has = inst.lookup.Get(nk.Convert(inst.namingStyle))
	}
	if !has {
		err = ErrNotFound
	}
	return
}
func (inst *HumanReadableNaturalKeyRegistry[C]) GetTagValue() identifier.TagValue {
	return inst.tv
}

func (inst *HumanReadableNaturalKeyRegistry[C]) Register(nk naming.StylableName, parents ...RegisteredNaturalKey) (r RegisteredNaturalKey, err error) {
	if nk.IsValid() {
		nk = nk.Convert(inst.namingStyle)
	}
	err = inst.contr.ValidateNaturalKeyHumanReadable(inst.tv, nk)
	if err != nil {
		err = eb.Build().Stringer("nk", nk).Errorf("unable to register invalid human readable natural key: %w", err)
		return
	}
	lu := inst.lookup
	var parentsId []identifier.TaggedId
	var parentsNk []naming.StylableName
	if len(parents) > 0 {
		parentsId = make([]identifier.TaggedId, 0, len(parents))
		parentsNk = make([]naming.StylableName, 0, len(parents))
		for _, parent := range parents {
			flags := parent.GetFlags()
			if flags.HasFinal() {
				err = eb.Build().Stringer("nk", nk).Stringer("parent", parent.naturalKey).Errorf("can not inherit from final parent")
				return
			}
			parentNk := parent.GetNaturalKey()
			parentRecord, hasParent := lu.Get(parentNk)
			if !hasParent {
				err = eb.Build().Stringer("parentNk", parentNk).Errorf("parent natural key does not yet seem to be registered in this registry")
				return
			}
			id := parentRecord.GetId()
			if slices.Contains(parentsId, id) {
				err = eb.Build().Stringer("parentNk", parentNk).Errorf("multiple parents resolved to the same id")
				return
			}
			parentsId = append(parentsId, parentRecord.GetId())
			parentsNk = append(parentsNk, parentRecord.GetNaturalKey())
		}
	}

	origin := getOrigin()
	var has bool
	r, has = lu.Get(nk)
	if has {
		if r.origin != origin {
			err = eb.Build().Str("origin1", r.origin).Str("origin2", origin).Stringer("nk", nk).Errorf("two different code locations register the same tag value")
			return
		}
	}
	r = RegisteredNaturalKey{
		id:                inst.tag.ComposeId(inst.untaggedOffset + identifier.UntaggedId(lu.Len())),
		origin:            origin,
		moduleInfo:        vcs.ModuleInfo(),
		naturalKey:        nk,
		parentsNaturalKey: parentsNk,
		parentsId:         parentsId,
	}
	lu.UpsertSingle(nk, r)
	return
}
func (inst RegisteredNaturalKey) MustAddParents(parents ...RegisteredNaturalKey) (r RegisteredNaturalKey) {
	var err error
	r, err = inst.AddParents(parents...)
	log.Panic().Err(err).Msg("unable to add parents")
	return
}
func (inst RegisteredNaturalKey) MustAddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (r RegisteredNaturalKey) {
	var err error
	r, err = inst.AddParentsVirtual(parents...)
	log.Panic().Err(err).Msg("unable to add virtual parents")
	return
}
func (inst RegisteredNaturalKey) AddParents(parents ...RegisteredNaturalKey) (r RegisteredNaturalKey, err error) {
	r = inst
	l := len(parents)
	if l == 0 {
		return
	}
	parentsId := slices.Grow(inst.parentsId, l)
	parentsNk := slices.Grow(inst.parentsNaturalKey, l)
	for _, parent := range parents {
		flags := parent.GetFlags()
		if flags.HasFinal() {
			err = eb.Build().Stringer("nk", inst.naturalKey).Stringer("parent", parent.naturalKey).Errorf("can not inherit from final parent")
			return
		}
		if slices.Contains(parentsId, parent.id) {
			err = eb.Build().Stringer("nk", inst.naturalKey).Stringer("parentNk", parent.naturalKey).Errorf("multiple parents resolved to the same id")
			return
		}
		parentsId = append(parentsId, parent.id)
		parentsNk = append(parentsNk, parent.naturalKey)
	}
	r.parentsId = parentsId
	r.parentsNaturalKey = parentsNk
	return
}
func (inst RegisteredNaturalKey) AddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (r RegisteredNaturalKey, err error) {
	r = inst
	l := len(parents)
	if l == 0 {
		return
	}
	parentsId := slices.Grow(inst.parentsId, l)
	parentsNk := slices.Grow(inst.parentsNaturalKey, l)
	for _, parent := range parents {
		flags := parent.GetFlags()
		if flags.HasFinal() {
			err = eb.Build().Stringer("nk", inst.naturalKey).Stringer("parent", parent.w.naturalKey).Errorf("can not inherit from final parent")
			return
		}
		if slices.Contains(parentsId, parent.w.id) {
			err = eb.Build().Stringer("nk", inst.naturalKey).Stringer("parentNk", parent.w.naturalKey).Errorf("multiple parents resolved to the same id")
			return
		}
		parentsId = append(parentsId, parent.w.id)
		parentsNk = append(parentsNk, parent.w.naturalKey)
	}
	r.parentsId = parentsId
	r.parentsNaturalKey = parentsNk
	return
}
func (inst RegisteredNaturalKey) MustAddRestriction(sectionName string, membershipSpec common.MembershipSpecE, cardinality CardinalitySpecE) RegisteredNaturalKey {
	inst.allowedColumnsSectionNames = append(inst.allowedColumnsSectionNames, sectionName)
	inst.allowedColumnsSectionMembership = append(inst.allowedColumnsSectionMembership, membershipSpec)
	for m := range membershipSpec.Iterate() {
		switch m {
		case common.MembershipSpecLowCardRef, common.MembershipSpecHighCardRef, common.MembershipSpecMixedLowCardRefHighCardParameters, common.MembershipSpecLowCardRefParametrized, common.MembershipSpecHighCardRefParametrized:
			break
		default:
			log.Panic().Stringer("m", m).Msg("found disallowed membership spec for natural key vdd")
		}
	}
	inst.allowedCardinality = append(inst.allowedCardinality, cardinality)
	return inst
}
func (inst RegisteredNaturalKey) GetModuleInfo() string {
	return inst.moduleInfo
}
func (inst RegisteredNaturalKey) GetNaturalKey() naming.StylableName {
	return inst.naturalKey
}

func (inst RegisteredNaturalKey) GetTagValue() identifier.TagValue {
	return inst.id.GetTag().GetValue()
}
func (inst RegisteredNaturalKey) GetId() identifier.TaggedId {
	return inst.id
}
func (inst RegisteredNaturalKey) GetOrigin() string {
	return inst.origin
}
func (inst RegisteredNaturalKey) GetParentsId() []identifier.TaggedId {
	return inst.parentsId
}
func (inst RegisteredNaturalKey) GetParentsNaturalKey() []naming.StylableName {
	return inst.parentsNaturalKey
}
func (inst RegisteredNaturalKey) GetNumberOfRestrictions() (n int) {
	return len(inst.allowedCardinality)
}
func (inst RegisteredNaturalKey) IterateRestrictionIndices() iter.Seq[int] {
	return func(yield func(int) bool) {
		n := len(inst.allowedCardinality)
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}
func (inst RegisteredNaturalKey) GetRestrictionCardinality(idx int) CardinalitySpecE {
	return inst.allowedCardinality[idx]
}
func (inst RegisteredNaturalKey) GetRestrictionSectionName(idx int) string {
	return inst.allowedColumnsSectionNames[idx]
}
func (inst RegisteredNaturalKey) GetRestrictionSectionMembership(idx int) common.MembershipSpecE {
	return inst.allowedColumnsSectionMembership[idx]
}
func (inst RegisteredNaturalKey) SetVirtual() RegisteredNaturalKeyVirtual {
	inst.flags = inst.flags.SetVirtual()
	return RegisteredNaturalKeyVirtual{
		w: inst,
	}
}
func (inst RegisteredNaturalKeyVirtual) ClearVirtual() RegisteredNaturalKey {
	r := inst.w
	r.flags = r.flags.ClearVirtual()
	return r
}
func (inst RegisteredNaturalKey) SetFinal() RegisteredNaturalKeyFinal {
	inst.flags = inst.flags.SetFinal()
	return RegisteredNaturalKeyFinal{
		w: inst,
	}
}
func (inst RegisteredNaturalKeyFinal) ClearFinal() RegisteredNaturalKey {
	r := inst.w
	r.flags = r.flags.ClearVirtual()
	return r
}
func (inst RegisteredNaturalKey) SetDeprecated() RegisteredNaturalKey {
	inst.flags = inst.flags.SetDeprecated()
	return inst
}
func (inst RegisteredNaturalKey) ClearDeprecated() RegisteredNaturalKey {
	inst.flags = inst.flags.ClearDeprecated()
	return inst
}
func (inst RegisteredNaturalKey) GetFlags() RegisteredValueFlagsE {
	return inst.flags
}

func (inst RegisteredNaturalKeyVirtual) GetNumberOfRestrictions() (n int) {
	return inst.w.GetNumberOfRestrictions()
}
func (inst RegisteredNaturalKeyVirtual) IterateRestrictionIndices() iter.Seq[int] {
	return inst.w.IterateRestrictionIndices()
}
func (inst RegisteredNaturalKeyVirtual) GetRestrictionCardinality(idx int) CardinalitySpecE {
	return inst.w.GetRestrictionCardinality(idx)
}
func (inst RegisteredNaturalKeyVirtual) GetRestrictionSectionName(idx int) string {
	return inst.w.GetRestrictionSectionName(idx)
}
func (inst RegisteredNaturalKeyVirtual) GetRestrictionSectionMembership(idx int) common.MembershipSpecE {
	return inst.w.GetRestrictionSectionMembership(idx)
}
func (inst RegisteredNaturalKeyVirtual) GetFlags() RegisteredValueFlagsE {
	return inst.w.flags
}
func (inst RegisteredNaturalKeyVirtual) GetModuleInfo() string {
	return inst.w.moduleInfo
}
func (inst RegisteredNaturalKeyVirtual) GetNaturalKey() naming.StylableName {
	return inst.w.naturalKey
}
func (inst RegisteredNaturalKeyVirtual) GetOrigin() string {
	return inst.w.origin
}
func (inst RegisteredNaturalKeyVirtual) GetParentsId() []identifier.TaggedId {
	return inst.w.parentsId
}
func (inst RegisteredNaturalKeyVirtual) GetParentsNaturalKey() []naming.StylableName {
	return inst.w.parentsNaturalKey
}
func (inst RegisteredNaturalKeyFinal) GetNumberOfRestrictions() (n int) {
	return inst.w.GetNumberOfRestrictions()
}
func (inst RegisteredNaturalKeyFinal) IterateRestrictionIndices() iter.Seq[int] {
	return inst.w.IterateRestrictionIndices()
}
func (inst RegisteredNaturalKeyFinal) GetRestrictionCardinality(idx int) CardinalitySpecE {
	return inst.w.GetRestrictionCardinality(idx)
}
func (inst RegisteredNaturalKeyFinal) GetRestrictionSectionName(idx int) string {
	return inst.w.GetRestrictionSectionName(idx)
}
func (inst RegisteredNaturalKeyFinal) GetRestrictionSectionMembership(idx int) common.MembershipSpecE {
	return inst.w.GetRestrictionSectionMembership(idx)
}
func (inst RegisteredNaturalKeyFinal) GetFlags() RegisteredValueFlagsE {
	return inst.w.flags
}
func (inst RegisteredNaturalKeyFinal) GetModuleInfo() string {
	return inst.w.moduleInfo
}
func (inst RegisteredNaturalKeyFinal) GetNaturalKey() naming.StylableName {
	return inst.w.naturalKey
}
func (inst RegisteredNaturalKeyFinal) GetOrigin() string {
	return inst.w.origin
}
func (inst RegisteredNaturalKeyFinal) GetParentsId() []identifier.TaggedId {
	return inst.w.parentsId
}
func (inst RegisteredNaturalKeyFinal) GetParentsNaturalKey() []naming.StylableName {
	return inst.w.parentsNaturalKey
}
