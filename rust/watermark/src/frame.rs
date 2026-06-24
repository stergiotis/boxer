//! The luma plane we operate on, plus PNG I/O and synthetic test bases.
//!
//! Everything is done in **sRGB-gamma luma, 0..255** — the same domain end to
//! end, so deltas and reference levels stay consistent (`EXPLANATION.md`
//! §Guardrails 2). PNG load goes through Rec.601 luma; we treat that as our
//! working space.

use std::path::Path;

use crate::Error;

/// A single-channel luma frame, row-major, values in `0.0..=255.0`.
#[derive(Clone, Debug)]
pub struct LumaFrame {
    pub w: u32,
    pub h: u32,
    pub y: Vec<f32>,
}

impl LumaFrame {
    /// A `w×h` frame filled with a constant level.
    pub fn filled(w: u32, h: u32, level: f32) -> Self {
        LumaFrame {
            w,
            h,
            y: vec![level; (w * h) as usize],
        }
    }

    #[inline]
    pub fn idx(&self, x: u32, y: u32) -> usize {
        (y * self.w + x) as usize
    }

    #[inline]
    pub fn at(&self, x: u32, y: u32) -> f32 {
        self.y[self.idx(x, y)]
    }

    #[inline]
    pub fn set(&mut self, x: u32, y: u32, v: f32) {
        let i = self.idx(x, y);
        self.y[i] = v;
    }

    /// A `cw×ch` crop with top-left at `(x0,y0)`. Panics if out of bounds.
    pub fn crop(&self, x0: u32, y0: u32, cw: u32, ch: u32) -> LumaFrame {
        assert!(x0 + cw <= self.w && y0 + ch <= self.h, "crop out of bounds");
        let mut y = Vec::with_capacity((cw * ch) as usize);
        for row in y0..y0 + ch {
            let base = (row * self.w + x0) as usize;
            y.extend_from_slice(&self.y[base..base + cw as usize]);
        }
        LumaFrame { w: cw, h: ch, y }
    }

    /// Load a PNG (any color type) as luma.
    pub fn load_png<P: AsRef<Path>>(path: P) -> Result<LumaFrame, Error> {
        let img = image::open(path).map_err(|e| Error::Image(e.to_string()))?;
        let gray = img.to_luma8();
        let (w, h) = (gray.width(), gray.height());
        let y = gray.as_raw().iter().map(|&b| b as f32).collect();
        Ok(LumaFrame { w, h, y })
    }

    /// Save as an 8-bit grayscale PNG (values clamped to 0..255 and rounded).
    pub fn save_png<P: AsRef<Path>>(&self, path: P) -> Result<(), Error> {
        let bytes: Vec<u8> = self
            .y
            .iter()
            .map(|&v| v.round().clamp(0.0, 255.0) as u8)
            .collect();
        let img = image::GrayImage::from_raw(self.w, self.h, bytes)
            .ok_or_else(|| Error::Image("buffer size mismatch".into()))?;
        img.save(path).map_err(|e| Error::Image(e.to_string()))
    }

    /// A smooth, band-limited synthetic "natural" base: low-frequency gradients
    /// plus a few sinusoids, in roughly 40..220. Deterministic in `seed`. Used to
    /// prove decode works over non-flat content (where per-cell local-DC removal
    /// matters), without pulling in image fixtures.
    pub fn synthetic_natural(w: u32, h: u32, seed: u64) -> LumaFrame {
        let s = (seed % 17) as f32;
        let mut y = Vec::with_capacity((w * h) as usize);
        let (fw, fh) = (w as f32, h as f32);
        for py in 0..h {
            for px in 0..w {
                let u = px as f32 / fw;
                let v = py as f32 / fh;
                let g = 60.0 + 120.0 * u * 0.5 + 80.0 * v * 0.5; // broad gradient
                let ripple = 18.0 * ((6.0 + s) * u + 2.0 * v + s).sin()
                    + 14.0 * ((3.0 + s) * v - u).cos()
                    + 8.0 * ((11.0) * u + (7.0) * v).sin();
                y.push((g + ripple).clamp(30.0, 225.0));
            }
        }
        LumaFrame { w, h, y }
    }
}
