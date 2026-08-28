package runtime

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// flushHex runs f against a fresh entity writer and returns the hex of the
// entity item it flushed.
func flushHex(t require.TestingT, f func(ew *EntityWriter, aw *AttributeWriter)) string {
	var b bytes.Buffer
	ew, err := NewEntityWriter()
	require.NoError(t, err)
	aw, err := NewAttributeWriter(1, 1)
	require.NoError(t, err)
	ew.Begin()
	f(ew, aw)
	require.NoError(t, ew.Flush(&b))
	// Everything the writers emit is canonical, so every entity these tests
	// build is checked against the table-free rules on its way out.
	require.NoError(t, VerifyCanonical(b.Bytes()))
	return hex.EncodeToString(b.Bytes())
}

// A small hand-built entity, byte for byte:
//
//	83                     array(3)
//	01                       version
//	a1                       map(1) — the plains
//	01                         key: PlainItemTypeEntityId
//	81 182a                    [42]
//	a2                       map(2) — the tagged slots
//	6173                       key: "s"
//	81                           one attribute
//	82 81 820001 626869            [[[0, 1]], "hi"]
//	63 753634                  key: "u64"
//	81                           one attribute
//	82 81 8201416d 07              [[[1, 'm']], 7]
func TestEntityGolden(t *testing.T) {
	got := flushHex(t, func(ew *EntityWriter, aw *AttributeWriter) {
		ew.BeginPlain(common.PlainItemTypeEntityId, 1).WriteUint(42)
		ew.EndPlain()

		aw.Begin()
		aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 1, nil, nil)
		aw.BeginValue().WriteTextString("hi")
		aw.EndValue()
		a, err := aw.End()
		require.NoError(t, err)
		ew.Slot("s").Add(a)

		aw.Begin()
		aw.Membership(0, mappingplan.MembershipChannelLowCardVerbatim, 0, []byte("m"), nil)
		aw.BeginValue().WriteUint(7)
		aw.EndValue()
		a, err = aw.End()
		require.NoError(t, err)
		ew.Slot("u64").Add(a)
	})
	require.Equal(t, "8301a10181182aa26173818281820001626869637536348182818201416d07", got)

	raw, err := hex.DecodeString(got)
	require.NoError(t, err)
	var ent any
	require.NoError(t, cbor.Unmarshal(raw, &ent))
	require.Equal(t, []any{
		Version,
		map[any]any{uint64(common.PlainItemTypeEntityId): []any{uint64(42)}},
		map[any]any{
			"s":   []any{[]any{[]any{[]any{uint64(0), uint64(1)}}, "hi"}},
			"u64": []any{[]any{[]any{[]any{uint64(1), []byte("m")}}, uint64(7)}},
		},
	}, ent)
}

// A slot that took no attribute is not a key: an entity's tagged map carries
// only the slots it actually has (ADR-0207 SD2).
func TestEntityOmitsEmptySlots(t *testing.T) {
	got := flushHex(t, func(ew *EntityWriter, aw *AttributeWriter) {
		ew.Slot("s")
		ew.Slot("u64")
		aw.Begin()
		aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 0, nil, nil)
		aw.BeginValue().WriteUint(1)
		aw.EndValue()
		a, err := aw.End()
		require.NoError(t, err)
		ew.Slot("u64").Add(a)
	})
	// [1, {}, {"u64": [[[[0,0]], 1]]}]
	require.Equal(t, "8301a0a16375363481828182000001", got)
}

// RFC 8949 §4.2.3 sorts map keys by their *encoded* bytes, and a text key's
// encoding starts with its length — so "s" precedes "f64" even though "f64"
// precedes "s" lexicographically.
func TestEntityTaggedKeysSortLengthFirst(t *testing.T) {
	sigs := []string{"f32-f32_u64", "s", "", "f64", "b"}
	got := flushHex(t, func(ew *EntityWriter, aw *AttributeWriter) {
		for _, sig := range sigs {
			aw.Begin()
			aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 0, nil, nil)
			aw.BeginValue().WriteUint(0)
			aw.EndValue()
			a, err := aw.End()
			require.NoError(t, err)
			ew.Slot(sig).Add(a)
		}
	})
	raw, err := hex.DecodeString(got)
	require.NoError(t, err)
	// The diagnostic notation keeps the encoded order, which a decoded Go map
	// would not.
	diag, err := cbor.Diagnose(raw)
	require.NoError(t, err)
	want := []string{`""`, `"b"`, `"s"`, `"f64"`, `"f32-f32_u64"`}
	at := make([]int, len(want))
	for i, key := range want {
		at[i] = strings.Index(diag, key+": ")
		require.GreaterOrEqual(t, at[i], 0, "key %s not in %s", key, diag)
		if i > 0 {
			require.Less(t, at[i-1], at[i], "%s must precede %s in %s", want[i-1], key, diag)
		}
	}
}

// The order attributes and their memberships are handed to the writer in is
// representation, not content: shuffling either must not move a byte. This is
// the "attribute-permutation property" of the ADR's verification plan.
func TestEntityIsIndependentOfInsertionOrder(t *testing.T) {
	type memb struct {
		ch  mappingplan.MembershipChannel
		ref uint64
	}
	type attr struct {
		slot  string
		val   uint64
		membs []memb
	}
	attrs := []attr{
		{slot: "u64", val: 3, membs: []memb{{mappingplan.MembershipChannelLowCardRef, 1}, {mappingplan.MembershipChannelHighCardRef, 7}}},
		{slot: "u64", val: 1, membs: []memb{{mappingplan.MembershipChannelLowCardRef, 2}}},
		{slot: "u64", val: 3, membs: []memb{{mappingplan.MembershipChannelLowCardRef, 1}, {mappingplan.MembershipChannelHighCardRef, 7}}},
		{slot: "s", val: 0, membs: []memb{{mappingplan.MembershipChannelLowCardRef, 5}, {mappingplan.MembershipChannelLowCardRef, 4}, {mappingplan.MembershipChannelLowCardRef, 5}}},
		{slot: "s", val: 9, membs: []memb{{mappingplan.MembershipChannelLowCardRef, 0}}},
	}
	write := func(t require.TestingT, attrOrder []int, membOrder [][]int) string {
		return flushHex(t, func(ew *EntityWriter, aw *AttributeWriter) {
			ew.BeginPlain(common.PlainItemTypeEntityId, 1).WriteUint(1)
			ew.EndPlain()
			for k, ai := range attrOrder {
				a := attrs[ai]
				aw.Begin()
				for _, mi := range membOrder[k] {
					aw.Membership(0, a.membs[mi].ch, a.membs[mi].ref, nil, nil)
				}
				aw.BeginValue().WriteUint(a.val)
				aw.EndValue()
				at, err := aw.End()
				require.NoError(t, err)
				ew.Slot(a.slot).Add(at)
			}
		})
	}
	identity := make([]int, len(attrs))
	membIdentity := make([][]int, len(attrs))
	for i := range attrs {
		identity[i] = i
		membIdentity[i] = make([]int, len(attrs[i].membs))
		for j := range attrs[i].membs {
			membIdentity[i][j] = j
		}
	}
	want := write(t, identity, membIdentity)

	rapid.Check(t, func(rt *rapid.T) {
		attrOrder := rapid.Permutation(identity).Draw(rt, "attributeOrder")
		membOrder := make([][]int, len(attrOrder))
		for k, ai := range attrOrder {
			membOrder[k] = rapid.Permutation(membIdentity[ai]).Draw(rt, "membershipOrder")
		}
		require.Equal(rt, want, write(rt, attrOrder, membOrder))
	})
}

// A flushed entity leaves nothing behind: the next one starts empty, and the
// writer's arenas are reused rather than reallocated.
func TestEntityWriterResetsBetweenEntities(t *testing.T) {
	var b bytes.Buffer
	ew, err := NewEntityWriter()
	require.NoError(t, err)
	aw, err := NewAttributeWriter(1, 1)
	require.NoError(t, err)

	ew.Begin()
	ew.BeginPlain(common.PlainItemTypeEntityId, 1).WriteUint(1)
	ew.EndPlain()
	aw.Begin()
	aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 0, nil, nil)
	aw.BeginValue().WriteUint(1)
	aw.EndValue()
	a, err := aw.End()
	require.NoError(t, err)
	ew.Slot("u64").Add(a)
	require.NoError(t, ew.Flush(&b))
	first := hex.EncodeToString(b.Bytes())

	// The second entity carries nothing at all.
	b.Reset()
	require.NoError(t, ew.Flush(&b))
	require.Equal(t, "8301a0a0", hex.EncodeToString(b.Bytes()))

	// And a repeat of the first is byte-identical.
	b.Reset()
	ew.BeginPlain(common.PlainItemTypeEntityId, 1).WriteUint(1)
	ew.EndPlain()
	aw.Begin()
	aw.Membership(0, mappingplan.MembershipChannelLowCardRef, 0, nil, nil)
	aw.BeginValue().WriteUint(1)
	aw.EndValue()
	a, err = aw.End()
	require.NoError(t, err)
	ew.Slot("u64").Add(a)
	require.NoError(t, ew.Flush(&b))
	require.Equal(t, first, hex.EncodeToString(b.Bytes()))
}

// The attribute bytes an entity holds survive the arena growing under them:
// the views a slot compares are bound at flush, not at Add.
func TestEntityArenaGrowthDoesNotStaleAttributes(t *testing.T) {
	var b bytes.Buffer
	ew, err := NewEntityWriter()
	require.NoError(t, err)
	aw, err := NewAttributeWriter(1, 1)
	require.NoError(t, err)
	ew.Begin()
	const n = 400
	for i := range n {
		aw.Begin()
		aw.Membership(0, mappingplan.MembershipChannelLowCardRef, uint64(n-i), nil, nil)
		cw := aw.BeginValue()
		cw.WriteBytes(bytes.Repeat([]byte{byte(i)}, 64))
		aw.EndValue()
		a, err2 := aw.End()
		require.NoError(t, err2)
		ew.Slot("y").Add(a)
	}
	require.NoError(t, ew.Flush(&b))

	var ent any
	require.NoError(t, cbor.Unmarshal(b.Bytes(), &ent))
	slot := ent.([]any)[2].(map[any]any)["y"].([]any)
	require.Len(t, slot, n)
	// Ordered by the membership ref, which counts down as i counts up.
	for i := range n {
		item := slot[i].([]any)
		membs := item[0].([]any)
		require.Equal(t, uint64(i+1), membs[0].([]any)[1])
		require.Equal(t, bytes.Repeat([]byte{byte(n - 1 - i)}, 64), item[1])
	}
}

// The plain map rejects a repeated item type — it is a map key — and the
// writer refuses the non-item-type zero value.
func TestEntityPlainSectionGuards(t *testing.T) {
	ew, err := NewEntityWriter()
	require.NoError(t, err)

	ew.Begin()
	ew.BeginPlain(common.PlainItemTypeEntityId, 0)
	ew.EndPlain()
	ew.BeginPlain(common.PlainItemTypeEntityId, 0)
	require.Error(t, ew.Err())

	ew.Begin()
	ew.BeginPlain(common.PlainItemTypeNone, 0)
	require.Error(t, ew.Err())

	ew.Begin()
	ew.BeginPlain(common.PlainItemTypeEntityId, 0)
	ew.BeginPlain(common.PlainItemTypeTransaction, 0)
	require.Error(t, ew.Err())

	ew.Begin()
	ew.BeginPlain(common.PlainItemTypeEntityId, 0)
	require.Error(t, ew.Flush(&bytes.Buffer{}))
}

// Plain keys are uints, and their encodings sort in ordinal order.
func TestEntityPlainKeysSortByOrdinal(t *testing.T) {
	got := flushHex(t, func(ew *EntityWriter, aw *AttributeWriter) {
		ew.BeginPlain(common.PlainItemTypeOpaque, 1).WriteUint(6)
		ew.EndPlain()
		ew.BeginPlain(common.PlainItemTypeEntityTimestamp, 1).WriteUint(2)
		ew.EndPlain()
		ew.BeginPlain(common.PlainItemTypeEntityId, 1).WriteUint(1)
		ew.EndPlain()
	})
	// [1, {1: [1], 2: [2], 6: [6]}, {}]
	require.Equal(t, "8301a3018101028102068106a0", got)
}

func TestCompareTextKeys(t *testing.T) {
	require.Negative(t, compareTextKeys("", "b"))
	require.Negative(t, compareTextKeys("s", "f64"))
	require.Negative(t, compareTextKeys("b", "s"))
	require.Zero(t, compareTextKeys("s", "s"))
	require.Positive(t, compareTextKeys("f32-f32_u64", "f64"))
}
