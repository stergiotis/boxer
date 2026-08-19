package play

import (
	"slices"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// play_signals_chrome.go is the logic half of ADR-0097 slice 5e: the Signals
// section's row model (the store made visible AND writable — the
// "signal-writing widget" of slice-5 D3), the unfilled-input detection the D3
// empty-state hint reads, and the `main` live-toggle's auto-run policy (D2's
// per-node liveness bit — `main` is its only Run-gated holder, so the toggle
// surface is a single checkbox). Rendering lives in play_graph_view.go
// (renderSignalsSection) and renderTopBar; everything here is UI-free.

// signalChromeRow is one row of the Signals section: a name that is held in
// the store, referenced by the buffer, or both.
type signalChromeRow struct {
	Name string
	// Types are the distinct declared types the name is read as — buffer
	// slot declarations (every occurrence) plus the panel-reserved type
	// when the name is a reserved panel signal. More than one entry is the
	// SD8 hazard the chrome warns on: one shared value, divergent casts.
	Types    []string
	Conflict bool
	// Held/Raw/Writer/Rev mirror the store (signalRows) — zero values when
	// the name is only referenced.
	Held   bool
	Raw    string
	Writer string
	Rev    uint64
	// Pinned: the buffer SET-binds the name, so the constant shadows any
	// held signal at execution (D1) — the editor still writes the store,
	// but the run won't consult it until the SET is removed.
	Pinned bool
	// Unfilled: the buffer references the name and neither a SET nor the
	// store fills it — a Run would fail server-side, so it is blocked with
	// a hint instead (D3's empty-state).
	Unfilled bool
	// Lags: this is selection_id and it was last written BEFORE the cursor
	// it accompanies. selection_id tracks the last LEEWAY selection, so
	// clicking a row of an id-less result leaves it pointing at the
	// previous one — by design, and invisible without saying so.
	Lags bool
}

// reservedSignalTypes maps the panel-written signal names to the types their
// writers encode for — the Map's viewport slots (ADR-0096 SD6), the
// Timeline's extent (slice 5d), and the selection cursor + the World's clicked
// country (slice 5b). Used by the chrome to type rows the buffer does not
// declare, to cross-check buffer declarations for conflicts, and (for the
// String-typed names) to give a referenced-but-unwritten signal an empty
// default rather than blocking the Run — see signalDefaultsEmpty.
func reservedSignalTypes() (out map[string]string) {
	out = make(map[string]string, len(mapViewportSignals)+4)
	for _, s := range mapViewportSignals {
		out[string(s)] = "UInt32"
	}
	out[string(signalTimelineMin)] = "DateTime64(3, 'UTC')"
	out[string(signalTimelineMax)] = "DateTime64(3, 'UTC')"
	out[string(signalSelection)] = "Int64"
	out[string(signalSelectionNode)] = "String"
	out[string(signalSelectionID)] = "UInt64"
	out[string(signalSelectionKey)] = "String"
	out[string(signalSelectionCountry)] = "String"
	return
}

// signalDefaultsEmpty reports whether a referenced reserved signal takes an
// implicit empty value when nothing has written it yet. True for the
// String-typed panel signals (selection_country, selection_node): their unset
// state means "nothing selected", a valid empty filter the server accepts, so a
// query referencing the name runs from the first frame instead of blocking as
// an unfilled input until a panel emits — selection_country is only written on
// a World click, so before the first click there is otherwise nothing to fill
// it and the Run is refused. The numeric reserved signals (the Map viewport,
// the Timeline extent) have no safe empty literal and are seeded by their own
// panels on render, so they never default here and still gate the Run until
// their panel writes them.
func signalDefaultsEmpty(name string) bool {
	return reservedSignalTypes()[name] == "String"
}

// signalTypeTable returns name → distinct declared types for the current
// buffer, re-parsing only when the debounced preview pipeline has caught up
// with an edit (formattedFor is the post-debounce buffer) — so the chrome
// never parses per keystroke and at rest costs a string compare.
func (inst *PlayApp) signalTypeTable() map[string][]string {
	if inst.sigTypesFor == inst.formattedFor || inst.sql != inst.formattedFor {
		return inst.sigTypes
	}
	inst.sigTypesFor = inst.formattedFor
	inst.sigTypes = nil
	raw := strings.TrimSpace(inst.formattedFor)
	if raw == "" {
		return nil
	}
	pr, err := nanopass.Parse(raw)
	if err != nil {
		return nil
	}
	inst.sigTypes = collectSlotTypes(pr)
	return inst.sigTypes
}

// collectSignalChrome builds the Signals section's rows: the union of the
// store's held signals and the buffer's referenced slot names, sorted by
// name. Reads only the frame's debounced caches and the store snapshot —
// cheap enough to run per frame while the Graph tab is visible.
func (inst *PlayApp) collectSignalChrome() (rows []signalChromeRow) {
	held := inst.graph.signalRows()
	byName := make(map[string]signalRow, len(held))
	heldSet := make(map[string]bool, len(held))
	names := make([]string, 0, len(held)+len(inst.paramSlots))
	for _, h := range held {
		byName[h.Name] = h
		heldSet[h.Name] = true
		names = append(names, h.Name)
	}
	referenced := make(map[string]bool, len(inst.paramSlots))
	for _, s := range inst.paramSlots {
		referenced[s.Name] = true
		if !heldSet[s.Name] {
			if _, dup := byName[s.Name]; !dup {
				byName[s.Name] = signalRow{Name: s.Name}
				names = append(names, s.Name)
			}
		}
	}
	// held is name-sorted and the appended referenced names keep editor
	// order — re-sort the union for a stable render order (widget ids key
	// on the name, but row ORDER is what the eye tracks).
	sort.Strings(names)

	types := inst.signalTypeTable()
	reserved := reservedSignalTypes()
	rows = make([]signalChromeRow, 0, len(names))
	for _, name := range names {
		h := byName[name]
		_, pinned := inst.paramSyncedValues[name]
		row := signalChromeRow{
			Name:     name,
			Held:     heldSet[name],
			Raw:      h.Raw,
			Writer:   h.Writer,
			Rev:      h.Rev,
			Pinned:   pinned,
			Unfilled: referenced[name] && !pinned && !heldSet[name] && !signalDefaultsEmpty(name),
		}
		row.Types = append(row.Types, types[name]...)
		if rt, isReserved := reserved[name]; isReserved {
			row.Types = appendDistinct(row.Types, rt)
		}
		row.Conflict = len(row.Types) > 1
		rows = append(rows, row)
	}
	markLaggingSelectionID(rows, byName)
	return
}

// markLaggingSelectionID flags the selection_id row when the cursor has moved
// on without it — the row the id points at is not the row that is selected.
// It is a cue, not a warning: leaving the last leeway id in place is the
// documented behaviour of an id-less click, and a query cross-filtering on
// {selection_id} is still answering about a real row.
func markLaggingSelectionID(rows []signalChromeRow, byName map[string]signalRow) {
	cursor, hasCursor := byName[signalSelection]
	id, hasID := byName[signalSelectionID]
	if !hasCursor || !hasID || id.Rev >= cursor.Rev {
		return
	}
	for i := range rows {
		if rows[i].Name == signalSelectionID {
			rows[i].Lags = true
			return
		}
	}
}

// unfilledInputs lists the buffer's referenced slot names that neither a SET
// binds nor the store holds (D3's unfilled inputs), off the debounced caches
// and the frame snapshot — O(#slots) per frame, no parse. The Run path
// re-derives the same set from a fresh parse (resolveRunSignals), so the
// hint and the guard cannot disagree for long.
func (inst *PlayApp) unfilledInputs() (names []string) {
	for _, s := range inst.paramSlots {
		if _, bound := inst.paramSyncedValues[s.Name]; bound {
			continue
		}
		if exprCategoryFor(s.Type).spliced() {
			// A SQL-valued slot is filled by its `-- play: expr` line at the
			// pinned tier or by the store at the live one (ADR-0187
			// §SD3), never by the prelude, and it is substituted before the
			// body reaches the wire. Checked here rather than falling through
			// because the generic signal test below cannot tell an empty
			// predicate from a filled one, and `WHERE ()` is not a query.
			if v, declared := inst.paramSyncedExprs[s.Name]; declared && v != "" {
				continue
			}
			if raw, held := inst.signalRawFor(s.Name); held && raw != "" {
				continue
			}
			names = append(names, s.Name)
			continue
		}
		if inst.frameSig != nil {
			if _, heldHere := inst.frameSig.Get(s.Name); heldHere {
				continue
			}
		}
		if signalDefaultsEmpty(s.Name) {
			continue // reserved String signal → empty default, never blocks a Run
		}
		names = append(names, s.Name)
	}
	return
}

// hasUnboundSlots reports whether the buffer references at least one name its
// prelude does not bind — the condition under which the Live toggle is
// meaningful (a fully SET-bound buffer has no signal inputs to react to).
func (inst *PlayApp) hasUnboundSlots() bool {
	for _, s := range inst.paramSlots {
		if _, bound := inst.paramSyncedValues[s.Name]; !bound {
			return true
		}
	}
	return false
}

// shouldAutoRun is the `main` live-toggle's per-frame decision (slice 5e,
// D2): with Live on, re-run when a referenced signal moved since the last
// Run — and only then. Buffer edits stay human-gated (the toggle is
// "re-run", not "run-as-you-type"); an observed intermediate already
// re-drives on its own lane; an in-flight run defers the decision to the
// completion frame, so rapid signal churn coalesces to one run per
// completion (latest-wins at completion rate, not interaction rate); an
// unfilled input blocks exactly as it blocks a manual Run.
func (inst *PlayApp) shouldAutoRun() bool {
	if !inst.liveMain || inst.requestRun || inst.lastSentSql == "" {
		return false
	}
	if strings.TrimSpace(inst.sql) != inst.lastSentSql {
		return false
	}
	if inst.observedNode != "" && inst.observedNode != inst.currentSplit.Sink {
		return false
	}
	if inst.graph.MainLoading() {
		return false
	}
	if len(inst.unfilledInputs()) > 0 {
		return false
	}
	return inst.runSignalsDiverged()
}

// autoRunLoopLimit is how many consecutive machine-driven auto-runs Live
// tolerates before it suspends itself. SD9's acyclicity guard covers data
// edges — node reads node — and says nothing about a panel that publishes a
// signal derived from the result of a query that reads it: that loop is
// damped by write-dedup and frame quantization, but a query whose output
// moves its own input a little every run ratchets instead of settling. The
// limit is deliberately generous: a genuine burst of panel activity (a drag
// across the Map, a Timeline brush) produces short streaks well under it,
// while a ratchet passes it in a second or two.
const autoRunLoopLimit = 8

// noteAutoRunFired records that Live fired an auto-run and trips the circuit
// breaker on a self-feeding one (ADR-0097, the 2026-07-22 review
// remediation). A streak counts only while BOTH hold:
//
//   - the buffer has not changed — an edit is a human saying what to run;
//   - every diverging name was last written by a machine. A human write
//     (signals-editor, param-widget, history) means a person is driving the
//     value, however fast, and is never a loop to break.
//
// Tripping unchecks Live rather than muting it, so the state on screen is the
// state of the system: the user sees an unchecked box and a reason, and
// re-checking it is the resume gesture.
func (inst *PlayApp) noteAutoRunFired() {
	sql := strings.TrimSpace(inst.sql)
	cycling := inst.machineDrivenDivergence()
	if sql != inst.autoRunStreakSql {
		inst.autoRunStreak = 0
		inst.autoRunStreakSql = sql
	}
	if len(cycling) == 0 {
		inst.autoRunStreak = 0 // a person moved it — not a loop
		return
	}
	inst.autoRunStreak++
	if inst.autoRunStreak < autoRunLoopLimit {
		return
	}
	inst.liveMain = false
	// The breaker is the one writer of liveMain that is not a person, so
	// re-anchor the workingset baseline instead of letting the flip read as
	// intent (ADR-0148 §SD4).
	inst.rebaseWorkingsetLive(false)
	inst.autoRunStreak = 0
	inst.liveSuspendReason = "Live suspended: " + strings.Join(cycling, ", ") +
		" kept moving with no edit (a query feeding its own input) — re-check Live to resume"
}

// resumeLiveAfterHumanAction clears the breaker's state on a human Run: the
// person has said what to run, so whatever the streak was measuring is over.
func (inst *PlayApp) resumeLiveAfterHumanAction() {
	inst.autoRunStreak = 0
	inst.liveSuspendReason = ""
}

// machineDrivenDivergence returns the names whose value diverges from the
// last Run's inputs, but ONLY when every one of them was last written by a
// machine; a single human writer among them returns nil, because the whole
// point of the witness is to tell a person driving a value from a query
// driving its own input.
//
// A diverging name the store does not hold at all (a value that was
// discarded) has no writer to judge, so it reads as human — the conservative
// side, where the breaker does not fire.
func (inst *PlayApp) machineDrivenDivergence() (names []string) {
	diverged := inst.divergedSignalNames()
	if len(diverged) == 0 {
		return
	}
	for _, name := range diverged {
		writer, known := inst.graph.signalWriterFor(name)
		if !known || isHumanSignalWriter(writer) {
			return nil
		}
	}
	return diverged
}

func appendDistinct(ss []string, s string) []string {
	if slices.Contains(ss, s) {
		return ss
	}
	return append(ss, s)
}
