---
type: reference
audience: IDS app authors and contributors interpreting designreview findings
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Rubric set and calibration thresholds may change before M4 of [ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md); pin a commit if you build tooling against it.

# IDS policy: Tier 2 LLM review

This is the rubric catalogue for the **Tier 2 — Recorded data with LLM review** policy layer of the ImZero2 Design System ([ADR-0029 §SD9](../../adr/0029-imzero2-design-system-and-policy-as-code.md)). Rubrics are evaluated by `public/keelson/designsystem/review/`, an offline CI driver invoked via `boxer.sh designsystem review …` — it walks screenshot-tour outputs and grades each PNG against perception-tier rules an AST walker cannot enforce — clutter, color-encoding semantics, density feel, legend completeness, animation rhythm.

**Audience:** contributors interpreting `designreview` findings, IDS app authors fixing rubric violations, and reviewers proposing new rubrics. **Status:** draft through M4 of [ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md); rubrics ship as *advisory* during calibration and graduate to error gates as their false-positive rate is bounded.

## How to read this catalogue

Each rubric entry has:

- **ID** — `V1`, `V2`, … — stable identifier; used in cache keys, JSON output, and config.
- **Version** — `V1.0`, `V1.1`, … — bumped when the prompt or criteria change; cache invalidated on bump.
- **Status** — `active` (running in CI), `proposed` (designed; not yet in the rubric set), `deferred` (out of scope for v1).
- **Inputs** — single PNG / cross-app PNG pair / PNG sequence.
- **Question(s)** — what the rubric asks the model.
- **Verdict semantics** — what `pass` / `warn` / `fail` mean for this rubric.
- **Promotion criteria** — what observed FP rate or stability threshold graduates the rubric from advisory to error gate.
- **Related ADRs / pattern docs** — where the underlying rule is documented.

## How the driver works

```
                ┌──────────────────────────────────┐
   git push ──> │  boxer.sh designsystem           │
                │   review …                       │
  manifest      │  1. manifest filter          │
  filter ─────> │     (touched paths → enable) │
                │  2. for each rubric:         │
  rubric set    │     for each input set:      │
  config ─────> │       cache lookup           │
                │       miss → LLM call        │
  token         │       record finding         │
  reference ──> │  3. write JSON + markdown    │
                │  4. cost-cap enforcement     │
                └──────────────┬───────────────┘
                               │
                               ▼
              doc/design-system/reviews/<run-id>/
                  ├── findings.json
                  └── report.md
```

**Manifest filter** — Tier 2 runs only when "design-relevant" files change. The filter (declared in `public/keelson/designsystem/review/manifest.go`) matches:

- Token module: `rust/imzero2/imzero2_egui/src/style/**`, `public/keelson/designsystem/styletokens/**`
- Pattern / policy docs: `doc/design-system/**`
- Screenshot-tour pipeline: `scripts/ci/screenshot-tour/**`
- Captured screenshots: any `tour-outputs/**` PNG present in the working tree
- App UI code: heuristic match on `*/apps/**/*.{go,rs}`

Pure backend changes (runtime, persist, factsstore, …) do *not* trigger Tier 2; the cache covers them implicitly because the screenshot inputs are unchanged.

**LLM invocation** — one call per (rubric, input-set, screenshot blake3). The driver:

1. Builds the prompt from §The rubric prompt template substituting the rubric-specific question and criteria.
2. Attaches the screenshot(s) as image attachments and the current token reference (semantic palette hex values, type scale points, density table) as a structured JSON sidecar.
3. Calls the model with `max_tokens` sized to expected output (~512 for verdict + rationale).
4. Parses the JSON response, validates the schema, records the finding.

Cache key: `blake3(screenshot_bytes || rubric_id || rubric_version || model_id)`. Cached findings are returned without re-calling the model. Cache lives under `doc/design-system/reviews/.cache/` and is checked into the repo so contributors don't re-pay for identical inputs.

**SSIM pre-filter** — between the manifest filter and the per-rubric LLM call, the driver runs a deterministic SSIM pass over the captured tour PNGs paired with a caller-supplied baseline set (the `BASELINE_DIR` argument below). Pairs with `SSIM ≥ ImperceptibleThreshold` (0.99) are functionally identical to the baseline and bypass LLM grading entirely (the cache hit rate would catch them anyway, but the SSIM gate avoids touching the LLM provider for trivially-unchanged scenes). Pairs below the threshold proceed to rubric evaluation. The pre-filter is exposed standalone as `boxer.sh designsystem review tour BASELINE_DIR CANDIDATE_DIR [--threshold=0.99] [--gate-below=N] [--quiet]` for local development and CI dry-runs — SSIM is never itself a build gate per [ADR-0029 §SD9](../../adr/0029-imzero2-design-system-and-policy-as-code.md), but the optional `--gate-below=N` (default off) opts into catastrophic-regression detection for the rare case where a low SSIM is unambiguously a bug rather than an LLM-worthy perceptual change. The library lives at `public/keelson/designsystem/review/tour/`; the deterministic pre-filter primitive at `public/keelson/designsystem/review/ssim/`.

**Cost discipline** — per-run hard cap (default `$0.50`); enforced by accumulating each LLM call's reported usage cost and aborting the run with `runtime.kind.designreviewBudgetExceeded` if exceeded. The cap is configurable via `designreview.yaml.budget_usd`.

**Output formats** — see §Output schema below.

## The rubric prompt template

All rubrics share a single prompt skeleton; only the per-rubric question and criteria block change. Sketch:

```
You are grading a screenshot against the ImZero2 Design System (IDS) rubric <ID>.

IDS token reference (current):
  <JSON sidecar with palette hex, type scale, density values>

Rubric question:
  <rubric-specific question>

Grading criteria:
  <bulleted criteria>

Output a JSON object with this shape:
{
  "verdict": "pass" | "warn" | "fail" | "skip",
  "rationale": "<1-3 sentence explanation>",
  "evidence": [{"location": "...", "issue": "..."}]   // optional, populated on warn / fail
}

Do not invent rules. Grade only against the criteria above and the IDS token reference.
Skip with "verdict": "skip" if the screenshot does not contain content relevant to this rubric.
```

The "do not invent rules" line is load-bearing — without it, multimodal models drift into general-purpose taste-grading ("this could use more whitespace") that contradicts IDS's deliberate restraint.

The token-reference sidecar prevents the model from grading colors against its own preferences (a sand-coloured background looks "warm" to a model trained on bright UI screenshots; IDS's dark-only palette needs the reference to interpret correctly).

## Rubric catalogue

### V1 — Clutter / hierarchy

**Version:** `V1.0`. **Status:** `active` at M4.

**Inputs:** single screenshot.

**Question:** Is the focal point obvious? Are secondary controls visually subordinated?

**Grading criteria.**

- Pass: exactly one element draws first attention (typography hierarchy, color, or position); secondary controls are visibly less prominent.
- Warn: focal point exists but competes with one or two secondary elements at similar visual weight.
- Fail: no clear focal point; multiple elements compete equally; or focal point is obscured by surrounding chrome.

**Verdict semantics.** Advisory during M4 calibration. Promotion criterion: ≤ 10% false-positive rate over 50 pilot screenshots before graduating to error.

**Related.** [ADR-0029 §SD9](../../adr/0029-imzero2-design-system-and-policy-as-code.md) named V1 as one of the three "most automatable" rubrics for the initial M4 pass.

### V2 — Color-encoding consistency

**Version:** `V1.0`. **Status:** `active` at M4.

**Inputs:** two screenshots from different apps (or two panels from one app) where the same data class appears.

**Question:** Do the chart colors carry the same meaning across these screenshots?

**Grading criteria.**

- Pass: same logical data (e.g., `cpu.user` series, `error` status) renders in the same color across both inputs.
- Warn: colors differ by ≤ one cycle position in `batlowS` or one semantic-role shift.
- Fail: same data renders with materially different colors; or qualitative palette used for a continuous magnitude; or sequential palette used for categorical series.

**Verdict semantics.** Advisory; the rubric also reads the IDS token reference and the qualitative-cycle index registry to ground "same logical data" comparisons.

**Related.** [ADR-0031 §SD6](../../adr/0031-imzero2-design-system-color.md) "color is never the sole encoding channel"; [patterns/plots.md](../patterns/plots.md) §color-encoding-details; [patterns/status-and-legends.md](../patterns/status-and-legends.md) §mandatory-triple.

### V3 — Empty-state quality

**Version:** `V1.0`. **Status:** `active` at M4.

**Inputs:** single screenshot of a panel that should be in an empty state.

**Question:** Is the empty state informative (icon + headline + body or CTA), or just blank?

**Grading criteria.**

- Pass: icon + headline visible; specific reason or CTA present; matches the variant from [patterns/empty-states.md](../patterns/empty-states.md) (No-data-yet / No-results / Loading / Error / etc.).
- Warn: empty state exists but lacks one element (e.g., icon present, headline missing) or uses generic "No data" when a specific reason is available.
- Fail: blank panel with no message; or wrong variant (e.g., Error state shown for what is actually Loading).

**Verdict semantics.** Advisory; high-confidence rubric (the variant taxonomy is well-defined). Likely first rubric to graduate to error.

**Related.** [patterns/empty-states.md](../patterns/empty-states.md) — the seven-variant taxonomy this rubric grades against.

### V4 — Density consistency within a screen

**Version:** `V1.0`. **Status:** `active` at M4.

**Inputs:** single screenshot containing multiple panels.

**Question:** Do adjacent panels share a density preset?

**Grading criteria.**

- Pass: all panels in the screenshot read as one density (Tight / Standard / Roomy); padding, type sizes, gap widths are consistent.
- Warn: one panel visibly diverges (e.g., a help dialog rendered Roomy inside a Tight app — known Tier 3 exemption).
- Fail: multiple panels at different densities without exemption annotation.

**Verdict semantics.** Advisory; rubric correlates with the `// designlint:density-exempt` annotation when present.

**Related.** [ADR-0032 §SD1](../../adr/0032-imzero2-design-system-spacing-density-motion.md); [patterns/tables.md](../patterns/tables.md) §density-behavior.

### V5 — Legend completeness

**Version:** `V1.0`. **Status:** `active` at M4.

**Inputs:** plot screenshot (single, or panel containing plots).

**Question:** Are series, axes, and units all labelled? Is the unit and time-range visible?

**Grading criteria.** Per [patterns/status-and-legends.md](../patterns/status-and-legends.md) §legends-completeness checklist:

- Every visible plot series has a legend entry.
- Each legend entry has a swatch + text label.
- Continuous legends have endpoint value labels and a unit.
- Diverging continuous legends have an explicit midpoint label.
- Time-series plots have a time-range label (start / end or relative).
- Axis units are visible (label, legend, or panel header).

Pass: all six conditions hold. Warn: one missing. Fail: two or more missing.

**Verdict semantics.** Advisory; second-highest-confidence rubric (the checklist is mechanical-looking but resists strict AST detection because legends are layout-positional).

**Related.** [patterns/plots.md](../patterns/plots.md), [patterns/status-and-legends.md](../patterns/status-and-legends.md), [patterns/time-range-picker.md](../patterns/time-range-picker.md).

### V6 — Iconography consistency

**Version:** `V1.0`. **Status:** `active` at M4.

**Inputs:** cross-app sample — 2–4 screenshots from different apps where icons appear.

**Question:** Does each icon mean the same thing in every app where it appears?

**Grading criteria.**

- Pass: an icon glyph in app A signals the same meaning as in app B (e.g., `fa-check-circle` always means "success / valid / healthy").
- Warn: one inconsistency on a low-stakes glyph (e.g., decorative use).
- Fail: same glyph carries different meanings across apps; or different glyphs used for the same meaning (e.g., one app uses `fa-check-circle` for success, another uses `fa-thumbs-up`).

**Verdict semantics.** Advisory; rubric reads the iconography catalogue from [patterns/iconography.md](../patterns/iconography.md) as a reference for canonical meaning↔glyph mapping.

**Related.** [patterns/iconography.md](../patterns/iconography.md) — the fleet-wide catalogue this rubric grades against.

### V7 — Typographic rhythm

**Version:** `V1.0`. **Status:** `active` at M4.

**Inputs:** single screenshot.

**Question:** Are heading-to-body and body-to-caption spacings consistent? Does the typography use the IDS type scale, or does it drift?

**Grading criteria.**

- Pass: heading / body / caption sizes match the active density's [type scale](../../adr/0030-imzero2-design-system-typography.md) §SD3; vertical spacing rhythm is uniform within a panel.
- Warn: one-off size deviation (rare but allowed for specific affordances).
- Fail: multiple sizes outside the scale; or spacing rhythm is broken (uneven leading between paragraphs of similar weight).

**Verdict semantics.** Advisory; rubric reads the type-scale token reference and verifies rendered cap heights against the active density.

**Related.** [ADR-0030 §SD3](../../adr/0030-imzero2-design-system-typography.md); [ADR-0032 §SD2](../../adr/0032-imzero2-design-system-spacing-density-motion.md) magnitude-ladder grid rule.

### V8 — Animation feel

**Version:** `V1.0`. **Status:** `active` at M4.

**Inputs:** PNG sequence (typically 4–8 frames from a tour transition).

**Question:** Do transitions complete within the tour budget? Are they smooth or jittery?

**Grading criteria.**

- Pass: animation completes within 4 frames *or* is pre-configured to a final state (per [`feedback_collapsingheader_tour`] and [ADR-0032 §SD5](../../adr/0032-imzero2-design-system-spacing-density-motion.md)). No mid-frame artefacts visible.
- Warn: animation completes within 6 frames or shows mild jitter.
- Fail: animation incomplete across the captured sequence (final frame still mid-transition); or visible frame-skip / tear artefacts; or animation triggered in reduced-motion mode.

**Verdict semantics.** Advisory; lowest-confidence rubric of the initial set (model judgment of "smooth vs jittery" is the most subjective).

**Related.** [ADR-0032 §SD5](../../adr/0032-imzero2-design-system-spacing-density-motion.md); [`feedback_collapsingheader_tour`].

### V9 — Grid alignment (proposed)

**Version:** `V0.1`. **Status:** `proposed` (per [ADR-0032 §Status Q3](../../adr/0032-imzero2-design-system-spacing-density-motion.md)).

**Inputs:** single screenshot.

**Question:** Are all visible panel positions and divider lines multiples of 2 px? Are top-level layout positions multiples of `Padding.Default` (8 px in Standard density)?

**Grading criteria.** Pending design; expected to verify:

- Panel left/right edges align to a 2 px grid.
- Internal dividers fall on the same grid.
- Top-level panel-to-panel gaps are multiples of the active density's `Padding.Default` value.

**Verdict semantics.** Not yet calibrated; deferred to post-M4 once V1–V8 stabilise. Heuristic-heavy at the pixel level; alternative is a Tier 1 lint candidate ([tier1-mechanical.md](./tier1-mechanical.md) §adding-new-rules candidate L11).

**Related.** [ADR-0032 §SD2](../../adr/0032-imzero2-design-system-spacing-density-motion.md) grid-alignment rule; [patterns/tables.md](../patterns/tables.md) flexible-column-last invariant (Tier 1 L5-adjacent territory).

## Calibration phase

All rubrics ship as **advisory** at M4 — findings appear in the report but do not fail CI. Per-rubric promotion to error gate follows this lifecycle:

1. **Pilot run.** First 50 screenshots from M1 carousel + M2 plot-consumer apps are graded by each rubric. The maintainer reviews findings and labels each as true-positive / false-positive / skip.
2. **False-positive threshold.** A rubric is eligible for promotion when its false-positive rate ≤ 10% over the pilot run *and* the rubric version has been stable for 30 days.
3. **Promotion.** Tier 3 ADR (or Amendment to [ADR-0029 §SD9](../../adr/0029-imzero2-design-system-and-policy-as-code.md)) bumps the rubric from `advisory` to `error`. Config knob in `designreview.yaml` reflects the change.
4. **Demotion.** A rubric that produces a flood of false positives after a token bump or pattern doc change demotes back to advisory and the rubric version increments.

V3 (empty-state quality) and V5 (legend completeness) are the likely first promotions because their criteria are well-defined and the model produces high-agreement verdicts.

## Model selection

The model choice is configurable per rubric. Defaults at M4:

| Rubric | Default model | Rationale |
|---|---|---|
| V1 (clutter) | `claude-haiku-4-5-20251001` | High-throughput; single-image judgment is in haiku's sweet spot |
| V2 (color encoding) | `claude-sonnet-4-6` | Multi-image cross-app comparison benefits from sonnet's stronger vision reasoning |
| V3 (empty-state) | `claude-haiku-4-5-20251001` | Well-defined criteria; cheap and accurate at haiku tier |
| V4 (density) | `claude-haiku-4-5-20251001` | Single-image perceptual judgment |
| V5 (legend) | `claude-sonnet-4-6` | Checklist-driven; sonnet for reliability on the six-item checklist |
| V6 (icons) | `claude-sonnet-4-6` | Cross-app comparison + catalogue lookup |
| V7 (typography) | `claude-sonnet-4-6` | Pixel-precise grading benefits from sonnet vision |
| V8 (animation) | `claude-opus-4-7` | Multi-frame sequence; opus for temporal reasoning |

Cost-per-call (approximate 2026 pricing):

- `claude-haiku-4-5-20251001`: ~$0.001 per image + ~$0.002 per output token block.
- `claude-sonnet-4-6`: ~$0.003 per image + similar output cost.
- `claude-opus-4-7`: ~$0.015 per image + ~$0.020 per output block.

A typical M4 calibration run touching 20 screenshots and all 8 rubrics costs ~$0.30–$0.50, fitting within the default budget. Per-app PR runs (manifest-filtered to ~5–10 screenshots) cost ~$0.05–$0.15.

**Local-model path.** LM Studio with vision-capable models (Pixtral, Qwen2-VL families) is configured opt-in via `designreview.yaml.provider: lmstudio`. Caveat: [`feedback_lmstudio_qwen3_reasoning`] memory note flags Qwen 3.x reasoning issues; prefer Pixtral or Qwen2-VL for v1. Local-model calls are free but slower; the manifest filter and cache rationale still applies.

**`adversarial-review` skill** is the precedent implementation in this repo's harness — Gemini API by default with LM Studio opt-in. The `designreview` driver inherits its provider-abstraction shape; the model-default differs (Anthropic / claude-haiku-4-5 instead of Gemini).

## Output schema

### `findings.json`

```json
{
  "run_id": "2026-05-14T15:00:00Z-abc123",
  "git_sha": "0123abcd...",
  "manifest_triggers": ["doc/design-system/patterns/plots.md", "..."],
  "total_cost_usd": 0.17,
  "budget_usd": 0.50,
  "model_calls": 12,
  "cache_hits": 28,
  "findings": [
    {
      "rubric_id": "V1",
      "rubric_version": "V1.0",
      "model": "claude-haiku-4-5-20251001",
      "screenshots": ["carousel/main.png"],
      "screenshot_blake3": "blake3:...",
      "verdict": "pass",
      "rationale": "Focal point is clear; secondary controls subordinated.",
      "evidence": [{"location": "top-right toolbar", "issue": "..."}],
      "cached": false,
      "cost_usd": 0.002
    }
  ]
}
```

### `report.md`

Human-readable summary; one section per rubric, then a roll-up across rubrics:

```markdown
# IDS designreview report — 2026-05-14T15:00:00Z

## Summary
- Total findings: 12 (8 pass, 3 warn, 1 fail)
- Total cost: $0.17 / $0.50 budget
- Cache hits: 28 / 40 (70%)

## V3 — Empty-state quality
### carousel/no-data.png — FAIL
Blank plot panel with no headline or icon. Variant taxonomy from
patterns/empty-states.md suggests "No-data-for-context" — add icon,
headline naming the active range, and an "Expand range" CTA.

## V5 — Legend completeness
...
```

## Adding new rubrics

Adding a Tier 2 rubric is a Tier 3 ([ADR-0029 §SD10](../../adr/0029-imzero2-design-system-and-policy-as-code.md)) decision — captured as an Amendment to [ADR-0029 §SD9](../../adr/0029-imzero2-design-system-and-policy-as-code.md) *and* a corresponding entry in this catalogue.

The criteria for a new Tier 2 rubric:

- The pattern is *perceptual* — it cannot be reliably caught by AST analysis. (If AST-catchable, prefer Tier 1.)
- The rubric question is well-defined and the criteria are concrete enough that a multimodal model can grade reproducibly.
- A reference doc (pattern or foundations) defines the canonical rule; the rubric grades against that doc, not against the model's own taste.
- The expected verdict distribution is bounded — a rubric that almost always passes wastes calls; one that almost always fails is mis-calibrated.
- The rubric starts at `proposed`, moves to `active`-`advisory` at M4-style pilot, and graduates to error per §Calibration phase.

Candidate rubrics under consideration:

- **V10 — Status-state freshness.** Detect "live" indicators in screenshots where the data is actually stale ([patterns/status-and-legends.md](../patterns/status-and-legends.md) data-freshness mapping).
- **V11 — Text contrast against background.** Programmatic WCAG contrast check on rendered text against actual pixel background — pure-math rule, may belong in [Tier 1 mechanical](./tier1-mechanical.md) instead.
- **V12 — Cross-app series-color registry.** Detect same logical series (`cpu.user`) rendered in different qualitative-cycle slots across apps.
- **V13 — Refresh-cadence vs render budget.** Detect refresh intervals shorter than observed panel render time (per [patterns/time-range-picker.md](../patterns/time-range-picker.md) anti-pattern).

## Further reading

- [ADR-0029 — design system + policy-as-code](../../adr/0029-imzero2-design-system-and-policy-as-code.md) — parent framework; §SD9 enumerates the initial rubric set; §SD13 hard performance invariant (no runtime presence).
- [tier1-mechanical.md](./tier1-mechanical.md) — companion catalogue for Tier 1 AST lints; Tier 2 picks up where AST detection fails.
- `tier3-human-review.md` *(forthcoming)* — process for Tier 3 ADR additions; how rubric promotions / demotions / new rubrics enter the system.
- [patterns/](../patterns/) — the six pattern docs that define the criteria each rubric grades against.
- `public/keelson/designsystem/review/` — the driver implementation (M4 deliverable); cli entry at `boxer.sh designsystem`.
- `adversarial-review` skill (this repo's harness) — provider-abstraction precedent for the `designreview` driver.
- [`feedback_lmstudio_qwen3_reasoning`] — local-model caveat for Qwen 3.x reasoning.
- [`reference_screenshot_tour`] — `IMZERO2_SCREENSHOT_DIR` artefact pipeline; the input substrate for Tier 2.
