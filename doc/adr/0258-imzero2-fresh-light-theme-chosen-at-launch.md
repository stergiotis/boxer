---
type: adr
status: proposed
date: 2026-09-24
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0258: imzero2 — a light theme, `fresh`, chosen at launch

## Context

The IDS is dark-only by decision: [ADR-0031](./0031-imzero2-design-system-color.md)
deferred a light theme "until a real daylight-office complaint surfaces"
and shipped no theme parameter, no second lightness spine and no
switching surface. Three things have changed since.

- There is a want for a second look that is light, friendly and a little
  playful, and still recognisably this system rather than stock egui.
- Go-side widgets never read `egui::Visuals`. They resolve colours from
  `styletokens`, the Go mirror of the palette (the
  [ADR-0035](./0035-keelson-namespace-introduction.md) layering rule keeps
  that package from importing the widget colour type), some of them at
  package init. A theme that only changed the Rust overlay would
  leave every custom widget dark inside light chrome.
- The colour generator ([ADR-0033](./0033-imzero2-design-system-palette-m0.md))
  already carries the machinery a second palette needs — OKLCh source,
  gamut clipping, the APCA gate over `pairs.toml`, CVD and IP-boundary
  checks, byte-compared artefacts in CI — but reads exactly one source.

Two egui facts shaped the overlay and are worth recording, because both
were found by looking at captures rather than at code:

- `Visuals::strong_text_color()` *is* `widgets.active.fg_stroke.color`.
  A pressed control's text colour is also every `RichText::strong`
  heading. On a light theme, light text on a pressed accent fill makes
  every bold heading vanish.
- Two colours are bound at construction time and never read from
  `Visuals`: the progress bar's fill and the etable selection stripe,
  both pinned to `ACCENT_DEFAULT` by the IDL for the reasons
  [ADR-0037](./0037-imzero2-design-system-palette-m1-refinement.md)
  gives. A theme that changes the palette without reaching them shows the
  other theme's accent in two places.

## Decision

### SD1 — A theme is chosen once, at launch, by `IMZERO2_THEME`

`IMZERO2_THEME=fresh` selects the light theme; unset, or anything else,
is the IDS dark palette. Both sides read the variable on their own — the
Rust overlay when it first needs it, the Go token package in its `init`
— so no opcode carries the choice and the two halves cannot disagree.

There is **no runtime switch**, unlike density
([ADR-0032 §SD1](./0032-imzero2-design-system-spacing-density-motion.md)).
Density can be re-applied because every consumer resolves spacing per
frame. Colour cannot: a widget may copy a palette token into a
package-level variable at init, and the code highlighters intern retained
colour holders at init, so a later switch would leave those consumers on
the old theme with nothing to tell them. Resolving the theme in the token
package's `init` is what makes the init-time copies *correct*: Go
initialises a package before any importer's initialisation runs, so the
palette variables are already the theme's by the time a widget copies
one. A runtime switch is a follow-on ADR that has to add a cache
invalidation contract first.

### SD2 — The theme's palette is a second source of truth under the same gate

`rust/imzero2/assets/colors/palette-fresh.toml` has the schema of
`palette.toml` and is emitted by the same generator into a Rust module
of the same constant names, a Go apply-function over the same variables,
and a colour page of its own. It is graded against the same `pairs.toml`,
so the APCA gate holds for both palettes or the generator fails, and its
emitted files are byte-compared by the CI drift check like the IDS ones.
The generator's theme table is where a third theme would be added.

The spine is one hue at ten lightnesses — a cool-tinted paper for panels,
a near-white step for windows and cards, a blue-black ink of the same hue
for text — so the tint and the ink belong together. Role emphasis inverts
against the dark palette: *subtle* is a tint that carries dark text,
*default* a mid tone that carries light text, *strong* the darker
foreground on a tint. The accent is orchid, at least 55° from every
status hue, so a hover ring or a selection never reads as a status.

### SD3 — What the overlay decides, and what it leaves to the IDS

The `fresh` overlay replaces only the colour, rounding, stroke and shadow
bindings of the IDS overlay. Spacing, the type scale, fonts and the
data-encoding palettes are shared, as is the density re-apply path, which
keeps whatever theme the process started with.

Its own decisions: widgets round at 8 px and windows and menus at 12 px;
windows and popups carry a hard offset shadow with no blur, which is the
one mark meant to be recognisable from across the room; sliders fill
their trailing rail in the accent tint; and, because of the strong-text
coupling above, the pressed state keeps ink text and is told by a deeper
accent tint and a stronger ring instead of by inverting.

### SD4 — Construction-time colours go through one theme-aware accessor

The two IDL sites that bind `ACCENT_DEFAULT` at construction, and the
slider rail override of ADR-0031's 2026-09-20 update, take their colour
from `style::accent_default()` / `style::slider_rail()`, which pick by the
active theme. Any future construction-time colour in the IDL goes through
the same door rather than naming a palette module.

### SD5 — Dark-tuned literals in widgets move onto tokens

A widget that had a hard-coded dark canvas keeps looking dark under any
theme. Those found in the gallery moved onto spine tokens (the animation
demo's canvas, the capability inspector's activity strip, the world map's
no-data fill), which also leaves the dark palette pixel-identical. The
code highlighters keep their dark-tuned per-language palettes and, under
the light theme, cap each colour's OKLab lightness so the hues survive
the flip; a hand-tuned light palette per language is a later refinement.

## Alternatives considered

- **Runtime switching now, mirroring density.** Rejected for this ADR:
  correct only with an invalidation contract for every init-time copy
  (SD1), which is its own design. The launch-time choice loses nothing
  the density precedent has, because density never needed the contract.
- **One palette, inverted arithmetically for the light theme.** Rejected:
  inverting L does not preserve APCA, which is asymmetric between polarities,
  and the role emphasis levels mean different things on the two spines
  (SD2). A second source graded by the same pairs is cheaper than a
  transform plus a proof.
- **egui's `Visuals::light()` as the light theme.** Rejected: it has no
  identity, and it reaches none of the Go-side widgets (Context).
- **Keeping the theme as an uncommitted experiment.** Rejected once the
  palette had to clear the gate: the value of the generator is that the
  gate runs in CI, which it only does for a committed source.

## Consequences

- Apps get a light look for free by setting one variable, and the tours
  capture under either theme with the same commands.
- Two palettes to keep in step. A new token is added to both files, and
  a new pair in `pairs.toml` gates both; the generator fails loudly on
  either.
- Advisory findings the light palette carries, recorded so they are not
  rediscovered: its faint dividers sit at about 2.3:1 against panel and
  surface, the same class of deliberate exception the dark palette makes
  for that token; its pale role tints are close to each other under CVD
  simulation, as the dark palette's are; and Okabe-Ito's yellow is weak
  on a white plot background.
- Go-side rounding and stroke are constants, so Go-drawn cards keep the
  IDS corner radii while egui widgets round more. A tokenised runtime
  radius is a follow-on.
- No light CSS target: [ADR-0076](./0076-ids-lean-classless-css-target.md)'s
  custom properties stay the dark palette's until that target wants a
  theme of its own.

## Status

Proposed. Built and captured behind `IMZERO2_THEME=fresh`: the widgets
gallery tour under both themes, and the play scene tour under both, with
identical pass and skip sets.
