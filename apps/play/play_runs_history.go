package play

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"

	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// play_runs_history.go is the ADR-0115 S2 glass surface: the History tab's
// "Recorded runs" section, reading captured KindQueryRun facts back from
// runtime.facts through whatever endpoint the user is on. It complements
// the session list above it: that one is this process's editor ring
// (restore an entry), this one is the server's durable record — every
// terminal query, every client, surviving restarts. Selecting a run shows
// its accounting; the drill-downs are handed to the editor as ordinary SQL
// (ReplaceSql) rather than rendered as bespoke panes — the tool observing
// itself with its own instrument.
//
// Fetching is deliberately manual-plus-on-reveal: the capture pipeline
// lands rows on a seconds-order cadence (queryrunsd MV refresh), so an
// auto-poll would mostly re-read an unchanged table. The fetch runs off
// the render thread with the armColumnDiag generation guard; the render
// polls a snapshot.

const (
	// runsHistoryFactsTable is the conventional capture destination
	// (factsschema DatabaseName.TableName). Endpoints without it (chlocal,
	// a foreign server) surface the server error plus a hint instead.
	runsHistoryFactsTable = "runtime.facts"
	// runsHistoryLimit bounds one read; queryrunfacts caps it further.
	runsHistoryLimit = 100
	// runsHistoryTimeout bounds one fetch round-trip (diagProbeTimeout's
	// rationale: generous for remote links, finite for hangs).
	runsHistoryTimeout = 15 * time.Second
	// runsDetailTextRunes caps the detail pane's inline query text; the
	// full text is one "open as query" away.
	runsDetailTextRunes = 2000
)

// runsHistoryDriver owns the async fetch and its render-thread snapshot.
type runsHistoryDriver struct {
	// fetch is the injected data path (nil = no endpoint: the section
	// stays hidden). Tests substitute it.
	fetch func(ctx context.Context) ([]queryrunfacts.HistoryRow, error)

	mu       sync.Mutex
	gen      uint64
	inFlight bool
	fetched  bool
	asOf     time.Time
	rows     []queryrunfacts.HistoryRow
	err      error

	// selected is the chosen run's fact id (0 = none). Render-thread-only.
	selected uint64
}

// newRunsHistoryDriver wires the driver against the live endpoint. A nil
// client (tests, legacy CLI) leaves fetch nil and the section hidden.
func newRunsHistoryDriver(client *Client) (d *runsHistoryDriver) {
	d = &runsHistoryDriver{}
	if client != nil {
		d.fetch = func(ctx context.Context) (rows []queryrunfacts.HistoryRow, err error) {
			sql, err := queryrunfacts.ComposeHistorySql(runsHistoryFactsTable, runsHistoryLimit)
			if err != nil {
				return
			}
			raw, err := client.rawTsvQuery(ctx, sql)
			if err != nil {
				return
			}
			rows, err = queryrunfacts.ParseHistoryRows(raw)
			return
		}
	}
	return
}

// refresh starts one background fetch; a no-op while one is in flight or
// without an endpoint. Completion stores under the generation guard.
func (inst *runsHistoryDriver) refresh() {
	if inst.fetch == nil {
		return
	}
	inst.mu.Lock()
	if inst.inFlight {
		inst.mu.Unlock()
		return
	}
	inst.inFlight = true
	inst.gen++
	gen := inst.gen
	fetch := inst.fetch
	inst.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), runsHistoryTimeout)
		defer cancel()
		rows, err := fetch(ctx)
		inst.mu.Lock()
		if gen == inst.gen {
			inst.rows, inst.err = rows, err
			inst.fetched = true
			inst.asOf = time.Now()
			inst.inFlight = false
		}
		inst.mu.Unlock()
	}()
}

// maybeRefreshOnReveal fires the first fetch when the section first
// renders (the History tab is lazy, so this is tab-reveal time).
func (inst *runsHistoryDriver) maybeRefreshOnReveal() {
	if inst.fetch == nil {
		return
	}
	inst.mu.Lock()
	needed := !inst.fetched && !inst.inFlight
	inst.mu.Unlock()
	if needed {
		inst.refresh()
	}
}

// snapshot returns the render-thread view. The rows slice is replaced,
// never mutated, by refresh — reading it without copying is safe.
func (inst *runsHistoryDriver) snapshot() (rows []queryrunfacts.HistoryRow, err error, inFlight bool, fetched bool, asOf time.Time) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.rows, inst.err, inst.inFlight, inst.fetched, inst.asOf
}

// rawTsvQuery POSTs sql (its own FORMAT clause included) to the live
// endpoint and returns the body — the fetchColumnNames transport without
// the parameter channel, for the readback SELECTs this file issues.
func (inst *Client) rawTsvQuery(ctx context.Context, sql string) (raw []byte, err error) {
	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, "POST", inst.URL(), strings.NewReader(sql))
	if err != nil {
		err = eh.Errorf("unable to build history request: %w", err)
		return
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if inst.cfg.User != "" {
		req.Header.Set("X-ClickHouse-User", inst.cfg.User)
	}
	if inst.cfg.Password != "" {
		req.Header.Set("X-ClickHouse-Key", inst.cfg.Password)
	}
	var resp *http.Response
	resp, err = inst.http.Do(req)
	if err != nil {
		err = eh.Errorf("history request failed: %w", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		err = eb.Build().Int("statusCode", resp.StatusCode).Str("body", strings.TrimSpace(string(body))).
			Errorf("history http %d", resp.StatusCode)
		return
	}
	raw, err = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		err = eh.Errorf("unable to read history response: %w", err)
		return
	}
	return
}

// runRowLabel is one list line: local wall time, duration, kind, and the
// first line of the query text — prefixed with "!" for runs that ended in
// an exception, matching the session list's char budget.
func runRowLabel(row queryrunfacts.HistoryRow) (label string) {
	status := ""
	if row.ExceptionCode != 0 || (row.Event != "" && row.Event != "QueryFinish") {
		status = "! "
	}
	text := row.QueryText
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	kind := row.Kind
	if kind == "" {
		kind = "?"
	}
	label = fmt.Sprintf("%s%s  %d ms  %s  %s",
		status, row.Ts.Local().Format("15:04:05"), row.DurationMs, kind, text)
	label = truncateRunes(label, historyLabelChar)
	return
}

// renderRecordedRuns is the History tab's facts-backed section. The
// caller (renderHistoryTab) already sits in the tab's ScrollArea.
func (inst *PlayApp) renderRecordedRuns() {
	d := inst.runsHist
	if d == nil || d.fetch == nil {
		return
	}
	d.maybeRefreshOnReveal()
	ids := inst.ids
	for range c.IdScope(ids.PrepareStr("recorded-runs")) {
		c.Separator().Send()
		rows, fetchErr, inFlight, fetched, asOf := d.snapshot()
		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel("Recorded runs") {
				rt.Strong()
			}
			if c.Button(ids.PrepareStr("refresh"), c.Atoms().Text("Refresh").Keep()).
				SendResp().HasPrimaryClicked() {
				d.refresh()
			}
		}
		switch {
		case !fetched && inFlight:
			diagWeak("Fetching captured runs…")
			return
		case fetchErr != nil:
			c.Label(firstErrorLine(fetchErr)).Wrap().Selectable(true).Send()
			diagWeak("Captured runs live in " + runsHistoryFactsTable + " — the queryrunsd service (ADR-0115) records them against this server.")
			return
		case len(rows) == 0:
			diagWeak("No captured runs yet — the capture pipeline lands terminal queries within its refresh cadence.")
			return
		}
		diagWeak(fmt.Sprintf("%d runs · fetched %s", len(rows), humanizeAgo(asOf)))
		for i := range rows {
			for range c.IdScope(ids.PrepareSeq(uint64(i))) {
				if c.Button(ids.PrepareStr("run"),
					c.Atoms().Text(runRowLabel(rows[i])).Keep()).
					Frame(false).
					Truncate().
					SendResp().HasPrimaryClicked() {
					if d.selected == rows[i].Id {
						d.selected = 0
					} else {
						d.selected = rows[i].Id
					}
				}
			}
		}
		if d.selected == 0 {
			return
		}
		for i := range rows {
			if rows[i].Id == d.selected {
				inst.renderRunDetail(rows[i])
				return
			}
		}
		// The selection aged out of the fetched window; drop it silently.
		d.selected = 0
	}
}

// renderRunDetail is the selected run's accounting plus the two editor
// hand-offs. Deliberately label-shaped (not a table): the numbers are
// one-per-line facts, and anything deeper is a query away.
func (inst *PlayApp) renderRunDetail(row queryrunfacts.HistoryRow) {
	ids := inst.ids
	c.Separator().Send()
	for rt := range c.RichTextLabel("Run detail") {
		rt.Strong()
	}
	diagWeak(row.Ts.UTC().Format("2006-01-02 15:04:05") + " UTC · " + row.Event + " · " + row.Kind)
	diagWeak("query_id " + row.QueryId)
	line := fmt.Sprintf("duration %d ms · read %d rows / %s · result %d rows / %s · peak memory %s",
		row.DurationMs, row.ReadRows, humanBytes(row.ReadBytes),
		row.ResultRows, humanBytes(row.ResultBytes), humanBytes(row.MemoryPeak))
	if row.WrittenRows > 0 || row.WrittenBytes > 0 {
		line += fmt.Sprintf(" · wrote %d rows / %s", row.WrittenRows, humanBytes(row.WrittenBytes))
	}
	diagWeak(line)
	diagWeak(fmt.Sprintf("normalized hash %016x", row.NormalizedHash))
	if row.App != "" || row.RunId != "" || row.Lane != "" {
		parts := make([]string, 0, 3)
		if row.App != "" {
			parts = append(parts, "app "+row.App)
		}
		if row.Lane != "" {
			parts = append(parts, "lane "+row.Lane)
		}
		if row.RunId != "" {
			parts = append(parts, "run "+row.RunId)
		}
		diagWeak(strings.Join(parts, " · "))
	}
	if row.ExceptionCode != 0 || row.Exception != "" {
		c.Label(fmt.Sprintf("exception %d: %s", row.ExceptionCode, row.Exception)).
			Wrap().Selectable(true).Send()
	}
	for rt := range c.RichTextLabel(truncateRunes(row.QueryText, runsDetailTextRunes)) {
		rt.Monospace()
	}
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("open-as-query"), c.Atoms().Text("Open as query").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.ReplaceSql(row.QueryText)
		}
		if c.Button(ids.PrepareStr("profile-as-query"), c.Atoms().Text("Profile events as query").Keep()).
			SendResp().HasPrimaryClicked() {
			if sql, err := queryrunfacts.ComposeProfileEventsSql(runsHistoryFactsTable, row.Id); err == nil {
				inst.ReplaceSql(sql)
			}
		}
	}
}

// firstErrorLine folds an error to its first line for the status slot.
func firstErrorLine(err error) (out string) {
	out = err.Error()
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		out = out[:i]
	}
	return
}
