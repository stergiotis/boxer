package registry

import (
	"iter"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/naturalkey"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// NewNaturalKeyRegistry creates a registry minting ids under a claimed tag
// value.
//
// The claim is the argument rather than the number because ids under one tag
// value are one id space: two vocabularies on one value are one vocabulary
// with two sets of names, and nothing local can notice. [tagmint] is where
// that is checked, and its token cannot be built anywhere else — so a registry
// cannot exist around the check (ADR-0183 D0).
//
// estSize is a capacity hint only — an id is what its registration declares,
// so nothing here moves when the hint changes.
func NewNaturalKeyRegistry[C contract.ContractI](claim tagmint.ClaimedTagValue, estSize int, namingStyle naming.NamingStyleE, contr C) (inst *HumanReadableNaturalKeyRegistry[C], err error) {
	if !claim.IsValid() {
		err = eb.Build().Errorf("a registry mints under a claimed tag value — claim one with tagmint.MustClaim(name, value, maxExpectedIds)")
		return
	}
	tagValue := claim.Value()
	err = contr.ValidateTagValue(tagValue)
	if err != nil {
		err = eb.Build().Str("claimant", claim.Name()).Uint32("tagValue", tagValue.Value()).Errorf("the contract refuses this tag value: %w", err)
		return
	}
	inst = &HumanReadableNaturalKeyRegistry[C]{
		tv:          tagValue,
		tag:         tagValue.GetTag(),
		lookup:      containers.NewBinarySearchGrowingKVOrdered[naming.StylableName, RegisteredNaturalKey](estSize),
		roots:       containers.NewBinarySearchGrowingKVOrdered[naming.StylableName, RegisteredNaturalKey](estSize),
		byOrdinal:   containers.NewBinarySearchGrowingKVOrdered[identifier.UntaggedId, naming.StylableName](estSize),
		namingStyle: namingStyle,
		contr:       contr,
		memEnc:      naturalkey.NewEncoder(),
	}
	return
}

// MustNewNaturalKeyRegistry is [NewNaturalKeyRegistry] for a package-level
// declaration.
func MustNewNaturalKeyRegistry[C contract.ContractI](claim tagmint.ClaimedTagValue, estSize int, namingStyle naming.NamingStyleE, contr C) (inst *HumanReadableNaturalKeyRegistry[C]) {
	var err error
	inst, err = NewNaturalKeyRegistry[C](claim,
		estSize,
		namingStyle,
		contr)
	if err != nil {
		log.Panic().Err(err).Msg("unable to create natural key registry")
	}
	return
}
func (inst *HumanReadableNaturalKeyRegistry[C]) Length() int {
	return inst.lookup.Len()
}

func (inst *HumanReadableNaturalKeyRegistry[C]) IterateAll() iter.Seq2[naming.StylableName, RegisteredNaturalKey] {
	return inst.lookup.IteratePairs()
}
func (inst *HumanReadableNaturalKeyRegistry[C]) IterateAllRoots() iter.Seq2[naming.StylableName, RegisteredNaturalKey] {
	return inst.roots.IteratePairs()
}

// MustBegin is [HumanReadableNaturalKeyRegistry.Begin] for a package-level
// declaration, where a refusal is a build-time fact wearing a run-time coat.
func (inst *HumanReadableNaturalKeyRegistry[C]) MustBegin(nk naming.StylableName, ordinal identifier.UntaggedId) (r RegisteredNaturalKeyDml) {
	var err error
	r, err = inst.Begin(nk, ordinal)
	if err != nil {
		log.Panic().Err(err).Stringer("nk", nk).Uint64("ordinal", ordinal.Value()).Msg("unable to register natural key")
	}
	return
}

// MustBeginNext is [HumanReadableNaturalKeyRegistry.BeginNext] for a
// package-level declaration.
func (inst *HumanReadableNaturalKeyRegistry[C]) MustBeginNext(nk naming.StylableName) (r RegisteredNaturalKeyDml) {
	var err error
	r, err = inst.BeginNext(nk)
	if err != nil {
		log.Panic().Err(err).Stringer("nk", nk).Msg("unable to register natural key")
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

// BeginNext registers nk at the next free ordinal — the registration-order
// regime, kept for registries whose contract allows it.
//
// A version-controlled vocabulary's contract does not: an id derived from how
// many names came before it moves when a name is inserted, a var block is
// reordered, or a file is renamed, and every such move silently re-points rows
// already written (ADR-0183 D0). Ephemeral registries — tests, throwaway
// fixtures — have no stored rows to re-point, which is the whole difference.
func (inst *HumanReadableNaturalKeyRegistry[C]) BeginNext(nk naming.StylableName) (r RegisteredNaturalKeyDml, err error) {
	err = inst.contr.ValidateImplicitOrdinal()
	if err != nil {
		err = eb.Build().Stringer("nk", nk).Errorf("unable to mint an ordinal from registration order: %w", err)
		return
	}
	return inst.begin(nk, identifier.UntaggedId(inst.lookup.Len()), true)
}

// Begin registers nk at ordinal, the untagged id composed with the registry's
// tag. The pair is the assignment: stated in source, reviewable in a diff, and
// unmoved by anything that happens around it.
//
// Refusals are at init, where a linked test sees them: an ordinal another name
// already holds, a name already registered from a different code location, or
// an ordinal wider than the registry's tag leaves room for.
func (inst *HumanReadableNaturalKeyRegistry[C]) Begin(nk naming.StylableName, ordinal identifier.UntaggedId) (r RegisteredNaturalKeyDml, err error) {
	return inst.begin(nk, ordinal, false)
}

func (inst *HumanReadableNaturalKeyRegistry[C]) begin(nk naming.StylableName, ordinal identifier.UntaggedId, implicit bool) (r RegisteredNaturalKeyDml, err error) {
	if nk.IsValid() {
		nk = nk.Convert(inst.namingStyle)
	}
	err = inst.contr.ValidateNaturalKeyHumanReadable(inst.tv, nk)
	if err != nil {
		err = eb.Build().Stringer("nk", nk).Errorf("unable to register invalid human readable natural key: %w", err)
		return
	}
	if maxId := inst.tag.GetMaxPossibleIdIncl(); ordinal > maxId {
		err = eb.Build().Stringer("nk", nk).Uint64("ordinal", ordinal.Value()).Uint64("maxOrdinal", maxId.Value()).Uint32("tagValue", inst.tv.Value()).Errorf("ordinal does not fit below the registry's tag — the tag's width class holds ordinals up to %d", maxId.Value())
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
		if !implicit && w.id.RemoveTag() != ordinal {
			err = eb.Build().Stringer("nk", nk).Uint64("ordinal", ordinal.Value()).Uint64("registered", w.id.RemoveTag().Value()).Str("origin", origin).Errorf("the same registration states two different ordinals")
			return
		}
		// Same-origin re-Begin: return the existing registration unchanged.
		// Previously this fell through and minted a fresh id from the grown
		// lu.Len(), making the natural-key -> id mapping unstable (review G-5).
		r = RegisteredNaturalKeyDml{
			w: w,
		}
		return
	}
	if other, taken := inst.byOrdinal.Get(ordinal); taken {
		err = eb.Build().Stringer("nk", nk).Stringer("heldBy", other).Uint64("ordinal", ordinal.Value()).Str("origin", origin).Errorf("ordinal %d is already held by %q — an ordinal names one membership for the lifetime of the data", ordinal.Value(), other)
		return
	}
	inst.byOrdinal.UpsertSingle(ordinal, nk)
	w = RegisteredNaturalKey{
		id:                              inst.tag.ComposeId(ordinal),
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
			if t.IsRoot() {
				inst.roots.UpsertSingle(nk, t)
			}
			return t
		},
	}
	lu.UpsertSingle(nk, w) // needed to deduplicate before .End()
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
func (inst RegisteredNaturalKey) IterateAllChildren() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	return func(yield func(identifier.TaggedId, RegisteredNaturalKey) bool) {
		for k, v := range inst.children.IteratePairs() {
			if !yield(k, v) {
				return
			}
		}
		for k, v := range inst.childrenVirtual.IteratePairs() {
			if !yield(k, v.w) {
				return
			}
		}
	}
}
func (inst RegisteredNaturalKey) IterateAllParents() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	return func(yield func(identifier.TaggedId, RegisteredNaturalKey) bool) {
		for k, v := range inst.parents.IteratePairs() {
			if !yield(k, v) {
				return
			}
		}
		for k, v := range inst.parentsVirtual.IteratePairs() {
			if !yield(k, v.w) {
				return
			}
		}
	}
}
func (inst RegisteredNaturalKey) GetNumberOfRestrictions() (n int) {
	return len(inst.allowedCardinality)
}
func (inst RegisteredNaturalKey) IterateRestrictionIndices() iter.Seq[int] {
	return func(yield func(int) bool) {
		n := len(inst.allowedCardinality)
		for i := range n {
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
func (inst RegisteredNaturalKey) IsRoot() bool {
	return inst.parents.IsEmpty() && inst.parentsVirtual.IsEmpty()
}
func (inst RegisteredNaturalKey) IsLeaf() bool {
	return inst.children.IsEmpty() && inst.childrenVirtual.IsEmpty()
}
func (inst RegisteredNaturalKey) GetParentsCount() int {
	return inst.parents.Len() + inst.parentsVirtual.Len()
}
func (inst RegisteredNaturalKey) GetChildrenCount() int {
	return inst.children.Len() + inst.childrenVirtual.Len()
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
func (inst RegisteredNaturalKeyVirtual) IsRoot() bool {
	return inst.w.IsRoot()
}
func (inst RegisteredNaturalKeyVirtual) IsLeaf() bool {
	return inst.w.IsLeaf()
}
func (inst RegisteredNaturalKeyVirtual) GetParentsCount() int {
	return inst.w.GetParentsCount()
}
func (inst RegisteredNaturalKeyVirtual) GetChildrenCount() int {
	return inst.w.GetChildrenCount()
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
func (inst RegisteredNaturalKeyVirtual) GetId() identifier.TaggedId {
	return inst.w.GetId()
}
func (inst RegisteredNaturalKeyVirtual) GetTagValue() identifier.TagValue {
	return inst.w.GetTagValue()
}
func (inst RegisteredNaturalKeyVirtual) IterateAllChildren() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	return func(yield func(identifier.TaggedId, RegisteredNaturalKey) bool) {
		for k, v := range inst.w.children.IteratePairs() {
			if !yield(k, v) {
				return
			}
		}
		for k, v := range inst.w.childrenVirtual.IteratePairs() {
			if !yield(k, v.w) {
				return
			}
		}
	}
}
func (inst RegisteredNaturalKeyVirtual) IterateAllParents() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	return func(yield func(identifier.TaggedId, RegisteredNaturalKey) bool) {
		for k, v := range inst.w.parents.IteratePairs() {
			if !yield(k, v) {
				return
			}
		}
		for k, v := range inst.w.parentsVirtual.IteratePairs() {
			if !yield(k, v.w) {
				return
			}
		}
	}
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
func (inst RegisteredNaturalKeyFinal) IsRoot() bool {
	return inst.w.IsRoot()
}
func (inst RegisteredNaturalKeyFinal) IsLeaf() bool {
	return inst.w.IsLeaf()
}
func (inst RegisteredNaturalKeyFinal) GetParentsCount() int {
	return inst.w.GetParentsCount()
}
func (inst RegisteredNaturalKeyFinal) GetChildrenCount() int {
	return inst.w.GetChildrenCount()
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
func (inst RegisteredNaturalKeyFinal) GetId() identifier.TaggedId {
	return inst.w.GetId()
}
func (inst RegisteredNaturalKeyFinal) IterateAllChildren() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	return func(yield func(identifier.TaggedId, RegisteredNaturalKey) bool) {
		for k, v := range inst.w.children.IteratePairs() {
			if !yield(k, v) {
				return
			}
		}
		for k, v := range inst.w.childrenVirtual.IteratePairs() {
			if !yield(k, v.w) {
				return
			}
		}
	}
}
func (inst RegisteredNaturalKeyFinal) IterateAllParents() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	return func(yield func(identifier.TaggedId, RegisteredNaturalKey) bool) {
		for k, v := range inst.w.parents.IteratePairs() {
			if !yield(k, v) {
				return
			}
		}
		for k, v := range inst.w.parentsVirtual.IteratePairs() {
			if !yield(k, v.w) {
				return
			}
		}
	}
}

func (inst RegisteredNaturalKeyFinal) GetTagValue() identifier.TagValue {
	return inst.w.GetTagValue()
}

func (inst RegisteredNaturalKeyConcrete) GetNumberOfRestrictions() (n int) {
	return inst.w.GetNumberOfRestrictions()
}
func (inst RegisteredNaturalKeyConcrete) IterateRestrictionIndices() iter.Seq[int] {
	return inst.w.IterateRestrictionIndices()
}
func (inst RegisteredNaturalKeyConcrete) GetRestrictionCardinality(idx int) CardinalitySpecE {
	return inst.w.GetRestrictionCardinality(idx)
}
func (inst RegisteredNaturalKeyConcrete) GetRestrictionSectionName(idx int) naming.StylableName {
	return inst.w.GetRestrictionSectionName(idx)
}
func (inst RegisteredNaturalKeyConcrete) GetRestrictionSectionMembership(idx int) common.MembershipSpecE {
	return inst.w.GetRestrictionSectionMembership(idx)
}
func (inst RegisteredNaturalKeyConcrete) GetFlags() RegisteredValueFlagsE {
	return inst.w.GetFlags()
}
func (inst RegisteredNaturalKeyConcrete) IsRoot() bool {
	return inst.w.IsRoot()
}
func (inst RegisteredNaturalKeyConcrete) IsLeaf() bool {
	return inst.w.IsLeaf()
}
func (inst RegisteredNaturalKeyConcrete) GetParentsCount() int {
	return inst.w.GetParentsCount()
}
func (inst RegisteredNaturalKeyConcrete) GetChildrenCount() int {
	return inst.w.GetChildrenCount()
}

func (inst RegisteredNaturalKeyConcrete) GetModuleInfo() string {
	return inst.w.moduleInfo
}
func (inst RegisteredNaturalKeyConcrete) GetNaturalKey() naming.StylableName {
	return inst.w.naturalKey
}
func (inst RegisteredNaturalKeyConcrete) GetOrigin() string {
	return inst.w.origin
}
func (inst RegisteredNaturalKeyConcrete) IterateAllChildren() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	return func(yield func(identifier.TaggedId, RegisteredNaturalKey) bool) {
		for k, v := range inst.w.children.IteratePairs() {
			if !yield(k, v) {
				return
			}
		}
		for k, v := range inst.w.childrenVirtual.IteratePairs() {
			if !yield(k, v.w) {
				return
			}
		}
	}
}
func (inst RegisteredNaturalKeyConcrete) IterateAllParents() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	return func(yield func(identifier.TaggedId, RegisteredNaturalKey) bool) {
		for k, v := range inst.w.parents.IteratePairs() {
			if !yield(k, v) {
				return
			}
		}
		for k, v := range inst.w.parentsVirtual.IteratePairs() {
			if !yield(k, v.w) {
				return
			}
		}
	}
}
func (inst RegisteredNaturalKeyConcrete) GetId() identifier.TaggedId {
	return inst.w.GetId()
}
func (inst RegisteredNaturalKeyConcrete) GetTagValue() identifier.TagValue {
	return inst.w.GetTagValue()
}
