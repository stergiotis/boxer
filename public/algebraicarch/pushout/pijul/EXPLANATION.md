---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# pijul — Patch-theory event-store demo

This package wraps Pijul (a patch-theory VCS) as a four-actor
event-store demo: a Server plus Alice/Bob/Charlie each operate a
working copy of a flat-key/value file `customer.txt`, exchange edits
through `pijul push` / `pijul pull` / "Email Patch" peer-to-peer
bundles, and surface the results in three imzero2/egui2 windows. The
point of the demo is to make the *commutativity* and *graph context*
properties of patch theory legible to engineers used to Git's
heuristic three-way merge.

## Background

Git merges files via diff3, a heuristic on linear text. Patch-theory
VCSes (Darcs, Pijul, [ojo](https://github.com/jneem/ojo)) instead
model files as objects in a category and merges as **pushouts**: the
unique "smallest" merge that preserves both inputs. The original paper
([Mimram & Di Giusto 2013](https://arxiv.org/abs/1311.3903)) defined
patches over plain files; Pijul generalises to *graggles* (graph +
file) so that pushouts always exist — at the cost of admitting
non-linear intermediate states that re-emerge as conflicts.

External references:

- Joe Neeman's [merging](https://jneem.github.io/merging/) /
  [pijul](https://jneem.github.io/pijul/) /
  [pseudo-edges](https://jneem.github.io/pseudo/) blog series.
- Pijul user manual: <https://pijul.org/manual/>.
- Native-Go reference implementation (planned migration target):
  `../../../../../../hackathon_2026/src/go/public/pushout/graggle/`.

## How it works

The package is layered around two seams:

- **`pijul_backend.go`** — defines the *domain* seam: `BackendI` (a
  factory for `RepoI` handles) and `RepoI` (one actor's working copy).
  These take and return [KVLine] cells, [PatchEnvelope] blobs, and
  [PatchMetadata] records — never raw text bytes. Every `pijul`-flavoured
  detail (textual flat-KV format, conflict-marker emission, side
  labels, trailing-newline invariant) is a backend-internal concern.
- **`pijul_text_backend.go`** — the `pijul-text` realisation of
  `BackendI` that drives a real `pijul` binary. `pijulTextBackend` and
  `pijulTextRepo` serialise cells to pijul's textual working-copy
  format on the way down and parse them back on the way up.
- **`pijul_pushout_backend.go`** — the `pushout-native` realisation of
  `BackendI` that uses the vendored `algebraicarch/pushout/graggle`
  package directly. Each actor holds an in-memory `*store.Graggle` plus
  an on-disk patch log under `<repoDir>/.pushout/`; `SetAndRecord` runs
  `patch.LineDiff` over the live subgraph and constructs a `Patch`
  natively; `State` walks `algo.LinearOrder()` (or, in conflict mode,
  groups live nodes by cell path) without any text round-trip. No
  `pijul` binary is involved. Selected at construction via
  `Config.Backend = pijul.NewPushoutBackend()` or by setting
  `PIJUL_BACKEND=pushout` before launching the demo.
- **`pijul_runner.go`** — the *CLI-verb* seam: `pijulRunnerI` is one
  method per `pijul` subcommand (Init, Clone, Add, Record, Push, Pull,
  ApplyPatch, Log, LatestHash, Credit, LatestChangeFile). It is
  unexported; only `pijulTextBackend` consumes it. `cliRunner` is the
  concrete implementation backed by `os/exec`.
- **`pijul_parser.go`** — pure parsers and serialisers for pijul's
  textual outputs (`parseRecordText`, `serializeRecordText`,
  `parseLogJSON`, `applyCreditToCells`). All package-private because
  they are text-backend internals.
- **`pijul_store.go`** — orchestration: per-actor state, the background
  task worker, and the parallel reload via `errgroup`. `DemoStore`
  composes a single `BackendI` + per-actor `RepoI` instances; demo
  actions like `SaveEdit` and `EmailPatch` are now thin wrappers over
  `state.Repo.SetAndRecord(...)` and `state.Repo.ExportLatest(...)`.
- **`pijul_render.go`** / **`pijul_playbook.go`** — egui2 view layer.
  Per-actor edit windows, central server/inbox window, and a
  storyboard window with five canned playbooks.

UI databindings are mediated through `UIState`: the renderer reserves
a stable `*string` per `<actor>_<path>` input key, and the worker
queues value overrides by *key* (not pointer) so that any later
re-allocation of the binding pointer does not strand pending overrides.

### Pitfall: the "Graph Context Ambiguity" pattern

Two users concurrently edit *different* lines in a small file, but
`pijul pull` returns a structural conflict instead of a clean
commutative merge.

**Cause:** Pijul models files as a graph of lines anchored by *context
nodes* — the unaffected lines surrounding each edit. To merge two
independent patches, Pijul needs the context nodes on either side of
each edit to be unambiguously identifiable. If a file is small and the
edits are close together, the surrounding context nodes overlap; Pijul
acts conservatively and reports a conflict rather than guess.

**Pattern:** ensure sufficient graph context.

- In tests: pad the test fixture with extra lines between keys so the
  algorithm has unambiguous anchor points. The demo fixture in
  `DemoStore.InitSystem` does this implicitly (8 well-separated KV
  rows).
- In production: group volatile keys together and separate them from
  static rows with structural boundaries (section headers, empty
  lines) to give the patch graph robust anchors.

### Pitfall: pull exits non-zero on conflict

`pijul pull` exits with code 1 when it injects conflict markers into
the working copy. This is *expected behavior*, not a fatal error.
`cliRunner.Pull` classifies the non-zero exit by inspecting the
underlying `*exec.ExitError` and returns `(hadConflict=true, err=nil)`
in that case; the orchestrator records an `[INFO]` audit line rather
than surfacing a fatal-error block in the UI.

## Invariants

Demo-level invariants (visible to anyone using `BackendI`/`RepoI`):

- `Push` and `Pull` require both endpoints to come from the same
  backend. Implementations type-assert and return an error otherwise.
- `Pull`'s `(hadConflict=true, err=nil)` return signals "applied with
  conflict markers", not failure. The orchestrator records an `[INFO]`
  audit line; the UI does not show a fatal-error block.
- `PendingOverrides` is keyed by inputKey string, never by `*string`,
  so overrides survive any pointer churn between the frame that
  queued them and the frame that applies them.
- The per-actor `CliLogs` ring buffer copies into a fresh slice when
  it overflows so the previously-grown backing array becomes garbage;
  a long demo session does not retain unbounded log memory.

Text-backend internal invariants (no longer visible to the demo, but
still load-bearing for the `pijul-text` realisation):

- The serialised tracked file always ends with a trailing newline.
  Without it, Pijul treats the EOF context node as overlapping and
  may promote unrelated edits into spurious conflicts.
- The conflict-marker side labels (`>>>>>>> 1`, `<<<<<<< 2`) are
  pijul's internal numbering, not patch hashes. The text backend emits
  them on serialisation and discards them on parse; the public
  `ConflictData` carries only the two side values.

Pushout-native backend internal invariants:

- The on-disk layout is `<repoDir>/.pushout/changes/<short-hex>.json`
  for envelope files plus `<repoDir>/.pushout/applied.txt` listing
  hashes in apply order (one per line). `applied.txt` is the apply
  *log*; the in-memory `*Graggle` is the apply *result*. Push/Pull
  computes a set-difference over `applied.txt` and ships envelopes in
  apply-log order so dependencies always precede dependents.
- `store.Graggle.DeleteNode` is idempotent: deleting an already-deleted
  node is a no-op. Two actors can independently delete the same node
  (the typical "both edited the same line" merge); without this, the
  second patch in a converged pair would fail to apply. The fix lives
  in the vendored `store/graggle.go` behind a `VENDOR DEVIATION:`
  comment so re-vendor reviewers can spot it; will be upstreamed to
  hackathon_2026 pushout. AddNode/AddEdge identities are patch-scoped
  so they do not need the same relaxation.
- Conflict cells are derived in `cellsFromConflictedGraggle` by
  grouping live nodes by their cell path. The demo's value model
  guarantees each path appears as one node per actor's edit, so the
  Alice/Bob sides come straight from the two live nodes' contents.
  Cycle conflicts are out of scope; cell ordering in conflict mode is
  alphabetical (the linear case preserves user order).
- `SetAndRecord` rejects "arbitrary new value" conflict resolution: a
  cell whose value matches neither side returns an error rather than
  attempting an open-ended structural rewrite. The demo's UI offers
  Keep-Alice / Keep-Bob buttons only, so this matches the user-facing
  affordance.

## Trade-offs

- The CLI runner has a 15-second timeout per mutating command and a
  5-second timeout for log/credit reads. These are generous for the
  demo's small dataset but would not survive on a production-scale
  repo; the planned native runner has no such bound.
- The demo serialises all mutating actions through a single worker
  goroutine. This is intentional — it reflects how a real
  multi-actor workflow treats each working copy as a serialised
  resource — but it means the UI shows a "processing" indicator for
  the slowest of the four actors during reload.
- `Email Patch` extracts the most recently modified file under
  `.pijul/changes/` rather than parsing Pijul's transactional state.
  This is a portable approximation that breaks if the user records
  multiple patches faster than mod-time resolution; the native runner
  will track patch identity directly.

## Further reading

- Theory: [A Categorical Theory of Patches](https://arxiv.org/abs/1311.3903).
- Native Go target: `../../../../../../hackathon_2026/src/go/public/pushout/DESIGN.md`.
- Decisions: ADRs may be added under `doc/adr/` once this experiment
  graduates from `llm_generated_*` provenance.
