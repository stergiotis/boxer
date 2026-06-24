---
type: explanation
audience: IDS app authors and contributors
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# IDS pattern: iconography

Every icon in an IDS-conformant ImZero2 app comes from one source: **Symbols Nerd Font Mono** ([ADR-0030 §SD12](../../adr/0030-imzero2-design-system-typography.md)). This doc is the master catalogue — meaning → glyph mappings for status indicators, common actions, navigation affordances, file/object types, data-operation verbs, and panel-level affordances. It also sets the size, color, and composition rules other pattern docs reference.

The catalogue here is the *recommended* starting set; concrete codepoints are pinned in M0 of the IDS rollout once the Nerd Fonts version is selected (occasional cross-version codepoint drift in the FontAwesome subset is real — see Background). Pattern docs ([patterns/status-and-legends.md](./status-and-legends.md), [patterns/empty-states.md](./empty-states.md), [patterns/tables.md](./tables.md), [patterns/plots.md](./plots.md)) reference this doc rather than each duplicating the catalogue.

## Background

**Why one icon source.** Mixing icon families (FontAwesome here, Material there, Codicons in a third place) is the single largest source of visual incoherence in dev-tool UIs. Strokes vary, glyph weights diverge, the "info" symbol looks subtly different in each, and operators reading dashboards must re-learn the vocabulary per panel. Choosing one source for the fleet is the cheapest, highest-leverage cohesion decision in iconography.

Symbols Nerd Font Mono aggregates many upstream families (FontAwesome, Codicons, Material Design Icons, Octicons, Powerline, Pomicons, Weather Icons, Devicons), each occupying a defined Unicode block. IDS uses the **FontAwesome 4 subset** as the primary catalogue — it's the most recognisable in 2026 dev-tool UIs, has the widest meaning coverage, and renders crisply at small UI sizes. Other subsets are used sparingly and only when FontAwesome lacks a suitable glyph.

**Codepoint stability across Nerd Fonts versions.** The FontAwesome 4 codepoint block (≈ U+F000 to U+F2FF) has been stable for years, but Nerd Fonts occasionally renumbers individual glyphs between major versions, and FontAwesome 5+ introduced an entirely different codepoint scheme that the Nerd Fonts project remaps in the U+F300–U+F5FF range. We pin a specific Nerd Fonts release version in M0 (per [ADR-0030 §SD12](../../adr/0030-imzero2-design-system-typography.md)) and the catalogue below is the recommended set as of that pinning.

**Foundational dependencies.**

- Font + distribution ([ADR-0030 §SD12](../../adr/0030-imzero2-design-system-typography.md)) — Symbols Nerd Font Mono embedded via `include_bytes!`; egui `FontDefinitions` fallback family on both Proportional and Monospace.
- Color ([ADR-0031](../../adr/0031-imzero2-design-system-color.md)) — semantic palette for status icons; `text.primary` / `text.secondary` for inline icons; never raw `Color32::from_rgb`.
- Spacing ([ADR-0032](../../adr/0032-imzero2-design-system-spacing-density-motion.md)) — icon sizes scale with density; `Gap.Inline` between icon and adjacent text.
- Patterns — [status-and-legends.md](./status-and-legends.md) (status icons, mandatory triple), [empty-states.md](./empty-states.md) (display-size variant icons).

## How it works

### Catalogue: status icons

Per [patterns/status-and-legends.md](./status-and-legends.md) §status-icons. These appear inline (badges, pills, banners) and at display size (empty states):

| Meaning | FontAwesome name | Codepoint (approx) | Notes |
|---|---|---|---|
| info | `fa-info-circle` | `\u{f05a}` | Default informational |
| info (alt) | `fa-info` | `\u{f129}` | Letter `i` in circle — alternate |
| success | `fa-check-circle` | `\u{f058}` | |
| warning | `fa-exclamation-triangle` | `\u{f071}` | Default warning |
| warning (alt) | `fa-exclamation-circle` | `\u{f06a}` | Circular alternate |
| error | `fa-times-circle` | `\u{f057}` | Default error |
| loading | `fa-spinner` | `\u{f110}` | Animated; respects reduced-motion |
| loading (alt) | `fa-circle-o-notch` | `\u{f1ce}` | Open-notch spinner alternate |
| neutral / idle | `fa-circle-o` | `\u{f10c}` | Open circle |
| pending | `fa-clock-o` | `\u{f017}` | Waiting / scheduled |
| paused | `fa-pause` | `\u{f04c}` | |
| running | `fa-play` | `\u{f04b}` | |
| stopped | `fa-stop` | `\u{f04d}` | |

### Catalogue: common actions

| Meaning | FontAwesome name | Codepoint (approx) | Notes |
|---|---|---|---|
| close / dismiss | `fa-times` | `\u{f00d}` | Window close, dialog close, chip remove |
| menu / hamburger | `fa-bars` | `\u{f0c9}` | Three-bar menu |
| ellipsis horizontal | `fa-ellipsis-h` | `\u{f141}` | "More" actions |
| ellipsis vertical | `fa-ellipsis-v` | `\u{f142}` | Vertical "more" actions |
| plus / add | `fa-plus` | `\u{f067}` | Add item, new entry |
| minus / remove | `fa-minus` | `\u{f068}` | Remove item |
| check / confirm | `fa-check` | `\u{f00c}` | Confirmation, validation pass |
| refresh / reload | `fa-refresh` | `\u{f021}` | Manual reload |
| sync | `fa-sync` | `\u{f021}` | Same glyph; semantic alias |
| save / disk | `fa-save` | `\u{f0c7}` | Floppy disk; "save" |
| copy | `fa-copy` | `\u{f0c5}` | Two-pages copy |
| pencil / edit | `fa-pencil` | `\u{f040}` | Inline edit |
| trash / delete | `fa-trash` | `\u{f1f8}` | Destructive |
| download | `fa-download` | `\u{f019}` | |
| upload | `fa-upload` | `\u{f093}` | |
| eye / show | `fa-eye` | `\u{f06e}` | Show / reveal |
| eye-slash / hide | `fa-eye-slash` | `\u{f070}` | Hide / mask |
| link / external | `fa-external-link` | `\u{f08e}` | External URL marker |

### Catalogue: navigation

| Meaning | FontAwesome name | Codepoint (approx) |
|---|---|---|
| chevron up | `fa-chevron-up` | `\u{f077}` |
| chevron down | `fa-chevron-down` | `\u{f078}` |
| chevron left | `fa-chevron-left` | `\u{f053}` |
| chevron right | `fa-chevron-right` | `\u{f054}` |
| caret up | `fa-caret-up` | `\u{f0d8}` |
| caret down | `fa-caret-down` | `\u{f0d7}` |
| caret left | `fa-caret-left` | `\u{f0d9}` |
| caret right | `fa-caret-right` | `\u{f0da}` |
| arrow up | `fa-arrow-up` | `\u{f062}` |
| arrow down | `fa-arrow-down` | `\u{f063}` |
| arrow left | `fa-arrow-left` | `\u{f060}` |
| arrow right | `fa-arrow-right` | `\u{f061}` |

**Caret vs. chevron.** Carets (`\u{f0d7}`–`\u{f0da}`) are filled wedges; use them for sortable column indicators in [tables.md](./tables.md) §interaction and for `CollapsingHeader`-style expand/collapse markers. Chevrons (`\u{f053}`–`\u{f077}`) are open angle brackets; use them for navigation affordances (back / forward, pagination, "view all"). Arrows are full directional indicators with shafts; use them for explicit movement (drag handles, "move to top," directional menus).

### Catalogue: data operations

| Meaning | FontAwesome name | Codepoint (approx) | Notes |
|---|---|---|---|
| filter | `fa-filter` | `\u{f0b0}` | Filter panel toggle |
| sort | `fa-sort` | `\u{f0dc}` | Unsorted column indicator |
| sort asc | `fa-sort-asc` / `fa-sort-up` | `\u{f0de}` | |
| sort desc | `fa-sort-desc` / `fa-sort-down` | `\u{f0dd}` | |
| columns | `fa-columns` | `\u{f0db}` | Column-visibility toggle |
| search | `fa-search` | `\u{f002}` | Search input |
| zoom in | `fa-search-plus` | `\u{f00e}` | |
| zoom out | `fa-search-minus` | `\u{f010}` | |
| filter-clear | `fa-times-circle` | `\u{f057}` | "Clear filter" CTA — reuses error icon |

### Catalogue: file / object types

| Meaning | FontAwesome name | Codepoint (approx) |
|---|---|---|
| folder | `fa-folder` | `\u{f07b}` |
| folder open | `fa-folder-open` | `\u{f07c}` |
| file generic | `fa-file` | `\u{f15b}` |
| file text | `fa-file-text-o` | `\u{f0f6}` |
| file code | `fa-file-code-o` | `\u{f1c9}` |
| database | `fa-database` | `\u{f1c0}` |
| server | `fa-server` | `\u{f233}` |
| user | `fa-user` | `\u{f007}` |
| users / group | `fa-users` | `\u{f0c0}` |

### Catalogue: tools and meta

| Meaning | FontAwesome name | Codepoint (approx) | Notes |
|---|---|---|---|
| gear / settings | `fa-cog` | `\u{f013}` | Settings panel, preferences |
| wrench / tools | `fa-wrench` | `\u{f0ad}` | Coming-soon empty state, dev tools |
| terminal | `fa-terminal` | `\u{f120}` | Shell, command palette |
| code / brackets | `fa-code` | `\u{f121}` | Code view toggle |
| lock | `fa-lock` | `\u{f023}` | Permission-denied empty state |
| unlock | `fa-unlock` | `\u{f09c}` | Granted state |
| question | `fa-question-circle` | `\u{f059}` | Help / hint |
| bell | `fa-bell` | `\u{f0f3}` | Notifications |
| flag | `fa-flag` | `\u{f024}` | Bookmark / mark |
| star | `fa-star` | `\u{f005}` | Favourites |
| heart | `fa-heart` | `\u{f004}` | Likes / saved (use sparingly — consumer-app feel) |

### Catalogue: plot / dashboard affordances

| Meaning | FontAwesome name | Codepoint (approx) | Notes |
|---|---|---|---|
| live / streaming | `fa-circle` | `\u{f111}` | Live indicator (often animated dot) |
| pause stream | `fa-pause-circle` | `\u{f28b}` | Paused live data |
| expand | `fa-expand` | `\u{f065}` | Fullscreen panel |
| compress | `fa-compress` | `\u{f066}` | Exit fullscreen |
| pin / fix | `fa-thumb-tack` | `\u{f08d}` | Pin panel / keep visible |

### Sizes

| Context | Tight (px) | Standard (px) | Roomy (px) |
|---|---:|---:|---:|
| Inline with `Caption` text | 9 | 11 | 12 |
| Inline with `Body` text | 12 | 13 | 14 |
| Inline with `Heading` text | 15 | 16 | 17 |
| Button icon (small) | 14 | 16 | 18 |
| Button icon (medium) | 18 | 20 | 22 |
| Button icon (large) | 22 | 24 | 28 |
| Status badge | 11 | 13 | 14 |
| Empty-state display | 24 | 32 | 48 |
| Title-bar indicator | 13 | 14 | 16 |

Inline-with-text sizes match the surrounding line height per [ADR-0030 §SD3](../../adr/0030-imzero2-design-system-typography.md) type scale; the icon visually centers on the cap height of the adjacent text. Display sizes (empty states, prominent status) decouple from text and follow the empty-state table per [empty-states.md](./empty-states.md).

### Color and tint

- **Inline icons** (within a text run): inherit the text color — `text.primary` for body text, `text.secondary` for caption / secondary text.
- **Status icons** ([status-and-legends.md](./status-and-legends.md)): tinted `semantic.<role>.default` (info / success / warning / error / neutral). Tint is *the* signalling — combined with the label and the icon shape, the triple satisfies CVD.
- **Plot annotation icons** (live indicator, pause): semantic palette per the same rules as plot annotations ([plots.md](./plots.md) §annotations).
- **Button icons**: inherit the button's label color (which itself is derived from button-state tokens).
- **Active state**: a momentary `semantic.accent.default` tint on click; resolves back to the inherited color after `Motion.Quick`.

**Never** raw `Color32::from_rgb`. Tier 1 lint L2 catches it.

### Composition with text

Three composition modes:

- **Icon + text** (most common): icon precedes text with `Gap.Inline` between. The icon and text vertically align on the text baseline (the icon centers on cap height, not on the full em).
- **Icon-only** (restricted): reserved for *well-known* contexts — title-bar indicators, status-bar entries, button toolbars with universal glyphs (close-X, settings-gear). Icon-only outside these contexts requires tooltip on hover (keyboard accessibility — see Invariants).
- **Text-only** (no icon): legend entries with swatches instead of glyphs, dense table columns where the icon would crowd, places where the icon would not add information.

When in doubt, include text. The mandatory triple from [ADR-0031 §SD6](../../adr/0031-imzero2-design-system-color.md) — color + icon + text — is the default for status; weakening to icon-only is the exception, not the norm.

### Icon-name conventions in code

Apps reference icons by symbolic names, not raw codepoints:

```rust
// In rust/imzero2/imzero2_egui/src/style/icons/mod.rs (or a per-app icons module):
pub const ICON_INFO_CIRCLE:    &str = "\u{f05a}";
pub const ICON_CHECK_CIRCLE:   &str = "\u{f058}";
pub const ICON_EXCLAMATION:    &str = "\u{f071}";
pub const ICON_TIMES_CIRCLE:   &str = "\u{f057}";
pub const ICON_SPINNER:        &str = "\u{f110}";
// …
```

Application code:

```rust
ui.label(format!("{}  Connected", icons::ICON_CHECK_CIRCLE));
```

Raw `"\u{f05a}"` literals in panel code are a Tier 3 anti-pattern — they sidestep the catalogue. Future Tier 1 lint candidate: flag string literals containing FontAwesome PUA codepoints outside the icons module.

### Adding new icons

Adding a glyph to the catalogue is a Tier 3 ([ADR-0029 §SD10](../../adr/0029-imzero2-design-system-and-policy-as-code.md)) decision — captured as an Amendment to this doc or, if the addition is part of a larger pattern change, a follow-on ADR. The criteria:

- The meaning is not already in the catalogue (don't duplicate).
- The glyph comes from the Symbols Nerd Font Mono catalogue (no inline SVG, no external icon imports).
- FontAwesome 4 subset is preferred; alternative subsets (Codicons, Material) acceptable only when FA lacks the meaning.
- The name follows `fa-<thing>` for FA glyphs, `cod-<thing>` for Codicons, etc.
- The codepoint is pinned to the current Nerd Fonts version.

The catalogue is the *fleet-wide vocabulary*; deviations across apps are a Tier 2 V6 finding (iconography consistency).

### Animation

Only the spinner is intrinsically animated. All other icons are static. Animated decorative icons (rotating stars, pulsing hearts, bouncing arrows) are forbidden — Swiss-minimalist motion is functional, not expressive.

A "live" data indicator (plot dashboards, streaming panels) is one acceptable exception: a small filled `fa-circle` may pulse with `Motion.Standard` opacity oscillation when data is actively streaming. Reduced-motion preference resolves the pulse to a static filled dot per [ADR-0032 §SD5](../../adr/0032-imzero2-design-system-spacing-density-motion.md).

### Density behavior

Icon sizes scale with the active density preset per the table above. The icon ↔ adjacent-text gap (`Gap.Inline`) also scales. Inline icon sizing tracks the surrounding text line-height, so density-driven type-scale changes automatically resize inline icons.

## Invariants

- All icons come from Symbols Nerd Font Mono. No inline SVG, no custom paint, no external icon imports.
- One meaning maps to one glyph fleet-wide. Two apps using the same meaning use the same glyph (Tier 2 V6).
- Status icons combine with a text label per [ADR-0031 §SD6](../../adr/0031-imzero2-design-system-color.md) — the mandatory triple. Icon-only status is reserved for well-known title-bar / status-bar contexts.
- Decorative icons (no meaning-bearing role) are forbidden.
- Inline icons inherit the surrounding text color; status icons take the semantic-role tint; raw color literals are banned (Tier 1 L2).
- Icon-only buttons in any non-well-known context require a tooltip naming the action (keyboard accessibility).
- Animated icons are limited to (a) the loading spinner and (b) the "live" streaming dot. All other icons are static.
- Reduced-motion preference resolves all icon animation to static glyphs.
- The catalogue is fleet-wide. Adding to it is a Tier 3 decision; per-app icon overrides are forbidden without escalation.
- Apps reference icons via the `icons::` module (symbolic names), not raw codepoint literals.
- FontAwesome 4 is the primary subset; alternative Nerd Fonts subsets used only when FA lacks the meaning.

## Trade-offs

- **One source vs many** — single-source means we are bound by FontAwesome 4's coverage; meanings outside that set need substitutes (e.g., "merge" or "branch" — FA has limited git iconography). The fix is to add via Tier 3, choosing from another Nerd Fonts subset (Codicons has comprehensive git iconography). Default: stay single-source unless a real coverage gap surfaces.
- **FontAwesome 4 vs newer subsets** — FA 4's glyph style is recognisable and battle-tested; FA 5/6 (and Codicons, Material) have more modern stroke weights. Switching is a fleet-wide visual refresh; defer indefinitely.
- **Icon-only vs icon + text** — icon-only is more compact but loses keyboard accessibility and CVD safety. Default: icon + text everywhere except well-known title-bar / status-bar slots.
- **Decorative vs functional** — decorative icons (a star next to a tab label "just because") add visual weight without semantic content. Swiss-minimalism rejects them; the rule is unforgiving.
- **Inline color inheritance vs explicit tint** — inline icons inheriting text color is the simplest rule but loses the chance to use color for emphasis (e.g., a red trash icon on a delete button). The pattern is consistent if buttons themselves carry the semantic color in their label / fill, and the icon inherits.
- **Animated indicators vs static** — animated "live" dots are widely-understood but contribute to motion that not every user wants. Reduced-motion handling mitigates; default is animated-when-allowed.
- **Per-app catalogue extensions vs fleet-wide curation** — per-app would let apps move faster; fleet-wide enforces cohesion. Default: fleet-wide; Tier 3 to extend.

## Anti-patterns

- Inline SVG, custom paint, or external icon imports (every icon comes from Symbols Nerd Font Mono).
- Different glyphs for the same meaning across apps or panels.
- Decorative icons with no information-bearing role.
- Mixing Nerd Fonts subsets visibly within one panel (FA + Material + Codicons all in one toolbar).
- Icon-only buttons without tooltips outside well-known contexts.
- Raw `"\u{f05a}"` codepoint literals in panel code (use `icons::ICON_INFO_CIRCLE`).
- Hardcoded `Color32::from_rgb` for icon tinting (Tier 1 L2).
- Animated icons beyond the spinner and live indicator.
- Ignoring reduced-motion preference for icon animation.
- Per-app icon catalogue extensions without Tier 3 escalation.
- Using a check-circle for a destructive action (semantic mismatch — check means "confirmed/success," not "yes I really want to delete").
- Tiny icons (< 9 px) — falls below the Nerd Font legibility floor.
- Oversized icons in compact contexts (a 32 px icon in a 13 pt body sentence breaks line-height rhythm).

## Further reading

- [ADR-0029 — design system + policy-as-code](../../adr/0029-imzero2-design-system-and-policy-as-code.md) — parent framework; Tier 2 rubric V6 (iconography consistency) is the load-bearing grader here.
- [ADR-0030 — typography](../../adr/0030-imzero2-design-system-typography.md) — §SD12 Symbols Nerd Font Mono as the embedded icon fallback family; egui `FontDefinitions` plumbing.
- [ADR-0031 — color foundations](../../adr/0031-imzero2-design-system-color.md) — semantic palette §SD2 for status tints; "color is never the sole encoding channel" §SD6.
- [ADR-0032 — spacing / density / motion](../../adr/0032-imzero2-design-system-spacing-density-motion.md) — icon sizing per density; `Gap.Inline` for icon-text spacing; `Motion.*` for animation; reduced-motion handling.
- [patterns/status-and-legends.md](./status-and-legends.md) — status icons (the canonical use case for the catalogue).
- [patterns/empty-states.md](./empty-states.md) — display-size icons per empty-state variant.
- [patterns/tables.md](./tables.md) — sort caret + boolean/flag icons in cells.
- [patterns/plots.md](./plots.md) — live indicator + annotation icons.
- patterns/time-range-picker.md *(forthcoming)* — calendar / clock icons for range affordances.
- [Symbols Nerd Font Mono cheat sheet](https://www.nerdfonts.com/cheat-sheet) — upstream catalogue with searchable per-glyph codepoints.
- [FontAwesome 4 icon index](https://fontawesome.com/v4/icons/) — primary IDS subset reference.
- [Nerd Fonts release page](https://github.com/ryanoasis/nerd-fonts/releases) — for SHA-pinned downloads.
