---
type: adr
status: accepted
date: 2026-08-22
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-22
---

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
content it cost **1.22 ms CPU/frame** at 1280×800 — 23–51× under the figure that
ruled the option out. (That was the figure this decision was taken on; §SD4's
tiling later brought the same size to 234 µs. The argument only got stronger.)

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
from 2 to 16 workers, so the knob buys tail latency and costs ~13–15 MiB per
worker — a memory dial, not a throughput one. On a four-core part the plateau is
narrower (flat 2→4, worse at 8, on both such machines measured), and the cap is
what keeps the default inside it.

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
- Fastest arm measured in wall-clock on every machine tried, by a margin that
  is itself machine-dependent: 0.25 ms p50 against wgpu's 1.34 ms here (5.4×),
  0.24 against 2.5–4.2 ms on a Zen 2 APU (10–17×), 0.30 against 9.3 ms on an
  Intel mobile part (31×). About 2.9× of the gap is incrementality (§SD4) the
  wgpu path could also have, so the like-for-like rasterizer win on *this*
  machine is ~1.9×; the wider margins elsewhere are the GPU arm's readback
  (§18.1) on weaker parts. **Headless only** — in a window the ordering
  reverses (survey §20.3).
- Fidelity is not a compromise: 98.3 % of pixels identical to the wgpu render
  across 92 gallery images, with a visible-delta share (0.31 %) barely above the
  0.22 % floor two runs of the *same* renderer produce.
- Re-opens server-side pixels for the appliance target (ADR-0128 M3). The
  1.22 ms CPU/frame at 1280×800 this originally cited was measured before §SD4's
  tiling; the rasterize step at that size is now 234 µs (2026-08-23 entry).

### Negative

- **Memory.** ~13–15 MiB per worker (mimalloc per-thread arenas plus each
  worker's in-flight primitive raster), which reproduces on every machine
  measured because it is a within-run difference. This host is no longer the
  one-core-and-nothing-else proposition ADR-0128 SD6 imagined. **How it compares
  to the wgpu arm is not a property of this host and should not be quoted as
  one:** 228 MiB against 118 on the reference machine, 156 against 166 on a
  Zen 2 APU, 167 against 141 on an Intel mobile part — the ratio flips sign with
  the machine and its Vulkan driver (2026-08-23 entry).
- **Tail.** p99 6.2 ms, and ~6.0 ms on both other machines measured — the one
  figure here that travels. The cause is measured and inherent: tail frames
  re-rasterize primitives whose cache went stale, which is the cost of drawing a
  whole frame on a CPU. Whether that loses to the GPU arm depends on the GPU:
  3.0 ms on the reference machine, 8.3–8.8 on a Zen 2 APU, 11.9 on an Intel
  mobile part, so the deficit this once recorded is a surplus on the latter
  two.
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

- **Lane.** `scripts/ci/rust_imzero2_check.sh`, run by `scripts/ci/lint.sh`,
  which `lint.yaml` runs in CI. ~50 s cold, ~3 s warm, and it skips gracefully
  without cargo like `h3_wasm_parity.sh`.
- **What it checks.**
  1. `cargo check --all-targets` across the five shipped feature sets — desktop,
     `headless`, `headless_svg`, `headless_wgpu`, `headless_soft`.
  2. `cargo test` under `headless_soft`: the crate's existing tests plus three
     that assert this ADR's contracts — the incremental blit agrees with the
     full one (§SD4), a resize drops the frame-buffer priming, and a texture
     mutated in place under an unchanged mesh still repaints (the staleness mode
     in §Consequences).
  3. That `headless`, `headless_svg` and `headless_soft` pull **no wgpu-family
     crate**, which ADR-0128 SD6 calls "a hard guarantee" and which a feature
     edit could otherwise break silently, since everything would still build.
- **What would fail.** A feature set that stops compiling; a dirty-tile bug that
  makes the incremental path diverge; a resize that leaves stale priming; a
  texture update that stops repainting; wgpu leaking into a GPU-less build. All
  three test properties were verified against deliberately broken
  implementations — each fails for its own reason and no other.
- **What it does not cover, and why that is accepted.**
  - **Fidelity against wgpu.** The strongest property found — that the two hosts
    agree pixel-for-pixel — needs a GPU, which CI does not have. It stays a
    manual gallery diff (`scripts/dev/play-screenshot-tour.sh` on each host,
    compared against a per-scene reproducibility floor built from two wgpu runs;
    exact equality is the wrong test, since several play panels draw
    time-dependent content and one scene disagrees with itself by 14.8 %).
  - **Performance.** No p50/p99 assertion; a config regression that costs 4×
    would still merge. `IMZERO2_HEADLESS_RASTER_STATS=1` makes it a one-command
    manual check, and a threshold in CI would be flaky on shared runners.
  - **`cargo clippy`.** `rust/imzero2/check.sh` runs it with `-D warnings` and
    it is red at HEAD with ~2,100 findings, mostly in the generated interpreter.
    Gating on it is its own piece of work; holding this lane hostage to it would
    have left the crate ungated for longer.
- **What this fixes beyond this ADR.** `rust/imzero2` had no automated gate at
  all, which is how three Dependabot bumps merged green and broken on
  2026-08-10, and how an eframe PR floated egui to 0.36 in a lock-only commit
  on 2026-08-19 (84 type errors). The feature-matrix check is aimed squarely at
  that class, and it is the larger share of this lane's value.

## Milestones

- **M0 — the host.** ✓ (2026-08-22) `headless_raster`/`headless_soft` split, vendored crate,
  build script (8089450f).
- **M1 — measurement.** ✓ (2026-08-22) `IMZERO2_HEADLESS_RASTER_STATS` on both pixel hosts
  (2831afff).
- **M2 — the configuration.** ✓ (2026-08-22) cached + pool (b3aa3980), tile-scoped blit
  (8c6696b3), four workers by default (41da3bdd).
- **M3 — placement advice.** ✓ (2026-08-22) affinity and L3 budget at startup (0b25c9e3).
- **M4 — a verification lane.** ✓ (2026-08-22) `rust_imzero2_check.sh`,
  wired into `lint.sh`; three contract tests, the feature matrix and the
  no-wgpu assertion. Fidelity-vs-wgpu and performance stay manual — see
  §Verification plan.
- **M5 — upstream the tiled blit.** ✓ (2026-08-22) filed as
  [DGriffin91/egui_software_backend#17](https://github.com/DGriffin91/egui_software_backend/pull/17),
  one commit on top of upstream `main`, additive, with tests that need no
  GPU. If it merges, the §SD2 delta shrinks to the two boxer-only items (a
  `pub` widening and a clippy allow) and the re-sync in `VENDORING.md`
  becomes near-trivial. Awaiting review.
- **M6 — musl-static + gokrazy probe.** Not started; inherits ADR-0128 M3. The
  remaining C dependencies in this graph are `ring` (via `rustls` ← `reqwest` ←
  the `walkers` map widget) and `mimalloc`, so an appliance build that drops the
  map widget drops the crypto closure with it.

## Status

Accepted 2026-08-22.

Recorded **after** the implementation rather than before it, which inverts
[CODINGSTANDARDS § Design Before Code](../../CODINGSTANDARDS.md#design-before-code).
The work began as a feasibility question and grew into a subsystem without a
decision point being marked; this ADR is the correction, and the sub-decisions
above are open to being reversed on review rather than presented as settled.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.

## Updates

### 2026-08-22 — M6 splits: the gokrazy half is done, musl-static is not

M6 pairs "musl-static + gokrazy probe" as one milestone, on the assumption that
an appliance image needs a statically-linked binary. The probe found that wrong,
and the two halves are separable.

`ldd` on the `headless_soft` binary lists **four files** — `libgcc_s`, `libm`,
`libc` and the loader, about 4.8 MB — so an image can simply carry them. The
measurement is easy to misread: `headless_wgpu` prints the same four, because it
reaches Vulkan through `dlopen` where `ldd` cannot see it. That invisible closure
is the one ADR-0128 was avoiding, and it is what this ADR removed.

**Done:** two gokrazy appliance images, booted and verified, recorded in
[ADR-0206](./0206-gokrazy-appliance-image.md). They differ only by whether a
static ffmpeg is present; the one without it degrades to the ADR-0128 mesh lane
on its own. Measured in-frame on the appliance: 1.1 ms per frame in Rust at
1920x984, against the 1.22 ms at 1280x800 recorded above.

**Still open:** the musl target. `cargo check --target
x86_64-unknown-linux-musl --features headless_soft` fails on exactly two build
scripts, `ring` and `libmimalloc-sys`, both only for a missing cross C compiler.
The allocator half is now handled — a new `fast_alloc` feature gates mimalloc,
on by default and in all four build scripts, so no shipped build changed, and
the CI lane gained a check for the allocator-free configuration. `ring` arrives
via `reqwest` <- `walkers` and leaves with ADR-0204 M4; a walkers feature gate
here was rejected as duplicating that milestone.

**Correction.** `Cargo.toml` described `headless_soft` and `headless_wgpu` as
mutually exclusive, "headless.rs refuses to compile with both". It does not:
`headless.rs` gates the CPU host on `not(feature = "headless_wgpu")`, which is
the precedence SD1 specifies. The comment was wrong and has been corrected; the
decision above was right.

### 2026-08-23 — re-measured on a four-core Zen 2 APU: p50 transfers, three recorded costs do not

Every number above comes from one machine — 16 cores / 32 threads, two 32 MB L3
domains. §11's harness has now been run unchanged on a second: four cores /
eight threads, **one L3 domain of 4 MiB**, integrated GPU.
[Survey §19](../adr-background-work/egui-software-backend-survey.md#19-a-second-machine-a-four-core-zen-2-apu-added-2026-08-23)
carries the full record. Nothing in §Decision reverses; SD1–SD5 stand as
written. Three recorded figures need qualifying, two of them in §Consequences.

**What held.** The software host's p50 is 241 µs at 1920×1200 against the
250 µs recorded here — within 4 %, on a part with a quarter of the cores and an
eighth of the L3, and one where identical work (tessellation) runs about twice
as slow. The §17.4 thread curve reproduces to within 8 % at one, two, four and
eight workers. §SD4 is why: the median frame repaints a dirty-tile set small
enough to fit anywhere, so p50 stopped tracking machine size. §SD3's default
selects four workers on both machines, and here the `hardware_threads / 2` cap
is what does it — eight workers is every thread on four cores and is worse on
p50, CPU and RSS alike, so the cap and not just the constant earns its keep.
§SD5's advice fired correctly and unprompted at startup.

**Correction 1 — "Memory: 228 MiB against wgpu's 118 MiB".** On this machine
the same two arms are 156 MiB and 166 MiB, with the CPU host the *lower* of the
two, so **the recorded 2× does not hold generally** and this entry should not be
read as a property of the host.

Why the reference machine saw 228 MiB is *not* settled here, and the absolutes
are confounded: the measurement tree had already dropped `walkers` (and with it
`reqwest`/`rustls`/`ring`) after this ADR was recorded, which takes memory off
both arms, and the wgpu arm's RSS is largely its Vulkan driver's — a different
driver on this machine. Per-thread allocator arenas scaling with 32 hardware
threads is a candidate cause, not a finding. What the measurement does support
is the within-machine, within-tree comparison: at the shipped default the CPU
host is level with the wgpu arm or below it.

**Correction 2 — the CPU-per-frame ordering.** §Consequences reports the GPU as
~20 % cheaper in CPU. Here it is ~15 % more expensive: 4.98 ms against 5.73,
replicated. Submitting to the Vulkan device and reading the frame back has a
host-side cost that does not shrink with the frame (survey §18.1). The measured
quantity is whole-client CPU, so the within-machine comparison is the part that
carries this; the cross-machine absolutes are not comparable.

**Correction 3 — how the L3 budget should be read.** The working-set note in
§SD5 fires at 4.4× over budget here, and the median barely moves: 4.4× the
pixels costs 1.29× at p50 and 2.2× at p99. Post-§SD4 the residency argument is
about the tail, not throughput. "Size a CPU-rasterized appliance at
1080p–1200p" survives; what widening the viewport costs is tail latency.

**One observation about §Alternatives.** O1 is retained above as the fallback
when something must go through a Vulkan device. On this machine that fallback
could not be constructed: the OS image ships only the vendor ICD and Mesa's
software Vulkan is not installable without rebuilding the image. On an
appliance-shaped system the lavapipe arm may simply not exist.

**A note on citing the wall-clock figure.** §Consequences records the CPU host
as "the fastest arm measured in wall-clock", and §19 widens that to 10× on the
second machine. Both are **headless** results and neither transfers to a
window: survey §20.3 measures the two painters on one span in an on-screen host
and finds wgpu 15–26 % *faster*. The cause is already in this ADR's own
material — the headless wgpu arm pays a synchronous GPU→CPU readback (survey
§18.1 puts it at 35 % of that frame) and a desktop wgpu arm pays none. The
comparison here is sound for the host this ADR is about; it should not be cited
in support of a software-rendered desktop edition, where the argument is the
dependency closure and not throughput.

Two variables moved at once, and it is worth stating plainly: these figures come
from a working tree a day newer than the one above, which had also dropped the
`walkers` map binding and gained portolan. Machine and tree are confounded in
every cross-machine number here. The within-machine comparisons — the arms
against each other, the thread sweep, the resolution sweep — are unaffected, and
each correction above rests on one of those.

Not re-measured: fidelity against wgpu (§10.3 of the survey is untouched — same
code, same 256-bit path) and anything in §Surfaces. The wgpu arm on this part
varies 1.7× run to run, so its figures above are quoted as a pair rather than a
best case.

### 2026-08-23 — a third machine, an Intel mobile part: p50 transfers again, and two orderings move once more

[Survey §19](../adr-background-work/egui-software-backend-survey.md) closed by
noting that two machines separate a host property from a machine property but
are "not enough to claim a curve". §11's harness has now been run unchanged on a
third: **four cores / eight threads, one L3 domain of 8 MiB, dual-channel DDR4,
an integrated GPU** — an Intel mobile part, where both earlier machines were
AMD. AVX2 and no AVX-512, so the same 256-bit code path executes as in §16.1.
Nothing here reverses §Decision; SD1–SD5 stand.

Protocol as §19.2, with one difference stated so the columns are read
correctly: `IMZERO2_HEADLESS_LISTEN` was left unset, so the host pushes a
`NullSink` and the full tessellate-and-rasterize path runs on every frame with
no carrier and no encoder in the loop. 1920×1200, 660 frames, 60 Hz,
`IMZERO2_HEADLESS_RASTER_STATS=1`. The frame is 31 clipped primitives and
~6,405 triangles (ClickHouse was running, so the panels populate). Adapter
identity was read back per §15's rule on every wgpu arm.

**Every arm held 60 Hz** — 660 frames in 11.02–11.07 s — except lavapipe, which
slipped to 11.21–11.47 s.

| arm | p50 | p90 | p99 | client CPU/frame | peak RSS |
| --- | --- | --- | --- | --- | --- |
| **software, 4 workers (default)**, n=7 | **296 µs** (203–309) | 533 | 6.0 ms | **1.82 ms** | 167 MiB |
| wgpu on the integrated GPU, n=5 | 9,313 µs (9,074–9,689) | 10.9 ms | 11.9 ms | 2.76 ms | **141 MiB** |
| wgpu on lavapipe, n=2 | 11,509 µs | 14.4–15.0 ms | 21–27 ms | 61.0 ms | 145 MiB |

**p50 transfers a third time.** 296 µs against 250 µs on the reference machine
and 241 µs on the Zen 2 — within 23 % across three parts differing 4× in cores
and 8× in L3. §19.3's explanation (post-§SD4 the median frame repaints a
dirty-tile set that fits anywhere) now has a third data point rather than a
second. The §17.4 thread curve reproduces in shape but with a narrower plateau:

| workers | 1 | 2 | **4 (default)** | 8 |
| --- | --- | --- | --- | --- |
| p50, this machine | 372 µs | 342 | **296** | 375 |
| p50, §19.6 Zen 2 | 360 | 259 | 241 | 297 |
| p50, §17.4 reference | 365 | 261 | 261 | 298 |
| client CPU/frame | 1.39 ms | 1.58 | 1.82 | 2.92 |
| peak RSS | 107 MiB | 111 | 167 | 199 |

All three agree to within 4 % at one worker; at two this part is ~31 % slower
than either. The default is the best of the four measured here, so §SD3's
`min(4, hardware_threads / 2)` again selects the right point without the
operator being involved.

**The lavapipe arm §19.1 could not construct exists here**, and it does not
change §14.3's reading: 61.0 ms of CPU per frame is ~3.7 cores at 60 Hz on a
four-core part, which is why it is the one arm that missed the tick. Against it
the software host is 39× in wall-clock and 34× in CPU.

**Correction 2 moves further in the same direction.** §Consequences reports the
GPU as ~20 % cheaper in CPU; §19.5 measured it ~15 % *more* expensive; here it
is **52 % more expensive** (2.76 ms against 1.82). Two machines of three now
order it that way, so the CPU-per-frame ordering should not be carried as a
property of the host. As in §19.5 the quantity is whole-client CPU, so the
within-machine comparison is what carries it.

**Correction 1's sign is not stable either.** §14.1 recorded the software host
at ~1.9× the wgpu arm's RSS (228 against 118 MiB); §19.5 found it *lower*
(156 against 166); here it is **higher again, by 1.18×** (167 against 141). The
direction flips with the machine and the Vulkan driver, which is the strongest
form of §19.5's point: this is not a property of the host, and the ADR should
not carry a memory ratio at all.

**What is new here is the GPU arm's absolute cost.** 9.3 ms against 1.34 ms on
the reference machine and 2.5–4.2 ms on the Zen 2 — a readback-bound frame
(§18.1) on a weak integrated GPU behind DDR4. It leaves the headless GPU arm
7 ms of margin in a 16.6 ms budget, where the software host uses 296 µs. The
software host is **31× faster in wall-clock** here, the largest gap recorded;
§18.2's unpadded row-copy is the first thing to look at for anyone who needs
that arm on this class of part.

Two caveats. **The arms differ in how reproducible they are**: the wgpu arm
replicated to 1.5 % across five runs, while the software arm spread 203–309 µs
across seven — this is a 15 W mobile part, and runs were spaced with a cooldown
after the first pair. The median is quoted and the spread is the error bar.
**And machine and tree are confounded exactly as §19.8 says**, though less so:
these figures come from a tree two commits after §19's.

The viewport sweep agrees with §19.4 on the conclusion and not on the split. On
8 MiB the §SD5 pixel budget is ~1.05 Mpx, so 1920×1200 sits 2.2× over it rather
than the Zen 2's 4.4×:

| | 912×568 | 1280×800 | 1920×1200 |
| --- | --- | --- | --- |
| p50 | 186 µs | 234 µs | 296 µs |
| p99 | 3.8 ms | 4.7 ms | 6.0 ms |

4.45× the pixels costs 1.59× at p50 and 1.56× at p99, against §19.4's 1.29× and
2.2×. The median barely moves on both machines; the tail penalty is milder here,
which is what twice the L3 should buy. "Size a CPU-rasterized appliance at
1080p–1200p" survives on a third part.

## References

- [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) — the headless host and pixel streaming.
- [ADR-0128](./0128-imzero2-mesh-draw-stream-codec-lane.md) — the appliance force, the mesh lane, and the `headless_wgpu` split this ADR extends.
- [ADR-0009](./0009-environment-variable-registry.md) — the env registry the two new knobs join.
- [The survey](../adr-background-work/egui-software-backend-survey.md) — every measurement, and the corrections.
- [`VENDORING.md`](../../rust/imzero2/vendor/egui_software_backend/VENDORING.md) — upstream commit, deltas, re-sync.
- [egui_software_backend](https://github.com/DGriffin91/egui_software_backend) — upstream, MIT OR Apache-2.0.
