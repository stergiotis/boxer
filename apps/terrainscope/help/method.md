---
type: explanation
audience: end-user
status: draft
title: Line of sight under uncertainty
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Line of sight under uncertainty

Three computations sit behind the window, each a strict generalisation of
the one before: a single line of sight, the bearing fan around it, and the
Monte-Carlo ensemble over the fan. The code is `public/science/geo/swisstopo`;
this page says what each assumes and what its numbers mean. None of them
measures the world: the inputs are the person's clicks and sliders, and the
probabilities are fractions of a pseudo-random ensemble under an assumption
stated below.

## One line of sight

The observer and target are converted to LV95, the Swiss grid, and the
terrain between them is sampled every 2 m along the straight ground line,
matching the tile resolution. The sight line is a straight line in
elevation from the terrain at the observer plus the observer height to the
terrain at the target plus the target height. The ray is *obstructed* if any
interior sample's terrain elevation is above the sight line at that
distance; the first such sample is the obstruction reported, with its
distance and elevation. The endpoints are never obstruction candidates.

What that leaves out: the sight line is straight — no earth curvature and no
atmospheric refraction, which matter at ranges of tens of kilometres; the
only thing that blocks a ray is a terrain sample, so nothing standing on the
terrain enters the verdict; and a profile the sampler cannot read is an
error, not an extrapolation.

## The bearing fan

The observer→target line is read in polar coordinates about the observer and
the bearing is swept by ±**sweep ±** in steps of **step**: the rays sit at
integer multiples of the step within the half-range, so a half-range that
is not a multiple of the step is rounded down to one that is. The range is
held constant, so the targets trace an arc; a half-range of 0 is the single
centre ray. Every ray shares one distance axis — rays are trimmed to the
shortest by at most one 2 m sample at the far end, which cannot change a
verdict. The fan is deterministic: the uncertainty below never moves it.

The *rays* chip gives the count; *visible / obstructed* are the nominal
verdicts — un-jittered, one per ray.

## The ensemble

The four geometric inputs — observer position, target position, observer
height, target height — each get an independent Gaussian whose standard
deviation is the matching σ slider: 1σ, in metres, which is what every
tooltip says it is. A σ of 0 pins that input. Position noise is drawn as
independent east and north offsets with that σ; height noise is a signed
offset. Each of the **samples** members draws all six normals, moves both
points and both heights, and runs the whole fan again. With 0 samples, or
every σ at 0, the ensemble is the nominal sweep alone.

The draw is seeded with a fixed constant, so the same parameters give the
same ensemble and the plots do not flicker between recomputes of the same
selection. That is reproducibility, not precision: 16 samples resolve a
probability to steps of 1/16.

What the reduced numbers are:

- **Visibility probability per ray** — the fan colour on the map, the
  percentage in each profile's legend entry, the *centre* chip — is the
  fraction of members in which that ray was clear. The nominal sweep is the
  central estimate and is not counted. With no members it is the nominal
  verdict, 0 or 1.
- **mean vis** is the mean of those fractions over all rays — the fan's
  overall robustness to the input noise, not the probability of any one
  sight line.
- **The envelope band** behind each profile is the minimum and maximum
  terrain elevation seen at each distance over the nominal profile and
  every member. It is a range of extremes, not a quantile interval: more
  samples widen it and never narrow it, and it says how far the terrain
  under a ray moves when the ray's endpoints move, not how uncertain the
  terrain is.
- **The ECDF** on the Distributions tab is, per randomised input, the
  fraction of samples at or below each realised deviation from the nominal,
  drawn as a step plot — the function jumps by 1/n at each draw. Position
  deviations are radial offsets (≥0); height deviations are signed. It
  shows what was actually drawn, so a reader can check the ensemble against
  the σ asked for; it says nothing about the terrain. Only inputs with a σ
  above 0 appear.

## What the method does not supply

There is no threshold. A ray at 50 % is one the ensemble could not settle,
and the σ that produced the figure are values the person set, not the
measured error of a position fix or a map. Every probability is conditional
on the Gaussian assumption, the σ and the sample count; a different σ or
seed gives different figures, which is why the Sweep tab shows the σ and the
count beside every result, so the condition travels with the number.
