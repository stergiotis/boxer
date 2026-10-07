package adhocdata

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

const testDoc = "---\ndatasets: [orders, regions]\ntabs: [table]\n---\n\n```sql\nSELECT * FROM keelson('orders')\n```\n"

func twoDatasets(t *testing.T) []BundleDatasetInput {
	return []BundleDatasetInput{
		{LocalName: "orders", ArrowIPCStream: int64Stream(t, false, 1, 2, 3)},
		{LocalName: "regions", ArrowIPCStream: int64Stream(t, false, 7)},
	}
}

var windowA = Identity{App: "test.app", Instance: 1}
var windowB = Identity{App: "test.app", Instance: 2}

func TestPublishBundleMakesEveryDatasetLiveUnderMintedAliases(t *testing.T) {
	svc := newTestService(t)
	res, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t), By: windowA})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), res.Revision)
	require.Len(t, res.Datasets, 2)
	assert.Equal(t, "sales__orders", res.Datasets[0].Alias)
	assert.Equal(t, uint64(3), res.Datasets[0].Rows)
	assert.Equal(t, "sales__regions", res.Datasets[1].Alias)
	assert.NotEmpty(t, res.DocumentDigest)

	for _, d := range res.Datasets {
		assert.True(t, svc.IsLive(d.Handle))
		got, rErr := svc.Resolve(d.Alias)
		require.NoError(t, rErr)
		assert.Equal(t, d.Handle, got.Handle, "the minted alias resolves to the bundle's dataset")
	}

	again, err := svc.ResolveBundle("sales", windowA, nil)
	require.NoError(t, err)
	assert.Equal(t, []byte(testDoc), again.Document)
	assert.Equal(t, res.Datasets, again.Datasets)
	assert.Equal(t, res.DocumentDigest, again.DocumentDigest)
}

func TestPublishBundleIsAllOrNothing(t *testing.T) {
	svc := newTestService(t)
	ds := twoDatasets(t)
	ds[1].ArrowIPCStream = unsupportedStream(t)
	_, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: ds, By: windowA})
	require.Error(t, err)
	assert.Equal(t, 0, svc.LiveCount(), "the dataset that sealed did not go live without its sibling")
	_, err = svc.ResolveBundle("sales", windowA, nil)
	assert.ErrorIs(t, err, ErrNoLiveBundle)
	_, err = svc.Resolve("sales__orders")
	assert.ErrorIs(t, err, ErrNoLiveDataset)
}

func TestPublishBundleRefusesAMalformedBundle(t *testing.T) {
	svc := newTestService(t)
	cases := map[string]BundlePublishInput{
		"no document":         {Alias: "b", Datasets: twoDatasets(t)},
		"no datasets":         {Alias: "b", Document: []byte(testDoc)},
		"separator in alias":  {Alias: "b__c", Document: []byte(testDoc), Datasets: twoDatasets(t)},
		"local name repeated": {Alias: "b", Document: []byte(testDoc), Datasets: []BundleDatasetInput{{LocalName: "x", ArrowIPCStream: int64Stream(t, false, 1)}, {LocalName: "x", ArrowIPCStream: int64Stream(t, false, 2)}}},
		"invalid local name":  {Alias: "b", Document: []byte(testDoc), Datasets: []BundleDatasetInput{{LocalName: "1x", ArrowIPCStream: int64Stream(t, false, 1)}}},
		"document too large":  {Alias: "b", Document: make([]byte, MaxBundleDocumentBytes+1), Datasets: twoDatasets(t)},
	}
	tooMany := BundlePublishInput{Alias: "b", Document: []byte(testDoc)}
	for i := range MaxBundleDatasets + 1 {
		tooMany.Datasets = append(tooMany.Datasets, BundleDatasetInput{LocalName: "d" + string(rune('a'+i)), ArrowIPCStream: int64Stream(t, false, 1)})
	}
	cases["too many datasets"] = tooMany
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			in.By = windowA
			_, err := svc.PublishBundle(in)
			require.Error(t, err)
			assert.Equal(t, 0, svc.LiveCount())
		})
	}
}

func TestRepublishBundleSwapsEverythingAndKeepsTheOldReadable(t *testing.T) {
	svc := newTestService(t)
	first, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t), By: windowA})
	require.NoError(t, err)
	doc2 := []byte(testDoc + "\n<!-- v2 -->\n")
	second, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: doc2, By: windowA,
		Datasets: []BundleDatasetInput{{LocalName: "orders", ArrowIPCStream: int64Stream(t, false, 9, 9)}}})
	require.NoError(t, err)
	assert.Equal(t, uint64(2), second.Revision)
	assert.Equal(t, first.CreatedAtUs, second.CreatedAtUs, "a republish keeps the creation instant, as a dataset's does")
	assert.NotEqual(t, first.DocumentDigest, second.DocumentDigest)

	for _, d := range first.Datasets {
		assert.False(t, svc.IsLive(d.Handle), "the previous revision's datasets left")
		_, registered := svc.reg.Lookup(d.Handle)
		assert.True(t, registered, "and stay readable for the grace")
	}
	_, err = svc.Resolve("sales__regions")
	assert.ErrorIs(t, err, ErrNoLiveDataset, "a local name the new revision dropped no longer resolves")
	got, err := svc.Resolve("sales__orders")
	require.NoError(t, err)
	assert.Equal(t, second.Datasets[0].Handle, got.Handle)
	assert.Equal(t, uint64(2), got.Rows)
	assert.Equal(t, 1, svc.LiveCount())
}

func TestBundleOwnershipAndAliasCollisions(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t), By: windowA})
	require.NoError(t, err)

	_, err = svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t), By: windowB})
	assert.ErrorIs(t, err, ErrNotOwner, "another window cannot republish the bundle")
	assert.ErrorIs(t, svc.RetractBundle("sales", windowB, nil), ErrNotOwner)

	_, err = svc.Publish(PublishInput{Alias: "sales__orders", ArrowIPCStream: int64Stream(t, false, 1), By: windowB})
	assert.ErrorIs(t, err, ErrAliasHeld, "a dataset cannot take a bundle member's alias")
	_, err = svc.Publish(PublishInput{Alias: "sales", ArrowIPCStream: int64Stream(t, false, 1), By: windowA})
	assert.ErrorIs(t, err, ErrAliasHeld, "nor the bundle's own alias, even by its owner")

	member, err := svc.Resolve("sales__orders")
	require.NoError(t, err)
	assert.ErrorIs(t, svc.Retract(member.Handle, windowA), ErrBundleMember, "a member is retracted with its bundle")
	_, err = svc.Publish(PublishInput{Alias: "sales__orders", Handle: member.Handle, ArrowIPCStream: int64Stream(t, false, 1), By: windowA})
	assert.ErrorIs(t, err, ErrBundleMember, "and republished with it")

	_, err = svc.Publish(PublishInput{Alias: "plain", ArrowIPCStream: int64Stream(t, false, 1), By: windowB})
	require.NoError(t, err)
	_, err = svc.PublishBundle(BundlePublishInput{Alias: "plain", Document: []byte(testDoc), Datasets: twoDatasets(t), By: windowA})
	assert.ErrorIs(t, err, ErrAliasHeld, "a bundle cannot take another owner's dataset alias")
}

func TestRetractBundleWithdrawsItWhole(t *testing.T) {
	svc := newTestService(t)
	res, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t), By: windowA})
	require.NoError(t, err)
	require.NoError(t, svc.RetractBundle("sales", windowA, nil))
	assert.Equal(t, 0, svc.LiveCount())
	_, err = svc.ResolveBundle("sales", windowA, nil)
	assert.ErrorIs(t, err, ErrNoLiveBundle)
	svc.FlushRetracts()
	for _, d := range res.Datasets {
		_, registered := svc.reg.Lookup(d.Handle)
		assert.False(t, registered)
	}
	_, err = svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t), By: windowB})
	require.NoError(t, err, "a retracted alias is free again")
}

func TestPublishBundleRespectsTheCountQuota(t *testing.T) {
	svc := newTestService(t)
	stream := int64Stream(t, false, 1)
	for range MaxDatasets - 1 {
		_, err := svc.Publish(PublishInput{Alias: "fill", ArrowIPCStream: stream})
		require.NoError(t, err)
	}
	_, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t), By: windowA})
	require.Error(t, err)
	assert.Equal(t, MaxDatasets-1, svc.LiveCount(), "a bundle that does not fit adds nothing")
}

// A receiver that publishes many bundles meets its own count first, and
// another window still publishes (ADR-0288 (proposed) §SD9).
func TestPublishBundleRespectsThePerOwnerQuota(t *testing.T) {
	svc := newTestService(t)
	stream := int64Stream(t, false, 1)
	for range MaxDatasetsPerOwner - 1 {
		_, err := svc.Publish(PublishInput{Alias: "fill", ArrowIPCStream: stream, By: windowA})
		require.NoError(t, err)
	}
	_, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t), By: windowA})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "per-owner")
	_, err = svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t), By: windowB})
	require.NoError(t, err)
}

func TestBundleCatalogs(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t), By: windowA})
	require.NoError(t, err)
	rows := svc.bundleCatalogRows()
	require.Len(t, rows, 1)
	assert.Equal(t, []string{"orders", "regions"}, rows[0].localNames)
	assert.Equal(t, []string{"sales__orders", "sales__regions"}, rows[0].datasetAliases)
	assert.Equal(t, int64(len(testDoc)), rows[0].documentBytes)
	for _, r := range svc.catalogRows() {
		assert.Equal(t, "sales", r.bundle)
	}
	batch, err := newBundleCatalogProvider(svc).Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer batch.Release()
	assert.Equal(t, int64(1), batch.NumRows())
}

func TestBundleOverTheBus(t *testing.T) {
	logger := testLogger(t)
	bus := inprocbus.NewInst(logger)
	svc, err := NewService(Config{Bus: bus, Registry: introspect.NewRegistry(), Dir: t.TempDir(), Log: logger})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	caps := []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}}

	var mu sync.Mutex
	var bundleEvents, datasetEvents []Event
	watcher := bus.NewClient("test.watcher", caps)
	_, err = watcher.Subscribe(SubjectBundleEventAll, func(msg *app.Msg) {
		ev, dErr := DecodeEvent(msg.Subject, msg.Payload)
		require.NoError(t, dErr)
		mu.Lock()
		bundleEvents = append(bundleEvents, ev)
		mu.Unlock()
	})
	require.NoError(t, err)
	_, err = SubscribeEvents(watcher, func(ev Event) {
		mu.Lock()
		datasetEvents = append(datasetEvents, ev)
		mu.Unlock()
	})
	require.NoError(t, err)

	window := bus.NewClient("test.app", caps)
	window.SetInstanceKey(1)
	pub, err := PublishBundleRequest(window, BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t)})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), pub.Revision)
	require.Len(t, pub.Datasets, 2)

	got, err := ResolveBundleRequest(window, "sales", nil)
	require.NoError(t, err)
	assert.Equal(t, []byte(testDoc), got.Document)
	assert.Equal(t, pub.Datasets[1].Handle, got.Datasets[1].Handle)
	assert.Equal(t, "sales__regions", got.Datasets[1].Alias)

	other := bus.NewClient("test.app", caps)
	other.SetInstanceKey(2)
	require.Error(t, RetractBundleRequest(other, "sales", nil), "the owner is the envelope's sender")

	require.NoError(t, window.Close())
	_, err = ResolveBundleRequest(other, "sales", nil)
	assert.ErrorIs(t, err, ErrNoLiveBundle, "a bundle goes with the window that published it")
	assert.Equal(t, 0, svc.LiveCount())

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bundleEvents, 2)
	assert.Equal(t, EventOpPublished, bundleEvents[0].Op)
	assert.Equal(t, "sales", bundleEvents[0].Bundle)
	assert.Equal(t, EventOpRetracted, bundleEvents[1].Op)
	assert.Len(t, datasetEvents, 4, "each dataset announces its own publish and retract")
	assert.True(t, slices.ContainsFunc(datasetEvents, func(ev Event) bool { return ev.Bundle == "sales" && ev.Alias == "sales__orders" }))
}

func TestResolveBundleRequestNamesNothingLive(t *testing.T) {
	logger := testLogger(t)
	bus := inprocbus.NewInst(logger)
	svc, err := NewService(Config{Bus: bus, Registry: introspect.NewRegistry(), Dir: t.TempDir(), Log: logger})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	c := bus.NewClient("test.app", []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}})
	_, err = ResolveBundleRequest(c, "nothing", nil)
	assert.True(t, errors.Is(err, ErrNoLiveBundle), err)
}
