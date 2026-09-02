---
type: reference
audience: contributor
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-27
---

# AGENTS.md

Orientation for AI coding agents — and new human contributors — working in this
repository. This file is a **router, not a rulebook**: it carries the handful of
repo-specific things that are easy to miss, and points at the authoritative
documents for everything else. When this file disagrees with a linked document,
the linked document wins.

## Start here

| You want to… | Read |
| --- | --- |
| Know what boxer is | [README.md](./README.md) |
| Know why it is shaped this way (the premises) | [doc/explanation/why-boxer.md](./doc/explanation/why-boxer.md) |
| See how the pieces fit — operation modes, data architecture | [doc/ARCHITECTURE.md](./doc/ARCHITECTURE.md) |
| Write Go in the house style | [CODINGSTANDARDS.md](./CODINGSTANDARDS.md) |
| Understand the toolchain (CI, lint, governance, supply-chain) | [doc/ENGINEERING_PRACTICES.md](./doc/ENGINEERING_PRACTICES.md) |
| Write or edit a doc / ADR | [doc/DOCUMENTATION_STANDARD.md](./doc/DOCUMENTATION_STANDARD.md) |
| See *why* the architecture is the way it is | [doc/adr/](./doc/adr/) |
| Read the analysis behind a decision — surveys, measurements, costed options | [doc/adr-background-work/](./doc/adr-background-work/) |
| Run a trial — a reproducible measurement protocol repeated across builds | [doc/trials/](./doc/trials/) |
| **Quote a performance number from a trial** | that trial's README **§0**, never a table under `runs/` — see [doc/trials § Citing a trial](./doc/trials/README.md#citing-a-trial) |
| Configure behaviour via env vars | [doc/env-vars.md](./doc/env-vars.md) |
| Run a task end to end | [doc/howto/](./doc/howto/) |
| Persist a new kind of fact to `boxer.facts` | [doc/explanation/facts-bound-record-stores.md](./doc/explanation/facts-bound-record-stores.md) |
| Snapshot a file tree into ClickHouse and query it | [doc/howto/lading-snapshot-store.md](./doc/howto/lading-snapshot-store.md) |
| Ingest a markdown vault and query its graph, tags and properties | [doc/howto/markdown-facts-obsidian-queries.md](./doc/howto/markdown-facts-obsidian-queries.md) |
| Diagnose janky / laggy rendering | [doc/howto/imzero2-render-troubleshooting.md](./doc/howto/imzero2-render-troubleshooting.md) |
| Report a vulnerability | [SECURITY.md](./SECURITY.md) |

## Build & test: read this first

**Always pass the repo's build tags**, even though [`./tags`](./tags) has been
empty since ADR-0212 retired the last one. Every `go build` / `test` /
`vet` / `run` reads the file rather than hardcoding its contents, so a tag added
later reaches every invocation at once:

```sh
go test  -tags="$(cat ./tags)" ./...
go build -tags="$(cat ./tags)" ./...
```

Editors and LSP read it the same way — export `GOFLAGS=-tags=<contents of
./tags>` so gopls resolves symbols. Details:
[ENGINEERING_PRACTICES §3 — Build-tag discipline](./doc/ENGINEERING_PRACTICES.md#3-build-tag-discipline).

**Building an artifact? Source the build environment.** Two files hold every
flag a shipped binary depends on, so no two build scripts can disagree
([ADR-0215](./doc/adr/0215-retire-mimalloc-reproducible-builds.md)):

- [scripts/dev/go-build-env.sh](./scripts/dev/go-build-env.sh) — tags,
  `-trimpath -buildvcs=auto`, `CGO_ENABLED=0`, `GOTOOLCHAIN` pinned to go.mod.
- [scripts/dev/rust-repro-env.sh](./scripts/dev/rust-repro-env.sh) — the
  `--remap-path-prefix` pair, beside `--locked` on every cargo invocation.

**`fast_alloc` / mimalloc is retired — do not reintroduce it.** Its C sources
compile `__DATE__, __TIME__` into the binary, so every build carried a wall
clock and no release artifact could ever be byte-reproducible. The manifest note
where the feature used to be has the measurement.

**Check module drift with `go mod tidy --diff`**, not `tidy` followed by
`git diff` — the `--diff` form reports drift without mutating `go.mod` / `go.sum`.

**Tests needing a live server or a heavy dependency go in the integration
lane** — `//go:build integration`, run by
[scripts/ci/gotest-integration.sh](./scripts/ci/gotest-integration.sh), never by
the default `go test ./...`. Details:
[ENGINEERING_PRACTICES §4 — Tests](./doc/ENGINEERING_PRACTICES.md#4-tests).

## Version control

Development is **trunk-based**: commit directly to `main`, keep every commit
buildable, keep commits small and single-concern. Full rules in
[CODINGSTANDARDS § Version Control](./CODINGSTANDARDS.md#version-control).

**Stage and commit by explicit path.** A working tree may be shared by more than
one concurrent agent session against a single git index, so `git add -A` can race
and clobber another session's staged work. Scope every commit to the files you
changed: `git commit -- <paths>` (or `git add <paths>` first).

**Don't commit unless asked.** Leave changes in the working tree for review.

## Design before code

For anything past a small, local change — a **new package or non-trivial
subsystem** — start with a design dialogue, and an ADR where it warrants one.
Agree on the shape before writing the implementation. See
[CODINGSTANDARDS § Design Before Code](./CODINGSTANDARDS.md#design-before-code).

When a peripheral piece is heavy or undecided, **descope it rather than gate the
whole design on it**: ship the light cut, record the deferral (an ADR, or a
`// deferred:` note), and move on. Don't block on the hardest 10%.

## ADRs

Architecture Decision Records in [doc/adr/](./doc/adr/) are the primary record of
*why*. Editing policy follows lifecycle stage:

- **Proposed / pre-acceptance** ADRs are living snapshots — edit in place and
  compact the exploration away, but keep the kill-reasons for rejected options.
- **Accepted** ADRs change only via dated entries under `## Updates`, never
  silent rewrites.
- A new decision that supersedes an old one gets its **own** ADR that references
  the superseded one.

## Writing style for committed prose

Repo docs are **descriptive and humble**. No taglines, manifestos, self-praise,
or quality claims ("robust", "comprehensive", "production-grade"). Lead with the
caveat; prefer retracting an overstatement to hedging it. Match the surrounding
document's tone.

## Detail budget

A document carries the decision and what justifies it; the tree carries
everything else. Before a sentence goes into a doc or ADR, two tests: could a
reader **regenerate it from the code** — then link, don't transcribe; would it
**go false after a refactor that doesn't revisit the decision** — then it
describes the environment, not the decision: anchor it to what survives (a
symbol, an `ADR-NNNN` marker, a registry key), date it, or drop it. Line
numbers, call-site inventories, step-by-step implementation plans, pasted test
output, "today" / "currently" / "not yet", and the route by which the decision
was reached all age in weeks. The shapes and what to do with each:
[DOCUMENTATION_STANDARD §4 — What to leave out](./doc/DOCUMENTATION_STANDARD.md#what-to-leave-out).

## Measurement claims

Trial numbers are the most mis-quoted content in this repository, because a
ratio survives being extracted from its page and its conditions do not. Before
repeating any figure from [doc/trials/](./doc/trials/):

- Quote the trial README's **§0 citable claim**, which states the condition
  the figure holds under. Tables under `runs/` are per-arm evidence.
- **No figure travels without the pair of arms it compares.** Several arms in
  these trials deliberately measure a *mistake* — a wrong read path, a missing
  declaration — and their numbers are not the system's cost.
- Superseded figures are deleted rather than annotated, so a number still in
  the tree is meant to be there; one you find in git history is not.

Full rules: [doc/trials § Citing a trial](./doc/trials/README.md#citing-a-trial).

## Privacy — this repo is public

Don't leak working context into committed files: no private or sibling repo
names (beyond this one), no personal filesystem paths (`/home/...`), no session
or data-volume counts, no individuals' personal details. Use generic
placeholders, and grep your diff for these before committing.

## Provenance / legacy markers

Authorship and AI-assistance provenance are tracked via **git trailers**, not
in-file build tags. The former `llm_generated` build-tag governance was retired
(ADR-0083) — do **not** reintroduce `//go:build llm_generated` (or similar)
markers on generated or AI-assisted files.

## Screenshots

Reach for the most specific built-in capture path before a generic one — each
step below only earns its keep once the step above can't reach the state you
need:

1. **Single widget, isolated.** The demo registry's `TestDriver`
   ([ADR-0057](./doc/adr/0057-demo-registry-and-drivers.md)) — set
   `IMZERO2_SCREENSHOT_DIR` (plus `IMZERO2_SCREENSHOT_SIZE`,
   `IMZERO2_SCREENSHOT_DETERMINISTIC`; see [doc/env-vars.md](./doc/env-vars.md))
   and run `hmi.sh`. Captures one PNG + one SVG per registered `Demo`.
2. **One app, a real scenario.** An app's own scripted-capture env vars,
   declared per [ADR-0009](./doc/adr/0009-environment-variable-registry.md) —
   e.g. play's `BOXER_PLAY_SCREENSHOT` / `BOXER_PLAY_SHOT_SETTLE` /
   `BOXER_PLAY_EXIT_ON_SHOT` / `BOXER_PLAY_FOCUS_*`
   (`apps/play/play_renderer.go`), which also race a PNG capture against an
   SVG export. `play` is the only app with this today — a new app that needs
   scripted screenshots should follow its pattern rather than skip to (3) or
   (4).
3. **Interactive / exploratory.** [`egui-mcp`](./doc/howto/egui-mcp.md) —
   `EGUI_INSPECTION=1` attaches an agent to the live widget tree to click,
   type, and `screenshot` mid-session. Use it to drive the UI into a state,
   not to capture one you already know how to reach directly.
4. **OS-level screenshot.** Last resort, for when 1–3 genuinely can't reach
   the target state (e.g. a transient dialog outside imzero2's control).
   This is the generic method the other three exist to avoid — if you land
   here, say so and note why.

## Subsystem notes (when you touch them)

- **leeway** — the data-mapping engine, a six-stage pipeline:
  describe → IR → map → DDL → marshal → query. Get oriented from the leeway ADRs
  (e.g. ADR-0066) before changing a stage; a change in one stage usually has a
  downstream pass that must move with it. **Reading a leeway table from SQL is
  a solved problem** — start at
  [doc/explanation/leeway-sql-read-surface.md](./doc/explanation/leeway-sql-read-surface.md)
  rather than hand-writing array arithmetic, which a measured trial found
  costs up to 3× and can silently truncate.
- **egui2 / imzero2** — the UI layer. The IDL is the source of truth: edit it
  under `definition/` and regenerate with `app egui2gen generate`. Do **not**
  hand-edit generated dispatch code (`interpreter.rs` is hybrid — only the marked
  region regenerates). Multi-child Go widgets must scope their id stack
  (`c.IdScope(...)`); a mismatched id stack compiles and vets clean but panics at
  render.
- **nanopass / dsl** — the SQL pipeline. Fix downstream passes for the canonical
  (function-call) form; if a shape isn't canonicalised, fix the canonicalize
  pass, not the consumer.

---

*Maintainers: keep this file short. New rules of general applicability belong in
CODINGSTANDARDS.md or ENGINEERING_PRACTICES.md — link them here, don't inline
them.*
