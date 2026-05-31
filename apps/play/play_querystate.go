package play

import (
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
)

// queryStateE is the playground's result↔input lifecycle: how the displayed
// result set relates to the editor SQL. It drives the status-bar message and
// the fsmview chip so the user can tell "no query yet" from "ran, 0 rows",
// and sees when the shown output is stale relative to the edited SQL.
//
// The three *Stale variants pair with their fresh counterparts: they mean
// "this result is still on screen, but the editor has changed since the run".
type queryStateE uint8

const (
	queryStateIdle        queryStateE = iota // no query has completed yet
	queryStateRunning                        // a query is in flight
	queryStateRows                           // ≥1 row, editor matches the run
	queryStateEmpty                          // 0 rows, editor matches the run
	queryStateFailed                         // query errored, editor matches the run
	queryStateRowsStale                      // editor edited since these rows
	queryStateEmptyStale                     // editor edited since this empty result
	queryStateFailedStale                    // editor edited since this error
)

func (s queryStateE) String() string {
	switch s {
	case queryStateIdle:
		return "idle"
	case queryStateRunning:
		return "running"
	case queryStateRows:
		return "rows"
	case queryStateEmpty:
		return "empty"
	case queryStateFailed:
		return "failed"
	case queryStateRowsStale:
		return "rows (stale)"
	case queryStateEmptyStale:
		return "empty (stale)"
	case queryStateFailedStale:
		return "failed (stale)"
	}
	return "?"
}

// observeQueryState derives the lifecycle state from this frame's store
// snapshot plus the editor buffer — a pure function (no side effects). The
// executed timestamp (advances every QueryStore finish) separates "never
// ran" (idle) from "ran, empty"; inst.sql vs inst.lastSentSql (both
// canonical/trimmed, so param edits and snippet insert/replace count too)
// is the staleness witness.
func (inst *PlayApp) observeQueryState(loading bool, numRows int64, executed time.Time, err error) queryStateE {
	if loading {
		return queryStateRunning
	}
	if executed.IsZero() && err == nil {
		return queryStateIdle
	}
	kind := queryStateRows
	switch {
	case err != nil:
		kind = queryStateFailed
	case numRows == 0:
		kind = queryStateEmpty
	}
	if inst.lastSentSql != "" && strings.TrimSpace(inst.sql) != inst.lastSentSql {
		switch kind {
		case queryStateRows:
			return queryStateRowsStale
		case queryStateEmpty:
			return queryStateEmptyStale
		case queryStateFailed:
			return queryStateFailedStale
		}
	}
	return kind
}

// syncQueryFSM mirrors the observed state into the render-thread-only
// fsmview.Machine once per frame, mirroring the projector-FSM pattern in
// play_projection.go (renderProjection). A rejected transition means
// observeQueryState produced an edge newQueryFSM didn't declare — logged,
// not fatal, and shows up as a missing arrow in the popup graph.
func (inst *PlayApp) syncQueryFSM(numRows int64, executed time.Time, err error) {
	obs := inst.observeQueryState(inst.store.IsLoading(), numRows, executed, err)
	if cur := inst.queryFSM.Current(); cur != obs {
		if e := inst.queryFSM.Transition(obs); e != nil {
			log.Warn().
				Stringer("from", cur).
				Stringer("to", obs).
				Err(e).
				Msg("play: query result FSM mirror transition rejected")
		}
	}
}

// newQueryFSM declares the query result lifecycle graph: Idle→Running→
// {Rows,Empty,Failed}, each result kind flips to its *Stale twin on an edit
// (and back on revert), and every settled state can re-Run. The rule set
// must cover every edge observeQueryState can produce, else syncQueryFSM
// logs a rejected transition.
func newQueryFSM() *fsmview.Machine[queryStateE] {
	m := fsmview.NewMachine(queryStateIdle, 64,
		fsmview.WithLabel(func(s queryStateE) string { return s.String() }),
		fsmview.WithStateOrder([]queryStateE{
			queryStateIdle,
			queryStateRunning,
			queryStateRows,
			queryStateEmpty,
			queryStateFailed,
			queryStateRowsStale,
			queryStateEmptyStale,
			queryStateFailedStale,
		}),
		fsmview.WithStateColor(queryStateColor),
	)
	m.AddRule(queryStateIdle, queryStateRunning).
		AddRule(queryStateRunning, queryStateRows, queryStateEmpty, queryStateFailed, queryStateIdle).
		AddRule(queryStateRows, queryStateRunning, queryStateRowsStale).
		AddRule(queryStateEmpty, queryStateRunning, queryStateEmptyStale).
		AddRule(queryStateFailed, queryStateRunning, queryStateFailedStale).
		AddRule(queryStateRowsStale, queryStateRunning, queryStateRows).
		AddRule(queryStateEmptyStale, queryStateRunning, queryStateEmpty).
		AddRule(queryStateFailedStale, queryStateRunning, queryStateFailed).
		EdgeLabel(queryStateIdle, queryStateRunning, "Run").
		EdgeLabel(queryStateRunning, queryStateRows, "rows").
		EdgeLabel(queryStateRunning, queryStateEmpty, "0 rows").
		EdgeLabel(queryStateRunning, queryStateFailed, "error").
		EdgeLabel(queryStateRunning, queryStateIdle, "cancel").
		EdgeLabel(queryStateRows, queryStateRunning, "Run").
		EdgeLabel(queryStateRows, queryStateRowsStale, "edit").
		EdgeLabel(queryStateEmpty, queryStateRunning, "Run").
		EdgeLabel(queryStateEmpty, queryStateEmptyStale, "edit").
		EdgeLabel(queryStateFailed, queryStateRunning, "Run").
		EdgeLabel(queryStateFailed, queryStateFailedStale, "edit").
		EdgeLabel(queryStateRowsStale, queryStateRunning, "Run").
		EdgeLabel(queryStateRowsStale, queryStateRows, "revert").
		EdgeLabel(queryStateEmptyStale, queryStateRunning, "Run").
		EdgeLabel(queryStateEmptyStale, queryStateEmpty, "revert").
		EdgeLabel(queryStateFailedStale, queryStateRunning, "Run").
		EdgeLabel(queryStateFailedStale, queryStateFailed, "revert")
	return m
}

// queryStateColor tints the fsmview chip / graph nodes by severity: live
// query accent, success/warn/error for the fresh result kinds, and a muted
// neutral for the *Stale twins so "stale" reads as greyed-out at a glance.
func queryStateColor(s queryStateE, _ bool) styletokens.RGBA8 {
	switch s {
	case queryStateRunning:
		return styletokens.AccentDefault
	case queryStateRows:
		return styletokens.SuccessDefault
	case queryStateEmpty:
		return styletokens.WarningDefault
	case queryStateFailed:
		return styletokens.ErrorDefault
	case queryStateRowsStale, queryStateEmptyStale, queryStateFailedStale:
		return styletokens.NeutralDefault
	}
	return styletokens.NeutralSubtle // idle
}

// queryStateTone maps the FSM state to a badge tone so the tethered summary's
// level-1 badge reads by severity: success rows, warning empty, error failed,
// muted neutral for idle and the stale twins.
func queryStateTone(s queryStateE) badge.ToneE {
	switch s {
	case queryStateRunning:
		return badge.TonePrimary
	case queryStateRows:
		return badge.ToneSuccess
	case queryStateEmpty:
		return badge.ToneWarning
	case queryStateFailed:
		return badge.ToneError
	}
	return badge.ToneNeutral // idle + the *Stale twins
}

// renderQuerySummary is the tethered summary's stat line
// ([fsmview.Widget.Summary]): muted small text keyed on the FSM state, rendered
// just right of the colored state badge. The full error text and the result
// grid live in the Table tab; the state graph / history live in the pop-out
// inspector window.
func (inst *PlayApp) renderQuerySummary(numRows int64, elapsed time.Duration, summary Summary, executed time.Time, err error) {
	var s string
	switch inst.queryFSM.Current() {
	case queryStateIdle:
		s = "type SQL and press Run"
	case queryStateRunning:
		s = "executing…"
	case queryStateRows:
		s = fmt.Sprintf("%d rows · %s · %s read · %s",
			numRows, elapsed.Round(time.Millisecond), humanBytes(summary.ReadBytes), humanizeAgo(executed))
	case queryStateEmpty:
		s = "0 rows · ran " + humanizeAgo(executed)
	case queryStateFailed:
		if err != nil {
			s = "errored: " + capLen(firstLine(err.Error()), 80)
		} else {
			s = "errored · " + humanizeAgo(executed)
		}
	case queryStateRowsStale:
		s = fmt.Sprintf("%d rows · editor changed", numRows)
	case queryStateEmptyStale:
		s = "0 rows · editor changed"
	case queryStateFailedStale:
		s = "errored · editor changed"
	}
	if s == "" {
		return
	}
	muted := color.Hex(styletokens.NeutralTextSecondary.AsHex())
	atoms := c.Atoms().BeginRichTextColored(muted, color.Transparent, s).Small().End().Keep()
	c.LabelAtoms(atoms).Send()
}

// humanizeAgo renders a coarse "Xs/Xm/Xh ago" for the time a result was
// produced. Empty for the zero time (no query has finished).
func humanizeAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	switch d := time.Since(t); {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func capLen(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
