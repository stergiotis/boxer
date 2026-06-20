---
type: reference
audience: contributor
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** A first cut; verify against the linked
> sources before relying on any single line.

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
| Write Go in the house style | [CODINGSTANDARDS.md](./CODINGSTANDARDS.md) |
| Understand the toolchain (CI, lint, governance, supply-chain) | [doc/ENGINEERING_PRACTICES.md](./doc/ENGINEERING_PRACTICES.md) |
| Write or edit a doc / ADR | [doc/DOCUMENTATION_STANDARD.md](./doc/DOCUMENTATION_STANDARD.md) |
| See *why* the architecture is the way it is | [doc/adr/](./doc/adr/) |
| Configure behaviour via env vars | [doc/env-vars.md](./doc/env-vars.md) |
| Run a task end to end | [doc/howto/](./doc/howto/) |
| Report a vulnerability | [SECURITY.md](./SECURITY.md) |

## Build & test: read this first

**Always pass the repo's build tags.** Every `go build` / `test` / `vet` / `run`
must carry them, or packages fail to compile with misleading "undefined" errors:

```sh
go test  -tags="$(cat ./tags)" ./...
go build -tags="$(cat ./tags)" ./...
```

The active tags live in [`./tags`](./tags). Editors and LSP need them too —
export `GOFLAGS=-tags=<contents of ./tags>` so gopls resolves symbols. Details:
[ENGINEERING_PRACTICES §3 — Build-tag discipline](./doc/ENGINEERING_PRACTICES.md#3-build-tag-discipline).

**Check module drift with `go mod tidy --diff`**, not `tidy` followed by
`git diff` — the `--diff` form reports drift without mutating `go.mod` / `go.sum`.

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
- **Accepted** ADRs change only via dated `## Update` sections, never silent
  rewrites.
- A new decision that supersedes an old one gets its **own** ADR that references
  the superseded one.

## Writing style for committed prose

Repo docs are **descriptive and humble**. No taglines, manifestos, self-praise,
or quality claims ("robust", "comprehensive", "production-grade"). Lead with the
caveat; prefer retracting an overstatement to hedging it. Match the surrounding
document's tone.

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

## Subsystem notes (when you touch them)

- **leeway** — the data-mapping engine, a six-stage pipeline:
  describe → IR → map → DDL → marshal → query. Get oriented from the leeway ADRs
  (e.g. ADR-0066) before changing a stage; a change in one stage usually has a
  downstream pass that must move with it.
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
