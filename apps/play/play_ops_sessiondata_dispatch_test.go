package play

import (
	"sync"
	"testing"
	"time"

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
// calls, the snapshot, the launcher's latch of the data session's listing
// in place of its frame, and the frame's end, where asynchronous commands
// settle.
func (inst *dispatchHost) frame() {
	inst.frames.Lock()
	defer inst.frames.Unlock()
	inst.eng.BeginFrame()
	inst.eng.ApplyQueued()
	inst.eng.TakeSnapshot()
	inst.l.inner.latchSessions()
	inst.eng.EndFrame()
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
	svc, err := agent.NewService(ctx, bus, zerolog.Nop(), agent.Config{Registry: reg, Host: host, TestGrants: true, Journal: agent.AcceptAnyIntent{}})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	host.eng.SetListener(func(e opengine.LogEntry) { svc.Listener()(key, e) })
	cli := agent.NewClient(bus.NewClient("test.coordinator", agent.ClientCaps("test: drive play")))

	dests := []string{DestinationClickHouse(r.host), sessiondata.DestinationSession(r.host)}
	g, err := cli.Request(ctx, agent.GrantRequest{Plan: "keep and drop a data set",
		Entries: []agent.GrantEntry{{Instance: key, Mode: agent.ModeAct}}, Destinations: dests})
	require.NoError(t, err)
	r.grant(g.Task, dests...)
	obo := &app.OnBehalfOf{Task: g.Task, Epoch: 1, Call: "c0", Destinations: dests}
	_, err = l.inner.client.defineSession(ctx, obo, "top", "SELECT 1 AS x", nil)
	require.NoError(t, err)
	// A run for the task put its mark on the window, as the live turn's
	// runs did before the drop.
	l.inner.client.SetAgentMark(obo)

	call := func(k string, op string, args string) agent.Outcome {
		t.Helper()
		out, cErr := cli.Call(ctx, agent.CallRequest{Handle: g.Handle, Instance: key, Operation: op, Args: args, Key: k, Turn: "turn-1"})
		require.NoError(t, cErr)
		return out
	}
	final := func(k string) agent.Outcome {
		t.Helper()
		var out agent.Outcome
		require.Eventually(t, func() bool {
			host.frame()
			st, sErr := cli.Status(ctx, g.Handle, k, 0)
			require.NoError(t, sErr)
			out = st
			return st.Phase == "completed" || st.Phase == "refused" || st.Phase == "conflict" || st.Phase == "failed"
		}, 5*time.Second, 5*time.Millisecond)
		return out
	}

	// applied is a command's outcome once a frame applied it: a call
	// answers accepted, and a conflict is found when the frame applies it.
	applied := func(k string) agent.Outcome {
		t.Helper()
		host.frame()
		st, sErr := cli.Status(ctx, g.Handle, k, 0)
		require.NoError(t, sErr)
		return st
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
