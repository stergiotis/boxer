package adhocdemo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/sqlapplet"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadhoc"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/playground"
)

// opsFixture is a window without a bus whose publishes land through the
// seams: the bundle publish answers at once, the tree publish answers
// with a fixed mount, and either fails when told to.
type opsFixture struct {
	inst       *App
	publishErr error
	treeErr    error
	published  []int
}

func newOpsFixture(t *testing.T) (f *opsFixture) {
	t.Helper()
	f = &opsFixture{inst: newApp()}
	f.inst.log = zerolog.Nop()
	f.inst.bundle = adhocdata.WindowAlias(bundleBase, 3)
	f.inst.publish = func(spec playground.BundleSpec) (res adhocdata.BundleResult, err error) {
		if f.publishErr != nil {
			return res, f.publishErr
		}
		f.published = append(f.published, len(spec.Datasets))
		return adhocdata.BundleResult{Alias: spec.Alias, Revision: uint64(len(f.published))}, nil
	}
	f.inst.publishTreeFn = func(ctx context.Context, gen int, mount identifier.TaggedId) (res ladingadhoc.PublishResult, err error) {
		if f.treeErr != nil {
			return res, f.treeErr
		}
		return ladingadhoc.PublishResult{Mount: identifier.TaggedId(0x3BFE363BCF148002), Entries: 7, ExpiresAt: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)}, nil
	}
	return
}

// settle runs the runner's landing and the frame's takeResults until the
// runner is idle, then returns the settle's answer.
func (f *opsFixture) settle(t *testing.T, call app.OperationCall, op string) (settled bool, raw []byte, err error) {
	t.Helper()
	require.Eventually(t, func() bool {
		return !f.inst.bundleJob.Running() && !f.inst.treeJob.Running()
	}, 3*time.Second, 5*time.Millisecond)
	f.inst.takeResults()
	return f.inst.Operations().(app.OperationsAsyncI).SettleAsync(call, op, nil)
}

func query[Out any](t *testing.T, inst *App, name string) (out Out) {
	t.Helper()
	raw, err := inst.Operations().Snapshot().Query(name, nil)
	require.NoError(t, err)
	out, err = buscodec.Decode[Out](raw)
	require.NoError(t, err)
	return
}

func command(t *testing.T, inst *App, call app.OperationCall, name string) (err error) {
	t.Helper()
	args, encErr := buscodec.Encode(appops.None{})
	require.NoError(t, encErr)
	_, err = inst.Operations().ApplyCommand(call, name, args)
	return
}

func refusal(err error) (r *app.OperationRefusal) {
	errors.As(err, &r)
	return
}

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	regen, found := m.Operations.Lookup(opRegenerate)
	require.True(t, found)
	assert.True(t, regen.Async)
	assert.Equal(t, app.OperationEffectDocument, regen.Effect)
	tree, found := m.Operations.Lookup(opPublishTree)
	require.True(t, found)
	assert.True(t, tree.Async)
	assert.Equal(t, app.OperationEffectConsequential, tree.Effect, "a snapshot in the store and a tally window are outside this window")
	view, found := m.Operations.Lookup(opGetView)
	require.True(t, found)
	assert.Equal(t, app.OperationClassQuery, view.Class)
}

// Before the first publish lands regenerate is refused and unavailable
// with what the window waits on; get_view says the same.
func TestRegenerateWaitsForTheFirstPublish(t *testing.T) {
	f := newOpsFixture(t)
	v := query[View](t, f.inst, opGetView)
	assert.Equal(t, -1, v.Published)
	assert.Equal(t, 0, v.Generation)
	assert.Equal(t, f.inst.bundle, v.Bundle)
	ok, reason := f.inst.Operations().Snapshot().Available(opRegenerate)
	assert.False(t, ok)
	assert.Equal(t, notPublishedReason, reason)
	err := command(t, f.inst, app.OperationCall{Key: "c1"}, opRegenerate)
	require.NotNil(t, refusal(err))
	assert.Equal(t, notPublishedReason, refusal(err).Reason)
	assert.Empty(t, f.inst.pendingGen, "a refused call waits for nothing")
}

// A regenerate asked by an agent settles when the republish lands, with
// the generation it asked for; a repeat raises the generation again; a
// failed publish settles the call with the failure.
func TestRegenerateSettlesWhenTheRepublishLands(t *testing.T) {
	f := newOpsFixture(t)
	// The first publish, as Mount starts it; without a bus the view is not
	// built, so the fixture stands in for it.
	require.True(t, f.inst.startPublish())
	require.Eventually(t, func() bool { return !f.inst.bundleJob.Running() }, 3*time.Second, 5*time.Millisecond)
	f.inst.takeResults()
	assert.Equal(t, 0, f.inst.publishedGen)
	f.inst.view = &viewStandIn
	defer func() { f.inst.view = nil }()

	call := app.OperationCall{Writer: "task:t", Key: "c1"}
	require.NoError(t, command(t, f.inst, call, opRegenerate))
	assert.Equal(t, 1, f.inst.gen)
	settled, _, err := f.inst.Operations().(app.OperationsAsyncI).SettleAsync(call, opRegenerate, nil)
	assert.False(t, settled, "pending until the publish lands")
	require.NoError(t, err)
	ok, reason := f.inst.Operations().Snapshot().Available(opRegenerate)
	if f.inst.bundleJob.Running() {
		assert.False(t, ok)
		assert.Equal(t, publishingReason, reason)
	}

	settled, raw, err := f.settle(t, call, opRegenerate)
	require.NoError(t, err)
	require.True(t, settled)
	out, err := buscodec.Decode[RegenerateOutcome](raw)
	require.NoError(t, err)
	assert.Equal(t, RegenerateOutcome{Generation: 1, Bundle: f.inst.bundle}, out)
	assert.Equal(t, []int{1, 1}, f.published, "two publishes, one dataset each")
	assert.Empty(t, f.inst.pendingGen)

	settled, _, err = f.inst.Operations().(app.OperationsAsyncI).SettleAsync(app.OperationCall{Key: "never"}, opRegenerate, nil)
	assert.True(t, settled)
	assert.NotNil(t, refusal(err))

	f.publishErr = errors.New("the service is away")
	bad := app.OperationCall{Key: "c2"}
	require.NoError(t, command(t, f.inst, bad, opRegenerate))
	settled, _, err = f.settle(t, bad, opRegenerate)
	assert.True(t, settled)
	assert.ErrorContains(t, err, "the service is away")
	assert.Equal(t, 1, f.inst.publishedGen, "a failed publish lands no generation")
	v := query[View](t, f.inst, opGetView)
	assert.Equal(t, 2, v.Generation)
	assert.Equal(t, 1, v.Published)
	assert.Contains(t, v.PublishError, "the service is away")
}

// viewStandIn is a bundle view the tests do not draw; the handler only
// asks whether the window has one, and reads a revision of 0 from it.
var viewStandIn sqlapplet.BundleView

// publish_tree settles with the landed snapshot, writes the mount the next
// publish reuses, and settles a failure as the error.
func TestPublishTreeSettlesWithTheSnapshot(t *testing.T) {
	f := newOpsFixture(t)
	call := app.OperationCall{Writer: "task:t", Key: "t1"}
	require.NoError(t, command(t, f.inst, call, opPublishTree))
	assert.Equal(t, 1, f.inst.treeGen)
	settled, raw, err := f.settle(t, call, opPublishTree)
	require.NoError(t, err)
	require.True(t, settled)
	out, err := buscodec.Decode[PublishTreeOutcome](raw)
	require.NoError(t, err)
	assert.Equal(t, 1, out.Generation)
	assert.EqualValues(t, 7, out.Entries)
	assert.Equal(t, f.inst.treeValue().Mount, out.Mount)
	assert.NotEmpty(t, out.Mount)
	v := query[View](t, f.inst, opGetView)
	assert.Equal(t, out.Mount, v.TreeMount)
	assert.Contains(t, v.TreeNote, "browsing 7 entries")
	assert.Equal(t, 1, v.TreePublished)

	f.treeErr = errors.New("ClickHouse not reachable")
	bad := app.OperationCall{Key: "t2"}
	require.NoError(t, command(t, f.inst, bad, opPublishTree))
	settled, _, err = f.settle(t, bad, opPublishTree)
	assert.True(t, settled)
	assert.ErrorContains(t, err, "not reachable")
	v = query[View](t, f.inst, opGetView)
	assert.Equal(t, 2, v.TreeGen)
	assert.Equal(t, 1, v.TreePublished)
	assert.Contains(t, v.TreeError, "not reachable")
	assert.Equal(t, bgjob.StateFailed, f.inst.treeJob.Snapshot().State)
}

// The two buttons go through the catalog where a host serves it, so the
// command log records the person's click under the operation an agent
// calls (A3); without a host the same button applies directly.
func TestTheButtonsAreTheCommands(t *testing.T) {
	f := newOpsFixture(t)
	eng := opengine.New(adhocOps.Catalog(), f.inst.Operations())
	ctx := app.NewStaticFrameContext(nil, nil)
	ctx.SetOperationsGesture(eng.Gesture)

	frame := func(body func()) {
		eng.BeginFrame()
		eng.ApplyQueued()
		eng.TakeSnapshot()
		f.inst.frameCtx = ctx
		body()
		f.inst.frameCtx = nil
		eng.EndFrame()
	}
	frame(func() { f.inst.publishTreeGesture() })
	log := eng.Log()
	require.NotEmpty(t, log)
	assert.Equal(t, opPublishTree, log[len(log)-1].Op)
	assert.Equal(t, opwire.WriterPerson, log[len(log)-1].Writer)
	assert.Equal(t, opwire.PhaseApplied, log[len(log)-1].Phase, log[len(log)-1].Reason)
	assert.Equal(t, 1, f.inst.treeGen)
	assert.Empty(t, f.inst.pendingTree, "the person's gesture waits on nothing")

	// Regenerate before the first publish: the command refuses, and the
	// fallback applies what the button always did, which is nothing while
	// no publish ran, so the log carries the refusal.
	frame(func() { f.inst.regenerateGesture() })
	var regen *opengine.LogEntry
	for _, e := range eng.Log() {
		if e.Op == opRegenerate {
			regen = &e
		}
	}
	require.NotNil(t, regen, "the click reached the catalog")
	assert.Equal(t, opwire.WriterPerson, regen.Writer)
	assert.Equal(t, opwire.PhaseRefused, regen.Phase)
	assert.Equal(t, notPublishedReason, regen.Reason)

	g := newOpsFixture(t)
	g.inst.publishTreeGesture()
	assert.Equal(t, 1, g.inst.treeGen)
}
