---
type: adr
status: proposed
date: 2026-11-20
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0390: A gate that tests the commit, and a ref that names the last green main

## Context

CI in this repository runs at a release tag and on manual dispatch, and
deliberately not on push or pull request
([ENGINEERING_PRACTICES §1](../ENGINEERING_PRACTICES.md#1-ci-surface), §10):
development is trunk-based and AI-assisted, and the local run of the CI
scripts is the working gate. Three things undercut that arrangement:

- **The local run tested the working tree, not a commit.** The tree is shared
  by concurrent sessions and is rarely clean, so a green run vouched for
  whatever was lying in it, and a red one could be someone else's edit.
- **Nothing tested `main` between releases.** The scheduled workflows run no
  tests. A consumer that pins a commit had no way to tell which commits had
  passed anything.
- **Several checks existed but blocked nothing.** The drift check after the
  tests lived only in the workflow YAML; staticcheck and errcheck covered
  `public/` only and could only warn, so a new finding was indistinguishable
  from the old ones; the integration lane, the fuzz lane and the benchmark
  smoke ran in no workflow; some generators were compared with their committed
  output by no test.

## Decision

1. **The local gate tests a commit.** `scripts/ci/gate.sh [<rev>]` checks the
   revision out into a detached worktree under `$TMPDIR` and runs the module
   checks, the build, gofmt over the changed files, vet and `-race -short`
   tests over the changed packages and their direct importers, the drift
   check, and — when the range touches the egui2 IDL, its bindings or
   `rust/imzero2` — the IDL check, the Rust crate check and the host digest.
   The worktree has one path per repository and user, so the Go build cache
   carries over between runs; it removes only the worktree it created. A
   committed `pre-push` hook runs it, opt-in per clone through
   `core.hooksPath`.
2. **A nightly workflow tests `main`, and `refs/heads/last-green` names the
   newest commit that passed it.** Every job green on `main` fast-forwards the
   ref through the refs API with `force=false`, so it never moves backwards.
   No push or pull-request trigger is added. A separate weekly workflow fuzzes.
3. **staticcheck and errcheck run over the whole module against committed
   baselines.** A finding outside the baseline fails the step; a baseline entry
   that no longer occurs is reported, so the file only shrinks. Entries are
   keyed by file and message without line numbers.
4. **A release is checked by a script and tagged by a person.**
   `scripts/ci/release.sh` runs every gate in a worktree of the candidate,
   counts a skipped part as a failure, and prints the signed-tag commands
   without running them.

## Alternatives

- **Push and pull-request triggers.** Rejected for the reasons §10 records:
  the workflow is direct-to-`main`, and a red push run reports after the fact
  what the local gate can report before it.
- **Test the working tree, but refuse a dirty one.** Rejected: in a shared
  tree it is never clean, and the refusal would push people to skip the gate.
- **A fresh temporary worktree per run.** Rejected: the Go build cache keys
  on the package directory, so every run would recompile the module.
- **A moving tag instead of a branch ref for last-green.** Rejected: a tag
  that moves breaks the expectation that tags are fixed, and the release tags
  share the namespace.
- **Warn-only static analysis with a findings count.** Rejected: a count
  hides which finding is new; a keyed baseline names it.
- **Fail on every finding outright.** Rejected for now: 19 staticcheck and 21
  errcheck findings exist, mostly in `apps/`, and fixing them is unrelated
  work.

## Consequences

### Positive

- A local green result describes a commit, which a later reader can check
  out.
- A consumer can pin `last-green` or any commit it has pointed at.
- A new staticcheck or errcheck finding blocks; an old one is visible by name.

### Negative

- `gate.sh` costs a full module build on its first run at a path, and the
  `-race` tests over a large range take minutes; above a package cap it drops
  `-race` and says so.
- The nightly workflow runs the long lanes on GitHub's runners every day.
- The baselines are files to keep current.

### Neutral

- Existing tags are lightweight and unsigned; the signed-tag procedure
  applies from the next tag.

## Verification plan — Tier 1

- **Lane.** `gate.sh` run locally; the `nightly` and `fuzz` workflows; the
  `staticcheck` and `errcheck` lint steps; `release.sh` at a release.
- **What would fail.** A commit whose tests leave generated code dirty fails
  `gate.sh` and the nightly; a new finding fails the lint step; a nightly job
  failing leaves `last-green` where it was.
- **Gap.** The workflows can only be exercised on GitHub; the ClickHouse
  server configuration in `scripts/ci/clickhouse_server.sh` and the
  `last-green` API calls have not run before their first scheduled run.

## Choices for review

- The nightly runs at 02:17 UTC and the fuzz workflow on Saturdays at
  04:23 UTC, 30 s per target by default.
- `last-green` is a branch, advanced only by the nightly, only from `main`.
- The integration job runs the default lane a second time with a server up,
  so the probe-and-skip tests execute for real.
- ClickHouse for CI is the 26.8 LTS static build, pinned to a release past a
  five-day age, like the dependency cooldown of
  [ADR-0362](./0362-dependency-updates-land-as-gated-local-commits.md).
- staticcheck aborting now fails its step instead of warning.
- `gate.sh` diffs against the merge-base with `origin/main`; above 150
  packages it tests without `-race`.
- `release.sh` uses a ClickHouse server already answering on port 8123 before
  starting the pinned one.

## Status

Proposed — awaiting review by the maintainer.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ENGINEERING_PRACTICES](../ENGINEERING_PRACTICES.md) §1, §2, §4, §6, §10.
- [ADR-0362](./0362-dependency-updates-land-as-gated-local-commits.md) — the
  gated local commit `release.sh` is modelled on.
- [How to sign and verify boxer releases](../howto/release-signing.md).
