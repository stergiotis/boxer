package play

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// fakeAppletDocs parses a document whose body is its SQL, reads the local
// names from a first line "-- datasets: a,b", and runs unless the SQL
// starts with INSERT. It stands in for sqlapplet's parser, which play
// cannot import.
func fakeAppletDocs(t *testing.T) {
	t.Helper()
	prev := appletDocParser.Load()
	SetAppletDocParser(func(path string, src []byte) (doc AppletDoc, err error) {
		text := string(src)
		if strings.HasPrefix(text, "---\n") {
			// play's own composed documents: frontmatter, then the first
			// sql fence is the buffer.
			for line := range strings.SplitSeq(text, "\n") {
				if names, ok := strings.CutPrefix(line, "datasets: ["); ok {
					doc.Datasets = strings.Split(strings.TrimSuffix(names, "]"), ", ")
				}
			}
			_, rest, _ := strings.Cut(text, "```sql\n")
			doc.Sql, _, _ = strings.Cut(rest, "\n```")
			doc.Introspection, doc.Runnable = true, true
			return
		}
		first, rest, _ := strings.Cut(text, "\n")
		if names, ok := strings.CutPrefix(first, "-- datasets: "); ok {
			doc.Datasets = strings.Split(names, ",")
			text = rest
		}
		doc.Sql, doc.Introspection = text, true
		doc.Runnable = !strings.HasPrefix(text, "INSERT")
		return
	})
	t.Cleanup(func() { appletDocParser.Store(prev) })
}

func intStream(t *testing.T, vals ...int64) []byte {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Int64Builder).AppendValues(vals, nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	stream, err := adhocdata.EncodeRecord(rec)
	require.NoError(t, err)
	return stream
}

func publishSalesBundle(t *testing.T, bus app.BusI, doc string, locals ...string) (res adhocdata.BundleResult) {
	t.Helper()
	in := adhocdata.BundlePublishInput{Alias: "sales", Document: []byte(doc)}
	for i, l := range locals {
		in.Datasets = append(in.Datasets, adhocdata.BundleDatasetInput{LocalName: l, ArrowIPCStream: intStream(t, int64(i))})
	}
	res, err := adhocdata.PublishBundleRequest(bus, in)
	require.NoError(t, err)
	return
}

// agentCall is the call open_bundle runs under in these tests; the
// attesting dispatcher below confirms it for the test window.
var agentCall = app.OnBehalfOf{Task: "t", Epoch: 1, Call: "t-1"}

// attestAll confirms agentCall for whichever window asks, as the agent
// dispatcher would for the window it sent the call to.
type attestAll struct{}

func (attestAll) CallContext(task string, epoch uint64, call string, sender app.AppIdT, senderInstance uint64) (cc app.CallContext, ok bool, reason string) {
	if task != agentCall.Task || call != agentCall.Call {
		return cc, false, "the task has no call by that id"
	}
	return app.CallContext{Task: task, Epoch: epoch, Call: call, Conversation: "conv", Turn: "turn-1", App: sender, Instance: senderInstance}, true, ""
}

// bundleLauncher is bindLauncher whose dataset service attests agentCall.
func bundleLauncher(t *testing.T) (l *PlayLauncher, h app.OperationsHandlerI, publisher app.BusI) {
	t.Helper()
	logger := zerolog.Nop()
	bus := inprocbus.NewInst(logger)
	svc, err := adhocdata.NewService(adhocdata.Config{Bus: bus, Registry: introspect.NewRegistry(), Dir: t.TempDir(), Log: logger})
	require.NoError(t, err)
	svc.SetCallContext(attestAll{})
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	caps := []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}}
	l, h = opsLauncher(t)
	l.inner.client = NewClient(ClientConfig{URL: "http://ch.example:8123/"}, nil)
	l.bus, l.log = bus.NewClient("test.play", caps), logger
	t.Cleanup(func() {
		if l.follower != nil {
			l.follower.Close()
		}
	})
	publisher = bus.NewClient("test.publisher", caps)
	return
}

func openBundleOp(t *testing.T, h app.OperationsHandlerI, alias string) (out OpenBundleResult, err error) {
	t.Helper()
	args, err := buscodec.Encode(OpenBundleArgs{Alias: alias})
	require.NoError(t, err)
	obo := agentCall
	raw, err := h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: &obo}, opOpenBundle, args)
	if err != nil {
		return
	}
	out, err = buscodec.Decode[OpenBundleResult](raw)
	require.NoError(t, err)
	return
}

func withQueryEndpoint(t *testing.T, url string) {
	t.Helper()
	prev := introspect.LocalQueryEndpoint()
	introspect.SetLocalQueryEndpoint(url)
	t.Cleanup(func() { introspect.SetLocalQueryEndpoint(prev) })
}

// settleBundle drives the frame's bundle step until cond holds.
func settleBundle(t *testing.T, l *PlayLauncher, cond func() bool) {
	t.Helper()
	require.Eventually(t, func() bool {
		l.syncBundle()
		return cond()
	}, 5*time.Second, 5*time.Millisecond)
}

// open_bundle moves the window to the introspection endpoint and returns at
// once; the next frames apply the document and bind its datasets under
// their local names, then run it (ADR-0288 (proposed) §SD4).
func TestOpenBundleAppliesTheDocumentAndBindsLocalNames(t *testing.T) {
	fakeAppletDocs(t)
	withQueryEndpoint(t, "http://127.0.0.1:1/query")
	l, h, publisher := bundleLauncher(t)
	t.Cleanup(l.closeBundle)
	pub := publishSalesBundle(t, publisher, "-- datasets: orders,regions\nSELECT * FROM keelson('orders')", "orders", "regions")

	out, err := openBundleOp(t, h, "sales")
	require.NoError(t, err)
	assert.True(t, out.Retargeted)
	assert.Equal(t, "http://127.0.0.1:1/query", l.inner.client.URL())
	assert.Equal(t, "sales@0", h.ResourceValue(opsResBundle), "nothing is resolved on the render goroutine")

	settleBundle(t, l, func() bool { return len(l.inner.client.datasetBindings) == 2 })
	assert.Equal(t, "SELECT * FROM keelson('orders')", l.inner.sql)
	assert.Equal(t, pub.Datasets[0].Handle, l.inner.client.datasetBindings["orders"])
	assert.Equal(t, pub.Datasets[1].Handle, l.inner.client.datasetBindings["regions"])
	assert.True(t, l.inner.requestRun, "a plain read runs once its datasets are bound")
	assert.Equal(t, "sales@1", h.ResourceValue(opsResBundle))
}

// A republish reloads the document and rebinds a dataset the new revision
// dropped; a retract leaves the window waiting, and a publish reopens it.
func TestABundleWindowFollowsRepublishAndRetract(t *testing.T) {
	fakeAppletDocs(t)
	withQueryEndpoint(t, "http://127.0.0.1:1/query")
	l, h, publisher := bundleLauncher(t)
	t.Cleanup(l.closeBundle)
	publishSalesBundle(t, publisher, "-- datasets: orders,regions\nSELECT 1", "orders", "regions")
	_, err := openBundleOp(t, h, "sales")
	require.NoError(t, err)
	settleBundle(t, l, func() bool { return len(l.inner.client.datasetBindings) == 2 })

	second := publishSalesBundle(t, publisher, "-- datasets: orders\nSELECT 2", "orders")
	settleBundle(t, l, func() bool {
		return l.inner.sql == "SELECT 2" && l.inner.client.datasetBindings["orders"] == second.Datasets[0].Handle
	})
	_, stillBound := l.inner.client.datasetBindings["regions"]
	assert.False(t, stillBound, "the local name the new revision dropped is unbound")
	assert.Equal(t, "sales@2", h.ResourceValue(opsResBundle))

	require.NoError(t, adhocdata.RetractBundleRequest(publisher, "sales", nil))
	settleBundle(t, l, func() bool { return len(l.inner.client.datasetBindings) == 0 })
	settleBundle(t, l, func() bool {
		return l.inner.datasetNotice != nil
	})

	publishSalesBundle(t, publisher, "-- datasets: orders\nSELECT 3", "orders")
	settleBundle(t, l, func() bool { return l.inner.sql == "SELECT 3" && len(l.inner.client.datasetBindings) == 1 })
}

// A document whose SQL is not a plain read is applied but not run.
func TestABundleThatWritesIsNotRun(t *testing.T) {
	fakeAppletDocs(t)
	withQueryEndpoint(t, "http://127.0.0.1:1/query")
	l, h, publisher := bundleLauncher(t)
	t.Cleanup(l.closeBundle)
	publishSalesBundle(t, publisher, "-- datasets: orders\nINSERT INTO t SELECT * FROM keelson('orders')", "orders")
	_, err := openBundleOp(t, h, "sales")
	require.NoError(t, err)
	settleBundle(t, l, func() bool { return len(l.inner.client.datasetBindings) == 1 })
	assert.False(t, l.inner.requestRun)
}

func TestOpenBundleRefusals(t *testing.T) {
	l, h, _ := bundleLauncher(t)
	t.Cleanup(l.closeBundle)
	prev := appletDocParser.Load()
	appletDocParser.Store(nil)
	t.Cleanup(func() { appletDocParser.Store(prev) })
	_, err := openBundleOp(t, h, "sales")
	assert.ErrorContains(t, err, "parser")

	fakeAppletDocs(t)
	withQueryEndpoint(t, "")
	_, err = openBundleOp(t, h, "sales")
	assert.ErrorContains(t, err, "introspection endpoint")
	withQueryEndpoint(t, "http://127.0.0.1:1/query")
	_, err = openBundleOp(t, h, "not an alias")
	assert.Error(t, err)
}

func TestDecodeBundleRows(t *testing.T) {
	body := []byte(`{"alias":"sales","publisher":"apps/notebook","revision":"3","local_names":["orders"],"dataset_aliases":["sales__orders"],"task":"task-1","turn":"turn-2"}` + "\n")
	got, err := decodeBundleRows(body)
	require.NoError(t, err)
	assert.Equal(t, []BundleInfo{{Alias: "sales", Publisher: "apps/notebook", Revision: 3, LocalNames: []string{"orders"},
		DatasetAliases: []string{"sales__orders"}, Task: "task-1", Turn: "turn-2"}}, got)
}

// An agent's open of a bundle under a call no dispatcher confirms is
// refused by the dataset service, and the window says why.
func TestAnUnattestedOpenSaysWhy(t *testing.T) {
	fakeAppletDocs(t)
	withQueryEndpoint(t, "http://127.0.0.1:1/query")
	l, h, publisher := bindLauncher(t)
	t.Cleanup(l.closeBundle)
	publishSalesBundle(t, publisher, "-- datasets: orders\nSELECT 1", "orders")
	_, err := openBundleOp(t, h, "sales")
	require.NoError(t, err, "open_bundle itself accepts; the resolve is what is refused")
	settleBundle(t, l, func() bool {
		st := l.bundle
		st.mu.Lock()
		defer st.mu.Unlock()
		return strings.Contains(st.waiting, "not attested")
	})
	assert.Empty(t, l.inner.client.datasetBindings)
}
