package play

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// labelledFakeExec serves one record and reports a dispatch label, as the
// client executor does.
type labelledFakeExec struct {
	rec      func() arrow.RecordBatch
	confined bool
}

func (inst labelledFakeExec) execute(ctx context.Context, c compiledNode, alloc memory.Allocator) (arrow.RecordBatch, *arrow.Schema, Summary, error) {
	rec, schema, summary, _, err := inst.executeLabelled(ctx, c, alloc, nil)
	return rec, schema, summary, err
}

func (inst labelledFakeExec) executeLabelled(_ context.Context, _ compiledNode, _ memory.Allocator, _ func(p runstream.Progress)) (arrow.RecordBatch, *arrow.Schema, Summary, bool, error) {
	rec := inst.rec()
	return rec, rec.Schema(), Summary{}, inst.confined, nil
}

// stringRec is a one-row-per-value record of string columns, nil entries
// being NULL.
func stringRec(t *testing.T, cols map[string][]*string, order ...string) arrow.RecordBatch {
	t.Helper()
	mem := memory.NewGoAllocator()
	fields := make([]arrow.Field, 0, len(order))
	arrs := make([]arrow.Array, 0, len(order))
	var n int64
	for _, name := range order {
		b := array.NewStringBuilder(mem)
		for _, v := range cols[name] {
			if v == nil {
				b.AppendNull()
				continue
			}
			b.Append(*v)
		}
		arrs = append(arrs, b.NewArray())
		n = int64(len(cols[name]))
		b.Release()
		fields = append(fields, arrow.Field{Name: name, Type: arrow.BinaryTypes.String, Nullable: true})
	}
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), arrs, n)
}

func strp(s string) *string { return &s }

// boundLaneApp binds the chart pane to the `recent` node, whose lane serves
// int64Rec("n", 10, 20, 30) under the given label.
func boundLaneApp(t *testing.T, confined bool) (*PlayLauncher, app.OperationsHandlerI) {
	t.Helper()
	l, h := opsLauncher(t)
	p := l.inner
	p.currentSplit = splitResult{
		Nodes: []splitNode{
			{ID: "recent", Kind: splitNodeCTE, SQL: "SELECT n FROM t"},
			{ID: "other", Kind: splitNodeCTE, SQL: "SELECT 1"},
			{ID: mainNodeID, Kind: splitNodeStatement, SQL: "WITH recent AS (SELECT n FROM t) SELECT * FROM recent"},
		},
		Sink: mainNodeID,
	}
	lane := newNodeLane(labelledFakeExec{rec: func() arrow.RecordBatch { return int64Rec("n", 10, 20, 30) }, confined: confined},
		memory.NewGoAllocator(), 0)
	t.Cleanup(lane.close)
	waitLaneReady(t, lane, "SELECT n FROM t")
	p.tabBindings = map[string]NodeID{"chart": "recent"}
	p.boundLanes = map[NodeID]*nodeLane{"recent": lane}
	p.resolvedNodes = map[string]NodeID{"chart": "recent"}
	return l, h
}

// ADR-0270, update of 2026-10-05: the reads take a pane or a node, and read
// the bound node's lane rather than the main result.
func TestResultReadsFollowAPaneToItsBoundNode(t *testing.T) {
	l, h := boundLaneApp(t, false)

	d := queryOp[ResultDescription](t, h, opDescribeResult, ResultArgs{Pane: "chart"})
	assert.Equal(t, "recent", d.Node)
	assert.Equal(t, int64(3), d.Rows)
	require.Len(t, d.Columns, 1)
	assert.Equal(t, "n", d.Columns[0].Name)
	assert.NotZero(t, d.Id)

	out := queryOp[SampleRows](t, h, opSampleRows, SampleArgs{Node: "recent", Rows: []int64{2, 0}})
	assert.Equal(t, "recent", out.Node)
	assert.Equal(t, [][]string{{"30"}, {"10"}}, out.Rows)
	assert.Equal(t, []int64{2, 0}, out.RowNumbers)
	assert.Equal(t, d.Id, out.ResultId)

	// The table is not bound: it is fed the main result, which holds none.
	_, err := h.Snapshot().Query(opSampleRows, mustEncode(t, SampleArgs{Pane: "table"}))
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Error(), "holds no result")

	for name, args := range map[string]SampleArgs{
		"both":          {Pane: "chart", Node: "recent"},
		"undrawn node":  {Node: "other"},
		"unknown node":  {Node: "nope"},
		"frameless":     {Pane: "network"},
		"not a panel":   {Pane: "editor"},
		"row past end":  {Node: "recent", Rows: []int64{3}},
		"rows + offset": {Node: "recent", Rows: []int64{0}, Offset: 1},
	} {
		_, err = h.Snapshot().Query(opSampleRows, mustEncode(t, args))
		require.ErrorAs(t, err, &refusal, name)
	}
	_, err = h.Snapshot().Query(opSampleRows, mustEncode(t, SampleArgs{Node: "other"}))
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Error(), "bind_pane")

	// A closed lane serves nothing; the read says so rather than reading a
	// released record.
	l.inner.boundLanes["recent"].close()
	_, err = h.Snapshot().Query(opSampleRows, mustEncode(t, SampleArgs{Pane: "chart"}))
	require.ErrorAs(t, err, &refusal)
}

// The window's label covers what its bound lanes hold (ADR-0270 §SD4,
// extended to lanes).
func TestABoundLaneOfConfinedDataLabelsTheWindow(t *testing.T) {
	l, h := boundLaneApp(t, true)
	assert.True(t, h.Confined())
	l.inner.boundLanes["recent"].close()
	assert.False(t, h.Confined(), "a closed lane holds nothing")

	_, h = boundLaneApp(t, false)
	assert.False(t, h.Confined())
}

// describe_result names handles and gloss labels, says when the result is
// leeway-shaped, and reports the main result's own truncation.
func TestDescribeResultGivesTheLeewayReadingAndTheTruncation(t *testing.T) {
	l, h := opsLauncher(t)
	cols := map[string][]*string{}
	for _, n := range schemaWithSymbol {
		cols[n] = []*string{strp("a")}
	}
	rec := stringRec(t, cols, schemaWithSymbol...)
	l.inner.graph.mainLane.finish("SELECT …", nil, time.Now(), rec, rec.Schema(), 1, Summary{}, nil,
		runstream.Terminal{State: runstream.TerminalTruncated, Reason: "max_result_rows"})

	d := queryOp[ResultDescription](t, h, opDescribeResult, ResultArgs{})
	assert.True(t, d.Leeway)
	assert.Equal(t, leewayReading, d.Reading)
	i := slices.IndexFunc(d.Columns, func(c Column) bool { return c.Name == "tv:symbol:value:val:s:124::I:0::data" })
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, "symbol:value", d.Columns[i].Handle)
	assert.True(t, d.Truncated)
	assert.Equal(t, "max_result_rows", d.TruncationReason)
	assert.Equal(t, string(mainNodeID), d.Node)

	out := queryOp[SampleRows](t, h, opSampleRows, SampleArgs{Node: string(mainNodeID)})
	assert.Equal(t, "max_result_rows", out.ResultPrefix)

	rec = stringRec(t, map[string][]*string{"size@gloss/bytes": {strp("1"), nil}, "name": {strp(""), strp("x")}}, "name", "size@gloss/bytes")
	l.inner.graph.mainLane.finish("SELECT …", nil, time.Now(), rec, rec.Schema(), 2, Summary{}, nil, runstream.Terminal{})
	d = queryOp[ResultDescription](t, h, opDescribeResult, ResultArgs{})
	assert.False(t, d.Leeway)
	assert.Empty(t, d.Reading)
	assert.False(t, d.Truncated)
	require.Len(t, d.Columns, 2)
	assert.Equal(t, "size", d.Columns[1].Label)
	assert.Empty(t, d.Columns[0].Label)

	out = queryOp[SampleRows](t, h, opSampleRows, SampleArgs{Fields: []string{"size"}})
	assert.Equal(t, []string{"size@gloss/bytes"}, out.Columns, "a gloss label picks its column")
	assert.Equal(t, []string{"size"}, out.Labels)
	assert.Equal(t, [][]int32{{}, {0}}, out.Nulls)
	assert.Empty(t, out.ResultPrefix)
}

// sample_rows says which bound cut it and where to read on.
func TestSampleRowsSaysWhichBoundCutIt(t *testing.T) {
	l, h := opsLauncher(t)
	vals := make([]int64, 60)
	rec := int64Rec("n", vals...)
	l.inner.graph.mainLane.finish("SELECT …", nil, time.Now(), rec, rec.Schema(), 60, Summary{}, nil, runstream.Terminal{})
	out := queryOp[SampleRows](t, h, opSampleRows, SampleArgs{Limit: 10, Offset: 5})
	assert.True(t, out.Truncated)
	assert.Contains(t, out.TruncationReason, "offset 15")
	assert.Equal(t, int64(5), out.RowNumbers[0])
	out = queryOp[SampleRows](t, h, opSampleRows, SampleArgs{Offset: 55})
	assert.False(t, out.Truncated, "the last rows are whole")
	assert.Empty(t, out.TruncationReason)
}

func TestResultReadsCatalogEntries(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	for _, name := range []string{opDescribeResult, opSampleRows} {
		spec, ok := m.Operations.Lookup(name)
		require.True(t, ok, name)
		assert.Equal(t, 2, int(spec.Version), name)
		assert.True(t, spec.Untrusted, name)
		assert.Equal(t, app.OperationEffectNone, spec.Effect, name)
		assert.Equal(t, []string{opsResResult}, spec.Reads, name)
		assert.Empty(t, spec.Writes, name)
	}
}

// An observed intermediate is what the unbound panels draw: the reads with
// neither pane nor node read its lane, the main result stays readable by
// name, and its label reaches the window's.
func TestResultReadsFollowAnObservedIntermediate(t *testing.T) {
	l, h := boundLaneApp(t, false)
	p := l.inner
	p.intermediateLane.close()
	p.intermediateLane = newNodeLane(labelledFakeExec{rec: func() arrow.RecordBatch { return int64Rec("m", 1, 2) }, confined: true},
		memory.NewGoAllocator(), 0)
	t.Cleanup(p.intermediateLane.close)
	waitLaneReady(t, p.intermediateLane, "SELECT 1")
	p.observedNode = "other"

	d := queryOp[ResultDescription](t, h, opDescribeResult, ResultArgs{})
	assert.Equal(t, "other", d.Node)
	assert.Equal(t, int64(2), d.Rows)
	assert.Empty(t, d.TruncationReason, "only the main lane carries a truncation")
	assert.True(t, h.Confined())

	_, err := h.Snapshot().Query(opSampleRows, mustEncode(t, SampleArgs{Node: string(mainNodeID)}))
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Error(), "holds no result", "the main result is resolved, and holds none here")
}
