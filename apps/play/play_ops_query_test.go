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
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

func TestQueryIsAnUntrustedAsynchronousRun(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	spec, ok := m.Operations.Lookup(opQuery)
	require.True(t, ok)
	assert.True(t, spec.Agents)
	assert.True(t, spec.Untrusted, "it answers with cells")
	assert.True(t, spec.Async)
	assert.Equal(t, app.OperationEffectRun, spec.Effect)
	assert.Equal(t, []string{opsResSql, opsResResult}, spec.Writes)
}

func TestQueryRefusesBeforeTheBufferChanges(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	obo := &app.OnBehalfOf{Task: "t", Epoch: 1}
	query := func(call app.OperationCall, in QueryArgs) error {
		_, err := h.ApplyCommand(call, opQuery, mustEncode(t, in))
		return err
	}
	var refusal *app.OperationRefusal

	require.ErrorAs(t, query(app.OperationCall{Writer: "task:t"}, QueryArgs{Sql: "SELECT 2"}), &refusal, "no agent context")
	require.ErrorAs(t, query(app.OperationCall{Writer: "task:t", OnBehalfOf: obo}, QueryArgs{Sql: "  "}), &refusal)

	err := query(app.OperationCall{Writer: "task:t", OnBehalfOf: obo}, QueryArgs{Sql: "SELECT 2;\nSELECT 3"})
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, err.Error(), "2 statements")

	err = query(app.OperationCall{Writer: "task:t", OnBehalfOf: obo}, QueryArgs{Sql: "SELECT {n:UInt8}"})
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, err.Error(), "n")

	p.client = NewClient(ClientConfig{URL: "http://ch.example:8123/"}, nil)
	err = query(app.OperationCall{Writer: "task:t", OnBehalfOf: obo}, QueryArgs{Sql: "SELECT 2"})
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, []string{"clickhouse:ch.example:8123"}, refusal.Destinations)

	assert.Equal(t, "SELECT 1", p.sql, "a refused query leaves the buffer as it was")
	assert.False(t, p.requestRun)
}

func TestQueryRunsAndAnswersWithTheFirstRows(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	obo := &app.OnBehalfOf{Task: "t", Epoch: 1}
	call := app.OperationCall{Writer: "task:t", Key: "k1", OnBehalfOf: obo}
	args := mustEncode(t, QueryArgs{Sql: "SELECT number AS n FROM numbers(3)", Limit: 2})
	_, err := h.ApplyCommand(call, opQuery, args)
	require.NoError(t, err)
	assert.Equal(t, "SELECT number AS n FROM numbers(3)", p.sql)
	assert.True(t, p.requestRun)
	assert.Equal(t, obo, p.takeAgentForRun(false), "the task asked for this run")

	settle := func() (QueryResult, bool) {
		settled, raw, err := h.(app.OperationsAsyncI).SettleAsync(call, opQuery, args)
		require.NoError(t, err)
		if !settled {
			return QueryResult{}, false
		}
		out, err := buscodec.Decode[QueryResult](raw)
		require.NoError(t, err)
		return out, true
	}
	_, settled := settle()
	assert.False(t, settled, "the run has not started")

	// The frame takes the request, and the result lands.
	p.requestRun = false
	mem := memory.NewGoAllocator()
	b := array.NewUint64Builder(mem)
	b.AppendValues([]uint64{0, 1, 2}, nil)
	col := b.NewArray()
	b.Release()
	schema := arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Uint64}}, nil)
	rec := array.NewRecordBatch(schema, []arrow.Array{col}, 3)
	p.graph.mainLane.finish("SELECT number AS n FROM numbers(3)", nil, time.Now(), rec, schema, 3, Summary{}, nil, runstream.Terminal{})
	_, settled = settle()
	assert.False(t, settled, "the frame has not drawn the result, so the result resource has not moved yet")

	// The frame draws it.
	drawn := func() {
		r, _, _, _, _, _, _, _, id := p.activeSnapshot()
		if r != nil {
			r.Release()
		}
		p.audioFollowResult(id)
	}
	drawn()
	out, settled := settle()
	require.True(t, settled)
	require.NotZero(t, out.ResultId)
	assert.Equal(t, out.ResultId, out.Result.Id)
	assert.Equal(t, int64(3), out.Result.Rows)
	require.NotNil(t, out.Sample)
	assert.Equal(t, out.ResultId, out.Sample.ResultId)
	assert.Equal(t, []string{"n"}, out.Sample.Columns)
	assert.Equal(t, [][]string{{"0"}, {"1"}}, out.Sample.Rows, "limit 2")
	assert.NotEmpty(t, out.Sample.StatementDigest, "the digest check reports as its source")
	assert.Empty(t, p.queryRuns, "a settled call is forgotten")
	_, _, remembered := snapshotCheck(p).lookup(out.ResultId)
	assert.True(t, remembered, "a check can find the result the model just learned of")
}

// A run the run path did not start leaves the result held before: the
// answer names no result and carries no rows of the old one.
func TestQueryWhoseRunDidNotHappenAnswersWithoutRows(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	mem := memory.NewGoAllocator()
	b := array.NewUint64Builder(mem)
	b.Append(7)
	col := b.NewArray()
	b.Release()
	schema := arrow.NewSchema([]arrow.Field{{Name: "x", Type: arrow.PrimitiveTypes.Uint64}}, nil)
	p.graph.mainLane.finish("SELECT 7 AS x", nil, time.Now(), array.NewRecordBatch(schema, []arrow.Array{col}, 1), schema, 1, Summary{}, nil, runstream.Terminal{})

	call := app.OperationCall{Writer: "task:t", Key: "k2", OnBehalfOf: &app.OnBehalfOf{Task: "t", Epoch: 1}}
	args := mustEncode(t, QueryArgs{Sql: "SELECT 8 AS x"})
	_, err := h.ApplyCommand(call, opQuery, args)
	require.NoError(t, err)
	p.requestRun = false
	p.audioFollowResult(ResultID(snapshotState(p).Result.Id))

	settled, raw, err := h.(app.OperationsAsyncI).SettleAsync(call, opQuery, args)
	require.NoError(t, err)
	require.True(t, settled)
	out, err := buscodec.Decode[QueryResult](raw)
	require.NoError(t, err)
	assert.Zero(t, out.ResultId)
	assert.Nil(t, out.Sample)
	assert.NotZero(t, out.Result.Id, "the state still names the result held")
}

// A window whose frame draws no panels never moves the result resource; a
// finished run settles after a few frames all the same.
func TestARunSettlesWhenTheFrameDrawsNoPanels(t *testing.T) {
	l, _ := opsLauncher(t)
	p := l.inner
	p.frameResult = 99
	for range runLandedFrames {
		assert.False(t, p.runLanded())
	}
	assert.True(t, p.runLanded())
	p.requestRun = true
	assert.False(t, p.runLanded(), "a run asked for has not landed")
}
