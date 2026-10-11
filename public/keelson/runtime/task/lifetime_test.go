package task

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

// A task spawned through ForApp holds the instance's lifetime scope until
// its worker reports its end, past the mount cancel that turns the handle
// terminal; a spawn after the scope retired is refused (ADR-0322).
func TestForAppTasksHoldTheLifetimeUntilTheWorkerEnds(t *testing.T) {
	busInst := inprocbus.NewInst(zerolog.Nop())
	stop := make(chan struct{})
	mc := app.NewStaticMountContext("test.app", zerolog.Nop(), nil, busInst.NewClient("test.app", ProducerCaps()), stop)
	scope := lifetime.New(nil)
	mc.SetLifetime(scope)
	api := ForApp(mc)

	h, err := api.Spawn(context.Background(), SpawnOpts{Kind: "test"})
	require.NoError(t, err)
	close(stop)
	scope.Retire()
	require.Eventually(t, func() bool { return h.Cancelled() }, 2*time.Second, time.Millisecond, "the mount cancel reaches the task")
	time.Sleep(20 * time.Millisecond)
	select {
	case <-scope.Done():
		t.Fatal("the scope drained while the worker still ran")
	default:
	}
	_, err = api.Spawn(context.Background(), SpawnOpts{Kind: "test"})
	assert.Error(t, err, "no task starts after the scope retired")

	require.NoError(t, h.Done(nil))
	require.NoError(t, h.Done(nil), "Done stays idempotent")
	require.Eventually(t, func() bool {
		select {
		case <-scope.Done():
			return true
		default:
			return false
		}
	}, 2*time.Second, time.Millisecond)
}
