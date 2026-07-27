package marshallreflect_test

// ADR-0146 M1: the read contract derived from a Plan, and the Detect verdict
// it produces against a row.
//
// The load-bearing test here is TestContract_ArityMatchesWriteSide: for every
// field shape, it MEASURES how many attributes the write path emits and
// asserts the derived slot admits that count. The contract's whole value is
// that it states what the writer guarantees, so a write-side change that
// breaks the guarantee has to fail here rather than silently widen what the
// read paths accept.

import (
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// --- one DTO per field shape the arity table distinguishes ---

type rcScalar struct {
	_        struct{} `kind:"rcScalar"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	V        string   `lw:"m,symbol"`
}

type rcUnit struct {
	_        struct{} `kind:"rcUnit"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	V        uint64   `lw:"m,u64Array,unit"`
}

type rcOption struct {
	_        struct{}              `kind:"rcOption"`
	ID       uint64                `lw:",id"`
	Tracking []byte                `lw:",naturalKey"`
	V        option.Option[string] `lw:"m,symbol"`
}

type rcSlice struct {
	_        struct{} `kind:"rcSlice"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	V        []uint64 `lw:"m,u64Array"`
}

type rcRoaring struct {
	_        struct{}        `kind:"rcRoaring"`
	ID       uint64          `lw:",id"`
	Tracking []byte          `lw:",naturalKey"`
	V        *roaring.Bitmap `lw:"m,u32Set"`
}

type rcConst struct {
	_        struct{} `kind:"rcConst"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	_        struct{} `lw:"m,symbol,const=pinned"`
	Other    []uint64 `lw:"n,u64Array"`
}

type rcMultiScalar struct {
	_        struct{}  `kind:"rcMultiScalar"`
	ID       uint64    `lw:",id"`
	Tracking []byte    `lw:",naturalKey"`
	B        time.Time `lw:"m,timeRange:beginIncl"`
	E        time.Time `lw:"m,timeRange:endExcl"`
}

type rcMixedTuple struct {
	_        struct{} `kind:"rcMixedTuple"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Text     string   `lw:"m,text:text"`
	WordLen  []uint32 `lw:"m,text:wordLength"`
	WordBag  []string `lw:"m,text:wordBag"`
}

type rcTupleElem struct {
	Label string `lw:"@membership,verbatim"`
	Value string `lw:"symbol:value"`
}

type rcTupleMany struct {
	_        struct{}      `kind:"rcTupleMany"`
	ID       uint64        `lw:",id"`
	Tracking []byte        `lw:",naturalKey"`
	Elems    []rcTupleElem `lw:"symbol"`
}

// --- helpers ---

func rcWrite[T any](t *testing.T, row T, lookup marshallreflect.LookupI) (arrow.RecordBatch, func()) {
	t.Helper()
	tbl := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	require.NoError(t, marshallreflect.Validate[T](tbl))
	require.NoError(t, marshallreflect.Marshal(tbl, []T{row}, lookup))
	recs, err := tbl.TransferRecords(nil)
	require.NoError(t, err)
	return recs[0], func() {
		for _, r := range recs {
			r.Release()
		}
	}
}

// rcAttrCount reads the attribute count for one section of row 0, using the
// generated RA reader the section name selects.
func rcAttrCount(t *testing.T, rec arrow.RecordBatch, section string) int {
	t.Helper()
	switch section {
	case "symbol":
		r := anchor.NewReadAccessTestTableTaggedSymbol()
		r.SetColumnIndices(r.GetColumnIndices())
		require.NoError(t, r.LoadFromRecord(rec))
		defer r.Release()
		return int(r.GetAttributes().GetNumberOfAttributes(0))
	case "u64Array":
		r := anchor.NewReadAccessTestTableTaggedU64Array()
		r.SetColumnIndices(r.GetColumnIndices())
		require.NoError(t, r.LoadFromRecord(rec))
		defer r.Release()
		return int(r.GetAttributes().GetNumberOfAttributes(0))
	case "u32Set":
		r := anchor.NewReadAccessTestTableTaggedU32Set()
		r.SetColumnIndices(r.GetColumnIndices())
		require.NoError(t, r.LoadFromRecord(rec))
		defer r.Release()
		return int(r.GetAttributes().GetNumberOfAttributes(0))
	case "timeRange":
		r := anchor.NewReadAccessTestTableTaggedTimeRange()
		r.SetColumnIndices(r.GetColumnIndices())
		require.NoError(t, r.LoadFromRecord(rec))
		defer r.Release()
		return int(r.GetAttributes().GetNumberOfAttributes(0))
	case "text":
		r := anchor.NewReadAccessTestTableTaggedText()
		r.SetColumnIndices(r.GetColumnIndices())
		require.NoError(t, r.LoadFromRecord(rec))
		defer r.Release()
		return int(r.GetAttributes().GetNumberOfAttributes(0))
	}
	t.Fatalf("no counter for section %q", section)
	return 0
}

// rcCheck writes one row, counts the attributes it produced in `section`, and
// asserts the derived contract slot for (section, membership) both admits that
// count and matches the expected arity bounds.
func rcCheck[T any](t *testing.T, name string, row T, lookup marshallreflect.LookupI, section, membership string, wantMin, wantMax, wantAttrs int) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		c, err := marshallreflect.Contract[T]()
		require.NoError(t, err)
		slot, ok := c.Slot(section, membership)
		require.Truef(t, ok, "contract has no slot for %s@%s; contract:\n%s", section, membership, c)
		require.Equal(t, wantMin, slot.MinAttrs, "MinAttrs")
		require.Equal(t, wantMax, slot.MaxAttrs, "MaxAttrs")

		rec, release := rcWrite(t, row, lookup)
		defer release()
		got := rcAttrCount(t, rec, section)
		require.Equal(t, wantAttrs, got, "attributes actually written")
		require.Truef(t, slot.Admits(got), "slot %s admits %d attributes", name, got)
	})
}

func TestContract_ArityMatchesWriteSide(t *testing.T) {
	lk := marshallreflect.MapLookup{"m": 1, "n": 2}
	bm := roaring.New()
	bm.Add(4)

	rcCheck(t, "scalar T", rcScalar{ID: 1, V: "x"}, lk, "symbol", "m", 1, 1, 1)
	rcCheck(t, "scalar T unit", rcUnit{ID: 1, V: 7}, lk, "u64Array", "m", 1, 1, 1)
	rcCheck(t, "Option present", rcOption{ID: 1, V: option.Some("x")}, lk, "symbol", "m", 0, 1, 1)
	rcCheck(t, "Option absent", rcOption{ID: 1}, lk, "symbol", "m", 0, 1, 0)
	rcCheck(t, "slice non-empty", rcSlice{ID: 1, V: []uint64{1, 2}}, lk, "u64Array", "m", 0, 1, 1)
	rcCheck(t, "slice empty", rcSlice{ID: 1}, lk, "u64Array", "m", 0, 1, 0)
	rcCheck(t, "roaring non-empty", rcRoaring{ID: 1, V: bm}, lk, "u32Set", "m", 0, 1, 1)
	rcCheck(t, "roaring nil", rcRoaring{ID: 1}, lk, "u32Set", "m", 0, 1, 0)
	rcCheck(t, "const", rcConst{ID: 1, Other: []uint64{1}}, lk, "symbol", "m", 1, 1, 1)
	rcCheck(t, "multi-sub-col all scalar", rcMultiScalar{ID: 1, B: time.Unix(1, 0), E: time.Unix(2, 0)}, lk, "timeRange", "m", 1, 1, 1)
	rcCheck(t, "multi-sub-col scalar+containers", rcMixedTuple{ID: 1, Text: "hi", WordLen: []uint32{2}, WordBag: []string{"hi"}}, lk, "text", "m", 1, 1, 1)
	rcCheck(t, "multi-sub-col scalar+empty containers", rcMixedTuple{ID: 1, Text: "hi"}, lk, "text", "m", 1, 1, 1)
	rcCheck(t, "tuple Many n=3", rcTupleMany{ID: 1, Elems: []rcTupleElem{
		{Label: "a", Value: "1"}, {Label: "b", Value: "2"}, {Label: "c", Value: "3"},
	}}, nil, "symbol", "", 0, mappingplan.ArityUnbounded, 3)
	rcCheck(t, "tuple Many n=0", rcTupleMany{ID: 1}, nil, "symbol", "", 0, mappingplan.ArityUnbounded, 0)
}

func TestContract_TupleOwnsItsSection(t *testing.T) {
	c, err := marshallreflect.Contract[rcTupleMany]()
	require.NoError(t, err)
	require.Len(t, c.Slots, 1)
	require.True(t, c.Slots[0].OwnsSection, "a dynamic tuple owns its section (ADR-0103)")
	require.Empty(t, c.Slots[0].Membership, "per-element memberships have no static name")
	// A tuple-owned section matches whatever membership is asked for.
	_, ok := c.Slot("symbol", "anything")
	require.True(t, ok)
}

func TestContract_SlotsCarryProjectingFields(t *testing.T) {
	c, err := marshallreflect.Contract[rcMixedTuple]()
	require.NoError(t, err)
	slot, ok := c.Slot("text", "m")
	require.True(t, ok)
	require.ElementsMatch(t, []string{"Text", "WordLen", "WordBag"}, slot.GoFields)
}

// --- Detect ---

func rcSymbolReaders(t *testing.T, rec arrow.RecordBatch) (*marshallreflect.SectionReaders, func()) {
	t.Helper()
	idR := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idR.SetColumnIndices(idR.GetColumnIndices())
	require.NoError(t, idR.LoadFromRecord(rec))
	symR := anchor.NewReadAccessTestTableTaggedSymbol()
	symR.SetColumnIndices(symR.GetColumnIndices())
	require.NoError(t, symR.LoadFromRecord(rec))
	return marshallreflect.NewSectionReaders(idR.Len()).
			PlainColumn("id", idR.ValueId).
			PlainColumn("naturalKey", idR.ValueNaturalKey).
			Section("symbol", symR.GetAttributes(), symR.GetMemberships()),
		func() { idR.Release(); symR.Release() }
}

func TestDetect_Exact(t *testing.T) {
	lk := marshallreflect.MapLookup{"m": 1}
	rec, release := rcWrite(t, rcScalar{ID: 1, Tracking: []byte("R"), V: "green"}, lk)
	defer release()
	readers, rel := rcSymbolReaders(t, rec)
	defer rel()

	p, err := marshallreflect.Detect[rcScalar](readers, 0, lk)
	require.NoError(t, err)
	require.Equal(t, mappingplan.PresenceExact, p)
}

func TestDetect_AbsentWhenRequiredSlotUnpopulated(t *testing.T) {
	lk := marshallreflect.MapLookup{"m": 1, "n": 2}
	// Write a row carrying only membership "n" in the u64Array section, so
	// rcScalar's required symbol@m slot is empty.
	rec, release := rcWrite(t, rcSlice{ID: 1, Tracking: []byte("R"), V: []uint64{9}}, marshallreflect.MapLookup{"m": 2})
	defer release()
	readers, rel := rcSymbolReaders(t, rec)
	defer rel()

	p, err := marshallreflect.Detect[rcScalar](readers, 0, lk)
	require.NoError(t, err)
	require.Equal(t, mappingplan.PresenceAbsent, p)
}

func TestDetect_AbsentWhenNothingPopulated(t *testing.T) {
	lk := marshallreflect.MapLookup{"m": 1}
	// An all-optional kind with its Option absent populates no slot at all.
	rec, release := rcWrite(t, rcOption{ID: 1, Tracking: []byte("R")}, lk)
	defer release()
	readers, rel := rcSymbolReaders(t, rec)
	defer rel()

	p, err := marshallreflect.Detect[rcOption](readers, 0, lk)
	require.NoError(t, err)
	require.Equal(t, mappingplan.PresenceAbsent, p,
		"an optional-only kind with nothing populated is Absent, not vacuously Exact")
}

func TestDetect_ExactWhenOptionalSlotPopulated(t *testing.T) {
	lk := marshallreflect.MapLookup{"m": 1}
	rec, release := rcWrite(t, rcOption{ID: 1, Tracking: []byte("R"), V: option.Some("x")}, lk)
	defer release()
	readers, rel := rcSymbolReaders(t, rec)
	defer rel()

	p, err := marshallreflect.Detect[rcOption](readers, 0, lk)
	require.NoError(t, err)
	require.Equal(t, mappingplan.PresenceExact, p)
}

// rcFlatVerbatim reads the `symbol` section's "health" membership as a flat
// mandatory scalar — the shape a component DTO has.
type rcFlatVerbatim struct {
	_        struct{} `kind:"rcFlatVerbatim"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Health   string   `lw:"health,symbol,verbatim"`
}

type rcFlatVerbatimOpt struct {
	_        struct{}              `kind:"rcFlatVerbatimOpt"`
	ID       uint64                `lw:",id"`
	Tracking []byte                `lw:",naturalKey"`
	Health   option.Option[string] `lw:"health,symbol,verbatim"`
}

// TestDetect_ApproximateOnSlotCollision is the case ADR-0146 exists for: two
// components wrote into one (section, membership) slot. Detect reports the row
// as recognisably carrying the kind but not conforming — where Unmarshal today
// either errors with no context (mandatory scalar) or silently returns an
// absent Option.
func TestDetect_ApproximateOnSlotCollision(t *testing.T) {
	rec, release := rcWrite(t, rcTupleMany{ID: 1, Tracking: []byte("R"), Elems: []rcTupleElem{
		{Label: "health", Value: "componentA"},
		{Label: "health", Value: "componentB"},
	}}, nil)
	defer release()
	readers, rel := rcSymbolReaders(t, rec)
	defer rel()

	p, err := marshallreflect.Detect[rcFlatVerbatim](readers, 0, nil)
	require.NoError(t, err)
	require.Equal(t, mappingplan.PresenceApproximate, p,
		"two attributes on a [1..1] slot: Presence holds, Validator does not")

	// The Option shape reaches the same verdict — today's decode silently
	// yields Has=false here, which is the failure mode Detect replaces.
	pOpt, err := marshallreflect.Detect[rcFlatVerbatimOpt](readers, 0, nil)
	require.NoError(t, err)
	require.Equal(t, mappingplan.PresenceApproximate, pOpt)
}

func TestDetectAll_PerRowVerdicts(t *testing.T) {
	lk := marshallreflect.MapLookup{"m": 1}
	tbl := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 2)
	rows := []rcOption{
		{ID: 1, Tracking: []byte("A"), V: option.Some("x")},
		{ID: 2, Tracking: []byte("B")},
	}
	require.NoError(t, marshallreflect.Marshal(tbl, rows, lk))
	recs, err := tbl.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	readers, rel := rcSymbolReaders(t, recs[0])
	defer rel()

	got, err := marshallreflect.DetectAll[rcOption](readers, lk)
	require.NoError(t, err)
	require.Equal(t, []mappingplan.PresenceE{
		mappingplan.PresenceExact,
		mappingplan.PresenceAbsent,
	}, got)
}

func TestDetect_MissingReaderIsReported(t *testing.T) {
	lk := marshallreflect.MapLookup{"m": 1}
	rec, release := rcWrite(t, rcScalar{ID: 1, V: "x"}, lk)
	defer release()
	idR := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idR.SetColumnIndices(idR.GetColumnIndices())
	require.NoError(t, idR.LoadFromRecord(rec))
	defer idR.Release()

	// No symbol section registered.
	readers := marshallreflect.NewSectionReaders(idR.Len()).
		PlainColumn("id", idR.ValueId).
		PlainColumn("naturalKey", idR.ValueNaturalKey)
	_, err := marshallreflect.Detect[rcScalar](readers, 0, lk)
	require.ErrorContains(t, err, "symbol")
}
