---
type: how-to
audience: developer using boxer from a repository of their own
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to start using boxer

For a developer who wants to build on boxer rather than work inside its tree.
Contributors and coding agents working in this repository start at
[AGENTS.md](../AGENTS.md) instead. Boxer is alpha: the API changes, and the
documents below are drafts unless their header says otherwise.

## 1. Decide what to pin

Boxer's `main` moves quickly and its tags are rare; pin a commit of `main`,
and bump it deliberately. The [README](../README.md#installation) has the
command and the reason, and links the versioning policy: what a bump may
change, and how a change is announced.

## 2. Build something small

Two tutorials, each from an empty directory to something that runs, in a
module of your own:

- [Your first app](tutorials/first-app.md) — one window, one value kept
  across runs, a help page, and a host binary that opens it. Needs the Rust
  client and a desktop session.
- [Your first leeway table](tutorials/leeway-first-table.md) — describe a
  table, generate its code, write rows, read them back, and query them with
  `clickhouse local`. Needs only Go.

## 3. Understand the shape

- [Why boxer](explanation/why-boxer.md) — the premises, with their costs.
- [Architecture](ARCHITECTURE.md) — the operation modes (desktop, headless,
  browser tab, appliance) and the data architecture.
- [The keelson-app skill](skills/keelson-app/SKILL.md) — what an idiomatic
  app looks like: data as Arrow, an explicit state machine, tasks,
  operations an agent can call, help.
- [The leeway explanation](../public/semistructured/leeway/EXPLANATION.md) —
  the data-mapping pipeline behind the tables.

## 4. Adopt the standards and the gate

[Adopting boxer downstream](howto/adopting-boxer-downstream.md) sets up
boxer's lint gate, coding and documentation standards in your repository
through the `go.mod` pin, and shows the host binaries — desktop, headless
and browser tab — for your own apps.

## 5. Look up a task

The [how-to guides](howto/) are recipes for one task each. Ones a newcomer
reaches for early:

- [Launch apps non-interactively](howto/launch-apps-non-interactively.md),
  and drive or capture them headless.
- [Run an app in a browser tab](howto/imzero2-in-the-browser.md).
- [Give an app durable work](howto/watchbill-jobs.md).
- [Persist a new kind of fact](explanation/facts-bound-record-stores.md).
- [Read a leeway table from SQL](explanation/leeway-sql-read-surface.md).
- [Build on a host with no network](howto/airgapped-build.md).

Package reference is on [pkg.go.dev](https://pkg.go.dev/github.com/stergiotis/boxer);
the decisions behind the design are the [ADRs](adr/).
