package runtime

import (
	"bytes"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// VerifyCanonical checks one entity item against everything the leeway
// canonical wire fixes without a table description (ADR-0210 SD1–SD5).
//
// The checks:
//
//   - the item is `[Version, plains:map, tagged:map]`, and nothing follows it;
//   - plain keys are readable item types, strictly increasing;
//   - slot keys are text, strictly increasing by their encoded bytes (RFC 8949
//     §4.2.3, length first), and no slot is present but empty;
//   - each attribute is `[memberships, values…(, disc)]` with its memberships
//     non-decreasing bytewise within each membership group, and every attribute
//     of one slot has the same outer length and the same number of membership
//     groups — the SD5 discriminator is uniform per slot, and the co-section
//     count is a property of the slot;
//   - a slot's attributes are non-decreasing under CompareAttributes;
//   - a set (tag 258) holds elements strictly increasing bytewise;
//   - every head is in the deterministic subset, which the CborReader enforces
//     as it reads.
//
// The value cardinalities CompareAttributes needs are derived from the value
// forms rather than from a table, by DeriveCardinality — the same function
// AttributeWriter.EndValue derives them with, so the order produced and the
// order verified cannot drift. A discriminator, being a uint, contributes a
// trailing 1 to every attribute of its slot, which is uniform and so does not
// disturb the comparison.
//
// The memberships element is read table-free too. A membership is always a
// two-element array whose first element is a uint, so an element of that array
// which is itself an empty array, or an array whose first element is an array,
// can only be a co-section group's own membership list (ADR-0210 SD3) — and
// the grouped form is only canonical for two or more groups, since one group
// is written flat.
func VerifyCanonical(item []byte) (err error) {
	r := NewCborReader(item)
	verifyEntity(r)
	if err = r.Err(); err != nil {
		return
	}
	if n := r.Remaining(); n != 0 {
		err = eb.Build().Int("trailing", n).Errorf("bytes left over after the entity item: %w", ErrOutOfRange)
	}
	return
}

// VerifyCanonicalSequence checks a CBOR sequence of entity items (RFC 8742)
// and returns how many it read. On failure n is the index of the entity that
// failed.
func VerifyCanonicalSequence(b []byte) (n int, err error) {
	r := NewCborReader(b)
	for !r.Done() {
		verifyEntity(r)
		if err = r.Err(); err != nil {
			err = eb.Build().Int("entity", n).Int("pos", r.Pos()).Errorf("unable to verify the entity: %w", err)
			return
		}
		n++
	}
	err = r.Err()
	return
}

// verifyEntity walks one entity item from the reader's current position,
// recording the first violation as the reader's sticky error.
func verifyEntity(r *CborReader) {
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
	verifyPlains(r)
	if r.err != nil {
		return
	}
	verifyTagged(r)
}

// verifyPlains walks the plains map: uint keys strictly increasing, each value
// an array of column values.
func verifyPlains(r *CborReader) {
	n := r.ReadMapHead()
	if r.err != nil {
		return
	}
	var prev uint64
	have := false
	for range n {
		k := r.ReadUint()
		if r.err != nil {
			return
		}
		if have && k <= prev {
			r.fail(eb.Build().Int("pos", r.pos).Uint64("key", k).Uint64("previous", prev).Errorf("plain item types are not strictly increasing: %w", ErrNotCanonical))
			return
		}
		prev, have = k, true
		nCols := r.ReadArrayHead()
		if r.err != nil {
			return
		}
		for range nCols {
			r.Skip()
			if r.err != nil {
				return
			}
		}
	}
}

// verifyTagged walks the tagged map: text keys strictly increasing by their
// encoded bytes, each value a non-empty array of attributes.
func verifyTagged(r *CborReader) {
	n := r.ReadMapHead()
	if r.err != nil {
		return
	}
	var prev []byte
	have := false
	for range n {
		sig := r.ReadText()
		if r.err != nil {
			return
		}
		if have && compareTextKeyBytes(prev, sig) >= 0 {
			r.fail(eb.Build().Int("pos", r.pos).Bytes("signature", sig).Bytes("previous", prev).Errorf("slot signatures are not strictly increasing: %w", ErrNotCanonical))
			return
		}
		prev, have = sig, true
		nAttrs := r.ReadArrayHead()
		if r.err != nil {
			return
		}
		if nAttrs == 0 {
			r.fail(eb.Build().Int("pos", r.pos).Bytes("signature", sig).Errorf("a slot with no attributes is omitted, not written empty: %w", ErrNotCanonical))
			return
		}
		verifySlot(r, sig, nAttrs)
		if r.err != nil {
			return
		}
	}
}

// verifySlot walks one slot's attribute array, checking the outer length and the
// membership-group count are uniform and the attributes are in canonical order.
func verifySlot(r *CborReader, sig []byte, nAttrs int) {
	outer := -1
	groups := -1
	var prev Attr
	for i := range nAttrs {
		cur := verifyAttribute(r, &outer, &groups)
		if r.err != nil {
			return
		}
		if i > 0 && CompareAttributes(&prev, &cur) > 0 {
			r.fail(eb.Build().Int("pos", r.pos).Bytes("signature", sig).Int("index", i).Errorf("attributes are not in canonical order: %w", ErrNotCanonical))
			return
		}
		prev = cur
	}
}

// verifyAttribute walks one attribute item and returns it in the shape
// CompareAttributes reads, with the cardinalities derived from the value forms.
// outer and groups carry the slot's attribute length and membership-group count
// across the call: -1 on the first attribute, the agreed value afterwards.
func verifyAttribute(r *CborReader, outer *int, groups *int) (attr Attr) {
	start := r.pos
	n := r.ReadArrayHead()
	if r.err != nil {
		return
	}
	if n < 1 {
		r.fail(eb.Build().Int("pos", r.pos).Errorf("an attribute carries at least its memberships array: %w", ErrOutOfRange))
		return
	}
	if *outer < 0 {
		*outer = n
	} else if n != *outer {
		r.fail(eb.Build().Int("pos", r.pos).Int("elements", n).Int("want", *outer).Errorf("attributes of one slot differ in length; the discriminator is uniform per slot: %w", ErrNotCanonical))
		return
	}

	membStart := r.pos
	nGroups, total := verifyMemberships(r)
	if r.err != nil {
		return
	}
	if *groups < 0 {
		*groups = nGroups
	} else if nGroups != *groups {
		r.fail(eb.Build().Int("pos", r.pos).Int("groups", nGroups).Int("want", *groups).Errorf("attributes of one slot differ in membership-group count; a slot's co-section count is fixed: %w", ErrNotCanonical))
		return
	}
	membEnd := r.pos

	cards := make([]uint64, 0, n-1)
	for range n - 1 {
		cards = append(cards, verifyValue(r))
		if r.err != nil {
			return Attr{}
		}
	}
	end := r.pos
	return Attr{
		Key:  AttributeKey{MembershipCount: uint32(total), Cards: cards},
		Item: r.b[start:end],
		Memb: r.b[membStart:membEnd],
		Vals: r.b[membEnd:end],
	}
}

// verifyMemberships walks one attribute's memberships element and reports how
// many groups it carries and how many memberships in total.
//
// The flat form (one group) and the grouped form (a co-section group's k lists)
// are told apart from the bytes, without a table: see VerifyCanonical.
func verifyMemberships(r *CborReader) (nGroups int, total int) {
	m := r.ReadArrayHead()
	if r.err != nil {
		return
	}
	if m == 0 || !membershipsAreGrouped(r) {
		verifyMembershipList(r, m)
		if r.err != nil {
			return 0, 0
		}
		return 1, m
	}
	if m < 2 {
		r.fail(eb.Build().Int("pos", r.pos).Int("groups", m).Errorf("a single membership group is written flat, not wrapped: %w", ErrNotCanonical))
		return 0, 0
	}
	for range m {
		g := r.ReadArrayHead()
		if r.err != nil {
			return 0, 0
		}
		verifyMembershipList(r, g)
		if r.err != nil {
			return 0, 0
		}
		total += g
	}
	return m, total
}

// membershipsAreGrouped peeks the first element of a non-empty memberships array
// — without consuming anything — and reports whether it is a co-section group's
// membership list rather than a membership.
func membershipsAreGrouped(r *CborReader) (grouped bool) {
	p := NewCborReader(r.b[r.pos:])
	mt, arg := p.ReadHead()
	if p.Err() != nil || mt != MajorTypeArray {
		return false
	}
	if arg == 0 {
		// An empty array cannot be a membership, which is always two elements.
		return true
	}
	inner, ok := p.PeekMajor()
	if !ok {
		return false
	}
	// A membership's first element is the channel ordinal, a uint; a group's is
	// a membership, an array.
	return inner == MajorTypeArray
}

// verifyMembershipList walks n memberships and requires them to be
// non-decreasing bytewise (SD3), which allows duplicates but not a permutation.
func verifyMembershipList(r *CborReader, n int) {
	var prev []byte
	have := false
	for j := range n {
		ms := r.pos
		r.ReadMembership()
		if r.err != nil {
			return
		}
		item := r.since(ms)
		if have && bytes.Compare(item, prev) < 0 {
			r.fail(eb.Build().Int("pos", r.pos).Int("index", j).Errorf("memberships are not sorted bytewise: %w", ErrNotCanonical))
			return
		}
		prev, have = item, true
	}
}

// verifyValue consumes one value of an attribute and returns its cardinality
// under the SD3 rule. A set's elements are checked to be sorted on the way
// past.
func verifyValue(r *CborReader) (card uint64) {
	if r.err != nil {
		return
	}
	// The cardinality is read off the heads by the same helper the encoder
	// uses; the walk below only has to consume the value and check the one
	// ordering a set carries.
	card, err := DeriveCardinality(r.b[r.pos:])
	if err != nil {
		r.fail(eb.Build().Int("pos", r.pos).Errorf("unable to derive the value's cardinality: %w", err))
		return 0
	}
	if r.IsNull() {
		r.ReadNull()
		return
	}
	mt, ok := r.PeekMajor()
	if !ok {
		r.Skip() // records the truncation
		return 0
	}
	if mt == MajorTypeTag {
		tag := r.ReadTag()
		if r.err != nil {
			return 0
		}
		if tag == TagSet {
			verifySetElements(r)
			if r.err != nil {
				return 0
			}
			return
		}
	}
	// Every other form — a scalar, an `h` array, a tagged temporal or network
	// value whose tag was just consumed — is consumed whole by Skip, which
	// validates every head it passes.
	r.Skip()
	if r.err != nil {
		return 0
	}
	return
}

// verifySetElements walks the array a tag 258 wraps, requiring its elements to
// be non-decreasing bytewise, which is what makes a set's bytes canonical
// (SD3). Equal adjacent elements are duplicates and are admissible: a set is a
// co-container of the section's arrays, so its length is content and the form
// keeps the duplicates rather than changing it.
func verifySetElements(r *CborReader) {
	n := r.ReadArrayHead()
	if r.err != nil {
		return
	}
	var prev []byte
	have := false
	for i := range n {
		e := r.ReadItemBytes()
		if r.err != nil {
			return
		}
		if have && bytes.Compare(prev, e) > 0 {
			r.fail(eb.Build().Int("pos", r.pos).Int("index", i).Errorf("set elements are not sorted bytewise: %w", ErrNotCanonical))
			return
		}
		prev, have = e, true
	}
}
