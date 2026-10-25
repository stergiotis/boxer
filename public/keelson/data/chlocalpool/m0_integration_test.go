//go:build integration

package chlocalpool

// ADR-0028's M0 spike as benchmarks: the arms the warm-pool premise rests on,
// re-runnable after a ClickHouse version bump. Arm D is the shape §Alternatives
// rejected as O2 — one long-lived `clickhouse local` serving HTTP after
// `SYSTEM START LISTEN` (ClickHouse 26.6) — measured beside the others so the
// pair can be compared on one host and one binary.
//
// The warm arm sleeps a dwell per worker with the timer stopped, so its wall
// time is far above its reported ns/op; bound it with a count, e.g.
//
//	go test -tags "$(cat ./tags),integration" -run '^$' -bench M0 -benchtime 30x ./public/keelson/data/chlocalpool/

import (
	"context"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stretchr/testify/require"
)

// m0Dwell is how long a warm worker sits blocked on stdin before it is timed.
// The M0 spike used 1 s; 3 s measured the same on 26.9.
const m0Dwell = time.Second

// m0Batch workers are spawned and dwell together, which divides the
// benchmark's wall time without changing what an op measures: idle workers
// are blocked on stdin, as MinIdle spares are in production.
const m0Batch = 4

type m0Query struct {
	name   string
	sql    string
	format string
}

var m0Queries = []m0Query{
	{name: "select1", sql: "SELECT 1", format: "TabSeparated"},
	{name: "arrow100k", sql: "SELECT number, toString(number) AS s FROM numbers(100000)", format: "ArrowStream"},
	// Arrow output is lz4_frame-compressed by default; over a local pipe the
	// compression is CPU on both ends with no bandwidth to save.
	{name: "arrow100k-uncompressed", sql: "SELECT number, toString(number) AS s FROM numbers(100000) SETTINGS output_format_arrow_compression_method = 'none'", format: "ArrowStream"},
}

func requireBinaryTB(tb testing.TB) (path string) {
	tb.Helper()
	path, err := LookupBinary()
	if err != nil {
		tb.Skipf("clickhouse not available: %v", err)
	}
	return
}

func m0Config(tb testing.TB) (cfg Config) {
	tb.Helper()
	cfg = Config{BinaryPath: requireBinaryTB(tb), BaseTmpDir: tb.TempDir()}.withDefaults()
	return
}

// submitTimed runs one statement on w and returns the time from the write to
// the first stdout byte and to process exit. The pool's callers wait for the
// exit — it carries the error surface — so total is what a request costs.
func submitTimed(w *Worker, q m0Query) (firstByte time.Duration, total time.Duration, err error) {
	t0 := time.Now()
	if err = w.WriteSQL(q.sql, q.format); err != nil {
		return
	}
	var one [1]byte
	_, readErr := io.ReadFull(w.Stdout(), one[:])
	firstByte = time.Since(t0)
	if readErr != nil && readErr != io.EOF {
		err = readErr
		return
	}
	if _, err = io.Copy(io.Discard, w.Stdout()); err != nil {
		return
	}
	err = w.Wait()
	total = time.Since(t0)
	return
}

// Arm A: a cold process per statement through --query.
func BenchmarkM0_ColdQuery(b *testing.B) {
	cfg := m0Config(b)
	for _, q := range m0Queries {
		b.Run(q.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				dir, err := os.MkdirTemp(cfg.BaseTmpDir, "cold-*")
				require.NoError(b, err)
				cmd, err := extbin.ClickHouseLocal.Command(context.Background(), extbin.Opts{Path: cfg.BinaryPath},
					"--path", dir, "--logger.console", "0", "--query", q.sql+" FORMAT "+q.format)
				require.NoError(b, err)
				cmd.Stdout = io.Discard
				b.StartTimer()
				err = cmd.Run()
				b.StopTimer()
				require.NoError(b, err)
				_ = os.RemoveAll(dir)
				b.StartTimer()
			}
		})
	}
}

// Arm B: a pre-spawned worker that has dwelled on stdin — the pool's hit path.
// first-byte-ns/op separates result latency from the process teardown that
// follows it.
func BenchmarkM0_WarmWorker(b *testing.B) {
	cfg := m0Config(b)
	for _, q := range m0Queries {
		b.Run(q.name, func(b *testing.B) {
			var firstByteSum time.Duration
			for done := 0; done < b.N; {
				b.StopTimer()
				n := min(m0Batch, b.N-done)
				workers := make([]*Worker, 0, n)
				for range n {
					w, err := newWorker(context.Background(), cfg, nil)
					require.NoError(b, err)
					workers = append(workers, w)
				}
				time.Sleep(m0Dwell)
				for _, w := range workers {
					b.StartTimer()
					firstByte, _, err := submitTimed(w, q)
					b.StopTimer()
					require.NoError(b, err)
					firstByteSum += firstByte
					_ = w.Close()
				}
				done += n
			}
			b.ReportMetric(float64(firstByteSum.Nanoseconds())/float64(b.N), "first-byte-ns/op")
		})
	}
}

// Arm B again, for ArrowStream results: eos-ns/op is when the stream's
// end-of-stream marker has been read, the earliest a broker could reply
// without waiting for the exit status; ns/op still includes the exit.
func BenchmarkM0_WarmWorkerArrowEOS(b *testing.B) {
	cfg := m0Config(b)
	for _, q := range m0Queries {
		if q.format != "ArrowStream" {
			continue
		}
		b.Run(q.name, func(b *testing.B) {
			var eosSum time.Duration
			for done := 0; done < b.N; {
				b.StopTimer()
				n := min(m0Batch, b.N-done)
				workers := make([]*Worker, 0, n)
				for range n {
					w, err := newWorker(context.Background(), cfg, nil)
					require.NoError(b, err)
					workers = append(workers, w)
				}
				time.Sleep(m0Dwell)
				for _, w := range workers {
					b.StartTimer()
					t0 := time.Now()
					require.NoError(b, w.WriteSQL(q.sql, q.format))
					r, err := ipc.NewReader(w.Stdout(), ipc.WithAllocator(memory.DefaultAllocator))
					require.NoError(b, err)
					for r.Next() {
					}
					require.NoError(b, r.Err())
					eosSum += time.Since(t0)
					r.Release()
					_, err = io.Copy(io.Discard, w.Stdout())
					require.NoError(b, err)
					require.NoError(b, w.Wait())
					b.StopTimer()
					_ = w.Close()
				}
				done += n
			}
			b.ReportMetric(float64(eosSum.Nanoseconds())/float64(b.N), "eos-ns/op")
		})
	}
}

// Arm C: spawn and submit at once — a pool miss.
func BenchmarkM0_ColdStdin(b *testing.B) {
	cfg := m0Config(b)
	for _, q := range m0Queries {
		b.Run(q.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				w, err := newWorker(context.Background(), cfg, nil)
				require.NoError(b, err)
				_, _, err = submitTimed(w, q)
				b.StopTimer()
				require.NoError(b, err)
				_ = w.Close()
				b.StartTimer()
			}
		})
	}
}

// Arm D: one listening `clickhouse local`, a keep-alive HTTP connection,
// one POST per op.
func BenchmarkM0_Listening(b *testing.B) {
	l := startListener(b)
	client := l.client
	for _, q := range m0Queries {
		b.Run(q.name, func(b *testing.B) {
			var firstByteSum time.Duration
			for i := 0; i < b.N; i++ {
				var firstByte time.Duration
				t0 := time.Now()
				trace := &httptrace.ClientTrace{GotFirstResponseByte: func() { firstByte = time.Since(t0) }}
				req, err := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace),
					http.MethodPost, l.base+"/?default_format="+q.format, strings.NewReader(q.sql))
				require.NoError(b, err)
				resp, err := client.Do(req)
				require.NoError(b, err)
				_, err = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				require.NoError(b, err)
				require.Equal(b, http.StatusOK, resp.StatusCode)
				firstByteSum += firstByte
			}
			b.ReportMetric(float64(firstByteSum.Nanoseconds())/float64(b.N), "first-byte-ns/op")
		})
	}
}

// The M0 decision gate as a test: a worker that dwelled on stdin must answer
// well before a cold process does, or the executor no longer preloads at
// spawn and the warm pool buys nothing (ADR-0028 §SD3 "Spawn readiness").
// The bound is loose — half the cold median — so load on the host does not
// flip it; on 26.9 the measured ratio is near 8x.
func TestM0_WarmWorkerPreloads(t *testing.T) {
	cfg := m0Config(t)
	q := m0Queries[0]
	const samples = 7
	cold := make([]time.Duration, 0, samples)
	warm := make([]time.Duration, 0, samples)
	for range samples {
		w, err := newWorker(context.Background(), cfg, nil)
		require.NoError(t, err)
		_, total, err := submitTimed(w, q)
		require.NoError(t, err)
		_ = w.Close()
		cold = append(cold, total)
	}
	workers := make([]*Worker, 0, samples)
	for range samples {
		w, err := newWorker(context.Background(), cfg, nil)
		require.NoError(t, err)
		workers = append(workers, w)
	}
	time.Sleep(m0Dwell)
	for _, w := range workers {
		firstByte, _, err := submitTimed(w, q)
		require.NoError(t, err)
		_ = w.Close()
		warm = append(warm, firstByte)
	}
	coldMedian, warmMedian := median(cold), median(warm)
	t.Logf("cold stdin total p50 %s, warm first byte p50 %s", coldMedian, warmMedian)
	require.Less(t, warmMedian, coldMedian/2, "warm worker is not markedly faster than a cold spawn: lazy executor init suspected")
}

func median(ds []time.Duration) (m time.Duration) {
	s := slices.Clone(ds)
	slices.Sort(s)
	m = s[len(s)/2]
	return
}
