//go:build llm_generated_opus47

package regex_explorer

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// fullWidth tells egui TextEdit to fill the available horizontal space in
// its parent layout, rather than using egui's ~280-pixel default width.
// Matches the float32::INFINITY convention used elsewhere in this repo
// (see play_renderer.go's sqlEditor).
var fullWidth = float32(math.Inf(1))

// app is the *active* App state pointer the renderer reads from on
// every frame. The factory ctor allocates a fresh App per Open() and
// the AppInstance.Frame method swaps this pointer to its per-window
// state for the duration of the frame, then restores it. The swap is
// safe because the Go render loop is single-threaded.
//
// The initial value gives tests (which exercise the renderer outside
// the AppInstance wrapper) a non-nil pointer. Production always swaps
// in fresh per-window state before any access. Every widget id the
// renderer derives goes through `app.ids` so cross-app collisions are
// avoided by the per-instance stack the windowhost pre-pushes a
// window-unique salt onto via c.IdScope (see windowhost.renderWindowBody).
var app = newApp()

// resultState carries the result of the most recent [App.RunMatch] query.
// Valid is false before the first successful query.
type resultState struct {
	Valid bool
	Value bool
}

// App holds per-session state for the regex explorer: current pattern and
// haystack bound to the UI text-edit widgets, last result of each kind of
// ClickHouse query, the path to the `clickhouse local` binary used to run
// queries, and the compiled-regexp cache used by the Go-side offset-highlight
// painter.
//
// Each query kind (match, extractAll, replaceRegexpAll, multiMatchAllIndices)
// has its own atomic-bool coalescer so all four can be in flight concurrently
// as independent `clickhouse local` subprocesses; results are stored under mu.
type App struct {
	mu       sync.RWMutex
	pattern  string
	haystack string

	lastMatchResult resultState
	lastMatchStats  clStats
	lastMatchErr    error
	matchRunning    atomic.Bool

	listMatches []string
	listStats   clStats
	listErr     error
	listRunning atomic.Bool

	replacement    string
	replaceResult  string
	replaceValid   bool
	replaceStats   clStats
	replaceErr     error
	replaceRunning atomic.Bool

	patternList   string
	multiSnapshot multiSnapshot
	multiStats    clStats
	multiErr      error
	multiRunning  atomic.Bool

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

	alloc memory.Allocator

	// bus is the per-instance BusI captured at Mount. All SQL goes
	// through ch.local.exec.regex_explorer via the broker; the
	// subprocess-shell-out path has been retired.
	bus runtimeapp.BusI

	// ids is the per-instance WidgetIdStack the host pre-prepares
	// with a window-unique salt every frame. Captured from
	// MountCtx.Ids() at Mount time; AppInstance.Frame swaps the
	// package-level [app] pointer to this *App, so every renderer
	// reaches the stack through app.ids and inherits the host's
	// salt. Cross-app id collisions cannot happen even when two
	// apps use the same label string. Tour mode and tests fall
	// back to the default stack populated by newApp().
	ids *c.WidgetIdStack

	compileCacheMu sync.Mutex
	compileCache   map[string]compileResult
}

// newApp builds the package-level [App]. clickhouse-local is reached
// via the chlocalbroker subject `ch.local.exec.regex_explorer`; no
// binary path or env var is consulted here. The fresh WidgetIdStack
// the App carries is the fallback used by tour mode and tests;
// AppInstance.Mount overrides it with the host-supplied per-instance
// stack so interactive multi-window renders don't collide.
func newApp() (inst *App) {
	inst = &App{
		ids:   c.NewWidgetIdStack(),
		alloc: memory.NewGoAllocator(),
	}
	return
}

// AppInstance is the per-window regex_explorer AppI value. Each
// dock-host Open() yields a fresh AppInstance with its own *App
// state (pattern, haystack, replacement, query results, mode flags,
// …). Frame() swaps the package-level [app] pointer to inst.state
// for the duration of the render call so the existing renderer code
// (which still reads `app.X`) sees per-window state.
//
// We use this swap rather than threading *App through every renderer
// to keep the refactor diff small (regex_explorer has 70+ `app.X`
// references). The swap is single-render-goroutine-safe: defer
// restores the previous pointer when Frame returns.
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
		inst.state.bus = ctx.Bus()
		inst.state.ids = ctx.Ids()
	}
	return
}

func (inst *AppInstance) Unmount(ctx runtimeapp.MountContextI) (err error) { return }

// Frame swaps the package-level `app` pointer to this instance's
// state for the duration of the render call, then restores it on
// return. The host has already pre-pushed a window-unique salt onto
// inst.state.ids via c.IdScope (windowhost.renderWindowBody), so
// every widget id the renderer derives from `app.ids` is scoped under
// that salt and cannot collide with another open app's ids.
//
// Kicks off the SD1 engine-fidelity tripwire on the first call
// (coalesced by [App.tripwireRan] on the per-instance state).
func (inst *AppInstance) Frame(ctx runtimeapp.FrameContextI) (err error) {
	prev := app
	app = inst.state
	defer func() { app = prev }()

	inst.state.RunTripwire(context.Background())
	RenderWindow()
	return
}

// Screenshot capture is enrolled via registry.Register in
// regex_explorer_tour.go (ADR-0057). Tour/gallery rendering is
// single-instance per scene, so each Demo reads/writes the package-level
// `app` directly (pinning its pattern/haystack) and draws through
// RenderWindow below; the central widgets TestDriver captures the result.

// RenderWindow draws the regex-explorer body into the caller's UI scope:
// left cheatsheet panel, central body with pattern / haystack inputs and
// tabbed results, and a bottom status bar. Per ADR-0026 Amendment
// 2026-05-12, the host wraps this in a runtime-created c.Window using
// Manifest.WindowTitle/Icon; the body uses only *Inside panel variants.
// PanelCentralInside is retained so the body has an owned layout scope —
// without it, the inputs flicker and steal width unpredictably from the
// left panel.
func RenderWindow() {
	for range c.PanelBottomInside(app.ids.PrepareStr("btm")).DefaultSize(24).Resizable(false).KeepIter() {
		renderStatusBar()
	}

	for range c.PanelLeftInside(app.ids.PrepareStr("cheat")).DefaultSize(280).Resizable(true).KeepIter() {
		renderCheatsheet()
	}

	for range c.PanelCentralInside().KeepIter() {
		renderBody()
	}
}

// renderBody draws the pattern input, haystack input, and the tabbed
// results area. Input changes auto-dispatch per-tab ClickHouse queries
// (each coalesced via its own atomic.Bool); the Go-side highlight preview
// in the Test tab repaints immediately every frame.
func renderBody() {
	changed := false

	for range c.Horizontal().KeepIter() {
		c.Label("Flags:").Send()
		if c.Checkbox(app.ids.PrepareStr("ci"), app.caseInsensitive, "case-insensitive (?i)").SendRespVal(&app.caseInsensitive).HasChanged() {
			changed = true
		}
		if c.Checkbox(app.ids.PrepareStr("ml"), app.multiline, "multiline (?m)").SendRespVal(&app.multiline).HasChanged() {
			changed = true
		}
		if c.Checkbox(app.ids.PrepareStr("dot"), app.dotAll, "dot-all (?s)").SendRespVal(&app.dotAll).HasChanged() {
			changed = true
		}
	}

	for range c.CollapsingHeader(app.ids.PrepareStr("hdr-pattern"), c.WidgetText().Text("Pattern (single regex — RE2 tabs)").Keep()).DefaultOpen(true).KeepIter() {
		resp := c.TextEdit(app.ids.PrepareStr("pattern"), app.pattern, false).
			DesiredWidth(fullWidth).
			HintText("regular expression").
			SendRespVal(&app.pattern)
		if resp.HasChanged() {
			changed = true
		}
		if resp.HasGainedFocus() || resp.HasFocus() {
			app.lastFocusedInput = 0
		}
		renderPatternCompileError(app.pattern)
	}

	for range c.CollapsingHeader(app.ids.PrepareStr("hdr-patternlist"), c.WidgetText().Text("Multi patterns (one regex per line — VectorScan multiMatchAllIndices)").Keep()).DefaultOpen(true).KeepIter() {
		listResp := c.TextEdit(app.ids.PrepareStr("patternList"), app.patternList, true).
			CodeEditor().
			DesiredWidth(fullWidth).
			DesiredRows(4).
			HintText("pattern 1\npattern 2\n...").
			SendRespVal(&app.patternList)
		if listResp.HasChanged() {
			changed = true
		}
		if listResp.HasGainedFocus() || listResp.HasFocus() {
			app.lastFocusedInput = 2
		}
		renderPatternListCompileErrors(app.patternList)
		renderMultiInline()
	}

	c.Separator().Horizontal().Send()

	c.Label("Haystack (trial text):").Send()
	haystackResp := c.TextEdit(app.ids.PrepareStr("haystack"), app.haystack, true).
		CodeEditor().
		DesiredWidth(fullWidth).
		DesiredRows(6).
		HintText("test string").
		SendRespVal(&app.haystack)
	if haystackResp.HasChanged() {
		changed = true
	}
	if haystackResp.HasGainedFocus() || haystackResp.HasFocus() {
		app.lastFocusedInput = 1
	}

	c.Separator().Horizontal().Send()

	c.UiSetMinHeight(260)
	for dock := range c.DockArea(app.ids.PrepareStr("tabs")) {
		for range dock.Tab(1, "Test") {
			renderTestTab()
		}
		for range dock.Tab(2, "List") {
			renderListTab()
		}
		for range dock.Tab(3, "Replace") {
			if renderReplaceTab() {
				changed = true
			}
		}
	}

	if changed && app.haystack != "" && isPatternValid() {
		ctx := context.Background()
		if !app.matchRunning.Load() {
			app.RunMatch(ctx)
		}
		if !app.listRunning.Load() {
			app.RunExtractAll(ctx)
		}
		if !app.replaceRunning.Load() {
			app.RunReplaceAll(ctx)
		}
	}
	if changed && app.haystack != "" && app.patternList != "" && !app.multiRunning.Load() {
		app.RunMultiMatch(context.Background(), app.patternList)
	}
}

// effectivePattern prepends an RE2 inline-flag group (e.g. "(?ims)") to the
// user-entered pattern based on the current flag-toggle state. Empty
// patterns are returned unchanged. The flag group is understood by both Go
// regexp and ClickHouse RE2, so the Go-side preview and the ClickHouse
// queries see equivalent patterns.
func effectivePattern(base string) (out string) {
	if base == "" {
		out = base
		return
	}
	var flags strings.Builder
	if app.caseInsensitive {
		flags.WriteByte('i')
	}
	if app.multiline {
		flags.WriteByte('m')
	}
	if app.dotAll {
		flags.WriteByte('s')
	}
	if flags.Len() == 0 {
		out = base
		return
	}
	out = "(?" + flags.String() + ")" + base
	return
}

// renderTestTab draws the Go-side highlight preview. No ClickHouse
// interaction; offsets are recomputed per frame from the cached compiled
// pattern, so the preview is always in sync with the current input.
func renderTestTab() {
	c.Label("Preview (Go RE2, byte offsets computed locally):").Send()
	renderHighlightedHaystack(app.pattern, app.haystack)
}

// renderMultiInline draws the per-line result rows for the Multi
// patterns input, right below the patternList TextEdit and its
// compile-error label. Each non-empty line of the current input gets:
//
//	<line-number> <marker>  |  <pattern text>
//
// where marker is one of:
//
//	✓  pattern hit the haystack (ClickHouse multiMatchAllIndices result)
//	·  pattern did not hit
//	⚠  pattern does not compile under Go regexp (skipped on CH dispatch)
//	…  pending — user just edited; waiting on ClickHouse
//
// Parses the current patternList live to show ⚠ markers as soon as the
// user types an invalid line; overlays Hit state from the last
// [multiSnapshot] only when its captured text matches the current
// input (otherwise the hits are stale and we fall back to "pending").
func renderMultiInline() {
	lines := parseAndValidatePatternList(app.patternList)
	if len(lines) == 0 {
		return
	}

	app.mu.RLock()
	snapshot := app.multiSnapshot
	multiErr := app.multiErr
	running := app.multiRunning.Load()
	multiStats := app.multiStats
	app.mu.RUnlock()

	overlay := snapshot.patternListText == app.patternList
	if overlay {
		lines = snapshot.lines
	}

	validCount := countValidMultiLines(lines)

	for range c.Horizontal().KeepIter() {
		switch {
		case running:
			c.Spinner().Size(14).Send()
			c.Label(fmt.Sprintf("multiMatchAllIndices over %d valid line(s)…", validCount)).Send()
		case multiErr != nil && overlay:
			c.Label(fmt.Sprintf("CH error: %v", multiErr)).Send()
		case !overlay:
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
				hits, validCount, len(lines), time.Duration(multiStats.ElapsedNs))).Send()
		}
	}

	for i, line := range lines {
		for range c.IdScope(app.ids.PrepareSeq(uint64(i))) {
			for range c.Horizontal().KeepIter() {
				mark := "·"
				switch {
				case line.Invalid:
					mark = "⚠"
				case !overlay:
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
func insertToken(tok string) {
	app.mu.Lock()
	switch app.lastFocusedInput {
	case 1:
		app.haystack += tok
	case 2:
		app.patternList += tok
	case 3:
		app.replacement += tok
	default:
		app.pattern += tok
	}
	app.mu.Unlock()
	dispatchQueriesFromShowcase()
}

// applyShowcase sets both the pattern and haystack inputs to showcase
// content, overriding whatever is currently in those fields, and triggers
// the per-tab query cascade. Used by the left-panel showcase buttons.
func applyShowcase(pattern string, haystack string) {
	app.mu.Lock()
	app.pattern = pattern
	app.haystack = haystack
	app.mu.Unlock()
	dispatchQueriesFromShowcase()
}

// dispatchQueriesFromShowcase mirrors the auto-dispatch block in
// renderBody for the case where App.pattern / App.haystack were mutated
// outside a TextEdit HasChanged event (cheatsheet click, showcase click).
// Like renderBody, skips ClickHouse dispatch when the pattern does not
// compile under Go regexp — the compile error is already shown next to
// the input, and spawning `clickhouse local` just to duplicate the
// error message is wasteful.
func dispatchQueriesFromShowcase() {
	ctx := context.Background()
	if app.haystack != "" && isPatternValid() {
		if !app.matchRunning.Load() {
			app.RunMatch(ctx)
		}
		if !app.listRunning.Load() {
			app.RunExtractAll(ctx)
		}
		if !app.replaceRunning.Load() {
			app.RunReplaceAll(ctx)
		}
	}
	if app.haystack != "" && app.patternList != "" && !app.multiRunning.Load() {
		app.RunMultiMatch(ctx, app.patternList)
	}
}

// renderReplaceTab draws the replacement TextEdit and the
// replaceRegexpAll result. Returns true when the replacement input
// changed, so the caller can include the change in the auto-dispatch
// trigger.
func renderReplaceTab() (changed bool) {
	for range c.Horizontal().KeepIter() {
		c.Label("Replacement:").Send()
		resp := c.TextEdit(app.ids.PrepareStr("replacement"), app.replacement, false).
			DesiredWidth(fullWidth).
			HintText("replacement pattern (use \\1, \\2, ... for capture groups)").
			SendRespVal(&app.replacement)
		if resp.HasChanged() {
			changed = true
		}
		if resp.HasGainedFocus() || resp.HasFocus() {
			app.lastFocusedInput = 3
		}
	}

	if !isPatternValid() {
		c.Label("(pattern invalid — see Pattern input)").Send()
		return
	}

	app.mu.RLock()
	result := app.replaceResult
	valid := app.replaceValid
	replaceErr := app.replaceErr
	running := app.replaceRunning.Load()
	app.mu.RUnlock()

	for range c.Horizontal().KeepIter() {
		switch {
		case running:
			c.Spinner().Size(14).Send()
			c.Label("Querying ClickHouse replaceRegexpAll...").Send()
		case replaceErr != nil:
			c.Label(fmt.Sprintf("CH error: %v", replaceErr)).Send()
		case !valid:
			c.Label("Result: (enter replacement and haystack)").Send()
		default:
			c.Label("Result:").Send()
		}
	}

	if valid && replaceErr == nil {
		for range c.ScrollArea().Vscroll(true).KeepIter() {
			c.Label(result).Send()
		}
	}
	return
}

// renderListTab draws the ClickHouse extractAll result — one row per match
// text. Rendered as a ScrollArea with sequential labels; match counts
// expected to stay small during interactive use. If the pattern is
// invalid Go-side, the list is suppressed and the header explains why —
// the red error next to the input is already the authoritative signal.
func renderListTab() {
	if !isPatternValid() {
		c.Label("(pattern invalid — see Pattern input)").Send()
		return
	}

	app.mu.RLock()
	matches := app.listMatches
	listErr := app.listErr
	running := app.listRunning.Load()
	app.mu.RUnlock()

	for range c.Horizontal().KeepIter() {
		if running {
			c.Spinner().Size(14).Send()
			c.Label("Querying ClickHouse extractAll...").Send()
		} else if listErr != nil {
			c.Label(fmt.Sprintf("CH error: %v", listErr)).Send()
		} else {
			c.Label(fmt.Sprintf("ClickHouse extractAll: %d match(es)", len(matches))).Send()
		}
	}

	if len(matches) == 0 {
		return
	}
	for range c.ScrollArea().Vscroll(true).KeepIter() {
		for i, m := range matches {
			for range c.IdScope(app.ids.PrepareSeq(uint64(i))) {
				for range c.Horizontal().KeepIter() {
					c.Label(fmt.Sprintf("%d:", i)).Send()
					c.Label(m).Send()
				}
			}
		}
	}
}

// renderStatusBar draws the bottom status bar: Go-side match count, SD1
// tripwire state, CH match boolean, wall-clock elapsed for the last
// `clickhouse local` subprocess, and — if the last query failed — the
// error.
func renderStatusBar() {
	app.mu.RLock()
	result := app.lastMatchResult
	stats := app.lastMatchStats
	matchErr := app.lastMatchErr
	app.mu.RUnlock()

	for range c.Horizontal().KeepIter() {
		localCount, localErr := countMatches(app.pattern, app.haystack)
		switch {
		case localErr != nil:
			c.Label(fmt.Sprintf("Go: compile error — %v", localErr)).Send()
		case localCount < 0:
			c.Label("Go: —").Send()
		default:
			c.Label(fmt.Sprintf("Go: %d match(es)", localCount)).Send()
		}
		c.Separator().Vertical().Send()

		tw := app.tripwireSnapshot()
		switch {
		case !tw.Done:
			c.Label("SD1: running...").Send()
		case tw.Err != nil:
			c.Label(fmt.Sprintf("SD1: blocked (%v)", tw.Err)).Send()
		case len(tw.Drifts) > 0:
			c.Label(fmt.Sprintf("SD1: DRIFT (%d case(s))", len(tw.Drifts))).Send()
		default:
			c.Label("SD1: ✓").Send()
		}
		c.Separator().Vertical().Send()

		switch {
		case !isPatternValid():
			c.Label("CH: (pattern invalid)").Send()
		case matchErr != nil:
			c.Label(fmt.Sprintf("CH: error — %v", matchErr)).Send()
		case !result.Valid:
			c.Label("CH: —").Send()
		default:
			label := "CH: match=false"
			if result.Value {
				label = "CH: match=true"
			}
			c.Label(label).Send()
			c.Separator().Vertical().Send()
			c.Label(fmt.Sprintf("elapsed: %s", time.Duration(stats.ElapsedNs))).Send()
		}
	}
}
