---
type: explanation
audience: IDS maintainers and contributors editing `imzero2_egui::style::tokens::visuals` or adding widgets that read `selection.bg_fill` / `widget_visuals.inactive.bg_fill`
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# egui `Visuals` field coupling — why some IDS widgets need per-call overrides

A handful of `egui::Visuals` fields are shared across multiple widgets that have *different* contrast requirements. IDS sets each field once at startup (per [ADR-0029 §SD2](../../adr/0029-imzero2-design-system-and-policy-as-code.md)), so when two widgets compete for the same field, only one of them gets the value it wants — the other has to be patched at the call site or accept a structural limitation.

This document captures the two collisions we've found so a future contributor doesn't re-derive them from `egui-0.34.2/src/widgets/*.rs`.

## Background

`apply_visuals` in `rust/imzero2/imzero2_egui/src/style/tokens/visuals.rs` writes IDS tokens into `egui::Visuals` once, at startup. egui then reads those fields whenever a widget paints itself. There are no per-widget style scopes in the IDS surface today — only the values pre-loaded in `Visuals` plus whatever a Go caller hands in via a builder method (e.g. `c.ProgressBar(...).Fill(col)`).

When two widgets read the *same* `Visuals` field for *different* visual roles, the IDS token assigned to that field can only satisfy one of them.

## Collision 1: `selection.bg_fill` — ProgressBar fill vs SelectableLabel selected background

**Who reads it:**

- `egui::SelectableLabel` paints its selected background with `selection.bg_fill`. Text (`text_color()` = `NEUTRAL_TEXT_PRIMARY`, L ≈ 0.93) sits on top.
- `egui::ProgressBar` (`progress_bar.rs:165`) paints the filled portion of the bar with `fill.unwrap_or(visuals.selection.bg_fill)`. The track (unfilled) uses `visuals.extreme_bg_color` (`NEUTRAL_BG_EXTREME`, L ≈ 0.06).
- `egui::Slider` trailing fill (`slider.rs:808`), only when `slider_trailing_fill = true` — disabled in IDS by default.

**What each role wants:**

- SelectableLabel needs `selection.bg_fill` *low* L so high-L text reads against it (APCA-safe contrast on a dark theme).
- ProgressBar needs `selection.bg_fill` *high* L so the bar reads against `extreme_bg_color` at L ≈ 0.06.

These two goals are arithmetically opposed. IDS resolves the conflict in SelectableLabel's favour: per [ADR-0037](../../adr/0037-imzero2-design-system-palette-m1-refinement.md) `selection.bg_fill = ACCENT_SUBTLE` (L = 0.20). The egui default for the same field is `Color32::from_rgb(0, 92, 128)` (L ≈ 0.40, sRGB `#005c80`) — readable as a progress fill against gray-10 but unreadable as a SelectableLabel background.

The ProgressBar side is patched at the IDS binding layer: the `progressBar` factory in `public/thestack/imzero2/egui2/definition/egui2_definition_d_widgets.go` emits construction code

```rust
egui::ProgressBar::new(progress).fill(imzero2_egui::style::tokens::palette_generated::ACCENT_DEFAULT)
```

so every progress bar starts with the bright fill `ACCENT_DEFAULT` (L = 0.80) regardless of what `selection.bg_fill` carries. Explicit `.Fill(col)` from Go still overrides because `egui::ProgressBar::fill` is idempotent — last call wins. Per-metric overrides like `apps/imztop/imztop_panel_mem.go`'s `colorMemFill` continue to work unchanged.

The Slider trailing fill is a latent third reader. It stays unaffected today because IDS does not enable `slider_trailing_fill`; enabling it would draw the trailing portion at L ≈ 0.20 against the L ≈ 0.24 rail — a ΔL ≈ 0.04 strip that's effectively invisible. If someone wants a trailing-fill slider, they would need a separate per-call override mechanism (none exists today).

## Collision 2: `widget_visuals.inactive.bg_fill` — Slider rail vs Slider handle

**Who reads it inside `egui::Slider::paint`:**

- Rail (`slider.rs:781`): `rect_filled(..., widget_visuals.inactive.bg_fill)`.
- Handle (`slider.rs:822`): `CircleShape { fill: interact().bg_fill, ... }`. At rest, `interact()` returns the `inactive` `WidgetVisuals`, so the handle reads the *same* `inactive.bg_fill`.

This is not an IDS choice — it is an egui-upstream binding. The default egui dark theme has the same property: `inactive.bg_fill` = `Color32::from_gray(60)` (L ≈ 0.30), used for both rail and handle. The handle is distinguished only by `inactive.fg_stroke` (1.0 px of `Color32::from_gray(180)`, L ≈ 0.55).

IDS does not make this worse. The rail/handle fill is `NEUTRAL_BG_SURFACE` (L = 0.24), only slightly darker than upstream's L ≈ 0.30, and IDS *improves* handle visibility by bumping `inactive.fg_stroke.color` to `NEUTRAL_TEXT_PRIMARY` (L = 0.93) — the outline reads at ΔL ≈ 0.69 against the rail, versus ΔL ≈ 0.25 in upstream egui.

There is no Visuals token that decouples `inactive.bg_fill` for rail-only or handle-only use. Changing the token to brighten the handle bleeds into every other widget that uses `inactive.bg_fill` (buttons, checkboxes, dragvalues, comboboxes) and also brightens the rail, so the rail/handle equality survives.

Three workarounds, none of them merged today:

1. **Bump `widgets.inactive.fg_stroke.width`** from `HAIR` (1.0) to `REGULAR` (1.5). Cheap. Affects only widgets that draw `fg_stroke` as a *real* stroke — slider handle outline, custom widgets explicitly painting `fg_stroke`. Text rendering reads only the colour, not the width, so buttons/labels are unaffected.
2. **Enable `slider_trailing_fill = true` AND bump `selection.bg_fill`** to a high-L token. Re-introduces Collision 1: SelectableLabel selected text loses contrast.
3. **Per-Slider style scope.** Would need codegen support analogous to the `progressBar` construction-time `.fill(...)`: the slider factory would push a temporary `inactive.bg_fill` override into a `ui.scope` before rendering. No infrastructure exists today.

The decision (2026-05-20) was to leave the slider as-is — the upstream-shared outline-only differentiation is the egui dark-theme idiom, and the IDS-brightened outline already exceeds upstream's contrast.

## Invariants

- IDS sets each `egui::Visuals` field exactly once, at startup. Anything that needs a different value for a specific widget must override at the call site.
- `selection.bg_fill` lives at L = 0.20 (`ACCENT_SUBTLE`) because SelectableLabel's text-on-fill contrast is the load-bearing constraint. Any widget that paints with this field and needs higher L must override per-call.
- `widget_visuals.inactive.bg_fill` is read by both rail and handle in `egui::Slider`. Treat the rail/handle as a single visual unit when changing it.
- Construction-time `.fill(...)` on `ProgressBar` is overridable — `egui::ProgressBar::fill` is last-write-wins.

## Trade-offs

Single-pass `apply_visuals` keeps the design-system surface flat and statically inspectable — every widget pays the same startup cost, and the policy lint can read tokens without simulating render paths. The cost is that widget-specific overrides have to happen either at the binding layer (construction-time injection, like ProgressBar) or at the Go call site (explicit method, like `imztop`'s per-metric `Fill(...)`). Adding a per-call style scope mechanism would unblock case-by-case overrides at the cost of significant codegen complexity.

## Further reading

- [ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md) — IDS startup-token policy (§SD2).
- [ADR-0037](../../adr/0037-imzero2-design-system-palette-m1-refinement.md) — the `selection.bg_fill = ACCENT_SUBTLE` decision.
- [ADR-0040](../../adr/0040-imzero2-design-system-palette-consolidated.md) — current palette state.
- `egui-0.34.2/src/widgets/progress_bar.rs:146,165` — fill site.
- `egui-0.34.2/src/widgets/slider.rs:781,808,822` — rail / trailing / handle sites.
- `rust/imzero2/imzero2_egui/src/style/tokens/visuals.rs` — IDS token assignments.
- `public/thestack/imzero2/egui2/definition/egui2_definition_d_widgets.go` — `progressBar` factory with the construction-time `.fill(...)`.
