---
type: reference
audience: IDS app authors and contributors checking palette tokens
status: draft
---

> **Status: draft — pre-human-review.** Generated artefact. Source: `src/rust/assets/colors/palette.toml`. Re-emit via `go run ./cmd/designsystem colors gen`.

# IDS color tokens (generated)

Generated 2026-06-11.

## Token table

| Token | OKLCh target (L, C, h°) | Post-clip C | sRGB |
|---|---|---|---|
| `neutral.spine.bg_extreme` | (0.060, 0.005, 240.0) | 0.005 | `#000101` |
| `neutral.spine.bg_faint` | (0.180, 0.005, 240.0) | 0.005 | `#101214` |
| `neutral.spine.bg_panel` | (0.160, 0.005, 240.0) | 0.005 | `#0b0e0f` |
| `neutral.spine.bg_surface` | (0.240, 0.005, 240.0) | 0.005 | `#1d2021` |
| `neutral.spine.border_default` | (0.580, 0.005, 240.0) | 0.005 | `#787b7d` |
| `neutral.spine.border_faint` | (0.480, 0.005, 240.0) | 0.005 | `#5b5e60` |
| `neutral.spine.text_disabled` | (0.500, 0.005, 240.0) | 0.005 | `#616466` |
| `neutral.spine.text_extreme` | (0.980, 0.005, 240.0) | 0.005 | `#f6f9fb` |
| `neutral.spine.text_primary` | (0.930, 0.005, 240.0) | 0.005 | `#e5e8eb` |
| `neutral.spine.text_secondary` | (0.740, 0.005, 240.0) | 0.005 | `#a8abae` |
| `semantic.accent.subtle` | (0.200, 0.030, 270.0) | 0.030 | `#111524` |
| `semantic.accent.default` | (0.800, 0.080, 270.0) | 0.080 | `#a9bcf2` |
| `semantic.accent.strong` | (0.900, 0.130, 270.0) | 0.069 | `#ccddff` |
| `semantic.error.subtle` | (0.200, 0.030, 25.0) | 0.030 | `#22100f` |
| `semantic.error.default` | (0.800, 0.120, 25.0) | 0.120 | `#ff9e96` |
| `semantic.error.strong` | (0.900, 0.130, 25.0) | 0.075 | `#ffccc5` |
| `semantic.info.subtle` | (0.200, 0.030, 240.0) | 0.030 | `#081822` |
| `semantic.info.default` | (0.800, 0.120, 240.0) | 0.120 | `#6fc8ff` |
| `semantic.info.strong` | (0.900, 0.130, 240.0) | 0.078 | `#afe6ff` |
| `semantic.neutral.subtle` | (0.220, 0.000, 240.0) | 0.000 | `#1b1b1b` |
| `semantic.neutral.default` | (0.800, 0.000, 240.0) | 0.000 | `#bebebe` |
| `semantic.neutral.strong` | (0.900, 0.000, 240.0) | 0.000 | `#dedede` |
| `semantic.success.subtle` | (0.200, 0.030, 145.0) | 0.030 | `#0d1a0d` |
| `semantic.success.default` | (0.800, 0.120, 145.0) | 0.120 | `#8bd28d` |
| `semantic.success.strong` | (0.900, 0.130, 145.0) | 0.130 | `#a6f5a8` |
| `semantic.warning.subtle` | (0.200, 0.030, 80.0) | 0.030 | `#1d1406` |
| `semantic.warning.default` | (0.800, 0.120, 80.0) | 0.120 | `#e6b55d` |
| `semantic.warning.strong` | (0.900, 0.130, 80.0) | 0.130 | `#ffd474` |

## APCA contrast pairs (primary gate)

Per the M0b refinement of [ADR-0031 §SD5](../../adr/0031-imzero2-design-system-color.md): APCA is the primary contrast metric — the SAPC S-curve algorithm Andrew Somers built for dark themes (proposed for WCAG 3.0 / Silver). Lc magnitudes; sign carries text-on-dark vs text-on-light orientation. Thresholds are size+weight-aware for text and category-driven for UI.

| Pair | Category | Spec | Lc | Threshold | Pass |
|---|---|---|---|---|---|
| `body-on-panel` | text | 13pt/400 | -92.4 | 90 | pass |
| `body-on-faint` | text | 13pt/400 | -92.1 | 90 | pass |
| `body-on-surface` | text | 13pt/400 | -90.6 | 90 | pass |
| `secondary-on-panel` | text | 11pt/500 | -56.3 | 100 | **fail** |
| `disabled-on-panel` | text | 13pt/400 | -21.7 | 90 | **fail** |
| `display-on-panel` | text | 22pt/600 | -103.3 | 70 | pass |
| `info-default-on-panel` | ui | meaningful | -67.8 | 60 | pass |
| `success-default-on-panel` | ui | meaningful | -69.2 | 60 | pass |
| `warning-default-on-panel` | ui | meaningful | -66.5 | 60 | pass |
| `error-default-on-panel` | ui | meaningful | -64.0 | 60 | pass |
| `accent-default-on-panel` | ui | meaningful | -66.6 | 60 | pass |
| `border-default-on-panel` | ui | ambient | -31.9 | 30 | pass |
| `border-faint-on-surface` | ui | floating | -17.5 | 15 | pass |
| `border-faint-on-panel` | ui | floating | -19.2 | 15 | pass |
| `body-on-selection` | text | 13pt/400 | -91.8 | 90 | pass |

## WCAG 2.1 contrast pairs (advisory)

Kept as a secondary signal — WCAG 2.1's relative-luminance math is known to misbehave on dark themes, which is why IDS upgraded to APCA. WCAG misses warn but do not gate; treat them as a cross-check and a legacy-compliance reference, not as authority.

| Pair | Kind | Ratio | AA | AAA |
|---|---|---|---|---|
| `body-on-panel` | body | 15.75:1 | pass | pass |
| `body-on-faint` | body | 15.26:1 | pass | pass |
| `body-on-surface` | body | 13.33:1 | pass | pass |
| `secondary-on-panel` | body | 8.40:1 | pass | pass |
| `disabled-on-panel` | body | 3.25:1 | fail | miss |
| `display-on-panel` | large | 18.32:1 | pass | pass |
| `info-default-on-panel` | ui | 10.50:1 | pass | n/a |
| `success-default-on-panel` | ui | 10.79:1 | pass | n/a |
| `warning-default-on-panel` | ui | 10.27:1 | pass | n/a |
| `error-default-on-panel` | ui | 9.76:1 | pass | n/a |
| `accent-default-on-panel` | ui | 10.30:1 | pass | n/a |
| `border-default-on-panel` | ui | 4.55:1 | pass | n/a |
| `border-faint-on-surface` | ui | 2.51:1 | fail | n/a |
| `border-faint-on-panel` | ui | 2.97:1 | fail | n/a |
| `body-on-selection` | body | 14.76:1 | pass | pass |

