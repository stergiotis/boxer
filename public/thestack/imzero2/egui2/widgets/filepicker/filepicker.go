// Package filepicker is an in-app file open / save / pick-folder dialog
// rendered as an egui::Window. The directory tree is walked Go-side via
// the stdlib [io/fs.FS] interface; the default backend is
// [os.DirFS]("/"), but callers can pass any fs.FS —
// [testing/fstest.MapFS] for tests, [os.DirFS](root) for sandboxed
// roots, [embed.FS] for static content, or a custom remote backend.
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
// [ModeOpen] picks an existing file. The right-side stat pane shows
// metadata for the active selection. With [WithMultiSelect] enabled,
// each click toggles the file in/out of the commit set — commit returns
// all picked paths in click order. [ModeSave] asks for a destination:
// the user types a filename and commit returns cwd-joined-name.
// [ModePickFolder] picks a directory: files are hidden from the
// listing, the primary button reads "Pick This Folder", and commit
// returns the current cwd.
//
// State that survives frames (cwd, selection, filename buffer, listing
// cache) lives on Inst. Per-instance ID isolation comes from an
// internal [bindings.IdScope] keyed on the instance's scope string —
// two pickers passed the same ids stack get distinct sub-widget IDs.
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
	"strings"

	"github.com/dustin/go-humanize"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

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
// Predicates receive the raw [fs.DirEntry] from the listing cache,
// so they can inspect [fs.DirEntry.Type] without an additional stat.
// For full-path filtering, close over inst.cwd or capture it from
// your host's render context. desc is shown in the footer as
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
// Each click in the listing toggles a file in or out of the commit set;
// the picker reflects membership via the selected-button highlight, and
// commit emits every set member in click order. Only meaningful in
// [ModeOpen] — silently ignored in [ModeSave] and [ModePickFolder].
//
// Defaults off (single-pick): a click replaces the prior selection.
func WithMultiSelect(enabled bool) (opt Option) {
	opt = func(inst *Inst) {
		inst.multiSelect = enabled
	}
	return
}

// Inst is one file-dialog instance. Construct via [New] — the zero
// value is unusable (the listing cache map is unallocated and the
// absId is zero, which would produce illegal sub-widget IDs).
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

	// Mutable state, persists across frames.
	open       bool
	cwd        string
	pendingCwd string
	// selected is the "active" path — last click in single-select mode,
	// last click (regardless of set membership) in multi-select mode.
	// It drives the stat-pane refresh and the "selected: …" footer
	// label; commit pulls from selectedSet/selectedOrdered.
	selected        string
	selectedSet     map[string]struct{}
	selectedOrdered []string
	filename        string
	cache           map[string]cachedDir
	lastErr         error

	// Stat cache for the currently-selected file (open mode only).
	// selectedStatPath is the cache key — if it equals inst.selected,
	// selectedInfo / selectedStatErr are valid; otherwise refreshStat
	// re-stats once and refreshes them.
	selectedInfo     fs.FileInfo
	selectedStatErr  error
	selectedStatPath string
}

// cachedDir holds one ReadDir result. Cached to avoid re-stat'ing on
// every frame; refreshed when the user navigates into the directory or
// when Show is called on the cwd directly.
type cachedDir struct {
	entries []fs.DirEntry
	err     error
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
		cache:         make(map[string]cachedDir, 8),
		selectedSet:   make(map[string]struct{}, 4),
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
// in place but drop the cached listing so the user sees a fresh stat.
//
// Idempotent on an already-visible dialog.
func (inst *Inst) Show() {
	if inst.open {
		return
	}
	inst.open = true
	inst.lastErr = nil
	if inst.cwd == "" {
		if inst.startDir != "" {
			inst.cwd = path.Clean(inst.startDir)
		} else {
			inst.cwd = "."
		}
	}
	delete(inst.cache, inst.cwd)
}

// Hide closes the dialog without emitting an action. Selection and
// last-error are cleared; cwd, filename buffer, and listing cache are
// preserved so the next Show resumes where the user left off.
func (inst *Inst) Hide() {
	inst.open = false
	inst.clearSelection()
	inst.lastErr = nil
}

// clearSelection wipes the active path, the multi-select set, and its
// ordered companion. Called whenever the picker is dismissed or
// navigates to a different cwd — keeps cross-frame selection state
// confined to one location instead of three.
func (inst *Inst) clearSelection() {
	inst.selected = ""
	for k := range inst.selectedSet {
		delete(inst.selectedSet, k)
	}
	inst.selectedOrdered = inst.selectedOrdered[:0]
}

// IsOpen reports whether the dialog is currently visible.
func (inst *Inst) IsOpen() (open bool) {
	open = inst.open
	return
}

// SetFilename overwrites the save-mode filename buffer. Useful for
// per-invocation suggestions ("alice_graggle.dot" vs "bob_graggle.dot")
// where [WithDefaultFilename] — fixed at construction — is too coarse.
// No-op in open mode (the field is unused there). Safe to call at any
// time; takes effect on the next Render frame.
func (inst *Inst) SetFilename(name string) {
	inst.filename = name
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
//   - ModeOpen + WithMultiSelect → one or more, in click order
//   - ModeSave                  → exactly one path (cwd + filename)
//   - ModePickFolder            → exactly one path (cwd)
//
// On ActionCancel, paths is empty. The dialog auto-hides on commit —
// the host does not need to call Hide.
func (inst *Inst) Render(ids *c.WidgetIdStack) (action ActionE, paths []string) {
	if !inst.open {
		return
	}

	// pendingCwd: a click in the previous frame staged a new cwd.
	// Apply it before iteration starts so the listing we render this
	// frame is consistent.
	if inst.pendingCwd != "" {
		inst.applyPendingCwd()
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
			}
		}
	}
	return
}

// refreshStat repopulates inst.selectedInfo / selectedStatErr when
// inst.selected has changed since the last call. Called once per
// frame at the top of Render. fs.Stat is a syscall (or equivalent)
// per backend, so the cache-key check matters — without it we'd
// re-stat on every frame that the picker is open.
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

// applyPendingCwd commits a staged directory change. Called once at the
// start of Render before any layout work — keeps the cwd switch out of
// the middle of an iteration. Drops the multi-select set too: matches
// the Nautilus / Files convention that navigating away clears the
// in-flight selection.
func (inst *Inst) applyPendingCwd() {
	target := path.Clean(inst.pendingCwd)
	inst.pendingCwd = ""
	inst.cwd = target
	inst.clearSelection()
	inst.lastErr = nil
	delete(inst.cache, target)
}

// renderBody draws the dialog interior inside the Window+IdScope scope.
//
// Layout uses the panel pattern from regex_explorer / hn_explorer:
// PanelTopInside pins the breadcrumbs to the top, PanelBottomInside
// pins the footer (and, in save mode, the filename row above it) to
// the bottom, and PanelCentralInside fills whatever's left for the
// scrolled listing. egui's panel system auto-splits the available
// rect — a Vertical layout would not, since Vertical's children grow
// with content and the ScrollArea would push everything below it off
// the bottom of the Window.
//
// Bottom panels stack from the bottom edge inward in declaration
// order — the FIRST PanelBottomInside sits at the very bottom, so
// the footer is declared before the (optional) filename row.
func (inst *Inst) renderBody(ids *c.WidgetIdStack) (action ActionE, paths []string) {
	for range c.PanelTopInside(ids.PrepareStr("crumbs-panel")).
		Resizable(false).KeepIter() {
		inst.renderBreadcrumbs(ids)
	}

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
		// (commit is cwd; per-entry metadata isn't actionable).
		for range c.PanelRightInside(ids.PrepareStr("stat-panel")).
			DefaultSize(inst.statPaneWidth).
			Resizable(true).
			KeepIter() {
			inst.renderStatPane()
		}
	}

	for range c.PanelCentralInside().KeepIter() {
		if inst.lastErr != nil {
			c.Label("error: " + inst.lastErr.Error()).Send()
			c.Separator().Horizontal().Send()
		}
		for range c.ScrollArea().Vscroll(true).KeepIter() {
			// CrossJustify(true) makes the listing buttons fill the
			// full horizontal width of the ScrollArea's inner ui.
			// Without it each button is text-wide and left-aligned,
			// which pushes the ScrollArea's reserved scrollbar gutter
			// in to where the longest button ends — leaving an empty
			// strip to the right of the scrollbar.
			for range c.UiWithLayout().
				MainDirTopDown().
				CrossJustify(true).
				KeepIter() {
				inst.renderListing(ids)
			}
		}
	}
	return
}

// renderBreadcrumbs draws the path bar as a row of clickable segments.
// A click stages the new cwd via pendingCwd; the actual switch happens
// on the next Render call (via applyPendingCwd).
func (inst *Inst) renderBreadcrumbs(ids *c.WidgetIdStack) {
	for range c.Horizontal().KeepIter() {
		// Root crumb is always present; navigates to FS root (".").
		if c.Button(ids.PrepareStr("crumb-root"), c.Atoms().Text("/").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.pendingCwd = "."
		}
		segs, prefixes := splitBreadcrumbs(inst.cwd)
		for i, seg := range segs {
			c.Label("›").Send()
			if c.Button(ids.PrepareStr("crumb:"+prefixes[i]), c.Atoms().Text(seg).Keep()).
				SendResp().HasPrimaryClicked() {
				inst.pendingCwd = prefixes[i]
			}
		}
	}
}

// renderListing emits the cwd's children as Button rows (dirs and
// files alike). Click on a dir stages cd; click on a file selects (or
// toggles, in multi-select mode). Files are hidden in ModePickFolder.
// The currently-selected file(s) show as "selected" buttons via
// .Selected(true) so users see the active pick before committing.
//
// Buttons (rather than NodeLeaf) are deliberate. egui_ltreeview's
// selection set persists across frames, so HasNodelikeSelected fires
// on every frame the node is in the selected set — not only on the
// click. That re-fires our pendingCwd / inst.selected updates after
// every navigation, walking ".." up to "/" or snapping back to the
// last-clicked directory. Buttons have one-shot HasPrimaryClicked
// semantics, which is what we need.
func (inst *Inst) renderListing(ids *c.WidgetIdStack) {
	cached, ok := inst.cache[inst.cwd]
	if !ok {
		es, err := readDirSorted(inst.fsys, inst.cwd)
		cached = cachedDir{entries: es, err: err}
		inst.cache[inst.cwd] = cached
		inst.lastErr = err
	}

	// ".." entry — navigate to parent dir, except at FS root.
	if inst.cwd != "." && inst.cwd != "" {
		upAtoms := c.Atoms().Text(icons.IconFolder + "  ..").Keep()
		if c.Button(ids.PrepareStr("up"), upAtoms).
			SendResp().HasPrimaryClicked() {
			inst.pendingCwd = path.Dir(inst.cwd)
		}
	}

	for _, de := range cached.entries {
		if !inst.entryVisible(de) {
			continue
		}
		full := path.Join(inst.cwd, de.Name())

		var labelText string
		if de.IsDir() {
			labelText = icons.IconFolder + "  " + de.Name() + "/"
		} else {
			labelText = icons.IconFile + "  " + de.Name()
		}
		labelAtoms := c.Atoms().Text(labelText).Keep()
		isSelected := !de.IsDir() && inst.isInSelectedSet(full)
		if c.Button(ids.PrepareStr("entry:"+full), labelAtoms).
			Selected(isSelected).
			SendResp().HasPrimaryClicked() {
			if de.IsDir() {
				inst.pendingCwd = full
			} else {
				inst.pickFile(full)
			}
		}
	}
}

// entryVisible reports whether de should appear in the listing. The
// three predicates compose AND-style: dot-prefixed names hide unless
// inst.showHidden is true; non-directories hide entirely in
// ModePickFolder; the user-supplied fileFilter has the final say on
// non-directory entries. Directories always bypass fileFilter so
// users can still navigate into a tree whose leaves the filter would
// reject.
func (inst *Inst) entryVisible(de fs.DirEntry) (ok bool) {
	if !inst.showHidden && isHiddenName(de.Name()) {
		return
	}
	if inst.mode == ModePickFolder && !de.IsDir() {
		return
	}
	if inst.fileFilter == nil || de.IsDir() {
		ok = true
		return
	}
	ok = inst.fileFilter(de)
	return
}

// isInSelectedSet reports whether p is part of the in-flight commit
// set. Used by renderListing to drive the per-row Selected(...)
// highlight; ModeOpen multi-select shows every set member highlighted,
// single-pick highlights at most one.
func (inst *Inst) isInSelectedSet(p string) (ok bool) {
	_, ok = inst.selectedSet[p]
	return
}

// pickFile records a click on a file row. In single-pick mode the set
// is replaced by {full}; in multi-select mode membership is toggled.
// inst.selected always tracks the latest click (so the stat pane and
// "selected: …" footer reflect what the user just touched, even when
// the click toggled the file *out* of the multi-select set).
func (inst *Inst) pickFile(full string) {
	inst.selected = full
	if inst.multiSelect {
		if _, in := inst.selectedSet[full]; in {
			delete(inst.selectedSet, full)
			inst.selectedOrdered = removeOrdered(inst.selectedOrdered, full)
			return
		}
		inst.selectedSet[full] = struct{}{}
		inst.selectedOrdered = append(inst.selectedOrdered, full)
		return
	}
	// Single-pick: collapse to one.
	for k := range inst.selectedSet {
		delete(inst.selectedSet, k)
	}
	inst.selectedOrdered = inst.selectedOrdered[:0]
	inst.selectedSet[full] = struct{}{}
	inst.selectedOrdered = append(inst.selectedOrdered, full)
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

		size := info.Size()
		if size < 0 {
			size = 0
		}
		c.Label("Size: " + humanize.Bytes(uint64(size))).Send()
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
// preview > placeholder. ModePickFolder shows the active cwd;
// multi-select Open shows the count; single-select Open shows the
// selected basename.
func (inst *Inst) renderFooterStatus() {
	switch {
	case inst.filterDesc != "":
		c.Label("filter: " + inst.filterDesc).Send()
	case inst.mode == ModePickFolder:
		c.Label("folder: " + inst.applyDisplayRoot(inst.cwd)).Send()
	case inst.mode == ModeOpen && inst.multiSelect && len(inst.selectedSet) > 0:
		c.Label(fmt.Sprintf("%d selected", len(inst.selectedSet))).Send()
	case inst.mode == ModeOpen && inst.selected != "":
		c.Label("selected: " + path.Base(inst.selected)).Send()
	default:
		c.Label(" ").Send()
	}
}

// canCommit reports whether the primary (Open/Save/Pick) button is
// enabled. Open requires at least one file in the selection set; Save
// requires a non-empty filename input; PickFolder is always commitable
// (the user can always pick the current folder, including the FS root).
func (inst *Inst) canCommit() (ok bool) {
	switch inst.mode {
	case ModeOpen:
		ok = len(inst.selectedSet) > 0
	case ModeSave:
		ok = strings.TrimSpace(inst.filename) != ""
	case ModePickFolder:
		ok = true
	}
	return
}

// commitPaths produces the path(s) to return for the active mode, with
// the configured display root applied to each. The slice always has
// exactly one element for ModeSave / ModePickFolder / single-pick
// ModeOpen; in multi-select ModeOpen it carries every selected entry
// in click order.
func (inst *Inst) commitPaths() (out []string) {
	switch inst.mode {
	case ModeOpen:
		if inst.multiSelect {
			out = make([]string, 0, len(inst.selectedOrdered))
			for _, p := range inst.selectedOrdered {
				out = append(out, inst.applyDisplayRoot(p))
			}
			return
		}
		out = []string{inst.applyDisplayRoot(inst.selected)}
	case ModeSave:
		p := path.Clean(path.Join(inst.cwd, strings.TrimSpace(inst.filename)))
		out = []string{inst.applyDisplayRoot(p)}
	case ModePickFolder:
		out = []string{inst.applyDisplayRoot(inst.cwd)}
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
