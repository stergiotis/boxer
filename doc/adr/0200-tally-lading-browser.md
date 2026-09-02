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
| `scripts/dev/fsbrowser-widget-scene.sh`, `scripts/dev/tally-scene.sh` | new headless scenes (ADR-0154 lane) | the verification plan below |
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
  `scripts/dev/fsbrowser-widget-scene.sh` (2026-08-20).
- **M2 — the app.** ✓ `apps/tally`: manifest, dock (Pane A over Preview /
  Info), Mounts with snapshots and follow-latest, Preview by type, Info from
  `fs()`, help book, the headless scene `scripts/dev/tally-scene.sh` against
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
`scripts/dev/files-pane-scene.sh` runs two: `synthetic`, whose rows are
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

§M1's headless scene now lives in `scripts/dev/play-screenshot-tour.sh` as
`34_fsbrowser`, and `scripts/dev/fsbrowser-widget-scene.sh` — the path this ADR
names — is a wrapper that selects it. One runner owns the private-binary build,
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
client cannot capture it, the same gap `waveform-scene.sh` has.

### 2026-09-02 — the widget's third host

mdedit grew a files pane over the snapshot store: fsbrowser's third host
after this app and play's Files tab. Nothing here changes — it composes §SD2's
widget with the ladingadapter/ladingview seams as designed, copying tally's
app-local `storeConn`/`lane` plumbing rather than sharing it (their
app-local-by-design status stands). Recorded in
[ADR-0178](./0178-mdedit-markdown-editor.md)'s Updates.

## References

- [ADR-0198](./0198-fs-snapshot-store.md) and
  [the survey page](../adr-background-work/lading-browser-survey.md).
- ADR-0026 (capabilities, Powerbox), ADR-0097 (play panels), ADR-0123,
  ADR-0132 (sqlapplet), ADR-0135 / ADR-0148 (launch kinds, workingsets),
  ADR-0151, ADR-0154, ADR-0176 (tree widget), ADR-0177 (keys), ADR-0186
  (glosses), ADR-0189 (`LW_COMPONENT`).
