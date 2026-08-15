package marshallreflect_test

// The arity-evolution corpus (R7 in
// doc/adr-background-work/leeway-components-consumer-complexity.md): a
// contract may WIDEN after data exists — the admits-set only grows — and the
// widened definition must keep reading rows written under the narrower one.
// One test per rung of the ladder, write-narrow-read-wide, plus the reverse
// direction pinned as loud.
//
// Rung 1: required scalar [1..1] → option.Option [0..1] (same shape).
// Rung 2: unit scalar (one-element array value) → container []T, and the
//         coexistence case — wide reader over rows from BOTH generations.
// Rung 3: the shape crossing — a scalar slot read under a dynamic-membership
//         tuple definition, which is section-scoped rather than
//         membership-scoped and reads differently for that reason.
// Reverse: container-written rows read under the narrow unit definition.

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// --- rung 1: symbol section, verbatim channel ---

type evNarrowSym struct {
	_        struct{} `kind:"evNarrowSym"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Health   string   `lw:"health,symbol,verbatim"`
}

type evWideSym struct {
	_        struct{}              `kind:"evWideSym"`
	ID       uint64                `lw:",id"`
	Tracking []byte                `lw:",naturalKey"`
	Health   option.Option[string] `lw:"health,symbol,verbatim"`
}

// --- rung 2: u64Array section, low-card-ref channel ---

type evNarrowUnit struct {
	_        struct{} `kind:"evNarrowUnit"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Battery  uint64   `lw:"battery,u64Array,unit"`
}

type evWideCont struct {
	_        struct{} `kind:"evWideCont"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Battery  []uint64 `lw:"battery,u64Array"`
}

func evReaders(t *testing.T, rec arrow.RecordBatch, section string) (*marshallreflect.SectionReaders, func()) {
	t.Helper()
	idR := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idR.SetColumnIndices(idR.GetColumnIndices())
	require.NoError(t, idR.LoadFromRecord(rec))
	sr := marshallreflect.NewSectionReaders(idR.Len()).
		PlainColumn("id", idR.ValueId).
		PlainColumn("naturalKey", idR.ValueNaturalKey)
	switch section {
	case "symbol":
		r := anchor.NewReadAccessTestTableTaggedSymbol()
		r.SetColumnIndices(r.GetColumnIndices())
		require.NoError(t, r.LoadFromRecord(rec))
		sr.Section("symbol", r.GetAttributes(), r.GetMemberships())
		return sr, func() { idR.Release(); r.Release() }
	case "u64Array":
		r := anchor.NewReadAccessTestTableTaggedU64Array()
		r.SetColumnIndices(r.GetColumnIndices())
		require.NoError(t, r.LoadFromRecord(rec))
		sr.Section("u64Array", r.GetAttributes(), r.GetMemberships())
		return sr, func() { idR.Release(); r.Release() }
	}
	t.Fatalf("no reader for section %q", section)
	return nil, func() {}
}

// slotOf resolves one slot out of a DTO type's derived read contract.
func slotOf[T any](t *testing.T, section, membership string) mappingplan.Slot {
	t.Helper()
	plan, err := marshallreflect.PlanFor[T]()
	require.NoError(t, err)
	contract, err := mappingplan.DeriveReadContract(plan)
	require.NoError(t, err)
	slot, ok := contract.Slot(section, membership)
	require.True(t, ok, "contract has no slot for %s@%s:\n%s", section, membership, contract)
	return slot
}

// TestArityEvolution_RequiredToOption pins rung 1: rows written under the
// required-scalar definition decode, with their values, under the Option
// definition — and the widening is admits-superset at the contract layer.
func TestArityEvolution_RequiredToOption(t *testing.T) {
	narrow := slotOf[evNarrowSym](t, "symbol", "health")
	wide := slotOf[evWideSym](t, "symbol", "health")
	require.True(t, narrow.Required(), "the narrow slot is [1..1]")
	require.False(t, wide.Required(), "the widened slot is [0..1]")
	for n := 0; n <= narrow.MaxAttrs+1; n++ {
		if narrow.Admits(n) {
			require.True(t, wide.Admits(n), "widening must be admits-superset: narrow admits %d, wide must too", n)
		}
	}

	original := []evNarrowSym{
		{ID: 1, Tracking: []byte("A"), Health: "green"},
		{ID: 2, Tracking: []byte("B"), Health: "amber"},
	}
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(original))
	require.NoError(t, marshallreflect.Marshal(table, original, nil))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	readers, release := evReaders(t, recs[0], "symbol")
	defer release()
	var got []evWideSym
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, nil))
	require.Len(t, got, len(original))
	for i, w := range original {
		require.True(t, got[i].Health.Has, "row %d: a narrow-written value must read as present", i)
		require.Equal(t, w.Health, got[i].Health.Val, "row %d", i)
	}
}

// TestArityEvolution_UnitToContainer pins rung 2 plus coexistence: rows
// written under the unit-scalar definition read as one-element slices under
// the container definition, beside rows written wide — there is no flag-day.
func TestArityEvolution_UnitToContainer(t *testing.T) {
	lookup := marshallreflect.MapLookup{"battery": 9}

	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 3)
	require.NoError(t, marshallreflect.Marshal(table, []evNarrowUnit{
		{ID: 1, Tracking: []byte("A"), Battery: 8500},
		{ID: 2, Tracking: []byte("B"), Battery: 7200},
	}, lookup))
	require.NoError(t, marshallreflect.Marshal(table, []evWideCont{
		{ID: 3, Tracking: []byte("C"), Battery: []uint64{10, 20, 30}},
	}, lookup))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	readers, release := evReaders(t, recs[0], "u64Array")
	defer release()
	var got []evWideCont
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, lookup))
	require.Len(t, got, 3)
	require.Equal(t, []uint64{8500}, got[0].Battery, "a unit-written row reads as a one-element container")
	require.Equal(t, []uint64{7200}, got[1].Battery)
	require.Equal(t, []uint64{10, 20, 30}, got[2].Battery, "a wide-written row reads unchanged beside it")
}

// TestArityEvolution_NarrowingIsLoud pins the reverse direction: a
// two-element container-written value read under the narrow unit definition
// is refused, naming the field that declared the value single (ADR-0183 D5).
//
// It used to decode a present row with the field ZERO-FILLED — no error, no
// truncation, and nothing to tell it from a legitimate zero. That was R7's
// narrowing finding in
// doc/adr-background-work/leeway-components-consumer-complexity.md, and the
// rung below D4's attribute-count discipline (arity_enforcement_test.go):
// narrowing is the breaking direction, so it must be loud on every read
// path.
func TestArityEvolution_NarrowingIsLoud(t *testing.T) {
	lookup := marshallreflect.MapLookup{"battery": 9}

	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	require.NoError(t, marshallreflect.Marshal(table, []evWideCont{
		{ID: 4, Tracking: []byte("D"), Battery: []uint64{50, 60}},
	}, lookup))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	readers, release := evReaders(t, recs[0], "u64Array")
	defer release()
	var got []evNarrowUnit
	err = marshallreflect.Unmarshal(readers, &got, lookup)
	require.Error(t, err, "a multi-element value under a unit definition is a contract violation, not a zero")
	require.ErrorContains(t, err, "Battery", "the error names the field that declared the value single")
	require.ErrorContains(t, err, "exactly one")
}

// A unit definition still reads the one-element rows it was written for —
// the refusal above is about the count, not about the shape crossing, and
// the narrow reader keeps working over its own generation's data.
func TestArityEvolution_NarrowUnitReadsItsOwnRows(t *testing.T) {
	lookup := marshallreflect.MapLookup{"battery": 9}

	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	require.NoError(t, marshallreflect.Marshal(table, []evNarrowUnit{
		{ID: 5, Tracking: []byte("E"), Battery: 8500},
	}, lookup))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	readers, release := evReaders(t, recs[0], "u64Array")
	defer release()
	var got []evNarrowUnit
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, lookup))
	require.Len(t, got, 1)
	require.Equal(t, uint64(8500), got[0].Battery)
}

// --- rung 3: the shape crossing (scalar → dynamic-membership tuple) ---

// evSymElem is one element of a dynamic-membership tuple over the symbol
// section: the membership travels IN the element (ADR-0103/0109) rather than
// being fixed by the field's tag.
type evSymElem struct {
	Memb  string `lw:"@membership,verbatim"`
	Value string `lw:"symbol:value"`
}

// evTupleSym is the same kind widened across the shape boundary: where
// evNarrowSym names one membership and takes its value, this takes the
// section.
type evTupleSym struct {
	_        struct{}    `kind:"evNarrowSym"`
	ID       uint64      `lw:",id"`
	Tracking []byte      `lw:",naturalKey"`
	Elems    []evSymElem `lw:"symbol"`
}

// The crossing works in the widening direction: rows written under the scalar
// definition read under the tuple definition as one element carrying the
// membership the writer used and the value it wrote.
//
// This is the rung ADR-0183 D5 left unsanctioned pending a pin. It is
// sanctioned now, with the caveat the next test states.
func TestArityEvolution_ScalarRowsReadUnderATupleDefinition(t *testing.T) {
	lookup := marshallreflect.MapLookup{}

	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	require.NoError(t, marshallreflect.Marshal(table, []evNarrowSym{
		{ID: 1, Tracking: []byte("A"), Health: "green"},
	}, lookup))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	readers, release := evReaders(t, recs[0], "symbol")
	defer release()
	var got []evTupleSym
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, lookup))
	require.Len(t, got, 1)
	require.Len(t, got[0].Elems, 1)
	assert.Equal(t, "health", got[0].Elems[0].Memb, "the membership the narrow writer used")
	assert.Equal(t, "green", got[0].Elems[0].Value)
}

// The caveat, and the reason this crossing is not just "wider": a tuple field
// is scoped to the SECTION, not to a membership. It reads every attribute the
// section carries, including ones written by a component that has nothing to
// do with it — which on a shared table means a tuple component sees its
// neighbours.
//
// The scalar shape does not: it is membership-matched, so the same row reads
// cleanly under the narrow definition. Widening a slot to a tuple therefore
// changes WHICH attributes a component consumes, not only how many, and that
// is the thing to know before crossing.
func TestArityEvolution_TupleReadsTheSectionScalarReadsItsMembership(t *testing.T) {
	lookup := marshallreflect.MapLookup{}

	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	require.NoError(t, marshallreflect.Marshal(table, []evTupleSym{
		{ID: 1, Tracking: []byte("A"), Elems: []evSymElem{
			{Memb: "health", Value: "green"},
			{Memb: "otherDomainNote", Value: "not ours"},
		}},
	}, lookup))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	// The tuple reader takes both attributes — the neighbour's included.
	tupleReaders, releaseTuple := evReaders(t, recs[0], "symbol")
	defer releaseTuple()
	var wide []evTupleSym
	require.NoError(t, marshallreflect.Unmarshal(tupleReaders, &wide, lookup))
	require.Len(t, wide, 1)
	assert.Len(t, wide[0].Elems, 2,
		"a tuple field consumes its section, so a co-resident component's attribute arrives too")

	// The scalar reader takes only its own membership, from the same row.
	scalarReaders, releaseScalar := evReaders(t, recs[0], "symbol")
	defer releaseScalar()
	var narrow []evNarrowSym
	require.NoError(t, marshallreflect.Unmarshal(scalarReaders, &narrow, lookup))
	require.Len(t, narrow, 1)
	assert.Equal(t, "green", narrow[0].Health,
		"membership matching is what keeps co-resident components apart")
}
