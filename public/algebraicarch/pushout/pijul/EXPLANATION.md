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
  [pseudo-edges](https://jneem.github.io/pseudo/) /
  [ids](https://jneem.github.io/ids/) /
  [cycles](https://jneem.github.io/cycles/) blog series.
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
  actions (`SaveEdit`, `ResolveConflict`, `DeleteCell`, `CreateCell`)
  funnel through one `recordWithCellsTransform` helper that reads the
  repo's live cells, applies a transform, and calls
  `state.Repo.SetAndRecord(...)`. `EmailPatch` similarly wraps
  `state.Repo.ExportLatest(...)`.
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

### Pitfall: `Unrecord` is local rollback, not erasure

An operator invokes `PushoutRepo.Unrecord(hash)`
(`pijul_pushout_backend.go:505`) on a patch carrying personal data,
expecting the data to be gone. The graggle's live subgraph no longer
shows the patch's effect — but the envelope file at
`.pushout/changes/<short>.json` and the `MetaByHash[hash]` entry are
still present, and a future `Pull` from any peer that still has the
patch will reapply it cleanly.

**Cause.** `Unrecord` is designed to be round-trippable. It calls
`Patch.Unapply` to rewind the graggle (un-deletes tombstones, removes
edges and nodes the patch added), removes the hash from `appliedHash`,
and rewrites `applied.txt`. It deliberately does *not* delete the
envelope, because the typical use case is "back out a local change so
a peer's newer version can be pulled in cleanly" — without the
preserved envelope, a subsequent re-apply would have to re-fetch the
bytes from a peer. Personal data carried in the patch's `Change.Content`
therefore survives on disk after `Unrecord`, and on every peer that
pulled the patch and has not itself run `Unrecord`. This is **not
erasure** under GDPR Art 17, FADP Art 32 al. 2(c), or ICO "put beyond
use".

**Git analogy.** Pushout has no equivalent of git's staging area or
dirty working tree — every `SetAndRecord` immediately produces a
content-addressed patch, comparable to a git commit. `Unrecord` is
therefore *not* "unstage + checkout" (those discard uncommitted work).
It is closer to **a `git rebase -i` drop that never gets
garbage-collected**: the patch leaves the applied history
(`appliedHash` / `applied.txt`), the graggle rewinds to its pre-patch
state, but the envelope persists in `.pushout/changes/` indefinitely
and can be re-introduced by `Pull` from any peer that still has it —
analogous to recovering a dropped commit by its hash from git's object
database, except that pushout's "object database" has no retention
horizon and no GC.

**Architecture for actual erasure.** Two related ADRs document the
mechanism (both deferred pending counsel review):

- [ADR-0025 (pebble2impl)](../../../../../pebble2impl/doc/adr/0025-pushout-forget-architecture.md)
  — GDPR scope; recommends Architecture C (vault-by-design +
  cooperative-purge fallback).
- [ADR-0027 (pebble2impl)](../../../../../pebble2impl/doc/adr/0027-pushout-forget-swiss-fadp.md)
  — FADP scope; recommends the leaner S2 (local meta-tier redaction),
  with S4 (= Architecture C) as the adequacy-hedge upgrade.

Both architectures use a different primitive than `Unrecord`: a
**compensating patch**, a *new* patch added to the graggle that
overwrites the affected node's content with a redaction marker while
preserving the structural NodeIDs that downstream dependents reference.
It is not an inverse of the original patch (an inverse would orphan
dependents); it is an additive overlay that destroys the personal-data
content while keeping the dependency structure intact.

**Antiquing's role**
(see [ADR-0039 (pebble2impl)](../../../../../pebble2impl/doc/adr/0039-pushout-antiquing.md)) is
*blast-radius minimisation* for compensation. If a dependent's antique
form does not actually reference the to-be-forgotten patch's nodes, no
compensating patch is needed for that dependent — the original patch
can simply be removed. Without antiquing, the system must
conservatively assume every literal reference is a real dependency,
inflating the number of compensating patches that must be constructed
and propagated.

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
- `types.HashBytes` uses BLAKE3 (not SHA-256). pushout's hash is purely
  a content-addressed identity — both algorithms give the same 32-byte
  output and equivalent collision resistance, but BLAKE3 is what the
  rest of pebble2impl already uses (leeway/card schema fingerprint,
  IMAP client). Marked `VENDOR DEVIATION:` in the vendored types
  package. Switching changes every patch hash; envelope files written
  by a SHA-256 build will fail Decode's hash-validation guard.
- Conflict cells are derived in `cellsFromConflictedGraggle` by
  grouping live nodes by their cell path. Every live node for a given
  path becomes one side of the resulting `ConflictData`: the first
  two as `AliceValue` / `BobValue` and any extras as `OtherValues`,
  so 3+-way conflicts (multiple actors editing the same cell, or
  cycle conflicts that surface as N live nodes for one path) render
  one Keep button per side. Cell ordering in conflict mode is
  alphabetical (the linear case preserves user order).
- `changesForResolution` accepts arbitrary chosen values: per
  conflicted path, every live sibling whose content does not match
  the chosen value is deleted, and the chosen value's existing node
  (if any) is kept. If no existing sibling matches — the user typed
  a brand-new value — every sibling is deleted and a new node is
  added carrying the chosen value, anchored to a parent / downstream
  shared by the conflict siblings via `commonAnchors`.

## Open design questions

### Antiquing

Pijul records each patch in its "most-antique" form — every patch is rewritten
to depend only on what it genuinely needs, so two patches that don't truly
depend on each other can be applied in either order. Boxer's port does not
currently perform this rewrite; this section documents what the gap is and
where it eventually bites. The full design-space analysis (architecture
options, subsidiary decisions, open questions) is in
[ADR-0039 (pebble2impl)](../../../../../pebble2impl/doc/adr/0039-pushout-antiquing.md); the text below is
the user-facing summary.

**Definition.** Given a patch `q` recorded against state including patch `p`,
`q` is *antiqued* if there exists a patch `a(q)` starting from an earlier state
such that the perfect merge of `p` and `a(q)` reproduces `q`. Joe Neeman's
[pijul post](https://jneem.github.io/pijul/) phrases it as "making `q` look
older than it really is." Perfect merges are associative, so repeated
antiquing converges: every patch has a unique most-antique form, and the
dependencies that the most-antique form still references *are* `q`'s true
dependencies. Patches that have been antiqued past each other are parallel —
applicable in either order — which is the property that lets cherry-pick work
across diverged history.

**Current state of the code.** `patch.NewPatch` records whatever the caller
hands it; `ComputeDependencies` (`graggle/patch/patch.go`) extracts
dependencies as literally referenced in change context fields. There is no
pass that rewrites changes to reduce the dependency set. `LineDiff`
(`graggle/patch/diff.go`) anchors new insertions at the LCS-immediate
neighbours, so it tends to produce near-antique patches when the diff is
localised — but that is an accidental property of the LCS choice, not a
guarantee. `changesForResolution`'s `commonAnchors`
(`pijul_pushout_backend.go:471`) picks the first live parent and child, which
can be more conservative than the antique form requires.

**Observed gap.** A patch may declare dependencies on patches it does not
truly need. A peer attempting `Apply` is then rejected for "missing
dependency" where an antiqued version would have applied cleanly. The gap
has not surfaced as a bug in the demo workflows so far; it is a latent
semantic divergence from the patch-theory framing the package adopts
(`Background` above), not a known regression.

**Why it eventually matters.**
[ADR-0025 SD4](../../../../../pebble2impl/doc/adr/0025-pushout-forget-architecture.md)
(compensating-patch construction for cooperative-purge erasure) names
antiquing as a prerequisite: to construct a compensating patch that overwrites
only the affected nodes, the system needs to know which dependent patches
genuinely require the to-be-forgotten patch's content — which is exactly what
the antiqued dependency set records. ADR-0027 ("Swiss-Only Forget
Architecture", FADP scope) reaches the same need indirectly: any deployment
that ever upgrades from S2 to S4 (vault + cooperative purge) inherits SD4's
antiquing prerequisite.

**Deferred decisions.** The shape of the algorithm — for each
`ChangeKindNewNode`, walk the live subgraph to find the up/down anchors that
minimise the patch's dependency set while still pinning the same
partial-order position relative to surrounding kept content, with a
deterministic tie-breaker among equally-minimal candidates — is sketched
but not committed to. "Canonical" here means *record-time canonicity given a
fixed input*: Alice and Bob recording against different graggle states
produce different patches by design, which is consistent with pijul's own
semantics. Placement (inside `LineDiff`, between `LineDiff` and `NewPatch`,
inside `NewPatch`, or as an independent post-record pass) is open. Conflict
resolution's `commonAnchors` may need a separate antiquing pass or may
benefit from the same one; that, too, is open. See
[ADR-0039 (pebble2impl)](../../../../../pebble2impl/doc/adr/0039-pushout-antiquing.md) SD1–SD10 and
OQ1–OQ6 for the enumerated options and the engineering recommendation
(staged B → C).

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
