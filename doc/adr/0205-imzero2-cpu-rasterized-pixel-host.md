---
type: adr
status: proposed
date: 2026-08-22
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted. The code described here is already in the tree, which is the wrong order — see §Status.

# ADR-0205: imzero2 — a CPU-rasterized pixel host

## Context

imzero2 has had two GPU-less headless hosts since
[ADR-0128](./0128-imzero2-mesh-draw-stream-codec-lane.md) M3, and neither
produces **pixels**: `headless` serves the mesh draw-stream lane, `headless_svg`
writes SVG. Everything that needs a raster frame — the PNG dump, an
[ADR-0154](./0154-headless-carrier-tree-and-driver.md) capture request, the
H.264 file sink, the carrier's video path — required `headless_wgpu`, and so a
Vulkan loader plus an ICD in the runtime image.

That is the force ADR-0128 named: an appliance image wants a minimal,
statically-linked userland, and "mesa/llvmpipe software rasterization plus an
external ffmpeg are a large dynamically-linked C closure". ADR-0128 answered it
by *avoiding pixels* — SD6 chose the mesh lane explicitly "without writing a CPU
rasterizer", on a measured 28.6–62 ms CPU/frame for llvmpipe at 1280×800.

Two things changed. `egui_software_backend` exists, is 4,690 lines, and compiles
against the pinned egui 0.35 with no source change. And measured on real imzero2
content it costs **1.22 ms CPU/frame** at 1280×800 — 23–51× under the figure
that ruled the option out.

The full measurement record, including several conclusions that were wrong on
the way, is
[the survey](../adr-background-work/egui-software-backend-survey.md).

## Design space (QOC)

**Question.** How should a boxer deployment with no GPU produce imzero2 pixels?

**Options.**

- **O1** — status quo: `headless_wgpu` on lavapipe (Mesa's software Vulkan).
- **O2** — no pixels: the ADR-0128 mesh lane only, renderer in the viewer.
- **O3** — vendor `egui_software_backend` behind a new host feature.
- **O4** — depend on `egui_software_backend` from crates.io.
- **O5** — write a first-party CPU rasterizer.

**Criteria.**

- **C1 — runtime dependency closure.** What the image must carry. Measured by
  `cargo tree` and `ldd`.
- **C2 — per-frame cost.** p50 and CPU-seconds per frame on a real play frame at
  1920×1200, via `IMZERO2_HEADLESS_RASTER_STATS`.
- **C3 — fidelity.** Visible pixel deltas against the wgpu render across the
  66-scene screenshot tour, gated by a per-scene reproducibility floor.
- **C4 — maintenance exposure.** Code we must understand, and who else maintains
  it.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | ++ | ++ | ++ | ++ |
| C2 | −− | ++ | ++ | ++ | ?  |
| C3 | ++ | −  | ++ | ++ | ?  |
| C4 | ++ | +  | +  | −  | −− |

O1 is worst on both axes that motivated the question: 5.96 ms p50 and **102.6 ms
CPU/frame**, so it cannot sustain 60 fps on the measurement machine at all, and
it still needs Mesa. O2 remains right for *streaming* and cannot serve a
scripted capture or an image endpoint. O5 is O3's work plus the rasterizer.
O3 and O4 differ only in C4, which §SD2 decides.

## Decision

We will add **`headless_soft`**, a third headless host that rasterizes on the
CPU using a vendored `egui_software_backend`, alongside — not instead of —
`headless_wgpu` and the mesh lane.

### SD1 — `headless_raster` is the capability; the two rasterizers are alternatives

`headless_wgpu` conflated "this build produces pixels" with "this build uses
wgpu". Split: **`headless_raster`** is what everything downstream of a frame
keys off (PNG dump, capture request, encoder lane, carrier video), and
`headless_wgpu` / `headless_soft` are its two implementations, selected behind a
`Raster` type alias with one inherent five-method surface. A build carrying both
resolves to wgpu — a precedence, mirroring how `main.rs` resolves
desktop/headless/headless_svg, not an error.

Deliberately an alias and not a trait: the choice is a compile-time feature, so
a trait would buy dynamic dispatch nobody uses and force both hosts into the
graph to name it.

### SD2 — vendored, not a crates.io dependency

`egui_software_backend` has one primary author, three releases, ~1.3k downloads,
and its egui-version bumps have arrived from outside contributors. imzero2
already pins a five-crate egui ring that must move in one commit; a sixth crate
that can lag a release is a scheduling risk. At 4,690 lines under MIT OR
Apache-2.0, carrying it is cheaper than being blocked by it — and the one egui
minor bump measured so far cost **zero lines** in the render core.

The copy lives at `rust/imzero2/vendor/egui_software_backend/`, is **excluded
from the workspace**, and keeps 16 of its 18 source files byte-identical to
upstream. `VENDORING.md` beside it carries the upstream commit, every local
delta with its reason, and the re-sync procedure; THIRD_PARTY_NOTICES §1.8
records the licence election (MIT).

### SD3 — cached compositing with a worker pool, four workers by default

Caching and `rayon` are **one** decision, because all of the crate's
parallelism is on its caching path — `render_direct` has no parallel variant.
Uncached wins single-threaded, which is why this host started there and was
wrong for two commits.

The pool defaults to **4 workers**, capped at half the hardware threads so a
small box keeps cores for the Go host, the carrier and the encoder.
`IMZERO2_HEADLESS_RASTER_THREADS` (ADR-0009) overrides. Since §SD4, p50 is flat
from 2 to 16 workers, so the knob buys tail latency and costs ~15 MiB per
worker — a memory dial, not a throughput one.

### SD4 — the blit is tile-scoped, and the host's frame buffer persists

Upstream's `blit_canvas_to_buffer` paints every *occupied* tile every frame,
which in a viewport-filling UI is all of them: measured at 60–75 % of the frame.
A `blit_dirty_to_buffer` addition paints only *dirty* tiles, resetting each to
the clear colour first so the alpha composite does not stack, and the host keeps
its frame buffer across frames rather than clearing it whole. p50 723 → 266 µs.

This is the only delta that adds behaviour rather than removing it, and it is
offered upstream.

### SD5 — the host advises on placement rather than taking it

Two machine properties govern cost and are readable at startup from
`/proc/self/status` and sysfs: whether the pool's CPU affinity spans more than
one L3 domain, and how large that domain is. The host logs both — an affinity
warning naming a `taskset` line when the pool is large enough for it to matter,
and a working-set note when the frame exceeds the L3 pixel budget.

It advises and does not act: this process cannot know what else the box is for.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Cargo feature set (`rust/imzero2/Cargo.toml`) | added `headless_raster`, `headless_soft` | `build_rust_headless_soft.sh`; ~25 cfg sites in `headless.rs` renamed from `headless_wgpu` to `headless_raster` |
| Env registry (ADR-0009) | added `IMZERO2_HEADLESS_RASTER_STATS`, `IMZERO2_HEADLESS_RASTER_THREADS` | `imzero2env.go`, regenerated `doc/env-vars.md` |
| `Raster` host contract (`headless.rs`, internal) | `render_and_readback` now takes an already-tessellated pass; `apply_textures_only` takes `&TexturesDelta` | both implementations; `ctx.tessellate` moved to the caller |
| Third-party inventory | new category: whole vendored crate | THIRD_PARTY_NOTICES §1 preamble + §1.8; `VENDORING.md` |
| `check.sh` | `cargo fmt -p …` instead of `--all` | — (cargo-fmt and clippy both reach path dependencies; workspace `exclude` does not spare them) |

## Alternatives

- **lavapipe under `headless_wgpu` (O1).** 4.5× slower and 65× more CPU per
  frame than this host, and still needs Mesa. Retained only as the fallback when
  something must go through a Vulkan device.
- **Mesh lane only (O2).** Unchanged as the answer for *streaming* — it encodes
  nothing, so it also avoids the ffmpeg half of the C closure that this ADR does
  **not** address. It cannot serve a scripted capture or an image endpoint.
- **crates.io dependency (O4).** Rejected on §SD2's scheduling risk, not on code
  quality.
- **First-party rasterizer (O5).** All of O3's integration work plus the
  rasterizer, against a permissively-licensed one that already passes a
  CPU-vs-GPU fidelity test.
- **Damage-tracked readback for the wgpu path.** Measured and declined: readback
  is 35 % of that frame, so the ceiling is ~1.5×. Survey §18.

## Consequences

### Positive

- A GPU-less deployment can produce pixels with **no Vulkan loader, no ICD, no
  Mesa**: 321 → 323 crates, zero wgpu-family, binary 45.6 → 39.0 MB. The
  renderer is 138 KB of `.text` against the GPU stack's 2.37 MB (17×), from
  4,690 source lines against 265,785 (57×).
- Fastest arm measured in wall-clock: 0.25 ms p50 against wgpu's 1.34 ms —
  though ~2.9× of that gap is incrementality (§SD4) the wgpu path could also
  have, so the like-for-like rasterizer win is ~1.9×.
- Fidelity is not a compromise: 98.3 % of pixels identical to the wgpu render
  across 92 gallery images, with a visible-delta share (0.31 %) barely above the
  0.22 % floor two runs of the *same* renderer produce.
- Re-opens server-side pixels for the appliance target (ADR-0128 M3): 1.22 ms
  CPU/frame at 1280×800.

### Negative

- **Memory.** 228 MiB client RSS at the default against wgpu's 118 MiB, ~15 MiB
  of it per worker (mimalloc per-thread arenas plus each worker's in-flight
  primitive raster). This host is no longer the one-core-and-nothing-else
  proposition ADR-0128 SD6 imagined.
- **Tail.** p99 6.2 ms against wgpu's 3.0 ms. The cause is measured and
  inherent: tail frames re-rasterize primitives whose cache went stale, which is
  the cost of drawing a whole frame on a CPU.
- **A sixth crate on the egui ring**, from a thin-bus-factor upstream, now
  carrying a local delta of our own (§SD4).
- **A staleness failure mode that did not exist before.** A tile that never goes
  dirty keeps its contents; the primitive cache key hashes a texture's *id*, not
  its contents, so a texture mutated in place under an unchanged mesh would
  persist as stale pixels. Unobserved across the gallery; unguarded.
- **The ffmpeg half of the C closure is untouched.** A *streaming* appliance
  still wants the mesh lane.

## Migration

Nothing to migrate. `headless_soft` is a new feature; no existing build changes
behaviour, and `headless_wgpu` wins where both are enabled. The `Raster`
contract reshaped in §Surfaces is internal to `headless.rs` and has no consumers
outside it.

- **Regeneration.** `doc/env-vars.md` via `public/app env gen-docs` after the
  two new `env.Spec`s. No FFI boundary moved, so neither side needs rebuilding
  for this ADR.

## Verification plan — Tier 1

- **Lane.** None automated. **This is the plan's gap and the largest open risk
  in this ADR.**
- **What would fail today.** Nothing. Every claim above was established by
  running `scripts/dev/play-screenshot-tour.sh` on each host and diffing the
  galleries by hand, gated against a per-scene reproducibility floor built from
  two wgpu runs. A rasterizer regression, or the §Consequences staleness bug,
  would reach a release unnoticed.
- **What the lane should be.** A golden-image test in the `//go:build
  integration` lane over a small subset of tour scenes, compared with a
  tolerance rather than exact equality — several play panels render
  time-dependent content, and one scene disagrees with itself by 14.8 %.
  `IMZERO2_HEADLESS_RASTER_STATS=1` gives the performance side.
- **Prior question.** The Rust side has no CI at all today (`cargo` never runs
  in workflows, and `check.sh`'s clippy gate is red at HEAD for unrelated
  reasons), so this is partly a question about whether that changes.

## Milestones

- **M0 — the host.** ✓ (2026-08-22) `headless_raster`/`headless_soft` split, vendored crate,
  build script (8089450f).
- **M1 — measurement.** ✓ (2026-08-22) `IMZERO2_HEADLESS_RASTER_STATS` on both pixel hosts
  (2831afff).
- **M2 — the configuration.** ✓ (2026-08-22) cached + pool (b3aa3980), tile-scoped blit
  (8c6696b3), four workers by default (41da3bdd).
- **M3 — placement advice.** ✓ (2026-08-22) affinity and L3 budget at startup (0b25c9e3).
- **M4 — a verification lane.** Not started; see §Verification plan.
- **M5 — upstream the tiled blit.** Not started.
- **M6 — musl-static + gokrazy probe.** Not started; inherits ADR-0128 M3. The
  remaining C dependencies in this graph are `ring` (via `rustls` ← `reqwest` ←
  the `walkers` map widget) and `mimalloc`, so an appliance build that drops the
  map widget drops the crypto closure with it.

## Status

Proposed — awaiting review by the code owner.

Recorded **after** the implementation rather than before it, which inverts
[CODINGSTANDARDS § Design Before Code](../../CODINGSTANDARDS.md#design-before-code).
The work began as a feasibility question and grew into a subsystem without a
decision point being marked; this ADR is the correction, and the sub-decisions
above are open to being reversed on review rather than presented as settled.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.

## References

- [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) — the headless host and pixel streaming.
- [ADR-0128](./0128-imzero2-mesh-draw-stream-codec-lane.md) — the appliance force, the mesh lane, and the `headless_wgpu` split this ADR extends.
- [ADR-0009](./0009-environment-variable-registry.md) — the env registry the two new knobs join.
- [The survey](../adr-background-work/egui-software-backend-survey.md) — every measurement, and the corrections.
- [`VENDORING.md`](../../rust/imzero2/vendor/egui_software_backend/VENDORING.md) — upstream commit, deltas, re-sync.
- [egui_software_backend](https://github.com/DGriffin91/egui_software_backend) — upstream, MIT OR Apache-2.0.
