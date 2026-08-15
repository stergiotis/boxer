package marshallreflect_test

// The failure-mode corpus (ADR-0183 D7's X-class): one small test per edge the
// consumer-complexity review found, so the current behaviour has an executable
// home rather than a paragraph in one of thirteen ADRs.
//
// Every test here pins something a consumer will meet. Three of them pin
// SILENCE — behaviour that is correct, unavoidable given the wire, and
// impossible to tell from a bug while reading the code. Those are the ones
// worth reading before debugging an empty field:
//
//	I1  a wrong lookup id reads exactly like honest absence
//	I2  a reader on a different assignment sees a whole batch as absent
//	R1  a partial row decodes present, with the missing fields zero-valued
//	R2  an empty container is unrepresentable — empty, nil and never-written
//	    are one observation
//	R6  string fields alias the Arrow buffer; the DTO outlives it only if
//	    the record is retained
//
// The edge letters are the inventory's in
// doc/adr-background-work/leeway-components-consumer-complexity.md.

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// fmDrone is the corpus's writer: one required slot, one optional, one
// container, over anchor's symbol and u64Array sections.
type fmDrone struct {
	_ struct{} `kind:"fmDrone"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Status  string                `lw:"fmStatus,symbol,verbatim"`
	Battery option.Option[uint64] `lw:"fmBattery,u64Array,unit"`
	Ticks   []uint64              `lw:"fmTicks,u64Array"`
}

func fmWrite(t *testing.T, rows []fmDrone, lookup marshallreflect.LookupI) (arrow.RecordBatch, func()) {
	t.Helper()
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(rows))
	require.NoError(t, marshallreflect.Marshal(table, rows, lookup))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	return recs[0], func() {
		for _, r := range recs {
			r.Release()
		}
	}
}

func fmReaders(t *testing.T, rec arrow.RecordBatch) (*marshallreflect.SectionReaders, func()) {
	t.Helper()
	idR := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idR.SetColumnIndices(idR.GetColumnIndices())
	require.NoError(t, idR.LoadFromRecord(rec))
	symR := anchor.NewReadAccessTestTableTaggedSymbol()
	symR.SetColumnIndices(symR.GetColumnIndices())
	require.NoError(t, symR.LoadFromRecord(rec))
	u64R := anchor.NewReadAccessTestTableTaggedU64Array()
	u64R.SetColumnIndices(u64R.GetColumnIndices())
	require.NoError(t, u64R.LoadFromRecord(rec))
	readers := marshallreflect.NewSectionReaders(idR.Len()).
		PlainColumn("id", idR.ValueId).
		PlainColumn("naturalKey", idR.ValueNaturalKey).
		Section("symbol", symR.GetAttributes(), symR.GetMemberships()).
		Section("u64Array", u64R.GetAttributes(), u64R.GetMemberships())
	return readers, func() {
		idR.Release()
		symR.Release()
		u64R.Release()
	}
}

// I1 — a wrong id is not a wrong answer, it is no answer.
//
// A ref-channel membership rides the wire as a uint64 and nothing else. A
// reader whose lookup maps the name to a different id asks for something that
// is not there, and for an Option or a container zero attributes is a legal
// state. So the field reads empty, no error is raised, and the observation is
// identical to the row honestly not carrying it. This cannot be made into a
// check — under fusion a component really may be absent from every row — which
// is why it is pinned here instead.
func TestI1_WrongLookupIdIsIndistinguishableFromAbsence(t *testing.T) {
	writer := marshallreflect.MapLookup{"fmBattery": 900, "fmTicks": 901}
	reader := marshallreflect.MapLookup{"fmBattery": 42, "fmTicks": 901} // battery re-pointed

	rec, release := fmWrite(t, []fmDrone{
		{ID: 1, Tracking: []byte("A"), Status: "flying", Battery: option.Some[uint64](88), Ticks: []uint64{1, 2}},
	}, writer)
	defer release()

	readers, releaseReaders := fmReaders(t, rec)
	defer releaseReaders()

	var got []fmDrone
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, reader),
		"a wrong id raises nothing: the slot is simply not populated")
	require.Len(t, got, 1)
	assert.False(t, got[0].Battery.Has, "the value written under the writer's id reads as absent")
	assert.Equal(t, []uint64{1, 2}, got[0].Ticks, "the slot whose id both agree on is unaffected")

	// The diagnosis exists and is one call away: what the reader asked for,
	// and what the section actually carries.
	rep, err := marshallreflect.InspectLookup[fmDrone](readers, reader)
	require.NoError(t, err)
	suspects := rep.Suspect()
	require.NotEmpty(t, suspects, "InspectLookup must name the slot that resolved but found nothing")
	assert.Equal(t, "fmBattery", suspects[0].Membership)
	assert.EqualValues(t, 42, suspects[0].ResolvedID, "the id the reader asked for")
	assert.Contains(t, rep.SectionRefIDs["u64Array"], uint64(900),
		"and the id the wire actually carries, which is the comparison that makes it obvious")
}

// I2 — the same silence one level up: a reader on a whole different
// assignment (a stale deployment, a regenerated store pointed at old rows)
// sees the batch as carrying nothing at all.
//
// Every slot resolves, every slot finds nothing, and no error is raised
// because "this batch does not contain this component" is an ordinary answer
// on a shared table. The report is what tells the two apart.
func TestI2_ReaderOnAnotherAssignmentSeesAnEmptyBatch(t *testing.T) {
	writer := marshallreflect.MapLookup{"fmBattery": 900, "fmTicks": 901}
	drifted := marshallreflect.MapLookup{"fmBattery": 800, "fmTicks": 801}

	rec, release := fmWrite(t, []fmDrone{
		{ID: 1, Tracking: []byte("A"), Status: "flying", Battery: option.Some[uint64](88), Ticks: []uint64{1}},
		{ID: 2, Tracking: []byte("B"), Status: "landed", Battery: option.Some[uint64](12), Ticks: []uint64{2}},
	}, writer)
	defer release()

	readers, releaseReaders := fmReaders(t, rec)
	defer releaseReaders()

	var got []fmDrone
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, drifted))
	require.Len(t, got, 2, "the rows are there; it is the component that is not")
	for i := range got {
		assert.False(t, got[i].Battery.Has, "row %d", i)
		assert.Empty(t, got[i].Ticks, "row %d", i)
	}

	rep, err := marshallreflect.InspectLookup[fmDrone](readers, drifted)
	require.NoError(t, err)
	assert.Equal(t, 2, rep.NumRows)
	for _, s := range rep.Suspect() {
		assert.Zero(t, s.Rows, "%s resolved to %d and found nothing", s.Membership, s.ResolvedID)
	}
	assert.Len(t, rep.Suspect(), 2, "every ref slot is suspect, which is what a drifted assignment looks like")
}

// R1 — present is not conforming.
//
// A row carrying some of a kind's memberships decodes as that kind, with the
// missing fields left at their zero values. Nothing in Unmarshal objects: the
// contract's job is arity, not completeness. Whether a row *conforms* is a
// separate question with a separate answer — Detect's verdict, or the Filter
// a generated Scan embeds.
func TestR1_PartialRowDecodesPresentWithZeroedFields(t *testing.T) {
	lookup := marshallreflect.MapLookup{"fmBattery": 900, "fmTicks": 901}

	// A writer that fills only part of the kind: no battery, no ticks.
	rec, release := fmWrite(t, []fmDrone{
		{ID: 1, Tracking: []byte("A"), Status: "flying"},
	}, lookup)
	defer release()

	readers, releaseReaders := fmReaders(t, rec)
	defer releaseReaders()

	var got []fmDrone
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, lookup))
	require.Len(t, got, 1)
	assert.Equal(t, "flying", got[0].Status)
	assert.False(t, got[0].Battery.Has, "absent optional")
	assert.Empty(t, got[0].Ticks, "absent container")

	// Detect is where conformance is answered. Here every required slot is
	// populated, so the row is at least approximate rather than absent — the
	// verdict, not the decode, is the conformance statement.
	p, err := marshallreflect.Detect[fmDrone](readers, 0, lookup)
	require.NoError(t, err)
	assert.NotEqual(t, mappingplan.PresenceAbsent, p,
		"the row carries the kind; how completely is what the verdict says")
}

// R2 — an empty container is unrepresentable, and that is a wire fact rather
// than a defect.
//
// A container with no elements writes no attribute at all, because the
// attribute is what carries the membership: with nothing to attach it to,
// there is nothing on the wire to say the slot was written empty. So "written
// empty", "written nil" and "never written" are one observation, and all three
// read back as an empty slice.
//
// What this costs a consumer: emptiness cannot be asserted, only observed. A
// component that must distinguish "no items" from "not collected" needs a
// second slot saying so — a boolean, a count — because the container alone
// cannot carry the difference.
func TestR2_EmptyContainerHasNoWireForm(t *testing.T) {
	lookup := marshallreflect.MapLookup{"fmBattery": 900, "fmTicks": 901}

	rec, release := fmWrite(t, []fmDrone{
		{ID: 1, Tracking: []byte("A"), Status: "flying", Ticks: []uint64{}},
		{ID: 2, Tracking: []byte("B"), Status: "flying", Ticks: nil},
		{ID: 3, Tracking: []byte("C"), Status: "flying", Ticks: []uint64{7}},
	}, lookup)
	defer release()

	readers, releaseReaders := fmReaders(t, rec)
	defer releaseReaders()

	// The wire says it first: across three rows only ONE carries the ticks
	// membership, and it is the row with an element.
	rep, err := marshallreflect.InspectLookup[fmDrone](readers, lookup)
	require.NoError(t, err)
	for _, s := range rep.Slots {
		if s.Membership == "fmTicks" {
			assert.Equal(t, 1, s.Rows, "the empty and nil rows wrote no attribute at all")
		}
	}

	var got []fmDrone
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, lookup))
	require.Len(t, got, 3)
	assert.Empty(t, got[0].Ticks, "written empty")
	assert.Empty(t, got[1].Ticks, "written nil")
	assert.Equal(t, []uint64{7}, got[2].Ticks, "written with an element")
	assert.Equal(t, got[0].Ticks, got[1].Ticks,
		"empty and nil are the same wire observation, so they are the same reading")
}

// R6 — a decoded string points into the Arrow record it came from.
//
// `string` fields are handed over without copying, which is what makes the
// read path cheap and what makes a DTO outliving its record a use-after-free
// that type-checks perfectly. This test does not release the record — it
// overwrites the buffer in place, which shows the same coupling deterministically
// and without undefined behaviour: nobody touches the DTO, and the DTO's value
// changes.
//
// The discipline: retain the record for as long as any decoded string from it
// is alive, or copy the field. []byte and [N]byte fields ARE copied on decode
// (asserted below), so this is a `string`-only hazard.
func TestR6_DecodedStringsAliasTheRecordBuffer(t *testing.T) {
	lookup := marshallreflect.MapLookup{"fmBattery": 900, "fmTicks": 901}

	rec, release := fmWrite(t, []fmDrone{
		{ID: 1, Tracking: []byte("TRACK"), Status: "flying"},
	}, lookup)
	defer release()

	readers, releaseReaders := fmReaders(t, rec)
	defer releaseReaders()

	var got []fmDrone
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, lookup))
	require.Len(t, got, 1)
	require.Equal(t, "flying", got[0].Status)

	// Find the buffer the symbol section's values live in and overwrite the
	// bytes "flying" was read from.
	values := findStringValues(t, rec, "flying")
	require.NotNil(t, values, "the written symbol must be somewhere in the record")
	buf := values.Data().Buffers()[2].Bytes() // 0 validity, 1 offsets, 2 data
	at := indexOfString(t, values, "flying")
	offsets := values.ValueOffsets()
	start, end := offsets[at], offsets[at+1]
	copy(buf[start:end], "GROUND"[:end-start])

	assert.NotEqual(t, "flying", got[0].Status,
		"the DTO's string was never touched, and changed: it points into the record")
	assert.Equal(t, []byte("TRACK"), got[0].Tracking,
		"[]byte fields are copied on decode, so the same overwrite cannot reach them")
}

// findStringValues locates the utf8 values array containing want. A tagged
// section's value column is a list, so the strings live in its child.
func findStringValues(t *testing.T, rec arrow.RecordBatch, want string) *array.String {
	t.Helper()
	for i := range int(rec.NumCols()) {
		var arr *array.String
		switch col := rec.Column(i).(type) {
		case *array.String:
			arr = col
		case *array.List:
			child, ok := col.ListValues().(*array.String)
			if !ok {
				continue
			}
			arr = child
		default:
			continue
		}
		for j := range arr.Len() {
			if arr.Value(j) == want {
				return arr
			}
		}
	}
	return nil
}

func indexOfString(t *testing.T, arr *array.String, want string) int {
	t.Helper()
	for j := range arr.Len() {
		if arr.Value(j) == want {
			return j
		}
	}
	t.Fatalf("%q not in array", want)
	return -1
}
