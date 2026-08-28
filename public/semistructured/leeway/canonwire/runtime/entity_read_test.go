package runtime

import (
	"bytes"
	"encoding/hex"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// The golden of TestEntityGolden, read back through the cursor readers: the
// plain, then the two slots in wire order, each with one attribute.
func TestEntityReaderReadsTheGolden(t *testing.T) {
	raw, err := hex.DecodeString("8301a10181182aa26173818281820001626869637536348182818201416d07")
	require.NoError(t, err)

	er := NewEntityReader(NewCborReader(raw))
	er.Begin()
	require.NoError(t, er.Err())

	itemType, nCols, ok := er.NextPlain()
	require.True(t, ok)
	require.Equal(t, common.PlainItemTypeEntityId, itemType)
	require.Equal(t, 1, nCols)
	require.Equal(t, uint64(42), er.Reader().ReadUint())
	_, _, ok = er.NextPlain()
	require.False(t, ok)

	ar := er.Attributes()

	sig, nAttrs, ok := er.NextSlot()
	require.True(t, ok)
	require.Equal(t, "s", string(sig))
	require.Equal(t, 1, nAttrs)
	hasDisc := ar.Begin(1, 1)
	require.False(t, hasDisc)
	require.Equal(t, 1, ar.NextGroup())
	ch, ref, verbatim, params := ar.Membership()
	require.Equal(t, mappingplan.MembershipChannelLowCardRef, ch)
	require.Equal(t, uint64(1), ref)
	require.Nil(t, verbatim)
	require.Nil(t, params)
	require.Equal(t, "hi", ar.Reader().ReadTextString())
	ar.End()
	require.NoError(t, er.Err())

	sig, nAttrs, ok = er.NextSlot()
	require.True(t, ok)
	require.Equal(t, "u64", string(sig))
	require.Equal(t, 1, nAttrs)
	hasDisc = ar.Begin(1, 1)
	require.False(t, hasDisc)
	require.Equal(t, 1, ar.NextGroup())
	ch, _, verbatim, _ = ar.Membership()
	require.Equal(t, mappingplan.MembershipChannelLowCardVerbatim, ch)
	require.Equal(t, []byte("m"), verbatim)
	require.Equal(t, uint64(7), ar.Reader().ReadUint())
	ar.End()

	_, _, ok = er.NextSlot()
	require.False(t, ok)
	er.End()
	require.NoError(t, er.Err())
	require.Zero(t, er.Remaining())
}

// An entity with no plains and no slots is the shortest one there is, and a
// caller may close it without ever asking for a slot.
func TestEntityReaderEmptyEntity(t *testing.T) {
	raw, err := hex.DecodeString("8301a0a0")
	require.NoError(t, err)
	er := NewEntityReader(NewCborReader(raw))
	er.Begin()
	_, _, ok := er.NextPlain()
	require.False(t, ok)
	er.End()
	require.NoError(t, er.Err())
	require.Zero(t, er.Remaining())
}

// Memberships written in any order come back sorted, and the attribute's
// values follow them on the same cursor.
func TestAttributeReaderMixedMembershipsComeBackSorted(t *testing.T) {
	type memb struct {
		ch       mappingplan.MembershipChannel
		ref      uint64
		verbatim []byte
		params   []byte
	}
	membs := []memb{
		{ch: mappingplan.MembershipChannelMixedLowCardRef, ref: 1, params: []byte("p")},
		{ch: mappingplan.MembershipChannelLowCardRef, ref: 9},
		{ch: mappingplan.MembershipChannelHighCardVerbatim, verbatim: []byte("zz")},
		{ch: mappingplan.MembershipChannelLowCardRef, ref: 2},
	}
	var b bytes.Buffer
	ew, err := NewEntityWriter()
	require.NoError(t, err)
	aw, err := NewAttributeWriter(1, 1)
	require.NoError(t, err)
	ew.Begin()
	aw.Begin()
	for _, m := range membs {
		aw.Membership(0, m.ch, m.ref, m.verbatim, m.params)
	}
	aw.BeginValue().WriteTextString("v")
	aw.EndValue()
	a, err := aw.End()
	require.NoError(t, err)
	ew.Slot("s").Add(a)
	require.NoError(t, ew.Flush(&b))
	require.NoError(t, VerifyCanonical(b.Bytes()))

	er := NewEntityReader(NewCborReader(b.Bytes()))
	er.Begin()
	sig, nAttrs, ok := er.NextSlot()
	require.True(t, ok)
	require.Equal(t, "s", string(sig))
	require.Equal(t, 1, nAttrs)
	ar := er.Attributes()
	hasDisc := ar.Begin(1, 1)
	require.False(t, hasDisc)
	nMemb := ar.NextGroup()
	require.Equal(t, 4, nMemb)
	got := make([]memb, 0, nMemb)
	for range nMemb {
		ch, ref, verbatim, params := ar.Membership()
		got = append(got, memb{ch: ch, ref: ref, verbatim: verbatim, params: params})
	}
	require.NoError(t, er.Err())
	// Sorted on the encoded item: the channel ordinal leads, then the identity.
	require.Equal(t, []memb{
		{ch: mappingplan.MembershipChannelLowCardRef, ref: 2},
		{ch: mappingplan.MembershipChannelLowCardRef, ref: 9},
		{ch: mappingplan.MembershipChannelHighCardVerbatim, verbatim: []byte("zz")},
		{ch: mappingplan.MembershipChannelMixedLowCardRef, ref: 1, params: []byte("p")},
	}, got)
	require.Equal(t, "v", ar.Reader().ReadTextString())
	ar.End()
	_, _, ok = er.NextSlot()
	require.False(t, ok)
	er.End()
	require.NoError(t, er.Err())
}

// An entity with two plains, two slots and a per-slot discriminator, written
// and read back whole.
func TestEntityReaderPlainsSlotsAndDiscriminator(t *testing.T) {
	var b bytes.Buffer
	ew, err := NewEntityWriter()
	require.NoError(t, err)
	ew.Begin()
	ew.BeginPlain(common.PlainItemTypeEntityId, 1).WriteBytes([]byte{0xaa})
	ew.EndPlain()
	cw := ew.BeginPlain(common.PlainItemTypeTransaction, 2)
	cw.WriteUint(11)
	cw.WriteBool(true)
	ew.EndPlain()

	aw1, err := NewAttributeWriter(1, 1)
	require.NoError(t, err)
	for _, v := range []string{"b", "a"} {
		aw1.Begin()
		aw1.Membership(0, mappingplan.MembershipChannelLowCardRef, 3, nil, nil)
		aw1.BeginValue().WriteTextString(v)
		aw1.EndValue()
		a, err2 := aw1.End()
		require.NoError(t, err2)
		ew.Slot("s").Add(a)
	}
	aw2, err := NewAttributeWriter(0, 1)
	require.NoError(t, err)
	for _, d := range []uint64{5, 2} {
		aw2.Begin()
		aw2.Membership(0, mappingplan.MembershipChannelHighCardRefParametrized, 0, nil, []byte("q"))
		aw2.Discriminator(d)
		a, err2 := aw2.End()
		require.NoError(t, err2)
		ew.Slot("").Add(a)
	}
	require.NoError(t, ew.Flush(&b))
	require.NoError(t, VerifyCanonical(b.Bytes()))

	er := NewEntityReader(NewCborReader(b.Bytes()))
	ar := er.Attributes()
	er.Begin()

	itemType, nCols, ok := er.NextPlain()
	require.True(t, ok)
	require.Equal(t, common.PlainItemTypeEntityId, itemType)
	require.Equal(t, 1, nCols)
	require.Equal(t, []byte{0xaa}, er.Reader().ReadBytes())
	itemType, nCols, ok = er.NextPlain()
	require.True(t, ok)
	require.Equal(t, common.PlainItemTypeTransaction, itemType)
	require.Equal(t, 2, nCols)
	require.Equal(t, uint64(11), er.Reader().ReadUint())
	require.True(t, er.Reader().ReadBool())
	_, _, ok = er.NextPlain()
	require.False(t, ok)

	// The empty signature sorts first: one byte of key against one plus one.
	sig, nAttrs, ok := er.NextSlot()
	require.True(t, ok)
	require.Empty(t, string(sig))
	require.Equal(t, 2, nAttrs)
	discs := make([]uint64, 0, nAttrs)
	for range nAttrs {
		hasDisc := ar.Begin(0, 1)
		require.True(t, hasDisc)
		require.Equal(t, 1, ar.NextGroup())
		ch, _, _, params := ar.Membership()
		require.Equal(t, mappingplan.MembershipChannelHighCardRefParametrized, ch)
		require.Equal(t, []byte("q"), params)
		discs = append(discs, ar.Discriminator())
		ar.End()
	}
	require.NoError(t, er.Err())
	// Equal memberships and no values: the discriminator breaks the tie.
	require.Equal(t, []uint64{2, 5}, discs)

	sig, nAttrs, ok = er.NextSlot()
	require.True(t, ok)
	require.Equal(t, "s", string(sig))
	require.Equal(t, 2, nAttrs)
	vals := make([]string, 0, nAttrs)
	for range nAttrs {
		ar.Begin(1, 1)
		ar.NextGroup()
		ar.Membership()
		vals = append(vals, ar.Reader().ReadTextString())
		ar.End()
	}
	require.Equal(t, []string{"a", "b"}, vals)

	_, _, ok = er.NextSlot()
	require.False(t, ok)
	er.End()
	require.NoError(t, er.Err())
	require.Zero(t, er.Remaining())
}

// A co-section slot, written through the writer and read back: two membership
// groups, one of them empty, and the reader walks them group by group.
func TestAttributeReaderCoGroupMemberships(t *testing.T) {
	var b bytes.Buffer
	ew, err := NewEntityWriter()
	require.NoError(t, err)
	aw, err := NewAttributeWriter(1, 2)
	require.NoError(t, err)
	ew.Begin()
	aw.Begin()
	aw.Membership(0, mappingplan.MembershipChannelHighCardRef, 8, nil, nil)
	aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 3, nil, nil)
	aw.BeginValue().WriteUint(1)
	aw.EndValue()
	a, err := aw.End()
	require.NoError(t, err)
	require.Equal(t, uint32(2), a.Key.MembershipCount)
	ew.Slot("u64_u64").Add(a)
	require.NoError(t, ew.Flush(&b))
	require.NoError(t, VerifyCanonical(b.Bytes()))

	er := NewEntityReader(NewCborReader(b.Bytes()))
	ar := er.Attributes()
	er.Begin()
	sig, nAttrs, ok := er.NextSlot()
	require.True(t, ok)
	require.Equal(t, "u64_u64", string(sig))
	require.Equal(t, 1, nAttrs)
	require.False(t, ar.Begin(1, 2))
	require.Equal(t, 2, ar.NextGroup())
	ch, ref, _, _ := ar.Membership()
	require.Equal(t, mappingplan.MembershipChannelLowCardRef, ch)
	require.Equal(t, uint64(3), ref)
	ch, ref, _, _ = ar.Membership()
	require.Equal(t, mappingplan.MembershipChannelHighCardRef, ch)
	require.Equal(t, uint64(8), ref)
	require.Zero(t, ar.NextGroup())
	require.Equal(t, uint64(1), ar.Reader().ReadUint())
	ar.End()
	_, _, ok = er.NextSlot()
	require.False(t, ok)
	er.End()
	require.NoError(t, er.Err())

	// Reading it as a standalone section's attribute is refused: the flat form
	// has memberships where the grouped one has groups.
	er2 := NewEntityReader(NewCborReader(b.Bytes()))
	er2.Begin()
	_, _, ok = er2.NextSlot()
	require.True(t, ok)
	er2.Attributes().Begin(1, 1)
	er2.Attributes().NextGroup()
	er2.Attributes().Membership()
	require.Error(t, er2.Err())
}

// A CBOR sequence is the same cycle repeated while bytes are left.
func TestEntityReaderWalksASequence(t *testing.T) {
	var b bytes.Buffer
	ew, err := NewEntityWriter()
	require.NoError(t, err)
	for i := range 3 {
		ew.Begin()
		ew.BeginPlain(common.PlainItemTypeEntityId, 1).WriteUint(uint64(i))
		ew.EndPlain()
		require.NoError(t, ew.Flush(&b))
	}
	n, err := VerifyCanonicalSequence(b.Bytes())
	require.NoError(t, err)
	require.Equal(t, 3, n)

	er := NewEntityReader(NewCborReader(b.Bytes()))
	got := make([]uint64, 0, 3)
	for er.Remaining() > 0 {
		er.Begin()
		_, nCols, ok := er.NextPlain()
		require.True(t, ok)
		require.Equal(t, 1, nCols)
		got = append(got, er.Reader().ReadUint())
		_, _, ok = er.NextPlain()
		require.False(t, ok)
		er.End()
		require.NoError(t, er.Err())
	}
	require.Equal(t, []uint64{0, 1, 2}, got)
}

func TestEntityReaderRejectsAWrongVersion(t *testing.T) {
	raw := rawEntity(t, 2, nil, nil)
	er := NewEntityReader(NewCborReader(raw))
	er.Begin()
	require.ErrorIs(t, er.Err(), ErrVersion)
	require.ErrorContains(t, er.Err(), "version 2")
}

func TestAttributeReaderRejectsAWrongOuterLength(t *testing.T) {
	// One membership and one value is 1+nCols for nCols == 1; asking for two
	// columns cannot fit.
	item := rawAttr(t, [][]byte{rawMemb(t, mappingplan.MembershipChannelLowCardRef, 0)}, rawVal(t, func(cw *CborWriter) { cw.WriteUint(1) }))
	raw := rawEntity(t, Version, nil, []rawSlot{{sig: "s", attrs: [][]byte{item}}})
	er := NewEntityReader(NewCborReader(raw))
	er.Begin()
	_, _, ok := er.NextSlot()
	require.True(t, ok)
	er.Attributes().Begin(2, 1)
	require.ErrorIs(t, er.Err(), ErrOutOfRange)
}

func TestEntityReaderRejectsUnreadSlots(t *testing.T) {
	item := rawAttr(t, nil, rawVal(t, func(cw *CborWriter) { cw.WriteUint(1) }))
	raw := rawEntity(t, Version, nil, []rawSlot{{sig: "s", attrs: [][]byte{item}}})
	er := NewEntityReader(NewCborReader(raw))
	er.Begin()
	er.End()
	require.ErrorIs(t, er.Err(), ErrOutOfRange)
}

// ---------------------------------------------------------------------------
// The round-trip property: a random entity written through the writers, checked
// with VerifyCanonical, read back through the cursor readers and written again
// must reproduce the first bytes exactly.

// The value kinds the property draws. They are the SD3 forms the runtime has
// typed readers for, plus the two containers and null, which are the three
// cases the cardinality rule distinguishes.
const (
	kindUint = iota
	kindInt
	kindText
	kindBytes
	kindBool
	kindF64
	kindNull
	kindArray
	kindSet
)

var allKinds = []int{kindUint, kindInt, kindText, kindBytes, kindBool, kindF64, kindNull, kindArray, kindSet}

type randMemb struct {
	ch       mappingplan.MembershipChannel
	ref      uint64
	verbatim []byte
	params   []byte
}

type randAttr struct {
	// membs holds one membership list per membership group — one group for a
	// standalone section, k for a co-section group of k (ADR-0210 SD3).
	membs   [][]randMemb
	vals    []any
	disc    uint64
	hasDisc bool
}

type randSlot struct {
	sig     string
	kinds   []int
	nGroups int
	hasDisc bool
	attrs   []randAttr
}

type randPlain struct {
	itemType common.PlainItemTypeE
	kinds    []int
	vals     []any
}

type randEntity struct {
	plains []randPlain
	slots  []randSlot
}

// textPool keeps the drawn text valid UTF-8, which ReadText requires.
var textPool = []string{"", "a", "hi", "zzz", "héllo", "µ"}

func drawValue(rt *rapid.T, kind int, label string) (v any) {
	switch kind {
	case kindUint:
		return rapid.Uint64().Draw(rt, label)
	case kindInt:
		return rapid.Int64().Draw(rt, label)
	case kindText:
		return rapid.SampledFrom(textPool).Draw(rt, label)
	case kindBytes:
		return rapid.SliceOfN(rapid.Byte(), 0, 4).Draw(rt, label)
	case kindBool:
		return rapid.Bool().Draw(rt, label)
	case kindF64:
		return rapid.Float64().Draw(rt, label)
	case kindNull:
		return nil
	case kindArray:
		return rapid.SliceOfN(rapid.Uint64(), 0, 3).Draw(rt, label)
	case kindSet:
		// Duplicates allowed: the writer sorts but keeps them, so the set's
		// cardinality is the number of elements drawn.
		return rapid.SliceOfN(rapid.Uint64(), 0, 3).Draw(rt, label)
	}
	return nil
}

// writeValue writes one drawn value and returns the cardinality the form
// derives from it (ADR-0210 SD3).
func writeValue(cw *CborWriter, sw *SetWriter, kind int, v any) (card uint64) {
	switch kind {
	case kindUint:
		cw.WriteUint(v.(uint64))
	case kindInt:
		cw.WriteInt(v.(int64))
	case kindText:
		cw.WriteTextString(v.(string))
	case kindBytes:
		cw.WriteBytes(v.([]byte))
	case kindBool:
		cw.WriteBool(v.(bool))
	case kindF64:
		cw.WriteF64(v.(float64))
	case kindNull:
		cw.WriteNull()
		return 0
	case kindArray:
		xs := v.([]uint64)
		cw.ArrayHead(len(xs))
		for _, x := range xs {
			cw.WriteUint(x)
		}
		return uint64(len(xs))
	case kindSet:
		xs := v.([]uint64)
		sw.Begin()
		for _, x := range xs {
			sw.Elem().WriteUint(x)
			sw.EndElem()
		}
		sw.Flush(cw)
		return uint64(len(xs))
	}
	return 1
}

func readValue(r *CborReader, kind int) (v any) {
	switch kind {
	case kindUint:
		return r.ReadUint()
	case kindInt:
		return r.ReadInt()
	case kindText:
		return r.ReadTextString()
	case kindBytes:
		return bytes.Clone(r.ReadBytes())
	case kindBool:
		return r.ReadBool()
	case kindF64:
		return r.ReadF64()
	case kindNull:
		r.ReadNull()
		return nil
	case kindArray:
		n := r.ReadArrayHead()
		xs := make([]uint64, 0, max(n, 0))
		for range n {
			xs = append(xs, r.ReadUint())
		}
		return xs
	case kindSet:
		n := r.ReadSetHead()
		xs := make([]uint64, 0, max(n, 0))
		for range n {
			xs = append(xs, r.ReadUint())
		}
		return xs
	}
	return nil
}

func drawEntity(rt *rapid.T) (e randEntity) {
	for _, itemType := range common.AllPlainItemTypes {
		if itemType == common.PlainItemTypeNone {
			continue
		}
		if !rapid.Bool().Draw(rt, "plain"+itemType.String()) {
			continue
		}
		p := randPlain{itemType: itemType}
		n := rapid.IntRange(0, 2).Draw(rt, "plainCols")
		for range n {
			k := rapid.SampledFrom(allKinds).Draw(rt, "plainKind")
			p.kinds = append(p.kinds, k)
			p.vals = append(p.vals, drawValue(rt, k, "plainValue"))
		}
		e.plains = append(e.plains, p)
	}
	for _, sig := range []string{"", "b", "s", "u64", "f32-f32_u64"} {
		if !rapid.Bool().Draw(rt, "slot"+sig) {
			continue
		}
		s := randSlot{
			sig:     sig,
			hasDisc: rapid.Bool().Draw(rt, "slotDisc"+sig),
			// A slot of two or three co-sections carries that many membership
			// groups; one is the flat form.
			nGroups: rapid.IntRange(1, 3).Draw(rt, "slotGroups"+sig),
		}
		nCols := rapid.IntRange(0, 2).Draw(rt, "slotCols")
		for range nCols {
			s.kinds = append(s.kinds, rapid.SampledFrom(allKinds).Draw(rt, "slotKind"))
		}
		nAttrs := rapid.IntRange(1, 3).Draw(rt, "nAttrs")
		for range nAttrs {
			a := randAttr{membs: make([][]randMemb, s.nGroups)}
			for g := range s.nGroups {
				nMemb := rapid.IntRange(0, 3).Draw(rt, "nMemberships")
				for range nMemb {
					a.membs[g] = append(a.membs[g], randMemb{
						ch:       rapid.SampledFrom(allChannels).Draw(rt, "channel"),
						ref:      rapid.Uint64().Draw(rt, "ref"),
						verbatim: rapid.SliceOfN(rapid.Byte(), 0, 3).Draw(rt, "verbatim"),
						params:   rapid.SliceOfN(rapid.Byte(), 0, 3).Draw(rt, "params"),
					})
				}
			}
			for _, k := range s.kinds {
				a.vals = append(a.vals, drawValue(rt, k, "value"))
			}
			if s.hasDisc {
				a.hasDisc = true
				a.disc = rapid.Uint64().Draw(rt, "discriminator")
			}
			s.attrs = append(s.attrs, a)
		}
		e.slots = append(e.slots, s)
	}
	return
}

// writeRandEntity encodes e with the same writers a generated encoder uses.
func writeRandEntity(t require.TestingT, e *randEntity, w io.Writer) {
	ew, err := NewEntityWriter()
	require.NoError(t, err)
	sw, err := NewSetWriter()
	require.NoError(t, err)
	ew.Begin()
	for i := range e.plains {
		p := &e.plains[i]
		cw := ew.BeginPlain(p.itemType, len(p.kinds))
		for j, k := range p.kinds {
			writeValue(cw, sw, k, p.vals[j])
		}
		ew.EndPlain()
	}
	for i := range e.slots {
		s := &e.slots[i]
		aw, err2 := NewAttributeWriter(len(s.kinds), s.nGroups)
		require.NoError(t, err2)
		for j := range s.attrs {
			a := &s.attrs[j]
			aw.Begin()
			for g := range a.membs {
				for _, m := range a.membs[g] {
					aw.Membership(g, m.ch, m.ref, m.verbatim, m.params)
				}
			}
			for c, k := range s.kinds {
				writeValue(aw.BeginValue(), sw, k, a.vals[c])
				aw.EndValue()
			}
			if a.hasDisc {
				aw.Discriminator(a.disc)
			}
			at, err3 := aw.End()
			require.NoError(t, err3)
			ew.Slot(s.sig).Add(at)
		}
	}
	require.NoError(t, ew.Flush(w))
}

// readRandEntity decodes b the way a generated decoder would: the column kinds
// come from the spec, keyed by signature and by plain item type, because the
// wire carries neither.
func readRandEntity(t require.TestingT, b []byte, spec *randEntity) (e randEntity) {
	byPlain := make(map[common.PlainItemTypeE][]int, len(spec.plains))
	for i := range spec.plains {
		byPlain[spec.plains[i].itemType] = spec.plains[i].kinds
	}
	bySlot := make(map[string]*randSlot, len(spec.slots))
	for i := range spec.slots {
		bySlot[spec.slots[i].sig] = &spec.slots[i]
	}

	er := NewEntityReader(NewCborReader(b))
	ar := er.Attributes()
	er.Begin()
	for {
		itemType, nCols, ok := er.NextPlain()
		if !ok {
			break
		}
		kinds := byPlain[itemType]
		require.Len(t, kinds, nCols)
		p := randPlain{itemType: itemType, kinds: kinds}
		for _, k := range kinds {
			p.vals = append(p.vals, readValue(er.Reader(), k))
		}
		e.plains = append(e.plains, p)
	}
	for {
		sig, nAttrs, ok := er.NextSlot()
		if !ok {
			break
		}
		want, found := bySlot[string(sig)]
		require.True(t, found, "unknown signature %q", string(sig))
		kinds := want.kinds
		s := randSlot{sig: string(sig), kinds: kinds, nGroups: want.nGroups}
		for range nAttrs {
			hasDisc := ar.Begin(len(kinds), want.nGroups)
			s.hasDisc = hasDisc
			a := randAttr{membs: make([][]randMemb, want.nGroups), hasDisc: hasDisc}
			for g := range want.nGroups {
				nMemb := ar.NextGroup()
				for range nMemb {
					ch, ref, verbatim, params := ar.Membership()
					a.membs[g] = append(a.membs[g], randMemb{ch: ch, ref: ref, verbatim: bytes.Clone(verbatim), params: bytes.Clone(params)})
				}
			}
			for _, k := range kinds {
				a.vals = append(a.vals, readValue(ar.Reader(), k))
			}
			if hasDisc {
				a.disc = ar.Discriminator()
			}
			ar.End()
			require.NoError(t, er.Err())
			s.attrs = append(s.attrs, a)
		}
		e.slots = append(e.slots, s)
	}
	er.End()
	require.NoError(t, er.Err())
	require.Zero(t, er.Remaining())
	return
}

// The M2 self-check in miniature: writer → bytes → VerifyCanonical → reader →
// writer, with the second pass byte-identical to the first.
func TestEntityRoundTripProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		spec := drawEntity(rt)
		var first bytes.Buffer
		writeRandEntity(rt, &spec, &first)
		require.NoError(rt, VerifyCanonical(first.Bytes()))

		back := readRandEntity(rt, first.Bytes(), &spec)
		var second bytes.Buffer
		writeRandEntity(rt, &back, &second)
		require.Equal(rt, hex.EncodeToString(first.Bytes()), hex.EncodeToString(second.Bytes()))
	})
}
