package play

import (
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
}

// reservedSignalTypes maps the panel-written signal names to the types their
// writers encode for — the Map's viewport slots (ADR-0096 SD6), the
// Timeline's extent (slice 5d), and the selection cursor (slice 5b). Used by
// the chrome to type rows the buffer does not declare, and to cross-check
// buffer declarations for conflicts.
func reservedSignalTypes() (out map[string]string) {
	out = make(map[string]string, len(mapViewportSignals)+3)
	for _, s := range mapViewportSignals {
		out[string(s)] = "UInt32"
	}
	out[string(signalTimelineMin)] = "DateTime64(3, 'UTC')"
	out[string(signalTimelineMax)] = "DateTime64(3, 'UTC')"
	out[string(signalSelection)] = "Int64"
	return
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
			Unfilled: referenced[name] && !pinned && !heldSet[name],
		}
		row.Types = append(row.Types, types[name]...)
		if rt, isReserved := reserved[name]; isReserved {
			row.Types = appendDistinct(row.Types, rt)
		}
		row.Conflict = len(row.Types) > 1
		rows = append(rows, row)
	}
	return
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
		if inst.frameSig != nil {
			if _, heldHere := inst.frameSig.Get(s.Name); heldHere {
				continue
			}
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

func appendDistinct(ss []string, s string) []string {
	for _, have := range ss {
		if have == s {
			return ss
		}
	}
	return append(ss, s)
}
