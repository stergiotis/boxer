---
type: adr
status: accepted
date: 2026-07-31
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-31
---

# ADR-0156: Qualitative data-encoding palette for a dark surface — Okabe-Ito, and a gate that enforces

## Context

[ADR-0031](./0031-imzero2-design-system-color.md) §SD3 adopted Crameri's
`batlowS` as the IDS qualitative palette and
[ADR-0033](./0033-imzero2-design-system-palette-m0.md) §SD4 fixed it to the
upstream first-10 subset. `styletokens.QualitativeCycle` has been the fleet's
single source of categorical colour since.

Four of those ten entries are illegible as a foreground on the IDS dark
spine. Measured against `NeutralBgSurface` (`#1d2021`, L\* 12.0) under WCAG
1.4.11's 3:1 floor for graphical objects, slots 0, 3, 6 and 9 read 1.00:1,
2.27:1, 1.56:1 and 1.89:1. Slot 0 is not low-contrast but *iso-luminant* with
the background, so no stroke width recovers it. The full measurement pass is
in
[the background-work page](../adr-background-work/qualitative-palette-dark-surface.md).

The palette is not defective. It is being used in a polarity it was not built
for. As a **fill** with dark text over it — which is how
`regex_explorer`'s per-capture-group cells use it — `batlowS` is fine. As a
**stroke, marker, glyph or text colour** on a dark surface, four of ten fail.
ADR-0031 §SD3 justified `batlowS` on the grounds that "each color sits at a
distinct L and hue, which means the palette degrades gracefully to
grayscale"; that distinct-L property *is* the defect on a fixed dark surface,
and the ADR named it as a feature without noticing the cost.

Three things made this worth an ADR rather than a patch:

- **It had already been worked around twice, silently.** `imztop` carried a
  fleet-invisible reshuffle (`markerColorOrder`) for its glyph markers;
  `codeview` carried a hand-picked four-colour set with the diagnosis
  written out in a comment. Neither escalated. A third site —
  `play`'s projection scatter — was a live user-visible bug, drawing its
  points at 1.00:1.
- **The design system's own shop window demonstrated the defect.** The
  `idsshowcase` data-encoding panel drew six series from slot 0 upward under
  the caption "QualitativeCycle drives series colors", with an invisible
  first series.
- **No gate would have caught it, and none would catch the next one.**
  ADR-0031 §SD5 states "WCAG AA mandatory" and then exempts the
  data-encoding palettes: "Pre-validated by publication […] no in-house
  re-verification." Publications validate CVD safety; they make no claim
  about contrast against one particular dark UI surface, because these
  palettes target print and light figure backgrounds.

A fourth fact emerged while designing the replacement gate and is recorded
here because it contradicts a reasonable reading of ADR-0031 §SD5: the
semantic palette's CVD check **does not enforce either**. `colors/gen`
collects ΔE ≤ 15 findings and prints them as advisory warnings; only APCA
gates the build. Measured, the semantic palette's own worst same-emphasis
pairs are ΔE 0.2–0.5 under deuteranopia. There was no enforcing CVD gate in
the tree to model a new one on.

## Decision

### SD1 — The qualitative cycle is Okabe-Ito, at seven entries

`styletokens.QualitativeCycle` (and its Rust mirror
`style::tokens::qualitative_cycle`) reads Okabe & Ito's Color Universal
Design set instead of `batlowS`. Every entry clears 3:1 against
`NeutralBgSurface` (minimum 3.16:1), and the palette's worst pair is ΔE 15.6
in normal vision and 7.5 under deuteranopia — the best CVD separation of
every candidate measured, by a wide margin.

**The cycle length drops from ten to seven, and seven is the honest count.**
No published CVD-safe qualitative set reaches ten entries that all clear 3:1
on a dark spine; the ones that clear it top out at seven. A ten-entry cycle
that keeps this property was not available to be chosen. Consumers needing
more than seven categories should vary a second channel — dash pattern,
marker shape — rather than expect an eighth hue. `QualitativeCycleLen` is
exported (and `QUALITATIVE_CYCLE_LEN` on the Rust side) so no consumer has to
hard-code the count; several did, and they were the sites that broke.

`batlowS` stays vendored. It remains correct for fills, which is the polarity
it is legible in, and dropping it would break the one call site using it that
way.

### SD2 — Subsetting is allowed; perturbation is not

The published Okabe-Ito set has **eight** entries, the first of which is
black. Black measures 1.28:1 against `NeutralBgSurface` — the publication
assumes a white figure background. The vendor converter drops it and keeps
the remaining seven **unmodified**.

This is the same operation ADR-0033 §SD4 already performs on `batlowS`
(truncate 100 upstream rows to the intended first 10), and it does not
weaken ADR-0031 §SD3's "no construction; no perturbation" rule: the values
that ship are byte-for-byte the published ones. Subsetting is a documented,
reviewable choice recorded in the upstream artefact; perturbation would
silently make the values ours. §SD3's rule stands, now read explicitly as
*no perturbation of the values that ship*, with subsetting permitted where
the ADR records the rule and the reason.

**Licensing gap, stated rather than papered over.** ADR-0031 §SD3 constrains
sources to "peer-reviewed, public-domain / MIT-licensed colormaps with
attribution and an upstream SHA pin". Okabe-Ito carries **no formal
license** — it is published as a standard and widely redistributed, not
shipped as a licensed software artefact. Attribution and the SHA pin are
satisfied (the transcribed upstream `.txt` is hashed like the Crameri
files); the license clause is not, and this ADR accepts that rather than
claiming a license the source does not grant.

### SD3 — Qualitative palettes are gated, in the vendor pipeline and in tests

This supersedes ADR-0031 §SD5's "no in-house re-verification" for
data-encoding palettes. Publication trust is retained for what publications
verify (CVD design intent) and dropped for what they do not (contrast against
the IDS spine).

Three floors, all **enforcing**, applied to any palette the vendor converter
marks `Qualitative`:

| floor | value | shipped palette measures |
|---|---|---|
| contrast vs `NeutralBgSurface` | ≥ 3:1 | 3.16 |
| worst pair, normal-vision OKLab ΔE | > 15 | 15.6 |
| worst pair, each simulated dichromacy | > 6 | 7.5 |

`NeutralBgSurface` is the binding surface — the lightest of the three IDS
dark surfaces, so clearing it clears `NeutralBgPanel` and `NeutralBgFaint`.
A palette that misses any floor is **not emitted**: the gate runs before the
write, so a bad LUT cannot reach the tree. The same floors are asserted as
unit tests over the shipped accessor, so a hand-edit is caught too.

**The dichromacy floor of 6 is empirical, not perceptual.** Nothing in the
literature supplies a ΔE floor for simulated-dichromacy categorical
palettes, and nothing in this tree reaches 15 under simulation — the
semantic palette measures 0.2–0.5. Six sits below what the published
CVD-designed reference achieves and above every candidate that was measured
and rejected, so it catches a regression without asserting a threshold that
does not exist. It should be re-derived, not inherited, if the palette
changes again.

Sequential and diverging ramps are **not** gated this way. They are sampled
by `t` and are *meant* to span lightness; a per-entry contrast floor would be
a category error.

### SD4 — APCA is not the gate for series colour

ADR-0031 §SD5 makes APCA the gate for the semantic palette. It is the wrong
instrument here and is deliberately not used: measured across the candidate
field, only an all-pastel palette clears |Lc| ≥ 45 on the dark spine, and
that palette has the worst tritanopia separation of the field. Gating series
colour on APCA 45 would select *against* CVD safety. WCAG 1.4.11's 3:1 for
graphical objects is the floor, as §SD3 sets it.

### SD5 — Distinctness is measured perceptually

`TestQualitativeCyclePairwiseDistinct` moves from an RGB-Euclidean floor
(dist² ≥ 225) to OKLab ΔE, and gains a CVD-simulated sibling. The RGB metric
was not merely weak — it *passed the shipped palette at ΔE 4.9*, because
equal RGB steps are not equal perceptual steps. ADR-0033 §SD4's regression
test caught the vendoring bug it was written for and would not have caught
this.

## Alternatives

Costed with measurements in
[the background-work page](../adr-background-work/qualitative-palette-dark-surface.md)
§5–§6. Summarised:

- **Reorder the existing cycle so bright stops come first**, generalising
  `imztop`'s local `markerColorOrder` fleet-wide. Cheapest option, no new
  vendoring, keeps §SD3 untouched. Rejected on measurement: the aggregate is
  unchanged — six of ten clear 3:1 in either order — so it defers the
  unusable slots rather than removing them, and a consumer with eight or more
  series still reaches them. The near-duplicate teals survive either way.
- **Lightness-lift `batlowS`'s dark entries.** Rejected: it breaks §SD3's
  no-perturbation rule and the upstream SHA pin, and yields a constructed
  palette carrying a vendored palette's name and citation. Dominated by both
  remaining options.
- **Keep ten entries by adopting a ten-entry published set** (Tol muted) and
  documenting the weak slots as an overflow tier. Rejected: three of its ten
  measure below 3:1 (minimum 1.35:1), so it does not fix the defect — it
  documents it.
- **Keep ten entries by concatenating Okabe-Ito with three further hues.**
  Rejected: the result is a constructed composite rather than an adopted
  publication, which §SD3 forbids and §SD2 above deliberately does not
  loosen, and the appended entries pull the worst-pair separation below the
  15.6 the pure set achieves.
- **A background-aware accessor** — `QualitativeCycleOn(bg)` or a
  surface-keyed table, so light- and dark-surface consumers resolve
  differently. The most general answer, and the largest API blast radius:
  every call site would have to pass its surface. Rejected as premature.
  There is no light theme in `styletokens`, and of the three dark surfaces
  `NeutralBgSurface` is the lightest, so one gate against it already covers
  the others. Worth revisiting only if a light theme lands.
- **Construct an IDS qualitative palette in OKLCh** at roughly constant
  lightness, the way the semantic palette is built, using the existing
  `colors/gen` + `gma` + `cvd` machinery. This was the option expected to
  win, and measurement killed it: at constant lightness the worst pair falls
  to **ΔE 0.1–1.1 under deuteranopia** at every lightness and every
  cardinality swept. Lightness is the one channel dichromats retain, so
  holding it constant removes the only separator that survives. Relaxing to a
  floored, non-constant band improves it to 5.0 — still below the published
  set's 7.5. Naive construction is measurably worse here than adoption,
  which is the same conclusion ADR-0031 §SD3 reached for sequential ramps,
  arrived at independently.

## Consequences

- Every foreground call site in the fleet is legible on the spine without
  local compensation. `imztop`'s `markerColorOrder` is deleted; its comment
  claiming "a 1.2 px stroke carries enough pixels that even the dark stops
  read fine" was wrong and is corrected in place — the navy measured 1.00:1,
  which no stroke width rescues.
- `codeview`'s hand-picked bracket-depth set rejoins the cycle. Beyond
  removing the divergence, this fixes a defect the hand-picked set had:
  its gold and orchid were ΔE 1.2 apart under deuteranopia, so consecutive
  depths were nearly indistinguishable to a deuteranope. See
  [ADR-0015](./0015-regex-pattern-syntax-highlighting.md) §SD2, updated.
- Consumers that hard-coded ten broke and were fixed: `play`'s detail
  timeline sized its palette slice at 10 and modulo'd its legend swatch at
  10; implot's series table was a fixed `[10]uint32` filled from the cycle,
  which would have aliased slot 7 onto slot 0 while the token accessor
  wrapped one position earlier, so the two disagreed past slot 9. The table
  is now sized per palette.
- The graph widget demo lost its node/edge colour offset. Five nodes plus
  five edges want ten distinct colours; seven are available, so no offset
  separates them. Edges took a structural neutral instead, which is the
  better encoding regardless — an edge there carries topology, not category.
- `idsshowcase` now draws one series per cycle entry rather than a fixed
  six, so the shop window shows the whole palette exactly once. It
  previously omitted four of ten colours while claiming to demonstrate the
  cycle.
- **A ten-series plot now repeats colours after seven.** This is a real
  regression in capacity, accepted deliberately: the alternative is
  repeating *illegible* colours after ten. The plots pattern is updated to
  say so and to point at the second-channel remedy.
- The IP-boundary check is unaffected — Okabe-Ito's values do not collide
  with the vendored design-system references, and the qualitative palette
  does not reach the CSS mirror.

## Status of superseded material

- ADR-0031 §SD3 — "adopt the published values verbatim" **stands**, now read
  as permitting documented subsetting (§SD2). Its `batlowS` recommendation
  for the qualitative role is superseded; `batlowS` remains a vendored fill
  palette.
- ADR-0031 §SD5 — the data-encoding "no in-house re-verification" exemption
  is **superseded** by §SD3 above. Its APCA gate for the *semantic* palette
  is untouched.
- ADR-0033 §SD4 — the first-10 truncation rule for `batlowS` still describes
  that palette correctly; `batlowS` is simply no longer the qualitative
  cycle.
- The advisory-only state of the semantic palette's CVD check is recorded
  here as an observation, not changed. Making it enforcing is a separate
  decision and would need its own numbers — the palette does not currently
  clear the floor it nominally has.
