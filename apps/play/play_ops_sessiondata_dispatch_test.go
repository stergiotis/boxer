package play

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/capture"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/inscribe"
	"github.com/stergiotis/boxer/public/keelson/runtime/sessiondata"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// dispatchHost is enough of a window host for the agent dispatcher to drive
// one play window's catalog: its engine, served on the operation subjects,
// with frames the test runs.
type dispatchHost struct {
	mu     sync.Mutex
	key    uint64
	eng    *opengine.Engine
	l      *PlayLauncher
	appId  app.AppIdT
	scene  *inscribe.Scene
	frames sync.Mutex
	// body is the app's frame between the snapshot and the frame's end;
	// nil latches the data session's listing alone.
	body func()
}

var _ agent.HostI = (*dispatchHost)(nil)

func (inst *dispatchHost) OpsInstances() (out []opwire.InstanceInfo) {
	return []opwire.InstanceInfo{{App: inst.appId, Alias: inst.appId.DerivedAlias(), Key: inst.key, Title: "play", Ops: true, Confined: inst.eng.Confined()}}
}
func (inst *dispatchHost) OpsRenderGoroutine() uint64 { return 0 }
func (inst *dispatchHost) OpsStatus(_ uint64, id string) (opwire.Outcome, bool) {
	return inst.eng.Status(id)
}
func (inst *dispatchHost) OpsCancel(_ uint64, id string) (opwire.Outcome, bool) {
	return inst.eng.Cancel(id)
}
func (inst *dispatchHost) OpsExpire(_ uint64, ids []string, reason string) {
	inst.eng.Expire(ids, reason)
}
func (inst *dispatchHost) OpsAttach(_ uint64, a bool) bool { inst.eng.SetAttached(a); return true }
func (inst *dispatchHost) RenderSvg(uint64, func() bool) (string, error) {
	return "", nil
}
func (inst *dispatchHost) RenderPixels([]uint64, func() bool) (string, error) {
	return "", nil
}
func (inst *dispatchHost) RenderTree([]uint64, func() bool) (string, error) {
	return "", nil
}
func (inst *dispatchHost) SourceStatus(string) (capture.SourceResult, bool) {
	return capture.SourceResult{}, false
}
func (inst *dispatchHost) OpsRevisions(uint64) (map[string]uint64, bool) {
	revs, _ := inst.eng.SnapshotRevisions()
	return revs, true
}
func (inst *dispatchHost) OpsUndo(_ uint64, id string) bool { return inst.eng.Undo(id) }
func (inst *dispatchHost) OpsUndoStatus(_ uint64, id string) (string, bool) {
	return inst.eng.UndoStatus(id)
}
func (inst *dispatchHost) OpsLogSince(_ uint64, seq uint64) ([]opengine.LogEntry, uint64, bool) {
	return inst.eng.LogSince(seq), inst.eng.LogSeq(), true
}
func (inst *dispatchHost) OpsOpen(app.AppIdT, string, []byte) (uint64, error) { return 0, nil }
func (inst *dispatchHost) OpsArrange(string, []uint64) error                  { return nil }
func (inst *dispatchHost) OpsRaise(uint64) error                              { return nil }
func (inst *dispatchHost) OpsPlace(uint64, float32, float32, float32, float32) error {
	return nil
}
func (inst *dispatchHost) OpsLayout(string, float32, int) error { return nil }
func (inst *dispatchHost) OpsFloat(uint64, bool) error          { return nil }
func (inst *dispatchHost) OpsClose(uint64) error                { return nil }
func (inst *dispatchHost) Marks() *inscribe.Scene {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.scene == nil {
		inst.scene = inscribe.NewScene()
	}
	return inst.scene
}

// frame runs one frame of the window as the window host does: queued
// calls, the snapshot, the app's frame (by default the launcher's latch of
// the data session's listing alone), and the frame's end, where
// asynchronous commands settle.
func (inst *dispatchHost) frame() {
	inst.frames.Lock()
	defer inst.frames.Unlock()
	inst.eng.BeginFrame()
	inst.eng.ApplyQueued()
	inst.eng.TakeSnapshot()
	if inst.body != nil {
		inst.body()
	} else {
		inst.l.inner.latchSessions()
	}
	inst.eng.EndFrame()
}

// dispatchRig is one play window served to the agent dispatcher over a
// bus, a coordinator's client, and a task granted act on the window with
// the session data service's destinations.
type dispatchRig struct {
	host *dispatchHost
	cli  *agent.Client
	g    agent.Grant
	obo  *app.OnBehalfOf
	// svc is the dispatcher, which a host service asks to attest a call.
	svc *agent.Service
}

// newDispatchRig serves l's catalog through h to a dispatcher, draws a frame
// whenever a query waits on one, as the host's repaint does, and grants a
// task act on the window with r's destinations.
func newDispatchRig(t *testing.T, r *sdRig, l *PlayLauncher, h app.OperationsHandlerI) (rig *dispatchRig) {
	t.Helper()
	dests := []string{DestinationClickHouse(r.host), sessiondata.DestinationSession(r.host)}
	rig = newDispatchRigFor(t, l, h, dests, nil)
	r.grant(rig.g.Task, dests...)
	return
}

// newDispatchRigFor is newDispatchRig with the destinations of the task's
// grant given, and the dispatcher's configuration open to configure.
func newDispatchRigFor(t *testing.T, l *PlayLauncher, h app.OperationsHandlerI, dests []string, configure func(cfg *agent.Config)) (rig *dispatchRig) {
	t.Helper()
	ctx := t.Context()
	playId := app.AppIdT("github.com/stergiotis/boxer/apps/play")
	catalog := (&PlayLauncher{}).Manifest().Operations

	reg := app.NewRegistry()
	m := (&PlayLauncher{}).Manifest()
	require.NoError(t, reg.RegisterFactory(m, func() (app.AppI, error) { return nil, nil }))
	bus := inprocbus.NewInst(zerolog.Nop())
	const key = 2
	host := &dispatchHost{key: key, eng: opengine.New(ctx, catalog, h), l: l, appId: m.Id}
	require.Equal(t, playId, m.Id)
	host.frame()
	hostClient := bus.NewClient(opwire.HostOpsAppId, []app.SubjectFilter{
		{Pattern: opwire.Pattern, Direction: app.CapDirectionSub},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub},
	})
	_, err := hostClient.Subscribe(opwire.Pattern, func(msg *app.Msg) {
		if msg.Sender != opwire.DispatcherAppId {
			return
		}
		_, _, op, _ := opwire.ParseSubject(msg.Subject)
		req, _ := buscodec.Decode[opwire.CallRequest](msg.Payload)
		_ = buscodec.Reply(hostClient.Publish, msg.Reply, opwire.CallReply{V: opwire.WireVersion, Outcome: host.eng.Submit(msg.Context(), op, req)})
	})
	require.NoError(t, err)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		// The host draws a frame whenever a query waits on one, as its
		// repaint does.
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Millisecond):
			}
			if host.eng.Waiting() {
				host.frame()
			}
		}
	}()
	cfg := agent.Config{Registry: reg, Host: host, TestGrants: true, Journal: agent.AcceptAnyIntent{}}
	if configure != nil {
		configure(&cfg)
	}
	svc, err := agent.NewService(ctx, bus, zerolog.Nop(), cfg)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	host.eng.SetListener(func(e opengine.LogEntry) { svc.Listener()(key, e) })
	cli := agent.NewClient(bus.NewClient("test.coordinator", agent.ClientCaps("test: drive play")))

	g, err := cli.Request(ctx, agent.GrantRequest{Plan: "keep and drop a data set",
		Entries: []agent.GrantEntry{{Instance: key, Mode: agent.ModeAct}}, Destinations: dests})
	require.NoError(t, err)
	return &dispatchRig{host: host, cli: cli, g: g, obo: &app.OnBehalfOf{Task: g.Task, Epoch: 1, Call: "c0", Destinations: dests}, svc: svc}
}

// call makes a call of the task under key k, as the chat does in a turn.
func (inst *dispatchRig) call(t *testing.T, k string, op string, args string) agent.Outcome {
	t.Helper()
	out, err := inst.cli.Call(t.Context(), agent.CallRequest{Handle: inst.g.Handle, Instance: inst.host.key, Operation: op, Args: args, Key: k, Turn: "turn-1"})
	require.NoError(t, err)
	return out
}

// final steps frames until the call under key k ended.
func (inst *dispatchRig) final(t *testing.T, k string) agent.Outcome {
	t.Helper()
	var out agent.Outcome
	require.Eventually(t, func() bool {
		inst.host.frame()
		st, err := inst.cli.Status(t.Context(), inst.g.Handle, k, 0)
		require.NoError(t, err)
		out = st
		return st.Phase == "completed" || st.Phase == "refused" || st.Phase == "conflict" || st.Phase == "failed"
	}, 5*time.Second, 5*time.Millisecond)
	return out
}

// applied is a command's outcome once a frame applied it: a call answers
// accepted, and a conflict is found when the frame applies it.
func (inst *dispatchRig) applied(t *testing.T, k string) agent.Outcome {
	t.Helper()
	inst.host.frame()
	st, err := inst.cli.Status(t.Context(), inst.g.Handle, k, 0)
	require.NoError(t, err)
	return st
}

// TestTheDataSetCatalogNeverHoldsAQueryBackThroughTheDispatcher: through the
// agent dispatcher, as a model drives play, drop_dataset needs no read of
// the data-set catalog first, a plain query afterwards does not conflict on
// it, the drop's settled revisions are the window's own once its listing
// landed, list_datasets reads the catalog, and keep_as needs no read of it.
func TestTheDataSetCatalogNeverHoldsAQueryBackThroughTheDispatcher(t *testing.T) {
	ctx := t.Context()
	r := newSdRig(t)
	l, h := opsLauncher(t)
	l.inner.client = r.client("github.com/x/play", 1)
	rig := newDispatchRig(t, r, l, h)
	host, obo := rig.host, rig.obo
	_, err := l.inner.client.defineSession(ctx, obo, "", "top", "SELECT 1 AS x", nil)
	require.NoError(t, err)
	// A run for the task put its mark on the window, as the live turn's
	// runs did before the drop.
	l.inner.client.SetAgentMark(obo)

	call := func(k string, op string, args string) agent.Outcome {
		t.Helper()
		return rig.call(t, k, op, args)
	}
	final := func(k string) agent.Outcome {
		t.Helper()
		return rig.final(t, k)
	}
	applied := func(k string) agent.Outcome {
		t.Helper()
		return rig.applied(t, k)
	}

	call("drop", opDropDataset, `{"name":"top"}`)
	out := final("drop")
	require.Equal(t, "completed", out.Phase, out.Reason)
	for range 5 {
		host.frame()
	}
	revs, _ := host.eng.SnapshotRevisions()
	assert.Equal(t, revs[opsResSessionDatasets], out.Revisions[opsResSessionDatasets],
		"the drop settles once the window's listing shows it, so its revision is the window's own")

	// The task reads the buffer and the result, as the direct tool's call
	// would have, so only the catalog could hold the query back.
	out = call("state", opGetState, `{}`)
	require.Equal(t, "completed", out.Phase, out.Reason)
	call("q1", opQuery, `{"sql":"SELECT 1"}`)
	out = applied("q1")
	assert.NotEqual(t, "conflict", out.Phase, "a plain query never conflicts on the data-set catalog: %s", out.Reason)

	out = call("list", opListDatasets, `{}`)
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.Contains(t, out.Revisions, opsResSessionDatasets, "list_datasets reads the data-set catalog")

	call("keep", opQuery, `{"sql":"SELECT 2 AS y","keep_as":"again"}`)
	out = applied("keep")
	assert.NotEqual(t, "conflict", out.Phase, "keeping needs no read of the catalog: %s", out.Reason)
}

// playFrame is play's frame short of drawing, as the window host runs it
// for a launcher: the agent mark judged after the write-back, the listings
// latched, the result the panels draw, the run asked for started, and the
// mark settled once the frame's own changes are in.
func playFrame(p *PlayApp) func() {
	return func() {
		p.checkAgentMark()
		p.latchSessions()
		p.latchExposures()
		rec, _, _, _, _, _, _, _, id := p.activeSnapshot()
		if rec != nil {
			rec.Release()
		}
		p.audioFollowResult(id)
		if p.requestRun && !p.graph.MainLoading() {
			p.requestRun = false
			auto := p.runIsAuto
			p.runIsAuto = false
			sub := p.requestSubquery
			p.requestSubquery = false
			p.runAgent = p.takeAgentForRun(auto)
			p.executeRun(auto, sub)
			p.runAgent = nil
		}
		p.settleAgentMark()
	}
}

// TestAKeepLeavesTheTaskCurrentAndOnlyThePersonPausesIt: through the agent
// dispatcher, with frames that run play's runs, a task that read the window
// keeps a data set and calls query again at once, with keep_as and
// without: no call is refused, every change to the buffer the keeps made
// is the task's, and the settled keep reports the revisions the window
// holds. An edit of the person's between two of the task's calls still
// pauses it until its next turn.
func TestAKeepLeavesTheTaskCurrentAndOnlyThePersonPausesIt(t *testing.T) {
	r := newSdRig(t)
	cl := r.client("github.com/x/play", 1)
	cl.reach.record(r.stub.url())
	p := NewPlayApp(cl, newLiveQueryGraph(cl, memory.NewGoAllocator(), 4), "SELECT 1", nil)
	t.Cleanup(p.graph.close)
	l := &PlayLauncher{inner: p}
	rig := newDispatchRig(t, r, l, l.Operations())
	rig.host.body = playFrame(p)
	task := opwire.WriterTask(rig.g.Task)
	notRefused := func(k string, args string) agent.Outcome {
		t.Helper()
		out := rig.call(t, k, opQuery, args)
		require.NotEqual(t, "refused", out.Phase, "%s: %s", k, out.Reason)
		out = rig.final(t, k)
		require.Equal(t, "completed", out.Phase, "%s: %s", k, out.Reason)
		return out
	}

	// The task reads the window first, as a model's get_state does, so a
	// change to the buffer by anyone else would pause it.
	out := rig.call(t, "state", opGetState, `{}`)
	require.Equal(t, "completed", out.Phase, out.Reason)
	require.Contains(t, out.Revisions, opsResSql)
	since := rig.host.eng.LogSeq()

	out = notRefused("keep1", `{"sql":"SELECT 1 AS x","keep_as":"traindocs"}`)
	revs, _ := rig.host.eng.SnapshotRevisions()
	assert.Equal(t, revs[opsResSql], out.Revisions[opsResSql], "the keep reports the buffer's revision the window holds")
	assert.Equal(t, "SELECT * FROM session('traindocs') LIMIT 20", p.sql, "the window samples the data set as its main run")
	// At once, as a model's next call is sent: with keep_as, then without.
	notRefused("keep2", `{"sql":"SELECT 2 AS x","keep_as":"maskdocs"}`)
	notRefused("plain", `{"sql":"SELECT * FROM session('maskdocs')"}`)
	// The frames after a settle, in which a sample run lands and the editor
	// draws its buffer again, change nothing of the person's either.
	notRefused("keep3", `{"sql":"SELECT 3 AS x","keep_as":"traindocs"}`)
	for range 5 {
		rig.host.frame()
	}
	notRefused("plain2", `{"sql":"SELECT 4"}`)
	for _, e := range rig.host.eng.LogSince(since) {
		if slices.Contains(e.Resources, opsResSql) {
			assert.Equal(t, task, e.Writer, "change %d to sql (%s, %s) is the task's", e.Seq, e.Op, e.Phase)
		}
	}

	// The person edits the buffer between two of the task's calls: the
	// write-back lands before the next frame.
	rig.host.frames.Lock()
	p.sql = "SELECT 42 AS mine"
	rig.host.frames.Unlock()
	rig.host.frame()
	out = rig.call(t, "after", opQuery, `{"sql":"SELECT 5"}`)
	require.Equal(t, "refused", out.Phase)
	assert.Contains(t, out.Reason, "paused: person changed sql")
	changes, err := rig.cli.Turn(t.Context(), rig.g.Handle)
	require.NoError(t, err)
	var persons int
	for _, c := range changes {
		if c.Writer == opwire.WriterPerson {
			persons++
			assert.Equal(t, []string{opsResSql}, c.Resources)
		}
	}
	assert.Equal(t, 1, persons, "the turn lists the person's one edit and nothing of the task's as the person's: %+v", changes)
	// Read again, as the turn's note asks, the task writes once more.
	out = rig.call(t, "reread", opGetState, `{}`)
	require.Equal(t, "completed", out.Phase, out.Reason)
	notRefused("resumed", `{"sql":"SELECT 6"}`)
}
