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
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdline"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/apps/mdedit/launchcfg"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsftp/sftptree"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdownhighlight"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/mdblocks"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/mdoutline"
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
	tipMountCommand = "Copy the command that mounts this document's snapshot read-only with rclone, the document and its images side by side. It runs `boxer fs sftp-stdio` with your shell's ClickHouse settings, so it needs rclone 1.64 or later and a `boxer` executable on PATH; change " + mountCommandDir + " to an empty directory of yours."
	tipCopy         = "Copy the whole document to the clipboard. This is the only way text leaves the app — there is no file I/O in this cut."
	tipDirty        = "Whether the buffer differs from the last checkpoint: a completed copy to the clipboard, or the document restored when the window opened. With no file to be out of step with, that is the only thing the marker can honestly mean."

	tipOutline = "The heading outline. Headings nest by level and a section folds away; clicking one scrolls the preview to it and takes the caret with it."

	tipStats = "Words, characters and a reading estimate over the PROSE only — fenced code bodies, markers, URLs, table rules and frontmatter are excluded, since none of them are read at prose speed. The estimate divides by 200 words a minute, a round convention rather than a measurement of anyone."

	hintEmpty = "Write markdown here, or paste a document with Ctrl+V. The preview on the right updates as you type."

	tipModeSplit  = "Source beside preview — the editing layout."
	tipModeSource = "Source only, full width. The preview pauses; the outline stays, since it navigates the caret."
	tipModeRead   = "Preview only, full width. The editor is hidden and the formatting and replace gestures go with it. Find still counts matches and takes the preview to them."
	tipModeBlocks = "The document as blocks, full width. Click a block to edit it as text; the others stay rendered. Enter at the end of a block starts a new one, Backspace at a block's start joins it to the one above, Escape selects the block, Ctrl+Z and Ctrl+Y walk the document's own undo. The formatting bar and replace go with the source pane; the outline follows the caret."
)

// Bar button labels, built once — identical retained bytes across frames
// intern to one blob.
var (
	atomsCopy         = c.Atoms().Text("Copy to clipboard").Keep()
	atomsMountCommand = c.Atoms().Text(icons.PhTerminalWindow + " rclone").Keep()
	atomsOpen         = c.Atoms().Text(icons.PhFolderOpen + " Open").Keep()
	atomsSave         = c.Atoms().Text(icons.PhFloppyDisk + " Save").Keep()
	atomsSaveAs       = c.Atoms().Text("Save as…").Keep()
)

// viewModeE selects which of the two panes render. A mode is a deliberate
// gesture, unlike the outline's width-driven hiding, so what a mode removes
// (the format bar, the replace row) disappears legibly with it.
type viewModeE uint8

const (
	// viewSplit is the editing layout: source beside preview. The zero value,
	// so a fresh window opens in it. Session-scoped on purpose — persisting a
	// layout preference would spend a persist key on low-value state.
	viewSplit viewModeE = iota
	// viewSource is the editor alone, full width.
	viewSource
	// viewRead is the preview alone, full width — the reading mode.
	viewRead
	// viewBlocks is the block editor alone, full width (ADR-0334 §SD8).
	viewBlocks
)

// viewModes is the segmented control, in render order.
var viewModes = []struct {
	key   string
	mode  viewModeE
	tip   string
	atoms typed.RetainedFffiHolderTyped[c.AtomsS]
}{
	{"mode-split", viewSplit, tipModeSplit, c.Atoms().Text(icons.PhColumns + " Split").Keep()},
	{"mode-source", viewSource, tipModeSource, c.Atoms().Text(icons.PhPencilSimple + " Source").Keep()},
	{"mode-read", viewRead, tipModeRead, c.Atoms().Text(icons.PhBookOpen + " Read").Keep()},
	{"mode-blocks", viewBlocks, tipModeBlocks, c.Atoms().Text(icons.PhTextAa + " Blocks").Keep()},
}

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

	// showSide is the reader's toggle for the side panel and sideView the
	// tab it shows (mdedit_side.go). The panel also needs the window to be
	// wide enough — see App.sideVisible.
	showSide bool
	sideView sideViewE
	// sidePaneW is the panel's measured content width, which the views fit
	// their widest controls to.
	sidePaneW float32

	// viewMode is which panes render — see viewModeE. Session-scoped: a fresh
	// window opens in the split.
	viewMode viewModeE

	// outline is the heading outline (see mdedit_outline.go): the hierarchy,
	// rebuilt whenever the parse moves, and the tree widget's own expansion
	// and selection, which survive a reparse because the hierarchy carries a
	// key column (slug#ord) for the widget to file under. Only the selection
	// is projected, from the caret.
	outline mdoutline.View

	// paneW/paneH are what the editor's pane probe reported last, held across
	// frames so a frame in which the probe does not come back (the pane was
	// not rendered) reuses the last known size instead of collapsing to the
	// fallback.
	paneW float32
	paneH float32

	// winW / winH are the window's inner size, from a probe taken before the
	// panels claim it. The width drives the source/preview split, the height
	// the proposal pane.
	winW float32
	winH float32

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

	// prop is the proposal pane's state (see mdedit_proposal.go): a
	// replacement an agent offered through propose_edit.
	prop proposalState

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

	// readName / boundName are the basenames the broker named in dialog
	// replies: the file the document was opened from, and the file it saves
	// to. Names only — never paths — and, like the handles they describe,
	// deliberately not persisted: they die with the window.
	readName  string
	boundName string

	// followActive says the opened file is being followed on disk, and
	// diskChanged / diskGone are what following found while the buffer held
	// unsaved edits. Render-thread only; the cross-goroutine half lives in
	// follow (mdedit_follow.go), under mu.
	followActive bool
	diskChanged  bool
	diskGone     bool
	follow       followState

	// readFromSnapshot marks readName as a lading-snapshot source rather than
	// a Powerbox-named file — the badge tooltip says which contract applies.
	readFromSnapshot bool
	// snapRef is the snapshot a document was read from, for the command that
	// mounts it; nil for any other document.
	snapRef *snapshotRef
	// snapImages are the images a snapshot document embeds
	// (mdedit_images.go); nil for any other document. handedSnap is a
	// handed-over document's snapshot, until the window has read it.
	snapImages *snapshotImages
	handedSnap *handedSnapshot

	// files is the side panel's files view state (mdedit_files.go).
	files filesState

	// stats is the word / character / reading-time readout, recomputed with
	// the spans.
	stats docStats

	// pendingInsert is the formatting snippet a bar click produced, handed to
	// the TextEdit as insertAtCursor and cleared once emitted.
	pendingInsert string

	// editorFieldId is the source TextEdit's widget id from the last frame,
	// the handle its captured keys are read back under (mdedit_keys.go).
	editorFieldId uint64

	// blocks is the block editor the Blocks mode hosts (mdedit_blocks.go),
	// built on first use.
	blocks *mdblocks.Editor
	// matchSpans is find's match list as the block editor takes it, and
	// matchSpansFor the list it was built from (mdedit_blocks.go).
	matchSpans    []mdblocks.Span
	matchSpansFor *matchSpan

	// pendingCaret is a caret move the app is asking the editor for, and
	// pendingCaretOk whether there is one — a separate flag rather than a
	// sentinel, so the zero value means "no request" and not "select the first
	// character".
	pendingCaret   caretRequest
	pendingCaretOk bool

	// numberAction is the outline's stashed numbering gesture, applied at the
	// top of the NEXT frame — the outline renders after the source pane, so a
	// rebind issued from its buttons would miss the frame's databinding
	// override (see mdedit_number.go).
	numberAction numberActionE

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
	// exportCheckpoint marks a clipboard write that exports the DOCUMENT and
	// so advances the dirty checkpoint on success. Copying a proposal rides
	// the same machinery with this false — that text never was the buffer,
	// and moving `saved` for it would clear the dirty badge over nothing.
	exportCheckpoint bool

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

	// One in-flight upload/send gesture at a time (mdedit_sendplay.go).
	// sendGesture names what ran (for the failure line) and sendMsg is the
	// success status, both composed in the goroutine.
	sending     bool
	sendDone    bool
	sendErr     error
	sendGesture string
	sendMsg     string
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
var _ app.OperationsAppI = (*App)(nil)

// Operations serves the catalog for this window (mdedit_ops.go).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

func newApp() (inst *App) {
	inst = &App{
		ids:    c.NewWidgetIdStack(),
		logger: log.Logger,
	}
	// The lading connection is lane-owned: the lane releases what it holds
	// when invalidated, superseded or closed, so nobody else may close it
	// (the lane's own contract).
	inst.files.conn.SetDispose(func(sc *storeConn) {
		if sc != nil {
			sc.close()
		}
	})
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.logger = ctx.Log()
	inst.bus = ctx.Bus()
	inst.store = ctx.Storage()
	if inst.handOver(ctx.LaunchConfig()) {
		return
	}
	if seed := DocSeed.Get(); seed != "" {
		inst.adoptDocument(seed, "a seeded document")
		return
	}
	if inst.store != nil {
		go inst.restore()
	}
	return
}

// handOver opens a document another app handed over (launchcfg,
// ADR-0178's update of 2026-10-03) as a document of its own: the window
// neither restores nor autosaves the one document mdedit keeps, so the
// person's own draft is never overwritten, and the handed text stays
// unsaved until it is saved to a file or copied out. It reports whether
// the window was opened with one.
func (inst *App) handOver(raw []byte) (handed bool) {
	if len(raw) == 0 {
		return false
	}
	l, err := buscodec.Decode[launchcfg.MdeditLaunch](raw)
	if err != nil {
		inst.logger.Warn().Err(err).Msg("mdedit: the launch config does not decode; opening as usual")
		return false
	}
	name := l.Name
	if name == "" {
		name = "a handed-over document"
	}
	inst.adoptDocument(l.Text, name)
	if l.Mount != 0 {
		inst.handedSnap = &handedSnapshot{mount: identifier.TaggedId(l.Mount), snap: time.Unix(0, l.SnapNanos), path: l.Path, name: name, text: l.Text}
		// The snapshot exists whether or not this window can read it.
		inst.snapRef = &snapshotRef{mount: inst.handedSnap.mount, snap: inst.handedSnap.snap}
	}
	return true
}

// adoptDocument makes text this window's document without a store behind
// it: no restore, no autosave, the person's own draft untouched. Shared by
// a handed-over document and a seeded one.
func (inst *App) adoptDocument(text string, name string) {
	inst.store = nil
	inst.src = text
	inst.cancelSnapshotLoad()
	inst.setSnapshotImages(nil)
	inst.snapRef = nil
	inst.status = "opened " + name + " — not autosaved: Save to keep it"
}

// Unmount persists the buffer one last time, synchronously. Every other
// persist runs off the render thread to keep a stalled bus out of the frame
// budget; this one cannot, because a goroutine started here would outlive the
// window and have nothing left to report to. A store that does not answer
// costs the close one request timeout — the trade the other way loses the
// document.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	// End the follow first, synchronously but bounded: the broker-side watch
	// pump is NOT stopped by the host closing this app's bus client — cap
	// revocation silences no pump — so the close must be explicit, and a
	// goroutine started here would race that client teardown.
	inst.mu.Lock()
	f := inst.follow
	inst.follow = followState{}
	inst.mu.Unlock()
	teardownFollow(inst.bus, f.handle, f.unsub)

	// The files pane's lanes: cancel what runs, release the connection the
	// conn lane owns (its dispose closes the stores).
	inst.files.load.Close()
	inst.files.mounts.Close()
	inst.files.conn.Close()

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
	inst.drainFollow()
	inst.drainSnapshotLoad()
	if inst.pollHandedSnapshot() || inst.pollSnapshotImages() {
		c.RequestRepaint()
	}
	// The host repaints reactively; while a follow is live, a low-rate tick
	// keeps frames — and so the drain above — coming for a disk change that
	// arrives with no input to wake the window.
	if inst.followActive {
		c.RequestRepaintAfter(followTickSecs)
	}
	inst.clearDiscardConfirm()
	inst.refreshDerived()
	// Last frame's outline numbering gesture, applied while the parse above
	// still describes the buffer and before any pane has read either.
	inst.applyPendingNumbering()
	// Before any pane reads pendingScroll, so a caret move and an outline
	// click in the same frame resolve in that order — the click wins, which is
	// the one the reader just made.
	inst.trackCaretSection()
	inst.autosave()

	// Probe the window before anything claims it, so the split below is a
	// share of the whole rather than of whatever is left.
	if w, h, ok := c.CapturePaneSize(inst.paneProbeSeq("window")); ok && w > 0 {
		inst.winW = w
		if h > 0 {
			inst.winH = h
		}
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

	// The proposal pane spans the window bottom, declared before the side
	// panels for two reasons: full width (a rendered proposal wants room),
	// and ORDER — an Apply click in it rebinds the buffer, and the rebind's
	// databinding override only lands if the editor's emit comes after it in
	// the same frame.
	if inst.proposalPaneOpen() {
		for range c.PanelBottomInside(inst.ids.PrepareStr("proposalpane")).
			ExactSize(inst.proposalPaneHeight()).KeepIter() {
			inst.renderProposalPane()
		}
	}

	// Panels must be declared before the central region claims what is left.
	// renderSource owns its own scroll area so its pane probe measures the
	// panel rather than the scrolled content.
	//
	// Each view mode only ever SKIPS panels and lets the central region absorb
	// what they released — no width math changes per mode, because every pane
	// derives from the measured window each frame rather than retaining a
	// size, which is what makes mode switching safe (see sourceSplitFrac).
	if inst.viewMode == viewSplit {
		for range c.PanelLeftInside(inst.ids.PrepareStr("sourcepane")).
			ExactSize(inst.sourceWidth()).KeepIter() {
			inst.renderSource()
		}
	}
	// The side panel is declared before the central region too, so an
	// outline click can still reach the preview in the same frame. It stays
	// in every mode — in the source-only one the outline still navigates the
	// caret.
	if inst.sideVisible() {
		for range c.PanelRightInside(inst.ids.PrepareStr("sidepane")).
			ExactSize(inst.sideWidth()).KeepIter() {
			inst.renderSide()
		}
	}
	for range c.PanelCentralInside().KeepIter() {
		if inst.viewMode == viewBlocks {
			inst.renderBlocks()
			continue
		}
		if !inst.previewVisible() {
			// Source-only: the editor takes the central region and with it the
			// full remainder. renderSource is pane-agnostic — it probes
			// whatever pane it renders in.
			inst.renderSource()
			continue
		}
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

// renderBar draws the action rows: document gestures and formatting on top,
// view state below, the replace row under those while its toggle is on, and
// the summary row last. Callers own the enclosing panel.
func (inst *App) renderBar() {
	// Row one — document gestures, then formatting. A format click's snippet
	// is stashed rather than applied: it has to reach the TextEdit as a
	// builder method on the widget itself, which renders later in this same
	// frame.
	for range c.HorizontalTop().KeepIter() {
		// File gestures (M4). Every button renders unconditionally and drops
		// the click while something is in flight, for the same reason the copy
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

		// Upload and send-to-play (mdedit_sendplay.go): the persistence
		// gesture alone, and the same plus a play window on the row. Both
		// rendered unconditionally like their neighbours; ClickHouse being
		// unreachable surfaces in the status, the same posture tally takes.
		uploadClicked, sendClicked := false, false
		for range c.HoverText(tipUpload).KeepIter() {
			uploadClicked = c.Button(inst.ids.PrepareStr("upload"), atomsUpload).SendResp().HasPrimaryClicked()
		}
		for range c.HoverText(tipSendPlay).KeepIter() {
			sendClicked = c.Button(inst.ids.PrepareStr("sendplay"), atomsSendPlay).SendResp().HasPrimaryClicked()
		}
		if !inst.sendInFlight() {
			switch {
			case sendClicked:
				inst.sendDoc(true)
			case uploadClicked:
				inst.sendDoc(false)
			}
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

		// The formatting bar edits through the editor, so it goes with it: a
		// mode that hides the pane hides the gestures aimed at it, and the
		// disappearance is legible because the mode was a deliberate click.
		if inst.editorVisible() {
			c.AddSpace(styletokens.GapInline(styletokens.DensityStandard))
			if snippet := inst.renderFormatBar(); snippet != "" {
				inst.pendingInsert = snippet
			}
		}
	}

	// Row two — view state: the mode segment, the side panel toggle and the
	// find group.
	for range c.HorizontalTop().KeepIter() {
		// The segmented mode control leads the row because it governs what
		// everything to its right applies to.
		for _, m := range viewModes {
			for range c.HoverText(m.tip).KeepIter() {
				if c.Button(inst.ids.PrepareStr(m.key), m.atoms).
					Selected(inst.viewMode == m.mode).SendResp().HasPrimaryClicked() {
					inst.viewMode = m.mode
				}
			}
		}
		// The panel toggle renders even when the window is too narrow to
		// honour it — a control that disappears at some width is harder to
		// understand than one that stays and has no effect — and the tooltip
		// is where that is said.
		inst.renderSideToggle()

		inst.renderFindRow()
	}

	// Row three — replace, present only while its toggle in the find group is
	// on AND the editor renders: rewriting text the reader cannot see is a
	// trap, so read mode suppresses the row without disturbing its toggle.
	// Unlike the side panel it has no width floor to meet: it is a row rather
	// than a column, so a narrow window wraps it instead of starving a pane.
	if inst.find.showReplace && inst.editorVisible() {
		for range c.HorizontalTop().KeepIter() {
			inst.renderReplaceRow()
		}
	}

	// The summary row, last, directly over the content it describes: which
	// document this is and where it came from, whether it is modified, the
	// readout and the status line. A row of its own, so the controls above
	// keep their place whatever the status says.
	for range c.HorizontalTop().KeepIter() {

		// Which file, never where — the broker names the basename in its
		// dialog replies and keeps the path (ADR-0026 §SD7).
		if label, tip, show := inst.fileBadge(); show {
			badge.New(inst.ids.PrepareStr("file"), label).
				Tone(badge.ToneNeutral).Variant(badge.VariantSoft).Size(badge.SizeSm).
				Tooltip(tip).Send()
		}
		// A snapshot document can be mounted outside the app; the window
		// copies the command and runs nothing (ADR-0342 §SD5, proposed).
		if inst.snapRef != nil {
			clicked := false
			for range c.HoverText(tipMountCommand).KeepIter() {
				clicked = c.Button(inst.ids.PrepareStr("mountcmd"), atomsMountCommand).Small().SendResp().HasPrimaryClicked()
			}
			if clicked && !inst.exportInFlight() {
				inst.copyMountCommand()
			}
		}

		label, tone := "no changes", badge.ToneNeutral
		if inst.dirty() {
			label, tone = "modified", badge.ToneWarning
		}
		badge.New(inst.ids.PrepareStr("dirty"), label).
			Tone(tone).Variant(badge.VariantSoft).Size(badge.SizeSm).
			Tooltip(tipDirty).Send()

		// What the follow found while it could not act (mdedit_follow.go):
		// the disk moved under unsaved edits, or the file went away.
		if inst.diskChanged {
			badge.New(inst.ids.PrepareStr("disk"), "changed on disk").
				Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).
				Tooltip(tipDiskChanged).Send()
		}
		if inst.diskGone {
			badge.New(inst.ids.PrepareStr("disk-gone"), "gone on disk").
				Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).
				Tooltip(tipDiskGone).Send()
		}

		for range c.HoverText(tipStats).KeepIter() {
			c.Label(inst.statsLine()).Send()
		}

		if inst.status != "" {
			c.Label(inst.status).Send()
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
	// The colour job's only consumer is the editor, so a mode that hides it
	// skips the build — typing a find query in read mode then costs nothing
	// here. The gate inside ensureHighlight keeps the return path honest: if
	// anything moved while the editor was hidden, the key comparison rebuilds
	// on the first frame it is back.
	if inst.editorVisible() {
		inst.ensureHighlight()
	}
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
	src   string
	query string
	fold  bool
	idx   int
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
		idx: inst.find.idx,
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
	// The pane is formatted (ADR-0334 §SD6): proportional prose, heading
	// lines scaled by level, code and tables monospace. Derived from the
	// same spans as the colours, so the two cannot disagree about where a
	// heading ends, and keyed by the same src through this function's gate.
	secs = codeview.MarkdownFontSections(inst.src, inst.lexSpans, secs)
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
	// Proportional, since the body of the formatted pane is. Heading rows are
	// taller than this and the row count is a floor the scroll area absorbs.
	c.MeasureTextSizeBind(inst.paneProbeSeq("row-w"), inst.paneProbeSeq("row-h"),
		rowProbeText, pt, false, nil, &inst.rowPx)
}

// renderSource emits the editing surface, sized to fill the pane it is given.
// It owns its own scroll area: the probe below has to run in the PANE, before
// anything is placed, and a probe inside the scroll area would be measuring the
// scrolled content instead.
func (inst *App) renderSource() {
	inst.measureRowHeight()
	// Last frame's captured keys, before the editor is emitted: a rebind they
	// issue has to precede the emit to land in this frame's override.
	inst.applyEditorKeys()

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
			ReportCursor().
			// The keyboard the pane answers for (mdedit_keys.go): Enter
			// plainly, Tab, and the formatting letters only under the
			// command modifier.
			CaptureKeys(uint64(editorKeyMask)).
			CaptureTab().
			CaptureKeyChords(uint64(editorChordMask), uint8(keycodes.ModCommand))
		inst.editorFieldId = b.Id()
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
				mdline.ByteToChar(inst.src, r.Start), mdline.ByteToChar(inst.src, r.Stop)), r.Focus)
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
	// markdown.Render opens its own scope under ScopeKey, so the preview and
	// the proposal preview in the same window cannot collide.
	in := markdown.Input{Ids: inst.ids, ScopeKey: "preview", Doc: inst.doc}
	if slug, changed := inst.takeScrollTarget(); changed {
		in.ScrollToSection = slug
	}
	res := markdown.Render(in)
	// A task box clicked in the preview flips in the source: the character
	// after its bracket, same length either way. A rebind from outside the
	// editor, like a replace — not an edit its undo describes.
	for _, t := range res.Tasks {
		if next, ok := toggleTaskAt(inst.src, t.Offset); ok {
			inst.rebindBuffer(next)
		}
	}
}

// toggleTaskAt flips the task box whose bracket sits at off in src.
func toggleTaskAt(src string, off int) (next string, ok bool) {
	i := off + 1
	if off < 0 || i >= len(src) || src[off] != '[' {
		return
	}
	switch src[i] {
	case ' ':
		return src[:i] + "x" + src[i+1:], true
	case 'x', 'X':
		return src[:i] + " " + src[i+1:], true
	}
	return
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

// editorVisible reports whether the current view mode renders the editor.
// Everything that edits the buffer through the editor — the format bar, the
// replace row — gates on it, so a mode never leaves a gesture pointing at a
// pane that is not there.
func (inst *App) editorVisible() (yes bool) {
	yes = inst.viewMode != viewRead && inst.viewMode != viewBlocks
	return
}

// previewVisible reports whether the current view mode renders the preview.
// Blocks mode renders neither pane: the widget is both.
func (inst *App) previewVisible() (yes bool) {
	yes = inst.viewMode != viewSource && inst.viewMode != viewBlocks
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
	inst.doc = markdown.ParseWith([]byte(inst.src), inst.parseOptions())
	inst.docSrc = inst.src
	inst.docOk = true
}

// trackCaretSection resolves the caret's section and, when it has changed
// since last frame, queues a scroll to it. Call once per frame, before
// anything that reads pendingScroll.
//
// It only ever queues on a CHANGE. Queuing every frame would re-issue the
// scroll continuously and pin the preview against the reader's own scrolling,
// which is the guard markdown.Input.ScrollToSection documents.
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

// exportClipboard starts a clipboard write for the current buffer — the
// document export, which checkpoints on success.
func (inst *App) exportClipboard() {
	inst.copyToClipboard(inst.src, true)
}

// mountCommandDir is the mount point the copied command names, for the
// reader to replace.
const mountCommandDir = "/mnt/x"

// copyMountCommand copies the rclone command that mounts the document's
// snapshot read-only: that snapshot, not `latest`, so the folder holds what
// the window was given.
func (inst *App) copyMountCommand() {
	if cmd, ok := inst.mountCommand(); ok {
		inst.copyToClipboard(cmd, false)
	}
}

// mountCommand is that command; ok is false for a document not read from a
// snapshot.
func (inst *App) mountCommand() (cmd string, ok bool) {
	r := inst.snapRef
	if r == nil {
		return "", false
	}
	return sftptree.RcloneMount(r.mount, r.snap, mountCommandDir), true
}

// copyToClipboard starts a clipboard write. The request runs off the render
// thread on purpose: bus.Request blocks until the broker answers or the
// request times out, so a host running without a clipboard service would
// otherwise freeze the frame for the whole timeout. checkpoint says whether a
// success advances `saved` — see exportCheckpoint.
func (inst *App) copyToClipboard(text string, checkpoint bool) {
	if inst.bus == nil {
		inst.status = "clipboard unavailable"
		return
	}
	inst.mu.Lock()
	inst.exporting = true
	inst.mu.Unlock()
	go func() {
		_, err := inst.bus.Request(clipboardbroker.SubjectWrite, []byte(text))
		inst.mu.Lock()
		inst.exporting = false
		inst.exportDone = true
		inst.exportErr = err
		inst.exportedText = text
		inst.exportCheckpoint = checkpoint
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
	expCheckpoint := inst.exportCheckpoint
	inst.exportDone = false
	perDone, perErr, perText := inst.persistDone, inst.persistErr, inst.persistedText
	inst.persistDone = false
	fileDone, fileRes := inst.fileDone, inst.fileRes
	inst.fileDone = false
	sendDone, sendErr := inst.sendDone, inst.sendErr
	sendGesture, sendMsg := inst.sendGesture, inst.sendMsg
	inst.sendDone = false
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
			// Only a DOCUMENT export checkpoints; a result copy does not.
			if expCheckpoint {
				inst.saved = expText
			}
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
	if sendDone {
		if sendErr != nil {
			inst.status = sendGesture + " failed: " + sendErr.Error()
			inst.logger.Warn().Err(sendErr).Str("gesture", sendGesture).Msg("mdedit: document gesture failed")
		} else {
			inst.status = sendMsg
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
