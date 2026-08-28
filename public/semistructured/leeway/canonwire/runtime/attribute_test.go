package runtime

import (
	"bytes"
	"encoding/hex"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// cloneAttr copies an Attr out of the AttributeWriter's scratch so it survives
// the writer's next Begin. Production code hands the Attr to SlotWriter.Add,
// which does the same copy into the entity arena.
func cloneAttr(a Attr) (out Attr) {
	item := slices.Clone(a.Item)
	off := len(a.Item) - len(a.Memb) - len(a.Vals)
	return Attr{
		Key: AttributeKey{
			MembershipCount: a.Key.MembershipCount,
			Cards:           slices.Clone(a.Key.Cards),
		},
		Item: item,
		Memb: item[off : off+len(a.Memb)],
		Vals: item[off+len(a.Memb):],
	}
}

// buildAttr runs f against a fresh single-group attribute of nCols columns and
// returns the finished Attr, detached from the writer.
func buildAttr(t *testing.T, nCols int, f func(aw *AttributeWriter)) (out Attr) {
	t.Helper()
	return buildAttrGroups(t, nCols, 1, f)
}

// buildAttrGroups is buildAttr for a slot of nGroups co-sections.
func buildAttrGroups(t *testing.T, nCols int, nGroups int, f func(aw *AttributeWriter)) (out Attr) {
	t.Helper()
	aw, err := NewAttributeWriter(nCols, nGroups)
	require.NoError(t, err)
	aw.Begin()
	f(aw)
	a, err := aw.End()
	require.NoError(t, err)
	return cloneAttr(a)
}

// The item is [memberships, v_1, …, v_n], the memberships array first.
func TestAttributeItemShape(t *testing.T) {
	a := buildAttr(t, 1, func(aw *AttributeWriter) {
		aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 3, nil, nil)
		aw.BeginValue().WriteTextString("a")
		aw.EndValue()
	})
	require.Equal(t, "82818200036161", hex.EncodeToString(a.Item))
	require.Equal(t, "81820003", hex.EncodeToString(a.Memb))
	require.Equal(t, "6161", hex.EncodeToString(a.Vals))
	require.Equal(t, uint32(1), a.Key.MembershipCount)
	require.Equal(t, []uint64{1}, a.Key.Cards)
}

// A value-less slot's attribute is [memberships] and carries no cardinalities.
func TestAttributeValueLessSlot(t *testing.T) {
	a := buildAttr(t, 0, func(aw *AttributeWriter) {
		aw.Membership(0, mappingplan.MembershipChannelLowCardVerbatim, 0, []byte("p"), nil)
	})
	require.Equal(t, "818182014170", hex.EncodeToString(a.Item))
	require.Empty(t, a.Vals)
	require.Empty(t, a.Key.Cards)
}

// Memberships are sorted bytewise on their encoded items, so insertion order
// does not reach the wire (ADR-0207 SD3).
func TestAttributeMembershipsSortedRegardlessOfInsertionOrder(t *testing.T) {
	add := func(aw *AttributeWriter, order []int) {
		for _, i := range order {
			switch i {
			case 0:
				aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 9, nil, nil)
			case 1:
				aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 2, nil, nil)
			case 2:
				aw.Membership(0, mappingplan.MembershipChannelHighCardVerbatim, 0, []byte("zz"), nil)
			case 3:
				aw.Membership(0, mappingplan.MembershipChannelMixedLowCardRef, 1, nil, []byte("p"))
			}
		}
	}
	want := buildAttr(t, 0, func(aw *AttributeWriter) { add(aw, []int{0, 1, 2, 3}) })
	for _, order := range [][]int{{3, 2, 1, 0}, {1, 3, 0, 2}, {2, 0, 3, 1}} {
		got := buildAttr(t, 0, func(aw *AttributeWriter) { add(aw, order) })
		require.Equal(t, hex.EncodeToString(want.Item), hex.EncodeToString(got.Item))
	}
	// [0,2] < [0,9] < [3,"zz"] < [4,[1,"p"]]: the channel ordinal leads, then
	// the identity's own bytes.
	require.Equal(t, "848200028200098203427a7a820482014170", hex.EncodeToString(want.Memb))
	require.Equal(t, uint32(4), want.Key.MembershipCount)
}

// Duplicate memberships are kept and stay adjacent — aliasing that repeats is
// content, not noise.
func TestAttributeDuplicateMembershipsKept(t *testing.T) {
	a := buildAttr(t, 0, func(aw *AttributeWriter) {
		aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 4, nil, nil)
		aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 1, nil, nil)
		aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 4, nil, nil)
	})
	require.Equal(t, uint32(3), a.Key.MembershipCount)
	require.Equal(t, "83820001820004820004", hex.EncodeToString(a.Memb))
}

// The cardinalities are derived from the value forms, in key order: a present
// scalar is 1, a null one 0, and a container its element count (ADR-0207 SD3).
func TestAttributeCardinalities(t *testing.T) {
	a := buildAttr(t, 3, func(aw *AttributeWriter) {
		aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 1, nil, nil)
		// A present scalar.
		aw.BeginValue().WriteUint(5)
		aw.EndValue()
		// A null one.
		aw.BeginValue().WriteNull()
		aw.EndValue()
		// An `h` array of three elements.
		cw := aw.BeginValue()
		cw.ArrayHead(3)
		cw.WriteUint(1)
		cw.WriteUint(2)
		cw.WriteUint(3)
		aw.EndValue()
	})
	require.Equal(t, []uint64{1, 0, 3}, a.Key.Cards)
	require.Equal(t, "05f683010203", hex.EncodeToString(a.Vals))
}

// The discriminator is a trailing element of the attribute item; it lengthens
// the outer array and rides in the compared values range.
func TestAttributeDiscriminator(t *testing.T) {
	with := buildAttr(t, 1, func(aw *AttributeWriter) {
		aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 1, nil, nil)
		aw.BeginValue().WriteUint(0)
		aw.EndValue()
		aw.Discriminator(1)
	})
	without := buildAttr(t, 1, func(aw *AttributeWriter) {
		aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 1, nil, nil)
		aw.BeginValue().WriteUint(0)
		aw.EndValue()
	})
	require.Equal(t, "828182000100", hex.EncodeToString(without.Item))
	require.Equal(t, "83818200010001", hex.EncodeToString(with.Item))
	require.Equal(t, "00", hex.EncodeToString(without.Vals))
	require.Equal(t, "0001", hex.EncodeToString(with.Vals))
}

// The canonical order: membership count, then cardinalities, then the
// memberships' bytes, then the values' bytes (ADR-0207 SD3).
func TestCompareAttributes(t *testing.T) {
	mk := func(nMemb uint32, cards []uint64, memb string, vals string) (a Attr) {
		mb, err := hex.DecodeString(memb)
		require.NoError(t, err)
		vb, err := hex.DecodeString(vals)
		require.NoError(t, err)
		item := append(append([]byte{0x82}, mb...), vb...)
		return Attr{
			Key:  AttributeKey{MembershipCount: nMemb, Cards: cards},
			Item: item,
			Memb: item[1 : 1+len(mb)],
			Vals: item[1+len(mb):],
		}
	}

	// Fewer memberships first, whatever the bytes say.
	one := mk(1, []uint64{9}, "81820009", "ff")
	two := mk(2, []uint64{0}, "82820000820000", "00")
	require.Negative(t, CompareAttributes(&one, &two))
	require.Positive(t, CompareAttributes(&two, &one))

	// Then the cardinalities, lexicographically.
	small := mk(1, []uint64{1, 5}, "81820009", "ff")
	large := mk(1, []uint64{2, 0}, "81820000", "00")
	require.Negative(t, CompareAttributes(&small, &large))

	// Then the memberships' bytes.
	membLo := mk(1, []uint64{1}, "81820000", "ff")
	membHi := mk(1, []uint64{1}, "81820009", "00")
	require.Negative(t, CompareAttributes(&membLo, &membHi))

	// Then the values' bytes.
	valLo := mk(1, []uint64{1}, "81820000", "00")
	valHi := mk(1, []uint64{1}, "81820000", "01")
	require.Negative(t, CompareAttributes(&valLo, &valHi))

	// Equal under all four: duplicates, which stay adjacent.
	dup := mk(1, []uint64{1}, "81820000", "00")
	require.Zero(t, CompareAttributes(&valLo, &dup))
}

func TestAttributeWriterRejectsMalformedUse(t *testing.T) {
	aw, err := NewAttributeWriter(1, 1)
	require.NoError(t, err)

	// Too few values.
	aw.Begin()
	aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 1, nil, nil)
	_, err = aw.End()
	require.Error(t, err)

	// Too many values.
	aw.Begin()
	aw.BeginValue().WriteUint(1)
	aw.EndValue()
	aw.BeginValue()
	require.Error(t, aw.Err())

	// A value left open.
	aw.Begin()
	aw.BeginValue().WriteUint(1)
	_, err = aw.End()
	require.Error(t, err)

	// A membership for a group the slot does not have.
	aw.Begin()
	aw.Membership(1, mappingplan.MembershipChannelLowCardRef, 0, nil, nil)
	require.Error(t, aw.Err())

	// An unknown channel.
	aw.Begin()
	aw.Membership(0, unknownChannel, 0, nil, nil)
	require.Error(t, aw.Err())

	_, err = NewAttributeWriter(-1, 1)
	require.Error(t, err)
	_, err = NewAttributeWriter(1, 0)
	require.Error(t, err)
}

// A slot of k > 1 co-sections carries k membership arrays in signature order,
// each sorted on its own (ADR-0207 SD3). An empty group is written as an empty
// array, not omitted.
func TestAttributeCoGroupMemberships(t *testing.T) {
	a := buildAttrGroups(t, 0, 2, func(aw *AttributeWriter) {
		aw.Membership(1, mappingplan.MembershipChannelLowCardRef, 4, nil, nil)
		aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 9, nil, nil)
		aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 2, nil, nil)
	})
	// [[[0,2],[0,9]],[[0,4]]] — sorted within each group, the groups in order.
	require.Equal(t, "828282000282000981820004", hex.EncodeToString(a.Memb))
	require.Equal(t, uint32(3), a.Key.MembershipCount)

	// A group with no memberships is still a group.
	empty := buildAttrGroups(t, 0, 2, func(aw *AttributeWriter) {
		aw.Membership(1, mappingplan.MembershipChannelLowCardRef, 1, nil, nil)
	})
	require.Equal(t, "828081820001", hex.EncodeToString(empty.Memb))
	require.Equal(t, uint32(1), empty.Key.MembershipCount)

	// Two empty groups: the shape says "two groups", which is what tells a
	// table-free reader the co-section form apart from the flat one.
	both := buildAttrGroups(t, 0, 2, func(aw *AttributeWriter) {})
	require.Equal(t, "828080", hex.EncodeToString(both.Memb))
	require.Zero(t, both.Key.MembershipCount)
}

// DeriveCardinality is the single definition of SD3's cardinality rule.
func TestDeriveCardinality(t *testing.T) {
	enc := func(f func(cw *CborWriter)) []byte {
		var b bytes.Buffer
		cw, err := NewCborWriter(&b)
		require.NoError(t, err)
		f(cw)
		require.NoError(t, cw.Err())
		return b.Bytes()
	}
	cases := []struct {
		name string
		item []byte
		want uint64
	}{
		{name: "uint", item: enc(func(cw *CborWriter) { cw.WriteUint(7) }), want: 1},
		{name: "null", item: enc(func(cw *CborWriter) { cw.WriteNull() }), want: 0},
		{name: "bool", item: enc(func(cw *CborWriter) { cw.WriteBool(false) }), want: 1},
		{name: "empty array", item: enc(func(cw *CborWriter) { cw.ArrayHead(0) }), want: 0},
		{name: "array", item: enc(func(cw *CborWriter) {
			cw.ArrayHead(3)
			cw.WriteUint(1)
			cw.WriteUint(2)
			cw.WriteUint(3)
		}), want: 3},
		{name: "temporal", item: enc(func(cw *CborWriter) { cw.WriteTemporal(time.Unix(1, 0)) }), want: 1},
		{name: "ipv4", item: enc(func(cw *CborWriter) { cw.WriteIPv4Raw(0x01020304) }), want: 1},
	}
	for _, tc := range cases {
		got, err := DeriveCardinality(tc.item)
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.want, got, tc.name)
	}

	sw, err := NewSetWriter()
	require.NoError(t, err)
	set := enc(func(cw *CborWriter) {
		sw.Begin()
		for _, v := range []uint64{5, 1, 5} {
			sw.Elem().WriteUint(v)
			sw.EndElem()
		}
		sw.Flush(cw)
	})
	// The set kept its duplicate, so its cardinality is the number of elements
	// offered — which is what makes it a co-container of the section's arrays.
	got, err := DeriveCardinality(set)
	require.NoError(t, err)
	require.Equal(t, uint64(3), got)

	_, err = DeriveCardinality(nil)
	require.Error(t, err)
}
