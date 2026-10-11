package bindings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIdDerivationIsInjective pins the property the whole response
// read-back path rests on: two widgets built from different ids must put
// different ids on the wire. r7 responses are a flat map keyed by that id
// and Sync compacts duplicates newest-wins, so a collapse costs the
// earlier widget its flags entirely — it renders, egui hit-tests it on its
// own auto-id and takes the click, and SendResp then reads nothing.
//
// The regression this guards is a normalisation that OR-ed bit 0 into
// every derived id, merging each even id with its odd successor.
// Consecutive integers — the obvious way to number a few buttons — were
// the worst case.
func TestIdDerivationIsInjective(t *testing.T) {
	for _, base := range []uint64{0, 1, 0x1000, 0xdeadbee0, 0xbb00ff0000, ^uint64(0) - 4} {
		seen := make(map[uint64]uint64, 8)
		for off := range uint64(5) {
			id := MakeAbsoluteIdHighEntropy(base + off).Derive()
			if prev, dup := seen[id]; dup {
				t.Errorf("MakeAbsoluteIdHighEntropy: %#x and %#x both derive %#x",
					base+prev, base+off, id)
			}
			seen[id] = off
		}

		// Same guarantee for the scope-relative spelling.
		ids := NewWidgetIdStack()
		seen = make(map[uint64]uint64, 8)
		for off := range uint64(5) {
			id := ids.PrepareHighEntropy(base + off).Derive()
			if prev, dup := seen[id]; dup {
				t.Errorf("PrepareHighEntropy: %#x and %#x both derive %#x",
					base+prev, base+off, id)
			}
			seen[id] = off
		}
	}

	// Sequence ids stay injective too: makeHighEntropy is a bijection, and
	// nothing downstream may fold two of its outputs together.
	seq := make(map[uint64]uint64, 1<<16)
	for i := range uint64(1 << 16) {
		id := MakeAbsoluteIdSeq(i).Derive()
		if prev, dup := seq[id]; dup {
			t.Fatalf("MakeAbsoluteIdSeq: %d and %d both derive %#x", prev, i, id)
		}
		seq[id] = i
	}
}

// TestScopedIdsAreOrderSensitive pins that a scope and the id inside it do
// not commute: a widget keyed by two ordinals — row and column, node and
// column — must not share an id with the one whose ordinals are swapped,
// and a widget keyed like its own scope must not cancel back to the scope's
// parent. Both held for a plain XOR of scope and id.
func TestScopedIdsAreOrderSensitive(t *testing.T) {
	const n = 64
	ids := NewWidgetIdStack()
	seen := make(map[uint64][2]uint64, n*n)
	for range IdScope(ids.PrepareStr("cell")) {
		root := ids.PrepareStr("cell").Derive()
		for a := range uint64(n) {
			for range IdScope(ids.PrepareSeq(a)) {
				for b := range uint64(n) {
					id := ids.PrepareSeq(b).Derive()
					if prev, dup := seen[id]; dup {
						t.Fatalf("Seq(%d)/Seq(%d) and Seq(%d)/Seq(%d) both derive %#x", prev[0], prev[1], a, b, id)
					}
					seen[id] = [2]uint64{a, b}
					if a == b {
						assert.NotEqual(t, root, id, "Seq(%d) inside Seq(%d) cancelled to the parent", b, a)
					}
				}
			}
		}
	}
	assert.Zero(t, ids.Depth(), "scopes pop back to the empty stack")
}

// TestRootRelativeIdEqualsAbsolute pins that the empty, unsalted stack
// leaves an id as it is: a relative id at the root and the absolute id of
// the same value put the same id on the wire.
func TestRootRelativeIdEqualsAbsolute(t *testing.T) {
	ids := NewWidgetIdStack()
	assert.Equal(t, MakeAbsoluteIdStr("topbar").Derive(), ids.PrepareStr("topbar").Derive())
	assert.Equal(t, MakeAbsoluteIdSeq(7).Derive(), ids.PrepareSeq(7).Derive())
	assert.Equal(t, MakeAbsoluteIdHighEntropy(0xdeadbeef).Derive(), ids.PrepareHighEntropy(0xdeadbeef).Derive())
}

// TestAbsoluteIdRawEqualsDerived pins that an AbsoluteWidgetId's numeric
// value is the id that goes on the wire. Several call sites key their own
// side tables with uint64(absId) (probe seqs, retained per-widget state)
// while the framework keys the wire with Derive(); the two must not drift.
func TestAbsoluteIdRawEqualsDerived(t *testing.T) {
	for _, s := range []string{"ctx-fit-x", "ctx-fit-y", "ctx-fit-both", "ctx-close", "", "a"} {
		id := MakeAbsoluteIdStr(s)
		assert.Equal(t, uint64(id), id.Derive(), "MakeAbsoluteIdStr(%q)", s)
	}
	for _, v := range []uint64{1, 2, 0xdeadbeef, 0xdeadbee0, ^uint64(0)} {
		id := MakeAbsoluteIdHighEntropy(v)
		assert.Equal(t, uint64(id), id.Derive(), "MakeAbsoluteIdHighEntropy(%#x)", v)
		assert.Equal(t, v, id.Derive(), "the caller's value is the id verbatim")
	}
	for _, i := range []uint64{0, 1, 2, 1 << 40} {
		id := MakeAbsoluteIdSeq(i)
		assert.Equal(t, uint64(id), id.Derive(), "MakeAbsoluteIdSeq(%d)", i)
	}
}

// TestDerivedIdIsNeverZero pins the one value egui rejects. The client
// builds every id with Id::from_high_entropy_bits, which panics on zero,
// so a zero id is a client crash rather than a misbehaving widget.
//
// The routes to it are a caller-supplied zero, and a relative id at the
// unsalted root, where the local id is the derived id.
func TestDerivedIdIsNeverZero(t *testing.T) {
	assert.Equal(t, zeroIdReplacement, MakeAbsoluteIdHighEntropy(0).Derive())
	assert.Equal(t, zeroIdReplacement, AbsoluteWidgetId(0).Derive())
	assert.Equal(t, zeroIdReplacement, NewWidgetIdStack().PrepareHighEntropy(0).Derive())

	ids := NewWidgetIdStack()
	outer := ids.PrepareStr("a").DeriveStacked()
	inner := ids.PrepareStr("a").Derive()
	assert.NotZero(t, inner, "same key nested under itself must not cancel to zero")
	ids.PopIdFromStackChecked(outer)

	salted := NewWidgetIdStack()
	salted.SetBaseSalt(hashLabelToId("topbar"))
	assert.NotZero(t, salted.PrepareStr("topbar").Derive(),
		"a salt equal to the label hash must not cancel to zero")
}

// TestWidgetIdStackBaseSalt pins the SetBaseSalt contract: the salt shifts
// every derived id, survives Reset (unlike a pushed scope), and zero keeps
// the unsalted behaviour byte-identical.
func TestWidgetIdStackBaseSalt(t *testing.T) {
	unsalted := NewWidgetIdStack()
	a := NewWidgetIdStack()
	a.SetBaseSalt(0x1111)
	b := NewWidgetIdStack()
	b.SetBaseSalt(0x2222)

	idPlain := unsalted.PrepareStr("topbar").Derive()
	idA := a.PrepareStr("topbar").Derive()
	idB := b.PrepareStr("topbar").Derive()
	assert.NotEqual(t, idPlain, idA)
	assert.NotEqual(t, idPlain, idB)
	assert.NotEqual(t, idA, idB, "same label under different salts must differ")
	assert.Equal(t, combineScopeId(0x1111, hashLabelToId("topbar")), idA,
		"the salt is the empty stack's base scope")

	// A zero salt is the legacy behaviour.
	z := NewWidgetIdStack()
	z.SetBaseSalt(0)
	assert.Equal(t, idPlain, z.PrepareStr("topbar").Derive())

	// Reset clears pushed scopes but keeps the base salt.
	scoped := a.PrepareStr("scope").DeriveStacked()
	assert.NotZero(t, scoped)
	a.Reset()
	assert.Equal(t, idA, a.PrepareStr("topbar").Derive(), "salt must survive Reset")

	// Scopes compose on top of the salt and pop back to it.
	base := b.PrepareStr("scope").DeriveStacked()
	inScope := b.PrepareStr("leaf").Derive()
	assert.Equal(t, combineScopeId(base, unsalted.PrepareStr("leaf").Derive()), inScope)
	b.PopIdFromStackChecked(base)
	assert.Equal(t, idB, b.PrepareStr("topbar").Derive())
}

// TestBlockIteratorBuilderLeavesTheStackAlone pins that building a block
// iterator does not open its scope: only ranging KeepIter does. A builder
// finished with Keep (or Send), or dropped, used to leave its id pushed for
// the rest of the frame, so every later sibling derived against the wrong
// base and its id drifted.
func TestBlockIteratorBuilderLeavesTheStackAlone(t *testing.T) {
	ids := NewWidgetIdStack()
	want := ids.PrepareStr("sibling").Derive()

	f := Frame(ids.PrepareStr("row"))
	_ = f.Keep()
	_ = CollapsingHeader(ids.PrepareStr("dropped"), WidgetText().Text("x").Keep())

	assert.Equal(t, 0, ids.Depth())
	assert.Equal(t, want, ids.PrepareStr("sibling").Derive())
}
