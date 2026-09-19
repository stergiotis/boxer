---
type: adr
status: accepted
date: 2026-08-20
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-21
---

# ADR-0200: tally — a browser for the lading store, and an `fs.FS` browser widget

## Context

The lading store ([ADR-0198](./0198-fs-snapshot-store.md), M0–M6 shipped)
reads back three ways — Go `fs.FS`, SQL macros, SFTP for rclone — and none of
them is a GUI. A power user of WinSCP, Cyberduck or FileZilla should
recognise a lading browser on first sight, and the browser should use what
the leeway shape gives every entry row: components formulated after the fact,
glosses, the snapshot instant as a key column, cross-mount SQL. The design
space, the feature inventories of the reference clients, the substrate
inventory and the metaphor shift are worked through in
[the survey page](../adr-background-work/lading-browser-survey.md); this ADR
records what fell out and the answers to its open forks.

Two facts carry the design. The adapter *is* an `fs.FS`, so a browser written
against `fs.FS` browses a snapshot with no lading code — and a browser over
`fs.FS` is wanted beyond lading (the `filepicker` dialog, a viewer over a
capability grant, an rclone remote). And every entry is a facts row keyed
`(mount, snapshot, path)`, so find, diff, history, `du` and integrity are one
query each, already pinned by `ladingsql`'s operations tests.

## Design space (QOC)

**Question.** What kind of thing is the lading browser, and where does its
code live? (Full matrix and reading: survey §6.)

**Options.** **O1** a sqlapplet book only · **O2** a play "Files" result panel
· **O3** a registered app composed from a reusable `fs.FS` browser widget ·
**O4** O3 with the widget later offered as O2's panel.

**Criteria.** C1 recognisability · C2 exploits the leeway shape · C3 cost now ·
C4 reuse beyond lading · C5 posture (read-only, visibility) · C6 scale.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | −  | ++ | ++ |
| C2 | ++ | ++ | +  | ++ |
| C3 | ++ | +  | −  | −− |
| C4 | −  | +  | ++ | ++ |
| C5 | ++ | ++ | +  | +  |
| C6 | +  | +  | +  | +  |

O3, with O1 as its M0 and O2 recorded as a follow-up whose trigger is the
first applet that wants a file list.

## Decision

We will build **`apps/tally`** — a registered, windowed keelson app that
browses, previews, inspects, compares and searches lading snapshots — on a new
reusable widget, **`widgets/fsbrowser`**, that renders any `fs.FS` as a list or
an outline with host-supplied metadata columns. The first milestone is a
sqlapplet book over the SQL surface. Nothing in the app mutates a snapshot.

### SD1 — Names and homes

`apps/tally` (a tally is the count of cargo checked against the bill of
lading), id `github.com/stergiotis/boxer/apps/tally`, topic Data, no new
`main()`. `public/thestack/imzero2/egui2/widgets/fsbrowser` for the widget.
The book is `apps/sqlapplet/booklading`, book id `lading`. The store keeps its
name; the tables keep `fs*`.

### SD2 — The browser widget contract

`fsbrowser` takes an `fs.FS`, a location (an `io/fs` path, `"."` the root),
caller-owned `State` (expansion keyed by path, selection, cursor) and an
optional `MetaProviderI` that supplies extra columns and badges per entry —
the seam through which the lading host adds hash, text guarantee, content
policy, error, expiry and component slots while an `os.DirFS` host adds
nothing. Two modes over the same rows: *list* on `endETable` (sortable
columns, `VisibleRange`-gated emission) and *outline* on the ADR-0176 tree
widget with the same columns. A breadcrumb, a quick filter over the loaded
listing, single / toggle / range selection and the ADR-0177 key subset
(Enter, Backspace, arrows, Space) belong to the widget; directories are read
on demand through `fs.ReadDir` and cached by the host's `CacheKey` (for a
snapshot, forever — nothing can invalidate it). `Result` reports navigation,
activation and selection changes; the widget never decides what a click
means beyond that.

### SD3 — A location is a triple

A pane shows `(mount, snapshot | latest, path)` — the SFTP spelling
`/<mount>/<snapshot>/<path>` is the address. *latest* is a follow toggle, not a
name. Two panes are independent by default; *synchronized browsing* locks the
path across them, which for one mount at two snapshots is time travel.

### SD4 — Data paths

Browse and preview go through `ladingadapter` (one `fs.FS` per `(mount,
snapshot)`, cached, millisecond calls). Everything store-wide — Mounts,
History, Diff, Find, Du, Problems — is SQL through `ladingsql.Expand` on
lanes off the render thread, memoised on `(sql, revision)`, results as Arrow
into the tables. Query spellings are app-local templates of the ADR-0198 §7
catalogue; extraction into an exported package waits for a third consumer.

### SD5 — Read-only, and what v1 leaves out (decided 2026-08-20)

- **No in-app ingest in v1.** Snapshots are taken by Go or the CLI; the
  Powerbox has no walk operation for a granted folder, and a free-text path
  would bypass it. Recorded follow-ups: `stat` / `walk` handle operations
  (ADR-0026 §SD3 named them; never shipped), or rclone remote strings.
- **No export in v1** beyond *Copy SFTP path* and *Copy rclone mount command*
  to the clipboard; single-file export via `fs.dialog.write` is the first
  addition when wanted.
- **Visibility is `VisibleAll` behind the `MountVisibilityI` seam.** The app
  holds the operator's ClickHouse credentials as play does, so it sees what
  they can read; the capability subject ADR-0198 left open stays deferred,
  trigger: remote access (ADR-0082) or a multi-owner store.
- **No purge, no annotations written from the GUI, no drag-and-drop, no
  walker progress seam** — each has its own trigger in the survey §10.

### SD6 — Macro arguments: slots, every mount, and `'latest'`

`fs()`, `fsdata()` and `fssnap()` accept a `{name:Type}` slot for the mount
and for the snapshot, resolved at expansion from the environment's bound
params (`SET param_name = …` in the prelude); an unbound slot is refused with
a message naming the slot, because the visibility check and the snapshot
resolution need the value at expansion. This is what lets a book chapter or a
play buffer take the mount as a knob rather than a literal. Two spellings
ADR-0198 §SD11 left open are decided with it: **`'*'` as the mount** names
every mount the caller may see — `fssnap('*')` is the store's ledger, `fs('*')`
every visible mount's newest snapshot, resolved per mount as a set of `(mount,
snapshot)` pairs — and is admitted only under a visibility the expansion can
enumerate (`VisibleAll`, or a `VisibleSet` rendered as an `IN` list; a yes/no
oracle refuses it); and **`'latest'` as the snapshot** spells what omission
means, so a bound snapshot knob has a value for "newest" — the same word the
SFTP head uses. `References` reports the resolved mount, a wildcard, or an
unbound call, so play's dispatch still routes the statement server-side.
play registers the mount policy kind (`ladingpolicy.PolicyComponentSQL`) so a
statement can name a mount by its declared name through `LW_COMPONENT`.

### SD7 — Chrome

One dock area: Mounts at the left; Pane A and Pane B (collapsible, shown by
default — compare is the store's strength); bottom tabs Preview, Info,
History, Diff, Find, Du, Problems, SQL. Actions: Compare, Find, Open in play
(a `windowhost.open` launch request with the selection bound into SQL), Copy
path / Copy rclone command (`clipboard.write`). Verbs the reference clients
have and this store cannot — rename, delete, mkdir, chmod, upload — are not
shown.

### SD8 — Renderings are declared

Sizes, times, mount ids and hashes render through the gloss catalog (ADR-0186);
preview is by type — text with highlighting, markdown, JSON, PNG/JPEG/GIF —
through the seams ADR-0123 already binds; binary preview is a Go-side hex dump
and a per-file diff a Go-side unified diff, both through `codeView`. A proper
diff or hex widget is its own decision.

### SD9 — Durable state

Pane locations, layout mode and synchronized-browsing flag compose a launch
kind (`tallyLaunch`, ADR-0135) so a window restores as a workingset
(ADR-0148); column widths ride ADR-0151. Nothing else persists.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `ladingsql` (exported API under `public/`) | `fs()` / `fsdata()` / `fssnap()` accept prelude-bound `{name:Type}` slots, `'*'` as the mount and `'latest'` as the snapshot (SD6); `References` gains `All` / `Unbound` | goldens (two new); play's `RegisterComponents` adds the policy kind; the passreg factory is unchanged |
| `public/thestack/imzero2/egui2/widgets/fsbrowser` | new exported widget package | the gallery (`registry.Demo`), glyph baseline, designlint scope |
| `apps/tally` | new app id, manifest (caps `windowhost.open`, `clipboard.write`), help book | the host's app roster and the capslock check's import list; the Apps menu |
| `public/fs/lading/ladingview` | new exported package: `Guard` / `Locked` (one lock over a store's adapter views) and `ReadHead` (a bounded stat-and-read for previews) | nothing else; it is what keeps file-handle plumbing out of the app package (see the 2026-08-20 update) |
| `apps/play/scenes/34_fsbrowser.scene.md`, `apps/tally/scenes/tally.scene.md` | new headless scenes (ADR-0154 lane) | the verification plan below |
| `public/keelson/vdd` (the facts vocabulary, ADR-0135 §SD2) | eight members added to the windowhost cohort: `tallyLaunchMountA/SnapA/DirA/MountB/SnapB/DirB` (textArray), `tallyLaunchSync` (bool), `tallyLaunchTarget` (symbol), ordinals 141–148 | the committed assignment golden; `apps/tally/launchcfg` (kind `tallyLaunch`, generated codec, `kindcheck` registration) |
| `apps/sqlapplet/booklading` | new book id `lading` | the book corpus test |
| `boxer fs snapshot` (CLI, `public/app/commands/ladingfs`) | new verb: walk a directory or an rclone remote into the store, optionally recording the policy under `--name` — the ingest route SD5 relies on | the how-to §3 |
| egui2 IDL, capability subjects, vocabularies, `boxer.facts` | **unchanged** | nothing |

## Alternatives

- **A sqlapplet book only (O1).** Query-first; fails recognisability. Kept as M0.
- **A play panel (O2).** Query-first posture; recorded as the follow-up.
- **Extending `filepicker` into the browser.** A dialog's contract (commit a
  path) is not a browser's; the widget is new and the dialog may adopt it.
- **A DnD opcode, a native tree node, a hex/diff node now.** Tier-1 IDL work
  for conveniences buttons and Go-side renderings cover.
- **Binding visibility to a new capability subject now.** Decided against for
  v1 (SD5); the seam keeps the door open.

## Consequences

### Positive

- A recognisable browser over snapshots with compare, history, find, `du`
  and integrity as views rather than features; the widget serves any `fs.FS`.
- The book ships first and every query in it is the app's, pinned twice.
- Macro knobs make lading usable from play and applets without literals.

### Negative

- A new exported widget to maintain; two panes and lazy trees carry the
  etable/tree id and culling disciplines (survey §9).
- Without ingest or export the app is a reader; the follow-ups are recorded,
  not built.

### Neutral

- `VisibleAll` is the operator's own credentials; a multi-user deployment
  revisits SD5.

## Migration — Tier 1

- **Breaks.** Nothing: additive macro arguments, new packages, a new app id,
  a new book, eight new vocabulary members (append-only ordinals).
- **Path.** None for existing callers; a literal mount keeps expanding as
  before.
- **Regeneration.** The vocabulary assignment golden
  (`BOXER_VOCAB_GOLDEN_REGEN=1 go test ./public/keelson/vdd/`) and
  `apps/tally/launchcfg/launchcfg.out.go` (its golden test with `-update`);
  no IDL, no store.
- **Old shape.** None.

## Verification plan — Tier 1

- **Lane: default `go test`.** `ladingsql`: slot resolution from the prelude,
  the unbound refusal, `References` on unresolved calls, goldens. `sqlapplet`:
  the lading book parses, classifies read-only, every knob prelude-bound,
  tab selections as declared. `fsbrowser`: model and layout tests over
  `fstest.MapFS`; a headless driver scene (ADR-0154) asserting navigation and
  selection state. `tally`: manifest validity, launch-kind round trip.
- **Lane: `//go:build integration`.** A seeded store; each book chapter's SQL
  executed through the expansion; the app's lanes against the seeded mount.
- **Lane: screenshots.** The widget in the gallery tour; the app through its
  capture env vars.
- **Gates.** doclint, codelint, designlint, the glyph and capslock
  baselines, `go mod tidy --diff`.
- **What would fail.** A slot resolving to the wrong mount; a chapter that
  mutates; a pane whose listing disagrees with `fs.ReadDir`; a diff that
  disagrees with the §7 query; an action that writes.
- **Gap.** Scale (10⁶-entry mounts) is asserted by construction — lazy reads,
  gated emission — not measured until a large mount exists in a lane.

## Status

Accepted 2026-08-21. Milestones:

- **M0 — the lading book.** ✓ SD6 in `ladingsql`; `booklading` with nine
  chapters — ledger, browse, find, content search, history, diff, du,
  problems, block audit — each pinned by the book test and executed against a
  live server in the integration lane (2026-08-20).
- **M1 — `fsbrowser`.** ✓ SD2 as `widgets/fsbrowser` (list and outline,
  breadcrumb, quick filter, sort, selection, keyboard, host columns), the
  gallery demo "file browser" over an in-memory tree, the headless scene
  `apps/play/scenes/34_fsbrowser.scene.md` (2026-08-20).
- **M2 — the app.** ✓ `apps/tally`: manifest, dock (Pane A over Preview /
  Info), Mounts with snapshots and follow-latest, Preview by type, Info from
  `fs()`, help book, the headless scene `apps/tally/scenes/tally.scene.md` against
  a seeded store (2026-08-20).
- **M3 — two panes and time.** ✓ Pane B beside Pane A (always shown; the
  dock divider is the collapse), a Target switch for which pane the Mounts
  clicks address, Sync browsing, the Diff tab (A's directory or the whole
  snapshot, coloured added / removed / modified, a click travels pane B),
  the History tab (timeline flags and the versions table, a click pins the
  snapshot), Open in play, Copy rclone mount (2026-08-20).
- **M4 — Find, Du, Problems.** ✓ Find (path pattern, extension, minimum
  size, or a content needle with exact line numbers; scope this directory /
  this snapshot / all mounts; a click travels), Du (the one-pass du table
  and a treemap of the largest files under the directory), Problems (the
  unreadable entries, and a BLAKE3 block audit run on demand) (2026-08-20).
- **M5 — components.** ✓ The Info pane's Components section: which
  registered kinds carry the entry — the store's own on the entry row (a
  root row is an entry and a snapshot) and every kind in the default
  component registry probed over its own table by the entry's key; the
  integration-lane example is the store's own root row rather than a test
  vocabulary (2026-08-20).
- **M6 — durability.** ✓ `tallyLaunch` (the two pane locations, sync,
  target) as a leeway-declared launch kind with a generated codec, declared
  on the manifest with `Workingset: true`; the window restores as a
  workingset and opens from a launch request; dirty means a choice made
  after the mounts were known (2026-08-21). Column widths persist through
  ADR-0151's resolver on every table — both panes in both modes, and the
  five result tables (2026-08-21).
- **M7 — the play panel.** ✓ The widget's second host (§Design space O4): a
  `Files` result tab in play, over a `path` column contract; the result is
  interned into a read-only `fs.FS` and browsed with `fsbrowser` in list or
  outline mode, the query's remaining columns riding as the browser's own, and
  a click published as both a path and a row. It reaches every applet with the
  rest of play's tab registry (2026-08-21).

## Updates

### 2026-08-20 — M0, M1 and M2 shipped; what they corrected

**M0.** Three macro spellings rather than one (§SD6 as written now): slots,
`'*'` as the mount, `'latest'` as the snapshot. The book needed the wildcard
— without it every chapter wanted a mount id typed in before it showed a
row, and a book whose every page opens empty is not a first milestone. play
registers the mount policy kind so a chapter can name a mount. `boxer fs
snapshot` joined the CLI because §SD5's "snapshots are taken by the CLI"
named a verb that did not exist.

**M1.** The widget is as §SD2 says, with one addition the outline needed:
an unread directory carries a placeholder child under a NUL-suffixed key so
it shows a disclosure control before anyone opened it, and a build loop that
re-binds the tree state after every growth so expansion keyed by path can
drive which directories are read. Verified by the headless lane, not by the
capture env vars §Verification sketched — an accessibility-tree scene asserts
the selection, the navigation and the outline mode in one run and leaves
captures behind; the app follows the same lane.

**M2.** Two things the design did not foresee. The capability gate (ADR-0026
§SD10, `capslock`) resolves an interface call on `fs.File` / `fs.FileInfo` to
every implementation, `*os.File` among them, so an app package that reads
through an adapter view still reports `CAPABILITY_FILES` — a true statement
about the call graph and a false one about the app. The file-handle plumbing
(the locking wrapper over a view, the bounded stat-and-read a preview wants)
therefore lives in `public/fs/lading/ladingview`, beside the adapter it
serves, and the app handles bytes. And egui_dock's tabs are not in the
accessibility tree, so a scene switches tabs by position; the scene script
says so where it does it. Also recorded: the browser's directory reads run on
the render thread through the adapter (a query each, cached forever per
snapshot), which the slow-frame log shows as one 20–40 ms frame per first
visit of a directory; a prefetching view is the follow-up if that ever reads
as a hitch.

**M3.** Pane B is always present rather than collapsible: egui_dock
reconciles a tab added after the first frame into the *first* leaf, so a
pane toggled on later would land beside Pane A as a tab, not to its right,
and the preset that puts it to the right has to include it from the start.
The dock divider is the collapse. The preset order matters too — the bottom
leaf is split off first so it spans the window, then the upper leaf is split
right for Pane B (imztop's shape). The lower tabs describe the *target*
pane, which is whichever pane was clicked last, and the Mounts clicks address
it; the diff reads A as the older side and B as the newer one, and the scene
points B at a second mount for want of a second snapshot of the first.

**M4.** Three tabs, all lanes over the §7 catalogue through the same
`runTable` → `stringTable` path as Diff and History, so the app's SQL lives
in one file of builders pinned by tests that expand every one of them. Find
runs only when its Search button arms a key built from the knobs and the
place, not on every keystroke. The treemap draws the largest 4 000 files
under the directory — exact below the cap, a top-N picture above it — because
a tree built from directory totals would double-count what a treemap sums
from its leaves. The audit is behind a button because it costs what the
snapshot weighs.

**M5.** Smaller than §SD2 sketched and more honest: no optional component
columns in the listing yet, because no domain writes a component over lading
entries today and a column that is empty on every row would be a claim
without evidence. What shipped is the read path and its worked example on
data that exists — the root row reading as `LadingEntry` and
`LadingSnapshot`, an ordinary file as `LadingEntry` alone — through the same
presence predicates ADR-0189 publishes, keyed by the entry's backbone
triple, over every registered kind's table. Columns come with the first
writer. The probes skip a table they cannot query rather than failing the
pane: a registered set over another shape is not this entry's concern.

**M6.** The launch kind is the smallest record that reproduces a window —
two locations, the sync flag, the target — and a pane that follows latest
records no snapshot, so a restore follows latest too rather than pinning
what happened to be newest at close. Dirty tracking is play's: a baseline of
the composed config, compared per frame; the baseline is taken only once
the mount list has arrived, because the list filling an empty pane is the
app's doing, not the reader's. Column-width persistence (ADR-0151) landed the
same day: the widget took a resolver as an input and runs the protocol itself
(per view, with the widget's own drag floor), the tree widget gained the
three fields the outline needed, and the app acquires one resolver on its
first frame and hands it to every table. ADR-0151's update of the date has
the detail and the honest limit — the headless lane cannot drag, so the
round trip is proven by the widget's resolver test, not by a captured drag.

### 2026-08-21 — M7: the widget's second host

The play panel §Design space recorded as a follow-up is built. Four things
were decided before it was: it is recorded **here** rather than in an ADR of
its own, being O4 of this decision rather than a new one; the rows-to-file-
system interning is **app-local** in `apps/play`, beside `play_hierarchy.go`,
which is the same shape — a column contract read by a panel — and follows
§SD4's rule that extraction waits for a third consumer; the contract is **one
required column**, `path`, with `is_dir`, `size`, `mtime`, `link_target` and
`is_symlink` read by name when present; and a directory's size stays **blank**,
which is the widget's own rule, rather than a rolled-up total the query did not
claim.

Every column the contract does not claim becomes a browser column, so the
panel is the first user of §SD2's host-column seam — tally passes none (M5:
"columns come with the first writer"). `SELECT * FROM fs('<mount>')` is
therefore a browser with hash, text guarantee and expiry beside name, size and
modified.

**A result is not a file system**, and three rules fell out of interning one.
A node other rows nest under is a directory whatever its `is_dir` said. A
repeated path keeps the FIRST row, so which snapshot an entry describes does
not depend on the ORDER BY — `fs('*')` merges the mounts into one tree, which
is what a reader asked for by not projecting `mount`. And a cell io/fs will
not accept is counted as skipped rather than clamped into the root, because a
clamped `..` would list as an entry the query never returned. The store's own
root row (`.` is the commit) lands on the root and lists nowhere. What the
interning dropped — at the row cap, or for want of a usable path — is in the
status line. `testing/fstest.TestFS` is the oracle for the type, the browser
reaching it only through `fs.ReadDir` and `fs.Stat`; it is what asked for the
`ReadDirFile` paging the widget itself never calls.

**The panel publishes twice**, and the split is what the tree makes honest:
`selection_key` is the path of whatever was clicked, `selection` the result row
behind it — which a synthesised directory does not have. It is the only panel
that writes both. It does not preview: a row is metadata rather than bytes, and
the Detail tab beside it already shows the row an entry names. A preview arm
waits for a result column carrying content; a rolled-up directory size and a
folded-`stack` arm (a `splitByChar` from a path) wait for someone to want them.

**§SD6's wildcard did not work in play at all**, and only a live drive found
it: enumerability was decided by the visibility's CONCRETE type, and play
bundles four pass seams in one struct by embedding, so `fs('*')` was refused
there as a yes/no oracle while every test — each passing a `VisibleAll{}`
directly — stayed green. `MountVisibilityI` now carries `EnumerateMounts`
(`MountScopeAll` / `MountScopeSet` / `MountScopeOpaque`) and the expansion asks
instead of switching, so a bundle forwards the capability with the rest of the
interface. That is a Tier-1 change to an exported interface with three
implementers; `VisibleUnderTag` still refuses a wildcard, now by saying so.

Two more, smaller. The tab is `NoScroll` for the Vocabulary tab's reason —
both browser modes are etables that scroll and cull themselves. And a column
dragged to the widget's own floor is stored at play's, which is wider by an
inset either side (`PaddingTight` against the widget's `PaddingInner`): one
resolver per app is ADR-0151's shape, so such a column comes back a couple of
points wider once and is stable after.

**A second SD6 defect, and this one is not fixed.** A `{name:Type}` slot
resolves from the environment's bound params — and play harvests the `SET
param_… ` prelude away (`ExtractParams`) *before* the pre-execute registry
runs, so `env.Extract` finds no params and `fs({m:String})` is refused with
"parameter slot is not bound in the prelude". Every chapter of the lading book
is written that way, so every one of them fails in a running applet while
passing its tests, which call `Expand` on the text WITH its prelude. Found by
driving the book's new `lad-tree` chapter; the same chapter with a literal
mount renders the pane over 937 files. The fix is a decision rather than a
patch — how a harvested param reaches an env-aware pass, which is a contract
between play and `passreg` — so it is recorded here and left. play's own
snippet for the panel takes a literal mount meanwhile, and says why.

Verification gained a lane the other scenes do not have — one that needs a
server, because a result panel has nothing to draw without a result.
`apps/play/scenes/files-pane` holds two: `synthetic`, whose rows are
literals, asserts the synthesised directories, Enter, the outline, and that a
click on a row-backed entry moves the Detail pane to the right row; `lading`
browses whatever the store holds and asserts the pane's own chrome. Both skip
rather than fail without a server.

The panel is documented where a reader meets it: two snippet sections in play's
own corpus — a result read as a tree, and a snapshot browsed — and a book
chapter, `lad-tree`, which is the applet the O2 follow-up named its trigger
for. The contract resolves against a column's GLOSS LABEL rather than its raw
name, because a file listing is exactly the query that glosses its sizes and
mount ids and `size@gloss/bytes` would otherwise miss the contract entirely.

### 2026-08-21 — the widget scene moved into the play tour

§M1's headless scene now lives in the play tour as `34_fsbrowser`
(`apps/play/scenes/34_fsbrowser.scene.md` since ADR-0248, which also removed the
wrapper script this ADR first named). One runner owns the private-binary build,
the FFFI staleness guard, the port-teardown wait and the capture index for
every headless scene; a second copy of that machinery per scene is what the
move removes, not the scene.

The tour learned three per-scene knobs to take it: a `--launch` target (the
scene launches `widgets`, not `play`), a trace prelude (the gallery's filter
box is its mount anchor, not play's Run button), and a viewport size. The
ClickHouse and fixture preconditions are now decided over the *selection*
rather than the scene table, so a run of gallery scenes alone no longer dies on
a server it never reads.

Two things the move made visible. The widget gallery declares no
`SurfaceHints`, so its window is the 900×640 `SurfaceApp` archetype whatever
the viewport — the scene therefore pins a 960×720 viewport and captures a full
frame instead of a window adrift in one, which is also a tighter picture than
the standalone script took. And `FSSCENE_DRY=1`, which that script's header
documented, has never passed for this trace: a dry run resolves anchors without
actuating, and most of these waits are on state a gesture produces. The wrapper
says so rather than repeating the claim.

### 2026-08-21 — the row paddings, and the floor mismatch above

The browser's rows were tuned in the tree widget, where the defects were —
ADR-0176's update of the same date has the measurements. Two of them are this
page's: the name column sat 4 points below the size and modified columns
because `nameCell` opened a `c.Horizontal()` inside the one the outline cell
already opened, and the selection outline was a point taller than the row
pitch on both modes' `rowChrome`.

The third closes what the M7 update above recorded. `MinColumnWidth` now counts
the widget's new cell inset, which is the `PaddingTight` play's resolver
already used, so the two floors are the same expression and a column dragged to
the widget's floor no longer comes back a couple of points wider.

### 2026-08-28 — Preview plays a recording, and how its bytes get to a decoder

Preview gained an audio kind: a file named like a recording opens as an
ADR-0208 player — waveform, transport, ruler, minimap — instead of a hex dump.
It is a preview kind and not a tab of its own, so the preview lane's key
governs it and a new selection closes the track. That made the lane the owner
of something with a lifetime, which it had not been: a lane may now carry a
disposer, and it releases the value it replaces, the value a superseded run
produced anyway, and whatever it holds when the app unmounts. Nothing else
keeps a pointer to an open recording.

SD4 said browse and preview go through `ladingadapter`, and that stands — but a
decoder wants a *file*, not bytes: the native WAV reader an `io.ReaderAt`,
ffmpeg and ffprobe an input they can name on their own command line and seek.
Staging a snapshot's recording as a plain file would leave it on disk after the
window closed, which is the durability [ADR-0134](./0134-adhoc-datasets.md)
exists to refuse, so staging reuses that store: its directory, its per-dataset
quota, its AES-GCM chunk format, its keys-in-memory-only rule, and its
sweep-at-start. A WAV is sealed into a BXAD file and read back through
`adhocdata.SeekableReader`, so its plaintext never leaves the process. Anything
else is ffmpeg's, and an external process can read neither our ciphertext nor a
stream: ffprobe seeks to establish a duration, prints `N/A` where it cannot,
and a source with no frame count is not a `pcm.SourceI`. ADR-0134 met the same wall at ClickHouse and answered it by
decrypting on our side of the boundary into a kernel object with no name; the
audio-shaped version of that answer is a memfd, which `decode.FdInputI` hands
to each spawned decoder as an inherited descriptor. Bounded by the same quota,
which for that branch also bounds anonymous memory.

Consequences worth stating rather than discovering. The peaks cache is off:
it is a plaintext derivative of a recording staged precisely so it leaves
nothing behind, so every open rebuilds — in the background, reported as a
keelson task, which is why the manifest gained `task.ProducerCaps()` (ADR-0038;
without them a task the app spawns is denied). Opening a recording opens the
audio device, falling back to the silent clock with the reason on screen.
Staging reads the whole recording out of the store before anything is drawn,
which is why the quota is a refusal at selection with both sizes named and not
a surprise part-way through. And the store dates the file, not the recording,
so the wall-clock readout is offered only where an entry's mtime gave frame 0
an epoch.

Staging is the app touching the disk itself, which is what ADR-0026 §SD10's
gate is for, and it is now tally's entry in the capslock baseline. The store's
owner is a runtime service that *has* the disk capability — but it publishes
Arrow datasets, not blobs, so there is no operation to ask it for. Giving it
one, so an app stages by request rather than by `os.OpenFile`, is how that
entry leaves; it is a change to ADR-0134's wire surface and quota accounting,
and it is not made here.

`scripts/dev/tally-audio-scene.sh` drives both staging shapes and the release,
asserting on the readouts: the waveform is painter output and the headless
client cannot capture it, the same gap the waveform scene has.

### 2026-09-02 — the widget's third host

mdedit grew a files pane over the snapshot store: fsbrowser's third host
after this app and play's Files tab. Nothing here changes — it composes §SD2's
widget with the ladingadapter/ladingview seams as designed, copying tally's
app-local `storeConn`/`lane` plumbing rather than sharing it (their
app-local-by-design status stands). Recorded in
[ADR-0178](./0178-mdedit-markdown-editor.md)'s Updates.

### 2026-09-03 — the quick filter is a regex over the subtree, run by the store

The widget's quick filter was a case-insensitive substring of the entry's
name, over the one directory listed. It is now one case-insensitive RE2
pattern over the io/fs path of every entry under the current directory, at
any depth, `/` the separator, typed in the regexedit box (ADR-0164 §SD4,
single-pattern mode — a space is a space, because paths carry them). A
pattern that does not compile degrades to a quoted literal and the box says
so, ADR-0164 §SD2's shape. While a filter is set both modes show the matches
as one list, each row named by its path under the current directory.

Where the pattern runs is the file system's choice, through a seam rather
than a walk: `fsmatch.FS` (`public/fs/fsmatch`) is "the entries under this
directory whose path matches this RE2", one call. The lading adapter answers
it with one query — `match()` over the path column inside the `startsWith`
range the key gives, the same shape as the Find tab's SQL and the book's
`lad-find` chapter — and `ladingview.Locked` forwards it under the guard, so
every host over a snapshot (this app, mdedit's files pane) filters a subtree
at the cost of one directory listing. A file system without the seam is
walked from the widget's cached listings, a bounded number of reads per
frame, which is what play's Files tab (a query result as a tree) and the
demo's plain tree get. Both
paths stop at a cap and say so: a filter narrows, and a pattern matching
thousands of paths is one to refine, not scroll. `fs.Glob` stays a walk
(ADR-0198 §SD8's reasoning — a glob in RE2 is a divergence nobody would
see); the seam is for a pattern that already is RE2 on both sides.

### 2026-09-18 — the dialog adopts the widget

Alternatives left "the dialog may adopt it" open, and until now it had not:
`filepicker` kept its own current directory, breadcrumb, listing cache,
directories-first sort, hidden-name check and selection set beside the
widget's. The two copies had drifted where a user sees it — SI sizes in one
and IEC in the other, two icon sets, a single click entering a directory in
the dialog and a double click in every browser pane — and three entries on
the dialog's deferred list (keys, modifier-aware multi-select, a symlink shown
as a link) were things the widget already did.

The dialog is now a shell around the widget. The window, the panels, the
modes and options, the filename row, the stat pane, the commit and the display
root stay the dialog's; everything between the breadcrumb and the last row is
`fsbrowser.Render` in list mode over a `State` the dialog owns. Its exported
surface is unchanged: the `With*Filter` predicates are still written against
`fs.DirEntry`, and the dialog presents the widget's cached `Entry` as one.
The package's explanation page carries the rest.

A third package holding a navigation model for both was the alternative. It
is rejected because `State` already is that model, and a model shared by two
renderers would have kept the half that drifted.

§SD2's contract gains two fields, both general rather than the dialog's:

- **`Input.Keep`** — a host predicate over an `Entry`; an entry it refuses is
  not a row in either mode, and a refused directory is not walked by a filter.
  It sits beside `ShowHidden` and runs wherever that does, including over what
  an `fsmatch.FS` answered, since the store cannot run a Go predicate — there
  it sees each match alone, so a match beneath a refused directory stays. The
  dialog's extension, glob and host filters and pick-folder's directories-only
  listing are this.
- **`Input.SingleSelect`** — every click replaces the selection; ctrl and
  shift are not read. The outline's selection is the tree widget's, so there
  the widget cuts a selection that grew past one back to the cursor, a frame
  after it grew.

What a user of the dialog sees change:

- A directory is entered by a double click or Enter, not a single click; a
  single click selects it. A double click or Enter on a file commits it in
  open mode; in save mode a click on a file gives the filename row its name.
- Pick-folder commits the one selected directory when there is one and the
  current directory otherwise, and the footer names which. With a click now
  selecting rather than entering, committing the current directory alone
  would return the parent of what the user just clicked.
- Multi-select is the widget's — a plain click replaces, ctrl toggles, shift
  extends — where every click used to toggle. The commit still returns files
  in the order they were picked, which the dialog keeps beside the widget's
  selection, a set.
- The listing has size and modified columns, sortable, and the quick filter.
  The cost is the widget's: each entry is stat'ed when its directory is read,
  which the dialog's name-only listing did not do.
- The widget caches a listing until told otherwise, which suits a snapshot and
  not the live tree a dialog browses. The dialog invalidates when it is shown
  and after each navigation, which is when its own cache used to be dropped.

Putting the widget in a window corrected two things about the widget itself,
and both reach its other hosts:

- **`MaxHeight` bounds the widget, not the table.** Its comment always said
  to feed it the pane's height "to bound the browser by its pane", and the
  table took all of it, so the browser stood taller than its pane by the
  breadcrumb and the filter row. A dock tab clips that; a window grows to fit
  its content, the next measurement reads the grown panel, and the dialog
  walked to the height of the screen. The widget now measures what stands
  above its table — two layout probes, their difference held on the `State` —
  and takes it off the table's share.
- **Default widths are applied when the host persists none.** `egui_table`
  fits a table it has not seen to its content, and these cells truncate, so
  their content is a few points wide: a host without a `colwidth.Resolver`
  opened with every column collapsed onto its ellipsis — seen in the gallery
  demo and the dialog; mdedit's files pane passes none either. Such a host's table now gets the
  defaults through the ADR-0151 apply, under a generation that moves once, on
  the view's second frame, and then never — so a drag stays. The second
  generation is for a table inside a new `egui::Window`: that window's sizing
  pass overwrites every width with the content's after the apply has been
  counted as done. The etable binding skipping its apply during a sizing pass,
  as the ctable binding skips its render, would retire the second generation;
  it is an IDL change and is deferred, marked `// deferred:` at
  `seedWidthEpoch`. The IDL stays as "Surfaces" left it.

`filepicker`'s WASM declaration (ADR-0080) moves to blocked: it imports
`fsbrowser`, `tree` and `regexedit`, which are declared so. That is by
entailment; no TinyGo run was made. None of its importers declared a verdict
that rested on it.

Verified in the unit lane of both packages, by the tour's `34_fsbrowser`
scene for the widget's existing contract, and by driving the gallery's filepicker
demo through `imzero2 drive` (ADR-0154) — the four modes' commits, pick order
under ctrl-click, the save-mode name, and the window holding its size. No
scene asserts the dialog; one beside that scene would.

### 2026-09-19 — the dialog's table spans it, and its layout is kept

Two things the first day of use asked for. The listing stopped short of the
dialog's right edge, fixed columns beside empty space; and a column dragged
in the dialog was forgotten with it, where every browser pane keeps its
widths through ADR-0151.

**`Input.FillWidth`** makes the columns span the pane, the name column taking
what the others leave. The first construction was the obvious one and is
recorded because it fails quietly: derive the name column and let the others
be dragged. With the name column derived, the right edge of every later
column stands at a position the pane fixes; the crate computes a dragged
width from the pointer and the column's *left* edge, the left edge moves as
the name column gives, the edge under the pointer never arrives, and the
width changes each frame by the pointer's offset — rate control where the
reader expects position control, running to the floor. What the widget does
instead is a splitter: a dragged edge takes from the column to its right, so
everything left of the edge stands still and the edge follows the pointer;
the last column's edge is the pane's and is not dragged; only a resize of the
pane moves the name column on its own. The layout is the widget's while
`FillWidth` is on, held per view on the `State`. The resolver is told that
layout, so a column that gave to its neighbour's drag persists as surely as
the dragged one, and is told for the name column what it sent itself: that
width follows the pane, and captured it would be written on every resize.

**The dialog's widths are the host's, under an identity of their own.**
ADR-0151 scopes overrides by app and hands the store to an app through its
frame context. A file dialog is not an app and has no frame context: the
window host raises one and the fs Powerbox bridge raises the rest, for
whichever app asked. Keying by the asking app would give one dialog a layout
per caller. It is keyed `runtime.filepicker`, a synthetic id as the fs
broker's `runtime.fs` is; `filepicker.NewColumnWidths` builds the resolver
with that identity and the widget's drag bounds, `hostboot` builds one over
the facts store, hands it to both dialog hosts and flushes it every frame —
not only while a dialog is open, since a width dragged just before a commit
is written after the dialog has gone. The instance tier is tagged by mode,
not by dialog instance: the bridge mints a dialog per request. A host that
passes no resolver gets the fill and no persistence.

Verified by use, through `imzero2 drive`: both interior edges land where the
pointer went, the right-hand neighbour gives, a resize of the dialog moves
the name column alone, the layout comes back after a restart of the host,
and a width dragged in the window host's save dialog is the width of the
bridge's open dialog. Not reached: the header's reset menu, which did not
open under the driver's secondary click; and `FillWidth` in outline mode,
which shares the layout code and has no host.

### 2026-09-19 — the filter's search is a background job

The 2026-09-03 update put the filter's search on the render thread: the
store's answer as one blocking call, a plain tree's walk as a budget of
directory reads per frame. The dialog made the second one felt — a walk from
a home directory stats every entry of thousands of directories, a frame's
share at a time — and neither had a way to be stopped.

The search is now a `bgjob` run (ADR-0038's producer, through the runner
every other inline job uses). It starts once the filter text has stood still
for a moment, because with `Input.Tasks` set each search is a task on the
bus and a reader typing a pattern would otherwise start and cancel one per
key. The filter row shows the standard `jobprogress` row inline — bar, share,
time left, the counts, Cancel — so the table does not move when a search
starts and ends; matches are listed as they are found, as before; a cancelled
search keeps what it found and says so. tally passes its task API, the file
dialog one `hostboot` builds under `runtime.filepicker`; mdedit's files pane
and play's Files tab pass none, and their searches are jobs of the widget's
own, off the render thread all the same.

A walk does not know how much it has left. Its share is the directories read
over the directories known, read and unread, reported as the most it has
been, so the bar can wait and does not go back; the time left is the
estimator's over that share and is as rough as the share is, most of all
early in a wide tree. The store's single call is indeterminate.

Three consequences, the first two now part of §SD2's contract:

- The host's `fs.FS` is read from the job's goroutine as well as the render
  thread. `os.DirFS` and a `ladingview.Locked` view allow that. play's result
  tree did not quite: it sorted a directory's children on first read into an
  unguarded cache, and now sorts them when the tree is built.
- `Input.Keep` is called from that goroutine too.
- A host that stops rendering a browser calls `State.StopSearch`, as the
  dialog does when it closes. The goroutine reads the file system itself
  rather than through the `State`'s listing cache, which stays the render
  thread's alone; a walk therefore re-reads directories the browser has
  listed.

The two caps are told apart where they were one message: more matches than
the list shows asks for a narrower pattern, directories left unread for a
search from further down — the usual outcome from a home directory, where
"narrow the pattern" was the wrong advice.

Verified in the unit lane under the race detector — the walk and the
store's call as functions, the job's start, streaming, cancel and failure
against a real runner — and by use in the dialog over a home directory: the
job row, the task in `keelson('tasks')` under the dialog's identity, Cancel.
The carrier driver could not focus the filter box, so the live run seeded
the filter in a scratch build; typing a pattern was not driven.

### 2026-09-19 — the lane is shared, and every wait is the standard row

§SD4 kept tally's store plumbing app-local, and the 2026-09-02 update
confirmed it when mdedit copied `storeConn` and `lane` rather than share
them. That stands for `storeConn`, whose trim per app is real. It is
withdrawn for the lane, for two reasons the day's earlier work produced.

The copies had stopped being the only ones of their kind. The widget's search
(the update above) needed the same thing — cancellable work off the render
thread, polled by the frame — and took it from `bgjob`, which also makes a
run a keelson task and estimates its progress. tally's lane, a line-for-line
twin in mdedit, did neither: twelve store-bound waits in tally were a spinner
and a sentence, with no way to stop a query and no entry in the task monitor.
A reader could cancel a filter in a browse pane and not the Find tab beside
it.

The lane is now `bgjob.Keyed` — tally's contract unchanged (a new key
supersedes, a repeated key is a cache hit, a nil run is a poll, a value with
a disposer is lane-owned), on the task and estimator plumbing `bgjob.Runner`
uses, plus the two things a wait needs: a snapshot the standard row draws,
and `Cancel`. A cancelled key stays answered, with `bgjob.ErrCancelled`, so
the next frame's demand does not start it again; tally says "was cancelled"
with a Run again, and Find's Search button re-runs a cancelled or failed
search with nothing changed. Each of tally's lanes is a task of its own kind
(`tally-find`, `tally-diff`, …); the connection is left out, being the app
coming up rather than work the reader asked for. mdedit's files pane has no
task caps and its lanes stay local; its mount listing shows the row.

Drawing a job is shared too: `widgets/bgjobrow` is the one mapping of a
`bgjob` snapshot onto `jobprogress`, used by tally, mdedit and the widget's
filter row. `jobprogress` keeps its charter of knowing no producer; the
adapter is a package beside it.

tally's queries report no progress of their own — they run through the
chlocal pool, which exposes none — so their rows are indeterminate: a bar
that moves, the sentence, Cancel. Verified in the unit lane under the race
detector, `Keyed` against a real bus for the task and its cancel. tally was
brought up headless on the shared lane and connected and listed the store
through it, but the store at hand held no mounts, so none of its waits was
reached; the row itself was seen in the widget's filter, through the same
adapter.

## References

- [ADR-0198](./0198-fs-snapshot-store.md) and
  [the survey page](../adr-background-work/lading-browser-survey.md).
- ADR-0026 (capabilities, Powerbox), ADR-0097 (play panels), ADR-0123,
  ADR-0132 (sqlapplet), ADR-0135 / ADR-0148 (launch kinds, workingsets),
  ADR-0151, ADR-0154, ADR-0176 (tree widget), ADR-0177 (keys), ADR-0186
  (glosses), ADR-0189 (`LW_COMPONENT`).
