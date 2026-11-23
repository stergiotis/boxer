package play

import (
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// play_runstream.go — play's side of the E3 frame contract (runstream) and
// of the ADR-0144 delivery role.
//
// The engine adapter hands back a stream of byte frames ending in exactly
// one terminal; this file turns those bytes into Arrow record batches and
// carries the engine's verdict through to the panels. What "the run
// finished" means is decided in one place — and now on the engine's side of
// the seam, where the response header and the declared row cap both live.
//
// What that buys, concretely: before it, a stream that died halfway and a
// stream that ended were told apart only by whether a particular error
// variable happened to be checked. Now the absence of a terminal frame is
// itself the answer, and nothing here can report an outcome it was never
// given.

// readResultRowCap recovers the row limit sql declares on itself, for the
// engine to judge the delivered result against (R9).
//
// Only the request's own settings are visible: play leaves non-`param_` SET
// statements in the body — ExtractParams harvests only the `param_*` ones —
// so the settings that bound this result are still readable here. A cap a
// server applies by default, or that a quota imposes, is not, and comes
// back short with nothing on the wire to say so.
//
// It reads them off the CST rather than through env.Extract, whose prelude
// harvest is line-based and therefore blind to the one-line form
// `SET max_result_rows=100; SELECT …` that play's own documentation uses.
//
// Best-effort, like the rest of the client-side path: unparseable SQL
// simply declares no cap.
func readResultRowCap(sql string) (cap queryengine.RowCap) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return
	}
	queryCtx := findFirstQuery(pr)
	if queryCtx == nil {
		return
	}
	n := queryCtx.GetChildCount()
	for i := range n {
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
					cap.MaxResultRows = parsed
				}
			case "result_overflow_mode":
				// ClickHouse defaults to `throw`, which raises instead of
				// truncating and is already loud, so only `break` can
				// produce a silently short result.
				cap.Breaks = strings.Trim(strings.TrimSpace(value), "'\"") == "break"
			}
			return
		})
	}
	return
}

// resultStream adapts an engine's frame stream to the Arrow decoder, which
// wants an io.Reader, and remembers the terminal frame on the way past.
//
// The lookahead in [openResultStream] is what keeps the failure path honest:
// a run the server rejected yields its terminal as the FIRST frame, and
// catching it there surfaces the server's own diagnostic instead of the
// Arrow decoder's complaint about an empty stream.
type resultStream struct {
	st      queryengine.StreamI
	pending []byte
	term    runstream.Terminal
	hasTerm bool
	// sensitivity is what the request was sent as: a run that read a
	// confined session data set or attached an ad-hoc dataset is confined
	// although its dispatch decision could not know. It is the wall's
	// word, where the run may go.
	sensitivity queryengine.SensitivityE
	// ownClear says everything sealed the run read is the reading task's
	// own publication at TLP:CLEAR, as the services that hold the data
	// answered (ownClearRun): the result is not labelled for it.
	ownClear bool
	// ownClearTask is the task the services answered that for: the label
	// is left off for that task's reading of the result and for no other
	// reader's.
	ownClearTask string
	// onClose runs once when the stream closes: the release of the data
	// session's lease the run held.
	onClose   func()
	closeOnce sync.Once
}

var _ io.Reader = (*resultStream)(nil)

// openResultStream wraps a delivered stream, failing immediately if the run
// ended before any bytes arrived.
func openResultStream(st queryengine.StreamI) (rs *resultStream, err error) {
	rs = &resultStream{st: st}
	rs.pull()
	if rs.hasTerm && rs.term.State == runstream.TerminalFailed {
		err = rs.term.Err
		_ = st.Close()
		rs = nil
		return
	}
	if text, is := rs.exceptionInPlaceOfResult(); is {
		err = eh.Errorf("clickhouse failed after it began to answer: %s", text) //boxer:lint disable=CS013 reason="the server's exception is the error's text, as it is when a status carries it"
		_ = st.Close()
		rs = nil
		return
	}
	return
}

// exceptionLimit bounds how much of a server's exception text is read from
// a body that holds it in place of a result.
const exceptionLimit = 16 << 10

// exceptionInPlaceOfResult reports the exception text of a body that begins
// with one. A server that fails once it has sent its response header has
// no status left to report the failure with and writes the exception where
// the result would be; an Arrow stream never begins with those bytes.
func (inst *resultStream) exceptionInPlaceOfResult() (text string, is bool) {
	const mark = "Code: "
	for len(inst.pending) <= len(mark) && !inst.hasTerm {
		f, ok := inst.st.Next()
		if !ok {
			break
		}
		switch f.Kind {
		case runstream.KindData:
			inst.pending = append(inst.pending[:len(inst.pending):len(inst.pending)], f.Data...)
		case runstream.KindTerminal:
			inst.term = f.Terminal
			inst.hasTerm = true
		}
	}
	if len(inst.pending) <= len(mark) || string(inst.pending[:len(mark)]) != mark || inst.pending[len(mark)] < '0' || inst.pending[len(mark)] > '9' {
		return
	}
	body, _ := io.ReadAll(io.LimitReader(inst, exceptionLimit))
	return strings.TrimSpace(string(body)), true
}

// pull advances until there are bytes to read or the stream is over.
// Progress frames are skipped: they are advisory, and this path is the
// connection holder, which already sees the in-band ticks (R8).
func (inst *resultStream) pull() {
	for len(inst.pending) == 0 && !inst.hasTerm {
		f, ok := inst.st.Next()
		if !ok {
			return
		}
		switch f.Kind {
		case runstream.KindData:
			inst.pending = f.Data
		case runstream.KindTerminal:
			inst.term = f.Terminal
			inst.hasTerm = true
		}
	}
}

func (inst *resultStream) Read(p []byte) (n int, err error) {
	inst.pull()
	if len(inst.pending) == 0 {
		err = io.EOF
		return
	}
	n = copy(p, inst.pending)
	inst.pending = inst.pending[n:]
	return
}

// Close releases the delivered stream. Safe to call more than once.
func (inst *resultStream) Close() (err error) {
	err = inst.st.Close()
	inst.closeOnce.Do(func() {
		if inst.onClose != nil {
			inst.onClose()
		}
	})
	return
}

// Confined reports the label a result of the run carries (ADR-0145): it
// read sealed data that is not all the reading task's own publication at
// TLP:CLEAR, whatever the dispatch decision said before the run was
// prepared.
func (inst *resultStream) Confined() (confined bool) {
	return inst.Sealed() && !inst.ownClear
}

// Sealed reports whether the run read sealed data, whoever published it:
// what the wall goes by. A result that is Sealed and not Confined holds
// only the reading task's own publications at TLP:CLEAR.
func (inst *resultStream) Sealed() (sealed bool) {
	return inst != nil && inst.sensitivity == queryengine.SensitivityConfined
}

// ClearFor is the task whose own publications at TLP:CLEAR are everything
// sealed the run read; empty for a run that read nothing sealed and for a
// labelled one. A result that is Sealed is unlabelled for that task alone.
func (inst *resultStream) ClearFor() (task string) {
	if inst.Sealed() && inst.ownClear {
		task = inst.ownClearTask
	}
	return
}

// terminal reports how the engine said the run ended, or ErrIncomplete when
// it never said — a producer that stopped, which must not read as a short
// answer.
//
// It drains first, because the Arrow decoder stops at the IPC
// end-of-stream marker rather than at end of body: the frames after that
// point — including the terminal — are still sitting in the stream, and a
// reader that never asked for them would conclude the producer had died.
// Whatever those trailing bytes are, they are not part of the decoded
// result, so they are discarded rather than returned.
func (inst *resultStream) terminal() (t runstream.Terminal, err error) {
	for !inst.hasTerm {
		inst.pending = nil
		inst.pull()
		if len(inst.pending) == 0 && !inst.hasTerm {
			// The stream really is exhausted, and it never said how the run
			// ended. Absence is the safe reading.
			err = runstream.ErrIncomplete
			return
		}
	}
	t = inst.term
	return
}

// drainRun decodes the delivered stream into record batches and reports how
// the run ended. The caller owns the returned batches and MUST release them;
// on any error none are returned and none are retained.
//
// Two things can go wrong independently, and they are kept apart: the
// TRANSFER can break, which the engine reports in its terminal, and the
// bytes can fail to DECODE, which only this side can see. A transfer that
// broke wins, because it explains the decode failure that follows from it.
func drainRun(rdr *ipc.Reader, rs *resultStream) (batches []arrow.RecordBatch, term runstream.Terminal, err error) {
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
		b := rdr.RecordBatch()
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

	final, termErr := rs.terminal()
	if termErr != nil {
		// The engine stopped without saying how the run ended. Nothing had
		// to go right for this to be caught, which is the point.
		release()
		err = termErr
		return
	}
	if readErr := rdr.Err(); readErr != nil && final.State != runstream.TerminalFailed {
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
