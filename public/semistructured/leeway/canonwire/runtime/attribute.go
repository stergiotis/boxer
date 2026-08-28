package runtime

import (
	"bytes"
	"cmp"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// AttributeKey is the cardinality prefix of an attribute's canonical sort key
// (ADR-0210 SD3): the membership count, then each value column's cardinality
// in key order. It is what CompareAttributes looks at before it compares any
// bytes, and it is not itself on the wire — the lengths it records are already
// implied by the encoded item.
//
// Cardinality convention (the ADR says "value cardinality / container
// length"): 1 for a present scalar, 0 for a null one, and the element count
// for a container — the array length of an `h` column, the set size of an `m`
// one. It is a function of the value's form alone, and DeriveCardinality is
// the one place it is computed. A value-less slot's attribute has no value
// columns and so an empty Cards.
type AttributeKey struct {
	// MembershipCount is the total number of memberships the attribute
	// carries, summed across its membership groups — a slot of k co-sections
	// has one group per section. Two attributes that spread the same number of
	// memberships differently across their groups compare equal here and are
	// separated by the memberships' encoded bytes, which carry the grouping.
	MembershipCount uint32
	// Cards holds one entry per value column of the slot's signature, in key
	// order. Once the attribute is held by a SlotWriter, Cards is a view into
	// that entity's cardinality arena and is valid until the entity is
	// flushed.
	Cards []uint64
}

// Attr is one finished attribute: its encoded item plus the ranges the
// canonical order compares, so a sort never re-parses CBOR.
//
// Item is the whole `[memberships, v_1, …, v_n(, disc)]` array item; Memb is
// the memberships array item inside it and Vals the rest — the values, and the
// SD5 discriminator when one is present. Memb and Vals are sub-slices of Item
// and together with the outer array head account for all of it.
//
// Ownership: the Attr returned by AttributeWriter.End views that writer's
// scratch buffer and is valid only until the next Begin. SlotWriter.Add copies
// the bytes into the entity's arena, and the copy is valid until the entity is
// flushed.
type Attr struct {
	Key  AttributeKey
	Item []byte
	Memb []byte
	Vals []byte

	// Offsets into the entity arena, for an Attr held by a SlotWriter. The
	// arena is appended to as later attributes arrive and may be reallocated,
	// so the three views above are re-bound from these just before the sort.
	start     int32
	membStart int32
	valsStart int32
	end       int32
	cardStart int32
	cardLen   int32
}

// CompareAttributes orders two attributes of the same slot as ADR-0210 SD3
// requires: by membership count, then by the value cardinalities
// lexicographically, then by the memberships' encoded bytes, then by the
// values' encoded bytes. Equal under all four means duplicate attributes,
// which stay adjacent and are both kept.
//
// The cardinalities lead so that the cheap comparison decides most pairs, and
// so that the order is stated in terms a reader of the ADR can check without
// decoding; the byte comparisons only break the ties the cardinalities leave.
func CompareAttributes(a *Attr, b *Attr) (r int) {
	if r = cmp.Compare(a.Key.MembershipCount, b.Key.MembershipCount); r != 0 {
		return
	}
	if r = slices.Compare(a.Key.Cards, b.Key.Cards); r != 0 {
		return
	}
	if r = bytes.Compare(a.Memb, b.Memb); r != 0 {
		return
	}
	return bytes.Compare(a.Vals, b.Vals)
}

// DeriveCardinality reports the ADR-0210 SD3 cardinality of one encoded value:
// the element count of an array or of a tag-258 set, 0 for `null`, 1 for any
// other item. Only the heads that decide the answer are read; the value's
// remaining bytes, and anything after it in item, are ignored.
//
// It is the single definition of the rule. AttributeWriter.EndValue derives a
// column's cardinality from the bytes it has just written, and the table-free
// checker derives the same number from the bytes it is about to walk, so the
// order the two produce cannot drift from the order the other verifies.
func DeriveCardinality(item []byte) (card uint64, err error) {
	if len(item) > 0 && item[0] == SimpleNull {
		return 0, nil
	}
	r := NewCborReader(item)
	mt, arg := r.ReadHead()
	if err = r.Err(); err != nil {
		return 0, err
	}
	switch mt {
	case MajorArray:
		return arg, nil
	case MajorTag:
		if arg != TagSet {
			// A temporal or a network value: one tagged item, cardinality one.
			return 1, nil
		}
		n := r.ReadArrayHead()
		if err = r.Err(); err != nil {
			return 0, err
		}
		return uint64(n), nil
	}
	return 1, nil
}

// AttributeWriter assembles one attribute item for a generated per-table
// encoder. The memberships may arrive in any order — each is encoded straight
// into a scratch buffer and the recorded ranges are sorted at End — while the
// values are written in key order, one BeginValue/EndValue pair per column of
// the slot's signature.
//
// A slot of k co-sections carries k membership groups, one per section in
// signature order (ADR-0210 SD3): the memberships element is then an array of
// k arrays, each sorted on its own. For k == 1 it stays the flat array, so a
// standalone section's bytes are what they always were.
//
// One writer per slot per encoding goroutine; it is reused across attributes
// and holds its scratch buffers as fields, so it must not be copied once
// constructed. Not goroutine-safe.
type AttributeWriter struct {
	nCols   int
	nGroups int

	mw   *CborWriter // membership scratch writer, bound to mbuf
	vw   *CborWriter // value scratch writer, bound to vbuf
	aw   *CborWriter // assembly writer, bound to abuf
	mbuf bytes.Buffer
	vbuf bytes.Buffer
	abuf bytes.Buffer

	// ranges holds one list of encoded-membership extents per group, in
	// signature order; each list is sorted on its own at End.
	ranges [][]scratchRange
	cards  []uint64

	valStart int
	nValues  int
	inValue  bool
	hasDisc  bool
	disc     uint64
	begun    bool
	err      error
}

// scratchRange is one encoded element's extent in a scratch buffer.
type scratchRange struct {
	start int
	end   int
}

// NewAttributeWriter returns a writer for attributes of a slot with nCols
// value columns spread over nGroups membership groups — one group per section
// of the slot, so a standalone section passes 1 and a co-section group of k
// sections passes k. A value-less slot passes nCols 0.
func NewAttributeWriter(nCols int, nGroups int) (inst *AttributeWriter, err error) {
	if nCols < 0 {
		err = eb.Build().Int("nCols", nCols).Errorf("negative column count")
		return
	}
	if nGroups < 1 {
		err = eb.Build().Int("nGroups", nGroups).Errorf("an attribute carries at least one membership group")
		return
	}
	inst = &AttributeWriter{
		nCols:   nCols,
		nGroups: nGroups,
		ranges:  make([][]scratchRange, nGroups),
		cards:   make([]uint64, nCols),
	}
	for g := range inst.ranges {
		inst.ranges[g] = make([]scratchRange, 0, 4)
	}
	if inst.mw, err = NewCborWriter(&inst.mbuf); err != nil {
		inst = nil
		return
	}
	if inst.vw, err = NewCborWriter(&inst.vbuf); err != nil {
		inst = nil
		return
	}
	if inst.aw, err = NewCborWriter(&inst.abuf); err != nil {
		inst = nil
		return
	}
	return
}

// Begin starts a new attribute, discarding whatever the previous one left in
// the scratch buffers — which is why the Attr that End returned is only valid
// until here.
func (inst *AttributeWriter) Begin() {
	inst.mbuf.Reset()
	inst.vbuf.Reset()
	inst.abuf.Reset()
	inst.mw.Reset(&inst.mbuf)
	inst.vw.Reset(&inst.vbuf)
	inst.aw.Reset(&inst.abuf)
	for g := range inst.ranges {
		inst.ranges[g] = inst.ranges[g][:0]
	}
	clear(inst.cards)
	inst.valStart = 0
	inst.nValues = 0
	inst.inValue = false
	inst.hasDisc = false
	inst.disc = 0
	inst.begun = true
	inst.err = nil
}

// Err returns the first error since Begin.
func (inst *AttributeWriter) Err() error { return inst.err }

func (inst *AttributeWriter) fail(err error) {
	if inst.err == nil {
		inst.err = err
	}
}

// Membership records one membership of the slot's group-th section, in
// signature order. Order does not matter: End sorts each group's encoded items
// bytewise (SD3) and keeps duplicates. verbatim and params are read here and
// need not outlive the call.
func (inst *AttributeWriter) Membership(group int, ch mappingplan.MembershipChannel, ref uint64, verbatim []byte, params []byte) {
	if inst.err != nil {
		return
	}
	if !inst.begun {
		inst.fail(eh.Errorf("membership added outside an attribute"))
		return
	}
	if inst.inValue {
		inst.fail(eh.Errorf("membership added inside a value"))
		return
	}
	if group < 0 || group >= inst.nGroups {
		inst.fail(eb.Build().Int("group", group).Int("nGroups", inst.nGroups).Errorf("membership added to a group outside the slot"))
		return
	}
	start := inst.mbuf.Len()
	inst.mw.WriteMembership(ch, ref, verbatim, params)
	if err := inst.mw.Err(); err != nil {
		inst.fail(err)
		return
	}
	inst.ranges[group] = append(inst.ranges[group], scratchRange{start: start, end: inst.mbuf.Len()})
}

// BeginValue opens the next value column, in key order, and returns the writer
// its value must be written with — a bare scalar, a definite `h` array, a
// tag-258 set, whatever the column's canonical type calls for (SD3). EndValue
// closes it.
//
// The returned writer is the attribute's value scratch; do not hold it across
// EndValue.
func (inst *AttributeWriter) BeginValue() (cw *CborWriter) {
	cw = inst.vw
	if inst.err != nil {
		return
	}
	if !inst.begun {
		inst.fail(eh.Errorf("value written outside an attribute"))
		return
	}
	if inst.inValue {
		inst.fail(eh.Errorf("value opened while another is still open"))
		return
	}
	if inst.nValues >= inst.nCols {
		inst.fail(eb.Build().Int("nCols", inst.nCols).Errorf("more values than the slot has columns"))
		return
	}
	inst.inValue = true
	inst.valStart = inst.vbuf.Len()
	return
}

// EndValue closes the value opened by BeginValue, derives the column's
// cardinality from the bytes just written (ADR-0210 SD3, through
// DeriveCardinality) and advances to the next column. The caller never states
// a cardinality: it is a function of the value's form, and reading it back off
// the bytes is what keeps the encoder and the table-free checker in agreement.
func (inst *AttributeWriter) EndValue() {
	if inst.err != nil {
		return
	}
	if !inst.inValue {
		inst.fail(eh.Errorf("no value is open"))
		return
	}
	if err := inst.vw.Err(); err != nil {
		inst.fail(err)
		return
	}
	card, err := DeriveCardinality(inst.vbuf.Bytes()[inst.valStart:])
	if err != nil {
		inst.fail(eb.Build().Int("column", inst.nValues).Errorf("unable to derive the value's cardinality: %w", err))
		return
	}
	inst.cards[inst.nValues] = card
	inst.inValue = false
	inst.nValues++
}

// Discriminator sets the optional SD5 discriminator, a small opaque integer a
// TaggerI produced for an attribute whose signature is ambiguous in the source
// table. It rides as a trailing element of the attribute item.
func (inst *AttributeWriter) Discriminator(d uint64) {
	if inst.err != nil {
		return
	}
	inst.hasDisc = true
	inst.disc = d
}

// End finishes the attribute and returns it.
//
// The item is `[memberships, v_1, …, v_n(, disc)]`, with each membership group
// sorted bytewise and duplicates kept — one flat array for a standalone
// section, an array of k arrays for a co-section group of k. attr's byte views
// point into this writer's scratch and are valid until the next Begin;
// SlotWriter.Add copies them into the entity arena.
func (inst *AttributeWriter) End() (attr Attr, err error) {
	if inst.err != nil {
		err = inst.err
		return
	}
	if !inst.begun {
		err = eh.Errorf("no attribute is open")
		return
	}
	if inst.inValue {
		err = eh.Errorf("a value is still open")
		return
	}
	if inst.nValues != inst.nCols {
		err = eb.Build().Int("written", inst.nValues).Int("nCols", inst.nCols).Errorf("attribute does not carry one value per column")
		return
	}
	inst.begun = false

	mbuf := inst.mbuf.Bytes()
	total := 0
	for g := range inst.ranges {
		rs := inst.ranges[g]
		slices.SortFunc(rs, func(a scratchRange, b scratchRange) int {
			return bytes.Compare(mbuf[a.start:a.end], mbuf[b.start:b.end])
		})
		total += len(rs)
	}

	nElems := 1 + inst.nCols
	if inst.hasDisc {
		nElems++
	}
	aw := inst.aw
	aw.ArrayHead(nElems)
	headLen := inst.abuf.Len()
	if inst.nGroups == 1 {
		// A standalone section: the memberships element is the flat array.
		aw.ArrayHead(len(inst.ranges[0]))
		for _, r := range inst.ranges[0] {
			aw.Write(mbuf[r.start:r.end])
		}
	} else {
		aw.ArrayHead(inst.nGroups)
		for g := range inst.ranges {
			aw.ArrayHead(len(inst.ranges[g]))
			for _, r := range inst.ranges[g] {
				aw.Write(mbuf[r.start:r.end])
			}
		}
	}
	membEnd := inst.abuf.Len()
	aw.Write(inst.vbuf.Bytes())
	if inst.hasDisc {
		aw.WriteUint(inst.disc)
	}
	if err = aw.Err(); err != nil {
		inst.fail(err)
		return
	}
	item := inst.abuf.Bytes()
	attr = Attr{
		Key: AttributeKey{
			MembershipCount: uint32(total),
			Cards:           inst.cards,
		},
		Item: item,
		Memb: item[headLen:membEnd],
		Vals: item[membEnd:],
	}

	return
}
