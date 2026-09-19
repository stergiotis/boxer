// Package filepicker is an in-app file open / save / pick-folder dialog
// rendered as an egui::Window. The directory tree is walked Go-side via
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
// One [Inst] models one dialog. Hosts construct it once (typically as a
// package-level variable), call [Inst.Show] to make it visible, and
// call [Inst.Render] every frame inside their render loop, passing the
// host's shared *WidgetIdStack. The dialog auto-hides on Open / Save /
// PickFolder / Cancel; the host reads the action and (zero or more)
// paths returned by Render to drive whatever follows.
//
// # Modes
//
// [ModeOpen] picks an existing file; a double click or Enter on a file
// commits it. The right-side stat pane shows metadata for the active
// selection. With [WithMultiSelect] enabled, ctrl-click toggles a file
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
// listing cache — and the filename buffer) lives on Inst. Per-instance
// ID isolation comes from an internal [bindings.IdScope] keyed on the
// instance's scope string — two pickers passed the same ids stack get
// distinct sub-widget IDs.
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
// [WithDisplayRoot] to set that — it's prepended to the path returned
// by Render's commit. [WithStartAtOsHome] is a small helper that
// resolves [os.UserHomeDir] and (if no display root has been set yet)
// auto-sets a "/" display root, matching the conventional OS dialog
// experience.
//
// # Column widths
//
// The listing's name column takes the width the size and modified
// columns leave, so the table spans the dialog; a dragged column edge
// takes from the column to its right. With [WithColumnWidths] (or
// [Inst.SetColumnWidths]) a drag is kept through the standard
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
const AppId app.AppIdT = "runtime.filepicker"

// NewColumnWidths builds the resolver a host hands to its dialogs through
// [WithColumnWidths], over the host's column-width store (a facts store
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

// Option configures the picker at construction. Pass to [New].
type Option func(*Inst)

// WithStartDir overrides the initial cwd. dir is an io/fs path —
// forward slashes only, no leading "/", "." for the FS root. Defaults
// to "." (the FS root).
func WithStartDir(dir string) (opt Option) {
	opt = func(inst *Inst) {
		inst.startDir = dir
	}
	return
}

// WithStartAtOsHome resolves the user's home directory via
// [os.UserHomeDir] and uses it as the starting cwd. Only meaningful
// with the default os.DirFS("/") backend (or a backend whose root
// matches the OS root) — the absolute home path is converted to an
// io/fs path by stripping the leading "/".
//
// As a convenience for the conventional "/home/..." reading, this
// option also sets [WithDisplayRoot] to "/" if no display root has
// been configured yet, so the path returned by Render comes back
// OS-absolute.
//
// On error or empty home, the option is a no-op (the picker falls back
// to its default starting cwd, the FS root ".").
func WithStartAtOsHome() (opt Option) {
	opt = func(inst *Inst) {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return
		}
		inst.startDir = strings.TrimPrefix(home, "/")
		if inst.displayRoot == "" {
			inst.displayRoot = "/"
		}
	}
	return
}

// WithDisplayRoot sets a string prefix prepended to the path returned by Render's commit.
//
// The picker's internal paths are io/fs (no leading "/"); this option lets the
// host present them in whatever rooted form makes sense for its backend:
//
//   - "" (default) — return raw io/fs paths.
//   - "/"          — return OS-absolute paths, suitable for [os.DirFS]("/").
//   - "/sandbox"   — return paths rooted at the sandbox, suitable for
//     [os.DirFS]("/sandbox") or any other anchored backend.
//
// A trailing "/" on prefix is tolerated and stripped.
func WithDisplayRoot(prefix string) (opt Option) {
	opt = func(inst *Inst) {
		inst.displayRoot = prefix
	}
	return
}

// WithStatPaneWidth sets the default width (in logical pixels) of the
// open-mode stat pane that shows file details for the selected file.
// Default 240. The pane is resizable; this is only the initial size.
// Ignored in save mode (no stat pane).
func WithStatPaneWidth(width float32) (opt Option) {
	opt = func(inst *Inst) {
		if width > 0 {
			inst.statPaneWidth = width
		}
	}
	return
}

// WithExtensionFilter restricts visible files to those whose suffix
// matches any of exts (case-insensitive, leading dot optional). Empty
// shows everything. Directories are always shown.
//
// Last-Option-wins: WithExtensionFilter / WithGlobFilter / WithFilter
// all occupy the same internal predicate slot. Pass at most one (or
// use [WithFilter] to compose your own).
func WithExtensionFilter(exts ...string) (opt Option) {
	opt = func(inst *Inst) {
		norm := normalizeExtensions(exts)
		if len(norm) == 0 {
			inst.fileFilter = nil
			inst.filterDesc = ""
			return
		}
		inst.fileFilter = func(de fs.DirEntry) bool {
			return passesExtFilter(de, norm)
		}
		inst.filterDesc = strings.Join(norm, " ")
	}
	return
}

// WithGlobFilter restricts visible files by glob pattern. Directories
// always pass.
//
// Each pattern is fed to [path.Match] and the results OR-combined
// across the pattern set. Examples:
//
//   - "*.go"           → any Go file in the current dir
//   - "test_*.go"      → only files starting with "test_"
//   - "?akefile"       → "makefile" or "Makefile" (? matches one char)
//
// Patterns operate on the basename only — they do NOT see the
// full path, because path.Match's "*" never crosses "/". For
// path-aware filters, pass a [WithFilter] predicate. Malformed
// patterns are silently skipped (path.Match's ErrBadPattern is
// treated as "no match"), so a typo can't crash the picker.
//
// Last-Option-wins: see [WithExtensionFilter].
func WithGlobFilter(patterns ...string) (opt Option) {
	opt = func(inst *Inst) {
		clean := make([]string, 0, len(patterns))
		for _, p := range patterns {
			if p = strings.TrimSpace(p); p != "" {
				clean = append(clean, p)
			}
		}
		if len(clean) == 0 {
			inst.fileFilter = nil
			inst.filterDesc = ""
			return
		}
		inst.fileFilter = func(de fs.DirEntry) bool {
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
		inst.filterDesc = strings.Join(clean, " ")
	}
	return
}

// WithFilter installs an arbitrary predicate as the visibility filter.
// Returning true keeps the entry visible; false hides it. Called for
// every cwd child each frame — keep it cheap (no stat, no allocation
// per call). nil disables filtering (everything passes).
//
// Predicates receive an [fs.DirEntry] view of the browser's cached
// entry, so [fs.DirEntry.Type] and [fs.DirEntry.Info] answer from the
// listing without another stat. The entry carries no directory, so a
// predicate sees the name alone. desc is shown in the footer as
// `filter: <desc>` — empty means "no label".
//
// Last-Option-wins: see [WithExtensionFilter].
func WithFilter(pred func(fs.DirEntry) bool, desc string) (opt Option) {
	opt = func(inst *Inst) {
		inst.fileFilter = pred
		inst.filterDesc = desc
	}
	return
}

// WithShowHiddenFiles seeds the runtime "show hidden" toggle. POSIX
// dot-prefixed names ([fs.DirEntry.Name] starting with ".") are
// hidden by default; the user can flip the toggle from the footer
// Checkbox at any time. Defaults to false.
func WithShowHiddenFiles(enabled bool) (opt Option) {
	opt = func(inst *Inst) {
		inst.showHidden = enabled
	}
	return
}

// WithFsBackend overrides the default os.DirFS("/") filesystem. Pass
// any fs.FS implementation: [testing/fstest.MapFS] for tests,
// [os.DirFS](root) for sandboxed paths, [embed.FS] for static content,
// etc. nil is ignored.
func WithFsBackend(fsys fs.FS) (opt Option) {
	opt = func(inst *Inst) {
		if fsys != nil {
			inst.fsys = fsys
		}
	}
	return
}

// WithTitle sets the window title. Defaults: "Open" / "Save". Empty is
// ignored.
func WithTitle(title string) (opt Option) {
	opt = func(inst *Inst) {
		if title != "" {
			inst.title = title
		}
	}
	return
}

// WithDefaultFilename pre-fills the save-mode filename input. Ignored
// in open mode.
func WithDefaultFilename(name string) (opt Option) {
	opt = func(inst *Inst) {
		inst.filename = name
	}
	return
}

// WithMultiSelect lets the user accumulate several files into one commit.
// A plain click replaces the selection, ctrl-click toggles a file in or
// out of it and shift-click extends it from the cursor — the browser
// widget's selection — and commit emits every selected file in the order
// picked. Only meaningful in [ModeOpen] — silently ignored in
// [ModeSave] and [ModePickFolder].
//
// Defaults off (single-pick): every click replaces the prior selection.
func WithMultiSelect(enabled bool) (opt Option) {
	opt = func(inst *Inst) {
		inst.multiSelect = enabled
	}
	return
}

// WithColumnWidths persists the listing's column widths through res, which
// the host builds once with [NewColumnWidths] and flushes once per frame —
// every frame, not only while a dialog is open, since a width dragged just
// before a commit is written after the dialog has gone. nil persists
// nothing. See [Inst.SetColumnWidths] for a dialog built before the store
// is known.
func WithColumnWidths(res *colwidth.Resolver) (opt Option) {
	opt = func(inst *Inst) {
		inst.widths = res
	}
	return
}

// WithTasks publishes the quick filter's search as a keelson background task
// (ADR-0038) through tasks, so the host's task monitor lists it and can
// cancel it. The host builds the API under [AppId] with the task producer
// caps. nil keeps the search the dialog's own: it still runs off the render
// thread, with its progress and Cancel in the filter row. See
// [Inst.SetTasks] for a dialog built before the bus is known.
func WithTasks(tasks task.TaskApiI) (opt Option) {
	opt = func(inst *Inst) {
		inst.tasks = tasks
	}
	return
}

// Inst is one file-dialog instance. Construct via [New] — the zero
// value is unusable (the absId is zero, which would produce illegal
// sub-widget IDs).
//
// Inst is not safe for concurrent use; restrict access to the host's
// single render-loop goroutine.
type Inst struct {
	// Identity (constructed once). scopeKey is the per-instance
	// string used as the IdScope key inside Render — two simultaneous
	// pickers get distinct sub-widget IDs without colliding on egui
	// state or FFFI databindings. absId names the egui::Window itself
	// (top-level windows use absolute IDs per SKILLS §3).
	absId    c.AbsoluteWidgetId
	scopeKey string
	mode     ModeE

	// Configuration.
	title         string
	startDir      string
	displayRoot   string
	fsys          fs.FS
	statPaneWidth float32
	multiSelect   bool
	// widths is the host's column-width resolver, nil for none; tasks its
	// task API for the filter's search, nil for none.
	widths *colwidth.Resolver
	tasks  task.TaskApiI
	// fileFilter is the visibility predicate for non-directory entries.
	// nil means "everything passes". filterDesc is the human-readable
	// footer label ("filter: <desc>"). Both are written exclusively by
	// the With{Extension,Glob,}Filter options — last-Option-wins.
	fileFilter func(fs.DirEntry) bool
	filterDesc string
	// showHidden mirrors the footer Checkbox: when false, dot-prefixed
	// names (POSIX hidden convention) are dropped from the listing.
	// Seedable at construction via [WithShowHiddenFiles]; flipped at
	// runtime by the user. Applies to dirs too — `.git/`, `.cache/`,
	// and similar hide.
	showHidden bool

	// Mutable state, persists across frames. The browser state holds
	// where the dialog is: current directory, listing cache, selection,
	// cursor, sort and quick filter (ADR-0200, 2026-09-18 update). It
	// is bound by pointer into the render loop, so it lives here and
	// Inst is only ever handled by pointer.
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

// New constructs a picker instance. idStr is a stable identity for this
// dialog; the picker derives both the Window's absolute ID and the
// internal IdScope key from idStr, so multiple pickers run side-by-side
// without colliding on egui state or FFFI databindings.
//
// The instance starts hidden; call [Inst.Show] before the next Render
// to make it visible.
func New(idStr string, mode ModeE, opts ...Option) (inst *Inst) {
	scopeKey := "filepicker:" + idStr
	inst = &Inst{
		absId:         c.MakeAbsoluteIdStr(scopeKey),
		scopeKey:      scopeKey,
		mode:          mode,
		fsys:          os.DirFS("/"),
		statPaneWidth: 240,
	}
	switch mode {
	case ModeSave:
		inst.title = "Save"
	case ModePickFolder:
		inst.title = "Pick folder"
	default:
		inst.title = "Open"
	}
	for _, opt := range opts {
		opt(inst)
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
func (inst *Inst) Show() {
	if inst.open {
		return
	}
	inst.open = true
	if !inst.started {
		inst.started = true
		inst.st.SetDir(inst.startDir)
	}
	inst.st.Invalidate()
}

// Hide closes the dialog without emitting an action. The selection is
// cleared; cwd, filename buffer, sort and quick filter are preserved so
// the next Show resumes where the user left off.
func (inst *Inst) Hide() {
	inst.open = false
	inst.clearSelection()
	// Nobody renders a hidden dialog, so nobody would see its search
	// through or cancel it.
	inst.st.StopSearch()
}

// clearSelection wipes the browser's selection and what the dialog
// derives from it. Navigation needs no call: the browser clears its
// selection on a directory change and syncPicks follows.
func (inst *Inst) clearSelection() {
	inst.st.ClearSelection()
	inst.picked = inst.picked[:0]
	inst.pickedDir = ""
	inst.selected = ""
}

// IsOpen reports whether the dialog is currently visible.
func (inst *Inst) IsOpen() (open bool) {
	open = inst.open
	return
}

// SetFilename overwrites the save-mode filename buffer. Useful for
// per-invocation suggestions ("alice_pushoutgraph.dot" vs "bob_pushoutgraph.dot")
// where [WithDefaultFilename] — fixed at construction — is too coarse.
// No-op in open mode (the field is unused there). Safe to call at any
// time; takes effect on the next Render frame.
func (inst *Inst) SetFilename(name string) {
	inst.filename = name
}

// SetColumnWidths is [WithColumnWidths] for a dialog constructed before the
// host's store was known. Safe to call at any time; takes effect on the
// next Render frame.
func (inst *Inst) SetColumnWidths(res *colwidth.Resolver) {
	inst.widths = res
}

// SetTasks is [WithTasks] for a dialog constructed before the host's bus was
// known. Safe to call at any time; takes effect with the next search.
func (inst *Inst) SetTasks(tasks task.TaskApiI) {
	inst.tasks = tasks
}

// widthTag names the dialog's table for the resolver's instance tier. It is
// the mode, not the instance: a host may mint a dialog per request, and a
// width dragged in one open dialog is wanted in the next.
func (inst *Inst) widthTag() (tag string) {
	_, action := primaryButtonFor(inst.mode)
	tag = "filepicker/" + action.String()
	return
}

// Render draws the dialog this frame and reports any committed action.
// Non-commit frames return ActionNone with a nil slice; the host should
// keep calling Render every frame until something other than ActionNone
// comes back.
//
// The ids stack must be the host's shared widget-id stack; Render
// internally opens a [bindings.IdScope] keyed by inst.scopeKey, so
// sub-widgets are uniquely identified per instance regardless of which
// outer scope the host has already pushed.
//
// On Action{Open,Save,PickFolder}, paths holds the picked path(s) with
// [WithDisplayRoot] applied (raw io/fs paths by default; OS-absolute
// when WithStartAtOsHome or WithDisplayRoot("/") was set):
//
//   - ModeOpen single-pick     → exactly one path
//   - ModeOpen + WithMultiSelect → one or more, in the order picked
//   - ModeSave                  → exactly one path (cwd + filename)
//   - ModePickFolder            → exactly one path (the selected
//     directory, or the cwd when none is selected)
//
// On ActionCancel, paths is empty. The dialog auto-hides on commit —
// the host does not need to call Hide.
func (inst *Inst) Render(ids *c.WidgetIdStack) (action ActionE, paths []string) {
	if !inst.open {
		return
	}
	inst.refreshStat()

	// Default window size by mode: ModeOpen hosts the stat pane on the
	// right (needs more horizontal room); ModeSave and ModePickFolder
	// have no stat pane and stay compact.
	defaultW, defaultH := float32(640), float32(480)
	if inst.mode == ModeOpen {
		defaultW, defaultH = 820, 500
	}

	label := c.WidgetText().Text(inst.title).Keep()
	for range c.Window(inst.absId, label).
		Resizable(true).
		Collapsible(false).
		TitleBar(true).
		DefaultOpen(true).
		DefaultSize(defaultW, defaultH).
		MinWidth(420).
		MinHeight(320).
		KeepIter() {

		for range c.IdScope(ids.PrepareStr(inst.scopeKey)) {
			action, paths = inst.renderBody(ids)
			if action != ActionNone {
				inst.open = false
				// Commit consumes the user's intent — wipe the
				// selection so a subsequent Show doesn't re-highlight
				// the prior pick. Cancel leaves state intact so an
				// accidental Cancel + re-Show resumes where the user
				// was. commitPaths has already been pulled into paths.
				if action != ActionCancel {
					inst.clearSelection()
				}
				inst.st.StopSearch()
			}
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
func (inst *Inst) refreshStat() {
	if inst.selected == "" {
		inst.selectedInfo = nil
		inst.selectedStatErr = nil
		inst.selectedStatPath = ""
		return
	}
	if inst.selectedStatPath == inst.selected {
		return
	}
	info, err := fs.Stat(inst.fsys, inst.selected)
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
func (inst *Inst) renderBody(ids *c.WidgetIdStack) (action ActionE, paths []string) {
	for range c.PanelBottomInside(ids.PrepareStr("footer-panel")).
		Resizable(false).KeepIter() {
		action, paths = inst.renderFooter(ids)
	}
	if inst.mode == ModeSave {
		for range c.PanelBottomInside(ids.PrepareStr("fname-panel")).
			Resizable(false).KeepIter() {
			inst.renderFilenameRow(ids)
		}
	}

	if inst.mode == ModeOpen {
		// Right panel must be declared BEFORE the central panel so
		// the central panel sees the right slice already removed
		// from its available rect. Declared AFTER the bottom panels
		// so the stat pane spans only the middle band, not the
		// full window height. ModePickFolder skips the stat pane
		// (commit is a directory; per-entry metadata isn't actionable).
		for range c.PanelRightInside(ids.PrepareStr("stat-panel")).
			DefaultSize(inst.statPaneWidth).
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
func (inst *Inst) renderBrowser(ids *c.WidgetIdStack) (action ActionE, paths []string) {
	// The probe reports the room left for the next widget, so it goes
	// before the browser; the answer is a frame late, hence held.
	if _, h, ok := c.CapturePaneSize(c.ProbeSeq(inst.scopeKey, "browser")); ok && h > 0 {
		inst.browserH = h
	}
	rootLabel := inst.displayRoot
	if rootLabel == "" {
		rootLabel = "/"
	}
	res := fsbrowser.Render(fsbrowser.Input{
		Ids:          ids,
		ScopeKey:     "browser",
		FS:           inst.fsys,
		RootLabel:    rootLabel,
		State:        &inst.st,
		Mode:         fsbrowser.ModeList,
		ShowHidden:   inst.showHidden,
		Keep:         inst.keep,
		SingleSelect: !(inst.multiSelect && inst.mode == ModeOpen),
		MaxHeight:    inst.browserH,
		FillWidth:    true,
		Widths:       inst.widths,
		WidthTag:     inst.widthTag(),
		Tasks:        inst.tasks,
	})
	if res.Navigated {
		// The widget caches a listing until told otherwise, which suits
		// a snapshot; behind a dialog is a live tree.
		inst.st.Invalidate()
	}
	if res.Navigated || res.SelectionChanged {
		inst.syncPicks(res.Rows)
	}
	if inst.mode == ModeSave {
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
	if inst.mode == ModeOpen && res.Activated >= 0 && res.Activated < len(res.Rows) {
		p := res.Rows[res.Activated].Path
		if !slices.Contains(inst.picked, p) {
			if !inst.multiSelect {
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
func (inst *Inst) keep(e fsbrowser.Entry) (ok bool) {
	if inst.mode == ModePickFolder && !e.IsDir {
		return
	}
	if inst.fileFilter == nil || e.IsDir {
		ok = true
		return
	}
	ok = inst.fileFilter(entryAsDirEntry{e: e})
	return
}

// syncPicks re-derives picked, pickedDir and selected from the browser's
// selection. rows is what the browser showed this frame; it says which
// selected paths are directories.
func (inst *Inst) syncPicks(rows []fsbrowser.Entry) {
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
func (inst *Inst) renderStatPane() {
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
func (inst *Inst) renderFilenameRow(ids *c.WidgetIdStack) {
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
func (inst *Inst) renderFooter(ids *c.WidgetIdStack) (action ActionE, paths []string) {
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
			primaryLabel, primaryAction := primaryButtonFor(inst.mode)

			canCommit := inst.canCommit()
			primaryAtoms := c.Atoms().Text(primaryLabel).Keep()
			for range c.EnabledUi(canCommit).KeepIter() {
				if c.Button(ids.PrepareStr("primary"), primaryAtoms).
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
func (inst *Inst) renderFooterStatus() {
	switch {
	case inst.filterDesc != "":
		c.Label("filter: " + inst.filterDesc).Send()
	case inst.mode == ModePickFolder:
		c.Label("folder: " + inst.applyDisplayRoot(inst.folderToCommit())).Send()
	case inst.mode == ModeOpen && inst.multiSelect && len(inst.picked) > 0:
		c.Label(fmt.Sprintf("%d selected", len(inst.picked))).Send()
	case inst.mode == ModeOpen && inst.selected != "":
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
func (inst *Inst) canCommit() (ok bool) {
	switch inst.mode {
	case ModeOpen:
		ok = len(inst.picked) > 0
	case ModeSave:
		ok = strings.TrimSpace(inst.filename) != ""
	case ModePickFolder:
		ok = true
	}
	return
}

// folderToCommit is what pick-folder returns: the one selected
// directory when there is one, the cwd otherwise. A click selects a
// directory rather than entering it, so committing the cwd alone would
// hand back the parent of the folder the user just clicked.
func (inst *Inst) folderToCommit() (dir string) {
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
func (inst *Inst) commitPaths() (out []string) {
	switch inst.mode {
	case ModeOpen:
		out = make([]string, 0, len(inst.picked))
		for _, p := range inst.picked {
			out = append(out, inst.applyDisplayRoot(p))
		}
	case ModeSave:
		p := path.Clean(path.Join(inst.st.Dir(), strings.TrimSpace(inst.filename)))
		out = []string{inst.applyDisplayRoot(p)}
	case ModePickFolder:
		out = []string{inst.applyDisplayRoot(inst.folderToCommit())}
	}
	return
}

// applyDisplayRoot prepends inst.displayRoot to an io/fs path and
// cleans the result. Empty displayRoot returns p unchanged. A
// trailing "/" on displayRoot is tolerated (stripped before joining).
func (inst *Inst) applyDisplayRoot(p string) (out string) {
	if inst.displayRoot == "" {
		out = p
		return
	}
	root := strings.TrimSuffix(inst.displayRoot, "/")
	out = path.Clean(root + "/" + p)
	return
}
