package adhocdata

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// recordingTarget stands in for a consumer's dataset ops and records what
// the follower drove.
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

// settle drives Sync until the worker has nothing in flight and no verdict
// or event is pending — the caller's loop at test speed.
func settle(t *testing.T, f *Follower, target TargetI) (bound bool, changed bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		bd, ch := f.Sync(target)
		bound = bound || bd
		changed = changed || ch
		f.mu.Lock()
		idle := !f.inFlight && len(f.verdicts) == 0 && len(f.events) == 0
		f.mu.Unlock()
		if idle {
			return
		}
		require.True(t, time.Now().Before(deadline), "follower did not settle")
		time.Sleep(2 * time.Millisecond)
	}
}

// due forces the reconcile tick on the next Sync.
func due(f *Follower) {
	f.mu.Lock()
	f.nextAt = time.Time{}
	f.mu.Unlock()
}

// TestNewFollowerAbsent covers the cases that must not allocate a
// follower: nothing declared, or no bus to bind against.
func TestNewFollowerAbsent(t *testing.T) {
	f, bindings := NewFollower(FollowerConfig{Bus: &app.NoopBus{}, Log: zerolog.Nop()})
	assert.Nil(t, f)
	assert.Nil(t, bindings)
	f, bindings = NewFollower(FollowerConfig{Log: zerolog.Nop(), Aliases: []string{"items"}})
	assert.Nil(t, f)
	assert.Nil(t, bindings)
}

// TestFollowerNoopBusFallsBackToPolling: a bus that can neither resolve nor
// subscribe (NoopBus) leaves every alias pending, at the poll interval, with
// the pending set reported once and not again, and no second round stacked
// on an in-flight one.
func TestFollowerNoopBusFallsBackToPolling(t *testing.T) {
	f, bindings := NewFollower(FollowerConfig{Bus: &app.NoopBus{}, Log: zerolog.Nop(), Aliases: []string{"pprof_cpu"}})
	require.NotNil(t, f)
	assert.Empty(t, bindings)
	assert.Nil(t, f.unsub, "no events on a NoopBus")
	assert.Equal(t, DefaultPollInterval, f.interval, "polls at the seconds-scale interval")
	assert.Equal(t, []string{"pprof_cpu"}, f.Pending())

	target := newRecordingTarget()
	// The first Sync reports the pending set built at construction, so the
	// consumer explains itself on the frame after Mount.
	bound, changed := f.Sync(target)
	assert.False(t, bound)
	assert.True(t, changed, "the first call must report the pending set")
	_, changed = f.Sync(target)
	assert.False(t, changed, "an unchanged pending set is not re-reported")

	// Construction holds the first tick off for an interval — the open-time
	// resolve just asked — and Sync does not stack a second round.
	f.mu.Lock()
	notYet := time.Now().Before(f.nextAt)
	f.nextAt = time.Time{}
	f.inFlight = true // pretend a worker is out
	f.mu.Unlock()
	assert.True(t, notYet)
	f.Sync(target)
	f.mu.Lock()
	stillOne := f.inFlight
	f.mu.Unlock()
	assert.True(t, stillOne, "Sync must not stack a second round on an in-flight one")
}

// TestFollowerHintsResolveThenBind: a `published` hint under a pending alias
// does not bind the event's handle — it asks the service and binds the
// answer; a hint onto the bound handle notifies the revision; a hint under a
// bound alias onto a different handle is ignored; a `retracted` unbinds and
// re-pends; a rejected bind settles rather than spinning.
func TestFollowerHintsResolveThenBind(t *testing.T) {
	r := newFakeResolver()
	f := newFollowerWith(r, zerolog.Nop())
	f.seed(nil, []string{"items", "other"})
	target := newRecordingTarget()
	target.rejectOn = "other"

	r.publish("items", "adhoc_h1000000000000000")
	r.publish("other", "adhoc_o1000000000000000")
	f.onEvent(Event{Op: EventOpPublished, Alias: "items", Handle: "adhoc_h1000000000000000", Revision: 1})
	f.onEvent(Event{Op: EventOpPublished, Alias: "other", Handle: "adhoc_o1000000000000000", Revision: 1})
	bound, changed := settle(t, f, target)
	assert.True(t, bound, "the accepted bind reports, so the caller re-runs")
	assert.True(t, changed)
	assert.Empty(t, f.Pending(), "a rejected bind settles the alias rather than retrying it forever")
	assert.Equal(t, map[string]string{"items": "adhoc_h1000000000000000"}, target.bound)
	assert.Equal(t, 2, r.questions(), "one question per hinted alias")

	// A stale hint (a handle the service does not know) binds what the
	// service says, not what the hint says.
	r.retract("items", "adhoc_h1000000000000000")
	f.onEvent(Event{Op: EventOpRetracted, Alias: "items", Handle: "adhoc_h1000000000000000"})
	settle(t, f, target)
	require.Equal(t, []string{"items"}, f.Pending())
	r.publish("items", "adhoc_h2000000000000000")
	f.onEvent(Event{Op: EventOpPublished, Alias: "items", Handle: "adhoc_stale00000000000", Revision: 1})
	bound, _ = settle(t, f, target)
	assert.True(t, bound)
	assert.Equal(t, "adhoc_h2000000000000000", target.bound["items"], "the answer binds, not the hint")

	// A publish under a bound alias onto a different handle is ignored: an
	// open consumer tracks re-captures through its handle.
	r.publish("items", "adhoc_h3000000000000000")
	f.onEvent(Event{Op: EventOpPublished, Alias: "items", Handle: "adhoc_h3000000000000000", Revision: 1})
	bound, changed = settle(t, f, target)
	assert.False(t, bound)
	assert.False(t, changed)
	assert.Equal(t, "adhoc_h2000000000000000", target.bound["items"])

	// A republish onto the bound handle notifies the revision.
	f.onEvent(Event{Op: EventOpPublished, Alias: "items", Handle: "adhoc_h2000000000000000", Revision: 2})
	settle(t, f, target)
	assert.Equal(t, uint64(2), target.revised["items"])

	// A retract of a handle nobody holds is a no-op.
	f.onEvent(Event{Op: EventOpRetracted, Alias: "items", Handle: "adhoc_unknown000000000"})
	_, changed = settle(t, f, target)
	assert.False(t, changed)
}

// TestFollowerReconcileCatchesLostEvents: with no events at all (as after a
// lost `published` or `retracted`), the tick alone binds a pending alias,
// replaces a binding whose handle has left with its successor, and re-pends
// a binding with no successor.
func TestFollowerReconcileCatchesLostEvents(t *testing.T) {
	r := newFakeResolver()
	f := newFollowerWith(r, zerolog.Nop())
	f.seed(nil, []string{"items"})
	target := newRecordingTarget()
	f.Sync(target) // consume the first-call pending report

	// Lost `published`: nothing arrives, the tick binds.
	r.publish("items", "adhoc_h1000000000000000")
	_, changed := settle(t, f, target)
	assert.False(t, changed, "before the tick nothing moves")
	due(f)
	bound, _ := settle(t, f, target)
	assert.True(t, bound)
	assert.Empty(t, f.Pending())
	assert.Equal(t, "adhoc_h1000000000000000", target.bound["items"])

	// Lost `retracted` with a successor: the tick swaps the binding.
	r.retract("items", "adhoc_h1000000000000000")
	r.publish("items", "adhoc_h2000000000000000")
	due(f)
	bound, _ = settle(t, f, target)
	assert.True(t, bound)
	assert.Empty(t, f.Pending())
	assert.Equal(t, "adhoc_h2000000000000000", target.bound["items"])
	assert.Equal(t, []string{"items"}, target.unbound)

	// A newer sibling while ours is live: the tick keeps the binding.
	r.publish("items", "adhoc_h3000000000000000")
	due(f)
	bound, changed = settle(t, f, target)
	assert.False(t, bound)
	assert.False(t, changed)
	assert.Equal(t, "adhoc_h2000000000000000", target.bound["items"])

	// Lost `retracted` with no successor: the tick re-pends and says so.
	r.retract("items", "adhoc_h2000000000000000")
	r.retract("items", "adhoc_h3000000000000000")
	due(f)
	_, changed = settle(t, f, target)
	assert.True(t, changed)
	assert.Equal(t, []string{"items"}, f.Pending())

	// A lost republish hint (same handle, revision 2): the tick notifies the
	// revision. A revision first learned by the tick is only recorded.
	r.publish("items", "adhoc_h5000000000000000")
	due(f)
	settle(t, f, target)
	require.Equal(t, "adhoc_h5000000000000000", target.bound["items"])
	assert.Empty(t, target.revised)
	r.publish("items", "adhoc_h5000000000000000") // revision 2, no hint
	due(f)
	_, changed = settle(t, f, target)
	assert.False(t, changed, "a republish moves no binding")
	assert.Equal(t, uint64(2), target.revised["items"], "the tick notified the revision")
	r.retract("items", "adhoc_h5000000000000000")
	due(f)
	settle(t, f, target)
	require.Equal(t, []string{"items"}, f.Pending())

	// A transport failure changes nothing and is retried next tick.
	r.setFailing(true)
	r.publish("items", "adhoc_h4000000000000000")
	due(f)
	_, changed = settle(t, f, target)
	assert.False(t, changed)
	r.setFailing(false)
	due(f)
	bound, _ = settle(t, f, target)
	assert.True(t, bound)
	assert.Equal(t, "adhoc_h4000000000000000", target.bound["items"])
}

// TestFollowerOrderWithinOneFrame pins that hints are replayed in arrival
// order: a retract of the bound handle followed by a publish of the alias's
// successor — both landing between two frames — ends bound to the
// successor, and the reverse order (publish, then retract of that same
// handle) ends pending.
func TestFollowerOrderWithinOneFrame(t *testing.T) {
	r := newFakeResolver()
	f := newFollowerWith(r, zerolog.Nop())
	f.seed(map[string]string{"items": "adhoc_h1000000000000000"}, nil)
	r.publish("items", "adhoc_h1000000000000000")
	target := newRecordingTarget()
	require.NoError(t, target.BindDataset("items", "adhoc_h1000000000000000"))

	// retract h1, publish h2 — one frame.
	r.retract("items", "adhoc_h1000000000000000")
	r.publish("items", "adhoc_h2000000000000000")
	f.onEvent(Event{Op: EventOpRetracted, Alias: "items", Handle: "adhoc_h1000000000000000"})
	f.onEvent(Event{Op: EventOpPublished, Alias: "items", Handle: "adhoc_h2000000000000000", Revision: 1})
	bound, changed := settle(t, f, target)
	assert.True(t, bound)
	assert.True(t, changed)
	assert.Empty(t, f.Pending(), "ends bound, nothing pending")
	assert.Equal(t, "adhoc_h2000000000000000", target.bound["items"])
	assert.Equal(t, []string{"items"}, target.unbound)

	// publish h3 under a bound alias (ignored), then retract h2 — one frame.
	r.publish("items", "adhoc_h3000000000000000")
	r.retract("items", "adhoc_h2000000000000000")
	f.onEvent(Event{Op: EventOpPublished, Alias: "items", Handle: "adhoc_h3000000000000000", Revision: 1})
	f.onEvent(Event{Op: EventOpRetracted, Alias: "items", Handle: "adhoc_h2000000000000000"})
	bound, changed = settle(t, f, target)
	assert.False(t, bound)
	assert.True(t, changed)
	assert.Equal(t, []string{"items"}, f.Pending(), "ends pending: h3 was a sibling, not our handle")
}

// TestFollowerLiveWithdrawal runs the follower against a real service over
// the in-proc bus: subscribe-before-resolve at open, `published` binds (via
// the resolve the hint triggers), `retracted` unbinds, the next publish
// rebinds — and, events aside, the reconcile tick alone recovers a binding.
func TestFollowerLiveWithdrawal(t *testing.T) {
	logger := zerolog.Nop()
	bus := inprocbus.NewInst(logger)
	svc, err := NewService(Config{
		Bus: bus, Registry: introspect.NewRegistry(), Dir: t.TempDir(), Log: logger,
		RetractGrace: 50 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	producer := bus.NewClient("test.producer", []app.SubjectFilter{
		{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"},
	})
	consumer := bus.NewClient("test.consumer", []app.SubjectFilter{
		{Pattern: SubjectResolve, Direction: app.CapDirectionPub, Reason: "test"},
		{Pattern: SubjectEventAll, Direction: app.CapDirectionSub, Reason: "test"},
	})

	f, bindings := NewFollower(FollowerConfig{Bus: consumer, Log: logger, Aliases: []string{"items"}})
	require.NotNil(t, f)
	t.Cleanup(f.Close)
	assert.NotNil(t, f.unsub, "events subscribed")
	assert.Equal(t, DefaultReconcileInterval, f.interval)
	assert.Empty(t, bindings)
	target := newRecordingTarget()

	res, err := PublishRequest(producer, PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 1, 2)})
	require.NoError(t, err)
	waitFor(t, func() bool { bound, _ := f.Sync(target); return bound }, "published hint resolved and bound the alias")
	assert.Equal(t, res.Handle, target.bound["items"])

	require.NoError(t, RetractRequest(producer, res.Handle))
	waitFor(t, func() bool { _, changed := f.Sync(target); return changed }, "retracted event unbound the alias")
	assert.Equal(t, []string{"items"}, target.unbound)
	assert.Equal(t, []string{"items"}, f.Pending())

	res2, err := PublishRequest(producer, PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 3)})
	require.NoError(t, err)
	waitFor(t, func() bool { bound, _ := f.Sync(target); return bound }, "the next publish rebound the alias")
	assert.Equal(t, res2.Handle, target.bound["items"])
	assert.NotEqual(t, res.Handle, res2.Handle)

	// Simulate a lost `retracted`: drop the events subscription, retract,
	// publish a successor, then let the tick recover the binding.
	f.Close()
	require.NoError(t, RetractRequest(producer, res2.Handle))
	res3, err := PublishRequest(producer, PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 4)})
	require.NoError(t, err)
	_, changed := f.Sync(target)
	assert.False(t, changed, "no event arrived; still bound to the dead handle")
	due(f)
	waitFor(t, func() bool { bound, _ := f.Sync(target); return bound }, "the reconcile tick swapped the binding")
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
