//go:build llm_generated_opus47

package chlocalpool

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireBinary skips the test if /usr/bin/clickhouse-local is absent.
// Most pool tests need a real subprocess; non-binary tests stand alone.
func requireBinary(t *testing.T) (path string) {
	t.Helper()
	p, err := exec.LookPath(DefaultBinaryPath)
	if err != nil {
		t.Skipf("clickhouse-local not installed at %s: %v", DefaultBinaryPath, err)
	}
	path = p
	return
}

func testConfig(t *testing.T) (cfg Config) {
	t.Helper()
	cfg = Config{
		BaseTmpDir:          t.TempDir(),
		MinIdle:             1,
		MaxConcurrent:       3,
		SpawnConcurrency:    1,
		MaxMemoryPerWorker:  256 << 20,
		SpawnTimeout:        5 * time.Second,
		WatchdogMaxLifetime: 60 * time.Second,
		KillGrace:           250 * time.Millisecond,
		StderrCapBytes:      4096,
	}
	return
}

func newTestPool(t *testing.T, cfg Config) (p *Pool) {
	t.Helper()
	requireBinary(t)
	logger := zerolog.New(zerolog.NewTestWriter(t))
	pool, err := New(cfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pool.Stop(ctx)
	})
	p = pool
	return
}

// waitFor polls until cond returns true or timeout fires. Used for
// concurrent assertions where the exact moment of state change is
// observably non-deterministic but bounded.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waitFor: condition %q not met within %s", msg, timeout)
}

func TestConfig_WithDefaultsFillsZeros(t *testing.T) {
	cfg := Config{}.withDefaults()
	assert.Equal(t, DefaultBinaryPath, cfg.BinaryPath)
	assert.Equal(t, DefaultMinIdle, cfg.MinIdle)
	assert.Equal(t, DefaultMaxConcurrent, cfg.MaxConcurrent)
	assert.Equal(t, DefaultSpawnConcurrency, cfg.SpawnConcurrency)
	assert.Equal(t, DefaultMaxMemoryPerWorker, cfg.MaxMemoryPerWorker)
	assert.Equal(t, DefaultSpawnTimeout, cfg.SpawnTimeout)
	assert.Equal(t, DefaultWatchdogMaxLifetime, cfg.WatchdogMaxLifetime)
	assert.Equal(t, DefaultKillGrace, cfg.KillGrace)
	assert.Equal(t, DefaultStderrCapBytes, cfg.StderrCapBytes)
}

func TestConfig_ValidateRejectsMinIdleExceedingMax(t *testing.T) {
	cfg := Config{MinIdle: 10, MaxConcurrent: 5}.withDefaults()
	cfg.MinIdle = 10 // override default
	cfg.MaxConcurrent = 5
	err := cfg.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MinIdle")
}

func TestConfig_ValidateRejectsSpawnConcurrencyExceedingMax(t *testing.T) {
	cfg := Config{
		MinIdle:          1,
		MaxConcurrent:    2,
		SpawnConcurrency: 5,
	}.withDefaults()
	cfg.SpawnConcurrency = 5
	cfg.MaxConcurrent = 2
	err := cfg.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SpawnConcurrency")
}

func TestNew_RejectsMissingBinary(t *testing.T) {
	cfg := Config{
		BinaryPath: "/nonexistent/clickhouse-local-xyz",
	}
	_, err := New(cfg, zerolog.Nop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "binary")
}

func TestPool_RoundTrip_SelectOne(t *testing.T) {
	pool := newTestPool(t, testConfig(t))
	// Wait for at least one warm worker to appear.
	waitFor(t, 3*time.Second, func() bool { return pool.Stats().Idle >= 1 }, "MinIdle filled")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer w.Close()

	require.NoError(t, w.WriteSQL("SELECT 1", "TabSeparated"))
	var buf bytes.Buffer
	_, err = io.Copy(&buf, w.Stdout())
	require.NoError(t, err)
	require.NoError(t, w.Wait())
	assert.Equal(t, "1\n", buf.String())
}

func TestPool_DifferentFormats(t *testing.T) {
	pool := newTestPool(t, testConfig(t))
	waitFor(t, 3*time.Second, func() bool { return pool.Stats().Idle >= 1 }, "MinIdle filled")

	cases := []struct {
		format string
		want   string
	}{
		{"TabSeparated", "1\n"},
		{"CSV", "1\n"},
		{"JSONEachRow", `{"1":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			w, err := pool.Acquire(ctx)
			require.NoError(t, err)
			defer w.Close()
			require.NoError(t, w.WriteSQL("SELECT 1", tc.format))
			var buf bytes.Buffer
			_, err = io.Copy(&buf, w.Stdout())
			require.NoError(t, err)
			require.NoError(t, w.Wait())
			assert.Contains(t, buf.String(), tc.want)
		})
	}
}

func TestPool_BadSQLSurfacesStderr(t *testing.T) {
	pool := newTestPool(t, testConfig(t))
	waitFor(t, 3*time.Second, func() bool { return pool.Stats().Idle >= 1 }, "MinIdle filled")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer w.Close()

	require.NoError(t, w.WriteSQL("SELECT * FROM nonexistent_table_x_y_z", "TabSeparated"))
	_, _ = io.Copy(io.Discard, w.Stdout())
	err = w.Wait()
	require.Error(t, err)
	tail := w.StderrTail()
	assert.NotEmpty(t, tail, "stderr should carry the CH error message")
}

func TestPool_AcquireBlocksUntilCloseFreesSlot(t *testing.T) {
	cfg := testConfig(t)
	cfg.MinIdle = 1
	cfg.MaxConcurrent = 1
	pool := newTestPool(t, cfg)
	waitFor(t, 3*time.Second, func() bool { return pool.Stats().Idle >= 1 }, "MinIdle filled")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w1, err := pool.Acquire(ctx)
	require.NoError(t, err)

	// Second Acquire must block (MaxConcurrent=1, w1 still acquired).
	acquired := make(chan *Worker, 1)
	go func() {
		w2, _ := pool.Acquire(ctx)
		acquired <- w2
	}()

	// Should not arrive yet.
	select {
	case <-acquired:
		t.Fatal("second Acquire returned before first was closed")
	case <-time.After(200 * time.Millisecond):
	}

	// Closing w1 frees the slot; refill spawns a replacement.
	require.NoError(t, w1.WriteSQL("SELECT 1", "TabSeparated"))
	_, _ = io.Copy(io.Discard, w1.Stdout())
	require.NoError(t, w1.Wait())
	require.NoError(t, w1.Close())

	select {
	case w2 := <-acquired:
		require.NotNil(t, w2)
		_ = w2.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("second Acquire never unblocked after close")
	}
}

func TestPool_AcquireRespectsContextCancel(t *testing.T) {
	cfg := testConfig(t)
	cfg.MinIdle = 1
	cfg.MaxConcurrent = 1
	pool := newTestPool(t, cfg)
	waitFor(t, 3*time.Second, func() bool { return pool.Stats().Idle >= 1 }, "MinIdle filled")

	bg := context.Background()
	w, err := pool.Acquire(bg)
	require.NoError(t, err)
	defer w.Close()

	// Pool is now saturated. Acquire with a short ctx should bail.
	ctx, cancel := context.WithTimeout(bg, 200*time.Millisecond)
	defer cancel()
	_, err = pool.Acquire(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

func TestPool_OnDemandSpawnWhenIdleDrainedButHeadroomExists(t *testing.T) {
	cfg := testConfig(t)
	cfg.MinIdle = 1
	cfg.MaxConcurrent = 3
	cfg.SpawnConcurrency = 1
	pool := newTestPool(t, cfg)
	waitFor(t, 3*time.Second, func() bool { return pool.Stats().Idle >= 1 }, "MinIdle filled")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// First Acquire takes the warm worker.
	w1, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer w1.Close()

	// Acquire again immediately, before refill has a chance to land —
	// the pool must spawn on demand (idle empty, live < MaxConcurrent).
	w2, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer w2.Close()

	waitFor(t, 3*time.Second, func() bool { return pool.Stats().Live >= 2 }, "live≥2 after two acquires")
	assert.GreaterOrEqual(t, pool.Stats().Acquired, 2)
}

func TestPool_RefillReplenishesIdleAfterAcquire(t *testing.T) {
	cfg := testConfig(t)
	cfg.MinIdle = 2
	cfg.MaxConcurrent = 4
	pool := newTestPool(t, cfg)
	waitFor(t, 5*time.Second, func() bool { return pool.Stats().Idle >= 2 }, "MinIdle=2 filled")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer w.Close()

	// After acquire, idle dropped to 1. Refill should restore to 2.
	waitFor(t, 5*time.Second, func() bool { return pool.Stats().Idle >= 2 }, "idle refilled to MinIdle")
}

func TestPool_StopDrainsAndJoins(t *testing.T) {
	cfg := testConfig(t)
	cfg.MinIdle = 2
	cfg.MaxConcurrent = 4
	pool := newTestPool(t, cfg)
	waitFor(t, 5*time.Second, func() bool { return pool.Stats().Idle >= 2 }, "MinIdle filled")

	// Acquire one (uncommitted — will be force-closed by Stop).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w, err := pool.Acquire(ctx)
	require.NoError(t, err)
	_ = w

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	require.NoError(t, pool.Stop(stopCtx))
	assert.True(t, pool.Stats().Stopped)
	assert.Equal(t, 0, pool.Stats().Live, "all workers torn down by Stop")
}

func TestPool_AcquireAfterStopFails(t *testing.T) {
	cfg := testConfig(t)
	cfg.MinIdle = 1
	pool := newTestPool(t, cfg)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer stopCancel()
	require.NoError(t, pool.Stop(stopCtx))

	_, err := pool.Acquire(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stopped")
}

func TestPool_WatchdogReapsForgottenWorker(t *testing.T) {
	cfg := testConfig(t)
	cfg.MinIdle = 1
	cfg.MaxConcurrent = 2
	cfg.WatchdogMaxLifetime = 400 * time.Millisecond
	pool := newTestPool(t, cfg)
	waitFor(t, 3*time.Second, func() bool { return pool.Stats().Idle >= 1 }, "MinIdle filled")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w, err := pool.Acquire(ctx)
	require.NoError(t, err)
	// Deliberately do NOT Close — the watchdog must reap it.
	// Tick rate = WatchdogMaxLifetime/4 = 100ms; wait generously.
	select {
	case <-w.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not reap forgotten worker within 2s (deadline=400ms)")
	}
}

func TestWorker_CloseIsIdempotent(t *testing.T) {
	pool := newTestPool(t, testConfig(t))
	waitFor(t, 3*time.Second, func() bool { return pool.Stats().Idle >= 1 }, "MinIdle filled")

	w, err := pool.Acquire(context.Background())
	require.NoError(t, err)

	// SELECT then close cleanly.
	require.NoError(t, w.WriteSQL("SELECT 1", "TabSeparated"))
	_, _ = io.Copy(io.Discard, w.Stdout())
	require.NoError(t, w.Wait())
	require.NoError(t, w.Close())
	require.NoError(t, w.Close()) // must not deadlock or error
}

func TestWorker_CloseWithoutSubmitTerminatesAndCleansUp(t *testing.T) {
	pool := newTestPool(t, testConfig(t))
	waitFor(t, 3*time.Second, func() bool { return pool.Stats().Idle >= 1 }, "MinIdle filled")

	w, err := pool.Acquire(context.Background())
	require.NoError(t, err)
	// Close without WriteSQL — worker is blocked on stdin; we SIGTERM it.
	require.NoError(t, w.Close())
	select {
	case <-w.Done():
	case <-time.After(time.Second):
		t.Fatal("close didn't mark Done within 1s")
	}
}

func TestPool_ConcurrentAcquireRoundTripSafely(t *testing.T) {
	cfg := testConfig(t)
	cfg.MinIdle = 2
	cfg.MaxConcurrent = 4
	pool := newTestPool(t, cfg)
	waitFor(t, 5*time.Second, func() bool { return pool.Stats().Idle >= 2 }, "MinIdle filled")

	const N = 6
	var wg sync.WaitGroup
	errCh := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			w, err := pool.Acquire(ctx)
			if err != nil {
				errCh <- err
				return
			}
			defer w.Close()
			if err = w.WriteSQL("SELECT 1", "TabSeparated"); err != nil {
				errCh <- err
				return
			}
			var buf bytes.Buffer
			if _, err = io.Copy(&buf, w.Stdout()); err != nil {
				errCh <- err
				return
			}
			if err = w.Wait(); err != nil {
				errCh <- err
				return
			}
			if !strings.Contains(buf.String(), "1") {
				errCh <- assertionErr("unexpected stdout")
				return
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent worker run failed: %v", err)
	}
}

type assertionErr string

func (e assertionErr) Error() string { return string(e) }
