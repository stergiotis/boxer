//! OKLab / OKLCh ↔ sRGB conversions per Björn Ottosson 2020.
//!
//! Mirrors `src/go/public/keelson/designsystem/colors/oklab/oklab.go`. Both ports follow
//! the same Ottosson reference; a CI drift check (TODO when designlint M0
//! lands) parses both files and asserts matrix equality.
//!
//! Used at design-time (the color generator emits sRGB hex constants from
//! OKLCh source coordinates). Not on the render path — IDS performance
//! invariant per ADR-0029 §SD13.

#![allow(dead_code)] // Rust-side mirror; the generator runs on the Go side.

use std::f32::consts::PI;

#[inline]
pub fn srgb_to_linear(c: f32) -> f32 {
    if c <= 0.040_45 {
        c / 12.92
    } else {
        ((c + 0.055) / 1.055).powf(2.4)
    }
}

#[inline]
pub fn linear_to_srgb(l: f32) -> f32 {
    if l <= 0.003_130_8 {
        12.92 * l
    } else {
        1.055 * l.powf(1.0 / 2.4) - 0.055
    }
}

/// Forward Ottosson 2020 transform: linear-sRGB → OKLab.
pub fn linear_srgb_to_oklab(r: f32, g: f32, b: f32) -> (f32, f32, f32) {
    let l = 0.412_221_47 * r + 0.536_332_5 * g + 0.051_445_995 * b;
    let m = 0.211_903_5 * r + 0.680_699_5 * g + 0.107_396_96 * b;
    let s = 0.088_302_46 * r + 0.281_718_85 * g + 0.629_978_7 * b;

    let l_ = l.cbrt();
    let m_ = m.cbrt();
    let s_ = s.cbrt();

    let l_out = 0.210_454_26 * l_ + 0.793_617_8 * m_ - 0.004_072_047 * s_;
    let a_out = 1.977_998_5 * l_ - 2.428_592_2 * m_ + 0.450_593_71 * s_;
    let b_out = 0.025_904_037 * l_ + 0.782_771_77 * m_ - 0.808_675_77 * s_;
    (l_out, a_out, b_out)
}

/// Inverse Ottosson 2020 transform: OKLab → linear-sRGB.
pub fn oklab_to_linear_srgb(l: f32, a: f32, b: f32) -> (f32, f32, f32) {
    let l_ = l + 0.396_337_78 * a + 0.215_803_76 * b;
    let m_ = l - 0.105_561_346 * a - 0.063_854_17 * b;
    let s_ = l - 0.089_484_18 * a - 1.291_485_5 * b;

    let l3 = l_ * l_ * l_;
    let m3 = m_ * m_ * m_;
    let s3 = s_ * s_ * s_;

    let r = 4.076_741_7 * l3 - 3.307_711_6 * m3 + 0.230_969_94 * s3;
    let g = -1.268_438 * l3 + 2.609_757_4 * m3 - 0.341_319_4 * s3;
    let b = -0.004_196_086_3 * l3 - 0.703_418_6 * m3 + 1.707_614_7 * s3;
    (r, g, b)
}

/// (L, a, b) → (L, C, h°). h ∈ [0, 360).
pub fn oklab_to_oklch(l: f32, a: f32, b: f32) -> (f32, f32, f32) {
    let c = (a * a + b * b).sqrt();
    let mut h = b.atan2(a) * 180.0 / PI;
    if h < 0.0 {
        h += 360.0;
    }
    (l, c, h)
}

/// (L, C, h°) → (L, a, b). Inverse of `oklab_to_oklch`.
pub fn oklch_to_oklab(l: f32, c: f32, h_deg: f32) -> (f32, f32, f32) {
    let h_rad = h_deg * PI / 180.0;
    (l, c * h_rad.cos(), c * h_rad.sin())
}
