package adhocdata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An app that reads a dataset a person published on a task's behalf,
// computes from it and publishes the outcome under a call of the same
// round declares the tier the conversation had before the read's outcome
// arrived: TLP:CLEAR. If it states nothing about what its rows derive from
// and names no inputs, the revision must not stand as the task's own clear
// work: the service handed the sealed bytes to that task itself.
func TestAPublisherThatStatesNothingAfterASealedReadIsNotClear(t *testing.T) {
	// The revision records "derives from sealed data" only when the
	// publisher says so or names a sealed input, and the field's zero
	// value means "derives from nothing sealed": a publisher that predates
	// the field, or forgets it, gets the relaxed answer. Closing it needs
	// one of two decisions. Either the service remembers that a task read
	// sealed data that is not its own and takes that task's later
	// publications as derived, which reverses the recorded choice that a
	// publisher stating nothing is judged by its call's tier; or the field
	// turns around, so a revision is clear only when its publisher asserts
	// it, which changes the wire field's meaning and every honest
	// publisher. Neither is a review's to make.
	t.Skip("open finding: a publisher that states nothing is taken as deriving from nothing sealed; needs a decision on the default")
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
	assert.False(t, back.OwnClear, "the person's rows, republished, read back as the task's own clear work")
	_, err = svc.Expose(t.Context(), ExposeInput{Alias: "summary__orders", By: windowA, OnBehalfOf: own})
	assert.Error(t, err, "and the task exposes them")
}
