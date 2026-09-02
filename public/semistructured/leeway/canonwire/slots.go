package canonwire

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// The slot table of ADR-0210 SD2: a table description reduced to the CT
// signatures that key its tagged slots, plus the two facts SD5's dispatch
// reads — which signatures more than one slot claims, and which membership
// channels each candidate section can store.
//
// This is generation-time code: it needs a common.TableDesc, which the wire
// runtime deliberately does not. The signature strings themselves are built by
// runtime.GroupOf and runtime.SignatureOf, which are pure string work and stay
// on the runtime side so a decoder can compare a key it read against one it
// computed.

// SlotSection is one tagged section as it sits inside a slot.
type SlotSection struct {
	// SectionIdx is the section's index in TableDesc.TaggedValuesSections.
	SectionIdx int
	Name       naming.StylableName
	// Group is the section's CT group — its position in the slot's signature.
	Group string
	// ColumnOrder maps a key position to an index into ValueColumnTypes:
	// ColumnOrder[k] is the declared column that supplies key position k.
	ColumnOrder []int
	// MembershipSpec is the section's declared spec, the input to the SD5
	// narrowing step.
	MembershipSpec common.MembershipSpecE
	// ValueColumnTypes are the section's value columns in *declaration* order;
	// ColumnOrder permutes them into key order.
	ValueColumnTypes []canonicaltypes.PrimitiveAstNodeI
}

// Slot is one wire slot: a co-section group, or a standalone section, under
// the signature that keys it. Sections are in signature order — the order the
// sorted groups appear in Signature.
type Slot struct {
	Signature string
	Sections  []SlotSection
}

// PlainSlot is one plain item type's columns. It is keyed on the wire by
// ItemType, not by Group (ADR-0210 SD2, fork 1).
type PlainSlot struct {
	ItemType common.PlainItemTypeE
	// Group is the plain section's CT group. It is not on the wire and no
	// construction-time comparison reads it (SD2: the decoder's typed reads
	// catch a width mismatch value by value); it keys the stable column
	// order and feeds the `table slots` report.
	Group string
	// ColumnOrder maps a key position to an index into Names / ColumnTypes.
	ColumnOrder []int
	Names       []naming.StylableName
	ColumnTypes []canonicaltypes.PrimitiveAstNodeI
}

// SlotTable is a table description reduced to what the wire form keys on.
type SlotTable struct {
	// Slots are ordered by signature, then by their first section's index.
	Slots []Slot
	// BySignature maps a signature to the ordinals of the slots carrying it.
	// An entry with two or more ordinals is an ambiguity set: SD5's dispatch
	// is needed for it, and the generated decoder refuses a nil DispatcherI.
	BySignature map[string][]int
	// Plains are ordered by item type ordinal, which is also the order their
	// CBOR uint keys sort in.
	Plains []PlainSlot
}

// BuildSlotTable reduces a table description to its slots (ADR-0210 SD2).
//
// One slot per co-section group — sections sharing a non-empty
// TaggedValuesSection.CoSectionGroup — and one per standalone section. Every
// canonical type is checked with IsValid(), because an invalid one renders as
// a string no parser accepts and would put an unreadable key on the wire.
func BuildSlotTable(tblDesc *common.TableDesc) (tbl SlotTable, err error) {
	if tblDesc == nil {
		err = eb.Build().Errorf("no table description")
		return
	}
	tbl.Slots = make([]Slot, 0, len(tblDesc.TaggedValuesSections))
	tbl.BySignature = make(map[string][]int, len(tblDesc.TaggedValuesSections))
	tbl.Plains = make([]PlainSlot, 0, len(common.AllPlainItemTypes))

	// Co-section groups first, in first-section order, so the grouping itself
	// does not depend on map iteration.
	coOrder := make([]naming.Key, 0, len(tblDesc.TaggedValuesSections))
	coMembers := make(map[naming.Key][]int, len(tblDesc.TaggedValuesSections))
	standalone := make([]int, 0, len(tblDesc.TaggedValuesSections))
	for i := range tblDesc.TaggedValuesSections {
		key := tblDesc.TaggedValuesSections[i].CoSectionGroup
		if key == "" {
			standalone = append(standalone, i)
			continue
		}
		if _, seen := coMembers[key]; !seen {
			coOrder = append(coOrder, key)
		}
		coMembers[key] = append(coMembers[key], i)
	}

	for _, key := range coOrder {
		var slot Slot
		slot, err = buildSlot(tblDesc, coMembers[key])
		if err != nil {
			err = eb.Build().Str("coSectionGroup", string(key)).Errorf("unable to build the co-section group's slot: %w", err)
			return
		}
		tbl.Slots = append(tbl.Slots, slot)
	}
	for _, i := range standalone {
		var slot Slot
		slot, err = buildSlot(tblDesc, []int{i})
		if err != nil {
			err = eb.Build().Stringer("section", tblDesc.TaggedValuesSections[i].Name).Errorf("unable to build the section's slot: %w", err)
			return
		}
		tbl.Slots = append(tbl.Slots, slot)
	}

	slices.SortStableFunc(tbl.Slots, func(a Slot, b Slot) int {
		if c := strings.Compare(a.Signature, b.Signature); c != 0 {
			return c
		}
		return cmp.Compare(a.Sections[0].SectionIdx, b.Sections[0].SectionIdx)
	})
	for i := range tbl.Slots {
		sig := tbl.Slots[i].Signature
		tbl.BySignature[sig] = append(tbl.BySignature[sig], i)
	}

	for _, itemType := range common.AllPlainItemTypes {
		if itemType == common.PlainItemTypeNone {
			continue
		}
		var plain PlainSlot
		var found bool
		plain, found, err = buildPlainSlot(tblDesc, itemType)
		if err != nil {
			err = eb.Build().Stringer("plainItemType", itemType).Errorf("unable to build the plain slot: %w", err)
			return
		}
		if found {
			tbl.Plains = append(tbl.Plains, plain)
		}
	}
	return
}

// buildSlot turns a set of section indices — one for a standalone section, the
// members of one co-section group otherwise — into a slot.
func buildSlot(tblDesc *common.TableDesc, sectionIdxs []int) (slot Slot, err error) {
	n := len(sectionIdxs)
	groups := make([]string, n)
	orders := make([][]int, n)
	for k, idx := range sectionIdxs {
		sec := &tblDesc.TaggedValuesSections[idx]
		for j, ct := range sec.ValueColumnTypes {
			if ct == nil || !ct.IsValid() {
				err = eb.Build().Stringer("section", sec.Name).Int("column", j).Errorf("invalid canonical type")
				return
			}
		}
		groups[k], orders[k] = runtime.GroupOf(sec.ValueColumnTypes)
	}
	sig, order := runtime.SignatureOf(groups)
	slot.Signature = sig
	slot.Sections = make([]SlotSection, 0, n)
	for _, k := range order {
		idx := sectionIdxs[k]
		sec := &tblDesc.TaggedValuesSections[idx]
		slot.Sections = append(slot.Sections, SlotSection{
			SectionIdx:       idx,
			Name:             sec.Name,
			Group:            groups[k],
			ColumnOrder:      orders[k],
			MembershipSpec:   sec.MembershipSpec,
			ValueColumnTypes: sec.ValueColumnTypes,
		})
	}
	return
}

// buildPlainSlot collects one plain item type's columns in declaration order.
// found is false when the table declares no column of that item type.
func buildPlainSlot(tblDesc *common.TableDesc, itemType common.PlainItemTypeE) (plain PlainSlot, found bool, err error) {
	n := tblDesc.CountStructuredItemsByType(itemType)
	if n == 0 {
		return
	}
	found = true
	plain.ItemType = itemType
	plain.Names = make([]naming.StylableName, 0, n)
	plain.ColumnTypes = make([]canonicaltypes.PrimitiveAstNodeI, 0, n)
	for i, t := range tblDesc.PlainValuesItemTypes {
		if t != itemType {
			continue
		}
		ct := tblDesc.PlainValuesTypes[i]
		if ct == nil || !ct.IsValid() {
			err = eb.Build().Int("column", i).Errorf("invalid canonical type")
			return
		}
		plain.Names = append(plain.Names, tblDesc.PlainValuesNames[i])
		plain.ColumnTypes = append(plain.ColumnTypes, ct)
	}
	plain.Group, plain.ColumnOrder = runtime.PlainGroupOf(plain.ColumnTypes)
	return
}

// Ambiguous returns the signatures carried by two or more slots, sorted. They
// are exactly the signatures SD5's dispatch has to resolve.
func (inst *SlotTable) Ambiguous() (sigs []string) {
	sigs = make([]string, 0, len(inst.BySignature))
	for sig, ordinals := range inst.BySignature {
		if len(ordinals) > 1 {
			sigs = append(sigs, sig)
		}
	}
	slices.Sort(sigs)
	return
}

// ChannelSpec maps a membership channel to the single common.MembershipSpecE
// bit a section must declare to accept it (ADR-0210 SD5 step 1). All eight
// channels are covered: a lossless form restores carriage, so no channel may
// be left without a spec.
func ChannelSpec(ch mappingplan.MembershipChannel) (spec common.MembershipSpecE, err error) {
	switch ch {
	case mappingplan.MembershipChannelLowCardRef:
		spec = common.MembershipSpecLowCardRef
	case mappingplan.MembershipChannelLowCardVerbatim:
		spec = common.MembershipSpecLowCardVerbatim
	case mappingplan.MembershipChannelHighCardRef:
		spec = common.MembershipSpecHighCardRef
	case mappingplan.MembershipChannelHighCardVerbatim:
		spec = common.MembershipSpecHighCardVerbatim
	case mappingplan.MembershipChannelMixedLowCardRef:
		spec = common.MembershipSpecMixedLowCardRefHighCardParameters
	case mappingplan.MembershipChannelMixedLowCardVerbatim:
		spec = common.MembershipSpecMixedLowCardVerbatimHighCardParameters
	case mappingplan.MembershipChannelLowCardRefParametrized:
		spec = common.MembershipSpecLowCardRefParametrized
	case mappingplan.MembershipChannelHighCardRefParametrized:
		spec = common.MembershipSpecHighCardRefParametrized
	default:
		err = eb.Build().Uint8("channel", uint8(ch)).Errorf("unknown membership channel")
	}
	return
}

// AllMembershipChannels lists the eight channels in ordinal order. The wire
// carries the ordinal (SD4), so this is also the order channel ordinals sort
// in.
var AllMembershipChannels = []mappingplan.MembershipChannel{
	mappingplan.MembershipChannelLowCardRef,
	mappingplan.MembershipChannelLowCardVerbatim,
	mappingplan.MembershipChannelHighCardRef,
	mappingplan.MembershipChannelHighCardVerbatim,
	mappingplan.MembershipChannelMixedLowCardRef,
	mappingplan.MembershipChannelMixedLowCardVerbatim,
	mappingplan.MembershipChannelLowCardRefParametrized,
	mappingplan.MembershipChannelHighCardRefParametrized,
}

// SpecAcceptsChannel reports whether a section declaring spec can store a
// membership arriving on ch. An unknown channel is accepted by nothing.
func SpecAcceptsChannel(spec common.MembershipSpecE, ch mappingplan.MembershipChannel) (accepts bool) {
	bit, err := ChannelSpec(ch)
	if err != nil {
		return false
	}
	return spec&bit != 0
}

// SpecChannels is the inverse of ChannelSpec over a whole spec: the channels a
// section declaring spec accepts, in ordinal order. MembershipSpecNone yields
// an empty slice, never nil.
func SpecChannels(spec common.MembershipSpecE) (chs []mappingplan.MembershipChannel) {
	chs = make([]mappingplan.MembershipChannel, 0, len(AllMembershipChannels))
	for _, ch := range AllMembershipChannels {
		if SpecAcceptsChannel(spec, ch) {
			chs = append(chs, ch)
		}
	}
	return
}
