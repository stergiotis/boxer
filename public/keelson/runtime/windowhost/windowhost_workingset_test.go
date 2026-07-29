package windowhost

import (
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

// wsApp is a workingset participant (ADR-0148 §SD4). It records when
// ComposeWorkingset ran relative to Unmount — the ordering the host must
// hold, since Unmount tears a real app down.
type wsApp struct {
	manifest     app.Manifest
	cfg          []byte
	dirty        bool
	composeErr   error
	composeCalls int
	unmountCalls int
	composedLate bool
}

var (
	_ app.AppI                = (*wsApp)(nil)
	_ app.WorkingsetComposerI = (*wsApp)(nil)
)

func (inst *wsApp) Manifest() (m app.Manifest)                { return inst.manifest }
func (inst *wsApp) Mount(ctx app.MountContextI) (err error)   { return }
func (inst *wsApp) Frame(ctx app.FrameContextI) (err error)   { return }
func (inst *wsApp) Unmount(ctx app.MountContextI) (err error) { inst.unmountCalls++; return }
func (inst *wsApp) ComposeWorkingset() (cfg []byte, dirty bool, err error) {
	inst.composeCalls++
	if inst.unmountCalls > 0 {
		inst.composedLate = true
	}
	cfg, dirty, err = inst.cfg, inst.dirty, inst.composeErr
	return
}

// mkWorkingsetManifest is the participating shape: a launch kind (§SD2
// makes the record an instance of it) plus the participation flag.
func mkWorkingsetManifest(id app.AppIdT) (m app.Manifest) {
	m = mkLaunchManifest(id, testCfgKind)
	m.Workingset = true
	return
}

// mountOpenWindows drives the lazy Mount the first Frame would, without
// the Rust runtime: the host only unmounts (and therefore only composes)
// windows whose instance actually mounted.
func mountOpenWindows(t *testing.T, h *Inst) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, w := range h.windows {
		if w.mount.mounted {
			continue
		}
		require.NoError(t, w.appInst.Mount(w.mountCtx))
		w.mount.mounted = true
		w.mount.mountCtx = w.mountCtx
	}
}

// newWorkingsetHost registers one singleton participant and returns the
// host with the audit wiring attached.
func newWorkingsetHost(t *testing.T, a *wsApp) (h *Inst, facts *factsstore.InMemoryFactsStore) {
	t.Helper()
	reg := app.NewRegistry()
	require.NoError(t, reg.Register(a))
	h = NewInst(reg, zerolog.Nop())
	facts = factsstore.NewInMemoryFactsStore()
	h.SetAudit("run-xyz", facts)
	return
}

func TestWorkingset_DirtyCloseWritesRow(t *testing.T) {
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws"), cfg: testCfgBytes, dirty: true}
	h, facts := newWorkingsetHost(t, a)

	k, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	h.Close(k, "user-close")
	h.reapClosed()

	rows := facts.Workingsets()
	require.Len(t, rows, 1)
	assert.Equal(t, app.AppIdT("test.ws"), rows[0].AppId)
	assert.Equal(t, WorkingsetDefaultName, rows[0].Name)
	assert.Equal(t, testCfgKind, rows[0].Kind)
	assert.Equal(t, testCfgBytes, rows[0].Config)
	assert.Equal(t, uint64(k), rows[0].TileKey)
	assert.Equal(t, "run-xyz", rows[0].RunId)
	assert.Equal(t, "user-close", rows[0].Reason, "the stop reason is the save provenance")

	cfg, kind, found, err := facts.LatestWorkingset("test.ws", WorkingsetDefaultName)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, testCfgBytes, cfg)
	assert.Equal(t, testCfgKind, kind)
}

func TestWorkingset_ComposesBeforeUnmount(t *testing.T) {
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws"), cfg: testCfgBytes, dirty: true}
	h, _ := newWorkingsetHost(t, a)

	k, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	h.Close(k, "user-close")
	h.reapClosed()

	assert.Equal(t, 1, a.composeCalls)
	assert.Equal(t, 1, a.unmountCalls)
	assert.False(t, a.composedLate, "compose must run before Unmount tears the app down")
}

func TestWorkingset_CleanCloseWritesNothing(t *testing.T) {
	// The poisoned-inheritance case: a window nobody acted in — a
	// launch-seeded one included — leaves the stored record alone.
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws"), cfg: testCfgBytes, dirty: false}
	h, facts := newWorkingsetHost(t, a)

	k, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	h.Close(k, "user-close")
	h.reapClosed()

	assert.Equal(t, 1, a.composeCalls, "the host still asks; the app's dirty flag is the gate")
	assert.Empty(t, facts.Workingsets())
}

func TestWorkingset_EmptyConfigWritesNothing(t *testing.T) {
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws"), cfg: nil, dirty: true}
	h, facts := newWorkingsetHost(t, a)

	k, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	h.Close(k, "user-close")
	h.reapClosed()

	assert.Empty(t, facts.Workingsets())
}

func TestWorkingset_ComposeErrorSkipsSaveButNotTheClose(t *testing.T) {
	a := &wsApp{
		manifest:   mkWorkingsetManifest("test.ws"),
		cfg:        testCfgBytes,
		dirty:      true,
		composeErr: errors.New("boom"),
	}
	h, facts := newWorkingsetHost(t, a)

	k, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	h.Close(k, "user-close")
	h.reapClosed()

	assert.Empty(t, facts.Workingsets())
	assert.Equal(t, 1, a.unmountCalls, "a failed compose must not disturb the close")
	assert.Equal(t, 0, h.Len())
	assert.Len(t, facts.Lifecycles(), 2, "the lifecycle trail is unaffected")
}

func TestWorkingset_RefusedBytesNotStored(t *testing.T) {
	// The record is a launch config: bytes OpenWithConfig would refuse are
	// bytes no restore could deliver, so the host does not store them.
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws"), cfg: []byte("not a valid payload"), dirty: true}
	h, facts := newWorkingsetHost(t, a)

	k, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	h.Close(k, "user-close")
	h.reapClosed()

	assert.Empty(t, facts.Workingsets())
}

func TestWorkingset_OversizeNotStored(t *testing.T) {
	a := &wsApp{
		manifest: mkWorkingsetManifest("test.ws"),
		cfg:      make([]byte, maxLaunchConfigBytes+1),
		dirty:    true,
	}
	h, facts := newWorkingsetHost(t, a)

	k, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	h.Close(k, "user-close")
	h.reapClosed()

	assert.Empty(t, facts.Workingsets())
}

func TestWorkingset_ReapAllWritesWithSuppliedReason(t *testing.T) {
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws"), cfg: testCfgBytes, dirty: true}
	h, facts := newWorkingsetHost(t, a)

	_, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	h.ReapAll("shutdown")

	rows := facts.Workingsets()
	require.Len(t, rows, 1)
	assert.Equal(t, "shutdown", rows[0].Reason)
	assert.False(t, a.composedLate)
}

func TestWorkingset_NonParticipatingAppUntouched(t *testing.T) {
	// Implementing the interface is not participation; the manifest says.
	m := mkLaunchManifest("test.ws", testCfgKind) // Workingset stays false
	a := &wsApp{manifest: m, cfg: testCfgBytes, dirty: true}
	h, facts := newWorkingsetHost(t, a)

	k, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	h.Close(k, "user-close")
	h.reapClosed()

	assert.Zero(t, a.composeCalls)
	assert.Empty(t, facts.Workingsets())
}

func TestWorkingset_ParticipantWithoutComposerIsDiagnosed(t *testing.T) {
	// counterApp does not implement WorkingsetComposerI. The mismatch can
	// only surface here (the instance exists only after Open), and must not
	// break the close.
	reg := app.NewRegistry()
	ca := &counterApp{manifest: mkWorkingsetManifest("test.ws")}
	require.NoError(t, reg.Register(ca))
	h := NewInst(reg, zerolog.Nop())
	facts := factsstore.NewInMemoryFactsStore()
	h.SetAudit("run-xyz", facts)

	k, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	h.Close(k, "user-close")
	h.reapClosed()

	assert.Empty(t, facts.Workingsets())
	assert.Equal(t, 0, h.Len())
	h.mu.Lock()
	_, warned := h.warnedNoComposer["test.ws"]
	h.mu.Unlock()
	assert.True(t, warned, "the mismatch is reported once per app id")
}

func TestWorkingset_SharedInstanceSkipsSaveUntilLastWindow(t *testing.T) {
	// A singleton-registered app shown twice shares one AppI: the state is
	// not the closing window's to save while another window still holds it
	// — the mirror of OpenWithConfig's refusal to deliver a config to an
	// instance that already has a window.
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws"), cfg: testCfgBytes, dirty: true}
	h, facts := newWorkingsetHost(t, a)

	k1, err := h.Open("test.ws")
	require.NoError(t, err)
	k2, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)

	h.Close(k1, "user-close")
	h.reapClosed()
	assert.Zero(t, a.composeCalls, "the surviving window still owns the instance")
	assert.Empty(t, facts.Workingsets())

	h.Close(k2, "user-close")
	h.reapClosed()
	assert.Equal(t, 1, a.composeCalls)
	require.Len(t, facts.Workingsets(), 1)
	assert.Equal(t, uint64(k2), facts.Workingsets()[0].TileKey)
}

func TestWorkingset_UnmountedWindowComposesNothing(t *testing.T) {
	// A window closed before its first Frame never mounted, so there is no
	// state to compose from.
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws"), cfg: testCfgBytes, dirty: true}
	h, facts := newWorkingsetHost(t, a)

	k, err := h.Open("test.ws")
	require.NoError(t, err)
	h.Close(k, "user-close")
	h.reapClosed()

	assert.Zero(t, a.composeCalls)
	assert.Zero(t, a.unmountCalls)
	assert.Empty(t, facts.Workingsets())
}

func TestWorkingset_NoFactsStoreSkipsCompose(t *testing.T) {
	// With no store attached the record has nowhere to land; composing
	// would be work for nothing (§SD5's degrade-quietly stance).
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws"), cfg: testCfgBytes, dirty: true}
	reg := app.NewRegistry()
	require.NoError(t, reg.Register(a))
	h := NewInst(reg, zerolog.Nop())

	k, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	h.Close(k, "user-close")
	h.reapClosed()

	assert.Zero(t, a.composeCalls)
	assert.Equal(t, 1, a.unmountCalls)
}
