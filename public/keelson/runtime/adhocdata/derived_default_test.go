package adhocdata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An app that reads a dataset a person published on a task's behalf,
// computes from it and publishes the outcome under a call of the same
// round declares the tier the conversation had before the read's outcome
// arrived: TLP:CLEAR. It states nothing about what its rows derive from
// and names no inputs, so the revision does not stand as the task's own
// clear work: only a publisher's assertion makes it so.
func TestAPublisherThatStatesNothingAfterASealedReadIsNotClear(t *testing.T) {
	r := newExposeRig(t)
	svc := r.svc
	d := &fakeDispatcher{call: agentCall(), grants: []string{"keelson:persons", "expose:summary"}}
	svc.SetDispatcher(d)
	own := oboOf(d.call)
	_, err := svc.Publish(PublishInput{Alias: "persons", ArrowIPCStream: int64Stream(t, false, 41, 42), By: windowB})
	require.NoError(t, err)

	// One round, every call at TLP:CLEAR: the app reads the person's
	// dataset for the task, and publishes what it made of it.
	got, err := svc.Read(t.Context(), "persons", windowA, own)
	require.NoError(t, err)
	require.False(t, got.OwnClear, "the read itself is answered as sealed")
	_, err = svc.PublishBundle(t.Context(), BundlePublishInput{Alias: "summary", Document: []byte(testDoc), By: windowA, OnBehalfOf: own,
		Datasets: []BundleDatasetInput{{LocalName: "orders", ArrowIPCStream: got.ArrowIPCStream}}})
	require.NoError(t, err)

	back, err := svc.Read(t.Context(), "summary__orders", windowA, own)
	require.NoError(t, err)
	assert.False(t, back.OwnClear, "the person's rows, republished, do not read back as the task's own clear work")
	_, err = svc.Expose(t.Context(), ExposeInput{Alias: "summary__orders", By: windowA, OnBehalfOf: own})
	assert.ErrorContains(t, err, "not known to derive from nothing sealed", "nor does the task expose them")
	audits := svc.auditRecords()
	var published AuditRecord
	for _, a := range audits {
		if a.Operation == AuditPublish && a.Bundle == "summary" {
			published = a
		}
	}
	assert.Equal(t, reasonNotStated, published.Reason, "the publish row says why the revision is sealed to its own task")
}

// A careless publisher — a call at TLP:CLEAR, no inputs named, nothing said
// about what the rows derive from — publishes sealed data: its task does
// not expose it and does not read it back as its own. The same publish
// with the assertion stands as the task's own clear work, and the service
// overrides an assertion an input contradicts.
func TestACarelessPublisherIsNeverExposedByItsTask(t *testing.T) {
	r := newExposeRig(t)
	svc := r.svc
	d := &fakeDispatcher{call: agentCall(), grants: []string{"expose:sales", "keelson:persons", "keelson-bundle:sales"}}
	svc.SetDispatcher(d)
	own := oboOf(d.call)
	require.Equal(t, uint8(0), d.call.Tier, "the call declares TLP:CLEAR")

	res := publishSales(t, svc, windowA, own, BundleProvenance{}, 1)
	assert.False(t, res.FromNothingSealed)
	_, err := svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA, OnBehalfOf: own})
	require.Error(t, err, "nothing was asserted, so nothing is the task's to release")
	assert.ErrorContains(t, err, "not known to derive from nothing sealed")
	got, err := svc.Read(t.Context(), "sales__orders", windowA, own)
	require.NoError(t, err)
	assert.False(t, got.OwnClear)
	label, err := svc.ReadLabel(t.Context(), res.Datasets[0].Handle, windowA, own)
	require.NoError(t, err)
	assert.False(t, label.OwnClear)
	_, err = svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA})
	assert.NoError(t, err, "the person exposes it")

	res = publishSales(t, svc, windowA, own, BundleProvenance{FromNothingSealed: true}, 2)
	assert.True(t, res.FromNothingSealed)
	_, err = svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA, OnBehalfOf: own})
	assert.NoError(t, err, "asserted, at TLP:CLEAR, by the task's own call")

	persons, err := svc.Publish(PublishInput{Alias: "persons", ArrowIPCStream: int64Stream(t, false, 9), By: windowB})
	require.NoError(t, err)
	res = publishSales(t, svc, windowA, own, BundleProvenance{FromNothingSealed: true, InputHandles: []string{persons.Handle}}, 3)
	assert.False(t, res.FromNothingSealed, "the service sees an input that is not the task's own and overrides the assertion")
	audits := svc.auditRecords()
	assert.Equal(t, reasonSealedInput, audits[len(audits)-1].Reason)

	// A publish no agent's call caused is nobody's own, asserted or not.
	res = publishSales(t, svc, windowA, nil, BundleProvenance{FromNothingSealed: true}, 4)
	got, err = svc.Read(t.Context(), "sales__orders", windowA, own)
	require.NoError(t, err)
	assert.False(t, got.OwnClear)
	audits = svc.auditRecords()
	assert.Empty(t, audits[len(audits)-2].Reason, "and its publish row carries no reason")
}
