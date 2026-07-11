package play

import (
	"fmt"
	"maps"
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
// ran" (idle) from "ran, empty". The staleness witness is twofold (ADR-0097
// slice-5 D2): inst.sql vs inst.lastSentSql (both canonical/trimmed, so
// param edits and snippet insert/replace count too), OR the buffer's current
// signal resolution diverging from what the last Run shipped.
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
	if inst.lastSentSql != "" &&
		(strings.TrimSpace(inst.sql) != inst.lastSentSql || inst.runSignalsDiverged()) {
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

// runSignalsDiverged reports whether the buffer's CURRENT signal resolution
// differs from what the last Run shipped — the signal half of the staleness
// witness (slice-5 D2): a referenced signal that moved since the run makes
// the shown result stale, symmetric with a buffer edit (and it clears the
// same way when the value moves back). It reads the debounced slot caches
// (paramSlots for the referenced names, paramSyncedValues for the SET-bound
// set — a SET pins, D1) against the frame snapshot: O(#slots) per frame, no
// parse.
func (inst *PlayApp) runSignalsDiverged() (diverged bool) {
	if len(inst.paramSlots) == 0 && len(inst.lastSentSigParams) == 0 {
		return
	}
	names := make([]string, 0, len(inst.paramSlots))
	for _, s := range inst.paramSlots {
		names = append(names, s.Name)
	}
	bound := make(map[string]bool, len(inst.paramSyncedValues))
	for name := range inst.paramSyncedValues {
		bound[name] = true
	}
	resolvedNow := resolveSignalNames(names, bound, inst.frameSig)
	diverged = !maps.Equal(resolvedNow, inst.lastSentSigParams)
	return
}

// syncQueryFSM mirrors the observed state into the render-thread-only
// fsmview.Machine once per frame, mirroring the projector-FSM pattern in
// play_projection.go (renderProjection). observeQueryState is a memoryless
// projection of the snapshot, so it can legitimately hand us an edge
// newQueryFSM never drew — e.g. a first query that finishes within a single
// repaint skips the running observation, landing idle→rows(stale). We use
// Mirror, which follows the edge regardless and reports declared=false so we
// can log it as a diagnostic. A rejecting Transition would instead wedge the
// mirror: the observer re-proposes the same unreachable target every frame,
// so one refusal freezes the FSM a state behind for good.
//
// loading comes from the same store Snapshot as numRows/executed/err (not a
// fresh IsLoading()), so the observer never sees "not loading" against a
// pre-finish snapshot — the torn read that used to manufacture a spurious
// idle and trigger exactly this wedge.
func (inst *PlayApp) syncQueryFSM(loading bool, numRows int64, executed time.Time, err error) {
	obs := inst.observeQueryState(loading, numRows, executed, err)
	if cur := inst.queryFSM.Current(); cur != obs {
		if declared := inst.queryFSM.Mirror(obs); !declared {
			log.Warn().
				Stringer("from", cur).
				Stringer("to", obs).
				Msg("play: query result FSM observed an undeclared edge (mirrored)")
		}
	}
}

// newQueryFSM declares the query result lifecycle graph: Idle→Running→
// {Rows,Empty,Failed}, each result kind flips to its *Stale twin on an edit
// (and back on revert), and every settled state can re-Run. These rules
// drive the drawn graph (the popup's arrows) and label the happy path;
// they need not be exhaustive, because syncQueryFSM mirrors observed state
// with [fsmview.Machine.Mirror] — an edge not declared here is followed and
// logged, not rejected. There is deliberately no Running→Idle edge: a cancel
// sets err+executed in the store, so it settles as Failed, not Idle.
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
		AddRule(queryStateRunning, queryStateRows, queryStateEmpty, queryStateFailed).
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
			s = "errored: " + truncateRunes(firstLine(err.Error()), 80)
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
