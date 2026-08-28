package runtime

import (
	"bytes"
	"errors"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// ErrNotCanonical is an item that is well-formed CBOR in the deterministic
// subset, and correctly shaped for the leeway canonical wire, but not in the
// order the form fixes: memberships or attributes out of order, map keys not
// strictly increasing, a slot present but empty. It is the class of violation
// a CborReader cannot see — the strictness of the reader is per item, the
// canonical order is a property of a sequence of them.
var ErrNotCanonical = errors.New("item is well-formed but not in canonical order")

// AttributeReader reads one attribute item, `[memberships, v_1, …, v_n(, disc)]`
// (ADR-0210 SD3), as a cursor over a CborReader.
//
// The values are not read here: the generated decoder knows each column's
// canonical type and calls the typed value readers on Reader() itself, which
// is what keeps the runtime free of both reflection and the table description.
// The reader's part is the framing — the outer length, the memberships array,
// the optional discriminator — and the one order the form asserts and a
// CborReader cannot check: the memberships must be non-decreasing bytewise, so
// duplicates are allowed but a permutation is not.
//
// The call order is Begin, then NextGroup followed by its Membership calls
// once per membership group, then the nCols values on Reader(), Discriminator
// when Begin reported one, End. A slot of k co-sections carries k groups in
// signature order (ADR-0210 SD3); a standalone section carries one, and its
// memberships element is the flat array NextGroup then reads.
//
// One reader per decoding goroutine, reused across attributes; not
// goroutine-safe.
type AttributeReader struct {
	r *CborReader

	nCols      int
	nGroups    int
	groupsRead int
	nMemb      int
	membRead   int
	inGroup    bool
	hasDisc    bool
	discRead   bool
	prevMemb   []byte
	haveMemb   bool
	begun      bool
}

// NewAttributeReader returns a reader over r.
func NewAttributeReader(r *CborReader) (inst *AttributeReader) {
	return &AttributeReader{r: r}
}

// Reader returns the underlying CborReader — the one the caller reads the
// attribute's values from, between the last Membership and End.
func (inst *AttributeReader) Reader() *CborReader { return inst.r }

// reset clears the cursor without touching the underlying reader. An attribute
// abandoned mid-read leaves the cursor open, and EntityReader.Reset calls this
// so the next entity does not meet it.
func (inst *AttributeReader) reset() {
	inst.nCols = 0
	inst.nGroups = 0
	inst.groupsRead = 0
	inst.nMemb = 0
	inst.membRead = 0
	inst.inGroup = false
	inst.hasDisc = false
	inst.discRead = false
	inst.prevMemb = nil
	inst.haveMemb = false
	inst.begun = false
}

// Err returns the underlying reader's first error.
func (inst *AttributeReader) Err() error { return inst.r.err }

// Begin reads the attribute's outer array head and, for a slot of more than one
// co-section, the head of the array of membership groups.
//
// nCols is how many value columns the slot's signature has and nGroups how many
// sections it joins — both of which the caller knows and the wire does not
// carry. The outer array is 1+nCols elements, or 2+nCols when the attribute
// carries an SD5 discriminator; hasDiscriminator says which, and any other
// length is refused.
//
// The memberships themselves are read group by group: call NextGroup nGroups
// times, and its Membership calls after each. For nGroups == 1 the flat array
// is the group, and NextGroup reads its head.
func (inst *AttributeReader) Begin(nCols int, nGroups int) (hasDiscriminator bool) {
	r := inst.r
	if r.err != nil {
		return
	}
	if inst.begun {
		r.fail(eb.Build().Int("pos", r.pos).Errorf("attribute opened while another is still open: %w", ErrOutOfRange))
		return
	}
	if nCols < 0 {
		r.fail(eb.Build().Int("nCols", nCols).Errorf("negative column count: %w", ErrOutOfRange))
		return
	}
	if nGroups < 1 {
		r.fail(eb.Build().Int("nGroups", nGroups).Errorf("an attribute carries at least one membership group: %w", ErrOutOfRange))
		return
	}
	n := r.ReadArrayHead()
	if r.err != nil {
		return
	}
	switch n {
	case 1 + nCols:
	case 2 + nCols:
		hasDiscriminator = true
	default:
		r.fail(eb.Build().Int("pos", r.pos).Int("elements", n).Int("nCols", nCols).Errorf("an attribute is 1+nCols elements, or 2+nCols with a discriminator: %w", ErrOutOfRange))
		return
	}
	if nGroups > 1 {
		g := r.ReadArrayHead()
		if r.err != nil {
			return false
		}
		if g != nGroups {
			r.fail(eb.Build().Int("pos", r.pos).Int("groups", g).Int("nGroups", nGroups).Errorf("the attribute does not carry one membership group per co-section: %w", ErrOutOfRange))
			return false
		}
	}
	inst.nCols = nCols
	inst.nGroups = nGroups
	inst.groupsRead = 0
	inst.nMemb = 0
	inst.membRead = 0
	inst.inGroup = false
	inst.hasDisc = hasDiscriminator
	inst.discRead = false
	inst.prevMemb = nil
	inst.haveMemb = false
	inst.begun = true
	return
}

// NextGroup opens the next membership group, in signature order, and returns
// how many memberships it carries — each of which is then read with
// Membership. It is called nGroups times, whatever the counts it reports.
func (inst *AttributeReader) NextGroup() (nMemberships int) {
	r := inst.r
	if r.err != nil {
		return
	}
	if !inst.begun {
		r.fail(eb.Build().Int("pos", r.pos).Errorf("membership group read outside an attribute: %w", ErrOutOfRange))
		return
	}
	if inst.inGroup && inst.membRead != inst.nMemb {
		r.fail(eb.Build().Int("pos", r.pos).Int("read", inst.membRead).Int("nMemberships", inst.nMemb).Errorf("a membership group was left with memberships unread: %w", ErrOutOfRange))
		return
	}
	if inst.groupsRead >= inst.nGroups {
		r.fail(eb.Build().Int("pos", r.pos).Int("nGroups", inst.nGroups).Errorf("more membership groups read than the slot has co-sections: %w", ErrOutOfRange))
		return
	}
	n := r.ReadArrayHead()
	if r.err != nil {
		return 0
	}
	inst.nMemb = n
	inst.membRead = 0
	inst.prevMemb = nil
	inst.haveMemb = false
	inst.inGroup = true
	inst.groupsRead++
	return n
}

// Membership reads the next membership of the group NextGroup opened and checks
// it does not sort before the one read just before it — the order is per group,
// so the first membership of a group is compared against nothing. verbatim and
// params are views into the reader's buffer.
func (inst *AttributeReader) Membership() (ch mappingplan.MembershipChannel, ref uint64, verbatim []byte, params []byte) {
	r := inst.r
	if r.err != nil {
		return
	}
	if !inst.inGroup {
		r.fail(eb.Build().Int("pos", r.pos).Errorf("membership read outside a membership group: %w", ErrOutOfRange))
		return
	}
	if inst.membRead >= inst.nMemb {
		r.fail(eb.Build().Int("pos", r.pos).Int("nMemberships", inst.nMemb).Errorf("more memberships read than the attribute carries: %w", ErrOutOfRange))
		return
	}
	start := r.pos
	ch, ref, verbatim, params = r.ReadMembership()
	if r.err != nil {
		return 0, 0, nil, nil
	}
	item := r.since(start)
	if inst.haveMemb && bytes.Compare(item, inst.prevMemb) < 0 {
		r.fail(eb.Build().Int("pos", r.pos).Int("index", inst.membRead).Errorf("memberships are not sorted bytewise: %w", ErrNotCanonical))
		return 0, 0, nil, nil
	}
	inst.prevMemb = item
	inst.haveMemb = true
	inst.membRead++
	return
}

// Discriminator reads the attribute's trailing SD5 discriminator. It is valid
// only when Begin reported one, and only after the values have been read.
func (inst *AttributeReader) Discriminator() (d uint64) {
	r := inst.r
	if r.err != nil {
		return
	}
	if !inst.begun {
		r.fail(eb.Build().Int("pos", r.pos).Errorf("discriminator read outside an attribute: %w", ErrOutOfRange))
		return
	}
	if !inst.hasDisc {
		r.fail(eb.Build().Int("pos", r.pos).Errorf("attribute carries no discriminator: %w", ErrOutOfRange))
		return
	}
	if inst.discRead {
		r.fail(eb.Build().Int("pos", r.pos).Errorf("discriminator read twice: %w", ErrOutOfRange))
		return
	}
	d = r.ReadUint()
	if r.err != nil {
		return 0
	}
	inst.discRead = true
	return
}

// End closes the attribute. It is bookkeeping: the reader is already positioned
// after the item if the caller read every membership group, every membership,
// every value and the discriminator, and End says so rather than seeking — the
// values are the caller's business and their count is not on the wire.
func (inst *AttributeReader) End() {
	r := inst.r
	if !inst.begun {
		r.fail(eb.Build().Int("pos", r.pos).Errorf("no attribute is open: %w", ErrOutOfRange))
		return
	}
	inst.begun = false
	if r.err != nil {
		return
	}
	if inst.groupsRead != inst.nGroups {
		r.fail(eb.Build().Int("pos", r.pos).Int("read", inst.groupsRead).Int("nGroups", inst.nGroups).Errorf("attribute closed with membership groups left unread: %w", ErrOutOfRange))
		return
	}
	if inst.membRead != inst.nMemb {
		r.fail(eb.Build().Int("pos", r.pos).Int("read", inst.membRead).Int("nMemberships", inst.nMemb).Errorf("attribute closed with memberships left unread: %w", ErrOutOfRange))
		return
	}
	if inst.hasDisc && !inst.discRead {
		r.fail(eb.Build().Int("pos", r.pos).Errorf("attribute closed with its discriminator unread: %w", ErrOutOfRange))
	}
}
