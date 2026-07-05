package marshallreflect_test

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor/codecdemo"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	raruntime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

func releaseAll(recs []arrow.RecordBatch) {
	for _, r := range recs {
		r.Release()
	}
}

func idReaderFor(t *testing.T, rec arrow.RecordBatch) *anchor.ReadAccessTestTablePlainEntityIdAttributes {
	t.Helper()
	r := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	r.SetColumnIndices(r.GetColumnIndices())
	require.NoError(t, r.LoadFromRecord(rec))
	return r
}

// --- ADR-0109 gate 1: a slice of ref memberships on one attribute. ---

// refSymTag maps a `symbol` attribute (single scalar sub-column) carrying its
// type-lineage: N ref memberships as node-ids on the lowCardRef channel, the id
// carried directly as a uint64 (no lookup).
type refSymTag struct {
	Ancestors []uint64 `lw:"@membership,lowCardRef"`
	Value     string   `lw:"symbol"`
}

type refSymDoc struct {
	_ struct{} `kind:"refSymDoc"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Tags []refSymTag `lw:"symbol"`
}

// refSymRows covers per-attribute ref-membership cardinalities N = 3 / 1 / 0
// (the N = 0 element keeps a nil Ancestors so it round-trips to nil) and an
// entity with zero attributes.
func refSymRows() []refSymDoc {
	return []refSymDoc{
		{ID: 6001, Tracking: []byte("REF-A"), Tags: []refSymTag{
			{Ancestors: []uint64{10, 20, 30}, Value: "Person"},
			{Ancestors: []uint64{40}, Value: "Company"},
			{Value: "Thing"}, // N = 0 ref memberships
		}},
		{ID: 6002, Tracking: []byte("REF-B"), Tags: []refSymTag{
			{Ancestors: []uint64{7, 8}, Value: "Asset"},
		}},
		{ID: 6003, Tracking: []byte("REF-C")}, // zero attributes
	}
}

// TestReflect_TupleRefSlice_DMLByteIdentityAndRoundTrip closes ADR-0109 gate 1:
// Marshal over a slice-of-ref-memberships tuple emits bytes identical to the
// explicit DML loop (AddMembershipLowCardRefP ×N per attribute), and Unmarshal
// restores the ancestor ids in wire order (N = 0 restores nil).
func TestReflect_TupleRefSlice_DMLByteIdentityAndRoundTrip(t *testing.T) {
	original := refSymRows()

	// Explicit DML loop — the byte-identity reference: one BeginAttribute per
	// element, one AddMembershipLowCardRefP per ancestor id.
	dmlTable := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(original))
	for _, r := range original {
		dmlTable.BeginEntity()
		dmlTable.SetId(r.ID, r.Tracking)
		sec := dmlTable.GetSectionSymbol()
		for _, e := range r.Tags {
			attr := sec.BeginAttribute(e.Value)
			for _, id := range e.Ancestors {
				attr.AddMembershipLowCardRefP(id)
			}
			attr.EndAttributeP()
		}
		sec.EndSection()
		require.NoError(t, dmlTable.CommitEntity())
	}
	dmlRecs, err := dmlTable.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, dmlRecs)
	defer releaseAll(dmlRecs)

	reflectTable := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(original))
	require.NoError(t, marshallreflect.Validate[refSymDoc](reflectTable))
	require.NoError(t, marshallreflect.Marshal(reflectTable, original, marshallreflect.NoLookup{}))
	reflectRecs, err := reflectTable.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(reflectRecs)

	require.Equal(t, len(dmlRecs), len(reflectRecs))
	for i := range dmlRecs {
		require.Truef(t, array.RecordEqual(dmlRecs[i], reflectRecs[i]),
			"record %d differs:\ndml=%s\nreflect=%s", i, dmlRecs[i], reflectRecs[i])
		require.Equal(t, ipcBytes(t, dmlRecs[i]), ipcBytes(t, reflectRecs[i]))
	}

	rec := dmlRecs[0]
	symReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symReader.SetColumnIndices(symReader.GetColumnIndices())
	require.NoError(t, symReader.LoadFromRecord(rec))
	defer symReader.Release()

	// The wire really carries 3 / 1 / 0 ref memberships on entity 0's attrs.
	var counts []int
	for attrJ := int64(0); attrJ < 3; attrJ++ {
		c := 0
		for range symReader.GetMemberships().GetMembValueLowCardRef(raruntime.EntityIdx(0), raruntime.AttributeIdx(attrJ)) {
			c++
		}
		counts = append(counts, c)
	}
	require.Equal(t, []int{3, 1, 0}, counts)

	idReader := idReaderFor(t, rec)
	defer idReader.Release()
	args := marshallreflect.NewSectionReaders(idReader.Len()).
		PlainColumn("id", idReader.ValueId).
		PlainColumn("naturalKey", idReader.ValueNaturalKey).
		Section("symbol", symReader.GetAttributes(), symReader.GetMemberships())
	var got []refSymDoc
	require.NoError(t, marshallreflect.Unmarshal(args, &got, marshallreflect.NoLookup{}))
	require.Equal(t, original, got)
}

// --- ADR-0109 gate 2a: repeated FIXED ref fields (edge aliasing). ---

// edgeTag maps a `foreignKey` attribute (uint64 value) under TWO fixed ref
// memberships on the lowCardRef channel — a typed predicate and a generic graph
// membership. foreignKey declares only the LowCardRef spec.
type edgeTag struct {
	Predicate uint64 `lw:"@membership,lowCardRef"`
	Generic   uint64 `lw:"@membership,lowCardRef"`
	Target    uint64 `lw:"foreignKey"`
}

type edgeDoc struct {
	_ struct{} `kind:"edgeDoc"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Edges []edgeTag `lw:"foreignKey"`
}

func TestReflect_TupleRepeatedFixedRef_DMLByteIdentityAndRoundTrip(t *testing.T) {
	original := []edgeDoc{
		{ID: 7001, Tracking: []byte("EDG-A"), Edges: []edgeTag{
			{Predicate: 100, Generic: 200, Target: 900},
			{Predicate: 101, Generic: 200, Target: 901},
		}},
		{ID: 7002, Tracking: []byte("EDG-B")},
	}

	// DML reference: BeginAttribute(Target) + two AddMembershipLowCardRefP.
	dmlTable := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(original))
	for _, r := range original {
		dmlTable.BeginEntity()
		dmlTable.SetId(r.ID, r.Tracking)
		sec := dmlTable.GetSectionForeignKey()
		for _, e := range r.Edges {
			attr := sec.BeginAttribute(e.Target)
			attr.AddMembershipLowCardRefP(e.Predicate)
			attr.AddMembershipLowCardRefP(e.Generic)
			attr.EndAttributeP()
		}
		sec.EndSection()
		require.NoError(t, dmlTable.CommitEntity())
	}
	dmlRecs, err := dmlTable.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(dmlRecs)

	reflectTable := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(original))
	require.NoError(t, marshallreflect.Validate[edgeDoc](reflectTable))
	require.NoError(t, marshallreflect.Marshal(reflectTable, original, marshallreflect.NoLookup{}))
	reflectRecs, err := reflectTable.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(reflectRecs)

	require.Equal(t, len(dmlRecs), len(reflectRecs))
	for i := range dmlRecs {
		require.Truef(t, array.RecordEqual(dmlRecs[i], reflectRecs[i]),
			"record %d differs:\ndml=%s\nreflect=%s", i, dmlRecs[i], reflectRecs[i])
		require.Equal(t, ipcBytes(t, dmlRecs[i]), ipcBytes(t, reflectRecs[i]))
	}

	rec := reflectRecs[0]
	idReader := idReaderFor(t, rec)
	defer idReader.Release()
	fkReader := anchor.NewReadAccessTestTableTaggedForeignKey()
	fkReader.SetColumnIndices(fkReader.GetColumnIndices())
	require.NoError(t, fkReader.LoadFromRecord(rec))
	defer fkReader.Release()

	args := marshallreflect.NewSectionReaders(idReader.Len()).
		PlainColumn("id", idReader.ValueId).
		PlainColumn("naturalKey", idReader.ValueNaturalKey).
		Section("foreignKey", fkReader.GetAttributes(), fkReader.GetMemberships())
	var got []edgeDoc
	require.NoError(t, marshallreflect.Unmarshal(args, &got, marshallreflect.NoLookup{}))
	require.Equal(t, original, got)
}

// --- ADR-0109 gate 2b: HETEROGENEOUS verbatim + ref on one element. ---

// namedTextTag maps a mixed-shape `text` attribute (S = 1, C = 2) under a
// verbatim Name and a ref Kind — two memberships on two different channels.
type namedTextTag struct {
	Name       string   `lw:"@membership,verbatim"`
	Kind       uint64   `lw:"@membership,lowCardRef"`
	Text       string   `lw:"text:text"`
	WordLength []uint32 `lw:"text:wordLength"`
	WordBag    []string `lw:"text:wordBag"`
}

type namedTextDoc struct {
	_ struct{} `kind:"namedTextDoc"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Notes []namedTextTag `lw:"text"`
}

func TestReflect_TupleHeterogeneous_DMLByteIdentityAndRoundTrip(t *testing.T) {
	original := []namedTextDoc{
		{ID: 8001, Tracking: []byte("HET-A"), Notes: []namedTextTag{
			{Name: "name", Kind: 50, Text: "Ivan I.", WordLength: []uint32{4, 2}, WordBag: []string{"Ivan", "I."}},
			{Name: "alias", Kind: 51, Text: "Vanya", WordLength: []uint32{5}, WordBag: []string{"Vanya"}},
		}},
		{ID: 8002, Tracking: []byte("HET-B")},
	}

	// DML reference: verbatim name then ref kind, in declaration order.
	dmlTable := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(original))
	for _, r := range original {
		dmlTable.BeginEntity()
		dmlTable.SetId(r.ID, r.Tracking)
		sec := dmlTable.GetSectionText()
		for _, e := range r.Notes {
			attr := sec.BeginAttribute(e.Text)
			for k := range e.WordLength {
				attr.AddToCoContainersP(e.WordLength[k], e.WordBag[k])
			}
			attr.AddMembershipLowCardVerbatimP([]byte(e.Name))
			attr.AddMembershipLowCardRefP(e.Kind)
			attr.EndAttributeP()
		}
		sec.EndSection()
		require.NoError(t, dmlTable.CommitEntity())
	}
	dmlRecs, err := dmlTable.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(dmlRecs)

	reflectTable := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(original))
	require.NoError(t, marshallreflect.Validate[namedTextDoc](reflectTable))
	require.NoError(t, marshallreflect.Marshal(reflectTable, original, marshallreflect.NoLookup{}))
	reflectRecs, err := reflectTable.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(reflectRecs)

	require.Equal(t, len(dmlRecs), len(reflectRecs))
	for i := range dmlRecs {
		require.Truef(t, array.RecordEqual(dmlRecs[i], reflectRecs[i]),
			"record %d differs:\ndml=%s\nreflect=%s", i, dmlRecs[i], reflectRecs[i])
		require.Equal(t, ipcBytes(t, dmlRecs[i]), ipcBytes(t, reflectRecs[i]))
	}

	rec := dmlRecs[0]
	idReader := idReaderFor(t, rec)
	defer idReader.Release()
	textReader := anchor.NewReadAccessTestTableTaggedText()
	textReader.SetColumnIndices(textReader.GetColumnIndices())
	require.NoError(t, textReader.LoadFromRecord(rec))
	defer textReader.Release()

	args := marshallreflect.NewSectionReaders(idReader.Len()).
		PlainColumn("id", idReader.ValueId).
		PlainColumn("naturalKey", idReader.ValueNaturalKey).
		Section("text", textReader.GetAttributes(), textReader.GetMemberships())
	var got []namedTextDoc
	require.NoError(t, marshallreflect.Unmarshal(args, &got, marshallreflect.NoLookup{}))
	require.Equal(t, original, got)
}

// --- ADR-0109 gate 3: negatives. ---

// TestValidate_TupleRefChannelMissingOnSection closes gate 3: a ref @membership
// on a section whose spec lacks that ref channel fails Validate (the DML lacks
// the method), not at marshal time. foreignKey declares only LowCardRef, so a
// highCardRef membership has no AddMembershipHighCardRefP.
func TestValidate_TupleRefChannelMissingOnSection(t *testing.T) {
	type fkHighTag struct {
		Ref   uint64 `lw:"@membership,highCardRef"`
		Value uint64 `lw:"foreignKey"`
	}
	type fkHighDoc struct {
		_        struct{}    `kind:"fkHighDoc"`
		ID       uint64      `lw:",id"`
		Tracking []byte      `lw:",naturalKey"`
		Tags     []fkHighTag `lw:"foreignKey"`
	}
	// The plan itself is valid — highCardRef on a uint64 is a legal ref membership.
	_, err := marshallreflect.PlanFor[fkHighDoc]()
	require.NoError(t, err)
	// The DML contract is not: foreignKey's InAttr has no AddMembershipHighCardRefP.
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	err = marshallreflect.Validate[fkHighDoc](table)
	require.Error(t, err)
	require.ErrorContains(t, err, "AddMembershipHighCardRefP")
}

// twoRefTag has TWO fixed ref memberships; used to force a membership
// count-mismatch when the wire carries a different number.
type twoRefTag struct {
	A     uint64 `lw:"@membership,lowCardRef"`
	B     uint64 `lw:"@membership,lowCardRef"`
	Value string `lw:"symbol"`
}

type twoRefDoc struct {
	_ struct{} `kind:"twoRefDoc"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Tags []twoRefTag `lw:"symbol"`
}

// TestUnmarshal_TupleMembershipCountMismatch closes gate 3: a DTO whose fixed
// @membership count disagrees with the wire fails Unmarshal with a clear error.
func TestUnmarshal_TupleMembershipCountMismatch(t *testing.T) {
	// Hand-write ONE lowCardRef membership per attribute while twoRefDoc declares
	// two fixed fields.
	dmlTable := anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1)
	dmlTable.BeginEntity()
	dmlTable.SetId(9001, []byte("MM"))
	sec := dmlTable.GetSectionSymbol()
	attr := sec.BeginAttribute("x")
	attr.AddMembershipLowCardRefP(1) // only one — the DTO expects two
	attr.EndAttributeP()
	sec.EndSection()
	require.NoError(t, dmlTable.CommitEntity())
	recs, err := dmlTable.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	defer releaseAll(recs)

	rec := recs[0]
	idReader := idReaderFor(t, rec)
	defer idReader.Release()
	symReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symReader.SetColumnIndices(symReader.GetColumnIndices())
	require.NoError(t, symReader.LoadFromRecord(rec))
	defer symReader.Release()

	args := marshallreflect.NewSectionReaders(idReader.Len()).
		PlainColumn("id", idReader.ValueId).
		PlainColumn("naturalKey", idReader.ValueNaturalKey).
		Section("symbol", symReader.GetAttributes(), symReader.GetMemberships())
	var got []twoRefDoc
	err = marshallreflect.Unmarshal(args, &got, marshallreflect.NoLookup{})
	require.Error(t, err)
	require.ErrorContains(t, err, "membership count mismatch on read")
}

// --- ADR-0109 gate 4: gen ≡ reflect over the codecdemo demo. ---

func lineageRows() []codecdemo.LineageDoc {
	return []codecdemo.LineageDoc{
		{ID: 1001, Tracking: []byte("LIN-A"),
			Types: []codecdemo.LineageTag{
				{Ancestors: []uint64{1, 2, 3}, Kind: "Person"},
				{Ancestors: []uint64{2}, Kind: "Company"},
				{Kind: "Thing"}, // N = 0
			},
			Edges: []codecdemo.EdgeTag{
				{Predicate: 100, Generic: 200, Target: 900},
			},
			Notes: []codecdemo.NamedText{
				{Name: "name", Kind: 50, Text: "Ivan", WordLength: []uint32{4}, WordBag: []string{"Ivan"}},
			},
		},
		{ID: 1002, Tracking: []byte("LIN-B"),
			Types: []codecdemo.LineageTag{
				{Ancestors: []uint64{7, 8}, Kind: "Asset"},
			},
			Notes: []codecdemo.NamedText{
				{Name: "alias", Kind: 51, Text: "empty", WordLength: nil, WordBag: nil},
			},
		},
		{ID: 1003, Tracking: []byte("LIN-C")}, // everything empty
	}
}

// TestGenVsReflect_TupleRefMulti closes ADR-0109 gate 4: codecdemo's generated
// LineageDocBuildEntities (marshallgen) and marshallreflect.Marshal emit
// byte-identical wire for the same rows across all three shapes (slice ref,
// repeated fixed ref, heterogeneous verbatim+ref), and each front-end's record
// decodes through the other's read path.
func TestGenVsReflect_TupleRefMulti(t *testing.T) {
	original := lineageRows()
	alloc := memory.NewGoAllocator()

	// gen front-end.
	var cols codecdemo.LineageDocColumns
	for _, r := range original {
		cols.Append(r)
	}
	genTable := anchor.NewInEntityTestTable(alloc, len(original))
	require.NoError(t, codecdemo.LineageDocBuildEntities(genTable, &cols))
	genRecs, err := genTable.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, genRecs)
	defer releaseAll(genRecs)

	// reflect front-end.
	reflectTable := anchor.NewInEntityTestTable(alloc, len(original))
	require.NoError(t, marshallreflect.Validate[codecdemo.LineageDoc](reflectTable))
	require.NoError(t, marshallreflect.Marshal(reflectTable, original, marshallreflect.NoLookup{}))
	reflectRecs, err := reflectTable.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, reflectRecs)
	defer releaseAll(reflectRecs)

	require.Equal(t, len(genRecs), len(reflectRecs))
	for i := range genRecs {
		require.Truef(t, array.RecordEqual(genRecs[i], reflectRecs[i]),
			"record %d differs between gen and reflect:\ngen=%s\nreflect=%s", i, genRecs[i], reflectRecs[i])
		require.Equal(t, ipcBytes(t, genRecs[i]), ipcBytes(t, reflectRecs[i]))
	}

	// Cross-decode 1: gen-write → reflect-read.
	rec := genRecs[0]
	idReader := idReaderFor(t, rec)
	defer idReader.Release()
	symReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symReader.SetColumnIndices(symReader.GetColumnIndices())
	require.NoError(t, symReader.LoadFromRecord(rec))
	defer symReader.Release()
	fkReader := anchor.NewReadAccessTestTableTaggedForeignKey()
	fkReader.SetColumnIndices(fkReader.GetColumnIndices())
	require.NoError(t, fkReader.LoadFromRecord(rec))
	defer fkReader.Release()
	textReader := anchor.NewReadAccessTestTableTaggedText()
	textReader.SetColumnIndices(textReader.GetColumnIndices())
	require.NoError(t, textReader.LoadFromRecord(rec))
	defer textReader.Release()

	args := marshallreflect.NewSectionReaders(idReader.Len()).
		PlainColumn("id", idReader.ValueId).
		PlainColumn("naturalKey", idReader.ValueNaturalKey).
		Section("symbol", symReader.GetAttributes(), symReader.GetMemberships()).
		Section("foreignKey", fkReader.GetAttributes(), fkReader.GetMemberships()).
		Section("text", textReader.GetAttributes(), textReader.GetMemberships())
	var got []codecdemo.LineageDoc
	require.NoError(t, marshallreflect.Unmarshal(args, &got, marshallreflect.NoLookup{}))
	require.Equal(t, original, got)

	// Cross-decode 2: reflect-write → gen-read (FillFromArrow).
	rec2 := reflectRecs[0]
	idReader2 := idReaderFor(t, rec2)
	defer idReader2.Release()
	symReader2 := anchor.NewReadAccessTestTableTaggedSymbol()
	symReader2.SetColumnIndices(symReader2.GetColumnIndices())
	require.NoError(t, symReader2.LoadFromRecord(rec2))
	defer symReader2.Release()
	fkReader2 := anchor.NewReadAccessTestTableTaggedForeignKey()
	fkReader2.SetColumnIndices(fkReader2.GetColumnIndices())
	require.NoError(t, fkReader2.LoadFromRecord(rec2))
	defer fkReader2.Release()
	textReader2 := anchor.NewReadAccessTestTableTaggedText()
	textReader2.SetColumnIndices(textReader2.GetColumnIndices())
	require.NoError(t, textReader2.LoadFromRecord(rec2))
	defer textReader2.Release()

	filled := &codecdemo.LineageDocColumns{}
	require.NoError(t, codecdemo.LineageDocFillFromArrow(
		filled,
		idReader2.Len(),
		idReader2.ValueId,
		idReader2.ValueNaturalKey,
		symReader2.GetAttributes(), symReader2.GetMemberships(),
		fkReader2.GetAttributes(), fkReader2.GetMemberships(),
		textReader2.GetAttributes(), textReader2.GetMemberships(),
	))
	require.Equal(t, cols.Len(), filled.Len())
	for i := 0; i < cols.Len(); i++ {
		require.Equal(t, cols.Row(i), filled.Row(i), "row %d", i)
	}
}
