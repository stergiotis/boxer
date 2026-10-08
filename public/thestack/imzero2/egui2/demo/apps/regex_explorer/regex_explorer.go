package regex_explorer

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/regexedit"
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

// Per-match render caps. Three surfaces here fan out one unit of UI per
// match, so their cost tracks the match count rather than the size of the
// input: a pattern like `.` over a few KB of haystack yields thousands of
// matches from an input that is itself a few KB. Uncapped, one such
// pattern put ~325 KB of tab body on the FFFI wire every frame and left
// the client laying out thousands of text jobs — a 30-80 ms frame.
//
// The two numbers differ because the units do. A highlight segment is one
// styled run inside a single LabelAtoms: one layout job with N sections. A
// match row is an id scope, a Horizontal and several labels — a widget
// each, an order of magnitude dearer.
//
// Both are display budgets, not analysis limits. Every surface keeps
// reporting the exact match count, and the haystack is still shown in
// full past the highlight cap (unstyled); only what is drawn is bounded.
const (
	maxHighlightedMatches = 500
	maxMatchRows          = 200
)

// App holds per-window state for the regex explorer: the current pattern
// and haystack bound to the UI text-edit widgets, the last result of each
// kind of ClickHouse query, and the compiled-regexp cache the Go-side
// highlight painter uses.
//
// The ClickHouse queries run on three [queryLane]s — the pattern
// functions, the replace functions, and multiMatchAllIndices — so they can
// be in flight concurrently as independent broker requests.
//
// Concurrency, precisely — the fields fall into three groups:
//
//   - Input and view state (pattern, haystack, replacement, patternList,
//     the flag toggles, lastFocusedInput, ids, the lanes, the analysis
//     memo) is confined to the render thread. The egui bindings write
//     several of these through pointers handed to SendRespVal, which no
//     lock could cover anyway, so the confinement is the invariant, not a
//     lock. A lane's worker touches only its bgjob.Runner, which carries
//     its own lock.
//   - The hand-off state (eval*) and bus are written off the render thread
//     or read from it by workers; mu covers those. The SD1 tripwire's
//     outcome is the process's, not an App's ([sharedTripwire]).
//   - compileCache has its own mutex (compileCacheMu) because the
//     tripwire goroutine shares it with the render thread.
type App struct {
	mu       sync.RWMutex
	pattern  string
	haystack string

	replacement string
	patternList string

	// One lane per query. Each owns its own in-flight state, last-good
	// result, input fingerprint, and error — so "is this showing the
	// current input?" is one comparison rather than a convention every
	// result surface has to remember to follow.
	fnLane      queryLane[fnOutcome]
	replaceLane queryLane[replaceOutcome]
	multiLane   queryLane[[]multiLine]

	// tab is the result tab on screen. Only its body is drawn; the lanes
	// converge whichever tab is showing.
	tab resultTabE

	caseInsensitive bool
	multiline       bool
	dotAll          bool

	// lastFocusedInput is the text input a cheatsheet token goes into, and
	// pendingInsert the tokens waiting for each input's next build — see
	// [App.insertToken].
	lastFocusedInput inputFieldE
	pendingInsert    [inputFieldCount]string

	// Extraction hand-off state (ADR-0017). Written by the worker
	// goroutine that publishes and opens, read by the render thread —
	// so it belongs to the mu group above.
	evalBusy   bool
	evalErr    string
	evalStatus string
	// evalKey fingerprints the inputs evalStatus / evalErr describe, so
	// an outcome is retired when the editors move on rather than
	// presented as current — the [queryLane] freshness rule.
	evalKey queryKey
	// goPub / chPub hold the two datasets, one handle each across
	// republishes, so one window holds at most two against the quotas.
	goPub, chPub *adhocdata.Publisher

	alloc memory.Allocator

	// bus is the per-instance BusI captured at Mount. All SQL goes
	// through ch.local.exec.regex_explorer via the broker.
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

	// analysisMemo backs [App.analysis]. Render-thread-confined, like the
	// inputs it is keyed on.
	analysisMemo patternAnalysis

	// Retained syntax-highlight jobs for the two pattern editors
	// (ADR-0015), one per box — the regexedit widget doc explains why they
	// must not be shared — rebuilt only when their buffer changes. The
	// painting lives in widgets/regexedit (ADR-0164 §SD4); validity stays
	// with getCompiledRegexp (ADR-0054), not the painter.
	// Render-thread-confined.
	patternHl     regexedit.Cache
	patternListHl regexedit.Cache
}

// inputFieldE names the text inputs a cheatsheet token can be appended
// to. The zero value is the pattern, which is where a token goes before
// any input has had focus.
type inputFieldE uint8

const (
	inputPattern inputFieldE = iota
	inputHaystack
	inputPatternList
	inputReplacement
	inputFieldCount
)

// resultTabE names the result tabs. The zero value, Matches, is the tab a
// window opens on.
type resultTabE uint8

const (
	tabMatches resultTabE = iota
	tabFunctions
	tabMulti
)

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
		goPub: adhocdata.NewWindowPublisher(goDatasetAlias),
		chPub: adhocdata.NewWindowPublisher(chDatasetAlias),
		// ClickHouse's own default — see [inlineFlags].
		dotAll: true,
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

// Unmount abandons anything still in flight: without it a closed window
// leaves up to four queries running against pooled clickhouse-local workers
// with nothing left to consume their results. The ad-hoc datasets this
// window published (ADR-0017) are not retracted here — the runtime retracts
// them when the host closes the window's bus client (ADR-0240 §SD5).
func (inst *AppInstance) Unmount(ctx runtimeapp.MountContextI) (err error) {
	if inst.state != nil {
		inst.state.cancelQueries()
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
// (once per process — see [App.RunTripwire]).
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
// left cheatsheet panel, central body with the inputs and the result tabs,
// and a bottom status bar. Per ADR-0026 Amendment 2026-05-12, the host
// wraps this in a runtime-created c.Window using Manifest.WindowTitle/Icon;
// the body uses only *Inside panel variants. PanelCentralInside is retained
// so the body has an owned layout scope — without it, the inputs flicker
// and steal width unpredictably from the left panel.
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

// renderBody draws the two inputs every tab reads — pattern and haystack,
// kept together because editing one while watching the other is the whole
// loop — then the tab row and the selected tab in a scroll area, which
// takes the rest of the height. Whatever a tab grows to, it scrolls inside
// that area rather than pushing anything off the window.
func (inst *App) renderBody() {
	for range c.Horizontal().KeepIter() {
		c.Label("Flags:").Send()
		c.Checkbox(inst.ids.PrepareStr("ci"), inst.caseInsensitive, "case-insensitive (?i)").SendRespVal(&inst.caseInsensitive)
		c.Checkbox(inst.ids.PrepareStr("ml"), inst.multiline, "multiline (?m)").SendRespVal(&inst.multiline)
		c.Checkbox(inst.ids.PrepareStr("dot"), inst.dotAll, "dot matches newline (?s)").SendRespVal(&inst.dotAll)
	}

	c.Label("Pattern").Send()
	// regexedit sets CodeEditor() and attaches the highlight job (the
	// monospace requirement is ADR-0015 §SD6, documented on
	// regexedit.Cache.Prepare).
	resp := inst.withInsert(inputPattern, inst.patternHl.TextEdit(inst.ids.PrepareStr("pattern"), inst.pattern, false, regexedit.ModeSingle)).
		DesiredWidth(editorWidth).
		HintText("regular expression").
		SendRespVal(&inst.pattern)
	if resp.HasGainedFocus() || resp.HasFocus() {
		inst.lastFocusedInput = inputPattern
	}
	inst.renderPatternCompileError()

	c.Label("Haystack").Send()
	haystackResp := inst.withInsert(inputHaystack, c.TextEdit(inst.ids.PrepareStr("haystack"), inst.haystack, true)).
		CodeEditor().
		DesiredWidth(editorWidth).
		DesiredRows(4).
		HintText("text to match against").
		SendRespVal(&inst.haystack)
	if haystackResp.HasGainedFocus() || haystackResp.HasFocus() {
		inst.lastFocusedInput = inputHaystack
	}

	c.Separator().Horizontal().Send()
	inst.renderTabRow()
	for range c.ScrollArea().Vscroll(true).KeepIter() {
		switch inst.tab {
		case tabFunctions:
			inst.renderFunctionsTab()
		case tabMulti:
			inst.renderMultiTab()
		default:
			inst.renderMatchesTab()
		}
	}

	// Converge the lanes on whatever is in the editors now. Runs every
	// frame rather than on a change edge, and whichever tab is showing: an
	// edit that lands while a query is in flight is not lost, it is simply
	// picked up by the next frame that finds a lane free (see [queryLane]).
	inst.reconcileQueries()
}

// renderTabRow draws the result-tab selector: plain selectable labels, not
// a dock. Three fixed tabs gain nothing from splitting, dragging or
// closing, and a dock's close and collapse controls read as actions that
// lose the results.
func (inst *App) renderTabRow() {
	matches := "Matches"
	if a := inst.analysis(); a.state == patternValid {
		matches = fmt.Sprintf("Matches (%d)", len(a.matches))
	}
	tabs := []struct {
		tab   resultTabE
		id    string
		title string
	}{
		{tabMatches, "tab-matches", matches},
		{tabFunctions, "tab-functions", "ClickHouse functions"},
		{tabMulti, "tab-multi", "Multi-pattern (VectorScan)"},
	}
	for range c.Horizontal().KeepIter() {
		for _, t := range tabs {
			if c.SelectableLabel(inst.ids.PrepareStr(t.id), inst.tab == t.tab, t.title).SendResp().HasPrimaryClicked() {
				inst.tab = t.tab
			}
		}
	}
}

// renderMatchesTab draws what Go's regexp finds: the highlighted haystack,
// where ClickHouse's extractAll would stop, and the capture groups. No
// ClickHouse interaction — it repaints on every keystroke.
func (inst *App) renderMatchesTab() {
	a := inst.analysis()
	switch a.state {
	case patternEmpty:
		weakLabel("Enter a pattern; its matches are highlighted here.")
		return
	case patternInvalid:
		weakLabel("The pattern does not compile — the error is under the Pattern input.")
		return
	}
	inst.renderHighlightedHaystack()
	inst.renderExtractAllStopNote()
	inst.renderCaptureGroups()
	c.Separator().Horizontal().Send()
	weakLabel("Highlighted by Go's regexp, which reads the pattern as ClickHouse's RE2 functions do (ADR-0054). The ClickHouse functions tab shows what ClickHouse itself returns.")
}

// weakLabel draws a de-emphasised line of explanatory text.
func weakLabel(text string) {
	for rt := range c.RichTextLabel(text) {
		rt.Weak()
	}
}

// renderTruncationNote states what a per-match surface left undrawn. Weak
// and terse: every caller already states the exact total, so this line
// only has to say that the drawing stopped early and by how much. Silent
// when nothing was dropped.
func renderTruncationNote(shown int, total int) {
	if shown >= total {
		return
	}
	weakLabel(fmt.Sprintf("… %d more not shown (display capped at %d)", total-shown, shown))
}

// renderExtractAllStopNote says, when it applies, that ClickHouse's
// extractAll returns fewer matches than Go highlights, and why: it stops
// at the first place the pattern matches the empty string (see
// [extractAllCount]). Silent when extractAll loses no non-empty match —
// stopping on a trailing empty match changes nothing a user can see.
func (inst *App) renderExtractAllStopNote() {
	a := inst.analysis()
	if a.extractAllStop < 0 || a.extractAllN >= len(a.matches) {
		return
	}
	weakLabel(fmt.Sprintf(
		"ClickHouse's extractAll stops at byte %d, where the pattern matches the empty string: it returns %d of these %d match(es). countMatches and extractAllGroups see all of them.",
		a.extractAllStop, a.extractAllN, len(a.matches)))
}

// Colours the tabs use for verdicts: agreement, and disagreement or error.
var (
	agreeFg = color.Hex(styletokens.SuccessDefault.AsHex()).Keep()
	warnFg  = color.Hex(styletokens.WarningDefault.AsHex()).Keep()
)

// coloredLabel draws text in fg on no background.
func coloredLabel(fg color.Color, text string) {
	for range c.RichTextLabelColored(fg, color.Transparent, text) {
	}
}

// renderLaneStatus draws the one-line state of a ClickHouse query above
// the result it feeds: a spinner while it runs, ClickHouse's own message
// when it failed (wrapped — it can be long), a wait marker when the lane
// holds nothing current, and otherwise the round-trip time and done. An
// answer the lane served without a query (zero elapsed) shows done alone.
func renderLaneStatus[T any](view laneView[T], done string) {
	switch {
	case view.Running:
		for range c.Horizontal().KeepIter() {
			c.Spinner().Size(14).Send()
			weakLabel("asking ClickHouse…")
		}
	case view.Err != nil:
		coloredLabel(warnFg, "ClickHouse: "+clickHouseMessage(view.Err))
	case !view.Fresh:
		weakLabel("waiting for ClickHouse…")
	default:
		text := done
		if view.Elapsed > 0 {
			if text != "" {
				text += " · "
			}
			text += "ClickHouse answered in " + fmtElapsed(view.Elapsed)
		}
		weakLabel(text)
	}
}

// fmtElapsed renders a round-trip time at the precision a person reads.
func fmtElapsed(d time.Duration) (s string) {
	if d < time.Millisecond {
		s = "<1 ms"
		return
	}
	s = strconv.FormatInt(d.Round(time.Millisecond).Milliseconds(), 10) + " ms"
	return
}

// renderFunctionsTab draws one row per ClickHouse regex function: what
// ClickHouse returns for the input, what the Go model predicts, and the
// expression itself to copy into a query. It is where the app answers its
// question — what will ClickHouse return? — and the playground hand-off,
// which compares the two engines in SQL, sits at its foot.
func (inst *App) renderFunctionsTab() {
	if inst.renderPatternNotReady() {
		return
	}
	a := inst.analysis()
	fnView := inst.fnLane.view(inst.singleKey())
	repView := inst.replaceLane.view(inst.replaceKey())
	predicted := predictFunctions(a)

	renderLaneStatus(fnView, "")
	for range c.Grid(inst.ids.PrepareStr("fns")).NumColumns(4).Striped(true).KeepIter() {
		for _, h := range []string{"function", "Go predicts", "", "ClickHouse returns"} {
			for rt := range c.RichTextLabel(h) {
				rt.Strong()
			}
		}
		c.EndRow()
		for _, fn := range patternFns {
			if fn == fnExtractAllGroups && !predicted.YieldsGroups {
				continue // ClickHouse rejects it for a pattern without a group
			}
			chText, chHas := "", fnView.Fresh && fnView.Err == nil
			if chHas {
				chText = fnValue(fn, fnView.Value)
			}
			modelled := fn != fnExtractAllGroups || a.groupsModelled()
			inst.renderFnRow(fn, a.pattern, chText, chHas, fnValue(fn, predicted), modelled)
		}
		for range c.IdScope(inst.ids.PrepareStr("replace-row")) {
			c.Label("replacement").Send()
			c.Label("").Send()
			c.Label("").Send()
			resp := inst.withInsert(inputReplacement, c.TextEdit(inst.ids.PrepareStr("replacement"), inst.replacement, false)).
				CodeEditor().
				DesiredWidth(320).
				HintText(`\1, \2 … for groups, \0 for the match`).
				SendRespVal(&inst.replacement)
			if resp.HasGainedFocus() || resp.HasFocus() {
				inst.lastFocusedInput = inputReplacement
			}
			c.EndRow()
		}
		for _, fn := range replaceFns {
			chText, chHas := "", repView.Fresh && repView.Err == nil
			switch {
			case chHas:
				chText = fnReplaceValue(fn, repView.Value)
			case isEngineRejection(repView.Err):
				// ClickHouse refused the replacement (`\9` with one
				// group): the row is where to say so. A transport
				// failure is the pattern lane's too, and the line above
				// the table already carries it.
				chText = clickHouseMessage(repView.Err)
			}
			inst.renderFnRow(fn, a.pattern, chText, chHas, "", false)
		}
	}
	if predicted.YieldsGroups {
		weakLabel("The pattern captures, so extract and extractAll return capture group 1, not the whole match; regexpExtract with index 0 and extractAllGroups show the rest.")
	}
	inst.renderExtractAllStopNote()
	if predicted.YieldsGroups && !a.groupsModelled() {
		weakLabel("extractAllGroups is not predicted here: the pattern matches the empty string, and ClickHouse walks empty matches differently from Go (byte by byte, and including the one right after a match).")
	}
	weakLabel("The replace functions are not modelled: replaceRegexpAll also replaces the empty match right after a match, which Go's regexp skips.")

	c.Separator().Horizontal().Send()
	inst.renderEvalHandoff()
}

// fnValue renders one pattern function's value the way ClickHouse prints
// it: numbers bare, strings and array elements as quoted literals — so an
// empty string reads as ” rather than as nothing. Equal values render
// equal, which is what the Functions tab compares.
func fnValue(fn chFnE, o fnOutcome) (text string) {
	switch fn {
	case fnMatch:
		text = "0"
		if o.Match {
			text = "1"
		}
	case fnCountMatches:
		text = strconv.FormatUint(o.Count, 10)
	case fnExtract:
		text = marshalling.EscapeString(o.Extract)
	case fnRegexpExtract:
		text = marshalling.EscapeString(o.RegexpExtract)
	case fnExtractAll:
		text = fmtStrings(o.ExtractAll)
	case fnExtractAllGroups:
		parts := make([]string, 0, len(o.Groups))
		for _, g := range o.Groups {
			parts = append(parts, fmtStrings(g))
		}
		text = "[" + strings.Join(parts, ",") + "]"
	}
	return
}

// fnReplaceValue renders a replace function's value as a quoted literal.
func fnReplaceValue(fn chFnE, o replaceOutcome) (text string) {
	if fn == fnReplaceRegexpOne {
		text = marshalling.EscapeString(o.One)
		return
	}
	text = marshalling.EscapeString(o.All)
	return
}

// fmtStrings renders an Array(String) as ClickHouse prints it.
func fmtStrings(ss []string) (text string) {
	parts := make([]string, 0, len(ss))
	for _, s := range ss {
		parts = append(parts, marshalling.EscapeString(s))
	}
	text = "[" + strings.Join(parts, ",") + "]"
	return
}

// maxCellRunes caps a ClickHouse value drawn in a Functions row, and
// maxVerdictRunes the Go value shown beside a disagreement; the whole
// value is in the cell's tooltip. A table row that wraps a long haystack
// is no longer a table.
const (
	maxCellRunes    = 120
	maxVerdictRunes = 40
)

// renderFnRow draws one function's row: name, the Go verdict, the copy
// action, then ClickHouse's value. The value goes last because it is the
// one column of unbounded width — on a narrow window it clips its own
// tail rather than pushing the verdict and the copy action out of view.
// chHas says whether chText is a current ClickHouse answer (otherwise it
// is an error message, or empty while waiting); modelled says whether
// goText is a prediction at all.
func (inst *App) renderFnRow(fn chFnE, pattern string, chText string, chHas bool, goText string, modelled bool) {
	expr := fn.expr(pattern, inst.replacement)
	for range c.IdScope(inst.ids.PrepareSeq(uint64(fn))) {
		for range c.HoverText(expr).KeepIter() {
			for rt := range c.RichTextLabel(fn.name()) {
				rt.Monospace()
			}
		}

		switch {
		case !modelled:
			weakLabel("—")
		case chHas && goText == chText:
			coloredLabel(agreeFg, "✓ same")
		case chHas:
			cellLabel(&warnFg, "≠ "+goText, maxVerdictRunes)
		default:
			cellLabel(nil, goText, maxVerdictRunes)
		}

		for range c.HoverText("copy " + expr).KeepIter() {
			if c.Button(inst.ids.PrepareStr("copy"), c.Atoms().Text("copy SQL").Keep()).Small().SendResp().HasPrimaryClicked() {
				c.CopyTextToClipboard(expr)
			}
		}

		switch {
		case chHas:
			cellLabel(nil, chText, maxCellRunes)
		case chText != "":
			cellLabel(&warnFg, chText, maxCellRunes)
		default:
			weakLabel("…")
		}
		c.EndRow()
	}
}

// cellLabel draws a table value in monospace, cut at limit runes with the
// whole value in a tooltip; fg, when set, colours it.
func cellLabel(fg *color.Color, text string, limit int) {
	shown, cut := truncateRunes(text, limit)
	draw := func() {
		var atoms c.AtomsFluid
		if fg != nil {
			atoms = c.Atoms().BeginRichTextColored(*fg, color.Transparent, shown).Monospace().End()
		} else {
			atoms = c.Atoms().BeginRichText(shown).Monospace().End()
		}
		// Truncate to the room left as well: the rune cap bounds the
		// wire, this keeps a long value from widening the table — and so
		// the notes under it — past a narrow window.
		c.LabelAtoms(atoms.Keep()).Truncate().Send()
	}
	if !cut {
		draw()
		return
	}
	full, _ := truncateRunes(text, 4000)
	for range c.HoverText(full).KeepIter() {
		draw()
	}
}

// truncateRunes cuts s to at most n runes, marking a cut with "…".
func truncateRunes(s string, n int) (out string, cut bool) {
	if utf8.RuneCountInString(s) <= n {
		out = s
		return
	}
	i := 0
	for range n {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	out = s[:i] + "…"
	cut = true
	return
}

// renderEvalHandoff draws the playground hand-off (ADR-0017 §SD6): one
// button that publishes both engines' extraction as ad-hoc datasets and
// opens a play window joined over them. Disabled, with the reason beside
// it, when there is nothing to hand off — a button that can only fail
// should not invite the click.
//
// The snapshot is taken on the render thread; the goroutine gets plain
// data and never touches c.* or a lane. A re-click while a hand-off is in
// flight is dropped.
func (inst *App) renderEvalHandoff() {
	// Keyed on the inputs on screen: an outcome describing a pattern the
	// user has since edited is dropped, not shown.
	busy, status, evalErr := inst.evalStatusView(inst.singleKey())
	notReady := ""
	if inst.haystack == "" {
		notReady = "enter a haystack to compare"
	}

	for range c.Horizontal().KeepIter() {
		label := "Compare both engines in the SQL playground"
		if busy {
			label = "Publishing…"
		}
		enabled := notReady == "" && !busy
		clicked := false
		for range c.HoverText("Publishes Go's matches and ClickHouse's extractAll / extractAllGroups output as two tables and opens a playground joined over them.").KeepIter() {
			for range c.Scope().KeepIter() {
				if !enabled {
					c.UiDisable()
				}
				clicked = c.Button(inst.ids.PrepareStr("evalplay"), c.Atoms().Text(label).Keep()).SendResp().HasPrimaryClicked()
			}
		}
		if clicked && enabled {
			inst.startEvalHandoff()
		}
		switch {
		case busy:
			c.Spinner().Size(14).Send()
		case notReady != "":
			weakLabel(notReady)
		case evalErr != "":
			coloredLabel(warnFg, "hand-off failed: "+evalErr)
		case status != "":
			weakLabel(status)
		}
	}
}

// startEvalHandoff snapshots both result sets and dispatches the worker.
// Render-thread only. A snapshot failure is reported in place rather than
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

// singleKey is the query fingerprint for the lane driven purely by the
// single pattern and the haystack, so a result surface can ask its lane
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

// renderMultiTab draws the multi-pattern input and its results together:
// one pattern per line, matched as a set by multiMatchAllIndices. The input
// lives in the tab rather than beside the single pattern because it is a
// separate tool on a separate engine, and keeping it next to its own
// results is what ADR-0054 asked of it.
func (inst *App) renderMultiTab() {
	weakLabel("One pattern per line, matched as a set by multiMatchAllIndices. VectorScan is a different engine from the RE2 functions, with its own limits on syntax; the flags above apply to every line.")
	listResp := inst.withInsert(inputPatternList, inst.patternListHl.TextEdit(inst.ids.PrepareStr("patternList"), inst.patternList, true, regexedit.ModeList)).
		DesiredWidth(editorWidth).
		DesiredRows(5).
		HintText("pattern 1\npattern 2\n...").
		SendRespVal(&inst.patternList)
	if listResp.HasGainedFocus() || listResp.HasFocus() {
		inst.lastFocusedInput = inputPatternList
	}
	// One parse feeds both the error summary and the per-line rows.
	lines := inst.parseAndValidatePatternList(inst.patternList)
	inst.renderPatternListCompileErrors(lines)
	inst.renderMultiLines(lines)
}

// renderMultiLines draws the per-line results under the pattern list:
//
//	<line-number> <marker>  |  <pattern text>  [ClickHouse's message]
//
// where marker is one of:
//
//	✓  the pattern hit the haystack
//	·  it did not
//	⚠  Go's regexp rejects it, so it is not sent (see [multiLine])
//	⛔ VectorScan refused it; the message says why
//	…  waiting on ClickHouse for the current input
//
// lines is the caller's live parse, so ⚠ appears as the user types. Hits
// come from the lane, and only when its result describes the current
// input — otherwise the row shows … rather than an older answer.
func (inst *App) renderMultiLines(lines []multiLine) {
	if len(lines) == 0 {
		return
	}

	view := inst.multiLane.view(inst.multiKey())
	if view.Fresh {
		lines = view.Value
	}
	validCount, hits, rejected := 0, 0, 0
	for _, l := range lines {
		switch {
		case l.Invalid:
		case l.Rejected != "":
			rejected++
		default:
			validCount++
			if l.Hit {
				hits++
			}
		}
	}
	done := fmt.Sprintf("%d line(s), none sendable (see errors above)", len(lines))
	if validCount > 0 {
		done = fmt.Sprintf("%d of %d line(s) hit", hits, validCount)
		if rejected > 0 {
			done += fmt.Sprintf(" · %d refused by VectorScan", rejected)
		}
	}
	renderLaneStatus(view, done)

	for i, line := range lines {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
			for range c.Horizontal().KeepIter() {
				mark := "·"
				switch {
				case line.Invalid:
					mark = "⚠"
				case !view.Fresh:
					mark = "…"
				case line.Rejected != "":
					mark = "⛔"
				case line.Hit:
					mark = "✓"
				}
				c.Label(fmt.Sprintf("%d %s", i+1, mark)).Send()
				c.Separator().Vertical().Send()
				for rt := range c.RichTextLabel(line.Text) {
					rt.Monospace()
				}
				if view.Fresh && line.Rejected != "" {
					coloredLabel(warnFg, line.Rejected)
				}
			}
		}
	}
}

// insertToken puts tok into the last-focused text input at its caret,
// replacing any selection — the editor's own caret, which it keeps across
// losing focus to the cheatsheet click. An input that has never had a caret
// takes the token at its end.
//
// The token waits in pendingInsert for the input's next build, which hands
// it to the widget ([App.withInsert]); the widget splices it and the text
// comes back through the binding a frame later. Tokens clicked before that
// build queue up in order. The replacement and pattern-list inputs live on
// tabs, so their tab is brought up: the token lands where it can be seen,
// and it lands at all only once the input is drawn.
func (inst *App) insertToken(tok string) {
	field := inst.lastFocusedInput
	inst.pendingInsert[field] += tok
	switch field {
	case inputReplacement:
		inst.tab = tabFunctions
	case inputPatternList:
		inst.tab = tabMulti
	}
}

// withInsert hands field's pending tokens to its editor's build and clears
// them. Cleared here, when the build carries them, rather than on the
// click: a token for an input that was not drawn this frame is kept, not
// dropped.
func (inst *App) withInsert(field inputFieldE, edit c.TextEditFluid) c.TextEditFluid {
	if tok := inst.pendingInsert[field]; tok != "" {
		edit = edit.InsertAtCursor(tok)
		inst.pendingInsert[field] = ""
	}
	return edit
}

// applyShowcase sets both the pattern and haystack inputs to showcase
// content, overriding whatever is currently in those fields. Used by the
// left-panel showcase buttons; like [App.insertToken], it only writes
// state — reconciliation does the rest.
func (inst *App) applyShowcase(pattern string, haystack string) {
	inst.pattern = pattern
	inst.haystack = haystack
}

// renderStatusBar draws the bottom status bar: the Go match count and the
// SD1 engine check, whose tooltip says what it checks and how it went.
func (inst *App) renderStatusBar() {
	for range c.Horizontal().KeepIter() {
		a := inst.analysis()
		switch a.state {
		case patternEmpty:
			c.Label("no pattern").Send()
		case patternInvalid:
			coloredLabel(warnFg, "pattern does not compile")
		default:
			c.Label(fmt.Sprintf("%d match(es)", len(a.matches))).Send()
		}
		c.Separator().Vertical().Send()

		label, tip := inst.engineCheckText()
		for range c.HoverText(tip).KeepIter() {
			c.Label(label).Send()
		}
	}
}

// engineCheckText words the SD1 tripwire's outcome for the status bar: a
// short label, and a tooltip that says what the check is and names the
// cases behind a non-green result.
func (inst *App) engineCheckText() (label string, tip string) {
	const about = "Engine check (ADR-0054 SD1): at startup a fixed set of patterns runs through Go's regexp and ClickHouse, and the app's predictions of ClickHouse are compared with what ClickHouse returns."
	tw, started, running := tripwireSnapshot()
	names := func(idx []int) (out string) {
		parts := make([]string, 0, len(idx))
		for _, i := range idx {
			parts = append(parts, tripwireCorpus[i].Name)
		}
		out = strings.Join(parts, ", ")
		return
	}
	switch {
	case !started:
		label, tip = "engine check: not started", about
	case running:
		label, tip = "engine check: running…", about
	case tw.Err != nil:
		label = "engine check: could not run"
		tip = about + "\n\nIt could not reach ClickHouse: " + clickHouseMessage(tw.Err)
	case len(tw.Drifts) > 0:
		label = fmt.Sprintf("engine check: %d mismatch(es)", len(tw.Drifts))
		tip = about + "\n\nMismatches — the app's prediction is wrong for: " + names(tw.Drifts) + ". The log has the details."
	default:
		label = "engine check ✓"
		tip = about + "\n\nAll predictions held."
		if len(tw.Known) > 0 {
			tip += fmt.Sprintf(" %d documented engine difference(s), each modelled: %s.", len(tw.Known), names(tw.Known))
		}
	}
	return
}
