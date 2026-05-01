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

The package is split into four concerns:

- **`pijul_runner.go`** — defines `PijulRunnerI`, a one-method-per-CLI-op
  interface (`Init`, `Clone`, `Add`, `Record`, `Push`, `Pull`,
  `ApplyPatch`, `Log`, `LatestHash`, `Credit`, `LatestChangeFile`).
  The current `cliRunner` implementation shells out to the system
  `pijul` binary; the planned native runner backs the same interface
  with an in-memory `Graggle` plus on-disk patches under
  `<repoDir>/.pijul/changes/`.
- **`pijul_parser.go`** — pure functions over Pijul's textual outputs:
  `ParsePijulFile` reads the working copy's flat-KV format and detects
  conflict blocks, `parseLogJSON` decodes `pijul log --output-format
  json`, and `applyCreditToLines` resolves cell-level provenance from
  `pijul credit` using oldest-wins graph-age comparison.
- **`pijul_store.go`** — orchestration: spawns and tears down the four
  per-actor working copies, runs a background worker that drains a
  `Task` queue, and parallelises the per-frame reload of all four
  actors via `errgroup`.
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

- The tracked file always ends with a trailing newline (see
  `DemoStore.saveStateToFileLocked`). Without it, Pijul treats the
  EOF context node as overlapping and may promote unrelated edits into
  spurious conflicts.
- The *side labels* (`>>>>>>> 1`, `<<<<<<< 2`) in conflict markers are
  Pijul's internal numbering, not patch hashes; the parser preserves
  them in `ConflictData.AliceLabel` / `BobLabel` for round-trip
  fidelity, but they carry no semantic information beyond "first" /
  "second".
- The per-actor `CliLogs` ring buffer copies into a fresh slice when
  it overflows so the previously-grown backing array becomes garbage;
  a long demo session does not retain unbounded log memory.
- `PendingOverrides` is keyed by inputKey string, never by `*string`,
  so overrides survive any pointer churn between the frame that
  queued them and the frame that applies them.

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
