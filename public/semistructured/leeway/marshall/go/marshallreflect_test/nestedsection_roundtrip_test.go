package marshallreflect_test

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	lw "github.com/stergiotis/boxer/public/semistructured/leeway/marshall/lw"
)

// Slice A, Step 1a — the nested-struct front-end's architecture prover.
//
// A NESTED, static-membership, cardinality-One section maps a struct value
// (whose fields are the section's sub-columns) onto a leeway section, emitting
// exactly one attribute per row. It must produce wire output byte-identical to
// the equivalent FLAT multi-sub-column DTO — proving the whole nested→plan→wire
// lowering (goplan.AddNestedSliceField), the AttrCardinalityOne enumeration, the
// static-membership resolution (H1: addMembership, not the raw dynamic path),
// and all four TupleSpec() dispatch sites — against the shipped flat rangeDrone
// as the byte reference.

// rangeWindow is the nested attribute struct for anchor's timeRange section:
// two time.Time scalar sub-columns. The sub-column column names (beginIncl /
// endExcl) differ from the Go field names, so they carry an explicit
// `lw:"<column>"` tag. Declaration order (Begin before End) matches the flat
// rangeDrone so the BeginAttribute(beginIncl, endExcl) argument order — hence
// the wire — is identical.
type rangeWindow struct {
	Begin time.Time `lw:"beginIncl"`
	End   time.Time `lw:"endExcl"`
}

// nestedRangeDrone is the nested spelling of rangeDrone (shapes_roundtrip_test.go):
// the two timeRange sub-columns live inside a struct value under one static
// membership ("window"), instead of two sibling fields sharing a tag.
type nestedRangeDrone struct {
	_        struct{}    `kind:"rangeDrone"`
	ID       uint64      `lw:",id"`
	Tracking []byte      `lw:",naturalKey"`
	Window   rangeWindow `lw:"window,timeRange"`
}

func TestNested_StaticOne_TimeRange_ByteIdenticalToFlat(t *testing.T) {
	lookup := marshallreflect.MapLookup{"window": 1}
	b1 := time.Unix(1_600_000_000, 0).UTC()
	e1 := time.Unix(1_600_003_600, 0).UTC()
	b2 := time.Unix(1_600_100_000, 0).UTC()
	e2 := time.Unix(1_600_200_000, 0).UTC()

	flat := []rangeDrone{
		{ID: 1, Tracking: []byte("R1"), Begin: b1, End: e1},
		{ID: 2, Tracking: []byte("R2"), Begin: b2, End: e2},
	}
	nested := []nestedRangeDrone{
		{ID: 1, Tracking: []byte("R1"), Window: rangeWindow{Begin: b1, End: e1}},
		{ID: 2, Tracking: []byte("R2"), Window: rangeWindow{Begin: b2, End: e2}},
	}

	// Preflight: the nested DTO satisfies the same DML write contract (exercises
	// the static-membership branch of checkSectionAttrContract — H7).
	require.NoError(t, marshallreflect.Validate[nestedRangeDrone](anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(nested))))

	flatRec, relF := marshalToRecord(t, flat, lookup)
	defer relF()
	nestedRec, relN := marshalToRecord(t, nested, lookup)
	defer relN()

	// The load-bearing invariant: the nested spelling is byte-identical to the
	// flat one — logically (RecordEqual) and on the wire (Arrow IPC bytes).
	require.True(t, array.RecordEqual(flatRec, nestedRec),
		"records differ:\nflat=%s\nnested=%s", flatRec, nestedRec)
	require.Equal(t, ipcBytes(t, flatRec), ipcBytes(t, nestedRec), "Arrow IPC bytes differ")

	// Round-trip: read the nested record back into the nested DTO.
	idReader, relID := loadIdReader(t, nestedRec)
	defer relID()
	trReader := anchor.NewReadAccessTestTableTaggedTimeRange()
	trReader.SetColumnIndices(trReader.GetColumnIndices())
	require.NoError(t, trReader.LoadFromRecord(nestedRec))
	defer trReader.Release()

	// One attribute per row (cardinality One).
	require.Equal(t, int64(1), trReader.GetAttributes().GetNumberOfAttributes(0))
	require.Equal(t, int64(1), trReader.GetAttributes().GetNumberOfAttributes(1))

	args := idReaders(idReader).
		Section("timeRange", trReader.GetAttributes(), trReader.GetMemberships())
	var got []nestedRangeDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(nested), len(got))
	for i := range nested {
		require.Equal(t, nested[i].ID, got[i].ID, "row %d ID", i)
		require.Equal(t, nested[i].Window.Begin.Unix(), got[i].Window.Begin.Unix(), "row %d Begin", i)
		require.Equal(t, nested[i].Window.End.Unix(), got[i].Window.End.Unix(), "row %d End", i)
		require.NotEqual(t, got[i].Window.Begin.Unix(), got[i].Window.End.Unix(), "row %d begin/end distinct", i)
	}
}

// TestNested_StaticOne_CrossDecodeFlat proves the two spellings are wire-compatible
// both ways: a record written by the FLAT DTO reads back into the NESTED DTO.
func TestNested_StaticOne_CrossDecodeFlat(t *testing.T) {
	lookup := marshallreflect.MapLookup{"window": 1}
	b := time.Unix(1_600_000_000, 0).UTC()
	e := time.Unix(1_600_003_600, 0).UTC()

	flat := []rangeDrone{{ID: 7, Tracking: []byte("X7"), Begin: b, End: e}}
	flatRec, rel := marshalToRecord(t, flat, lookup)
	defer rel()

	idReader, relID := loadIdReader(t, flatRec)
	defer relID()
	trReader := anchor.NewReadAccessTestTableTaggedTimeRange()
	trReader.SetColumnIndices(trReader.GetColumnIndices())
	require.NoError(t, trReader.LoadFromRecord(flatRec))
	defer trReader.Release()

	args := idReaders(idReader).
		Section("timeRange", trReader.GetAttributes(), trReader.GetMemberships())
	var got []nestedRangeDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Len(t, got, 1)
	require.Equal(t, uint64(7), got[0].ID)
	require.Equal(t, b.Unix(), got[0].Window.Begin.Unix())
	require.Equal(t, e.Unix(), got[0].Window.End.Unix())
}

// Slice A, Step 1b — a nested single-container, all-container (no scalar) section.
//
// It exercises the container-append path in marshalTupleSection and the S=0
// splice (H2): a cardinality-One all-container element whose container is empty
// emits ZERO attributes, exactly like the flat single-sub-column container path
// (marshalContainer). The single sub-column takes the flat default column name
// "value" (the tag-free case). Byte reference: the shipped flat sliceDrone.

// u32Nest is the nested attribute struct for anchor's u32Array section: one
// container sub-column. Tag-free → the sub-column takes the flat default column
// name "value".
type u32Nest struct {
	Values []uint32
}

type nestedSliceDrone struct {
	_        struct{} `kind:"sliceDrone"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Codes    u32Nest  `lw:"codes,u32Array"`
}

func TestNested_StaticOne_SingleContainer_ByteIdenticalToFlat(t *testing.T) {
	lookup := marshallreflect.MapLookup{"codes": 1}

	// Row 2 carries an empty container — it must splice to zero attributes in
	// both spellings (H2), so the two records stay byte-identical.
	flat := []sliceDrone{
		{ID: 1, Tracking: []byte("S1"), Codes: []uint32{30, 10, 20, 10}},
		{ID: 2, Tracking: []byte("S2"), Codes: nil},
		{ID: 3, Tracking: []byte("S3"), Codes: []uint32{99}},
	}
	nested := []nestedSliceDrone{
		{ID: 1, Tracking: []byte("S1"), Codes: u32Nest{Values: []uint32{30, 10, 20, 10}}},
		{ID: 2, Tracking: []byte("S2"), Codes: u32Nest{Values: nil}},
		{ID: 3, Tracking: []byte("S3"), Codes: u32Nest{Values: []uint32{99}}},
	}

	require.NoError(t, marshallreflect.Validate[nestedSliceDrone](anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(nested))))

	flatRec, relF := marshalToRecord(t, flat, lookup)
	defer relF()
	nestedRec, relN := marshalToRecord(t, nested, lookup)
	defer relN()

	require.True(t, array.RecordEqual(flatRec, nestedRec),
		"records differ:\nflat=%s\nnested=%s", flatRec, nestedRec)
	require.Equal(t, ipcBytes(t, flatRec), ipcBytes(t, nestedRec), "Arrow IPC bytes differ")

	idReader, relID := loadIdReader(t, nestedRec)
	defer relID()
	u32Reader := anchor.NewReadAccessTestTableTaggedU32Array()
	u32Reader.SetColumnIndices(u32Reader.GetColumnIndices())
	require.NoError(t, u32Reader.LoadFromRecord(nestedRec))
	defer u32Reader.Release()

	// H2: the empty-container row emits zero attributes (the splice).
	require.Equal(t, int64(1), u32Reader.GetAttributes().GetNumberOfAttributes(0))
	require.Equal(t, int64(0), u32Reader.GetAttributes().GetNumberOfAttributes(1))
	require.Equal(t, int64(1), u32Reader.GetAttributes().GetNumberOfAttributes(2))

	args := idReaders(idReader).
		Section("u32Array", u32Reader.GetAttributes(), u32Reader.GetMemberships())
	var got []nestedSliceDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(nested), len(got))
	require.Equal(t, []uint32{30, 10, 20, 10}, got[0].Codes.Values, "row 0 container order + multiplicity")
	require.Nil(t, got[1].Codes.Values, "row 1 spliced empty container reads back nil")
	require.Equal(t, []uint32{99}, got[2].Codes.Values, "row 2 container")
}

// Slice A, Step 1c — a nested MIXED section: a scalar sub-column plus co-container
// sub-columns, authored as PARALLEL []T fields (the flat-tuple-element form; the
// chosen 1c design, not a bundled []Word). It must be byte-identical to the flat
// multi-sub-column textDoc, with the co-containers zipping in lockstep
// (equal-length checked at runtime). No new codec machinery — 1a/1b's widened
// tuple path already emits/reads a scalar + N co-containers.

type flatTextDoc struct {
	_          struct{} `kind:"textDoc"`
	ID         uint64   `lw:",id"`
	Tracking   []byte   `lw:",naturalKey"`
	Text       string   `lw:"prose,text:text"`
	WordLength []uint32 `lw:"prose,text:wordLength"`
	WordBag    []string `lw:"prose,text:wordBag"`
}

// prose is the nested attribute struct for anchor's mixed text section: one
// scalar sub-column plus two parallel co-container sub-columns.
type prose struct {
	Text       string   `lw:"text"`
	WordLength []uint32 `lw:"wordLength"`
	WordBag    []string `lw:"wordBag"`
}

type nestedTextDoc struct {
	_        struct{} `kind:"textDoc"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Body     prose    `lw:"prose,text"`
}

func TestNested_StaticOne_CoContainers_ByteIdenticalToFlat(t *testing.T) {
	lookup := marshallreflect.MapLookup{"prose": 1}

	// Row 3 has the scalar present but empty co-containers (N=0): the attribute
	// still emits (the scalar is the presence signal) in both spellings.
	flat := []flatTextDoc{
		{ID: 1, Tracking: []byte("T1"), Text: "hello world", WordLength: []uint32{5, 5}, WordBag: []string{"hello", "world"}},
		{ID: 2, Tracking: []byte("T2"), Text: "hi", WordLength: []uint32{2}, WordBag: []string{"hi"}},
		{ID: 3, Tracking: []byte("T3"), Text: "empty", WordLength: nil, WordBag: nil},
	}
	nested := []nestedTextDoc{
		{ID: 1, Tracking: []byte("T1"), Body: prose{Text: "hello world", WordLength: []uint32{5, 5}, WordBag: []string{"hello", "world"}}},
		{ID: 2, Tracking: []byte("T2"), Body: prose{Text: "hi", WordLength: []uint32{2}, WordBag: []string{"hi"}}},
		{ID: 3, Tracking: []byte("T3"), Body: prose{Text: "empty", WordLength: nil, WordBag: nil}},
	}

	require.NoError(t, marshallreflect.Validate[nestedTextDoc](anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(nested))))

	flatRec, relF := marshalToRecord(t, flat, lookup)
	defer relF()
	nestedRec, relN := marshalToRecord(t, nested, lookup)
	defer relN()

	require.True(t, array.RecordEqual(flatRec, nestedRec),
		"records differ:\nflat=%s\nnested=%s", flatRec, nestedRec)
	require.Equal(t, ipcBytes(t, flatRec), ipcBytes(t, nestedRec), "Arrow IPC bytes differ")

	idReader, relID := loadIdReader(t, nestedRec)
	defer relID()
	txReader := anchor.NewReadAccessTestTableTaggedText()
	txReader.SetColumnIndices(txReader.GetColumnIndices())
	require.NoError(t, txReader.LoadFromRecord(nestedRec))
	defer txReader.Release()

	// Scalar present ⇒ one attribute per row, including the empty-co-container row.
	require.Equal(t, int64(1), txReader.GetAttributes().GetNumberOfAttributes(0))
	require.Equal(t, int64(1), txReader.GetAttributes().GetNumberOfAttributes(2))

	args := idReaders(idReader).
		Section("text", txReader.GetAttributes(), txReader.GetMemberships())
	var got []nestedTextDoc
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(nested), len(got))
	require.Equal(t, "hello world", got[0].Body.Text)
	require.Equal(t, []uint32{5, 5}, got[0].Body.WordLength, "row 0 co-container 1")
	require.Equal(t, []string{"hello", "world"}, got[0].Body.WordBag, "row 0 co-container 2")
	require.Equal(t, "hi", got[1].Body.Text)
	require.Equal(t, []uint32{2}, got[1].Body.WordLength)
	require.Equal(t, "empty", got[2].Body.Text)
	require.Nil(t, got[2].Body.WordLength, "row 2 empty co-container reads back nil")
	require.Nil(t, got[2].Body.WordBag)
}

// Slice A, Step 3 — Optional cardinality (`*S` and `option.Option[S]`): a section
// carrying zero-or-one attribute. A single-scalar Optional section is byte-identical
// to the flat `option.Option[T]` scalar (the shipped optDrone): present → one
// attribute, absent → zero (the splice).

type symOpt struct {
	Val string // single scalar sub-column, column "value" (tag-free)
}
type nestedOptPtr struct {
	_        struct{} `kind:"optDrone"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Note     *symOpt  `lw:"note,symbol"` // Optional via pointer
}
type nestedOptOpt struct {
	_        struct{}              `kind:"optDrone"`
	ID       uint64                `lw:",id"`
	Tracking []byte                `lw:",naturalKey"`
	Note     option.Option[symOpt] `lw:"note,symbol"` // Optional via option.Option
}

func TestNested_Optional_ByteIdenticalToFlatOption(t *testing.T) {
	lookup := marshallreflect.MapLookup{"note": 1}

	flat := []optDrone{
		{ID: 1, Tracking: []byte("O1"), Note: option.Some("hello")},
		{ID: 2, Tracking: []byte("O2"), Note: option.None[string]()},
		{ID: 3, Tracking: []byte("O3"), Note: option.Some("world")},
	}
	ptr := []nestedOptPtr{
		{ID: 1, Tracking: []byte("O1"), Note: &symOpt{Val: "hello"}},
		{ID: 2, Tracking: []byte("O2"), Note: nil},
		{ID: 3, Tracking: []byte("O3"), Note: &symOpt{Val: "world"}},
	}
	opt := []nestedOptOpt{
		{ID: 1, Tracking: []byte("O1"), Note: option.Some(symOpt{Val: "hello"})},
		{ID: 2, Tracking: []byte("O2"), Note: option.None[symOpt]()},
		{ID: 3, Tracking: []byte("O3"), Note: option.Some(symOpt{Val: "world"})},
	}

	flatRec, r1 := marshalToRecord(t, flat, lookup)
	defer r1()
	ptrRec, r2 := marshalToRecord(t, ptr, lookup)
	defer r2()
	optRec, r3 := marshalToRecord(t, opt, lookup)
	defer r3()

	// Both Optional spellings are byte-identical to the flat option.Option[T].
	require.Equal(t, ipcBytes(t, flatRec), ipcBytes(t, ptrRec), "*S Optional differs from flat Option")
	require.Equal(t, ipcBytes(t, flatRec), ipcBytes(t, optRec), "option.Option[S] differs from flat Option")

	// Round-trip the pointer form; the absent row stays a nil pointer.
	idReader, rID := loadIdReader(t, ptrRec)
	defer rID()
	symReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symReader.SetColumnIndices(symReader.GetColumnIndices())
	require.NoError(t, symReader.LoadFromRecord(ptrRec))
	defer symReader.Release()
	require.Equal(t, int64(1), symReader.GetAttributes().GetNumberOfAttributes(0))
	require.Equal(t, int64(0), symReader.GetAttributes().GetNumberOfAttributes(1)) // absent

	args := idReaders(idReader).
		Section("symbol", symReader.GetAttributes(), symReader.GetMemberships())
	var got []nestedOptPtr
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))
	require.Len(t, got, 3)
	require.NotNil(t, got[0].Note)
	require.Equal(t, "hello", got[0].Note.Val)
	require.Nil(t, got[1].Note, "absent → nil pointer")
	require.NotNil(t, got[2].Note)
	require.Equal(t, "world", got[2].Note.Val)
}

// Slice A, Step 3 — static-Many cardinality (`[]S` with a static membership): N
// attributes per row, all carrying the SAME static membership. A single-scalar
// static-Many section is byte-identical to the flat `,explode` shape (N single-value
// attributes, one membership), which the flat grammar reaches only for a single
// sub-column. The `[]S` form is disambiguated from a dynamic-membership tuple by
// the tag naming a membership (`tags,symbol` vs a bare `symbol`).

type symOne struct {
	Val string
}
type flatExplodeSym struct {
	_        struct{} `kind:"manyDrone"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Tags     []string `lw:"tags,symbol,explode"`
}
type nestedManySym struct {
	_        struct{} `kind:"manyDrone"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Blocks   []symOne `lw:"tags,symbol"`
}

func TestNested_StaticMany_ByteIdenticalToFlatExplode(t *testing.T) {
	lookup := marshallreflect.MapLookup{"tags": 1}

	flat := []flatExplodeSym{
		{ID: 1, Tracking: []byte("M1"), Tags: []string{"a", "b", "c"}},
		{ID: 2, Tracking: []byte("M2"), Tags: []string{"x"}},
		{ID: 3, Tracking: []byte("M3"), Tags: nil},
	}
	nested := []nestedManySym{
		{ID: 1, Tracking: []byte("M1"), Blocks: []symOne{{Val: "a"}, {Val: "b"}, {Val: "c"}}},
		{ID: 2, Tracking: []byte("M2"), Blocks: []symOne{{Val: "x"}}},
		{ID: 3, Tracking: []byte("M3"), Blocks: nil},
	}

	require.NoError(t, marshallreflect.Validate[nestedManySym](anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(nested))))

	flatRec, r1 := marshalToRecord(t, flat, lookup)
	defer r1()
	nestedRec, r2 := marshalToRecord(t, nested, lookup)
	defer r2()

	require.True(t, array.RecordEqual(flatRec, nestedRec),
		"records differ:\nflat=%s\nnested=%s", flatRec, nestedRec)
	require.Equal(t, ipcBytes(t, flatRec), ipcBytes(t, nestedRec), "Arrow IPC bytes differ")

	idReader, rID := loadIdReader(t, nestedRec)
	defer rID()
	symReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symReader.SetColumnIndices(symReader.GetColumnIndices())
	require.NoError(t, symReader.LoadFromRecord(nestedRec))
	defer symReader.Release()

	// N attributes per row, one per []S element.
	require.Equal(t, int64(3), symReader.GetAttributes().GetNumberOfAttributes(0))
	require.Equal(t, int64(1), symReader.GetAttributes().GetNumberOfAttributes(1))
	require.Equal(t, int64(0), symReader.GetAttributes().GetNumberOfAttributes(2))

	args := idReaders(idReader).
		Section("symbol", symReader.GetAttributes(), symReader.GetMemberships())
	var got []nestedManySym
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))
	require.Len(t, got, 3)
	require.Equal(t, []symOne{{Val: "a"}, {Val: "b"}, {Val: "c"}}, got[0].Blocks)
	require.Equal(t, []symOne{{Val: "x"}}, got[1].Blocks)
	require.Nil(t, got[2].Blocks, "zero elements → nil slice")
}

// Slice A, Step 4 — the lw marker package. lw.Single[T] is the value-shape marker
// for the unit / BeginAttributeSingle shape (a container column carrying one
// element as T); at the entity level it is byte-identical to a flat `,unit` field.

type flatUnitDrone struct {
	_        struct{} `kind:"unitDrone"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Batt     uint64   `lw:"batt,u64Array,unit"`
}
type nestedUnitDrone struct {
	_        struct{}          `kind:"unitDrone"`
	ID       uint64            `lw:",id"`
	Tracking []byte            `lw:",naturalKey"`
	Batt     lw.Single[uint64] `lw:"batt,u64Array"`
}

func TestNested_EntitySingle_ByteIdenticalToFlatUnit(t *testing.T) {
	lookup := marshallreflect.MapLookup{"batt": 1}
	flat := []flatUnitDrone{
		{ID: 1, Tracking: []byte("U1"), Batt: 88},
		{ID: 2, Tracking: []byte("U2"), Batt: 12},
	}
	nested := []nestedUnitDrone{
		{ID: 1, Tracking: []byte("U1"), Batt: lw.Single[uint64]{Val: 88}},
		{ID: 2, Tracking: []byte("U2"), Batt: lw.One[uint64](12)},
	}

	require.NoError(t, marshallreflect.Validate[nestedUnitDrone](anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(nested))))

	flatRec, r1 := marshalToRecord(t, flat, lookup)
	defer r1()
	nestedRec, r2 := marshalToRecord(t, nested, lookup)
	defer r2()

	require.True(t, array.RecordEqual(flatRec, nestedRec),
		"records differ:\nflat=%s\nnested=%s", flatRec, nestedRec)
	require.Equal(t, ipcBytes(t, flatRec), ipcBytes(t, nestedRec), "Arrow IPC bytes differ")

	idReader, rID := loadIdReader(t, nestedRec)
	defer rID()
	u64Reader := anchor.NewReadAccessTestTableTaggedU64Array()
	u64Reader.SetColumnIndices(u64Reader.GetColumnIndices())
	require.NoError(t, u64Reader.LoadFromRecord(nestedRec))
	defer u64Reader.Release()
	require.Equal(t, int64(1), u64Reader.GetAttributes().GetNumberOfAttributes(0))

	args := idReaders(idReader).
		Section("u64Array", u64Reader.GetAttributes(), u64Reader.GetMemberships())
	var got []nestedUnitDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))
	require.Len(t, got, 2)
	require.Equal(t, uint64(88), got[0].Batt.Val, "lw.Single unwraps to Val on read")
	require.Equal(t, uint64(12), got[1].Batt.Val)
}

// nestedIPDrone is the lane form of ipDrone (canonicaloverride_test): lw.IPv4 /
// lw.IPv6 relabel a [4]byte / [16]byte to the network canonical by TYPE, exactly
// as `,ct=v` / `,ct=w` do by flag. Verified at the Plan level (the sections have
// no marshallable RA readers, like the flat ct= test).
type nestedIPDrone struct {
	_   struct{} `kind:"ipdn"`
	Id  uint64   `lw:",id"`
	Src lw.IPv4  `lw:"src,ipv4Section"`
	Dst lw.IPv6  `lw:"dst,ipv6Section"`
}

func TestNested_Lanes_PlanCanonicalMatchesCt(t *testing.T) {
	plan, err := marshallreflect.PlanFor[nestedIPDrone]()
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, f := range plan.Fields {
		switch f.GoFieldName {
		case "Src":
			require.True(t, f.Canonical.IsNetworkNode(), "lw.IPv4 → network canonical (≡ ,ct=v)")
			require.Equal(t, "[4]byte", f.GoType(), "lw.IPv4 Go shape is [4]byte")
			seen["Src"] = true
		case "Dst":
			require.True(t, f.Canonical.IsNetworkNode(), "lw.IPv6 → network canonical (≡ ,ct=w)")
			require.Equal(t, "[16]byte", f.GoType(), "lw.IPv6 Go shape is [16]byte")
			seen["Dst"] = true
		}
	}
	require.True(t, seen["Src"] && seen["Dst"], "both lane fields present")
}

// nestedCidrDrone is the lane form of ipCidrDrone (canonicaloverride_test):
// lw.IPv4Prefix / lw.IPv6Prefix relabel a [5]byte / [17]byte to the CIDR network
// canonical by TYPE, exactly as `,ct=vc` / `,ct=wc` do by flag. Verified at the
// Plan level, like the flat ct= CIDR test.
type nestedCidrDrone struct {
	_    struct{}      `kind:"ipcn"`
	Id   uint64        `lw:",id"`
	Net4 lw.IPv4Prefix `lw:"net4,cidr4Section"`
	Net6 lw.IPv6Prefix `lw:"net6,cidr6Section"`
}

func TestNested_CidrLanes_PlanCanonicalMatchesCt(t *testing.T) {
	plan, err := marshallreflect.PlanFor[nestedCidrDrone]()
	require.NoError(t, err)

	got := map[string]string{}
	for _, f := range plan.Fields {
		switch f.GoFieldName {
		case "Net4":
			require.True(t, f.Canonical.IsNetworkNode(), "lw.IPv4Prefix → network canonical (≡ ,ct=vc)")
			got["Net4"] = f.GoType()
		case "Net6":
			require.True(t, f.Canonical.IsNetworkNode(), "lw.IPv6Prefix → network canonical (≡ ,ct=wc)")
			got["Net6"] = f.GoType()
		}
	}
	require.Equal(t, "[5]byte", got["Net4"], "lw.IPv4Prefix Go shape is [5]byte (4 address + 1 prefix byte)")
	require.Equal(t, "[17]byte", got["Net6"], "lw.IPv6Prefix Go shape is [17]byte (16 address + 1 prefix byte)")
}

// lw.Single is not yet supported as a NESTED sub-column (only at the entity
// level); the plan builder rejects it (Slice-A Step 4 boundary).
type badNestedSingle struct {
	Reading lw.Single[uint64]
}
type badSingleDrone struct {
	_   struct{}        `kind:"badSingle"`
	ID  uint64          `lw:",id"`
	Bad badNestedSingle `lw:"m,u64Array"`
}

func TestNested_RejectSingleInStruct(t *testing.T) {
	_, err := marshallreflect.PlanFor[badSingleDrone]()
	require.Error(t, err, "lw.Single as a nested sub-column is deferred")
	require.Contains(t, err.Error(), "lw.Single")
}

// Slice A, Step 5 — dynamic memberships via lw.* marker TYPES (lw.Ref / lw.Verbatim),
// the tag-free spelling of the `@membership` tuple. A []S whose element carries a
// marker membership field is a dynamic nested tuple, byte-identical to the flat
// `@membership` tuple.

// --- verbatim membership (a text-section tuple, like labeledtextdoc) ---

type flatTextTag struct {
	Label      string   `lw:"@membership,verbatim"`
	Text       string   `lw:"text:text"`
	WordLength []uint32 `lw:"text:wordLength"`
	WordBag    []string `lw:"text:wordBag"`
}
type flatTextTagDoc struct {
	_        struct{}      `kind:"textTagDoc"`
	ID       uint64        `lw:",id"`
	Tracking []byte        `lw:",naturalKey"`
	Texts    []flatTextTag `lw:"text"`
}
type nestedTextTag struct {
	Label      lw.Verbatim // marker membership — verbatim channel
	Text       string      `lw:"text"`
	WordLength []uint32    `lw:"wordLength"`
	WordBag    []string    `lw:"wordBag"`
}
type nestedTextTagDoc struct {
	_        struct{}        `kind:"textTagDoc"`
	ID       uint64          `lw:",id"`
	Tracking []byte          `lw:",naturalKey"`
	Texts    []nestedTextTag `lw:"text"`
}

func TestNested_DynamicVerbatim_ByteIdenticalToAtMembership(t *testing.T) {
	lookup := marshallreflect.NoLookup{}
	flat := []flatTextTagDoc{
		{ID: 1, Tracking: []byte("D1"), Texts: []flatTextTag{
			{Label: "title", Text: "hello world", WordLength: []uint32{5, 5}, WordBag: []string{"hello", "world"}},
			{Label: "body", Text: "hi", WordLength: []uint32{2}, WordBag: []string{"hi"}},
		}},
		{ID: 2, Tracking: []byte("D2"), Texts: nil},
	}
	nested := []nestedTextTagDoc{
		{ID: 1, Tracking: []byte("D1"), Texts: []nestedTextTag{
			{Label: "title", Text: "hello world", WordLength: []uint32{5, 5}, WordBag: []string{"hello", "world"}},
			{Label: "body", Text: "hi", WordLength: []uint32{2}, WordBag: []string{"hi"}},
		}},
		{ID: 2, Tracking: []byte("D2"), Texts: nil},
	}

	require.NoError(t, marshallreflect.Validate[nestedTextTagDoc](anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(nested))))

	flatRec, r1 := marshalToRecord(t, flat, lookup)
	defer r1()
	nestedRec, r2 := marshalToRecord(t, nested, lookup)
	defer r2()

	require.True(t, array.RecordEqual(flatRec, nestedRec), "records differ:\nflat=%s\nnested=%s", flatRec, nestedRec)
	require.Equal(t, ipcBytes(t, flatRec), ipcBytes(t, nestedRec), "Arrow IPC bytes differ")

	idReader, rID := loadIdReader(t, nestedRec)
	defer rID()
	txReader := anchor.NewReadAccessTestTableTaggedText()
	txReader.SetColumnIndices(txReader.GetColumnIndices())
	require.NoError(t, txReader.LoadFromRecord(nestedRec))
	defer txReader.Release()
	require.Equal(t, int64(2), txReader.GetAttributes().GetNumberOfAttributes(0))
	require.Equal(t, int64(0), txReader.GetAttributes().GetNumberOfAttributes(1))

	args := idReaders(idReader).Section("text", txReader.GetAttributes(), txReader.GetMemberships())
	var got []nestedTextTagDoc
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))
	require.Len(t, got, 2)
	require.Len(t, got[0].Texts, 2)
	require.Equal(t, lw.Verbatim("title"), got[0].Texts[0].Label, "verbatim membership reads into lw.Verbatim")
	require.Equal(t, "hello world", got[0].Texts[0].Text)
	require.Equal(t, lw.Verbatim("body"), got[0].Texts[1].Label)
	require.Nil(t, got[1].Texts, "zero elements → nil slice")
}

// --- repeated ref membership ([]lw.Ref, carried directly — ADR-0109) ---

type flatRefTag struct {
	Ancestors []uint64 `lw:"@membership,lowCardRef"`
	Kind      string   `lw:"symbol"`
}
type flatRefDoc struct {
	_        struct{}     `kind:"refTagDoc"`
	ID       uint64       `lw:",id"`
	Tracking []byte       `lw:",naturalKey"`
	Types    []flatRefTag `lw:"symbol"`
}
type nestedRefTag struct {
	Ancestors []lw.Ref // repeated ref membership, ids carried directly
	Kind      string   // value sub-column, tag-free → "value"
}
type nestedRefDoc struct {
	_        struct{}       `kind:"refTagDoc"`
	ID       uint64         `lw:",id"`
	Tracking []byte         `lw:",naturalKey"`
	Types    []nestedRefTag `lw:"symbol"`
}

func TestNested_DynamicRepeatedRef_ByteIdenticalToAtMembership(t *testing.T) {
	lookup := marshallreflect.NoLookup{}
	flat := []flatRefDoc{
		{ID: 1, Tracking: []byte("R1"), Types: []flatRefTag{
			{Ancestors: []uint64{10, 20, 30}, Kind: "person"},
			{Ancestors: []uint64{40}, Kind: "thing"},
		}},
	}
	nested := []nestedRefDoc{
		{ID: 1, Tracking: []byte("R1"), Types: []nestedRefTag{
			{Ancestors: []lw.Ref{10, 20, 30}, Kind: "person"},
			{Ancestors: []lw.Ref{40}, Kind: "thing"},
		}},
	}

	require.NoError(t, marshallreflect.Validate[nestedRefDoc](anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(nested))))

	flatRec, r1 := marshalToRecord(t, flat, lookup)
	defer r1()
	nestedRec, r2 := marshalToRecord(t, nested, lookup)
	defer r2()

	require.True(t, array.RecordEqual(flatRec, nestedRec), "records differ:\nflat=%s\nnested=%s", flatRec, nestedRec)
	require.Equal(t, ipcBytes(t, flatRec), ipcBytes(t, nestedRec), "Arrow IPC bytes differ")

	idReader, rID := loadIdReader(t, nestedRec)
	defer rID()
	symReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symReader.SetColumnIndices(symReader.GetColumnIndices())
	require.NoError(t, symReader.LoadFromRecord(nestedRec))
	defer symReader.Release()

	args := idReaders(idReader).Section("symbol", symReader.GetAttributes(), symReader.GetMemberships())
	var got []nestedRefDoc
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))
	require.Len(t, got, 1)
	require.Len(t, got[0].Types, 2)
	require.Equal(t, []lw.Ref{10, 20, 30}, got[0].Types[0].Ancestors, "repeated ref reads into []lw.Ref")
	require.Equal(t, "person", got[0].Types[0].Kind)
	require.Equal(t, []lw.Ref{40}, got[0].Types[1].Ancestors)
}

// --- heterogeneous memberships (verbatim + ref on one attribute, like lineagedoc) ---

type flatNamedText struct {
	Name       string   `lw:"@membership,verbatim"`
	Kind       uint64   `lw:"@membership,lowCardRef"`
	Text       string   `lw:"text:text"`
	WordLength []uint32 `lw:"text:wordLength"`
	WordBag    []string `lw:"text:wordBag"`
}
type flatNamedDoc struct {
	_        struct{}        `kind:"namedDoc"`
	ID       uint64          `lw:",id"`
	Tracking []byte          `lw:",naturalKey"`
	Notes    []flatNamedText `lw:"text"`
}
type nestedNamedText struct {
	Name       lw.Verbatim // membership #1 (verbatim)
	Kind       lw.Ref      // membership #2 (ref) — heterogeneous channels
	Text       string      `lw:"text"`
	WordLength []uint32    `lw:"wordLength"`
	WordBag    []string    `lw:"wordBag"`
}
type nestedNamedDoc struct {
	_        struct{}          `kind:"namedDoc"`
	ID       uint64            `lw:",id"`
	Tracking []byte            `lw:",naturalKey"`
	Notes    []nestedNamedText `lw:"text"`
}

func TestNested_DynamicHeterogeneous_ByteIdenticalToAtMembership(t *testing.T) {
	lookup := marshallreflect.NoLookup{}
	flat := []flatNamedDoc{
		{ID: 1, Tracking: []byte("N1"), Notes: []flatNamedText{
			{Name: "author", Kind: 7, Text: "ann", WordLength: []uint32{3}, WordBag: []string{"ann"}},
		}},
	}
	nested := []nestedNamedDoc{
		{ID: 1, Tracking: []byte("N1"), Notes: []nestedNamedText{
			{Name: "author", Kind: 7, Text: "ann", WordLength: []uint32{3}, WordBag: []string{"ann"}},
		}},
	}

	require.NoError(t, marshallreflect.Validate[nestedNamedDoc](anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(nested))))

	flatRec, r1 := marshalToRecord(t, flat, lookup)
	defer r1()
	nestedRec, r2 := marshalToRecord(t, nested, lookup)
	defer r2()

	require.True(t, array.RecordEqual(flatRec, nestedRec), "records differ:\nflat=%s\nnested=%s", flatRec, nestedRec)
	require.Equal(t, ipcBytes(t, flatRec), ipcBytes(t, nestedRec), "Arrow IPC bytes differ")

	idReader, rID := loadIdReader(t, nestedRec)
	defer rID()
	txReader := anchor.NewReadAccessTestTableTaggedText()
	txReader.SetColumnIndices(txReader.GetColumnIndices())
	require.NoError(t, txReader.LoadFromRecord(nestedRec))
	defer txReader.Release()

	args := idReaders(idReader).Section("text", txReader.GetAttributes(), txReader.GetMemberships())
	var got []nestedNamedDoc
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))
	require.Len(t, got, 1)
	require.Len(t, got[0].Notes, 1)
	require.Equal(t, lw.Verbatim("author"), got[0].Notes[0].Name)
	require.Equal(t, lw.Ref(7), got[0].Notes[0].Kind)
	require.Equal(t, "ann", got[0].Notes[0].Text)
}

// A One-cardinality section with a per-attribute lw.* membership field is
// rejected: dynamic memberships require a slice ([]S) in Slice A.
type oneWithMarker struct {
	M   lw.Verbatim
	Val string `lw:"value"`
}
type oneMarkerDrone struct {
	_   struct{}      `kind:"om"`
	ID  uint64        `lw:",id"`
	Bad oneWithMarker `lw:"symbol"`
}

func TestNested_RejectOneWithMarkerMembership(t *testing.T) {
	_, err := marshallreflect.PlanFor[oneMarkerDrone]()
	require.Error(t, err, "One-cardinality dynamic membership is deferred")
	require.Contains(t, err.Error(), "must be a slice")
}
