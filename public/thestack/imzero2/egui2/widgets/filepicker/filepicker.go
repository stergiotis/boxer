// Package filepicker is a semi-retained imzero2 widget (ADR-0267): an in-app
// file open / save / pick-folder dialog rendered as an egui::Window. The directory tree is walked Go-side via
// the stdlib [io/fs.FS] interface; the default backend is
// [os.DirFS]("/"), but callers can pass any fs.FS —
// [testing/fstest.MapFS] for tests, [os.DirFS](root) for sandboxed
// roots, [embed.FS] for static content, or a custom remote backend.
//
// The dialog is a shell around the fsbrowser widget (ADR-0200, 2026-09-18
// update): breadcrumb, quick filter, listing, sort, selection and keys
// are the widget's, in list mode; the window, the modes, the filename
// row, the stat pane and what a commit returns are this package's.
//
// One [Dialog] models one dialog. Hosts construct it once with [New],
// handing over their *WidgetIdStack, a scope key and an [Options], call
// [Dialog.Show] to make it visible, and call [Dialog.Render] every frame
// inside their render loop. The dialog auto-hides on Open / Save /
// PickFolder / Cancel; the host reads the [Events] Render returns — the
// action and (zero or more) paths — to drive whatever follows. [Dialog.Opts]
// is re-read every frame, so a host changes a knob by assignment.
//
// # Modes
//
// [ModeOpen] picks an existing file; a double click or Enter on a file
// commits it. The right-side stat pane shows metadata for the active
// selection. With [Options.MultiSelect] enabled, ctrl-click toggles a file
// in/out of the commit set and shift-click extends it — commit returns
// all picked paths in the order picked. [ModeSave] asks for a
// destination: the user types a filename (or clicks an existing file to
// take its name) and commit returns cwd-joined-name. [ModePickFolder]
// picks a directory: files are hidden from the listing, the primary
// button reads "Pick This Folder", and commit returns the one selected
// directory, or the cwd when none is selected. In every mode a
// directory is entered by a double click or Enter; a single click
// selects it.
//
// State that survives frames (the browser's State — cwd, selection,
// listing cache — and the filename buffer) lives on Dialog. Render opens
// one [bindings.IdScope] keyed on the scope key given to New, so two
// dialogs on one ids stack differ by scope key alone; the window itself
// carries the one absolute id the dialog owns, derived from that scope.
//
// Visibility is owned entirely by Go. There is no [X] close button on
// the Window — the framework's egui::Window is constructed without an
// `&mut bool open`, so egui never paints one. Cancel / Open / Save are
// the only ways to dismiss the dialog.
//
// # Path semantics
//
// Internally the picker uses io/fs paths: forward slashes only, no
// leading "/", no "..", and "." for the FS root. Most hosts want a
// rendered path with a leading "/" (or some other prefix); use
// [Options.DisplayRoot] to set that — it's prepended to the path returned
// by Render's commit. [Options.StartAtOsHome] resolves [os.UserHomeDir] as
// the starting cwd and, if no display root is set, implies a "/" display
// root, matching the conventional OS dialog experience.
//
// # Column widths
//
// The listing's name column takes the width the size and modified
// columns leave, so the table spans the dialog; a dragged column edge
// takes from the column to its right. With [Options.ColumnWidths] (which
// may also be assigned later through [Dialog.Opts]) a drag is kept through the standard
// column-width persistence (ADR-0151): build
// the resolver with [NewColumnWidths], share it between every dialog
// the host shows, and flush it once per frame. Without one the dialog
// renders at its defaults and nothing persists.
//
// # Stat pane (open mode)
//
// In open mode the dialog grows a right-side pane showing the
// currently-selected file's metadata: size (humanized via
// dustin/go-humanize), permission bits, and modification time
// (relative + absolute). The pane is populated by [io/fs.Stat] and
// cached per selection — switching files re-stats once, then reads
// from cache until the user picks a different file. Save mode has no
// stat pane (the user is typing a new filename, not inspecting an
// existing one).
package filepicker

import (
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/dustin/go-humanize"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsbrowser"
)

// AppId is the identity the dialog acts under: its column widths are stored
// under it, and its searches are tasks of it. The dialog is the host's, not an
// app's: whichever app's request raised it, it is one dialog to its user, so
// it has a synthetic id of its own, the way the fs broker is "runtime.fs".
const AppId app.AppIdT = "runtime.filepicker" // designlint:ignore=L12 (a host-owned dialog, named like the runtime services; widths are stored under it)

// NewColumnWidths builds the resolver a host hands to its dialogs through
// [Options.ColumnWidths], over the host's column-width store (its state store
// satisfies it), and loads what is stored. It exists so the two things a
// call site can get wrong are decided here: the identity, and the bounds —
// which must be the ones the browser drags against, or a stored width and a
// dragged one disagree (ADR-0151).
//
// res is nil only when the resolver could not be built. A failed load
// returns the resolver with the error: the dialog then starts from its
// defaults and still captures.
func NewColumnWidths(store colwidth.StoreI) (res *colwidth.Resolver, err error) {
	res, err = colwidth.New(store, colwidth.Opts{
		AppId:     AppId,
		MinPoints: float64(fsbrowser.MinColumnWidth(styletokens.ActiveDensity())),
		MaxPoints: float64(fsbrowser.MaxColumnWidth),
	})
	if err != nil {
		res = nil
		return
	}
	err = res.Load()
	return
}

// fullWidth tells egui TextEdit to fill the available horizontal space
// in its parent layout. Mirrors regex_explorer.go's convention.
var fullWidth = float32(math.Inf(1))

// ModeE selects what the dialog asks for: an existing file (open), a
// new/replacement file path (save), or a directory (pick-folder).
type ModeE uint8

const (
	ModeOpen       ModeE = 0
	ModeSave       ModeE = 1
	ModePickFolder ModeE = 2
)

// ActionE is the user's resolution of one Render call. Non-commit frames
// return ActionNone; commit frames hide the dialog and emit one of
// ActionOpen / ActionSave / ActionPickFolder / ActionCancel.
type ActionE uint8

const (
	ActionNone       ActionE = 0
	ActionOpen       ActionE = 1
	ActionSave       ActionE = 2
	ActionCancel     ActionE = 3
	ActionPickFolder ActionE = 4
)

// String renders the action as a stable telemetry token.
func (inst ActionE) String() (s string) {
	switch inst {
	case ActionNone:
		s = "none"
	case ActionOpen:
		s = "open"
	case ActionSave:
		s = "save"
	case ActionCancel:
		s = "cancel"
	case ActionPickFolder:
		s = "pick-folder"
	default:
		s = "<invalid>"
	}
	return
}

// Options configures a dialog. Pass it to [New]; it stays on [Dialog.Opts],
// which Render re-reads every frame, so a knob changes by assignment. The
// zero value is a plain open dialog over os.DirFS("/") rooted at the FS
// root. Fields that seed persistent state — StartDir / StartAtOsHome (read
// on the first Show), ShowHidden and DefaultFilename (read at New) — set the
// starting point only; the user moves on from there.
type Options struct {
	// Mode selects what the dialog asks for; zero is [ModeOpen].
	Mode ModeE
	// Title is the window title; empty uses the mode's default ("Open",
	// "Save", "Pick folder").
	Title string
	// Modal draws the dialog as a modal instead of a window: centred over a
	// backdrop that blocks the rest of the viewport until the person picks
	// or cancels, at the mode's default size, with the title as a heading.
	// For a pick that grants something, so no window can cover it.
	Modal bool
	// StartDir is the initial cwd, an io/fs path — forward slashes only, no
	// leading "/", "." for the FS root. Empty means the FS root. Read on the
	// first Show.
	StartDir string
	// StartAtOsHome resolves the user's home directory via [os.UserHomeDir]
	// and starts there instead of StartDir. Only meaningful with the default
	// os.DirFS("/") backend (or one rooted at the OS root) — the absolute
	// home path becomes an io/fs path by stripping the leading "/". For the
	// conventional "/home/..." reading it also implies a "/" DisplayRoot
	// when none is set, so committed paths come back OS-absolute. On error
	// or an empty home the dialog starts at StartDir.
	StartAtOsHome bool
	// DisplayRoot is prepended to the path a commit returns:
	//
	//   - "" (default) — raw io/fs paths (or "/" under StartAtOsHome).
	//   - "/"          — OS-absolute paths, suitable for [os.DirFS]("/").
	//   - "/sandbox"   — paths rooted at the sandbox, for an anchored backend.
	//
	// A trailing "/" is tolerated and stripped.
	DisplayRoot string
	// StatPaneWidth is the default width (logical pixels) of the open-mode
	// stat pane; 0 takes 240. The pane is resizable; this is only the
	// initial size. Ignored outside open mode.
	StatPaneWidth float32
	// Extensions restricts visible files to those whose suffix matches any
	// of them (case-insensitive, leading dot optional). Directories always
	// show. Globs and Filter, when set, take precedence over Extensions.
	Extensions []string
	// Globs restricts visible files by [path.Match] pattern, OR-combined:
	// "*.go", "test_*.go", "?akefile". Patterns see the basename only — "*"
	// never crosses "/"; for path-aware filters use Filter. Malformed
	// patterns are skipped. Directories always show. Filter, when set,
	// takes precedence over Globs.
	Globs []string
	// Filter is an arbitrary visibility predicate for non-directory
	// entries: true keeps the entry. Called for every cwd child each frame,
	// so keep it cheap. It receives an [fs.DirEntry] view of the browser's
	// cached entry — Type and Info answer from the listing without another
	// stat — carrying the name alone. Set, it outranks Globs and Extensions.
	Filter func(fs.DirEntry) bool
	// FilterDesc labels Filter in the footer as `filter: <desc>`; empty
	// shows no label. Extensions and Globs label themselves.
	FilterDesc string
	// ShowHidden seeds the runtime "show hidden" toggle for dot-prefixed
	// names; the user flips it from the footer Checkbox. Read at New.
	ShowHidden bool
	// FS is the filesystem to browse: [testing/fstest.MapFS] for tests,
	// [os.DirFS](root) for sandboxed paths, [embed.FS] for static content.
	// nil takes os.DirFS("/"). Set it before the first Show; the browser
	// caches listings against it.
	FS fs.FS
	// DefaultFilename pre-fills the save-mode filename input; read at New.
	// [Dialog.SetFilename] changes it per invocation.
	DefaultFilename string
	// MultiSelect lets the user accumulate several files into one commit
	// (ctrl-click toggles, shift-click extends); commit emits every
	// selected file in the order picked. Only meaningful in [ModeOpen].
	MultiSelect bool
	// ColumnWidths persists the listing's column widths (ADR-0151): the host
	// builds it once with [NewColumnWidths] and flushes it once per frame —
	// every frame, not only while a dialog is open. nil persists nothing.
	ColumnWidths *colwidth.Resolver
	// Tasks publishes the quick filter's search as a keelson background
	// task (ADR-0038), so the host's task monitor lists it and can cancel
	// it; built under [AppId] with the task producer caps. nil keeps the
	// search the dialog's own, still off the render thread.
	Tasks task.TaskApiI
}

// mode is the dialog's mode.
func (inst *Dialog) mode() ModeE { return inst.Opts.Mode }

// title is the window title, the mode's default when Opts.Title is empty.
func (inst *Dialog) title() (t string) {
	if t = inst.Opts.Title; t != "" {
		return
	}
	switch inst.mode() {
	case ModeSave:
		return "Save"
	case ModePickFolder:
		return "Pick folder"
	}
	return "Open"
}

// fsys is the filesystem to browse, os.DirFS("/") when Opts.FS is nil.
func (inst *Dialog) fsys() fs.FS {
	if inst.Opts.FS != nil {
		return inst.Opts.FS
	}
	if inst.defaultFS == nil {
		inst.defaultFS = os.DirFS("/")
	}
	return inst.defaultFS
}

// statPaneWidth is the stat pane's default width, 240 when unset.
func (inst *Dialog) statPaneWidth() float32 {
	if inst.Opts.StatPaneWidth > 0 {
		return inst.Opts.StatPaneWidth
	}
	return 240
}

// displayRoot is the prefix a commit prepends: Opts.DisplayRoot, or "/"
// under StartAtOsHome when none is set.
func (inst *Dialog) displayRoot() string {
	if inst.Opts.DisplayRoot == "" && inst.Opts.StartAtOsHome {
		return "/"
	}
	return inst.Opts.DisplayRoot
}

// startDir is the cwd the first Show resolves: the OS home under
// StartAtOsHome (falling back to StartDir when it cannot be resolved), else
// StartDir, else the FS root.
func (inst *Dialog) startDir() (dir string) {
	if inst.Opts.StartAtOsHome {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return strings.TrimPrefix(home, "/")
		}
	}
	if dir = inst.Opts.StartDir; dir == "" {
		dir = "."
	}
	return
}

// filterPred is the visibility predicate for non-directory entries — Filter,
// else Globs, else Extensions — and nil when none is set (everything
// passes). Compiled from Opts each call; the closures are cheap.
func (inst *Dialog) filterPred() func(fs.DirEntry) bool {
	if inst.Opts.Filter != nil {
		return inst.Opts.Filter
	}
	if clean := cleanGlobs(inst.Opts.Globs); len(clean) > 0 {
		return func(de fs.DirEntry) bool {
			if de.IsDir() {
				return true
			}
			name := de.Name()
			for _, p := range clean {
				match, err := path.Match(p, name)
				if err == nil && match {
					return true
				}
			}
			return false
		}
	}
	if norm := normalizeExtensions(inst.Opts.Extensions); len(norm) > 0 {
		return func(de fs.DirEntry) bool {
			return passesExtFilter(de, norm)
		}
	}
	return nil
}

// filterDesc is the footer's `filter: <desc>` label for the predicate
// filterPred picks, empty for none.
func (inst *Dialog) filterDesc() string {
	if inst.Opts.Filter != nil {
		return inst.Opts.FilterDesc
	}
	if clean := cleanGlobs(inst.Opts.Globs); len(clean) > 0 {
		return strings.Join(clean, " ")
	}
	if norm := normalizeExtensions(inst.Opts.Extensions); len(norm) > 0 {
		return strings.Join(norm, " ")
	}
	return ""
}

// cleanGlobs drops blank patterns.
func cleanGlobs(patterns []string) (clean []string) {
	for _, p := range patterns {
		if p = strings.TrimSpace(p); p != "" {
			clean = append(clean, p)
		}
	}
	return
}

// Dialog is one file dialog. Construct it with [New]; the zero value has no
// id stack and cannot render.
//
// Dialog is not safe for concurrent use; restrict access to the host's
// single render-loop goroutine.
type Dialog struct {
	// Opts is the configuration, re-read every frame (ADR-0267 W11).
	Opts Options

	// ids and scopeKey are the host's id stack and this dialog's scope under
	// it; every child id is derived under IdScope(scopeKey) at Render.
	ids      *c.WidgetIdStack
	scopeKey string
	// defaultFS is the os.DirFS("/") fsys() lends when Opts.FS is nil, built
	// once.
	defaultFS fs.FS

	// showHidden mirrors the footer Checkbox: when false, dot-prefixed
	// names (POSIX hidden convention) are dropped from the listing. Seeded
	// from Opts.ShowHidden at New; flipped at runtime by the user. Applies
	// to dirs too — `.git/`, `.cache/`, and similar hide.
	showHidden bool

	// Mutable state, persists across frames. The browser state holds
	// where the dialog is: current directory, listing cache, selection,
	// cursor, sort and quick filter (ADR-0200, 2026-09-18 update). It
	// is bound by pointer into the render loop, so it lives here and
	// Dialog is only ever handled by pointer.
	open    bool
	started bool
	st      fsbrowser.State
	// picked is the selected files in the order they were picked, which
	// is what a multi-select commit returns; the browser's own selection
	// is a set. pickedDir is the one selected directory, when the
	// selection is exactly that — what pick-folder commits instead of
	// the cwd. selected is the file the cursor is on: it drives the stat
	// pane and the "selected: …" footer label. All three are derived
	// from the browser's selection by syncPicks.
	picked    []string
	pickedDir string
	selected  string
	filename  string
	// browserH is the central panel's measured height, a frame late and
	// held across frames; zero until the first measurement.
	browserH float32

	// Stat cache for the currently-selected file (open mode only).
	// selectedStatPath is the cache key — if it equals inst.selected,
	// selectedInfo / selectedStatErr are valid; otherwise refreshStat
	// re-stats once and refreshes them.
	selectedInfo     fs.FileInfo
	selectedStatErr  error
	selectedStatPath string
}

// Events is what one Render reports. Non-commit frames carry ActionNone and
// no paths.
type Events struct {
	// Action is what the user did this frame: nothing, a commit in the
	// dialog's mode, or a cancel. The dialog has hidden itself on anything
	// but ActionNone.
	Action ActionE
	// Paths are the committed paths with the display root applied (raw io/fs
	// paths by default; OS-absolute under StartAtOsHome or a "/" display
	// root): one for a single-pick open, save or pick-folder; one or more,
	// in the order picked, for a multi-select open; none on cancel.
	Paths []string
}

// New constructs a dialog under the host's id stack, scoped by scopeKey (two
// dialogs on one stack differ by it alone; empty uses "filepicker"). The
// instance starts hidden; call [Dialog.Show] before the next Render to make
// it visible.
func New(ids *c.WidgetIdStack, scopeKey string, opts Options) (inst *Dialog) {
	if scopeKey == "" {
		scopeKey = "filepicker"
	}
	inst = &Dialog{
		Opts:       opts,
		ids:        ids,
		scopeKey:   scopeKey,
		showHidden: opts.ShowHidden,
		filename:   opts.DefaultFilename,
	}
	return
}

// Show requests the dialog to be drawn from the next frame onwards.
// First-Show resolves the initial cwd; subsequent Show calls leave cwd
// in place. Either way the cached listings are dropped: the browser
// caches a directory until told otherwise, and the tree behind a dialog
// is a live one that changed while the dialog was away.
//
// Idempotent on an already-visible dialog.
func (inst *Dialog) Show() {
	if inst.open {
		return
	}
	inst.open = true
	if !inst.started {
		inst.started = true
		inst.st.SetDir(inst.startDir())
	}
	inst.st.Invalidate()
}

// Hide closes the dialog without emitting an action. The selection is
// cleared; cwd, filename buffer, sort and quick filter are preserved so
// the next Show resumes where the user left off.
func (inst *Dialog) Hide() {
	inst.open = false
	inst.clearSelection()
	// Nobody renders a hidden dialog, so nobody would see its search
	// through or cancel it.
	inst.st.StopSearch()
}

// clearSelection wipes the browser's selection and what the dialog
// derives from it. Navigation needs no call: the browser clears its
// selection on a directory change and syncPicks follows.
func (inst *Dialog) clearSelection() {
	inst.st.ClearSelection()
	inst.picked = inst.picked[:0]
	inst.pickedDir = ""
	inst.selected = ""
}

// IsOpen reports whether the dialog is currently visible.
func (inst *Dialog) IsOpen() (open bool) {
	open = inst.open
	return
}

// SetFilename overwrites the save-mode filename buffer. Useful for
// per-invocation suggestions ("alice_pushoutgraph.dot" vs "bob_pushoutgraph.dot")
// where [Options.DefaultFilename] — read at New — is too coarse.
// No-op in open mode (the field is unused there). Safe to call at any
// time; takes effect on the next Render frame.
func (inst *Dialog) SetFilename(name string) {
	inst.filename = name
}

// widthTag names the dialog's table for the resolver's instance tier. It is
// the mode, not the instance: a host may mint a dialog per request, and a
// width dragged in one open dialog is wanted in the next.
func (inst *Dialog) widthTag() (tag string) {
	_, action := primaryButtonFor(inst.mode())
	tag = "filepicker/" + action.String()
	return
}

// Render draws the dialog this frame and reports what the user did. Non-commit
// frames return ActionNone with no paths; the host keeps calling Render every
// frame. See [Events] for what a commit carries. The dialog auto-hides on
// commit or cancel — the host does not need to call Hide.
//
// Render opens one IdScope keyed by the scope given to New, so sub-widgets
// are identified per dialog regardless of the outer scope the host has
// pushed; the window carries the one absolute id the dialog owns, derived
// from that scope (ADR-0267 W6).
func (inst *Dialog) Render() (ev Events) {
	if !inst.open {
		return
	}
	inst.refreshStat()
	ids := inst.ids

	// Default window size by mode: ModeOpen hosts the stat pane on the
	// right (needs more horizontal room); ModeSave and ModePickFolder
	// have no stat pane and stay compact.
	defaultW, defaultH := float32(640), float32(480)
	if inst.mode() == ModeOpen {
		defaultW, defaultH = 820, 500
	}

	// The window id is derived under the dialog's scope, which is opened
	// for the derivation alone here and emits nothing.
	var winId c.AbsoluteWidgetId
	for range c.IdScope(ids.PrepareStr(inst.scopeKey)) {
		winId = c.MakeAbsoluteIdHighEntropy(ids.PrepareStr("window").Derive())
	}

	if inst.Opts.Modal {
		for range c.Modal(winId).KeepIter() {
			// A modal is sized by its body and cannot be resized: the
			// body gets the window's default size as a fixed box.
			c.UiSetMinWidth(defaultW)
			c.UiSetMaxWidth(defaultW)
			c.UiSetMinHeight(defaultH)
			c.UiSetMaxHeight(defaultH)
			// The title a window carries in its bar, as a panel of its
			// own: the body is laid out in panels, which would otherwise
			// take the whole box and draw over it.
			var titleId c.WidgetIdCreatorI
			for range c.IdScope(ids.PrepareStr(inst.scopeKey)) {
				titleId = c.MakeAbsoluteIdHighEntropy(ids.PrepareStr("modal-title").Derive())
			}
			for range c.PanelTopInside(titleId).Resizable(false).KeepIter() {
				for rt := range c.RichTextLabel(inst.title()) {
					rt.Heading()
				}
			}
			ev = inst.renderScoped(ids)
		}
		return
	}
	label := c.WidgetText().Text(inst.title()).Keep()
	for range c.Window(winId, label).
		Resizable(true).
		Collapsible(false).
		TitleBar(true).
		DefaultOpen(true).
		DefaultSize(defaultW, defaultH).
		MinWidth(420).
		MinHeight(320).
		KeepIter() {
		ev = inst.renderScoped(ids)
	}
	return
}

// renderScoped is the dialog's body under its scope, and what the person
// did; a commit or a cancel closes the dialog.
func (inst *Dialog) renderScoped(ids *c.WidgetIdStack) (ev Events) {
	for range c.IdScope(ids.PrepareStr(inst.scopeKey)) {
		ev.Action, ev.Paths = inst.renderBody(ids)
		if ev.Action != ActionNone {
			inst.open = false
			// Commit consumes the user's intent — wipe the
			// selection so a subsequent Show doesn't re-highlight
			// the prior pick. Cancel leaves state intact so an
			// accidental Cancel + re-Show resumes where the user
			// was. commitPaths has already been pulled into paths.
			if ev.Action != ActionCancel {
				inst.clearSelection()
			}
			inst.st.StopSearch()
		}
	}
	return
}

// refreshStat repopulates inst.selectedInfo / selectedStatErr when
// inst.selected has changed since the last call. Called once per
// frame at the top of Render. fs.Stat is a syscall (or equivalent)
// per backend, so the cache-key check matters — without it we'd
// re-stat on every frame that the picker is open. The listing's own
// entry is not used: it reports a symlink as the link, and the pane
// describes what Open would open.
func (inst *Dialog) refreshStat() {
	if inst.selected == "" {
		inst.selectedInfo = nil
		inst.selectedStatErr = nil
		inst.selectedStatPath = ""
		return
	}
	if inst.selectedStatPath == inst.selected {
		return
	}
	info, err := fs.Stat(inst.fsys(), inst.selected)
	inst.selectedInfo = info
	inst.selectedStatErr = err
	inst.selectedStatPath = inst.selected
}

// renderBody draws the dialog interior inside the Window+IdScope scope.
//
// Layout uses the panel pattern from regex_explorer:
// PanelBottomInside pins the footer (and, in save mode, the filename
// row above it) to the bottom, and PanelCentralInside fills whatever's
// left for the browser. egui's panel system auto-splits the available
// rect — a Vertical layout would not, since Vertical's children grow
// with content and the listing would push everything below it off
// the bottom of the Window.
//
// Bottom panels stack from the bottom edge inward in declaration
// order — the FIRST PanelBottomInside sits at the very bottom, so
// the footer is declared before the (optional) filename row.
func (inst *Dialog) renderBody(ids *c.WidgetIdStack) (action ActionE, paths []string) {
	for range c.PanelBottomInside(ids.PrepareStr("footer-panel")).
		Resizable(false).KeepIter() {
		action, paths = inst.renderFooter(ids)
	}
	if inst.mode() == ModeSave {
		for range c.PanelBottomInside(ids.PrepareStr("fname-panel")).
			Resizable(false).KeepIter() {
			inst.renderFilenameRow(ids)
		}
	}

	if inst.mode() == ModeOpen {
		// Right panel must be declared BEFORE the central panel so
		// the central panel sees the right slice already removed
		// from its available rect. Declared AFTER the bottom panels
		// so the stat pane spans only the middle band, not the
		// full window height. ModePickFolder skips the stat pane
		// (commit is a directory; per-entry metadata isn't actionable).
		for range c.PanelRightInside(ids.PrepareStr("stat-panel")).
			DefaultSize(inst.statPaneWidth()).
			Resizable(true).
			KeepIter() {
			inst.renderStatPane()
		}
	}

	for range c.PanelCentralInside().KeepIter() {
		if a, p := inst.renderBrowser(ids); action == ActionNone {
			action, paths = a, p
		}
	}
	return
}

// renderBrowser draws the breadcrumb, the quick filter and the listing —
// all of it the fsbrowser widget in list mode — and turns what the widget
// reports into the dialog's terms: a navigation drops the cached
// listings, a selection change re-derives the picks, a click on a file
// in save mode offers its name, and an activated file (double click or
// Enter) commits in open mode.
//
// This runs after the footer and the filename row, which panels declare
// first, so what it derives is what they show next frame.
func (inst *Dialog) renderBrowser(ids *c.WidgetIdStack) (action ActionE, paths []string) {
	// The probe reports the room left for the next widget, so it goes
	// before the browser; the answer is a frame late, hence held.
	if _, h, ok := c.CapturePaneSize(ids.ProbeSeq("browser")); ok && h > 0 {
		inst.browserH = h
	}
	rootLabel := inst.displayRoot()
	if rootLabel == "" {
		rootLabel = "/"
	}
	res := fsbrowser.Render(fsbrowser.Input{
		Ids:          ids,
		ScopeKey:     "browser",
		FS:           inst.fsys(),
		RootLabel:    rootLabel,
		State:        &inst.st,
		Mode:         fsbrowser.ModeList,
		ShowHidden:   inst.showHidden,
		Keep:         inst.keep,
		SingleSelect: !(inst.Opts.MultiSelect && inst.mode() == ModeOpen),
		MaxHeight:    inst.browserH,
		FillWidth:    true,
		Widths:       inst.Opts.ColumnWidths,
		WidthTag:     inst.widthTag(),
		Tasks:        inst.Opts.Tasks,
	})
	if res.Navigated {
		// The widget caches a listing until told otherwise, which suits
		// a snapshot; behind a dialog is a live tree.
		inst.st.Invalidate()
	}
	if res.Navigated || res.SelectionChanged {
		inst.syncPicks(res.Rows)
	}
	if inst.mode() == ModeSave {
		// Clicking an existing file offers its name, the usual way to
		// save over it or to start from it.
		if row := max(res.Clicked, res.Activated); row >= 0 && row < len(res.Rows) && !res.Rows[row].IsDir {
			inst.filename = res.Rows[row].Name
			// The filename row registered its binding earlier this
			// frame; without the override the frontend's buffer would
			// write the old text back over the new.
			c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.filename)
		}
		return
	}
	if inst.mode() == ModeOpen && res.Activated >= 0 && res.Activated < len(res.Rows) {
		p := res.Rows[res.Activated].Path
		if !slices.Contains(inst.picked, p) {
			if !inst.Opts.MultiSelect {
				inst.picked = inst.picked[:0]
			}
			inst.picked = append(inst.picked, p)
		}
		action = ActionOpen
		paths = inst.commitPaths()
	}
	return
}

// keep is the browser's row predicate: pick-folder lists directories
// only, and the host's filter has the final say on non-directory
// entries. Directories always bypass the filter so users can still
// navigate into a tree whose leaves the filter would reject. Hidden
// names are the browser's own concern (its ShowHidden).
func (inst *Dialog) keep(e fsbrowser.Entry) (ok bool) {
	if inst.mode() == ModePickFolder && !e.IsDir {
		return
	}
	pred := inst.filterPred()
	if pred == nil || e.IsDir {
		ok = true
		return
	}
	ok = pred(entryAsDirEntry{e: e})
	return
}

// syncPicks re-derives picked, pickedDir and selected from the browser's
// selection. rows is what the browser showed this frame; it says which
// selected paths are directories.
func (inst *Dialog) syncPicks(rows []fsbrowser.Entry) {
	sel := inst.st.Selection()
	dirs := make(map[string]bool, len(sel))
	for i := range rows {
		if inst.st.IsSelected(rows[i].Path) {
			dirs[rows[i].Path] = rows[i].IsDir
		}
	}
	inst.picked = reconcilePicks(inst.picked, sel, dirs)
	inst.pickedDir = ""
	if len(sel) == 1 && dirs[sel[0]] {
		inst.pickedDir = sel[0]
	}
	inst.selected = ""
	if cur := inst.st.Cursor(); cur != "" && slices.Contains(inst.picked, cur) {
		inst.selected = cur
	}
}

// reconcilePicks brings the ordered picks in line with a selection: a
// pick no longer selected goes, a newly selected file is appended — the
// order of first selection is the order a multi-select commit returns.
// dirs says, for the selected paths the listing shows, whether each is
// a directory; a directory is never a pick, and a selected path the
// listing no longer shows (a quick filter took its row) stays a pick if
// it was one and is not made one otherwise. sel is sorted, so files
// selected in one gesture (a shift-click range) are appended in listing
// order by path.
func reconcilePicks(prev []string, sel []string, dirs map[string]bool) (out []string) {
	selected := make(map[string]struct{}, len(sel))
	for _, p := range sel {
		selected[p] = struct{}{}
	}
	out = prev[:0]
	for _, p := range prev {
		if _, ok := selected[p]; ok {
			out = append(out, p)
			delete(selected, p)
		}
	}
	for _, p := range sel {
		if _, fresh := selected[p]; !fresh {
			continue
		}
		if isDir, shown := dirs[p]; shown && !isDir {
			out = append(out, p)
		}
	}
	return
}

// renderStatPane draws the open-mode right pane with metadata for the
// currently-selected file. Reads from the cache populated by
// refreshStat earlier in the frame; never re-stats here.
//
// States:
//   - no selection           → "Select a file to see details"
//   - stat error             → "stat failed: <err>"
//   - info available         → name (bold) + size + mode + mtime
func (inst *Dialog) renderStatPane() {
	for range c.Vertical().KeepIter() {
		switch {
		case inst.selected == "":
			c.Label("Select a file to see details").Send()
			return
		case inst.selectedStatErr != nil:
			c.Label("stat failed: " + inst.selectedStatErr.Error()).Send()
			return
		case inst.selectedInfo == nil:
			// refreshStat ran without populating; treat as loading.
			c.Label("(loading…)").Send()
			return
		}

		info := inst.selectedInfo
		for rt := range c.RichTextLabel(path.Base(inst.selected)) {
			rt.Strong()
		}
		c.Separator().Horizontal().Send()

		// IEC units, as the listing's size column shows them.
		size := max(info.Size(), 0)
		c.Label("Size: " + humanize.IBytes(uint64(size))).Send()
		c.Label("Mode: " + info.Mode().String()).Send()

		c.Label("Modified:").Send()
		mt := info.ModTime()
		c.Label("  " + humanize.Time(mt)).Send()
		c.Label("  " + mt.Format("2006-01-02 15:04:05")).Send()
	}
}

// renderFilenameRow draws the save-mode filename input. The TextEdit's
// SendRespVal-bound buffer carries a one-frame lag (the framework's
// FFFI databindings reset every Sync), but pressing Save in the same
// frame as the last keystroke commits whatever's in inst.filename — the
// most recently synced value. In practice users pause before clicking
// Save, so the lag is invisible.
func (inst *Dialog) renderFilenameRow(ids *c.WidgetIdStack) {
	for range c.Horizontal().KeepIter() {
		c.Label("File name:").Send()
		c.TextEdit(ids.PrepareStr("fname"), inst.filename, false).
			DesiredWidth(fullWidth).
			HintText("filename.ext").
			SendRespVal(&inst.filename)
	}
}

// renderFooter draws the bottom-row controls: a status label on the
// left (filter summary or selection preview) and the Cancel + primary
// (Open / Save / Pick This Folder) buttons right-aligned.
//
// Right-alignment uses UiWithLayout.MainDirRightToLeft. In RTL main
// direction the FIRST drawn child appears rightmost — so emit primary
// before Cancel to get the conventional `Cancel` `Open` reading order.
func (inst *Dialog) renderFooter(ids *c.WidgetIdStack) (action ActionE, paths []string) {
	for range c.Horizontal().KeepIter() {
		// "Hidden" toggle lives on the left edge so it groups visually
		// with the filter/status label rather than the commit buttons.
		// SendRespVal is gated on .changed() (egui Checkbox semantics),
		// so the value updates only on actual clicks — no per-frame
		// thrash. One-frame lag, like every r9_* binding.
		c.Checkbox(ids.PrepareStr("show-hidden"), inst.showHidden, "Hidden").
			SendRespVal(&inst.showHidden)
		inst.renderFooterStatus()

		for range c.UiWithLayout().MainDirRightToLeft().KeepIter() {
			primaryLabel, primaryAction := primaryButtonFor(inst.mode())

			canCommit := inst.canCommit()
			primaryAtoms := c.Atoms().Text(primaryLabel).Keep()
			for range c.EnabledUi(canCommit).KeepIter() {
				if c.Button(ids.PrepareStr("primary"), primaryAtoms).
					Kind(c.ButtonKindPrimary).
					SendResp().HasPrimaryClicked() {
					action = primaryAction
					paths = inst.commitPaths()
				}
			}

			if c.Button(ids.PrepareStr("cancel"), c.Atoms().Text("Cancel").Keep()).
				SendResp().HasPrimaryClicked() {
				action = ActionCancel
			}
		}
	}
	return
}

// primaryButtonFor names the footer's commit button per mode. Pulled
// out of renderFooter so the label/action mapping is greppable and
// testable without driving a render context.
func primaryButtonFor(mode ModeE) (label string, action ActionE) {
	switch mode {
	case ModeSave:
		label, action = "Save", ActionSave
	case ModePickFolder:
		label, action = "Pick This Folder", ActionPickFolder
	default:
		label, action = "Open", ActionOpen
	}
	return
}

// renderFooterStatus draws the small status text at the left edge of
// the footer row. Priority: filter summary > mode-specific selection
// preview > placeholder. ModePickFolder shows the folder a commit
// would return; multi-select Open shows the count; single-select Open
// shows the selected basename.
func (inst *Dialog) renderFooterStatus() {
	switch {
	case inst.filterDesc() != "":
		c.Label("filter: " + inst.filterDesc()).Send()
	case inst.mode() == ModePickFolder:
		c.Label("folder: " + inst.applyDisplayRoot(inst.folderToCommit())).Send()
	case inst.mode() == ModeOpen && inst.Opts.MultiSelect && len(inst.picked) > 0:
		c.Label(fmt.Sprintf("%d selected", len(inst.picked))).Send()
	case inst.mode() == ModeOpen && inst.selected != "":
		c.Label("selected: " + path.Base(inst.selected)).Send()
	default:
		c.Label(" ").Send()
	}
}

// canCommit reports whether the primary (Open/Save/Pick) button is
// enabled. Open requires at least one picked file — a selected
// directory is not one; Save requires a non-empty filename input;
// PickFolder is always commitable (the user can always pick the current
// folder, including the FS root).
func (inst *Dialog) canCommit() (ok bool) {
	switch inst.mode() {
	case ModeOpen:
		ok = len(inst.picked) > 0
	case ModeSave:
		_, ok = inst.saveTarget()
	case ModePickFolder:
		ok = true
	}
	return
}

// saveTarget is the io/fs path the typed filename names, relative to the
// cwd. The name may carry subdirectories, but ok is false when it is blank,
// names a directory ("." or "..") rather than a file, or climbs above the
// FS root (fs.ValidPath rejects a leading "..").
func (inst *Dialog) saveTarget() (p string, ok bool) {
	name := strings.TrimSpace(inst.filename)
	if name == "" {
		return
	}
	switch path.Base(path.Clean(name)) {
	case ".", "..", "/":
		return
	}
	p = path.Clean(path.Join(inst.st.Dir(), name))
	ok = fs.ValidPath(p)
	return
}

// folderToCommit is what pick-folder returns: the one selected
// directory when there is one, the cwd otherwise. A click selects a
// directory rather than entering it, so committing the cwd alone would
// hand back the parent of the folder the user just clicked.
func (inst *Dialog) folderToCommit() (dir string) {
	dir = inst.pickedDir
	if dir == "" {
		dir = inst.st.Dir()
	}
	return
}

// commitPaths produces the path(s) to return for the active mode, with
// the configured display root applied to each. The slice always has
// exactly one element for ModeSave / ModePickFolder / single-pick
// ModeOpen; in multi-select ModeOpen it carries every picked file in
// the order picked.
func (inst *Dialog) commitPaths() (out []string) {
	switch inst.mode() {
	case ModeOpen:
		out = make([]string, 0, len(inst.picked))
		for _, p := range inst.picked {
			out = append(out, inst.applyDisplayRoot(p))
		}
	case ModeSave:
		if p, ok := inst.saveTarget(); ok {
			out = []string{inst.applyDisplayRoot(p)}
		}
	case ModePickFolder:
		out = []string{inst.applyDisplayRoot(inst.folderToCommit())}
	}
	return
}

// applyDisplayRoot prepends inst.displayRoot() to an io/fs path and
// cleans the result. Empty displayRoot returns p unchanged. A
// trailing "/" on displayRoot is tolerated (stripped before joining).
func (inst *Dialog) applyDisplayRoot(p string) (out string) {
	if inst.displayRoot() == "" {
		out = p
		return
	}
	root := strings.TrimSuffix(inst.displayRoot(), "/")
	out = path.Clean(root + "/" + p)
	return
}
