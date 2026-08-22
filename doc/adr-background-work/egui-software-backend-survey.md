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

> **Superseded by §10.6 (2026-08-22).** The ordering below was measured on
> synthetic content and does **not** survive contact with a real imzero2
> frame: measured inside the host, the CPU rasterizer is ~2.4× *slower* than
> either wgpu path, not faster. Keep reading for how the synthetic case
> behaves; take §10.6 for what the host actually does.

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
was reverted. §10.6 later settled the flags properly, on a reproducible scene
with a real timer: reverting was right, by a factor of eighteen.

### 10.5 What is still not measured

- `pixels_per_point ≠ 1.0`, which is what a carrier viewer reports. The whole
  gallery runs at 1.0, and §3 found that non-integer scaling is exactly where
  the rounding disagreements concentrate.
- Anything about a machine that is not this one. §10.6's ordering could plainly
  differ on a host with a weak CPU and a competent GPU, or with fewer cores
  for llvmpipe to spread over.

*(Per-frame cost in the real host was on this list until §10.6 measured it,
and the two raster-optimisation flags until §10.6 settled them.)*

## 11 Per-frame cost in the real host (added 2026-08-22)

§5's numbers came from a synthetic benchmark. `IMZERO2_HEADLESS_RASTER_STATS`
now samples the real thing at the one point both pixel hosts share, so §5.1's
central claim can be checked instead of assumed. It does not hold.

### 11.1 The measurement

Play launched headless per
[the non-interactive launch how-to](../howto/launch-apps-non-interactively.md),
continuous cadence, 1920×1200, 600 frames per arm, one arm at a time on an
otherwise idle machine. A frame is ~8,840 triangles across ~31 clipped
primitives. Figures are the rasterize step only — tessellation is reported
separately because it is identical work under either host, and it costs
0.07–0.10 ms in all arms.

| arm | p50 | p90 | p99 |
| --- | --- | --- | --- |
| wgpu on the integrated GPU | **1.34 ms** | 1.71 | 3.00 |
| wgpu on lavapipe (Mesa software Vulkan) | **1.33 ms** | 1.60 | 2.14 |
| **CPU rasterizer (`headless_soft`)** | **3.17 ms** | 3.93 | 4.57 |
| CPU, `with_caching(true)` | 3.56 ms | 5.81 | 14.56 |
| CPU, both raster optimisations off | 56.18 ms | 61.68 | 75.72 |

### 11.2 What that overturns

**§5.1 is wrong about real content, in both directions.** It reported the CPU
path at 0.30 ms and lavapipe at 2.17 ms on synthetic content at a similar size;
the real frame costs the CPU path **10× more** and lavapipe **1.6× less**. The
ordering inverts: on this machine the CPU rasterizer is **~2.4× slower** than
either wgpu arm, and lavapipe is indistinguishable from the real GPU.

The synthetic case underestimated a real frame in the way a CPU rasterizer
cares about most. It drew ~2,500 triangles of text over one panel background;
a play frame draws ~8,840 across a dock of nested panels, each painting its own
full-area background, so covered pixels and overdraw are both far higher. A GPU
absorbs overdraw; a scanline rasterizer pays for it. That is the whole gap, and
it is a reason to distrust *any* synthetic figure for this class of work rather
than a reason to distrust this crate.

**The `bytes` intuition from §5.1 — that the CPU wins because it skips the
readback — also fails.** It skips it, and is still slower, because rasterizing
the frame costs more than the readback saves.

### 11.3 What it does not overturn

- **`with_caching(false)` was the right call** (§5.2), now on real content: the
  cache costs 12 % at p50 and 3.2× at p99, exactly the "re-composite a whole
  second canvas" shape §5.2 predicted.
- **The two raster optimisations are load-bearing**, which §10.4 left open.
  Turning them off costs **18×** and buys nothing — the pixel diff against
  wgpu is unchanged to within a few hundred sub-threshold pixels, and the
  visible-delta count is identical. Their doc comments hedge ("things *should*
  look the same"); on this gallery they do.
- **The absolute cost is not a problem.** 3.17 ms/frame is a ~315 fps ceiling;
  at the headless host's default 60 Hz it is ~19 % of one core, and the ADR-0062
  reactive cadence means most frames are not drawn at all.

### 11.4 So what is `headless_soft` actually for

Not speed. On this machine it costs about 1.8 ms per rendered frame against
lavapipe, and buys: no Vulkan loader, no ICD, no Mesa in the runtime image
(§10.1 — three crates, 6.8 MB of binary, and a dependency the airgap tooling
currently warns about when it is missing). That is a deployment-surface trade,
and it should be argued on those terms rather than on throughput.

## 12 Code footprint, and how well the rasterizer is optimised (added 2026-08-22)

### 12.1 Footprint

Symbol sizes from the two built hosts (`nm --size-sort -S`, attributed by the
leading path segment of the demangled name), and source lines from the crates
they come from:

| | machine code (`.text`) | Rust source |
| --- | --- | --- |
| GPU stack: `naga`, `wgpu-core`, `wgpu-hal`, `wgpu`, `wgpu-types`, `egui-wgpu`, `ash`, `glow`, the allocators | **2,367,505 B** | 265,785 lines |
| CPU stack: `egui_software_backend`, `constify`, `strength_reduce`, `bytemuck` | **138,103 B** | 4,690 lines |

**17× less machine code, 57× less source.** Half the GPU stack's code is one
crate: `naga` at 1,166,900 B, a shader compiler — which is exactly the kind of
thing a software rasterizer does not need to carry.

Whole-binary `.text` drops further than the attributed difference, 13.42 MB →
9.83 MB, because removing wgpu also removes its share of the generic
instantiations that land in `core`/`alloc` (`core` alone: 2.33 MB → 1.52 MB).

### 12.2 What a frame actually costs it

Measured on the same play frame as §11 (1920×1200, ~8,835 triangles), counting
triangle areas directly from the primitives rather than trusting the crate's own
buckets — those record a triangle's *pre-clip bounding box* and *half* a rect's,
in opposite directions, which is a good way to be wrong by 5× in either
direction:

| | pixels |
| --- | --- |
| frame | 2,304,000 |
| true triangle area (clip-bounded) | 9,360,547 |
| **overdraw** | **4.1×** |
| triangle *bounding-box* area | 55,170,777 (5.9× the true area) |

That last row is why the rasterizer's span-walking matters: egui draws text as
thin quads, so a rasterizer that filled bounding boxes would do six times the
work.

Holding the UI fixed and scaling only `IMZERO2_HEADLESS_PIXELS_PER_POINT` (so
the triangle count stays at ~7,440 and only the pixel count moves) separates
the two halves of the cost:

| ppp | target | p50 |
| --- | --- | --- |
| 1.0 | 1280×800 (1.02 Mpx) | 1.83 ms |
| 1.25 | 1600×1000 (1.60 Mpx) | 2.54 ms |
| 1.5 | 1920×1200 (2.30 Mpx) | 3.11 ms |

≈ **0.81 ms fixed + 1.0 ns per frame pixel** (±5 % — the fit is mildly
superlinear, presumably cache). At 1920×1200 that is roughly a quarter fixed,
three quarters fill. Per *covered* pixel the fill rate is ~4.1 Gpx/s
single-threaded — about 0.9 pixels per cycle, or ~33 GB/s of read-modify-write
traffic, which is the right order for one core against DDR5 plus a large L3.
Stack sampling (`eu-stack`, on-CPU samples only) agrees: the leaves are
`color::avx2::dispatch_avx2` and `egui_blend_u8_slice_one_src_tinted_fn_avx2`,
i.e. the blend inner loop, not setup. 76 % of triangles and 75 % of rects need
alpha blending; 84 % of triangles carry varying vertex colours, so most of the
frame takes the general interpolating path rather than a flat fill.

**Read that as: the inner loop is in good shape, and there is little headroom
in it.** The gains available are not in the blend — they are in doing less of
it, or doing it on more than one core.

### 12.3 The path that is fast is the path that is single-threaded

`render_direct` — the uncached path §5.2 chose and §10 committed — has **no
rayon variant**. All three of the crate's parallel sites (`render_prims_to_cache`,
`update_canvas_from_cached`, `blit_canvas_to_buffer`) are on the *caching*
path. So the configuration in the tree is single-threaded by construction, and
the `rayon` feature genuinely buys it nothing — which is what §5.2 observed,
without noticing *why*.

Turn both on together and the picture inverts. Same frame, same 1920×1200,
600 frames, p50 of the rasterize step:

| configuration | p50 | p90 | p99 |
| --- | --- | --- | --- |
| direct, no rayon — **what is committed** | 3.17 ms | 3.93 | 4.57 |
| cached, no rayon | 3.56 ms | 5.81 | 14.56 |
| cached + rayon, 1 thread | 2.13 ms | 3.51 | 14.10 |
| cached + rayon, 8 threads | 0.75 ms | 1.14 | 5.19 |
| **cached + rayon, 12–16 threads** | **0.70 ms** | **1.05** | 4.5–4.9 |
| cached + rayon, 24 threads | 0.92 ms | 1.28 | 4.65 |
| cached + rayon, 32 threads (rayon's default here) | 1.08 ms | 1.39 | 4.64 |

**4.5× faster than the committed configuration at p50, 3.7× at p90, and the
tail is unchanged.** It is also faster than either wgpu arm in §11 (1.34 ms on
the GPU, 1.33 ms on lavapipe) — so §11.4's "not speed" conclusion holds only
for the configuration currently in the tree.

Note the 1-thread row: the rayon code path is faster than the non-rayon one
*even with a single worker* (2.13 vs 3.56 ms), so this is not purely
parallelism — the tiled iteration it uses has better locality.

Fidelity does not pay for it. Against the wgpu render of the same scene, the
cached+rayon output is marginally **closer** than the direct output
(`08_treemap_category`: 2,505 visible deltas vs 2,758, max Δ 95 vs 112), and on
table-shaped scenes the two CPU modes differ by six to ten pixels at Δ=1.

What it costs: **two crates** (`rayon`, `rayon-core`), 168 KB of binary, a
second full-screen canvas (~9 MB at 1920×1200), and a thread pool — which for
an appliance host is a real question, not a free lunch. It also needs a
thread-count policy: rayon's default on this machine (32) is 1.5× off the
optimum, and the optimum is around half the hardware threads.

### 12.4 Consequence

§5.2 and the `with_caching(false)` decision it justified were measured in a
world without rayon, and are wrong in one with it. Correcting that is a change
to a committed default plus a new dependency and a thread pool, so it is a
decision rather than a fix, and it is left open here.

## 13 Scaling, and whether `rayon` is needed (added 2026-08-22)

§12.3 found a configuration 4.5× faster than the committed one. Two follow-up
questions: how far does that scale, and does it have to be `rayon` that gets
it there.

### 13.1 With pixels

Single-threaded, direct path, UI held fixed and only `pixels_per_point` moved:

| target | pixels | p50 | ns/pixel |
| --- | --- | --- | --- |
| 1920×1200 | 2.30 M | 2.98 ms | 1.29 |
| 2880×1800 | 5.18 M | 5.21 ms | 1.00 |
| 3840×2400 | 9.22 M | 12.41 ms | 1.35 |

Linear to about 5 Mpx, then a knee — at 4K the canvas and frame buffer are
~37 MB each and stop fitting alongside each other in L3. §12.2's
"0.81 ms fixed + 1.0 ns/pixel" holds up to the knee and understates past it.

### 13.2 With threads

Cached + rayon, same frame, sweeping `RAYON_NUM_THREADS`:

| threads | 1920×1200 (19 tile rows) | 3840×2400 (37 tile rows) |
| --- | --- | --- |
| 1 | 1.73 ms — 1.00× | 18.45 ms — 1.00× |
| 2 | 1.26 ms — 1.38× | |
| 4 | 0.87 ms — 1.98× | 6.26 ms — 2.95× |
| 8 | 0.71 ms — 2.43× | 4.18 ms — 4.41× |
| 12 | **0.68 ms — 2.54×** | |
| 16 | 0.76 ms — 2.28× | **3.63 ms — 5.08×** |
| 24 | 0.95 ms — 1.82× | |
| 32 | 1.08 ms — 1.60× | 3.64 ms — 5.07× |

**It scales badly, and predictably so.** Peak speedup is 2.5× at 1920×1200 and
5.1× at 3840×2400, on a machine with 32 hardware threads — 21 % and 32 %
parallel efficiency at their respective optima. Fitting Amdahl to the low end
gives a serial fraction of ~33 % at 1200p and ~14 % at 2400p.

The reason is the decomposition, not the machine. Two of the three parallel
sections split the frame into **rows of 64-pixel tiles** — 19 of them at
1920×1200, 37 at 3840×2400 — so the available width is the tile-row count, and
resolution is what buys more of it. The third splits the **31 clipped
primitives**, which are wildly uneven (one full-viewport background against
runs of glyphs), so the largest single primitive bounds that section however
many threads are free. Past the optimum both effects invert into contention and
the curve turns back down.

Practical consequence: the useful setting is around **half the hardware
threads**, and rayon's default (all of them) is 1.5× off the optimum at 1200p.

### 13.3 With content

Not isolated, and worth saying so rather than implying otherwise. The play
table virtualises its rows, so widening the query from 20 to 2,000 rows moves
the triangle count from 8,841 to 8,845 and the frame cost not at all. The
per-primitive half of the cost is the 0.81 ms fixed term in §12.2 — about a
quarter of a 1200p frame — but nothing here varies it independently.

### 13.4 Is it `rayon`?

Three questions stacked inside one, and they have different answers.

**Is parallelism needed?** To beat the GPU, yes. Single-threaded direct is
2.98 ms against wgpu's 1.30 ms at 1920×1200; cached+rayon at 12–16 threads is
0.70 ms. Without threads the software host is a GPU-less *fallback*; with them
it is faster than the thing it replaces.

**Does it need a persistent pool?** Yes — this is the load-bearing answer. The
three sites are ordinary data-parallel shapes (`par_chunks_mut` twice, an
order-preserving `par_iter().map().collect()` once) that `std::thread::scope`
expresses directly. But scope spawns OS threads per call, and per *empty*
parallel section that costs:

| threads | `std::thread::scope` | rayon (warm pool) |
| --- | --- | --- |
| 4 | 49.2 µs | 5.5 µs |
| 8 | 80.4 µs | 6.5 µs |
| 16 | **153.6 µs** | **5.7 µs** |
| 32 | 288.1 µs | 17.1 µs |

At 16 threads that is 27× rayon's dispatch, and there are three sections per
frame: ~460 µs of pure overhead against a ~700 µs frame. Spawn-per-call would
give back roughly two thirds of the win and land slower than wgpu.

**Does it need `rayon` specifically?** No. Any persistent pool would do, and
the shapes here use none of rayon's cleverness beyond work-stealing over the
uneven primitive list. The trade is 2 crates and 168 KB against ~150 lines of
condvar-and-work-queue that would then be ours to maintain and get right. On a
321-crate graph, and for a measured 4.5×, the crates look like the cheaper side
of that — but it is a judgement, not a measurement.

One thing the table does explain independently: rayon's own dispatch cost grows
from 5.7 µs at 16 threads to 17.1 µs (p90 43.5) at 32, which is part of why 32
threads regressed in §13.2.

### 13.5 Caveat

One machine, 32 hardware threads, one frame. The tile-row bound is structural
and should transfer; the specific optimum thread count and the L3 knee will not.
