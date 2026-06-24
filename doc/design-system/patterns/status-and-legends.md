---
type: explanation
audience: IDS app authors and contributors
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# IDS pattern: status and legends

Status badges and legends are how IDS apps communicate *meaning* — translating the semantic palette, the data-encoding palette, and the icon vocabulary into recognisable visual conventions. A connection state, a plot series, a heatmap value range, a row-level health indicator all reach for the same primitives: an **icon + text + color** triple presented in a small, conventional layout. This doc covers both because they share those primitives; they differ only in *surface* — status appears at the data point (a row, a card, a panel header); legends appear *adjacent to* data they describe.

This is an *explanation* of why IDS status and legends look the way they do, not a tutorial. For canonical code, see `regex_explorer` (status badges per match), `imztop` (status bar + sparkline legends), or a Grafana-replacement panel (plot legend + heatmap continuous legend).

## Background

**Where status appears.** Every nontrivial app communicates state somewhere — a connection indicator in the title bar, an empty-state hint when a panel has no data, a per-row freshness flag in a metrics table, a validation marker beside a form field, a header banner when the runtime detects degraded health. Status is the primary mechanism for "what is happening right now?"

**Where legends appear.** Plots without legends are unreadable: a line chart with three coloured lines is just three coloured lines without a key. Heatmaps need a continuous legend to translate cell color back to numeric magnitude. Tables with status cells benefit from a status key when the same status type appears repeatedly.

**Foundational dependencies.** Status and legends draw on every foundations sub-ADR:

- Color ([ADR-0031](../../adr/0031-imzero2-design-system-color.md)) — semantic palette (info / success / warning / error / neutral) for status; Crameri data-encoding palette (`batlowS` qualitative, `batlow` sequential, `vik` diverging) for plot legends; the semantic and data-encoding palettes are *disjoint* by construction so a chart color never collides with a status meaning.
- Typography ([ADR-0030](../../adr/0030-imzero2-design-system-typography.md)) — `Caption` for badge labels, `Body` for banner text, `Caption.Mono` for technical status values.
- Icons ([ADR-0030 §SD12](../../adr/0030-imzero2-design-system-typography.md)) — Symbols Nerd Font Mono is the icon fallback family; status icons come from its FontAwesome / Octicons / Codicons subsets.
- Spacing ([ADR-0032](../../adr/0032-imzero2-design-system-spacing-density-motion.md)) — `Padding.Inner`, `Gap.Inline`, `Gap.Items` for badge interiors and legend layouts; density preset scales all of these.

**The mandatory triple.** Per [ADR-0031 §SD6](../../adr/0031-imzero2-design-system-color.md), color is never the sole encoding channel. Every status carries **icon + text + color**; every plot legend entry carries **swatch + label** (plus marker shape or line style where applicable). This is a hard CVD-safety / accessibility floor — not a stylistic preference. Tier 2 rubric V2 (color-encoding consistency) and V5 (legend completeness) verify across the fleet.

## How it works

### Status — semantic roles

Six semantic roles are available per [ADR-0031 §SD2](../../adr/0031-imzero2-design-system-color.md); five carry status meaning and one is explicitly *not* a status:

| Role     | Status meaning | Use cases |
|----------|---------------|-----------|
| `info`    | informational / neutral attention | a hint, a tip, "is connecting", neutral state changes |
| `success` | positive / healthy | "connected", "up to date", "validated", "passed" |
| `warning` | caution / attention needed | "stale data", "deprecation pending", "degraded" |
| `error`   | failure / destructive | "disconnected", "failed", "invalid", "crashed" |
| `neutral` | default / no state | placeholder, "idle", "n/a" |
| `accent`  | *not a status* — selection / focus / branded highlight only | reserved for selection state, focus rings, brand emphasis |

The `accent` exclusion is load-bearing: accent colors are used for *selection and focus*, not state. A button highlighted with `accent` is not "in error" or "in success" — it's *selected* or *focused*. This separation prevents the screenshot from looking like every interactive element is reporting a state.

### Status — anatomy

A status badge is the most common form: a small, inline visual element communicating state. The triple is the structure:

```
┌──────────────────────┐
│ ●  running           │   ← icon + label
└──────────────────────┘
  ↑   ↑
  color-tinted glyph    Caption text, text.primary or semantic.*.default
```

- **Icon** — from Symbols Nerd Font Mono ([ADR-0030 §SD12](../../adr/0030-imzero2-design-system-typography.md)). Glyph is tinted with `semantic.<role>.default` color. Width tracks the cell of the mono font.
- **Label** — short text, `Caption` size (11 pt at Standard density), `text.primary` or `semantic.<role>.default` color depending on form (see below).
- **Color** — the icon color is the semantic role's `default` emphasis; the label color is also `default` for banners or `text.primary` for badges where the icon already carries the color signal.

### Status — presentation forms

| Form | Use | Layout |
|---|---|---|
| **Badge** | inline marker beside or within a cell, row, card | icon + label, no background, `Padding.Hair` inset |
| **Pill** | small standalone marker | icon + label, `semantic.<role>.subtle` background, `Rounding.Sm`, `Padding.Inner` |
| **Indicator** | icon-only marker in *well-known* contexts | icon only, no text — only acceptable where the icon meaning is doc-defined and consistent (e.g., the connection light in a title bar) |
| **Banner** | full-width strip across a panel / dialog | icon + label + optional action button, `semantic.<role>.subtle` background, `Stroke.Hair` `semantic.<role>.default` top + bottom borders, `Padding.Outer` vertical, `Padding.Loose` horizontal |
| **Inline text** | within prose | colored span of `Body` text with leading icon |
| **Status bar entry** | bottom-of-window persistent status | `Caption.Mono` cell with leading icon; one status per cell; cells separated by `Gap.Sections` |

Pills carry a background tint and are the most visually prominent; banners take the whole row and are reserved for state that affects an entire panel. Badges are the lowest-weight form and the most common in tables.

The **Indicator** (icon-only) form is restricted: it's accessible only when the icon is known to readers (the title-bar connection light, the status-bar idle-vs-busy dot). New consumers should not invent icon-only conventions — Tier 2 rubric V6 verifies icon meaning consistency across the fleet.

### Status — mapping data states to semantic roles

The canonical mapping for the recurring data-state vocabularies:

| Domain | State | Role |
|---|---|---|
| Connection | connecting | `info` |
| Connection | connected | `success` |
| Connection | disconnected | `neutral` (if intentional) or `error` (if not) |
| Data freshness | live (updated this frame) | `success` |
| Data freshness | recent (within freshness window) | `neutral` |
| Data freshness | stale (past freshness window) | `warning` |
| Data freshness | loading | `info` |
| Data freshness | error / unreachable | `error` |
| Validation | valid | `success` (or `neutral` if "no issue" is the default) |
| Validation | invalid | `error` |
| Validation | warning (e.g., deprecation) | `warning` |
| Long-running task | queued | `neutral` |
| Long-running task | running | `info` |
| Long-running task | succeeded | `success` |
| Long-running task | failed | `error` |
| Long-running task | cancelled | `neutral` |
| Process / service | running healthy | `success` |
| Process / service | running degraded | `warning` |
| Process / service | stopped | `neutral` |
| Process / service | crashed | `error` |

This mapping is fleet-wide; the same state in two different apps gets the same color. Tier 2 rubric V2 (color-encoding consistency) verifies. Deviations require a Tier 3 ADR.

### Status — icon catalogue (recommended)

Starting set from the Symbols Nerd Font Mono FontAwesome / Octicons range. The exact codepoints will be pinned in `patterns/iconography.md` once that doc lands; what follows is the recommended mapping per status role:

| Role | Glyph | FontAwesome name (approximate codepoint) |
|------|-------|------------------------------------------|
| `info` (general) | ⓘ | `fa-info-circle` (≈ `\u{f05a}`) |
| `info` (loading) | ◔ | `fa-spinner` (≈ `\u{f110}`) — *animated; respects reduced-motion* |
| `success` | ✓ | `fa-check-circle` (≈ `\u{f058}`) |
| `warning` | ⚠ | `fa-exclamation-triangle` (≈ `\u{f071}`) |
| `error` | ✕ | `fa-times-circle` (≈ `\u{f057}`) |
| `neutral` (queued / idle) | ◯ | `fa-circle-o` (≈ `\u{f10c}`) |
| `neutral` (pending) | ◷ | `fa-clock-o` (≈ `\u{f017}`) |

The loading spinner is the only intrinsically-animated status icon; its animation honours `Motion.Quick` (80 ms per rotation step) and resolves to a static glyph when reduced-motion is active ([ADR-0032 §SD5](../../adr/0032-imzero2-design-system-spacing-density-motion.md)). Other status icons are static.

### Status — transitions

State changes between two semantic roles use `Motion.Quick` (80 ms) cross-fade — the old icon and color fade out as the new fade in. No bounce, no slide, no scale — Swiss-minimalist motion is functional, not expressive. Reduced-motion preference resolves the transition to an instant swap.

For status bars where multiple statuses are visible, the *changing* cell cross-fades; adjacent cells do not animate.

### Legends — what they communicate

A legend translates a visual encoding back to its meaning. Three encoding types, three legend types:

- **Categorical encoding** (qualitative palette) → **discrete legend**: a list of swatch + label pairs.
- **Ordered encoding** (sequential palette) → **continuous legend (gradient bar)**: a colored bar with value labels at endpoints.
- **Signed encoding** (diverging palette) → **continuous legend with midpoint**: a colored bar with min / mid / max labels.

A fourth case — **status key** — appears when a table or panel has many status badges and the user benefits from an explicit "here's what each color means" panel.

### Legends — discrete legend anatomy

For plots with multiple series:

```
┌────────────────────────────┐
│ ──  cpu.user               │
│ ──  cpu.system             │
│ ──  cpu.iowait             │
│ ··  cpu.idle (dashed)      │
└────────────────────────────┘
```

- **Swatch**: 12 × 12 px filled rectangle (Standard density; scales with density) using the series' qualitative color from `batlowS`. For line plots, the swatch may be a short colored line segment matching the line style.
- **Label**: `Caption` size, `text.primary` color.
- **Gap swatch ↔ label**: `Gap.Inline`.
- **Gap entry ↔ entry**: `Gap.Items` vertically, `Gap.Sections` horizontally.

For plots with > 8 series, the legend is collapsible (click to expand) and shows the first 6 entries plus "+N more" until expanded. Tier 2 rubric V5 (legend completeness) tolerates this collapsing as long as all entries are reachable.

### Legends — continuous legend (gradient bar) anatomy

For heatmaps, density plots, magnitude shading:

```
sequential (batlow):
 [▮▮▮▮▮▮▮▮▮▮▮▮]
 0          100   (units in the panel's units convention)

diverging (vik):
 [▮▮▮▮▮▮│▮▮▮▮▮▮]
−50      0     +50
```

- **Bar dimensions**: 80 × 8 px horizontal, 8 × 80 px vertical (Standard density; scales).
- **Palette interpolation**: rendered from the active Crameri palette LUT ([ADR-0031 §SD3](../../adr/0031-imzero2-design-system-color.md)).
- **Value labels**: `Caption.Mono.Numeric` at endpoints (and midpoint for diverging); `text.secondary` color.
- **Unit suffix**: appended to one of the value labels or as a separate label below the bar.

For diverging legends, the midpoint label is mandatory — without it, the legend doesn't communicate the sign of the encoding. Tier 2 rubric V5 catches missing midpoint labels.

### Legends — status key

A status key is a discrete legend whose entries carry status semantics (icon + label + color triple), placed adjacent to a table or panel where status badges recur. Anatomy is the discrete legend but the swatch is replaced by the status icon at its tinted color:

```
●  running       (semantic.success.default)
●  degraded      (semantic.warning.default)
●  stopped       (semantic.neutral.default)
●  crashed       (semantic.error.default)
```

The icon is the swatch; the text label is the meaning. Status keys are not always needed — for tables where the icons are obvious and the badge labels speak for themselves, the key is redundant. Add a key when the same panel has > 4 distinct statuses *or* when the audience for the panel includes operators who don't already know the convention.

### Legends — placement

| Plot type | Preferred placement | Fallback |
|---|---|---|
| Small plot (< 200 px tall), few series | right of plot, vertical | below plot, horizontal |
| Wide plot (> 600 px), few series | below plot, horizontal | right of plot |
| Dense plot (≥ 8 series) | below plot, collapsible | side panel toggle |
| Heatmap | right of heatmap, vertical gradient bar | below heatmap, horizontal |
| Multiple plots in one panel | shared legend at panel level | per-plot legends if domains differ |

Legends are placed *adjacent to* the data, not overlapping. Overlap (legend rendered on top of plot data) is an anti-pattern — Tier 2 rubric V1 (clutter / hierarchy) catches it.

### Legends — completeness

Tier 2 rubric V5 grades plot legend completeness against this checklist:

- Every series visible in the plot has a legend entry.
- Each entry has a swatch (or line-segment or marker shape) *and* a text label.
- Continuous legends have endpoint value labels and a unit indication.
- Diverging legends have an explicit midpoint label.
- Time-series plots have a time-range label (start / end of the displayed window).
- Units are visible somewhere in the panel — on the axis, the legend, or as a panel header annotation.

Missing any of these is a V5 finding.

## Invariants

- Every status carries all three of {icon, text, color}. Color-only or icon-only status (outside the documented title-bar / status-bar indicator forms) is a CVD violation.
- The `accent` semantic role is reserved for selection / focus / brand emphasis. It is *not* a status. Tier 2 rubric V2 catches `accent` used for state.
- Status icons come from the Symbols Nerd Font Mono catalogue; the role → glyph mapping is fleet-wide. Apps do not invent app-local status icons.
- Data-state → semantic-role mapping is fleet-wide per the canonical table in this doc. Deviations require Tier 3.
- Plot legends use the data-encoding palettes (Crameri qualitative `batlowS`, sequential `batlow`, diverging `vik`). Status keys use the semantic palette. They never collide on the same surface.
- Discrete legends have one entry per visible series; continuous legends carry endpoint value labels and units; diverging continuous legends carry an explicit midpoint label.
- Status transitions use `Motion.Quick` cross-fade; reduced-motion resolves to instant swap.
- Legends are placed adjacent to the data they describe — never overlapping the plot area.
- The loading spinner is the only intrinsically-animated status icon; all others are static.
- Sequential palette only for ordered data; diverging only for signed; qualitative only for categorical. The three encoding intents never cross.

## Trade-offs

- **Pill vs. badge as the default form.** Pills are more visually prominent (filled background) but compete with surrounding content for attention; badges are quieter but easier to miss in dense tables. Default: badges in tables; pills in cards / panel headers where the status is the primary information.
- **Status bar vs. inline status.** A persistent status bar concentrates state information but adds chrome; inline status (within rows / cards) keeps state close to the data but distributes attention. Default: inline for per-data-point status; status bar for app-level state (connection, freshness, current user, environment indicator).
- **Icon catalog choice.** Nerd Fonts aggregates many sources (FontAwesome, Octicons, Material Icons, Codicons). The status set here favours FontAwesome for recognisability; some teams prefer Material's more geometric set. Switching is a Tier 3 ADR — it would change the fleet-wide visual character.
- **Legend collapsibility.** Collapsing legends reduce visual weight on dense panels but hide information until clicked. Default: collapse only when > 8 entries; below that, the legend is always visible.
- **Continuous legend on every heatmap vs. per-panel.** When a panel hosts multiple heatmaps with the same palette and range, one shared legend serves all. When ranges differ, per-heatmap legends are mandatory. Sharing requires the ranges to be identical — close-but-not-equal ranges are confusing.
- **Status density.** Showing every possible status state visibly invites overload. Default: collapse "neutral" / "idle" / "n/a" states to *no visible badge* (absent the badge means the default state); show badges only for non-default states.
- **Loading spinner vs. shimmer / skeleton.** Skeleton placeholders are a popular modern pattern but pull design weight; IDS chooses spinner for simplicity and clarity. Reconsider if a real animation-vs-skeleton case lands.

## Anti-patterns

- Status badges without icons (color + label only — fails CVD).
- Status indicators without labels in unfamiliar contexts (icon-only is reserved for the well-known title-bar / status-bar slots).
- Using `accent` for state ("highlighted" ≠ "succeeded").
- Mapping a custom state vocabulary outside the canonical table (Tier 3 escalation if a domain genuinely needs new roles).
- Plot legend rendered *inside* the plot area (occlusion).
- Continuous legend without endpoint value labels.
- Diverging continuous legend without an explicit midpoint label.
- Loading state shown as "live" data (the data-freshness pattern is load-bearing — stale data must read as stale).
- Per-app icon catalog (status icons are fleet-wide; Tier 3 to extend or change).
- Crameri sequential palette used for categorical legend (use `batlowS` for categorical).
- Animation on every status change beyond cross-fade (no bounce, no slide).

## Further reading

- [ADR-0029 — design system + policy-as-code](../../adr/0029-imzero2-design-system-and-policy-as-code.md) — parent framework; Tier 2 rubric V2 (color-encoding), V5 (legend completeness), V6 (iconography consistency) are the load-bearing graders here.
- [ADR-0030 — typography](../../adr/0030-imzero2-design-system-typography.md) — `Caption`, `Caption.Mono`, `Body.Numeric` token rationale; §SD12 Symbols Nerd Font Mono icon-fallback family.
- [ADR-0031 — color foundations](../../adr/0031-imzero2-design-system-color.md) — semantic palette §SD2; Crameri data-encoding §SD3; "color is never the sole encoding channel" §SD6.
- [ADR-0032 — spacing / density / motion](../../adr/0032-imzero2-design-system-spacing-density-motion.md) — `Padding.*`, `Gap.*`, `Rounding.Sm` for pills; `Motion.Quick` for status transitions.
- [patterns/tables.md](./tables.md) — status cell typography and shading; column-type conventions.
- patterns/plots.md *(forthcoming)* — plot legend placement and per-series secondary encoding (line style, marker shape).
- patterns/iconography.md *(forthcoming)* — master Nerd Font codepoint catalogue and per-glyph meaning.
- patterns/empty-states.md *(forthcoming)* — status-adjacent: "no data yet" is a kind of status.
- [Symbols Nerd Font Mono cheat sheet](https://www.nerdfonts.com/cheat-sheet) — upstream icon catalogue.
