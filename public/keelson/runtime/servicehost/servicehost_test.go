package servicehost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

const testKind = "servicehostTestCfg"

var testCfg = []byte("servicehost-test-cfg")

func init() {
	kindcheck.Register(testKind, func(b []byte) (err error) {
		if string(b) != string(testCfg) {
			err = errors.New("not the fixture payload")
		}
		return
	})
}

// recorder is a headless app that records what the host did around it.
type recorder struct {
	manifest app.Manifest

	mountErr   error
	unmountErr error
	// mountBlocks keeps Mount from returning until Cancel() fires, for the
	// "reaped while starting" case.
	mountBlocks bool
	// tail is work the app counts into its lifetime scope; it ends when
	// release is closed.
	tail    bool
	release chan struct{}

	mountCtx        app.MountContextI
	mounted         chan struct{}
	cancelledBefore bool
	publishErr      error
	unmountCalls    int
	cfgSeen         []byte
	reasonSeen      app.LaunchReasonE
	keySeen         uint64
}

var _ app.AppI = (*recorder)(nil)

func (inst *recorder) Manifest() (m app.Manifest) { return inst.manifest }
func (inst *recorder) Mount(ctx app.MountContextI) (err error) {
	inst.mountCtx = ctx
	inst.cfgSeen = ctx.LaunchConfig()
	inst.reasonSeen = ctx.LaunchReason()
	inst.keySeen = ctx.InstanceKey()
	_, _ = ctx.Bus().Subscribe("t.evt", func(msg *app.Msg) {})
	if inst.tail {
		app.LifetimeOf(ctx).Go(func(c context.Context) { <-inst.release })
	}
	if inst.mounted != nil {
		close(inst.mounted)
	}
	if inst.mountBlocks {
		<-ctx.Cancel()
	}
	err = inst.mountErr
	return
}
func (inst *recorder) Frame(ctx app.FrameContextI) (err error) { panic("Frame on a service") }
func (inst *recorder) Unmount(ctx app.MountContextI) (err error) {
	inst.unmountCalls++
	select {
	case <-ctx.Cancel():
		inst.cancelledBefore = true
	default:
	}
	inst.publishErr = ctx.Bus().Publish("t.bye", nil)
	err = inst.unmountErr
	return
}

func headless(id app.AppIdT) (m app.Manifest) {
	m = app.Manifest{
		Id: id, Version: "0.1.0", Display: string(id), Summary: "fixture", Topics: []app.TopicT{app.TopicRuntime},
		Surface: app.SurfaceHeadless, Caps: []app.SubjectFilter{{Pattern: "t.>", Direction: app.CapDirectionBoth}},
	}
	return
}

type fixture struct {
	reg   *app.Registry
	bus   *inprocbus.Inst
	facts *factsstore.InMemoryFactsStore
	apps  map[app.AppIdT]*recorder
}

func newFixture(t *testing.T, recs ...*recorder) (f *fixture) {
	t.Helper()
	f = &fixture{reg: app.NewRegistry(), bus: inprocbus.NewInst(zerolog.Nop()), facts: factsstore.NewInMemoryFactsStore(), apps: map[app.AppIdT]*recorder{}}
	for _, r := range recs {
		r := r
		f.apps[r.manifest.Id] = r
		require.NoError(t, f.reg.RegisterFactory(r.manifest, func() (app.AppI, error) { return r, nil }))
	}
	windowed := app.Manifest{Id: "test.windowed", Version: "0.1.0", Display: "w", Summary: "fixture", Topics: []app.TopicT{app.TopicRuntime}, Surface: app.SurfaceWindowed}
	require.NoError(t, f.reg.RegisterFactory(windowed, func() (app.AppI, error) { return &recorder{manifest: windowed}, nil }))
	return
}

func (f *fixture) host(t *testing.T) (h *Inst) {
	t.Helper()
	h = New(Config{Registry: f.reg, Bus: f.bus, Facts: f.facts, RunId: "run-1", Log: zerolog.New(zerolog.NewTestWriter(t)), ShutdownDrain: 2 * time.Second})
	return
}

func rowOf(t *testing.T, h *Inst, id app.AppIdT) (row Row) {
	t.Helper()
	for _, r := range h.Rows() {
		if r.Id == id {
			return r
		}
	}
	t.Fatalf("no row for %s", id)
	return
}

func TestStart_MountsSelectedOnlyAndKnowsTheRest(t *testing.T) {
	a := &recorder{manifest: headless("test.a")}
	b := &recorder{manifest: headless("test.b")}
	f := newFixture(t, a, b)
	h := f.host(t)
	require.NoError(t, h.Start(context.Background(), []app.AppIdT{"test.a", "test.windowed", "test.absent"}, nil))
	require.NoError(t, h.Wait(context.Background()))

	ra, rb := rowOf(t, h, "test.a"), rowOf(t, h, "test.b")
	assert.True(t, ra.Selected)
	assert.Equal(t, StateRunning, ra.State)
	assert.NotZero(t, ra.InstanceKey)
	assert.Equal(t, ra.InstanceKey, a.keySeen)
	assert.False(t, rb.Selected)
	assert.Equal(t, StateNotSelected, rb.State)
	assert.Nil(t, b.mountCtx, "an unselected service is never mounted")
	assert.Len(t, h.Rows(), 2, "the windowed app has no row")
	assert.Equal(t, 1, h.Running())

	lc := f.facts.Lifecycles()
	require.Len(t, lc, 1)
	assert.Equal(t, factsstore.AppLifecyclePhaseStarted, lc[0].Phase)
	assert.Equal(t, ra.InstanceKey, lc[0].TileKey)
	assert.Equal(t, "run-1", lc[0].RunId)
}

func TestReapAll_LeaveUnmountDrainUnload(t *testing.T) {
	a := &recorder{manifest: headless("test.a"), tail: true, release: make(chan struct{})}
	f := newFixture(t, a)
	h := f.host(t)
	require.NoError(t, h.Start(context.Background(), []app.AppIdT{"test.a"}, nil))
	require.NoError(t, h.Wait(context.Background()))
	require.Equal(t, 1, len(f.bus.Subscriptions()), "the app's subscription is live")

	done := make(chan struct{})
	go func() { h.ReapAll(StopReasonShutdown); close(done) }()
	// Unmount ran, after leave; the bus still answered inside it; the
	// client is not closed while the tail runs.
	require.Eventually(t, func() bool { return a.unmountCalls == 1 }, time.Second, 5*time.Millisecond)
	assert.True(t, a.cancelledBefore, "Cancel() fired before Unmount")
	assert.NoError(t, a.publishErr, "the bus was live inside Unmount")
	select {
	case <-done:
		t.Fatal("ReapAll returned before the tail ended")
	case <-time.After(50 * time.Millisecond):
	}
	assert.Equal(t, 1, len(f.bus.Subscriptions()), "the client stays open while the scope drains")
	close(a.release)
	<-done

	assert.Equal(t, 0, len(f.bus.Subscriptions()), "unload released the subscription")
	row := rowOf(t, h, "test.a")
	assert.Equal(t, StateStopped, row.State)
	assert.Equal(t, StopReasonShutdown, row.StopReason)
	assert.False(t, row.StoppedAt.IsZero())
	lc := f.facts.Lifecycles()
	require.Len(t, lc, 2)
	assert.Equal(t, factsstore.AppLifecyclePhaseStopped, lc[1].Phase)
	assert.Equal(t, StopReasonShutdown, lc[1].StopReason)
	h.ReapAll(StopReasonShutdown) // idempotent
	assert.Len(t, f.facts.Lifecycles(), 2)
}

func TestReapAll_TheDrainDeadlineReleasesAnyway(t *testing.T) {
	a := &recorder{manifest: headless("test.a"), tail: true, release: make(chan struct{})}
	f := newFixture(t, a)
	h := New(Config{Registry: f.reg, Bus: f.bus, Facts: f.facts, Log: zerolog.Nop(), ShutdownDrain: 50 * time.Millisecond})
	require.NoError(t, h.Start(context.Background(), []app.AppIdT{"test.a"}, nil))
	require.NoError(t, h.Wait(context.Background()))
	start := time.Now()
	h.ReapAll(StopReasonShutdown)
	assert.Less(t, time.Since(start), time.Second)
	assert.Equal(t, 0, len(f.bus.Subscriptions()))
	assert.Equal(t, StateStopped, rowOf(t, h, "test.a").State)
	close(a.release)
}

func TestMountError_IsRecordedAndReleased(t *testing.T) {
	a := &recorder{manifest: headless("test.a"), mountErr: errors.New("no store")}
	f := newFixture(t, a)
	h := f.host(t)
	require.NoError(t, h.Start(context.Background(), []app.AppIdT{"test.a"}, nil))
	require.NoError(t, h.Wait(context.Background()))
	require.Eventually(t, func() bool { return rowOf(t, h, "test.a").State == StateFailed }, time.Second, 5*time.Millisecond)
	row := rowOf(t, h, "test.a")
	assert.Equal(t, StopReasonMountError, row.StopReason)
	assert.Contains(t, row.LastError, "no store")
	assert.Equal(t, 0, h.Running())
	require.Eventually(t, func() bool { return len(f.bus.Subscriptions()) == 0 }, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return len(f.facts.Lifecycles()) == 2 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, StopReasonMountError, f.facts.Lifecycles()[1].StopReason)
	h.ReapAll(StopReasonShutdown)
	assert.Equal(t, 0, a.unmountCalls, "a failed Mount is not unmounted")
	assert.Len(t, f.facts.Lifecycles(), 2)
}

func TestUnmountError_IsFailedWithItsReason(t *testing.T) {
	a := &recorder{manifest: headless("test.a"), unmountErr: errors.New("leak")}
	f := newFixture(t, a)
	h := f.host(t)
	require.NoError(t, h.Start(context.Background(), []app.AppIdT{"test.a"}, nil))
	require.NoError(t, h.Wait(context.Background()))
	h.ReapAll(StopReasonShutdown)
	row := rowOf(t, h, "test.a")
	assert.Equal(t, StateFailed, row.State)
	assert.Equal(t, StopReasonUnmountError, row.StopReason)
	assert.Contains(t, row.LastError, "leak")
	assert.Equal(t, StopReasonUnmountError, f.facts.Lifecycles()[1].StopReason)
}

func TestReapAll_WhileStartingWaitsForMountThenUnmounts(t *testing.T) {
	a := &recorder{manifest: headless("test.a"), mountBlocks: true, mounted: make(chan struct{})}
	f := newFixture(t, a)
	h := f.host(t)
	require.NoError(t, h.Start(context.Background(), []app.AppIdT{"test.a"}, nil))
	<-a.mounted
	assert.Equal(t, StateStarting, rowOf(t, h, "test.a").State)
	h.ReapAll(StopReasonShutdown)
	assert.Equal(t, 1, a.unmountCalls, "leave let Mount return, then Unmount ran")
	assert.Equal(t, StateStopped, rowOf(t, h, "test.a").State)
}

func TestSeeds_DeliveredAndChecked(t *testing.T) {
	m := headless("test.a")
	m.LaunchKind = testKind
	a := &recorder{manifest: m}
	f := newFixture(t, a)
	h := f.host(t)
	require.NoError(t, h.Start(context.Background(), []app.AppIdT{"test.a"}, []Seed{{AppId: "test.a", Kind: testKind, Config: testCfg}}))
	require.NoError(t, h.Wait(context.Background()))
	assert.Equal(t, testCfg, a.cfgSeen)
	assert.Equal(t, app.LaunchReasonCaller, a.reasonSeen)

	bad := f.host(t)
	err := bad.Start(context.Background(), []app.AppIdT{"test.a"}, []Seed{{AppId: "test.a", Kind: testKind, Config: []byte("wrong")}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused")
	plainMan := headless("test.plain")
	p := &recorder{manifest: plainMan}
	g := newFixture(t, p)
	gh := g.host(t)
	err = gh.Start(context.Background(), []app.AppIdT{"test.plain"}, []Seed{{AppId: "test.plain", Kind: testKind, Config: testCfg}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LaunchKind")
}

func TestStart_Twice(t *testing.T) {
	f := newFixture(t)
	h := f.host(t)
	require.NoError(t, h.Start(context.Background(), nil, nil))
	require.Error(t, h.Start(context.Background(), nil, nil))
}

func TestProvider_RendersRows(t *testing.T) {
	a := &recorder{manifest: headless("test.a")}
	b := &recorder{manifest: headless("test.b")}
	f := newFixture(t, a, b)
	h := f.host(t)
	require.NoError(t, h.Start(context.Background(), []app.AppIdT{"test.b"}, nil))
	require.NoError(t, h.Wait(context.Background()))
	reg := introspect.NewRegistry()
	require.NoError(t, RegisterIntrospect(reg, h))
	p, ok := reg.Lookup(TableName)
	require.True(t, ok)
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	require.EqualValues(t, 2, rec.NumRows())
	assert.Equal(t, "not_selected", rec.Column(2).ValueStr(0))
	assert.Equal(t, "running", rec.Column(2).ValueStr(1))
	assert.Equal(t, "", rec.Column(4).ValueStr(0), "never started: empty, not a zero time")
	assert.NotEqual(t, "", rec.Column(4).ValueStr(1))

	empty := introspect.NewRegistry()
	require.NoError(t, RegisterIntrospect(empty, nil))
	ep, _ := empty.Lookup(TableName)
	erec, err := ep.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer erec.Release()
	assert.EqualValues(t, 0, erec.NumRows())
}

func TestPatternsOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"fs.dialog.read", "fs.dialog.read", true},
		{"fs.dialog.read", "fs.dialog.write", false},
		{"fs.dialog.read", "fs.>", true},
		{"fs.>", "fs.dialog.read", true},
		{"fs.*.read", "fs.dialog.>", true},
		{"fs.dialog", "fs.dialog.read", false},
		{"task.>", "task.*.cancel", true},
		{"", "fs.>", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, PatternsOverlap(c.a, c.b), "%s ~ %s", c.a, c.b)
	}
}

func TestUnansweredCaps(t *testing.T) {
	subs := []inprocbus.SubscriptionInfo{{Pattern: "fs.dialog.read"}, {Pattern: inprocbus.InboxPrefix + "abc"}}
	caps := []app.SubjectFilter{
		{Pattern: "fs.dialog.read", Direction: app.CapDirectionPub},
		{Pattern: "ch.local.exec.default", Direction: app.CapDirectionPub},
		{Pattern: "app.x.event.>", Direction: app.CapDirectionSub},
	}
	assert.Equal(t, []string{"ch.local.exec.default"}, UnansweredCaps(subs, caps))
}

func TestSelect_UnsetTouchesNothing(t *testing.T) {
	Where.Override("")
	t.Cleanup(Where.ClearOverride)
	ids, set, err := Select(context.Background(), nil, app.NewRegistry(), false, zerolog.Nop())
	require.NoError(t, err)
	assert.False(t, set)
	assert.Nil(t, ids)
}

func TestSelect_SetWithoutChlocalFails(t *testing.T) {
	Where.Override("true")
	t.Cleanup(Where.ClearOverride)
	_, set, err := Select(context.Background(), nil, app.NewRegistry(), false, zerolog.Nop())
	assert.True(t, set)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clickhouse-local")
}

func TestCompose(t *testing.T) {
	assert.Equal(t, "surface = 'headless' AND launchable AND (kind = 'demo')", Compose("kind = 'demo'"))
}
