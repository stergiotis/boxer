//go:build integration

package chlocalpool

// What one request costs the others sharing a listening `clickhouse local` —
// the isolation a one-shot worker gets by construction and a long-lived one
// has to buy. The process-wide defaults in `clickhouse local` 26.9 bound
// nothing: max_concurrent_queries = 0, concurrent_threads_soft_limit = 0, and
// max_server_memory_usage is 90% of RAM.

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	neighbourCap = "max_server_memory_usage=2000000000"
	// About 1 GiB peak resident, including the array's own freed growth
	// buffers that jemalloc's decay has not returned yet: well under
	// neighbourCap alone, well over it four at a time.
	capShareQuery = "SELECT groupArray(number) FROM numbers(30000000) SETTINGS max_threads = 1 FORMAT Null"
	// Leaves well over 1 GiB resident after it finishes.
	residentQuery = "SELECT groupArray(number) FROM numbers(45000000) SETTINGS max_threads = 1 FORMAT Null"
	sleeperQuery  = "SELECT sleepEachRow(1) FROM numbers(3) SETTINGS max_block_size = 1 FORMAT Null"
)

func TestListen_HungQueriesDoNotBlockOthers(t *testing.T) {
	l := startListener(t)
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() { _, _, _ = l.postCtx(context.Background(), nil, sleeperQuery) })
	}
	defer wg.Wait()
	require.Eventually(t, func() bool {
		_, body := l.post(t, nil, "SELECT count() FROM system.processes")
		n, _ := strconv.Atoi(body)
		return n > 64
	}, 2*time.Second, 20*time.Millisecond, "the sleepers are running")

	t0 := time.Now()
	status, body := l.post(t, nil, "SELECT 1")
	elapsed := time.Since(t0)
	require.Equal(t, http.StatusOK, status, body)
	assert.Less(t, elapsed, 500*time.Millisecond, "with no max_concurrent_queries, a flood of slow queries does not queue the next one")
}

// The process-wide memory cap is one budget for every request in the process:
// requests that each fit alone fail together. Each case gets a fresh process
// so one request's resident leftovers do not decide the next.
func TestListen_ProcessMemoryCapIsShared(t *testing.T) {
	alone := startListener(t, listenerOpts{ServerSettings: []string{neighbourCap}})
	status, body := alone.post(t, nil, capShareQuery)
	require.Equal(t, http.StatusOK, status, "alone it fits: %s", body)

	l := startListener(t, listenerOpts{ServerSettings: []string{neighbourCap}})
	var wg sync.WaitGroup
	bodies := make([]string, 4)
	for i := range bodies {
		wg.Go(func() { _, bodies[i], _ = l.postCtx(context.Background(), nil, capShareQuery) })
	}
	wg.Wait()
	failed := 0
	for _, b := range bodies {
		if strings.Contains(b, "(total) memory limit exceeded") {
			failed++
		}
	}
	assert.Positive(t, failed, "concurrent requests under one process cap")
}

// Freed memory stays resident until jemalloc's dirty-page decay returns it,
// and max_server_memory_usage is checked against resident memory, so a large
// request leaves less room for the ones after it for a while. Both worker
// shapes keep the allocator's defaults — the delay is accepted, not tuned
// away — so this pins that it is bounded: the memory goes back within
// seconds, not at process exit. Read from /proc, so Linux only.
func TestListen_FreedMemoryReturnsAfterDecay(t *testing.T) {
	l := startListener(t)
	pid := l.cmd.Process.Pid
	idle := residentMiB(t, pid)
	status, body := l.post(t, nil, residentQuery)
	require.Equal(t, http.StatusOK, status, body)
	after := residentMiB(t, pid)
	assert.Greater(t, after-idle, 300, "the request's freed memory is still resident")

	t0 := time.Now()
	require.Eventually(t, func() bool { return residentMiB(t, pid)-idle < 150 }, 20*time.Second, 100*time.Millisecond,
		"freed memory not returned within 20s")
	t.Logf("resident %d MiB idle, %d MiB after the request, back near idle after %s", idle, after, time.Since(t0).Round(100*time.Millisecond))
}

func residentMiB(t *testing.T, pid int) (mib int) {
	t.Helper()
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		t.Skipf("no /proc: %v", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "VmRSS:"); ok {
			kb, convErr := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(v), " kB"))
			require.NoError(t, convErr)
			mib = kb / 1024
			return
		}
	}
	t.Fatal("no VmRSS line")
	return
}

// A process that dies takes its in-flight requests with it; they fail at
// once rather than hang.
func TestListen_KilledProcessFailsInFlightRequests(t *testing.T) {
	l := startListener(t)
	errs := make(chan error, 8)
	for range cap(errs) {
		go func() {
			_, _, err := l.postCtx(context.Background(), nil, sleeperQuery)
			errs <- err
		}()
	}
	require.Eventually(t, func() bool {
		_, body := l.post(t, nil, "SELECT count() FROM system.processes")
		n, _ := strconv.Atoi(body)
		return n > cap(errs)
	}, 2*time.Second, 20*time.Millisecond)
	require.NoError(t, l.cmd.Process.Signal(syscall.SIGKILL))
	deadline := time.After(time.Second)
	for range cap(errs) {
		select {
		case err := <-errs:
			assert.Error(t, err)
		case <-deadline:
			t.Fatal("an in-flight request outlived its process by a second")
		}
	}
}

// The recovery cost of a crash: spawning a listener until it answers /ping.
// A pipe worker's equivalent is BenchmarkM0_ColdStdin.
func BenchmarkListen_Restart(b *testing.B) {
	for i := 0; i < b.N; i++ {
		l := startListener(b)
		b.StopTimer()
		l.close()
		<-l.exited
		b.StartTimer()
	}
}

// Victim latency on a listener while a neighbour runs an all-cores query in
// the same process, against the same neighbour in a separate process. The
// difference between the two is what sharing the process adds to plain CPU
// contention.
func BenchmarkListen_Neighbour(b *testing.B) {
	const heavy = "SELECT sum(cityHash64(number)) FROM numbers(100000000000) SETTINGS max_execution_time = 600 FORMAT Null"
	victims := []m0Query{m0Queries[0], m0Queries[1]}
	cfg := m0Config(b)

	inProcess := func(b *testing.B, l *listener) (stop func()) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _, _ = l.postCtx(ctx, url.Values{"query_id": {"neighbour"}, "readonly": {"2"}, "cancel_http_readonly_queries_on_client_close": {"1"}}, heavy)
		}()
		require.Eventually(b, func() bool {
			_, body := l.post(b, nil, "SELECT count() FROM system.processes WHERE query_id = 'neighbour'")
			return body == "1"
		}, 2*time.Second, 20*time.Millisecond, "the neighbour is running")
		return func() { cancel(); <-done }
	}
	outOfProcess := func(b *testing.B, _ *listener) (stop func()) {
		cmd, err := extbin.ClickHouseLocal.Command(context.Background(), extbin.Opts{Path: cfg.BinaryPath},
			"--path", b.TempDir(), "--logger.console", "0", "--query", heavy)
		require.NoError(b, err)
		require.NoError(b, cmd.Start())
		time.Sleep(300 * time.Millisecond)
		require.NoError(b, cmd.Process.Signal(syscall.Signal(0)), "the neighbour is running")
		return func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
	}
	arms := []struct {
		name  string
		start func(*testing.B, *listener) func()
	}{
		{name: "idle"},
		{name: "in-process", start: inProcess},
		{name: "out-of-process", start: outOfProcess},
	}
	for _, arm := range arms {
		for _, q := range victims {
			b.Run(arm.name+"/"+q.name, func(b *testing.B) {
				l := startListener(b)
				if arm.start != nil {
					stop := arm.start(b, l)
					defer stop()
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					status, body, err := l.postCtx(context.Background(), url.Values{"default_format": {q.format}}, q.sql)
					require.NoError(b, err)
					require.Equal(b, http.StatusOK, status, body)
				}
			})
		}
	}
}
