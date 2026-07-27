package marshallreflect_test

// ADR-0146 D6 against a REAL generated DML, not the recording mock.
//
// ADR-0070 D3 claimed two DTOs could both emit into one section, producing two
// BeginSectionFoo…EndSection cycles. anchor.InEntityTestTable does not:
// BeginEntity calls beginSections() once, EndSection returns the section to
// Initial, and nothing reopens it. The second visit failed at the next
// BeginAttribute, surfacing from CommitEntity as a bare "invalid state
// transition" — which is why the claim survived in tests driving a mock with no
// state machine.
//
// The overlap itself is legitimate: facts are fused and enriched by stages that
// do not know every component, so two DTOs contributing to one section is
// normal. RowComposer therefore buffers contributions and writes them into ONE
// shared frame. These tests use the real table, so they would catch a
// regression in either layer.

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

type svSymbolA struct {
	_        struct{} `kind:"svSymbolA"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	State    string   `lw:"svHealth,symbol"`
}

type svSymbolB struct {
	_        struct{} `kind:"svSymbolB"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Grade    string   `lw:"svGrade,symbol"`
}

type svU64 struct {
	_        struct{} `kind:"svU64"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Nums     []uint64 `lw:"svNums,u64Array"`
}

func svLookup() marshallreflect.MapLookup {
	return marshallreflect.MapLookup{"svHealth": 1, "svGrade": 2, "svNums": 3}
}

// Two DTOs on one section, distinct memberships: the shared frame carries both,
// and each component reads back independently. This is the case the DML's
// once-per-entity section frame made impossible to express by stacking.
func TestSectionVisit_OverlapSharesOneFrameOnARealDML(t *testing.T) {
	tbl := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	rc := marshallreflect.NewRowComposer(tbl, svLookup())

	require.NoError(t, rc.BeginRow(svSymbolA{ID: 1, Tracking: []byte("R"), State: "green"}))
	require.NoError(t, rc.AddSections(svSymbolB{ID: 1, Tracking: []byte("R"), Grade: "A"}))
	require.NoError(t, rc.CommitRow(),
		"a shared section frame must not trip the DML state machine")

	recs, err := tbl.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	idR := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idR.SetColumnIndices(idR.GetColumnIndices())
	require.NoError(t, idR.LoadFromRecord(recs[0]))
	defer idR.Release()
	symR := anchor.NewReadAccessTestTableTaggedSymbol()
	symR.SetColumnIndices(symR.GetColumnIndices())
	require.NoError(t, symR.LoadFromRecord(recs[0]))
	defer symR.Release()

	require.Equal(t, int64(2), symR.GetAttributes().GetNumberOfAttributes(0),
		"both contributions reached the wire")

	mk := func() *marshallreflect.SectionReaders {
		return marshallreflect.NewSectionReaders(idR.Len()).
			PlainColumn("id", idR.ValueId).PlainColumn("naturalKey", idR.ValueNaturalKey).
			Section("symbol", symR.GetAttributes(), symR.GetMemberships())
	}
	var gotA []svSymbolA
	require.NoError(t, marshallreflect.Unmarshal(mk(), &gotA, svLookup()))
	require.Equal(t, "green", gotA[0].State)
	var gotB []svSymbolB
	require.NoError(t, marshallreflect.Unmarshal(mk(), &gotB, svLookup()))
	require.Equal(t, "A", gotB[0].Grade,
		"each component reads its own slot off the fused row")
}

// Disjoint sections stack cleanly through the real DML — ADR-0070 D1, which is
// implemented and unaffected by D3's retraction.
func TestSectionVisit_DisjointSectionsStackOnARealDML(t *testing.T) {
	tbl := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	rc := marshallreflect.NewRowComposer(tbl, svLookup())

	require.NoError(t, rc.BeginRow(svSymbolA{ID: 1, Tracking: []byte("R"), State: "green"}))
	require.NoError(t, rc.AddSections(svU64{ID: 1, Tracking: []byte("R"), Nums: []uint64{7, 8}}))
	require.NoError(t, rc.CommitRow(), "CommitEntity must not report a state-transition error")

	recs, err := tbl.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	symR := anchor.NewReadAccessTestTableTaggedSymbol()
	symR.SetColumnIndices(symR.GetColumnIndices())
	require.NoError(t, symR.LoadFromRecord(recs[0]))
	defer symR.Release()
	uR := anchor.NewReadAccessTestTableTaggedU64Array()
	uR.SetColumnIndices(uR.GetColumnIndices())
	require.NoError(t, uR.LoadFromRecord(recs[0]))
	defer uR.Release()

	require.Equal(t, int64(1), symR.GetAttributes().GetNumberOfAttributes(0))
	require.Equal(t, int64(1), uR.GetAttributes().GetNumberOfAttributes(0))
}

// The case that broke the removed two-pass API: one section holding fields of
// different runtime cardinality. Emitting it in a single visit is exactly what
// the passes could not do.
type svMixedCard struct {
	_        struct{} `kind:"svMixedCard"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	One      []uint64 `lw:"svOne,u64Array"`
	Many     []uint64 `lw:"svMany,u64Array"`
}

func TestSectionVisit_MixedRuntimeCardinalityInOneSection(t *testing.T) {
	tbl := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	rc := marshallreflect.NewRowComposer(tbl, marshallreflect.MapLookup{"svOne": 1, "svMany": 2})

	require.NoError(t, rc.BeginRow(svMixedCard{
		ID: 1, Tracking: []byte("R"),
		One: []uint64{42}, Many: []uint64{1, 2, 3},
	}))
	require.NoError(t, rc.CommitRow(),
		"both cardinality classes emit inside one section frame")

	recs, err := tbl.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	uR := anchor.NewReadAccessTestTableTaggedU64Array()
	uR.SetColumnIndices(uR.GetColumnIndices())
	require.NoError(t, uR.LoadFromRecord(recs[0]))
	defer uR.Release()
	require.Equal(t, int64(2), uR.GetAttributes().GetNumberOfAttributes(0),
		"one attribute per field, both in the same visit")
}
