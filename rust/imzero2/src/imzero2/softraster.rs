//! CPU rasterizer for the `headless_soft` pixel host.
//!
//! Drop-in companion to `headless.rs`'s `Gpu`: same five-method surface, same
//! output contract — tightly-packed sRGB **BGRA** in host memory, which is
//! what every downstream consumer already takes (the PNG dump, the ADR-0154
//! capture request, `encoderpipe`'s `rawvideo -pix_fmt bgra` input, and the
//! carrier). `headless.rs` picks one of the two behind the `Raster` alias, so
//! nothing downstream of a frame knows which one produced the bytes.
//!
//! What disappears relative to the wgpu path: the instance/adapter/device
//! request, the offscreen texture, the 256-byte-row-aligned staging buffer,
//! `copy_texture_to_buffer`, the async map plus its blocking poll, and the
//! row-by-row unpad copy. The rasterizer writes straight into the caller's
//! frame buffer, so the frame is already where the sinks want it.
//!
//! Configuration, and why (measurements in
//! `doc/adr-background-work/egui-software-backend-survey.md` §§12–13):
//!
//! - **`ColorFieldOrder::Bgra`** matches the wgpu host's
//!   `TextureFormat::Bgra8Unorm`, so the two hosts are byte-comparable.
//! - **`with_caching(true)` plus the crate's `rayon` feature.** These two are
//!   one decision, because the crate's parallelism lives entirely on its
//!   caching path — `render_direct` has no parallel variant at all. Uncached
//!   is the faster of the two *single-threaded* (3.17 ms vs 3.56 ms at
//!   1920×1200), which is why this host started there; cached with a warm pool
//!   is 0.70 ms, 4.5× better than either, and quicker than the wgpu host it
//!   stands in for. Fidelity is unchanged — marginally closer to the wgpu
//!   render, if anything.
//! - **Half the hardware threads.** The work splits into rows of 64-pixel
//!   tiles, so useful parallel width is the tile-row count (19 at 1920×1200,
//!   37 at 3840×2400) and one very uneven primitive list; past the peak,
//!   contention takes it back. Measured speedup peaks at 2.5× / 12 threads at
//!   1920×1200 and 5.1× / 16 at 3840×2400 on a 32-thread machine. Rayon's own
//!   default is every hardware thread, ~1.5× off the optimum.
//!
//! The costs of that choice, stated plainly, because they are larger than the
//! canvas alone suggests. Measured peak RSS of this process at 1920×1200:
//!
//! | pool | RSS | p50 | p99 |
//! | --- | --- | --- | --- |
//! | uncached, 1 thread (what this host used to be) | 57 MiB | 3.17 ms | 4.57 ms |
//! | cached, 1 | 171 MiB | 2.07 ms | 15.7 ms |
//! | cached, 4 | 248 MiB | 0.93 ms | 5.6 ms |
//! | cached, 8 | 282 MiB | 0.73 ms | 5.4 ms |
//! | cached, 16 | 416 MiB | 0.76 ms | 4.5 ms |
//!
//! So **`IMZERO2_HEADLESS_RASTER_THREADS` is a memory knob as much as a speed
//! one** — roughly 15 MiB per worker, most of it per-thread allocator arenas
//! (imzero2 runs mimalloc) and the in-flight per-primitive raster each worker
//! holds. The p50 plateau starts around 8 workers; past that only p99 improves,
//! at ~17 MiB each. Against the uncached path this host replaced, that is 3–7×
//! the memory, so it is no longer the one-core-and-nothing-else proposition it
//! was, and a memory-constrained deployment should set the knob deliberately
//! rather than take the default.
//!
//! The tail is this configuration's weak point in general: even at 16 workers
//! p99 is 4.5 ms against ~2.0 ms for the same frame through wgpu on lavapipe,
//! because a frame in which any primitive changed re-composites the whole
//! canvas (the crate carries a `// TODO use tiles` exactly there). Latency-
//! sensitive streaming should weigh that against the better p50.

use egui_software_backend::{BufferMutRef, ColorFieldOrder, EguiSoftwareRender};

use crate::imzero2::cputopo;
use crate::imzero2::headless::HeadlessError;

/// Cap handed to egui for its font-atlas texture, and the ceiling
/// [`crate::imzero2::headless`] clamps viewer resize requests to. The wgpu
/// host reads this from the adapter; with no adapter we pick a value large
/// enough that the atlas is never the limiting factor, matching the GPU-less
/// SVG host (`headless_svg.rs`). egui only uses it to bound atlas growth, so
/// an over-estimate is harmless.
const MAX_TEXTURE_SIDE: usize = 8192;

/// Worker count for the rasterizer's pool. `IMZERO2_HEADLESS_RASTER_THREADS`
/// wins when set to a positive value; otherwise half the hardware threads,
/// floored at one. See the module doc for why half.
fn raster_threads() -> usize {
    let hw = std::thread::available_parallelism().map_or(1, std::num::NonZeroUsize::get);
    match std::env::var("IMZERO2_HEADLESS_RASTER_THREADS")
        .ok()
        .and_then(|v| v.trim().parse::<usize>().ok())
    {
        Some(n) if n > 0 => n,
        _ => (hw / 2).max(1),
    }
}

/// CPU render state: the rasterizer plus the geometry it was last sized to.
/// There is no device, no queue and no staging buffer — the whole struct is
/// the renderer's own texture cache, a worker pool and three numbers.
pub struct Soft {
    render: EguiSoftwareRender,
    width_px: u32,
    height_px: u32,
    pixels_per_point: f32,
    /// Mirrors `Gpu::max_texture_side` so the two are interchangeable behind
    /// the `Raster` alias. Constant here — see [`MAX_TEXTURE_SIDE`].
    pub max_texture_side: usize,
    /// Cache topology, probed once. `None` when it cannot be read — containers
    /// and VMs commonly report no L3 at all — in which case no advice is given.
    topo: Option<cputopo::Topology>,
    /// Whether the last geometry was reported as over the L3 budget, so a
    /// resize only speaks up when the verdict actually changes.
    over_budget: bool,
}

impl Soft {
    /// Infallible in practice; the `Result` exists so the call site reads the
    /// same under either host (`Raster::new(..)?`).
    pub fn new(
        width_px: u32,
        height_px: u32,
        pixels_per_point: f32,
    ) -> Result<Self, HeadlessError> {
        // Sizing the global pool rather than letting rayon default to every
        // hardware thread. `build_global` fails only if something already
        // initialised it, in which case that pool is what we get — worth a
        // line in the log, not worth failing a render host over.
        let threads = raster_threads();
        if let Err(e) = rayon::ThreadPoolBuilder::new().num_threads(threads).build_global() {
            tracing::warn!(error = %e, threads, "rayon global pool already initialised — keeping it");
        }
        // Advice, not enforcement: this process cannot know what else the box
        // is for, so it names the pin rather than taking it.
        let topo = cputopo::probe();
        match &topo {
            Some(t) if t.domains_spanned > 1 => tracing::warn!(
                domains = t.domains_spanned,
                l3_mib = t.l3_bytes / (1024 * 1024),
                threads,
                "worker pool may be scheduled across {} L3 domains; pinning it to one was measured \
                 ~1.33x faster while the frame fits that domain — e.g. taskset -c {} (or systemd \
                 CPUAffinity=)",
                t.domains_spanned,
                t.pin_hint
            ),
            _ => {}
        }
        tracing::info!(
            width_px,
            height_px,
            pixels_per_point,
            threads,
            hardware_threads =
                std::thread::available_parallelism().map_or(1, std::num::NonZeroUsize::get),
            simd = cfg!(any(target_arch = "x86_64", target_arch = "aarch64")),
            l3_domains = topo.as_ref().map(|t| t.domains_spanned),
            l3_budget_px = topo.as_ref().map(cputopo::Topology::max_pixels),
            "headless CPU rasterizer up (no wgpu, no Vulkan loader, no ICD)"
        );
        let mut soft = Self {
            render: EguiSoftwareRender::new(ColorFieldOrder::Bgra).with_caching(true),
            width_px,
            height_px,
            pixels_per_point,
            max_texture_side: MAX_TEXTURE_SIDE,
            topo,
            over_budget: false,
        };
        // Also at startup, not only on resize: a host launched straight into a
        // 4K viewport would otherwise never hear about it.
        soft.advise_working_set(width_px, height_px);
        Ok(soft)
    }

    /// Consume a pass's texture deltas without rendering. Used when no sink
    /// wants pixels (no viewer, no dump): deltas are incremental, so dropping
    /// them would permanently corrupt the renderer's texture state for a
    /// viewer that connects later. Mirrors `Gpu::apply_textures_only`.
    pub fn apply_textures_only(&mut self, textures_delta: &egui::TexturesDelta) {
        self.render.set_textures(textures_delta);
        self.render.free_textures(textures_delta);
    }

    /// Adopt a new physical size and scale (viewport resize). Nothing to
    /// rebuild: the frame buffer is the caller's and is resized per frame, and
    /// the rasterizer's texture cache is size-agnostic. It does drop its
    /// primitive cache on a size change, which costs nothing here because
    /// caching is off.
    pub fn resize(&mut self, width_px: u32, height_px: u32, pixels_per_point: f32) {
        self.width_px = width_px;
        self.height_px = height_px;
        self.pixels_per_point = pixels_per_point;
        self.advise_working_set(width_px, height_px);
    }

    /// Say something when the frame's hot working set crosses the L3 budget,
    /// and only when the verdict changes — a viewer dragging a window edge
    /// resizes on every frame.
    fn advise_working_set(&mut self, width_px: u32, height_px: u32) {
        let Some(topo) = &self.topo else { return };
        let px = u64::from(width_px) * u64::from(height_px);
        let over = px > topo.max_pixels();
        if over == self.over_budget {
            return;
        }
        self.over_budget = over;
        if over {
            // A 16:10 frame at the budget: w = sqrt(px * a), h = sqrt(px / a).
            // Scaling sqrt(px) by `a` directly would square the aspect ratio.
            const ASPECT: f64 = 1.6;
            let budget = topo.max_pixels() as f64;
            let (w, h) = ((budget * ASPECT).sqrt(), (budget / ASPECT).sqrt());
            tracing::info!(
                width_px,
                height_px,
                frame_px = px,
                l3_budget_px = topo.max_pixels(),
                l3_mib = topo.l3_bytes / (1024 * 1024),
                "frame working set (frame buffer + canvas) no longer fits one L3 domain — cost \
                 per pixel rises past here, and pinning the pool stops helping; roughly {:.0}x{:.0} \
                 is the budget on this machine",
                w,
                h
            );
        } else {
            tracing::info!(frame_px = px, "frame working set fits one L3 domain again");
        }
    }

    /// Rasterize one already-tessellated pass into `frame` as tightly-packed
    /// BGRA. Same signature and same output as `Gpu::render_and_readback`,
    /// minus the readback — there is nothing to read back from.
    pub fn render_and_readback(
        &mut self,
        clipped: &[egui::ClippedPrimitive],
        textures_delta: &egui::TexturesDelta,
        frame: &mut Vec<u8>,
    ) -> Result<(), HeadlessError> {
        let (w, h) = (self.width_px as usize, self.height_px as usize);

        frame.resize(w * h * 4, 0);
        // `[u8; 4]` is align-1 and `w * h * 4` divides by 4, so this cast can
        // only succeed; `bytemuck` is here purely to spell it without
        // `unsafe`, which the workspace lints deny.
        let pixels: &mut [[u8; 4]] = bytemuck::cast_slice_mut(frame.as_mut_slice());
        // The rasterizer composites over whatever the buffer already holds and
        // never guarantees full coverage, so the frame has to start from a
        // known state. Opaque black is what the wgpu host's
        // `LoadOp::Clear(wgpu::Color::BLACK)` leaves, which keeps the two
        // hosts byte-comparable on any pixel the UI does not paint.
        pixels.fill([0, 0, 0, 255]);

        let mut buffer = BufferMutRef::new(pixels, w, h);
        // `self.pixels_per_point`, not the pass's: it is the same value —
        // `run_main_loop` feeds one scale into both `RawInput` and `resize` —
        // and taking it from state is what keeps this symmetric with the wgpu
        // host, which reads its own `ScreenDescriptor`.
        self.render.render(&mut buffer, clipped, textures_delta, self.pixels_per_point);
        Ok(())
    }
}
