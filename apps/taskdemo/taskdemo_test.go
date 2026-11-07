package taskdemo

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// setupApp wires an in-proc bus + a per-app client with the manifest
// caps and a lifetime scope the way the host does, then runs Mount so the
// taskmonitor widget is live. cleanup is the host's close: retire the
// scope, unmount, wait for the drain.
func setupApp(t *testing.T) (a *App, scope *lifetime.Scope, cleanup func()) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)
	id := app.AppIdT(manifest.Id)
	busC := bus.NewClient(id, manifest.Caps)
	a = newApp()
	mc := app.NewStaticMountContext(id, zerolog.Nop(), nil, busC, nil)
	scope = lifetime.New(nil)
	mc.SetLifetime(scope)
	require.NoError(t, a.Mount(mc))
	cleanup = func() {
		scope.Retire()
		_ = a.Unmount(mc)
		<-scope.Done()
	}
	return
}

// waitFor polls a predicate up to 2s; replaces fixed sleeps in tests
// that race the bus dispatch goroutine.
func waitFor(predicate func() bool) (ok bool) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			ok = true
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	return
}

func TestManifest_DeclaresProducerCaps(t *testing.T) {
	require.Len(t, manifest.Caps, 1)
	cap := manifest.Caps[0]
	assert.Equal(t, "task.>", cap.Pattern)
	assert.Equal(t, app.CapDirectionBoth, cap.Direction)
}

func TestApp_ShortTaskRunsToDone(t *testing.T) {
	a, _, cleanup := setupApp(t)
	defer cleanup()

	a.durationSec = 0.1 // 100ms

	a.startTask(task.UnitItems)
	require.True(t, waitFor(func() bool {
		return a.monitor.InflightCount() == 0 && a.monitor.HistoryCount() == 1
	}), "task should reach history within 2s")
}

func TestApp_SimulateErrorReachesErrorTerminal(t *testing.T) {
	a, _, cleanup := setupApp(t)
	defer cleanup()

	a.durationSec = 0.2 // 200ms; error trigger at ~160ms
	a.simulateError = true

	a.startTask(task.UnitBytes)
	require.True(t, waitFor(func() bool {
		return a.monitor.HistoryCount() == 1
	}), "errored task should reach history within 2s")
}

// The host's close (ADR-0322): the scope is retired, which cancels the
// worker's task, Unmount returns without waiting, and the drain ends once
// the worker has left.
func TestApp_UnmountCancelsInFlight(t *testing.T) {
	a, scope, _ := setupApp(t)

	a.durationSec = 10.0 // long enough that the close is the only way out
	a.startTask(task.UnitItems)

	require.True(t, waitFor(func() bool { return a.monitor.InflightCount() > 0 }),
		"task should be in-flight before the close")

	scope.Retire()
	require.NoError(t, a.Unmount(nil))
	select {
	case <-scope.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the worker did not leave the scope after the retire")
	}
	assert.NoError(t, a.Unmount(nil), "Unmount is idempotent")
	// Nothing starts once the scope retired: the drain stays done.
	a.startTask(task.UnitItems)
	<-scope.Done()
}

func TestApp_NoBusGracefulNoop(t *testing.T) {
	a := newApp()
	mc := app.NewStaticMountContext(app.AppIdT(manifest.Id), zerolog.Nop(), nil, nil, nil)
	require.NoError(t, a.Mount(mc))

	// NoBus path: StaticMountContext substitutes NoopBus, task.ForApp
	// wraps it; monitor.Start errors on the watch subscribe. startTask
	// then errors at Spawn — no panic, no inflight row.
	a.startTask(task.UnitItems)
	time.Sleep(50 * time.Millisecond)
	assert.Zero(t, a.monitor.InflightCount())
}
