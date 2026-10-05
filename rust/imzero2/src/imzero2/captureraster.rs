//! The capture rasterizer (ADR-0281 §SD5): the vendored software backend,
//! drawing a capture's tessellated shapes into host memory on every native
//! host — the desktop host, the mesh-only headless host and both headless
//! pixel hosts — apart from whatever paints the live frame.
//!
//! It takes the uncached, single-threaded path: a capture is one frame now
//! and then, and the cached path's parallel pool belongs to the live frame
//! of the `headless_soft` host (softraster.rs).

use egui_software_backend::{BufferMutRef, ColorFieldOrder, EguiSoftwareRender};

use crate::imzero2::interpreter::capture_replay::CaptureRasterI;

/// The background a capture is drawn on, the live frame's.
const CLEAR: [u8; 4] = [0, 0, 0, 255];

/// Made on the first capture. Textures a capture does not set again are
/// freed before the next.
#[derive(Default)]
pub struct SoftCaptureRaster {
    render: Option<EguiSoftwareRender>,
    held: std::collections::HashSet<egui::TextureId>,
}

impl CaptureRasterI for SoftCaptureRaster {
    fn rasterize(
        &mut self,
        clipped: &[egui::ClippedPrimitive],
        textures: &egui::TexturesDelta,
        width_px: u32,
        height_px: u32,
        pixels_per_point: f32,
    ) -> Result<Vec<u8>, String> {
        let render = self.render.get_or_insert_with(|| {
            EguiSoftwareRender::new(ColorFieldOrder::Rgba).with_caching(false)
        });
        let now: std::collections::HashSet<egui::TextureId> =
            textures.set.iter().map(|(id, _)| *id).collect();
        let stale: Vec<egui::TextureId> = self.held.difference(&now).copied().collect();
        if !stale.is_empty() {
            render.free_textures(&egui::TexturesDelta {
                set: Vec::new(),
                free: stale,
            });
        }
        self.held = now;
        let (w, h) = (width_px as usize, height_px as usize);
        let mut frame = vec![0u8; w * h * 4];
        let pixels: &mut [[u8; 4]] = bytemuck::cast_slice_mut(frame.as_mut_slice());
        pixels.fill(CLEAR);
        let mut buffer = BufferMutRef::new(pixels, w, h);
        render.render(&mut buffer, clipped, textures, pixels_per_point);
        Ok(frame)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A red square on the capture background lands where egui put it, in
    /// RGBA order.
    #[test]
    fn a_shape_lands_in_rgba() {
        let ctx = egui::Context::default();
        let raw = egui::RawInput {
            screen_rect: Some(egui::Rect::from_min_size(
                egui::Pos2::ZERO,
                egui::vec2(32.0, 16.0),
            )),
            ..Default::default()
        };
        let out = ctx.run_ui(raw, |ui| {
            ui.painter().rect_filled(
                egui::Rect::from_min_size(egui::pos2(0.0, 0.0), egui::vec2(8.0, 8.0)),
                0.0,
                egui::Color32::from_rgb(255, 0, 0),
            );
        });
        let clipped = ctx.tessellate(out.shapes, out.pixels_per_point);
        let mut r = SoftCaptureRaster::default();
        let rgba = r.rasterize(&clipped, &out.textures_delta, 32, 16, 1.0).expect("raster");
        assert_eq!(rgba.len(), 32 * 16 * 4);
        assert_eq!(&rgba[0..4], &[255, 0, 0, 255]);
        let far = (15 * 32 + 31) * 4;
        assert_eq!(&rgba[far..far + 4], &CLEAR);
    }
}
