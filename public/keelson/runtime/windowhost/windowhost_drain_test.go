package windowhost

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
)

// ADR-0322 (proposed): between Unmount and the bus close the host drains
// the instance's lifetime scope. tailApp starts work in Mount that, once
// the work context ends, writes its tail over the bus and leaves when
// release is closed.
type tailApp struct {
	manifest app.Manifest
	release  chan struct{}
	tailErr  chan error
	scope    *lifetime.Scope
}

func (inst *tailApp) Manifest() (m app.Manifest) { return inst.manifest }
func (inst *tailApp) Mount(ctx app.MountContextI) (err error) {
	inst.scope = lifetime.New(app.LifetimeOf(ctx))
	bus := ctx.Bus()
	_, err = bus.Subscribe("t.evt", func(msg *app.Msg) {})
	inst.scope.Go(func(work context.Context) {
		<-work.Done()
		tail := inst.scope.Tail(work)
		select {
		case <-inst.release:
		case <-tail.Done():
		}
		inst.tailErr <- bus.Publish("t.tail", nil)
	})
	return
}
func (inst *tailApp) Frame(ctx app.FrameContextI) (err error)   { return }
func (inst *tailApp) Unmount(ctx app.MountContextI) (err error) { return }

func openTailApp(t *testing.T) (h *Inst, bus *inprocbus.Inst, made *tailApp, k WindowKeyT) {
	t.Helper()
	reg := app.NewRegistry()
	made = &tailApp{manifest: edgeManifest("test.tail"), release: make(chan struct{}), tailErr: make(chan error, 1)}
	require.NoError(t, reg.RegisterFactory(made.manifest, func() (a app.AppI, err error) { return made, nil }))
	bus = inprocbus.NewInst(zerolog.Nop())
	h = NewInst(reg, zerolog.Nop())
	h.SetBus(bus)
	k, err := h.Open("test.tail")
	require.NoError(t, err)
	mountForTest(t, h, k)
	require.Len(t, bus.Subscriptions(), 1)
	return
}

// A window close leaves the desktop at once; the bus stays live for the
// tail of the work the app counted in, and closes when that work left.
func TestDrain_CloseWaitsForTheTailBeforeTheBusCloses(t *testing.T) {
	h, bus, made, k := openTailApp(t)
	h.Close(k, "")
	h.reapClosed()
	assert.Equal(t, 0, h.Len(), "the window is gone at once")
	time.Sleep(20 * time.Millisecond)
	assert.Len(t, bus.Subscriptions(), 1, "the client is live while the instance drains")
	close(made.release)
	select {
	case err := <-made.tailErr:
		assert.NoError(t, err, "the tail is written over a live bus")
	case <-time.After(2 * time.Second):
		t.Fatal("the tail did not run")
	}
	require.Eventually(t, func() bool { return len(bus.Subscriptions()) == 0 }, 2*time.Second, time.Millisecond, "the client closes once drained")
}

// Past the deadline the scope is abandoned: the tail is told to stop, and
// the bus closes regardless.
func TestDrain_TheDeadlineAbandonsTheScope(t *testing.T) {
	h, bus, made, k := openTailApp(t)
	h.SetDrainDeadlines(30*time.Millisecond, 0)
	h.Close(k, "")
	h.reapClosed()
	select {
	case <-made.tailErr:
	case <-time.After(2 * time.Second):
		t.Fatal("the abandoned tail was not told to stop")
	}
	require.Eventually(t, func() bool { return len(bus.Subscriptions()) == 0 }, 2*time.Second, time.Millisecond)
}

// At shutdown ReapAll waits for the drain, within its budget.
func TestDrain_ReapAllWaitsWithinTheShutdownBudget(t *testing.T) {
	h, bus, made, _ := openTailApp(t)
	go func() {
		time.Sleep(30 * time.Millisecond)
		close(made.release)
	}()
	h.ReapAll("shutdown")
	assert.NoError(t, <-made.tailErr)
	assert.Empty(t, bus.Subscriptions(), "released when ReapAll returns")

	h2, bus2, made2, _ := openTailApp(t)
	h2.SetDrainDeadlines(0, 30*time.Millisecond)
	start := time.Now()
	h2.ReapAll("shutdown")
	assert.Less(t, time.Since(start), time.Second, "the budget bounds the wait")
	assert.Empty(t, bus2.Subscriptions())
	<-made2.tailErr
}
