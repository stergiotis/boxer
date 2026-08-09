// Package mdedit is a keelson app for writing Obsidian-flavored markdown
// beside its rendered form. The source pane is a plain monospace TextEdit, the
// preview is a markdown.Doc reparsed whenever the buffer changes, and the
// caret drives the preview's scroll at heading granularity.
//
// The app is a composition of pieces that already existed — the obsidian
// goldmark stack, the markdown widget, the ADR-0130 TextEdit seam, the
// clipboard Powerbox — and adds no IDL method, no Rust and no dependency.
// ADR-0178 records the decision, the measured reparse costs behind the update
// policy, and what the later milestones reach; this package re-argues none of
// it.
//
// The first cut has no file I/O by design. Text arrives through egui's own
// paste, which is the widget's business and needs no capability, and leaves
// through clipboard.write. The buffer is persisted to the app's own keelson
// store — not the filesystem — so it survives the window.
//
// Two constraints shape the implementation and are easy to undo by accident:
// markdown.Parse issues FFI opcodes as it lowers the AST, so it may only run
// on the render goroutine; and everything that reaches the bus runs OFF it,
// because a request blocks until the broker answers or the request times out.
//
// Lifecycle: Mount captures the id stack, logger, bus and storage and starts
// the restore; Frame renders inside the host-owned window; Unmount persists
// the buffer one last time.
package mdedit

import (
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdownhighlight"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

const (
	// docKey is the persist key the buffer is stored under. A single NATS
	// token — the persist service rejects dotted keys.
	docKey = "document"

	// sourceSplitFrac is the share of the window the source pane takes, and
	// windowFallbackWidthPx stands in for one frame before the window probe
	// reports.
	//
	// The split is recomputed from the measured window every frame and applied
	// with ExactSize, rather than left to egui's resizable side panel. A
	// SidePanel keeps its width in retained state and CLAMPS that state when
	// the window shrinks — destructively. `DefaultSize` only seeds the first
	// frame, so once a narrow window has squeezed the panel down, widening the
	// window again leaves the source pane stuck at its clamped width, a ribbon
	// a few characters wide beside a preview that took everything. Deriving
	// the width instead means every resize lands somewhere sane and nothing
	// can get stuck.
	//
	// The cost is the drag handle: a pinned panel is not user-resizable. A
	// split the reader can drag would need its own retained fraction and a
	// splitter widget, which is a bigger thing than this fix.
	sourceSplitFrac       = float32(0.46)
	windowFallbackWidthPx = float32(1100)

	// sourceMinWidthPx keeps the source pane usable in a narrow window, down
	// to the point where an even split is all that is left to give.
	sourceMinWidthPx = float32(220)

	// autosaveEvery throttles the persist. The buffer changes on every
	// keystroke and the store is reached over the bus, so persisting per edit
	// would queue a request behind every character.
	autosaveEvery = 3 * time.Second
)

// Editor sizing. egui defaults a TextEdit to spacing().text_edit_width (280pt)
// and, for a multiline one, to four rows — neither of which has anything to do
// with the pane it sits in, so an editor that does not state both simply does
// not fill its space. Go cannot ask egui for the pane inline; it probes for it
// and reads the answer on the next frame (see paneProbeSeq).
const (
	// paneProbeSalt namespaces this app's pane probes inside the shared r21
	// slot map; threading it through the instance's id stack makes each slot
	// window-unique, so two open editors do not size each other.
	paneProbeSalt uint64 = 0x6d64656469740151

	// editorFallbackWidthPx and editorFallbackRows size the editor on its
	// first frame, before the probe has reported. One frame of a
	// conservatively sized editor, not 280pt forever.
	editorFallbackWidthPx float32 = 480.0
	editorFallbackRows    uint32  = 24

	// editorScrollbarPx is the width held back from the measured pane for the
	// vertical scrollbar. The probe runs before the scroll area exists, so it
	// cannot see it. A deliberate over-estimate: too much leaves a few unused
	// pixels, too little pushes the text's right edge under the scrollbar.
	editorScrollbarPx float32 = 16.0

	// editorChromePx is the TextEdit frame's own vertical margin, held back so
	// a document that exactly fits its rows does not overflow the pane by the
	// margin and raise a scrollbar over nothing.
	editorChromePx float32 = 8.0

	// rowHeightSeedFactor converts a font's pt size into a frame-0 row height,
	// used until the measurement lands. Over-estimates on purpose: too tall
	// means one row too few for one frame, too short means an editor taller
	// than its pane.
	rowHeightSeedFactor float64 = 1.45

	// rowProbeText measures a row. Any non-empty string does — a single
	// unwrapped line's galley height IS the font's row height.
	rowProbeText = "Mg"
)

// Hover help — stated at the widget that raises the question rather than in a
// block of prose.
const (
	tipCopy  = "Copy the whole document to the clipboard. This is the only way text leaves the app — there is no file I/O in this cut."
	tipDirty = "Whether the buffer differs from the last checkpoint: a completed copy to the clipboard, or the document restored when the window opened. With no file to be out of step with, that is the only thing the marker can honestly mean."

	tipOutline = "Show the heading outline beside the preview. Headings nest by level and a section folds away; clicking one scrolls the preview to it and takes the caret with it. The column needs a window wide enough to spare it, and hides itself below that rather than starving the two panes doing the work."

	tipStats = "Words, characters and a reading estimate over the PROSE only — fenced code bodies, markers, URLs, table rules and frontmatter are excluded, since none of them are read at prose speed. The estimate divides by 200 words a minute, a round convention rather than a measurement of anyone."

	hintEmpty = "Write markdown here, or paste a document with Ctrl+V. The preview on the right updates as you type."
)

// Bar button labels, built once — identical retained bytes across frames
// intern to one blob.
var (
	atomsCopy   = c.Atoms().Text("Copy to clipboard").Keep()
	atomsOpen   = c.Atoms().Text(icons.PhFolderOpen + " Open").Keep()
	atomsSave   = c.Atoms().Text(icons.PhFloppyDisk + " Save").Keep()
	atomsSaveAs = c.Atoms().Text("Save as…").Keep()
)

// App is the per-window mdedit instance.
type App struct {
	// ids is the per-instance WidgetIdStack. The host pre-pushes a
	// window-unique salt onto it before every Frame() call (ADR-0026 §SD9,
	// windowhost.renderWindowBody), so every id derived from it is unique
	// across concurrently open instances. Captured from ctx.Ids() in Mount;
	// the app must NOT Reset() it.
	ids *c.WidgetIdStack

	logger zerolog.Logger
	bus    app.BusI
	store  app.StorageI

	// src is the document. It is the FFI write-back target for the source
	// TextEdit, so it has to be a field on a struct that outlives the frame:
	// the frontend writes into it one frame after the edit.
	src string

	// cursor is the caret range the editor reports under ReportCursor, packed
	// low=start / high=end as CHAR offsets. Also an FFI write-back target, and
	// bound to the same widget id as src — the two databindings live in
	// separate maps, so text and caret travel independently.
	cursor uint64

	// saved is the document as of the last checkpoint. dirty is DERIVED by
	// comparing it against src rather than stored as a flag, so no edit path
	// can forget to raise it.
	saved string

	// doc is the parsed preview and docSrc is the source it was parsed from;
	// together they are the reparse gate. docOk separates "parsed an empty
	// document" from "never parsed".
	doc    *markdown.Doc
	docSrc string
	docOk  bool

	// caretSlug is the section the caret was last in. It exists ONLY as the
	// baseline for change detection, and an outline click deliberately does
	// not touch it: if a click moved it, the next frame would see the caret's
	// real section as "changed" and drag the preview straight back.
	caretSlug string

	// pendingScroll is the section the preview should scroll to on the next
	// render, set either by the caret entering a new section or by an outline
	// click, and consumed by the render. One channel for both sources, so
	// they cannot fight over the same frame.
	pendingScroll string

	// showOutline is the reader's toggle. The column also needs the window to
	// be wide enough — see App.outlineVisible.
	showOutline bool

	// The outline pane's state (see mdedit_outline.go). outline is the
	// hierarchy, rebuilt whenever outlineDoc stops matching the current parse;
	// outlineState is the tree widget's own expansion and selection, and the
	// authority for which sections are closed. It survives a reparse because
	// the hierarchy carries a key column (slug#ord) for the widget to file
	// under — a node index would name a different heading one keystroke later.
	// Only the selection is projected, from the caret.
	outline      outlineModel
	outlineDoc   *markdown.Doc
	outlineState tree.State
	// outlineRevealed is the section the outline last scrolled itself to, so a
	// caret that has stayed put does not re-issue the scroll every frame.
	outlineRevealed string
	// outlinePaneW/H are what the outline's pane probe last reported, held
	// across frames for the same reason paneW/paneH are.
	outlinePaneW float32
	outlinePaneH float32

	// paneW/paneH are what the editor's pane probe reported last, held across
	// frames so a frame in which the probe does not come back (the pane was
	// not rendered) reuses the last known size instead of collapsing to the
	// fallback.
	paneW float32
	paneH float32

	// winW is the window's inner width, from a probe taken before the panels
	// claim it. It drives the source/preview split.
	winW float32

	// rowPx is the measured monospace row height and rowFontPt the size it
	// describes, so a density change re-seeds rather than carrying the old
	// font's answer. Measured rather than assumed: the host may leave the
	// monospace face unconfigured, and a hardcoded row height is off by enough
	// to leave a visible strip of the pane unused.
	rowPx     float64
	rowFontPt float32

	// lexSpans is the source tier's spans and lexSrc the buffer they describe.
	// Keyed by text rather than memoised: per-keystroke content is new by
	// construction, so ADR-0125's LRU would only churn (ADR-0130 §6).
	//
	// The spans are kept because two consumers want them — the colour job
	// below and the prose readout, which counts words by asking which bytes
	// the lexer decided were prose. One lex serves both.
	lexSrc   string
	lexOk    bool
	lexSpans []markdownhighlight.Span

	// hlJob is the colour job the editor is coloured from and hlStyled the
	// find overlay riding on it, with hlKey the inputs both were built from.
	//
	// They share a gate because they share a dependency: a find match has to
	// become a boundary in the COLOUR tier for its overlay to land on its own
	// bytes rather than on the whole prose run around it (see mdedit_find.go).
	// So the job moves when the match set moves, not only when the text does.
	hlJob      typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	hlStyled   typed.RetainedFffiHolderTyped[c.StyledSectionsS]
	hlJobOk    bool
	hlStyledOk bool
	hlKey      highlightKey
	hlKeyOk    bool

	// find is the find-and-replace bar's own state (M3).
	find findState

	// rebindSrc marks a buffer this app rewrote rather than the reader typed,
	// so renderSource knows to drop the editor's cached copy after the emit.
	// A flag rather than a direct call because the override has to happen
	// where the databinding is registered — see App.rebindBuffer.
	rebindSrc bool

	// confirmDiscard is the armed half of Open's two-click confirmation, and
	// confirmDiscardSrc the buffer it was armed against — typing disarms it.
	// See App.clearDiscardConfirm.
	confirmDiscard    bool
	confirmDiscardSrc string

	// writeHandle is the fs.handle.{uuid} prefix a save dialog granted, empty
	// until the reader has chosen a file. Holding it is what makes the second
	// and every later Save silent (ADR-0178 M4).
	//
	// It is NOT the file's identity — the broker never tells the app the path
	// — and it does not survive the window: handles are revoked when the app's
	// bus client goes, so a restored document asks where to save again. The
	// persisted buffer is what survives, which is the point of keeping the
	// app's own store beside the file rather than instead of it.
	writeHandle string

	// stats is the word / character / reading-time readout, recomputed with
	// the spans.
	stats docStats

	// pendingInsert is the formatting snippet a bar click produced, handed to
	// the TextEdit as insertAtCursor and cleared once emitted.
	pendingInsert string

	// pendingCaret is a caret move the app is asking the editor for, and
	// pendingCaretOk whether there is one — a separate flag rather than a
	// sentinel, so the zero value means "no request" and not "select the first
	// character".
	pendingCaret   caretRequest
	pendingCaretOk bool

	// status is the one-line report under the action bar.
	status string

	// persistedSrc is the buffer as of the last successful persist, and
	// lastPersistAt when a persist was last STARTED. Both are render-thread
	// only; the goroutine reports back through the guarded fields below.
	persistedSrc  string
	lastPersistAt time.Time

	// mu guards everything the clipboard and persist goroutines touch. The
	// render thread drains it once per frame in drainAsync. No c.* call ever
	// happens off the render goroutine — the framework is single-threaded.
	mu sync.Mutex

	exporting    bool
	exportDone   bool
	exportErr    error
	exportedText string

	persisting    bool
	persistDone   bool
	persistErr    error
	persistedText string

	restoreDone bool
	restoreOk   bool
	restoreText string

	// One in-flight file gesture at a time (M4). A dialog is a human deciding
	// in a picker, so the window between claiming and finishing is long enough
	// that a second click is a real possibility rather than a theoretical one.
	fileBusy bool
	fileDone bool
	fileRes  fileResult
}

// caretRequest is a caret position the app asks the editor to take, in BYTE
// offsets into the buffer — the unit everything Go knows about the document is
// in. renderSource converts to the chars the caret channel speaks.
//
// Focus is a parameter rather than a behaviour, the ADR-0130 rule: an
// unfocused TextEdit paints neither caret nor selection, so a gesture meaning
// "take me there" has to ask for it — but taking focus for every move would
// pull it out of the find field a keystroke at a time.
type caretRequest struct {
	Start int
	Stop  int
	Focus bool
}

// requestCaret queues a caret move for the next emit. One slot: a second
// request in the same frame replaces the first rather than queueing behind it,
// since only one of them can be where the caret ends up anyway.
func (inst *App) requestCaret(start, stop int, focus bool) {
	inst.pendingCaret = caretRequest{Start: start, Stop: stop, Focus: focus}
	inst.pendingCaretOk = true
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		ids:    c.NewWidgetIdStack(),
		logger: log.Logger,
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.logger = ctx.Log()
	inst.bus = ctx.Bus()
	inst.store = ctx.Storage()
	if inst.store != nil {
		go inst.restore()
	}
	return
}

// Unmount persists the buffer one last time, synchronously. Every other
// persist runs off the render thread to keep a stalled bus out of the frame
// budget; this one cannot, because a goroutine started here would outlive the
// window and have nothing left to report to. A store that does not answer
// costs the close one request timeout — the trade the other way loses the
// document.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	if inst.store == nil || inst.src == inst.persistedSrc {
		return
	}
	// Wait out an autosave already in flight. Its snapshot is older than this
	// one, so letting the two race would sometimes leave the store holding the
	// stale version — a silent few seconds of lost work on the next open. The
	// wait is bounded: a wedged store must not hold the window open.
	for i := 0; i < 100 && inst.persistInFlight(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if serr := inst.store.Set(docKey, []byte(inst.src)); serr != nil {
		inst.logger.Warn().Err(serr).Msg("mdedit: final persist failed")
	}
	return
}

// Frame renders the app body. The host has already pre-pushed a window-unique
// salt onto inst.ids (ADR-0026 §SD9), so the app must not Reset() the stack or
// wrap the body in its own instance salt.
func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.renderBody()
	return
}

func (inst *App) renderBody() {
	inst.drainAsync()
	inst.clearDiscardConfirm()
	inst.refreshDerived()
	// Before any pane reads pendingScroll, so a caret move and an outline
	// click in the same frame resolve in that order — the click wins, which is
	// the one the reader just made.
	inst.trackCaretSection()
	inst.autosave()

	// Probe the window before anything claims it, so the split below is a
	// share of the whole rather than of whatever is left.
	if w, _, ok := c.CapturePaneSize(inst.paneProbeSeq("window")); ok && w > 0 {
		inst.winW = w
	}

	for range c.PanelTopInside(inst.ids.PrepareStr("bar")).KeepIter() {
		inst.renderBar()
	}
	// A replace in the bar above rewrote the buffer AFTER everything derived
	// from it was computed. Recomputing here rather than leaving it to the
	// next frame is what keeps the source pane from being handed a colour job
	// and an overlay list describing the text it no longer holds — every gate
	// is a comparison, so the frames where nothing was replaced pay four of
	// them.
	inst.refreshDerived()

	// Panels must be declared before the central region claims what is left.
	// renderSource owns its own scroll area so its pane probe measures the
	// panel rather than the scrolled content.
	for range c.PanelLeftInside(inst.ids.PrepareStr("sourcepane")).
		ExactSize(inst.sourceWidth()).KeepIter() {
		inst.renderSource()
	}
	// The outline is declared before the central region too, so its click can
	// still reach the preview in the same frame.
	if inst.outlineVisible() {
		for range c.PanelRightInside(inst.ids.PrepareStr("outlinepane")).
			ExactSize(inst.outlineWidth()).KeepIter() {
			inst.renderOutline()
		}
	}
	for range c.PanelCentralInside().KeepIter() {
		// Hscroll for the same reason as the source pane: markdown holds
		// things that do not wrap — a wide table, a long fenced line — and an
		// unscrollable horizontal axis would push their width back out into
		// the window's minimum instead of scrolling them.
		for range c.ScrollArea().Hscroll(true).Vscroll(true).AutoShrink(false, false).KeepIter() {
			inst.renderPreview()
		}
	}
}

// ---------------------------------------------------------------------------
// Panes
// ---------------------------------------------------------------------------

// renderBar draws the two action rows: formatting on top, document state
// below. Callers own the enclosing panel.
func (inst *App) renderBar() {
	// Row one — formatting. A click's snippet is stashed rather than applied:
	// it has to reach the TextEdit as a builder method on the widget itself,
	// which renders later in this same frame.
	for range c.HorizontalTop().KeepIter() {
		if snippet := inst.renderFormatBar(); snippet != "" {
			inst.pendingInsert = snippet
		}
	}

	for range c.HorizontalTop().KeepIter() {
		// File row (M4). Every button renders unconditionally and drops the
		// click while something is in flight, for the same reason the copy
		// button below does — and more so here, since a file dialog is open
		// for as long as a person takes to decide.
		openClicked, saveClicked, saveAsClicked := false, false, false
		for range c.HoverText(tipOpen).KeepIter() {
			openClicked = c.Button(inst.ids.PrepareStr("open"), atomsOpen).SendResp().HasPrimaryClicked()
		}
		for range c.HoverText(tipSave).KeepIter() {
			saveClicked = c.Button(inst.ids.PrepareStr("save"), atomsSave).SendResp().HasPrimaryClicked()
		}
		for range c.HoverText(tipSaveAs).KeepIter() {
			saveAsClicked = c.Button(inst.ids.PrepareStr("saveas"), atomsSaveAs).SendResp().HasPrimaryClicked()
		}
		if !inst.fileInFlight() {
			switch {
			case openClicked:
				inst.openFile()
			case saveAsClicked:
				inst.saveFile(true)
			case saveClicked:
				inst.saveFile(false)
			}
		}

		// Whether there is a file, never which one — the broker hands over a
		// handle and keeps the path, so naming it would be a guess.
		if inst.fileBound() {
			badge.New(inst.ids.PrepareStr("file"), "file bound").
				Tone(badge.ToneNeutral).Variant(badge.VariantSoft).Size(badge.SizeSm).
				Tooltip(tipFileBound).Send()
		}

		// The button renders unconditionally. Dropping it from the tree while
		// a request is in flight would collapse the row and strobe it back on
		// milliseconds later; the click is ignored instead.
		clicked := false
		for range c.HoverText(tipCopy).KeepIter() {
			clicked = c.Button(inst.ids.PrepareStr("copy"), atomsCopy).SendResp().HasPrimaryClicked()
		}
		if clicked && !inst.exportInFlight() {
			inst.exportClipboard()
		}

		label, tone := "no changes", badge.ToneNeutral
		if inst.dirty() {
			label, tone = "modified", badge.ToneWarning
		}
		badge.New(inst.ids.PrepareStr("dirty"), label).
			Tone(tone).Variant(badge.VariantSoft).Size(badge.SizeSm).
			Tooltip(tipDirty).Send()

		// The outline toggle names its own count, so the button says what
		// turning it on would show. It renders even when the window is too
		// narrow to honour it — a control that disappears at some width is
		// harder to understand than one that stays and has no effect — and
		// the tooltip is where that is said.
		var headings []markdown.HeadingInfo
		if inst.doc != nil {
			headings = inst.doc.Headings()
		}
		for range c.HoverText(tipOutline).KeepIter() {
			c.Checkbox(inst.ids.PrepareStr("outline"), inst.showOutline, outlineSummary(headings)).
				SendRespVal(&inst.showOutline)
		}

		for range c.HoverText(tipFind).KeepIter() {
			c.Checkbox(inst.ids.PrepareStr("find"), inst.find.show, findToggleLabel).
				SendRespVal(&inst.find.show)
		}

		for range c.HoverText(tipStats).KeepIter() {
			c.Label(inst.statsLine()).Send()
		}

		if inst.status != "" {
			c.Label(inst.status).Send()
		}
	}

	// Row three — find and replace, present only when asked for. Unlike the
	// outline it has no width floor to meet: it is a row rather than a column,
	// so a narrow window wraps it instead of starving a pane.
	if inst.find.show {
		for range c.HorizontalTop().KeepIter() {
			inst.renderFindBar()
		}
	}
}

// statsLine renders the readout. Reading time is omitted below a minute rather
// than shown as "0 min" or rounded up to a minute a 20-word note does not
// deserve.
func (inst *App) statsLine() (s string) {
	var b strings.Builder
	b.WriteString(itoa(inst.stats.Words))
	b.WriteString(" words · ")
	b.WriteString(itoa(inst.stats.Chars))
	b.WriteString(" chars")
	if inst.stats.ReadMinutes > 0 && inst.stats.Words >= wordsPerMinute/4 {
		b.WriteString(" · ~")
		b.WriteString(itoa(inst.stats.ReadMinutes))
		b.WriteString(" min read")
	}
	return b.String()
}

// refreshDerived brings everything computed FROM the buffer back into step
// with it, in dependency order: the preview parse and the lex are independent,
// the match list needs the buffer, and the colour job needs the match list.
//
// Every stage is gated on its own inputs, so calling this twice in a frame —
// which renderBody does, because a replace can rewrite the buffer between the
// bar and the panes — costs four comparisons when nothing moved. All of it
// must run on the render goroutine: the parse and the job both issue FFI
// opcodes as they build retained holders.
func (inst *App) refreshDerived() {
	inst.syncDoc()
	inst.ensureLex()
	inst.ensureMatches()
	inst.ensureHighlight()
}

// ensureLex lexes the buffer once per change, for the two consumers that read
// spans: the colour job below and the prose readout.
func (inst *App) ensureLex() {
	if inst.lexOk && inst.lexSrc == inst.src {
		return
	}
	inst.lexSrc = inst.src
	inst.lexOk = true
	if inst.src == "" {
		inst.lexSpans = nil
		inst.stats = docStats{}
		return
	}
	inst.lexSpans = markdownhighlight.HighlightLex([]byte(inst.src))
	inst.stats = countStats(inst.src, inst.lexSpans)
}

// highlightKey is what the colour job and the find overlay are both built
// from. Comparing it is the gate: equal keys mean equal output, so neither is
// rebuilt.
//
// The find fields are in it because a match boundary has to become a COLOUR
// boundary — see mdedit_find.go on why an overlay otherwise tints the whole
// prose run it sits in. Which match is current is in it for the same reason:
// the current one is painted differently, so moving to the next rebuilds both.
type highlightKey struct {
	src     string
	query   string
	fold    bool
	showing bool
	idx     int
}

// ensureHighlight builds the editor's colour job and the find overlay riding
// on it. Must run on the render goroutine — both constructions issue FFI
// opcodes.
//
// The spans index the buffer's own bytes — the reason M1 needed a lexer rather
// than the existing canonicalising highlighter, whose spans describe a rewrite
// of the input and would colour the wrong bytes here. The Rust layouter
// applies them advisorily and reconciles the one-frame staleness, so a job
// built from last frame's text still lands correctly on this frame's
// (ADR-0130 §Decision 2-3).
func (inst *App) ensureHighlight() {
	key := highlightKey{
		src: inst.src, query: inst.find.query, fold: !inst.find.matchCase,
		showing: inst.find.show, idx: inst.find.idx,
	}
	if inst.hlKeyOk && inst.hlKey == key {
		return
	}
	inst.hlKey = key
	inst.hlKeyOk = true
	if inst.src == "" {
		// An empty job would claim zero bytes of an empty buffer; skipping the
		// method entirely leaves the hint text rendering as it does today.
		inst.hlJobOk, inst.hlStyledOk = false, false
		return
	}
	// Held across frames rather than rebuilt per frame, which is the opposite
	// of what BuildStyledSections expects of a producer. Its "uncached" note
	// is sized for play's handful of sections; a find can paint hundreds, and
	// they move only when this key does.
	secs := matchSections(inst.find.matches, inst.find.idx, maxPaintedMatches)
	spans := splitSpansAt(inst.lexSpans, matchCuts(inst.find.matches, inst.find.idx, maxPaintedMatches))
	inst.hlJob = codeview.BuildMarkdownFromSpans(inst.src, spans)
	inst.hlJobOk = true
	inst.hlStyled, inst.hlStyledOk = codeview.BuildStyledSections(secs)
}

// sourceWidth is the source pane's width for this frame: a fixed share of the
// measured window, floored so a narrow window still leaves something to write
// in, and never more than half of what there is to divide.
func (inst *App) sourceWidth() (px float32) {
	winW := inst.winW
	if winW <= 0 {
		winW = windowFallbackWidthPx
	}
	px = winW * sourceSplitFrac
	if px < sourceMinWidthPx {
		px = min(sourceMinWidthPx, winW*0.5)
	}
	return
}

// paneProbeSeq is the r21 slot for the editor's pane probe. Never
// CaptureAvailableSize: that register is one process-wide scalar that the
// frame's last capture wins, so anything else on screen that probes would size
// this editor instead.
func (inst *App) paneProbeSeq(role string) (seq uint64) {
	return c.ProbeSeq("mdedit", role) ^ inst.ids.PrepareHighEntropy(paneProbeSalt).Derive()
}

// rowHeight is the editor's row height — the measurement once it lands, floored
// at the font size, since no row is shorter than its em box.
func (inst *App) rowHeight() (px float32) {
	return max(float32(inst.rowPx), inst.rowFontPt)
}

// measureRowHeight (re-)arms the row-height probe. Re-emitted every frame
// because Sync resets databindings, and re-seeded when a density change moves
// the font size. The answer is constant after the first Sync, so the one-frame
// lag is a warm-up rather than a lag.
func (inst *App) measureRowHeight() {
	pt := styletokens.ScaledPt(styletokens.BodyPt, styletokens.DensityStandard)
	if inst.rowFontPt != pt {
		inst.rowFontPt = pt
		inst.rowPx = float64(pt) * rowHeightSeedFactor
	}
	c.MeasureTextSizeBind(inst.paneProbeSeq("row-w"), inst.paneProbeSeq("row-h"),
		rowProbeText, pt, true, nil, &inst.rowPx)
}

// renderSource emits the editing surface, sized to fill the pane it is given.
// It owns its own scroll area: the probe below has to run in the PANE, before
// anything is placed, and a probe inside the scroll area would be measuring the
// scrolled content instead.
func (inst *App) renderSource() {
	inst.measureRowHeight()

	// Probe first, before placing anything. CaptureUiAvailableRect reports the
	// space left for the NEXT widget, which from here is the editor's whole
	// pane — and, crucially, a rect that does not move when the editor draws
	// into it. Sizing off the editor's own min_rect instead would ratchet: the
	// measurement would grow with the content that the measurement sized.
	if w, h, ok := c.CapturePaneSize(inst.paneProbeSeq("editor-pane")); ok && w > 0 {
		inst.paneW, inst.paneH = w, h
	}

	width := editorFallbackWidthPx
	if inst.paneW > 0 {
		width = max(inst.paneW-editorScrollbarPx, 1)
	}
	rows := editorFallbackRows
	if rh := inst.rowHeight(); inst.paneH > 0 && rh > 0 {
		if n := int((inst.paneH - editorChromePx) / rh); n >= 1 {
			rows = uint32(n)
		}
	}

	// AutoShrink(false, false) keeps the pane's full size rather than
	// collapsing onto the content, so a short document still leaves the editor
	// filling its pane instead of shrinking to a few lines.
	//
	// Hscroll(true) is not about scrolling — the editor wraps, so its bar
	// never appears. It is about which way the width flows. egui sizes a
	// scroll-area axis by (direction_enabled, auto_shrink): (true, false)
	// keeps the size it was OFFERED, while (false, false) reports
	// max(available, content) — i.e. an unscrollable axis pushes its content
	// width back out into the layout. With the horizontal axis pushing, the
	// editor's desired width became part of the window's MINIMUM width, and
	// since that desired width is itself derived from the window, the floor
	// rose every time the window did and then blocked the way back: widen
	// once, and the window could never be dragged narrow again. Enabling the
	// axis severs that path — the editor takes the width it is given and
	// reports nothing upward.
	for range c.ScrollArea().Hscroll(true).Vscroll(true).AutoShrink(false, false).KeepIter() {
		b := c.TextEdit(inst.ids.PrepareStr("editor"), inst.src, true).
			CodeEditor().
			DesiredWidth(width).
			DesiredRows(rows).
			HintText(hintEmpty).
			ReportCursor()
		if inst.hlJobOk {
			b = b.HighlightJob(inst.hlJob)
			// Only inside the branch, and that is not tidiness: the styled
			// overlays reach the layouter through the highlight job, and a
			// highlight job is the only thing that installs one. Sent without
			// it they would be taken, dropped, and paint nothing.
			if inst.hlStyledOk {
				b = b.SectionStyled(inst.hlStyled)
			}
		}
		// Hand the formatting bar's snippet to the widget, which splices it at
		// its own persisted caret next frame and replaces any selection with
		// it (ADR-0063). Cleared here rather than on click, so a click that
		// landed while this pane was not rendering is not silently dropped.
		if inst.pendingInsert != "" {
			b = b.InsertAtCursor(inst.pendingInsert)
			inst.pendingInsert = ""
		}
		// Move the caret where something asked it to go. One-shot: sent every
		// frame it would pin the caret against the person typing. Whether it
		// takes focus is the requester's call — jumping to a heading or a
		// match means "take me there", a replacement means "the text moved",
		// and only the first of those should pull focus out of wherever the
		// reader is working.
		if inst.pendingCaretOk {
			r := inst.pendingCaret
			b = b.SetCursor(c.PackCursorRange(
				byteToChar(inst.src, r.Start), byteToChar(inst.src, r.Stop)), r.Focus)
			inst.pendingCaretOk = false
		}
		_ = b.SendRespValCursor(&inst.src, &inst.cursor)

		// Both overrides are applied HERE, immediately after the emit, and not
		// in drainAsync with everything else. OverrideDatabindingSPtr resolves
		// the pointer through the databindings registered for THIS frame, and
		// SendRespValCursor is what registers them — applied any earlier they
		// would find nothing to override, and the frontend's cached buffer
		// would win at the next Sync.
		inst.applyRestore()
		if inst.rebindSrc {
			// A find-and-replace rewrite (M3). Same mechanism as the restore,
			// different origin: the editor is holding a buffer this app
			// computed, so its own copy has to be dropped rather than pushed
			// back over it.
			c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.src)
			inst.rebindSrc = false
		}
	}
}

// renderPreview emits the rendered document. Callers own the enclosing scroll
// area.
func (inst *App) renderPreview() {
	if inst.doc == nil {
		c.Label("Nothing to preview yet.").Send()
		return
	}
	// markdown.Doc derives ids for its code blocks, blockquotes and callouts
	// from a per-Render sequence and deliberately does NOT open its own scope;
	// supplying one is the caller's job whenever more than one doc might share
	// a parent scope (markdown EXPLANATION, "Caller-provided IdScope").
	for range c.IdScope(inst.ids.PrepareStr("preview")) {
		if slug, changed := inst.takeScrollTarget(); changed {
			inst.doc.Render(inst.ids, markdown.WithScrollToSection(slug))
		} else {
			inst.doc.Render(inst.ids)
		}
	}
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

// dirty reports whether the buffer differs from the last checkpoint — a
// completed clipboard export, or the restore that opened the window.
func (inst *App) dirty() (yes bool) {
	yes = inst.src != inst.saved
	return
}

// syncDoc reparses the preview when the buffer no longer matches what the
// current document was parsed from.
//
// Two properties make the simple gate affordable, both recorded in ADR-0178:
// only frames in which the text actually changed pay anything, and a reparse
// at editor-typical document sizes is a low single-digit percentage of a
// frame. It must run on the render goroutine — markdown.Parse builds retained
// FFI holders as it lowers, so it cannot be moved to a worker the way the SQL
// editor's semantic tier was.
func (inst *App) syncDoc() {
	if inst.docOk && inst.docSrc == inst.src {
		return
	}
	inst.doc = markdown.Parse([]byte(inst.src))
	inst.docSrc = inst.src
	inst.docOk = true
}

// trackCaretSection resolves the caret's section and, when it has changed
// since last frame, queues a scroll to it. Call once per frame, before
// anything that reads pendingScroll.
//
// It only ever queues on a CHANGE. Queuing every frame would re-issue the
// scroll continuously and pin the preview against the reader's own scrolling,
// which is the guard markdown.WithScrollToSection documents.
func (inst *App) trackCaretSection() {
	if inst.doc == nil {
		return
	}
	caret, _ := c.UnpackCursorRange(inst.cursor)
	slug, changed := scrollTarget(inst.src, inst.doc.Headings(), caret, inst.caretSlug)
	if !changed {
		return
	}
	inst.caretSlug = slug
	inst.pendingScroll = slug
}

// takeScrollTarget consumes the queued scroll target, whichever source set it.
func (inst *App) takeScrollTarget() (slug string, ok bool) {
	if inst.pendingScroll == "" {
		return "", false
	}
	slug = inst.pendingScroll
	inst.pendingScroll = ""
	return slug, true
}

// ---------------------------------------------------------------------------
// Off-thread work
// ---------------------------------------------------------------------------

func (inst *App) exportInFlight() (busy bool) {
	inst.mu.Lock()
	busy = inst.exporting
	inst.mu.Unlock()
	return
}

func (inst *App) persistInFlight() (busy bool) {
	inst.mu.Lock()
	busy = inst.persisting
	inst.mu.Unlock()
	return
}

// exportClipboard starts a clipboard write for the current buffer. The request
// runs off the render thread on purpose: bus.Request blocks until the broker
// answers or the request times out, so a host running without a clipboard
// service would otherwise freeze the frame for the whole timeout.
func (inst *App) exportClipboard() {
	if inst.bus == nil {
		inst.status = "clipboard unavailable"
		return
	}
	snapshot := inst.src
	inst.mu.Lock()
	inst.exporting = true
	inst.mu.Unlock()
	go func() {
		_, err := inst.bus.Request(clipboardbroker.SubjectWrite, []byte(snapshot))
		inst.mu.Lock()
		inst.exporting = false
		inst.exportDone = true
		inst.exportErr = err
		inst.exportedText = snapshot
		inst.mu.Unlock()
	}()
}

// autosave persists the buffer at most once per autosaveEvery, and only when
// it differs from what last landed in the store.
func (inst *App) autosave() {
	if inst.store == nil || inst.src == inst.persistedSrc {
		return
	}
	now := time.Now()
	if !inst.lastPersistAt.IsZero() && now.Sub(inst.lastPersistAt) < autosaveEvery {
		return
	}
	inst.mu.Lock()
	if inst.persisting {
		inst.mu.Unlock()
		return
	}
	inst.persisting = true
	inst.mu.Unlock()

	inst.lastPersistAt = now
	snapshot := inst.src
	go func() {
		err := inst.store.Set(docKey, []byte(snapshot))
		inst.mu.Lock()
		inst.persisting = false
		inst.persistDone = true
		inst.persistErr = err
		inst.persistedText = snapshot
		inst.mu.Unlock()
	}()
}

// restore reads the persisted document. Runs once, off the render thread, from
// Mount.
func (inst *App) restore() {
	value, found, err := inst.store.Get(docKey)
	if err != nil {
		inst.logger.Warn().Err(err).Msg("mdedit: restore failed")
	}
	inst.mu.Lock()
	inst.restoreDone = true
	inst.restoreOk = err == nil && found
	inst.restoreText = string(value)
	inst.mu.Unlock()
}

// drainAsync moves finished off-thread work onto the render thread. Called
// once at the top of a frame.
func (inst *App) drainAsync() {
	inst.mu.Lock()
	expDone, expErr, expText := inst.exportDone, inst.exportErr, inst.exportedText
	inst.exportDone = false
	perDone, perErr, perText := inst.persistDone, inst.persistErr, inst.persistedText
	inst.persistDone = false
	fileDone, fileRes := inst.fileDone, inst.fileRes
	inst.fileDone = false
	inst.mu.Unlock()

	if fileDone {
		inst.applyFileResult(fileRes)
	}

	if expDone {
		switch {
		case expErr != nil:
			inst.status = "copy failed: " + expErr.Error()
			inst.logger.Warn().Err(expErr).Msg("mdedit: clipboard write failed")
		default:
			// The checkpoint is what actually landed on the clipboard, not
			// what the buffer holds now — the reader may have typed while the
			// request was in flight, and that typing is genuinely unsaved.
			inst.saved = expText
			inst.status = "copied to clipboard"
		}
	}
	if perDone {
		if perErr != nil {
			// persistedSrc is left behind, so the throttle retries.
			inst.logger.Warn().Err(perErr).Msg("mdedit: persist failed")
		} else {
			inst.persistedSrc = perText
		}
	}
}

// applyRestore installs a restored document, if one arrived and the reader has
// not already started writing.
func (inst *App) applyRestore() {
	inst.mu.Lock()
	done, ok, text := inst.restoreDone, inst.restoreOk, inst.restoreText
	inst.restoreDone = false
	inst.mu.Unlock()
	if !done || !ok {
		return
	}
	// A restore never clobbers work in progress. The store is read from Mount
	// and may answer several frames later; if the reader typed in the meantime
	// their buffer wins and the persisted copy is dropped.
	if inst.src != "" {
		return
	}
	inst.src = text
	inst.saved = text
	inst.persistedSrc = text
	// The editor's frontend holds its own cache of the buffer and would
	// overwrite this assignment at the next Sync. The override tells it to
	// drop that cache for this widget.
	c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.src)
}
