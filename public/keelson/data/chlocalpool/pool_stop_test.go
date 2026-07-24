package chlocalpool

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression, 2026-07-24 review: Stop closed every tracked worker but left
// the idle channel's entries buffered, and Acquire's fast path popped from
// idle without consulting stopped. The next Acquire therefore returned a
// worker whose subprocess had already been reaped, with a nil error, and
// the caller only found out when its writes hit a closed pipe.
//
// TestPool_AcquireAfterStopFails does not reach this: it stops the pool
// before the initial refill has landed, so idle is empty and the fast path
// never fires. This one waits for the buffered worker first.
func TestPool_AcquireAfterStopWithBufferedIdleFails(t *testing.T) {
	cfg := testConfig(t)
	cfg.MinIdle = 1
	pool := newTestPool(t, cfg)
	waitFor(t, 5*time.Second, func() bool { return pool.Stats().Idle >= 1 }, "MinIdle filled")

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, pool.Stop(stopCtx))

	w, err := pool.Acquire(context.Background())
	require.Error(t, err, "Acquire must refuse once the pool is stopped")
	assert.Contains(t, err.Error(), "stopped")
	assert.Nil(t, w, "a refused Acquire must not also hand back a worker")
}

// Stop must leave the pool's own bookkeeping self-consistent. Before the
// fix it reported Live:0 alongside Idle:1 — an idle worker that the pool
// no longer counted as live, which is precisely the one Acquire would
// hand out.
func TestPool_StopEmptiesIdleBuffer(t *testing.T) {
	cfg := testConfig(t)
	cfg.MinIdle = 2
	cfg.MaxConcurrent = 4
	pool := newTestPool(t, cfg)
	waitFor(t, 5*time.Second, func() bool { return pool.Stats().Idle >= 2 }, "MinIdle filled")

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, pool.Stop(stopCtx))

	s := pool.Stats()
	assert.True(t, s.Stopped)
	assert.Equal(t, 0, s.Live, "all workers torn down")
	assert.Equal(t, 0, s.Idle, "idle buffer must not retain reaped workers")
	assert.Equal(t, 0, s.Acquired)
}

// Acquire racing Stop must resolve one way or the other, never both. The
// select in the blocking path can see a buffered worker and a closed
// stopCh simultaneously and picks between ready cases at random, so the
// decision has to be made under the lock rather than by the select.
func TestPool_AcquireRacingStopReturnsWorkerXorError(t *testing.T) {
	cfg := testConfig(t)
	cfg.MinIdle = 2
	cfg.MaxConcurrent = 4
	pool := newTestPool(t, cfg)
	waitFor(t, 5*time.Second, func() bool { return pool.Stats().Idle >= 2 }, "MinIdle filled")

	const racers = 8
	var wg sync.WaitGroup
	results := make([]struct {
		w   *Worker
		err error
	}, racers)

	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			results[i].w, results[i].err = pool.Acquire(ctx)
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = pool.Stop(ctx)
	}()

	close(start)
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			assert.Nilf(t, r.w, "racer %d returned both a worker and an error %v", i, r.err)
			continue
		}
		assert.NotNilf(t, r.w, "racer %d returned neither a worker nor an error", i)
		// A worker handed over before Stop's snapshot is reaped under its
		// caller by design, so its liveness is not assertable here. What
		// must hold is that the pool acknowledged the handover rather than
		// serving it out of a stale buffer.
		if r.w != nil {
			_ = r.w.Close()
		}
	}

	// Whatever the interleaving, the pool ends up stopped and drained.
	s := pool.Stats()
	assert.True(t, s.Stopped)
	assert.Equal(t, 0, s.Idle, "idle buffer must not retain workers after Stop")
}

// A stopped pool refuses repeatedly; the refusal is not a one-shot that
// leaves later callers to fall through to the buffered fast path.
func TestPool_AcquireAfterStopFailsRepeatedly(t *testing.T) {
	cfg := testConfig(t)
	cfg.MinIdle = 2
	pool := newTestPool(t, cfg)
	waitFor(t, 5*time.Second, func() bool { return pool.Stats().Idle >= 2 }, "MinIdle filled")

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, pool.Stop(stopCtx))

	for i := range 5 {
		w, err := pool.Acquire(context.Background())
		require.Errorf(t, err, "Acquire %d after Stop must fail", i)
		assert.Nil(t, w)
	}
}
