//! Invalid input is rejected, including mutation after checked construction.
use watermark::{decode_frame, encode_frame, Error, LumaFrame, Payload, TileSpec};

#[test]
fn nonfinite_mutation_is_rejected_by_encode_and_decode() {
    let spec = TileSpec::default();
    for value in [f32::NAN, f32::INFINITY, f32::NEG_INFINITY, -1.0, 256.0] {
        let mut frame = LumaFrame::filled(464, 432, 128.0).unwrap();
        frame.pixels_mut()[0] = value;
        assert!(matches!(
            decode_frame(&frame, &spec),
            Err(Error::InvalidLuma)
        ));
        assert!(matches!(
            encode_frame(&frame, &Payload([0; 8]), &spec),
            Err(Error::InvalidLuma)
        ));
    }
}

#[test]
fn constructors_reject_invalid_dimensions_storage_and_contrast() {
    for (w, h) in [(0, 1), (1, 0), (u32::MAX, u32::MAX)] {
        assert!(LumaFrame::filled(w, h, 128.0).is_err());
    }
    for n in [0, 3, 5] {
        assert!(LumaFrame::from_luma(2, 2, vec![128.0; n]).is_err());
    }
    assert!(LumaFrame::from_luma(2, 2, vec![f32::NAN; 4]).is_err());
    for delta in [f32::NAN, f32::INFINITY, -8.0, 0.0, 1.99, 127.01] {
        assert!(matches!(TileSpec::new(delta), Err(Error::InvalidDelta)));
    }
    for delta in [2.0, 8.0, 127.0] {
        assert!(TileSpec::new(delta).is_ok());
    }
}

#[test]
fn one_aligned_tile_is_valid_below_guaranteed_window() {
    let s = TileSpec::default();
    let p = Payload([42; 8]);
    let base = LumaFrame::filled(s.tile_w(), s.tile_h(), 255.0).unwrap();
    let wm = encode_frame(&base, &p, &s).unwrap();
    assert_eq!(decode_frame(&wm, &s).unwrap(), p);
    for (w, h) in [(1, 1), (231, 216), (232, 215)] {
        let small = LumaFrame::filled(w, h, 128.0).unwrap();
        assert!(matches!(
            decode_frame(&small, &s),
            Err(Error::NoCompleteTile)
        ));
        assert!(matches!(
            encode_frame(&small, &p, &s),
            Err(Error::NoCompleteTile)
        ));
    }
}

#[test]
fn out_of_bounds_crop_and_origins_are_errors() {
    let frame = LumaFrame::filled(464, 432, 128.0).unwrap();
    let s = TileSpec::default();
    assert!(frame.crop(u32::MAX, 0, 1, 1).is_err());
    assert!(frame.crop(0, 0, 0, 1).is_err());
    assert!(watermark::decode::recover_words(&frame, &[(u32::MAX, 0)], &s).is_err());
    assert!(watermark::sample::inner_mean(&frame, u32::MAX, 0, 0, 0, &s).is_none());
    assert!(watermark::sample::inner_and_ring(&frame, 0, 0, u32::MAX, 0, &s).is_none());
}
