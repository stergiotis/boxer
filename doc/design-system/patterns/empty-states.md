---
type: explanation
audience: IDS app authors and contributors
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# IDS pattern: empty states

An empty state is how a panel communicates "I have nothing to show, and here's why or what to do." Every panel that can be empty needs one — a blank panel with no message is the most-cited UX failure in [ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md) §SD9 Tier 2 rubric V3 (empty-state quality), and one of the easiest mistakes to fix at scale once the pattern is codified.

This doc covers the empty-state taxonomy (seven distinct variants, each with its own rules), the anatomy (icon + headline + body + CTA), the per-variant content patterns, and how empty states compose with plot and table surfaces. It is an *explanation* of how IDS empty states behave; for canonical code, see the `regex_explorer` no-matches state and the Grafana-replacement panels' no-data overlays.

## Background

**Why empty states matter.** A panel that shows nothing is ambiguous: is it still loading? Is the query wrong? Is there genuinely no data? Is the network broken? The user cannot tell the difference between these unless the panel says so. Operators interpreting a dashboard at 03:00 must not guess; the empty state is the difference between "everything is fine, no errors logged" and "the logging pipeline is broken." This pattern is load-bearing for operator trust.

**Where empty states appear.** Anywhere data can be absent: tables with no rows, plots with no points, lists with no items, search results with no matches, file pickers with no files yet, dashboards with no time-range overlap, dialog forms before the user fills them. The empty state is the *default* visual for any data-bearing surface.

**Foundational dependencies.**

- Color ([ADR-0031](../../adr/0031-imzero2-design-system-color.md)) — semantic palette per variant: `info` for neutral cases, `warning` for stale/degraded, `error` for failures, `neutral` for "intentionally empty." Never the data-encoding palette.
- Typography ([ADR-0030](../../adr/0030-imzero2-design-system-typography.md)) — `Heading` for the headline; `Body` for the explanatory text; `Caption` for context detail; button typography for CTAs.
- Spacing ([ADR-0032](../../adr/0032-imzero2-design-system-spacing-density-motion.md)) — `Padding.Loose` around the empty-state stack; `Gap.Items` between icon/headline/body/CTAs.
- Icons ([ADR-0030 §SD12](../../adr/0030-imzero2-design-system-typography.md)) — Symbols Nerd Font Mono glyphs; large display size (32–48 px in Standard density).
- Motion ([ADR-0032 §SD5](../../adr/0032-imzero2-design-system-spacing-density-motion.md)) — loading spinner respects reduced-motion; transitions between empty / populated states use `Motion.Quick` cross-fade.
- Status conventions ([patterns/status-and-legends.md](./status-and-legends.md)) — empty-state icons share the same FontAwesome catalogue as inline status badges.

## How it works

### Empty-state taxonomy

Seven variants. Each has a specific situation, a recommended icon family, a headline pattern, and a CTA pattern:

| Variant | Situation | Icon (status role) | Headline pattern | CTAs |
|---|---|---|---|---|
| **No-data-yet** | Fresh state; never used | `info` or feature-specific glyph | Action-oriented ("Add your first query") | Primary CTA = the start action |
| **No-results** | Filtered / searched, returned empty | `info` (search glyph) | "No matches" / "No results" | "Clear filter" / "Adjust query" |
| **Loading** | Data being fetched; transient | `info` (spinner — animated) | "Loading..." or query-specific | Optional "Cancel" if cancellable |
| **Error** | Data couldn't be loaded | `error` | Short, specific ("Connection failed") | "Retry" / context-specific recovery |
| **No-data-for-context** | Data exists, but not for current selection | `info` | Specific to context ("No data in this range") | Context-modifying action ("Expand range") |
| **Permission-denied** | User lacks access | `warning` (lock glyph) | "Access denied" or role-specific | "Request access" / re-auth |
| **Coming-soon** | Feature exists in scaffold, not implemented | `neutral` (construction glyph) | "Coming soon" | None or "Learn more" |

Picking the right variant is the first decision; getting it wrong (e.g., showing a generic "no data" for what is actually an error) communicates wrong information. Tier 2 rubric V3 grades correctness of variant choice against the visible context — an LLM looking at a panel with a query in flight should not see an Error empty state.

### Anatomy

An empty state is a vertically-centered stack inside the empty area of a panel:

```
┌──────────────────────────────────────┐
│                                      │
│              [ICON]                  │   ← 32–48 px (Standard), tinted with the variant's semantic color
│                                      │
│             Headline                 │   ← Heading size, text.primary
│                                      │
│        Body explanation              │   ← Body size, text.secondary
│        (1–2 lines)                   │
│                                      │
│      [Primary CTA]  [Secondary]      │   ← Button row, optional
│                                      │
└──────────────────────────────────────┘
```

- **Icon** — large, tinted with `semantic.<role>.default` per the variant's semantic role. Glyph from the Symbols Nerd Font Mono catalogue ([patterns/status-and-legends.md](./status-and-legends.md) for the canonical codepoints).
- **Headline** — `Heading` size, `Medium` weight, `text.primary` color. Short (≤ 6 words). Specific to the variant — "No matches" or "Connection failed", not generic "No data."
- **Body** — `Body` size, `Regular` weight, `text.secondary` color. One or two sentences explaining the reason or the next step. Optional but recommended.
- **CTAs** — primary action filled button with `semantic.accent.default` fill (or `semantic.info.default` when accent reads too brand-heavy for the context); secondary action ghost button with `text.secondary` text on `bg.surface`. Optional; depends on whether action is meaningful.
- **Stack centering** — horizontally and vertically centered within the empty area. `Padding.Loose` around the stack gives breathing room from the surrounding panel.
- **Gap** — `Gap.Items` between each element (icon → headline → body → CTAs).

The whole stack is *the* empty-state; we do not add decorative backgrounds, frames, or illustrations. Swiss-minimalist restraint applies: information density is low here (the panel is empty), so the empty-state stack should feel intentional rather than dressed up.

### Per-variant content patterns

**No-data-yet (welcoming).** Shown the first time the user opens a panel that has no content. Icon is feature-specific (a `+` for adding, a folder for files, a query-symbol for queries). Headline names the action: "Add your first query", "Open a file to begin." Body is one sentence explaining what the panel does. CTA is the primary start action — a single button that does the most useful thing.

**No-results (filtered).** Shown when a search or filter excludes everything. Icon is a search glyph (`fa-search` ≈ `\u{f002}`). Headline is "No matches" or "No results." Body shows the active filter — `query: "foo"` or `range: 14:00 → 15:00` — formatted in `Body.Mono` for the technical value. CTAs are "Clear filter" (primary) and "Adjust filter" or context-specific edits (secondary).

**Loading (transient).** Shown while data is in flight. Icon is the animated spinner (`fa-spinner` ≈ `\u{f110}`); reduced-motion preference resolves the spinner to a static glyph per [ADR-0032 §SD5](../../adr/0032-imzero2-design-system-spacing-density-motion.md). Headline is "Loading..." or query-specific ("Running query..."). Body is optional; useful when the operation is identifiable ("Querying ClickHouse..."). CTA is "Cancel" only when cancellation is actually wired up; otherwise omit.

A loading state that exceeds an *expected* duration becomes its own UX problem. Apps may transition from Loading to a "still loading" variant after ~5 s ("This is taking longer than usual — still trying.") and to an Error variant after ~30 s ("Timed out. Retry?"). The exact thresholds are app-specific.

**Error.** Shown when data couldn't be loaded. Icon is `fa-times-circle` ≈ `\u{f057}` tinted `semantic.error.default`. Headline is short and specific: "Connection failed", "Query syntax error", "Permission denied" — not "Error." Body shows the *primary* error text in `Body`, preserved verbatim from the underlying error (operators trust raw error text; paraphrasing loses information). For developer-facing apps, technical detail (stack trace, error code) goes under a collapsing `Details` wrapper at `Caption.Mono` size.

CTAs include "Retry" when the error is transient (network blip, server restart) and a recovery action when the error is correctable (e.g., "Edit query" for a syntax error). For permission errors, the CTA depends on the auth model (re-auth, request access, switch user).

**No-data-for-context.** Shown when data exists in the system but not for the current narrow context (a time range, a filter combination, a selected entity). Icon is `fa-info-circle` ≈ `\u{f05a}`. Headline names the context — "No data in this range" or "No events for this host." Body shows the active context — the time range, the selection — in `Body.Mono` for the technical values. CTAs propose broadening the context: "Expand range to last 24 h", "Clear host filter."

Subtle distinction from No-results: No-results is a query/filter applied by the user with no matches; No-data-for-context is the implicit context (current selection, current time window) producing no overlap. Both render similarly but the CTA differs — No-results clears the explicit filter; No-data-for-context modifies the implicit context.

**Permission-denied.** Shown when the user lacks the capability to view the data. Icon is `fa-lock` ≈ `\u{f023}` tinted `semantic.warning.default`. Headline is "Access denied" or specific to the missing capability ("Requires `admin` role"). Body explains what capability is needed and why. CTAs depend on the auth model.

For ImZero2 apps under [ADR-0026](../../adr/0026-app-runtime-and-capability-subjects.md), permission-denied corresponds to a missing cap-grant in the app's `Manifest.Caps`. The body text can name the missing subject (`ch.local.exec.scratchpad`) directly — operators reading the dashboard recognise the syntax.

**Coming-soon.** Shown in scaffolded panels where the implementation is deliberately deferred. Icon is `fa-wrench` ≈ `\u{f0ad}` or `fa-construction` (alternative) tinted `text.secondary`. Headline is "Coming soon." Body is optional context about why and when. No CTA, or "Learn more" linking to the relevant ADR. Use sparingly — coming-soon panels should rarely ship; prefer hiding the panel entirely until implemented.

### Density behavior

| Element | Tight | Standard | Roomy |
|---|---|---|---|
| Icon size | 24 px | 32 px | 48 px |
| Icon ↔ headline gap | `Gap.Items` (per density) | per density | per density |
| Stack vertical padding | `Padding.Outer` (8) | `Padding.Loose` (12) | `Padding.Loose` (16) |
| Headline size | per type-scale Tight Heading (15 pt) | Standard Heading (16 pt) | Roomy Heading (17 pt) |
| CTA button padding | per density | per density | per density |

Density scaling applies uniformly; the empty state itself does not override the active density.

### Transitions

Empty → populated and populated → empty transitions use `Motion.Quick` (80 ms) cross-fade. Reduced-motion preference resolves to instant swap. The transition is symmetric — fading *in* an empty state when data is cleared uses the same duration as fading *out* when data arrives.

Loading-state-to-data transitions specifically: the spinner fades out as the data renders. For plot transitions, the empty-overlay cross-fade does not retrigger animations of underlying plot frame elements (axes, grid) — those stay in place.

### Composition with plots and tables

**Empty plot.** Render the plot frame (axes, grid, legend area) and overlay the empty state in the *plot data area* — the inner rectangle bounded by the axes. The frame stays visible; only the data area shows the empty state. This preserves visual continuity with neighboring populated plots in a dashboard.

**Empty table.** Render the header row (column labels) and show the empty state in the body region below the header. Don't render zebra-striped placeholder rows — they suggest data that isn't there.

**Empty full panel.** When the whole panel has no scaffolding (no axes, no header) — e.g., a list view, a file-picker pane — center the empty state in the full panel area.

**Empty card.** Cards may have a heading bar; the empty state lives below the heading bar in the card body. Optionally, the heading bar shows a status badge ("loading" pill) while the body shows the loading state.

### Interaction

- **CTAs as primary actions.** The primary CTA in an empty state is *the* action for that empty state. Hidden CTAs (mouse-only hover-reveal) are an anti-pattern — operators using keyboard navigation lose them.
- **Retry behavior for transient errors.** "Retry" CTAs should be debounced (the user clicking twice in a second should not fire two retry requests). Visual feedback: the CTA disables briefly while the retry is in flight; the empty state transitions through Loading state again on retry.
- **Cancel behavior for loading.** When "Cancel" is shown, the cancellation must be wired through to the underlying operation — cancelling a query, aborting a fetch. Cancelled loading transitions to a `neutral` empty state ("Cancelled. Retry?").
- **Keyboard focus.** When an empty state has CTAs, the primary CTA receives focus when the empty state becomes visible — keyboard users can press Enter immediately.

## Invariants

- Every panel that can be empty has an empty state — blank panels with no message are forbidden (Tier 2 V3).
- An empty state has at minimum an icon and a headline. Body and CTAs are recommended but variant-dependent.
- Variant choice matches the panel's true situation: Loading while data is in flight; Error on failure; No-results for filtered-empty; etc. Tier 2 V3 grades the variant against the panel context.
- Specific reasons over generic "No data." "No data in this time range" or "Connection refused" instead of "Empty."
- Error empty states preserve the underlying error text verbatim in the Body (or under `Details` for verbose technical content); paraphrasing is forbidden.
- Loading uses the spinner glyph; spinner respects reduced-motion (`Motion.*` → 0 when set, resolving to a static glyph).
- CTA actions are real — primary CTAs must be wired through to the underlying operation. Shipping a CTA that does nothing is a Tier 3 escalation.
- Icons come from the Symbols Nerd Font Mono catalogue and the variant's semantic color; never raw `Color32::from_rgb`.
- Empty plot states render inside the plot data area (preserving axes/grid); empty table states render below the header row.
- Empty-state stack is vertically and horizontally centered within the empty area.
- The empty state respects the active density preset; it does not override.

## Trade-offs

- **Welcoming vs. minimal.** First-use empty states benefit from a longer, more guiding tone ("Add your first query to begin exploring."); experienced-user empty states benefit from terseness ("No queries yet."). Default: welcoming tone for first-use; terse for everywhere else. The distinction is recoverable from app state (has this user ever populated this panel?).
- **Body text vs. icon-only.** For unambiguous empty states (a search with no matches in a clearly-marked search panel), icon + headline can suffice; body is redundant. For ambiguous cases (a dashboard panel that could be loading or could be empty), body explains. Default: include body unless the panel context is unambiguous.
- **CTAs vs. no-CTA.** Some empty states have no useful action (Coming-soon, certain Permission-denied cases). Forcing a CTA where none exists creates fake buttons. Default: omit CTAs when there's no real action.
- **Loading thresholds.** When should "Loading..." escalate to "Still loading..." to "Timed out"? Too short and users see flicker on fast operations; too long and they wait without feedback. Default: 5 s for "still loading," 30 s for timeout; per-operation tunable.
- **Generic "No data" vs. context-specific reason.** Specific reasons require app code to know *why* the panel is empty. Generic "No data" is the failure case. Default: invest in specific reasons; generic "No data" is a placeholder for the M3 backfill to replace.
- **Inline error text vs. Details collapsing wrapper.** Long error messages (stack traces, multi-line server responses) crowd the empty state. Short error summaries communicate to non-developers. Default: short headline summary; full text under `Details` collapsing wrapper at `Caption.Mono`.
- **Transitions vs. instant swap.** Cross-fade transitions feel polished but cost frames; instant swap is jarring on slow networks. Default: `Motion.Quick` cross-fade with reduced-motion respect; instant when motion is disabled.

## Anti-patterns

- Blank panel with no message (the most-cited V3 failure).
- Generic "No data" when a specific reason is available.
- Loading spinner that never resolves (operations must have timeouts).
- Animated spinner ignoring reduced-motion preference.
- Empty-state CTA wired to a no-op (every CTA must do something).
- Long technical error message in primary view (use `Details` collapsing wrapper).
- Paraphrasing or sanitising error text (operators trust raw error output).
- Decorative illustrations or large empty-state imagery (defeats Swiss-minimalist restraint).
- Empty-state background different from the panel background (visual noise without semantic content).
- Hidden CTAs (hover-only reveal — fails keyboard navigation).
- Multiple competing empty states for the same condition (one canonical variant per situation).
- Empty plot rendered as a full panel with no axes (preserve plot frame even when empty).
- Empty table without the header row (the header tells the user what to expect when data arrives).
- Loading state that shows nothing — no spinner, no headline, just blank (loading IS an empty state, not the absence of one).

## Further reading

- [ADR-0029 — design system + policy-as-code](../../adr/0029-imzero2-design-system-and-policy-as-code.md) — parent framework; Tier 2 rubric V3 (empty-state quality) is the load-bearing grader here.
- [ADR-0030 — typography](../../adr/0030-imzero2-design-system-typography.md) — `Heading`, `Body`, `Caption.Mono`, button typography conventions.
- [ADR-0031 — color foundations](../../adr/0031-imzero2-design-system-color.md) — semantic palette §SD2 for variant tinting; never the data-encoding palette.
- [ADR-0032 — spacing / density / motion](../../adr/0032-imzero2-design-system-spacing-density-motion.md) — `Padding.Loose` for stack padding; `Gap.Items` for inter-element gap; `Motion.Quick` for transitions; reduced-motion handling.
- [ADR-0026 — App runtime and capability subjects](../../adr/0026-app-runtime-and-capability-subjects.md) — Permission-denied variant references missing cap-grant from `Manifest.Caps`.
- [patterns/status-and-legends.md](./status-and-legends.md) — status icon catalogue; semantic role definitions reused here per variant.
- [patterns/tables.md](./tables.md) — empty-table composition (header + body); no zebra-striped placeholder rows.
- [patterns/plots.md](./plots.md) — empty-plot composition (preserve plot frame; overlay in data area).
- patterns/iconography.md *(forthcoming)* — master Nerd Font codepoint catalogue including the empty-state icon set.
- patterns/time-range-picker.md *(forthcoming)* — No-data-for-context for time-narrow cases uses the range picker as its CTA target.
- [Symbols Nerd Font Mono cheat sheet](https://www.nerdfonts.com/cheat-sheet) — upstream icon catalogue.
