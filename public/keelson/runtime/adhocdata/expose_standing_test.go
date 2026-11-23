package adhocdata

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// A task's exposure serves the task's own clear publication and nothing
// else: a republish under the alias by the person, or by another task, at
// the same columns and under the same owner, ends it rather than handing
// the new rows to whoever holds the task's token.
func TestATasksExposureEndsWhenTheRevisionIsNoLongerItsOwn(t *testing.T) {
	for _, tc := range []struct {
		name      string
		republish func(d *fakeDispatcher) *app.OnBehalfOf
	}{
		{name: "the person republishes", republish: func(*fakeDispatcher) *app.OnBehalfOf { return nil }},
		{name: "another task republishes", republish: func(d *fakeDispatcher) *app.OnBehalfOf { return d.as(taskTwo) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newExposeRig(t)
			d := &fakeDispatcher{call: agentCall(), grants: []string{"expose:sales"}}
			r.svc.SetDispatcher(d)
			own := oboOf(d.call)
			publishSales(t, r.svc, windowA, own, BundleProvenance{FromNothingSealed: true}, 1)
			exp, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA, OnBehalfOf: own})
			require.NoError(t, err)
			status, body, _ := fetch(t, http.MethodGet, exp.URL, exp.Token)
			require.Equal(t, http.StatusOK, status)
			require.Equal(t, []int64{1}, int64Values(t, body))

			publishSales(t, r.svc, windowA, tc.republish(d), BundleProvenance{FromNothingSealed: true}, 2)
			status, body, _ = fetch(t, http.MethodGet, exp.URL, exp.Token)
			assert.Equal(t, http.StatusNotFound, status, "the task's token reads no revision the task did not publish")
			assert.Equal(t, "not found\n", string(body), "and is told nothing of it")
			found := false
			for _, rec := range r.records() {
				found = found || (rec.Exposure == exp.Id && rec.Action == ExposureAuditLapse && rec.Reason == "a republish is not the task's own publication")
			}
			assert.True(t, found, "and the trail says why")
			list, err := r.svc.Exposures(t.Context(), windowA, nil)
			require.NoError(t, err)
			assert.Empty(t, list)
		})
	}
}

// The person's exposure is not a task's: it follows a republish under the
// alias whoever made it, as before.
func TestThePersonsExposureFollowsARepublishByATask(t *testing.T) {
	r := newExposeRig(t)
	d := &fakeDispatcher{call: agentCall()}
	r.svc.SetDispatcher(d)
	publishSales(t, r.svc, windowA, nil, BundleProvenance{FromNothingSealed: true}, 1)
	exp, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA})
	require.NoError(t, err)
	publishSales(t, r.svc, windowA, oboOf(d.call), BundleProvenance{FromNothingSealed: true}, 2)
	status, body, _ := fetch(t, http.MethodGet, exp.URL, exp.Token)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, []int64{2}, int64Values(t, body))
}

// Under a race between a reader holding a task's token and republishes by
// the person, no read is ever answered with rows the task did not publish:
// the revision judged is the revision opened.
func TestATasksTokenNeverReadsThePersonsRowsUnderARace(t *testing.T) {
	r := newExposeRig(t)
	d := &fakeDispatcher{call: agentCall(), grants: []string{"expose:sales"}}
	r.svc.SetDispatcher(d)
	own := oboOf(d.call)
	var served, ended int
	for i := range 60 {
		publishSales(t, r.svc, windowA, own, BundleProvenance{FromNothingSealed: true}, 1)
		exp, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA, OnBehalfOf: own})
		require.NoError(t, err)
		done := make(chan struct{})
		theirs := int64Stream(t, false, 2)
		go func() {
			defer close(done)
			if _, pErr := r.svc.PublishBundle(t.Context(), BundlePublishInput{Alias: "sales", Document: []byte(testDoc), By: windowA,
				Datasets: []BundleDatasetInput{{LocalName: "orders", ArrowIPCStream: theirs}}}); pErr != nil {
				t.Errorf("the person's republish: %v", pErr)
			}
		}()
		for range 4 {
			status, body, _ := fetch(t, http.MethodGet, exp.URL, exp.Token)
			switch status {
			case http.StatusOK:
				require.Equal(t, []int64{1}, int64Values(t, body), "round %d: the task's token read the person's rows", i)
				served++
			case http.StatusNotFound:
				ended++
			default:
				t.Fatalf("round %d: status %d", i, status)
			}
		}
		<-done
		status, _, _ := fetch(t, http.MethodGet, exp.URL, exp.Token)
		require.Equal(t, http.StatusNotFound, status, "round %d: after the person's republish the exposure is over", i)
	}
	t.Logf("%d reads of the task's revision, %d refused", served, ended)
}
