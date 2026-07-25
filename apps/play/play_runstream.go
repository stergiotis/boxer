package play

import (
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// play_runstream.go — the synchronous binding of the E3 frame contract
// (runstream). An HTTP response is the degenerate case of a result stream:
// one data frame per record batch, then exactly one terminal frame. Both of
// play's result paths drain through here, so what "the run finished" means
// is decided once rather than in each of them.
//
// What this buys, concretely: before it, a stream that died halfway and a
// stream that ended were told apart only by whether a particular error
// variable happened to be checked. Now the absence of a terminal frame is
// itself the answer, and the collector refuses to report an outcome it was
// never given.

// resultRowCap is a row limit the outgoing request declared on itself,
// recovered from its own SET prelude.
//
// Only the request's own settings are visible here. A cap that a server
// applies by default, or that a quota imposes, is not — those results come
// back short with nothing on the wire to say so, and boxer cannot make that
// honest from the client side.
type resultRowCap struct {
	// maxResultRows is `SET max_result_rows`, zero when unset.
	maxResultRows uint64
	// breaks is true when result_overflow_mode is `break`. ClickHouse
	// defaults to `throw`, which raises instead of truncating and is
	// already loud, so only `break` can produce a silently short result.
	breaks bool
}

// readResultRowCap recovers the row limit sql declares on itself. play
// leaves non-`param_` SET statements in the body — ExtractParams harvests
// only the `param_*` ones — so the settings that bound this result are
// still readable here.
//
// It reads them off the CST rather than through env.Extract, whose prelude
// harvest is line-based and therefore blind to the one-line form
// `SET max_result_rows=100; SELECT …` that play's own documentation uses.
//
// Best-effort, like the rest of the client-side path: unparseable SQL
// simply declares no cap.
func readResultRowCap(sql string) (cap resultRowCap) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return
	}
	queryCtx := findFirstQuery(pr)
	if queryCtx == nil {
		return
	}
	n := queryCtx.GetChildCount()
	for i := 0; i < n; i++ {
		setStmt, ok := queryCtx.GetChild(i).(*grammar1.SetStmtContext)
		if !ok {
			continue
		}
		_ = iterateSettingExprs(setStmt, func(expr *grammar1.SettingExprContext) (stopErr error) {
			name, value, exErr := extractSettingNameValue(pr, expr)
			if exErr != nil {
				return
			}
			switch name {
			case "max_result_rows":
				parsed, convErr := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
				if convErr == nil {
					cap.maxResultRows = parsed
				}
			case "result_overflow_mode":
				cap.breaks = strings.Trim(strings.TrimSpace(value), "'\"") == "break"
			}
			return
		})
	}
	return
}

// terminalFor decides how a run that completed its transfer ended: capped
// against the declared limit, or whole.
//
// The comparison is deliberately `>=` and the wording deliberately hedged.
// With `result_overflow_mode = break` ClickHouse stops at a block boundary,
// so the result can exceed the cap slightly; and a result that is complete
// at exactly the cap is indistinguishable from one that was cut there —
// ClickHouse does not say which. Reporting the ambiguous case as possibly
// capped is the direction R9 asks for: a reader who is told "this may be a
// prefix" can check, whereas a reader silently handed a prefix cannot.
func (inst resultRowCap) terminalFor(summary Summary) (t runstream.Terminal) {
	if inst.maxResultRows == 0 || !inst.breaks || summary.ResultRows < inst.maxResultRows {
		t = runstream.Complete()
		return
	}
	t = runstream.Truncated("reached max_result_rows=" + strconv.FormatUint(inst.maxResultRows, 10) +
		" with result_overflow_mode=break — the result may be a prefix")
	return
}

// drainRun reads an Arrow IPC response as a frame stream and returns the
// batches plus how the run ended. The caller owns the returned batches and
// MUST release them; on any error none are returned and none are retained.
//
// A read failure part-way is a failed terminal rather than a silently short
// result, which is the whole point of routing both result paths through
// here.
func drainRun(rdr *ipc.Reader, summary Summary, cap resultRowCap) (batches []arrow.RecordBatch, term runstream.Terminal, err error) {
	var col runstream.Collector[arrow.RecordBatch]
	var seq runstream.Seq
	next := func() (s runstream.Seq) {
		seq++
		s = seq
		return
	}
	release := func() {
		for _, b := range col.Data() {
			b.Release()
		}
	}

	for rdr.Next() {
		b := rdr.Record()
		b.Retain()
		pushErr := col.Push(runstream.DataFrame(next(), b))
		if pushErr != nil {
			// Unreachable short of a bug in this function — the sequence is
			// generated here and no terminal has been pushed. Surface it
			// rather than continuing with a stream nobody validated.
			b.Release()
			release()
			err = pushErr
			return
		}
	}

	readErr := rdr.Err()
	final := cap.terminalFor(summary)
	if readErr != nil {
		final = runstream.Failed(readErr)
	}
	err = col.Push(runstream.TerminalFrame[arrow.RecordBatch](next(), final))
	if err != nil {
		release()
		return
	}

	term, err = col.Terminal()
	if err != nil {
		release()
		return
	}
	if term.State == runstream.TerminalFailed {
		release()
		err = term.Err
		return
	}
	batches = col.Data()
	return
}
