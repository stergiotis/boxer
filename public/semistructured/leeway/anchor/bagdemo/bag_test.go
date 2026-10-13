package bagdemo

import (
	"bytes"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// bagRows covers parameters of several lengths and none, a value-less
// attribute, and an entity carrying no attribute at all.
func bagRows() []BagDoc {
	return []BagDoc{
		{ID: 1,
			Strings: []BagString{{Path: "/calls/_/name", Params: []uint64{0}, Value: "search"}, {Path: "/calls/_/name", Params: []uint64{1}, Value: "fetch"}, {Path: "/title", Value: "plain"}},
			Symbols: []BagSymbol{{Path: "/kind", Value: "tool"}},
			Nulls:   []BagNull{{Path: "/calls/_/opts/_", Params: []uint64{0, 3}}}},
		{ID: 2, Nulls: []BagNull{{Path: "/empty"}}},
		{ID: 3},
	}
}

func notedRows() []NotedDoc {
	return []NotedDoc{
		{ID: 1, Sections: []NotedSection{{Heading: []uint64{1}, Text: "intro"}, {Heading: []uint64{2, 1}, Text: "details"}}},
		{ID: 2},
	}
}

func transfer(t *testing.T, table *InEntityBagTable) (recs []arrow.RecordBatch) {
	t.Helper()
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	t.Cleanup(func() {
		for _, r := range recs {
			r.Release()
		}
	})
	return
}

func ipcBytes(t *testing.T, rec arrow.RecordBatch) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(rec.Schema()))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// readers loads the table's read access over rec.
type readers struct {
	id     *ReadAccessBagTablePlainEntityIdAttributes
	str    *ReadAccessBagTableTaggedStringArray
	sym    *ReadAccessBagTableTaggedSymbol
	null   *ReadAccessBagTableTaggedNull
	length int
}

func load(t *testing.T, rec arrow.RecordBatch) (r readers) {
	t.Helper()
	r.id = NewReadAccessBagTablePlainEntityIdAttributes()
	r.id.SetColumnIndices(r.id.GetColumnIndices())
	require.NoError(t, r.id.LoadFromRecord(rec))
	r.str = NewReadAccessBagTableTaggedStringArray()
	r.str.SetColumnIndices(r.str.GetColumnIndices())
	require.NoError(t, r.str.LoadFromRecord(rec))
	r.sym = NewReadAccessBagTableTaggedSymbol()
	r.sym.SetColumnIndices(r.sym.GetColumnIndices())
	require.NoError(t, r.sym.LoadFromRecord(rec))
	r.null = NewReadAccessBagTableTaggedNull()
	r.null.SetColumnIndices(r.null.GetColumnIndices())
	require.NoError(t, r.null.LoadFromRecord(rec))
	r.length = r.id.Len()
	t.Cleanup(func() {
		r.id.Release()
		r.str.Release()
		r.sym.Release()
		r.null.Release()
	})
	return
}

// Both front-ends write the same bytes for a dynamic tuple on the mixed
// verbatim channel with parameters, a `,unit` value and a value-less
// element, and for a static nested section on the mixed ref channel with
// parameters (ADR-0326 §SD1–§SD3); the generated decode and the reflect
// decode read the rows back as written.
func TestBothFrontEndsAgreeOnParametrisedTuples(t *testing.T) {
	rows := bagRows()
	cols := &BagDocColumns{}
	for _, r := range rows {
		cols.Append(r)
	}
	gen := NewInEntityBagTable(memory.NewGoAllocator(), len(rows))
	require.NoError(t, BagDocBuildEntities(gen, cols))
	genRecs := transfer(t, gen)

	refl := NewInEntityBagTable(memory.NewGoAllocator(), len(rows))
	require.NoError(t, marshallreflect.Validate[BagDoc](refl))
	require.NoError(t, marshallreflect.Marshal(refl, rows, marshallreflect.NoLookup{}))
	reflRecs := transfer(t, refl)
	require.True(t, array.RecordEqual(genRecs[0], reflRecs[0]), "gen=%s\nreflect=%s", genRecs[0], reflRecs[0])
	require.Equal(t, ipcBytes(t, genRecs[0]), ipcBytes(t, reflRecs[0]))

	r := load(t, genRecs[0])
	got := &BagDocColumns{}
	require.NoError(t, BagDocFillFromArrow(got, r.length, r.id.ValueId,
		r.str.GetAttributes(), r.str.GetMemberships(),
		r.sym.GetAttributes(), r.sym.GetMemberships(),
		r.null, r.null.GetMemberships()))
	for i, want := range rows {
		require.Equal(t, want, got.Row(i), "row %d", i)
	}
	args := marshallreflect.NewSectionReaders(r.length).
		PlainColumn("id", r.id.ValueId).
		Section("stringArray", r.str.GetAttributes(), r.str.GetMemberships()).
		Section("symbol", r.sym.GetAttributes(), r.sym.GetMemberships()).
		Section("null", r.null, r.null.GetMemberships())
	var reflGot []BagDoc
	require.NoError(t, marshallreflect.Unmarshal(args, &reflGot, marshallreflect.NoLookup{}))
	require.Equal(t, rows, reflGot)

	noted := notedRows()
	ncols := &NotedDocColumns{}
	for _, n := range noted {
		ncols.Append(n)
	}
	ngen := NewInEntityBagTable(memory.NewGoAllocator(), len(noted))
	require.NoError(t, NotedDocBuildEntities(ngen, ncols))
	ngenRecs := transfer(t, ngen)
	lookup := marshallreflect.MapLookup{"noteSection": kindNoteSection}
	nrefl := NewInEntityBagTable(memory.NewGoAllocator(), len(noted))
	require.NoError(t, marshallreflect.Validate[NotedDoc](nrefl))
	require.NoError(t, marshallreflect.Marshal(nrefl, noted, lookup))
	require.Equal(t, ipcBytes(t, ngenRecs[0]), ipcBytes(t, transfer(t, nrefl)[0]))
}

// One entity carries all three kinds on the same sections: BagDoc on the
// mixed verbatim channel, NotedDoc on the mixed ref channel, PlainDoc on the
// ref channel. Each ReadRow claims its own attributes and leaves the
// others' alone (ADR-0326 §SD4); an entity without a kind's attributes
// reads that kind as absent.
func TestReadRowClaimsItsAttributesOnSharedSections(t *testing.T) {
	bag := bagRows()[0]
	noted := notedRows()[0]
	plain := PlainDoc{ID: 1, Title: "a title", Tags: []string{"x", "y"}}

	table := NewInEntityBagTable(memory.NewGoAllocator(), 2)
	table.BeginEntity()
	table.SetId(1)
	sym := table.GetSectionSymbol()
	require.NoError(t, BagDocEmitSectionSymbol(sym, bag))
	require.NoError(t, NotedDocEmitSectionSymbol(sym, noted))
	require.NoError(t, PlainDocEmitSectionSymbol(sym, plain))
	sym.EndSection()
	str := table.GetSectionStringArray()
	require.NoError(t, BagDocEmitSectionStringArray(str, bag))
	require.NoError(t, PlainDocEmitSectionStringArray(str, plain))
	str.EndSection()
	null := table.GetSectionNull()
	require.NoError(t, BagDocEmitSectionNull(null, bag))
	null.EndSection()
	require.NoError(t, table.CommitEntity())
	table.BeginEntity()
	table.SetId(2)
	require.NoError(t, table.CommitEntity())
	r := load(t, transfer(t, table)[0])

	gotBag, present, err := BagDocReadRow(0,
		r.str.GetAttributes(), r.str.GetMemberships(),
		r.sym.GetAttributes(), r.sym.GetMemberships(),
		r.null, r.null.GetMemberships())
	require.NoError(t, err)
	require.True(t, present)
	bag.ID = 0 // ReadRow leaves the envelope to the caller
	require.Equal(t, bag, gotBag)

	gotNoted, present, err := NotedDocReadRow(0, r.sym.GetAttributes(), r.sym.GetMemberships())
	require.NoError(t, err)
	require.True(t, present)
	noted.ID = 0
	require.Equal(t, noted, gotNoted)

	gotPlain, present, err := PlainDocReadRow(0,
		r.sym.GetAttributes(), r.sym.GetMemberships(),
		r.str.GetAttributes(), r.str.GetMemberships())
	require.NoError(t, err)
	require.True(t, present)
	plain.ID = 0
	require.Equal(t, plain, gotPlain)

	_, present, err = BagDocReadRow(1,
		r.str.GetAttributes(), r.str.GetMemberships(),
		r.sym.GetAttributes(), r.sym.GetMemberships(),
		r.null, r.null.GetMemberships())
	require.NoError(t, err)
	require.False(t, present, "an entity without the kind's attributes reads it as absent")
	_, present, err = NotedDocReadRow(1, r.sym.GetAttributes(), r.sym.GetMemberships())
	require.NoError(t, err)
	require.False(t, present)

	// The reflection front-end's ReadComponent claims the same attributes.
	readers := marshallreflect.NewSectionReaders(r.length).
		PlainColumn("id", r.id.ValueId).
		Section("stringArray", r.str.GetAttributes(), r.str.GetMemberships()).
		Section("symbol", r.sym.GetAttributes(), r.sym.GetMemberships()).
		Section("null", r.null, r.null.GetMemberships())
	lookup := marshallreflect.MapLookup{"noteSection": kindNoteSection, "plainTitle": kindPlainTitle, "plainTag": kindPlainTag}
	bag.ID, noted.ID, plain.ID = 1, 1, 1 // ReadComponent reads the envelope
	reflBag, present, err := readers.ReadComponent[BagDoc](0, lookup)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, bag, reflBag)
	reflNoted, present, err := readers.ReadComponent[NotedDoc](0, lookup)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, noted, reflNoted)
	reflPlain, present, err := readers.ReadComponent[PlainDoc](0, lookup)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, plain, reflPlain)
	p, err := readers.Detect[BagDoc](1, lookup)
	require.NoError(t, err)
	require.Equal(t, mappingplan.PresenceAbsent, p)
	p, err = readers.Detect[NotedDoc](1, lookup)
	require.NoError(t, err)
	require.Equal(t, mappingplan.PresenceAbsent, p)
}

func headedRows() []HeadedDoc {
	return []HeadedDoc{
		{ID: 1,
			Sections: []HeadedSection{{Heading: "Purpose", Text: "why"}, {Heading: "", Text: "untitled"}},
			Events:   []HeadedEvent{{Phase: []byte("created"), By: "a"}, {By: "b"}},
			Marks:    []HeadedMark{{Id: 7, Label: "x"}, {Id: 8}}},
		{ID: 2},
	}
}

// Text parameters (ADR-0326 §SD1) are the blob itself: both front-ends
// write the same bytes, both decodes read them back, and ReadRow and
// ReadComponent claim HeadedDoc's attributes beside the other kinds'.
func TestTextParametersRoundTrip(t *testing.T) {
	rows := headedRows()
	lookup := marshallreflect.MapLookup{"headedSection": kindHeadedSection, "headedBy": kindHeadedBy}
	cols := &HeadedDocColumns{}
	for _, r := range rows {
		cols.Append(r)
	}
	gen := NewInEntityBagTable(memory.NewGoAllocator(), len(rows))
	require.NoError(t, HeadedDocBuildEntities(gen, cols))
	genRecs := transfer(t, gen)
	refl := NewInEntityBagTable(memory.NewGoAllocator(), len(rows))
	require.NoError(t, marshallreflect.Validate[HeadedDoc](refl))
	require.NoError(t, marshallreflect.Marshal(refl, rows, lookup))
	require.Equal(t, ipcBytes(t, genRecs[0]), ipcBytes(t, transfer(t, refl)[0]))

	r := load(t, genRecs[0])
	got := &HeadedDocColumns{}
	require.NoError(t, HeadedDocFillFromArrow(got, r.length, r.id.ValueId,
		r.str.GetAttributes(), r.str.GetMemberships(),
		r.sym.GetAttributes(), r.sym.GetMemberships(),
		r.null, r.null.GetMemberships()))
	for i, want := range rows {
		require.Equal(t, want, got.Row(i), "row %d", i)
	}
	readers := marshallreflect.NewSectionReaders(r.length).
		PlainColumn("id", r.id.ValueId).
		Section("stringArray", r.str.GetAttributes(), r.str.GetMemberships()).
		Section("symbol", r.sym.GetAttributes(), r.sym.GetMemberships()).
		Section("null", r.null, r.null.GetMemberships())
	var reflGot []HeadedDoc
	require.NoError(t, marshallreflect.Unmarshal(readers, &reflGot, lookup))
	require.Equal(t, rows, reflGot)

	// Shared sections: HeadedDoc beside BagDoc and NotedDoc.
	headed, bag, noted := rows[0], bagRows()[0], notedRows()[0]
	table := NewInEntityBagTable(memory.NewGoAllocator(), 1)
	table.BeginEntity()
	table.SetId(1)
	sym := table.GetSectionSymbol()
	require.NoError(t, BagDocEmitSectionSymbol(sym, bag))
	require.NoError(t, HeadedDocEmitSectionSymbol(sym, headed))
	require.NoError(t, NotedDocEmitSectionSymbol(sym, noted))
	sym.EndSection()
	str := table.GetSectionStringArray()
	require.NoError(t, HeadedDocEmitSectionStringArray(str, headed))
	require.NoError(t, BagDocEmitSectionStringArray(str, bag))
	str.EndSection()
	null := table.GetSectionNull()
	require.NoError(t, BagDocEmitSectionNull(null, bag))
	require.NoError(t, HeadedDocEmitSectionNull(null, headed))
	null.EndSection()
	require.NoError(t, table.CommitEntity())
	s := load(t, transfer(t, table)[0])

	gotHeaded, present, err := HeadedDocReadRow(0,
		s.str.GetAttributes(), s.str.GetMemberships(),
		s.sym.GetAttributes(), s.sym.GetMemberships(),
		s.null, s.null.GetMemberships())
	require.NoError(t, err)
	require.True(t, present)
	want := headed
	want.ID = 0
	require.Equal(t, want, gotHeaded)
	gotBag, present, err := BagDocReadRow(0,
		s.str.GetAttributes(), s.str.GetMemberships(),
		s.sym.GetAttributes(), s.sym.GetMemberships(),
		s.null, s.null.GetMemberships())
	require.NoError(t, err)
	require.True(t, present)
	bag.ID = 0
	require.Equal(t, bag, gotBag)

	shared := marshallreflect.NewSectionReaders(s.length).
		PlainColumn("id", s.id.ValueId).
		Section("stringArray", s.str.GetAttributes(), s.str.GetMemberships()).
		Section("symbol", s.sym.GetAttributes(), s.sym.GetMemberships()).
		Section("null", s.null, s.null.GetMemberships())
	reflHeaded, present, err := shared.ReadComponent[HeadedDoc](0, lookup)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, headed, reflHeaded)
}
