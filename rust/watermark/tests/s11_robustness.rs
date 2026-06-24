//! Hardening — adversarial inputs to the public decode API must return an error,
//! never panic, and never a wrong payload.

use watermark::decode::decode_frame;
use watermark::render::encode_frame;
use watermark::{LumaFrame, Payload, TileSpec};

#[test]
fn all_nan_frame_errors_without_panic() {
    let spec = TileSpec::default();
    let f = LumaFrame {
        w: spec.window_w(),
        h: spec.window_h(),
        y: vec![f32::NAN; (spec.window_w() * spec.window_h()) as usize],
    };
    // Must not panic; a garbage frame cannot yield a valid payload.
    assert!(decode_frame(&f, &spec).is_err());
}

#[test]
fn nan_poked_into_watermark_does_not_panic() {
    let spec = TileSpec::default();
    let base = LumaFrame::filled(spec.window_w(), spec.window_h(), 128.0);
    let mut wm = encode_frame(&base, &Payload([1, 2, 3, 4, 5, 6, 7, 8]), &spec);
    // Corrupt a swathe of pixels with non-finite values.
    for v in wm.y.iter_mut().step_by(7) {
        *v = f32::NAN;
    }
    let _ = decode_frame(&wm, &spec); // only requirement: no panic
}

#[test]
fn sub_window_frames_report_no_complete_tile() {
    let spec = TileSpec::default();
    for &(w, h) in &[(1u32, 1u32), (100, 100), (spec.tile_w, spec.tile_h)] {
        let f = LumaFrame::filled(w, h, 128.0);
        // Smaller than the 2×tile window → no fully contained tile, no panic.
        assert!(decode_frame(&f, &spec).is_err(), "{w}x{h} should error");
    }
}
