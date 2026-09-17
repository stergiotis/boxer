//! Opaque PNG input and color-preserving reconstruction of modified luminance.
//! Partial alpha requires compositing before watermarking or decoding.

use crate::{Error, LumaFrame};
use image::{DynamicImage, RgbaImage};
use std::path::Path;

pub struct ImageFrame {
    rgba: RgbaImage,
    gray: bool,
}

/// Rec.709 weights on gamma-encoded channels, matching image's luma8 convention.
fn luma(p: &[u8; 4]) -> f32 {
    (2126.0 * p[0] as f32 + 7152.0 * p[1] as f32 + 722.0 * p[2] as f32) / 10000.0
}

impl ImageFrame {
    pub fn load_png<P: AsRef<Path>>(path: P) -> Result<Self, Error> {
        let image = image::open(path)?;
        let gray = matches!(
            image,
            DynamicImage::ImageLuma8(_)
                | DynamicImage::ImageLumaA8(_)
                | DynamicImage::ImageLuma16(_)
                | DynamicImage::ImageLumaA16(_)
        );
        // Check before reducing bit depth so an almost-opaque 16-bit pixel is not
        // silently rounded up to opaque.
        if image.to_rgba16().pixels().any(|p| p[3] != u16::MAX) {
            return Err(Error::NonOpaque);
        }
        Ok(Self {
            rgba: image.into_rgba8(),
            gray,
        })
    }

    pub fn luma(&self) -> Result<LumaFrame, Error> {
        LumaFrame::from_luma(
            self.rgba.width(),
            self.rgba.height(),
            self.rgba.pixels().map(|p| luma(&p.0)).collect(),
        )
    }

    pub fn save_with_luma<P: AsRef<Path>>(&self, target: &LumaFrame, path: P) -> Result<(), Error> {
        target.validate()?;
        if (target.w(), target.h()) != self.rgba.dimensions() {
            return Err(Error::BadDimensions(
                "color and luminance dimensions differ".into(),
            ));
        }
        if self.gray {
            return target.save_png(path);
        }
        let mut rgba = self.rgba.clone();
        for (pixel, &new_y) in rgba.pixels_mut().zip(target.pixels()) {
            let old_y = luma(&pixel.0);
            if new_y == old_y {
                continue;
            }
            for channel in &mut pixel.0[..3] {
                *channel = crate::render::move_mean(*channel as f32, old_y, new_y).round() as u8;
            }
        }
        rgba.save(path)?;
        Ok(())
    }
}
