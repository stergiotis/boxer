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

## References

- [How to survey ADR status and implementation degree](../howto/adr-overview.md)
- [CODINGSTANDARDS.md § ADR References](../../CODINGSTANDARDS.md#adr-references)
