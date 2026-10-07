package play

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// ADR-0270, update of 2026-10-05: the Table, Files, Chat and Map panes'
// operations.
func TestTableFilesChatMapCatalogEntries(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	require.NoError(t, m.Operations.Validate())
	for _, q := range []struct {
		op    string
		reads []string
	}{
		{opGetTable, []string{opsResTable, opsResResult, opsResPanes}},
		{opGetFiles, []string{opsResFiles, opsResResult, opsResPanes}},
		{opGetChatPane, []string{opsResResult, opsResPanes}},
		{opGetMap, []string{opsResMap, opsResPanes}},
	} {
		spec, ok := m.Operations.Lookup(q.op)
		require.True(t, ok, q.op)
		assert.Equal(t, app.OperationClassQuery, spec.Class, q.op)
		assert.Equal(t, app.OperationEffectNone, spec.Effect, q.op)
		assert.True(t, spec.Untrusted, "%s quotes the data", q.op)
		assert.True(t, spec.Agents, q.op)
		assert.Equal(t, q.reads, spec.Reads, q.op)
	}
	for _, c := range []struct {
		op      string
		effect  app.OperationEffectE
		writes  []string
		gesture bool
	}{
		{opSetTableOptions, app.OperationEffectDocument, []string{opsResTable}, true},
		{opSetFilesOptions, app.OperationEffectDocument, []string{opsResFiles}, true},
		{opSelectFilesPath, app.OperationEffectDocument, []string{opsResSignals, opsResFiles}, true},
		{opSetMapView, app.OperationEffectView, []string{opsResMap}, false},
		{opSetMapOptions, app.OperationEffectDocument, []string{opsResMap, opsResSignals}, true},
	} {
		spec, ok := m.Operations.Lookup(c.op)
		require.True(t, ok, c.op)
		assert.Equal(t, app.OperationClassCommand, spec.Class, c.op)
		assert.Equal(t, c.effect, spec.Effect, c.op)
		assert.False(t, spec.Untrusted, c.op)
		assert.True(t, spec.Agents, c.op)
		assert.Equal(t, c.writes, spec.Writes, c.op)
		assert.Equal(t, c.gesture, spec.Gesture != "", c.op)
	}
	_, ok := m.Operations.Lookup("set_chat_pane_options")
	assert.False(t, ok, "the Chat pane's pickers stay the person's")
}

// tableResult lands a two-column result on the main lane and marks the
// Table as having drawn it.
func tableResult(t *testing.T, p *PlayApp, names []string, sizes []int64) (arrow.RecordBatch, ResultID) {
	t.Helper()
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "size@gloss/bytes", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	b := array.NewRecordBuilder(mem, schema)
	defer b.Release()
	for _, n := range names {
		if n == "" {
			b.Field(0).AppendNull()
			continue
		}
		b.Field(0).(*array.StringBuilder).Append(n)
	}
	b.Field(1).(*array.Int64Builder).AppendValues(sizes, nil)
	rec := b.NewRecordBatch()
	p.graph.mainLane.finish("SELECT …", nil, time.Now(), rec, schema, int64(len(names)), Summary{}, nil, runstream.Terminal{})
	_, _, _, _, _, _, _, _, id := p.graph.MainSnapshot()
	drawnPane(p, tablePaneId, id, schema)
	p.tableDrawn = tableDrawnMark{schema: schema, rows: int64(len(names)), visCols: []int{0, 1}}
	p.pager.Configure(int64(len(names)))
	return rec, id
}

func TestGetTableReadsCaptionsSortAndThePage(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	_, id := tableResult(t, p, []string{"b", "", "a"}, []int64{10, 2048, 7})

	r := queryOp[TableReading](t, h, opGetTable, GetTableArgs{Rows: true})
	assert.Equal(t, uint64(id), r.Drawn.ResultId)
	assert.Equal(t, int64(3), r.Rows)
	assert.Equal(t, int64(1), r.Page)
	assert.Equal(t, int64(100), r.PageSize)
	assert.Nil(t, r.Sort)
	assert.False(t, r.Leeway)
	require.Len(t, r.Columns, 2)
	assert.Equal(t, "size", r.Columns[1].Label)
	assert.NotEmpty(t, r.Columns[1].Gloss)
	assert.True(t, r.Columns[0].Shown)
	require.Len(t, r.PageRows, 3)
	assert.Equal(t, []int32{0}, r.PageRows[1].Nulls, "NULL is kept apart from empty text")
	face := r.PageRows[1].Cells[1]
	assert.NotEqual(t, "2048", face, "the cell reads as the gloss renders it")

	require.NoError(t, applyOp(t, h, opSetTableOptions, SetTableOptionsArgs{SortBy: strp("size"), Descending: boolp(true)}))
	assert.True(t, p.tableSort.active)
	assert.Equal(t, 1, p.tableSort.col)
	assert.True(t, p.tableSort.desc)
	r = queryOp[TableReading](t, h, opGetTable, GetTableArgs{Rows: true})
	require.NotNil(t, r.Sort)
	assert.Equal(t, "size@gloss/bytes", r.Sort.Column)
	assert.Equal(t, "size", r.Sort.Handle)
	assert.True(t, r.Sort.Descending)
	require.Len(t, r.PageRows, 3)
	assert.Equal(t, []int64{1, 0, 2}, []int64{r.PageRows[0].Row, r.PageRows[1].Row, r.PageRows[2].Row}, "drawn largest first")

	require.NoError(t, applyOp(t, h, opSetTableOptions, SetTableOptionsArgs{RawCells: boolp(true)}))
	r = queryOp[TableReading](t, h, opGetTable, GetTableArgs{Rows: true, Limit: 1})
	assert.True(t, r.RawCells)
	require.Len(t, r.PageRows, 1)
	assert.Equal(t, "2048", r.PageRows[0].Cells[1])
	assert.Equal(t, int32(2), r.More)

	require.NoError(t, applyOp(t, h, opSetTableOptions, SetTableOptionsArgs{SortBy: strp("")}))
	assert.False(t, p.tableSort.active, "an empty sort_by gives back the result's order")
}

func TestSetTableOptionsRefusesAndPages(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	names := make([]string, 120)
	sizes := make([]int64, 120)
	for i := range names {
		names[i] = "n"
		sizes[i] = int64(i)
	}
	tableResult(t, p, names, sizes)

	for _, in := range []SetTableOptionsArgs{
		{},
		{SortBy: strp("nope")},
		{Descending: boolp(true)},
		{SupportColumns: boolp(true)},
		{PageSize: int64p(30)},
		{Page: int64p(4)},
		{Page: int64p(1), Row: int64p(3)},
		{Row: int64p(500)},
		{Granularity: strp(tableGranAttr)},
	} {
		require.Error(t, applyOp(t, h, opSetTableOptions, in), "%+v", in)
	}
	assert.False(t, p.tableSort.active, "a refused call applies nothing")

	require.NoError(t, applyOp(t, h, opSetTableOptions, SetTableOptionsArgs{PageSize: int64p(50), Row: int64p(110)}))
	assert.Equal(t, int64(50), p.pager.PageSize())
	assert.Equal(t, int64(2), p.pager.CurrentPage())
	require.NoError(t, applyOp(t, h, opSetTableOptions, SetTableOptionsArgs{Page: int64p(2)}))
	assert.Equal(t, int64(1), p.pager.CurrentPage())
	// Under a descending sort record row 110 draws at position 9: page 1.
	require.NoError(t, applyOp(t, h, opSetTableOptions, SetTableOptionsArgs{SortBy: strp("size"), Descending: boolp(true), Row: int64p(110)}))
	assert.Equal(t, int64(0), p.pager.CurrentPage())
	assert.Contains(t, tableOptionsDigest(p), "sort=1:1:1")
}

// A header click cycles the sort through set_table_options, logged as the
// person's.
func TestTableHeaderClickGoesThroughSetTableOptions(t *testing.T) {
	l, eng := gestureLauncher(t)
	p := l.inner
	rec, _ := tableResult(t, p, []string{"b", "a"}, []int64{1, 2})
	schema := rec.Schema()
	p.requestTableOptions(p.tableSortClick(schema, 0))
	e := lastEntry(t, eng)
	assert.Equal(t, opSetTableOptions, e.Op)
	assert.Equal(t, opwire.WriterPerson, e.Writer)
	assert.Equal(t, []string{opsResTable}, e.Resources)
	assert.True(t, p.tableSort.active)
	assert.False(t, p.tableSort.desc)
	p.requestTableOptions(p.tableSortClick(schema, 0))
	assert.True(t, p.tableSort.desc)
	p.requestTableOptions(p.tableSortClick(schema, 0))
	assert.False(t, p.tableSort.active, "the third click gives back the result's order")

	bare, _ := opsLauncher(t)
	tableResult(t, bare.inner, []string{"b"}, []int64{1})
	bare.inner.requestTableOptions(bare.inner.tableSortClick(schema, 1))
	assert.True(t, bare.inner.tableSort.active, "applied directly where no host routes it")
}

func int64p(v int64) *int64 { return &v }
