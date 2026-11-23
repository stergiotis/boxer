package adhocdata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// as makes the dispatcher attest a changed call from here on and returns
// its on-behalf-of context.
func (inst *fakeDispatcher) as(change func(cc *app.CallContext)) (obo *app.OnBehalfOf) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	change(&inst.call)
	return oboOf(inst.call)
}

func taskTwo(cc *app.CallContext) { cc.Task, cc.Call = "task-2", "task-2-1" }
func taskOne(cc *app.CallContext) { cc.Task, cc.Call = "task-1", "task-1-7" }

func publishSales(t *testing.T, svc *Service, by Identity, obo *app.OnBehalfOf, prov BundleProvenance, vals ...int64) (res BundleResult) {
	t.Helper()
	res, err := svc.PublishBundle(t.Context(), BundlePublishInput{Alias: "sales", Document: []byte(testDoc), By: by, OnBehalfOf: obo, Provenance: prov,
		Datasets: []BundleDatasetInput{{LocalName: "orders", ArrowIPCStream: int64Stream(t, false, vals...)}}})
	require.NoError(t, err)
	return
}

func TestStandingOf(t *testing.T) {
	clear := option.Some(app.CallContext{Task: "task-1", Tier: uint8(agentrun.TierClear)})
	amber := option.Some(app.CallContext{Task: "task-1", Tier: uint8(agentrun.TierAmber)})
	green := option.Some(app.CallContext{Task: "task-1", Tier: uint8(agentrun.TierGreen)})
	assert.Equal(t, standingOwnClear, standingOf(clear, false, "task-1"))
	assert.Equal(t, standingDerived, standingOf(clear, true, "task-1"))
	assert.Equal(t, standingLabelled, standingOf(amber, false, "task-1"))
	assert.Equal(t, standingLabelled, standingOf(green, false, "task-1"), "every tier above CLEAR is a label")
	assert.Equal(t, standingLabelled, standingOf(amber, true, "task-1"))
	assert.Equal(t, standingOthers, standingOf(clear, false, "task-2"), "another task's")
	assert.Equal(t, standingOthers, standingOf(option.None[app.CallContext](), false, "task-1"), "a person's, or an app's own")
	assert.Equal(t, standingOthers, standingOf(clear, false, ""), "no task owns anything")
	assert.Equal(t, standingOthers, standingOf(option.Some(app.CallContext{}), false, ""), "an empty task matches no publisher")
	for _, s := range allStandings {
		assert.NotEmpty(t, s.String())
	}
}

// A publication is the task's own clear work only when its publisher
// asserted that its rows derive from nothing sealed and no input the
// service resolves says otherwise, whatever tier its call declared: the
// task may not expose any other, and a republish that is not asserted ends
// an exposure the task made.
func TestATaskDoesNotExposeWhatDerivesFromSealedData(t *testing.T) {
	r := newExposeRig(t)
	svc := r.svc
	d := &fakeDispatcher{call: agentCall(), grants: []string{"expose:sales", "expose:summary", "keelson:persons"}}
	svc.SetDispatcher(d)
	own := oboOf(d.call)

	res := publishSales(t, svc, windowA, own, BundleProvenance{}, 1)
	assert.False(t, res.FromNothingSealed)
	audits := svc.auditRecords()
	assert.Equal(t, reasonNotStated, audits[len(audits)-1].Reason, "the publish row says so")
	_, err := svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA, OnBehalfOf: own})
	require.Error(t, err, "the same-round publish of a confined result is not the task's to expose")
	assert.ErrorContains(t, err, "not known to derive from nothing sealed")
	assert.ErrorContains(t, err, "the person exposes the rest")
	_, err = svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA})
	assert.NoError(t, err, "the person exposes it")

	clean := publishSales(t, svc, windowA, own, BundleProvenance{FromNothingSealed: true}, 2)
	assert.True(t, clean.FromNothingSealed)
	audits = svc.auditRecords()
	assert.Empty(t, audits[len(audits)-1].Reason)
	exp, err := svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA, OnBehalfOf: own})
	require.NoError(t, err)

	persons, err := svc.Publish(PublishInput{Alias: "persons", ArrowIPCStream: int64Stream(t, false, 9), By: windowB})
	require.NoError(t, err)
	summary := func(inputs ...string) (out BundleResult) {
		out, err = svc.PublishBundle(t.Context(), BundlePublishInput{Alias: "summary", Document: []byte(testDoc), By: windowA, OnBehalfOf: own,
			Datasets:   []BundleDatasetInput{{LocalName: "orders", ArrowIPCStream: int64Stream(t, false, 6)}},
			Provenance: BundleProvenance{InputHandles: inputs, FromNothingSealed: true}})
		require.NoError(t, err)
		return
	}
	assert.True(t, summary(clean.Datasets[0].Handle).FromNothingSealed, "an input that is the task's own clear publication seals nothing")
	_, err = svc.Expose(t.Context(), ExposeInput{Alias: "summary__orders", By: windowA, OnBehalfOf: own})
	assert.NoError(t, err)
	assert.False(t, summary(clean.Datasets[0].Handle, persons.Handle).FromNothingSealed, "an input a person published is sealed data, whatever the publisher asserted")
	_, err = svc.Expose(t.Context(), ExposeInput{Alias: "summary__orders", By: windowA, OnBehalfOf: own})
	assert.ErrorContains(t, err, "not known to derive from nothing sealed")
	assert.False(t, summary("adhoc_00000000000000ff").FromNothingSealed, "an input the service does not hold is not known to be the task's")
	assert.False(t, summary(res.Datasets[0].Handle).FromNothingSealed, "an input that itself derived from sealed data carries it on")

	publishSales(t, svc, windowA, own, BundleProvenance{}, 3)
	status, _, _ := fetch(t, "GET", exp.URL, exp.Token)
	assert.Equal(t, 404, status, "a republish not asserted to derive from nothing sealed ends the task's exposure")
	found := false
	for _, rec := range r.records() {
		found = found || (rec.Exposure == exp.Id && rec.Reason == "a republish is not known to derive from nothing sealed")
	}
	assert.True(t, found, "and the trail says why")
}

// A refusal for a label says what still leaves the label off and who
// exposes instead: the task has no call that lowers it.
func TestALabelRefusalSaysWhoExposesInstead(t *testing.T) {
	r := newExposeRig(t)
	d := &fakeDispatcher{call: agentCall(), grants: []string{"expose:sales"}}
	r.svc.SetDispatcher(d)
	publishSales(t, r.svc, windowA, oboOf(d.call), BundleProvenance{FromNothingSealed: true}, 1)

	amber := d.as(func(cc *app.CallContext) { cc.Tier = uint8(agentrun.TierAmber) })
	_, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA, OnBehalfOf: amber})
	require.Error(t, err)
	assert.ErrorContains(t, err, "the call is labelled AMBER")
	assert.ErrorContains(t, err, "Reading back what the task itself published at TLP:CLEAR leaves a run there")
	assert.ErrorContains(t, err, "the person exposes the rest, from play's Data sets tab")

	publishSales(t, r.svc, windowA, amber, BundleProvenance{FromNothingSealed: true}, 2)
	clear := d.as(func(cc *app.CallContext) { cc.Tier = uint8(agentrun.TierClear) })
	_, err = r.svc.Expose(t.Context(), ExposeInput{Alias: "sales__orders", By: windowA, OnBehalfOf: clear})
	require.Error(t, err)
	assert.ErrorContains(t, err, "was published by a call labelled AMBER")
	assert.ErrorContains(t, err, "the person exposes the rest, from play's Data sets tab")
}
