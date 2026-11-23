package play

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspecthttp"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/sessiondata"
)

// ownClearRig is one play window driven through the agent dispatcher, with
// the dataset service and the session data service of a host behind it:
// both ask the dispatcher itself who a call is, so the tier a call
// declares is the tier its publish is recorded at. A stub stands for the
// ClickHouse server and another for this process's introspection plane.
// The rig keeps the conversation's label as a coordinator does: a call
// declares the tier the conversation had when its round began, and an
// outcome labelled confined raises it for the rounds after.
type ownClearRig struct {
	*dispatchRig
	t      *testing.T
	adhoc  *adhocdata.Service
	server *sdStub
	plane  *sdStub
	win    app.BusI
	person app.BusI
	cl     *Client
	p      *PlayApp
	tier   agentrun.TierE
	n      int
	// lastKey is the key of the last call made.
	lastKey string
}

// outcome is a call's end and the label a coordinator reads it under.
type outcome struct {
	agent.Outcome
	Text     string
	Confined bool
}

func newOwnClearRig(t *testing.T) (r *ownClearRig) {
	t.Helper()
	fakeAppletDocs(t)
	ctx := t.Context()
	logger := zerolog.Nop()
	reg := introspect.NewRegistry()
	// The host's predicate: every ad-hoc dataset is sealed.
	introspect.SetLocalSealedPredicate(reg.IsSealed)
	t.Cleanup(func() { introspect.SetLocalSealedPredicate(nil) })
	bus := inprocbus.NewInst(logger)
	adhoc, err := adhocdata.NewService(adhocdata.Config{Bus: bus, Registry: reg, Dir: t.TempDir(), Log: logger})
	require.NoError(t, err)
	listener := introspecthttp.New(introspecthttp.Config{Registry: reg}, logger)
	require.NoError(t, listener.Start())
	sd, err := sessiondata.NewService(sessiondata.Config{Bus: bus, Log: logger, SweepInterval: -1, Adhoc: adhoc})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sd.Close(context.Background())
		_ = adhoc.Close(context.Background())
		_ = listener.Stop(context.Background())
	})
	r = &ownClearRig{t: t, adhoc: adhoc, server: newSdStub(t), plane: newSdStub(t), tier: agentrun.TierClear}
	withQueryEndpoint(t, r.plane.url())

	m := (&PlayLauncher{}).Manifest()
	caps := []app.SubjectFilter{
		{Pattern: "runtime.adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"},
		{Pattern: sessiondata.SubjectPrefix + ">", Direction: app.CapDirectionBoth, Reason: "test"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionSub, Reason: "replies"},
	}
	// The window the dispatcher routes the task's calls to: the services
	// attest a request by the app and instance it came from.
	win := bus.NewClient(m.Id, caps)
	win.SetInstanceKey(2)
	r.win, r.person = win, bus.NewClient("test.person", caps)
	r.cl = NewClient(ClientConfig{URL: r.server.url(), User: "u", Password: "p"}, nil)
	r.cl.SetSessionBus(win)
	r.cl.SetResolver(autoResolver)
	r.cl.reach.record(r.server.url())
	r.p = NewPlayApp(r.cl, newLiveQueryGraph(r.cl, memory.NewGoAllocator(), 8), "SELECT 1", nil)
	t.Cleanup(r.p.graph.close)
	r.p.bus = win
	l := &PlayLauncher{inner: r.p}
	l.bus, l.log, l.ioCtx = win, logger, ctx

	host := endpointHost(r.server.url())
	r.dispatchRig = newDispatchRigFor(t, l, l.Operations(), []string{DestinationClickHouse(host), sessiondata.DestinationSession(host),
		adhocdata.DestinationExpose("final"), adhocdata.DestinationExpose("leak"), adhocdata.DestinationExpose("people"),
		adhocdata.DestinationBundle("people"), adhocdata.DestinationBundle("kernel_top")},
		// A local model route: a confined result reaches the coordinator
		// with its label, as it did in the conversation this is about,
		// instead of being withheld behind a data handle.
		func(cfg *agent.Config) { cfg.ModelLocal = func() bool { return true } })
	// The window's frame: the follower binds the aliases the task's
	// queries name, then play's own frame runs.
	frame := playFrame(r.p)
	r.host.body = func() {
		if l.follower != nil {
			l.follower.Sync(l.inner)
		}
		frame()
	}
	t.Cleanup(func() {
		if l.follower != nil {
			l.follower.Close()
		}
	})
	adhoc.SetDispatcher(r.svc)
	sd.SetDispatcher(r.svc)
	// The task reads the window first, as a model's get_state does.
	out := r.call(opGetState, `{}`)
	require.Equal(t, "completed", out.Phase, out.Reason)
	return
}

// call makes one call of the task at the tier the conversation holds,
// follows it to its end and reads its result as a coordinator does. It
// does not raise the conversation's label: endRound does, so the calls of
// one round all declare the tier the round began with.
func (inst *ownClearRig) call(op string, args string) (out outcome) {
	inst.t.Helper()
	inst.n++
	key := "k" + strconv.Itoa(inst.n)
	inst.lastKey = key
	got, err := inst.cli.Call(inst.t.Context(), agent.CallRequest{Handle: inst.g.Handle, Instance: inst.host.key, Operation: op, Args: args,
		Key: key, Turn: "turn-1", Tier: uint8(inst.tier)})
	require.NoError(inst.t, err)
	require.Eventually(inst.t, func() bool {
		if got.Final() {
			return true
		}
		inst.host.frame()
		got, err = inst.cli.Status(inst.t.Context(), inst.g.Handle, key, 0)
		require.NoError(inst.t, err)
		return got.Final()
	}, 10*time.Second, 2*time.Millisecond, "%s did not end: %s %s", op, got.Phase, got.Reason)
	// The host keeps drawing: the frames after a call are what the task's
	// next read of the window sees.
	for range 3 {
		inst.host.frame()
	}
	out.Outcome, out.Confined = got, got.Confined
	if got.ResultRef != "" {
		res, rErr := inst.cli.Read(inst.t.Context(), inst.g.Handle, got.ResultRef)
		require.NoError(inst.t, rErr)
		out.Text, out.Confined = res.Text, out.Confined || res.Confined
	}
	return
}

// endRound raises the conversation's label by what the round's outcomes
// carried: the calls of the next round declare it.
func (inst *ownClearRig) endRound(outs ...outcome) {
	for _, o := range outs {
		if o.Confined {
			inst.tier = agentrun.TierAmber
		}
	}
}

// round is one call as a round of its own.
func (inst *ownClearRig) round(op string, args string) (out outcome) {
	inst.t.Helper()
	out = inst.call(op, args)
	inst.endRound(out)
	return
}

// appPublishes publishes bundle with a three-row dataset result as an app
// does on the task's behalf: under a call of the task routed to the
// window, at the tier the conversation holds, asserting that the rows it
// computed derive from nothing sealed. It returns the dataset's handle; the
// task's query binds the alias in the window.
func (inst *ownClearRig) appPublishes(bundle string) (handle string) {
	inst.t.Helper()
	return inst.publish(inst.win, bundle, inst.attestedCall(), true)
}

// appPublishesCarelessly is appPublishes by an app that says nothing about
// what its rows derive from.
func (inst *ownClearRig) appPublishesCarelessly(bundle string) (handle string) {
	inst.t.Helper()
	return inst.publish(inst.win, bundle, inst.attestedCall(), false)
}

// attestedCall makes a call of the task to the window, at the tier the
// conversation holds, and returns the context work done under it carries:
// the dispatcher attests it as the task's, at that tier.
func (inst *ownClearRig) attestedCall() (obo *app.OnBehalfOf) {
	inst.t.Helper()
	out := inst.call(opGetState, `{}`)
	require.Equal(inst.t, "completed", out.Phase, out.Reason)
	for _, a := range inst.svc.Actions() {
		if a.Key == inst.lastKey && a.CallId != "" {
			return &app.OnBehalfOf{Task: inst.g.Task, Epoch: a.Epoch, Call: a.CallId, Destinations: inst.obo.Destinations}
		}
	}
	inst.t.Fatal("the dispatcher recorded no call")
	return
}

// publishResult is the task's publish_result of the window's main result
// under bundle. A consequential command waits for the person under a test
// grant, so the command is handed to the window's engine as the dispatcher
// would hand it, under a call of the task the dispatcher attests at the
// tier the conversation holds.
func (inst *ownClearRig) publishResult(bundle string) (phase string, reason string) {
	inst.t.Helper()
	obo := inst.attestedCall()
	inst.n++
	id := "direct-" + strconv.Itoa(inst.n)
	out := inst.host.eng.Submit(inst.t.Context(), opPublishResult, opwire.CallRequest{CallId: id, Writer: opwire.WriterTask(inst.g.Task), Key: id,
		Args: mustEncode(inst.t, PublishResultArgs{Bundle: bundle}), OnBehalfOf: obo})
	require.Eventually(inst.t, func() bool {
		if out.Phase.Final() {
			return true
		}
		inst.host.frame()
		out, _ = inst.host.eng.Status(id)
		return out.Phase.Final()
	}, 10*time.Second, 2*time.Millisecond, "publish_result did not end: %s %s", out.Phase, out.Reason)
	for range 3 {
		inst.host.frame()
	}
	return out.Phase.String(), out.Reason
}

// field is the value at path in a result's model JSON.
func field(t *testing.T, text string, path ...string) (v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal([]byte(text), &v), text)
	for _, k := range path {
		m, ok := v.(map[string]any)
		require.True(t, ok, "%s in %s", k, text)
		v = m[k]
	}
	return
}

// listed is the dataset name in a list_datasets answer.
func listed(t *testing.T, text string, name string) (d map[string]any) {
	t.Helper()
	all, _ := field(t, text, "datasets").([]any)
	for _, e := range all {
		if m, ok := e.(map[string]any); ok && m["name"] == name {
			return m
		}
	}
	t.Fatalf("no dataset %s in %s", name, text)
	return
}

// personPublishes publishes bundle as a person's window does.
func (inst *ownClearRig) personPublishes(bundle string) (handle string) {
	inst.t.Helper()
	return inst.publish(inst.person, bundle, nil, false)
}

func (inst *ownClearRig) publish(by app.BusI, bundle string, obo *app.OnBehalfOf, fromNothingSealed bool) (handle string) {
	inst.t.Helper()
	res, err := adhocdata.PublishBundleRequest(inst.t.Context(), by, adhocdata.BundlePublishInput{Alias: bundle, Document: []byte("SELECT 1"),
		OnBehalfOf: obo, Datasets: []adhocdata.BundleDatasetInput{{LocalName: "result", ArrowIPCStream: arrowRows()}},
		Provenance: adhocdata.BundleProvenance{FromNothingSealed: fromNothingSealed}})
	require.NoError(inst.t, err)
	return res.Datasets[0].Handle
}

// reread is the task reading the window again after a change that was not
// its own — a republish the window's follower bound — as a turn's note
// asks it to.
func (inst *ownClearRig) reread() {
	inst.t.Helper()
	for range 3 {
		inst.host.frame()
	}
	_, err := inst.cli.Turn(inst.t.Context(), inst.g.Handle)
	require.NoError(inst.t, err)
	out := inst.call(opGetState, `{}`)
	require.Equal(inst.t, "completed", out.Phase, out.Reason)
}

func jsonArgs(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// The motivating sequence, through the dispatcher: an app publishes a table
// on the task's behalf at TLP:CLEAR, the task reads it in play, publishes
// the play result and exposes it. No step is refused and none labels the
// conversation, though the statement is placed and sent as one that reads
// sealed data.
func TestATaskReadsPublishesAndExposesItsOwnClearWork(t *testing.T) {
	r := newOwnClearRig(t)
	handle := r.appPublishes("kernel_top")
	stmt := "SELECT x FROM keelson('kernel_top__result')"

	out := r.round(opQuery, jsonArgs(t, map[string]any{"sql": stmt}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.False(t, out.Confined, "the read of the task's own clear publication leaves the outcome unlabelled")
	assert.Equal(t, agentrun.TierClear, r.tier)
	// The wall saw sealed data all the same: the decision is confined, the
	// run went to the plane and the window remembers it read sealed data.
	dec := r.cl.Dispatch(stmt, "")
	assert.True(t, dec.Confined(), "the statement names sealed data")
	assert.Equal(t, "introspection", dec.Class())
	assert.Contains(t, r.plane.lastRun().SQL, handle, "the run reads the dataset by its handle on this process's plane")
	assert.True(t, r.p.graph.MainSealed())
	assert.False(t, r.p.graph.MainConfined())
	assert.False(t, r.p.windowConfined())
	hist := r.p.graph.MainHistory()
	assert.True(t, hist[len(hist)-1].Confined, "the person's history shows the run read sealed data")
	// What play itself writes of such a result elsewhere is walled as
	// before: the verdicts' writer asks whether the result is of sealed
	// data, and an endpoint that may not see sealed data is refused it.
	require.NotNil(t, r.p.seriesLabels.confined)
	assert.True(t, r.p.seriesLabels.confined())
	elsewhere := NewClient(ClientConfig{URL: "http://ch.example:8123/"}, nil)
	_, err := elsewhere.appStatement(t.Context(), tsLabelsDDL, nil, appWriteLabel{confined: r.p.seriesLabels.confined()})
	assert.ErrorContains(t, err, "not declared as allowed to see sealed data")

	// An assertion over the result, as a citation's check makes it, reads
	// the same dataset and is not labelled either.
	chk := r.round(opCheck, jsonArgs(t, map[string]any{"result_id": field(t, out.Text, "result_id"), "statement": "SELECT count() FROM keelson('kernel_top__result')"}))
	require.Equal(t, "completed", chk.Phase, chk.Reason)
	assert.False(t, chk.Confined)
	assert.Equal(t, false, field(t, chk.Text, "confined"), "the check's own word, which a coordinator reads beside the outcome's label")

	phase, reason := r.publishResult("final")
	require.Equal(t, "completed", phase, reason)
	pub, err := adhocdata.ResolveBundleRequest(t.Context(), r.win, "final", nil)
	require.NoError(t, err)
	assert.Equal(t, r.g.Task, pub.PublisherTask)

	out = r.round(opExposeDataset, jsonArgs(t, map[string]any{"name": "final__result"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.False(t, out.Confined)
	assert.Equal(t, agentrun.TierClear, r.tier, "the conversation ends as it began")
	url, _ := field(t, out.Text, "url").(string)
	token, _ := field(t, out.Text, "token").(string)
	status, body := getWithToken(t, url, token)
	assert.Equal(t, 200, status)
	assert.NotEmpty(t, body)

	// Reading the published result back is a read of the task's own work
	// too: the publish named the dataset it read, which the service found
	// to be the task's own.
	out = r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('final__result')"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.False(t, out.Confined)
}

// The same sequence over a dataset a person published is labelled at the
// read and refused at the exposure, as before: the conversation's label
// rises with the read, the result is published as derived from sealed
// data, and the exposing call is refused for its label.
func TestATaskReadingAPersonsDatasetIsLabelledAndNotExposed(t *testing.T) {
	r := newOwnClearRig(t)
	r.personPublishes("people")

	out := r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('people__result')"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.True(t, out.Confined, "a dataset a person published is sealed data to the task")
	assert.Equal(t, agentrun.TierAmber, r.tier, "and labels the conversation")
	assert.True(t, r.p.graph.MainConfined())
	chk := r.round(opCheck, jsonArgs(t, map[string]any{"result_id": field(t, out.Text, "result_id"), "statement": "SELECT count() FROM keelson('people__result')"}))
	require.Equal(t, "completed", chk.Phase, chk.Reason)
	assert.True(t, chk.Confined)
	assert.Equal(t, true, field(t, chk.Text, "confined"))

	phase, reason := r.publishResult("final")
	require.Equal(t, "completed", phase, reason)
	out = r.round(opExposeDataset, jsonArgs(t, map[string]any{"name": "final__result"}))
	require.Equal(t, "refused", out.Phase)
	assert.Contains(t, out.Reason, "labelled AMBER")
	assert.Contains(t, out.Reason, "the person exposes the rest")

	// The person's own dataset is not the task's to expose either.
	out = r.round(opExposeDataset, jsonArgs(t, map[string]any{"name": "people__result"}))
	require.Equal(t, "refused", out.Phase)
	assert.Contains(t, out.Reason, "was not published by this task's call")
}

// A model asks for the query, the publish of its result and the exposure
// in one round: every call declares the tier the conversation had before
// the query's outcome arrived. The published result carries its own label,
// so it is not the task's clear work whatever tier the publish declared.
func TestAResultPublishedInTheRoundItArrivedInCarriesItsLabel(t *testing.T) {
	r := newOwnClearRig(t)
	r.personPublishes("people")

	q := r.call(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('people__result')"}))
	require.Equal(t, "completed", q.Phase, q.Reason)
	require.True(t, q.Confined)
	require.Equal(t, agentrun.TierClear, r.tier, "the round's calls all declare the tier it began with")
	phase, reason := r.publishResult("leak")
	require.Equal(t, "completed", phase, reason)
	pub, err := adhocdata.ResolveBundleRequest(t.Context(), r.win, "leak", r.attestedCall())
	require.NoError(t, err)
	require.Equal(t, r.g.Task, pub.PublisherTask, "the publish is the task's own, declared at TLP:CLEAR")
	exp := r.call(opExposeDataset, jsonArgs(t, map[string]any{"name": "leak__result"}))
	require.Equal(t, "refused", exp.Phase, "a result of sealed data published at TLP:CLEAR is not exposed by the task")
	assert.Contains(t, exp.Reason, "not known to derive from nothing sealed")
	r.endRound(q, exp)

	// Read back, it labels as any sealed data does, and stays unexposed.
	r.tier = agentrun.TierClear
	out := r.call(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('leak__result')"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.True(t, out.Confined, "a publication derived from sealed data does not read back as the task's own")
}

// The wall is the same for a task's own publication: joined with a server
// table it is attached only on an endpoint that has shown it may see sealed
// data, and sent as confined; only the label of the result differs. One
// input that is not the task's own labels the statement.
func TestTheWallIsTheSameForATasksOwnPublication(t *testing.T) {
	r := newOwnClearRig(t)
	r.appPublishes("kernel_top")
	r.personPublishes("people")
	joined := "SELECT count() FROM t JOIN keelson('kernel_top__result') AS k USING x"

	out := r.round(opQuery, jsonArgs(t, map[string]any{"sql": joined}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.False(t, out.Confined, "attached as the task's own clear publication")
	run := r.server.lastRun()
	assert.Equal(t, []string{"__adhoc_0"}, run.Files, "the dataset went to the server as external data")
	assert.True(t, r.p.graph.MainSealed(), "and the run is one that read sealed data")
	assert.True(t, r.cl.Dispatch(joined, "").Confined())

	// An endpoint that has not shown it may see sealed data is refused the
	// statement, whoever published the dataset.
	unproven := NewClient(ClientConfig{URL: r.server.url(), User: "u", Password: "p"}, nil)
	unproven.SetSessionBus(r.win)
	res, err := adhocdata.ResolveRequest(t.Context(), r.win, "kernel_top__result")
	require.NoError(t, err)
	unproven.bindDataset("kernel_top__result", res.Handle)
	dec := unproven.Dispatch(joined, "")
	require.True(t, dec.Refused(), dec.Reason())
	assert.Contains(t, dec.Reason(), "has not demonstrated it may see sealed data")
	_, err = unproven.prepareSession(t.Context(), joined, &ExecOptions{Agent: r.attestedCall()}, dispatchDecision{targetURL: r.server.url(), class: dispatchClassSession})
	require.Error(t, err, "and the session data service refuses the task's call too, before it reads the dataset")
	assert.Contains(t, err.Error(), "not declared as allowed to see sealed data")

	mixed := "SELECT count() FROM t JOIN keelson('kernel_top__result') AS k USING x JOIN keelson('people__result') AS p USING x"
	out = r.round(opQuery, jsonArgs(t, map[string]any{"sql": mixed}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.True(t, out.Confined, "a dataset a person published beside the task's own labels the statement")
	assert.Equal(t, agentrun.TierAmber, r.tier)
}

// On this process's plane, too, one sealed input that is not the task's own
// labels the statement.
func TestAMixedStatementOnThePlaneIsLabelled(t *testing.T) {
	r := newOwnClearRig(t)
	r.appPublishes("kernel_top")
	r.personPublishes("people")
	out := r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT k.x FROM keelson('kernel_top__result') AS k JOIN keelson('people__result') AS p USING x"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.True(t, out.Confined)
	assert.Equal(t, agentrun.TierAmber, r.tier)
}

// What a task published above TLP:CLEAR, what another publisher replaced
// since, and what a person reads are labelled as before.
func TestOnlyTheTasksOwnClearRevisionReadsBackUnlabelled(t *testing.T) {
	r := newOwnClearRig(t)
	stmt := jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('kernel_top__result')"})

	// Published at AMBER: read from a conversation that is at TLP:CLEAR
	// again, as a new run of the task would be.
	r.tier = agentrun.TierAmber
	r.appPublishes("kernel_top")
	r.tier = agentrun.TierClear
	out := r.call(opQuery, stmt)
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.True(t, out.Confined, "published by the task's call under a label above TLP:CLEAR")

	// Published again at TLP:CLEAR, the live revision is the task's own.
	r.appPublishes("kernel_top")
	r.reread()
	out = r.call(opQuery, stmt)
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.False(t, out.Confined)

	// The person republishes under the same alias, and the window's
	// follower binds the new revision: the same statement is labelled from
	// then on. The alias has one owner, so the republish comes from the
	// publishing window without a call of the task.
	r.publish(r.win, "kernel_top", nil, false)
	r.reread()
	out = r.call(opQuery, stmt)
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.True(t, out.Confined, "republished by someone else since")

	// A run that is no call of the task reads the task's publication as
	// sealed data: a client of the person's, with no task behind it.
	handle := r.appPublishes("kernel_top")
	persons := NewClient(ClientConfig{URL: r.server.url(), User: "u", Password: "p"}, nil)
	persons.SetSessionBus(r.win)
	persons.SetResolver(autoResolver)
	persons.bindDataset("kernel_top__result", handle)
	rdr, rs, _, err := persons.ExecuteArrowStream(t.Context(), "SELECT x FROM keelson('kernel_top__result')", memory.NewGoAllocator(), nil, nil,
		persons.Dispatch("SELECT x FROM keelson('kernel_top__result')", ""))
	require.NoError(t, err)
	assert.True(t, rs.Sealed())
	assert.True(t, rs.Confined(), "the person's run keeps the label")
	rdr.Release()
	require.NoError(t, rs.Close())
}

// A run that fails names what it would have read: over the task's own
// clear publication its failure is not labelled, over a person's dataset
// it is.
func TestAFailedRunOverTheTasksOwnPublicationIsNotLabelled(t *testing.T) {
	r := newOwnClearRig(t)
	r.appPublishes("kernel_top")
	r.personPublishes("people")
	r.plane.mu.Lock()
	r.plane.failRuns = true
	r.plane.mu.Unlock()

	out := r.call(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT nope FROM keelson('kernel_top__result')"}))
	require.Equal(t, "failed", field(t, out.Text, "result", "phase"), out.Text)
	assert.Contains(t, field(t, out.Text, "result", "error"), "UNKNOWN_TABLE")
	assert.False(t, out.Confined, "the failure quotes nothing but the task's own work")
	assert.False(t, r.p.windowConfined())
	assert.True(t, r.p.graph.MainSealed())

	out = r.call(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT nope FROM keelson('people__result')"}))
	require.Equal(t, "failed", field(t, out.Text, "result", "phase"), out.Text)
	assert.True(t, out.Confined, "a failed run over a person's dataset keeps the statement's label")
}

// A data set the task keeps from its own clear publication, the sample run
// over it and the listings that describe it carry no label; kept from a
// person's dataset they do.
func TestAKeptDataSetOfTheTasksOwnPublicationIsNotLabelled(t *testing.T) {
	r := newOwnClearRig(t)
	r.appPublishes("kernel_top")
	r.personPublishes("people")

	out := r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('kernel_top__result')", "keep_as": "mine"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.False(t, out.Confined, "the keep and its sample run read only the task's own work")
	assert.Equal(t, false, field(t, out.Text, "kept", "confined"), "the answer says reading it does not confine this task")
	assert.Contains(t, r.server.lastRun().SQL, "__sd_mine_r1", "the sample reads the kept table")

	out = r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT count() FROM session('mine')"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.False(t, out.Confined)
	assert.True(t, r.p.graph.MainSealed(), "the wall still holds the data set as derived from sealed data")

	// The window binds both ad-hoc datasets once the task read them.
	out = r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('kernel_top__result')"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	require.False(t, out.Confined)
	out = r.call(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('people__result')"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	require.True(t, out.Confined)
	out = r.call(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT 1"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	require.False(t, out.Confined, "the window holds an ordinary result again")

	out = r.round(opListDatasets, `{}`)
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.False(t, out.Confined)
	own := listed(t, out.Text, "kernel_top__result")
	assert.Equal(t, false, own["confined"], "the listing says reading it does not confine this task")
	assert.NotEmpty(t, own["columns"])
	assert.Equal(t, false, listed(t, out.Text, "mine")["confined"])
	assert.NotEmpty(t, listed(t, out.Text, "mine")["columns"])
	theirs := listed(t, out.Text, "people__result")
	assert.Equal(t, true, theirs["confined"], "a person's dataset is listed as confining")
	assert.Empty(t, theirs["columns"], "and its columns are left out of the listing")
	assert.Equal(t, agentrun.TierClear, r.tier)

	out = r.round(opListDatasets, `{"name":"kernel_top__result"}`)
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.False(t, out.Confined, "asked for by name, the task's own dataset shows its columns unlabelled")
	assert.NotEmpty(t, listed(t, out.Text, "kernel_top__result")["columns"])

	out = r.round(opListDatasets, `{"name":"people__result"}`)
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.True(t, out.Confined, "asked for by name, a person's dataset shows its columns and labels the answer")
	r.tier = agentrun.TierClear

	out = r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('people__result')", "keep_as": "theirs"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.True(t, out.Confined)
	assert.Equal(t, true, field(t, out.Text, "kept", "confined"))

	// A result of a confined data set, published, carries that on: the
	// publish names no ad-hoc dataset as its input, so only the result's
	// own label says so.
	r.tier = agentrun.TierClear
	q := r.call(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT count() FROM session('theirs')"}))
	require.Equal(t, "completed", q.Phase, q.Reason)
	require.True(t, q.Confined)
	phase, reason := r.publishResult("leak")
	require.Equal(t, "completed", phase, reason)
	exp := r.call(opExposeDataset, jsonArgs(t, map[string]any{"name": "leak__result"}))
	require.Equal(t, "refused", exp.Phase)
	assert.Contains(t, exp.Reason, "not known to derive from nothing sealed")
}

// A lane's run and an explain go through the same labelled execution as
// the main run: over the task's own clear publication their result is not
// labelled, over a person's dataset it is, and a failure is judged alike.
func TestALaneOverTheTasksOwnPublicationIsNotLabelled(t *testing.T) {
	r := newOwnClearRig(t)
	r.appPublishes("kernel_top")
	r.personPublishes("people")
	// The task's queries bind both datasets in the window.
	out := r.call(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('kernel_top__result')"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	out = r.call(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('people__result')"}))
	require.Equal(t, "completed", out.Phase, out.Reason)

	ex := clientExecutor{client: r.cl, opts: &ExecOptions{Agent: r.attestedCall()}}
	run := func(sql string) (confined bool, err error) {
		rec, _, _, confined, err := ex.executeLabelled(t.Context(), compiledNode{SQL: sql}, memory.NewGoAllocator(), nil)
		if rec != nil {
			rec.Release()
		}
		return
	}
	confined, err := run("SELECT x FROM keelson('kernel_top__result')")
	require.NoError(t, err)
	assert.False(t, confined)
	confined, err = run("SELECT x FROM keelson('people__result')")
	require.NoError(t, err)
	assert.True(t, confined)

	r.plane.mu.Lock()
	r.plane.failRuns = true
	r.plane.mu.Unlock()
	confined, err = run("SELECT x FROM keelson('kernel_top__result')")
	require.Error(t, err)
	assert.False(t, confined, "a failed lane over the task's own work")
	confined, err = run("SELECT x FROM keelson('people__result')")
	require.Error(t, err)
	assert.True(t, confined)
}

// The label is lifted only where the services answered for everything
// sealed the run read: every case play cannot tell keeps it.
func TestTheLabelStaysWhereverPlayCannotTell(t *testing.T) {
	r := newOwnClearRig(t)
	handle := r.appPublishes("kernel_top")
	theirs := r.personPublishes("people")
	obo := r.attestedCall()
	ctx := t.Context()
	confined, ordinary := queryengine.SensitivityConfined, queryengine.SensitivityOrdinary
	read := func(names ...string) (sql string) {
		sql = "SELECT 1 FROM keelson('" + names[0] + "') AS t0"
		for i, n := range names[1:] {
			sql += " JOIN keelson('" + n + "') AS t" + strconv.Itoa(i+1) + " USING x"
		}
		return
	}

	assert.True(t, r.cl.ownClearRun(ctx, confined, read(handle), obo, nil), "the task's own clear publication, by its handle")
	assert.False(t, r.cl.ownClearRun(ctx, ordinary, read(handle), obo, nil), "a run that read nothing sealed has no label to lift")
	assert.False(t, r.cl.ownClearRun(ctx, confined, read(handle), nil, nil), "the person's run")
	assert.False(t, r.cl.ownClearRun(ctx, confined, read(handle), &app.OnBehalfOf{Destinations: obo.Destinations}, nil), "a gate without a task")
	assert.False(t, r.cl.ownClearRun(ctx, confined, read(handle, theirs), obo, nil), "one dataset that is not the task's")
	assert.False(t, r.cl.ownClearRun(ctx, confined, "SELECT 1", obo, nil), "confined for a reason the statement does not show")
	forged := *obo
	forged.Call = "no-such-call"
	assert.False(t, r.cl.ownClearRun(ctx, confined, read(handle), &forged, nil), "a call the dispatcher does not attest")
	other := *obo
	other.Task = "another-task"
	assert.False(t, r.cl.ownClearRun(ctx, confined, read(handle), &other, nil), "another task's name on the call")

	// Sealed data that is no ad-hoc dataset: an app's sealed table on the
	// plane is nobody's publication.
	introspect.SetLocalSealedPredicate(func(name string) bool { return name == "vault" || name == handle })
	assert.False(t, r.cl.ownClearRun(ctx, confined, read(handle, "vault"), obo, nil))
	assert.True(t, r.cl.ownClearRun(ctx, confined, read(handle), obo, nil))

	// A client that cannot ask the dataset service.
	deaf := NewClient(ClientConfig{URL: r.server.url()}, nil)
	assert.False(t, deaf.ownClearRun(ctx, confined, read(handle), obo, nil))

	// In a data session the binding answers; a sealed name it did not
	// replace is one the service did not judge.
	own := &sessionHold{b: sessiondata.Binding{Sensitivity: confined, OwnClear: true}}
	assert.True(t, r.cl.ownClearRun(ctx, confined, "SELECT 1 FROM `__adhoc_0`", obo, own))
	assert.False(t, r.cl.ownClearRun(ctx, confined, "SELECT 1 FROM `__adhoc_0` JOIN vault USING x", obo, own))
	assert.False(t, r.cl.ownClearRun(ctx, confined, "SELECT 1 FROM `__adhoc_0`", nil, own), "the person's statement")
	assert.False(t, r.cl.ownClearRun(ctx, confined, "SELECT 1 FROM `__adhoc_0`", obo, &sessionHold{b: sessiondata.Binding{Sensitivity: confined}}))

	// A failure is marked only when the run would have been unlabelled.
	cause := errors.New("boom")
	dec := dispatchDecision{sensitivity: confined}
	marked := r.cl.markOwnClearFailure(ctx, cause, dec, read(handle), obo, nil)
	assert.True(t, failedOwnClear(marked))
	assert.ErrorIs(t, marked, cause)
	assert.Equal(t, "boom", marked.Error(), "it reads as the error it wraps")
	assert.False(t, failedOwnClear(r.cl.markOwnClearFailure(ctx, cause, dec, read(theirs), obo, nil)))
	assert.False(t, failedOwnClear(r.cl.markOwnClearFailure(ctx, cause, dec, read(handle), nil, nil)))
	assert.False(t, failedOwnClear(cause))
	assert.False(t, failedOwnClear(nil))
}
