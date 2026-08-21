package play

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
)

// queryStateE is the playground's result↔input lifecycle: how the displayed
// result set relates to the run's inputs — the editor SQL plus its referenced
// signals (slice-5 D2). It drives the status-bar message and the fsmview chip
// so the user can tell "no query yet" from "ran, 0 rows", and sees when the
// shown output is stale relative to the current inputs.
//
// The three *Stale variants pair with their fresh counterparts: they mean
// "this result is still on screen, but an input — the buffer or a referenced
// signal — has changed since the run".
type queryStateE uint8

const (
	queryStateIdle        queryStateE = iota // no query has completed yet
	queryStateRunning                        // a query is in flight
	queryStateRows                           // ≥1 row, inputs match the run
	queryStateEmpty                          // 0 rows, inputs match the run
	queryStateFailed                         // query errored, inputs match the run
	queryStateRowsStale                      // inputs changed since these rows
	queryStateEmptyStale                     // inputs changed since this empty result
	queryStateFailedStale                    // inputs changed since this error
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

// runSignalsDiverged reports whether the signals the last Run SHIPPED would
// resolve differently now — the signal half of the staleness witness
// (slice-5 D2): a signal the run used that moved since makes the shown
// result stale, symmetric with a buffer edit (and it clears the same way
// when the value moves back). The comparison ranges over the last run's own
// params, not the buffer's slots: a narrowed run (a statement of several,
// a subquery) ships a subset of the buffer, and a signal referenced only
// OUTSIDE what ran cannot invalidate its result — comparing buffer-wide
// read as perpetual divergence the moment a run narrowed, staling every
// result and looping the Live toggle into its breaker. A run that shipped
// no params has nothing a signal move can stale. O(#shipped params) per
// frame, no parse.
func (inst *PlayApp) runSignalsDiverged() (diverged bool) {
	if len(inst.lastSentSigParams) == 0 {
		return
	}
	diverged = !maps.Equal(inst.resolveLastRunSignalsNow(), inst.lastSentSigParams)
	return
}

// resolveLastRunSignalsNow re-resolves exactly the names the last Run
// shipped, URL-keyed — the left-hand side of the staleness comparison.
// Shared with the Live circuit breaker so the breaker judges exactly the
// values the witness compares (resolveSignalNamesWithDefaults is the same
// helper the Run uses, so all three stay in lockstep on the reserved-String
// default). The bound set is the buffer's current SET-synced names: the
// staleness branches run only on an unchanged buffer, where that equals
// the set the run resolved against.
func (inst *PlayApp) resolveLastRunSignalsNow() map[string]string {
	names := make([]string, 0, len(inst.lastSentSigParams))
	for key := range inst.lastSentSigParams {
		names = append(names, strings.TrimPrefix(key, "param_"))
	}
	bound := make(map[string]bool, len(inst.paramSyncedValues))
	for name := range inst.paramSyncedValues {
		bound[name] = true
	}
	return resolveSignalNamesWithDefaults(names, bound, inst.frameSig)
}

// divergedSignalNames names the shipped signals whose current value differs
// from what the last Run sent — runSignalsDiverged's witness, itemised for
// the Live circuit breaker, which has to say WHICH signal is cycling. Names
// are bare (no `param_` prefix) and sorted, so a status line built from
// them is stable frame to frame. A name that no longer resolves (bound in
// the meantime, value gone from the store) counts as diverged, like any
// other change to what a re-run would send.
func (inst *PlayApp) divergedSignalNames() (names []string) {
	if len(inst.lastSentSigParams) == 0 {
		return
	}
	now := inst.resolveLastRunSignalsNow()
	seen := make(map[string]struct{}, len(inst.lastSentSigParams))
	for key, sent := range inst.lastSentSigParams {
		if v, ok := now[key]; ok && v == sent {
			continue
		}
		seen[strings.TrimPrefix(key, "param_")] = struct{}{}
	}
	names = make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return
}

// mirroredStateI is what a state has to be to ride a per-frame FSM mirror:
// comparable (fsmview's own constraint) and able to name itself in the
// diagnostic below.
type mirroredStateI interface {
	comparable
	fmt.Stringer
}

// mirrorObservedFSM drives a render-thread-only fsmview.Machine to the state
// this frame observed, and grades what it saw. Mirror follows any edge and
// reports whether a rule declared it; a rejecting Transition would instead
// wedge the mirror, since a memoryless observer re-proposes the same refused
// target every frame and the machine never catches up.
//
// An undeclared edge is not by itself a defect, and warning about one made
// noise of an ordinary event. A frame SAMPLES an asynchronous lifecycle: a
// query that starts and finishes between two repaints — a couple of
// milliseconds against a ~16 ms frame — presents its result with no `running`
// frame in between, so the observer hands us idle→rows and the declared path
// idle→running→rows is exactly the one it took unwatched. Those log at debug.
// What stays a warning is a target the declared graph cannot reach at all:
// there the observation contradicts the model rather than outrunning it,
// which is how the torn (loading, executed) read surfaced when it
// manufactured a spurious idle.
func mirrorObservedFSM[T mirroredStateI](m *fsmview.Machine[T], obs T, subject string) {
	cur := m.Current()
	if cur == obs {
		return
	}
	if declared := m.Mirror(obs); declared {
		return
	}
	ev := log.Warn()
	msg := "play: " + subject + " FSM observed an edge the declared graph cannot reach (mirrored)"
	if m.CanReach(cur, obs) {
		ev = log.Debug()
		msg = "play: " + subject + " FSM skipped states no frame sampled (mirrored)"
	}
	ev.Stringer("from", cur).Stringer("to", obs).Msg(msg)
}

// syncQueryFSM mirrors the observed state into the render-thread-only
// fsmview.Machine once per frame, mirroring the projector-FSM pattern in
// play_projection.go (renderProjection). observeQueryState is a memoryless
// projection of the snapshot, so it can legitimately hand us an edge
// newQueryFSM never drew — e.g. a first query that finishes within a single
// repaint skips the running observation, landing idle→rows(stale).
// mirrorObservedFSM follows it either way and grades it for the log.
//
// loading comes from the same store Snapshot as numRows/executed/err (not a
// fresh IsLoading()), so the observer never sees "not loading" against a
// pre-finish snapshot — the torn read that used to manufacture a spurious
// idle, an edge no declared path reaches and so still a warning.
func (inst *PlayApp) syncQueryFSM(loading bool, numRows int64, executed time.Time, err error) {
	mirrorObservedFSM(inst.queryFSM,
		inst.observeQueryState(loading, numRows, executed, err), "query result")
}

// newQueryFSM declares the query result lifecycle graph: Idle→Running→
// {Rows,Empty,Failed}, each result kind flips to its *Stale twin on an input
// change — a buffer edit or a referenced-signal move — and back on revert,
// and every settled state can re-Run. These rules
// drive the drawn graph (the popup's arrows) and label the happy path;
// they need not be exhaustive, because syncQueryFSM mirrors observed state
// with [fsmview.Machine.Mirror] — an edge not declared here is followed, not
// rejected, and warned about only when no declared path reaches its target
// (mirrorObservedFSM). A sub-frame-fast run, whose result the observer hands
// over with no `running` frame in between, is a skip of this graph, not a
// contradiction of it — so it earns no arrow. There is deliberately no Running→Idle edge: a cancel
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
		EdgeLabel(queryStateRows, queryStateRowsStale, "input change").
		EdgeLabel(queryStateEmpty, queryStateRunning, "Run").
		EdgeLabel(queryStateEmpty, queryStateEmptyStale, "input change").
		EdgeLabel(queryStateFailed, queryStateRunning, "Run").
		EdgeLabel(queryStateFailed, queryStateFailedStale, "input change").
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
// just right of the colored state badge. The full error text lives in the
// Diagnostics tab; the state graph / history live in the pop-out inspector
// window.
func (inst *PlayApp) renderQuerySummary(numRows int64, elapsed time.Duration, summary Summary, executed time.Time, err error, truncation string) {
	s := inst.querySummaryLine(numRows, elapsed, summary, executed, err, truncation)
	// A Run refused on unfilled inputs (5e, D3) reports where its result
	// summary would have landed — the FSM chip beside it keeps showing the
	// last settled state. Retires as soon as the inputs are filled or
	// edited away, no Run needed.
	if inst.runBlockedReason != "" && len(inst.unfilledInputs()) > 0 {
		s = "Run blocked: " + inst.runBlockedReason
	}
	// The write gate's refusal and the async write's outcome (ADR-0181
	// §SD8 M3). Both are set on Run and cleared by the next Run, so they
	// render as they stand — no re-derivation, unlike the unfilled gate.
	if inst.writeGateNotice != "" {
		s = "Run blocked: " + inst.writeGateNotice
	}
	if w := inst.writeRun.status(); w != "" {
		s = w
	}
	// The Live circuit breaker's verdict outranks the summary: Live went off
	// on its own, which the user has to be told, and the line names what was
	// cycling. Cleared by the next human Run.
	if inst.liveSuspendReason != "" {
		s = inst.liveSuspendReason
	}
	if s == "" {
		return
	}
	muted := color.Hex(styletokens.NeutralTextSecondary.AsHex())
	atoms := c.Atoms().BeginRichTextColored(muted, color.Transparent, s).Small().End().Keep()
	c.LabelAtoms(atoms).Send()
}

// querySummaryLine is the FSM-keyed one-line result summary, shared by the
// status bar and the Diagnostics tab's "Last run" section.
func (inst *PlayApp) querySummaryLine(numRows int64, elapsed time.Duration, summary Summary, executed time.Time, err error, truncation string) string {
	var s string
	// What the last run shipped, appended below to the states that describe a
	// result. Not to idle or running: before there are rows, "subquery only"
	// describes nothing on screen.
	scope := runScopeNote(inst.lastRunScope)
	switch inst.queryFSM.Current() {
	case queryStateIdle:
		return "type SQL and press Run"
	case queryStateRunning:
		if v := inst.frameProgress; v.fresh {
			// Live tick from the in-band progress headers (ADR-0115 plane A),
			// with the rate and ETA the tracker derives from it: the badge
			// counts up while the server reads.
			return "executing… " + formatProgressLine(v)
		}
		return "executing…"
	case queryStateRows:
		// What the run returned, then what it cost. Read ROWS ride beside read
		// bytes: bytes alone say how much came off disk but not how much work
		// that was, and the rate the two imply is what distinguishes a query
		// that was slow because it read a lot from one that was starved. The
		// read clause is dropped rather than printed as zeros on an endpoint
		// that reports no summary (chlocal, mocks).
		s = fmt.Sprintf("%s rows · %s", humanize.Comma(numRows), elapsed.Round(time.Millisecond))
		if summary.ReadRows > 0 || summary.ReadBytes > 0 {
			s += fmt.Sprintf(" · read %s rows / %s",
				humanize.Comma(int64(summary.ReadRows)), humanize.IBytes(summary.ReadBytes))
		}
		if rate := formatRowRate(summary.ReadRows, elapsed); rate != "" {
			s += " · " + rate
		}
		s += " · " + humanizeAgo(executed)
		// A capped result is indistinguishable from a whole one unless the
		// line says so (R9), and the row count alone reads as the answer.
		if truncation != "" {
			s = fmt.Sprintf("%s rows, capped · %s · %s",
				humanize.Comma(numRows), truncateRunes(truncation, 80), humanizeAgo(executed))
		}
	case queryStateEmpty:
		s = "0 rows · ran " + humanizeAgo(executed)
	case queryStateFailed:
		if err != nil {
			s = "errored: " + truncateRunes(firstLine(err.Error()), 80)
		} else {
			s = "errored · " + humanizeAgo(executed)
		}
	// "inputs changed", not "editor changed": since slice-5 D2 the staleness
	// witness also fires on a moved referenced signal, with the buffer
	// untouched.
	case queryStateRowsStale:
		s = fmt.Sprintf("%s rows · inputs changed", humanize.Comma(numRows))
	case queryStateEmptyStale:
		s = "0 rows · inputs changed"
	case queryStateFailedStale:
		s = "errored · inputs changed"
	}
	if s == "" {
		return s
	}
	return s + scope
}

// runScopeNote is what a run-subquery gesture appends to the summary.
//
// Only the two outcomes a result cannot show on its own are worth a word. A
// narrowed run says so because the rows on screen are not the buffer's answer;
// a gesture that found nothing to narrow to says so because it is otherwise
// indistinguishable from a plain Run — which is exactly how the shortcut first
// read as broken.
func runScopeNote(scope runScopeE) string {
	switch scope {
	case runScopeSubquery:
		return " · subquery only"
	case runScopeNoSubquery:
		return " · no subquery at the caret — ran the whole query"
	}
	return ""
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
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
	}
	return s
}
