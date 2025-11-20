package registry

import (
	"iter"

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

func NewNaturalKeyRegistry[C contract.ContractI](tagValue identifier.TagValue, estSize int, namingStyle naming.NamingStyleE, untaggedOffset identifier.UntaggedId, contr C) (inst *HumanReadableNaturalKeyRegistry[C], err error) {
	lookup := containers.NewBinarySearchGrowingKVOrdered[naming.StylableName, RegisteredNaturalKey](estSize)
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

func (inst *HumanReadableNaturalKeyRegistry[C]) MustBegin(nk naming.StylableName) (r RegisteredNaturalKeyDml) {
	var err error
	r, err = inst.Begin(nk)
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

func (inst *HumanReadableNaturalKeyRegistry[C]) Begin(nk naming.StylableName) (r RegisteredNaturalKeyDml, err error) {
	if nk.IsValid() {
		nk = nk.Convert(inst.namingStyle)
	}
	err = inst.contr.ValidateNaturalKeyHumanReadable(inst.tv, nk)
	if err != nil {
		err = eb.Build().Stringer("nk", nk).Errorf("unable to register invalid human readable natural key: %w", err)
		return
	}
	lu := inst.lookup
	origin := getOrigin()
	var has bool
	var w RegisteredNaturalKey
	w, has = lu.Get(nk)
	if has {
		if w.origin != origin {
			err = eb.Build().Str("origin1", w.origin).Str("origin2", origin).Stringer("nk", nk).Errorf("two different code locations register the same natural key value")
			return
		}
	}
	w = RegisteredNaturalKey{
		id:                              inst.tag.ComposeId(inst.untaggedOffset + identifier.UntaggedId(lu.Len())),
		origin:                          origin,
		moduleInfo:                      vcs.ModuleInfo(),
		naturalKey:                      nk,
		parents:                         containers.NewBinarySearchGrowingKVOrdered[identifier.TaggedId, RegisteredNaturalKey](1),
		parentsVirtual:                  containers.NewBinarySearchGrowingKVOrdered[identifier.TaggedId, RegisteredNaturalKeyVirtual](1),
		children:                        containers.NewBinarySearchGrowingKVOrdered[identifier.TaggedId, RegisteredNaturalKey](1),
		childrenVirtual:                 containers.NewBinarySearchGrowingKVOrdered[identifier.TaggedId, RegisteredNaturalKeyVirtual](1),
		allowedColumnsSectionNames:      nil,
		allowedColumnsSectionMembership: nil,
		allowedCardinality:              nil,
		flags:                           0,
		register: func(t RegisteredNaturalKey) RegisteredNaturalKey {
			lu.UpsertSingle(nk, t)
			return t
		},
	}
	lu.UpsertSingle(nk, w)
	r = RegisteredNaturalKeyDml{
		w: w,
	}
	return
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
func (inst RegisteredNaturalKey) IterateParents() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	return inst.parents.IteratePairs()
}
func (inst RegisteredNaturalKey) IterateParentsVirtual() iter.Seq2[identifier.TaggedId, RegisteredNaturalKeyVirtual] {
	return inst.parentsVirtual.IteratePairs()
}
func (inst RegisteredNaturalKey) IterateChildren() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	return inst.children.IteratePairs()
}
func (inst RegisteredNaturalKey) IterateChildrenVirtual() iter.Seq2[identifier.TaggedId, RegisteredNaturalKeyVirtual] {
	return inst.childrenVirtual.IteratePairs()
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
func (inst RegisteredNaturalKey) GetRestrictionSectionName(idx int) naming.StylableName {
	return inst.allowedColumnsSectionNames[idx]
}
func (inst RegisteredNaturalKey) GetRestrictionSectionMembership(idx int) common.MembershipSpecE {
	return inst.allowedColumnsSectionMembership[idx]
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
func (inst RegisteredNaturalKeyVirtual) GetRestrictionSectionName(idx int) naming.StylableName {
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

func (inst RegisteredNaturalKeyVirtual) IterateParents() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	return inst.w.parents.IteratePairs()
}
func (inst RegisteredNaturalKeyVirtual) IterateParentsVirtual() iter.Seq2[identifier.TaggedId, RegisteredNaturalKeyVirtual] {
	return inst.w.parentsVirtual.IteratePairs()
}
func (inst RegisteredNaturalKeyVirtual) IterateChildren() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	return inst.w.children.IteratePairs()
}
func (inst RegisteredNaturalKeyVirtual) IterateChildrenVirtual() iter.Seq2[identifier.TaggedId, RegisteredNaturalKeyVirtual] {
	return inst.w.childrenVirtual.IteratePairs()
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
func (inst RegisteredNaturalKeyFinal) GetRestrictionSectionName(idx int) naming.StylableName {
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
func (inst RegisteredNaturalKeyFinal) IterateParents() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	return inst.w.parents.IteratePairs()
}
func (inst RegisteredNaturalKeyFinal) IterateParentsVirtual() iter.Seq2[identifier.TaggedId, RegisteredNaturalKeyVirtual] {
	return inst.w.parentsVirtual.IteratePairs()
}
