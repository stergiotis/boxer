---
type: reference
audience: IDS contributors proposing Tier 3 changes; reviewers gating them
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Procedure and pending-decisions list will evolve as Tier 3 ADRs land; pin a commit if you build tooling against the running list.

# IDS policy: Tier 3 human review

This is the process and backlog for the **Tier 3 — Human review** policy layer of the ImZero2 Design System ([ADR-0029 §SD10](../../adr/0029-imzero2-design-system-and-policy-as-code.md)). Tier 3 is reserved for cases that *cannot* be checked mechanically ([Tier 1](./tier1-mechanical.md)) and *exceed* what a rubric-graded LLM can usefully advise on ([Tier 2](./tier2-llm-review.md)).

Each Tier 3 case is captured as a follow-on ADR using boxer's canonical template (`doc/templates/adr/0000-template.md`). There is no design-system-specific ADR schema; the existing template's `Subsidiary design decisions` section is sufficient for token batches, pattern additions, rule promotions, and exemptions.

**Audience:** contributors who realise their change touches one of the Tier 3 case classes; reviewers gating Tier 3 ADRs. **Status:** draft — both the case-class taxonomy and the pending-decisions list grow as the design system evolves.

## How to read this catalogue

The doc has three operational sections:

- **§Case classes** — what counts as a Tier 3 decision. If your change matches one of these, you need an ADR.
- **§Process flow** — how to go from "I realise this is Tier 3" to "ADR accepted, implementation begun."
- **§Pending decisions** — the running backlog drawn from the open-questions sections of the parent framework ADRs. Each row is a future Tier 3 ADR waiting on a trigger (M0 work, a real case emerging, an external event).

A fourth section, **§Index of accepted Tier 3 ADRs**, is empty for now — the framework ADRs ([0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md) – [0032](../../adr/0032-imzero2-design-system-spacing-density-motion.md)) are themselves the system that *defines* Tier 3, not outputs *of* Tier 3. Accepted Tier 3 ADRs (0033+ in the IDS-touching range) will land in the index as they land in `doc/adr/`.

## Case classes that require Tier 3 review

Each row names a class of change that requires a Tier 3 ADR. The "Trigger" column names what makes someone realise an ADR is needed; the "Required ADR sections" column names which SDs (Subsidiary Decisions) the ADR template should populate beyond the standard Context / QOC / Decision / Alternatives / Consequences / Status / References.

| Case class | Trigger | Required ADR sections |
|---|---|---|
| **Token additions** (new color slot, new spacing rung, new type-scale step, new motion duration) | A new ADR proposes a token outside the existing scale | IP boundary check (per [ADR-0029 §SD12](../../adr/0029-imzero2-design-system-and-policy-as-code.md)); generator output diff; backfill plan |
| **Token removals / renames** | A token is found to be unused or misnamed | Migration plan; deprecation cycle; affected app inventory |
| **Tier 1 rule additions** | A pattern keeps coming up in PR review and is mechanically detectable | [tier1-mechanical.md](./tier1-mechanical.md) catalogue entry; AST detector design; exemption mechanism; FP-rate target |
| **Tier 2 rubric additions** | A perceptual rule keeps surfacing and resists Tier 1 mechanical detection | [tier2-llm-review.md](./tier2-llm-review.md) catalogue entry; prompt + criteria; pilot calibration plan; model choice |
| **Tier 2 rubric promotion** (advisory → error gate) | A rubric's pilot FP rate is ≤ 10% over 50 screenshots and version stable for 30 days | Pilot data summary; promotion gate evidence; rollback criteria |
| **Tier 2 rubric demotion** (error → advisory) | A previously-promoted rubric starts producing false-positive floods after a token bump | Evidence of FP flood; root cause; version-bump plan |
| **Density-policy exemption** | An app argues for per-screen density override (e.g., help dialog wants Roomy inside a Tight app) | Use case justification; exemption annotation; expected scope |
| **Novel pattern** | A use case appears that the [six pattern docs](../patterns/) don't cover (command palette, keybinding map, global search, snapshot diff viewer, …) | New pattern doc + its content; the anti-patterns it forbids; cross-pattern interactions |
| **Cross-app convention** | A fleet-wide convention (file-tree navigation, command palette structure, keybinding map) emerges from multiple apps | Convention spec; migration plan per app; sunset for prior conventions |
| **Custom-painted widget** | A use case genuinely cannot be expressed in egui's existing widget set (per [ADR-0029 §SD1](../../adr/0029-imzero2-design-system-and-policy-as-code.md) no-widgets boundary) | Why egui composition fails; paint discipline; performance budget; maintenance plan |
| **Foundation refinement** (font family swap, palette nudge, color space change) | A trigger from the parent ADR fires (e.g., [ADR-0030 §SD10](../../adr/0030-imzero2-design-system-typography.md) Aile → Onest hinting threshold) | Trigger evidence; before/after screenshots; rollback path |
| **IP boundary deviation** | A token name or hex value matches a published design-system entry verbatim | Boundary-check log; ΔL nudge or rename plan; re-derivation evidence |
| **Local-model migration** ([Tier 2 §model-selection](./tier2-llm-review.md)) | The default Anthropic API is being moved to LM Studio (or vice versa) for cost / privacy reasons | Cost analysis; latency analysis; model-quality regression test |
| **Inspirations / attributions revision** | A typography or color-science upstream is renamed, deprecated, or replaced | Direct edit to [INSPIRATIONS.md](../INSPIRATIONS.md) suffices for non-normative entries; Tier 3 only if attribution obligations change |

The classes overlap by design — a "new pattern doc" that introduces a new color token is both a *novel pattern* and a *token addition*; the ADR covers both in one document. Don't split into two ADRs unless the work milestones diverge meaningfully.

## Process flow

1. **Recognition.** Read your change description against §Case classes. If your change matches one, Tier 3 applies.

2. **Pre-ADR discussion (optional but recommended).** Open a GitHub issue or surface in chat to validate the framing before committing to an ADR draft. This catches the case where "I thought I needed Tier 3 but actually a memory note suffices" before writing 300 lines of ADR.

3. **Draft.** Seed the ADR file from boxer's template:

    ```
    cp doc/templates/adr/0000-template.md doc/adr/<NNNN>-<slug>.md
    ```

    `<NNNN>` is the next free ADR number (`ls doc/adr/ | sort | tail -1`). `<slug>` is a kebab-case description (e.g., `0033-imzero2-design-system-palette-m0`).

4. **Populate.** Fill in the standard ADR sections (Context, Design space if QOC threshold met, Decision, Alternatives, Consequences, Status, References) plus the case-class-specific SDs from the §Case classes table.

5. **Cross-reference.** Add links from the parent framework ADR ([0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md) for meta concerns; [0030](../../adr/0030-imzero2-design-system-typography.md) / [0031](../../adr/0031-imzero2-design-system-color.md) / [0032](../../adr/0032-imzero2-design-system-spacing-density-motion.md) for foundations) to the new ADR. Update the §Pending decisions table here if your ADR resolves a row.

6. **Pass doclint.** `boxer gov doclint doc/adr/<NNNN>-<slug>.md` must report 0 errors / 0 warns.

7. **Review.** Default reviewer is **@spx**. External counsel applies in specific domains:

    - Data-protection counsel for ADRs touching PII / FADP / EDPB-track erasure architecture (precedent: [ADR-0027](../../adr/0027-pushout-forget-swiss-fadp.md)).
    - Security review for ADRs touching auth, capability subjects, or cryptographic primitives.
    - Multimodal-model vendor review for Tier 2 model-default changes (rare).

    The ADR `reviewed-by` and `reviewed-date` front-matter fields are populated when review completes.

8. **Accept.** Flip the ADR's `status:` from `proposed` to `accepted`. Update the status banner. Add an index entry to §Index of accepted Tier 3 ADRs in this file.

9. **Implement.** Follow the phasing milestones in the ADR. Each milestone is a follow-on PR (or set of PRs); they do *not* re-require Tier 3 review — the ADR is the contract, the implementation is mechanical.

10. **Amend (if needed).** Phasing milestones may discover that the ADR's assumptions don't hold (the M0 spike on [ADR-0028](../../adr/0028-chlocal-low-latency-sql-cap.md) is precedent). Add an Amendment section to the ADR documenting the discovery; the original decision is preserved; the amendment refines it.

11. **Supersede (if eventually wrong).** If an accepted ADR is later replaced (a foundation refinement, a sunset), the new ADR's status records "Supersedes ADR-NNNN" and the superseded ADR's status flips to `Superseded by ADR-<new>`. ADRs are append-only; supersession is recorded, not deleted.

## ADR template adapted for Tier 3

The standard boxer ADR template (`doc/templates/adr/0000-template.md`) is sufficient for every Tier 3 case. The case-class-specific SDs from §Case classes go into the `Subsidiary design decisions` section; the template's `Design space (QOC)` section is mandatory when ≥ 3 viable options are evaluated against ≥ 3 explicit criteria (per the template's threshold note) and optional below that.

Three conventions hold for Tier 3 ADRs specifically:

- **Status starts at `proposed`.** Flip to `accepted` only after human review per §Process flow step 7.
- **References include the parent framework ADR.** [ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md) for meta; the relevant foundations ADR for tokens / typography / color / spacing.
- **A `Status` section names open questions.** Each open question is a future Tier 3 candidate or an explicit deferral. The open-questions in [ADR-0030](../../adr/0030-imzero2-design-system-typography.md), [ADR-0031](../../adr/0031-imzero2-design-system-color.md), [ADR-0032](../../adr/0032-imzero2-design-system-spacing-density-motion.md) Status sections are the seed for the §Pending decisions table below.

## Pending decisions (running backlog)

This is the actionable list of Tier 3 decisions waiting on a trigger. Each row names a parent ADR open question, the trigger condition, and the target milestone. As triggers fire, rows convert into accepted Tier 3 ADRs and move to §Index.

| ID | Source | Topic | Trigger | Target milestone |
|---|---|---|---|---|
| T3-001 | [ADR-0030 §Status Q1](../../adr/0030-imzero2-design-system-typography.md) | Iosevka release version pin | M0 of [ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md) (token build) | M0 |
| T3-002 | [ADR-0030 §SD10](../../adr/0030-imzero2-design-system-typography.md) | Aile → Onest fallback swap | Hinting artefacts on ≥ 2 OS / DPI combinations | post-M1 |
| T3-003 | [ADR-0030 §Status Q3](../../adr/0030-imzero2-design-system-typography.md) | Display-font escape hatch | Screenshot context requiring 28+ pt | TBD |
| T3-004 | [ADR-0030 §Status Q4](../../adr/0030-imzero2-design-system-typography.md) | i18n / non-Latin scripts | Non-Latin locale lands | TBD |
| T3-005 | [ADR-0030 §Status Q5](../../adr/0030-imzero2-design-system-typography.md) | Variable-axis font migration | Token API stabilises | post-M1 |
| T3-006 | [ADR-0030 §Status Q6](../../adr/0030-imzero2-design-system-typography.md) | Build-container SHA pinning | Build drift incident | TBD |
| T3-007 | [ADR-0031 §Status Q1](../../adr/0031-imzero2-design-system-color.md) | Accent hue final pick (h=295 violet default) | M0 swatch comparison | M0 |
| T3-008 | [ADR-0031 §Status Q2](../../adr/0031-imzero2-design-system-color.md) | Sequential palette bundle (Crameri subset) | M2 plot-integration testing | M2 |
| T3-009 | [ADR-0031 §Status Q3](../../adr/0031-imzero2-design-system-color.md) | Diverging midpoint policy | M2 plot-integration testing | M2 |
| T3-010 | [ADR-0031 §Status Q4](../../adr/0031-imzero2-design-system-color.md) | OKLab implementation pinning | M0 | M0 |
| T3-011 | [ADR-0031 §Status Q5](../../adr/0031-imzero2-design-system-color.md) | Theme-change bus subjects | If light-theme support added (deferred — dark only v1) | TBD |
| T3-012 | [ADR-0031 §Status Q7](../../adr/0031-imzero2-design-system-color.md) | Plot legend swatch saturation | M2 plot-integration testing | M2 |
| T3-013 | [ADR-0031 §Status Q8](../../adr/0031-imzero2-design-system-color.md) | Snarl node-class palette sourcing | First snarl + plot mixed view | TBD |
| T3-014 | [ADR-0032 §Status Q1](../../adr/0032-imzero2-design-system-spacing-density-motion.md) | Stroke 1.5 px sub-pixel calibration | M1 testing artefact on ≥ 2 OS / DPI | M1 |
| T3-015 | [ADR-0032 §Status Q3](../../adr/0032-imzero2-design-system-spacing-density-motion.md) | V9 grid-alignment rubric | Post-M4 calibration of V1–V8 | post-M4 |
| T3-016 | [ADR-0032 §Status Q4](../../adr/0032-imzero2-design-system-spacing-density-motion.md) | Magnitude ladder extension (>32 / >48 px) | M2 backfill surfaces real need | M2 |
| T3-017 | [ADR-0032 §Status Q5](../../adr/0032-imzero2-design-system-spacing-density-motion.md) | Density per-screen override | Real case emerges | TBD |
| T3-018 | [ADR-0032 §Status Q7](../../adr/0032-imzero2-design-system-spacing-density-motion.md) | OS-detection crate selection | M3 reduced-motion plumbing | M3 |
| T3-019 | [tier1-mechanical.md §adding-new-rules](./tier1-mechanical.md) | L10 raw codepoint literals lint | Apps shipping raw `"\u{f05a}"` literals at non-trivial frequency | M2 |
| T3-020 | [tier1-mechanical.md §adding-new-rules](./tier1-mechanical.md) | L11 grid-alignment lint vs Tier 2 V9 | Decision deferred; alternative to T3-015 | post-M4 |
| T3-021 | [tier1-mechanical.md §adding-new-rules](./tier1-mechanical.md) | L12 mixed-density tables lint | Cross-file analysis design | post-M2 |
| T3-022 | [tier2-llm-review.md §adding-new-rubrics](./tier2-llm-review.md) | V10 status-state freshness rubric | Real "live" vs "stale" mismatches surface | post-M4 |
| T3-023 | [tier2-llm-review.md §adding-new-rubrics](./tier2-llm-review.md) | V11 WCAG contrast (Tier 1 vs Tier 2 placement) | Implementation prototype | post-M4 |
| T3-024 | [tier2-llm-review.md §adding-new-rubrics](./tier2-llm-review.md) | V12 cross-app series-color registry | Two apps with `cpu.user` series exist | post-M4 |
| T3-025 | [tier2-llm-review.md §adding-new-rubrics](./tier2-llm-review.md) | V13 refresh-cadence vs render-budget rubric | Time-range-picker lands in a real panel | post-M2 |

**Triage cadence.** Pending rows are reviewed quarterly. Rows with `TBD` triggers stay open indefinitely; rows tied to specific milestones move when the milestone fires. Rows that are abandoned (the trigger condition becomes irrelevant) get a one-line "abandoned: <reason>" note in this column and migrate to a separate §Abandoned subsection.

## Index of accepted Tier 3 ADRs

No accepted Tier 3 ADRs yet — the framework itself ([ADRs 0029–0032](../../adr/)) is in `proposed` state. As Tier 3 ADRs land, entries appear here with the following schema:

| ADR | Title | Case class | Status | Date | Source pending row | Notes |
|---|---|---|---|---|---|---|
| *(none yet)* | | | | | | |

**Schema notes.** *Case class* is one of §Case classes labels. *Source pending row* is the `T3-NNN` ID from §Pending decisions that the ADR resolves (or `n/a` if the ADR introduces a genuinely new line). *Notes* captures one-line context — usually a milestone reached or a deviation from the original pending-row plan.

## Promotion / demotion lifecycle reference

For completeness, the lifecycle states across all three tiers:

- **Tier 1 rules** have severities `off` / `warn` / `error`. Per-rule + per-file via [tier1-mechanical.md](./tier1-mechanical.md) annotations. The default-severity flip from `warn` to `error` (M5 of [ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md)) is itself a Tier 3 decision.
- **Tier 2 rubrics** have statuses `proposed` / `active-advisory` / `active-error` / `demoted`. Promotion is a Tier 3 decision per [tier2-llm-review.md §calibration-phase](./tier2-llm-review.md). Demotion is also Tier 3 — the ADR documents the FP-flood evidence and the version-bump plan.
- **Tier 3 ADRs** have statuses `proposed` / `accepted` / `deprecated` / `superseded`. Each transition between these is itself recorded in the ADR (Status section); the ADR is append-only.

## Adding new case classes

If a recurring change pattern doesn't fit any of the existing case classes, the natural response is to add a new row to §Case classes. This is itself a Tier 3 decision — captured as an Amendment to this file (which is non-normative — direct edits OK) plus an Amendment to [ADR-0029 §SD10](../../adr/0029-imzero2-design-system-and-policy-as-code.md) (the parent ADR that enumerates the original six classes).

Criteria for a new case class:

- The pattern recurs across ≥ 3 changes in different PRs.
- The pattern doesn't fit cleanly under an existing class.
- The pattern is materially distinct in *what gets reviewed* — same review process under a different label is not a new class.
- An ADR section can be specified for the new class (i.e., reviewers know what to look for).

## Further reading

- [ADR-0029 — design system + policy-as-code](../../adr/0029-imzero2-design-system-and-policy-as-code.md) — parent framework; §SD10 enumerates the original case classes; §SD12 IP boundary cross-cuts every Tier 3 token-batch ADR.
- [ADR-0030 — typography](../../adr/0030-imzero2-design-system-typography.md), [ADR-0031 — color](../../adr/0031-imzero2-design-system-color.md), [ADR-0032 — spacing / density / motion](../../adr/0032-imzero2-design-system-spacing-density-motion.md) — foundations ADRs whose open questions seed §Pending decisions.
- [tier1-mechanical.md](./tier1-mechanical.md) — Tier 1 lint catalogue; rule additions land here via Tier 3.
- [tier2-llm-review.md](./tier2-llm-review.md) — Tier 2 rubric catalogue; rubric additions / promotions / demotions land here via Tier 3.
- [INSPIRATIONS.md](../INSPIRATIONS.md) — non-normative attribution log; per ADR-0029 §SD11 updates do not require an ADR but should go through PR review.
- [`doc/templates/adr/0000-template.md`](../../templates/adr/0000-template.md) — boxer's canonical ADR template; copy it to seed a new ADR.
- [`public/gov/doclint`](../../../public/gov/doclint) — the `boxer gov doclint` subcommand that verifies front-matter and link integrity for all docs (including ADRs).
