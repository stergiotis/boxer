---
type: adr
status: superseded
date: 2026-05-16
superseded-by: ADR-0040
---

> **Superseded by [ADR-0040](./0040-imzero2-design-system-palette-consolidated.md)** (2026-05-17). The current authoritative palette state — including the M1 spine bump (L=0.16) and bg.surface 0.24 — lives in ADR-0040. This document remains as historical record of the M1 refinement decision (the QOC across spine L options, the rationale for choosing L=0.16, the text-softening mechanics) and the 2026-05-17 amendment that widened the panel↔surface elevation step.

# ADR-0037: IDS palette M1 refinement — spine bump and text softening for desktop dark-mode comfort

## Context

[ADR-0033 §SD3](./0033-imzero2-design-system-palette-m0.md) committed an OKLCh-anchored neutral spine that landed extremely dark in execution: `bg.panel = L=0.13` (sRGB `#060809`), `bg.faint = L=0.17`, `bg.surface = L=0.20`, plus `bg.extreme = L=0.06` for modal scrims. The M0b execution amendment to that ADR (2026-05-16) further bumped `text_primary` from L=0.90 → 0.95 and `text_secondary` from L=0.72 → 0.78 to clear APCA Lc-90 for body text on the L=0.13 panel.

Real-world UX feedback on an uncalibrated standard laptop screen (the most common deployment substrate for IDS apps) surfaced three specific complaints with the M0b configuration:

1. **The panel reads as near-black, not as a designed substrate.** Most desktop dark-mode systems (VS Code Dark+, GitHub canvas, Windows 11, Material 3 surface, macOS Sequoia chrome) sit at L≈0.16–0.27. IDS at L=0.13 is in pure-black territory (Vercel/Linear aesthetic), which photographs well but causes perceptual halation when high-luminance (L=0.95) text sits on near-zero luminance for non-trivial reading sessions, particularly for readers with astigmatism. The OLED-power argument doesn't apply — pebble2impl's deployment targets are desktop LCDs/IPS panels, not phones.

2. **The spine ladder visually compresses at the dark end.** The L=0.06 / 0.13 / 0.17 / 0.20 ladder for `extreme / panel / faint / surface` has steps of ΔL=0.07 / 0.04 / 0.03. Most LCDs cannot reliably differentiate L=0.06 from L=0.13 from L=0.17 — the bottom of the ladder visually collapses. The "alternating row" affordance (`bg.faint` vs `bg.panel` at ΔL=0.04) is at the edge of perceptibility on a 6-bit-temporal panel.

3. **Text-on-near-black perceptual contrast is too high on uncalibrated displays.** APCA-Lc, the M0b gate, assumes a calibrated display at standard gamma. Uncalibrated laptops typically show *higher* perceived contrast than APCA predicts for dark backgrounds, so APCA-passing configurations can read as harsh in practice.

A screenshot tour was run at four panel L values (0.13 / 0.16 / 0.18 / 0.20) — both single-token-bump and full-spine-bump variants — plus a soft-text variant at L=0.18 and L=0.16. Seven screenshot sets were preserved as eyeball evidence:

```text
/tmp/tour-before-stroke/    — baseline (L=0.13)
/tmp/tour-bg-0_16/          — bg_panel=0.16 only (spine intact)
/tmp/tour-bg-0_18/          — bg_panel=0.18 only (spine inverted; informative failure)
/tmp/tour-bg-spine/         — L=0.18 spine bump
/tmp/tour-bg-0_20/          — L=0.20 spine bump
/tmp/tour-bg-018-soft/      — L=0.18 spine + soft text (modest line softening attempted, APCA-blocked)
/tmp/tour-bg-016-soft/      — L=0.16 spine + soft text (chosen configuration)
```

These artefacts are reference-only; they are not checked in (size cost; regenerable from `palette.toml` at any commit).

The line-softening lever — lowering `border_default` from 0.58 to ~0.48 to address the "everything is outlined in white" feel — turned out to be **blocked across the entire 0.13–0.20 panel L range** by APCA's Lc-30 ambient-UI floor. The Lc-30 gate requires a roughly constant minimum border luminance regardless of panel L within this range; the original M0b value of 0.58 was already sitting at the gate.

## Design space (QOC)

**Question.** Within APCA gates, what neutral-spine and text-luminance configuration best balances (a) the M0b "near-black canvas as data substrate" intent against (b) real-world desktop dark-mode comfort on uncalibrated displays?

**Options.**

- **O1 — Keep M0b baseline (L=0.13 spine, text 0.95).** Status quo. Preserves the "darker than convention" identity at the cost of the UX complaints above.
- **O2 — L=0.16 spine + soft text (chosen).** `bg.panel = 0.16` / `bg.faint = 0.18` / `bg.surface = 0.20`; `text_primary = 0.93`; `text_secondary = 0.74`. Modest panel lift; spine ladder preserved; APCA-clean.
- **O3 — L=0.18 spine + soft text.** `bg.panel = 0.18` / `bg.faint = 0.20` / `bg.surface = 0.22`; same text-soft as O2. Larger panel lift; matches GitHub canvas territory; still APCA-clean.
- **O4 — L=0.20 spine + soft text.** `bg.panel = 0.20` / `bg.faint = 0.22` / `bg.surface = 0.24`. Approaches industry-mainstream dark-mode chrome (Windows 11, Atom One Dark). Erases IDS identity-distinguishing darkness.
- **O5 — Any spine L + APCA exemption on `border-default-on-panel`.** Unlocks line softening (`border_default` 0.58 → 0.48–0.50). Requires modifying the ADR-0033 M0b "APCA as primary gate" contract.

**Criteria.**

- **C1 — Panel-lift affordance restoration.** Resolves the "looks like the screen is off" complaint; alternating-row affordance perceptible; halation on body text reduced.
- **C2 — Identity preservation.** Stays on the darker-than-convention side of the industry spectrum (ADR-0031 §SD4 deliberate position); doesn't slide into "indistinguishable from VS Code / Windows".
- **C3 — APCA gate compliance.** All 12 contrast pairs clear their Lc floors without exemption (per ADR-0029 §SD8 / ADR-0033 §SD6 — APCA is the primary contrast gate).
- **C4 — Token-batch scope.** Changes confined to neutral-spine + text tokens; semantic palette and data-encoding palettes untouched; minimum-surface change.
- **C5 — Generator reproducibility.** Regenerator produces byte-deterministic `palette_generated.{rs,go}` from the new `palette.toml`; CI re-verification holds.
- **C6 — Line softening.** Resolves the "everything is outlined in white" complaint by reducing `border_default` luminance.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (M0b) | O2 (L=0.16, chosen) | O3 (L=0.18) | O4 (L=0.20) | O5 (APCA exempt) |
|----|----------|---------------------|-------------|-------------|------------------|
| C1 | −−       | +                   | ++          | ++          | varies           |
| C2 | ++       | ++                  | +           | −           | varies           |
| C3 | ++       | ++                  | ++          | ++          | −                |
| C4 | ++       | ++                  | ++          | ++          | +                |
| C5 | ++       | ++                  | ++          | ++          | ++               |
| C6 | −−       | −                   | −           | −           | ++               |

O2 dominates O1 on C1 without dropping below O1 on C2-C5. O3 wins on C1 over O2 but trades identity (C2). O4 loses C2 outright. O5 unlocks C6 but the cost (modifying the M0b APCA-as-primary-gate contract) is disproportionate to the marginal aesthetic gain at this scope. O2 is the smallest viable change that resolves the most-cited complaint (panel lift) while preserving every M0b architectural commitment.

## Decision

We will adopt **O2 — L=0.16 neutral-spine bump + text softening**, with `border_default` held at 0.58 (APCA-locked). Concretely the `palette.toml` deltas vs the ADR-0033 M0b execution result:

| Token | M0b | This ADR | Driver |
|---|---:|---:|---|
| `bg.panel` | 0.13 | **0.16** | C1 — resolves near-black halation, lifts canvas to GitHub-territory |
| `bg.faint` | 0.17 | **0.18** | Spine-ladder preservation: must stay > `bg.panel` |
| `bg.surface` | 0.20 | **0.20** | Unchanged in value; ladder still has 0.02 step over `bg.faint` |
| `bg.extreme` | 0.06 | **0.06** | Unchanged — modal scrim contexts still want near-black |
| `border_faint` | 0.26 | **0.26** | Unchanged — dividers vs `bg.faint` ΔL=0.08 still comfortable |
| `border_default` | 0.58 | **0.58** | APCA Lc-30 ambient floor — softening deferred (see §Status) |
| `text_primary` | 0.95 | **0.93** | Headroom comfort — clears APCA Lc-90 body floor on L=0.16 panel |
| `text_secondary` | 0.78 | **0.74** | Caption headroom (APCA-exempt at 11pt, comfort-driven) |
| `text_disabled` | 0.50 | **0.50** | Unchanged — APCA-exempt per ADR-0031 §SD5 |
| `text_extreme` | 0.98 | **0.98** | Unchanged — rare display-emphasis use |

The semantic palette (info / success / warning / error / neutral / accent × 3 emphasis levels) is **untouched**. Scientific palette LUTs (Crameri, viridis family) are **untouched**.

The change is implemented as edits to `src/rust/imzero2_egui/assets/colors/palette.toml` followed by regeneration via `scripts/designcolors.sh gen`. The regenerator emits new bytes for `palette_generated.{rs,go}`, `color.md`, and `ip-boundary-check.md` deterministically; CI byte-equality verification holds.

## Subsidiary design decisions

### SD1 — Spine ladder preservation rule

The neutral spine must maintain `bg.extreme < bg.panel < bg.faint < bg.surface` in OKLCh L space. The L=0.18-panel-only experiment (without bumping `bg.faint` from 0.17) produced an inverted ladder where alternating rows rendered *darker* than the panel they sit on — broke alternating-row affordance entirely.

Future panel-L adjustments must therefore be batched with `bg.faint` and `bg.surface` adjustments to preserve the ladder. Single-token bumps to spine values are rejected at intake.

### SD2 — APCA remains the primary contrast gate; line softening deferred

[ADR-0033 §SD6](./0033-imzero2-design-system-palette-m0.md) established APCA-Lc as the primary contrast gate (WCAG 2.1 is advisory). This ADR honors that contract. The discovered consequence is that `border_default` cannot drop below ~0.58 across any spine L in the 0.13–0.20 range — APCA's Lc-30 ambient-UI floor requires a roughly constant minimum border luminance.

Line softening (the "everything is outlined in white" complaint) therefore requires either:
- A separate Tier 3 ADR exempting `border-default-on-panel` from the Lc-30 gate, OR
- A re-classification of `border_default` as a non-ambient role with a different APCA threshold (Lc-25 floor, e.g.), OR
- Per-pair font_pt / font_weight tuning in `pairs.toml` analogous to the M0b body-text adjustment — but borders are not text and don't take this lever.

This ADR explicitly defers the line-softening decision. If real-world feedback under O2 still reports lines as too harsh, a follow-on Tier 3 ADR can address it independently without re-opening the spine values.

### SD3 — `text_primary` 0.95 → 0.93 mechanics

The M0b amendment bumped `text_primary` from the original ADR-0033 §SD3 target of 0.90 up to 0.95 specifically because the L=0.13 panel demanded Lc≥90 for 13pt/400 body — and 0.90 didn't clear it.

With the panel now at L=0.16, the contrast equation rebalances: 0.93 clears Lc-90 on `bg.panel`, `bg.faint`, and `bg.surface` simultaneously. (At 0.92 the gate fails on `bg.faint` by Lc=89.9 and `bg.surface` by Lc=89.3 — empirically determined by the generator's verifier.)

The choice of 0.93 (vs 0.94 or 0.95) maximises the perceptual softening while staying within the gate. APCA passes with ~0.5 Lc headroom on the lightest panel surface (`bg.surface`), enough to absorb minor token nudges in future without re-failing.

### SD4 — `text_secondary` 0.78 → 0.74 mechanics

`text_secondary` at 0.78 was a M0b bump (from 0.72) for 11pt Caption legibility on `bg.panel = 0.13`. Caption-on-panel at 11pt regular is *explicitly APCA-exempt* per ADR-0031 §SD5 — APCA's body-text floor doesn't apply to text below 12pt regardless of color choice.

With the panel at L=0.16 the perceptual contrast budget is more comfortable; 0.74 preserves caption legibility on uncalibrated displays while reducing the "everything is high contrast" feeling. Below 0.72 caption text becomes uncomfortably dim on calibrated displays; 0.74 is the empirical sweet spot.

### SD5 — IP-boundary check rerun

ADR-0029 §SD12 / ADR-0033 §SD7 require an IP-boundary verbatim search on every token batch. The regenerator runs this automatically; results lands in `doc/design-system/foundations/ip-boundary-check.md`. Post-regeneration: **0 (hex, role) collisions** across all 8 cached published-system palettes (Spectrum, Material, Carbon, Tailwind, Radix, Fluent, Polaris, Bootstrap).

Specifically: the new `bg.panel = #0D0E10` was searched against `gray-100` / `slate-100` / `surface-base` / `bg-default` token names across the cache; no role+value match. The previous M0b collision (`#161616` vs Carbon `gray-100`) is unaffected (that hit `neutral.subtle`, not the spine).

### SD6 — Screenshot evidence preservation policy

The seven screenshot sets that informed this ADR (paths in §Context) are **not** checked in. Rationale: each set is ~10 MB of PNG; preserving all seven inflates the repo permanently for what is calibration evidence at one point in time. Re-running the tour at any spine L is a one-command operation (`scripts/dev/hmi_screenshots.sh` after editing `palette.toml` + regenerating).

If a future ADR wants to revisit the spine choice with fresh evidence, the workflow is documented in the [§Implementation playbook](#implementation-playbook) below.

### SD7 — Implementation playbook (for future spine ADRs)

```bash
# 1. Edit palette.toml — adjust spine values per SD1 (preserve ladder).
# 2. Regenerate:
bash scripts/designcolors.sh gen
# 3. Inspect verifier output for APCA failures + WCAG / CVD warnings.
# 4. Run screenshot tour:
bash scripts/dev/hmi_screenshots.sh   # or directly:
# rm -f doc/screenshots/tour/*.png
# IMZERO2_SCREENSHOT_DIR="$(pwd)/doc/screenshots/tour" timeout 60 \
#   bash src/rust/hmi.sh --launch "subject_alias = 'widgets'"
# 5. Eyeball comparisons across configurations.
# 6. Land final values + regenerated artefacts in one commit referencing the ADR.
```

The tour requires `--launch "subject_alias = 'widgets'"` to capture the widget-showcase apps (the slider-bearing pages); the default no-launch mode picks a smaller subset.

## Alternatives

- **O1 — Keep M0b baseline (L=0.13 spine).** Rejected on C1; the status quo is what this ADR is written to address.
- **O3 — L=0.18 spine.** Rejected on C2; preserves slightly less of the "darker than convention" identity. Side-by-side comparison at /tmp/tour-bg-spine/ vs /tmp/tour-bg-016-soft/ shows the affordance lift between L=0.16 and L=0.18 is at the edge of just-noticeable-difference, so the marginal C1 gain doesn't justify the C2 loss.
- **O4 — L=0.20 spine.** Rejected on C2; matches Windows 11 / Atom One Dark territory and erases IDS's identity-distinguishing darkness. The marginal C1 gain over O3 is even smaller, and `border_faint` would also need a follow-on bump (vs `bg.surface = 0.24`, the divider/surface contrast compresses to ΔL=0.02).
- **O5 — APCA exemption on border (line softening).** Rejected at intake for this ADR; the cost (modifying the M0b APCA-as-primary-gate contract) is disproportionate to scope. Worth a separate Tier 3 ADR if real-world feedback under O2 still flags lines as too harsh.
- **Bumping body font weight 400 → 500 in `pairs.toml`.** Would relax the APCA Lc-90 body gate, potentially allowing `text_primary` to drop further than 0.93. Rejected on C4 (out of scope — typography decisions live under ADR-0030, not here) and because the resulting text-weight shift is its own visible change requiring its own ADR.
- **Increasing body font size 13pt → 14pt.** Same family as the weight bump; same rejection.
- **Per-screen / per-app spine override.** Considered for cases where an app legitimately wants the original L=0.13 panel (e.g., a plot-canvas app where the data is meant to dominate). Rejected; the design system tokens are fleet-wide. Per-app deviations require a Tier 3 density-exemption-shaped ADR per [tier3-human-review.md](../design-system/policy/tier3-human-review.md) §Case classes.

## Consequences

### Positive

- **Resolves the most-cited UX complaint** ("near-black panel looks like the screen is off") without escalating to a wholesale palette overhaul.
- **APCA contract preserved.** The M0b "APCA is the primary contrast gate" decision stands; no exemptions, no per-pair downgrades.
- **Token budget unchanged.** 28 generated `Color32` constants stay at 28; only luminance values move. Spine ladder, semantic palette, scientific palettes all untouched.
- **Cheap to revert.** If real-world feedback under L=0.16 prefers L=0.13, the rollback is the inverse of this ADR's edit: 4 lines in `palette.toml` plus a regenerator run. Cost: minutes.
- **Establishes the per-knob playbook** (SD7) for future spine refinements; the 4×7 screenshot-tour evidence pattern is documented as the canonical procedure.
- **Headroom-aware text values.** The 0.93 / 0.74 text choices are calibrated to the new panel L with explicit APCA headroom (~0.5 Lc) for future minor token nudges.

### Negative

- **Drift from M0b "darker than convention" identity.** L=0.16 puts IDS in GitHub-canvas / Tailwind slate-900 territory — still on the darker side, but closer to mainstream than the M0b L=0.13 commitment. The ADR-0031 §Context "scientifically-grounded color + Swiss-restrained chroma" framing is preserved; the "underpopulated dark quadrant" framing is softened.
- **Line softening still open.** The "everything is outlined in white" complaint remains unaddressed. The deferred Tier 3 exemption ADR is the path forward if needed; this ADR doesn't pre-commit to that escalation.
- **Tour-screenshot baseline regenerates.** All committed tour screenshots (none currently committed but reference sets at `/tmp/tour-*`) will need re-capture if used as reference material in subsequent docs. The 7 sets preserved during this iteration are sufficient for retrospective reference; future tours will capture under the new spine.
- **Apps with hardcoded `bg.panel`-aware assumptions need re-eyeballing.** Apps that visually balanced against the L=0.13 panel (e.g., the imztop dashboard's gauge backgrounds, the snarl viewport tint) may want their own minor adjustments. None are expected to break; some may look slightly less harmonious until the per-app eyeball pass.

### Neutral

- **APCA verifier output gains a "headroom" line on body pairs.** Body-on-surface now passes at Lc=90.6 (vs the M0b 90.0+ epsilon); the comfort margin is visible in the regenerator's report. Useful for future tweaks; not load-bearing.
- **`text_primary` at 0.93 keeps Lc above 100 against `bg.panel = 0.16`.** No legibility concern for body text against the primary canvas; the gate that bites is on `bg.surface` (lightest in the spine).
- **`bg.extreme` at 0.06 is unchanged.** Modal scrim contexts still get near-black; the spine bump only affects the non-modal substrate.
- **The seven screenshot reference sets in `/tmp/` are deleted on next reboot.** Per SD6 this is acceptable; the tour is re-runnable. If a future ADR wants to cite them durably, a `doc/screenshots/design-iterations/` subdirectory under the design-system docs would be the natural home — but adding that is itself a follow-on decision, not in scope here.

## Status

Proposed — awaiting review by @spx.

Open questions:

1. **Line softening follow-on?** If real-world use under L=0.16 still flags border lines as too crisp on uncalibrated displays, a Tier 3 ADR exempting `border-default-on-panel` from APCA's Lc-30 gate is the unblocking path. Defer until real-world feedback under O2 lands.
2. **Per-app spine deviation case classes.** No app is currently asking for L=0.13 retention, but the imztop / snarl / regex_explorer screens haven't been re-eyeballed under the new spine yet. If any of them visibly needs the darker canvas, a per-app exemption ADR is the right vehicle.
3. **Documenting the screenshot-iteration workflow as a how-to.** The SD7 playbook is captured in this ADR; promoting it to `doc/howto/imzero2-design-system-iteration.md` would put it where contributors look. Defer until a second spine ADR uses the playbook.
4. **`bg.extreme` review.** At L=0.06 the modal scrim is essentially pure black; with the panel now at L=0.16 the scrim-vs-panel contrast jumps from ΔL=0.07 to ΔL=0.10. May read as too aggressive in practice. Re-eyeball under modal contexts and bump to ~0.08 if needed.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-05-17 — bg.surface 0.20 → 0.24 for visible panel↔surface elevation step

UX feedback on uncalibrated displays surfaced that the `bg.surface` vs `bg.panel` elevation step (ΔL=0.04, RGB delta ~5 units) was below the perceptibility floor — egui's `Window` paints its title bar with `window_fill = bg.surface` and the content area below with `panel_fill = bg.panel`, but the two read as the same surface. The title bar visually "blended into" the panel content, defeating the elevation distinction that the M0a spine ladder anticipated.

The post-fix spine ladder:

| Token | M0a | ADR-0037 M1 | This amendment | Notes |
|---|---:|---:|---:|---|
| `bg.extreme` | 0.06 | 0.06 | 0.06 | unchanged — modal scrim |
| `bg.panel` | 0.13 | **0.16** | 0.16 | unchanged here (set by ADR-0037 M1) |
| `bg.faint` | 0.17 | **0.18** | 0.18 | unchanged here — alternating-row affordance subtle by design |
| **`bg.surface`** | **0.20** | **0.20** | **0.24** | **this amendment** — `#1D2021`; ΔL to `bg.panel` widens from 0.04 → 0.08 |

The `bg.surface` value was kept at 0.20 during the M1 spine bump per §SD1's ladder-preservation rule ("bg.faint must stay > bg.panel; bg.surface must stay > bg.faint"). The rule was honored — the ladder remained valid — but the *gap-to-bg.panel* compressed from the M0a value of 0.07 down to 0.04 because `bg.panel` lifted while `bg.surface` stayed put. This amendment restores the elevation step at the same lift magnitude `bg.panel` originally gained (≈0.04).

**Verifier results.** 28 tokens regenerated. 13 APCA pairs gated cleanly — including the `body-on-surface` pair which recomputed at the new luminance (text_primary L=0.93 on bg.surface L=0.24 still clears Lc-90 with headroom). 0 WCAG warnings. 0 IP-boundary collisions across all 8 cached published-system palettes. 68 CVD advisories (unchanged from the M1 + ADR-0033 amendment baseline).

**No follow-on tokens affected.** Border tokens, text tokens, semantic-palette tokens, and the data-encoding palettes are all untouched. The selection bg_fill (the post-ADR-0037 selection fix that uses `ACCENT_SUBTLE`) is also untouched — it sits at L=0.20 (the role-internal `accent.subtle` L, not the spine `bg.surface` L); the spine bump doesn't coincidentally bump the accent fill.

> **Correction (2026-05-17b amendment, below):** the "no follow-on tokens affected" claim above was incorrect for `border_faint`. The original ADR-0037 line 162 already predicted this — *"vs `bg.surface = 0.24`, the divider/surface contrast compresses to ΔL=0.02"* — but the prediction wasn't carried into the amendment's follow-on review. The compressed `border_faint`↔`bg.surface` pair shipped at 1.05:1 WCAG ratio (effectively invisible) until UX feedback on the errorview demo surfaced it. The 2026-05-17b amendment carries the follow-on bump and adds gating pairs to prevent recurrence.

### Verifier coverage gap (still open)

This amendment fixes the *symptom* but does not close the *coverage gap*. `bg.surface` vs `bg.panel` is **not** an entry in `pairs.toml`. Adding it directly today would fail an APCA Lc-30 gate (Lc≈8 between L=0.16 and L=0.24 is still below the ambient-UI floor), but that gate is the *wrong threshold* for the relationship: APCA is designed for foreground readability *against* a background, not for elevation-step perception *between* two backgrounds.

Closing this gap properly requires either:

- **(a)** Introducing a new pair category (`surface-elevation` or similar) with a ΔL threshold rather than an APCA Lc threshold (small generator change in `cmd/designsystem colors gen`'s verifier pipeline), or
- **(b)** Adding the pair with a relaxed APCA threshold or as `advisory-only`, accepting that the failure is documented evidence rather than a blocker.

Path (a) is the more principled answer; path (b) is the lighter touch. Either is its own Tier 3 ADR. Out of scope for this amendment.

### Open question

5. **`bg.faint` re-evaluation.** With `bg.surface` now at 0.24 and `bg.faint` at 0.18, the faint→surface gap is 0.06 (perceptible) but the panel→faint gap remains 0.02 (alternating-row affordance). Real-world feedback under tables / alternating rows hasn't reported the panel→faint compression as a problem yet — but if it does, a follow-on amendment could lift `bg.faint` to ~0.20 to widen panel→faint without re-compressing faint→surface. Defer until evidence surfaces.

`status` stays `proposed` per the ADR convention; the M1 body + 2026-05-17 amendment chain is owed a single human-review pass.

### 2026-05-17b — border_faint 0.26 → 0.48; closes the divider compression predicted at line 162

UX feedback on the errorview demo (screenshot: nested `CollapsingHeader` indent vlines on the `Structured — leaf carries CBOR` section) surfaced that egui's tree-indent guide lines were *invisible* — the eye reached for the structural hint that the indent contract promises and found none. Pixel sampling confirmed the regression: the vlines stroke at `border_faint` rgb `(34, 36, 38)` (L=0.26) against the `bg.surface` rgb `(29, 32, 33)` (L=0.24) yielded **WCAG 1.05:1** — well below the 3:1 floor for non-text UI components.

The compression was foreseen. The original M1 design-space analysis rejected O4 (L=0.20 spine) on C2 grounds and noted in §Alternatives line 162: *"vs `bg.surface = 0.24`, the divider/surface contrast compresses to ΔL=0.02"*. The 2026-05-17 amendment then took `bg.surface` to 0.24 anyway (for a separate panel↔surface elevation goal); the predicted divider compression became real but wasn't picked up because the relevant pair was not in `pairs.toml`. The amendment's "No follow-on tokens affected" line (corrected above) was the moment the prediction should have triggered a follow-on `border_faint` review.

This amendment carries the follow-on token bump and adds gating pairs.

**Token bump.**

| Token | M1 | 2026-05-17 | This amendment | sRGB | Driver |
|---|---:|---:|---:|---|---|
| `bg.surface` | 0.20 | 0.24 | 0.24 | `#1D2021` | unchanged here (set by 2026-05-17 amendment) |
| **`border_faint`** | **0.26** | **0.26** | **0.48** | **`#5B5E60`** | **this amendment** — restores divider perceptibility |

ΔL to `bg.surface` widens from 0.02 → 0.24; ΔL to `bg.panel` widens from 0.10 → 0.32. `border_faint` stays clearly below `border_default = 0.58` (ΔL=0.10), preserving the faint/default ladder semantics. The role distinction at the visuals binding (`widgets.noninteractive.bg_stroke` vs `widgets.inactive.bg_stroke`) carries the rest of the discrimination.

**Why L=0.48 specifically.** Calibrated for "visible structural hint that doesn't compete with first-class borders". The L=0.48 value clears APCA Lc-15 (floating UI floor, the right gate for subtle dividers — see below) with ~2-4 Lc headroom: Lc=-17.5 against `bg.surface`, Lc=-19.2 against `bg.panel`. WCAG ratios are **2.51:1 (surface) / 2.97:1 (panel)** — both fall just below the advisory 3:1 floor for non-text UI. This is **deliberate**: the role is *subtle divider*, not *meaningful UI element*, and the WCAG 3:1 threshold targets the latter. Sitting just under that threshold reads as a soft hint while still being ~17× brighter than the broken 1.05:1 it replaces.

An earlier draft of this amendment used L=0.53 (which clears WCAG 3:1 on both surfaces, 3.10 / 3.66). UX review judged it "too contrasty" — the divider read as competing with the first-class `border_default` rather than receding into the role of background structure. L=0.48 is the perceptual sweet spot for "you notice it when you look for it; it doesn't pull your eye when you don't".

**Why "floating" not "ambient" gate.** The role is *subtle divider*, not *meaningful UI element*. The stricter `ambient` gate (APCA Lc-30, what `border_default` sits at) would require `border_faint` L > 0.58 — brighter than `border_default` itself, which collapses the faint/default ladder. `floating` (Lc-15) is the right gate for "quiet structural hint": still verifiable, still regression-blocking, faithful to the role name.

**On the two advisory WCAG warnings.** The verifier now prints two new lines: `border-faint-on-surface: 2.51:1 < WCAG AA floor 3.0:1` and `border-faint-on-panel: 2.97:1 < WCAG AA floor 3.0:1`. These are **expected and accepted** per the calibration above. ADR-0033 §SD6 makes APCA the primary gate and WCAG advisory-only specifically to allow this kind of role-specific exception — APCA's size/weight-aware floors are the right tool for non-text UI, and the floating Lc-15 gate is what governs here. The WCAG lines remain visible in the regenerator output so any *unintended* drift onto this region still surfaces, but they aren't gating.

**Pairs.toml additions.** Two new entries close the coverage gap that let this regression ship:

```toml
[[pair]]
name        = "border-faint-on-surface"
fg          = "neutral.spine.border_faint"
bg          = "neutral.spine.bg_surface"
category    = "ui"
ui_kind     = "floating"

[[pair]]
name        = "border-faint-on-panel"
fg          = "neutral.spine.border_faint"
bg          = "neutral.spine.bg_panel"
category    = "ui"
ui_kind     = "floating"
```

The pair count rises from 13 → 15. Both new pairs clear the APCA gate at the bumped L: Lc=-17.5 (surface), Lc=-19.2 (panel); WCAG ratios 2.51:1 (surface), 2.97:1 (panel) — under the advisory 3:1 floor by design (see calibration paragraph above).

**Verifier results.** 28 tokens regenerated. 15 APCA pairs gate cleanly (was 13; +2 from this amendment). 2 advisory WCAG warnings (was 0; the 2 new pairs sit just below the WCAG 3:1 advisory floor by design — see the calibration paragraph above). 0 IP-boundary collisions across all 8 cached published-system palettes. 68 CVD advisories (unchanged baseline; `border_faint` is C=0.005, the CVD pipeline ignores neutral-spine tokens).

**Verifier coverage scope.** The new pairs close the specific gap for `border_faint`. They do not generalize: a future addition or rename of a `widgets.noninteractive.bg_stroke` consumer that ends up on a different background will need its own pair. The closer-to-systemic fix (an "every stroke-bound token gets gated against every surface it could sit on" generator pass) is a Tier 3 ADR; out of scope for this amendment.

**Side-effect on `text_disabled`.** `text_disabled` sits at L=0.50; `border_faint` now at L=0.48. The ΔL=0.02 gap is below just-noticeable-difference for grey-on-grey, so the two read as nearly the same colour at a glance. This is fine — `text_disabled` paints glyph strokes (anti-aliased letterforms, low fill density) and `border_faint` paints 1px stroke geometry (sharp edges, high fill density per pixel). The role and rendering shape carry the discrimination; the OKLCh near-coincidence is structural, not a regression.

### 2026-05-17b open questions

6. **Generalize the "stroke-bound on every surface" gate.** This amendment closed the `border_faint` case but the underlying class — "tokens bound to widget-stroke roles must clear contrast against every surface they're drawn on" — is not yet a generator invariant. A small generator pass (enumerate `widgets.{noninteractive,inactive,...}.{bg,fg}_stroke` bindings × spine backgrounds, emit synthetic pair entries) would catch the next instance automatically. Tier 3 if pursued.
7. **Re-examine `bg.faint` ↔ `bg.panel` ΔL=0.02.** Same class of compression as the original `border_faint` ↔ `bg.surface` problem; flagged as open question 5 above. The errorview surface that motivated this amendment doesn't exercise alternating rows, so the issue hasn't been UX-flagged — but the same "prediction without a gate" failure mode applies. Adding an alternating-row showcase to the screenshot tour would surface it.

`status` stays `proposed` per the ADR convention; the M1 body + both 2026-05-17 amendments are owed a single human-review pass.

## References

- [ADR-0029 — design system + policy-as-code](./0029-imzero2-design-system-and-policy-as-code.md) — parent framework; §SD13 hard performance invariant (tokens apply once at startup, no render-path cost).
- [ADR-0031 — color foundations](./0031-imzero2-design-system-color.md) — parent color ADR; §SD4 dark-theme neutral spine that this ADR refines; §SD2 semantic palette which is **not** touched here.
- [ADR-0033 — palette M0](./0033-imzero2-design-system-palette-m0.md) — ADR that committed the originally-proposed spine; §SD3 OKLCh targets; §SD6 APCA-as-primary-gate contract; §SD7 IP-boundary check method; 2026-05-16 amendment that landed the M0b execution adjustments.
- [tier3-human-review.md](../design-system/policy/tier3-human-review.md) — Tier 3 process; this ADR fits the "Foundation refinement" case class (palette nudge driven by real-world feedback).
- [`scripts/designcolors.sh`](../../scripts/designcolors.sh) — regenerator entry point.
- [`scripts/dev/hmi_screenshots.sh`](../../scripts/dev/hmi_screenshots.sh) — tour capture entry point.
- [APCA / SAPC-APCA](https://git.apcacontrast.com/) — Andrew Somers' contrast model; the primary gate per ADR-0033 §SD6.
