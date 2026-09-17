//! Remote logical points and local physical pixels (ADR-0243 §SD2).

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Viewport {
    pub x: f32,
    pub y: f32,
    pub width: f32,
    pub height: f32,
    pub remote_width: u32,
    pub remote_height: u32,
    pub pixels_per_point: f32,
}

impl Viewport {
    pub fn new(local: (u32, u32), remote: (u32, u32), ppp: f32, fit: bool) -> Option<Self> {
        if local.0 == 0
            || local.1 == 0
            || remote.0 == 0
            || remote.1 == 0
            || !ppp.is_finite()
            || ppp <= 0.0
        {
            return None;
        }
        let scale = if fit {
            (local.0 as f32 / remote.0 as f32).min(local.1 as f32 / remote.1 as f32)
        } else {
            1.0
        };
        let width = remote.0 as f32 * scale;
        let height = remote.1 as f32 * scale;
        Some(Self {
            x: (local.0 as f32 - width) * 0.5,
            y: (local.1 as f32 - height) * 0.5,
            width,
            height,
            remote_width: remote.0,
            remote_height: remote.1,
            pixels_per_point: ppp,
        })
    }

    pub fn logical(&self, x: f32, y: f32, captured: bool) -> Option<(f32, f32)> {
        if !x.is_finite() || !y.is_finite() {
            return None;
        }
        let u = (x - self.x) / self.width;
        let v = (y - self.y) / self.height;
        if !captured && (!(0.0..1.0).contains(&u) || !(0.0..1.0).contains(&v)) {
            return None;
        }
        Some((
            u * self.remote_width as f32 / self.pixels_per_point,
            v * self.remote_height as f32 / self.pixels_per_point,
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn letterbox_and_dpi() {
        let v = Viewport::new((1000, 1000), (1920, 1080), 2.0, true).unwrap();
        assert_eq!(v.logical(500.0, 500.0, false), Some((480.0, 270.0)));
        assert_eq!(v.logical(100.0, 10.0, false), None);
        assert!(v.logical(100.0, 10.0, true).is_some());
    }
    #[test]
    fn one_to_one_is_centered_and_cropped() {
        let v = Viewport::new((800, 600), (1600, 1200), 1.0, false).unwrap();
        assert_eq!(v.logical(0.0, 0.0, false), Some((400.0, 300.0)));
    }
    #[test]
    fn invalid_geometry_is_rejected() {
        for ppp in [0.0, -1.0, f32::NAN, f32::INFINITY] {
            assert!(Viewport::new((1, 1), (1, 1), ppp, true).is_none());
        }
        assert!(Viewport::new((0, 1), (1, 1), 1.0, true).is_none());
    }
}
