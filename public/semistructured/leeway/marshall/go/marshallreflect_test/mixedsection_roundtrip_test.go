package marshallreflect_test

import (
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor/codecdemo"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	raruntime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

// reflectTextDoc mirrors codecdemo.TextDoc (same lw: tags) so the two
// front-ends marshal identical rows through anchor's mixed-shape `text`
// section (scalar `text` + co-containers `wordLength` / `wordBag`,
// ADR-0101).
type reflectTextDoc struct {
	_ struct{} `kind:"textDoc"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Text       string   `lw:"prose,text:text"`
	WordLength []uint32 `lw:"prose,text:wordLength"`
	WordBag    []string `lw:"prose,text:wordBag"`
}

// textDocRows covers the three container cardinalities: N > 1, N = 1 and
// N = 0 (the attribute still emits — the scalar tuple is the presence
// signal; the empty containers read back as nil slices).
func textDocRows() []reflectTextDoc {
	return []reflectTextDoc{
		{ID: 2001, Tracking: []byte("TXT-A"), Text: "hello brave world", WordLength: []uint32{5, 5, 5}, WordBag: []string{"hello", "brave", "world"}},
		{ID: 2002, Tracking: []byte("TXT-B"), Text: "one", WordLength: []uint32{3}, WordBag: []string{"one"}},
		{ID: 2003, Tracking: []byte("TXT-C"), Text: "", WordLength: nil, WordBag: nil},
	}
}

// TestGenVsReflect_MixedTextSection closes the byte-identity invariant for
// the mixed-shape path (ADR-0101 D4): codecdemo.TextDocBuildEntities
// (marshallgen) and marshallreflect.Marshal must emit identical wire bytes
// for the same rows, and a gen-written record must decode through the
// reflect read side.
func TestGenVsReflect_MixedTextSection(t *testing.T) {
	original := textDocRows()
	lookup := marshallreflect.MapLookup{"prose": 1}
	allocator := memory.NewGoAllocator()

	// --- gen front-end (marshallgen BuildEntities) ---
	var cols codecdemo.TextDocColumns
	for _, r := range original {
		cols.Append(codecdemo.TextDoc{ID: r.ID, Tracking: r.Tracking, Text: r.Text, WordLength: r.WordLength, WordBag: r.WordBag})
	}
	genTable := anchor.NewInEntityTestTable(allocator, len(original))
	require.NoError(t, codecdemo.TextDocBuildEntities(genTable, &cols))
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
	require.NoError(t, marshallreflect.Validate[reflectTextDoc](reflectTable))
	require.NoError(t, marshallreflect.Marshal(reflectTable, original, lookup))
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

	// Cross-decode: gen-write → reflect-read.
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
	var got []reflectTextDoc
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(original), len(got))
	for i := range original {
		require.Equal(t, original[i], got[i], "row %d", i)
	}
}

// TestReflect_MixedTextSection_ZipLengthMismatch pins the runtime
// zip-length contract (ADR-0101 D2): co-container slices of unequal
// length are an error, not a panic and not silent truncation.
func TestReflect_MixedTextSection_ZipLengthMismatch(t *testing.T) {
	rows := []reflectTextDoc{
		{ID: 1, Tracking: []byte("BAD"), Text: "x", WordLength: []uint32{1, 2}, WordBag: []string{"a"}},
	}
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(rows))
	err := marshallreflect.Marshal(table, rows, marshallreflect.MapLookup{"prose": 1})
	require.Error(t, err)
	require.ErrorContains(t, err, "co-container slices have different lengths")
}

// reflectGeoBlob targets anchor's all-container `geoArea` section
// (polyLat F32h + polyLng F32h + h3 U64m — S = 0, C = 3). A row with
// every container empty is spliced: no attribute on the wire, nil
// slices on read-back (ADR-0101 D2).
type reflectGeoBlob struct {
	_ struct{} `kind:"geoBlob"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	PolyLat []float32 `lw:"geo,geoArea:polyLat"`
	PolyLng []float32 `lw:"geo,geoArea:polyLng"`
	H3      []uint64  `lw:"geo,geoArea:h3"`
}

func TestReflect_GeoAreaAllContainerTuple(t *testing.T) {
	original := []reflectGeoBlob{
		{ID: 3001, Tracking: []byte("GEO-A"), PolyLat: []float32{1.5, 2.5}, PolyLng: []float32{3.5, 4.5}, H3: []uint64{10, 11}},
		{ID: 3002, Tracking: []byte("GEO-B")}, // all containers empty → spliced
	}
	lookup := marshallreflect.MapLookup{"geo": 1}
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(original))
	require.NoError(t, marshallreflect.Validate[reflectGeoBlob](table))
	require.NoError(t, marshallreflect.Marshal(table, original, lookup))

	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
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

	geoReader := anchor.NewReadAccessTestTableTaggedGeoArea()
	geoReader.SetColumnIndices(geoReader.GetColumnIndices())
	require.NoError(t, geoReader.LoadFromRecord(rec))
	defer geoReader.Release()

	// The splice is visible on the wire: row 0 carries one attribute,
	// row 1 none.
	require.Equal(t, int64(1), geoReader.GetAttributes().GetNumberOfAttributes(raruntime.EntityIdx(0)))
	require.Equal(t, int64(0), geoReader.GetAttributes().GetNumberOfAttributes(raruntime.EntityIdx(1)))

	args := marshallreflect.NewSectionReaders(idReader.Len()).
		PlainColumn("id", idReader.ValueId).
		PlainColumn("naturalKey", idReader.ValueNaturalKey).
		Section("geoArea", geoReader.GetAttributes(), geoReader.GetMemberships())
	var got []reflectGeoBlob
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(original), len(got))
	for i := range original {
		require.Equal(t, original[i], got[i], "row %d", i)
	}
}

// plainOnlyOwner supplies the entity frame for the RowComposer pass test
// without contributing any tagged section.
type plainOnlyOwner struct {
	_ struct{} `kind:"plainOnly"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`
}

// TestRowComposer_MixedCardinalityPasses pins ADR-0101 D7: the shared
// container length N routes the tuple attribute into the single-value
// pass (N ≤ 1) or the multi-value pass (N > 1).
func TestRowComposer_MixedCardinalityPasses(t *testing.T) {
	lookup := marshallreflect.MapLookup{"prose": 1}
	multi := reflectTextDoc{ID: 1, Tracking: []byte("A"), Text: "hello brave world", WordLength: []uint32{5, 5, 5}, WordBag: []string{"hello", "brave", "world"}}
	single := reflectTextDoc{ID: 2, Tracking: []byte("B"), Text: "one", WordLength: []uint32{3}, WordBag: []string{"one"}}

	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 2)
	m := marshallreflect.NewRowComposer(table, lookup)

	// Row 0: N = 3 → emits only in the multi-value pass.
	require.NoError(t, m.BeginRow(plainOnlyOwner{ID: multi.ID, Tracking: multi.Tracking}))
	require.NoError(t, m.AddSingleValueAttributes(multi))
	require.NoError(t, m.AddMultiValueAttributes(multi))
	require.NoError(t, m.CommitRow())

	// Row 1: N = 1 → emits only in the single-value pass.
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

	// Each row carries exactly one text attribute — each pass emitted it
	// exactly once (never zero, never twice).
	require.Equal(t, int64(1), textReader.GetAttributes().GetNumberOfAttributes(raruntime.EntityIdx(0)))
	require.Equal(t, int64(1), textReader.GetAttributes().GetNumberOfAttributes(raruntime.EntityIdx(1)))
}

// TestValidate_MixedSectionContract pins the tightened preflight
// (ADR-0101 D3): a DTO whose sub-column classes disagree with the target
// DML's BeginAttribute arity / container-append method fails Validate
// instead of panicking mid-marshal.
func TestValidate_MixedSectionContract(t *testing.T) {
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)

	// One container mapped where the DML has two co-containers: the plan
	// wants AddToContainerP (C = 1), anchor's text attribute only has
	// AddToCoContainersP.
	type textDocOneContainer struct {
		_          struct{} `kind:"textDocOneContainer"`
		ID         uint64   `lw:",id"`
		Tracking   []byte   `lw:",naturalKey"`
		Text       string   `lw:"prose,text:text"`
		WordLength []uint32 `lw:"prose,text:wordLength"`
	}
	err := marshallreflect.Validate[textDocOneContainer](table)
	require.Error(t, err)
	require.ErrorContains(t, err, "AddToContainerP")

	// Two scalar sub-columns mapped where the DML's BeginAttribute takes
	// one: arity mismatch reported.
	type textDocTwoScalars struct {
		_          struct{} `kind:"textDocTwoScalars"`
		ID         uint64   `lw:",id"`
		Tracking   []byte   `lw:",naturalKey"`
		Text       string   `lw:"prose,text:text"`
		Bogus      string   `lw:"prose,text:bogus"`
		WordLength []uint32 `lw:"prose,text:wordLength"`
		WordBag    []string `lw:"prose,text:wordBag"`
	}
	err = marshallreflect.Validate[textDocTwoScalars](table)
	require.Error(t, err)
	require.ErrorContains(t, err, "BeginAttribute takes 1 arg(s), want 2")
}

// TestPlanFor_MixedSectionRejections pins the plan-time structural rules
// (ADR-0101 D3): shapes the tuple model cannot express fail at PlanFor /
// Validate time for both front-ends, never at marshal time.
func TestPlanFor_MixedSectionRejections(t *testing.T) {
	t.Run("explode", func(t *testing.T) {
		type bad struct {
			_    struct{} `kind:"bad"`
			ID   uint64   `lw:",id"`
			Text string   `lw:"prose,text:text"`
			WL   []uint32 `lw:"prose,text:wordLength,explode"`
			WB   []string `lw:"prose,text:wordBag"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "`explode` not supported in a multi-sub-column section")
	})
	t.Run("unit", func(t *testing.T) {
		type bad struct {
			_    struct{} `kind:"bad"`
			ID   uint64   `lw:",id"`
			Text string   `lw:"prose,text:text,unit"`
			WL   []uint32 `lw:"prose,text:wordLength"`
			WB   []string `lw:"prose,text:wordBag"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "`unit` not supported in a multi-sub-column section")
	})
	t.Run("option", func(t *testing.T) {
		type bad struct {
			_    struct{}              `kind:"bad"`
			ID   uint64                `lw:",id"`
			Text option.Option[string] `lw:"prose,text:text"`
			WL   []uint32              `lw:"prose,text:wordLength"`
			WB   []string              `lw:"prose,text:wordBag"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "Option[T] not supported in a multi-sub-column section")
	})
	t.Run("roaring", func(t *testing.T) {
		type bad struct {
			_    struct{}        `kind:"bad"`
			ID   uint64          `lw:",id"`
			Text string          `lw:"prose,text:text"`
			WL   *roaring.Bitmap `lw:"prose,text:wordLength"`
			WB   []string        `lw:"prose,text:wordBag"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "roaring.Bitmap not supported in a multi-sub-column section")
	})
	t.Run("multi-field sub-column", func(t *testing.T) {
		type bad struct {
			_     struct{} `kind:"bad"`
			ID    uint64   `lw:",id"`
			Text  string   `lw:"prose,text:text"`
			Text2 string   `lw:"other,text:text"`
			WL    []uint32 `lw:"prose,text:wordLength"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "multi-field sub-column in multi-sub-column section not supported")
	})
	t.Run("multiple memberships", func(t *testing.T) {
		// Static form: separate fields with differing memberships on one
		// multi-sub-column section stay rejected — which membership pairs
		// with which sub-column tuple is genuinely ambiguous.
		type bad struct {
			_    struct{} `kind:"bad"`
			ID   uint64   `lw:",id"`
			Text string   `lw:"prose,text:text"`
			WL   []uint32 `lw:"other,text:wordLength"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "multi-sub-column section with multiple memberships not supported")

		// Tuple form (ADR-0103): many memberships on one multi-sub-column
		// section ARE expressible — one per element of a slice-of-struct
		// field, each element one attribute. The same intent, accepted.
		type textElem struct {
			Label      string   `lw:"@membership,verbatim"`
			Text       string   `lw:"text:text"`
			WordLength []uint32 `lw:"text:wordLength"`
			WordBag    []string `lw:"text:wordBag"`
		}
		type good struct {
			_     struct{}   `kind:"good"`
			ID    uint64     `lw:",id"`
			Texts []textElem `lw:"text"`
		}
		plan, err := marshallreflect.PlanFor[good]()
		require.NoError(t, err)
		require.Len(t, plan.Fields, 3) // one TaggedField per sub-column
		for _, f := range plan.Fields {
			require.Equal(t, "Texts", f.TupleField)
			require.Len(t, f.TupleMemberships, 1)
			require.Equal(t, "Label", f.TupleMemberships[0].GoField)
		}
	})
	t.Run("const", func(t *testing.T) {
		// Verbatim channel throughout so the earlier kindXxx-collision rule
		// (const + value field sharing a ref membership) does not fire first.
		type bad struct {
			_    struct{} `kind:"bad"`
			_    struct{} `lw:"marker,text,verbatim,const=fixed"`
			ID   uint64   `lw:",id"`
			Text string   `lw:"marker,text:text,verbatim"`
			WL   []uint32 `lw:"marker,text:wordLength,verbatim"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "const field cannot share a multi-sub-column section")
	})
}
