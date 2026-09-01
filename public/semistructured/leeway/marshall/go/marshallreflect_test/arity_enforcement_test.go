package marshallreflect_test

// ADR-0146 D4 / M2a: Unmarshal enforces the read contract's slot arity.
//
// Each test here writes a row carrying TWO attributes on one (section,
// membership) slot — the shape produced when two components claim it — and
// asserts the decode now refuses. Before M2a the three shapes failed three
// different ways: a mandatory scalar errored with a message naming neither the
// section nor the membership, an Option silently decoded as absent, and a
// container silently concatenated both producers' values into one slice.

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// --- writers that put N attributes on one membership ---

type aeSymElem struct {
	Label string `lw:"@membership,verbatim"`
	Value string `lw:"symbol:value"`
}

type aeSymWriter struct {
	_        struct{}    `kind:"aeSymWriter"`
	ID       uint64      `lw:",id"`
	Tracking []byte      `lw:",naturalKey"`
	Elems    []aeSymElem `lw:"symbol"`
}

type aeArrElem struct {
	Label string   `lw:"@membership,verbatim"`
	Value []string `lw:"symbolArray:value"`
}

type aeArrWriter struct {
	_        struct{}    `kind:"aeArrWriter"`
	ID       uint64      `lw:",id"`
	Tracking []byte      `lw:",naturalKey"`
	Elems    []aeArrElem `lw:"symbolArray"`
}

// --- readers: the flat component shapes ---

type aeScalarReader struct {
	_        struct{} `kind:"aeScalarReader"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Health   string   `lw:"health,symbol,verbatim"`
}

type aeOptionReader struct {
	_        struct{}              `kind:"aeOptionReader"`
	ID       uint64                `lw:",id"`
	Tracking []byte                `lw:",naturalKey"`
	Health   option.Option[string] `lw:"health,symbol,verbatim"`
}

type aeSliceReader struct {
	_        struct{} `kind:"aeSliceReader"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Tags     []string `lw:"health,symbolArray,verbatim"`
}

func aeWrite[T any](t *testing.T, row T) (arrow.RecordBatch, func()) {
	t.Helper()
	tbl := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	require.NoError(t, marshallreflect.Marshal(tbl, []T{row}, nil))
	recs, err := tbl.TransferRecords(nil)
	require.NoError(t, err)
	return recs[0], func() {
		for _, r := range recs {
			r.Release()
		}
	}
}

func aeReaders(t *testing.T, rec arrow.RecordBatch, section string) (*marshallreflect.SectionReaders, func()) {
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
	case "symbolArray":
		r := anchor.NewReadAccessTestTableTaggedSymbolArray()
		r.SetColumnIndices(r.GetColumnIndices())
		require.NoError(t, r.LoadFromRecord(rec))
		sr.Section("symbolArray", r.GetAttributes(), r.GetMemberships())
		return sr, func() { idR.Release(); r.Release() }
	}
	t.Fatalf("no reader for section %q", section)
	return nil, func() {}
}

func TestArity_ContainerCollisionIsRefused(t *testing.T) {
	rec, release := aeWrite(t, aeArrWriter{ID: 1, Tracking: []byte("R"), Elems: []aeArrElem{
		{Label: "health", Value: []string{"A1", "A2"}},
		{Label: "health", Value: []string{"B1", "B2"}},
	}})
	defer release()
	readers, rel := aeReaders(t, rec, "symbolArray")
	defer rel()

	var got []aeSliceReader
	err := marshallreflect.Unmarshal(readers, &got, nil)
	require.Error(t, err, "two attributes on a [0..1] slot must not silently concatenate")
	f := ebtest.Fields(t, err)
	assert.Equal(t, "symbolArray", f["section"], "the error names the slot")
	assert.Equal(t, "health", f["membership"])
	require.NotContains(t, got, aeSliceReader{}, "no row is appended on failure")
}

func TestArity_OptionCollisionIsRefused(t *testing.T) {
	rec, release := aeWrite(t, aeSymWriter{ID: 1, Tracking: []byte("R"), Elems: []aeSymElem{
		{Label: "health", Value: "componentA"},
		{Label: "health", Value: "componentB"},
	}})
	defer release()
	readers, rel := aeReaders(t, rec, "symbol")
	defer rel()

	var got []aeOptionReader
	err := marshallreflect.Unmarshal(readers, &got, nil)
	require.Error(t, err, "an Option must not silently decode as absent when its slot collides")
	assert.Equal(t, "health", ebtest.Fields(t, err)["membership"])
}

func TestArity_ScalarCollisionNamesTheSlot(t *testing.T) {
	rec, release := aeWrite(t, aeSymWriter{ID: 1, Tracking: []byte("R"), Elems: []aeSymElem{
		{Label: "health", Value: "componentA"},
		{Label: "health", Value: "componentB"},
	}})
	defer release()
	readers, rel := aeReaders(t, rec, "symbol")
	defer rel()

	var got []aeScalarReader
	err := marshallreflect.Unmarshal(readers, &got, nil)
	require.Error(t, err)
	// The pre-M2a message was "expected exactly one occurrence per row", which
	// named neither the slot nor the cause.
	f := ebtest.Fields(t, err)
	assert.Equal(t, "symbol", f["section"], "the error names the slot")
	assert.Equal(t, "health", f["membership"])
	assert.Contains(t, f["fields"], "Health", "the projecting DTO field is named")
}

// A single attribute on the slot still decodes — the gate rejects surplus, not
// the ordinary case.
func TestArity_SingleAttributeStillDecodes(t *testing.T) {
	rec, release := aeWrite(t, aeSymWriter{ID: 1, Tracking: []byte("R"), Elems: []aeSymElem{
		{Label: "health", Value: "only"},
	}})
	defer release()
	readers, rel := aeReaders(t, rec, "symbol")
	defer rel()

	var got []aeScalarReader
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, nil))
	require.Len(t, got, 1)
	require.Equal(t, "only", got[0].Health)
}

// An empty slot for an Option reader is still absent, not an error: the slot is
// [0..1] and zero is inside it.
func TestArity_OptionEmptySlotIsStillAbsent(t *testing.T) {
	rec, release := aeWrite(t, aeSymWriter{ID: 1, Tracking: []byte("R"), Elems: []aeSymElem{
		{Label: "somethingElse", Value: "x"},
	}})
	defer release()
	readers, rel := aeReaders(t, rec, "symbol")
	defer rel()

	var got []aeOptionReader
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, nil))
	require.Len(t, got, 1)
	require.False(t, got[0].Health.Has)
}

// A mandatory scalar whose slot is empty is refused, and the message says the
// slot is under-populated rather than over-populated.
func TestArity_MandatoryScalarMissingSlot(t *testing.T) {
	rec, release := aeWrite(t, aeSymWriter{ID: 1, Tracking: []byte("R"), Elems: []aeSymElem{
		{Label: "somethingElse", Value: "x"},
	}})
	defer release()
	readers, rel := aeReaders(t, rec, "symbol")
	defer rel()

	var got []aeScalarReader
	err := marshallreflect.Unmarshal(readers, &got, nil)
	require.Error(t, err)
	assert.EqualValues(t, 1, ebtest.Fields(t, err)["min"], "the required minimum is reported")
}
