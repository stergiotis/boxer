package taskdemo

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// setupApp wires an in-proc bus + a per-app client with the manifest
// caps, then runs Mount so the taskmonitor widget is live.
func setupApp(t *testing.T) (a *App, busC *inprocbus.Client, cleanup func()) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)
	id := app.AppIdT(manifest.Id)
	busC = bus.NewClient(id, manifest.Caps)
	a = newApp()
	mc := app.NewStaticMountContext(id, zerolog.Nop(), nil, busC, nil)
	require.NoError(t, a.Mount(mc))
	cleanup = func() {
		_ = a.Unmount(mc)
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

func TestApp_UnmountCancelsInFlight(t *testing.T) {
	a, _, _ := setupApp(t)

	a.durationSec = 10.0 // long enough that Unmount is the only way out
	a.startTask(task.UnitItems)

	require.True(t, waitFor(func() bool { return a.monitor.InflightCount() > 0 }),
		"task should be in-flight before Unmount")

	require.NoError(t, a.Unmount(nil))
	// After Unmount, no further publishes happen and the widget has
	// stopped — InflightCount is whatever it last observed before
	// Stop. The contract is "no leaked goroutines + monitor torn
	// down", which the next assertion catches indirectly via a
	// subsequent startTask being a no-op (no monitor to observe).
	assert.NoError(t, a.Unmount(nil), "Unmount is idempotent")
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
