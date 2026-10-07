package sqlapplet

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

func viewRig(t *testing.T) (bus *inprocbus.Inst, publisher app.BusI, viewBus app.BusI) {
	t.Helper()
	prev := introspect.LocalQueryEndpoint()
	introspect.SetLocalQueryEndpoint("http://127.0.0.1:1/query")
	t.Cleanup(func() { introspect.SetLocalQueryEndpoint(prev) })
	bus = inprocbus.NewInst(zerolog.Nop())
	svc, err := adhocdata.NewService(adhocdata.Config{Bus: bus, Registry: introspect.NewRegistry(), Dir: t.TempDir(), Log: zerolog.Nop()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	publisher = bus.NewClient("test.producer", []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}})
	viewBus = bus.NewClient("test.receiver", BundleViewCaps)
	return
}

func viewInts(t *testing.T, vals ...int64) []byte {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Int64Builder).AppendValues(vals, nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	stream, err := adhocdata.EncodeRecord(rec)
	require.NoError(t, err)
	return stream
}

func publishView(t *testing.T, bus app.BusI, sql string, locals ...string) (res adhocdata.BundleResult) {
	t.Helper()
	spec := play.BundleSpec{Alias: "counts", Sql: sql, Tabs: []string{"table"}}
	for i, l := range locals {
		spec.Datasets = append(spec.Datasets, adhocdata.BundleDatasetInput{LocalName: l, ArrowIPCStream: viewInts(t, int64(i))})
	}
	res, err := play.PublishBundleE(bus, spec)
	require.NoError(t, err)
	return
}

func syncUntil(t *testing.T, v *BundleView, cond func() bool) {
	t.Helper()
	require.Eventually(t, func() bool {
		v.Sync()
		return cond()
	}, 5*time.Second, 5*time.Millisecond)
}

// A bundle view is one constructor and the receiver's manifest entry: it
// builds the embedded play from the bundle's document, binds the datasets
// under their local names, runs once they are bound, and opens the bundle,
// not its buffer, in a playground (ADR-0288 (proposed) §SD7).
func TestABundleViewShowsTheBundle(t *testing.T) {
	_, publisher, viewBus := viewRig(t)
	pub := publishView(t, publisher, "SELECT * FROM keelson('result')", "result")
	v := NewBundleView("counts", BundleViewConfig{Bus: viewBus, Log: zerolog.Nop(), StampAppId: "test.receiver#counts"})
	t.Cleanup(v.Close)
	assert.Nil(t, v.Inner(), "nothing is asked of the bus before the first frame")

	syncUntil(t, v, func() bool { return v.Inner() != nil && len(v.Inner().DatasetBindingsForTest()) == 1 })
	inner := v.Inner()
	assert.Equal(t, "SELECT * FROM keelson('result')", inner.BufferForTest())
	assert.Equal(t, pub.Datasets[0].Handle, inner.DatasetBindingsForTest()["result"])
	assert.True(t, inner.RunRequestedForTest(), "a plain read runs once its datasets are bound")
	assert.Equal(t, "counts", inner.OpenPlaygroundBundleForTest())
	assert.Equal(t, uint64(1), v.Revision())
}

// A view not synced for a while shows the bundle as it stands on the next
// frame: a republish of its data rebinds, a new document rebuilds the
// embedded play, a retract and republish in between land as the last one.
func TestABundleViewCatchesUpOnTheFrameItIsDrawn(t *testing.T) {
	_, publisher, viewBus := viewRig(t)
	publishView(t, publisher, "SELECT * FROM keelson('result')", "result")
	v := NewBundleView("counts", BundleViewConfig{Bus: viewBus, Log: zerolog.Nop()})
	t.Cleanup(v.Close)
	syncUntil(t, v, func() bool { return v.Inner() != nil && len(v.Inner().DatasetBindingsForTest()) == 1 })
	first := v.Inner()

	// Undrawn while the bundle moves twice: a new document, a retract, a
	// republish.
	publishView(t, publisher, "SELECT n FROM keelson('result') ORDER BY n", "result")
	require.NoError(t, adhocdata.RetractBundleRequest(publisher, "counts", nil))
	last := publishView(t, publisher, "SELECT count() FROM keelson('rows')", "rows")
	assert.Equal(t, first, v.Inner(), "nothing reached the undrawn view")

	syncUntil(t, v, func() bool {
		return v.Inner() != first && v.Inner() != nil && v.Inner().DatasetBindingsForTest()["rows"] == last.Datasets[0].Handle
	})
	assert.Equal(t, "SELECT count() FROM keelson('rows')", v.Inner().BufferForTest())
	_, stale := v.Inner().DatasetBindingsForTest()["result"]
	assert.False(t, stale)
}

// A view whose bundle is not live says what it waits for, and opens the
// bundle once it is published.
func TestABundleViewWaitsForItsBundle(t *testing.T) {
	_, publisher, viewBus := viewRig(t)
	v := NewBundleView("counts", BundleViewConfig{Bus: viewBus, Log: zerolog.Nop()})
	t.Cleanup(v.Close)
	syncUntil(t, v, func() bool {
		v.mu.Lock()
		defer v.mu.Unlock()
		return !v.inflight && v.waiting != "not asked yet"
	})
	assert.Nil(t, v.Inner())
	publishView(t, publisher, "SELECT * FROM keelson('result')", "result")
	syncUntil(t, v, func() bool { return v.Inner() != nil && len(v.Inner().DatasetBindingsForTest()) == 1 })
}
