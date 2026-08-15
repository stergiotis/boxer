package windowhost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// ADR-0188 §SD1/§SD2: the closing edge runs leave → unmount → unload. The
// app below records, at each step it can observe, what the host had done
// by then: whether Cancel() had fired when Unmount ran, and whether the bus
// was still usable inside Unmount.

type edgeApp struct {
	manifest app.Manifest
	mountCtx app.MountContextI

	subscribed      bool
	cancelledBefore bool  // Cancel() had fired when Unmount ran
	publishErr      error // Publish inside Unmount
	unmountCalls    int
}

var _ app.AppI = (*edgeApp)(nil)

func (inst *edgeApp) Manifest() (m app.Manifest) { return inst.manifest }
func (inst *edgeApp) Mount(ctx app.MountContextI) (err error) {
	inst.mountCtx = ctx
	// A subscription the app never releases — the host must.
	_, err = ctx.Bus().Subscribe("t.evt", func(msg *app.Msg) {})
	inst.subscribed = err == nil
	return
}
func (inst *edgeApp) Frame(ctx app.FrameContextI) (err error) { return }
func (inst *edgeApp) Unmount(ctx app.MountContextI) (err error) {
	inst.unmountCalls++
	select {
	case <-ctx.Cancel():
		inst.cancelledBefore = true
	default:
	}
	inst.publishErr = ctx.Bus().Publish("t.bye", nil)
	return
}

func edgeManifest(id app.AppIdT) (m app.Manifest) {
	m = mkManifest(id)
	m.Caps = []app.SubjectFilter{{Pattern: "t.>", Direction: app.CapDirectionBoth}}
	return
}

// mountForTest performs the lazy Mount that Frame would run (Frame needs
// the Rust runtime), mirroring renderWindowBody's bookkeeping.
func mountForTest(t *testing.T, h *Inst, k WindowKeyT) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, w := range h.windows {
		if w.key != k {
			continue
		}
		if !w.mount.mounted {
			require.NoError(t, w.appInst.Mount(w.mountCtx))
			w.mount.mounted = true
			w.mount.mountCtx = w.mountCtx
		}
		return
	}
	t.Fatalf("window %d not found", k)
}

func TestClosingEdge_LeaveUnmountUnload_FactoryApp(t *testing.T) {
	reg := app.NewRegistry()
	var made *edgeApp
	require.NoError(t, reg.RegisterFactory(edgeManifest("test.edge"), func() (a app.AppI, err error) {
		made = &edgeApp{manifest: edgeManifest("test.edge")}
		a = made
		return
	}))
	bus := inprocbus.NewInst(zerolog.Nop())
	h := NewInst(reg, zerolog.Nop())
	h.SetBus(bus)

	k, err := h.Open("test.edge")
	require.NoError(t, err)
	mountForTest(t, h, k)
	require.True(t, made.subscribed)
	require.Len(t, bus.Subscriptions(), 1, "the app's unreleased subscription is live")
	rows := bus.Subscriptions()
	assert.Equal(t, uint64(k), rows[0].InstanceKey, "the subscription is attributed to the window")

	// Cancel() is a real channel now: not fired before close.
	select {
	case <-made.mountCtx.Cancel():
		t.Fatal("Cancel() fired before the window closed")
	default:
	}

	h.Close(k, "")
	h.reapClosed()
	require.Equal(t, 0, h.Len())

	assert.Equal(t, 1, made.unmountCalls)
	assert.True(t, made.cancelledBefore, "leave: Cancel() fires before Unmount")
	assert.NoError(t, made.publishErr, "unmount: the bus is still usable inside Unmount")
	assert.Empty(t, bus.Subscriptions(), "unload: the host released the subscription the app forgot")
	err = made.mountCtx.Bus().Publish("t.late", nil)
	require.Error(t, err, "a goroutine outliving Unmount sees a closed client")
	assert.True(t, errors.Is(err, inprocbus.ErrClosed))
	_, ok := bus.ClientByAppId("test.edge")
	assert.False(t, ok, "no live client remains for the reaped window")
}

func TestClosingEdge_NeverMountedWindowReleasesItsOwnClient(t *testing.T) {
	reg := app.NewRegistry()
	require.NoError(t, reg.RegisterFactory(edgeManifest("test.edge"), func() (a app.AppI, err error) {
		a = &edgeApp{manifest: edgeManifest("test.edge")}
		return
	}))
	bus := inprocbus.NewInst(zerolog.Nop())
	h := NewInst(reg, zerolog.Nop())
	h.SetBus(bus)

	_, err := h.Open("test.edge")
	require.NoError(t, err)
	require.Len(t, bus.LiveClients(), 1)
	h.CloseAll("")
	h.reapClosed()
	assert.Empty(t, bus.LiveClients(), "a window closed before its first Frame still returns its client")
}

func TestClosingEdge_SingletonSharedMount_CarriesMountingWindowToLastRelease(t *testing.T) {
	// A singleton shown in two windows is Mounted once, with the FIRST
	// window's context. If that window closes first, the app still holds
	// its stop channel and bus client — so they must survive until the
	// last window releases the instance, and then go with it.
	reg := app.NewRegistry()
	single := &edgeApp{manifest: edgeManifest("test.single")}
	require.NoError(t, reg.Register(single))
	bus := inprocbus.NewInst(zerolog.Nop())
	h := NewInst(reg, zerolog.Nop())
	h.SetBus(bus)

	k1, err := h.Open("test.single")
	require.NoError(t, err)
	k2, err := h.Open("test.single")
	require.NoError(t, err)
	mountForTest(t, h, k1) // mounts with window 1's context
	mountForTest(t, h, k2) // already mounted; no-op
	require.Len(t, bus.LiveClients(), 2)
	require.Len(t, bus.Subscriptions(), 1)

	h.Close(k1, "")
	h.reapClosed()
	require.Equal(t, 1, h.Len())
	assert.Equal(t, 0, single.unmountCalls, "instance stays mounted through window 2")
	select {
	case <-single.mountCtx.Cancel():
		t.Fatal("the mounting window's stop channel must not close while the instance is shared")
	default:
	}
	assert.Len(t, bus.Subscriptions(), 1, "the app's subscription (made through window 1's client) survives window 1")
	assert.Len(t, bus.LiveClients(), 2, "window 1's client is carried, not closed")

	h.Close(k2, "")
	h.reapClosed()
	require.Equal(t, 0, h.Len())
	assert.Equal(t, 1, single.unmountCalls)
	assert.True(t, single.cancelledBefore, "leave ran on the carried window before Unmount")
	assert.NoError(t, single.publishErr, "the carried client was still open inside Unmount")
	assert.Empty(t, bus.Subscriptions())
	assert.Empty(t, bus.LiveClients(), "both windows' clients are closed at the last release")
}

func TestClosingEdge_SingletonSharedMount_NonMountingWindowClosesOwnResources(t *testing.T) {
	// The mirror case: the SECOND window closes first. Its context is not
	// the one the app was mounted with, so its stop channel and client
	// (unobserved by the app) close at its own reap.
	reg := app.NewRegistry()
	single := &edgeApp{manifest: edgeManifest("test.single")}
	require.NoError(t, reg.Register(single))
	bus := inprocbus.NewInst(zerolog.Nop())
	h := NewInst(reg, zerolog.Nop())
	h.SetBus(bus)

	k1, err := h.Open("test.single")
	require.NoError(t, err)
	k2, err := h.Open("test.single")
	require.NoError(t, err)
	mountForTest(t, h, k1)
	require.Len(t, bus.LiveClients(), 2)

	h.Close(k2, "")
	h.reapClosed()
	assert.Equal(t, 0, single.unmountCalls)
	assert.Len(t, bus.LiveClients(), 1, "window 2's own client closes at its reap")
	assert.Len(t, bus.Subscriptions(), 1, "the app's subscription is untouched")

	h.Close(k1, "")
	h.reapClosed()
	assert.Equal(t, 1, single.unmountCalls)
	assert.True(t, single.cancelledBefore)
	assert.Empty(t, bus.LiveClients())
	assert.Empty(t, bus.Subscriptions())
}

// taskApp spawns a background task through task.ForApp in Mount and never
// cancels it: ADR-0038's cascade-cancel on window close is what must end
// it, which is exactly the channel §SD2 makes real.
type taskApp struct {
	manifest app.Manifest
	handle   task.HandleI
	spawnErr error
}

var _ app.AppI = (*taskApp)(nil)

func (inst *taskApp) Manifest() (m app.Manifest) { return inst.manifest }
func (inst *taskApp) Mount(ctx app.MountContextI) (err error) {
	inst.handle, inst.spawnErr = task.ForApp(ctx).Spawn(context.Background(), task.SpawnOpts{Kind: "edge.test"})
	return
}
func (inst *taskApp) Frame(ctx app.FrameContextI) (err error)   { return }
func (inst *taskApp) Unmount(ctx app.MountContextI) (err error) { return }

func TestClosingEdge_TaskSpawnedViaForAppIsCancelledOnClose(t *testing.T) {
	m := mkManifest("test.task")
	m.Caps = task.ProducerCaps()
	reg := app.NewRegistry()
	var made *taskApp
	require.NoError(t, reg.RegisterFactory(m, func() (a app.AppI, err error) {
		made = &taskApp{manifest: m}
		a = made
		return
	}))
	bus := inprocbus.NewInst(zerolog.Nop())
	h := NewInst(reg, zerolog.Nop())
	h.SetBus(bus)

	k, err := h.Open("test.task")
	require.NoError(t, err)
	mountForTest(t, h, k)
	require.NoError(t, made.spawnErr)
	require.False(t, made.handle.Cancelled(), "task runs while the window is open")

	h.Close(k, "")
	h.reapClosed()

	select {
	case <-made.handle.Ctx().Done():
	case <-time.After(2 * time.Second):
		t.Fatal("task was not cancelled by the window's closing edge")
	}
	assert.True(t, made.handle.Cancelled())
}
