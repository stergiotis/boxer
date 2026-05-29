---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imzero2 filepicker — Explanation

The `filepicker` package composes an in-app file open / save /
pick-folder dialog from the imzero2 / egui2 widget primitives — a
top-level `egui::Window` holding an inside-panel layout (top
breadcrumb, bottom footer, optional bottom filename row, optional
right stat pane, central listing). The filesystem is walked Go-side
via the stdlib [`io/fs.FS`] interface, so the default `os.DirFS("/")`
backend trades trivially with a sandboxed `os.DirFS(root)`, an
`embed.FS`, a `testing/fstest.MapFS`, or a custom remote backend
without touching `Render`.

[`io/fs.FS`]: https://pkg.go.dev/io/fs#FS

## Background

The framework offers no native "file dialog" widget; OS-native pickers
(via `rfd::FileDialog` on the Rust side) would spawn a separate native
window and bypass the Go side's permission/visibility model. Both are
deal-breakers for hosts that want a sandboxed view, a remote source, or
just consistent in-egui chrome. The filepicker therefore composes
existing primitives — `Window`, `Panel*Inside`, `ScrollArea`, `Button`,
`TextEdit`, `UiWithLayout` — and walks the filesystem Go-side so the
host stays authoritative over what the user can see.

The `io/fs` adoption (commit `eff5b52f`) is the lynchpin that makes the
backend interchangeable. `fs.ReadDir(fsys, name)` and `fs.Stat(fsys,
name)` dispatch to whichever optional methods the FS implements,
falling back to `fs.File`-based traversal otherwise — so the picker
treats every backend uniformly.

## How it works

### Modes

`ModeE` is `ModeOpen`, `ModeSave`, or `ModePickFolder`. Each shapes the
body layout, the primary-button label, the listing filter, and what
`commitPaths` returns:

| Mode             | Listing               | Stat pane | Filename row | Primary             | commitPaths       |
|------------------|-----------------------|-----------|--------------|---------------------|-------------------|
| `ModeOpen`       | dirs + files          | yes       | no           | "Open"              | `[selected]`      |
| `ModeOpen` (multi) | dirs + files        | yes       | no           | "Open"              | set, click-order  |
| `ModeSave`       | dirs + files          | no        | yes          | "Save"              | `[cwd/filename]`  |
| `ModePickFolder` | dirs only             | no        | no           | "Pick This Folder"  | `[cwd]`           |

`WithMultiSelect(true)` is meaningful only for `ModeOpen`. Each click
in the listing toggles a file in / out of `selectedSet` (membership)
and appends / removes from `selectedOrdered` (commit order). The
`Selected(true)` highlight reflects set membership, and the footer
status flips from `selected: <name>` to `N selected`. Navigation
(breadcrumb / `..` / folder click) calls `clearSelection`, matching
the Nautilus / Files convention — selection is per-directory.

### Path model

Internally the picker uses **io/fs paths**: forward slashes only, no
leading `/`, no `..`, with `"."` as the FS root. `cwd`, `pendingCwd`,
`selected`, cache keys, breadcrumb segments, and `commitPath`'s
intermediate result all live in this domain. The `path` package (not
`path/filepath`) provides `Join` / `Dir` / `Base` / `Clean`.

The `WithDisplayRoot(prefix)` option introduces a presentation-layer
prefix that is prepended only at commit time. `WithStartAtOsHome()`
auto-sets it to `"/"` so OS-backed pickers return `"/home/spx/file.go"`
to the host even though the picker walked `home/spx/file.go`
internally.

### Per-instance ID isolation

Two pickers receiving the same `*WidgetIdStack` would produce identical
sub-widget FFFI IDs (and therefore collide on egui state and FFFI
databindings) without scoping. The Window itself uses an absolute ID
(`MakeAbsoluteIdStr("filepicker:" + idStr)`) per SKILLS §3, but the
absolute ID does not push onto the WidgetIdStack — so the body
widgets must be scoped explicitly.

`Render` opens an internal `c.IdScope(ids.PrepareStr(scopeKey))`
keyed on the picker's instance string. Inside the scope every
`ids.PrepareStr("foo")` XORs against the pushed scope key, yielding
distinct IDs across instances even when the host shares one ids stack.

### Listing-click semantics

Listing rows are `Button`s, not `NodeLeaf`s. `egui_ltreeview` retains
its selection set across frames, so `HasNodelikeSelected` fires every
frame the node is selected — the click is indistinguishable from
"still selected from last frame." A `Button`'s `HasPrimaryClicked`
fires exactly once per click, which is the semantic the picker
needs.

### Visibility filters

The listing applies three predicates per entry, AND-combined inside
`Inst.entryVisible`:

1. **Hidden-file toggle** — when `inst.showHidden` is false, names
   matching `isHiddenName` (POSIX dot-prefix) hide regardless of
   mode. Default off; seeded by [`WithShowHiddenFiles`] and flipped
   at runtime by the `Hidden` Checkbox in the footer.
2. **Mode-driven filter** — `ModePickFolder` hides non-directory
   entries entirely so the listing only offers cd-targets.
3. **User filter (single predicate slot)** — directories always
   bypass this; non-directories run through `inst.fileFilter`. Only
   one filter is active at a time:
   - `WithExtensionFilter(".go", ".md")` — case-insensitive suffix
     match (back-compat shape).
   - `WithGlobFilter("*.go", "test_*.go")` — `path.Match` per
     pattern, OR-combined. Operates on the basename only, since
     `path.Match`'s `*` never crosses `/`. Malformed patterns are
     silently skipped (host typos can't crash the dialog).
   - `WithFilter(pred, "desc")` — arbitrary `func(fs.DirEntry) bool`
     with a footer label. Use for path-aware filters by closing over
     the host's cwd context.

   Last-Option-wins: passing several `With*Filter*` options overwrites
   the slot; the footer label always reflects the *last* one set.

### Staged cwd changes

Breadcrumb / dir-button clicks set `inst.pendingCwd` rather than
mutating `inst.cwd` mid-iteration. `Render` applies the staged change
once at the top of the frame (clearing the selection and the cache
entry for the new cwd), then renders consistently.

### Stat caching

`refreshStat()` runs once per frame at the top of `Render` and uses
`selectedStatPath` as a cache key — if it equals `selected`, the cached
`fs.FileInfo` is reused. Switching files invalidates once; staying on
the same file is a free no-op.

## Invariants

- The Window's egui ID is an `AbsoluteWidgetId` derived from
  `"filepicker:" + idStr`; it does not push onto the WidgetIdStack and
  must therefore be paired with an explicit `IdScope` for the body's
  widgets to be uniquely identified per instance.
- Listing rows must be `Button`s (one-shot click), not
  `NodeLeaf`s — see the click-semantics rationale above.
- Bottom panels stack from the bottom edge inward in declaration
  order; the footer must be declared **before** the optional filename
  row so the footer sits at the very bottom of the Window.
- The right (stat) panel must be declared **after** the bottom panels
  and **before** the central panel, so the stat pane spans only the
  middle band (between top breadcrumb and bottom footer) rather than
  the full window height. The right panel is `ModeOpen`-only;
  `ModeSave` and `ModePickFolder` skip it.
- The listing's `c.UiWithLayout().MainDirTopDown().CrossJustify(true)`
  wrapper is required for the buttons to fill the central panel's
  full horizontal width; without it the ScrollArea reserves its
  scrollbar gutter mid-panel.
- Internal cwd `"."` means "FS root"; `splitBreadcrumbs` returns empty
  slices for it. `path.Dir(".")` is `"."`, so the up-button must guard
  against `cwd == "."` to avoid a no-op tap.
- `inst.selected` (the "active" path) and `inst.selectedSet` /
  `inst.selectedOrdered` (the commit set + click order) are kept in
  sync by `pickFile`. Tests and helpers that mutate selection state
  must go through `pickFile` (or directly through `clearSelection`)
  to avoid drift between the three.
- `inst.fileFilter` and `inst.filterDesc` always move together — each
  `With*Filter*` option writes both or neither. The footer reads
  `filterDesc` (not the predicate), so a custom predicate registered
  with a blank desc disappears from the footer status but still
  filters the listing.

## Trade-offs

These follow from the problem shape, not from a particular code
choice — they would constrain any in-app file picker walking the FS
Go-side from a single render-loop goroutine.

- **Synchronous traversal.** `fs.ReadDir` and `fs.Stat` block the
  render-loop goroutine. On a directory with tens of thousands of
  entries, the first-emit frame visibly stalls. Listing cache makes
  subsequent frames cheap, but the initial walk is unavoidably
  bounded by FS latency × entry count.
- **One-frame lag on selection / typed input.** `Button.SendResp` and
  `TextEdit.SendRespVal` both report the previous frame's state — the
  picker observes the user's click on frame N+1, not N. For
  user-initiated commits (button clicks), this is invisible; for
  programmatic round-trips, the host must wait an extra frame.
- **`fs.FS` has no `Lstat` analogue.** `fs.Stat` follows symlinks, so
  a symlinked file appears as its target. Distinguishing the link
  from the target needs an OS-level call outside the `fs.FS`
  interface.
- **One selection at a time.** `inst.selected` is a single string; the
  egui_ltreeview-style selection set isn't surfaced because we use
  Buttons for click semantics.

## Out of scope (potential roadmap)

Captured here so the deferred items don't get lost. Each is an
addition the current shape can absorb without an architectural
rewrite, but none are blocking the v1 use case.

- **Preview pane** — text / image / hex preview alongside the stat
  pane. Needs a small async fetcher (the FR pattern from
  `egui2_methods.go`'s `EtPrefetchInfo`) so the file body is read off
  the render goroutine.
- **Async stat / large-dir handling** — a goroutine that snapshots
  `ReadDir` results into a sync.Map; `Render` polls the snapshot
  pointer and shows a progress hint when the goroutine is mid-walk.
  Pattern mirrors `regex_explorer.matchRunning` coalescer.
- **Recent paths sidebar** — small persisted ring buffer keyed by
  `Inst.idStr`, surfaced as a left panel. Needs a host-supplied
  persistence shim (writing to disk inside `filepicker` would
  contradict the "host owns IO" stance).
- **Vim-style keybindings** — `j`/`k` to move selection, `Enter` to
  commit, `Esc` to cancel, `g`/`G` to jump. Needs a focused-row state
  machine and access to the egui keyboard event stream for a
  non-focused widget — not currently exposed.
- **Modifier-aware multi-select** — the v1 multi-select implementation
  toggles on every plain click because `ResponseFlagsE` does not
  surface Ctrl/Shift state. Wiring modifier bits through the FFFI2
  click response would let the picker offer the classic file-manager
  semantics (plain click replaces, Ctrl+click toggles, Shift+click
  range-extends) on top of the existing set.
- **"New folder" UI** — `+` button next to the breadcrumb; opens a
  small inline TextEdit for the new dir name; calls `os.Mkdir` (or a
  hypothetical `fs.MkdirFS` extension when one exists). Save mode
  benefits most.
- **Recursive globs** — `path.Match` (used by [`WithGlobFilter`])
  does not implement `**` cross-separator wildcards. Hosts wanting
  `**/*.md`-style inclusion can drop down to [`WithFilter`] and
  call `doublestar.Match` (or roll their own walker), but native
  support inside `WithGlobFilter` would have to swap matchers or
  borrow boxer's path-glob package.
- **Symlink loop guard** — bound the listing cache size and the
  per-frame ReadDir count so a malicious cyclic symlink can't blow
  memory or stall the loop. Low priority — fs.Stat already follows
  links, and `os.ReadDir` doesn't recurse on its own.
- **"This is a symlink to X" indication** — separate from preview;
  shows the link target inline in the stat pane. Needs `os.Lstat`
  (or an `fs.LstatFS` interface), so it's backend-coupled.
- **Tunable Window size / theme** — currently hardcoded
  `820×500` (open) / `640×480` (save). Options like
  `WithDefaultWindowSize(w, h)` would let hosts override.

## Further reading

- Stdlib reference: <https://pkg.go.dev/io/fs>
- Stdlib reference: <https://pkg.go.dev/testing/fstest>
- Humanization: <https://pkg.go.dev/github.com/dustin/go-humanize>
- Sibling packages: `markdown/EXPLANATION.md`,
  `regex_explorer/regex_explorer.go` (panel-based Window layout),
  `hn_explorer/hn_explorer.go` (split-pane layout).
