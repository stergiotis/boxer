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
	// What Mount observed on the context — the restore tests read these.
	gotCfg    []byte
	gotReason app.LaunchReasonE
}

var (
	_ app.AppI                = (*wsApp)(nil)
	_ app.WorkingsetComposerI = (*wsApp)(nil)
)

func (inst *wsApp) Manifest() (m app.Manifest) { return inst.manifest }
func (inst *wsApp) Mount(ctx app.MountContextI) (err error) {
	inst.gotCfg = ctx.LaunchConfig()
	inst.gotReason = ctx.LaunchReason()
	return
}
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

// registerSharedInstance registers a as a factory whose ctor hands out that
// one instance for every Open. Two reasons: these tests need a fixed instance
// to inspect afterwards, and a workingset participant may not be Register()ed
// at all — the registry refuses that pair outright (ADR-0148 §SD4). What is
// left is the case the registry cannot detect and the host therefore still
// checks at delivery time: one AppI behind however many windows.
func registerSharedInstance(t *testing.T, reg *app.Registry, a app.AppI) {
	t.Helper()
	require.NoError(t, reg.RegisterFactory(a.Manifest(), func() (out app.AppI, err error) {
		out = a
		return
	}))
}

// newWorkingsetHost registers one participant and returns the
// host with the audit wiring attached.
func newWorkingsetHost(t *testing.T, a *wsApp) (h *Inst, facts *factsstore.InMemoryFactsStore) {
	t.Helper()
	reg := app.NewRegistry()
	registerSharedInstance(t, reg, a)
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
	registerSharedInstance(t, reg, ca)
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
	// Two windows over one shared AppI (registerSharedInstance): the state is
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
	registerSharedInstance(t, reg, a)
	h := NewInst(reg, zerolog.Nop())

	k, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	h.Close(k, "user-close")
	h.reapClosed()

	assert.Zero(t, a.composeCalls)
	assert.Equal(t, 1, a.unmountCalls)
}

// Restore (ADR-0148 §SD5): a plain open of a participant carries the
// stored record, and every failure to produce one leaves a plain window
// rather than a failed open.

// seedWorkingset writes a usable record for id so the next plain open
// restores it.
func seedWorkingset(t *testing.T, facts *factsstore.InMemoryFactsStore, id app.AppIdT, cfg []byte) {
	t.Helper()
	_, err := facts.WriteWorkingset(factsstore.WorkingsetRow{
		RunId: "run-prev", AppId: id, Name: WorkingsetDefaultName,
		Kind: testCfgKind, Config: cfg, TileKey: 1, Reason: "user-close",
	})
	require.NoError(t, err)
}

func TestRestore_PlainOpenCarriesStoredRecord(t *testing.T) {
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws")}
	h, facts := newWorkingsetHost(t, a)
	seedWorkingset(t, facts, "test.ws", testCfgBytes)

	k, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)

	assert.Equal(t, testCfgBytes, a.gotCfg, "the stored record arrives as a launch config")
	assert.Equal(t, app.LaunchReasonRestore, a.gotReason)

	launches := facts.Launches()
	require.Len(t, launches, 1, "a restore is audited as a launch")
	assert.Equal(t, WorkingsetCallerAppId, launches[0].CallerAppId)
	assert.Equal(t, app.AppIdT("test.ws"), launches[0].TargetAppId)
	assert.Equal(t, testCfgKind, launches[0].ConfigKind)
	assert.Equal(t, testCfgBytes, launches[0].Config)
	assert.Equal(t, uint64(k), launches[0].TileKey)
	assert.Equal(t, "run-xyz", launches[0].RunId, "the restore joins the current run, not the one that saved it")
}

func TestRestore_CloseThenReopenRoundTrip(t *testing.T) {
	// The whole loop: a dirty close writes the record, the next plain open
	// hands it back.
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws"), cfg: testCfgBytes, dirty: true}
	h, facts := newWorkingsetHost(t, a)

	k, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	assert.Nil(t, a.gotCfg, "nothing stored yet: the first open is plain")
	assert.Equal(t, app.LaunchReasonPlain, a.gotReason)
	h.Close(k, "user-close")
	h.reapClosed()
	require.Len(t, facts.Workingsets(), 1)

	_, err = h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	assert.Equal(t, testCfgBytes, a.gotCfg)
	assert.Equal(t, app.LaunchReasonRestore, a.gotReason)
}

func TestRestore_NoRecordOpensPlain(t *testing.T) {
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws")}
	h, facts := newWorkingsetHost(t, a)

	_, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)

	assert.Nil(t, a.gotCfg)
	assert.Equal(t, app.LaunchReasonPlain, a.gotReason)
	assert.Empty(t, facts.Launches(), "a plain open writes no launch row")
}

func TestRestore_KindMismatchDegradesToPlain(t *testing.T) {
	// The app's launch kind moved since the record was written.
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws")}
	h, facts := newWorkingsetHost(t, a)
	_, err := facts.WriteWorkingset(factsstore.WorkingsetRow{
		AppId: "test.ws", Name: WorkingsetDefaultName,
		Kind: "someOtherKind", Config: testCfgBytes,
	})
	require.NoError(t, err)

	_, err = h.Open("test.ws")
	require.NoError(t, err, "a stale record degrades the open, it does not fail it")
	mountOpenWindows(t, h)
	assert.Nil(t, a.gotCfg)
	assert.Equal(t, app.LaunchReasonPlain, a.gotReason)
}

func TestRestore_RefusedBytesDegradeToPlain(t *testing.T) {
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws")}
	h, facts := newWorkingsetHost(t, a)
	seedWorkingset(t, facts, "test.ws", []byte("not a valid payload"))

	_, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	assert.Nil(t, a.gotCfg)
	assert.Equal(t, app.LaunchReasonPlain, a.gotReason)
}

func TestRestore_TombstonedRecordOpensPlain(t *testing.T) {
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws")}
	h, facts := newWorkingsetHost(t, a)
	seedWorkingset(t, facts, "test.ws", testCfgBytes)
	require.NoError(t, facts.DeleteWorkingset("test.ws", WorkingsetDefaultName))

	_, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	assert.Nil(t, a.gotCfg)
	assert.Equal(t, app.LaunchReasonPlain, a.gotReason)
}

func TestRestore_NonParticipantIgnoresStoredRecord(t *testing.T) {
	m := mkLaunchManifest("test.ws", testCfgKind) // Workingset stays false
	a := &wsApp{manifest: m}
	h, facts := newWorkingsetHost(t, a)
	seedWorkingset(t, facts, "test.ws", testCfgBytes)

	_, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	assert.Nil(t, a.gotCfg, "participation is the manifest's word, not the store's")
	assert.Equal(t, app.LaunchReasonPlain, a.gotReason)
}

func TestRestore_NoFactsStoreOpensPlain(t *testing.T) {
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws")}
	reg := app.NewRegistry()
	registerSharedInstance(t, reg, a)
	h := NewInst(reg, zerolog.Nop())

	_, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	assert.Nil(t, a.gotCfg)
	assert.Equal(t, app.LaunchReasonPlain, a.gotReason)
}

func TestRestore_RefusedForAlreadyOpenSharedInstance(t *testing.T) {
	// A restore is a config delivery, so it meets the ADR-0135 refusal: the
	// shared instance already has a window, and Mount — where a config is
	// consumed — runs once per instance. The registry refuses a declared
	// singleton outright (§SD4); this is the case it cannot see, a factory
	// handing out one instance, and the reason the host still checks here.
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws")}
	h, facts := newWorkingsetHost(t, a)
	seedWorkingset(t, facts, "test.ws", testCfgBytes)

	_, err := h.Open("test.ws")
	require.NoError(t, err)
	mountOpenWindows(t, h)
	require.Equal(t, app.LaunchReasonRestore, a.gotReason)

	_, err = h.Open("test.ws")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "never be delivered")
	assert.Equal(t, 1, h.Len())
}

func TestRestore_FactoryAppRestoresPerWindow(t *testing.T) {
	// The supported shape: each Open mints its own instance, so every
	// window consumes its own copy of the record.
	m := mkWorkingsetManifest("test.factory")
	var instances []*wsApp
	reg := app.NewRegistry()
	require.NoError(t, reg.RegisterFactory(m, func() (a app.AppI, ctorErr error) {
		w := &wsApp{manifest: m}
		instances = append(instances, w)
		a = w
		return
	}))
	h := NewInst(reg, zerolog.Nop())
	facts := factsstore.NewInMemoryFactsStore()
	h.SetAudit("run-xyz", facts)
	seedWorkingset(t, facts, "test.factory", testCfgBytes)

	_, err := h.Open("test.factory")
	require.NoError(t, err)
	_, err = h.Open("test.factory")
	require.NoError(t, err)
	mountOpenWindows(t, h)

	require.Len(t, instances, 2)
	for i, w := range instances {
		assert.Equal(t, testCfgBytes, w.gotCfg, "window %d restores its own copy", i)
		assert.Equal(t, app.LaunchReasonRestore, w.gotReason, "window %d", i)
	}
}

func TestRestore_CallerConfigOutranksStoredRecord(t *testing.T) {
	// A caller who states the arguments gets exactly those, with the
	// reason that says so — the stored record is not consulted.
	m := mkWorkingsetManifest("test.factory")
	var instances []*wsApp
	reg := app.NewRegistry()
	require.NoError(t, reg.RegisterFactory(m, func() (a app.AppI, ctorErr error) {
		w := &wsApp{manifest: m}
		instances = append(instances, w)
		a = w
		return
	}))
	h := NewInst(reg, zerolog.Nop())
	facts := factsstore.NewInMemoryFactsStore()
	h.SetAudit("run-xyz", facts)
	seedWorkingset(t, facts, "test.factory", []byte("not a valid payload"))

	_, err := h.OpenWithConfig("test.factory", testCfgKind, testCfgBytes)
	require.NoError(t, err, "an unusable stored record cannot spoil a caller-delivered open")
	mountOpenWindows(t, h)
	require.Len(t, instances, 1)
	assert.Equal(t, testCfgBytes, instances[0].gotCfg)
	assert.Equal(t, app.LaunchReasonCaller, instances[0].gotReason)
	assert.Empty(t, facts.Launches(), "the open service, not the host, audits caller opens")
}

func TestWindowInfos_ReportLaunchProvenance(t *testing.T) {
	// The introspection surface reads this snapshot (keelson('windows')):
	// a restored window has to be tellable from a plain one, since nothing
	// in the window itself says so.
	a := &wsApp{manifest: mkWorkingsetManifest("test.ws")}
	h, facts := newWorkingsetHost(t, a)
	seedWorkingset(t, facts, "test.ws", testCfgBytes)

	_, err := h.Open("test.ws")
	require.NoError(t, err)
	infos := h.WindowInfos()
	require.Len(t, infos, 1)
	assert.Equal(t, app.LaunchReasonRestore, infos[0].LaunchReason)
	assert.Equal(t, testCfgKind, infos[0].ConfigKind)
	assert.Equal(t, len(testCfgBytes), infos[0].ConfigBytes)
	assert.False(t, infos[0].SharesInstance)
}

func TestWindowInfos_PlainOpenAndSharedInstance(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "test.a")
	h := NewInst(reg, zerolog.Nop())

	_, err := h.Open("test.a")
	require.NoError(t, err)
	infos := h.WindowInfos()
	require.Len(t, infos, 1)
	assert.Equal(t, app.LaunchReasonPlain, infos[0].LaunchReason)
	assert.Empty(t, infos[0].ConfigKind)
	assert.Zero(t, infos[0].ConfigBytes)
	assert.False(t, infos[0].SharesInstance, "one window holds the instance alone")

	// A second window over the same singleton shares the instance — the
	// state neither window may save nor be handed a config for.
	_, err = h.Open("test.a")
	require.NoError(t, err)
	infos = h.WindowInfos()
	require.Len(t, infos, 2)
	assert.True(t, infos[0].SharesInstance)
	assert.True(t, infos[1].SharesInstance)
}
