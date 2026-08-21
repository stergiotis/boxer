---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-08-21; nothing here is a
> decision. Provenance is three-tiered: (a) claims about this repository were
> verified against the working tree on the compile date; (b) every number in §5
> and §6 was *measured* on the compile date — one machine (32-thread x86-64
> laptop-class CPU with an integrated GPU), one build each, synthetic content,
> so read them as observations, not proofs; (c) upstream facts come from the
> `egui_software_backend` repository at commit-date 2026-04-15 and from
> crates.io metadata read on the compile date. Integration effort in §8 was an
> estimate when written; §10, added 2026-08-22, replaces it with what building
> it actually cost and measured.

# A software-only pixel host for imzero2's Rust side

## 1 Question and scope

Could [`egui_software_backend`](https://github.com/DGriffin91/egui_software_backend)
(MIT OR Apache-2.0) carry a software-only build of imzero2's Rust side, and if
so, where do the two floors sit — **hardware requirements** and **dependency
footprint** for a headless setup, with AccessKit dropped from the production
build (no `egui_mcp`)?

Three hosts share one source tree today ([ADR-0024](../adr/0024-imzero2-remote-access-browser-viewer.md),
[ADR-0128](../adr/0128-imzero2-mesh-draw-stream-codec-lane.md) M3):

| Feature | Windowing | GPU | Produces |
| --- | --- | --- | --- |
| `desktop` (default) | eframe + winit | wgpu | an on-screen window |
| `headless_wgpu` | none | wgpu, offscreen | BGRA pixels → PNG / H.264 / carrier |
| `headless` | none | none | tessellated meshes (draw-stream lane) |
| `headless_svg` | none | none | SVG files (pre-tessellation shapes) |

So imzero2 already has two GPU-less hosts. Neither produces **pixels**. That is
the gap this crate would close: a host with no Vulkan stack that still serves
PNG, H.264, and the scripted-screenshot path. Everything below is about that
gap. Replacing eframe on the desktop host is a separate, larger question and is
treated only in §8.

## 2 What the crate is

- **Size**: 5,696 lines of Rust across `src/`. Tile-based (64 px) primitive
  cache, SIMD colour paths (AVX2 / SSE4.1 / NEON) selected at runtime, scalar
  fallback, optional `rayon` parallelism.
- **`#![no_std]`** with `extern crate alloc`; `std` is a feature that only adds
  runtime SIMD detection and `ahash/std`.
- **Direct dependencies** (with `--no-default-features`): `egui`,
  `strength_reduce`, `constify` (a proc-macro published by the same author),
  `ahash`. The optional `winit` feature adds winit + softbuffer + egui-winit +
  bytemuck; `test_render` adds `egui_kittest` + `image`.
- **Adoption is thin.** 1,338 all-time / 876 recent downloads on crates.io;
  three releases; last commit 2026-04-15; the egui 0.34 bump was contributed by
  an outside party rather than the author. Treat bus factor as a first-order
  risk (§7).

Version pairing is published as 0.0.1–0.0.2 → egui 0.33, 0.0.3 → egui 0.34.
imzero2 pins egui 0.35, so the crate is nominally one minor behind.

## 3 The version gap is nominal, not real (measured)

Bumping `egui = "0.34"` to `"0.35"` in the crate's manifest and changing
nothing else, the **library compiles clean** — zero source edits, no warnings
beyond the crate's own lint set.

Only the two optional feature surfaces break, and both are surfaces a
first-party imzero2 host would not use:

| Surface | Breakage at 0.35 |
| --- | --- |
| `winit` feature | 2 errors: `ViewportBuilder` gained a `monitor` field; `ViewportCommand::SetMonitor` is a new variant to match |
| `test_render` feature | `TestRenderer::render` is `#[cfg]`-gated in `egui_kittest` 0.35; the optional dep needs `features = ["snapshot", "wgpu"]` |

Both are one-line fixes. The render core — the part imzero2 would consume — has
no 0.34→0.35 port cost at all.

## 4 imzero2 emits nothing the backend cannot render (verified against the tree)

The backend logs an error and skips `epaint::Primitive::Callback`, and its
texture handling covers `TexturesDelta` only.

- **Paint callbacks: zero.** No `PaintCallback` construction anywhere in
  `rust/imzero2`, nor in the pinned widget ring (`egui_extras` 0.35,
  `egui_table` 0.9, `egui_dock` 0.20, `egui_graphs` 0.31, `walkers` 0.56) —
  only the `egui`/`epaint` type definitions themselves.
- **`TextureId::User`: zero.** `meshlane.rs` states the invariant outright, and
  every texture in the tree is minted through `ctx.load_texture` (`image.rs`,
  `scrolling_texture.rs`, and `walkers`' own `Tile::new`), so all of them
  arrive as `Managed` ids inside `TexturesDelta`.
- **`TextureOptions`**: the backend honours `wrap_mode` and `magnification` but
  requires `magnification == minification`. imzero2 only ever passes
  `TextureOptions::LINEAR` / `NEAREST` (`scrolling_texture::filter_to_options`),
  which satisfy that. No mipmaps are requested anywhere.
- **Single viewport.** imzero2 drives `ViewportId::ROOT` only; `egui_dock`'s
  floating tabs are in-viewport `egui::Window`s, not OS viewports.

**Fidelity against a real GPU.** The crate's own CPU-vs-GPU comparison test —
the full egui demo at 1280×720, three `pixels_per_point` values × eight
renderer configurations, diffed against a wgpu render — **passes unmodified
under egui 0.35** on a real Vulkan adapter. Thresholds are 8 differing pixels at
`ppp = 1.0`, 27 at 1.0833, 15 at 1.5. At `ppp = 1.0` five of the eight
configurations matched exactly. Non-integer `ppp` is where the rounding
disagreements live — relevant, since a connected viewer reports its own
`devicePixelRatio × zoom`.

An independent check on the two internal modes: with caching on vs off, output
agrees to **≤ 2/255 per channel** over 30 frames — accumulated blend rounding,
not a structural difference.

## 5 (a) The hardware floor

### 5.1 The headline A/B

Identical synthetic content (a full-viewport grid of ~2,300–5,100 triangles of
text, the shape a dock-plus-table imzero2 view actually paints), same machine,
mean per-frame cost. The GPU column mirrors `headless.rs::render_and_readback`
exactly — tessellate, `update_texture`, render pass, `copy_texture_to_buffer`,
blocking map, unpad memcpy:

| 1920×1080, animated | ms/frame | threads |
| --- | --- | --- |
| software backend, direct mode, SIMD | **0.30** | 1 |
| software backend, direct mode, scalar (no SIMD) | **0.41** | 1 |
| wgpu on an integrated GPU + readback | 0.55 | GPU |
| wgpu on lavapipe (Mesa software Vulkan) + readback | 2.17 | all 32 |

| 3840×2160, animated | ms/frame | threads |
| --- | --- | --- |
| software backend, direct mode, SIMD | **1.70** | 1 |
| software backend, direct mode, scalar | **1.90** | 1 |
| wgpu on an integrated GPU + readback | 3.56 | GPU |
| wgpu on lavapipe + readback | 7.63 | all 32 |

One CPU thread beats both GPU paths. The GPU numbers are not a GPU indictment —
they are dominated by the readback round-trip, which a CPU renderer simply does
not have. Since **every** imzero2 pixel host needs the frame in host memory
(PNG, ffmpeg, carrier), that round-trip is unavoidable on the GPU path and free
here. Against lavapipe — the answer the repo currently documents for GPU-less
deployment ([ADR-0095](../adr/0095-airgapped-build-bundle.md),
[doc/howto/airgapped-build.md](../howto/airgapped-build.md)) — it is ~5–7×
faster on a single thread.

Caveats: the software figures exclude `ctx.tessellate` (0.03 ms at 1080p,
0.21 ms at 4K) while the GPU figures include it; adding it back does not change
the ordering. Content is synthetic; no imzero2 frame was rendered through the
backend.

### 5.2 Turn caching **off** — the crate's default is wrong for imzero2

The crate's docs say "rendering without caching is much slower and primarily
intended for testing". For a full-viewport UI the opposite holds, by an order of
magnitude:

| 1920×1080, animated | caching on | caching off + opaque clear |
| --- | --- | --- |
| SIMD, 1 thread | 3.01 ms | **0.30 ms** |
| scalar, 1 thread | 14.40 ms | **0.41 ms** |
| 3840×2160, SIMD | 23.41 ms | **1.70 ms** |

The cache path keeps an intermediate full-screen canvas and re-composites it
whenever *any* primitive changed (`lib.rs` carries a `// TODO use tiles` where
the per-tile update would go), then alpha-blends the whole canvas over the
output. That is two full-screen passes per frame regardless of what moved. The
win only materialises for a mostly-static UI over a mostly-empty background —
the egui demo's floating windows, not a dock filling the viewport.

Two consequences worth noting: `rayon` parallelises the *cache* path and does
nothing measurable for the direct path, so the appliance build needs neither;
and SIMD matters far less in direct mode (1.4×) than in cache mode (5×), because
direct mode is bandwidth-bound. To match the wgpu path's
`LoadOp::Clear(BLACK)`, the host must memset the buffer to opaque black each
frame — that cost is already inside the numbers above.

### 5.3 Memory

Peak RSS of a whole process (egui + fonts + demo state + framebuffer):

| Resolution | direct mode | cache mode |
| --- | --- | --- |
| 1280×720 | 16.1 MiB | 30.5 MiB |
| 1920×1080 | 19.5 MiB | 39.2 MiB |
| 3840×2160 | 25.8 MiB | 71.8 MiB |

Direct mode at 4K sits *below* the 31.6 MiB the framebuffer nominally occupies,
because only covered pages are ever touched. Cache mode adds roughly a second
framebuffer plus the per-primitive raster cache.

### 5.4 What the floor actually is

- **CPU**: any target rustc can build. SIMD is a runtime bonus, not a
  requirement; the scalar path costs ~1.4× in direct mode. One core suffices at
  1080p; the measurement was clock- and IPC-favourable, so a slow in-order core
  will be materially worse and has not been measured.
- **GPU**: none. No Vulkan loader, no ICD, no Mesa. This is the part the
  crate-count table in §6 does not show: today's GPU-less path needs
  `libvulkan` + lavapipe present in the image at runtime, and the airgap
  tooling already warns when they are missing (`scripts/dev/airgap-lib.sh`).
  A software host removes that system dependency entirely.
- **Memory**: framebuffer + ~12 MiB, in direct mode.

## 6 (b) The dependency floor

Crate counts are unique normal (non-dev, non-build) dependencies resolved for
`x86_64-unknown-linux-gnu`, measured with `cargo tree` against the repo's
current lockfile.

### 6.1 Where imzero2 sits today

| Build | Crates |
| --- | --- |
| `desktop` + `inspection` (the default) | 435 |
| `headless_wgpu` | 353 |
| `headless` (carrier + mesh lane, no GPU) | 318 |
| `headless_svg` / `--no-default-features` | 307 |

Swapping wgpu for the software backend puts a **GPU-less pixel host at ~320
crates** — the 318 of `headless` plus the backend's marginal cost, which is
just **`strength_reduce` and `constify`**. Everything else it needs, egui
already pulls. That is the whole footprint answer for the renderer: two crates.

### 6.2 Which means the renderer is not what sets the floor

The 307-crate GPU-less floor is set by imzero2's **unconditional** widget ring,
not by any host feature. Removing one dependency at a time from a manifest that
mirrors imzero2's, and counting what becomes unreachable:

| Dependency | Crates reachable only through it |
| --- | --- |
| `walkers` (map tiles; pulls `reqwest` + rustls + image decode) | 72 |
| `h3o` (hex grid) | 24 |
| `egui_graphs` | 15 |
| `tracing-subscriber` | 5 |
| `egui_dock` | 4 |
| `subsetter`, `mimalloc`, `egui_table`, `blake3`, `rand` | ≤ 3 each |

Cumulatively, on top of the software-renderer swap:

| Scenario | Crates |
| --- | --- |
| full widget ring (today's GPU-less floor) | 307 |
| − map widget (`walkers` + `reqwest`) | 165 |
| − `h3o` | 137 |
| − `egui_graphs`, `egui_dock`, `egui_table`, `subsetter`, `png`, `blake3`, `memmap2`, `earcutr`, `petgraph` | 97 |
| − `egui_extras`, `jiff`, `ttf-parser`, `base64`, `lru`, `roaring`, `rand`, `mimalloc` | 72 |
| − the tracing stack (bare `log` only) | 58 |
| `egui` + the software backend alone | **55** |

Each cut costs opcodes: `walkers` alone is referenced 144 times in the
generated interpreter, so shedding it means feature-gating a widget family in
the IDL and the generated dispatch, not just deleting a manifest line. Whether
that is worth doing is an independent question from the renderer — but it is
the question that actually governs the appliance's dependency footprint.

### 6.3 Dropping AccessKit

The user-facing ask — drop AccessKit for production, keep `egui_mcp` out — is
measurable but lands only on the **desktop** host:

| eframe as imzero2 configures it | Crates |
| --- | --- |
| `accesskit, default_fonts, wgpu, wayland, x11` | 250 |
| same, minus `accesskit` | 186 |

**64 crates**, essentially the whole zbus / D-Bus / AT-SPI async stack
(`zbus`, `zvariant`, `atspi*`, `async-*`, `serde`, `phf`, `toml_edit`).

Two things to keep straight:

1. **The headless hosts never had it.** They build
   `--no-default-features`, so `inspection` (and its `eframe/accesskit`) is
   already absent. Dropping AccessKit buys the appliance nothing; it is a
   desktop-build saving.
2. **`egui` 0.35 depends on `accesskit` unconditionally** — it is a plain
   `[dependencies]` entry, not a feature. What can be dropped is the *platform
   adapter* (`accesskit_winit` → `accesskit_unix` → zbus), not AccessKit
   itself. One crate stays in every build no matter what.

Also worth stating plainly: dropping the adapter drops **screen-reader support**
for the desktop app, not merely the MCP hook. `egui_mcp` reads the same tree,
but so does an actual assistive technology.

## 7 Risks

- **Bus factor.** ~1.3k downloads, one primary author, four months since the
  last commit, egui-version bumps arriving from contributors. imzero2 already
  pins a five-crate egui ring that moves as one commit
  ([the ring note in `rust/imzero2/Cargo.toml`](../../rust/imzero2/Cargo.toml)),
  and this would be a sixth crate that must move with it — held back today by
  `egui_graphs`, and tomorrow possibly by this one. Mitigation, given 5.7k
  lines and a permissive licence, is vendoring; §3 shows the maintenance
  delta across one egui minor was zero lines for the core.
- **Non-integer `pixels_per_point`.** Fidelity is exact at `ppp = 1.0` and
  within tens of pixels at 1.0833 / 1.5. A carrier viewer reports its own
  scale, so served frames will usually be non-integer. Whether tens of
  differing pixels matter depends on what consumes the frame — a video stream,
  yes; a golden-image test, no.
- **No mipmaps, and `minification` must equal `magnification`.** Satisfied
  today; it is a constraint on future texture code, not a current defect.
- **Cache mode is the crate's tested-and-advertised path**, and §5.2 recommends
  the other one. Direct mode is covered by the upstream comparison test (four
  of its eight configurations), so this is not untested territory — but it is
  the less-travelled one.
- **The measurements are synthetic.** No imzero2 opcode stream was rendered
  through the backend. The next step that would settle it is §8 O1.

## 8 Costed options

**O1 — a `headless_soft` feature on the existing headless host.** The seam is
one function: `headless.rs::render_and_readback(ctx, out, screen, frame)`
already has exactly the right signature, and `ColorFieldOrder::Bgra` matches its
`TARGET_FORMAT = Bgra8Unorm`. The body collapses to tessellate → clear →
`EguiSoftwareRender::render` into `frame`; `WgpuState`, the padded staging
buffer, the async map, the poll, and the unpad loop all disappear. Every
downstream consumer (`FrameSink`, PNG dump, `encoderpipe`, `wscarrier`) already
takes tightly-packed BGRA and is untouched. Estimate: small — one struct, one
function, one feature flag, plus a `Vec<u8>`↔`&mut [[u8; 4]]` cast. This is the
option that would replace §5's synthetic numbers with real ones.

**O2 — O1, then trim the widget ring** behind feature flags, per §6.2. Real
footprint work, and independent of the renderer. Needs an IDL/codegen story for
optional opcode families before it is costable.

**O3 — replace eframe on the desktop host** with the crate's winit + softbuffer
integration. Not recommended on this evidence: it trades a maintained
windowing/IME/clipboard/HiDPI layer for a thin one, to save GPU usage on a
machine that has a GPU. The AccessKit saving in §6.3 is available without it —
by flipping one eframe feature.

**O4 — do nothing.** The mesh draw-stream lane ([ADR-0128](../adr/0128-imzero2-mesh-draw-stream-codec-lane.md))
is already the GPU-less answer for the carrier, and it moves less data than
pixels. O1's value is precisely the cases the mesh lane does not cover: PNG
dumps, scripted screenshot capture ([ADR-0154](../adr/0154-headless-carrier-tree-and-driver.md)),
the H.264 file sink, and any viewer that cannot execute the mesh lane.

## 9 Open questions

- What does a real imzero2 frame cost through the backend, at the resolutions
  and cadences the carrier actually serves? (Answered by building O1.)
- Does the direct-mode "composite over whatever is in the buffer" behaviour
  interact badly with the carrier's damage tracking, or is the per-frame opaque
  clear enough?
- Is the `ppp ≠ 1.0` rounding visible in an H.264 stream at the bitrates
  [ADR-0024](../adr/0024-imzero2-remote-access-browser-viewer.md) targets?
- Would vendoring (§7) be preferable to a crates.io dependency from day one,
  given the version-ring coupling?

## 10 What building it actually cost (added 2026-08-22)

§8's O1 was built and committed as the `headless_soft` feature. This section
records what that changed about the estimates above; §§1–9 are left as they
were written on 2026-08-21.

### 10.1 The estimates that held

- **The seam was one function.** `render_and_readback` kept its signature; the
  CPU body is tessellate → clear → rasterize. The wgpu state, the padded
  staging buffer, the async map, the poll and the unpad loop all disappeared,
  and no downstream consumer changed.
- **The dependency cost was three crates**, exactly the ones §6.1 predicted:
  `egui_software_backend`, `constify`, `strength_reduce`. `headless` 318 →
  `headless_soft` **321**, with **no wgpu-family crate in the graph**. Binary
  45.6 MB → 38.9 MB.
- **Nothing imzero2 paints was unrenderable** (§4). No paint callback ever
  reached the backend's error path across the whole gallery.

### 10.2 Two things the survey did not anticipate

Both are cargo behaviours rather than rendering ones, and both bite any
vendored Rust source in this tree:

- **`exclude` does not spare vendored code from the lint gates.** `cargo fmt
  --all` reaches path dependencies, and `cargo clippy --workspace -- -D
  warnings` lints them. `check.sh` now names first-party packages instead of
  `--all`, and both vendored crate roots carry `#![allow(clippy::all)]`.
- **A nested `[workspace]` inside the imzero2 workspace directory is rejected
  outright**, even for an excluded path. The vendored manifest's `[workspace]`
  (and its inert `[workspace.lints.*]`) had to go, with both packages named in
  imzero2's `exclude` instead.

### 10.3 Fidelity, measured on the real gallery

`scripts/dev/play-screenshot-tour.sh` run end to end on each host: 66 scenes,
92 PNGs, and **the same single failure on both** — `03_detail_glosses`, a
driver-trace locator miss, so pre-existing and host-independent.

A second wgpu tour gives a per-scene reproducibility floor, which turns out to
be necessary: several play panels render time-dependent content (a "ran N s
ago" line, per-pass timings), and one scene is outright nondeterministic. All
figures below mask the bottom 120 px, where the two live status lines sit.

| over all 92 images | pixels | share |
| --- | --- | --- |
| identical | 184,545,441 | **98.2796 %** |
| Δ = 1 (rounding) | 2,335,484 | 1.2438 % |
| Δ 2..31 | 394,221 | 0.2099 % |
| **Δ ≥ 32 (plausibly visible)** | **500,854** | **0.2667 %** |
| *the same measure, wgpu vs wgpu* | *418,959* | *0.2231 %* |

The last row is the point: the visible-difference budget between the two
renderers is barely above what two runs of the **same** renderer already
produce. Per scene, 79 of 92 images have a renderer delta more than twice
their own noise floor; the other 13 are the timing-dependent panels, where the
two cannot be separated at all.

**The dominant signature is one line, repeated.** For **65 of 92** images every
visible difference sits on **six scanlines or fewer**, and in the common case
on exactly three — the dock tab-bar separators. wgpu paints that row
`(29,32,33)`, the CPU rasterizer `(91,94,96)`: a 62/255 gap on a single
one-pixel row, with the rows above and below differing by a handful of pixels
each. So it is a hairline drawn at a different effective opacity, not coverage
spread across two rows. At 1920×1200 that is 2,123 pixels — 0.1 % of the
frame, and the entire "visible" budget for most scenes.

At the other end, `34_fsbrowser_list` has 4.73 % of pixels differing and
**one** pixel at Δ ≥ 32: a broad flat region off by exactly 1/255.

### 10.4 A trap worth recording

`08_sankey` first looked like a real rasterizer defect — 11.9 % of pixels
differing, 208 k of them visible, large flat areas in different palette
colours. It is not: **six wgpu renders of that scene disagree with each other
by up to 14.8 %**, more than wgpu disagrees with the CPU renderer. The same
palette lands on different regions from run to run.

Chasing it produced a false positive on the way. Turning off the crate's
`with_convert_tris_to_rects` and `with_allow_raster_opt` — the two whose docs
say things "*should*" look the same — appeared to halve that scene's
difference. It was a luckier draw from the same distribution, and the change
was reverted. **Those two flags remain unevaluated**: settling whether they
cost fidelity needs a reproducible scene, and this gallery's most mesh-heavy
scenes are exactly the ones that are not reproducible.

### 10.5 What is still not measured

- Per-frame cost inside the real host. The tour's per-scene wall clock is
  dominated by process launch, ClickHouse and settle time, and the runs
  contended for the machine, so nothing in §5.1 is confirmed or refuted by it.
- `pixels_per_point ≠ 1.0`, which is what a carrier viewer reports. The whole
  gallery runs at 1.0.
- The two raster-optimisation flags (§10.4).
