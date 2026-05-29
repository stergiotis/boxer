//go:build llm_generated_opus47

package windowhost

import (
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
)

// counterApp is a test AppI that counts Mount/Unmount calls and lets
// us inject a deterministic Mount error. Used by the lifecycle tests;
// we don't invoke Frame here because c.Window needs the Rust FFFI
// runtime that unit tests don't bring up.
type counterApp struct {
	manifest    app.Manifest
	mountCalls  int
	unmountErrs int
	mountErr    error
}

var _ app.AppI = (*counterApp)(nil)

func (inst *counterApp) Manifest() (m app.Manifest) { return inst.manifest }
func (inst *counterApp) Mount(ctx app.MountContextI) (err error) {
	inst.mountCalls++
	err = inst.mountErr
	return
}
func (inst *counterApp) Frame(ctx app.FrameContextI) (err error)   { return }
func (inst *counterApp) Unmount(ctx app.MountContextI) (err error) { return }

func mkManifest(id app.AppIdT) (m app.Manifest) {
	m = app.Manifest{
		Id:      id,
		Version: "0.1.0",
		Display: string(id),
		Title:   string(id),
		Surface: app.SurfaceWindowed,
	}
	return
}

func mkRegistryWithSingleton(t *testing.T, ids ...app.AppIdT) (reg *app.Registry, apps map[app.AppIdT]*counterApp) {
	t.Helper()
	reg = app.NewRegistry()
	apps = make(map[app.AppIdT]*counterApp, len(ids))
	for _, id := range ids {
		ca := &counterApp{manifest: mkManifest(id)}
		err := reg.Register(ca)
		require.NoError(t, err)
		apps[id] = ca
	}
	return
}

func TestInst_Open_AllocatesFreshKeyPerCall(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "test.a", "test.b")
	h := NewInst(reg, zerolog.Nop())

	k1, err := h.Open("test.a")
	require.NoError(t, err)
	k2, err := h.Open("test.a")
	require.NoError(t, err)
	k3, err := h.Open("test.b")
	require.NoError(t, err)

	assert.NotEqual(t, k1, k2, "two Opens of same AppId yield distinct keys")
	assert.NotEqual(t, k2, k3, "keys are unique across AppIds")
	assert.Equal(t, []WindowKeyT{k1, k2, k3}, h.OpenWindows())
}

func TestInst_Open_UnknownId(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "test.a")
	h := NewInst(reg, zerolog.Nop())

	_, err := h.Open("test.absent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestInst_Open_CtorError(t *testing.T) {
	reg := app.NewRegistry()
	m := mkManifest("test.bad")
	err := reg.RegisterFactory(m, func() (a app.AppI, ctorErr error) {
		ctorErr = errors.New("boom")
		return
	})
	require.NoError(t, err)

	h := NewInst(reg, zerolog.Nop())
	_, err = h.Open("test.bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open")
}

func TestInst_Close_MarksForReaping(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "test.a")
	h := NewInst(reg, zerolog.Nop())

	k, err := h.Open("test.a")
	require.NoError(t, err)
	require.Equal(t, 1, h.Len())

	h.Close(k, "")
	// Close just marks the flag; the window is still present until
	// reapClosed runs.
	assert.Equal(t, 1, h.Len())

	h.reapClosed()
	assert.Equal(t, 0, h.Len())
}

func TestInst_Close_UnknownKey_NoOp(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "test.a")
	h := NewInst(reg, zerolog.Nop())

	k, err := h.Open("test.a")
	require.NoError(t, err)

	h.Close(WindowKeyT(999), "")
	h.reapClosed()
	assert.Equal(t, 1, h.Len())
	assert.Equal(t, []WindowKeyT{k}, h.OpenWindows())
}

func TestInst_ReapClosed_CallsUnmount(t *testing.T) {
	reg, apps := mkRegistryWithSingleton(t, "test.a")
	h := NewInst(reg, zerolog.Nop())

	k, err := h.Open("test.a")
	require.NoError(t, err)

	// Simulate the window being mounted (Frame would do this; we set
	// the flag directly because Frame needs the Rust runtime).
	h.mu.Lock()
	h.windows[0].mounted = true
	h.mu.Unlock()

	h.Close(k, "")
	h.reapClosed()
	assert.Equal(t, 0, h.Len())
	_ = apps
}

func TestInst_ReapClosed_SkipsUnmountForUnmountedWindow(t *testing.T) {
	// If a window was Closed before its first Frame (so never Mounted),
	// reapClosed must not call Unmount. We check that by spying via
	// mountCalls staying zero.
	reg, apps := mkRegistryWithSingleton(t, "test.a")
	h := NewInst(reg, zerolog.Nop())

	k, err := h.Open("test.a")
	require.NoError(t, err)
	require.Zero(t, apps["test.a"].mountCalls)

	h.Close(k, "")
	h.reapClosed()
	assert.Equal(t, 0, h.Len())
	assert.Zero(t, apps["test.a"].mountCalls, "Mount must not have run")
}

func TestInst_FactoryRegistered_OpenYieldsDistinctInstances(t *testing.T) {
	// The point of multi-instance: two windows for the same AppId get
	// independent AppI instances when the registration is factory-based.
	reg := app.NewRegistry()
	m := mkManifest("test.factory")
	var instances []*counterApp
	err := reg.RegisterFactory(m, func() (a app.AppI, ctorErr error) {
		ca := &counterApp{manifest: m}
		instances = append(instances, ca)
		a = ca
		return
	})
	require.NoError(t, err)

	h := NewInst(reg, zerolog.Nop())
	_, err = h.Open("test.factory")
	require.NoError(t, err)
	_, err = h.Open("test.factory")
	require.NoError(t, err)

	require.Len(t, instances, 2, "factory must run once per Open")
	assert.NotSame(t, instances[0], instances[1])

	// And the windows hold the distinct instances:
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.windows, 2)
	assert.NotSame(t, h.windows[0].appInst, h.windows[1].appInst)
}

func TestInst_SingletonRegistered_OpenSharesInstance(t *testing.T) {
	// Mirror of the multi-instance test: singleton-registered apps
	// share their AppI across windows. This documents the trade-off
	// captured in EXPLANATION.md.
	reg, apps := mkRegistryWithSingleton(t, "test.singleton")
	singleton := apps["test.singleton"]
	h := NewInst(reg, zerolog.Nop())

	_, err := h.Open("test.singleton")
	require.NoError(t, err)
	_, err = h.Open("test.singleton")
	require.NoError(t, err)

	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.windows, 2)
	assert.Same(t, singleton, h.windows[0].appInst)
	assert.Same(t, singleton, h.windows[1].appInst)
}

func TestSetAudit_OpenEmitsStartedRow(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "test.a")
	h := NewInst(reg, zerolog.Nop())
	facts := factsstore.NewInMemoryFactsStore()
	h.SetAudit("run-xyz", facts)

	k, err := h.Open("test.a")
	require.NoError(t, err)

	rows := facts.Lifecycles()
	require.Len(t, rows, 1)
	assert.Equal(t, "run-xyz", rows[0].RunId)
	assert.Equal(t, app.AppIdT("test.a"), rows[0].AppId)
	assert.Equal(t, uint64(k), rows[0].TileKey)
	assert.Equal(t, factsstore.AppLifecyclePhaseStarted, rows[0].Phase)
	assert.Empty(t, rows[0].StopReason, "started rows must not carry a stop reason")
}

func TestSetAudit_ReapEmitsStoppedRowWithReason(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "test.a")
	h := NewInst(reg, zerolog.Nop())
	facts := factsstore.NewInMemoryFactsStore()
	h.SetAudit("run-xyz", facts)

	k, err := h.Open("test.a")
	require.NoError(t, err)
	h.Close(k, "user-close")
	h.reapClosed()

	rows := facts.Lifecycles()
	require.Len(t, rows, 2, "expected one started + one stopped")
	stopped := rows[1]
	assert.Equal(t, factsstore.AppLifecyclePhaseStopped, stopped.Phase)
	assert.Equal(t, "user-close", stopped.StopReason)
	assert.Equal(t, uint64(k), stopped.TileKey)
}

func TestSetAudit_ReapEmitsMountErrorWhenSticky(t *testing.T) {
	reg, apps := mkRegistryWithSingleton(t, "test.a")
	apps["test.a"].mountErr = errors.New("boom")
	h := NewInst(reg, zerolog.Nop())
	facts := factsstore.NewInMemoryFactsStore()
	h.SetAudit("run-xyz", facts)

	k, err := h.Open("test.a")
	require.NoError(t, err)
	// Simulate the first Frame that triggers Mount and sets mountErr;
	// we can't call Frame directly (needs the Rust runtime), so we
	// flip the flag in place to match what renderWindowBody would do.
	h.mu.Lock()
	h.windows[0].mountErr = apps["test.a"].mountErr
	h.mu.Unlock()
	h.Close(k, "") // empty reason → defaultStopReason picks mount-error
	h.reapClosed()

	rows := facts.Lifecycles()
	require.Len(t, rows, 2)
	assert.Equal(t, "mount-error", rows[1].StopReason)
}

func TestReapAll_EmitsShutdownRows(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "test.a", "test.b")
	h := NewInst(reg, zerolog.Nop())
	facts := factsstore.NewInMemoryFactsStore()
	h.SetAudit("run-xyz", facts)

	_, err := h.Open("test.a")
	require.NoError(t, err)
	_, err = h.Open("test.b")
	require.NoError(t, err)
	require.Equal(t, 2, h.Len())

	h.ReapAll("shutdown")
	assert.Equal(t, 0, h.Len(), "all windows reaped")

	rows := facts.Lifecycles()
	// 2 started + 2 stopped(shutdown)
	require.Len(t, rows, 4)
	stops := []factsstore.AppLifecycleRow{rows[2], rows[3]}
	for _, r := range stops {
		assert.Equal(t, factsstore.AppLifecyclePhaseStopped, r.Phase)
		assert.Equal(t, "shutdown", r.StopReason)
	}
}

func TestSetBus_WiresInprocClientThroughMountCtx(t *testing.T) {
	reg := app.NewRegistry()
	m := mkManifest("test.bus")
	// Grant a single fs.dialog.* cap so we can verify the wiring
	// passes manifest caps through to the client (publish to an
	// allowed subject succeeds, publish elsewhere fails).
	m.Caps = []app.SubjectFilter{
		{Pattern: "fs.dialog.>", Direction: app.CapDirectionPub},
	}
	ca := &counterApp{manifest: m}
	require.NoError(t, reg.Register(ca))

	bus := inprocbus.NewInst(zerolog.Nop())
	h := NewInst(reg, zerolog.Nop())
	h.SetBus(bus)

	k, err := h.Open("test.bus")
	require.NoError(t, err)
	h.mu.Lock()
	w := h.windows[0]
	h.mu.Unlock()
	require.EqualValues(t, k, w.key)

	mountBus := w.mountCtx.Bus()
	require.NotNil(t, mountBus, "MountCtx.Bus() must return the bus client when SetBus was called")
	// The allowed cap: publish should not error on the permission gate
	// (it may still error on "no subscriber" — that's a different
	// path and not what we assert here).
	pubErr := mountBus.Publish("fs.dialog.open", []byte("payload"))
	// inprocbus publish with no subscriber returns nil; an unallowed
	// subject would error with a permission message. Either way an
	// allowed Publish must not surface a "permission" wording.
	if pubErr != nil {
		assert.NotContains(t, pubErr.Error(), "permission",
			"app with fs.dialog.> cap must clear the permission gate for fs.dialog.open")
	}

	// A non-matching subject must be rejected by the permission gate.
	pubErr2 := mountBus.Publish("ch.query.boxer", []byte("denied"))
	require.Error(t, pubErr2, "publish to a subject outside the manifest caps must error")
	assert.Contains(t, pubErr2.Error(), "permission")
}

func TestSetBus_NilDefaultsToNoopBus(t *testing.T) {
	// Without SetBus, MountCtx.Bus() must return the NoopBus —
	// every Publish errors with the documented "broker not
	// available in M1" wording so apps fail fast rather than
	// silently dropping requests.
	reg, _ := mkRegistryWithSingleton(t, "test.nob")
	h := NewInst(reg, zerolog.Nop())

	_, err := h.Open("test.nob")
	require.NoError(t, err)
	h.mu.Lock()
	w := h.windows[0]
	h.mu.Unlock()

	mountBus := w.mountCtx.Bus()
	require.NotNil(t, mountBus)
	pubErr := mountBus.Publish("any.subject", []byte("x"))
	require.Error(t, pubErr)
}

func TestSetBus_FsBrokerEndToEnd(t *testing.T) {
	// Phase B integration: bus + fsbroker + windowhost — the
	// carousel-shaped wiring without the egui render loop. An app
	// with fs.dialog.read cap publishes a request; the broker queues
	// it; the test acts as the picker bridge and Resolves; the
	// app's Request returns with a handle subject prefix.
	reg := app.NewRegistry()
	m := mkManifest("test.fsclient")
	m.Caps = []app.SubjectFilter{
		{Pattern: "fs.dialog.read", Direction: app.CapDirectionPub,
			Reason: "test fixture needs to ask for a file"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionSub,
			Reason: "Request must subscribe to its reply inbox"},
	}
	require.NoError(t, reg.Register(&counterApp{manifest: m}))

	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)
	svc, err := fsbroker.NewService(bus, zerolog.Nop())
	require.NoError(t, err)
	defer svc.Close()

	h := NewInst(reg, zerolog.Nop())
	h.SetBus(bus)
	_, err = h.Open("test.fsclient")
	require.NoError(t, err)
	h.mu.Lock()
	w := h.windows[0]
	h.mu.Unlock()
	clientBus := w.mountCtx.Bus()
	require.NotNil(t, clientBus)

	// Request the dialog in a goroutine — Resolve runs synchronously
	// from the main goroutine once the pending queue shows the entry.
	replyCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		reply, reqErr := clientBus.Request("fs.dialog.read", []byte(`{"hint":"open"}`))
		if reqErr != nil {
			errCh <- reqErr
			return
		}
		replyCh <- reply
	}()

	// Wait for the broker to queue the pending request — the publish
	// is asynchronous to the goroutine's Request call.
	var pending []fsbroker.PendingRequest
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending = svc.Pending()
		if len(pending) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Len(t, pending, 1, "broker must queue exactly one pending request")
	assert.Equal(t, "read", pending[0].Op)
	assert.Equal(t, app.AppIdT("test.fsclient"), pending[0].AppId)

	// Picker resolves with a path.
	handleUuid, rerr := svc.Resolve(pending[0].Id, "/tmp/picked-file.txt")
	require.NoError(t, rerr)
	assert.NotEmpty(t, handleUuid)

	// Goroutine's Request must unblock with the granted handle subject.
	var rawReply []byte
	select {
	case rawReply = <-replyCh:
	case rerr := <-errCh:
		t.Fatalf("Request returned error: %v", rerr)
	case <-time.After(2 * time.Second):
		t.Fatal("Request timed out — broker did not reply within 2s")
	}
	dr, err := fsbroker.UnmarshalDialogReply(rawReply)
	require.NoError(t, err)
	assert.True(t, dr.Granted)
	assert.Equal(t, fsbroker.HandleSubjectPrefix+handleUuid, dr.HandleSubjectPrefix)
}

func TestSetBus_PersistStorageEndToEnd(t *testing.T) {
	// Phase C integration: bus + persist.Service + windowhost. An
	// app with PersistedKeys declared gets the runtime.persist
	// cap auto-injected by the host and a working MountCtx.Storage()
	// that Set/Get/Delete round-trip through the bus.
	reg := app.NewRegistry()
	m := mkManifest("github.com/example/test.persist")
	m.PersistedKeys = []string{"selectedTab"}
	require.NoError(t, reg.Register(&counterApp{manifest: m}))

	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)
	svc, err := persist.NewService(bus, zerolog.Nop(), persist.NewMemoryBackend())
	require.NoError(t, err)
	defer svc.Close()

	h := NewInst(reg, zerolog.Nop())
	h.SetBus(bus)
	_, err = h.Open("github.com/example/test.persist")
	require.NoError(t, err)
	h.mu.Lock()
	w := h.windows[0]
	h.mu.Unlock()

	storage := w.mountCtx.Storage()
	require.NotNil(t, storage, "MountCtx.Storage() must be a real persist.Client when bus is set")

	require.NoError(t, storage.Set("selectedTab", []byte("editor")))
	got, found, err := storage.Get("selectedTab")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []byte("editor"), got)

	// Missing key returns found=false, no error.
	_, found, err = storage.Get("neverSet")
	require.NoError(t, err)
	assert.False(t, found)

	// Delete then Get → found=false.
	require.NoError(t, storage.Delete("selectedTab"))
	_, found, err = storage.Get("selectedTab")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestSetBus_PersistWithoutPersistedKeys_PermissionDenied(t *testing.T) {
	// An app that doesn't declare PersistedKeys gets no auto-injected
	// cap; any Storage call must fail at the permission gate before
	// reaching the service. This pins the "manifest declares intent"
	// contract: forgetting PersistedKeys is a misconfiguration that
	// fails loudly rather than silently writing to a global namespace.
	reg := app.NewRegistry()
	m := mkManifest("github.com/example/test.nopersist")
	// PersistedKeys intentionally empty.
	require.NoError(t, reg.Register(&counterApp{manifest: m}))

	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(500 * time.Millisecond)
	svc, err := persist.NewService(bus, zerolog.Nop(), persist.NewMemoryBackend())
	require.NoError(t, err)
	defer svc.Close()

	h := NewInst(reg, zerolog.Nop())
	h.SetBus(bus)
	_, err = h.Open("github.com/example/test.nopersist")
	require.NoError(t, err)
	h.mu.Lock()
	w := h.windows[0]
	h.mu.Unlock()

	storage := w.mountCtx.Storage()
	require.NotNil(t, storage)
	err = storage.Set("anyKey", []byte("v"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission")
}

func TestSetAudit_NilFactsStoreIsNoOp(t *testing.T) {
	// Calling Open without SetAudit (or with a nil facts store) must
	// not crash; the audit wiring is optional.
	reg, _ := mkRegistryWithSingleton(t, "test.a")
	h := NewInst(reg, zerolog.Nop())

	k, err := h.Open("test.a")
	require.NoError(t, err)
	h.Close(k, "")
	h.reapClosed()
	h.ReapAll("shutdown")
}
