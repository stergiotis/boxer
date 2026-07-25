// Package queryprogress observes inflight query runs and publishes what it
// sees on the bus — the E7 extension point of
// doc/explanation/query-system-requirements.md.
//
// The requirement is R8, issuer-decoupled observability: a run's progress
// must be visible to parties other than the connection holder. In-band
// progress headers cannot provide that — they arrive only on the socket
// that issued the query, so a second window, or ops tooling, or a dashboard
// watching a run somebody else started, has no way in. Polling
// `system.processes` does, because the server already knows about every run
// regardless of who is holding its connection.
//
// Three guarantees define what a consumer may conclude, and two of them are
// about what this package refuses to do:
//
//   - **Ticks only.** The poller NEVER synthesises a terminal frame. A run
//     vanishing from system.processes is ambiguous — finished, killed, or
//     failed — and guessing would let a wrong outcome into the one place
//     R9 makes authoritative. Terminal truth comes from the result path or
//     from query_log; this plane does not compete with it.
//   - **Absence means nothing.** A run shorter than one tick never appears
//     at all. No progress frame is not evidence of no progress, of a stall,
//     or of anything else.
//   - **Self-excluding.** The poller's own statements are structurally
//     invisible: it reports only ids that were explicitly registered, and
//     it never registers its own.
//
// Staleness is bounded by the tick and by nothing else: a tick reports what
// the server said at that moment, and the next says nothing until it
// arrives.
package queryprogress

import (
	"context"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/runid"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Interval bounds. A tick costs one small query against system.processes,
// so the floor is about not hammering a server rather than about cost here.
const (
	MinInterval     = 100 * time.Millisecond
	MaxInterval     = 60 * time.Second
	DefaultInterval = 500 * time.Millisecond
)

// maxResponseBytes caps a tick's response read. system.processes rows are
// small and the watch set is bounded by the caller, so anything past this is
// a server answering something other than what was asked.
const maxResponseBytes = 4 << 20

// Options configures a Poller. Endpoint and Bus are required.
type Options struct {
	// Endpoint is the ClickHouse HTTP endpoint of the ONE server this
	// poller watches. A poller is bound to a single server because
	// system.processes is per-server: a run is only visible where it runs
	// (R10), and pointing one poller at a cluster address would silently
	// report whichever member answered.
	Endpoint string
	// User and Password authenticate the polling statement.
	User     string
	Password string
	// HTTPClient defaults to a client with Interval-derived timeout.
	HTTPClient *http.Client
	// Bus receives the ticks. Needs PublishFilter.
	Bus app.BusI
	// Interval is the tick period, clamped to [MinInterval, MaxInterval].
	Interval time.Duration
	Log      zerolog.Logger
}

// Poller watches one server and publishes a tick per registered run per
// tick. Safe for concurrent use: Watch and Unwatch may be called from any
// goroutine while the loop runs.
type Poller struct {
	endpoint string
	user     string
	password string
	http     *http.Client
	bus      app.BusI
	interval time.Duration
	log      zerolog.Logger

	mu      sync.Mutex
	watched map[string]uint64 // query id -> next sequence number

	cancel context.CancelFunc
	done   chan struct{}
}

// New builds a poller. It does not start ticking; call Start.
func New(opts Options) (inst *Poller, err error) {
	if opts.Endpoint == "" {
		err = eh.Errorf("queryprogress: Endpoint is required")
		return
	}
	if opts.Bus == nil {
		err = eh.Errorf("queryprogress: Bus is required")
		return
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	interval = min(max(interval, MinInterval), MaxInterval)
	httpClient := opts.HTTPClient
	if httpClient == nil {
		// A tick that outlives its own period is worse than a missed one:
		// it would queue behind the next.
		httpClient = &http.Client{Timeout: interval}
	}
	inst = &Poller{
		endpoint: opts.Endpoint,
		user:     opts.User,
		password: opts.Password,
		http:     httpClient,
		bus:      opts.Bus,
		interval: interval,
		log:      opts.Log,
		watched:  make(map[string]uint64, 8),
	}
	return
}

// Watch registers a run. Ticks are published on [Subject](queryID) until
// Unwatch.
//
// Registration is what makes a run visible here at all, and it is also the
// self-exclusion: the poller's own statements are never registered, so they
// can never be reported.
func (inst *Poller) Watch(queryID string) (err error) {
	if !runid.Valid(queryID) {
		err = eb.Build().Str("queryId", queryID).Errorf("queryprogress: query id is not safe as a subject token or SQL literal")
		return
	}
	inst.mu.Lock()
	if _, ok := inst.watched[queryID]; !ok {
		inst.watched[queryID] = 1
	}
	inst.mu.Unlock()
	return
}

// Unwatch deregisters a run.
//
// The CALLER decides when: deregistration belongs to whoever holds the
// result path, at the moment it delivers the terminal frame. The poller
// will not do it on its own, because the only signal it has — the run
// leaving system.processes — cannot distinguish finished from killed from
// failed, and acting on it would amount to inventing an outcome.
func (inst *Poller) Unwatch(queryID string) {
	inst.mu.Lock()
	delete(inst.watched, queryID)
	inst.mu.Unlock()
}

// Watched returns the registered ids, sorted.
func (inst *Poller) Watched() (ids []string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ids = make([]string, 0, len(inst.watched))
	for id := range inst.watched {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return
}

// Start begins ticking until Close.
func (inst *Poller) Start() {
	if inst.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	inst.cancel = cancel
	inst.done = make(chan struct{})
	go inst.loop(ctx)
}

// Close stops the tick loop and waits for it to finish.
func (inst *Poller) Close() (err error) {
	if inst.cancel != nil {
		inst.cancel()
		inst.cancel = nil
	}
	if inst.done != nil {
		<-inst.done
		inst.done = nil
	}
	return
}

func (inst *Poller) loop(ctx context.Context) {
	defer close(inst.done)
	ticker := time.NewTicker(inst.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			inst.Tick(ctx)
		}
	}
}

// Tick performs one observation: a single query covering every registered
// run, and one published frame per run the server reported as inflight.
// Exported so a caller can drive the cadence itself, and so tests need no
// wall clock.
//
// One query per tick, not one per run: a watch set of thirty runs must not
// become thirty round trips, and batching also makes the ids share a single
// consistent view of the server's process list.
func (inst *Poller) Tick(ctx context.Context) {
	ids := inst.Watched()
	if len(ids) == 0 {
		return
	}
	rows, err := inst.query(ctx, ids)
	if err != nil {
		// A failed tick is a missed observation, not a statement about any
		// run. Nothing is published and nothing is deregistered.
		inst.log.Debug().Err(err).Msg("queryprogress: tick failed")
		return
	}
	for _, row := range rows {
		inst.publish(row)
	}
}

// processRow is one system.processes row for a watched run.
type processRow struct {
	queryID  string
	progress runstream.Progress
}

// pollSQL builds the tick's statement. Ids are interpolated as literals,
// which is safe only because Watch rejected everything outside
// runid.Valid's charset — quotes cannot appear.
func pollSQL(ids []string) (sql string) {
	var b strings.Builder
	b.WriteString("SELECT query_id, read_rows, read_bytes, total_rows_approx, " +
		"toUInt64(elapsed * 1000000000), memory_usage FROM system.processes WHERE query_id IN (")
	for i, id := range ids {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("'")
		b.WriteString(id)
		b.WriteString("'")
	}
	b.WriteString(") FORMAT TabSeparated")
	sql = b.String()
	return
}

func (inst *Poller) query(ctx context.Context, ids []string) (rows []processRow, err error) {
	req, err := http.NewRequestWithContext(ctx, "POST", inst.endpoint, strings.NewReader(pollSQL(ids)))
	if err != nil {
		err = eh.Errorf("queryprogress: build request: %w", err)
		return
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if inst.user != "" {
		req.Header.Set("X-ClickHouse-User", inst.user)
	}
	if inst.password != "" {
		req.Header.Set("X-ClickHouse-Key", inst.password)
	}
	resp, err := inst.http.Do(req)
	if err != nil {
		err = eh.Errorf("queryprogress: request failed: %w", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		err = eb.Build().Int("statusCode", resp.StatusCode).Str("body", strings.TrimSpace(string(raw))).
			Errorf("queryprogress: system.processes http %d", resp.StatusCode)
		return
	}
	raw, rerr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if rerr != nil {
		err = eh.Errorf("queryprogress: read response: %w", rerr)
		return
	}
	rows = parseProcessRows(string(raw))
	return
}

// parseProcessRows reads the TabSeparated tick response. A malformed line is
// skipped rather than failing the tick: a partial observation is still a
// true one, and this plane is advisory throughout.
func parseProcessRows(raw string) (rows []processRow) {
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			continue
		}
		rows = append(rows, processRow{
			queryID: f[0],
			progress: runstream.Progress{
				ReadRows:        parseU64(f[1]),
				ReadBytes:       parseU64(f[2]),
				TotalRowsToRead: parseU64(f[3]),
				ElapsedNs:       parseU64(f[4]),
				MemoryUsage:     parseU64(f[5]),
			},
		})
	}
	return
}

func parseU64(s string) (n uint64) {
	n, _ = strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return
}

// publish emits one tick for a run, if it is still watched. The sequence is
// per-run and advances only when a frame is actually published, so a
// subscriber's collector sees a strictly increasing sequence.
func (inst *Poller) publish(row processRow) {
	inst.mu.Lock()
	seq, watched := inst.watched[row.queryID]
	if watched {
		inst.watched[row.queryID] = seq + 1
	}
	inst.mu.Unlock()
	if !watched {
		// Unwatched between the query and here — the result already landed,
		// so this observation is stale and says nothing new.
		return
	}
	payload, err := EncodeTick(Tick{QueryID: row.queryID, Seq: seq, Progress: row.progress})
	if err != nil {
		inst.log.Warn().Err(err).Str("queryId", row.queryID).Msg("queryprogress: encode failed")
		return
	}
	perr := inst.bus.Publish(Subject(row.queryID), payload)
	if perr != nil {
		inst.log.Warn().Err(perr).Str("queryId", row.queryID).Msg("queryprogress: publish failed")
	}
}
