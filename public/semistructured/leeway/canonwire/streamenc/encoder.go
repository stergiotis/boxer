package streamenc

import (
	"bytes"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/boxer/public/unsafeperf"
)

// Encoder writes every entity a streamreadaccess.Driver drives through it as
// one canonical-wire entity item (ADR-0210 SD1), appended to an in-memory
// CBOR sequence. It is a SinkI with the MembershipSinkI, ArrowValueSinkI and
// CoSectionTagSinkI capabilities and must be driven with the Arrow lane — the
// text lane is refused, as a formatted value is not the exact value.
//
// Per attribute the encoder holds the column views and the memberships the
// driver pushes (values arrive before tags) and, at EndTaggedValue, writes
// them through the slot's runtime.AttributeWriter in key order; at EndEntity
// the runtime.EntityWriter sorts the slots and attributes and flushes the
// item. The bytes of one batch are held until the next BeginBatch.
//
// Not goroutine-safe; one Encoder per driving goroutine.
type Encoder struct {
	ew   *runtime.EntityWriter
	sets *runtime.SetWriter

	slots    []*slotBinding
	sections map[naming.StylableName]*sectionBinding
	plains   map[common.PlainItemTypeE]*plainBinding
	// coGroupColSections is, per co-section group, the owning section of
	// every merged value column in the driver's merged order — the IR walk
	// the driver's own merge performs (the canonform.Encoder construction).
	coGroupColSections map[naming.Key][]naming.StylableName

	// batch
	buf     bytes.Buffer
	offsets []int // entity i is buf[offsets[i]:offsets[i+1]]

	// section
	curSlot    *slotBinding
	curSection *sectionBinding // the standalone section, or the co-group's first
	curPlain   *plainBinding
	inCoGroup  bool
	coGroupKey naming.Key
	curGroup   int // membership group of the tags being pushed

	// attribute / plain section
	cols    []colView
	members []memberRec
	curCol  *colView
	ordered []int // key position → index into cols

	err error
}

var _ streamreadaccess.SinkI = (*Encoder)(nil)
var _ streamreadaccess.MembershipSinkI = (*Encoder)(nil)
var _ streamreadaccess.ArrowValueSinkI = (*Encoder)(nil)
var _ streamreadaccess.CoSectionTagSinkI = (*Encoder)(nil)

// slotBinding is one wire slot with the writers bound to it.
type slotBinding struct {
	sig   string
	nCols int
	aw    *runtime.AttributeWriter
	sw    *runtime.SlotWriter
}

// sectionBinding places one tagged section inside its slot: which membership
// group its tags go to (signature order) and, per value column name, the
// absolute key position of that column in the slot's signature.
type sectionBinding struct {
	slot   *slotBinding
	group  int
	keyPos map[naming.StylableName]int
}

// plainBinding is one plain item type's key order.
type plainBinding struct {
	itemType common.PlainItemTypeE
	nCols    int
	keyPos   map[naming.StylableName]int
}

type viewKindE uint8

const (
	viewKindNone viewKindE = iota
	viewKindScalar
	viewKindArray
	viewKindSet
)

// view is a zero-copy reference into the retained RecordBatch: one element
// for a scalar, a range for a container.
type view struct {
	kind  viewKindE
	arr   arrow.Array
	idx   int
	start int
	end   int
}

type colView struct {
	keyPos int
	ct     canonicaltypes.PrimitiveAstNodeI
	v      view
}

type memberRec struct {
	group    int
	ch       mappingplan.MembershipChannel
	ref      uint64
	verbatim string
	params   string
}

// NewEncoder prepares an encoder for the table the driver will read. tblDesc
// is the description the slot table is computed from (ADR-0210 SD2) and ir
// the same IR the driver is constructed with; the IR is consulted once, to
// learn which section owns each merged value column of a co-section group.
func NewEncoder(tblDesc *common.TableDesc, ir *common.IntermediateTableRepresentation) (inst *Encoder, err error) {
	if tblDesc == nil {
		err = eh.Errorf("streamenc: the encoder needs the table description")
		return
	}
	if ir == nil {
		err = eh.Errorf("streamenc: the encoder needs the table's intermediate representation")
		return
	}
	var tbl canonwire.SlotTable
	tbl, err = canonwire.BuildSlotTable(tblDesc)
	if err != nil {
		err = eh.Errorf("streamenc: unable to build the slot table: %w", err)
		return
	}
	inst = &Encoder{
		slots:              make([]*slotBinding, 0, len(tbl.Slots)),
		sections:           make(map[naming.StylableName]*sectionBinding, len(tblDesc.TaggedValuesSections)),
		plains:             make(map[common.PlainItemTypeE]*plainBinding, len(tbl.Plains)),
		coGroupColSections: make(map[naming.Key][]naming.StylableName, 4),
	}
	if inst.ew, err = runtime.NewEntityWriter(); err != nil {
		inst = nil
		return
	}
	if inst.sets, err = runtime.NewSetWriter(); err != nil {
		inst = nil
		return
	}
	for i := range tbl.Slots {
		slot := &tbl.Slots[i]
		sb := &slotBinding{sig: slot.Signature}
		off := 0
		for g := range slot.Sections {
			sec := &slot.Sections[g]
			names := tblDesc.TaggedValuesSections[sec.SectionIdx].ValueColumnNames
			if len(names) != len(sec.ColumnOrder) {
				err = eb.Build().Stringer("section", sec.Name).Int("names", len(names)).Int("columns", len(sec.ColumnOrder)).Errorf("streamenc: section names and types disagree in count")
				inst = nil
				return
			}
			binding := &sectionBinding{slot: sb, group: g, keyPos: make(map[naming.StylableName]int, len(names))}
			for k, declared := range sec.ColumnOrder {
				binding.keyPos[names[declared]] = off + k
			}
			if _, dup := inst.sections[sec.Name]; dup {
				err = eb.Build().Stringer("section", sec.Name).Errorf("streamenc: section name is not unique in the table")
				inst = nil
				return
			}
			inst.sections[sec.Name] = binding
			off += len(sec.ColumnOrder)
		}
		sb.nCols = off
		if sb.aw, err = runtime.NewAttributeWriter(off, len(slot.Sections)); err != nil {
			err = eb.Build().Str("slot", slot.Signature).Errorf("streamenc: unable to create the attribute writer: %w", err)
			inst = nil
			return
		}
		sb.sw = inst.ew.Slot(slot.Signature)
		inst.slots = append(inst.slots, sb)
	}
	for i := range tbl.Plains {
		p := &tbl.Plains[i]
		binding := &plainBinding{itemType: p.ItemType, nCols: len(p.ColumnOrder), keyPos: make(map[naming.StylableName]int, len(p.ColumnOrder))}
		for k, declared := range p.ColumnOrder {
			binding.keyPos[p.Names[declared]] = k
		}
		inst.plains[p.ItemType] = binding
	}
	for _, tv := range ir.TaggedValueDesc {
		if tv == nil || tv.CoSectionGroup == "" {
			continue
		}
		key := tv.CoSectionGroup
		names := inst.coGroupColSections[key]
		for _, cp := range []*common.IntermediateColumnProps{tv.Scalar, tv.NonScalarHomogenousArray, tv.NonScalarSet} {
			if cp == nil {
				continue
			}
			for range cp.Names {
				names = append(names, tv.SectionName)
			}
		}
		inst.coGroupColSections[key] = names
	}
	return
}

// NumEntities is the number of entity items written since the last BeginBatch.
func (inst *Encoder) NumEntities() int {
	if len(inst.offsets) == 0 {
		return 0
	}
	return len(inst.offsets) - 1
}

// Bytes returns the CBOR sequence (RFC 8742) of every entity of the current
// batch, in driving order. The slice aliases the encoder's buffer and is
// valid until the next BeginBatch; copy it to keep it.
func (inst *Encoder) Bytes() []byte { return inst.buf.Bytes() }

// Entity returns the i-th entity item of the current batch. The slice
// aliases the encoder's buffer and is valid until the next BeginBatch; copy
// it to keep it.
func (inst *Encoder) Entity(i int) []byte {
	return inst.buf.Bytes()[inst.offsets[i]:inst.offsets[i+1]]
}

// Err returns the first error the encoder met since the last BeginBatch; the
// driver also surfaces it through the End* return values.
func (inst *Encoder) Err() error { return inst.err }

func (inst *Encoder) fail(err error) {
	if inst.err == nil && err != nil {
		inst.err = err
	}
}

// --- batch / entity ---

func (inst *Encoder) BeginBatch() {
	inst.buf.Reset()
	inst.offsets = append(inst.offsets[:0], 0)
	inst.err = nil
}

func (inst *Encoder) EndBatch() (err error) { return inst.err }

func (inst *Encoder) BeginEntity() {
	inst.ew.Begin()
}

func (inst *Encoder) EndEntity() (err error) {
	if inst.err != nil {
		return inst.err
	}
	if err = inst.ew.Flush(&inst.buf); err != nil {
		inst.fail(eh.Errorf("streamenc: unable to flush the entity: %w", err))
		return inst.err
	}
	inst.offsets = append(inst.offsets, inst.buf.Len())
	return
}

// --- plain sections ---

func (inst *Encoder) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.curSlot = nil
	inst.curSection = nil
	inst.curPlain = inst.plains[itemType]
	inst.cols = inst.cols[:0]
	inst.curCol = nil
	if inst.curPlain == nil {
		inst.fail(eb.Build().Stringer("itemType", itemType).Errorf("streamenc: driven plain item type is not in the table description"))
	}
}

func (inst *Encoder) EndPlainSection() (err error) {
	if inst.err != nil {
		return inst.err
	}
	p := inst.curPlain
	inst.curPlain = nil
	if p == nil {
		return inst.err
	}
	if !inst.orderColumns(p.nCols, "plain section") {
		return inst.err
	}
	cw := inst.ew.BeginPlain(p.itemType, p.nCols)
	for _, ci := range inst.ordered {
		inst.writeView(cw, &inst.cols[ci])
	}
	inst.ew.EndPlain()
	if err = inst.ew.Err(); err != nil {
		inst.fail(err)
	}
	return inst.err
}

func (inst *Encoder) BeginPlainValue()           {}
func (inst *Encoder) EndPlainValue() (err error) { return inst.err }

// --- tagged sections ---

func (inst *Encoder) BeginTaggedSections()           {}
func (inst *Encoder) EndTaggedSections() (err error) { return inst.err }

func (inst *Encoder) BeginCoSectionGroup(name naming.Key) {
	inst.inCoGroup = true
	inst.coGroupKey = name
}

func (inst *Encoder) EndCoSectionGroup() (err error) {
	inst.inCoGroup = false
	inst.coGroupKey = ""
	return inst.err
}

func (inst *Encoder) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, useAspects useaspects.AspectSet, nAttrs int) {
	inst.curPlain = nil
	inst.curSection = inst.sections[name]
	if inst.curSection == nil {
		inst.curSlot = nil
		inst.fail(eb.Build().Stringer("section", name).Errorf("streamenc: driven section is not in the table description"))
		return
	}
	inst.curSlot = inst.curSection.slot
	if inst.inCoGroup {
		owners := inst.coGroupColSections[inst.coGroupKey]
		if len(owners) != len(valueNames) {
			inst.fail(eb.Build().Str("coGroup", string(inst.coGroupKey)).Int("irColumns", len(owners)).Int("drivenColumns", len(valueNames)).Errorf("streamenc: co-section group column count disagrees between the IR and the driver"))
		}
	}
}

func (inst *Encoder) EndSection() (err error) {
	inst.curSlot = nil
	inst.curSection = nil
	return inst.err
}

// --- attributes ---

func (inst *Encoder) BeginTaggedValue() {
	inst.cols = inst.cols[:0]
	inst.members = inst.members[:0]
	inst.curCol = nil
	inst.curGroup = 0
	if inst.curSection != nil {
		inst.curGroup = inst.curSection.group
	}
}

func (inst *Encoder) EndTaggedValue() (err error) {
	if inst.err != nil {
		return inst.err
	}
	sb := inst.curSlot
	if sb == nil {
		return inst.err
	}
	if !inst.orderColumns(sb.nCols, "attribute") {
		return inst.err
	}
	aw := sb.aw
	aw.Begin()
	for i := range inst.members {
		m := &inst.members[i]
		aw.Membership(m.group, m.ch, m.ref, unsafeperf.UnsafeStringToBytes(m.verbatim), unsafeperf.UnsafeStringToBytes(m.params))
	}
	for _, ci := range inst.ordered {
		cw := aw.BeginValue()
		inst.writeView(cw, &inst.cols[ci])
		aw.EndValue()
	}
	if inst.err != nil {
		return inst.err
	}
	attr, aerr := aw.End()
	if aerr != nil {
		inst.fail(eb.Build().Str("slot", sb.sig).Errorf("streamenc: unable to finish the attribute: %w", aerr))
		return inst.err
	}
	sb.sw.Add(attr)
	return inst.err
}

// orderColumns fills inst.ordered with the index into inst.cols of the column
// at every key position 0..nCols-1, refusing a missing or duplicated
// position: the driver must have delivered exactly one view per value column
// of the slot (or plain section).
func (inst *Encoder) orderColumns(nCols int, what string) (ok bool) {
	if len(inst.cols) != nCols {
		inst.fail(eb.Build().Str("in", what).Int("driven", len(inst.cols)).Int("declared", nCols).Errorf("streamenc: driven column count disagrees with the key order"))
		return false
	}
	if cap(inst.ordered) < nCols {
		inst.ordered = make([]int, nCols)
	}
	inst.ordered = inst.ordered[:nCols]
	for k := range inst.ordered {
		inst.ordered[k] = -1
	}
	for ci := range inst.cols {
		pos := inst.cols[ci].keyPos
		if pos < 0 || pos >= nCols || inst.ordered[pos] >= 0 {
			inst.fail(eb.Build().Str("in", what).Int("keyPos", pos).Errorf("streamenc: driven column lands on a key position that is out of range or already taken"))
			return false
		}
		inst.ordered[pos] = ci
	}
	return true
}

// writeView writes one column's value from its Arrow view (ADR-0210 SD3):
// the scalar form, a definite array of element forms in stored order, or a
// tag-258 set of the element forms sorted bytewise with duplicates kept.
func (inst *Encoder) writeView(cw *runtime.CborWriter, c *colView) {
	v := &c.v
	switch v.kind {
	case viewKindScalar:
		if err := writeScalar(cw, v.arr, v.idx, c.ct); err != nil {
			inst.fail(err)
		}
	case viewKindArray:
		elem := scalarOf(c.ct)
		cw.ArrayHead(v.end - v.start)
		for i := v.start; i < v.end; i++ {
			if err := writeScalar(cw, v.arr, i, elem); err != nil {
				inst.fail(err)
				return
			}
		}
	case viewKindSet:
		elem := scalarOf(c.ct)
		s := inst.sets
		s.Begin()
		for i := v.start; i < v.end; i++ {
			if err := writeScalar(s.Elem(), v.arr, i, elem); err != nil {
				inst.fail(err)
				return
			}
			s.EndElem()
		}
		if err := s.Err(); err != nil {
			inst.fail(err)
			return
		}
		s.Flush(cw)
	default:
		inst.fail(errNoView)
	}
}

// --- columns and values ---

func (inst *Encoder) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI, valueSemantics valueaspects.AspectSet) {
	inst.curCol = nil
	if inst.err != nil {
		return
	}
	var pos int
	var known bool
	switch {
	case inst.curPlain != nil:
		pos, known = inst.curPlain.keyPos[name]
	case inst.curSection != nil:
		binding := inst.curSection
		if inst.inCoGroup {
			owners := inst.coGroupColSections[inst.coGroupKey]
			i := len(inst.cols)
			if i >= len(owners) {
				inst.fail(eb.Build().Str("coGroup", string(inst.coGroupKey)).Int("column", i).Errorf("streamenc: more merged columns than the co-section group declares"))
				return
			}
			binding = inst.sections[owners[i]]
			if binding == nil {
				inst.fail(eb.Build().Stringer("section", owners[i]).Errorf("streamenc: co-section owner is not in the table description"))
				return
			}
		}
		pos, known = binding.keyPos[name]
	default:
		inst.fail(eb.Build().Stringer("name", name).Errorf("streamenc: column driven outside a section"))
		return
	}
	if !known {
		inst.fail(eb.Build().Stringer("name", name).Errorf("streamenc: driven column is not in the table description"))
		return
	}
	inst.cols = append(inst.cols, colView{keyPos: pos, ct: canonicalType})
	inst.curCol = &inst.cols[len(inst.cols)-1]
}

func (inst *Encoder) EndColumn() { inst.curCol = nil }

func (inst *Encoder) BeginScalarValue() {
	if inst.curCol != nil {
		inst.curCol.v.kind = viewKindScalar
	}
}
func (inst *Encoder) EndScalarValue() (err error) { return inst.err }

func (inst *Encoder) BeginHomogenousArrayValue(card int) {
	if inst.curCol != nil {
		inst.curCol.v.kind = viewKindArray
	}
}
func (inst *Encoder) EndHomogenousArrayValue() {}

func (inst *Encoder) BeginSetValue(card int) {
	if inst.curCol != nil {
		inst.curCol.v.kind = viewKindSet
	}
}
func (inst *Encoder) EndSetValue() {}

func (inst *Encoder) BeginValueItem(index int) {}
func (inst *Encoder) EndValueItem()            {}

// WriteArrowScalar / WriteArrowRange are the typed lane (ArrowValueSinkI).
func (inst *Encoder) WriteArrowScalar(arr arrow.Array, flatIdx int) {
	if inst.curCol == nil {
		return
	}
	inst.curCol.v.arr = arr
	inst.curCol.v.idx = flatIdx
}

func (inst *Encoder) WriteArrowRange(arr arrow.Array, start int, end int) {
	if inst.curCol == nil {
		return
	}
	inst.curCol.v.arr = arr
	inst.curCol.v.start = start
	inst.curCol.v.end = end
}

// Write and WriteString are the text lane, which the encoder refuses.
func (inst *Encoder) Write(p []byte) (n int, err error) {
	inst.fail(errNoView)
	return len(p), nil
}

func (inst *Encoder) WriteString(s string) (n int, err error) {
	inst.fail(errNoView)
	return len(s), nil
}

// --- memberships ---

func (inst *Encoder) BeginTags(nTags int) {}
func (inst *Encoder) EndTags()            {}

// BeginCoSectionTags routes the tags that follow to the named co-section's
// membership group (ADR-0210 SD3: one group per section, in signature order).
func (inst *Encoder) BeginCoSectionTags(sectionName naming.StylableName, useAspects useaspects.AspectSet) {
	b := inst.sections[sectionName]
	if b == nil || inst.curSlot == nil || b.slot != inst.curSlot {
		inst.fail(eb.Build().Stringer("section", sectionName).Errorf("streamenc: co-section tags for a section outside the current slot"))
		return
	}
	inst.curGroup = b.group
}

func (inst *Encoder) addMember(ch mappingplan.MembershipChannel, ref uint64, verbatim string, params string) {
	inst.members = append(inst.members, memberRec{group: inst.curGroup, ch: ch, ref: ref, verbatim: verbatim, params: params})
}

// AddMembershipRef and the four calls after it are the MembershipSinkI lane.
// They carry the channel whole: cardinality is the lowCard flag, identity is
// the call — so all eight mappingplan channels are reached (ADR-0210 SD4).
// The parametrized channels carry the params blob only; the ref the driver
// passes for them is always zero and is not on the wire.
func (inst *Encoder) AddMembershipRef(lowCard bool, ref uint64) {
	if lowCard {
		inst.addMember(mappingplan.MembershipChannelLowCardRef, ref, "", "")
	} else {
		inst.addMember(mappingplan.MembershipChannelHighCardRef, ref, "", "")
	}
}

func (inst *Encoder) AddMembershipVerbatim(lowCard bool, verbatim string) {
	if lowCard {
		inst.addMember(mappingplan.MembershipChannelLowCardVerbatim, 0, verbatim, "")
	} else {
		inst.addMember(mappingplan.MembershipChannelHighCardVerbatim, 0, verbatim, "")
	}
}

func (inst *Encoder) AddMembershipRefParametrized(lowCard bool, ref uint64, params string) {
	if lowCard {
		inst.addMember(mappingplan.MembershipChannelLowCardRefParametrized, 0, "", params)
	} else {
		inst.addMember(mappingplan.MembershipChannelHighCardRefParametrized, 0, "", params)
	}
}

func (inst *Encoder) AddMembershipMixedLowCardRefHighCardParam(ref uint64, params string) {
	inst.addMember(mappingplan.MembershipChannelMixedLowCardRef, ref, "", params)
}

func (inst *Encoder) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, params string) {
	inst.addMember(mappingplan.MembershipChannelMixedLowCardVerbatim, 0, verbatim, params)
}
