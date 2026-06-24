//! Stage 4 — render Golay codewords as a tiled luma grid over a base frame.
//!
//! Only each cell's **inner block** is touched: data cells get `±delta`, the
//! 4-px outer ring is left at the base image (it serves as the per-cell local-DC
//! reference and deblocking guard at decode time). Reference cells have their
//! inner block forced to a fixed level. The pattern is identical in every tile,
//! so the modulation is exactly periodic with `(tile_w, tile_h)`.

use crate::fec::{self, N_WORDS};
use crate::frame::LumaFrame;
use crate::layout::{CellKind, TileSpec};
use crate::Payload;

/// Apply the watermark for the 7 codewords across the whole frame, tiling from
/// the top-left. Partial tiles at the right/bottom edges are rendered too (so
/// any crop sees full tiles); off-frame pixels are simply skipped.
pub fn render_into(frame: &mut LumaFrame, words: &[u32; N_WORDS], spec: &TileSpec) {
    let mut ty = 0u32;
    while ty < frame.h {
        let mut tx = 0u32;
        while tx < frame.w {
            render_tile(frame, tx, ty, words, spec);
            tx += spec.tile_w;
        }
        ty += spec.tile_h;
    }
}

fn render_tile(frame: &mut LumaFrame, tx: u32, ty: u32, words: &[u32; N_WORDS], spec: &TileSpec) {
    for cell in spec.cells() {
        let (ix, iy, size) = spec.inner_rect(cell.col as u32, cell.row as u32);
        let (x0, y0) = (tx + ix, ty + iy);
        match cell.kind {
            CellKind::Data { word, bit } => {
                let one = (words[word as usize] >> (fec::BITS_PER_WORD as u8 - 1 - bit)) & 1 == 1;
                let d = if one { spec.delta } else { -spec.delta };
                paint(frame, x0, y0, size, |v| (v + d).clamp(0.0, 255.0));
            }
            CellKind::Ref { level } => {
                let lv = level.luma();
                paint(frame, x0, y0, size, |_| lv);
            }
        }
    }
}

#[inline]
fn paint(frame: &mut LumaFrame, x0: u32, y0: u32, size: u32, f: impl Fn(f32) -> f32) {
    let x1 = (x0 + size).min(frame.w);
    let y1 = (y0 + size).min(frame.h);
    for y in y0..y1 {
        for x in x0..x1 {
            let i = frame.idx(x, y);
            frame.y[i] = f(frame.y[i]);
        }
    }
}

/// Top-level encode: payload → CRC → Golay → tiled render over `base`.
pub fn encode_frame(base: &LumaFrame, payload: &Payload, spec: &TileSpec) -> LumaFrame {
    let words = fec::encode_info(&payload.to_info_bits());
    let mut f = base.clone();
    render_into(&mut f, &words, spec);
    f
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::layout::RefLevel;

    #[test]
    fn modulation_is_periodic_on_flat_base() {
        let spec = TileSpec::default();
        let base = LumaFrame::filled(2 * spec.tile_w, 2 * spec.tile_h, 128.0);
        let wm = encode_frame(&base, &Payload([1, 2, 3, 4, 5, 6, 7, 8]), &spec);
        for y in 0..wm.h - spec.tile_h {
            for x in 0..wm.w - spec.tile_w {
                assert_eq!(
                    wm.at(x, y),
                    wm.at(x + spec.tile_w, y + spec.tile_h),
                    "not periodic at ({x},{y})"
                );
            }
        }
    }

    #[test]
    fn reference_cells_read_back_nominal() {
        let spec = TileSpec::default();
        let base = LumaFrame::filled(spec.tile_w, spec.tile_h, 100.0);
        let wm = encode_frame(&base, &Payload([0xaa; 8]), &spec);
        for cell in spec.cells() {
            if let CellKind::Ref { level } = cell.kind {
                let (ix, iy, size) = spec.inner_rect(cell.col as u32, cell.row as u32);
                for y in iy..iy + size {
                    for x in ix..ix + size {
                        assert_eq!(wm.at(x, y), level.luma());
                    }
                }
                let _ = RefLevel::Black; // keep import meaningful
            }
        }
    }

    #[test]
    fn emit_sample_png() {
        // Visual artifact for eyeballing Δ subtlety; lands in target/.
        let spec = TileSpec::default();
        let base = LumaFrame::synthetic_natural(640, 480, 3);
        let wm = encode_frame(
            &base,
            &Payload([0xde, 0xad, 0xbe, 0xef, 0x12, 0x34, 0x56, 0x78]),
            &spec,
        );
        let out = format!("{}/target/sample_watermark.png", env!("CARGO_MANIFEST_DIR"));
        wm.save_png(&out).expect("save sample png");
        eprintln!("wrote {out}");
    }
}
