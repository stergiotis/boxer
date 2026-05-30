//go:build llm_generated_opus47

package capdemo

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
)

// setupApp wires a real bus + fsbroker + persist service, mints a per-
// app inprocbus.Client with the matching declared caps, and returns
// the App with its Mount already run. fsSvc is exposed so tests can
// drive the picker resolution from the main goroutine; cleanup closes
// both services.
func setupApp(t *testing.T) (a *App, fsSvc *fsbroker.Service, cleanup func()) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)
	fs, err := fsbroker.NewService(bus, zerolog.Nop())
	require.NoError(t, err)
	ps, err := persist.NewService(bus, zerolog.Nop(), persist.NewMemoryBackend())
	require.NoError(t, err)

	id := app.AppIdT(manifest.Id)
	// Reproduce the Cap set the windowhost would mint: the manifest's
	// declared caps + the auto-injected persist cap for PersistedKeys.
	caps := append([]app.SubjectFilter(nil), manifest.Caps...)
	caps = append(caps, app.SubjectFilter{
		Pattern:   persist.SubjectPrefix + id.SubjectAlias() + ".>",
		Direction: app.CapDirectionPub,
		Reason:    "test fixture: PersistedKeys auto-inject",
	})
	busC := bus.NewClient(id, caps)
	storage, err := persist.NewClient(busC, id)
	require.NoError(t, err)

	a = newApp()
	mc := app.NewStaticMountContext(id, zerolog.Nop(), storage, busC, nil)
	require.NoError(t, a.Mount(mc))

	fsSvc = fs
	cleanup = func() {
		fs.Close()
		ps.Close()
	}
	return
}

func TestApp_PersistSet_Get_Delete_RoundTrip(t *testing.T) {
	a, _, cleanup := setupApp(t)
	defer cleanup()

	// Save.
	a.runPersistSet("hello scratch")
	a.mu.Lock()
	require.Contains(t, a.persistStatus, "saved")
	a.mu.Unlock()

	// Load — must overwrite scratchpad with the persisted value.
	a.scratchpad = "" // clear local field to prove Get populates it
	a.runPersistGet()
	a.mu.Lock()
	assert.Equal(t, "hello scratch", a.scratchpad)
	assert.Contains(t, a.persistStatus, "loaded")
	a.mu.Unlock()

	// Delete + Get → absent.
	a.runPersistDelete()
	a.runPersistGet()
	a.mu.Lock()
	assert.Contains(t, a.persistStatus, "absent")
	a.mu.Unlock()
}

func TestApp_FsPick_HandleReadRoundTrip(t *testing.T) {
	a, fsSvc, cleanup := setupApp(t)
	defer cleanup()

	// Stage a real file for the broker's handle.read to slurp.
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "capdemo-sample.txt")
	require.NoError(t, os.WriteFile(tmpFile, []byte("the quick brown fox"), 0600))

	// Drive the dialog asynchronously: the app's runPick blocks on
	// the bus until we Resolve from this test goroutine.
	done := make(chan struct{})
	go func() {
		a.runPick()
		close(done)
	}()

	// Wait for the broker to queue a pending request, then resolve.
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
	require.NotEmpty(t, reqId, "broker must queue a pending request within 2s")
	_, err := fsSvc.Resolve(reqId, tmpFile)
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runPick did not finish within 2s of Resolve")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	assert.False(t, a.pickInFlight, "busy must clear at the end of runPick")
	assert.NotEmpty(t, a.lastHandlePrefix, "handle subject must be recorded")
	assert.Empty(t, a.fileErr)
	assert.Equal(t, len("the quick brown fox"), a.previewTotal)
	assert.Equal(t, "the quick brown fox", string(a.previewBytes))
}

func TestApp_FsPick_NoBus_Errors(t *testing.T) {
	// An app mounted on an M1 host (NoopBus) must record a fileErr
	// rather than crash. The Bus comes through MountContextI; if the
	// host wires nil, NewStaticMountContext substitutes NoopBus.
	a := newApp()
	mc := app.NewStaticMountContext(app.AppIdT(manifest.Id), zerolog.Nop(), nil, nil, nil)
	require.NoError(t, a.Mount(mc))

	a.runPick()
	a.mu.Lock()
	defer a.mu.Unlock()
	assert.NotEmpty(t, a.fileErr, "NoopBus error must surface to fileErr")
	assert.False(t, a.pickInFlight)
}

func TestApp_PersistSet_NoStorage_Errors(t *testing.T) {
	a := newApp()
	mc := app.NewStaticMountContext(app.AppIdT(manifest.Id), zerolog.Nop(), nil, nil, nil)
	require.NoError(t, a.Mount(mc))

	a.runPersistSet("anything")
	a.mu.Lock()
	defer a.mu.Unlock()
	assert.NotEmpty(t, a.persistStatus)
}

func TestManifest_DeclaresExpectedCaps(t *testing.T) {
	require.Len(t, manifest.Caps, 4)
	patterns := make([]string, 0, len(manifest.Caps))
	for _, cap := range manifest.Caps {
		patterns = append(patterns, cap.Pattern)
	}
	assert.Contains(t, patterns, fsbroker.SubjectDialogRead)
	assert.Contains(t, patterns, fsbroker.SubjectDialogWatch)
	assert.Contains(t, patterns, fsbroker.HandleSubjectPrefix+">")
	assert.Contains(t, patterns, clipboardbroker.SubjectWrite)
	require.Len(t, manifest.PersistedKeys, 1)
	assert.Equal(t, scratchpadKey, manifest.PersistedKeys[0])
}

// resolveWatchPick drives runWatchPick by waiting for the pending
// fs.dialog.watch request and resolving it with dir. The same shape as
// TestApp_FsPick_HandleReadRoundTrip's pending-then-resolve pattern.
func resolveWatchPick(t *testing.T, a *App, fsSvc *fsbroker.Service, dir string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		a.runWatchPick()
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	var reqId string
	for time.Now().Before(deadline) {
		pending := fsSvc.Pending()
		if len(pending) == 1 && pending[0].Op == "watch" {
			reqId = pending[0].Id
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.NotEmpty(t, reqId, "fsbroker must queue a watch dialog within 2s")
	_, err := fsSvc.Resolve(reqId, dir)
	require.NoError(t, err)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runWatchPick did not finish within 2s of Resolve")
	}
}

func TestApp_FsWatch_RoundTrip(t *testing.T) {
	a, fsSvc, cleanup := setupApp(t)
	defer cleanup()

	tmpDir := t.TempDir()
	resolveWatchPick(t, a, fsSvc, tmpDir)

	a.mu.Lock()
	require.True(t, a.watchActive, "watch should be active after pick")
	assert.Equal(t, "inotify", a.watchBackend, "default backend on tmpfs/ext4 should be inotify")
	assert.Empty(t, a.watchErr)
	a.mu.Unlock()

	// Wait briefly for the kernel to register the watch FD, then write
	// a file inside the watched dir; the broker should publish a Create
	// event and the app's handler should land it in watchEvents.
	time.Sleep(60 * time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "hello.txt"), []byte("hi"), 0o644))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		seen := false
		for _, ev := range a.watchEvents {
			if ev.Name == "hello.txt" && ev.Kind == fsbroker.WatchEventCreate {
				seen = true
				break
			}
		}
		a.mu.Unlock()
		if seen {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	t.Fatalf("create event for hello.txt never arrived; got %d events: %+v",
		len(a.watchEvents), a.watchEvents)
}

func TestApp_FsWatch_PollFallback_BackendReported(t *testing.T) {
	a, fsSvc, cleanup := setupApp(t)
	defer cleanup()

	// Force the poller via the bound checkbox state.
	a.watchUsePoller = true

	tmpDir := t.TempDir()
	resolveWatchPick(t, a, fsSvc, tmpDir)

	a.mu.Lock()
	defer a.mu.Unlock()
	require.True(t, a.watchActive)
	assert.Equal(t, "poller", a.watchBackend, "PollFallback must route to the poller backend")
	assert.Empty(t, a.watchErr)
}

func TestApp_FsWatch_Recursive_RoundTrip(t *testing.T) {
	// Recursive checkbox plumbs through to WatchRequest.Recursive so
	// events from a pre-existing subdirectory surface with their
	// forward-slash relpath.
	a, fsSvc, cleanup := setupApp(t)
	defer cleanup()

	a.watchRecursive = true

	tmpDir := t.TempDir()
	subdir := filepath.Join(tmpDir, "sub")
	require.NoError(t, os.Mkdir(subdir, 0o755))

	resolveWatchPick(t, a, fsSvc, tmpDir)
	a.mu.Lock()
	require.True(t, a.watchActive)
	assert.Empty(t, a.watchErr)
	a.mu.Unlock()

	// Inotify AddWatch is synchronous but the kernel's queue plumbing
	// races on some kernels; give it a brief window.
	time.Sleep(60 * time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(subdir, "deep.txt"), []byte("d"), 0o644))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		seen := false
		for _, ev := range a.watchEvents {
			if ev.Name == "sub/deep.txt" {
				seen = true
				break
			}
		}
		a.mu.Unlock()
		if seen {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	t.Fatalf("recursive event for sub/deep.txt never arrived; got %d events: %+v",
		len(a.watchEvents), a.watchEvents)
}

func TestApp_FsWatch_StopReleasesSubscription(t *testing.T) {
	a, fsSvc, cleanup := setupApp(t)
	defer cleanup()

	tmpDir := t.TempDir()
	resolveWatchPick(t, a, fsSvc, tmpDir)

	// Trigger one event so we know the pipe is live.
	time.Sleep(60 * time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0o644))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		gotEvent := len(a.watchEvents) > 0
		a.mu.Unlock()
		if gotEvent {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Stop the watch; the handle stays alive but the subscription is gone.
	a.runWatchStop()

	a.mu.Lock()
	assert.False(t, a.watchActive)
	assert.Nil(t, a.watchUnsubscribe)
	preStop := len(a.watchEvents)
	a.mu.Unlock()

	// Touch the directory again; no new events should land.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0o644))
	time.Sleep(200 * time.Millisecond)

	a.mu.Lock()
	defer a.mu.Unlock()
	// The handler is gated on watchActive — even if the broker's pump
	// raced a Closed event past the unsubscribe, the active-check drops
	// it.
	assert.Equal(t, preStop, len(a.watchEvents),
		"no new events should append after Stop; got %d before, %d after",
		preStop, len(a.watchEvents))
	_ = fsSvc // keep service alive across the assertion window
}
