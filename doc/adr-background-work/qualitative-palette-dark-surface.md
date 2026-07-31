---
type: explanation
audience: package maintainer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-31
---

> **Provenance.** Measurement pass compiled 2026-07-31, ahead of
> [ADR-0156](../adr/0156-qualitative-palette-dark-surface.md). Every number on
> this page was computed against the working tree on that date using the
> in-repo colour tooling (`colors/apca`, `colors/contrast`, `colors/cvd`,
> `colors/oklab`) — none is estimated or quoted from a publication. The
> candidate palettes' *values* come from their publications; the
> measurements of those values are ours. The throwaway harness that produced
> them was not kept: the checks worth keeping became the gate and the tests
> described in §7.

# The IDS qualitative palette on a dark surface — measurements

## 1 Question

Whether `styletokens.QualitativeCycle` is usable as a *foreground* — a
stroke, marker or glyph — on the IDS dark spine, and if not, what should
replace it.

The cycle read Crameri's `batlowS` first-10 subset. Two widgets had already
worked around it locally without escalating, and a third was drawing an
invisible scatter point in `play`.

## 2 The defect, measured

Against `NeutralBgSurface` (`#1d2021`, L\* 12.0) — the implot plot-area fill,
and the lightest of the three IDS dark surfaces — under WCAG 1.4.11's 3:1
floor for graphical objects:

| slot | hex | L\* | WCAG | APCA Lc |
|---|---|---|---|---|
| 0 | `#011959` | 12.1 | **1.00:1** | 1.3 |
| 1 | `#faccfa` | 87.2 | 11.78:1 | −82.4 |
| 2 | `#828231` | 52.9 | 4.05:1 | −32.0 |
| 3 | `#226061` | 37.1 | **2.27:1** | −15.2 |
| 4 | `#f19d6b` | 72.1 | 7.63:1 | −58.2 |
| 5 | `#4d734d` | 44.8 | 3.03:1 | −22.8 |
| 6 | `#114360` | 26.7 | **1.56:1** | −6.4 |
| 7 | `#fdb4b4` | 80.1 | 9.65:1 | −70.5 |
| 8 | `#c09036` | 62.9 | 5.69:1 | −44.8 |
| 9 | `#175262` | 32.0 | **1.89:1** | −10.7 |

Four of ten sit below 3:1. Slot 0 is not merely low-contrast — at 1.00:1 it
is *the same luminance as the background*, so no stroke width recovers it.

A second defect is independent of background: slots 3, 6 and 9 are
near-duplicate dark teals. The palette's worst pair is **ΔE 4.9 in normal
vision** (slots 3/9), which means it never met a ΔE > 15 separation bar at
all. Its CVD numbers barely degrade from there (3.9 under tritanopia) only
because what separation it has comes from lightness, which dichromacy
preserves.

## 3 Why no surface tuning could fix it

`batlowS` spans L\* 12.1 → 87.2. Sweeping a uniform grey background across
the full range and counting entries that clear the APCA thin-line floor
(|Lc| ≥ 45):

| bg L\* | 0 | 5 | 11 | 20 | 34 | 54 | 72 | 89 | 100 |
|---|---|---|---|---|---|---|---|---|---|
| entries clearing | 4 | 4 | 4 | 3 | 2 | 1 | 2 | 6 | 7 |

The best any uniform background achieves is 7/10, at near-white. The palette
straddles every possible background, so lightening or darkening the plot
surface cannot fix it. This was a palette problem, not a surface problem.

## 4 The gate that was not there

ADR-0031 §SD5 exempted the data-encoding palettes from in-house
verification, trusting the publication. That trust is reasonable for CVD —
Crameri does verify it — but a publication targeting print and light figure
backgrounds makes no claim about contrast against one particular dark UI
surface.

A second finding, which changed the gate's design: **the semantic palette's
CVD check does not enforce either.** `colors/gen` collects ΔE ≤ 15 findings
and prints them as `WARN: … (advisory at M0a)`; only APCA gates the build.
Measured, the semantic palette's own worst same-emphasis pairs are:

| emphasis | normal | deuteranopia | protanopia | tritanopia |
|---|---|---|---|---|
| subtle | 1.8 | 0.3 | 1.2 | 0.8 |
| default | 6.4 | 0.5 | 2.2 | 3.8 |
| strong | 3.9 | 0.2 | 1.4 | 1.6 |

So there was no enforcing CVD gate anywhere to model a new one on, and ΔE 15
is not reachable under simulation by anything in the tree. §7 sets the floors
accordingly. Bringing the *semantic* palette's advisory check up to an
enforcing one is out of scope here and left as it stands.

## 5 Candidates

Measured on `NeutralBgSurface`. "worst ΔE" is the minimum pairwise OKLab
distance across the whole palette, in normal vision and under each simulated
dichromacy.

| candidate | n | clears 3:1 | min ratio | L\* span | worst ΔE norm / deut / prot / trit |
|---|---|---|---|---|---|
| **Okabe-Ito, black dropped** | 7 | **7/7** | 3.16 | 46.0–89.1 | **15.6 / 7.5 / 9.5 / 8.4** |
| Tol vibrant | 7 | 7/7 | 3.16 | 46.0–75.9 | 12.9 / 6.1 / 8.5 / 6.0 |
| Tol muted | 10 | 7/10 | 1.35 | 22.4–88.1 | 12.0 / 5.1 / 8.0 / 7.2 |
| Tol bright | 7 | 6/7 | 2.69 | 41.6–77.4 | 10.6 / 7.9 / 7.2 / 3.3 |
| Tol light | 9 | 9/9 | 6.52 | 67.1–88.1 | 8.9 / 5.4 / 4.8 / **3.7** |
| Tol medium-contrast | 6 | 4/6 | 1.70 | 29.2–83.1 | 17.1 / 12.4 / 8.4 / 10.1 |
| Okabe-Ito, as published | 8 | 7/8 | 1.28 | 0.0–89.1 | 15.6 / 7.5 / 9.5 / 8.4 |
| seaborn Deep | 10 | 10/10 | 3.39 | 47.9–75.3 | 6.8 / **2.1** / **1.7** / 5.6 |
| `batlowS` (shipped) | 10 | 6/10 | 1.00 | 12.1–87.2 | 4.9 / 4.9 / 4.7 / 3.9 |

Two results worth stating plainly:

- **seaborn Deep — implot's `PaletteDeep` — is not the safe fallback it
  looks like.** It clears contrast everywhere but collapses under
  dichromacy: ΔE 1.7 under protanopia. Reverting implot's default to it
  would have traded a contrast bug for a CVD bug.
- **No published CVD-safe qualitative set reaches ten entries that all clear
  3:1 on a dark spine.** The ones that clear it top out at seven. A
  ten-entry cycle was not available to be kept.

## 6 Options costed

- **O1 — reorder the cycle so bright stops come first** (generalising
  imztop's local `markerColorOrder`). Measured: aggregate unchanged, 6/10
  either way. It defers the unusable slots rather than removing them — a
  consumer with 8+ series still reaches them — and the near-duplicate teals
  survive. Rejected.
- **O2 — lightness-lift the dark entries.** Breaks §SD3's no-perturbation
  rule and the upstream SHA pin, and the result is a constructed palette
  wearing a vendored palette's name. Dominated by O3 and O5; not measured
  separately.
- **O3 — vendor a second, screen-oriented published set.** Fits §SD3's
  "adopt, don't construct" rule best. §5 is the survey. **Chosen.**
- **O4 — background-aware accessor** (`QualitativeCycleOn(bg)`). Solves a
  problem that does not exist: there is no light theme in `styletokens`, and
  of the three dark surfaces `NeutralBgSurface` is the lightest, so clearing
  it clears the other two. Largest API blast radius of the five, for no
  measured benefit today. Rejected as premature.
- **O5 — construct an IDS palette in OKLCh at roughly constant L.** Killed
  by measurement, and instructively. Sampling n hues at constant OKLab L
  with maximum in-gamut chroma:

  | L | n=7 | n=8 | n=10 | (worst ΔE under deuteranopia) |
  |---|---|---|---|---|
  | 0.55 | 0.9 | 0.6 | 0.6 | |
  | 0.65 | 0.7 | 0.8 | 0.5 | |
  | 0.75 | 0.6 | 0.6 | 0.5 | |
  | 0.85 | 0.3 | 0.2 | 0.1 | |

  Constant lightness is the worst possible choice for CVD safety:
  lightness is the one channel dichromats retain, and holding it constant
  removes the only separator that survives. Relaxing to a *floored,
  non-constant* band (L 0.60→0.90, hue-swept) improves it to deut ΔE 5.0 at
  n=10 — better, but still below Okabe-Ito's 7.5. Naive construction is
  measurably worse than the published CVD-designed sets, which is
  presumably why those sets exist.

There is also a gate-design finding hiding in this table: **APCA |Lc| ≥ 45 is
the wrong gate for series colours on a dark spine.** Only an all-pastel
palette clears it (Tol light, 9/9) — and Tol light has the worst tritanopia
separation in the field (3.7). Gating on APCA 45 would have selected against
CVD safety. WCAG 1.4.11's 3:1 is the right floor here.

## 7 What the numbers imply for the gate

Three floors, all enforcing, calibrated against what the shipped palette
actually achieves:

| floor | value | shipped palette | rejects |
|---|---|---|---|
| contrast vs `NeutralBgSurface` | ≥ 3:1 | 3.16 | `batlowS` (4 entries) |
| worst pair, normal vision | > 15 | 15.6 | `batlowS` (4.9), Deep (6.8) |
| worst pair, each dichromacy | > 6 | 7.5 | `batlowS` (3.9), Deep (1.7), O5 (0.1–1.1) |

The third is empirical, not perceptual. Nothing in the literature supplies a
ΔE floor for dichromacy-simulated categorical palettes, and §4 shows nothing
in this tree reaches 15 under simulation. Six sits below what the published
CVD-designed reference achieves and above every candidate rejected here, so
it catches a regression without pretending to a threshold that does not
exist. It should be re-derived, not inherited, if the palette ever changes.

## 8 Incidental finding — codeview's hand-picked set

`codeview/regex.go` had diverged from the cycle with four hand-picked
bracket-depth colours, justified by the batlowS defect. Measured, the
hand-picked set is fine on contrast (min 6.73:1) but poor under simulation:

| set | min ratio | worst ΔE norm / deut / prot / trit |
|---|---|---|
| hand-picked (gold/orchid/sky/sage) | 6.73 | 9.5 / **1.2** / 3.0 / 7.9 |
| cycle slots 0–3 (Okabe-Ito) | 4.79 | 17.0 / 14.6 / 11.3 / 11.3 |

Its gold and orchid are ΔE 1.2 apart under deuteranopia, so *consecutive*
bracket depths — the one thing that set exists to distinguish — were nearly
indistinguishable to a deuteranope. Rejoining the cycle fixes that and costs
2 points of contrast headroom, still well clear of the floor.
