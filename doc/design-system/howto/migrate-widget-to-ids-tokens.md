---
type: how-to
audience: widget authors fixing designlint L2 findings or migrating ad-hoc colors to IDS
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to migrate a widget's colors to IDS tokens

Convert raw `color.RGB(...)` / `color.RGBA(...)` literals inside a widget
package to references against the IDS semantic palette or data-encoding
LUTs (per [ADR-0031](../../adr/0031-imzero2-design-system-color.md))
so the widget reads as part of the fleet rather than as ad-hoc taste.
This recipe covers what to map to what, the L2-quiet bridge from
`styletokens.RGBA8` to `color.Color`, and the gotchas that bit each of
the M5 backfill PRs.

## When to use this recipe

You're touching a widget under `public/thestack/imzero2/egui2/widgets/`
or `public/keelson/runtime/` and `designlint` reports L2
violations (raw color literals outside the token module). The widget
likely defines per-status colors (badge / status pill / log row tint /
match highlighter / etc.) or chrome (panel bg, frame stroke, leaf text)
that pre-date the IDS palette ([ADR-0031 §SD2](../../adr/0031-imzero2-design-system-color.md)
landed 2026-05-14; widgets older than that were authored without it).

This recipe also applies when adding a new widget: prefer reaching
for IDS tokens from day one rather than picking ad-hoc hex values that
will eventually need migration.

If your widget needs *runtime-driven* palette colors (a heatmap, a
user-supplied colormap, a hash-distributed accent rotation), see the
[Data-driven colors](#data-driven-colors) section instead.

## Prerequisites

- The widget's package is in the Go AST scan range of
  [`scripts/ci/lint.sh`](../../../scripts/ci/lint.sh)'s `designlint`
  step (anything under `public/...`).
- The widget can import
  `github.com/stergiotis/boxer/public/keelson/designsystem/styletokens`.
  Per [ADR-0035](../../adr/0035-keelson-namespace-introduction.md),
  widgets in `thestack/` and runtime packages in `keelson/runtime/`
  may both depend on `keelson/designsystem/`; the reverse is not
  permitted.
- Familiarity with the four IDS color surfaces:
  the semantic palette ([ADR-0031 §SD2](../../adr/0031-imzero2-design-system-color.md)
  — six roles × three emphasis), the neutral spine (§SD4 — ten dark-theme
  L points), the data-encoding LUTs (§SD3 — Crameri + viridis-family),
  and the bridge sentinels (`color.Transparent`, `color.FromImage`).

## The recipe at a glance

1. Run `designlint` to see the current L2 findings for your file.
2. For each call site, decide which IDS surface owns the color:
   *semantic*, *neutral spine*, *data-encoding*, or *sentinel*.
3. Pick the matching token from the cheat sheet below.
4. Replace `color.RGB(...)` / `color.RGBA(...)` with
   `color.Hex(styletokens.<Token>.AsHex())` — the bridge that satisfies
   L2 without violating the keelson/thestack layering.
5. Update the per-call doc comment to cite the IDS role and link to
   ADR-0031 if the migration is non-obvious.
6. Re-run `designlint` to confirm the file is clean; run any existing
   widget tests; commit.

## Steps

### 1. Inventory the current L2 violations

Build the lint binary into a tempfile and run it through
`go vet -vettool=` so the loader threads build tags correctly
(see [`feedback_multichecker_tags_deprecated`] —
`multichecker.Main`'s own `-tags` flag is a no-op and produces
misleading load-error output otherwise):

```bash
tags="$(cat tags | tr -d '\n')"
tmpbin=$(mktemp -t designlint.XXXXXX)
go build -tags "$tags" -o "$tmpbin" ./public/keelson/designsystem/lint/cmd/designlint
go vet -vettool="$tmpbin" -tags "$tags" \
    ./public/thestack/imzero2/egui2/widgets/<your-widget>/...
rm -f "$tmpbin"
```

Each finding names the call site and the rule's remediation hint.

### 2. Classify each call site

Match the use case against this cheat sheet — sourced from the M5
backfill arc (commits `60703d41` through `888b5374`). When in doubt,
pick the row whose **Use case** description most matches your call site
and follow the **Token** column.

#### Sentinels (no semantic role)

| Use case | Token | Form at call site |
|---|---|---|
| Fully-transparent placeholder (no fill, no stroke) | `color.Transparent` | `color.Transparent` or `color.Transparent.Keep()` |
| Bridge from `image/color.Color` (runtime palette, colormap index) | `color.FromImage(src)` | `color.FromImage(myPaletteEntry).Keep()` |

#### Semantic palette ([ADR-0031 §SD2](../../adr/0031-imzero2-design-system-color.md))

For status-tinted UI affordances. Six roles, three emphasis levels each.

| Use case | Token (`styletokens.*`) |
|---|---|
| Informational text, links, neutral attention | `InfoDefault` |
| Success / healthy / valid state | `SuccessDefault` |
| Caution / stale / degraded | `WarningDefault` |
| Failure / error / destructive | `ErrorDefault` |
| Disconnected / idle / "no opinion" status | `NeutralDefault` |
| Selected / focused / branded highlight | `AccentDefault` |
| Quiet tinted background under a status row | `<role>.Subtle` (L≈0.20 dark) |
| Emphasised state, hover-active tint | `<role>.Strong` (L≈0.90 lighter) |
| High-contrast dark text on `<role>.Default` fill | `NeutralBgExtreme` |
| Tone-coloured text on `<role>.Subtle` fill | `<role>.Default` or `<role>.Strong` |

**Common mistake — picking the wrong role.** Don't reach for `Accent`
to communicate state (`accent` is reserved for selection and brand
highlights per ADR-0031 §SD2); don't use a semantic role for a chart
series identity (use data-encoding instead). The
[status-and-legends pattern doc](../patterns/status-and-legends.md)
catalogues the canonical state → role mapping for recurring
vocabularies (connection / data-freshness / validation / long-running
task / process state).

#### Neutral spine ([ADR-0031 §SD4](../../adr/0031-imzero2-design-system-color.md))

For chrome: panel surfaces, borders, text. Dark-theme only in v1.

| Use case | Token (`styletokens.*`) | Approx L |
|---|---|---|
| Modal scrim base | `NeutralBgExtreme` | 0.06 |
| Default panel fill | `NeutralBgPanel` | 0.13 |
| Alternating-row faint surface | `NeutralBgFaint` | 0.17 |
| Raised card / surface over a panel | `NeutralBgSurface` | 0.20 |
| Subtle divider | `NeutralBorderFaint` | 0.26 |
| Standard border / window stroke | `NeutralBorderDefault` | 0.58 |
| Disabled text | `NeutralTextDisabled` | 0.50 |
| Caption / secondary text | `NeutralTextSecondary` | 0.78 |
| Body text | `NeutralTextPrimary` | 0.95 |
| Display / strong-emphasis text | `NeutralTextExtreme` | 0.98 |

#### Data-driven colors

For palette-indexed series, heatmap cells, magnitude-shaded tables, or
any color that maps a runtime value into a perceptually-uniform ramp.
Mirrors the Rust `style::tokens::sequential` / `diverging` /
`qualitative_cycle` accessors landed in ADR-0033 §M0b.

| Use case | Accessor | Return |
|---|---|---|
| Series identity (categorical) | `styletokens.QualitativeCycle(idx)` | wraps mod 10 over BatlowS |
| Ordered magnitude (heatmap, density) | `styletokens.Sequential(palette, t)` | clamped t ∈ [0, 1] |
| Signed deviation from baseline | `styletokens.Diverging(palette, t)` | clamped t ∈ [-1, 1] |
| Discrete N-tier sample of a sequential palette | hand-roll a `sample8`-style helper that loops over `Sequential(p, i/(N-1))` | see treemap's `palettes.go` |

Defaults per [ADR-0031 §SD3](../../adr/0031-imzero2-design-system-color.md):
`SequentialBatlow` for sequential, `DivergingVik` for diverging,
`QualitativeCycle` always reads BatlowS. Matplotlib alternates
(`SequentialViridis` / `SequentialMagma` / `SequentialPlasma` /
`SequentialInferno`) are available for callers who explicitly want
that visual character.

### 3. Add the styletokens import

Insert into the import block alphabetically among the keelson
imports (Go's standard third-party position; keelson sorts before
thestack alphabetically):

```go
import (
    // ... existing stdlib + third-party ...

    "github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
    c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
    "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)
```

The widget still imports `color` for the `Color` type and the `Hex` /
`Transparent` / `FromImage` helpers; styletokens supplies the IDS
palette source values.

### 4. Rewrite each call site through the bridge

The canonical pattern is `color.Hex(styletokens.<Token>.AsHex())`.
`AsHex()` packs the `RGBA8` token (R/G/B/A bytes) into a
`0xRRGGBBAA` `uint32` that `color.Hex` consumes — neither call trips
`designlint` L2 (which only flags `color.RGB` / `color.RGBA`
constructors).

**Replacing a status-tinted fill:**

```go
// before
warnFill := color.RGB(0xf5, 0x9e, 0x0b)

// after
warnFill := color.Hex(styletokens.WarningDefault.AsHex())
```

**Replacing a pre-built var block (typical for widget-level palettes):**

```go
// before
var (
    rowTintWarn  = color.RGBA(0xf5, 0x9e, 0x0b, 0x22)
    rowTintError = color.RGBA(0xef, 0x44, 0x44, 0x33)
    detailErrorFg = color.RGB(0xff, 0xb4, 0xb4)
)

// after
var (
    rowTintWarn   = color.Hex(styletokens.WarningSubtle.AsHex())
    rowTintError  = color.Hex(styletokens.ErrorSubtle.AsHex())
    detailErrorFg = color.Hex(styletokens.ErrorDefault.AsHex())
)
```

Note the alpha-based tinting (`0x22` / `0x33`) drops out — the IDS
`Subtle` emphasis is already low-saturation L≈0.20 so the row reads as a
wash at full opacity.

**Replacing a fully-transparent placeholder:**

```go
// before
transparentBg := color.RGBA(0, 0, 0, 0)

// after
transparentBg := color.Transparent
```

(The 19-site transparent-placeholder backfill landed in commit
`60703d41`; `color.Transparent` is the sentinel that replaced
all of them.)

**Replacing a `Selected` / `Solid` variant's fg-on-fill pair:**

The pre-IDS recipe was usually `white-on-tone` (e.g., white text on
red error fill). The IDS palette has uniformly light `Default` tones
(L≈0.80), so the correct fg is *dark*, not white — `NeutralBgExtreme`
reads at high APCA contrast across all roles:

```go
// before
errFg := color.RGB(255, 255, 255)
errBg := color.RGB(200, 0, 0)

// after
errFg := color.Hex(styletokens.NeutralBgExtreme.AsHex())
errBg := color.Hex(styletokens.ErrorDefault.AsHex())
```

This recipe came out of the badge migration (commit `8e5d40f3`) and
is reused by the regex_explorer compile-error label (`888b5374`) and
elsewhere.

### 5. Update doc comments

The pre-IDS comments often described the *visual character* of the
colors (e.g., "yellow highlighter pen", "soft red", "Tailwind 500/600
family"). Rewrite them to cite the IDS role + ADR section. This makes
the intent durable across future palette tweaks (the colors will
change; the role won't).

Example (from the markdown widget, `85cb26d4`):

```go
// before
// Inline highlight palette — yellow "highlighter pen" background with a
// dark foreground, picked to read against egui's default visuals.
var (
    highlightFg = color.RGB(20, 20, 20)
    highlightBg = color.RGB(245, 230, 100)
)

// after
// Inline highlight palette — "highlighter pen" effect using the IDS
// accent role (ADR-0031 §SD2 reserves accent for "branded highlights,
// selection, focus rings"). AccentDefault is the bright L≈0.80
// highlight area; NeutralBgExtreme is the high-contrast dark text.
var (
    highlightFg = color.Hex(styletokens.NeutralBgExtreme.AsHex())
    highlightBg = color.Hex(styletokens.AccentDefault.AsHex())
)
```

**Important attribution check** ([`feedback_palette_attribution_check`]):
verify the original hex values against the canonical 500-tier anchors
before writing "Tailwind" / "Material" / etc. in the migration comment.
Tailwind blue-500 is exactly `#3b82f6`; Material green-500 is exactly
`#4caf50`; matplotlib viridis starts at `#440154`. If the value
doesn't match any standard anchor exactly, prefer "hand-picked tone"
or "near-Tailwind" rather than asserting a source.

### 6. Verify

Re-run designlint and any widget tests:

```bash
tags="$(cat tags | tr -d '\n')"

# Designlint should report zero L2 in the file:
tmpbin=$(mktemp -t designlint.XXXXXX)
go build -tags "$tags" -o "$tmpbin" ./public/keelson/designsystem/lint/cmd/designlint
go vet -vettool="$tmpbin" -tags "$tags" \
    ./public/thestack/imzero2/egui2/widgets/<your-widget>/...
rm -f "$tmpbin"

# Widget tests (if any) — token replacements should be byte-identical
# for matching tokens; visual character may shift modestly when the
# IDS palette differs from the pre-IDS values.
go test -tags "$tags" ./public/thestack/imzero2/egui2/widgets/<your-widget>/...
```

### 7. Commit

Conventional commit; one widget (or one tight cluster of related
widgets) per commit. Reference [ADR-0031](../../adr/0031-imzero2-design-system-color.md)
in the subject and document the per-token mapping in the body. The
M5 backfill commits (`60703d41` through `888b5374`) are precedent for
the body shape.

```
refactor(<widget>): migrate <surface> to IDS semantic palette (ADR-0031)

  <before-token>  <hex>  → <styletokens.Token>  <role.emphasis>
  ...

  <character notes — visual shifts callers will see>

  After: 0 L2 findings in <file> (was N). Fleet count: M → M-N.
```

## Verification (end-to-end)

You can confirm the recipe is well-applied across the fleet by running
the full lint sweep — designlint shows up as a `warn` step:

```bash
bash scripts/ci/lint.sh
```

Expected: the `designlint` summary line shows `warn` (or `pass` once
the fleet count reaches zero) with no L2 findings in the file you
migrated. Existing widget tests pass.

For visual confirmation, the carousel demo's `idsshowcase` app
(commit `8c4ddaec`) is the canonical IDS token catalogue and shows
every semantic role and emphasis side-by-side. Launch via
[`rust/imzero2/hmi.sh`](../../../rust/imzero2/hmi.sh) to see your migrated
widget alongside the catalogue.

## Troubleshooting

- **Symptom:** `designlint` reports L2 findings I'm sure I migrated.
  **Cause:** the file may not be in the build (e.g., a build tag
  doesn't match) or `multichecker.Main`'s deprecated `-tags` flag is
  silently no-opping.
  **Fix:** use `go vet -vettool=` per step 1, not direct
  `go run ./public/keelson/designsystem/lint/cmd/designlint`. See
  [`feedback_multichecker_tags_deprecated`].

- **Symptom:** Build fails with `cannot use color.Color as ...` after
  the migration.
  **Cause:** the call site needed `.Keep()` for the retained-Color32
  transport ([ADR-0052](../../adr/0052-imzero2-unified-color-type.md))
  but I dropped it when rewriting.
  **Fix:** `.Keep()` works on the bridge: `color.Hex(styletokens.X.AsHex()).Keep()`.
  Re-check the original call site for `.Keep()` and preserve it.

- **Symptom:** `styletokens` import added but not used after the
  rewrite.
  **Cause:** only some `color.RGB` calls in the file qualify for
  migration; the rest are computational (channel arithmetic) or
  bridging from a runtime source.
  **Fix:** for computational packing, use `color.Hex(packed)` with
  inline bit-shifting (the treemap `deriveCellColors` precedent in
  commit `dcb31c59`); for runtime-supplied colors, use
  `color.FromImage(src)`. Drop the unused styletokens import if all
  call sites turn out to be one of these two cases.

- **Symptom:** APCA contrast looks lower after the swap.
  **Cause:** the pre-IDS `fgOnSolid` may have been white on a
  hot-saturated red (high contrast but visually aggressive); the IDS
  `ErrorDefault` is a lighter L≈0.80 coral, so white-on-fill would
  fall below the APCA floor. The correct fg is `NeutralBgExtreme`
  (near-black) — high APCA contrast on a light tint.
  **Fix:** for `Solid` variants where the original was white-on-tone,
  use `NeutralBgExtreme` as the fg, not white / `NeutralTextExtreme`.
  This is the badge `fgOnSolid` recipe from commit `8e5d40f3`.

- **Symptom:** Visual character of the widget shifted noticeably.
  **Expected.** Per [ADR-0029 §SD14 M5](../../adr/0029-imzero2-design-system-and-policy-as-code.md),
  fleet-wide migration is the explicit goal — visual cohesion across
  the fleet trumps per-widget character preservation. The Crameri /
  viridis defaults and the Swiss-restrained C≈0.10 chroma are
  deliberate. If a widget really needs to deviate (a specific brand
  affinity, a regulated color requirement), that's a Tier 3 escalation
  per [tier3-human-review.md](../policy/tier3-human-review.md).

## Further reading

- [ADR-0029 — design system + policy-as-code](../../adr/0029-imzero2-design-system-and-policy-as-code.md) — §SD8 (mechanical lints) and §SD14 (M5 backfill milestone) frame this recipe.
- [ADR-0031 — color foundations](../../adr/0031-imzero2-design-system-color.md) — §SD2 (semantic palette), §SD3 (data-encoding), §SD4 (neutral spine), §SD6 (egui::Visuals binding).
- [ADR-0033 — palette M0](../../adr/0033-imzero2-design-system-palette-m0.md) — §M0b amendment lists the executed sRGB hex values per token.
- [ADR-0035 — keelson namespace](../../adr/0035-keelson-namespace-introduction.md) — the layering reason why `styletokens` lives in keelson and the bridge goes through `color.Hex(token.AsHex())` rather than `styletokens.AsColor(token)`.
- [tier1-mechanical.md](../policy/tier1-mechanical.md) — full L2 catalogue + the shipped-vs-deferred lint status table.
- [patterns/status-and-legends.md](../patterns/status-and-legends.md) — canonical state → semantic-role mapping for recurring vocabularies.
- [patterns/tables.md](../patterns/tables.md) — table-cell typography + color conventions; magnitude shading uses the data-encoding palettes.
- [patterns/plots.md](../patterns/plots.md) — series colors via `QualitativeCycle`; heatmap via `Sequential`; annotation via semantic palette.
- [`color.Transparent`](../../../public/thestack/imzero2/egui2/widgets/color/color.go) and [`color.FromImage`](../../../public/thestack/imzero2/egui2/widgets/color/color.go) — the two L2-quiet bridge sentinels added during M5.
- [`feedback_multichecker_tags_deprecated`] — why step 1 uses `go vet -vettool=`.
- [`feedback_palette_attribution_check`] — verify hex anchors before claiming the source palette in commit messages.
