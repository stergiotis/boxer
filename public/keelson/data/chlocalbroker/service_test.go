package chlocalbroker

import (
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

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

func requireBinary(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(chlocalpool.DefaultBinaryPath); err != nil {
		t.Skipf("clickhouse-local not installed at %s: %v", chlocalpool.DefaultBinaryPath, err)
	}
}

func newTestBroker(t *testing.T) (svc *Service, callerBus *inprocbus.Client) {
	t.Helper()
	requireBinary(t)
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(15 * time.Second)

	poolCfg := chlocalpool.Config{
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
	s, err := NewService(bus, poolCfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	})

	caller := bus.NewClient("test.caller", []app.SubjectFilter{
		{Pattern: SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})

	svc = s
	callerBus = caller
	return
}

func waitForPool(t *testing.T, svc *Service, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		stats := svc.Stats()
		if s, ok := stats.PerPool[name]; ok && s.Idle >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waitForPool: pool %q never reached Idle>=1 within %s", name, timeout)
}

func TestNewService_RejectsNilBus(t *testing.T) {
	_, err := NewService(nil, chlocalpool.Config{}, zerolog.Nop())
	require.Error(t, err)
}

func TestExecOnPool_RoundTrip(t *testing.T) {
	svc, caller := newTestBroker(t)

	rep, err := ExecOnPool(context.Background(), caller,"scratchpad", ExecRequest{
		SQL:    "SELECT 1",
		Format: "TabSeparated",
	})
	require.NoError(t, err)
	require.NotNil(t, rep)
	require.NoError(t, rep.Err())

	body, err := io.ReadAll(rep)
	require.NoError(t, err)
	assert.Equal(t, "1\n", string(body))
	assert.Equal(t, "text/tab-separated-values", rep.ContentType)
	assert.Greater(t, rep.Elapsed, time.Duration(0))
	require.NoError(t, rep.Close())

	// The pool for "scratchpad" should now be tracked in the service.
	waitForPool(t, svc, "scratchpad", 3*time.Second)
}

func TestExecOnPool_FormatVariations(t *testing.T) {
	_, caller := newTestBroker(t)

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
			rep, err := ExecOnPool(context.Background(), caller,"scratchpad", ExecRequest{
				SQL:    "SELECT 1",
				Format: tc.format,
			})
			require.NoError(t, err)
			require.NoError(t, rep.Err())
			body, err := io.ReadAll(rep)
			require.NoError(t, err)
			assert.Contains(t, string(body), tc.want)
			require.NoError(t, rep.Close())
		})
	}
}

func TestExecOnPool_BadSQLReturnsErrorWithStderr(t *testing.T) {
	_, caller := newTestBroker(t)

	rep, err := ExecOnPool(context.Background(), caller,"scratchpad", ExecRequest{
		SQL:    "SELECT * FROM nonexistent_table_z_z_z",
		Format: "TabSeparated",
	})
	require.NoError(t, err, "bus request should succeed; worker error surfaces via rep.Err()")
	require.NotNil(t, rep)
	require.Error(t, rep.Err())
	assert.Contains(t, rep.Err().Error(), "stderr")
}

func TestExecOnPool_StreamingRejectedInM2(t *testing.T) {
	_, caller := newTestBroker(t)

	rep, err := ExecOnPool(context.Background(), caller,"scratchpad", ExecRequest{
		SQL:       "SELECT 1",
		Format:    "TabSeparated",
		Streaming: true,
	})
	require.NoError(t, err)
	require.Error(t, rep.Err())
	assert.Contains(t, rep.Err().Error(), "streaming")
}

func TestExecOnPool_NoCapDenied(t *testing.T) {
	requireBinary(t)
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(5 * time.Second)

	poolCfg := chlocalpool.Config{BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 2, SpawnConcurrency: 1}
	svc, err := NewService(bus, poolCfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})

	// Caller without the cap.
	caller := bus.NewClient("test.caller.no.cap", nil)

	_, err = ExecOnPool(context.Background(), caller,"scratchpad", ExecRequest{
		SQL:    "SELECT 1",
		Format: "TabSeparated",
	})
	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "permission") ||
			strings.Contains(err.Error(), "denied"),
		"expected permission/denied in error, got: %s", err.Error())
}

func TestExecOnPool_ConcurrentRequests(t *testing.T) {
	_, caller := newTestBroker(t)

	const N = 5
	var wg sync.WaitGroup
	errCh := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rep, e := ExecOnPool(context.Background(), caller,"scratchpad", ExecRequest{
				SQL:    "SELECT 1",
				Format: "TabSeparated",
			})
			if e != nil {
				errCh <- e
				return
			}
			defer rep.Close()
			if repErr := rep.Err(); repErr != nil {
				errCh <- repErr
				return
			}
			body, readErr := io.ReadAll(rep)
			if readErr != nil {
				errCh <- readErr
				return
			}
			if !strings.Contains(string(body), "1") {
				errCh <- assertionErr("unexpected stdout")
				return
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Errorf("concurrent exec failed: %v", e)
	}
}

func TestService_SeparatePoolsAreIsolated(t *testing.T) {
	svc, caller := newTestBroker(t)

	rep1, err := ExecOnPool(context.Background(), caller,"pool_a", ExecRequest{SQL: "SELECT 1", Format: "TabSeparated"})
	require.NoError(t, err)
	require.NoError(t, rep1.Err())
	_, _ = io.ReadAll(rep1)
	_ = rep1.Close()

	rep2, err := ExecOnPool(context.Background(), caller,"pool_b", ExecRequest{SQL: "SELECT 2", Format: "TabSeparated"})
	require.NoError(t, err)
	require.NoError(t, rep2.Err())
	_, _ = io.ReadAll(rep2)
	_ = rep2.Close()

	stats := svc.Stats()
	assert.Contains(t, stats.PerPool, "pool_a")
	assert.Contains(t, stats.PerPool, "pool_b")
	assert.GreaterOrEqual(t, stats.Pools, 2)
}

type assertionErr string

func (e assertionErr) Error() string { return string(e) }
