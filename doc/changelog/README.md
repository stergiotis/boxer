---
type: how-to
audience: contributor
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-30
---

# Compiling a changelog entry

This directory holds window-bounded changelog compilations, one file per
window, named by the window bounds: `YYYY-MM-DD--YYYY-MM-DD.md`. Entries are
date-range compilations, not release notes — a window may contain several tags
or none. Each entry states its own scope in the opening paragraph (e.g.
feature commits only vs. the full commit range) and carries a **Coverage and
continuation** table so the next compilation can pick up exactly where the
last one ended, with no gap and no overlap. The oldest entry
(2026-07-02 – 2026-07-16) has no such table — it predates the rule; chain from
the newest entry, not from it.

[INDEX.md](./INDEX.md) is a generated cross-entry table of contents: every
entry's thematic section headings, newest first, linked to their anchors. It
is extraction, not summarisation — the reviewed summary of a window is that
entry's *window in brief* — and it is regenerated, never edited (step 7).

The `summarize_*.sh` scripts here are a separate, experimental lineage: they
drive `gov commitdigest` through an external LLM and keep resume state under
`./summaries/`. They do not produce the hand-compiled entries described below.

## Process

1. **Find the boundary.** Open the newest entry and read its *Coverage and
   continuation* table; take the full hash in the *Covered through* row. Never
   select the window by date — dates drift across rebases and time zones; the
   hash is exact.

   **When that hash is unreachable** — `git log <hash>..HEAD` fails with
   "unknown revision" because history was rewritten after the entry was
   compiled — chain from the previous entry's *own* commit instead
   (`git log --oneline -- doc/changelog/<file>`). Its parent is at or after the
   recorded compilation point, so nothing before it can be an uncovered
   feature. Then sweep the stale entry's citations
   (`grep -oE '\`[0-9a-f]{8}\`' <file>` piped through `git cat-file -t`) and
   record the replacements in the new entry, so the old one stays usable. This
   happened at the 2026-08-07 boundary; see that entry's chain note.

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
   status from the ADR front-matter, don't assume. Those markings go stale as
   ADRs are accepted, so say in the scope paragraph when they were last
   verified; re-check them whenever the entry is revisited, and write
   "accepted since" rather than silently rewriting what was true at
   compilation. Where a prior entry already details an overlapping arc, link
   it instead of re-telling.

5. **Write the entry** with the shape of the existing ones: front-matter
   (`type: reference`, `status: draft`), the mandatory draft banner, a scope
   paragraph, a *window in brief* section, the tag and ADR-status paragraphs,
   the *Coverage and continuation* table (window, covered-through hash = HEAD
   at compilation, commit counts, pointer to the previous entry, and any chain
   note), then the thematic body. Prose style per
   [AGENTS.md](../../AGENTS.md#writing-style-for-committed-prose):
   descriptive, no self-praise, no working-context leaks.

   The **window in brief** exists because the body serves a different reader
   than the summary does. The body is a lookup surface — roughly one commit
   hash every twenty words — which is what someone auditing a single arc needs
   and what someone asking "what happened this fortnight" cannot read. So the
   brief carries no hashes and no links, states the two or three through-lines
   the window actually had, and says what the window costs a reader (breaking
   changes, whether anything shipped under a tag). Keep it under ~250 words;
   everything in it must be cited in the body. Entries before
   2026-08-07 – 2026-08-16 predate this section. Keep continuation machinery —
   chain notes, rewritten-hash mappings — down with the *Coverage and
   continuation* table rather than above the first thing that changed.

6. **Commit by explicit path** (`git add <files>; git commit -- <files>`),
   leaving `status: draft` until a human review flips it. The flip target is
   `stable`, not `accepted` — these are descriptive docs, and `accepted`
   belongs to the ADR state machine
   ([DOCUMENTATION_STANDARD §4](../DOCUMENTATION_STANDARD.md)). It requires
   `reviewed-by` + `reviewed-date` and removal of the draft banner; doclint
   checks both.

7. **Regenerate the index.** `boxer.sh gov changelogindex` rewrites
   [INDEX.md](./INDEX.md) from the entries; commit it alongside the entry.
   `--check` verifies without writing, and a drift test in
   `public/gov/changelogindex` fails CI when an entry lands without a
   regeneration. A new machinery section (one that would repeat in every
   entry) belongs in that package's skip list, not in the index.

## Cadence

Compile roughly every two to four weeks, or when a release tag or a completed
ADR arc makes a natural cut. Windows chain: each entry starts at the previous
entry's covered-through hash, so the series stays seamless regardless of
cadence. (The first two entries overlap — the 2026-06-24 – 2026-07-22 entry
was compiled as a fixed four-week look-back before this chaining rule
existed.)
