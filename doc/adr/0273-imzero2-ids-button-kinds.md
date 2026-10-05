---
type: adr
status: proposed
date: 2026-10-02
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0273: imzero2 — button kinds named by role, coloured from the IDS palette

## Context

Every imzero2 button is drawn the same way: the IDS overlay's
`widgets.*` visuals ([ADR-0031](./0031-imzero2-design-system-color.md)),
a neutral fill with an accent ring on hover. A view has no way to say
which of its actions is the main one, or which one destroys something.
Web toolkits answer this with a small set of named button kinds; the
names used here follow the Carbon set (primary, secondary, tertiary,
ghost, danger).

Two things constrain the answer:

- egui's `Button` takes its fill, stroke and text colour from
  `widgets.{inactive,hovered,active}` for the state it is in. Its own
  `fill()` / `stroke()` builders set one value for every state, so a
  button coloured through them gives no hover or press feedback.
- The two themes ([ADR-0258](./0258-imzero2-fresh-light-theme-chosen-at-launch.md))
  disagree about which emphasis clears 4.5:1 under text on a fill. On
  the dark spine, text in `bg.extreme` on `accent.default` is about 11:1.
  On `fresh` the same pair is about 4.2:1, which fails the WCAG
  body-text threshold; `accent.strong` gives about 6.6:1.

## Decision

### SD1 — Six kinds, named by role

`ButtonKindE` in the Go bindings: `Secondary` (the zero value),
`Primary`, `Tertiary`, `Ghost`, `Danger`, `DangerGhost`. A call site
names the role, `c.Button(id, atoms).Kind(c.ButtonKindPrimary)`, and
the client chooses the colours. Badge's two-axis model (tone ×
variant) was considered and not taken: names that say what a button is
for are harder to misuse than 24 combinations of how it looks.

The zero value is the button the theme already draws, so every existing
button is a secondary one and no call site changes.

### SD2 — The client colours the button for its own call

The `button` IDL node has an unexported `kind` method; the hand-written
`Kind(ButtonKindE)` wraps it. The client adds the button through
`imzero2_egui::style::button::IdsButton`, which writes the kind's
colours into the three `widgets` state slots and the cleared
`override_text_color` for the button's own `ui` call, then puts them
back, as `IdsSlider` does for the rail. Stroke widths, corner radii and
the hover expansion stay the theme's. The two ghost kinds also turn off
the frame at rest.

Go names the role and sends no colours, so a kind follows the theme the
client runs and costs one byte on the wire.

### SD3 — One colour table per theme, held to contrast by tests

Each theme has its own table of fill, stroke and text per kind and
state, built only from palette tokens (`button.rs`, `dark` and
`fresh`). A shared rule over the role tokens does not work, because of
the contrast gap described in Context. The tests check, for both themes:

- text is at least 4.5:1 against the kind's fill, or against the panel
  and window grounds where the fill is clear;
- a filled kind's rest fill, and the tertiary outline, are at least 3:1
  against both grounds (WCAG 1.4.11);
- the override is gone once the button has been added.

Press and keyboard focus share egui's `active` slot. Every kind draws
a `text.extreme` ring in that slot, so a focused filled button can be
told from a hovered one.

`ButtonKindE` and the Rust `KIND_*` constants are compared by a Go test
that reads `button.rs`.

### SD4 — A screenshot tour keeps its neutral strokes

`apply_tour_neutral_overrides` collapses hover and press strokes onto
the rest stroke so a capture does not depend on where the cursor is.
`IdsButton` does the same for the kinds while that override is active.

## Alternatives considered

- **Tone × emphasis, as badge has.** Rejected: 24 combinations that
  name how a button looks, where a call site needs to say what it is
  for. A role a kind does not cover is a new kind (Deferred).
- **A Go-only widget over `Button::fill` / `stroke`.** Rejected: those
  set one colour for every state, so hover and press would show no
  colour change (Context).
- **Sending colours from Go.** Rejected: the client already knows the
  theme and the state, and Go knows neither until a frame later.

## Consequences

- A kind's text colour applies to plain text atoms. A rich-text atom
  with its own colour keeps that colour, and on a filled kind that
  colour may not be readable.
- On `fresh`, a hovered primary button keeps its fill and shows the
  hover only through the ring and the expansion, because no accent
  token darker than `strong` exists. Danger does change fill on hover,
  because `error.default` already clears 4.5:1.
- Secondary is the overlay's ordinary button rather than a
  `neutral.subtle` fill. On the dark spine `neutral.subtle` is
  indistinguishable from the window fill.
- `selected(true)` still uses the selection colours, whatever the kind.

## Deferred

- Kinds for the success, warning and info roles. No call site has asked
  for them; adding one is a row in each theme table and a constant.
- Go widgets that compose buttons (dialogs, the filepicker footer) do
  not use the kinds yet. Each adoption is a one-line change at its call
  site.

## Status

Proposed. Built behind `ButtonFluid.Kind`; the IDS showcase has a row of
one button per kind, captured at rest and on hover under both themes by
a headless scene.
