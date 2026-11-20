---
type: adr
status: proposed
date: 2026-11-20
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0388: Contract snapshots — what is compared against the last release

## Context

The tree's goldens (DDL, codecs, vocabularies) are rewritten by their own
tests, and CI checks only that the tree is clean afterwards. A golden that
changes in a commit is therefore accepted as soon as it is regenerated: the
check says the generated text matches the generator, not that it still
matches what a deployment or another build holds. Nothing compared any
stored, wire or command-line shape with the shape at the last release, so a
column rename, a membership id that moved, a removed CLI flag or a bumped
wire version reached `main` looking like any other regeneration.

## Decision

We keep a **contract snapshot** of the tree in `contracts/<commit>/`,
named for the commit it was taken at and pointed to by `contracts/BASELINE`,
and compare the tree against it with `boxer gov contracts diff`
(`scripts/ci/contracts.sh`). The baseline moves when the owner takes a
release; between releases it stays fixed, so every difference accumulated
since is in view.

**What is in a snapshot** (`public/gov/contracts`), with the rule that
decides whether a difference is **additive**:

| Kind | What | Additive when |
| --- | --- | --- |
| `ddl` | the DDL golden of every production table (`*_ddl_clickhouse.out.sql`, not examples, demos, fixtures, the leeway documentation anchor or the shared-section demonstration store) | every line of the baseline is still there, compared as trimmed lines with a trailing comma dropped; a new table |
| `membership` | the membership-assignment golden of every vocabulary | every line of the baseline is still there; a new vocabulary |
| `lwsurface` | the leeway SQL surface's generation, digest and function roster (ADR-0171) | functions only added; a changed digest only with a higher generation, because an older build leaves a newer surface in place (ADR-0171, update of 2026-11-20) |
| `env` | the registered environment variable names (ADR-0009) | names only added, or removed with a tombstone (`env.RetiredVars`) |
| `idl` | the imzero2 IDL fingerprint | never: the fingerprint cannot say what changed |
| `version` | the wire and format version constants — the bus `wireVersion`s, `capture.TreeVersion`, `jackstay.PlanFormatVersion`, the canonical wire's `Version`, the notebook, workstatelet and scene `FormatVersion`s | only added: a version moves exactly when an older reader cannot read the newer form |
| `cli` | every command path and flag under the boxer CLI | commands and flags only added |

A removed item is never additive, except a tombstoned environment variable.

**Acknowledging a non-additive change.** A note under `doc/migration/`
names the change on a line `contract: <kind>/<item>` (the key `diff`
prints). The note is the place a reader upgrading a deployment finds what to
do, so writing it is what makes the change pass; `diff` reports the change
as acknowledged and names the note. The baseline is not regenerated to make
a change pass.

## Alternatives

- **Regenerate the baseline with the change.** Every commit would carry its
  own baseline, and the comparison would be against the previous commit
  rather than the release; a change and its acknowledgment would be the same
  diff to a reviewer. Killed: it compares against the wrong thing.
- **Compare against a git tag.** Needs the tag to exist and a build of the
  tagged tree in CI. Tagging is the owner's act, and a committed snapshot
  makes the comparison an offline file read. Kept as a possible later source
  for the snapshot, not the comparison.
- **Byte-compare the goldens.** Every added column would need an
  acknowledgment; the line rule lets additions through and stops at the
  first line of the baseline that moved.
- **Classify the IDL by parsing it.** The fingerprint is what the client
  and the host already agree on; a structural IDL diff is the IDL
  handshake's to own. Deferred.

## Consequences

### Positive

- A shape change that a deployment or another build depends on is seen in
  the commit that makes it, and passes only with a migration note.
- The snapshot is plain text; reviewing a baseline move reads as a diff.

### Negative

- A snapshot is only as complete as its kinds. Codec goldens and the
  canonical-wire field layout are not in it; their version constants are.
- The line rule is a heuristic: a DDL change that keeps every line and
  changes meaning (a new `TTL` line, say) passes as additive.
- The `cli` kind covers the boxer CLI's tree as linked into it, not other
  binaries' flags.
- Any branch that changes a contract will see the baseline as stale until
  the baseline is retaken after integration.

### Neutral

- The snapshot is taken by the boxer CLI, which links every package; the
  roster, the registry and the IDL fingerprint are read in-process, the
  goldens and version constants from the tree.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `contracts/<commit>/`, `contracts/BASELINE` | added | retaken at a release |
| `boxer gov contracts snapshot` / `diff` | added | `scripts/ci/contracts.sh` |
| `doc/migration/*.md` | gains `contract: <kind>/<item>` lines | — |

## Migration — Tier 1

Nothing to migrate: the change is additive.

## Verification plan — Tier 1

- **Lane.** `scripts/ci/contracts.sh` against the committed baseline; the
  diff rules are unit-tested in `public/gov/contracts`.
- **What would fail.** A non-additive difference without a note naming it
  exits non-zero and lists `NOT ADDITIVE <kind>/<item>`.
- **Gap.** The script is not wired into `scripts/ci/lint.sh`. A baseline
  taken on a branch goes stale when other branches land; it is retaken
  after integration.

## Choices for review

- Snapshot directory named by the commit's 9-character short hash, with a
  `BASELINE` pointer file, rather than a tag name.
- Acknowledgment by a migration-note line rather than a regenerated
  snapshot or a separate allowlist file.
- The production/non-production split of DDL goldens, including treating
  the shared-section store as a demonstration.
- A version-constant increase counted as non-additive.
- The `cli` kind leaves out urfave/cli's `help` command and `--help` flag.

## Status

Proposed — awaiting review by the maintainer.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0009](./0009-environment-variable-registry.md) — the environment
  variable registry and its tombstones.
- [ADR-0106](./0106-identity-fibonacci-tags-build-tag-retirement.md) — a
  golden that locks two implementations to one another, which a snapshot
  does not replace.
- [ADR-0171](./0171-leeway-sql-read-surface.md) — the SQL read surface and
  its stamp.
- [doc/migration/](../migration/) — the notes that acknowledge a change.
