package runtime

import (
	"bytes"
	"cmp"
	"io"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// Version is the leeway canonical wire's form version (ADR-0210 SD1). It is
// the first element of every entity item, and a decoder refuses a version it
// does not implement.
//
// Any change to the form — the slot keys of SD2, the attribute and value forms
// of SD3, the membership form of SD4, the discriminator of SD5, or SD6's
// generated surface where it moves bytes — bumps this constant. It is declared
// in source rather than derived, so bumping it is a deliberate edit that shows
// up in the same commit as the goldens it moves.
const Version uint64 = 1

// SlotWriter accumulates the attributes of one slot within the entity being
// written. It is obtained from EntityWriter.Slot and lives as long as the
// EntityWriter, its attribute list reset per entity.
type SlotWriter struct {
	signature string
	ent       *EntityWriter
	attrs     []Attr
	// The shape of the entity's first attribute in this slot; every later one
	// must match (SD5: the discriminator is uniform per slot, the co-section
	// count is a property of the slot).
	nElems  int32
	nGroups int32
}

// Signature returns the slot's key.
func (inst *SlotWriter) Signature() string { return inst.signature }

// Len returns how many attributes the slot has taken for this entity.
func (inst *SlotWriter) Len() int { return len(inst.attrs) }

// Add takes a finished attribute, copying its bytes and cardinalities into the
// entity's arenas. attr may therefore be the Attr an AttributeWriter is about
// to overwrite on its next Begin. An attribute whose shape differs from the
// slot's first — a discriminator where the others carry none, a different
// membership-group count — is refused: the writer must not assemble an item
// VerifyCanonical rejects.
func (inst *SlotWriter) Add(attr Attr) {
	e := inst.ent
	if e.err != nil {
		return
	}
	if len(inst.attrs) == 0 {
		inst.nElems = attr.nElems
		inst.nGroups = attr.nGroups
	} else if attr.nElems != inst.nElems || attr.nGroups != inst.nGroups {
		e.fail(eb.Build().Str("signature", inst.signature).Int32("elements", attr.nElems).Int32("want", inst.nElems).Int32("groups", attr.nGroups).Int32("wantGroups", inst.nGroups).Errorf("attributes of one slot differ in shape; the discriminator is uniform per slot and the co-section count is fixed: %w", ErrNotCanonical))
		return
	}
	start := int32(len(e.arena))
	e.arena = append(e.arena, attr.Item...)
	end := int32(len(e.arena))
	valsStart := end - int32(len(attr.Vals))
	membStart := valsStart - int32(len(attr.Memb))
	cardStart := int32(len(e.cardArena))
	e.cardArena = append(e.cardArena, attr.Key.Cards...)
	inst.attrs = append(inst.attrs, Attr{
		Key:       AttributeKey{MembershipCount: attr.Key.MembershipCount},
		start:     start,
		membStart: membStart,
		valsStart: valsStart,
		end:       end,
		cardStart: cardStart,
		cardLen:   int32(len(attr.Key.Cards)),
	})
}

// bind re-derives the byte and cardinality views of every held attribute from
// their arena offsets. The arenas are appended to as attributes arrive and may
// be reallocated, so the views are only materialised here, once, just before
// the sort that reads them.
func (inst *SlotWriter) bind(arena []byte, cardArena []uint64) {
	for i := range inst.attrs {
		a := &inst.attrs[i]
		a.Item = arena[a.start:a.end]
		a.Memb = arena[a.membStart:a.valsStart]
		a.Vals = arena[a.valsStart:a.end]
		a.Key.Cards = cardArena[a.cardStart : a.cardStart+a.cardLen]
	}
}

// plainSpan is one plain section's encoded array in the plain scratch buffer.
type plainSpan struct {
	itemType common.PlainItemTypeE
	start    int
	end      int
}

// EntityWriter assembles one entity item of the leeway canonical wire.
//
// The item is the 3-element array of ADR-0210 SD1:
//
//	[ version:uint, plains:map, tagged:map ]
//
// plains is keyed by PlainItemTypeE ordinal (CBOR uint) and tagged by CT
// signature (CBOR text), both sorted as RFC 8949 §4.2.3 requires. A slot with
// no attributes is omitted rather than written empty.
//
// The writer holds attribute bytes back only until Flush — the sort of SD3
// needs the whole entity, and nothing more. Allocations are amortised across
// entities: one byte arena and one cardinality arena per entity, both
// re-sliced rather than reallocated, and the per-slot attribute lists reused.
//
// It holds its scratch buffers as fields and must not be copied once
// constructed. Not goroutine-safe; one writer per encoding goroutine.
type EntityWriter struct {
	cw *CborWriter // bound to the destination at Flush
	pw *CborWriter // plain scratch writer, bound to pbuf

	pbuf   bytes.Buffer
	plains []plainSpan

	arena     []byte
	cardArena []uint64

	slots    map[string]*SlotWriter
	nonEmpty []*SlotWriter // scratch: the slots that carry attributes, per flush
	inPlain  bool
	begun    bool
	err      error
}

// NewEntityWriter returns a writer with its scratch buffers bound.
func NewEntityWriter() (inst *EntityWriter, err error) {
	inst = &EntityWriter{
		plains:    make([]plainSpan, 0, len(common.AllPlainItemTypes)),
		arena:     make([]byte, 0, 4096),
		cardArena: make([]uint64, 0, 64),
		slots:     make(map[string]*SlotWriter, 8),
		nonEmpty:  make([]*SlotWriter, 0, 8),
	}
	if inst.cw, err = NewCborWriter(io.Discard); err != nil {
		inst = nil
		return
	}
	if inst.pw, err = NewCborWriter(&inst.pbuf); err != nil {
		inst = nil
		return
	}
	return
}

// Begin starts a new entity, discarding anything a previous one left behind.
// Flush already resets, so Begin is only required before the first entity —
// calling it before each is harmless and is the clearer shape for a generated
// encoder.
func (inst *EntityWriter) Begin() {
	inst.pbuf.Reset()
	inst.pw.Reset(&inst.pbuf)
	inst.plains = inst.plains[:0]
	inst.arena = inst.arena[:0]
	inst.cardArena = inst.cardArena[:0]
	for _, s := range inst.slots {
		s.attrs = s.attrs[:0]
	}
	inst.inPlain = false
	inst.begun = true
	inst.err = nil
}

// Err returns the first error since Begin.
func (inst *EntityWriter) Err() error { return inst.err }

func (inst *EntityWriter) fail(err error) {
	if inst.err == nil {
		inst.err = err
	}
}

// BeginPlain opens the plain section of the given item type and returns the
// writer its nCols column values must be written with, in the key order
// PlainGroupOf produced. The array head is written here; the caller writes the
// values and closes with EndPlain.
//
// An item type may be opened at most once per entity — it is a map key.
func (inst *EntityWriter) BeginPlain(itemType common.PlainItemTypeE, nCols int) (cw *CborWriter) {
	cw = inst.pw
	if inst.err != nil {
		return
	}
	if !inst.begun {
		inst.fail(eh.Errorf("plain section written outside an entity"))
		return
	}
	if inst.inPlain {
		inst.fail(eh.Errorf("plain section opened while another is still open"))
		return
	}
	if itemType == common.PlainItemTypeNone || itemType >= common.MaxPlainItemTypeExcl {
		inst.fail(eb.Build().Stringer("plainItemType", itemType).Errorf("not a writable plain item type"))
		return
	}
	for i := range inst.plains {
		if inst.plains[i].itemType == itemType {
			inst.fail(eb.Build().Stringer("plainItemType", itemType).Errorf("plain item type already written for this entity"))
			return
		}
	}
	if nCols < 0 {
		inst.fail(eb.Build().Int("nCols", nCols).Errorf("negative column count"))
		return
	}
	inst.plains = append(inst.plains, plainSpan{itemType: itemType, start: inst.pbuf.Len()})
	inst.inPlain = true
	cw.ArrayHead(nCols)
	return
}

// EndPlain closes the plain section opened by BeginPlain.
func (inst *EntityWriter) EndPlain() {
	if inst.err != nil {
		return
	}
	if !inst.inPlain {
		inst.fail(eh.Errorf("no plain section is open"))
		return
	}
	if err := inst.pw.Err(); err != nil {
		inst.fail(err)
		return
	}
	inst.plains[len(inst.plains)-1].end = inst.pbuf.Len()
	inst.inPlain = false
}

// Slot returns the writer for the slot keyed by signature, creating it on
// first use. The returned writer stays valid for the life of the EntityWriter;
// its attribute list is reset per entity.
func (inst *EntityWriter) Slot(signature string) (sw *SlotWriter) {
	sw = inst.slots[signature]
	if sw == nil {
		sw = &SlotWriter{signature: signature, ent: inst, attrs: make([]Attr, 0, 4)}
		inst.slots[signature] = sw
	}
	return
}

// Flush writes the entity item to w and resets for the next one. The reset
// happens whether or not the write succeeded, so a failed entity cannot leak
// into its successor.
func (inst *EntityWriter) Flush(w io.Writer) (err error) {
	defer inst.Begin()
	if inst.err != nil {
		err = inst.err
		return
	}
	if inst.inPlain {
		err = eh.Errorf("a plain section is still open")
		return
	}

	inst.nonEmpty = inst.nonEmpty[:0]
	for _, s := range inst.slots {
		if len(s.attrs) == 0 {
			continue
		}
		s.bind(inst.arena, inst.cardArena)
		slices.SortFunc(s.attrs, func(a Attr, b Attr) int { return CompareAttributes(&a, &b) })
		inst.nonEmpty = append(inst.nonEmpty, s)
	}
	// RFC 8949 §4.2.3: map keys sort by their *encoded* bytes. A text key's
	// encoding starts with the length, so shorter keys sort first and equal
	// lengths fall back to a bytewise comparison of the content — never a
	// plain lexicographic order over the strings.
	slices.SortFunc(inst.nonEmpty, func(a *SlotWriter, b *SlotWriter) int {
		return compareTextKeys(a.signature, b.signature)
	})
	// Plain keys are uints; their encodings sort in the same order as the
	// ordinals, and AllPlainItemTypes is already ordinal-ordered.
	slices.SortFunc(inst.plains, func(a plainSpan, b plainSpan) int {
		return cmp.Compare(a.itemType, b.itemType)
	})

	cw := inst.cw
	cw.Reset(w)
	cw.ArrayHead(3)
	cw.WriteUint(Version)

	pbuf := inst.pbuf.Bytes()
	cw.MapHead(len(inst.plains))
	for i := range inst.plains {
		p := &inst.plains[i]
		cw.WriteUint(uint64(p.itemType))
		cw.Write(pbuf[p.start:p.end])
	}

	cw.MapHead(len(inst.nonEmpty))
	for _, s := range inst.nonEmpty {
		cw.WriteTextString(s.signature)
		cw.ArrayHead(len(s.attrs))
		for i := range s.attrs {
			cw.Write(s.attrs[i].Item)
		}
	}
	err = cw.Err()
	return
}

// compareTextKeys orders two CBOR text-string map keys by their encoded bytes
// (RFC 8949 §4.2.3): length first, then the content bytewise. The head byte
// carries the length, so a shorter string always sorts before a longer one
// whatever the content.
func compareTextKeys(a string, b string) (r int) {
	if r = cmp.Compare(len(a), len(b)); r != 0 {
		return
	}
	return strings.Compare(a, b)
}
