package play

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

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// bindLauncher is a mounted window over a running ad-hoc service, and a
// publisher on the same bus.
func bindLauncher(t *testing.T) (l *PlayLauncher, h app.OperationsHandlerI, publisher app.BusI) {
	t.Helper()
	logger := zerolog.Nop()
	bus := inprocbus.NewInst(logger)
	svc, err := adhocdata.NewService(adhocdata.Config{Bus: bus, Registry: introspect.NewRegistry(), Dir: t.TempDir(), Log: logger})
	require.NoError(t, err)
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

func publishInts(t *testing.T, bus app.BusI, alias string) (res adhocdata.PublishResult) {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2}, nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	stream, err := adhocdata.EncodeRecord(rec)
	require.NoError(t, err)
	res, err = adhocdata.PublishRequest(bus, adhocdata.PublishInput{Alias: alias, ArrowIPCStream: stream})
	require.NoError(t, err)
	return
}

func bindDataset(t *testing.T, h app.OperationsHandlerI, alias string) (out BindDatasetResult, err error) {
	args, err := buscodec.Encode(BindDatasetArgs{Alias: alias})
	require.NoError(t, err)
	raw, err := h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: &app.OnBehalfOf{Task: "t"}}, opBindDataset, args)
	if err != nil {
		return
	}
	out, err = buscodec.Decode[BindDatasetResult](raw)
	require.NoError(t, err)
	return
}

// waitBound drives the frame's follower step until the alias is bound.
func waitBound(t *testing.T, l *PlayLauncher, alias string) {
	t.Helper()
	require.Eventually(t, func() bool {
		l.follower.Sync(l.inner)
		for _, a := range l.inner.client.DatasetAliases() {
			if a == alias {
				return true
			}
		}
		return false
	}, 5*time.Second, 5*time.Millisecond)
}

// A window whose launch config declared nothing binds an alias another
// window published; the binding runs nothing, and binding it again reports
// it bound.
func TestBindDatasetBindsAPublishedAlias(t *testing.T) {
	l, h, publisher := bindLauncher(t)
	res := publishInts(t, publisher, "gitpulse_change_facts")

	out, err := bindDataset(t, h, "gitpulse_change_facts")
	require.NoError(t, err)
	assert.Equal(t, BindDatasetResult{Alias: "gitpulse_change_facts", ReadWith: "keelson('gitpulse_change_facts')",
		Destination: "keelson:gitpulse_change_facts", Waiting: adhocdata.WaitNotAsked}, out)
	require.NotNil(t, l.follower)
	assert.Empty(t, l.inner.client.DatasetAliases(), "nothing resolves on the render goroutine")

	waitBound(t, l, "gitpulse_change_facts")
	assert.Equal(t, res.Handle, l.inner.client.datasetBindings["gitpulse_change_facts"])
	assert.False(t, l.inner.requestRun, "an agent's bind runs nothing")
	assert.Equal(t, "gitpulse_change_facts|", h.ResourceValue(opsResDatasets))

	out, err = bindDataset(t, h, "gitpulse_change_facts")
	require.NoError(t, err)
	assert.True(t, out.Bound)
}

// An alias nothing is published under yet waits, and binds once it is.
func TestBindDatasetWaitsForAPublish(t *testing.T) {
	l, h, publisher := bindLauncher(t)
	_, err := bindDataset(t, h, "later")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		l.follower.Sync(l.inner)
		return l.follower.Waiting()["later"] == adhocdata.WaitNoLive
	}, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, "|later", h.ResourceValue(opsResDatasets))

	// list_datasets names the waiting alias and why, rather than nothing.
	list, err := listDatasets(l.inner.client, nil, "", l.follower.Waiting())
	require.NoError(t, err)
	require.Len(t, list.Datasets, 1)
	assert.Equal(t, DatasetInfo{Alias: "later", Destination: "keelson:later", Granted: true, Waiting: adhocdata.WaitNoLive}, list.Datasets[0])
	list, err = listDatasets(l.inner.client, nil, "later", l.follower.Waiting())
	require.NoError(t, err)
	assert.Equal(t, adhocdata.WaitNoLive, list.Datasets[0].Waiting)

	publishInts(t, publisher, "later")
	waitBound(t, l, "later")
}

func TestBindDatasetRefusesAHandleAndANonIdentifier(t *testing.T) {
	_, h, _ := bindLauncher(t)
	_, err := bindDataset(t, h, "adhoc_0123456789abcdef")
	assert.ErrorContains(t, err, "dataset handle")
	_, err = bindDataset(t, h, "not an alias")
	assert.ErrorContains(t, err, "bare identifier")
}

// Binding several aliases in a row is never a conflict: what bind_dataset
// writes, the followed set, does not move when a bind lands later.
func TestBindDatasetWritesOnlyTheFollowedSet(t *testing.T) {
	spec, ok := (&PlayLauncher{}).Manifest().Operations.Lookup(opBindDataset)
	require.True(t, ok)
	assert.Equal(t, []string{opsResFollowed}, spec.Writes)

	l, h, publisher := bindLauncher(t)
	publishInts(t, publisher, "a")
	_, err := bindDataset(t, h, "a")
	require.NoError(t, err)
	followed := h.ResourceValue(opsResFollowed)
	assert.Equal(t, "a", followed)
	waitBound(t, l, "a")
	assert.Equal(t, followed, h.ResourceValue(opsResFollowed), "the bind landing leaves the followed set as it was")
}

// A launch config's alias binding re-runs the buffer, as it always has; the
// count is what tells it apart from an agent's.
func TestBoundLaunchAliasesCountsOnlyTheConfigs(t *testing.T) {
	l, _, _ := bindLauncher(t)
	l.launchAliases = []string{"declared"}
	l.inner.client.bindDataset("declared", "adhoc_1")
	l.inner.client.bindDataset("added", "adhoc_2")
	assert.Equal(t, 1, l.boundLaunchAliases())
}
