package play

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stretchr/testify/require"
)

// progressTestServer accepts one connection, reads the request, writes
// script(conn) and closes. It returns the base URL.
func progressTestServer(t *testing.T, script func(conn net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, aErr := ln.Accept()
		if aErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Drain the request head (and its small body) before scripting the
		// response.
		br := bufio.NewReader(conn)
		for {
			line, rErr := br.ReadString('\n')
			if rErr != nil {
				return
			}
			if line == "\r\n" || line == "\n" {
				break
			}
		}
		script(conn)
	}()
	return "http://" + ln.Addr().String() + "/"
}

// fakeProgressExec implements both executor interfaces: each
// executeWithProgress call parks until release closes, exposing its sink
// so the test can fire ticks at controlled moments.
type fakeProgressExec struct {
	mu      sync.Mutex
	sinks   []func(p runstream.Progress)
	release chan struct{}
}

func (inst *fakeProgressExec) execute(ctx context.Context, c compiledNode, alloc memory.Allocator) (arrow.RecordBatch, *arrow.Schema, Summary, error) {
	return inst.executeWithProgress(ctx, c, alloc, nil)
}

func (inst *fakeProgressExec) executeWithProgress(ctx context.Context, c compiledNode, alloc memory.Allocator, onProgress func(p runstream.Progress)) (arrow.RecordBatch, *arrow.Schema, Summary, error) {
	inst.mu.Lock()
	inst.sinks = append(inst.sinks, onProgress)
	inst.mu.Unlock()
	select {
	case <-inst.release:
		return nil, nil, Summary{}, nil
	case <-ctx.Done():
		return nil, nil, Summary{}, ctx.Err()
	}
}

func (inst *fakeProgressExec) sink(t *testing.T, i int) func(p runstream.Progress) {
	t.Helper()
	require.Eventually(t, func() bool {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		return len(inst.sinks) > i
	}, 2*time.Second, time.Millisecond)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.sinks[i]
}

func TestLaneProgressTickAndGate(t *testing.T) {
	exec := &fakeProgressExec{release: make(chan struct{})}
	lane := newNodeLane(exec, nil, 0)
	defer lane.close()

	v := lane.demand(compiledNode{SQL: "SELECT A"})
	require.True(t, v.loading)
	require.False(t, v.progressFresh, "no tick yet")

	exec.sink(t, 0)(runstream.Progress{ReadRows: 10, TotalRowsToRead: 100})
	p, fresh := lane.progressView()
	require.True(t, fresh)
	require.EqualValues(t, 10, p.ReadRows)
	v = lane.demand(compiledNode{SQL: "SELECT A"})
	require.True(t, v.progressFresh)
	require.EqualValues(t, 10, v.progress.ReadRows)

	close(exec.release)
	require.Eventually(t, func() bool {
		view := lane.demand(compiledNode{SQL: "SELECT A"})
		defer func() {
			if view.rec != nil {
				view.rec.Release()
			}
		}()
		return !view.loading
	}, 2*time.Second, time.Millisecond)
	_, fresh = lane.progressView()
	require.False(t, fresh, "a landed run shows no stale ticks")
}

func TestLaneProgressSupersededTickDiscarded(t *testing.T) {
	exec := &fakeProgressExec{release: make(chan struct{})}
	defer close(exec.release)
	lane := newNodeLane(exec, nil, 0)
	defer lane.close()

	lane.demand(compiledNode{SQL: "SELECT A"})
	oldSink := exec.sink(t, 0)
	lane.demand(compiledNode{SQL: "SELECT B"}) // supersedes A
	newSink := exec.sink(t, 1)

	oldSink(runstream.Progress{ReadRows: 999}) // late tick from the superseded run
	_, fresh := lane.progressView()
	require.False(t, fresh, "a superseded run's tick must not paint the new run's badge")

	newSink(runstream.Progress{ReadRows: 5})
	p, fresh := lane.progressView()
	require.True(t, fresh)
	require.EqualValues(t, 5, p.ReadRows)
}

// TestQueryStoreProgressEndToEnd runs the real ExecuteArrowStream against
// the dribble server: the store's Progress() must go fresh while the
// header block is still open, then gate off once the run lands (the body
// is not a valid ArrowStream — the run finishes with an error, which is
// irrelevant to progress gating).
func TestQueryStoreProgressEndToEnd(t *testing.T) {
	proceed := make(chan struct{})
	baseURL := progressTestServer(t, func(conn net.Conn) {
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\n")
		_, _ = io.WriteString(conn, "X-ClickHouse-Progress: {\"read_rows\":\"42\",\"total_rows_to_read\":\"84\"}\r\n")
		<-proceed
		_, _ = io.WriteString(conn, "Content-Length: 0\r\n\r\n")
	})
	store := NewQueryStore(NewClient(ClientConfig{URL: baseURL}, nil), nil, 10, "progress-test")
	defer store.Close()
	store.Execute("SELECT 1", nil, "")

	require.Eventually(t, func() bool {
		_, fresh := store.Progress()
		return fresh
	}, 2*time.Second, time.Millisecond, "the tick must surface while the run is in flight")
	p, _ := store.Progress()
	require.EqualValues(t, 42, p.ReadRows)

	close(proceed)
	require.Eventually(t, func() bool { return !store.IsLoading() }, 2*time.Second, time.Millisecond)
	_, fresh := store.Progress()
	require.False(t, fresh)
}

// Every decimal tier gets its suffix — a trillion-row scan reads "1.5T",
// not "1500.0B" (the review's last piece of trivia).
func TestHumanCountTiers(t *testing.T) {
	cases := map[uint64]string{
		999:               "999",
		1_500:             "1.5K",
		2_500_000:         "2.5M",
		3_500_000_000:     "3.5B",
		1_500_000_000_000: "1.5T",
	}
	for n, want := range cases {
		require.Equal(t, want, humanCount(n), "n=%d", n)
	}
}

func TestFormatProgressLine(t *testing.T) {
	v := progressView{
		fresh: true,
		p: runstream.Progress{ReadRows: 1_946_964_294, ReadBytes: 15_575_714_352,
			TotalRowsToRead: 2_500_000_000, ElapsedNs: 300_006_531, MemoryUsage: 1_145_567},
		knownTotal: true, percent: 77, fraction: 0.7788,
		rate: 1_200_000, eta: 80 * time.Second, etaValid: true,
	}
	s := formatProgressLine(v)
	require.Contains(t, s, "1.9B / 2.5B rows (77%)")
	// Binary units, spelled as such: the divisor was always 1024 and the label
	// used to say KB/GB (humanBytes, retired 2026-08-05 for humanize.IBytes).
	require.Contains(t, s, "14 GiB read")
	require.Contains(t, s, "1.2M rows/s")
	require.Contains(t, s, "ETA 1m20s")
	require.Contains(t, s, "mem 1.1 MiB")
	require.Contains(t, s, "300ms")

	// No total, no warm estimator: rows and bytes are all there is to say.
	require.Equal(t, "12 rows · 0 B read",
		formatProgressLine(progressView{fresh: true, p: runstream.Progress{ReadRows: 12}}))

	// The top bar's short form carries the percentage the bar cannot show
	// legibly, then prefers the ETA, falling back to the rate and to the bare
	// row count while both warm up.
	require.Equal(t, "77% · ETA 1m20s", formatProgressBrief(v))
	require.Equal(t, "1% · 1.2M rows/s",
		formatProgressBrief(progressView{fresh: true, knownTotal: true, percent: 1, rate: 1_200_000}))
	require.Equal(t, "1%", formatProgressBrief(progressView{fresh: true, knownTotal: true, percent: 1}))
	require.Equal(t, "1.2M rows/s", formatProgressBrief(progressView{fresh: true, rate: 1_200_000}))
	require.Equal(t, "12 rows",
		formatProgressBrief(progressView{fresh: true, p: runstream.Progress{ReadRows: 12}}))

	// The pane strip drops the memory/elapsed tail the status bar carries.
	strip := formatProgressStrip(v)
	require.Equal(t, "1.9B / 2.5B rows · 1.2M rows/s · ETA 1m20s", strip)
}

// The landed-run readout that takes the progress row's slot between fetches:
// exact counts, an abbreviated rate, and the lane's wall clock.
func TestFormatLaneStats(t *testing.T) {
	require.Equal(t,
		"5,000,000,000 rows read · 314,368 returned · 200 M rows/s · 25s",
		formatLaneStats(laneStats{valid: true, readRows: 5_000_000_000, returned: 314_368,
			elapsed: 25 * time.Second}))

	// The clock is rounded to the millisecond, not printed raw.
	require.Contains(t,
		formatLaneStats(laneStats{valid: true, readRows: 12, returned: 4,
			elapsed: 1500600 * time.Microsecond}),
		"· 1.501s")

	// Under an SI step the magnitude is spelled out, and the padding
	// SIWithDigits leaves where its unit would go must not double the space.
	require.Equal(t, "500 rows read · 500 returned · 500 rows/s · 1s",
		formatLaneStats(laneStats{valid: true, readRows: 500, returned: 500, elapsed: time.Second}))

	// A run with no clock (or one too fast to have a meaningful rate) still
	// reports what it moved; an unmeasurable throughput is left unsaid rather
	// than divided by zero.
	require.Equal(t, "12 rows read · 4 returned",
		formatLaneStats(laneStats{valid: true, readRows: 12, returned: 4}))
}

// The stats mirror the SERVED result every demand, so a failed or absent one
// clears them rather than leaving the previous run's numbers under a new error.
func TestStatsFromLaneMirrorsServedResult(t *testing.T) {
	rec := int64Rec("n", 1, 2, 3)
	defer rec.Release()

	s := statsFromLane(laneView{key: "k", rec: rec, elapsed: 2 * time.Second,
		summary: Summary{ReadRows: 100}})
	require.True(t, s.valid)
	require.Equal(t, uint64(100), s.readRows)
	require.Equal(t, int64(3), s.returned)
	require.Equal(t, 2*time.Second, s.elapsed)

	require.False(t, statsFromLane(laneView{}).valid, "nothing served yet")
	require.False(t, statsFromLane(laneView{key: "k", err: errors.New("boom")}).valid,
		"a failed run has no accounting worth showing")
}

// progressTicks feeds the tracker a run at a constant rate: one tick per
// `spacing`, `perTick` rows each, with the server-reported elapsed clock the
// real transport supplies. Returns the last view.
func progressTicks(tr *progressTracker, lane string, n int, perTick uint64, total uint64, spacing time.Duration) (v progressView) {
	base := time.Unix(1700000000, 0)
	for i := 1; i <= n; i++ {
		elapsed := time.Duration(i) * spacing
		v = tr.observe(base.Add(elapsed), lane, runstream.Progress{
			ReadRows:        perTick * uint64(i),
			TotalRowsToRead: total,
			ElapsedNs:       uint64(elapsed.Nanoseconds()),
		}, true)
	}
	return
}

func TestProgressTrackerEstimates(t *testing.T) {
	tr := &progressTracker{}
	// One tick in: the fraction is exact, but an ETA needs a rate, and a
	// rate needs two spacings to measure.
	v := progressTicks(tr, "main", 1, 250_000, 10_000_000, 250*time.Millisecond)
	require.True(t, v.fresh)
	require.True(t, v.knownTotal)
	require.EqualValues(t, 2, v.percent)
	require.InDelta(t, 0.025, v.fraction, 0.001)
	require.False(t, v.etaValid, "no ETA off a single tick")
	require.Zero(t, v.rate)

	// Three ticks of 250k rows every 250 ms is 1M rows/s; 10M total leaves
	// 9.25M to read (the estimator's ETA has second resolution).
	v = progressTicks(tr, "main", 3, 250_000, 10_000_000, 250*time.Millisecond)
	require.True(t, v.etaValid)
	require.InDelta(t, 1_000_000, v.rate, 50_000)
	require.InDelta(t, 9.25, v.eta.Seconds(), 1)

	// A row count that cannot be reached still yields a fraction of 1 and a
	// zero ETA rather than an overrun.
	v = tr.observe(time.Unix(1700000001, 0), "main", runstream.Progress{
		ReadRows: 12_000_000, TotalRowsToRead: 10_000_000, ElapsedNs: uint64(time.Second)}, true)
	require.EqualValues(t, 100, v.percent)
	require.EqualValues(t, 1, v.fraction)
	require.True(t, v.etaValid)
	require.Zero(t, v.eta)
}

// TestProgressTrackerRepeatedTick is the property the render loop depends on:
// observe runs every frame (~16 ms) while ticks land every ~250 ms, so the
// same tick is seen a dozen times. Folding those re-reads in as samples would
// drag the smoothed rate toward zero and inflate the ETA.
func TestProgressTrackerRepeatedTick(t *testing.T) {
	tr := &progressTracker{}
	v := progressTicks(tr, "main", 4, 250_000, 10_000_000, 250*time.Millisecond)
	rate, eta := v.rate, v.eta
	require.NotZero(t, rate)

	last := runstream.Progress{ReadRows: 1_000_000, TotalRowsToRead: 10_000_000,
		ElapsedNs: uint64(time.Second)}
	now := time.Unix(1700000001, 0)
	for i := 0; i < 60; i++ {
		now = now.Add(16 * time.Millisecond)
		v = tr.observe(now, "main", last, true)
	}
	require.Equal(t, rate, v.rate, "re-reading one tick must not move the rate")
	require.Equal(t, eta, v.eta)
}

// TestProgressTrackerReAnchors covers the three ways the run underneath the
// tracker changes: the lane gate closing, the observed lane switching, and a
// row count that restarts. Each must drop the previous run's level, or the
// new run's first ETA is the old run's.
func TestProgressTrackerReAnchors(t *testing.T) {
	base := time.Unix(1700000000, 0)

	t.Run("gate off", func(t *testing.T) {
		tr := &progressTracker{}
		progressTicks(tr, "main", 4, 250_000, 10_000_000, 250*time.Millisecond)
		v := tr.observe(base, "main", runstream.Progress{}, false)
		require.Equal(t, progressView{}, v, "a landed run shows nothing")
		require.False(t, tr.tracking)
		// The next run starts cold.
		v = tr.observe(base, "main", runstream.Progress{
			ReadRows: 10, TotalRowsToRead: 1_000_000, ElapsedNs: uint64(250 * time.Millisecond)}, true)
		require.False(t, v.etaValid)
		require.Zero(t, v.rate)
	})

	t.Run("lane switch", func(t *testing.T) {
		tr := &progressTracker{}
		progressTicks(tr, "main", 4, 250_000, 10_000_000, 250*time.Millisecond)
		v := tr.observe(base, "by_kind", runstream.Progress{
			ReadRows: 10, TotalRowsToRead: 1_000_000, ElapsedNs: uint64(250 * time.Millisecond)}, true)
		require.False(t, v.etaValid, "the intermediate lane's run is not the main lane's")
		require.Zero(t, v.rate)
	})

	t.Run("rows restart", func(t *testing.T) {
		tr := &progressTracker{}
		progressTicks(tr, "main", 4, 250_000, 10_000_000, 250*time.Millisecond)
		// Same lane, same fresh gate, but a run that starts over: the
		// supersede path can hand the render thread the new run's first
		// tick without the gate ever closing.
		v := tr.observe(base, "main", runstream.Progress{
			ReadRows: 1_000, TotalRowsToRead: 10_000_000, ElapsedNs: uint64(250 * time.Millisecond)}, true)
		require.False(t, v.etaValid)
		require.Zero(t, v.rate)
	})
}

// TestProgressTrackerNoElapsed covers the endpoints that report no run clock
// (the tick shape is shared with the polled producer): the tracker falls back
// to wall time and still refuses to fold a re-read of the same tick.
func TestProgressTrackerNoElapsed(t *testing.T) {
	tr := &progressTracker{}
	now := time.Unix(1700000000, 0)
	var v progressView
	for i := 1; i <= 4; i++ {
		now = now.Add(250 * time.Millisecond)
		v = tr.observe(now, "main", runstream.Progress{
			ReadRows: 250_000 * uint64(i), TotalRowsToRead: 10_000_000}, true)
	}
	require.False(t, tr.useElapsed)
	require.True(t, v.etaValid)
	require.InDelta(t, 1_000_000, v.rate, 50_000)

	rate := v.rate
	for i := 0; i < 10; i++ {
		now = now.Add(250 * time.Millisecond)
		v = tr.observe(now, "main", runstream.Progress{
			ReadRows: 1_000_000, TotalRowsToRead: 10_000_000}, true)
	}
	require.Equal(t, rate, v.rate)
}
