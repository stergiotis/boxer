//! Stage 6 — per-tile brightness/gamma self-calibration from reference cells.
//!
//! Each tile carries reference cells at known levels (black/mid/white). Their
//! *measured* luma reveals whatever brightness offset and gamma the channel
//! applied. We build a monotonic **measured→nominal** transfer function by
//! piecewise-linear interpolation through those anchors and apply it to both the
//! inner-block and ring means before differencing.
//!
//! The bit decision (`sign(inner − ring)`) is already invariant to any monotonic
//! transform, so calibration is not what makes brightness/gamma decode *work* —
//! it linearizes the delta so its magnitude is uniform across brightness, which
//! improves the worst-case noise margin and lets multi-tile soft-combine weight
//! tiles fairly. A failed fit (reference cells don't span ≥2 distinct levels)
//! also flags a tile that is not a trustworthy watermark tile.

use crate::layout::RefLevel;

/// A measured→nominal luma transfer function fitted from reference cells.
#[derive(Clone, Debug)]
pub struct Calibration {
    /// `(measured, nominal)` anchors, strictly increasing in `measured`.
    anchors: Vec<(f32, f32)>,
}

impl Calibration {
    /// Fit from `(measured_inner_mean, level)` samples. Averages the measured
    /// value per level, then orders the level anchors. Returns `None` if fewer
    /// than 2 distinct, monotonically-ordered levels are available.
    pub fn fit(samples: &[(f32, RefLevel)]) -> Option<Calibration> {
        let mut sum = [0f32; 3];
        let mut cnt = [0u32; 3];
        for &(m, lv) in samples {
            // Reject non-finite measurements: a NaN/Inf reaching the sort below
            // would panic, and decode given garbage must return an error, never
            // panic (LumaFrame.y is a public `Vec<f32>`).
            if m.is_finite() {
                let i = lv as usize;
                sum[i] += m;
                cnt[i] += 1;
            }
        }
        let nominals = [
            RefLevel::Black.luma(),
            RefLevel::Mid.luma(),
            RefLevel::White.luma(),
        ];
        let mut anchors: Vec<(f32, f32)> = (0..3)
            .filter(|&i| cnt[i] > 0)
            .map(|i| (sum[i] / cnt[i] as f32, nominals[i]))
            .collect();
        if anchors.len() < 2 {
            return None;
        }
        anchors.sort_by(|a, b| a.0.partial_cmp(&b.0).unwrap_or(std::cmp::Ordering::Equal));
        // Measured order must agree with nominal order (monotonic, no ties), and
        // the gain must be sane. A near-flat reference set (tiny measured gaps →
        // huge interpolation slope) means a destroyed or false-located tile; we
        // reject it so its calibration can't explode and, in a multi-tile crop,
        // dominate the (unweighted) soft-combine. Real channels — including the
        // gamma sweep — keep slopes well under this bound (~1–2).
        const MAX_SLOPE: f32 = 8.0;
        for w in anchors.windows(2) {
            if w[1].0 <= w[0].0 || w[1].1 <= w[0].1 {
                return None;
            }
            if (w[1].1 - w[0].1) / (w[1].0 - w[0].0) > MAX_SLOPE {
                return None;
            }
        }
        Some(Calibration { anchors })
    }

    /// Map a measured luma into nominal space. Inside the anchor range this is a
    /// piecewise-linear interpolation; outside it, a linear extrapolation off the
    /// nearest segment.
    pub fn to_nominal(&self, m: f32) -> f32 {
        let a = &self.anchors;
        if m <= a[0].0 {
            return interp(a[0], a[1], m);
        }
        for w in a.windows(2) {
            if m <= w[1].0 {
                return interp(w[0], w[1], m);
            }
        }
        let n = a.len();
        interp(a[n - 2], a[n - 1], m)
    }

    /// Calibrated soft value for a data cell given its inner and ring means.
    pub fn soft(&self, inner: f32, ring: f32) -> f32 {
        self.to_nominal(inner) - self.to_nominal(ring)
    }
}

#[inline]
fn interp(p0: (f32, f32), p1: (f32, f32), m: f32) -> f32 {
    let t = (m - p0.0) / (p1.0 - p0.0);
    p0.1 + t * (p1.1 - p0.1)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn identity_when_measured_equals_nominal() {
        let s = [
            (16.0, RefLevel::Black),
            (128.0, RefLevel::Mid),
            (240.0, RefLevel::White),
        ];
        let c = Calibration::fit(&s).unwrap();
        for m in [0.0, 16.0, 80.0, 128.0, 200.0, 255.0] {
            assert!((c.to_nominal(m) - m).abs() < 1e-3, "m={m}");
        }
    }

    #[test]
    fn inverts_brightness_and_gamma() {
        // Forward channel transform: gamma then a brightness offset.
        let gamma = 0.6f32;
        let offset = 25.0f32;
        let fwd = |n: f32| 255.0 * (n / 255.0).powf(gamma) + offset;
        let s = [
            (fwd(16.0), RefLevel::Black),
            (fwd(128.0), RefLevel::Mid),
            (fwd(240.0), RefLevel::White),
        ];
        let c = Calibration::fit(&s).unwrap();
        // Recovers the anchor nominals exactly, and is close in between.
        assert!((c.to_nominal(fwd(16.0)) - 16.0).abs() < 1e-2);
        assert!((c.to_nominal(fwd(128.0)) - 128.0).abs() < 1e-2);
        assert!((c.to_nominal(fwd(240.0)) - 240.0).abs() < 1e-2);
    }

    #[test]
    fn fit_rejects_single_level() {
        let s = [(50.0, RefLevel::Mid), (51.0, RefLevel::Mid)];
        assert!(Calibration::fit(&s).is_none());
    }

    #[test]
    fn fit_rejects_degenerate_gain() {
        // Near-flat reference set (huge implied slope) → reject, so a bad tile's
        // calibration cannot explode and hijack a multi-tile combine.
        let s = [
            (100.0, RefLevel::Black),
            (100.5, RefLevel::Mid),
            (101.0, RefLevel::White),
        ];
        assert!(Calibration::fit(&s).is_none());
    }

    #[test]
    fn fit_ignores_non_finite_without_panic() {
        // A NaN measurement must not panic the sort; here it drops Mid, leaving
        // two valid levels that still fit.
        let s = [
            (16.0, RefLevel::Black),
            (f32::NAN, RefLevel::Mid),
            (240.0, RefLevel::White),
        ];
        let c = Calibration::fit(&s).expect("two finite levels still fit");
        assert!((c.to_nominal(16.0) - 16.0).abs() < 1e-2);
        // All-NaN input simply yields no calibration (no panic).
        let bad = [(f32::NAN, RefLevel::Black), (f32::NAN, RefLevel::White)];
        assert!(Calibration::fit(&bad).is_none());
    }
}
