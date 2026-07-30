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
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
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
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)
	fs, err := fsbroker.NewService(bus, zerolog.Nop())
	require.NoError(t, err)
	ps, err := persist.NewService(bus, zerolog.Nop(), persist.NewMemoryBackend())
	require.NoError(t, err)

	id := app.AppIdT("github.com/stergiotis/boxer/apps/play")
	caps := []app.SubjectFilter{
		{Pattern: fsbroker.SubjectDialogRead, Direction: app.CapDirectionPub,
			Reason: "Load .sql via Powerbox"},
		{Pattern: fsbroker.HandleSubjectPrefix + ">", Direction: app.CapDirectionPub,
			Reason: "read granted handle"},
		// Host-injected for PersistedKeys=[lastSql].
		{Pattern: persist.SubjectPrefix + id.SubjectAlias() + ".>", Direction: app.CapDirectionPub,
			Reason: "test fixture: persist auto-inject"},
	}
	busC := bus.NewClient(id, caps)
	storage, err := persist.NewClient(busC, id)
	require.NoError(t, err)

	graph := newLiveQueryGraph(nil, memory.NewGoAllocator(), 10)
	inst = NewPlayApp(nil, graph, "-- initial")
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
	inst := NewPlayApp(nil, graph, "-- initial")
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

	inst2 := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "-- default")
	inst2.SetCapabilities(inst.bus, inst.storage, zerolog.Nop())
	inst2.RestorePersistedSql()
	assert.Equal(t, "SELECT 1 AS persisted", inst2.sql,
		"Restore must replace default with the persisted value")
}

func TestPlayApp_RestorePersistedSql_NilStorage_NoOp(t *testing.T) {
	graph := newLiveQueryGraph(nil, memory.NewGoAllocator(), 10)
	inst := NewPlayApp(nil, graph, "-- initial")
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
	// Four declared Caps: fs dialog + fs handle wildcard + chlocalbroker
	// pool for the time-range evaluator + windowhost.open for the
	// Save-as-applet launch (ADR-0135 §SD7). The applet-store save cap moved
	// out with the O4 authoring form (now apps/sqlappletcreator).
	require.Len(t, m.Caps, 4)
	patterns := make([]string, 0, len(m.Caps))
	for _, cap := range m.Caps {
		patterns = append(patterns, cap.Pattern)
	}
	assert.Contains(t, patterns, fsbroker.SubjectDialogRead)
	assert.Contains(t, patterns, fsbroker.HandleSubjectPrefix+">")
	assert.Contains(t, patterns, "ch.local.exec."+timerangepicker.PoolName)
	assert.Contains(t, patterns, windowhost.OpenSubject)
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
