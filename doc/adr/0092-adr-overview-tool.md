---
type: adr
status: accepted
date: 2026-06-20
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-20
---

# ADR-0092: ADR overview tooling — decision status × code-evidence implementation degree

## Context

The ADR corpus records each decision's *lifecycle* in front-matter `status`
(`proposed → accepted / deferred / superseded / withdrawn`). It records nothing
about a decision's *implementation degree* — whether an accepted decision is
unbuilt, partially built, shipped, or superseded-in-practice. That state lived
only in body prose and tribal knowledge, and the two axes are independent: an
`accepted` ADR can sit anywhere from untouched to shipped-then-replaced, so the
`status` column alone does not answer "what is actually built?".

As the corpus grew this gap made it hard to see which decisions still need
implementation, which were quietly built ahead of acceptance, and which packages
realise a given decision. We want that surveyable with ordinary queries rather
than by re-reading every ADR.

## Design space (QOC)

**Question.** Where does the *implementation-degree* signal come from?

**Options.**

- **O1 — Structured front-matter field.** Add an `implementation:` enum to every
  ADR's front-matter and hand-maintain it.
- **O2 — Code evidence.** Derive it from how source cites each ADR: the
  `ADR-NNNN` markers (with `§` section qualifiers) already written in comments,
  plus the Phase/Cut/Step/Milestone/SD vocabulary the ADRs define.
- **O3 — Heuristic prose scan.** Infer it from body "Update" sections and
  keyword matching (`SHIPPED`, `COMMITTED`, `deferred`, …).

**Criteria.**

- **C1 — Fidelity to actual build state.** Does the signal track reality or
  intent?
- **C2 — Maintenance friction.** Cost of keeping it current.
- **C3 — Drill-down.** Can it point at the exact files/lines that realise a
  decision?
- **C4 — No doc-schema churn.** Avoids a corpus-wide front-matter migration.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (front-matter field) | O2 (code evidence) | O3 (prose heuristic) |
|----|-------------------------|--------------------|----------------------|
| C1 | −  (records intent, drifts) | ++ (grounded in code) | −  (fuzzy) |
| C2 | −− (hand-maintained, every ADR) | ++ (rides existing markers) | +  |
| C3 | −− (ADR-level only)     | ++ (file + line + section) | −  |
| C4 | −− (migrates the corpus) | ++                 | ++ |

## Decision

We add `boxer adr` (`public/app/commands/adr`). It parses the ADR front-matter
and body markers, scans the source tree for `ADR-NNNN` citations and their `§`
qualifiers, and emits two Apache Arrow IPC tables — `adr` (one row per ADR, with
the rolled-up citation footprint) and `coderef` (one row per citation: file,
line, language, package, qualifier, snippet). `clickhouse-local` queries them;
canned reports cross the two axes and a `query` subcommand runs arbitrary SQL.
Without `clickhouse-local` the overview degrades to a plain-text board.

Implementation degree is sourced from **code evidence (O2)**. This couples the
tool to a convention: a [coding standard](../../CODINGSTANDARDS.md#adr-references)
makes an `ADR-NNNN` marker mandatory wherever code realises a decision, pinned to
the `§` section where the decision is decomposed, so the evidence stays
trustworthy and the per-section fidelity survives.

## Alternatives

- **Structured `implementation:` front-matter field (O1).** Rejected: a
  hand-maintained field records intent, not reality, drifts from the code, and
  forces a front-matter migration across the whole corpus for a signal the code
  already carries.
- **Heuristic prose scan (O3).** Rejected as the primary source: fuzzy and not
  validatable. A reduced form survives — Update-section detection and a freshness
  date feed the `adr` table as secondary signals, not the degree verdict.
- **Typed per-package reference registry (`packageprops`-style).** Considered and
  rejected: `packageprops` ([ADR-0080](./0080-packageprops-per-package-declarations.md))
  is package-granular and dense (one record per package), whereas ADR references
  are line-granular and many-to-many. A typed declaration would lose the
  file/line evidence that powers drill-down, add annotation friction that
  depresses compliance, and introduce a second source of truth. If a typed *role*
  (implements / enforces / tests vs. mentions) is wanted later, the cheaper path
  is enriching the comment grammar — the verb already lands in the
  `coderef.snippet` column — before reaching for a registry.

## Consequences

### Positive

- The decided-vs-built question is answerable with SQL: proposed-but-built ADRs
  (acceptance candidates), accepted-but-unreferenced ADRs (un-built or
  non-code), and the per-ADR implementation footprint all fall out of the
  `adr × coderef` join.
- Drill-down to the exact files, lines, and sections that realise any decision.
- No new front-matter schema; the signal rides markers the code already carries.

### Negative

- The signal is only as good as the markers, which is why the coding standard
  mandates them; an un-marked implementation reads as un-built.
- `impl_evidence` (`none` / `referenced` / `broad`) is a coarse heuristic from
  citation footprint, not a verdict — `none` legitimately includes
  documentation, convention, and removal ADRs.

### Neutral

- A broadly-adopted ADR (e.g. ADR-0080, cited by almost every package's
  `package_props.go`) dominates raw citation counts by breadth, not depth; read
  `code_pkgs` / `impl_evidence` with that in mind.
- Front-matter is parsed by a small hand-rolled flat-scalar reader rather than a
  YAML dependency, since the corpus uses only top-level scalars. Markdown is
  excluded from the citation scan so ADR-to-ADR cross-links never inflate counts.

## Status

Accepted (2026-06-20).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-07-15 — Sub-item progress as a third axis: the `subtask` table, a declared `✓`, and the `adrboard` board

The two axes above answer "was this decided?" and "was this built?" for a whole
ADR. Neither answers "how far along is it?" for an ADR that decomposed itself
into parts. This entry adds that, sources it from **declaration**, and therefore
has to be reconciled with O1's rejection above.

**What the corpus decomposes into.** A survey found 673 sub-item declarations
across 67 of the 115 ADRs (median ~10 each): 613 subsidiary design decisions
(`SD`), 51 milestones (`M`), 9 steps. Two shapes are in use and both carry
weight — a heading (`### SD3 — Subject taxonomy`) and a bold list item under a
Decision section (`- **SD1 — Provider registry.** …`, in fact the more common
form). The `plan_markers` column saw neither as a structure: it was a body-wide
regex sweep yielding a deduplicated set of marker *strings*, with no titles, no
anchors and no per-marker state — a hint that a plan exists, not the plan. It
stays for compatibility; `subtask` supersedes it.

**Why this axis is declared and O1 still stands.** O1 was rejected for the
ADR-level implementation degree, where code evidence already carries the signal
and a hand-maintained field would only restate it, worse and staler. That
argument does not reach the sub-item axis, because there the evidence is not
merely redundant — it is *absent*. 613 of 673 sub-items are subsidiary design
*decisions*, not units of work. Some are buildable and get cited; many cannot be,
in principle: an IP boundary, a performance posture, a naming rule leave no line
of code to pin a `§` marker to, and never will. Measured, `§`-qualified citations
reach 79 of 227 heading-form sub-items — and a permanent "no evidence" verdict on
a decision that was never going to have any is a false negative, not a drift
signal. Evidence cannot speak for those sub-items; the author can. So: the ADR
declares a sub-item done by writing a `✓` immediately after its declaration's
title text — the end of the heading, or just past the closing `**`.

```markdown
### SD3 — Subject taxonomy ✓

- **SD1 — Provider registry + interface.** ✓ A `TableProvider` declares…
```

The glyph is the one the corpus already reaches for when it hand-tracks progress
(the ADR-0042 codec inventory, the ADR-0012 option matrix). Done-ness is binary:
there is no in-progress state, and none is proposed. The *inventory* stays
derived — the body is scanned for declarations — so the `✓` declares only what
the survey cannot: whether a declared thing is finished. Declaration and survey
stay separable exactly as in [ADR-0080](./0080-packageprops-per-package-declarations.md)
§SD3.

**A declaration is a marker, an em-dash, and a title.** The dash is what
separates a declaration from prose that merely names a marker: `- **M1 is
unblocked.**` is a status remark, and a dated `### 2026-05-23 — M3 landed`
heading is an Update, not a declaration of M3. All 677 real declarations use an
em-dash; the en-dash is reserved for ranges, and admitting it read
`- **Phase 0–1** — …` as a declaration of "Phase 0" titled "1". The rule misses
a handful of oddballs (a dual `SD4 + SD7 —` heading, a few parenthesised
`Phase 5 (landed)` bullets); adding a dash is how an author brings one onto the
board. A marker declared twice — an original plus an Update that re-decides it —
folds into one sub-item whose done-ness is the OR, so a later Update can mark it.

**Tables.** A third Arrow table, `subtask`, joins `adr` and `coderef`: one row
per declaration (`num`, `marker`, `kind`, `ordinal`, `title`, `done`, `shape`,
`line`, `code_refs`). `code_refs` counts the citations pinning that exact marker,
so the evidence axis is now readable at the granularity an ADR decomposed itself
into, not just per ADR. `adr` gains `subtasks_total` / `subtasks_done` /
`subtasks_cited`; the last is independent of `subtasks_done`, not disjoint from
it, so the two do not subtract.

**Declared and evidenced are both shown, and are not the same claim.** The two
signals answer different questions and neither subsumes the other: measured
across the corpus, code cites 98 of 614 SDs by `§marker` and **0 of 51
milestones** — code pins decisions, not milestones — while `✓` starts at zero
everywhere. Reporting only the declaration would have read `0/674` while a sixth
of the corpus is demonstrably realized, which is no signal at all; reporting only
the evidence is the O1 conflation. So each sub-item lands in exactly one of three
buckets: **declared done** (a `✓` — the only claim of completion, and only an
author can make it), **cited but undeclared** (source names its `§marker`;
evidence, not a verdict — and precisely the worklist of sub-items worth a human
`✓`), or **neither** (unbuilt, or nothing to build). A `✓` outranks evidence: an
author's mark is never downgraded by what the code does or doesn't say.

**A board.** `apps/adrboard` renders the same model as a read-only kanban: one
card per ADR, filed in the lane of its `status`, with the three buckets as a
packed dot tally along the card's bottom edge. Sub-items are deliberately *not*
cards — lifecycle lanes would file a policy SD as un-started work forever. The
board is a view, never an editor: a card's lane is its frontmatter status, which
is a reviewed decision rather than something to drag.

**Seam.** The corpus model moved to `public/gov/adrcorpus` — a pure library over
markdown, no CLI, no Arrow, no clickhouse. `boxer adr` keeps the Arrow tables and
the query surface and imports it; the board imports it too, so a GUI app no
longer has to link a command (and `urfave/cli`, `extbin`, the chlocal pool with
it) to read an ADR.

**Three fixes found on the way.** `ScanCodeRefs` skipped its own walk root when
that root's basename began with a dot — which every relative parent path
(`..`, `../../..`) has — because the hidden-directory rule meant for descendants
was applied to the root as well. It returned zero citations and no error, so
`boxer adr --root ..` reported the entire corpus as un-built: a total, silent
false negative in the axis this ADR exists to measure. Only the default `--root .`
escaped, `.` being special-cased, which is why it went unseen. The name rules now
apply to descendants only. Relatedly, an empty `excludeDir`/`outDir` became
`filepath.Abs("")` — the working directory — and silently dropped it from the
scan; empty now means no exclusion.

The `§` qualifier regex captured `2026` out of `ADR-0026 §2026-05-12`, a dated
Update reference read as a section pin. A bare qualifier is now capped at two
digits and anchored, so no prefix of a year matches; letter-prefixed sections
(`§SD3`, `§B1`, `§Q3`) stay unbounded, the vocabulary being open. Separately, the
corpus briefly carried two different ADRs numbered 0119, authored concurrently
and since renumbered by their authors; the board keys cards on position rather
than number because that race can recur, and a board keyed on a domain id breaks
exactly while the corpus is being edited.

**Caveats.** No sub-item is marked `✓` at the time of writing: the convention is
new, so the board's progress signal is entirely the evidence bucket until the
corpus is annotated by hand. Milestones are the weak spot — they carry no `§`
pins at all, so an M-only ADR reads as all-grey regardless of what shipped; their
landed-signal exists but only as prose in dated Update headings (`M1–M4 landed`,
`M3 landed; M3a deferred`), which is fuzzy enough that it is deliberately not
parsed. A `✓` inside a heading changes that heading's anchor slug; a dozen
intra-corpus links point at sub-item anchors and would need updating if those
particular headings are marked. Neither doclint nor any other check yet
reconciles the declared set against the surveyed inventory.

## References

- [How to survey ADR status and implementation degree](../howto/adr-overview.md)
- [CODINGSTANDARDS.md § ADR References](../../CODINGSTANDARDS.md#adr-references)
