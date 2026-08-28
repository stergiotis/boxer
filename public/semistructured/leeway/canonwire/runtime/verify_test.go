package runtime

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// The raw builders below assemble an entity item element by element, without
// the sorting the writers apply — which is the only way to produce the
// non-canonical bytes VerifyCanonical has to refuse.

type rawPlain struct {
	key  uint64
	vals [][]byte
}

type rawSlot struct {
	sig   string
	attrs [][]byte
}

func rawEncode(t require.TestingT, f func(cw *CborWriter)) (b []byte) {
	var buf bytes.Buffer
	cw, err := NewCborWriter(&buf)
	require.NoError(t, err)
	f(cw)
	require.NoError(t, cw.Err())
	return buf.Bytes()
}

func rawVal(t require.TestingT, f func(cw *CborWriter)) (b []byte) { return rawEncode(t, f) }

func rawMemb(t require.TestingT, ch mappingplan.MembershipChannel, ref uint64) (b []byte) {
	return rawEncode(t, func(cw *CborWriter) { cw.WriteMembership(ch, ref, nil, nil) })
}

func rawAttr(t require.TestingT, membs [][]byte, vals ...[]byte) (b []byte) {
	return rawEncode(t, func(cw *CborWriter) {
		cw.ArrayHead(1 + len(vals))
		cw.ArrayHead(len(membs))
		for _, m := range membs {
			cw.Write(m)
		}
		for _, v := range vals {
			cw.Write(v)
		}
	})
}

// rawAttrGroups builds an attribute whose memberships element is the co-section
// form of ADR-0207 SD3: one array per membership group, in signature order.
func rawAttrGroups(t require.TestingT, groups [][][]byte, vals ...[]byte) (b []byte) {
	return rawEncode(t, func(cw *CborWriter) {
		cw.ArrayHead(1 + len(vals))
		cw.ArrayHead(len(groups))
		for _, g := range groups {
			cw.ArrayHead(len(g))
			for _, m := range g {
				cw.Write(m)
			}
		}
		for _, v := range vals {
			cw.Write(v)
		}
	})
}

func rawEntity(t require.TestingT, version uint64, plains []rawPlain, slots []rawSlot) (b []byte) {
	return rawEncode(t, func(cw *CborWriter) {
		cw.ArrayHead(3)
		cw.WriteUint(version)
		cw.MapHead(len(plains))
		for _, p := range plains {
			cw.WriteUint(p.key)
			cw.ArrayHead(len(p.vals))
			for _, v := range p.vals {
				cw.Write(v)
			}
		}
		cw.MapHead(len(slots))
		for _, s := range slots {
			cw.WriteTextString(s.sig)
			cw.ArrayHead(len(s.attrs))
			for _, a := range s.attrs {
				cw.Write(a)
			}
		}
	})
}

// uintVal is the shortest value there is, for vectors whose values are beside
// the point.
func uintVal(t require.TestingT, v uint64) (b []byte) {
	return rawVal(t, func(cw *CborWriter) { cw.WriteUint(v) })
}

// The golden of TestEntityGolden passes unchanged. Every other entity the
// writers emit is verified where it is written: flushHex checks each one.
func TestVerifyCanonicalAcceptsTheGolden(t *testing.T) {
	raw, err := hex.DecodeString("8301a10181182aa26173818281820001626869637536348182818201416d07")
	require.NoError(t, err)
	require.NoError(t, VerifyCanonical(raw))
}

// The value forms of SD3, each in a slot of its own, straight from the
// writers: containers, null, floats and the tagged lanes all pass.
func TestVerifyCanonicalAcceptsTheValueForms(t *testing.T) {
	var b bytes.Buffer
	ew, err := NewEntityWriter()
	require.NoError(t, err)
	sw, err := NewSetWriter()
	require.NoError(t, err)
	aw, err := NewAttributeWriter(1, 1)
	require.NoError(t, err)
	ew.Begin()

	add := func(sig string, card uint64, f func(cw *CborWriter)) {
		aw.Begin()
		aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 1, nil, nil)
		f(aw.BeginValue())
		aw.EndValue()
		a, err2 := aw.End()
		require.NoError(t, err2)
		ew.Slot(sig).Add(a)
	}
	add("f64", 1, func(cw *CborWriter) { cw.WriteF64(-0.0) })
	add("b", 1, func(cw *CborWriter) { cw.WriteBool(true) })
	add("n", 0, func(cw *CborWriter) { cw.WriteNull() })
	add("sh", 2, func(cw *CborWriter) {
		cw.ArrayHead(2)
		cw.WriteTextString("a")
		cw.WriteTextString("b")
	})
	add("um", 4, func(cw *CborWriter) {
		sw.Begin()
		for _, v := range []uint64{9, 1, 5, 9} {
			sw.Elem().WriteUint(v)
			sw.EndElem()
		}
		sw.Flush(cw)
	})
	require.NoError(t, ew.Flush(&b))
	require.NoError(t, VerifyCanonical(b.Bytes()))

	// The set kept its duplicate, so its cardinality is four and the two equal
	// adjacent elements passed the non-decreasing check inside it.
	n, err := VerifyCanonicalSequence(b.Bytes())
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestVerifyCanonicalRejectsHandMutatedEntities(t *testing.T) {
	lo := rawMemb(t, mappingplan.MembershipChannelLowCardRef, 1)
	hi := rawMemb(t, mappingplan.MembershipChannelHighCardRef, 1)
	one := uintVal(t, 1)
	two := uintVal(t, 2)

	cases := []struct {
		name string
		raw  []byte
		is   error
	}{
		{
			name: "memberships out of order",
			raw:  rawEntity(t, Version, nil, []rawSlot{{sig: "u64", attrs: [][]byte{rawAttr(t, [][]byte{hi, lo}, one)}}}),
			is:   ErrNotCanonical,
		},
		{
			name: "attributes out of order",
			raw: rawEntity(t, Version, nil, []rawSlot{{sig: "u64", attrs: [][]byte{
				rawAttr(t, [][]byte{lo}, two),
				rawAttr(t, [][]byte{lo}, one),
			}}}),
			is: ErrNotCanonical,
		},
		{
			name: "an empty slot",
			raw:  rawEntity(t, Version, nil, []rawSlot{{sig: "u64"}}),
			is:   ErrNotCanonical,
		},
		{
			name: "slot keys not length-first",
			raw: rawEntity(t, Version, nil, []rawSlot{
				{sig: "f64", attrs: [][]byte{rawAttr(t, [][]byte{lo}, one)}},
				{sig: "s", attrs: [][]byte{rawAttr(t, [][]byte{lo}, one)}},
			}),
			is: ErrNotCanonical,
		},
		{
			name: "slot keys repeated",
			raw: rawEntity(t, Version, nil, []rawSlot{
				{sig: "s", attrs: [][]byte{rawAttr(t, [][]byte{lo}, one)}},
				{sig: "s", attrs: [][]byte{rawAttr(t, [][]byte{lo}, one)}},
			}),
			is: ErrNotCanonical,
		},
		{
			name: "plain keys out of order",
			raw: rawEntity(t, Version, []rawPlain{
				{key: uint64(common.PlainItemTypeTransaction), vals: [][]byte{one}},
				{key: uint64(common.PlainItemTypeEntityId), vals: [][]byte{one}},
			}, nil),
			is: ErrNotCanonical,
		},
		{
			name: "a discriminator next to an attribute without one",
			raw: rawEntity(t, Version, nil, []rawSlot{{sig: "u64", attrs: [][]byte{
				rawAttr(t, [][]byte{lo}, one),
				rawAttr(t, [][]byte{lo}, one, uintVal(t, 0)),
			}}}),
			is: ErrNotCanonical,
		},
		{
			name: "set elements out of order",
			raw: rawEntity(t, Version, nil, []rawSlot{{sig: "um", attrs: [][]byte{
				rawAttr(t, [][]byte{lo}, rawVal(t, func(cw *CborWriter) {
					cw.Tag(TagSet)
					cw.ArrayHead(2)
					cw.WriteUint(2)
					cw.WriteUint(1)
				})),
			}}}),
			is: ErrNotCanonical,
		},
		{
			name: "a wrong version",
			raw:  rawEntity(t, Version+1, nil, nil),
			is:   ErrVersion,
		},
		{
			name: "not a three-element array",
			raw:  rawEncode(t, func(cw *CborWriter) { cw.ArrayHead(2); cw.WriteUint(Version); cw.MapHead(0) }),
			is:   ErrOutOfRange,
		},
		{
			name: "an unknown membership channel",
			raw: rawEntity(t, Version, nil, []rawSlot{{sig: "u64", attrs: [][]byte{
				rawAttr(t, [][]byte{rawEncode(t, func(cw *CborWriter) {
					cw.ArrayHead(2)
					cw.WriteUint(uint64(len(allChannels)))
					cw.WriteUint(0)
				})}, one),
			}}}),
			is: ErrUnknownChannel,
		},
	}
	for _, tc := range cases {
		require.ErrorIs(t, VerifyCanonical(tc.raw), tc.is, tc.name)
	}
}

// A co-section slot carries one membership array per section. The checker tells
// that form apart from the flat one without a table: an element of the
// memberships array that is empty, or whose own first element is an array,
// cannot be a membership.
func TestVerifyCanonicalAcceptsCoGroupMemberships(t *testing.T) {
	lo := rawMemb(t, mappingplan.MembershipChannelLowCardRef, 1)
	hi := rawMemb(t, mappingplan.MembershipChannelHighCardRef, 1)
	one := uintVal(t, 1)
	raw := rawEntity(t, Version, nil, []rawSlot{{sig: "u64_u64", attrs: [][]byte{
		// Two memberships, one group of them empty.
		rawAttrGroups(t, [][][]byte{{lo, hi}, {}}, one),
		// Three, spread over both groups: more memberships, so it sorts after.
		rawAttrGroups(t, [][][]byte{{lo}, {lo, hi}}, one),
	}}})
	require.NoError(t, VerifyCanonical(raw))

	// Both groups empty is still the grouped form — the shape is what says so.
	empty := rawEntity(t, Version, nil, []rawSlot{{sig: "u64_u64", attrs: [][]byte{
		rawAttrGroups(t, [][][]byte{{}, {}}, one),
	}}})
	require.NoError(t, VerifyCanonical(empty))
}

func TestVerifyCanonicalRejectsMalformedCoGroups(t *testing.T) {
	lo := rawMemb(t, mappingplan.MembershipChannelLowCardRef, 1)
	hi := rawMemb(t, mappingplan.MembershipChannelHighCardRef, 1)
	one := uintVal(t, 1)
	cases := []struct {
		name string
		raw  []byte
		is   error
	}{
		{
			name: "a group's memberships out of order",
			raw: rawEntity(t, Version, nil, []rawSlot{{sig: "u64_u64", attrs: [][]byte{
				rawAttrGroups(t, [][][]byte{{hi, lo}, {}}, one),
			}}}),
			is: ErrNotCanonical,
		},
		{
			name: "one group wrapped instead of written flat",
			raw: rawEntity(t, Version, nil, []rawSlot{{sig: "u64", attrs: [][]byte{
				rawAttrGroups(t, [][][]byte{{lo}}, one),
			}}}),
			is: ErrNotCanonical,
		},
		{
			name: "attributes of one slot differ in group count",
			raw: rawEntity(t, Version, nil, []rawSlot{{sig: "u64_u64", attrs: [][]byte{
				rawAttr(t, [][]byte{lo}, one),
				rawAttrGroups(t, [][][]byte{{lo}, {hi}}, one),
			}}}),
			is: ErrNotCanonical,
		},
	}
	for _, tc := range cases {
		require.ErrorIs(t, VerifyCanonical(tc.raw), tc.is, tc.name)
	}
}

// The same attribute twice is a duplicate, not a violation: SD3 keeps them and
// they stay adjacent.
func TestVerifyCanonicalAcceptsDuplicateAttributes(t *testing.T) {
	lo := rawMemb(t, mappingplan.MembershipChannelLowCardRef, 1)
	one := uintVal(t, 1)
	raw := rawEntity(t, Version, nil, []rawSlot{{sig: "u64", attrs: [][]byte{
		rawAttr(t, [][]byte{lo, lo}, one),
		rawAttr(t, [][]byte{lo, lo}, one),
	}}})
	require.NoError(t, VerifyCanonical(raw))
}

// Trailing bytes are a sequence, not an entity: VerifyCanonical refuses them
// and VerifyCanonicalSequence counts them.
func TestVerifyCanonicalAndSequenceDisagreeOnTrailingBytes(t *testing.T) {
	one := rawEntity(t, Version, nil, nil)
	two := append(bytes.Clone(one), one...)
	require.ErrorIs(t, VerifyCanonical(two), ErrOutOfRange)

	n, err := VerifyCanonicalSequence(two)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	// A sequence stops at the first entity that does not verify, and reports
	// how many came before it.
	bad := append(bytes.Clone(one), rawEntity(t, Version+1, nil, nil)...)
	n, err = VerifyCanonicalSequence(bad)
	require.ErrorIs(t, err, ErrVersion)
	require.Equal(t, 1, n)
}
