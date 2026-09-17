mod common;
use watermark::{decode::decode_at_origins, layout::CellKind, sample::cell_soft};
use watermark::{decode_frame, encode_frame, Payload, TileSpec};

#[test]
fn pathological_cells_have_contrast_and_single_tile_recovery() {
    for delta in [2.0, 8.0, 127.0] {
        let s = TileSpec::new(delta).unwrap();
        for name in common::CASES {
            let base = common::fixture(name, s.tile_w(), s.tile_h());
            for p in [
                Payload([0; 8]),
                Payload([255; 8]),
                Payload::from_hex("deadbeef12345678").unwrap(),
            ] {
                let wm = encode_frame(&base, &p, &s).unwrap();
                let words = watermark::fec::encode_info(&p.to_info_bits());
                for c in s.cells() {
                    if let CellKind::Data { word, bit } = c.kind {
                        let one = (words[word as usize] >> (23 - bit)) & 1 == 1;
                        let soft = cell_soft(&wm, 0, 0, c.col as u32, c.row as u32, &s).unwrap();
                        assert!(
                            (soft.abs() - delta).abs() < 0.003,
                            "{name} contrast {soft} delta {delta}"
                        );
                        assert_eq!(soft > 0.0, one, "{name}");
                    }
                }
                assert!(wm
                    .pixels()
                    .iter()
                    .all(|v| v.is_finite() && (0.0..=255.0).contains(v)));
                assert_eq!(
                    decode_at_origins(&wm, &[(0, 0)], &s).unwrap(),
                    p,
                    "{name} delta {delta}"
                );
                // Exercise byte quantization at both accepted contrast endpoints,
                // independently of the file-I/O test at the default contrast.
                let mut quantized = wm.clone();
                for value in quantized.pixels_mut() {
                    *value = value.round();
                }
                assert_eq!(
                    decode_at_origins(&quantized, &[(0, 0)], &s).unwrap(),
                    p,
                    "quantized {name} delta {delta}"
                );
            }
        }
    }
}

#[test]
fn pathological_png_crops_locate_and_recover() {
    let s = TileSpec::default();
    let p = Payload::from_hex("deadbeef12345678").unwrap();
    let dir = tempfile::tempdir().unwrap();
    for name in common::CASES {
        let base = common::fixture(name, 696, 648);
        let wm = encode_frame(&base, &p, &s).unwrap();
        let path = dir.path().join(format!("{name}.png"));
        wm.save_png(&path).unwrap();
        let wm = watermark::LumaFrame::load_png(&path).unwrap();
        for (x, y) in [(0, 0), (1, 1), (17, 29), (100, 100), (231, 215), (232, 216)] {
            let crop = wm.crop(x, y, 464, 432).unwrap();
            let expected = ((232 - x % 232) % 232, (216 - y % 216) % 216);
            assert_eq!(
                decode_at_origins(&crop, &[expected], &s).unwrap(),
                p,
                "{name} {x},{y} known"
            );
            assert_eq!(
                decode_frame(&crop, &s).unwrap(),
                p,
                "{name} {x},{y} located"
            );
        }
    }
}

#[test]
fn unwatermarked_corpus_is_rejected() {
    let s = TileSpec::default();
    for name in common::CASES {
        let base = common::fixture(name, 464, 432);
        assert!(decode_frame(&base, &s).is_err(), "unmarked {name}");
    }
}

#[test]
fn textured_gamma_brightness_and_noise_are_tested_together() {
    let s = TileSpec::new(16.0).unwrap();
    let p = Payload([53; 8]);
    for name in ["text", "checker8", "noise"] {
        // Mild transforms are acceptance cases; stronger transforms remain
        // characterization cases because texture changes the order of means.
        for gamma in [0.8f32, 0.95, 1.05, 1.2] {
            let mut wm = encode_frame(&common::fixture(name, 232, 216), &p, &s).unwrap();
            for (i, v) in wm.pixels_mut().iter_mut().enumerate() {
                let noise = ((i * 17 % 13) as f32 - 6.0) * 0.25;
                *v = (255.0 * (*v / 255.0).powf(gamma) + 3.0 + noise).clamp(0.0, 255.0);
            }
            let result = decode_at_origins(&wm, &[(0, 0)], &s);
            if (0.95..=1.05).contains(&gamma) {
                assert_eq!(
                    result.unwrap_or_else(|e| panic!("{name} gamma{gamma}: {e}")),
                    p
                );
            } else {
                match result {
                    Ok(got) => assert_eq!(got, p),
                    Err(
                        watermark::Error::Uncorrectable
                        | watermark::Error::InvalidPadding
                        | watermark::Error::CrcMismatch,
                    ) => {}
                    Err(e) => panic!("unexpected input/evidence failure: {e}"),
                }
            }
        }
    }
}

#[test]
fn failed_calibration_does_not_vote_against_a_good_tile() {
    let s = TileSpec::default();
    let p = Payload([7; 8]);
    let base = common::fixture("text", 464, 216);
    let mut wm = encode_frame(&base, &p, &s).unwrap();
    for cell in s.cells() {
        if matches!(cell.kind, CellKind::Ref { .. }) {
            let (x, y, n) = s.inner_rect(cell.col as u32, cell.row as u32);
            for dy in 0..n {
                for dx in 0..n {
                    wm.set(x + dx, y + dy, 128.0);
                }
            }
        }
    }
    let report = watermark::decode::decode_report_at_origins(&wm, &[(0, 0), (232, 0)], &s).unwrap();
    assert_eq!(report.payload.unwrap(), p);
    assert_eq!(report.recovered.complete_tiles, 2);
    assert_eq!(report.recovered.tiles, 1);
    assert!(matches!(
        decode_at_origins(&wm, &[(0, 0)], &s),
        Err(watermark::Error::NoUsableTile)
    ));
}
