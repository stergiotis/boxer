package adhocdata

import (
	"bytes"
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

func int64Values(t *testing.T, stream []byte) (vals []int64) {
	t.Helper()
	rdr, err := ipc.NewReader(bytes.NewReader(stream))
	require.NoError(t, err)
	defer rdr.Release()
	for rdr.Next() {
		vals = append(vals, rdr.RecordBatch().Column(0).(*array.Int64).Int64Values()...)
	}
	require.NoError(t, rdr.Err())
	return
}

func readService(t *testing.T) (svc *Service, bus *inprocbus.Inst) {
	logger := testLogger(t)
	bus = inprocbus.NewInst(logger)
	svc, err := NewService(Config{Bus: bus, Registry: introspect.NewRegistry(), Dir: t.TempDir(), Log: logger})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	captureAudits(svc)
	return
}

// A reader gets the stream as sealed — what every reader of the dataset
// reads, under the digest the publish was audited with — and the read is
// audited too (ADR-0288 (proposed) §SD6).
func TestReadAllEReturnsTheSealedStream(t *testing.T) {
	svc, bus := readService(t)
	caps := []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}}
	producer := bus.NewClient("test.play", caps)
	producer.SetInstanceKey(1)
	reader := bus.NewClient("test.notebook", caps)
	reader.SetInstanceKey(2)

	pub, err := PublishBundleRequest(producer, BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t)})
	require.NoError(t, err)

	got, err := ReadAllE(reader, "sales__orders", nil)
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2, 3}, int64Values(t, got.ArrowIPCStream))
	assert.Equal(t, "sales", got.Bundle)
	assert.Equal(t, pub.Datasets[0].Handle, got.Handle)
	assert.Equal(t, uint64(3), got.Rows)
	plain, _ := readAll(t, svc.reg, got.Handle)
	assert.Equal(t, plain, got.ArrowIPCStream, "the bytes /table serves to play")

	audits := svc.auditRecords()
	require.Len(t, audits, 2)
	assert.Equal(t, AuditRead, audits[1].Operation)
	assert.Equal(t, Identity{App: "test.notebook", Instance: 2}, audits[1].By)
	assert.Equal(t, audits[0].StreamDigests[0], audits[1].StreamDigests[0], "the read and the publish name the same bytes")

	_, err = ReadAllE(reader, "nothing", nil)
	assert.ErrorIs(t, err, ErrNoLiveDataset)
}

func TestReadAllECatchesADigestMismatch(t *testing.T) {
	svc, bus := readService(t)
	c := bus.NewClient("test.app", []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}})
	res, err := svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 4, 5)})
	require.NoError(t, err)
	svc.mu.RLock()
	rec := svc.live[res.Handle]
	svc.mu.RUnlock()
	rec.mu.Lock()
	rec.streamDigest = "00000000000000000000000000000000"
	rec.mu.Unlock()
	_, err = ReadAllE(c, "items", nil)
	assert.ErrorIs(t, err, ErrDigestMismatch)
}

func TestAnAgentsReadIsAttested(t *testing.T) {
	svc, bus := readService(t)
	d := &fakeDispatcher{call: agentCall()}
	d.call.App, d.call.Instance = "test.notebook", 2
	svc.SetDispatcher(d)
	_, err := svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 1)})
	require.NoError(t, err)
	reader := bus.NewClient("test.notebook", []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}})
	reader.SetInstanceKey(2)

	// Held to the grant as a run in play is (§SD6): refused, naming what
	// the grant would have to list, until it lists it.
	_, err = ReadAllE(reader, "items", oboOf(d.call))
	var ge *GrantError
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, "keelson:items", ge.Destination)
	assert.NotContains(t, ge.Error(), "agent limit: agent limit")
	d.mu.Lock()
	d.grants = []string{"keelson:items"}
	d.mu.Unlock()
	_, err = ReadAllE(reader, "items", oboOf(d.call))
	require.NoError(t, err)
	audits := svc.auditRecords()
	require.Len(t, audits, 2)
	assert.Equal(t, AuditRefused, audits[0].Outcome)
	audits = audits[1:]
	require.True(t, audits[0].Context.Has)
	assert.Equal(t, "turn-2", audits[0].Context.Val.Turn)

	other := bus.NewClient("test.other", []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}})
	_, err = ReadAllE(other, "items", oboOf(d.call))
	require.Error(t, err, "another app cannot read under the call")
}

// A bundle's dataset is covered by the bundle, and a refusal asks for the
// bundle, which covers its other datasets too.
func TestAnAgentsReadOfABundleNeedsTheBundle(t *testing.T) {
	svc, bus := readService(t)
	d := &fakeDispatcher{call: agentCall()}
	d.call.App, d.call.Instance = "test.notebook", 2
	svc.SetDispatcher(d)
	_, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t)})
	require.NoError(t, err)
	reader := bus.NewClient("test.notebook", []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}})
	reader.SetInstanceKey(2)
	_, err = ReadAllE(reader, "sales__orders", oboOf(d.call))
	var ge *GrantError
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, "keelson-bundle:sales", ge.Destination)
	d.mu.Lock()
	d.grants = []string{"keelson-bundle:sales"}
	d.mu.Unlock()
	got, err := ReadAllE(reader, "sales__regions", oboOf(d.call))
	require.NoError(t, err)
	assert.Equal(t, []int64{7}, int64Values(t, got.ArrowIPCStream))
}

func TestReadDestinations(t *testing.T) {
	assert.Equal(t, []string{"keelson-bundle:sales", "keelson:sales__orders"}, ReadDestinations("sales__orders"))
	assert.Equal(t, []string{"keelson:stats_w3"}, ReadDestinations("stats_w3"))
}

// A task reads what it published (ADR-0288 (proposed) §SD4): the live
// revision's attested publisher reads it without a grant entry; any other
// task still needs one, and the publisher task travels on the resolve.
func TestATaskReadsWhatItPublished(t *testing.T) {
	svc, bus := readService(t)
	d := &fakeDispatcher{call: agentCall()}
	svc.SetDispatcher(d)
	_, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t),
		By: windowA, OnBehalfOf: oboOf(d.call)})
	require.NoError(t, err)
	window := bus.NewClient(windowA.App, []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}})
	window.SetInstanceKey(windowA.Instance)

	got, err := ReadAllE(window, "sales__orders", oboOf(d.call))
	require.NoError(t, err, "no grant entry: the task published it")
	assert.Equal(t, "task-1", got.PublisherTask)
	res, err := ResolveBundleRequest(window, "sales", nil)
	require.NoError(t, err)
	assert.Equal(t, "task-1", res.PublisherTask, "the publisher task travels on the resolve")

	d.mu.Lock()
	d.call.Task, d.call.Call = "task-2", "task-2-1"
	d.mu.Unlock()
	_, err = ReadAllE(window, "sales__orders", oboOf(d.call))
	var ge *GrantError
	require.ErrorAs(t, err, &ge, "another task still needs the grant")
	assert.Equal(t, "keelson-bundle:sales", ge.Destination)
}
