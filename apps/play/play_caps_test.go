package play

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker"
)

// setupPlayWithCaps wires bus + fsbroker + persist + a per-app bus
// client carrying the manifest-declared caps + the host-injected
// persist cap, then constructs PlayApp with the caps already attached.
// Mirrors capdemo's setupApp test fixture.
func setupPlayWithCaps(t *testing.T) (inst *PlayApp, fsSvc *fsbroker.Service, cleanup func()) {
	return setupPlayWithCapsTimeout(t, 2*time.Second)
}

// setupPlayWithCapsTimeout is setupPlayWithCaps with the transport's default
// request wait as a parameter, so a test can make that default deliberately
// too short and prove the picker path does not rely on it.
func setupPlayWithCapsTimeout(t *testing.T, busTimeout time.Duration) (inst *PlayApp, fsSvc *fsbroker.Service, cleanup func()) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(busTimeout)
	fs, err := fsbroker.NewService(bus, zerolog.Nop())
	require.NoError(t, err)
	ps, err := persist.NewService(bus, zerolog.Nop(), persist.NewMemoryBackend())
	require.NoError(t, err)

	id := app.AppIdT("github.com/stergiotis/boxer/apps/play")
	// The MANIFEST's own caps, not a hand-copy of them. A copy drifts, and it
	// drifted in exactly the direction that matters: it kept granting
	// fs.handle.> after the manifest stopped declaring it, so the round-trip
	// test below would have gone on passing on an authority the real app no
	// longer has. Only the host-injected persist cap is added, because the
	// windowhost derives that one from PersistedKeys rather than the manifest.
	caps := append([]app.SubjectFilter(nil), (&PlayLauncher{}).Manifest().Caps...)
	caps = append(caps, app.SubjectFilter{
		Pattern:   persist.SubjectPrefix + id.SubjectAlias() + ".>",
		Direction: app.CapDirectionPub,
		Reason:    "test fixture: persist auto-inject",
	})
	busC := bus.NewClient(id, caps)
	storage, err := persist.NewClient(busC, id)
	require.NoError(t, err)

	graph := newLiveQueryGraph(nil, memory.NewGoAllocator(), 10)
	inst = NewPlayApp(nil, graph, "-- initial", nil)
	inst.SetCapabilities(busC, storage, zerolog.Nop())

	fsSvc = fs
	cleanup = func() {
		fs.Close()
		ps.Close()
	}
	return
}

func TestPlayApp_LoadFromPicker_RoundTrip(t *testing.T) {
	inst, fsSvc, cleanup := setupPlayWithCaps(t)
	defer cleanup()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "sample.sql")
	require.NoError(t, os.WriteFile(tmpFile, []byte("SELECT 42 AS hello"), 0600))

	done := make(chan struct{})
	go func() {
		inst.loadFromPicker()
		close(done)
	}()

	// Resolve the pending dialog from the main goroutine — simulates
	// the carousel's picker bridge.
	var reqId string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending := fsSvc.Pending()
		if len(pending) == 1 {
			reqId = pending[0].Id
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotEmpty(t, reqId)
	_, err := fsSvc.Resolve(reqId, tmpFile)
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loadFromPicker did not finish within 2s of Resolve")
	}

	inst.pickMu.Lock()
	assert.False(t, inst.pickInFlight, "busy must clear at end of loadFromPicker")
	assert.Empty(t, inst.pickErr)
	inst.pickMu.Unlock()

	// The goroutine only stashes; the buffer lands on the render thread at the
	// next frame top (consumePickedSql) — inst.sql is render-thread-only, so
	// the goroutine assigning it directly would race a concurrent frame.
	inst.consumePickedSql()
	assert.Equal(t, "SELECT 42 AS hello", inst.sql, "editor buffer must be replaced with file contents")
}

func TestPlayApp_LoadFromPicker_NilBus_NoOp(t *testing.T) {
	graph := newLiveQueryGraph(nil, memory.NewGoAllocator(), 10)
	inst := NewPlayApp(nil, graph, "-- initial", nil)
	// No SetCapabilities call → inst.bus stays nil.

	inst.loadFromPicker() // must not panic
	assert.Equal(t, "-- initial", inst.sql)
}

func TestPlayApp_RestorePersistedSql_ReadsALegacyValue(t *testing.T) {
	// The one-release read bridge (ADR-0148 §SD8): nothing writes the key
	// any more, so the test writes it the way a pre-workingset session
	// left it and asserts a fresh app still finds it.
	inst, _, cleanup := setupPlayWithCaps(t)
	defer cleanup()
	require.NoError(t, inst.storage.Set(persistKeyLastSql, []byte("SELECT 1 AS persisted")))

	inst2 := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "-- default", nil)
	inst2.SetCapabilities(inst.bus, inst.storage, zerolog.Nop())
	inst2.RestorePersistedSql()
	assert.Equal(t, "SELECT 1 AS persisted", inst2.sql,
		"Restore must replace default with the persisted value")
}

func TestPlayApp_RestorePersistedSql_NilStorage_NoOp(t *testing.T) {
	graph := newLiveQueryGraph(nil, memory.NewGoAllocator(), 10)
	inst := NewPlayApp(nil, graph, "-- initial", nil)
	// No SetCapabilities → inst.storage stays nil.
	inst.RestorePersistedSql()
	assert.Equal(t, "-- initial", inst.sql, "Restore is a no-op when storage is nil")
}

func TestPlayApp_RestorePersistedSql_EmptyValue_KeepsDefault(t *testing.T) {
	inst, _, cleanup := setupPlayWithCaps(t)
	defer cleanup()
	// Storage has nothing set yet — Restore should leave inst.sql alone.
	original := inst.sql
	inst.RestorePersistedSql()
	assert.Equal(t, original, inst.sql)
}

func TestManifest_DeclaresFsAndPersist(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	// Five declared Caps: fs dialog + chlocalbroker pool for the time-range
	// evaluator + windowhost.open for the Save-as-applet launch (ADR-0135
	// §SD7) + adhoc.publish for the timeseries fixture lab (ADR-0163 §SD7) +
	// clipboard.write for the Copy buttons. The applet-store save cap moved
	// out with the O4 authoring form (now apps/sqlappletcreator); the
	// fs.handle.> wildcard came out once the broker's dynamic per-handle
	// grant was shown to be sufficient.
	//
	// The count is asserted on purpose: a capability is an authority this app
	// is granted, so adding one has to be a deliberate edit here rather than
	// something that rides along with a feature.
	require.Len(t, m.Caps, 5)
	patterns := make([]string, 0, len(m.Caps))
	for _, cap := range m.Caps {
		patterns = append(patterns, cap.Pattern)
	}
	assert.Contains(t, patterns, fsbroker.SubjectDialogRead)
	// No static fs.handle.> — the broker grants the narrow fs.handle.{uuid}.>
	// when the USER approves the picker and revokes it on close, so the
	// wildcard would only convert a per-file, revocable grant into standing
	// authority over every handle. TestPlayApp_LoadFromPicker_RoundTrip runs
	// on exactly these caps, which is what shows it is unnecessary.
	for _, cap := range m.Caps {
		assert.NotContains(t, cap.Pattern, fsbroker.HandleSubjectPrefix,
			"handle caps are granted dynamically, never declared")
	}
	assert.Contains(t, patterns, "ch.local.exec."+timerangepicker.PoolName)
	assert.Contains(t, patterns, windowhost.OpenSubject)
	assert.Contains(t, patterns, adhocdata.SubjectPublish)
	// clipboard.write — the Definition pane's per-fence Copy buttons and
	// gloss/taggedid's block face both request it. It was missing until
	// 2026-08-16, which made those Copy buttons render and then be denied;
	// CanCopy gates the affordance on the bus, not on the grant, so an
	// undeclared cap fails silently. Declared, it is audited on every use.
	assert.Contains(t, patterns, clipboardbroker.SubjectWrite)
	assert.NotContains(t, patterns, appletstore.SubjectSave)
	// PersistedKeys → host-injected runtime.persist.play.> cap. Both keys
	// are read-only now (ADR-0148 §SD8): the buffers are saved as a
	// workingset record, and the cap is what the one-release read bridge
	// needs. Drop the keys and this assertion when the bridge retires.
	require.Len(t, m.PersistedKeys, 2)
	assert.Contains(t, m.PersistedKeys, persistKeyLastSql)
	assert.Contains(t, m.PersistedKeys, persistKeyTimelineBandsSql)
	// Workingset participation (§SD7) requires a launch kind.
	assert.True(t, m.Workingset)
	assert.Equal(t, launchcfg.Kind, m.LaunchKind)
	require.NoError(t, m.Validate())
}

// TestRegisteredAsFactory pins the half of workingset participation the
// manifest cannot state (ADR-0148 §SD4): a singleton registration hands one
// instance to every window, and a config — a restored record included — is
// delivered at the Mount that runs once per instance, so play must mint one
// instance per Open. The registration mode is what keelson('apps').registration
// reports.
func TestRegisteredAsFactory(t *testing.T) {
	for _, r := range app.DefaultRegistry.Registrations() {
		if r.Manifest.Id != AppId {
			continue
		}
		assert.False(t, r.Singleton, "play declares a workingset, so each window needs its own instance")
		return
	}
	t.Fatalf("play is not in the default registry; its init() should have registered %s", AppId)
}

// TestPlayApp_LoadFromPicker_OutlastsTheTransportDefault is the regression
// guard on a defect the round-trip test above could not see, because it
// resolves the dialog immediately: fs.dialog.read is answered when somebody
// finishes choosing in a file picker, and the bus transport's own default is
// far shorter than that. Under it the load failed with "request timeout" while
// the picker was still on screen.
//
// The default is set deliberately tiny here and the dialog held well past it.
// A picker path that waits the transport default fails; one that asks for
// fsbroker.DialogTimeout does not.
func TestPlayApp_LoadFromPicker_OutlastsTheTransportDefault(t *testing.T) {
	inst, fsSvc, cleanup := setupPlayWithCapsTimeout(t, 40*time.Millisecond)
	defer cleanup()

	tmpFile := filepath.Join(t.TempDir(), "slow.sql")
	require.NoError(t, os.WriteFile(tmpFile, []byte("SELECT 'picked late'"), 0600))

	done := make(chan struct{})
	go func() {
		inst.loadFromPicker()
		close(done)
	}()

	var reqId string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pending := fsSvc.Pending(); len(pending) == 1 {
			reqId = pending[0].Id
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.NotEmpty(t, reqId)

	// Stand in for a person reading the dialog: far longer than the transport
	// default, far shorter than what the picker path asks for.
	time.Sleep(300 * time.Millisecond)
	_, err := fsSvc.Resolve(reqId, tmpFile)
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loadFromPicker did not finish after a slow Resolve")
	}

	inst.pickMu.Lock()
	assert.Empty(t, inst.pickErr, "a dialog answered slowly is not an error")
	inst.pickMu.Unlock()
}
