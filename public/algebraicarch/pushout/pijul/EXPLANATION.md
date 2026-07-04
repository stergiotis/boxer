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
- GUI demo consumer: an external repository, which imports this
  repo's pushout packages as a module dependency.

## How it works

This repo holds the *domain* half of the demo — backends, parsers, and
the graggle engine — and is the canonical implementation. The
GUI/orchestration half (the demo store, the task worker, egui2 windows
and playbooks) lives in the external GUI consumer, which depends on
this repo.

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
  `BackendI`: a thin KV adapter over the domain-neutral engine
  (`pushout/repo`, ADR-0079). The adapter translates cells to changes
  (`patch.LineDiff` on the clean path, conflict resolution otherwise)
  and reads back through `Engine().View` read transactions; everything
  else — persistence and recovery (`repo/filestore` under
  `<repoDir>/.pushout/`), wire codecs (`envelope` framing + `jsonv1`),
  dependency gating, identity disambiguation, retention sweeps, and
  transactional verbs — is engine property. `Init` opens or RECOVERS
  (an existing store is replayed, not reset); `Push`/`Pull` ride the
  transport-agnostic `exchange` package over its in-process carrier;
  `Clone` is open-fresh-plus-pull. No `pijul` binary is involved. Each
  seam (storage, codec, transport) ships an executable conformance
  suite (`repo/storagetest`, `envelope/codectest`,
  `exchange/exchangetest`) for alternative implementations — the next
  demonstrator implements NATS transport and a custom codec against
  them.
- **`pijul_runner.go`** — the *CLI-verb* seam: `RunnerI` is one
  method per `pijul` subcommand (Init, Clone, Add, Record, Push, Pull,
  ApplyPatch, Log, LatestHash, Credit, LatestChangeFile). Only
  `pijulTextBackend` consumes it; the interface is exported so test
  fakes can satisfy it. `cliRunner` is the concrete implementation
  backed by `os/exec`.
- **`pijul_parser.go`** — pure parsers and serialisers for pijul's
  textual formats (`ParseRecordText`, `SerializeRecordText`,
  `ParseLogJSON`, `ApplyCreditToCells`, plus the shared cell-line
  codec `formatCellLine`/`splitKVLine` — values are `strconv`-quoted
  so quotes, backslashes, and newlines round-trip byte-exactly).

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
  algorithm has unambiguous anchor points. The external demo's init
  fixture does this implicitly (8 well-separated KV rows).
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

An operator invokes `PushoutRepo.Unrecord(hash)` on a patch carrying
personal data, expecting the data to be gone. The graggle's live
subgraph no longer shows the patch's effect — but the envelope file under
`.pushout/changes/` and the engine's patch-store entry are
still present, and a future `Pull` from any peer that still has the
patch will reapply it cleanly.

**Cause.** `Unrecord` is designed to be round-trippable
(`TestClaim_UnrecordRoundTripViaPull` pins this). It refuses while any
applied patch declares the target as a dependency ("unrecord dependents
first"), then calls `Patch.Unapply` against a clone (un-deletes
tombstones it holds the last deleter for, removes edges and nodes the
patch added), removes the hash from `appliedHash`, and rewrites
`applied.txt`. It deliberately does *not* delete the envelope, because
the typical use case is "back out a local change so a peer's newer
version can be pulled in cleanly" — without the preserved envelope, a
subsequent re-apply would have to re-fetch the bytes from a peer.
Personal data carried in the patch's `Change.Content` therefore
survives on disk after `Unrecord`, and on every peer that pulled the
patch and has not itself run `Unrecord`. This is **not erasure** under
GDPR Art 17, FADP Art 32 al. 2(c), or ICO "put beyond use".

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

**Architecture for actual erasure.** Documented in
[ADR-0025](../../../../doc/adr/0025-pushout-forget-architecture.md);
**Architecture A** (vault-by-design, per-occurrence nonce commitments —
ADR-0025 SD6) is the selected erasure architecture for a greenfield,
multi-actor deployment.

Under Architecture A the patch DAG is never rewritten. PII-flagged
fields are intercepted at `SetAndRecord` time: cleartext plus a fresh
256-bit nonce are written to a controller-side vault (realised as a
Leeway facts table in ClickHouse, per ADR-0025 SD5), and
`Change.Content` carries a fixed-size carrier token
`(version, vaultRef, C)` in place of the raw bytes, where `C` is the
keyed-BLAKE3 commitment under that row's nonce (ADR-0025 SD6).
Unchanged values reuse their existing token byte-for-byte, so diffs
stay quiet. The `ForgetSubject` / `ForgetRefs` operations (ADR-0025
SD9) shred the subject's at-rest key (SD12) and delete the vault rows
under the mutation-finality contract in ADR-0025 SD11 — after which
each in-patch commitment is an unrecoverable, mutually uncorrelatable
32-byte residue. The vault is never propagated; only the patch
envelope (with carrier tokens) flows through Push / Pull. One
mechanism discharges both GDPR Art 17 and FADP Art 32(2)(c); under
the Swiss relative approach peers hold non-personal data throughout,
since they never see vault rows or nonces.

The earlier architecture-design discussion considered a *compensating
patch* mechanism (a new patch added to the graggle to overwrite affected
node content with a redaction marker). That mechanism belonged to the
rejected Architectures B and C in ADR-0025 and the superseded ADR-0027.
It is not used under Architecture A.

[ADR-0027](../../../../doc/adr/0027-pushout-forget-swiss-fadp.md)
— the FADP-scope variant — is superseded by ADR-0025's selection.
Architecture A also clears the FADP axis under BGE 136 II 508 + Art 32(4).

**Antiquing's role** (see
[ADR-0039](../../../../doc/adr/0039-pushout-antiquing.md))
is no longer load-bearing for erasure under Architecture A — the
dependency-minimisation output it produces was a prerequisite for
compensating-patch construction, which is not performed. Antiquing
remains a real correctness / theoretical-alignment question for pushout
on its own merits; ADR-0039's Decision section is deferred pending the
internal SD1–SD10 + OQ1–OQ6 design dialogue.

## Invariants

Demo-level invariants (visible to anyone using `BackendI`/`RepoI`):

- `Push` and `Pull` require both endpoints to come from the same
  backend. Implementations type-assert and return an error otherwise.
- `Pull`'s `(hadConflict=true, err=nil)` return signals "applied with
  conflict markers", not failure. The orchestrator records an `[INFO]`
  audit line; the UI does not show a fatal-error block.
- Cell paths are validated up front (non-empty, no spaces, quotes, or
  newlines); values are unrestricted — the quoted line format
  round-trips any byte sequence.
- While the graggle is conflicted, `SetAndRecord` records conflict
  resolutions *and* edits/deletions of clean cells in one patch;
  creating a brand-new cell is rejected with a clear error (no linear
  order means no reliable anchor for a new row).

(UI-level invariants — pending-override keying, the CLI-log ring
buffer — belong to the GUI half in the external consumer.)

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

- The on-disk layout (repo/filestore) is
  `<repoDir>/.pushout/changes/<hh>/<full-hex-hash>` for framed envelope
  files (sharded by the hash's first byte), `applied.txt` listing
  hashes in apply order, and `snapshot.bin`. The log is the system of
  record; the snapshot accelerates recovery and is honored only when
  its applied list is a PREFIX of the log — otherwise it is discarded
  and the log replays from empty (correctness never depends on
  snapshot freshness). All writes are atomic (temp+fsync+rename); a
  torn trailing log line is a never-acknowledged append and is
  dropped on load. Sweeps snapshot BEFORE acknowledging, so purge
  markers survive crashes. Push/Pull computes a set-difference over
  the applied lists and ships envelopes in apply-log order so
  dependencies always precede dependents
  (`TestClaim_PushShipsDepsFirst`). Envelope files are
  content-addressed and first-writer-wins.
- Patch identity is the BLAKE3 hash of the canonicalized dependency
  set plus the changes. Author and description stay envelope-level
  provenance, so two actors recording the identical edit against the
  same state converge on one patch. Dependency tampering fails the
  hash check at `envelope.Validate` (run on every Registry
  encode/decode). Changing either the hash function
  or its scope invalidates previously persisted envelope files.
- Tombstones track their deleters: `store.Graggle.DeleteNode` records
  the deleting patch and tolerates further deleters (two actors
  editing the same line is the normal convergent case); a node is
  resurrected only when its *last* deleter is unapplied. `Apply` gates
  dependencies on the *applied* set (not merely seen envelopes), and
  `Unrecord` refuses while a dependent is applied — together these
  keep the unapply direction sound.
- When a freshly recorded patch's identity collides with an applied
  patch (re-creating a deleted cell with identical content),
  `SetAndRecord` deterministically shifts the new nodes' identity
  space so the patch stays applicable while concurrent identical
  re-creations still converge.
- Conflict cells are derived in `cellsFromConflictedGraggle` by
  grouping live nodes by their cell path. Every live node for a given
  path becomes one side of the resulting `ConflictData`: the first
  two as `AliceValue` / `BobValue` and any extras as `OtherValues`,
  so 3+-way conflicts (multiple actors editing the same cell, or
  cycle conflicts that surface as N live nodes for one path) render
  one Keep button per side. Cell ordering in conflict mode is
  alphabetical (the linear case preserves user order).
- `changesForResolution` classifies conflicted paths via
  `algo.DetectConflicts` order/cycle membership (bare path
  multiplicity would misclassify linearly-ordered duplicate keys) and
  accepts arbitrary chosen values: per conflicted path, every live
  sibling whose content does not match the chosen value is deleted,
  and the chosen value's existing node (if any) is kept. If no
  existing sibling matches — the user typed a brand-new value — every
  sibling is deleted and a new node is added carrying the chosen
  value, anchored to a parent / downstream shared by the conflict
  siblings via `commonAnchors`.

These properties are exercised continuously by a rapid state machine
(`pushout_statemachine_test.go`) that drives random verb sequences over
three repos and checks graggle invariants, content conservation, and
post-sync convergence after every action;
`scripts/dev/cover_pushout.sh` enforces a per-package coverage floor
over the tree.

## Open design questions

### Antiquing

Pijul records each patch in its "most-antique" form — every patch is rewritten
to depend only on what it genuinely needs, so two patches that don't truly
depend on each other can be applied in either order. Boxer's port does not
currently perform this rewrite; this section documents what the gap is and
where it eventually bites. The full design-space analysis (architecture
options, subsidiary decisions, open questions) is in
[ADR-0039](../../../../doc/adr/0039-pushout-antiquing.md); the text below is
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
(`pijul_pushout_backend.go`) picks the first live parent and child, which
can be more conservative than the antique form requires.

**Observed gap.** A patch may declare dependencies on patches it does not
truly need. A peer attempting `Apply` is then rejected for "missing
dependency" where an antiqued version would have applied cleanly. The gap
has not surfaced as a bug in the demo workflows so far; it is a latent
semantic divergence from the patch-theory framing the package adopts
(`Background` above), not a known regression.

**Why it matters.** Antiquing is not a compliance prerequisite —
ADR-0025's Architecture A (vault-by-design) does not construct
compensating patches, and ADR-0027 is superseded by that decision.
Compensating-patch construction for cooperative-purge erasure (the
rejected ADR-0025 Architecture B; inapplicable SD4) would have needed
the dependency-minimisation output antiquing produces; that path is
not pursued. What remains is the on-its-own-merits correctness story:
without antiquing, a peer attempting `Apply` may be rejected for "missing
dependency" where an antiqued version would have applied cleanly, and
`commonAnchors`'s "first live parent / first live child" pick over-anchors
in conflict-resolution paths. The
[ADR-0039](../../../../doc/adr/0039-pushout-antiquing.md)
engineering recommendation (staged B → C) addresses both, independently
of any forget-architecture milestone.

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
[ADR-0039](../../../../doc/adr/0039-pushout-antiquing.md) SD1–SD10 and
OQ1–OQ6 for the enumerated options and the engineering recommendation
(staged B → C).

## Trade-offs

- The CLI runner has a 15-second timeout per mutating command and a
  5-second timeout for log/credit reads. These are generous for the
  demo's small dataset but would not survive on a production-scale
  repo; the native backend has no such bound.
- The demo serialises all mutating actions through a single worker
  goroutine. This is intentional — it reflects how a real
  multi-actor workflow treats each working copy as a serialised
  resource — but it means the UI shows a "processing" indicator for
  the slowest of the four actors during reload.
- On the text backend, `Email Patch` extracts the most recently
  modified file under `.pijul/changes/` rather than parsing Pijul's
  transactional state. This is a portable approximation that breaks if
  the user records multiple patches faster than mod-time resolution;
  the native backend reads the envelope by patch hash directly.

## Further reading

- Theory: [A Categorical Theory of Patches](https://arxiv.org/abs/1311.3903).
- Decisions: [ADR-0079](../../../../doc/adr/0079-pushout-production-storage-codec-exchange.md)
  — production architecture (storage, wire-codec, and transport seams,
  recovery semantics, conformance-suite pattern).
- Distributed operation: [explanation](../../../../doc/explanation/pushout-distributed-operation.md)
  — clocks and causality, version comparison, delta-discovery ladder
  (set-reconciliation sketches), cherry-picking, broker vs. mesh
  topologies.
