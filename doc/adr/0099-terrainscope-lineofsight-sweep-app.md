---
type: adr
status: proposed
date: 2026-06-28
---

# ADR-0099: `terrainscope` — a keelson app for terrain line-of-sight and polar viewshed sweeps

## Context

swissALTI3D-backed terrain analysis lives in two places today:

- `public/science/geo/swisstopo` — the library: LV95 conversion, `ElevationSampler`
  (2 m COG tiles), `SampleProfile`, and `LineOfSight` (one straight sight-line +
  first obstruction).
- Two consumers: the `swisstopo line-of-sight` CLI subcommand, and an interactive
  **elevation-profile demo scene** in the imzero2 widgets tour (slippy map → click
  two points → elevation profile).

We want this functionality as a first-class **keelson app** (ADR-0026), not a demo
scene, and to add a new analysis: interpret the sight-line in polar coordinates
about the observer and **sweep the bearing by a small range (±θ), recording the
height profile of every ray** — a partial viewshed fan.

The capability model raises one architectural question: tile reading is filesystem
I/O. ADR-0090 established that GUI apps do not perform such I/O directly — a
headless service owns it and publishes over the bus, leaving the GUI a zero-fs-cap
bus client that survives the ADR-0085 sandbox. The same logic applies to tile
reads.

## Decision

Add `apps/terrainscope` (SurfaceWindowed), ported from the elevation-profile demo,
plus the sweep computation in the library. Phased, descope-over-gate:

- **Library** — `ElevationSampler.LineOfSightSweep(from, fromHeight, to, toHeight,
  halfRangeDeg, stepDeg) (LOSSweepResult, error)`. The polar interpretation is a
  rotation of the `to − from` vector about `from` by each offset δ ∈ [−halfRange,
  +halfRange] (step `stepDeg`), holding the range constant; `LineOfSight` runs per
  ray. Because range and the metre-step are constant, every ray shares one distance
  axis, so the result is a clean (angle × distance → elevation) field — rays are
  trimmed to the shortest length to absorb the ±1-sample raggedness floating-point
  rounding of the rotated range introduces at exact step boundaries. Pure,
  headless-testable.
- **Phase 1 (first cut)** — port the demo into the app: map, two-click selection,
  elevation profile. Retire the demo scene.
- **Phase 2** — single-ray line-of-sight overlay (sight-line + obstruction marker).
- **Phase 3** — the polar sweep: render the fan (overlaid profiles first;
  angle × distance heatmap later) and record the profiles (in-UI; CSV export via
  the `fs.dialog.save` broker later).
- **Phase 4 (deferred)** — extract a headless elevation service (sole tile reader,
  answers profile / LOS / sweep over the bus); `terrainscope` becomes a zero-fs-cap
  bus client (ADR-0090 shape).
- **Phase 5 (landed)** — input uncertainty. `ElevationSampler.LineOfSightSweepEnsemble`
  takes an `EnsembleSpec` and Monte-Carlo samples **each non-angle input from its own
  Gaussian** — observer position, target position, observer height, target height
  (the bearing fan stays deterministic) — seeded for reproducibility. It reduces the
  ensemble to a per-bearing visibility probability and a per-(bearing, distance)
  terrain envelope, and records each variable's realised draws (`LOSEnsembleResult`;
  pure `aggregateEnsemble` is unit-tested without tiles). The app renders: the map
  fan coloured by visibility probability; the sweep plot's elevation envelope band +
  per-ray visibility percentages; and a **distributions pane** — the empirical CDF
  (one step-line per variable) of the recorded draws. Every analysis parameter
  (heights, sweep range/step, the four σ, samples) is a **live control** that
  recomputes reactively, coalesced to at most one in-flight worker (leading +
  trailing). The map and each plot live in **dockable panes** (egui_dock tabs the
  user can split), and the ensemble's wall-clock is measured and shown (controls +
  sweep summary).

**Tile access:** Phase 1–3 read tiles directly from `$SWISSTOPO_TILES_DIR` (the env
var moves from the demo package to the app), matching the demo. This is the chosen
first cut; the principled bus-service split is Phase 4.

## Alternatives considered

- **Headless service first.** The capability-clean end-state, but front-loads
  bus/service infrastructure before anything renders. Deferred to Phase 4, not
  rejected (descope-over-gate).
- **Extend the CLI only.** The functionality already has a CLI; the ask is
  explicitly for an app. Rejected.
- **Keep it a demo scene.** Rejected — "an app, not just a demo." The scene is
  retired in favour of the app.
- **Sweep in true geodesic azimuth.** LV95 is a conformal projection; rotating in
  grid space over a few km / few degrees is negligibly different and avoids a
  geodesy dependency. Grid-space rotation chosen; noted in code.

## Consequences

- The elevation-profile demo scene and its registration are removed; the widgets
  tour loses that scene until `terrainscope` ships its own screenshot tour
  (deferred). No golden image assets reference it.
- `SWISSTOPO_TILES_DIR` is now declared by `apps/terrainscope`; `doc/env-vars.md`
  is regenerated to reflect the new declaring package (Name unchanged).
- Under the ADR-0085 sandbox the direct-read app needs a filesystem capability
  until Phase 4 removes the direct read. Documented, accepted for the desktop / dev
  first cut.
