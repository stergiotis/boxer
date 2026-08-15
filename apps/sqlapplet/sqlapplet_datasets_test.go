package sqlapplet

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// recordingTarget stands in for the play instance's dataset ops (which are
// exercised against a real instance in the pprof book test) and records
// what the binder drove.
type recordingTarget struct {
	bound    map[string]string
	unbound  []string
	revised  map[string]uint64
	rejectOn string // alias whose bind is rejected
}

func newRecordingTarget() *recordingTarget {
	return &recordingTarget{bound: map[string]string{}, revised: map[string]uint64{}}
}

func (r *recordingTarget) BindDataset(alias, handle string) error {
	if alias == r.rejectOn {
		return eh.Errorf("rejected")
	}
	r.bound[alias] = handle
	return nil
}

func (r *recordingTarget) UnbindDataset(alias string) error {
	delete(r.bound, alias)
	r.unbound = append(r.unbound, alias)
	return nil
}

func (r *recordingTarget) NotifyDatasetRevision(alias string, revision uint64) {
	r.revised[alias] = revision
}

// fakeResolver is the service's truth as a test sees it: which handles are
// live, and what each alias resolves to. It answers instantly and counts
// questions, so a test can drive the worker deterministically.
type fakeResolver struct {
	mu      sync.Mutex
	newest  map[string]string   // alias → newest live handle
	live    map[string]struct{} // live handles
	rev     map[string]uint64   // handle → revision
	failing bool                // transport failure on every call
	asked   int
}

func newFakeResolver() *fakeResolver {
	return &fakeResolver{newest: map[string]string{}, live: map[string]struct{}{}, rev: map[string]uint64{}}
}

// publish mints (revision 1) or republishes (revision+1) handle under alias.
func (f *fakeResolver) publish(alias, handle string) {
	f.mu.Lock()
	f.newest[alias] = handle
	f.live[handle] = struct{}{}
	f.rev[handle]++
	f.mu.Unlock()
}

func (f *fakeResolver) retract(alias, handle string) {
	f.mu.Lock()
	delete(f.live, handle)
	if f.newest[alias] == handle {
		delete(f.newest, alias)
	}
	f.mu.Unlock()
}

func (f *fakeResolver) setFailing(v bool) {
	f.mu.Lock()
	f.failing = v
	f.mu.Unlock()
}

func (f *fakeResolver) questions() (n int) {
	f.mu.Lock()
	n = f.asked
	f.mu.Unlock()
	return
}

func (f *fakeResolver) resolveVerify(alias string, boundHandle string) (handle string, revision uint64, boundLive bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked++
	if f.failing {
		err = eh.Errorf("transport down")
		return
	}
	handle = f.newest[alias]
	revision = f.rev[handle]
	if boundHandle != "" {
		_, boundLive = f.live[boundHandle]
	}
	return
}

// settle drives sync until the worker has nothing in flight and no verdict
// or event is pending — the render-thread loop at test speed.
func settle(t *testing.T, b *datasetBinder, target datasetTargetI) (bound bool, notice []byte, changed bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		bd, n, ch := b.sync(target)
		bound = bound || bd
		notice = n
		changed = changed || ch
		b.mu.Lock()
		idle := !b.inFlight && len(b.verdicts) == 0 && len(b.events) == 0
		b.mu.Unlock()
		if idle {
			return
		}
		require.True(t, time.Now().Before(deadline), "binder did not settle")
		time.Sleep(2 * time.Millisecond)
	}
}

// due forces the reconcile tick on the next sync.
func due(b *datasetBinder) {
	b.mu.Lock()
	b.nextAt = time.Time{}
	b.mu.Unlock()
}

// TestRenderDatasetNotice pins the text the applet shows over empty panes:
// the alias, because that is what the failing query and the catalog name,
// and the author's hint, because nothing else can say how to produce it.
func TestRenderDatasetNotice(t *testing.T) {
	assert.Nil(t, renderDatasetNotice(nil, "hint"), "a cleared condition renders nothing")

	one := string(renderDatasetNotice([]string{"pprof_cpu"}, "Capture one from imzrt → Profiles → Capture CPU."))
	assert.Contains(t, one, "Waiting for dataset `pprof_cpu`")
	assert.Contains(t, one, "Capture one from imzrt")
	assert.Contains(t, one, "no need to reopen")

	many := string(renderDatasetNotice([]string{"a", "b"}, ""))
	assert.Contains(t, many, "Waiting for datasets `a`, `b`")
}

// TestNewDatasetBinderAbsent covers the cases that must not allocate a
// binder: nothing declared, or no bus to bind against.
func TestNewDatasetBinderAbsent(t *testing.T) {
	b, bindings := newDatasetBinder(&app.NoopBus{}, zerolog.Nop(), "", nil)
	assert.Nil(t, b)
	assert.Nil(t, bindings)
	b, bindings = newDatasetBinder(nil, zerolog.Nop(), "", []string{"items"})
	assert.Nil(t, b)
	assert.Nil(t, bindings)
}

// TestDatasetBinderNoopBusFallsBackToPolling: a bus that can neither
// resolve nor subscribe (NoopBus) leaves every alias pending, at the poll
// interval, with the notice pushed once and not again, and no second round
// stacked on an in-flight one.
func TestDatasetBinderNoopBusFallsBackToPolling(t *testing.T) {
	b, bindings := newDatasetBinder(&app.NoopBus{}, zerolog.Nop(), "Capture it.", []string{"pprof_cpu"})
	require.NotNil(t, b)
	assert.Empty(t, bindings)
	assert.Nil(t, b.unsub, "no events on a NoopBus")
	assert.Equal(t, datasetRetryInterval, b.interval, "polls at the seconds-scale interval")
	assert.Equal(t, []string{"pprof_cpu"}, b.pendingAliases())

	target := newRecordingTarget()
	// The first sync hands over the notice built at construction, so the
	// window explains itself on the frame after Mount.
	bound, notice, changed := b.sync(target)
	assert.False(t, bound)
	assert.True(t, changed, "the first frame must push the notice")
	assert.Contains(t, string(notice), "`pprof_cpu`")
	assert.Contains(t, string(notice), "Capture it.")

	// Re-set only on change: SetDatasetNotice reparses, and sync runs every
	// frame.
	_, _, changed = b.sync(target)
	assert.False(t, changed, "an unchanged notice must not be re-pushed")

	// Construction holds the first tick off for an interval — the open-time
	// resolve just asked — and sync does not stack a second round.
	b.mu.Lock()
	notYet := time.Now().Before(b.nextAt)
	b.nextAt = time.Time{}
	b.inFlight = true // pretend a worker is out
	b.mu.Unlock()
	assert.True(t, notYet)
	b.sync(target)
	b.mu.Lock()
	stillOne := b.inFlight
	b.mu.Unlock()
	assert.True(t, stillOne, "sync must not stack a second round on an in-flight one")
}

// TestDatasetBinderHintsResolveThenBind: a `published` hint under a pending
// alias does not bind the event's handle — it asks the service and binds the
// answer; a hint onto the bound handle notifies the revision; a hint under a
// bound alias onto a different handle is ignored; a `retracted` unbinds and
// re-pends; a rejected bind settles rather than spinning.
func TestDatasetBinderHintsResolveThenBind(t *testing.T) {
	f := newFakeResolver()
	b := newDatasetBinderWith(f, zerolog.Nop(), "")
	b.seed(nil, []string{"items", "other"})
	target := newRecordingTarget()
	target.rejectOn = "other"

	// The hint names h1; the service's truth is h1 too.
	f.publish("items", "adhoc_h1000000000000000")
	f.publish("other", "adhoc_o1000000000000000")
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpPublished, Alias: "items", Handle: "adhoc_h1000000000000000", Revision: 1})
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpPublished, Alias: "other", Handle: "adhoc_o1000000000000000", Revision: 1})
	bound, notice, changed := settle(t, b, target)
	assert.True(t, bound, "the accepted bind reports, so the caller re-runs")
	assert.True(t, changed)
	assert.Nil(t, notice, "with nothing pending the notice clears")
	assert.Equal(t, map[string]string{"items": "adhoc_h1000000000000000"}, target.bound)
	assert.Empty(t, b.pendingAliases(), "a rejected bind settles the alias rather than retrying it forever")
	assert.Equal(t, 2, f.questions(), "one question per hinted alias")

	// A stale hint (a handle the service does not know) binds what the
	// service says, not what the hint says.
	f.retract("items", "adhoc_h1000000000000000")
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpRetracted, Alias: "items", Handle: "adhoc_h1000000000000000"})
	settle(t, b, target)
	require.Equal(t, []string{"items"}, b.pendingAliases())
	f.publish("items", "adhoc_h2000000000000000")
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpPublished, Alias: "items", Handle: "adhoc_stale00000000000", Revision: 1})
	bound, _, _ = settle(t, b, target)
	assert.True(t, bound)
	assert.Equal(t, "adhoc_h2000000000000000", target.bound["items"], "the answer binds, not the hint")

	// A publish under a bound alias onto a different handle is ignored: an
	// open applet tracks re-captures through its handle (ADR-0134).
	f.publish("items", "adhoc_h3000000000000000")
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpPublished, Alias: "items", Handle: "adhoc_h3000000000000000", Revision: 1})
	bound, _, changed = settle(t, b, target)
	assert.False(t, bound)
	assert.False(t, changed)
	assert.Equal(t, "adhoc_h2000000000000000", target.bound["items"])

	// A republish onto the bound handle notifies the revision.
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpPublished, Alias: "items", Handle: "adhoc_h2000000000000000", Revision: 2})
	settle(t, b, target)
	assert.Equal(t, uint64(2), target.revised["items"])

	// A retract of a handle nobody holds is a no-op.
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpRetracted, Alias: "items", Handle: "adhoc_unknown000000000"})
	_, _, changed = settle(t, b, target)
	assert.False(t, changed)
}

// TestDatasetBinderReconcileCatchesLostEvents: with no events at all (as
// after a lost `published` or `retracted`), the tick alone binds a pending
// alias, replaces a binding whose handle has left with its successor, and
// re-pends a binding with no successor.
func TestDatasetBinderReconcileCatchesLostEvents(t *testing.T) {
	f := newFakeResolver()
	b := newDatasetBinderWith(f, zerolog.Nop(), "")
	b.seed(nil, []string{"items"})
	target := newRecordingTarget()

	b.sync(target) // consume the first-frame notice push

	// Lost `published`: nothing arrives, the tick binds.
	f.publish("items", "adhoc_h1000000000000000")
	_, _, changed := settle(t, b, target)
	assert.False(t, changed, "before the tick nothing moves")
	due(b)
	bound, notice, _ := settle(t, b, target)
	assert.True(t, bound)
	assert.Nil(t, notice)
	assert.Equal(t, "adhoc_h1000000000000000", target.bound["items"])

	// Lost `retracted` with a successor: the tick swaps the binding.
	f.retract("items", "adhoc_h1000000000000000")
	f.publish("items", "adhoc_h2000000000000000")
	due(b)
	bound, notice, _ = settle(t, b, target)
	assert.True(t, bound)
	assert.Nil(t, notice)
	assert.Equal(t, "adhoc_h2000000000000000", target.bound["items"])
	assert.Equal(t, []string{"items"}, target.unbound)

	// A newer sibling while ours is live: the tick keeps the binding.
	f.publish("items", "adhoc_h3000000000000000")
	due(b)
	bound, _, changed = settle(t, b, target)
	assert.False(t, bound)
	assert.False(t, changed)
	assert.Equal(t, "adhoc_h2000000000000000", target.bound["items"])

	// Lost `retracted` with no successor: the tick re-pends and says so.
	f.retract("items", "adhoc_h2000000000000000")
	f.retract("items", "adhoc_h3000000000000000")
	due(b)
	_, notice, changed = settle(t, b, target)
	assert.True(t, changed)
	assert.Contains(t, string(notice), "`items`")
	assert.Equal(t, []string{"items"}, b.pendingAliases())

	// A lost republish hint (same handle, revision 2): the tick notifies
	// the revision. A revision first learned by the tick is only recorded.
	f.publish("items", "adhoc_h5000000000000000")
	due(b)
	settle(t, b, target)
	require.Equal(t, "adhoc_h5000000000000000", target.bound["items"])
	assert.Empty(t, target.revised)
	f.publish("items", "adhoc_h5000000000000000") // revision 2, no hint
	due(b)
	_, _, changed = settle(t, b, target)
	assert.False(t, changed, "a republish moves no binding")
	assert.Equal(t, uint64(2), target.revised["items"], "the tick notified the revision")
	f.retract("items", "adhoc_h5000000000000000")
	due(b)
	settle(t, b, target)
	require.Equal(t, []string{"items"}, b.pendingAliases())

	// A transport failure changes nothing and is retried next tick.
	f.setFailing(true)
	f.publish("items", "adhoc_h4000000000000000")
	due(b)
	_, _, changed = settle(t, b, target)
	assert.False(t, changed)
	f.setFailing(false)
	due(b)
	bound, _, _ = settle(t, b, target)
	assert.True(t, bound)
	assert.Equal(t, "adhoc_h4000000000000000", target.bound["items"])
}

// TestDatasetBinderOrderWithinOneFrame pins that hints are replayed in
// arrival order: a retract of the bound handle followed by a publish of the
// alias's successor — both landing between two frames — ends bound to the
// successor, and the reverse order (publish, then retract of that same
// handle) ends pending.
func TestDatasetBinderOrderWithinOneFrame(t *testing.T) {
	f := newFakeResolver()
	b := newDatasetBinderWith(f, zerolog.Nop(), "")
	b.seed(map[string]string{"items": "adhoc_h1000000000000000"}, nil)
	f.publish("items", "adhoc_h1000000000000000")
	target := newRecordingTarget()
	require.NoError(t, target.BindDataset("items", "adhoc_h1000000000000000"))

	// retract h1, publish h2 — one frame.
	f.retract("items", "adhoc_h1000000000000000")
	f.publish("items", "adhoc_h2000000000000000")
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpRetracted, Alias: "items", Handle: "adhoc_h1000000000000000"})
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpPublished, Alias: "items", Handle: "adhoc_h2000000000000000", Revision: 1})
	bound, notice, changed := settle(t, b, target)
	assert.True(t, bound)
	assert.True(t, changed)
	assert.Nil(t, notice, "ends bound, nothing pending")
	assert.Equal(t, "adhoc_h2000000000000000", target.bound["items"])
	assert.Equal(t, []string{"items"}, target.unbound)
	assert.Empty(t, b.pendingAliases())

	// publish h3 under a bound alias (ignored), then retract h2 — one frame.
	f.publish("items", "adhoc_h3000000000000000")
	f.retract("items", "adhoc_h2000000000000000")
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpPublished, Alias: "items", Handle: "adhoc_h3000000000000000", Revision: 1})
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpRetracted, Alias: "items", Handle: "adhoc_h2000000000000000"})
	bound, notice, changed = settle(t, b, target)
	assert.False(t, bound)
	assert.True(t, changed)
	assert.Contains(t, string(notice), "`items`", "ends pending: h3 was a sibling, not our handle")
	assert.Equal(t, []string{"items"}, b.pendingAliases())
}

// TestDatasetBinderLiveWithdrawal runs the binder against a real service
// over the in-proc bus: subscribe-before-resolve at open, `published` binds
// (via the resolve the hint triggers), `retracted` unbinds, the next publish
// rebinds — and, events aside, the reconcile tick alone recovers a binding.
func TestDatasetBinderLiveWithdrawal(t *testing.T) {
	logger := zerolog.Nop()
	bus := inprocbus.NewInst(logger)
	svc, err := adhocdata.NewService(adhocdata.Config{
		Bus: bus, Registry: introspect.NewRegistry(), Keys: fakeKeyRegistrar{}, Dir: t.TempDir(), Log: logger,
		RetractGrace: 50 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	producer := bus.NewClient("test.producer", []app.SubjectFilter{
		{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"},
	})
	appletBus := bus.NewClient("test.applet", []app.SubjectFilter{
		{Pattern: adhocdata.SubjectResolve, Direction: app.CapDirectionPub, Reason: "test"},
		{Pattern: adhocdata.SubjectEventAll, Direction: app.CapDirectionSub, Reason: "test"},
	})

	b, bindings := newDatasetBinder(appletBus, logger, "hint", []string{"items"})
	require.NotNil(t, b)
	t.Cleanup(b.close)
	assert.NotNil(t, b.unsub, "events subscribed")
	assert.Equal(t, datasetReconcileInterval, b.interval)
	assert.Empty(t, bindings)
	target := newRecordingTarget()

	res, err := adhocdata.PublishRequest(producer, adhocdata.PublishInput{Alias: "items", ArrowIPCStream: int64ArrowStream(t, 1, 2)})
	require.NoError(t, err)
	waitFor(t, func() bool { bound, _, _ := b.sync(target); return bound }, "published hint resolved and bound the alias")
	assert.Equal(t, res.Handle, target.bound["items"])

	require.NoError(t, adhocdata.RetractRequest(producer, res.Handle))
	waitFor(t, func() bool { _, _, changed := b.sync(target); return changed }, "retracted event unbound the alias")
	assert.Equal(t, []string{"items"}, target.unbound)
	assert.Equal(t, []string{"items"}, b.pendingAliases())

	res2, err := adhocdata.PublishRequest(producer, adhocdata.PublishInput{Alias: "items", ArrowIPCStream: int64ArrowStream(t, 3)})
	require.NoError(t, err)
	waitFor(t, func() bool { bound, _, _ := b.sync(target); return bound }, "the next publish rebound the alias")
	assert.Equal(t, res2.Handle, target.bound["items"])
	assert.NotEqual(t, res.Handle, res2.Handle)

	// Simulate a lost `retracted`: drop the events subscription, retract,
	// publish a successor, then let the tick recover the binding.
	b.close()
	require.NoError(t, adhocdata.RetractRequest(producer, res2.Handle))
	res3, err := adhocdata.PublishRequest(producer, adhocdata.PublishInput{Alias: "items", ArrowIPCStream: int64ArrowStream(t, 4)})
	require.NoError(t, err)
	_, _, changed := b.sync(target)
	assert.False(t, changed, "no event arrived; still bound to the dead handle")
	due(b)
	waitFor(t, func() bool { bound, _, _ := b.sync(target); return bound }, "the reconcile tick swapped the binding")
	assert.Equal(t, res3.Handle, target.bound["items"])
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting: %s", what)
}

// fakeKeyRegistrar satisfies adhocdata.KeyRegistrarI for a service that never
// decrypts in these tests.
type fakeKeyRegistrar struct{}

func (fakeKeyRegistrar) RegisterDatasetKey(string, []byte) {}
func (fakeKeyRegistrar) DeregisterDatasetKey(string)       {}

// int64ArrowStream builds a one-column Int64 Arrow IPC stream.
func int64ArrowStream(t *testing.T, vals ...int64) []byte {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Int64Builder).AppendValues(vals, nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	return buf.Bytes()
}
