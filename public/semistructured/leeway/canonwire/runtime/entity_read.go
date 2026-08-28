package runtime

import (
	"bytes"
	"cmp"
	"errors"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// ErrVersion is an entity whose leading version integer is not Version. The
// wrapping error carries the number seen, because a decoder that refuses a
// form it does not implement (ADR-0207 SD1) is the one place where the number
// itself is the whole diagnosis.
var ErrVersion = errors.New("unsupported wire version")

// EntityReader reads one entity item, `[version, plains, tagged]` (ADR-0207
// SD1), as a cursor over a CborReader.
//
// It is the mirror of EntityWriter and, like AttributeReader, it reads the
// framing and leaves the values to the caller: the generated decoder knows
// each column's canonical type and calls the typed value readers on Reader().
// The orders the form fixes and a CborReader does not check are checked here —
// plain keys strictly increasing, slot keys strictly increasing in the RFC 8949
// §4.2.3 length-first sense, no slot present but empty.
//
// The call order is Begin, NextPlain until it reports no more (reading nCols
// values on Reader() after each), NextSlot until it reports no more (reading
// nAttrs attributes through Attributes() after each), End. Reading a CBOR
// sequence is that cycle repeated while Remaining() is positive.
//
// Begin cannot read the tagged map's head, which sits after the plains'
// contents: it is read by the first NextSlot, or by End when the caller never
// asked for a slot.
//
// One reader per decoding goroutine, reused across entities; not
// goroutine-safe.
type EntityReader struct {
	r    *CborReader
	attr AttributeReader

	nPlains      int
	plainsRead   int
	prevPlainKey uint64
	havePlainKey bool

	nSlots     int
	slotsRead  int
	prevSig    []byte
	haveSig    bool
	taggedOpen bool

	begun bool
}

// NewEntityReader returns a reader over r, with the attribute reader it hands
// out bound to the same underlying reader.
func NewEntityReader(r *CborReader) (inst *EntityReader) {
	inst = &EntityReader{r: r}
	inst.attr.r = r
	return
}

// Reader returns the underlying CborReader — the one the caller reads a plain
// section's values from.
func (inst *EntityReader) Reader() *CborReader { return inst.r }

// Reset points the underlying reader at b and clears the cursor's own
// bookkeeping, so a reader left mid-entity — by a decode that refused the
// entity and abandoned it — is usable for the next one. It is the entry point
// a decoder that reads entity after entity out of separate buffers uses in
// place of CborReader.Reset.
func (inst *EntityReader) Reset(b []byte) {
	inst.r.Reset(b)
	inst.attr.reset()
	inst.nPlains = 0
	inst.plainsRead = 0
	inst.prevPlainKey = 0
	inst.havePlainKey = false
	inst.nSlots = 0
	inst.slotsRead = 0
	inst.prevSig = nil
	inst.haveSig = false
	inst.taggedOpen = false
	inst.begun = false
}

// Attributes returns the attribute reader for the slot NextSlot last opened.
// It is one reader reused across every attribute of every slot, so it must not
// be held across the NextSlot that ends the slot it belongs to.
func (inst *EntityReader) Attributes() *AttributeReader { return &inst.attr }

// Err returns the underlying reader's first error.
func (inst *EntityReader) Err() error { return inst.r.err }

// Remaining returns how many bytes are left unread — zero after the last
// entity of a CBOR sequence, positive while another one follows.
func (inst *EntityReader) Remaining() int { return inst.r.Remaining() }

// Begin reads the entity's array head, its version and the head of its plains
// map. A version other than Version is refused: the form is not
// forwards-compatible by design, so a decoder that met a newer one has no
// grounds to guess at the rest.
func (inst *EntityReader) Begin() {
	r := inst.r
	if r.err != nil {
		return
	}
	if inst.begun {
		r.fail(eb.Build().Int("pos", r.pos).Errorf("entity opened while another is still open: %w", ErrOutOfRange))
		return
	}
	if n := r.ReadArrayHead(); r.err == nil && n != 3 {
		r.fail(eb.Build().Int("pos", r.pos).Int("elements", n).Errorf("an entity is a three-element array: %w", ErrOutOfRange))
	}
	if r.err != nil {
		return
	}
	v := r.ReadUint()
	if r.err != nil {
		return
	}
	if v != Version {
		r.fail(eb.Build().Int("pos", r.pos).Uint64("version", v).Uint64("want", Version).Errorf("entity carries version %d: %w", v, ErrVersion))
		return
	}
	n := r.ReadMapHead()
	if r.err != nil {
		return
	}
	inst.nPlains = n
	inst.plainsRead = 0
	inst.prevPlainKey = 0
	inst.havePlainKey = false
	inst.nSlots = 0
	inst.slotsRead = 0
	inst.prevSig = nil
	inst.haveSig = false
	inst.taggedOpen = false
	inst.begun = true
}

// NextPlain opens the next plain section, in wire order, and returns its item
// type and its column count. ok is false once the plains map is exhausted, and
// the caller moves on to NextSlot.
//
// The keys must be strictly increasing: they are CBOR uints, whose encodings
// sort in the same order as the ordinals, so a canonical map is one whose item
// types ascend. The caller reads the nCols values on Reader().
func (inst *EntityReader) NextPlain() (itemType common.PlainItemTypeE, nCols int, ok bool) {
	r := inst.r
	if r.err != nil {
		return
	}
	if !inst.begun {
		r.fail(eb.Build().Int("pos", r.pos).Errorf("plain section read outside an entity: %w", ErrOutOfRange))
		return
	}
	if inst.plainsRead >= inst.nPlains {
		return
	}
	k := r.ReadUint()
	if r.err != nil {
		return
	}
	if inst.havePlainKey && k <= inst.prevPlainKey {
		r.fail(eb.Build().Int("pos", r.pos).Uint64("key", k).Uint64("previous", inst.prevPlainKey).Errorf("plain item types are not strictly increasing: %w", ErrNotCanonical))
		return
	}
	if k == uint64(common.PlainItemTypeNone) || k >= uint64(common.MaxPlainItemTypeExcl) {
		r.fail(eb.Build().Int("pos", r.pos).Uint64("key", k).Errorf("not a readable plain item type: %w", ErrOutOfRange))
		return
	}
	n := r.ReadArrayHead()
	if r.err != nil {
		return
	}
	inst.prevPlainKey = k
	inst.havePlainKey = true
	inst.plainsRead++
	return common.PlainItemTypeE(k), n, true
}

// NextSlot opens the next tagged slot, in wire order, and returns its CT
// signature and how many attributes it carries. ok is false once the tagged
// map is exhausted.
//
// The keys must be strictly increasing by their encoded bytes (RFC 8949
// §4.2.3): length first, then the content bytewise. A slot with no attributes
// is not canonical either — the writer omits an empty slot rather than writing
// the key.
//
// signature is a view into the reader's buffer; copy it to keep it. The caller
// reads the nAttrs attributes through Attributes().
func (inst *EntityReader) NextSlot() (signature []byte, nAttrs int, ok bool) {
	r := inst.r
	if r.err != nil {
		return
	}
	if !inst.begun {
		r.fail(eb.Build().Int("pos", r.pos).Errorf("slot read outside an entity: %w", ErrOutOfRange))
		return
	}
	inst.openTagged()
	if r.err != nil {
		return
	}
	if inst.slotsRead >= inst.nSlots {
		return
	}
	sig := r.ReadText()
	if r.err != nil {
		return
	}
	if inst.haveSig && compareTextKeyBytes(inst.prevSig, sig) >= 0 {
		r.fail(eb.Build().Int("pos", r.pos).Bytes("signature", sig).Bytes("previous", inst.prevSig).Errorf("slot signatures are not strictly increasing: %w", ErrNotCanonical))
		return
	}
	n := r.ReadArrayHead()
	if r.err != nil {
		return
	}
	if n == 0 {
		r.fail(eb.Build().Int("pos", r.pos).Bytes("signature", sig).Errorf("a slot with no attributes is omitted, not written empty: %w", ErrNotCanonical))
		return
	}
	inst.prevSig = sig
	inst.haveSig = true
	inst.slotsRead++
	return sig, n, true
}

// openTagged reads the tagged map's head the first time a slot is asked for.
// It sits after the plains' contents, so Begin cannot have read it, and the
// plains must be exhausted before the cursor is there.
func (inst *EntityReader) openTagged() {
	r := inst.r
	if inst.taggedOpen || r.err != nil {
		return
	}
	if inst.plainsRead != inst.nPlains {
		r.fail(eb.Build().Int("pos", r.pos).Int("read", inst.plainsRead).Int("nPlains", inst.nPlains).Errorf("the tagged map was reached with plain sections left unread: %w", ErrOutOfRange))
		return
	}
	n := r.ReadMapHead()
	if r.err != nil {
		return
	}
	inst.nSlots = n
	inst.taggedOpen = true
}

// End closes the entity. The cursor is then on the byte after the entity item,
// which is the next item of a CBOR sequence or the end of the buffer — so End
// checks the bookkeeping that guarantees it: every plain section and every slot
// consumed.
func (inst *EntityReader) End() {
	r := inst.r
	if !inst.begun {
		r.fail(eb.Build().Int("pos", r.pos).Errorf("no entity is open: %w", ErrOutOfRange))
		return
	}
	inst.begun = false
	if r.err != nil {
		return
	}
	if inst.plainsRead != inst.nPlains {
		r.fail(eb.Build().Int("pos", r.pos).Int("read", inst.plainsRead).Int("nPlains", inst.nPlains).Errorf("entity closed with plain sections left unread: %w", ErrOutOfRange))
		return
	}
	// A caller that never asked for a slot has left the tagged map's head
	// unread; it is read here, and the entity is only over if it is empty.
	inst.openTagged()
	if r.err != nil {
		return
	}
	if inst.slotsRead != inst.nSlots {
		r.fail(eb.Build().Int("pos", r.pos).Int("read", inst.slotsRead).Int("nSlots", inst.nSlots).Errorf("entity closed with slots left unread: %w", ErrOutOfRange))
	}
}

// compareTextKeyBytes orders two CBOR text-string map keys by their encoded
// bytes (RFC 8949 §4.2.3), over the decoded content: length first, then the
// content bytewise. It is compareTextKeys over the reader's views.
func compareTextKeyBytes(a []byte, b []byte) (r int) {
	if r = cmp.Compare(len(a), len(b)); r != 0 {
		return
	}
	return bytes.Compare(a, b)
}
