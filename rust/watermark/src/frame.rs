//! Gamma-encoded luminance in 0..255, PNG I/O and deterministic test content.

use std::path::Path;

use crate::Error;

/// A row-major luminance plane with checked dimensions and fixed-length storage.
#[derive(Clone, Debug)]
pub struct LumaFrame {
    w: u32,
    h: u32,
    y: Vec<f32>,
}

impl LumaFrame {
    pub fn from_luma(w: u32, h: u32, y: Vec<f32>) -> Result<Self, Error> {
        if y.len() != pixel_count(w, h)? {
            return Err(Error::BadDimensions(
                "pixel buffer length does not match dimensions".into(),
            ));
        }
        let frame = Self { w, h, y };
        frame.validate()?;
        Ok(frame)
    }

    pub fn filled(w: u32, h: u32, level: f32) -> Result<Self, Error> {
        if !level.is_finite() || !(0.0..=255.0).contains(&level) {
            return Err(Error::InvalidLuma);
        }
        let n = pixel_count(w, h)?;
        let mut y = Vec::new();
        y.try_reserve_exact(n)
            .map_err(|_| Error::BadDimensions("frame allocation failed".into()))?;
        y.resize(n, level);
        Ok(Self { w, h, y })
    }

    pub fn w(&self) -> u32 {
        self.w
    }
    pub fn h(&self) -> u32 {
        self.h
    }
    pub fn pixels(&self) -> &[f32] {
        &self.y
    }
    /// Mutation cannot change dimensions/storage length. Encode/decode recheck values.
    pub fn pixels_mut(&mut self) -> &mut [f32] {
        &mut self.y
    }

    pub fn validate(&self) -> Result<(), Error> {
        if self
            .y
            .iter()
            .any(|v| !v.is_finite() || !(0.0..=255.0).contains(v))
        {
            return Err(Error::InvalidLuma);
        }
        Ok(())
    }

    /// Indexing requires coordinates inside the frame.
    #[inline]
    pub fn idx(&self, x: u32, y: u32) -> usize {
        assert!(x < self.w && y < self.h, "pixel outside frame");
        y as usize * self.w as usize + x as usize
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

    pub fn crop(&self, x0: u32, y0: u32, cw: u32, ch: u32) -> Result<Self, Error> {
        if x0 as u64 + cw as u64 > self.w as u64 || y0 as u64 + ch as u64 > self.h as u64 {
            return Err(Error::BadDimensions("crop outside frame".into()));
        }
        let mut out = Self::filled(cw, ch, 0.0)?;
        for row in 0..ch {
            let base = (y0 + row) as usize * self.w as usize + x0 as usize;
            let dest = row as usize * cw as usize;
            out.y[dest..dest + cw as usize].copy_from_slice(&self.y[base..base + cw as usize]);
        }
        out.validate()?;
        Ok(out)
    }

    /// Load an opaque PNG using the same luminance convention as color encoding.
    pub fn load_png<P: AsRef<Path>>(path: P) -> Result<Self, Error> {
        crate::image_io::ImageFrame::load_png(path)?.luma()
    }

    pub fn save_png<P: AsRef<Path>>(&self, path: P) -> Result<(), Error> {
        self.validate()?;
        let bytes = self.y.iter().map(|v| v.round() as u8).collect();
        let img = image::GrayImage::from_raw(self.w, self.h, bytes)
            .ok_or_else(|| Error::BadDimensions("pixel buffer length mismatch".into()))?;
        img.save(path)?;
        Ok(())
    }

    /// Smooth gradients and sinusoids, not representative natural-image detail.
    pub fn synthetic_natural(w: u32, h: u32, seed: u64) -> Result<Self, Error> {
        let mut frame = Self::filled(w, h, 0.0)?;
        let s = (seed % 17) as f32;
        for py in 0..h {
            for px in 0..w {
                let (u, v) = (px as f32 / w as f32, py as f32 / h as f32);
                let g = 60.0 + 60.0 * u + 40.0 * v;
                let ripple = 18.0 * ((6.0 + s) * u + 2.0 * v + s).sin()
                    + 14.0 * ((3.0 + s) * v - u).cos()
                    + 8.0 * (11.0 * u + 7.0 * v).sin();
                frame.set(px, py, (g + ripple).clamp(30.0, 225.0));
            }
        }
        Ok(frame)
    }
}

fn pixel_count(w: u32, h: u32) -> Result<usize, Error> {
    let count = (w as usize)
        .checked_mul(h as usize)
        .filter(|&n| w > 0 && h > 0 && n <= isize::MAX as usize / std::mem::size_of::<f32>());
    count.ok_or_else(|| {
        Error::BadDimensions("nonzero, representable frame dimensions required".into())
    })
}
