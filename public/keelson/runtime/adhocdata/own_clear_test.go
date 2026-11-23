package adhocdata

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// Only the reading task's own publication at TLP:CLEAR reads back as its
// own: a person's read, an app's own read, another task's read, a dataset a
// person published, one published above CLEAR and one republished by
// someone else since all stay sealed.
func TestOnlyATasksOwnClearPublicationReadsBackAsItsOwn(t *testing.T) {
	svc := newTestService(t)
	d := &fakeDispatcher{call: agentCall(), grants: []string{"keelson-bundle:sales", "keelson:persons", "keelson-bundle:people"}}
	svc.SetDispatcher(d)
	own := oboOf(d.call)
	publishSales(t, svc, windowA, own, BundleProvenance{}, 1, 2, 3)

	got, err := svc.Read(t.Context(), "sales__orders", windowA, own)
	require.NoError(t, err)
	assert.True(t, got.OwnClear, "the task's own call published it at TLP:CLEAR")
	audits := svc.auditRecords()
	last := audits[len(audits)-1]
	assert.Equal(t, AuditRead, last.Operation)
	assert.Equal(t, AuditApplied, last.Outcome)
	assert.Contains(t, last.Reason, "the task's own publication at TLP:CLEAR", "the trail says why the read left no label")
	assert.Contains(t, last.Reason, "task-1-7", "and names the call that published it")

	got, err = svc.Read(t.Context(), "sales__orders", windowA, nil)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "a person's read, and an app's own, name no task")
	audits = svc.auditRecords()
	assert.Empty(t, audits[len(audits)-1].Reason)

	_, err = svc.Read(t.Context(), "sales__orders", windowB, own)
	assert.ErrorIs(t, err, ErrUnattested, "a call the dispatcher does not confirm reads nothing")

	other := d.as(taskTwo)
	got, err = svc.Read(t.Context(), "sales__orders", windowA, other)
	require.NoError(t, err, "its grant lists the bundle")
	assert.False(t, got.OwnClear, "another task's read")

	own = d.as(taskOne)
	_, err = svc.Publish(PublishInput{Alias: "persons", ArrowIPCStream: int64Stream(t, false, 9), By: windowB})
	require.NoError(t, err)
	got, err = svc.Read(t.Context(), "persons", windowA, own)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "a dataset a person published on its own")
	_, err = svc.PublishBundle(t.Context(), BundlePublishInput{Alias: "people", Document: []byte(testDoc), By: windowB, Datasets: twoDatasets(t)})
	require.NoError(t, err)
	got, err = svc.Read(t.Context(), "people__orders", windowA, own)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "a bundle a person published")

	amber := d.as(func(cc *app.CallContext) { cc.Tier = uint8(agentrun.TierAmber) })
	publishSales(t, svc, windowA, amber, BundleProvenance{}, 4)
	clear := d.as(func(cc *app.CallContext) { cc.Tier = uint8(agentrun.TierClear) })
	got, err = svc.Read(t.Context(), "sales__orders", windowA, clear)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "its live revision was published above TLP:CLEAR")

	publishSales(t, svc, windowA, clear, BundleProvenance{}, 5)
	got, err = svc.Read(t.Context(), "sales__orders", windowA, clear)
	require.NoError(t, err)
	assert.True(t, got.OwnClear, "published again at TLP:CLEAR, the live revision is its own")

	amber = d.as(func(cc *app.CallContext) { cc.Tier = uint8(agentrun.TierAmber) })
	got, err = svc.Read(t.Context(), "sales__orders", windowA, amber)
	require.NoError(t, err)
	assert.True(t, got.OwnClear, "the tier of the reading call does not decide: the read adds no label to whatever the conversation holds")

	publishSales(t, svc, windowA, nil, BundleProvenance{}, 6)
	clear = d.as(func(cc *app.CallContext) { cc.Tier = uint8(agentrun.TierClear) })
	got, err = svc.Read(t.Context(), "sales__orders", windowA, clear)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "the person republished it since")

	publishSales(t, svc, windowA, d.as(taskTwo), BundleProvenance{}, 7)
	got, err = svc.Read(t.Context(), "sales__orders", windowA, d.as(taskOne))
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "another task republished it since")
}

// A publication that derives from sealed data does not read back as the
// task's own, whatever tier its call declared: the publisher's declaration
// and an input the service finds sealed both keep it sealed.
func TestAPublicationDerivedFromSealedDataReadsBackSealed(t *testing.T) {
	svc := newTestService(t)
	d := &fakeDispatcher{call: agentCall()}
	svc.SetDispatcher(d)
	own := oboOf(d.call)
	publishSales(t, svc, windowA, own, BundleProvenance{DerivedFromSealed: true}, 1)
	got, err := svc.Read(t.Context(), "sales__orders", windowA, own)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "declared at TLP:CLEAR in the round the sealed result arrived in")

	clean := publishSales(t, svc, windowA, own, BundleProvenance{}, 2)
	persons, err := svc.Publish(PublishInput{Alias: "persons", ArrowIPCStream: int64Stream(t, false, 9), By: windowB})
	require.NoError(t, err)
	summary := func(inputs ...string) {
		_, err = svc.PublishBundle(t.Context(), BundlePublishInput{Alias: "summary", Document: []byte(testDoc), By: windowA, OnBehalfOf: own,
			Datasets:   []BundleDatasetInput{{LocalName: "orders", ArrowIPCStream: int64Stream(t, false, 6)}},
			Provenance: BundleProvenance{InputHandles: inputs}})
		require.NoError(t, err)
	}
	summary(clean.Datasets[0].Handle)
	got, err = svc.Read(t.Context(), "summary__orders", windowA, own)
	require.NoError(t, err)
	assert.True(t, got.OwnClear, "made from the task's own clear publication alone")
	summary(clean.Datasets[0].Handle, persons.Handle)
	got, err = svc.Read(t.Context(), "summary__orders", windowA, own)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "made from a dataset a person published too")
}

// The answer belongs to the revision the read hands out: a reader opened
// before a republish keeps that revision's publication, and under
// concurrent republishes no read is ever answered as the task's own with
// the bytes of the labelled revision.
func TestTheAnswerBelongsToTheRevisionRead(t *testing.T) {
	svc := newTestService(t)
	d := &fakeDispatcher{call: agentCall(), grants: []string{"keelson-bundle:sales"}}
	svc.SetDispatcher(d)
	clear := oboOf(d.call)
	publishSales(t, svc, windowA, clear, BundleProvenance{}, 1)

	rec, err := svc.newest("sales__orders")
	require.NoError(t, err)
	rc, pub, err := rec.openPublished()
	require.NoError(t, err)
	publishSales(t, svc, windowA, nil, BundleProvenance{}, 2)
	assert.Equal(t, standingOwnClear, standingOf(pub.context, pub.derived, "task-1"), "the reader opened before the republish reads the task's revision")
	require.NoError(t, rc.Close())
	got, err := svc.Read(t.Context(), "sales__orders", windowA, clear)
	require.NoError(t, err)
	assert.False(t, got.OwnClear)
	assert.Equal(t, []int64{2}, int64Values(t, got.ArrowIPCStream))

	// One publisher alternates the task's clear revision (value 1) and the
	// person's (value 2); the dispatcher attests the one call throughout.
	clearStream, sealedStream := int64Stream(t, false, 1), int64Stream(t, false, 2)
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; !stop.Load(); i++ {
			in := BundlePublishInput{Alias: "sales", Document: []byte(testDoc), By: windowA,
				Datasets: []BundleDatasetInput{{LocalName: "orders", ArrowIPCStream: sealedStream}}}
			if i%2 == 0 {
				in.OnBehalfOf, in.Datasets[0].ArrowIPCStream = clear, clearStream
			}
			if _, pErr := svc.PublishBundle(t.Context(), in); pErr != nil {
				t.Errorf("publish: %v", pErr)
				return
			}
		}
	})
	var own, sealed int
	for i := 0; i < 400; i++ {
		res, rErr := svc.Read(t.Context(), "sales__orders", windowA, clear)
		require.NoError(t, rErr)
		vals := int64Values(t, res.ArrowIPCStream)
		require.Len(t, vals, 1)
		require.Equal(t, vals[0] == 1, res.OwnClear, "read %d: value %d answered own-clear=%v", i, vals[0], res.OwnClear)
		if res.OwnClear {
			own++
		} else {
			sealed++
		}
	}
	stop.Store(true)
	wg.Wait()
	t.Logf("%d reads of the task's revision, %d of the person's", own, sealed)
}

// A dataset read by its handle through this process's plane is judged by
// the handle: a bundle's dataset keeps its bytes and its publication for as
// long as the handle is registered, so the answer for the handle holds for
// every read of it.
func TestAHandlesLabelIsItsPublications(t *testing.T) {
	svc, bus := readService(t)
	d := &fakeDispatcher{call: agentCall()}
	svc.SetDispatcher(d)
	own := oboOf(d.call)
	first := publishSales(t, svc, windowA, own, BundleProvenance{}, 1)
	handle := first.Datasets[0].Handle

	got, err := svc.ReadLabel(t.Context(), handle, windowA, own)
	require.NoError(t, err)
	assert.True(t, got.OwnClear)
	assert.Equal(t, "sales__orders", got.Alias)
	audits := svc.auditRecords()
	last := audits[len(audits)-1]
	assert.Equal(t, AuditReadLabel, last.Operation)
	assert.Equal(t, []string{handle}, last.Handles)
	assert.Contains(t, last.Reason, "the task's own publication at TLP:CLEAR")
	require.True(t, last.Context.Has)
	assert.Equal(t, "conv-9", last.Context.Val.Conversation, "the row is attributed to the reading call")

	n := len(svc.auditRecords())
	got, err = svc.ReadLabel(t.Context(), handle, windowA, nil)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "the person's read")
	got, err = svc.ReadLabel(t.Context(), "adhoc_00000000000000ff", windowA, own)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "a handle the service does not hold")
	assert.Len(t, svc.auditRecords(), n, "an answer that leaves a read sealed changes nothing and is not recorded")

	_, err = svc.ReadLabel(t.Context(), handle, windowB, own)
	assert.ErrorIs(t, err, ErrUnattested)
	audits = svc.auditRecords()
	assert.Equal(t, AuditRefused, audits[len(audits)-1].Outcome)

	got, err = svc.ReadLabel(t.Context(), handle, windowA, d.as(taskTwo))
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "another task's read")
	own = d.as(taskOne)

	solo, err := svc.Publish(PublishInput{Alias: "persons", ArrowIPCStream: int64Stream(t, false, 9), By: windowA})
	require.NoError(t, err)
	got, err = svc.ReadLabel(t.Context(), solo.Handle, windowA, own)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "a dataset published on its own has no attested publisher")
	_, err = svc.Publish(PublishInput{Alias: "sales__orders", Handle: handle, ArrowIPCStream: int64Stream(t, false, 8), By: windowA})
	assert.ErrorIs(t, err, ErrBundleMember, "a bundle's dataset is never replaced under its handle")

	// The person republishes: the old handle still reads the task's bytes
	// until it is unloaded, the new one reads the person's.
	second := publishSales(t, svc, windowA, nil, BundleProvenance{}, 2)
	got, err = svc.ReadLabel(t.Context(), handle, windowA, own)
	require.NoError(t, err)
	assert.True(t, got.OwnClear, "the handle that left still names the task's revision")
	got, err = svc.ReadLabel(t.Context(), second.Datasets[0].Handle, windowA, own)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "the republish has a handle of its own")
	svc.FlushRetracts()
	got, err = svc.ReadLabel(t.Context(), handle, windowA, own)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "an unloaded handle reads nothing")

	third := publishSales(t, svc, windowA, own, BundleProvenance{DerivedFromSealed: true}, 3)
	got, err = svc.ReadLabel(t.Context(), third.Datasets[0].Handle, windowA, own)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "derived from sealed data")

	// Over the bus, as an app asks.
	fourth := publishSales(t, svc, windowA, own, BundleProvenance{}, 4)
	window := bus.NewClient(windowA.App, []app.SubjectFilter{{Pattern: "runtime.adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}})
	window.SetInstanceKey(windowA.Instance)
	res, err := ReadLabelRequest(t.Context(), window, fourth.Datasets[0].Handle, own)
	require.NoError(t, err)
	assert.True(t, res.OwnClear)
	assert.Equal(t, "sales", res.Bundle)
	res, err = ReadLabelRequest(t.Context(), window, fourth.Datasets[0].Handle, nil)
	require.NoError(t, err)
	assert.False(t, res.OwnClear)
	other := bus.NewClient(windowB.App, []app.SubjectFilter{{Pattern: "runtime.adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}})
	other.SetInstanceKey(windowB.Instance)
	_, err = ReadLabelRequest(t.Context(), other, fourth.Datasets[0].Handle, own)
	assert.Error(t, err, "a window the call was not routed to is refused")
}

// The answer crosses the bus with the stream, so an app that takes a
// dataset in on a task's behalf can label what it makes of it; a reader
// that does not look at it stays sealed, the field's zero value.
func TestTheReadReplyCarriesTheAnswer(t *testing.T) {
	svc, bus := readService(t)
	d := &fakeDispatcher{call: agentCall()}
	svc.SetDispatcher(d)
	own := oboOf(d.call)
	publishSales(t, svc, windowA, own, BundleProvenance{}, 1, 2)
	window := bus.NewClient(windowA.App, []app.SubjectFilter{{Pattern: "runtime.adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}})
	window.SetInstanceKey(windowA.Instance)

	got, err := ReadAll(t.Context(), window, "sales__orders", own)
	require.NoError(t, err)
	assert.True(t, got.OwnClear)
	assert.Equal(t, []int64{1, 2}, int64Values(t, got.ArrowIPCStream))
	got, err = ReadAll(t.Context(), window, "sales__orders", nil)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "the app's own read")
	assert.False(t, ReadResult{}.OwnClear, "the zero value is the sealed one")

	// The declaration crosses the bus on a publish.
	_, err = PublishBundleRequest(t.Context(), window, BundlePublishInput{Alias: "sales", Document: []byte(testDoc), OnBehalfOf: own,
		Datasets:   []BundleDatasetInput{{LocalName: "orders", ArrowIPCStream: int64Stream(t, false, 3)}},
		Provenance: BundleProvenance{DerivedFromSealed: true}})
	require.NoError(t, err)
	got, err = ReadAll(t.Context(), window, "sales__orders", own)
	require.NoError(t, err)
	assert.False(t, got.OwnClear, "the publisher declared the rows derived from sealed data")
}
