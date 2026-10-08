package play

import (
	"bytes"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

func mainIntResult(t *testing.T, l *PlayLauncher, term runstream.Terminal, vals ...int64) {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Int64Builder).AppendValues(vals, nil)
	rec := rb.NewRecordBatch()
	l.inner.graph.mainLane.finish("SELECT number AS n FROM numbers(3)", nil, time.Now(), rec, rec.Schema(), rec.NumRows(), Summary{}, nil, term)
}

func publishResultOp(t *testing.T, h app.OperationsHandlerI, in PublishResultArgs) (out PublishResultOutcome, err error) {
	t.Helper()
	obo := agentCall
	raw, err := h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: &obo}, opPublishResult, mustEncode(t, in))
	if err != nil {
		return
	}
	out, err = buscodec.Decode[PublishResultOutcome](raw)
	require.NoError(t, err)
	return
}

func waitPublished(t *testing.T, l *PlayLauncher) (last LastPublish) {
	t.Helper()
	require.Eventually(t, func() bool {
		last = l.lastPublish()
		return !last.Pending
	}, 5*time.Second, 5*time.Millisecond)
	return
}

// The whole main result leaves as a one-dataset bundle whose document reads
// it; a reader takes the rows in as play held them (ADR-0288
// §SD4).
func TestPublishResultPublishesTheMainResultAsABundle(t *testing.T) {
	fakeAppletDocs(t)
	l, h, reader := bundleLauncher(t)
	mainIntResult(t, l, runstream.Terminal{}, 0, 1, 2)

	out, err := publishResultOp(t, h, PublishResultArgs{Bundle: "counts", Sql: "SELECT n FROM keelson('result') ORDER BY n", Tabs: []string{"chart"}})
	require.NoError(t, err)
	assert.Equal(t, PublishResultOutcome{Bundle: "counts", Dataset: "counts__result", Rows: 3, Columns: 1, Following: "list_bundles"}, out)
	last := waitPublished(t, l)
	require.Empty(t, last.Error)
	assert.Equal(t, uint64(1), last.Revision)

	got, err := adhocdata.ResolveBundleRequest(reader, "counts", nil)
	require.NoError(t, err)
	doc := string(got.Document)
	assert.Contains(t, doc, "datasets: [result]")
	assert.Contains(t, doc, "tabs: [chart]")
	assert.Contains(t, doc, "SELECT n FROM keelson('result') ORDER BY n")
	assert.Contains(t, doc, "SELECT number AS n FROM numbers(3)", "the query that produced the rows rides along")

	read, err := adhocdata.ReadAll(reader, "counts__result", nil)
	require.NoError(t, err)
	rdr, err := ipc.NewReader(bytes.NewReader(read.ArrowIPCStream))
	require.NoError(t, err)
	defer rdr.Release()
	var vals []int64
	for rdr.Next() {
		vals = append(vals, rdr.RecordBatch().Column(0).(*array.Int64).Int64Values()...)
	}
	assert.Equal(t, []int64{0, 1, 2}, vals)
}

// A prefix is refused: a dataset made of it would miss rows with nothing
// to say so.
func TestPublishResultRefusesAPrefix(t *testing.T) {
	fakeAppletDocs(t)
	l, h, _ := bundleLauncher(t)
	mainIntResult(t, l, runstream.Terminal{State: runstream.TerminalTruncated, Reason: "max_result_rows"}, 0, 1)
	_, err := publishResultOp(t, h, PublishResultArgs{Bundle: "counts"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prefix")
	assert.Empty(t, l.lastPublish().Bundle, "nothing was published")
}

func TestPublishResultRefusals(t *testing.T) {
	fakeAppletDocs(t)
	l, h, _ := bundleLauncher(t)
	_, err := publishResultOp(t, h, PublishResultArgs{Bundle: "counts"})
	assert.ErrorContains(t, err, "no result")
	mainIntResult(t, l, runstream.Terminal{}, 1)
	_, err = publishResultOp(t, h, PublishResultArgs{Bundle: "a__b"})
	assert.ErrorContains(t, err, "double underscore")
	_, err = publishResultOp(t, h, PublishResultArgs{Bundle: "counts", LocalName: "not a name"})
	assert.Error(t, err)
}

// A published result opens in another window: the bundle's document runs
// over the dataset under its local name.
func TestAPublishedResultOpensInAnotherWindow(t *testing.T) {
	fakeAppletDocs(t)
	withQueryEndpoint(t, "http://127.0.0.1:1/query")
	l, h, _ := bundleLauncher(t)
	t.Cleanup(l.closeBundle)
	mainIntResult(t, l, runstream.Terminal{}, 4, 5)
	_, err := publishResultOp(t, h, PublishResultArgs{Bundle: "counts"})
	require.NoError(t, err)
	require.Empty(t, waitPublished(t, l).Error)

	_, err = openBundleOp(t, h, "counts")
	require.NoError(t, err)
	settleBundle(t, l, func() bool { return len(l.inner.client.datasetBindings) == 1 })
	assert.Equal(t, "SELECT * FROM keelson('result')", l.inner.sql)
	assert.True(t, adhocdata.IsHandle(l.inner.client.datasetBindings["result"]))
}
