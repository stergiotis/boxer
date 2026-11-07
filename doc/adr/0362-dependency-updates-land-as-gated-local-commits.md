---
type: adr
status: proposed
date: 2026-11-07
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0362: Dependency updates land as gated local commits; Dependabot only alerts

## Context

Go module and Cargo lockfile updates arrived as Dependabot pull requests, one
per dependency, merged on GitHub. Since 2026-06-27 every workflow runs on `v*`
tags and manual dispatch only, so nothing checked those PRs before they
merged. [ADR-0205](./0205-imzero2-cpu-rasterized-pixel-host.md) records the
result: three bumps merged green and broken on 2026-08-10, and a lock-only egui
float on 2026-08-19.

Dependabot also cannot run the repository's own follow-up steps. A change to
`rust/imzero2` must refresh the browser host digest in the same commit
([ADR-0278](./0278-tab-mode-for-downstream-apps.md) SD5), and the license gate
([ADR-0004](./0004-license-gate-cyclonedx.md), ADR-0246) runs only locally or on
a tag. Its PRs also add a branch and a merge commit to a trunk-based history.

## Decision

1. **Updates are local commits.** [scripts/dev/updatedeps.sh](../../scripts/dev/updatedeps.sh)
   updates one target per run: the Go module, or one `rust/*` lockfile. It
   runs that target's gates and leaves the change in the tree, or commits it by
   explicit path with `--commit`. The result is one commit per target on
   `main`.
2. **The cooldown is the script's.** A version published fewer than
   `--cooldown-days` days ago (default 5, Dependabot's previous setting) does
   not land. The script checks the publish time of every version that changed
   in `go.mod` or the lockfile, not only the ones it asked for. Go reads it from
   the module proxy; Cargo from the crates.io sparse index (`pubtime`).
3. **Only compatible versions move.** The Go side moves within a major, through
   `go get`. The Cargo side runs `cargo update` and never edits `Cargo.toml`.
   The egui ring therefore cannot step a minor on its own: that stays a human
   edit of the manifest, as the ring's note in `.github/dependabot.yml` already
   requires. Versions `cargo update` cannot reach are listed, not applied.
4. **A skipped gate fails the run.** The Rust CI scripts print `skipped` and
   exit 0 when a toolchain is missing. Under the update script, a skip counts
   as a failure.
5. **Dependabot stays as the alert channel.** Version-update PRs for `gomod`
   and `cargo` are switched off (`open-pull-requests-limit: 0`). Security
   alerts continue. A security PR is a notice: the fix lands through the
   script. GitHub Actions updates still come as PRs, because they change only
   CI and their SHA pins are what Dependabot does well.

The tag-only CI decision of 2026-06-27 is unchanged.

## Alternatives

- **Keep Dependabot and add `pull_request` CI triggers.** This gates the PRs,
  but still can't refresh the host digest. It also reverses the tag-only
  decision for every PR, to cover traffic that a local run handles.
- **Renovate.** It can group the egui ring and batch updates. Running the
  digest refresh needs self-hosted `postUpgradeTasks`, so it adds an operated
  service to a supply chain the repository keeps small. Its updates are still
  PRs, so it still needs CI triggers.
- **The previous `updatedeps.sh`** (`go get -u ./...`, `go mod tidy`). Kill
  reason: it had no cooldown, no gates and no Rust side.

## Consequences

- Updates happen when someone runs the script, not on a schedule. If nobody
  runs it, dependencies go stale; security alerts are the backstop.
- The gates run against the working tree. In a tree shared with other
  sessions, a red gate may come from someone else's uncommitted change. The
  script refuses to start if its own target files are dirty.
- The Cargo cooldown is the script's, not cargo's, because cargo 1.96 has no
  stable minimum publish age. When cargo gains one, the script should use it.
- A `0.x` minor bump (for example `protox 0.9 → 0.10`) is a semver-major
  change. It no longer arrives automatically; the script lists it as not
  reachable by `cargo update`.

## Status

Proposed 2026-11-07, awaiting review.

## References

- [ADR-0205](./0205-imzero2-cpu-rasterized-pixel-host.md) — the broken bumps and the imzero2 gate
- [ADR-0278](./0278-tab-mode-for-downstream-apps.md) SD5 — the browser host digest
- [ADR-0004](./0004-license-gate-cyclonedx.md) — license gate
- [ENGINEERING_PRACTICES §6](../ENGINEERING_PRACTICES.md#6-supply-chain-and-license-gates)
