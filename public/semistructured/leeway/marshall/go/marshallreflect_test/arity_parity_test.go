package marshallreflect_test

// ADR-0146 M2b: the generated codec and the reflect codec must refuse the same
// colliding wire. The arity gate is derived from one contract, but the two
// front-ends enforce it through different code — an emitted counter versus a
// runtime tally — so the agreement needs a test that drives both over the
// SAME bytes.

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor/codecdemo"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// apCollidingElem writes one attribute per element into anchor's `symbol`
// section, each carrying its own ref membership — so two elements can claim
// the same membership id, which is the collision no ordinary DTO can express.
type apCollidingElem struct {
	Memb  uint64 `lw:"@membership"`
	Value string `lw:"symbol:value"`
}

type apCollidingWriter struct {
	_        struct{}          `kind:"apCollidingWriter"`
	ID       uint64            `lw:",id"`
	Tracking []byte            `lw:",naturalKey"`
	Elems    []apCollidingElem `lw:"symbol"`
}

// apWideUnitWriter writes anchor's u64Array slot as a CONTAINER — two
// elements inside ONE attribute — while codecdemo.DroneMission's Battery
// declares that same slot `,unit`. The attribute count is one either way, so
// this passes the arity gate above and only the value count distinguishes
// the two contracts (ADR-0183 D5).
type apWideUnitWriter struct {
	_        struct{} `kind:"apWideUnitWriter"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Status   string   `lw:"droneStatus,symbol"`
	Battery  []uint64 `lw:"battery,u64Array"`
}

// TestArityParity_GenAndReflectRefuseAMultiValuedUnit is the value-count rung
// of the same agreement: a container-written value read under a unit
// definition must be refused by BOTH front-ends, over the same bytes. It read
// as a present row with the field zero-filled until ADR-0183 D5 — the silent
// half of the narrowing direction, where the attribute-count half above was
// already loud.
func TestArityParity_GenAndReflectRefuseAMultiValuedUnit(t *testing.T) {
	lookup := marshallreflect.MapLookup{"droneStatus": 1, "battery": 2}

	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	require.NoError(t, marshallreflect.Marshal(table, []apWideUnitWriter{{
		ID: 1, Tracking: []byte("TRK"), Status: "IN_TRANSIT", Battery: []uint64{50, 60},
	}}, lookup))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	rec := recs[0]

	idReader := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idReader.SetColumnIndices(idReader.GetColumnIndices())
	require.NoError(t, idReader.LoadFromRecord(rec))
	defer idReader.Release()
	symbolReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symbolReader.SetColumnIndices(symbolReader.GetColumnIndices())
	require.NoError(t, symbolReader.LoadFromRecord(rec))
	defer symbolReader.Release()
	u64ArrayReader := anchor.NewReadAccessTestTableTaggedU64Array()
	u64ArrayReader.SetColumnIndices(u64ArrayReader.GetColumnIndices())
	require.NoError(t, u64ArrayReader.LoadFromRecord(rec))
	defer u64ArrayReader.Release()

	require.Equal(t, int64(1), u64ArrayReader.GetAttributes().GetNumberOfAttributes(0),
		"the fixture must carry ONE attribute — the divergence is inside it, not across attributes")

	// --- generated front-end ---
	var cols codecdemo.DroneMissionColumns
	genErr := codecdemo.DroneMissionFillFromArrow(
		&cols, 1,
		idReader.ValueId, idReader.ValueNaturalKey,
		symbolReader.GetAttributes(), symbolReader.GetMemberships(),
		u64ArrayReader.GetAttributes(), u64ArrayReader.GetMemberships(),
	)

	// --- reflect front-end ---
	args := marshallreflect.NewSectionReaders(idReader.Len()).
		PlainColumn("id", idReader.ValueId).
		PlainColumn("naturalKey", idReader.ValueNaturalKey).
		Section("symbol", symbolReader.GetAttributes(), symbolReader.GetMemberships()).
		Section("u64Array", u64ArrayReader.GetAttributes(), u64ArrayReader.GetMemberships())
	var got []reflectDrone
	reflectErr := marshallreflect.Unmarshal(args, &got, lookup)

	require.Error(t, genErr, "the generated codec must refuse a multi-element value under a unit definition")
	require.Error(t, reflectErr, "the reflect codec must refuse a multi-element value under a unit definition")
	for name, err := range map[string]error{"gen": genErr, "reflect": reflectErr} {
		pf := ebtest.Fields(t, err)
		assert.Equalf(t, "u64Array", pf["section"], "%s front-end names the section", name)
		assert.Equalf(t, "battery", pf["membership"], "%s front-end names the membership", name)
		assert.Equalf(t, "Battery", ebtest.Fields(t, err)["field"], "%s front-end names the field", name)
	}
}

// TestArityParity_GenAndReflectRefuseTheSameWire drives codecdemo.DroneMission
// — whose Status field claims symbol@droneStatus (kind id 1) — over a record
// carrying TWO attributes on that slot, through both front-ends.
func TestArityParity_GenAndReflectRefuseTheSameWire(t *testing.T) {
	// droneStatus resolves to 1 in the anchor fixture the cross-decode test
	// uses; both elements claim it.
	const kindDroneStatus = 1
	lookup := marshallreflect.MapLookup{"droneStatus": kindDroneStatus, "battery": 2}

	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	require.NoError(t, marshallreflect.Marshal(table, []apCollidingWriter{{
		ID: 1, Tracking: []byte("TRK"), Elems: []apCollidingElem{
			{Memb: kindDroneStatus, Value: "componentA"},
			{Memb: kindDroneStatus, Value: "componentB"},
		},
	}}, nil))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	rec := recs[0]

	idReader := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idReader.SetColumnIndices(idReader.GetColumnIndices())
	require.NoError(t, idReader.LoadFromRecord(rec))
	defer idReader.Release()
	symbolReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symbolReader.SetColumnIndices(symbolReader.GetColumnIndices())
	require.NoError(t, symbolReader.LoadFromRecord(rec))
	defer symbolReader.Release()
	u64ArrayReader := anchor.NewReadAccessTestTableTaggedU64Array()
	u64ArrayReader.SetColumnIndices(u64ArrayReader.GetColumnIndices())
	require.NoError(t, u64ArrayReader.LoadFromRecord(rec))
	defer u64ArrayReader.Release()

	require.Equal(t, int64(2), symbolReader.GetAttributes().GetNumberOfAttributes(0),
		"the fixture must actually carry two attributes on the slot")

	// --- generated front-end ---
	var cols codecdemo.DroneMissionColumns
	genErr := codecdemo.DroneMissionFillFromArrow(
		&cols, 1,
		idReader.ValueId, idReader.ValueNaturalKey,
		symbolReader.GetAttributes(), symbolReader.GetMemberships(),
		u64ArrayReader.GetAttributes(), u64ArrayReader.GetMemberships(),
	)

	// --- reflect front-end ---
	args := marshallreflect.NewSectionReaders(idReader.Len()).
		PlainColumn("id", idReader.ValueId).
		PlainColumn("naturalKey", idReader.ValueNaturalKey).
		Section("symbol", symbolReader.GetAttributes(), symbolReader.GetMemberships()).
		Section("u64Array", u64ArrayReader.GetAttributes(), u64ArrayReader.GetMemberships())
	var got []reflectDrone
	reflectErr := marshallreflect.Unmarshal(args, &got, lookup)

	require.Error(t, genErr, "the generated codec must refuse a colliding slot")
	require.Error(t, reflectErr, "the reflect codec must refuse a colliding slot")
	// Both name the slot, so a caller can tell which component collided.
	for name, err := range map[string]error{"gen": genErr, "reflect": reflectErr} {
		pf := ebtest.Fields(t, err)
		assert.Equalf(t, "symbol", pf["section"], "%s front-end names the section", name)
		assert.Equalf(t, "droneStatus", pf["membership"], "%s front-end names the membership", name)
	}
}
