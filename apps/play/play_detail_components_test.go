package play

import (
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	factsdml "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mddocfacts"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/componentview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fieldview"
)

// TestPlayComponentStores_MatchRegistry pins the reflect-read roster
// (playComponentStores) to the SQL roster (RegisterComponents): the kinds the
// Detail pane can show are exactly the kinds LW_COMPONENT can name. A kind in
// one and not the other is a surface silently dark for whoever authored
// against it.
func TestPlayComponentStores_MatchRegistry(t *testing.T) {
	r := componentsql.NewRegistry()
	require.NoError(t, RegisterComponents(r))
	want := r.Kinds()
	slices.Sort(want)

	var got []string
	for _, st := range playComponentStores() {
		b := componentview.NewBinder()
		require.NoError(t, st.bind(b), st.name)
		for _, bd := range b.Bindings() {
			got = append(got, string(bd.Kind()))
		}
		_, err := storeLookup(st.name, st.ids)
		require.NoError(t, err, st.name)
	}
	slices.Sort(got)
	assert.Equal(t, want, got)
}

// factsRecord marshals one SysMem row and one MdDoc row into a boxer.facts
// record through the generated DML — the schema order a SELECT * returns.
func factsRecord(t *testing.T) arrow.RecordBatch {
	t.Helper()
	dml := factsdml.NewInEntityFacts(memory.NewGoAllocator(), 2)
	sysLookup, err := storeLookup("Sysmetrics", sysmfacts.SysmetricsMembershipIds)
	require.NoError(t, err)
	mem := []sysmfacts.SysMem{{
		Id: 1, NaturalKey: []byte("host-a"), Ts: time.Unix(1_700_000_000, 0).UTC(),
		Kind: "sysMem", Host: "host-a",
		TotalBytes: 16 << 30, FreeBytes: 4 << 30, AvailableBytes: 8 << 30, UsedBytes: 8 << 30,
	}}
	require.NoError(t, marshallreflect.Marshal(dml, mem, sysLookup))
	mdLookup, err := storeLookup("Mddoc", mddocfacts.MddocMembershipIds)
	require.NoError(t, err)
	docs := []mddocfacts.MdDoc{{
		Id: 2, NaturalKey: []byte("doc-b"), Ts: time.Unix(1_700_000_100, 0).UTC(),
		Kind: "mdDoc", Title: "Notes", FileName: "notes.md", Content: "# Notes\n", ContentHash: "ab", Words: 1,
	}}
	require.NoError(t, marshallreflect.Marshal(dml, docs, mdLookup))
	recs, err := dml.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	t.Cleanup(func() { recs[0].Release() })
	require.EqualValues(t, 2, recs[0].NumRows())
	return recs[0]
}

// TestComponentDetail_PhysicalColumnsMatchDDLOrder pins the alignment the
// driver relies on: the generated read access's default column index i is the
// i-th column of a SELECT * over boxer.facts, and factsPhysicalColumns names it.
func TestComponentDetail_PhysicalColumnsMatchDDLOrder(t *testing.T) {
	rec := factsRecord(t)
	cd := newComponentDetail(c.NewWidgetIdStack())
	require.NoError(t, cd.buildErr)
	schema := rec.Schema()
	require.Equal(t, schema.NumFields(), len(cd.physical), "one physical name per facts column")
	for i := range schema.NumFields() {
		assert.Equal(t, schema.Field(i).Name, cd.physical[i], "column %d", i)
	}
	for k, d := range cd.defaults {
		assert.Less(t, int(d), len(cd.physical), "default index %d of slot %d", d, k)
	}
}

func kindsOf(comps []componentview.Component) (kinds []string) {
	for _, comp := range comps {
		kinds = append(kinds, string(comp.Kind))
	}
	return
}

// TestComponentDetail_DetectsPerRow reads the two rows back: each carries
// exactly its own kind, and the decoded value is the DTO the row was written
// from. The same driver instance serves both rows off one loaded record.
func TestComponentDetail_DetectsPerRow(t *testing.T) {
	rec := factsRecord(t)
	cd := newComponentDetail(c.NewWidgetIdStack())
	require.NoError(t, cd.buildErr)
	t.Cleanup(cd.release)

	comps, err := cd.componentsFor(rec, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"SysMem"}, kindsOf(comps))
	mem, ok := comps[0].Value.(sysmfacts.SysMem)
	require.True(t, ok, "value is the DTO itself: %T", comps[0].Value)
	assert.Equal(t, "host-a", mem.Host)
	assert.EqualValues(t, 16<<30, mem.TotalBytes)

	comps, err = cd.componentsFor(rec, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"MdDoc"}, kindsOf(comps))
	doc, ok := comps[0].Value.(mddocfacts.MdDoc)
	require.True(t, ok)
	assert.Equal(t, "Notes", doc.Title)
	assert.EqualValues(t, 1, doc.Words)

	// The per-row cache answers the same row again without re-decoding.
	again, err := cd.componentsFor(rec, 1)
	require.NoError(t, err)
	assert.Equal(t, kindsOf(comps), kindsOf(again))

	// Out of range is empty, not an error.
	comps, err = cd.componentsFor(rec, 7)
	require.NoError(t, err)
	assert.Empty(t, comps)
}

// TestComponentDetail_AlignsByName permutes the record's columns: the reader
// binds by position, so without the name alignment every slot would read the
// wrong column. Detection must be unchanged.
func TestComponentDetail_AlignsByName(t *testing.T) {
	rec := factsRecord(t)
	n := rec.Schema().NumFields()
	fields := make([]arrow.Field, 0, n)
	cols := make([]arrow.Array, 0, n)
	for i := n - 1; i >= 0; i-- {
		fields = append(fields, rec.Schema().Field(i))
		cols = append(cols, rec.Column(i))
	}
	permuted := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, rec.NumRows())
	t.Cleanup(permuted.Release)

	cd := newComponentDetail(c.NewWidgetIdStack())
	require.NoError(t, cd.buildErr)
	t.Cleanup(cd.release)
	comps, err := cd.componentsFor(permuted, 1)
	require.NoError(t, err)
	assert.Equal(t, []string{"MdDoc"}, kindsOf(comps))
}

// TestComponentDetail_NotFactsShaped: a result missing a facts column is not a
// facts row — no report, no error, no panic — and the verdict is cached per
// schema.
func TestComponentDetail_NotFactsShaped(t *testing.T) {
	rec := factsRecord(t)
	n := rec.Schema().NumFields()
	fields := make([]arrow.Field, 0, n-1)
	cols := make([]arrow.Array, 0, n-1)
	for i := 1; i < n; i++ {
		fields = append(fields, rec.Schema().Field(i))
		cols = append(cols, rec.Column(i))
	}
	narrow := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, rec.NumRows())
	t.Cleanup(narrow.Release)

	cd := newComponentDetail(c.NewWidgetIdStack())
	require.NoError(t, cd.buildErr)
	t.Cleanup(cd.release)
	comps, err := cd.componentsFor(narrow, 0)
	require.NoError(t, err)
	assert.Empty(t, comps)
	assert.Error(t, cd.schemaErr)
	assert.False(t, cd.ensureSchema(narrow.Schema()), "cached negative verdict")
}

// TestDtoFields covers the projection: option absence, bytes, nested slices,
// and the skipped kind marker.
func TestDtoFields(t *testing.T) {
	doc := mddocfacts.MdHeading{Id: 3, Text: "H", Level: 2}
	fields := dtoFields(doc)
	names := make([]string, 0, len(fields))
	byName := map[string]fieldview.Field{}
	for _, f := range fields {
		names = append(names, f.Name)
		byName[f.Name] = f
	}
	assert.NotContains(t, names, "_")
	assert.Equal(t, fieldview.KindString, byName["Text"].Kind)
	assert.Equal(t, "H", byName["Text"].Str)
	assert.Equal(t, fieldview.KindBytes, byName["NaturalKey"].Kind)
	assert.Equal(t, "absent", byName["Anchor"].Str, "an absent option reads as absent")

	// A []uint8 over a u8Array section is a list of small numbers, not bytes;
	// only a natural key or a blob-section value is opaque.
	cpu := sysmfacts.SysCpu{PerCorePercent: []uint8{1, 2}}
	f := dtoFields(cpu)
	var cores fieldview.Field
	for _, x := range f {
		if x.Name == "PerCorePercent" {
			cores = x
		}
	}
	require.Equal(t, fieldview.KindArray, cores.Kind)
	require.Len(t, cores.Children, 2)
	assert.Equal(t, "[1]", cores.Children[1].Name)
	assert.Equal(t, fieldview.KindUint, cores.Children[1].Kind)
	assert.EqualValues(t, 2, cores.Children[1].Uint)
}
