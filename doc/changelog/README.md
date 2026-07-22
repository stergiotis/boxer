---
type: how-to
audience: contributor
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Compiling a changelog entry

This directory holds window-bounded changelog compilations, one file per
window, named by the window bounds: `YYYY-MM-DD--YYYY-MM-DD.md`. Entries are
date-range compilations, not release notes — a window may contain several tags
or none. Each entry states its own scope in the opening paragraph (e.g.
feature commits only vs. the full commit range) and carries a **Coverage and
continuation** table so the next compilation can pick up exactly where the
last one ended, with no gap and no overlap.

The `summarize_*.sh` scripts here are a separate, experimental lineage: they
drive `gov commitdigest` through an external LLM and keep resume state under
`./summaries/`. They do not produce the hand-compiled entries described below.

## Process

1. **Find the boundary.** Open the newest entry and read its *Coverage and
   continuation* table; take the full hash in the *Covered through* row. Never
   select the window by date — dates drift across rebases and time zones; the
   hash is exact.

2. **Extract the window.**

   ```sh
   git log <last-hash>..HEAD --date=short --pretty='%ad %h %s'   # everything
   git log <last-hash>..HEAD --pretty='%s' | grep -cE '^feat'    # feat count
   git log <last-hash>..HEAD --oneline | grep -E '!:'            # breaking
   ```

   Also note tags inside the window (`git tag --sort=creatordate`) and how far
   HEAD sits past the last tag.

3. **Check for churn.** Features added *and removed* within the window must
   not be listed as shipped — give them their own closing section. (Compare
   the feat list against later `refactor`/`chore` removals; `git log
   --diff-filter=D --name-only` over `doc/adr/` catches withdrawn ADRs.)

4. **Group thematically**, not chronologically: by ADR arc and subsystem.
   Cite commit hashes for every claim and link ADRs relatively
   (`../adr/NNNN-….md`), marking the ones still **proposed** — read the
   status from the ADR front-matter, don't assume. Where a prior entry
   already details an overlapping arc, link it instead of re-telling.

5. **Write the entry** with the shape of the existing ones: front-matter
   (`type: reference`, `status: draft`), the mandatory draft banner, a scope
   paragraph, the *Coverage and continuation* table (window, covered-through
   hash = HEAD at compilation, commit counts, pointer to the previous entry),
   then the thematic body. Prose style per
   [AGENTS.md](../../AGENTS.md#writing-style-for-committed-prose):
   descriptive, no self-praise, no working-context leaks.

6. **Commit by explicit path** (`git add <files>; git commit -- <files>`),
   leaving `status: draft` until a human review flips it.

## Cadence

Compile roughly every two to four weeks, or when a release tag or a completed
ADR arc makes a natural cut. Windows chain: each entry starts at the previous
entry's covered-through hash, so the series stays seamless regardless of
cadence. (The first two entries overlap — the 2026-06-24 – 2026-07-22 entry
was compiled as a fixed four-week look-back before this chaining rule
existed.)
