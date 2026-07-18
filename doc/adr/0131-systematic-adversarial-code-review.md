---
type: adr
status: proposed
date: 2026-07-18
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0131: Systematic adversarial code review

## Context

Adversarial review already happens in this repo, but ad hoc — the leeway
2026-06 deep review, the caching and nanopass remediation sweeps, the play
contract review each ran once, by hand, and left their record scattered across
ADR `Updates` and commit messages. Nothing says *which* subsystems have been
adversarially examined, *at what revision*, or *when that examination went
stale*. Trunk-based development compounds this: there is no merge gate and no
pull-request thread to carry a review, so a review that is not recorded
co-located with the code is, in practice, not recorded at all.

The repo already has the pieces of an answer, each scoped to one narrow domain:

- **The documentation state machine** records a review as `reviewed-by` /
  `reviewed-date` front-matter and gates it with `boxer gov doclint` (DL003) —
  but its stance is deliberately anti-churn: a review covers the body *as it
  stands at the flip* and is never re-opened by later edits.
- **The Tier-2 `designreview` driver** ([ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD9)
  grades screenshots adversarially and keys each finding on
  `blake3(input ‖ rubric_version ‖ model)` — so re-review *is* a cache miss.
  This is the mechanism that generalizes, but it grades pixels, not source.
- **`/code-review` (tiered) and `ultra`** already perform the review itself.
- **`packageprops`** ([ADR-0080](./0080-packageprops-per-package-declarations.md))
  is the repo's committed, diffable, IDE-navigable per-package record, whose
  `props verify` already reconciles a *declared* value against a *freshly
  computed* one and gates CI on drift — structurally identical to detecting a
  stale review.

What is missing is the connective rule: review-critical code is reviewed
adversarially, the review is recorded co-located with the code, and material
drift from the reviewed revision triggers a re-review. The shape below was
settled in a design dialogue on 2026-07-18.

## Design space (QOC)

**Question.** Where should a per-subsystem review record live so it is
co-located, navigable, reconcilable against drift, and harvestable?

**Options.**

- **O1** — A fresh checked-in ledger under `doc/reviews/` (findings JSON + a
  coverage index), detached from the source it describes.
- **O2** — `git notes` attached to reviewed commits.
- **O3** — A `Review` field group on `packageprops.Props`, declared in each
  reviewed package's `package_props.go` *(chosen)*.

**Criteria.**

- **C1** — Co-located with the code it describes.
- **C2** — IDE-navigable / refactor-safe (find-references lists reviewed packages).
- **C3** — A drift reconciler already exists (re-review trigger is not new build).
- **C4** — Harvestable into a facts/overview table.
- **C5** — Low ceremony; durable under trunk-based git.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −  | −  | ++ |
| C2 | −  | −− | ++ |
| C3 | −  | −  | ++ |
| C4 | +  | −  | ++ |
| C5 | +  | −− | +  |

O3 dominates on C1–C4: it reuses `props verify` as the re-review trigger (C3)
and `props harvest --emit` as the facts bridge (C4, already ADR-0080 open-Q#4),
where O1/O2 would rebuild both. `git notes` (O2) are additionally fragile — they
do not fetch or push by default and merge awkwardly. O1 survives only as the
*sidecar* for the heavy findings content (see SD2), not as the record's home.

## Decision

Adopt a standing rule — **review-critical code is reviewed adversarially, the
review is recorded, and drift from the reviewed revision triggers a
re-review**. The operative rule lives in
[CODINGSTANDARDS § Adversarial Code Review](../../CODINGSTANDARDS.md#adversarial-code-review);
this ADR records the decision and its rejected alternatives, and does not
restate the rule.

The record is a new `Review` field group on `packageprops.Props`
([ADR-0080](./0080-packageprops-per-package-declarations.md)), matching that
file's local naming (`WASMState`, `Kind` — no `E` suffix):

```go
// ReviewState is the human verdict of a package's last adversarial review —
// the half no oracle can compute. Freshness is not stored here; `props verify`
// derives it by comparing Review.Hash against the current normalized source.
type ReviewState uint8

const (
	ReviewUnreviewed   ReviewState = iota // no review asserted (zero value)
	ReviewClean                           // reviewed; no open findings
	ReviewAcceptedRisk                    // reviewed; open findings knowingly accepted
)

type Review struct {
	State    ReviewState
	Hash     string // gofmt-normalized package-source digest the review covered
	By       string // "code-review@<tier>" | "ultra"
	Sha      string // commit reviewed
	Date     string // YYYY-MM-DD
	Findings string // content-address / path of the findings sidecar
}
```

### Subsidiary design decisions

- **SD1 — The record extends `packageprops`, not a new store.** Per the QOC
  above; the drift reconciler and the facts bridge already exist there.
- **SD2 — Marker summary in `Props`, findings content in a sidecar.** `Props` is
  a deliberately clean, zero-dep, tag-free vocabulary (ADR-0080 §SD5); the
  repros and per-finding dispositions live in the `/code-review` / `ultra`
  findings blob that `Review.Findings` references, mirroring how `designreview`
  splits its verdict summary from `report.md`.
- **SD3 — Re-review is normalized-hash drift.** A review-aware `props verify`
  recomputes each marked package's gofmt-normalized source digest and flags any
  that differ from `Review.Hash`. This is the computable half, reconciled like
  the WASM* verdicts; `ReviewState` is the curated half, unreconciled like
  `Kind`. Normalizing the hash keeps comment- and whitespace-only churn from
  firing a spurious re-review.
- **SD4 — The engine is `/code-review` + `ultra`.** Tier the depth to blast
  radius; reserve `ultra` for the highest-blast-radius subsystems. Every finding
  carries a concrete failure scenario, and is itself verified (an attempt to
  refute it) before it is acted on.
- **SD5 — Advisory first, graduate on a bounded false-positive rate.** `verify`
  reports drift without failing CI until the signal is calibrated, then
  graduates to a gate — the same lifecycle the Tier-2 rubrics follow
  (ADR-0029 §SD9 Calibration).
- **SD6 — Scope is opt-in.** A package acquires a review obligation once it
  carries a `Review` marker or is designated review-critical; the zero value
  asserts nothing, as `Kind` does. The initial review-critical set is the
  subsystems where a latent defect is expensive — leeway's pipeline stages, the
  nanopass passes, the FFI boundary, identity and marshalling.

## Alternatives

- **Rebuild the `adversarial-review` driver tree.** Rejected: its
  `cmd/adversarial-review` tree was never imported from pebble2impl (the
  dependent command is dropped at `public/app/main.go:95`), and `/code-review` +
  `ultra` already cover the engine. Provider abstraction can be revisited if a
  local-model path is wanted later.
- **A hard CI gate from day one.** Rejected: an un-calibrated reviewer's false
  positives would make the gate resented and quickly bypassed. Advisory-first
  with an explicit false-positive budget (SD5) is how the design-review tier
  earned its gates.
- **Per-file or per-symbol review hashing.** Rejected for v1: package
  granularity matches how adversarial review is actually scoped here (whole
  subsystems, not single functions), and finer state can move into the sidecar
  later without touching the `Props` coordination point.

## Consequences

### Positive

- The review record is committed, diffable, and IDE-navigable — find-references
  on a `ReviewState` constant *is* the coverage report.
- The re-review trigger is not new machinery: it reuses the `props verify`
  declared-vs-computed reconciliation that already gates CI.
- Projecting review status as queryable facts (a `play` dashboard, drift as a
  `GROUP BY`) is the existing `props harvest --emit` path, not extra build.

### Negative

- `Props` gains a coordination-point field; ADR-0080 flags that adding a field
  touches the generator and, potentially, every declaration. A one-time cost.
- A findings-sidecar format and the review-aware `verify` reconciliation are new
  build.
- Per-package granularity re-stales an entire package on any material change to
  it, even when the changed code was outside the reviewed concern.

### Neutral

- `packageprops` rolled out across `public/` but not `apps/` (ADR-0080), so
  initial coverage is `public/` — where the review-critical subsystems live.
- The review-critical set is a curated judgment, like `KindIntegrationTest`;
  there is no oracle that computes membership.

## Status

Proposed — awaiting review by @spx.

Open questions:

1. **Scope mechanism** — a distinct `review-critical` designation that `verify`
   hard-requires (errors when such a package is unreviewed *or* stale), versus
   pure opt-in (only ever-reviewed packages are reconciled). Leaning
   opt-in-plus-a-flag.
2. **Normalized-hash definition** — gofmt-normalized bytes (conservative;
   comment edits still fire) versus AST-structural (comment-insensitive).
3. **Sidecar format and the facts bridge** — defer to ADR-0080 open-Q#4.
4. **CLI home** — a review-aware pass under the existing `props` command group
   versus a new `boxer review` entry point.

On acceptance: ADR-0080 gains a dated `## Update` introducing the `Review` field
group (mirroring the 2026-07-02 `Kind` update), and the rule lands in
CODINGSTANDARDS § Adversarial Code Review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0080](./0080-packageprops-per-package-declarations.md) — `packageprops`; the record's substrate (§SD4 growth, §SD5 clean vocabulary).
- [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD9 — the Tier-2 adversarial-review precedent (cache-as-trigger, advisory→gate calibration).
- [ADR-0092](./0092-adr-overview-tool.md) — the status × code-evidence harvest pattern the facts projection would follow.
- [CODINGSTANDARDS § Adversarial Code Review](../../CODINGSTANDARDS.md#adversarial-code-review) — the operative rule.
- `/code-review` and `ultra` — the review engines (harness commands).
