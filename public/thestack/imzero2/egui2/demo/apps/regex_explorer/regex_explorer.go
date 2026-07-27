package regex_explorer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow/memory"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// editorWidth is the desired width (egui points) for the pattern,
// haystack, and replacement TextEdits. It is finite on purpose.
//
// egui's TextEdit allocates min(desired_width, available_width). The
// previous value was float32::INFINITY — "take all available width" —
// but inside the runtime-owned, resizable egui::Window, egui runs a
// content-sizing pass in which available width is unbounded, so the
// editors reported an unbounded desired size and the window auto-grew
// out to the host-window edge. That balloon is undesirable in general
// and especially when the explorer is embedded as a tethered inspector,
// where it should stay compact near its anchor. A finite width caps the
// editors — and therefore the window's natural width — while the min()
// clamp still lets them shrink on a narrow host, so it never overflows.
// ~800 fills the central panel at the manifest's 1100 preferred width
// (minus the 280-pt cheatsheet panel); tune for readability. Compare
// configview, which uses a finite DesiredWidth(280) for the same reason.
const editorWidth = float32(800)

// App holds per-window state for the regex explorer: the current pattern
// and haystack bound to the UI text-edit widgets, the last result of each
// kind of ClickHouse query, and the compiled-regexp cache the Go-side
// highlight painter uses.
//
// Each query kind (match, extractAll, replaceRegexpAll, multiMatchAllIndices)
// has its own atomic-bool coalescer so all four can be in flight
// concurrently as independent broker requests.
//
// Concurrency, precisely — the fields fall into three groups:
//
//   - Input and view state (pattern, haystack, replacement, patternList,
//     the flag toggles, lastFocusedInput, ids) is confined to the render
//     thread. The egui bindings write several of these through pointers
//     handed to SendRespVal, which no lock could cover anyway, so the
//     confinement is the invariant, not the lock.
//   - Query results, errors, stats, the tripwire outcome, and bus are
//     written by worker goroutines and read by the render thread; mu
//     covers those. Input state is also read under mu when a dispatcher
//     snapshots it for a worker, which is harmless and keeps the snapshot
//     in one place.
//   - compileCache has its own mutex (compileCacheMu) because the
//     tripwire goroutine shares it with the render thread and must not
//     contend on mu with the query workers.
type App struct {
	mu       sync.RWMutex
	pattern  string
	haystack string

	replacement string
	patternList string

	// One lane per ClickHouse call. Each owns its own in-flight state,
	// last-good result, input fingerprint, and error — so "is this
	// showing the current input?" is one comparison rather than a
	// convention every result surface has to remember to follow.
	matchLane   queryLane[bool]
	listLane    queryLane[listOutcome]
	replaceLane queryLane[string]
	multiLane   queryLane[[]multiLine]

	caseInsensitive bool
	multiline       bool
	dotAll          bool

	// lastFocusedInput is 0 for pattern, 1 for haystack, 2 for patternList,
	// 3 for replacement. Cheatsheet token-clicks append into the field with
	// this index. True cursor-position insertion is not exposed through the
	// current FFFI2 binding.
	lastFocusedInput int

	tripwireRan atomic.Bool
	tripwire    tripwireResult

	// Extraction hand-off state (ADR-0017). Written by the worker
	// goroutine that publishes and opens, read by the render thread —
	// so it belongs to the mu group above. evalGoHandle / evalChHandle
	// are retained across republishes so one window holds at most two
	// datasets against the ADR-0134 MaxDatasets cap.
	evalBusy   bool
	evalErr    string
	evalStatus string
	// evalKey fingerprints the inputs evalStatus / evalErr describe, so
	// an outcome is retired when the editors move on rather than
	// presented as current — the [queryLane] freshness rule.
	evalKey      queryKey
	evalGoHandle string
	evalChHandle string

	alloc memory.Allocator

	// bus is the per-instance BusI captured at Mount. All SQL goes
	// through ch.local.exec.regex_explorer via the broker; the
	// subprocess-shell-out path has been retired.
	//
	// Guarded by mu: a host may re-attach a bus between frames
	// (regexsummary pushes one on every open frame) while query
	// goroutines are still in flight, so writes go through [App.setBus]
	// and reads through [App.busSnapshot]. Reading the field directly
	// from a query goroutine races with the render thread.
	bus runtimeapp.BusI

	// ids is the per-instance WidgetIdStack the host pre-prepares
	// with a window-unique salt every frame. Captured from
	// MountCtx.Ids() at Mount time; every renderer reaches the stack
	// through its receiver's ids and so inherits the host's salt.
	// Cross-app id collisions cannot happen even when two apps use
	// the same label string. Demo scenes rebind it per frame from the
	// gallery's stack; tests keep the default stack from newApp().
	//
	// Render-thread-confined, like [App.pattern] and friends — see the
	// mu comment above.
	ids *c.WidgetIdStack

	compileCacheMu sync.Mutex
	compileCache   map[string]compileResult

	// Retained syntax-highlight jobs for the two pattern editors
	// (ADR-0015), rebuilt only when their buffer changes. Render-thread-
	// confined, like the input state they mirror — see the mu comment
	// above.
	patternHl     highlightCache
	patternListHl highlightCache
}

// newApp builds one [App] — the unit of per-window state. clickhouse-local
// is reached via the chlocalbroker subject `ch.local.exec.regex_explorer`;
// no binary path or env var is consulted here. The fresh WidgetIdStack the
// App carries is the fallback used by tests; AppInstance.Mount (and the
// demo scenes' BusInit) override it with the host-supplied per-instance
// stack so interactive multi-window renders don't collide.
func newApp() (inst *App) {
	inst = &App{
		ids:   c.NewWidgetIdStack(),
		alloc: memory.NewGoAllocator(),
	}
	return
}

// setBus attaches bus as the transport for subsequent queries. Safe to
// call from the render thread while queries are in flight; an in-flight
// query keeps whatever [App.busSnapshot] handed it when it started.
func (inst *App) setBus(bus runtimeapp.BusI) {
	inst.mu.Lock()
	inst.bus = bus
	inst.mu.Unlock()
}

// busSnapshot returns the currently attached transport. Query goroutines
// must reach the bus through here rather than touching the field, so a
// host re-attaching a bus mid-flight does not race them.
func (inst *App) busSnapshot() (bus runtimeapp.BusI) {
	inst.mu.RLock()
	bus = inst.bus
	inst.mu.RUnlock()
	return
}

// AppInstance is the per-window regex_explorer AppI value. Each host
// Open() yields a fresh AppInstance with its own *App state (pattern,
// haystack, replacement, query results, mode flags, …), and Frame()
// renders that state directly.
//
// Every renderer is a method on *App, so per-window state reaches them
// through the receiver. This is deliberate: the app previously kept a
// package-level *App that Frame swapped in and out for the duration of
// each render call, which worked only as long as nothing outside the
// render thread read it — and the SD1 tripwire goroutine did, racing
// the swap and landing in whichever window happened to be drawing.
type AppInstance struct {
	state *App
}

var _ runtimeapp.AppI = (*AppInstance)(nil)

func newInstance() (inst *AppInstance) {
	inst = &AppInstance{
		state: newApp(),
	}
	return
}

func (inst *AppInstance) Manifest() (m runtimeapp.Manifest) { m = manifest; return }

// Mount captures the host's BusI and per-instance WidgetIdStack
// on inst.state. The bus is used by query goroutines to publish on
// ch.local.exec.<pool> via the chlocalbroker (ADR-0028 §SD9). The
// ids stack is pre-prepared by the host every frame with a window-
// unique salt so the renderer can derive widget ids that cannot
// collide with another app's ids — even when two apps use the same
// label string (e.g. "btm" for their bottom panel).
func (inst *AppInstance) Mount(ctx runtimeapp.MountContextI) (err error) {
	if inst.state != nil {
		inst.state.setBus(ctx.Bus())
		inst.state.ids = ctx.Ids()
	}
	return
}

// Unmount abandons anything still in flight and retracts anything this
// window published. Without the first a closed window leaves up to four
// queries running against pooled clickhouse-local workers with nothing
// left to consume their results; without the second its ad-hoc datasets
// (ADR-0017) would sit in the ephemeral store until process exit.
func (inst *AppInstance) Unmount(ctx runtimeapp.MountContextI) (err error) {
	if inst.state != nil {
		inst.state.cancelQueries()
		inst.state.retractEvalDatasets()
	}
	return
}

// Frame renders this instance's state. The host has already pre-pushed a
// window-unique salt onto inst.state.ids via c.IdScope
// (windowhost.renderWindowBody), so every widget id the renderer derives
// from inst.ids is scoped under that salt and cannot collide with another
// open app's ids.
//
// Kicks off the SD1 engine-fidelity tripwire on the first call
// (coalesced by [App.tripwireRan] on the per-instance state).
func (inst *AppInstance) Frame(ctx runtimeapp.FrameContextI) (err error) {
	inst.state.RunTripwire(context.Background())
	inst.state.RenderWindow()
	return
}

// Screenshot capture is enrolled via registry.Register in
// regex_explorer_tour.go (ADR-0057), which allocates one [App] per demo
// scene through the registry's stateful BusInit/RenderStateful contract
// and draws it through RenderWindow below; the central widgets TestDriver
// captures the result.

// RenderWindow draws the regex-explorer body into the caller's UI scope:
// left cheatsheet panel, central body with pattern / haystack inputs and
// tabbed results, and a bottom status bar. Per ADR-0026 Amendment
// 2026-05-12, the host wraps this in a runtime-created c.Window using
// Manifest.WindowTitle/Icon; the body uses only *Inside panel variants.
// PanelCentralInside is retained so the body has an owned layout scope —
// without it, the inputs flicker and steal width unpredictably from the
// left panel.
func (inst *App) RenderWindow() {
	for range c.PanelBottomInside(inst.ids.PrepareStr("btm")).DefaultSize(24).Resizable(false).KeepIter() {
		inst.renderStatusBar()
	}

	for range c.PanelLeftInside(inst.ids.PrepareStr("cheat")).DefaultSize(280).Resizable(true).KeepIter() {
		inst.renderCheatsheet()
	}

	for range c.PanelCentralInside().KeepIter() {
		inst.renderBody()
	}
}

// renderBody draws the pattern input, haystack input, and the tabbed
// results area. The Go-side highlight preview repaints every frame; the
// ClickHouse-backed tabs read whatever their lane currently holds and the
// lanes converge on the inputs at the end of the frame.
func (inst *App) renderBody() {
	for range c.Horizontal().KeepIter() {
		c.Label("Flags:").Send()
		c.Checkbox(inst.ids.PrepareStr("ci"), inst.caseInsensitive, "case-insensitive (?i)").SendRespVal(&inst.caseInsensitive)
		c.Checkbox(inst.ids.PrepareStr("ml"), inst.multiline, "multiline (?m)").SendRespVal(&inst.multiline)
		c.Checkbox(inst.ids.PrepareStr("dot"), inst.dotAll, "dot-all (?s)").SendRespVal(&inst.dotAll)
	}

	for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-pattern"), c.WidgetText().Text("Pattern (single regex — RE2 tabs)").Keep()).DefaultOpen(true).KeepIter() {
		// CodeEditor() is not cosmetic here: the Rust highlight layouter
		// resolves TextStyle::Monospace unconditionally, so without it
		// the field's font would change the moment a character is typed
		// and a job appears (ADR-0015 §SD6).
		patternEdit := c.TextEdit(inst.ids.PrepareStr("pattern"), inst.pattern, false).
			CodeEditor().
			DesiredWidth(editorWidth).
			HintText("regular expression")
		if job, ok := inst.patternHighlightJob(inst.pattern); ok {
			patternEdit = patternEdit.HighlightJob(job)
		}
		resp := patternEdit.SendRespVal(&inst.pattern)
		if resp.HasGainedFocus() || resp.HasFocus() {
			inst.lastFocusedInput = 0
		}
		inst.renderPatternCompileError(inst.pattern)
	}

	for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-patternlist"), c.WidgetText().Text("Multi patterns (one regex per line — VectorScan multiMatchAllIndices)").Keep()).DefaultOpen(true).KeepIter() {
		listEdit := c.TextEdit(inst.ids.PrepareStr("patternList"), inst.patternList, true).
			CodeEditor().
			DesiredWidth(editorWidth).
			DesiredRows(4).
			HintText("pattern 1\npattern 2\n...")
		if job, ok := inst.patternListHighlightJob(inst.patternList); ok {
			listEdit = listEdit.HighlightJob(job)
		}
		listResp := listEdit.SendRespVal(&inst.patternList)
		if listResp.HasGainedFocus() || listResp.HasFocus() {
			inst.lastFocusedInput = 2
		}
		// One parse feeds both the error summary and the per-line rows.
		lines := inst.parseAndValidatePatternList(inst.patternList)
		inst.renderPatternListCompileErrors(lines)
		inst.renderMultiInline(lines)
	}

	c.Separator().Horizontal().Send()

	c.Label("Haystack (trial text):").Send()
	haystackResp := c.TextEdit(inst.ids.PrepareStr("haystack"), inst.haystack, true).
		CodeEditor().
		DesiredWidth(editorWidth).
		DesiredRows(6).
		HintText("test string").
		SendRespVal(&inst.haystack)
	if haystackResp.HasGainedFocus() || haystackResp.HasFocus() {
		inst.lastFocusedInput = 1
	}

	c.Separator().Horizontal().Send()

	inst.renderEvalHandoff()

	c.UiSetMinHeight(260)
	for dock := range c.DockArea(inst.ids.PrepareStr("tabs")) {
		for range dock.Tab(1, "Preview (Go)") {
			inst.renderPreviewTab()
		}
		for range dock.Tab(2, "List") {
			inst.renderListTab()
		}
		for range dock.Tab(3, "Replace") {
			inst.renderReplaceTab()
		}
	}

	// Converge the lanes on whatever is in the editors now. Runs every
	// frame rather than on a change edge: an edit that lands while a
	// query is in flight is not lost, it is simply picked up by the next
	// frame that finds a lane free (see [queryLane]).
	inst.reconcileQueries()
}

// renderEvalHandoff draws the extraction hand-off row (ADR-0017 §SD6):
// one button that publishes both engines' extraction as ad-hoc datasets
// and opens a play window joined over them.
//
// It sits outside the tab DockArea rather than inside either tab —
// deliberate neutral ground, since the Go half comes from Preview and the
// ClickHouse half from List, and placing it in one would imply that tab
// owns the hand-off.
//
// Above the DockArea, not below it: the DockArea takes the rest of the
// body's height, so a row emitted after it is pushed off the bottom of
// the window and the button cannot be clicked (seen in the
// regex-explorer-highlighting demo capture).
//
// The snapshot is taken here, on the render thread; the goroutine gets
// plain data and never touches c.* or a lane. A re-click while a
// hand-off is in flight is dropped.
func (inst *App) renderEvalHandoff() {
	// Keyed on the inputs on screen: an outcome describing a pattern the
	// user has since edited is dropped, not shown. The lanes beside it
	// say "(stale)" for the same reason.
	busy, status, evalErr := inst.evalStatusView(inst.singleKey())

	for range c.Horizontal().KeepIter() {
		label := "Query this extraction in the playground"
		if busy {
			label = "Publishing…"
		}
		if c.Button(inst.ids.PrepareStr("evalplay"), c.Atoms().Text(label).Keep()).
			SendResp().HasPrimaryClicked() && !busy {
			inst.startEvalHandoff()
		}
		switch {
		case busy:
			c.Spinner().Size(14).Send()
		case evalErr != "":
			c.Label("hand-off failed: " + evalErr).Send()
		case status != "":
			c.Label(status).Send()
		}
	}
}

// startEvalHandoff snapshots both result sets and dispatches the worker.
// Render-thread only. A snapshot failure (no pattern, no haystack, a
// pattern that does not compile) is reported in place rather than
// dispatched — there is nothing to publish.
func (inst *App) startEvalHandoff() {
	snap, err := inst.snapshotEval()
	if err != nil {
		// snapshotEval failed before it could build a key, so stamp the
		// current one — the message describes what is on screen now.
		// Built outside the lock: it reads render-thread input state.
		key := inst.singleKey()
		inst.mu.Lock()
		inst.evalKey = key
		inst.evalErr = err.Error()
		inst.evalStatus = ""
		inst.mu.Unlock()
		return
	}
	inst.mu.Lock()
	if inst.evalBusy {
		inst.mu.Unlock()
		return
	}
	inst.evalBusy = true
	inst.evalErr = ""
	inst.evalStatus = ""
	inst.mu.Unlock()
	go inst.requestEvalInPlay(snap)
}

// singleKey is the query fingerprint for the two lanes driven purely by
// the single pattern and the haystack. Render-side mirror of the key
// [App.reconcileSingle] builds, so a result surface can ask its lane
// whether what it holds describes what is on screen.
func (inst *App) singleKey() (key queryKey) {
	key = makeQueryKey(inst.effectivePattern(inst.pattern), inst.haystack)
	return
}

// replaceKey extends [App.singleKey] with the replacement text.
func (inst *App) replaceKey() (key queryKey) {
	key = makeQueryKey(inst.effectivePattern(inst.pattern), inst.haystack, inst.replacement)
	return
}

// multiKey is the query fingerprint for the VectorScan lane.
func (inst *App) multiKey() (key queryKey) {
	key = makeQueryKey(inst.patternList, inst.haystack, inst.flagPrefix())
	return
}

// renderPreviewTab draws the Go-side highlight preview and, when the
// pattern captures, the per-match group breakdown. No ClickHouse
// interaction: offsets are recomputed per frame from the cached compiled
// pattern, so the preview is always in sync with the current input — which
// is exactly why it is the tab that can afford to repaint on every
// keystroke.
func (inst *App) renderPreviewTab() {
	c.Label("Preview (Go RE2, byte offsets computed locally):").Send()
	inst.renderHighlightedHaystack(inst.pattern, inst.haystack)
	inst.renderCaptureGroups(inst.pattern, inst.haystack)
}

// renderMultiInline draws the per-line result rows for the Multi patterns
// input, right below the patternList TextEdit and its compile-error label.
// Each non-empty line of the current input gets:
//
//	<line-number> <marker>  |  <pattern text>
//
// where marker is one of:
//
//	✓  pattern hit the haystack (ClickHouse multiMatchAllIndices result)
//	·  pattern did not hit
//	⚠  pattern does not compile under Go regexp (skipped on CH dispatch)
//	…  pending — waiting on ClickHouse for the current input
//
// lines is the caller's live parse, so ⚠ markers appear as soon as the
// user types an invalid line. Hit state comes from the lane, and only when
// the lane's result describes the current input — otherwise the row shows
// … rather than presenting an older answer as this one.
func (inst *App) renderMultiInline(lines []multiLine) {
	if len(lines) == 0 {
		return
	}

	view := inst.multiLane.view(inst.multiKey())
	if view.Fresh {
		lines = view.Value
	}
	validCount := countValidMultiLines(lines)

	for range c.Horizontal().KeepIter() {
		switch {
		case view.Running:
			c.Spinner().Size(14).Send()
			c.Label(fmt.Sprintf("multiMatchAllIndices over %d valid line(s)…", validCount)).Send()
		case view.Err != nil:
			c.Label(fmt.Sprintf("CH error: %v", view.Err)).Send()
		case !view.Fresh:
			c.Label(fmt.Sprintf("pending… %d valid / %d total line(s)", validCount, len(lines))).Send()
		case validCount == 0:
			c.Label(fmt.Sprintf("%d line(s), all invalid (see errors above)", len(lines))).Send()
		default:
			hits := 0
			for _, l := range lines {
				if l.Hit {
					hits++
				}
			}
			c.Label(fmt.Sprintf("hits: %d / %d valid (%d total)  elapsed: %s",
				hits, validCount, len(lines), view.Elapsed)).Send()
		}
	}

	for i, line := range lines {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
			for range c.Horizontal().KeepIter() {
				mark := "·"
				switch {
				case line.Invalid:
					mark = "⚠"
				case !view.Fresh:
					mark = "…"
				case line.Hit:
					mark = "✓"
				}
				c.Label(fmt.Sprintf("%d %s", i+1, mark)).Send()
				c.Separator().Vertical().Send()
				c.Label(line.Text).Send()
			}
		}
	}
}

// insertToken appends tok to the last-focused text input. True
// cursor-position insertion is not exposed through the current FFFI2
// binding; appending is the closest accurate approximation for the
// cheatsheet's intended left-to-right pattern construction flow.
//
// No dispatch here: the lanes pick the edit up when renderBody
// reconciles at the end of this frame.
func (inst *App) insertToken(tok string) {
	switch inst.lastFocusedInput {
	case 1:
		inst.haystack += tok
	case 2:
		inst.patternList += tok
	case 3:
		inst.replacement += tok
	default:
		inst.pattern += tok
	}
}

// applyShowcase sets both the pattern and haystack inputs to showcase
// content, overriding whatever is currently in those fields. Used by the
// left-panel showcase buttons; like [App.insertToken], it only writes
// state — reconciliation does the rest.
func (inst *App) applyShowcase(pattern string, haystack string) {
	inst.pattern = pattern
	inst.haystack = haystack
}

// renderReplaceTab draws the replacement TextEdit and the
// replaceRegexpAll result.
func (inst *App) renderReplaceTab() {
	for range c.Horizontal().KeepIter() {
		c.Label("Replacement:").Send()
		resp := c.TextEdit(inst.ids.PrepareStr("replacement"), inst.replacement, false).
			DesiredWidth(editorWidth).
			HintText("replacement pattern (use \\1, \\2, ... for capture groups)").
			SendRespVal(&inst.replacement)
		if resp.HasGainedFocus() || resp.HasFocus() {
			inst.lastFocusedInput = 3
		}
	}

	if inst.renderPatternNotReady() {
		return
	}

	view := inst.replaceLane.view(inst.replaceKey())

	for range c.Horizontal().KeepIter() {
		switch {
		case view.Running:
			c.Spinner().Size(14).Send()
			c.Label("Querying ClickHouse replaceRegexpAll...").Send()
		case view.Err != nil:
			c.Label(fmt.Sprintf("CH error: %v", view.Err)).Send()
		case !view.Has:
			c.Label("Result: (enter a haystack)").Send()
		case !view.Fresh:
			c.Label("Result (stale — refreshing):").Send()
		default:
			c.Label(fmt.Sprintf("Result:  elapsed: %s", view.Elapsed)).Send()
		}
	}

	if view.Has && view.Err == nil {
		for range c.ScrollArea().Vscroll(true).KeepIter() {
			c.Label(view.Value).Send()
		}
	}
}

// renderListTab draws the ClickHouse extractAll result — one row per
// element. Rendered as a ScrollArea with sequential labels; match counts
// are expected to stay small during interactive use.
//
// When the pattern captures, extractAll returns capture group 1 rather
// than the full match, and the tab says so and shows the full
// extractAllGroups breakdown alongside. Silently listing group values
// under a "matches" heading would contradict the Preview tab, which
// highlights full matches.
func (inst *App) renderListTab() {
	if inst.renderPatternNotReady() {
		return
	}

	view := inst.listLane.view(inst.singleKey())
	out := view.Value

	for range c.Horizontal().KeepIter() {
		switch {
		case view.Running:
			c.Spinner().Size(14).Send()
			c.Label("Querying ClickHouse extractAll...").Send()
		case view.Err != nil:
			c.Label(fmt.Sprintf("CH error: %v", view.Err)).Send()
		case !view.Has:
			c.Label("ClickHouse extractAll: (enter a haystack)").Send()
		case !view.Fresh:
			c.Label("ClickHouse extractAll: (stale — refreshing)").Send()
		default:
			c.Label(fmt.Sprintf("ClickHouse extractAll: %d element(s)  elapsed: %s", len(out.Matches), view.Elapsed)).Send()
		}
	}

	if !view.Has || view.Err != nil {
		return
	}

	if out.YieldsGroups {
		c.Label("note: the pattern captures, so extractAll returns capture group 1 — not the full match. Full matches are highlighted in the Preview tab.").Send()
	}

	for range c.ScrollArea().Vscroll(true).KeepIter() {
		for i, m := range out.Matches {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
				for range c.Horizontal().KeepIter() {
					c.Label(fmt.Sprintf("%d:", i)).Send()
					c.Label(m).Send()
					if i < len(out.Groups) {
						c.Separator().Vertical().Send()
						c.Label("groups: " + strings.Join(out.Groups[i], " | ")).Send()
					}
				}
			}
		}
	}
}

// renderStatusBar draws the bottom status bar: Go-side match count, SD1
// tripwire state, the ClickHouse match boolean, and the wall-clock elapsed
// for the query that produced it.
func (inst *App) renderStatusBar() {
	for range c.Horizontal().KeepIter() {
		localCount, localErr := inst.countMatches(inst.pattern, inst.haystack)
		switch {
		case localErr != nil:
			c.Label(fmt.Sprintf("Go: compile error — %v", localErr)).Send()
		default:
			c.Label(fmt.Sprintf("Go: %d match(es)", localCount)).Send()
		}
		c.Separator().Vertical().Send()

		tw := inst.tripwireSnapshot()
		switch {
		case !tw.Done:
			c.Label("SD1: running...").Send()
		case tw.Err != nil:
			c.Label(fmt.Sprintf("SD1: blocked (%v)", tw.Err)).Send()
		case len(tw.Drifts) > 0:
			c.Label(fmt.Sprintf("SD1: DRIFT (%d case(s))", len(tw.Drifts))).Send()
		case len(tw.Known) > 0:
			// Green, with the ledger count alongside: the engines agree
			// everywhere we expect them to, and differ only where we
			// already know they do.
			c.Label(fmt.Sprintf("SD1: ✓ (%d known)", len(tw.Known))).Send()
		default:
			c.Label("SD1: ✓").Send()
		}
		c.Separator().Vertical().Send()

		view := inst.matchLane.view(inst.singleKey())
		switch {
		case inst.patternState() == patternEmpty:
			c.Label("CH: (no pattern)").Send()
		case inst.patternState() == patternInvalid:
			c.Label("CH: (pattern invalid)").Send()
		case view.Err != nil:
			c.Label(fmt.Sprintf("CH: error — %v", view.Err)).Send()
		case !view.Has:
			c.Label("CH: —").Send()
		default:
			label := "CH: match=false"
			if view.Value {
				label = "CH: match=true"
			}
			if !view.Fresh {
				// The lane is holding an older answer while a newer query
				// runs. Saying so beats presenting it as current, which is
				// the failure this whole lane arrangement exists to stop.
				label += " (stale)"
			}
			c.Label(label).Send()
			c.Separator().Vertical().Send()
			c.Label(fmt.Sprintf("elapsed: %s", view.Elapsed)).Send()
		}
	}
}
