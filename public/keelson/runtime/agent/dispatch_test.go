package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// The test participant: a document with text and a consequential export.

type doc struct {
	mu       sync.Mutex
	text     string
	editing  bool
	confined bool
	gotRef   []byte
}

type docSnap struct{ text string }

type setTextArgs struct {
	Text string `desc:"the new text"`
}
type textResult struct{ Text string }
type useRefArgs struct {
	Source string `desc:"a result reference"`
}

const docAppId app.AppIdT = "github.com/x/apps/doc"

var docOps = func() *appops.Set[*doc, docSnap] {
	s := appops.NewSet(func(d *doc) docSnap { return docSnap{text: d.text} })
	s.Resource("text", "the text", func(d *doc) any { return d.text })
	s.Editing("text", func(d *doc) bool { return d.editing })
	s.Confined(func(d *doc) bool { return d.confined })
	appops.Command(s, app.OperationSpec{Name: "set_text", Version: 1, Summary: "replace the text",
		Effect: app.OperationEffectDocument, Writes: []string{"text"}, Agents: true},
		func(d *doc, call app.OperationCall, in setTextArgs) (appops.None, error) {
			d.text = in.Text
			return appops.None{}, nil
		})
	appops.Command(s, app.OperationSpec{Name: "use_ref", Version: 1, Summary: "take a result by reference",
		Effect: app.OperationEffectDocument, Writes: []string{"text"}, Refs: []string{"source"}, Agents: true},
		func(d *doc, call app.OperationCall, in useRefArgs) (appops.None, error) {
			d.gotRef = call.RefData["source"]
			return appops.None{}, nil
		})
	appops.Query(s, app.OperationSpec{Name: "get_text", Version: 1, Summary: "read the text", Reads: []string{"text"}, Agents: true, Untrusted: true},
		func(sn docSnap, in appops.None) (textResult, error) { return textResult{Text: sn.text}, nil })
	appops.Command(s, app.OperationSpec{Name: "export", Version: 1, Summary: "export the text",
		Effect: app.OperationEffectConsequential, Reads: []string{"text"}, Agents: true},
		func(d *doc, call app.OperationCall, in appops.None) (appops.None, error) { return appops.None{}, nil })
	appops.Command(s, app.OperationSpec{Name: "wipe", Version: 1, Summary: "clear everything",
		Effect: app.OperationEffectDocument, Writes: []string{"text"}},
		func(d *doc, call app.OperationCall, in appops.None) (appops.None, error) {
			d.text = ""
			return appops.None{}, nil
		})
	return s
}()

// fakeHost stands in for the window host: engines per instance, served on
// the operation subjects, with frames run by the test.
type fakeHost struct {
	mu      sync.Mutex
	engines map[uint64]*opengine.Engine
	docs    map[uint64]*doc
	render  atomic.Uint64
}

func (inst *fakeHost) OpsInstances() (out []opwire.InstanceInfo) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for k, e := range inst.engines {
		out = append(out, opwire.InstanceInfo{App: docAppId, Alias: docAppId.SubjectAlias(), Key: k, Title: "Doc", Ops: true,
			Confined: e.Confined()})
	}
	return
}
func (inst *fakeHost) OpsRenderGoroutine() uint64 { return inst.render.Load() }
func (inst *fakeHost) eng(k uint64) *opengine.Engine {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.engines[k]
}
func (inst *fakeHost) OpsStatus(k uint64, id string) (opwire.Outcome, bool) {
	return inst.eng(k).Status(id)
}
func (inst *fakeHost) OpsCancel(k uint64, id string) (opwire.Outcome, bool) {
	return inst.eng(k).Cancel(id)
}
func (inst *fakeHost) OpsExpire(k uint64, ids []string, reason string) {
	inst.eng(k).Expire(ids, reason)
}
func (inst *fakeHost) OpsAttach(k uint64, a bool) bool {
	if e := inst.eng(k); e != nil {
		e.SetAttached(a)
		return true
	}
	return false
}
func (inst *fakeHost) OpsCapture(k uint64) (string, error) { return "cap-1", nil }
func (inst *fakeHost) OpsRevisions(k uint64) (map[string]uint64, bool) {
	revs, _ := inst.eng(k).SnapshotRevisions()
	return revs, true
}
func (inst *fakeHost) OpsUndo(k uint64, id string) bool { return inst.eng(k).Undo(id) }
func (inst *fakeHost) OpsOpen(appId app.AppIdT, kind string, cfg []byte) (uint64, error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	key := uint64(100 + len(inst.engines))
	d := &doc{text: "opened"}
	inst.engines[key] = opengine.New(docOps.Catalog(), docOps.Bind(d))
	inst.docs[key] = d
	return key, nil
}
func (inst *fakeHost) OpsLogSince(k uint64, seq uint64) ([]opengine.LogEntry, uint64, bool) {
	e := inst.eng(k)
	return e.LogSince(seq), e.LogSeq(), true
}
func (inst *fakeHost) OpsUndoStatus(k uint64, id string) (string, bool) {
	return inst.eng(k).UndoStatus(id)
}
func (inst *fakeHost) OpsCaptureStatus(job string) (opwire.CaptureStatus, bool) {
	return opwire.CaptureStatus{Phase: opwire.PhaseCompleted, Path: "/dev/null", MediaType: "image/svg+xml"}, true
}

// frame runs one frame of instance k; person runs where the write-back
// would land.
func (inst *fakeHost) frame(k uint64) {
	e := inst.eng(k)
	e.BeginFrame()
	e.ApplyQueued()
	e.TakeSnapshot()
	e.EndFrame()
}

type rig struct {
	t      *testing.T
	host   *fakeHost
	svc    *Service
	cli    *Client
	bus    *inprocbus.Inst
	docKey uint64
}

func newRig(t *testing.T, testGrants bool) *rig {
	t.Helper()
	return newRigWith(t, func(cfg *Config) { cfg.TestGrants = testGrants })
}

func newRigWith(t *testing.T, configure func(cfg *Config)) *rig {
	t.Helper()
	reg := app.NewRegistry()
	require.NoError(t, reg.RegisterFactory(app.Manifest{Id: docAppId, Display: "Doc", Summary: "edit a doc",
		Surface: app.SurfaceWindowed, Topics: []app.TopicT{app.AllTopics[0]}, Operations: docOps.Catalog()},
		func() (app.AppI, error) { return nil, nil }))
	bus := inprocbus.NewInst(zerolog.Nop())
	host := &fakeHost{engines: map[uint64]*opengine.Engine{}, docs: map[uint64]*doc{}}
	d := &doc{text: "start"}
	host.engines[7] = opengine.New(docOps.Catalog(), docOps.Bind(d))
	host.docs[7] = d
	host.frame(7)
	// The window host's side of the operation subjects.
	hostClient := bus.NewClient(opwire.HostOpsAppId, []app.SubjectFilter{
		{Pattern: opwire.Pattern, Direction: app.CapDirectionSub},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub},
	})
	_, err := hostClient.Subscribe(opwire.Pattern, func(msg *app.Msg) {
		if msg.Sender != opwire.DispatcherAppId {
			return
		}
		_, key, op, _ := opwire.ParseSubject(msg.Subject)
		req, _ := buscodec.Decode[opwire.CallRequest](msg.Payload)
		_ = buscodec.Reply(hostClient.Publish, msg.Reply, opwire.CallReply{V: opwire.WireVersion, Outcome: host.eng(key).Submit(op, req)})
	})
	require.NoError(t, err)
	cfg := Config{Registry: reg, Host: host}
	configure(&cfg)
	svc, err := NewService(bus, zerolog.Nop(), cfg)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	host.engines[7].SetListener(func(e opengine.LogEntry) { svc.Listener()(7, e) })
	cli := NewClient(bus.NewClient("test.coordinator", ClientCaps("test: drive apps")))
	return &rig{t: t, host: host, svc: svc, cli: cli, bus: bus, docKey: 7}
}

func (inst *rig) grant(mode ModeE, ops ...string) Grant {
	g, err := inst.cli.Request(context.Background(), GrantRequest{Plan: "edit the doc",
		Entries: []GrantEntry{{Instance: inst.docKey, Mode: mode, Operations: ops}}})
	require.NoError(inst.t, err)
	return g
}

func (inst *rig) call(g Grant, key string, op string, args string) Outcome {
	out, err := inst.cli.Call(context.Background(), CallRequest{Handle: g.Handle, Instance: inst.docKey, Operation: op, Args: args, Key: key})
	require.NoError(inst.t, err)
	return out
}

func TestGrantNeedsTheTestFlag(t *testing.T) {
	r := newRig(t, false)
	_, err := r.cli.Request(context.Background(), GrantRequest{Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, "coordinator", "without test grants, a request needs a registered coordinator and the person")
}

func TestReadThenWriteThroughTheDispatcher(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	ctx := context.Background()

	q := r.call(g, "k1", "get_text", "{}")
	require.Equal(t, "completed", q.Phase, q.Reason)
	res, err := r.cli.Read(ctx, g.Handle, q.ResultRef)
	require.NoError(t, err)
	assert.Equal(t, `{"text":"start"}`, res.Text)

	// No expectation given: the dispatcher uses the revision k1 read.
	c := r.call(g, "k2", "set_text", `{"text":"agent"}`)
	require.Equal(t, "accepted", c.Phase, c.Reason)
	r.host.frame(7)
	st, err := r.cli.Status(ctx, g.Handle, "k2", 0)
	require.NoError(t, err)
	assert.Equal(t, "applied", st.Phase)
	r.host.frame(7)
	st, err = r.cli.Status(ctx, g.Handle, "k2", 0)
	require.NoError(t, err)
	assert.Equal(t, "rendered", st.Phase)
	assert.Equal(t, "agent", r.host.docs[7].text)

	// A repeated key returns the first outcome and does not queue again.
	again := r.call(g, "k2", "set_text", `{"text":"other"}`)
	assert.Equal(t, "rendered", again.Phase)
	r.host.frame(7)
	assert.Equal(t, "agent", r.host.docs[7].text)
}

func TestPersonWinsATie(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	r.call(g, "read", "get_text", "{}")
	r.call(g, "write", "set_text", `{"text":"agent"}`)
	r.host.docs[7].text = "person" // the write-back of the frame the call was queued in
	r.host.frame(7)
	st, err := r.cli.Status(context.Background(), g.Handle, "write", 0)
	require.NoError(t, err)
	assert.Equal(t, "conflict", st.Phase)
	assert.Equal(t, "person", r.host.docs[7].text)
}

func TestWriteWithoutAReadAsksToReadFirst(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	r.call(g, "w", "set_text", `{"text":"agent"}`)
	r.host.frame(7)
	st, _ := r.cli.Status(context.Background(), g.Handle, "w", 0)
	assert.Equal(t, "conflict", st.Phase)
	assert.Contains(t, st.Reason, "read first")
}

func TestTheDispatcherDecides(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	assert.Equal(t, "denied", r.call(g, "a", "wipe", "{}").Phase, "not exposed to agents")
	assert.Equal(t, "input_required", r.call(g, "b", "export", "{}").Phase, "consequential")
	assert.Equal(t, "refused", r.call(g, "c", "set_text", `{"text":3}`).Phase, "arguments outside the schema")
	assert.Equal(t, "refused", r.call(g, "d", "nope", "{}").Phase)
	out, err := r.cli.Call(context.Background(), CallRequest{Handle: g.Handle, Instance: 99, Operation: "get_text", Key: "e"})
	require.NoError(t, err)
	assert.Equal(t, "refused", out.Phase, "no window by that key is open")

	obs := r.grant(ModeObserve)
	assert.Equal(t, "completed", r.call(obs, "a", "get_text", "{}").Phase)
	assert.Equal(t, "input_required", r.call(obs, "b", "set_text", `{"text":"x"}`).Phase)

	narrow := r.grant(ModeAct, "get_text")
	assert.Equal(t, "input_required", r.call(narrow, "a", "set_text", `{"text":"x"}`).Phase)

	other := NewClient(r.bus.NewClient("test.other", ClientCaps("test")))
	out, err = other.Call(context.Background(), CallRequest{Handle: g.Handle, Instance: 7, Operation: "get_text", Key: "x"})
	require.NoError(t, err)
	assert.Equal(t, "denied", out.Phase, "a handle is honoured only from its actor")
}

func TestBudget(t *testing.T) {
	r := newRig(t, true)
	g, err := r.cli.Request(context.Background(), GrantRequest{Calls: 1, Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
	require.NoError(t, err)
	assert.Equal(t, "completed", r.call(g, "a", "get_text", "{}").Phase)
	assert.Equal(t, "input_required", r.call(g, "b", "get_text", "{}").Phase)
}

func TestCancelStopAndDetach(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	g := r.grant(ModeAct)
	r.call(g, "r", "get_text", "{}")
	r.call(g, "w1", "set_text", `{"text":"x"}`)
	out, err := r.cli.Cancel(ctx, g.Handle, "w1")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", out.Phase)

	r.call(g, "w2", "set_text", `{"text":"y"}`)
	require.NoError(t, r.cli.Stop(ctx, g.Handle))
	r.host.frame(7)
	assert.Equal(t, "start", r.host.docs[7].text)
	_, err = r.cli.Status(ctx, g.Handle, "w2", 0)
	require.NoError(t, err)
	assert.Equal(t, "denied", r.call(g, "z", "get_text", "{}").Phase, "a stopped task's handle is refused")

	g2 := r.grant(ModeAct)
	require.NoError(t, r.cli.Detach(ctx, g2.Handle, 7))
	assert.Equal(t, "input_required", r.call(g2, "a", "get_text", "{}").Phase)
	insts, err := r.cli.List(ctx, g2.Handle)
	require.NoError(t, err)
	assert.Empty(t, insts)
}

func TestEditingConflicts(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	r.call(g, "r", "get_text", "{}")
	r.host.docs[7].editing = true
	r.call(g, "w", "set_text", `{"text":"x"}`)
	r.host.frame(7)
	st, _ := r.cli.Status(context.Background(), g.Handle, "w", 0)
	assert.Equal(t, "conflict", st.Phase)
	assert.Contains(t, st.Reason, "editing")
}

func TestStatusWaitsForAFinalPhase(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	r.call(g, "r", "get_text", "{}")
	r.call(g, "w", "set_text", `{"text":"x"}`)
	go func() {
		time.Sleep(50 * time.Millisecond)
		r.host.frame(7)
		r.host.frame(7)
	}()
	st, err := r.cli.Status(context.Background(), g.Handle, "w", 2*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "rendered", st.Phase)
}

func TestRequestFromTheRenderGoroutineIsRefused(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	r.host.render.Store(opwire.GoroutineId())
	_, err := r.cli.Call(context.Background(), CallRequest{Handle: g.Handle, Instance: 7, Operation: "get_text", Key: "a"})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, "render goroutine")
}

func TestCaptureAndRecords(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	g := r.grant(ModeObserve)
	out, err := r.cli.Capture(ctx, g.Handle, 7, "cap")
	require.NoError(t, err)
	assert.Equal(t, "completed", out.Phase)
	res, err := r.cli.Read(ctx, g.Handle, out.Job)
	require.NoError(t, err)
	assert.Equal(t, "image/svg+xml", res.MediaType)

	r.call(g, "q", "get_text", "{}")
	acts := r.svc.Actions()
	require.NotEmpty(t, acts)
	var sawFinal bool
	for _, a := range acts {
		if a.Operation == "get_text" && a.Decision == "final" {
			sawFinal = true
			assert.Equal(t, "completed", a.Phase)
			assert.True(t, a.Test)
		}
	}
	assert.True(t, sawFinal)

	reg := introspect.NewRegistry()
	require.NoError(t, RegisterIntrospect(reg, r.svc))
	for _, name := range []string{TableGrants, TableActions} {
		p, ok := reg.Lookup(name)
		require.True(t, ok)
		rec, err := p.Snapshot(introspect.Projection{})
		require.NoError(t, err)
		assert.Positive(t, rec.NumRows(), name)
		rec.Release()
	}
}
