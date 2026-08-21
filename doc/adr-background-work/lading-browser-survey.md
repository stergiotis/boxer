---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# A browser for the lading store — design-space survey and build plan

> **Provenance.** Compiled 2026-08-20, ahead of any decision: nothing in here
> is settled, and an ADR — not this page — is where any of it would become
> one. Provenance is three-tiered and marked throughout: (a) claims about this
> repository were verified against the working tree on the compile date;
> (b) the feature inventories of WinSCP, FileZilla and Cyberduck in §4 were
> checked against the vendors' own documentation pages on the compile date
> (links under References); (c) the commander-family conventions and the
> keyboard idioms in §4 come from general knowledge and are marked *(c)* —
> pointers, not verified fact.

> **Decision record.** The forks in §10 were answered on 2026-08-20 and the
> resulting decision is [ADR-0200](../adr/0200-tally-lading-browser.md)
> (proposed); this page stays the analysis behind it.

## 1 Question and scope

The lading store ([ADR-0198](../adr/0198-fs-snapshot-store.md), proposed;
M0–M6 shipped) writes a walk of an `io/fs` tree as one immutable snapshot
into facts-shaped ClickHouse tables and reads it back three ways — as a Go
`fs.FS`, as SQL through the `fs()` / `fsdata()` / `fssnap()` macros, and as
SFTP over a pipe for rclone. None of the three is a GUI. The question this
page works through:

> What would a native GUI for lading look like that (1) a power user of
> WinSCP, Cyberduck or FileZilla recognises on first sight, and (2) uses what
> the leeway shape gives every entry row and no file-transfer client has —
> components formulated after the fact, a shared vocabulary, glosses, time
> as a key column, cross-mount SQL?

In scope: browsing, previewing, inspecting, comparing, searching, and
driving snapshots (taking one, exporting from one). Out of scope by the
store's own contract: editing anything *inside* a snapshot. Lading has no
write path — not in the walker, not in the adapter, not over SFTP — and this
page does not invent one. "Transfer", the organising metaphor of the
reference clients, therefore maps differently here (§4.3), and that mapping
is most of the design.

The page ends with a recommended shape (§7), a milestone plan (§8), the house
rules that bind the build (§9) and the forks that need a decision before an
ADR is written (§10).

## 2 What lading provides today (tier a)

Verified against `public/fs/lading/` and ADR-0198's milestone updates.

| Surface | What a GUI gets from it | What a GUI must respect |
| --- | --- | --- |
| `boxer.fsmeta` / `fsdata` / `fssnap` | the facts `TableDesc` on tables the store owns; key `(mount, snap, path)`; materialised `name` / `dir` / `depth` / `ext`; bloom filter on `dir` | partitions and `TTL` by expiry day; the store decodes positionally, so nothing may add stored columns |
| per-entry attributes (`fs()` columns) | `path snap expires_at node_kind content mode block_size blocks size mtime link_target err content_hash text name dir depth ext is_dir is_symlink` | no owner / group (`io/fs` has none; rclone carries no modes in either direction); `content` is `none` / `blocks` / `ref`; `text = true` is a guarantee a very long line forfeits |
| snapshot index (`fssnap()`, `ladingadapter.Mounts / Snapshots / Latest`) | one row per *complete* snapshot: instant, expiry, entries, bytes, policy applied — the store's own "what was loaded" ledger | `Snapshots` does not page yet; a complete snapshot is the only kind a query can see |
| mount policy record (`ladingpolicy`, a kind in `boxer.facts`) | the closest thing to a site manager: name, store, retention class, text rule, inline threshold | written on purpose by `RecordPolicy`, not per walk; a mount without a record has an id and nothing else; name → id resolution is the application's |
| `io/fs` adapter (`ladingadapter.Open / OpenLatest`) | an immutable `fs.FS` per `(mount, snap)`: `StatFS ReadDirFS ReadFileFS GlobFS ReadLinkFS SubFS`, `File` is `ReaderAt` + `Seeker`; caches never invalidate; symlinks resolved inside the snapshot; `ErrNoContent` and `ErrReferenced` are typed | a query per call, millisecond latency — batch-shaped, not a hot path; a text file is materialised whole on first read (bounded by the inline threshold); a `ref` entry has size, mtime and hash but no bytes unless a `SourceFetcherI` is wired, and none exists yet |
| SQL surface (`ladingsql.Expand(Config{Visibility}, sql)`) | `fs(m[, snap])`, `fsdata(m[, snap])`, `fssnap(m)`; `'*'` widens to every complete snapshot; mount spelled bare, decimal or hex; `References(sql)` lists the mounts a statement names; the §7 catalogue (grep with exact line numbers, history, diff, `du`, identical content, audit, across mounts) runs as tests | no `PREWHERE` through a macro; name-as-sugar refused; "every mount" is a direct read of `boxer.fssnap`, the macro spelling is open; `Visibility` nil refuses everything — `VisibleAll` / `VisibleSet` / `VisibleUnderTag` exist, nothing binds them to the capability broker (the subject is an open decision) |
| rclone head (`boxer fs sftp-stdio --mount …`) | `/<mount>/<snapshot>/<path>` with `latest`; `rclone mount`, `serve s3/webdav/nfs`, `hasher`, `union` on top | read-only; serial against the store; a GUI should hand the mount command to the clipboard rather than re-implement a transport |
| ingest (`ladingingest.Snapshot(ctx, fsys, mount, policy, stores)`) | any `fs.FS` — `os.DirFS`, `embed.FS`, a zip, a capability grant, `ladingremote.Serve(ctx, "s3:bucket/prefix")`; the commit rule makes a failed walk invisible; `Result{Snap ExpiresAt Entries Bytes Blocks Stored Referenced Skipped Errors}` | **no progress seam** — a walk reports only when it returns; a GUI "transfer queue" needs one (§10) |
| identity | mount = the caller's tagged id; `LW_ID_TAG_VALUE` / `LW_ID_BODY` for display and grouping; 16 hex digits over SFTP | the store mints nothing; a GUI that shows names reads the policy record and falls back to hex |
| retention | `expires_at` per row, whole-day classes, whole-part drops | "this snapshot disappears on <day>" is a column, which no reference client can show because none has the concept |

Two facts from this table carry the rest of the page. The adapter *is* an
`fs.FS`, so anything written against `fs.FS` browses a snapshot with no
lading-specific code — including the repository's own `filepicker` widget
(§3.2). And every entry is a facts row keyed by the backbone, so the
questions a file manager answers with a second tool (find, diff, history,
du, integrity) are one query each here, and already tested.

## 3 What the GUI substrate provides today (tier a)

### 3.1 Opcodes

The egui2 IDL carries 97 factory nodes. The ones a browser composes from:
`dockAreaRaw` (tabbed docking, split presets, Go-owned tab set); `endETable`
/ `newTable` / `table` (the table families, `endETable` being the one the
tree widget rides); `codeView` / `codeViewJob` (text with highlighting;
methods `extend section selectable truncate wrap` — no line-number gutter or
scroll-to-line method of its own); `image` / `imageRelease`; `contextMenu`,
`menuBar`, `menuButton`; `window`; `hoverUi` / `hoverText`; `textEdit`
(with the highlight seam); `timeRangePicker`, `datePickerButton`,
`dateTimePickerButton`; `progressBar`, `spinner`; `selectableLabel`,
`atoms` / `labelAtoms`; `styledSections`; `scope` / `tintedScope`; the
painter family; focus and key capture (ADR-0177).

Absent, and relevant: a drag-and-drop payload node (egui has the mechanism;
no opcode exposes it), a native tree node (the tree is Go over `endETable`,
ADR-0176), a breadcrumb node (`filepicker` hand-rolls one from buttons), a
splitter other than the dock, a hex view, a diff view. Adding an IDL node is
a Tier-1 surface under the ADR trigger and renumbers opcodes on both sides.

### 3.2 Go widgets

| Widget | What it contributes | Caveat |
| --- | --- | --- |
| `widgets/tree` (ADR-0176) | columnar `Tree{Labels, Parents, Keys}`, caller-owned `State` keyed by `Keys`, an `Outline` column plus `Columns []Column{Header, Width, Resizable, Cell}`, `Result{Clicked, Activated, Toggled}`, cursor distinct from selection, etable-backed culling | no type-ahead (text events are outside ADR-0177's vocabulary); indent guides deferred; a host that reloads must `Bind` before mutating |
| `widgets/filepicker` | an in-app open / save / pick-folder dialog over **any `fs.FS`** — breadcrumb, listing, stat pane, extension / glob / predicate filters, hidden-file toggle, multi-select; `WithFsBackend(fsys)` | a dialog, not a browser: the listing is a column of buttons, no columns, no sort, no tree; it proves the seam and sets the `io/fs` path model (`"."` root, no leading `/`) |
| `codeview`, `gohighlight`, `jsonhighlight`, `markdown`, `imagedecode` | preview by type: highlighted text, JSON, rendered markdown, PNG/JPEG/GIF | `codeview` interning is not a render cache — cache per `(entry, snapshot)` on the caller's side |
| `timeline` (ADR-0043) | versions of a path on a UTC axis; play's Detail already paints per-row temporal attributes with it, non-interactive | an annotations-only timeline needs an explicit range |
| `treemap`, `icicle`, `sankey`, `layeredgraph` | `du` as a treemap or icicle over the directory hierarchy | the hierarchy contract normalises sizes (play's Treemap panel) |
| `jobprogress`, `taskmonitor` (ADR-0038) | a transfer-queue analogue: in-flight tasks with progress and cancel, a rolling history, failures through `errorview` | needs a producer that emits `task.<id>.progress` — the walker has none today |
| `errorview`, `badge`, `inspector.AnchorToggle`, `lazypane`, `pager`, `selector` | error chains, status badges, "pin this inspector to a window", skipping hidden dock bodies, paging, pick-one strips | — |
| `leewaywidgets` + play's Detail card driver | a leeway row rendered as a card, every attribute with its gloss | lives in play today; reuse is through play's exported seams or a copy of the driver pattern |
| `public/hmi/gloss` (ADR-0186) | `gloss/bytes duration epoch length luhn masked raw taggedid temperature url` — declared renderings for size, times, mount ids, hashes, paths | inline faces are core; block faces are bound by name in play |
| `sqleditor` (ADR-0147) | an SQL tab with completion | — |

### 3.3 Apps and host seams

- **App registry and manifest** — `apps/capinspector` is the shape: one
  package, `app_register.go` with a `Manifest{Id Display Title Icon Topics
  Keywords Surface SurfaceHints Caps PersistedKeys LaunchKind Workingset
  Help}` and a factory; no new `main()` (CODINGSTANDARDS § Entry Points).
- **Docking** — `c.DockArea(id)` with `InitRoot` / `Split` / `Tab` /
  `TabNoScroll` / `ActivateTab`; imztop, imzrt and play are the precedents;
  `lazypane` skips hidden bodies.
- **Launch requests** (ADR-0135) — `windowhost.open` with a leeway-declared
  config; play accepts `PlayLaunch{Sql AutoRun Live BandsSql Tab}`, so "open
  this selection in play" is one audited request.
- **Workingsets** (ADR-0148), **column widths** (ADR-0151), the **app-state
  manager** (ADR-0185, proposed) — the GUI's own state is data: the last
  location, pinned comparisons, layout mode, bookmarks.
- **Powerbox** (ADR-0026 §SD7, `fsbroker`) — `fs.dialog.read` / `.write` /
  `.bundle`; handle ops shipped: `.read .write .close .watch .unwatch`; the
  app never learns a path; declare only `fs.dialog.*`, never `fs.handle.>`;
  pass `fsbroker.DialogTimeout` for dialogs.
- **Clipboard** — `clipboard.write` as a brokered subject.
- **Tasks** (ADR-0038) — `task.<id>.{created,progress,cancel,done,error}`,
  the producer `HandleI`, the `taskmonitor` consumer.
- **play as a surface** — `NewLivePlayApp(client, sql, maxHistory, rules)`
  embeds a full play; sqlapplet (ADR-0132) mints an attenuated one per
  markdown document with `tabs: [panel[:cte][@zone]]`, a params strip and a
  derived security class; content-typed cells (ADR-0123, `label@mime`:
  `text/markdown text/plain application/json application/sql text/x-go
  image/png|jpeg|gif`) and glosses render Detail and Table cells by
  declaration; the Table pane has leeway display modes.
- **Help**, **screenshot lanes** (the AGENTS.md ladder: `TestDriver`, an
  app's own capture env vars per ADR-0009, egui-mcp, the headless tree
  driver of ADR-0154), and the **design system** (ADR-0029 / 0030 / 0031 /
  0032 / 0044 / 0065 — tokens, type scale, density, Phosphor icons with a
  `PhFile*` / `PhFolder*` family per type, surface archetypes).

### 3.4 Data access from an app

play is the precedent and the browser has no reason to differ:
`chclient.ConfigFromEnv()` (the `CLICKHOUSE_URL` / `USER` / `PASSWORD`
names shared with the server tooling, deliberately not `BOXER_`-prefixed) →
`chclient.New` → `storeexec.New` for the generated stores and the adapter, and
an Arrow client for result sets. play declares no `ch.query.*` capability;
its caps are `fs.dialog.read`, the chlocal exec pool, `windowhost.open`,
ad-hoc publish and `clipboard.write`. Every store-wide query runs off the
render thread on a memoised lane (play's node lanes; the `imzero2-fetchers`
rule that fetchers run only from `Sync()`).

### 3.5 Gaps, and the cheapest route past each

| Gap | Consequence for a browser | Cheapest route | Decision class |
| --- | --- | --- | --- |
| no drag-and-drop opcode | no dragging entries between panes | buttons, F-keys, context menus carry every action | IDL node = Tier 1, own ADR, defer |
| no hex viewer | binary preview | a Go-side hex dump rendered through `codeView` | none |
| no diff viewer | "what changed in this file" | a Go-side unified diff through `codeView` (+/- prefixes) | a proper widget is its own decision |
| `codeView` has no scroll-to-line | grep hit → preview jump | render the hit's window (±N lines) with the hit first; whole-file later | none |
| no breadcrumb widget | every pane hand-rolls one | extract `filepicker`'s into the browser widget | none |
| no tree type-ahead | jump-by-typing | a quick-filter `TextEdit` above the listing | none |
| no walk progress seam | a "transfer queue" that shows only start and end | an optional progress callback on `ladingingest` → a task producer | exported API under `public/` = Tier 1, small |
| visibility not bound to the broker | the GUI is the first interactive caller that must pick | `VisibleAll` behind the `MountVisibilityI` seam, the subject decided in the ADR | capability subject = Tier 1 |

## 4 What the reference clients teach

### 4.1 Feature inventories

**WinSCP** *(b)* — two interfaces: *Commander* (two panels, local left and
remote right, each with directory view, drive view, path label and status
bar) and *Explorer* (one remote pane beside a directory tree).
*Synchronize* (direction; modes synchronize / mirror / timestamps; preview).
*Compare Directories* (Commander only — highlights files new or different
against the opposite panel). *Find* (a file mask, a root, one row per match
with attributes). *File masks* with include | exclude halves and size / time
constraints. *Custom commands* (local or remote, applied to directories or
not). Sessions in folders, keep-up-to-date, a queue, an internal editor.

**FileZilla** *(b)* — four panes (local tree + list, remote tree + list) over
a transfer queue and a message log. *Site Manager* (nested folders, per-site
transfer settings, colour labels, a default remote directory). *Directory
comparison* colour-coded by size or modification time. *Filename filters*
(name regex, size, permissions). *Synchronized browsing* (navigate both
trees in step). Remote *file search* with regex. Bookmarks, tabs.

**Cyberduck** *(b)* — one window. *List* and *Outline* views (expandable
folders, arrow keys); tabs; spring-loaded folders; *Filter and Search*
(wildcards); sort by column header; a local-disk browser; *Edit* with an
external application; new folder or file; move / duplicate by drag with
modifiers; copy between servers via two windows; rename inline; delete or
trash; symbolic links; the *Info* window (attributes, permissions, per-cloud
panels); *Quick Look* (space bar); open or copy the HTTP URL; share; open in
Terminal; print a listing; folder icon badges for permissions; *Versions /
Revert* (the Info window's Versions tab, for S3, B2, Drive and others since
8.4); bookmarks with nickname, URL and comment; synchronize with a preview;
Cryptomator vaults.

**The commander family** *(c)* — Norton, Total, Midnight and Double
Commander, Krusader, ForkLift, Commander One: two equal panes; Tab switches
pane; Enter opens or descends, Backspace goes up; Insert or Space selects;
F3 view, F4 edit, F5 copy, F6 move, F7 new directory, F8 delete; Ctrl+F or a
typed prefix filters; a "hotlist" of directories; tabs per pane; a command
line along the bottom; brief / full / thumbnail list modes; a quick-view pane
showing the selected file beside the list.

### 4.2 The conventions that make a client recognisable, and their lading analogue

| Convention | What the user expects | Lading analogue |
| --- | --- | --- |
| two panes with a transfer between them | F5 / drag copies from one to the other | each pane is a `(mount, snapshot, path)` — the *same* mount at two snapshots is a first-class case (time travel), and so is a live local tree; the transfer is *export* (store → local through the Powerbox) or *snapshot* (local / remote → store, a walk as a task); store ↔ store transfer does not exist, *compare* does, and is one `FULL OUTER JOIN` |
| path bar, breadcrumb, directory tree | click to jump, type a path | the same, with a snapshot segment in the location — the SFTP spelling `/<mount>/<snapshot>/<path>` is the address; *latest* is a follow toggle rather than a name |
| sortable detail columns | name, size, type, modified, permissions, owner | name, size, mtime, mode, kind, ext, hash, blocks, text, content policy, err, expiry — plus any component's slots (§5); no owner or group |
| quick filter, file masks | substring / glob / regex, size and date constraints | a quick filter over the loaded listing; a *Find* that compiles to `WHERE` over `fs()` for a subtree, a mount or every visible mount; content search is `fsdata()` with exact line numbers for text files |
| Quick Look, F3 view | preview without downloading | the blocks are already on the server: `ReadFile` through the adapter → `codeView` / image / markdown / JSON / hex dump; a `ref` entry shows size, hash and mtime and says the bytes were not stored |
| Info window, properties | attributes, permissions, checksums, versions | a leeway card (every attribute with its gloss), the BLAKE3 hash, the block map, the text guarantee, the expiry; *Versions* is `fs(m, '*') WHERE path = …` as a timeline and a table; *what else is known* is component presence (§5) |
| compare directories, synchronize preview | colour-coded added / removed / changed and a checklist | the diff query over two snapshots or two mounts, painted into both panes and listed as a checklist; synchronized browsing is a lock on the path across panes — for time travel it is the default you want |
| site manager, bookmarks | named connections in folders with colours | the mount list: policy name, store, tag group, newest snapshot, entries, bytes, expiry; bookmarks are `(mount, snapshot-or-latest, path)` saved as workingset state |
| transfer queue, message log | progress, retries, history | a *Tasks* pane over ADR-0038 tasks (walks, exports) and the snapshot ledger `fssnap` — the bill of lading itself |
| custom commands, open in terminal | run something on the selection | *Open in play* with the selection bound into SQL; *Copy SFTP path*; *Copy rclone mount command* — SQL is the command language |
| rename, delete, mkdir, chmod, upload into place | mutation | none inside a snapshot, by contract; the only mutations are *take a snapshot* and *purge a mount* (a lightweight `DELETE`, audited, behind a non-sticky grant if offered at all — §10) |
| keyboard idioms | Tab, Enter, Backspace, F-keys | ADR-0177 focus-scoped capture per pane; the key vocabulary is a declared subset |

### 4.3 The metaphor shift

The reference clients move bytes between two live places. Lading holds
*states of one place over time*. The recognisable chrome survives the move —
two panes, a path bar, list and outline views, preview, info, find, compare,
a queue, bookmarks — but the primary verbs change: *pin* (choose a
snapshot), *travel* (same path, another snapshot), *compare*, *search across
time and mounts*, *export*, *snapshot*. Keeping the chrome is what makes the
thing recognisable; changing the verbs is what keeps it honest. A pane that
offered "rename" or "delete" would be misdescribing the store, and WinSCP's
users would expect the rename to stick.

## 5 What the leeway shape adds (tier a)

Each item names the mechanism, so the claim is checkable.

1. **Components formulated after the fact.** Every entry is a facts row
   keyed `(id = mount, ts = snapshot, naturalKey = path)`. A component is
   satisfied structurally, per row, at read time (the `leeway-components`
   skill; ADR-0146). So a scanner, a classifier or a reviewer's CLI can
   write its *own* kind — MIME sniff, licence, "contains secrets", owning
   team, ticket, review status, a retention override — keyed by the same
   triple, into `boxer.facts` or a facts-shaped table of its own (the
   ADR-0198 O3 pattern), and the browser shows it beside name and size
   without lading changing. The read is a join on the backbone: `fs(m)`
   against the kind's projection via `LW_COMPONENT('Kind')` (ADR-0189), or
   presence via `LW_COMPONENT_FILTER`; the archetype — the set of kinds a
   row carries — is `ArchetypePresence` in Go and the ADR-0193 (proposed)
   survey in SQL. What a GUI does with it: optional columns discoverable
   from the registry (`keelson('memberships')`, ADR-0174's rosters), an
   "also known about this file" section in Info, an archetype summary per
   mount, filter by presence. Reading existing kinds costs nothing new;
   *writing* annotations from the GUI needs vocabulary entries (a Tier-1
   registry) and a write path, and is a separate decision (§10).
2. **Glosses and content-typed cells.** Size is `gloss/bytes`, times are
   `gloss/epoch`, the mount is `gloss/taggedid`, the hash is hex or masked,
   the SFTP path is `gloss/url`; a preview is `label@mime`. The Table and
   Info panes are declared renderings, and the same statement pasted into
   play renders the same way — the pasteable-complete stance of sqlapplet.
3. **Time as a key column.** The snapshot is `ts`. History, diff, "when did
   this path first appear", "what changed in the last week across every
   mount I can see", "what disappears tomorrow" — each is a `WHERE` clause;
   the timeline widget paints versions; the time-range picker selects
   snapshot windows; *latest* versus *pinned* is explicit in the location.
4. **Cross-mount and store-wide questions.** Identical content across mounts
   by `content_hash`; which mounts carry a path; newest snapshot per mount;
   mounts per tag. The Find pane's scope selector is *this directory / this
   mount / every visible mount*, and only the predicate changes.
5. **Integrity and problems as views.** `BLAKE3(data) != hash` per file or
   snapshot → a badge; `err` rows → a Problems pane; `text` → "search is
   exact here"; `content` → "not stored" / "referenced" badges; `expires_at`
   → a column. None of these is a feature to build; each is a query to show.
6. **The vocabulary is the GUI's schema.** The 28 memberships and four kind
   markers of `ladingvocab` are the column roster; `keelson('memberships')`
   names them; the data catalog (ADR-0170) classifies the tables; the
   vocabulary panel (ADR-0174) lists the `fs*` macros. The browser's SQL tab
   and play agree by construction.
7. **The GUI's own state is data.** Locations, layout mode, pinned
   comparisons, bookmarks go into a launch-kind DTO (ADR-0135 / 0148),
   column widths into ADR-0151's store, and the app-state manager
   (ADR-0185, proposed) can list and clear them — the data-centricity
   invariant applied to the tool itself.

## 6 Options for the shape (QOC)

**Question.** What kind of thing is the lading browser, and where does its
code live?

**Options.**

- **O1 — a sqlapplet book only.** Chapters over `fs()` / `fssnap()` /
  `fsdata()` with `tabs:` zones (table, detail, treemap, timeline) and
  params for mount, snapshot and directory. Zero Go.
- **O2 — a play panel genre.** A "Files" result panel that accepts a
  `(path, is_dir, size, mtime, …)`-shaped result — a hierarchy contract like
  the Treemap panel's — and renders list or tree plus preview; available in
  play and in every applet. Query-first.
- **O3 — a registered app.** Under `apps/`, composed from a reusable
  browser widget (list and outline over `fs.FS`, with a metadata column
  provider), the dock, preview / info / diff / find / tasks panes and SQL
  lanes. Browse-first, WinSCP- and Cyberduck-shaped.
- **O4 — O3, then its widget offered as O2's panel.** One widget, two hosts.

**Criteria.**

- **C1** — recognisability to WinSCP / Cyberduck / FileZilla users: browse-
  first chrome, two panes, preview, info, compare, find, bookmarks, queue.
- **C2** — exploits the leeway shape (§5).
- **C3** — cost now: reuse of shipped seams against new code and Tier-1
  surfaces touched.
- **C4** — reuse beyond lading: any `fs.FS` (embed, zip, grant, rclone
  remote), any facts-shaped hierarchy.
- **C5** — posture: read-only by construction, the visibility seam, no new
  capability surface.
- **C6** — scale: mounts of 10⁶ entries, lazy loading, server-side paging,
  sort and filter.

**Assessment.**

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | −  | ++ | ++ |
| C2 | ++ | ++ | +  | ++ |
| C3 | ++ | +  | −  | −− |
| C4 | −  | +  | ++ | ++ |
| C5 | ++ | ++ | +  | +  |
| C6 | +  | +  | +  | +  |

O1 is the cheapest proof and fails C1 outright — it is a notebook, not a file
manager — but it costs nothing to ship first and every query it carries is
reused by O3's panes, so it is the M0 of any option rather than an
alternative to one. O2 inherits play's posture: a query first, files after;
a file manager is the other way round. O3 is the shape the question asks
for; its cost is the new widget, and C4 repays it — a browser over any
`fs.FS` is wanted by the `filepicker`, by a viewer over a capability grant
and by rclone remotes. O4 is O3 plus a second host and is the follow-up once
the widget exists, not a different first move. **Recommendation: O3, with
O1 as its M0 and O2 recorded as a follow-up with a trigger** (the first
applet that wants a file list).

## 7 The recommended shape

A sketch for the dialogue, not a specification. Names are provisional (§10).

### 7.1 Layout

```text
┌ actions: Snapshot…  Export…  Compare  Find  Open in play  Copy path ─────────────────┐
├─ Mounts ──────┬─ Pane A  mount ▸ snapshot ▸ path      [list | outline]  ⟲ latest ─┬─ Pane B ─┤
│ by tag        │ name ▾   size   mtime   kind  ext  hash  …  ⊕ component columns   │ (same)   │
│  mount name   │ ▸ dir/                                                             │          │
│   latest · n  │   file.txt                 12.4 KiB   2026-08-19 13:02  text        │          │
│   expires     │ quick filter ▢                                                      │          │
│ snapshots     │                                                                     │          │
│  ▁▃▅ timeline │                                                                     │          │
├───────────────┴─────────────────────────────────────────────────────────────────────┴──────────┤
│ Preview │ Info │ History │ Diff │ Find │ Du │ Problems │ Tasks │ SQL                            │
└───────────────────────────────────────────────────────────────────────────────────────────────┘
```

One dock area. Pane B is collapsible: shown, the window is a Commander;
hidden, it is an Explorer / Cyberduck browser with an inspector below. The
mode persists with the workingset.

### 7.2 Panes

- **Mounts** — every visible mount grouped by tag value; policy name or hex
  id; newest complete snapshot, entries, bytes, expiry; a snapshot list with
  a timeline strip. Click sets a pane's mount; a snapshot click pins it;
  *latest* follows.
- **Browser pane** (the reusable widget) — location tuple with breadcrumb;
  list mode on `endETable` (sortable columns, `VisibleRange` gating) and
  outline mode on the tree widget (columns on the same rows); quick filter;
  selection (single, toggle, range); keyboard per ADR-0177; extra columns
  from a `MetaProviderI`, so the lading host supplies hash / text /
  content / err / expiry and component slots while a plain `os.DirFS` host
  supplies nothing. Data from `fs.FS` calls — `ReadDir` per directory on
  expand, cached per `(mount, snapshot)` because nothing can invalidate.
  Above a row threshold a directory switches to a paged SQL listing
  (`ORDER BY` server-side).
- **Preview** — `ReadFile` through the adapter, rendered by type: text with
  highlighting (`codeView`), images, markdown, JSON; a hex dump for the rest;
  "not stored" and "referenced" stated, never an empty box. Bounded by the
  inline threshold by construction.
- **Info** — the leeway card of the entry row with glosses; the hash; blocks
  and block size; text guarantee; policy; expiry; the components present on
  the row (§5.1).
- **History** — `fs(m, '*') WHERE path = …`: a timeline of versions and a
  table of size / mtime / hash per snapshot; click travels Pane A.
- **Diff** — Pane A against Pane B (two snapshots of one mount, or two
  mounts): added / removed / modified / same, painted into both listings
  and listed as a checklist; per-file text diff through `codeView`.
- **Find** — name (glob → `match` / `LIKE`), content (the `fsdata()` grep
  with line numbers on text files), size, mtime, ext, kind; scope this
  directory / this mount / every visible mount; results as a flat list;
  activate → travel and preview at the hit.
- **Du** — the one-pass directory-size query as a treemap or icicle.
- **Problems** — `err != ''` rows and the block audit per snapshot.
- **Tasks** — walks and exports in flight (`taskmonitor`), the `fssnap`
  ledger as history.
- **SQL** — the current selection bound as parameters (`{m:UInt64}`,
  `{snap:…}`, `{dir:String}`); *Open in play* by launch request first;
  an embedded play instance is the heavier option.

### 7.3 Data paths

The adapter serves browse and preview (one `fs.FS` per `(mount, snapshot)`,
cached). Everything store-wide — Mounts, History, Diff, Find, Du, Problems —
is SQL through `ladingsql.Expand` with the visibility seam, on lanes off the
render thread memoised on `(sql, revision)`, results as Arrow into the
tables. The query spellings are the ADR-0198 §7 catalogue, parameterised
once and shared by the app, the M0 book and the existing tests (§10.9).

### 7.4 Actions

*Snapshot…* — an rclone remote string (`ladingremote.Serve`) or a local tree
(§10.4), with a policy, run as a task; the mount appears in Mounts when its
root row lands. *Export…* — one file through `fs.dialog.write`; a folder
needs handle operations the Powerbox does not have yet (§10.5); *Copy rclone
mount command* covers the rest today. *Open in play*, *Copy SFTP path*.
*Purge mount* — deferred (§10.11).

### 7.5 Where the code lives

- `apps/<name>/` — registration, manifest, dock, the lading-specific panes,
  help book, scripted-capture env vars.
- `public/thestack/imzero2/egui2/widgets/fsbrowser/` (name provisional) —
  the pane widget over `fs.FS` + `MetaProviderI`, with a gallery demo over
  an `embed.FS`; `filepicker` adopts it later rather than now.
- `public/fs/lading/ladingqueries/` (name provisional) — the §7 catalogue
  as exported, parameterised builders, so the app, the book and the tests
  carry one spelling. Exported API under `public/` is Tier 1; the content is
  SQL text the tests already pin.

## 8 Milestones

Each names its deliverable, its acceptance and its stop point. Order
rationale: M0 de-risks the queries with no Go; M1 is the only genuinely new
widget and the reusable one; M2 is the first recognisable thing; M3 is the
lading-specific payoff; M4 and M5 the metadata payoff; M6 touches processes
and capabilities, so it goes late; M7 is durability through shipped seams.

- **M0 — the lading book** (sqlapplet, O1). Chapters: mounts (`fssnap` with
  the policy record), snapshots of a mount, a directory (param `dir`, table
  + detail zones), find, history of a path, diff of two snapshots, du
  (`treemap:nodes`), problems / audit. *Acceptance:* book test green, each
  chapter pasteable into play, one screenshot. *Stop:* none.
- **M1 — the browser widget.** List and outline modes over `fs.FS`,
  breadcrumb, quick filter, sort, selection, keyboard, `MetaProviderI`;
  gallery demo over `embed.FS` and — in the integration lane — a lading
  snapshot; a headless driver scene. *Acceptance:* demo screenshot, driver
  scene asserting state, `filepicker` unchanged. *Stop:* the widget's name
  and package.
- **M2 — the app.** Manifest, dock, Mounts, one browser pane, Preview, Info;
  help book; scripted capture. *Acceptance:* opens on a provisioned store,
  lists mounts, browses a snapshot, previews text and an image, shows the
  card. *Stop:* the app's name; the visibility seam wired to `VisibleAll`.
- **M3 — two panes and time.** Pane B, Diff / Compare, synchronized
  browsing, History with the timeline, the SQL tab by launch request.
  *Acceptance:* the diff of two snapshots matches the §7 query; travel
  round-trips. *Stop:* embedded play or not.
- **M4 — Find, Du, Problems, Tasks.** *Acceptance:* a content hit opens the
  preview at the hit; `du` matches the query; an `err` row is listed.
  *Stop:* the walker's progress seam (Tier 1, small) — without it Tasks
  shows start and end only.
- **M5 — components.** Optional columns from the registry, "also known" in
  Info, archetype summary per mount; an integration-lane example with a
  test vocabulary, no new production kind. *Stop:* none for reading;
  writing annotations is §10.10.
- **M6 — actions.** Snapshot… (rclone remotes first), Export… (single
  file), Copy commands. *Stop:* §10.4 and §10.5 — local-tree ingest and
  folder export need decisions outside this app.
- **M7 — durability and polish.** Workingset DTO, column widths, app-state,
  the screenshot tour, the ADR's acceptance flip.

## 9 House rules that bind the build

- Never resolve a query on the render thread (play's lanes; the
  `imzero2-fetchers` rule).
- Table rows: gate emission on `VisibleRange()`, fixed row heights (ADR-0176
  SD12 is a height budget).
- Tree: key `State` by path through `Keys`; `Bind` before mutating after a
  rebuild; project selection from the app's own key.
- Ids: one `c.IdScope` per pane, `ids.PrepareSeq` for rows; two panes with
  one id stack collide silently.
- Sizing: `CapturePaneSize`, not `CaptureAvailableSize`; derive splits,
  never retain them (ADR-0178 contract 6).
- Glyphs: Phosphor for control glyphs (the glyph-fallback rule is a lint
  gate).
- Leeway reads through the read surface — `fs()`'s projection, `LW_GET`,
  `LW_COMPONENT` — never hand-written lane arithmetic.
- SQL recipes stay SQL (the thin-pass boundary): the app carries templates,
  not compiled statistics.
- Capabilities: declare `fs.dialog.*` only, never `fs.handle.>`; use the
  exported dialog and handle-op timeouts.
- Screenshots by the AGENTS.md ladder; prose humble and private; the ADR
  near the recent median length, analysis on this page.
- Tiers: a new app and a new widget are Tier 2 (a new shape, so an ADR); a
  DnD opcode, a capability subject, a vocabulary entry, the walker's
  progress API are Tier 1 and get their own records.

## 10 Open forks for the design dialogue

1. **Names** — the app and the widget. The store keeps `lading`; the
   browser could follow the metaphor (a *tally* is the count of cargo
   checked against the bill of lading) or not. Listed, not chosen.
2. **Default chrome** — Commander (two panes) or Explorer / Cyberduck (one
   pane and an inspector). Compare is the store's strength, which argues for
   two panes by default with Pane B collapsible; a single-pane start is less
   demanding on first sight.
3. **Visibility** — `VisibleAll` behind the seam for v1, or the capability
   subject decided now (ADR-0198's open point; Tier 1).
4. **Ingest from the GUI** — rclone remote strings only (no path grant
   needed); local trees through the Powerbox (needs a walkable handle, new
   `fs.handle` operations = Tier 1); or CLI-only for v1.
5. **Export** — one file through `fs.dialog.write` now; folder export needs
   directory-creating handle operations; or the rclone command only.
6. **O2** — whether the browser widget also becomes a play panel, and when.
7. **Diff and hex** — Go-side renderings through `codeView` for v1, or
   proper widgets (each its own decision).
8. **Drag-and-drop** — an IDL node, deferred; F-keys, menus and buttons carry
   every action meanwhile.
9. **Shared query templates** — exported builders beside `ladingsql`, or
   app-local strings duplicated into the book.
10. **Annotations from the GUI** — tags, labels or verdicts as a new kind:
    vocabulary, write path, capability — an ADR of its own; reading is M5.
11. **Purge mount** — offered (audited request, non-sticky grant) or left to
    the CLI and SQL.
12. **The walker's progress seam** — an optional callback on
    `ladingingest.Snapshot`, so a walk can be an ADR-0038 task with a bar.

## References

- Repository: [ADR-0198](../adr/0198-fs-snapshot-store.md) (proposed) and
  its [how-to](../howto/lading-snapshot-store.md), [compact
  design](./iofs-clickhouse-snapshot-store-compact.md) and [plan
  page](./iofs-clickhouse-snapshot-store-plan.md); ADR-0026 (app runtime,
  capability subjects, Powerbox), ADR-0038 (tasks), ADR-0043 (timeline),
  ADR-0097 (play's query graph and panel contract), ADR-0123 (content-typed
  cells), ADR-0132 (sqlapplet), ADR-0135 (launch requests), ADR-0146 /
  ADR-0183 (component read contract, id regime), ADR-0148 (workingsets),
  ADR-0151 (column widths), ADR-0154 (headless driver), ADR-0166 (treemap,
  proposed), ADR-0170 (data catalog), ADR-0174 (vocabulary panel), ADR-0176
  (tree widget), ADR-0177 (focus-scoped keys), ADR-0178 (mdedit's sizing
  contracts), ADR-0185 (app-state manager, proposed), ADR-0186 (glosses),
  ADR-0189 (`LW_COMPONENT`), ADR-0193 (component survey, proposed); the
  [app composition survey](./app-composition-survey.md) for the substrate
  inventory this page leans on; the `leeway-components` skill.
- WinSCP documentation (tier b): [Synchronize](https://winscp.net/eng/docs/ui_synchronize),
  [Compare Directories](https://winscp.net/eng/docs/task_compare_directories),
  [Find](https://winscp.net/eng/docs/ui_find), [Masks](https://winscp.net/eng/docs/file_mask),
  [Custom commands](https://winscp.net/eng/docs/custom_commands).
- FileZilla documentation (tier b): [feature list](https://filezillapro.com/docs/v3/features-v3/filezilla-pro-features/),
  [directory comparison](https://filezillapro.com/docs/v3/features-v3/local-remote-files/),
  [synchronized browsing](https://filezillapro.com/docs/v3/faq/synchronized-browsing/).
- Cyberduck documentation (tier b): [Browser](https://docs.cyberduck.io/cyberduck/browser/),
  [Bookmarks](https://docs.cyberduck.io/cyberduck/bookmarks/),
  [release notes](https://version.cyberduck.io/changelog.html) (Versions tab).
