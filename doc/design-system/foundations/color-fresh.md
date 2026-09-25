---
type: reference
audience: IDS app authors and contributors checking palette tokens
status: draft
---

> **Status: draft — pre-human-review.** Generated artefact. Source: `rust/imzero2/assets/colors/palette-fresh.toml`. Re-emit via `./boxer.sh designsystem colors gen`.

# IDS color tokens — the fresh theme (generated)

Generated 2026-09-24.

## Token table

| Token | OKLCh target (L, C, h°) | Post-clip C | sRGB |
|---|---|---|---|
| `neutral.spine.bg_extreme` | (1.000, 0.012, 265.0) | 0.012 | `#fbffff` |
| `neutral.spine.bg_faint` | (0.945, 0.012, 265.0) | 0.012 | `#e9edf5` |
| `neutral.spine.bg_panel` | (0.965, 0.012, 265.0) | 0.012 | `#eff4fc` |
| `neutral.spine.bg_surface` | (0.990, 0.012, 265.0) | 0.012 | `#f8fcff` |
| `neutral.spine.border_default` | (0.620, 0.012, 265.0) | 0.012 | `#83868e` |
| `neutral.spine.border_faint` | (0.720, 0.012, 265.0) | 0.012 | `#a1a5ac` |
| `neutral.spine.text_disabled` | (0.680, 0.012, 265.0) | 0.012 | `#9598a0` |
| `neutral.spine.text_extreme` | (0.200, 0.012, 265.0) | 0.012 | `#13161c` |
| `neutral.spine.text_primary` | (0.270, 0.012, 265.0) | 0.012 | `#24262c` |
| `neutral.spine.text_secondary` | (0.500, 0.012, 265.0) | 0.012 | `#60636a` |
| `semantic.accent.subtle` | (0.950, 0.045, 315.0) | 0.045 | `#fae6ff` |
| `semantic.accent.default` | (0.600, 0.160, 315.0) | 0.160 | `#a35ec1` |
| `semantic.accent.strong` | (0.500, 0.180, 315.0) | 0.180 | `#8838a8` |
| `semantic.error.subtle` | (0.930, 0.040, 25.0) | 0.040 | `#ffdedb` |
| `semantic.error.default` | (0.580, 0.170, 25.0) | 0.170 | `#cb4644` |
| `semantic.error.strong` | (0.480, 0.170, 25.0) | 0.170 | `#a92227` |
| `semantic.info.subtle` | (0.930, 0.040, 240.0) | 0.040 | `#d1ecff` |
| `semantic.info.default` | (0.560, 0.140, 240.0) | 0.140 | `#007cbd` |
| `semantic.info.strong` | (0.460, 0.150, 240.0) | 0.133 | `#005e99` |
| `semantic.neutral.subtle` | (0.920, 0.010, 265.0) | 0.010 | `#e1e5eb` |
| `semantic.neutral.default` | (0.500, 0.010, 265.0) | 0.010 | `#606369` |
| `semantic.neutral.strong` | (0.350, 0.010, 265.0) | 0.010 | `#383b40` |
| `semantic.success.subtle` | (0.930, 0.050, 150.0) | 0.050 | `#d1f2d7` |
| `semantic.success.default` | (0.560, 0.130, 150.0) | 0.130 | `#2d8949` |
| `semantic.success.strong` | (0.460, 0.130, 150.0) | 0.130 | `#006b2d` |
| `semantic.warning.subtle` | (0.950, 0.050, 75.0) | 0.050 | `#ffeaca` |
| `semantic.warning.default` | (0.580, 0.140, 75.0) | 0.140 | `#a96b00` |
| `semantic.warning.strong` | (0.500, 0.130, 75.0) | 0.125 | `#8c5600` |

## APCA contrast pairs (primary gate)

Per the M0b refinement of [ADR-0031 §SD5](../../adr/0031-imzero2-design-system-color.md): APCA is the primary contrast metric — the SAPC S-curve algorithm Andrew Somers built for dark themes (proposed for WCAG 3.0 / Silver). Lc magnitudes; sign carries text-on-dark vs text-on-light orientation. Thresholds are size+weight-aware for text and category-driven for UI.

| Pair | Category | Spec | Lc | Threshold | Pass |
|---|---|---|---|---|---|
| `body-on-panel` | text | 13pt/400 | +95.2 | 90 | pass |
| `body-on-faint` | text | 13pt/400 | +91.2 | 90 | pass |
| `body-on-surface` | text | 13pt/400 | +99.9 | 90 | pass |
| `secondary-on-panel` | text | 11pt/500 | +73.3 | 100 | **fail** |
| `disabled-on-panel` | text | 13pt/400 | +48.3 | 90 | **fail** |
| `display-on-panel` | text | 22pt/600 | +98.0 | 70 | pass |
| `info-default-on-panel` | ui | meaningful | +64.1 | 60 | pass |
| `success-default-on-panel` | ui | meaningful | +63.2 | 60 | pass |
| `warning-default-on-panel` | ui | meaningful | +63.2 | 60 | pass |
| `error-default-on-panel` | ui | meaningful | +64.6 | 60 | pass |
| `accent-default-on-panel` | ui | meaningful | +62.3 | 60 | pass |
| `border-default-on-panel` | ui | ambient | +57.2 | 30 | pass |
| `border-faint-on-surface` | ui | floating | +46.4 | 15 | pass |
| `border-faint-on-panel` | ui | floating | +41.8 | 15 | pass |
| `body-on-selection` | text | 13pt/400 | +91.0 | 90 | pass |
| `info-default-on-surface` | ui | meaningful | +68.7 | 60 | pass |
| `success-default-on-surface` | ui | meaningful | +67.9 | 60 | pass |
| `warning-default-on-surface` | ui | meaningful | +67.9 | 60 | pass |
| `error-default-on-surface` | ui | meaningful | +69.3 | 60 | pass |
| `accent-default-on-surface` | ui | meaningful | +66.9 | 60 | pass |
| `secondary-on-surface` | text | 11pt/500 | +77.9 | 100 | **fail** |
| `extreme-on-surface` | text | 13pt/600 | +102.7 | 85 | pass |
| `mark-text-on-highlight` | text | 13pt/400 | +91.1 | 90 | pass |

## WCAG 2.1 contrast pairs (advisory)

Kept as a secondary signal — WCAG 2.1's relative-luminance math is known to misbehave on dark themes, which is why IDS upgraded to APCA. WCAG misses warn but do not gate; treat them as a cross-check and a legacy-compliance reference, not as authority.

| Pair | Kind | Ratio | AA | AAA |
|---|---|---|---|---|
| `body-on-panel` | body | 13.69:1 | pass | pass |
| `body-on-faint` | body | 12.89:1 | pass | pass |
| `body-on-surface` | body | 14.66:1 | pass | pass |
| `secondary-on-panel` | body | 5.45:1 | pass | miss |
| `disabled-on-panel` | body | 2.61:1 | fail | miss |
| `display-on-panel` | large | 16.40:1 | pass | pass |
| `info-default-on-panel` | ui | 4.12:1 | pass | n/a |
| `success-default-on-panel` | ui | 3.97:1 | pass | n/a |
| `warning-default-on-panel` | ui | 3.97:1 | pass | n/a |
| `error-default-on-panel` | ui | 4.23:1 | pass | n/a |
| `accent-default-on-panel` | ui | 3.86:1 | pass | n/a |
| `border-default-on-panel` | ui | 3.30:1 | pass | n/a |
| `border-faint-on-surface` | ui | 2.40:1 | fail | n/a |
| `border-faint-on-panel` | ui | 2.24:1 | fail | n/a |
| `body-on-selection` | body | 12.84:1 | pass | pass |
| `info-default-on-surface` | ui | 4.41:1 | pass | n/a |
| `success-default-on-surface` | ui | 4.25:1 | pass | n/a |
| `warning-default-on-surface` | ui | 4.25:1 | pass | n/a |
| `error-default-on-surface` | ui | 4.53:1 | pass | n/a |
| `accent-default-on-surface` | ui | 4.13:1 | pass | n/a |
| `secondary-on-surface` | body | 5.83:1 | pass | miss |
| `extreme-on-surface` | body | 17.56:1 | pass | pass |
| `mark-text-on-highlight` | body | 12.87:1 | pass | pass |

