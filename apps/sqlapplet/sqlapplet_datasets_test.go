package sqlapplet

import (
	"bytes"
	"context"
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
// resolve nor subscribe (NoopBus) leaves every alias pending, in poll
// mode, with the notice pushed once and not again, and no second resolve
// stacked on an in-flight one.
func TestDatasetBinderNoopBusFallsBackToPolling(t *testing.T) {
	b, bindings := newDatasetBinder(&app.NoopBus{}, zerolog.Nop(), "Capture it.", []string{"pprof_cpu"})
	require.NotNil(t, b)
	assert.Empty(t, bindings)
	assert.True(t, b.pollMode, "no events on a NoopBus")
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

	// Construction holds the first poll off for an interval — the open-time
	// resolve just missed — and sync does not stack a second resolve.
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
	assert.True(t, stillOne, "sync must not stack a second resolve on an in-flight one")
}

// TestDatasetBinderSettlesAndUnbinds walks the render-thread handoff through
// the mailbox: a parked bind lands (or, rejected, still settles rather than
// spinning forever), a retract of the bound handle unbinds and re-pends, a
// republish notifies a revision.
func TestDatasetBinderSettlesAndUnbinds(t *testing.T) {
	b, _ := newDatasetBinder(&app.NoopBus{}, zerolog.Nop(), "", []string{"items", "other"})
	require.NotNil(t, b)
	target := newRecordingTarget()
	target.rejectOn = "other"

	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpPublished, Alias: "items", Handle: "adhoc_deadbeef01234567", Revision: 1})
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpPublished, Alias: "other", Handle: "adhoc_deadbeef76543210", Revision: 1})
	bound, notice, changed := b.sync(target)
	assert.True(t, bound, "the accepted bind reports, so the caller re-runs")
	assert.True(t, changed)
	assert.Nil(t, notice, "with nothing pending the notice clears")
	assert.Equal(t, map[string]string{"items": "adhoc_deadbeef01234567"}, target.bound)
	assert.Empty(t, b.pendingAliases(), "a rejected bind settles the alias rather than retrying it forever")

	// A publish under a bound alias onto a different handle is ignored: an
	// open applet tracks re-captures through its handle (ADR-0134).
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpPublished, Alias: "items", Handle: "adhoc_other0000000000", Revision: 1})
	bound, _, changed = b.sync(target)
	assert.False(t, bound)
	assert.False(t, changed)
	assert.Equal(t, "adhoc_deadbeef01234567", target.bound["items"])

	// A republish onto the bound handle notifies the revision.
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpPublished, Alias: "items", Handle: "adhoc_deadbeef01234567", Revision: 2})
	b.sync(target)
	assert.Equal(t, uint64(2), target.revised["items"])

	// A retract of the bound handle unbinds, re-pends, and says so.
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpRetracted, Alias: "items", Handle: "adhoc_deadbeef01234567", Revision: 2})
	bound, notice, changed = b.sync(target)
	assert.False(t, bound)
	assert.True(t, changed)
	assert.Contains(t, string(notice), "`items`")
	assert.Equal(t, []string{"items"}, target.unbound)
	assert.NotContains(t, target.bound, "items")
	assert.Equal(t, []string{"items"}, b.pendingAliases())

	// A retract of an unknown handle changes nothing.
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpRetracted, Handle: "adhoc_unknown000000000"})
	_, _, changed = b.sync(target)
	assert.False(t, changed)

	// The next publish under the alias binds again — possibly a different
	// producer's dataset, which the handle tells apart.
	b.onEvent(adhocdata.Event{Op: adhocdata.EventOpPublished, Alias: "items", Handle: "adhoc_fresh00000000000", Revision: 1})
	bound, notice, _ = b.sync(target)
	assert.True(t, bound)
	assert.Nil(t, notice)
	assert.Equal(t, "adhoc_fresh00000000000", target.bound["items"])
}

// TestDatasetBinderLiveWithdrawal runs the binder against a real service
// over the in-proc bus: subscribe-before-resolve at open, `published` binds
// without polling, `retracted` unbinds, and the next publish rebinds.
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
	assert.False(t, b.pollMode, "events subscribed")
	assert.Empty(t, bindings)
	target := newRecordingTarget()

	res, err := adhocdata.PublishRequest(producer, adhocdata.PublishInput{Alias: "items", ArrowIPCStream: int64ArrowStream(t, 1, 2)})
	require.NoError(t, err)
	waitFor(t, func() bool { bound, _, _ := b.sync(target); return bound }, "published event bound the alias")
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
