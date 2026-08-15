package play

// play_write_run.go — ADR-0181 §SD8 M3: the write half of Run. Expansion,
// diagnostics and preview treat an INSERT … SELECT wrapper like any other
// statement; EXECUTING one is gated behind BOXER_PLAY_ALLOW_WRITES, and an
// allowed write runs on its own small path — a write answers with a
// summary, not an Arrow stream, so it never enters the result machinery.
// sqlapplet hosts the same engine, so the gate governs it too.

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// runIsInsertWrapper reports whether the shipped text is the INSERT wrapper.
// The SET prelude is harvested first (grammar1 is single-statement); a text
// grammar1 cannot parse is not a wrapper — it ships exactly as before and
// the server names the problem.
func runIsInsertWrapper(runSQL string) bool {
	residual, _, err := ExtractParams(runSQL)
	if err != nil {
		return false
	}
	pr, err := nanopass.Parse(residual)
	if err != nil {
		return false
	}
	return pr.InsertStmt() != nil
}

// writeRunState is the async write's delivery seam: the goroutine writes,
// the render loop reads via status(). A mutex rather than a lane, because a
// write has no stream to manage — one request, one outcome line. The
// outcome persists until the next Run so the user sees how the write ended
// even if it finished between frames.
type writeRunState struct {
	mu       sync.Mutex
	inFlight bool
	outcome  string
}

func (inst *writeRunState) begin() {
	inst.mu.Lock()
	inst.inFlight, inst.outcome = true, ""
	inst.mu.Unlock()
}

func (inst *writeRunState) finish(outcome string) {
	inst.mu.Lock()
	inst.inFlight, inst.outcome = false, outcome
	inst.mu.Unlock()
}

func (inst *writeRunState) clear() {
	inst.mu.Lock()
	inst.inFlight, inst.outcome = false, ""
	inst.mu.Unlock()
}

func (inst *writeRunState) status() string {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.inFlight {
		return "write running …"
	}
	return inst.outcome
}

// executeWriteRun ships the write off the render thread and reports the
// outcome on the query summary line. Background-rooted context, like the
// store's fetches: the write must finish or fail on its own account, not
// with a frame.
func (inst *PlayApp) executeWriteRun(runSQL string, sigParams map[string]string) {
	inst.writeRun.begin()
	client := inst.client
	state := &inst.writeRun
	go func() {
		start := time.Now()
		summary, err := client.ExecuteWrite(context.Background(), runSQL, sigParams)
		if err != nil {
			log.Warn().Err(err).Msg("play: write run failed")
			state.finish("write failed: " + firstErrorLine(err))
			return
		}
		state.finish(fmt.Sprintf("write OK — %d row(s), %d byte(s) written in %d ms",
			summary.WrittenRows, summary.WrittenBytes, time.Since(start).Milliseconds()))
	}()
}

// ExecuteWrite ships a write statement (the INSERT wrapper) and returns the
// server's summary. The body is BuildStatement's — every pre-execute
// rewrite applied, target adoption included, and no FORMAT appended — and
// the response is drained to its terminal, because ClickHouse can raise a
// failure after the status line and a write path that stops at the header
// would report such a run as OK (the documented chclient.Exec gap; this
// path must not inherit it).
func (inst *Client) ExecuteWrite(ctx context.Context, sql string, signals map[string]string) (summary Summary, err error) {
	dec := inst.Dispatch(sql, "")
	eng, err := inst.engineFor(dec)
	if err != nil {
		return
	}
	q, params := inst.BuildStatement(sql)
	opts := newExecOptions("write")
	req := queryengine.Request{
		SQL:         q,
		Params:      bareParams(signals, params),
		Settings:    map[string]string{"replace_running_query": "1"},
		Sensitivity: dec.sensitivity,
		RunID:       opts.QueryID,
	}
	if lc := inst.composeLogComment(sql, q, params, signals, opts); lc != "" {
		req.Settings["log_comment"] = lc
	}
	st, res, err := eng.Deliver(ctx, req)
	if err != nil {
		return
	}
	summary = summaryFrom(res.Summary)
	rs, err := openResultStream(st)
	if err != nil {
		return
	}
	// A successful INSERT body is empty; whatever arrives is drained so the
	// terminal is reached and judged.
	_, _ = io.Copy(io.Discard, rs)
	term, tErr := rs.terminal()
	_ = rs.Close()
	if tErr != nil {
		err = eh.Errorf("write: %w", tErr)
		return
	}
	if term.State == runstream.TerminalFailed {
		err = term.Err
		if err == nil {
			err = eh.Errorf("write: the run ended failed without a diagnostic")
		}
	}
	return
}
