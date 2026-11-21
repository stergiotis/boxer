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
scripts is the working gate. Runner cost is to stay low, so recurring work
belongs on the workstation rather than on hosted runners. Three things
undercut the arrangement:

- **The local run tested the working tree, not a commit.** The tree is shared
  by concurrent sessions and is rarely clean, so a green run vouched for
  whatever was lying in it, and a red one could be someone else's edit.
- **Nothing tested `main` between releases.** A consumer that pins a commit
  had no way to tell which commits had passed anything.
- **Several checks existed but blocked nothing.** The drift check after the
  tests lived only in the workflow YAML; staticcheck and errcheck covered
  `public/` only and could only warn, so a new finding was indistinguishable
  from the old ones; the integration lane, the fuzz lane and the benchmark
  smoke ran nowhere on a schedule; some generators were compared with their
  committed output by no test.

## Decision

1. **The gate, the nightly run and the fuzz run are local scripts, and each
   tests a commit.** They check the commit out into a detached worktree under
   `$TMPDIR` (one stable path per script, repository and user, so the Go build
   cache carries over; only the worktree the script created is ever removed).
   - `scripts/ci/gate.sh [<rev>]` — before a push: module checks, build, gofmt
     on the changed files, vet and `-race -short` tests over the changed
     packages and their direct importers, the drift check, and the IDL, Rust
     crate and host digest checks when the range touches them. A committed
     `pre-push` hook runs it, opt-in per clone through `core.hooksPath`.
   - `scripts/ci/nightly.sh` — `origin/main` once a day: the test lanes with
     coverage, the Go lint steps, the integration lane against a ClickHouse
     server, benchmark smoke, the viewer and watermark crates.
   - `scripts/ci/fuzz_weekly.sh` — every fuzz target, a bounded time each.
   Scheduling is the workstation's: example systemd user units at idle
   priority are committed; nothing installs them.
2. **`last-green` is advanced locally and published by hand.** A full green
   nightly run fast-forwards the local `refs/heads/last-green` to the tested
   commit — never forced, never backwards — and prints the push command. A
   run that skipped or could not start part of its work does not move it.
3. **Workflows stay dispatch-only wrappers or tag-time checks.** No schedule
   is added for tests, lint, integration, coverage, benchmarks or fuzzing, and
   no workflow is added for them: a dispatch of the existing tag-time
   workflows already gives a clean-machine run.
4. **staticcheck and errcheck run over the whole module against committed
   baselines.** A finding outside the baseline fails the step; a baseline entry
   that no longer occurs is reported, so the file only shrinks. Entries are
   keyed by file and message without line numbers.
5. **A release is checked by a script and tagged by a person.**
   `scripts/ci/release.sh` runs every gate in a worktree of the candidate,
   counts a skipped part as a failure, and prints the signed-tag commands
   without running them. It shares its step wrapper with the nightly run.
6. **The nightly run and the release check use a ClickHouse server they
   started.** Before any test step, `CLICKHOUSE_ENDPOINT` and `CLICKHOUSE_URL`
   name `127.0.0.1` at free ports away from ClickHouse's defaults, and the
   variables that would name another server are cleared; the pinned server of
   `clickhouse_server.sh` is started there under the run's state directory and
   stopped on exit. A server already answering on the default port is used
   only on an explicit flag, `--use-running-clickhouse`. The server is
   provisioned with what a host's first run creates. The integration lane
   runs with `--strict-skips`, so a skipped member outside a committed
   baseline of expected skips fails the step.

## Alternatives

- **A nightly and a weekly fuzz workflow on GitHub's cron, with the runner
  advancing `last-green` through the refs API.** Built first and killed:
  every run is recurring runner cost for work the workstation does at no
  cost, and it needed `contents: write` on a scheduled job.
- **A dispatch-only nightly workflow wrapping `nightly.sh`.** Killed: it adds
  no check a dispatch of `test.yaml`, `lint.yaml` or `watermark.yaml` does
  not, and it would repeat their toolchain setup.
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
- **Use the ClickHouse server already answering on port 8123, and start the
  pinned one only when none does.** Rejected: on a workstation that server
  holds real databases, and the lanes create, drop and write scratch
  databases and read `system.query_log`; a release check also should not
  depend on a server whose version and settings it did not choose.
- **Run the lanes in a network namespace of their own, where 8123 is free.**
  Not taken: unprivileged namespaces are restricted on some distributions,
  and the downloads the other steps need would have to be arranged inside it.
- **Fail the integration step on every skipped member.** Rejected: most
  skips in the lane are by design — diagnostics, fuzz-seed entries,
  tag-gated and opt-in tests — so the step could never pass.
- **Warn-only static analysis with a findings count, or failing on every
  finding outright.** Rejected: a count hides which finding is new, and the
  existing findings, mostly in `apps/`, are unrelated work.

## Consequences

### Positive

- A local green result describes a commit, which a later reader can check
  out.
- A consumer can pin `last-green` once someone has pushed it.
- No recurring runner minutes.
- A new staticcheck or errcheck finding blocks; an old one is visible by name.

### Negative

- `main` is tested nightly only where a workstation runs the timer, and
  `last-green` is only as current as the last manual push; nothing on GitHub
  says whether either happened.
- Each run starts a ClickHouse server (the first run downloads ~240 MB), and
  the default lane's probe-and-skip tests share it with the parallel test
  binaries in the release check. Outside these scripts, the default lane's
  probe-and-skip tests still reach whatever answers at the address the
  environment names, `localhost:8123` when it names none.
- A live-server test that builds its client from a hard-coded address
  escapes the run's server; nothing but review and a grep catches one.
- `gate.sh` costs a full module build on its first run at a path; above a
  package cap it drops `-race`.
- The baselines are files to keep current.

### Neutral

- Existing tags are lightweight and unsigned; the signed-tag procedure
  applies from the next tag.

## Verification plan — Tier 1

- **Lane.** `gate.sh` before a push; `nightly.sh` and `fuzz_weekly.sh` from
  the user timers; the `staticcheck` and `errcheck` lint steps; `release.sh`
  at a release.
- **What would fail.** A commit whose tests leave generated code dirty fails
  `gate.sh` and the nightly run; a new finding fails the lint step; a nightly
  run that fails, skips or cannot start a step leaves `last-green` where it
  was and exits non-zero, which the timer unit shows; an integration lane
  whose server was never reached lists its skipped members and fails.
- **Gap.** A workstation that does not run the timers runs no nightly check,
  and nothing reports the absence. A test that dials a hard-coded address
  instead of the configured one is not caught by any step.

## Choices for review

- The nightly run fails when a step reports a skipped part or cannot start,
  including the integration lane when its server does not start;
  `last-green` only moves on a run where everything ran.
- Both scripts start their own ClickHouse server by default and never fall
  back to one already answering; `--use-running-clickhouse` is the only way
  to use that one, and its help says the lanes write scratch databases there.
  The ports are the first free ones from 18123 (HTTP) and 19000 (native).
  nightly.sh still accepts `--start-clickhouse`, as a no-op, so an existing
  unit file keeps working.
- `release.sh` starts the server before the first gate, so the default lane
  runs against it as well; `nightly.sh` starts it for the integration step
  only, and its first default-lane pass runs with the address pointing at a
  port where nothing answers.
- `--strict-skips` fails the integration step on a skipped member that
  `scripts/ci/integration-skips-baseline.txt` does not list, and reports a
  listed one that ran. The baseline holds the skips by design and those for a
  tool, a corpus, the headless client or server state the run does not
  provision, in named groups; deleting a group makes that absence a failure.
  No ClickHouse-server test is listed, so a lane whose server was never
  reached fails.
- The run's server is provisioned as a host's first run leaves one — the
  facts table and leeway's SQL read surface, built from the commit under
  test — and keeps `asynchronous_metric_log`; the lading store, the data
  catalog and the anchor demo table are not provisioned, and their tests are
  in the baseline.
- `last-green` is a branch ref, moved only by `nightly.sh`, only on a full run.
- The example timers fire at 02:17 daily and Saturdays at 04:23, with
  `GOMAXPROCS=8`, `Nice=19`, idle I/O; fuzzing is 30 s per target.
- The pinned throwaway server is the 26.8 LTS static build, pinned to a
  release past a five-day age, like the dependency cooldown of
  [ADR-0362](./0362-dependency-updates-land-as-gated-local-commits.md).
- staticcheck aborting fails its step instead of warning.
- `gate.sh` diffs against the merge-base with `origin/main`; above 150
  packages it tests without `-race`.

## Status

Proposed — awaiting review by the maintainer.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ENGINEERING_PRACTICES](../ENGINEERING_PRACTICES.md) §1, §2, §4, §6, §10.
- [ADR-0362](./0362-dependency-updates-land-as-gated-local-commits.md) — the
  gated local commit `release.sh` is modelled on.
- [How to sign and verify boxer releases](../howto/release-signing.md).
