---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imzero2 filepicker — Explanation

The `filepicker` package is an in-app file open / save / pick-folder
dialog: a top-level `egui::Window` holding an inside-panel layout
(bottom footer, optional bottom filename row, optional right stat
pane) around the `fsbrowser` widget, which fills the central panel
with the breadcrumb, the quick filter and the listing
([ADR-0200](../../../../../../doc/adr/0200-tally-lading-browser.md),
2026-09-18 update). The filesystem is walked Go-side via the stdlib
[`io/fs.FS`] interface, so the default `os.DirFS("/")` backend trades
with a sandboxed `os.DirFS(root)`, an `embed.FS`, a
`testing/fstest.MapFS`, or a custom remote backend without touching
`Render`.

[`io/fs.FS`]: https://pkg.go.dev/io/fs#FS

## Background

The framework offers no native "file dialog" widget; OS-native pickers
(via `rfd::FileDialog` on the Rust side) would spawn a separate native
window and bypass the Go side's permission/visibility model. Both are
deal-breakers for hosts that want a sandboxed view, a remote source, or
just consistent in-egui chrome. The filepicker therefore composes
existing primitives and walks the filesystem Go-side so the host stays
authoritative over what the user can see.

The dialog first carried its own navigation — current directory,
breadcrumb, listing cache, sort, selection set. `fsbrowser` later grew
the same things for the browser panes, and the two drifted apart in
ways a user saw (size units, icons, which click enters a directory).
The dialog now keeps only what makes it a dialog: the window, the
modes, the filename row, the stat pane, and what a commit returns.

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

| Mode             | Listing               | Stat pane | Filename row | Primary             | commitPaths                    |
|------------------|-----------------------|-----------|--------------|---------------------|--------------------------------|
| `ModeOpen`       | dirs + files          | yes       | no           | "Open"              | `[picked file]`                |
| `ModeOpen` (multi) | dirs + files        | yes       | no           | "Open"              | picked files, in pick order    |
| `ModeSave`       | dirs + files          | no        | yes          | "Save"              | `[cwd/filename]`               |
| `ModePickFolder` | dirs only             | no        | no           | "Pick This Folder"  | `[selected dir]`, else `[cwd]` |

In every mode a single click selects a row and a double click or Enter
*activates* it: a directory is entered; a file commits in open mode
and gives its name to the filename row in save mode (a single click
does that too). Pick-folder commits the one selected directory when
there is one, because a click now selects rather than enters —
committing the cwd alone would return the parent of the folder the
user just clicked. The footer names the folder a commit would return.

`WithMultiSelect(true)` is meaningful only for `ModeOpen`; it is what
turns the browser's `SingleSelect` off, so ctrl-click toggles and
shift-click extends. Navigation clears the selection — the browser's
rule, and the Nautilus / Files convention: selection is per-directory.

### Picks

The browser's selection is a set of paths, files and directories
alike. The dialog derives three things from it whenever the browser
reports a change (`syncPicks`): `picked`, the selected *files* in the
order they were first selected, which is what a multi-select commit
returns; `pickedDir`, the selection when it is exactly one directory;
and `selected`, the file under the cursor, which drives the stat pane.
`reconcilePicks` is the pure part and carries the ordering rules.

### Path model

Internally the picker uses **io/fs paths**: forward slashes only, no
leading `/`, no `..`, with `"."` as the FS root — the browser's path
model, so the cwd, the selection, the picks and `commitPaths`'
intermediate result all live in this domain. The `path` package (not
`path/filepath`) provides `Join` / `Dir` / `Base` / `Clean`.

The `WithDisplayRoot(prefix)` option introduces a presentation-layer
prefix that is prepended only at commit time. `WithStartAtOsHome()`
auto-sets it to `"/"` so OS-backed pickers return `"/home/test-user/file.go"`
to the host even though the picker walked `home/test-user/file.go`
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

### Visibility filters

Three restrictions decide what is a row, and a fourth is the user's:

1. **Hidden-file toggle** — the browser's `ShowHidden`, fed from
   `inst.showHidden`. Default off; seeded by [`WithShowHiddenFiles`]
   and flipped at runtime by the `Hidden` Checkbox in the footer.
2. **Mode-driven filter** — `ModePickFolder` hides non-directory
   entries entirely so the listing only offers directories.
3. **Host filter (single predicate slot)** — directories always
   bypass this; non-directories run through `inst.fileFilter`. 2 and
   3 together are `Inst.keep`, handed to the browser as its `Keep`.
   The predicates are written against `fs.DirEntry`; `entryAsDirEntry`
   presents the browser's cached entry as one. Only one filter is
   active at a time:
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
4. **Quick filter** — the browser's regex box, over the paths of the
   subtree under the cwd. It narrows within what 1–3 admit. Its search
   is a background job with the standard progress row and Cancel in the
   filter row; with `WithTasks` / `SetTasks` it is also a keelson task
   (ADR-0038) under the dialog's `AppId`, which `hostboot` wires. The
   dialog stops a search in `Hide` and on commit, since nobody renders
   a closed dialog's search. Because the search runs on its own
   goroutine, `Inst.keep` — and so a host's `WithFilter` predicate — is
   called off the render thread too and must read nothing the render
   thread writes; the built-in filters read only what `New` set.

### A live tree behind a caching widget

`fsbrowser` caches a directory listing until its host says otherwise,
which suits the snapshot stores it was built for. A dialog browses a
live tree, so it calls `State.Invalidate` in `Show` and after every
navigation the browser reports — the moments the dialog's own cache
used to be dropped.

### Column widths

The listing spans the dialog: the browser's `FillWidth`, under which the
name column takes what the size and modified columns leave, a dragged
edge takes from the column to its right, and a resize of the dialog
moves the name column alone (`fsbrowser/fill.go` has the reasoning,
including the construction that does not work).

Dragged widths persist through the standard column-width persistence
(ADR-0151) when the host supplies a resolver — `WithColumnWidths`, or
`SetColumnWidths` for a dialog built before the store was known. The
dialog is not an app, so its widths are not keyed by one: the resolver
comes from `NewColumnWidths`, which fixes the identity
(`AppId`, `runtime.filepicker`) and the drag bounds the
browser uses. A host builds one, shares it between every dialog it
raises, and flushes it every frame — also while no dialog is open, since
a drag made just before a commit is written after the dialog has gone.
The instance tier is tagged by mode (`filepicker/open`, `…/save`,
`…/pick-folder`), not by dialog instance, because a host may mint a
dialog per request. Without a resolver the listing still fills, and
nothing persists.

### Stat caching

`refreshStat()` runs once per frame at the top of `Render` and uses
`selectedStatPath` as a cache key — if it equals `selected`, the cached
`fs.FileInfo` is reused. Switching files invalidates once; staying on
the same file is a free no-op. The pane stats rather than reading the
listing's entry: the listing reports a symlink as the link, and the
pane describes what Open would open. Sizes are IEC, as in the listing.

## Invariants

- The Window's egui ID is an `AbsoluteWidgetId` derived from
  `"filepicker:" + idStr`; it does not push onto the WidgetIdStack and
  must therefore be paired with an explicit `IdScope` for the body's
  widgets to be uniquely identified per instance.
- `Inst` holds the browser's `State` by value and the browser binds
  pointers into it across frames (the filter text), so an `Inst` is
  only ever handled by pointer.
- `renderBrowser` runs after the footer and the filename row, because
  panels are declared before the central panel. What it derives this
  frame is what they show next frame; a save-mode click that rewrites
  `inst.filename` must override the filename row's databinding, which
  was registered earlier in the same frame.
- The browser's `MaxHeight` is fed from a probe of the central panel
  placed *before* the browser. Without a ceiling the table takes its
  auto-fit cap, the window's content grows, and the next probe reads
  the grown panel.
- Bottom panels stack from the bottom edge inward in declaration
  order; the footer must be declared **before** the optional filename
  row so the footer sits at the very bottom of the Window.
- The right (stat) panel must be declared **after** the bottom panels
  and **before** the central panel, so the stat pane spans only the
  middle band (between top breadcrumb and bottom footer) rather than
  the full window height. The right panel is `ModeOpen`-only;
  `ModeSave` and `ModePickFolder` skip it.
- `inst.picked`, `inst.pickedDir` and `inst.selected` are derived
  from the browser's selection by `syncPicks` and by nothing else.
  Tests that need a selection set it on the browser `State` and call
  `syncPicks`, as the `selectRows` test helper does.
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
  bounded by FS latency × entry count. The browser asks every entry
  for its info when it reads a directory — that is where the size and
  modified columns come from — so the bound is one stat per entry
  where the dialog's earlier name-only listing paid none.
- **One-frame lag on selection / typed input.** `Button.SendResp` and
  `TextEdit.SendRespVal` both report the previous frame's state — the
  picker observes the user's click on frame N+1, not N. For
  user-initiated commits (button clicks), this is invisible; for
  programmatic round-trips, the host must wait an extra frame.
- **A symlink to a directory is not a directory.** The listing
  reports an entry's type as `fs.ReadDir` gives it, the link itself,
  so such a link lists as a file: it cannot be entered, and
  pick-folder does not show it.
- **The WASM props follow the browser's.** The dialog imports
  `fsbrowser`, `tree` and `regexedit`, which are declared blocked
  under TinyGo, so the dialog's declaration is blocked by entailment.

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
- **"This is a symlink to X" indication** — the listing marks a link
  with its own glyph; showing the target inline in the stat pane
  needs `os.Readlink` (or an `fs.ReadLinkFS`), so it's
  backend-coupled.
- **Following a symlink to a directory** — see Trade-offs.
- **Keys beyond the browser's** — arrows, Home / End, Page Up / Down,
  Enter and Backspace work once the listing has focus. Esc to cancel
  and focus on open are the dialog's to add.
- **Persisted sort and hidden-files toggle** — the column widths
  persist; the sort order and the `Hidden` toggle still start from
  their defaults in each dialog.
- **Tunable Window size / theme** — currently hardcoded
  `820×500` (open) / `640×480` (save). Options like
  `WithDefaultWindowSize(w, h)` would let hosts override.

## Further reading

- Stdlib reference: <https://pkg.go.dev/io/fs>
- Stdlib reference: <https://pkg.go.dev/testing/fstest>
- Humanization: <https://pkg.go.dev/github.com/dustin/go-humanize>
- Sibling packages: `markdown/EXPLANATION.md`,
  `regex_explorer/regex_explorer.go` (panel-based Window layout).
