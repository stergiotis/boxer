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
//! Two settings differ from the vendored crate's defaults, both deliberate:
//!
//! - **`ColorFieldOrder::Bgra`** matches the wgpu host's
//!   `TextureFormat::Bgra8Unorm`, so the two hosts are byte-comparable.
//! - **`with_caching(false)`.** The crate's default is the other way round and
//!   its docs call the uncached path "primarily intended for testing", but its
//!   cache keeps a second full-screen canvas and re-composites all of it
//!   whenever any primitive changed — two extra full-screen passes per frame
//!   regardless of what actually moved. That wins for a few floating windows
//!   over an empty background and loses by roughly 10× for a dock filling the
//!   viewport, which is what imzero2 paints. Measurements:
//!   `doc/adr-background-work/egui-software-backend-survey.md`.

use egui_software_backend::{BufferMutRef, ColorFieldOrder, EguiSoftwareRender};

use crate::imzero2::headless::HeadlessError;

/// Cap handed to egui for its font-atlas texture, and the ceiling
/// [`crate::imzero2::headless`] clamps viewer resize requests to. The wgpu
/// host reads this from the adapter; with no adapter we pick a value large
/// enough that the atlas is never the limiting factor, matching the GPU-less
/// SVG host (`headless_svg.rs`). egui only uses it to bound atlas growth, so
/// an over-estimate is harmless.
const MAX_TEXTURE_SIDE: usize = 8192;

/// CPU render state: the rasterizer plus the geometry it was last sized to.
/// There is no device, no queue and no staging buffer — the whole struct is
/// the renderer's own texture cache and three numbers.
pub struct Soft {
    render: EguiSoftwareRender,
    width_px: u32,
    height_px: u32,
    pixels_per_point: f32,
    /// Mirrors `Gpu::max_texture_side` so the two are interchangeable behind
    /// the `Raster` alias. Constant here — see [`MAX_TEXTURE_SIDE`].
    pub max_texture_side: usize,
}

impl Soft {
    /// Infallible in practice; the `Result` exists so the call site reads the
    /// same under either host (`Raster::new(..)?`).
    pub fn new(
        width_px: u32,
        height_px: u32,
        pixels_per_point: f32,
    ) -> Result<Self, HeadlessError> {
        tracing::info!(
            width_px,
            height_px,
            pixels_per_point,
            simd = cfg!(any(target_arch = "x86_64", target_arch = "aarch64")),
            "headless CPU rasterizer up (no wgpu, no Vulkan loader, no ICD)"
        );
        Ok(Self {
            render: EguiSoftwareRender::new(ColorFieldOrder::Bgra).with_caching(false),
            width_px,
            height_px,
            pixels_per_point,
            max_texture_side: MAX_TEXTURE_SIDE,
        })
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
