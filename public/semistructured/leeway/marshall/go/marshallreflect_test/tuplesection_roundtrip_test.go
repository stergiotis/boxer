package marshallreflect_test

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor/codecdemo"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	raruntime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

// labeledText is the tuple element over anchor's mixed-shape `text`
// section (scalar `text` + co-containers `wordLength` / `wordBag`): each
// element emits ONE attribute carrying its own membership (ADR-0103).
type labeledText struct {
	Label      string   `lw:"@membership,verbatim"`
	Text       string   `lw:"text:text"`
	WordLength []uint32 `lw:"text:wordLength"`
	WordBag    []string `lw:"text:wordBag"`
}

// labeledDoc maps N labeledText elements into the ONE `text` section —
// the multi-membership multi-sub-column shape the static grammar cannot
// express (one membership per multi-sub-column section).
type labeledDoc struct {
	_ struct{} `kind:"labeledDoc"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Texts []labeledText `lw:"text"`
}

// labeledDocRows covers the attribute cardinalities N = 3 / 1 / 0 per
// entity, distinct memberships per attribute, and per-element container
// lengths 2 / 0 / 1 (an element with empty co-containers still emits —
// its presence in the slice is the signal).
func labeledDocRows() []labeledDoc {
	return []labeledDoc{
		{ID: 4001, Tracking: []byte("TPL-A"), Texts: []labeledText{
			{Label: "title", Text: "hello world", WordLength: []uint32{5, 5}, WordBag: []string{"hello", "world"}},
			{Label: "summary", Text: "empty containers", WordLength: nil, WordBag: nil},
			{Label: "note", Text: "one", WordLength: []uint32{3}, WordBag: []string{"one"}},
		}},
		{ID: 4002, Tracking: []byte("TPL-B"), Texts: []labeledText{
			{Label: "title", Text: "solo", WordLength: []uint32{4}, WordBag: []string{"solo"}},
		}},
		{ID: 4003, Tracking: []byte("TPL-C")}, // zero elements → zero attributes
	}
}

// writeLabeledDocsViaDML hand-drives anchor's generated DML the way a
// consumer's hand-written multi-attribute loop does: one BeginAttribute
// per element, each with its own verbatim membership. The tuple codec
// must reproduce these bytes exactly (ADR-0103 verification gate 1).
func writeLabeledDocsViaDML(t *testing.T, rows []labeledDoc) *anchor.InEntityTestTable {
	t.Helper()
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(rows))
	for _, r := range rows {
		table.BeginEntity()
		table.SetId(r.ID, r.Tracking)
		sec := table.GetSectionText()
		for _, e := range r.Texts {
			attr := sec.BeginAttribute(e.Text)
			for k := range e.WordLength {
				attr.AddToCoContainersP(e.WordLength[k], e.WordBag[k])
			}
			attr.AddMembershipLowCardVerbatimP([]byte(e.Label))
			attr.EndAttributeP()
		}
		sec.EndSection()
		require.NoError(t, table.CommitEntity())
	}
	return table
}

// TestReflect_TupleTextSection_DMLByteIdentityAndRoundTrip closes gate 1
// of ADR-0103 for the reflect front-end: Marshal over the tuple DTO must
// emit bytes identical to the explicit DML loop (BeginAttribute × N per
// row, one membership each), and Unmarshal must round-trip the elements
// — memberships included — in wire order.
func TestReflect_TupleTextSection_DMLByteIdentityAndRoundTrip(t *testing.T) {
	original := labeledDocRows()

	dmlTable := writeLabeledDocsViaDML(t, original)
	dmlRecs, err := dmlTable.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, dmlRecs)
	defer func() {
		for _, r := range dmlRecs {
			r.Release()
		}
	}()

	reflectTable := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(original))
	require.NoError(t, marshallreflect.Validate[labeledDoc](reflectTable))
	require.NoError(t, marshallreflect.Marshal(reflectTable, original, marshallreflect.NoLookup{}))
	reflectRecs, err := reflectTable.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, reflectRecs)
	defer func() {
		for _, r := range reflectRecs {
			r.Release()
		}
	}()

	require.Equal(t, len(dmlRecs), len(reflectRecs), "record-batch count must match")
	for i := range dmlRecs {
		require.Truef(t, array.RecordEqual(dmlRecs[i], reflectRecs[i]),
			"record %d differs between DML loop and reflect front-end:\ndml=%s\nreflect=%s", i, dmlRecs[i], reflectRecs[i])
		require.Equalf(t, ipcBytes(t, dmlRecs[i]), ipcBytes(t, reflectRecs[i]),
			"record %d IPC bytes differ between DML loop and reflect front-end", i)
	}

	// Round-trip: decode the DML-written record through the tuple read path.
	rec := dmlRecs[0]

	idReader := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idReader.SetColumnIndices(idReader.GetColumnIndices())
	require.NoError(t, idReader.LoadFromRecord(rec))
	defer idReader.Release()

	textReader := anchor.NewReadAccessTestTableTaggedText()
	textReader.SetColumnIndices(textReader.GetColumnIndices())
	require.NoError(t, textReader.LoadFromRecord(rec))
	defer textReader.Release()

	// The wire really carries N attributes per entity: 3 / 1 / 0.
	attrs := textReader.GetAttributes()
	require.Equal(t, int64(3), attrs.GetNumberOfAttributes(raruntime.EntityIdx(0)))
	require.Equal(t, int64(1), attrs.GetNumberOfAttributes(raruntime.EntityIdx(1)))
	require.Equal(t, int64(0), attrs.GetNumberOfAttributes(raruntime.EntityIdx(2)))

	args := marshallreflect.NewSectionReaders(idReader.Len()).
		PlainColumn("id", idReader.ValueId).
		PlainColumn("naturalKey", idReader.ValueNaturalKey).
		Section("text", textReader.GetAttributes(), textReader.GetMemberships())
	var got []labeledDoc
	require.NoError(t, marshallreflect.Unmarshal(args, &got, marshallreflect.NoLookup{}))

	require.Equal(t, len(original), len(got))
	for i := range original {
		require.Equal(t, original[i], got[i], "row %d", i)
	}
}

// TestReflect_TupleZipLengthMismatch pins the per-element zip contract:
// co-container slices of unequal length inside ONE element are an error,
// not a panic and not silent truncation.
func TestReflect_TupleZipLengthMismatch(t *testing.T) {
	rows := []labeledDoc{
		{ID: 1, Tracking: []byte("BAD"), Texts: []labeledText{
			{Label: "ok", Text: "fine", WordLength: []uint32{1}, WordBag: []string{"a"}},
			{Label: "bad", Text: "broken", WordLength: []uint32{1, 2}, WordBag: []string{"a"}},
		}},
	}
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(rows))
	err := marshallreflect.Marshal(table, rows, marshallreflect.NoLookup{})
	require.Error(t, err)
	require.ErrorContains(t, err, "co-container slices have different lengths")
}

// symbolTag / symbolDoc exercise the tuple form over a SINGLE-sub-column
// section (anchor `symbol`, scalar `value`): the tuple grammar is not
// bound to multi-sub-column sections — S + C ≥ 1 suffices. The element
// struct also uses a []byte membership field (the other supported
// membership Go type).
type symbolTag struct {
	Label []byte `lw:"@membership,verbatim"`
	Value string `lw:"symbol"`
}

type symbolDoc struct {
	_ struct{} `kind:"symbolDoc"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Tags []symbolTag `lw:"symbol"`
}

func TestReflect_TupleSingleSubColumnSection(t *testing.T) {
	original := []symbolDoc{
		{ID: 5001, Tracking: []byte("SYM-A"), Tags: []symbolTag{
			{Label: []byte("color"), Value: "red"},
			{Label: []byte("size"), Value: "xl"},
		}},
		{ID: 5002, Tracking: []byte("SYM-B")},
	}

	// Explicit DML loop — the byte-identity reference.
	dmlTable := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(original))
	for _, r := range original {
		dmlTable.BeginEntity()
		dmlTable.SetId(r.ID, r.Tracking)
		sec := dmlTable.GetSectionSymbol()
		for _, e := range r.Tags {
			attr := sec.BeginAttribute(e.Value)
			attr.AddMembershipLowCardVerbatimP(e.Label)
			attr.EndAttributeP()
		}
		sec.EndSection()
		require.NoError(t, dmlTable.CommitEntity())
	}
	dmlRecs, err := dmlTable.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range dmlRecs {
			r.Release()
		}
	}()

	reflectTable := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(original))
	require.NoError(t, marshallreflect.Validate[symbolDoc](reflectTable))
	require.NoError(t, marshallreflect.Marshal(reflectTable, original, marshallreflect.NoLookup{}))
	reflectRecs, err := reflectTable.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range reflectRecs {
			r.Release()
		}
	}()

	require.Equal(t, len(dmlRecs), len(reflectRecs))
	for i := range dmlRecs {
		require.Truef(t, array.RecordEqual(dmlRecs[i], reflectRecs[i]),
			"record %d differs:\ndml=%s\nreflect=%s", i, dmlRecs[i], reflectRecs[i])
		require.Equal(t, ipcBytes(t, dmlRecs[i]), ipcBytes(t, reflectRecs[i]))
	}

	// Round-trip through the tuple read path.
	rec := reflectRecs[0]
	idReader := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idReader.SetColumnIndices(idReader.GetColumnIndices())
	require.NoError(t, idReader.LoadFromRecord(rec))
	defer idReader.Release()
	symReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symReader.SetColumnIndices(symReader.GetColumnIndices())
	require.NoError(t, symReader.LoadFromRecord(rec))
	defer symReader.Release()

	args := marshallreflect.NewSectionReaders(idReader.Len()).
		PlainColumn("id", idReader.ValueId).
		PlainColumn("naturalKey", idReader.ValueNaturalKey).
		Section("symbol", symReader.GetAttributes(), symReader.GetMemberships())
	var got []symbolDoc
	require.NoError(t, marshallreflect.Unmarshal(args, &got, marshallreflect.NoLookup{}))
	require.Equal(t, original, got)
}

// TestRowComposer_TupleCardinalityPasses pins the ADR-0101 D7 rule at
// element grain: a tuple element routes into the single-value pass when
// its shared container length is ≤ 1 and into the multi-value pass when
// it is > 1. As with static sections, one section frame opens at most
// once per entity (the DML protocol), so a row's tuple elements must all
// classify into the same pass — mirroring the existing mixed-shape pass
// test, each row here emits in exactly one pass, never zero, never both.
func TestRowComposer_TupleCardinalityPasses(t *testing.T) {
	multi := labeledDoc{ID: 1, Tracking: []byte("A"), Texts: []labeledText{
		{Label: "m1", Text: "two words", WordLength: []uint32{3, 5}, WordBag: []string{"two", "words"}},
		{Label: "m2", Text: "more words", WordLength: []uint32{4, 5}, WordBag: []string{"more", "words"}},
	}}
	single := labeledDoc{ID: 2, Tracking: []byte("B"), Texts: []labeledText{
		{Label: "s1", Text: "one", WordLength: []uint32{3}, WordBag: []string{"one"}},
		{Label: "s2", Text: "empty", WordLength: nil, WordBag: nil},
	}}

	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 2)
	m := marshallreflect.NewRowComposer(table, marshallreflect.NoLookup{})

	// Row 0: both elements N > 1 → the section emits only in the
	// multi-value pass.
	require.NoError(t, m.BeginRow(plainOnlyOwner{ID: multi.ID, Tracking: multi.Tracking}))
	require.NoError(t, m.AddSingleValueAttributes(multi))
	require.NoError(t, m.AddMultiValueAttributes(multi))
	require.NoError(t, m.CommitRow())

	// Row 1: both elements N ≤ 1 → only the single-value pass.
	require.NoError(t, m.BeginRow(plainOnlyOwner{ID: single.ID, Tracking: single.Tracking}))
	require.NoError(t, m.AddSingleValueAttributes(single))
	require.NoError(t, m.AddMultiValueAttributes(single))
	require.NoError(t, m.CommitRow())

	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	textReader := anchor.NewReadAccessTestTableTaggedText()
	textReader.SetColumnIndices(textReader.GetColumnIndices())
	require.NoError(t, textReader.LoadFromRecord(recs[0]))
	defer textReader.Release()

	// Every element emitted exactly once, in element order.
	require.Equal(t, int64(2), textReader.GetAttributes().GetNumberOfAttributes(raruntime.EntityIdx(0)))
	require.Equal(t, int64(2), textReader.GetAttributes().GetNumberOfAttributes(raruntime.EntityIdx(1)))
	var labels []string
	for entity := 0; entity < 2; entity++ {
		for attrJ := int64(0); attrJ < 2; attrJ++ {
			for mv := range textReader.GetMemberships().GetMembValueLowCardVerbatim(raruntime.EntityIdx(entity), raruntime.AttributeIdx(attrJ)) {
				labels = append(labels, string(mv))
			}
		}
	}
	require.Equal(t, []string{"m1", "m2", "s1", "s2"}, labels)
}

// TestGenVsReflect_TupleTextSection closes the byte-identity invariant
// for the tuple path across the two front-ends: codecdemo's generated
// LabeledTextDocBuildEntities (marshallgen) and marshallreflect.Marshal
// must emit identical wire bytes for the same rows, and each front-end's
// record must decode through the other's read path. Together with
// TestReflect_TupleTextSection_DMLByteIdentityAndRoundTrip this pins
// gen ≡ reflect ≡ explicit DML loop.
func TestGenVsReflect_TupleTextSection(t *testing.T) {
	original := labeledDocRows()
	allocator := memory.NewGoAllocator()

	// --- gen front-end (marshallgen BuildEntities) ---
	var cols codecdemo.LabeledTextDocColumns
	for _, r := range original {
		texts := make([]codecdemo.LabeledText, 0, len(r.Texts))
		for _, e := range r.Texts {
			texts = append(texts, codecdemo.LabeledText{Label: e.Label, Text: e.Text, WordLength: e.WordLength, WordBag: e.WordBag})
		}
		if len(texts) == 0 {
			texts = nil
		}
		cols.Append(codecdemo.LabeledTextDoc{ID: r.ID, Tracking: r.Tracking, Texts: texts})
	}
	genTable := anchor.NewInEntityTestTable(allocator, len(original))
	require.NoError(t, codecdemo.LabeledTextDocBuildEntities(genTable, &cols))
	genRecs, err := genTable.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, genRecs)
	defer func() {
		for _, r := range genRecs {
			r.Release()
		}
	}()

	// --- reflect front-end (marshallreflect.Marshal) ---
	reflectTable := anchor.NewInEntityTestTable(allocator, len(original))
	require.NoError(t, marshallreflect.Marshal(reflectTable, original, marshallreflect.NoLookup{}))
	reflectRecs, err := reflectTable.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, reflectRecs)
	defer func() {
		for _, r := range reflectRecs {
			r.Release()
		}
	}()

	require.Equal(t, len(genRecs), len(reflectRecs), "record-batch count must match")
	for i := range genRecs {
		require.Truef(t, array.RecordEqual(genRecs[i], reflectRecs[i]),
			"record %d differs between gen and reflect front-ends:\ngen=%s\nreflect=%s", i, genRecs[i], reflectRecs[i])
		require.Equalf(t, ipcBytes(t, genRecs[i]), ipcBytes(t, reflectRecs[i]),
			"record %d IPC bytes differ between gen and reflect front-ends", i)
	}

	// Cross-decode 1: gen-write → reflect-read.
	rec := genRecs[0]
	idReader := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idReader.SetColumnIndices(idReader.GetColumnIndices())
	require.NoError(t, idReader.LoadFromRecord(rec))
	defer idReader.Release()
	textReader := anchor.NewReadAccessTestTableTaggedText()
	textReader.SetColumnIndices(textReader.GetColumnIndices())
	require.NoError(t, textReader.LoadFromRecord(rec))
	defer textReader.Release()

	args := marshallreflect.NewSectionReaders(idReader.Len()).
		PlainColumn("id", idReader.ValueId).
		PlainColumn("naturalKey", idReader.ValueNaturalKey).
		Section("text", textReader.GetAttributes(), textReader.GetMemberships())
	var got []labeledDoc
	require.NoError(t, marshallreflect.Unmarshal(args, &got, marshallreflect.NoLookup{}))
	require.Equal(t, original, got)

	// Cross-decode 2: reflect-write → gen-read (FillFromArrow).
	rec2 := reflectRecs[0]
	idReader2 := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idReader2.SetColumnIndices(idReader2.GetColumnIndices())
	require.NoError(t, idReader2.LoadFromRecord(rec2))
	defer idReader2.Release()
	textReader2 := anchor.NewReadAccessTestTableTaggedText()
	textReader2.SetColumnIndices(textReader2.GetColumnIndices())
	require.NoError(t, textReader2.LoadFromRecord(rec2))
	defer textReader2.Release()

	filled := &codecdemo.LabeledTextDocColumns{}
	require.NoError(t, codecdemo.LabeledTextDocFillFromArrow(
		filled,
		idReader2.Len(),
		idReader2.ValueId,
		idReader2.ValueNaturalKey,
		textReader2.GetAttributes(),
		textReader2.GetMemberships(),
	))
	require.Equal(t, cols.Len(), filled.Len())
	for i := 0; i < cols.Len(); i++ {
		require.Equal(t, cols.Row(i), filled.Row(i), "row %d", i)
	}
}

// TestValidate_TupleContract pins the preflight for tuple sections: a
// tuple DTO whose sub-column classes disagree with the target DML's
// BeginAttribute arity / container-append method fails Validate instead
// of panicking mid-marshal.
func TestValidate_TupleContract(t *testing.T) {
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)

	// One container mapped where the DML has two co-containers: the plan
	// wants AddToContainerP (C = 1), anchor's text attribute only has
	// AddToCoContainersP.
	type oneContainerElem struct {
		Label      string   `lw:"@membership,verbatim"`
		Text       string   `lw:"text:text"`
		WordLength []uint32 `lw:"text:wordLength"`
	}
	type oneContainerDoc struct {
		_        struct{}           `kind:"oneContainerDoc"`
		ID       uint64             `lw:",id"`
		Tracking []byte             `lw:",naturalKey"`
		Texts    []oneContainerElem `lw:"text"`
	}
	err := marshallreflect.Validate[oneContainerDoc](table)
	require.Error(t, err)
	require.ErrorContains(t, err, "AddToContainerP")

	// Two scalar sub-columns mapped where the DML's BeginAttribute takes
	// one: arity mismatch reported.
	type twoScalarElem struct {
		Label      string   `lw:"@membership,verbatim"`
		Text       string   `lw:"text:text"`
		Bogus      string   `lw:"text:bogus"`
		WordLength []uint32 `lw:"text:wordLength"`
		WordBag    []string `lw:"text:wordBag"`
	}
	type twoScalarDoc struct {
		_        struct{}        `kind:"twoScalarDoc"`
		ID       uint64          `lw:",id"`
		Tracking []byte          `lw:",naturalKey"`
		Texts    []twoScalarElem `lw:"text"`
	}
	err = marshallreflect.Validate[twoScalarDoc](table)
	require.Error(t, err)
	require.ErrorContains(t, err, "BeginAttribute takes 1 arg(s), want 2")
}

// TestPlanFor_TupleRejections pins the plan-time rules of the tuple
// grammar (ADR-0103): unrepresentable element structs fail at PlanFor /
// Validate for both front-ends, never as a reflect panic mid-marshal.
func TestPlanFor_TupleRejections(t *testing.T) {
	t.Run("second membership field", func(t *testing.T) {
		type elem struct {
			Label  string `lw:"@membership,verbatim"`
			Label2 string `lw:"@membership,verbatim"`
			Text   string `lw:"text:text"`
		}
		type bad struct {
			_     struct{} `kind:"bad"`
			ID    uint64   `lw:",id"`
			Texts []elem   `lw:"text"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "second `@membership` field")
	})
	t.Run("missing membership field", func(t *testing.T) {
		type elem struct {
			Text string `lw:"text:text"`
		}
		type bad struct {
			_     struct{} `kind:"bad"`
			ID    uint64   `lw:",id"`
			Texts []elem   `lw:"text"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "needs exactly one `@membership` field")
	})
	t.Run("membership without channel flag", func(t *testing.T) {
		type elem struct {
			Label string `lw:"@membership"`
			Text  string `lw:"text:text"`
		}
		type bad struct {
			_     struct{} `kind:"bad"`
			ID    uint64   `lw:",id"`
			Texts []elem   `lw:"text"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "requires an explicit verbatim channel flag")
	})
	t.Run("membership on a ref channel", func(t *testing.T) {
		type elem struct {
			Label string `lw:"@membership,highCardRef"`
			Text  string `lw:"text:text"`
		}
		type bad struct {
			_     struct{} `kind:"bad"`
			ID    uint64   `lw:",id"`
			Texts []elem   `lw:"text"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "verbatim channel")
	})
	t.Run("membership with non-string type", func(t *testing.T) {
		type elem struct {
			Label uint64 `lw:"@membership,verbatim"`
			Text  string `lw:"text:text"`
		}
		type bad struct {
			_     struct{} `kind:"bad"`
			ID    uint64   `lw:",id"`
			Texts []elem   `lw:"text"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "must be a string or []byte scalar")
	})
	t.Run("element targets a different section", func(t *testing.T) {
		type elem struct {
			Label string `lw:"@membership,verbatim"`
			Text  string `lw:"other:text"`
		}
		type bad struct {
			_     struct{} `kind:"bad"`
			ID    uint64   `lw:",id"`
			Texts []elem   `lw:"text"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "targets a different section")
	})
	t.Run("duplicate sub-column", func(t *testing.T) {
		type elem struct {
			Label string `lw:"@membership,verbatim"`
			Text  string `lw:"text:text"`
			Text2 string `lw:"text:text"`
		}
		type bad struct {
			_     struct{} `kind:"bad"`
			ID    uint64   `lw:",id"`
			Texts []elem   `lw:"text"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "sub-column appears on two tuple element fields")
	})
	t.Run("option in element", func(t *testing.T) {
		type elem struct {
			Label string                `lw:"@membership,verbatim"`
			Text  option.Option[string] `lw:"text:text"`
		}
		type bad struct {
			_     struct{} `kind:"bad"`
			ID    uint64   `lw:",id"`
			Texts []elem   `lw:"text"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "Option[T] not supported inside a tuple element")
	})
	t.Run("explode flag in element", func(t *testing.T) {
		type elem struct {
			Label string   `lw:"@membership,verbatim"`
			WL    []uint32 `lw:"text:wordLength,explode"`
		}
		type bad struct {
			_     struct{} `kind:"bad"`
			ID    uint64   `lw:",id"`
			Texts []elem   `lw:"text"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "not supported inside a tuple element")
	})
	t.Run("channel flag on a value field", func(t *testing.T) {
		type elem struct {
			Label string `lw:"@membership,verbatim"`
			Text  string `lw:"text:text,verbatim"`
		}
		type bad struct {
			_     struct{} `kind:"bad"`
			ID    uint64   `lw:",id"`
			Texts []elem   `lw:"text"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "channel flag belongs on the `@membership` field")
	})
	t.Run("no value fields", func(t *testing.T) {
		type elem struct {
			Label string `lw:"@membership,verbatim"`
		}
		type bad struct {
			_     struct{} `kind:"bad"`
			ID    uint64   `lw:",id"`
			Texts []elem   `lw:"text"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "needs at least one value field")
	})
	t.Run("static field shares the tuple's section", func(t *testing.T) {
		type elem struct {
			Label string `lw:"@membership,verbatim"`
			Text  string `lw:"text:text"`
		}
		type bad struct {
			_     struct{} `kind:"bad"`
			ID    uint64   `lw:",id"`
			Texts []elem   `lw:"text"`
			Ueber string   `lw:"prose,text:text"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "section is mapped by a tuple field")
	})
	t.Run("two tuple fields on one section", func(t *testing.T) {
		type elem struct {
			Label string `lw:"@membership,verbatim"`
			Text  string `lw:"text:text"`
		}
		type bad struct {
			_      struct{} `kind:"bad"`
			ID     uint64   `lw:",id"`
			Texts  []elem   `lw:"text"`
			Texts2 []elem   `lw:"text"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "two tuple fields map one section")
	})
	t.Run("reserved @ membership at top level", func(t *testing.T) {
		type bad struct {
			_    struct{} `kind:"bad"`
			ID   uint64   `lw:",id"`
			Text string   `lw:"@membership,text:text,verbatim"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "reserved for the tuple element grammar")
	})
	t.Run("foreign-package element struct", func(t *testing.T) {
		type bad struct {
			_    struct{}                   `kind:"bad"`
			ID   uint64                     `lw:",id"`
			Recs []anchor.InEntityTestTable `lw:"text"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "must be declared in the DTO's package")
	})
	t.Run("outer tag with flags", func(t *testing.T) {
		type elem struct {
			Label string `lw:"@membership,verbatim"`
			Text  string `lw:"text:text"`
		}
		type bad struct {
			_     struct{} `kind:"bad"`
			ID    uint64   `lw:",id"`
			Texts []elem   `lw:"text,verbatim"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "tuple field tag takes no flags")
	})
}
