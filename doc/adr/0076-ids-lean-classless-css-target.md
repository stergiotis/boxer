---
type: adr
status: accepted
date: 2026-06-11
reviewed-by: "@spx"
reviewed-date: 2026-06-21
---

# ADR-0076: Lean classless CSS render target for IDS

## Context

IDS (the ImZero2 Design System, [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md))
was scoped from the start to a single renderer: its tokens are "materialised as
an `egui::Style` / `Visuals` overlay" (ADR-0029 §SD1), with a hand-mirrored Go
surface in `styletokens` for code that sits Go-side of the egui boundary. The
colour layer is generated — `palette.toml` → `colors/gen` → `colors/emit` emits
`palette_generated.rs` + `palette_generated.go` + two markdown specs ([ADR-0033](./0033-imzero2-design-system-palette-m0.md) §SD5).

A need surfaced that the egui-only framing does not cover: rendering **server-side
HTML multi-page pages** in the IDS look, with no egui runtime. The concrete first
consumer is the obsidian-markdown renderer
(`public/semistructured/markdown/obsidian`), which today ships its own
hand-written `static/obsidian.css` — an independent light theme on a `--ob-*`
variable contract, unrelated to IDS.

Two clarifications bound the scope:

- **"Lean", not "light".** The ask is a small, standalone, reusable stylesheet —
  *lean* in the sense of few moving parts, drop-in, no build step. It is **not** a
  light *colour* theme. IDS stays dark-only; a light colour polarity remains the
  deliberate deferral of [ADR-0031](./0031-imzero2-design-system-color.md)
  (Context fourth force, §O12) and is explicitly out of scope here.
- **The generator was not runnable in boxer.** `gen.go` read `src/rust/...`
  input paths and emitted `./src/go/cmd/...` provenance headers — the upstream
  pebble2impl layout. The committed `palette_generated.*` were imported
  artefacts; `doc/design-system/foundations/` did not exist. Honouring the
  "single source of truth" intent for a CSS colour target meant first making the
  generator run in-tree.

## Decision

Add a **lean, classless CSS render target** to IDS: the tokens gain a second
binding surface (CSS custom properties) alongside the egui one. Both are
consumers of the same token source; this extends, not supersedes, ADR-0029/0031.

### §SD1 — CSS as a fifth generated emit target (colours)

`emit.CssFile(tokens)` emits `ids-palette.css`: one `:root` block of kebab-case
`--ids-*` custom properties, neutral spine then semantic roles, in the same
deterministic order as the Rust/Go emitters. Values are the identical
gamut-clipped sRGB hexes — only the binding surface differs, so colours stay
**single-sourced** from `palette.toml`. Naming: the default emphasis drops its
suffix to read as the bare role (`--ids-accent`), with `--ids-<role>-subtle` /
`--ids-<role>-strong` for the variants; neutral-spine tokens drop the spine
prefix (`--ids-bg-panel`); `semantic.neutral.*` stays distinct as `--ids-neutral-*`.

### §SD2 — Localise the generator to run in-tree

`gen.go` input/output paths move to the boxer `rust/imzero2/...` layout;
`emit.GoFile` now emits the `//go:build llm_generated_opus47` constraint the
committed file carries, so regeneration is faithful. Re-running the generator
re-emits `palette_generated.{rs,go}` with **corrected provenance headers and
byte-identical RGB values** (verified: the only diff is the two header comment
lines), and materialises `color.md` + `ip-boundary-check.md` for the first time.
The APCA gate passes (0 failures); the standing WCAG/CVD advisories are
unchanged. Per [feedback_reconcile_imported_adrs], localising imported-artefact
paths in place is the boxer norm.

### §SD3 — Hand-author `ids.css` (scalars + classless base + obsidian coverage)

A second, hand-authored file completes the system:

- **Scalar tokens** — spacing / radius / stroke / type / motion as `--ids-*`
  custom properties, a mirror of the `styletokens` Go/Rust constants at
  **Standard density**. These ladders are *not* generated on the Go side either
  (they are hand-mirrored under a drift test, ADR-0032 §SD8); the CSS mirrors
  the same split rather than over-fitting the generator to read them.
- **Classless element base** — bare HTML (`body`, headings, `a`, lists,
  `table`, `code`/`pre`, form controls, `mark`, `del`, …) styled directly, so
  semantic markup needs no classes. `body` carries a centred reading column
  (matching the obsidian scaffold's own `max-width: 50em`).
- **Obsidian-markdown coverage** — every class the obsidian renderer emits
  (`wikilink`(`-broken`), `embed-image`/`embed-note`(`.embed-broken`), `tag`,
  `callout` + the full `callout-<type>` vocabulary, `callout-title`/`-content`,
  `frontmatter` + `dl`/`dt`/`dd`, `contains-task-list`/`task-list-item`),
  retargeted from `--ob-*` onto `--ids-*` in the dark aesthetic. Callout types
  map to IDS tones (note/info → info, tip/success → success, question/warning →
  warning, failure/danger → error, example → accent, quote → neutral). This
  makes `ids.css` a **drop-in replacement** for `static/obsidian.css`.

### §SD4 — Home, fonts, fidelity

Files live at `public/keelson/designsystem/web/` (`ids-palette.css` generated,
`ids.css` hand-authored; one `@import` chains them, one `<link>` consumes them).
Font stacks default to **system fonts** so the sheet is asset-free and
standalone; the IDS web-fonts (Iosevka Aile / IDS Mono / Phosphor — embedded
TTFs, Regular-only per ADR-0030 §SD7) are a deferred opt-in `@font-face` add-on.
A `prefers-reduced-motion` block zeroes transitions.

## Consequences

**Positive**

- IDS tokens reach the web; the obsidian renderer's output is themeable in the
  IDS look with no egui runtime.
- Colours remain single-sourced from `palette.toml`; the generator is now
  runnable in boxer and its provenance is corrected, with the two foundations
  markdown specs materialised for the first time.
- The classless shape means content pages need zero markup changes.

**Negative / caveats**

- **Not pixel-identical to egui.** CSS approximates the widgets; it shares
  tokens and feel, not immediate-mode rendering.
- **A second hand-mirror.** The scalar `:root` block duplicates the Go/Rust
  scalar constants. Surface is small (~25 values) and Standard-density only; a
  CSS↔Go drift test is the obvious follow-on (see Status).
- **Dark-only, system-font-only, Standard-density-only** in this cut. The
  light-*colour* polarity stays parked at ADR-0031; the IDS web-fonts and a
  `[data-density]` switch are deferred.
- `ids.css` is an *alternative* stylesheet; the obsidian package still embeds
  its own `obsidian.css` default. Wiring `ids.css` in as a selectable
  stylesheet is left to the consumer / a follow-on.

## Alternatives considered

- **Hand-author the palette CSS** from the committed constants. Rejected:
  forfeits the single source of truth; drift between egui and web colours.
- **Re-map `--ob-*` onto `--ids-*` and reuse `obsidian.css`.** Rejected: not
  standalone, and couples IDS to the obsidian package's variable contract.
- **A full component / utility framework** (buttons, cards, grid). Rejected:
  violates "lean"; the classless base + tone vocabulary suffice for MPAs.
- **An external pipeline** (Style Dictionary / W3C design tokens). Rejected:
  new dependency for what the in-house generator already does as one more emit.

## Status — open questions

1. CSS↔Go scalar-token drift test (mirror of `styletokens_drift_test.go`).
2. IDS web-fonts as an opt-in `ids-fonts.css` (`@font-face` + WOFF2; blocked on
   the ADR-0030 §SD7 subsetting / weight decision).
3. `[data-density]` Tight/Roomy switch over the spacing/type ladders.
4. Wiring `ids.css` into the obsidian renderer — **done**: selectable via
   `obsidian.Stylesheet(obsidian.StylesheetIDS)`, backed by the
   `keelson/designsystem/web` embed package (`web.Stylesheet()` folds the
   `@import` in for inlining; `web.FS()` serves the linked two-file form).
   Making it the renderer's *default* stylesheet remains open.
5. Light-*colour* polarity remains an ADR-0031 deferral, unaffected by this ADR.
