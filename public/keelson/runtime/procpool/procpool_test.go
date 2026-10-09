package procpool

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeWorker struct {
	id         int64
	sp         *fakeSpawner
	release    ReleaseFunc
	closeOnce  sync.Once
	closed     chan struct{}
	closeDelay time.Duration
}

// Close stands for a worker whose teardown takes closeDelay — a sandbox being
// deleted — and which releases its slot only once that is over.
func (inst *fakeWorker) Close() (err error) {
	inst.closeOnce.Do(func() {
		if inst.closeDelay > 0 {
			time.Sleep(inst.closeDelay)
		}
		inst.sp.alive.Add(-1)
		close(inst.closed)
		inst.release()
	})
	return
}

type fakeSpawner struct {
	spawns     atomic.Int64
	alive      atomic.Int64
	maxAlive   atomic.Int64
	failWith   error
	readyAfter time.Duration
	closeDelay time.Duration
	// dieDuringSpawn releases the slot before Spawn returns, as a worker
	// whose process died while it was being readied would.
	dieDuringSpawn bool
	lastDeadline   atomic.Pointer[time.Time]
}

func (inst *fakeSpawner) Spawn(ctx context.Context, release ReleaseFunc) (w *fakeWorker, err error) {
	n := inst.spawns.Add(1)
	if d, ok := ctx.Deadline(); ok {
		inst.lastDeadline.Store(&d)
	}
	if inst.failWith != nil {
		err = inst.failWith
		return
	}
	if inst.readyAfter > 0 {
		select {
		case <-time.After(inst.readyAfter):
		case <-ctx.Done():
			err = ctx.Err()
			return
		}
	}
	a := inst.alive.Add(1)
	for {
		m := inst.maxAlive.Load()
		if a <= m || inst.maxAlive.CompareAndSwap(m, a) {
			break
		}
	}
	w = &fakeWorker{id: n, sp: inst, release: release, closed: make(chan struct{}), closeDelay: inst.closeDelay}
	if inst.dieDuringSpawn {
		_ = w.Close()
	}
	return
}

var _ SpawnerI[*fakeWorker] = (*fakeSpawner)(nil)

func newPool(t *testing.T, cfg Config, sp *fakeSpawner, logger zerolog.Logger) (p *Pool[*fakeWorker]) {
	t.Helper()
	p, err := New(cfg, SpawnerI[*fakeWorker](sp), logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Stop(ctx)
	})
	return
}

func TestConfig_Validate(t *testing.T) {
	sp := &fakeSpawner{}
	for _, tc := range []struct {
		cfg  Config
		want string
	}{
		{Config{}, "MaxConcurrent"},
		{Config{MaxConcurrent: 1, MinIdle: 2}, "MinIdle"},
		{Config{MaxConcurrent: 1, SpawnConcurrency: 2}, "SpawnConcurrency"},
	} {
		_, err := New(tc.cfg, SpawnerI[*fakeWorker](sp), zerolog.Nop())
		require.Error(t, err)
		assert.Contains(t, err.Error(), tc.want)
	}
	assert.Zero(t, sp.spawns.Load())
}

func TestPool_MinIdleZeroSpawnsOnDemandOnly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sp := &fakeSpawner{readyAfter: 7 * time.Second}
		p := newPool(t, Config{MaxConcurrent: 2}, sp, zerolog.Nop())
		time.Sleep(time.Minute)
		assert.Zero(t, sp.spawns.Load(), "no spares were asked for")

		start := time.Now()
		w, err := p.Acquire(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 7*time.Second, time.Since(start), "the caller pays the readiness time")
		require.NoError(t, w.Close())
		synctest.Wait()
		assert.Equal(t, Stats{}, p.Stats())
	})
}

func TestPool_RefillKeepsMinIdleReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sp := &fakeSpawner{readyAfter: 7 * time.Second}
		p := newPool(t, Config{MinIdle: 1, MaxConcurrent: 2}, sp, zerolog.Nop())
		time.Sleep(8 * time.Second)
		require.Equal(t, 1, p.Stats().Idle)

		start := time.Now()
		w, err := p.Acquire(t.Context())
		require.NoError(t, err)
		assert.Zero(t, time.Since(start), "a warm spare costs nothing")

		time.Sleep(8 * time.Second)
		s := p.Stats()
		assert.Equal(t, 1, s.Idle, "the spare was replaced")
		assert.Equal(t, 1, s.Acquired)
		require.NoError(t, w.Close())
	})
}

func TestPool_DeadSpareIsNotHandedOut(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sp := &fakeSpawner{}
		p := newPool(t, Config{MinIdle: 1, MaxConcurrent: 1}, sp, zerolog.Nop())
		synctest.Wait()
		require.Equal(t, 1, p.Stats().Idle)

		p.mu.Lock()
		spare := p.idle[0].w
		p.mu.Unlock()
		require.NoError(t, spare.Close())
		synctest.Wait()

		w, err := p.Acquire(t.Context())
		require.NoError(t, err)
		assert.NotSame(t, spare, w)
		assert.Equal(t, int64(2), sp.spawns.Load())
		require.NoError(t, w.Close())
	})
}

// A slot counts against MaxConcurrent until its worker has released it, not
// until Close was called — the property a licence seat needs.
func TestPool_SlotIsBusyUntilReleased(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sp := &fakeSpawner{closeDelay: 3 * time.Second}
		p := newPool(t, Config{MaxConcurrent: 1}, sp, zerolog.Nop())
		w, err := p.Acquire(t.Context())
		require.NoError(t, err)

		go func() { _ = w.Close() }()
		start := time.Now()
		w2, err := p.Acquire(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 3*time.Second, time.Since(start))
		assert.Equal(t, int64(1), sp.maxAlive.Load())
		require.NoError(t, w2.Close())
	})
}

func TestPool_WorkerDyingDuringSpawnIsAnError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sp := &fakeSpawner{dieDuringSpawn: true}
		p := newPool(t, Config{MaxConcurrent: 1}, sp, zerolog.Nop())
		_, err := p.Acquire(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exited")
		assert.Equal(t, Stats{}, p.Stats())
	})
}

func TestPool_SpawnFailureIsReturnedAndNotRetriedInALoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		boom := errors.New("no valid password found")
		sp := &fakeSpawner{failWith: boom}
		p := newPool(t, Config{MinIdle: 1, MaxConcurrent: 2}, sp, zerolog.Nop())
		time.Sleep(time.Minute)
		assert.Equal(t, int64(1), sp.spawns.Load(), "one refill attempt, then quiet")

		_, err := p.Acquire(t.Context())
		require.ErrorIs(t, err, boom)
		assert.Equal(t, Stats{}, p.Stats())
	})
}

func TestPool_SpawnTimeoutIsTheSpawnersDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sp := &fakeSpawner{readyAfter: time.Hour}
		p := newPool(t, Config{MaxConcurrent: 1, SpawnTimeout: 30 * time.Second}, sp, zerolog.Nop())
		start := time.Now()
		_, err := p.Acquire(t.Context())
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, 30*time.Second, time.Since(start))
		require.NotNil(t, sp.lastDeadline.Load())
	})
}

func TestPool_WatchdogZeroLetsAWorkerBeHeld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sp := &fakeSpawner{}
		p := newPool(t, Config{MaxConcurrent: 1}, sp, zerolog.Nop())
		w, err := p.Acquire(t.Context())
		require.NoError(t, err)
		time.Sleep(24 * time.Hour)
		select {
		case <-w.closed:
			t.Fatal("a held worker was reaped with the watchdog off")
		default:
		}
		require.NoError(t, w.Close())
	})
}

// The reap log reports the age since handover, which is what the deadline
// judges, beside the worker's whole age; a worker that idled first makes the
// two differ.
func TestPool_WatchdogReportsAgeSinceAcquisition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs syncBuffer
		sp := &fakeSpawner{}
		p := newPool(t, Config{MinIdle: 1, MaxConcurrent: 1, WatchdogMaxLifetime: 400 * time.Millisecond}, sp, zerolog.New(&logs))
		synctest.Wait()
		time.Sleep(time.Second)

		w, err := p.Acquire(t.Context())
		require.NoError(t, err)
		<-w.closed

		line := logs.find("watchdog reaping forgotten worker")
		require.NotNil(t, line, "no reap line in %q", logs.String())
		acquired := time.Duration(line["acquired_age"].(float64)) * time.Millisecond
		worker := time.Duration(line["worker_age"].(float64)) * time.Millisecond
		assert.Equal(t, 500*time.Millisecond, acquired, "first tick past the deadline")
		assert.Equal(t, time.Second+acquired, worker)
	})
}

func TestPool_StopClosesHeldWorkersAndRefusesAcquire(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sp := &fakeSpawner{closeDelay: time.Second}
		p, err := New(Config{MinIdle: 1, MaxConcurrent: 2}, SpawnerI[*fakeWorker](sp), zerolog.Nop())
		require.NoError(t, err)
		w, err := p.Acquire(t.Context())
		require.NoError(t, err)
		synctest.Wait()

		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()
		require.Error(t, p.Stop(ctx), "teardown outlasts this deadline")
		require.NoError(t, p.Stop(t.Context()), "and a second call waits it out")
		<-w.closed
		assert.Zero(t, sp.alive.Load())

		_, err = p.Acquire(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stopped")
	})
}

func TestPool_AcquireWaitsAndHonoursCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sp := &fakeSpawner{}
		p := newPool(t, Config{MaxConcurrent: 1}, sp, zerolog.Nop())
		w, err := p.Acquire(t.Context())
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		_, err = p.Acquire(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cancelled")
		require.NoError(t, w.Close())
	})
}

// Under random acquire / close / spontaneous death, the spawner never has
// more than MaxConcurrent workers alive, and Stop leaves none.
func TestPool_ConcurrencyNeverExceedsMax(t *testing.T) {
	const maxConcurrent = 3
	sp := &fakeSpawner{readyAfter: time.Millisecond, closeDelay: time.Millisecond}
	p, err := New(Config{MinIdle: 2, MaxConcurrent: maxConcurrent, SpawnConcurrency: 2, SpawnTimeout: time.Second}, SpawnerI[*fakeWorker](sp), zerolog.Nop())
	require.NoError(t, err)

	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			r := rand.New(rand.NewPCG(uint64(g), 7))
			for range 50 {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				w, err := p.Acquire(ctx)
				cancel()
				if err != nil {
					continue
				}
				time.Sleep(time.Duration(r.IntN(500)) * time.Microsecond)
				_ = w.Close()
			}
		})
	}
	wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, p.Stop(ctx))
	assert.LessOrEqual(t, sp.maxAlive.Load(), int64(maxConcurrent))
	assert.Zero(t, sp.alive.Load())
	assert.Equal(t, Stats{Stopped: true}, p.Stats())
}
