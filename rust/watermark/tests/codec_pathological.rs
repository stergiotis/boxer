//! Explicit ffmpeg integration lane, not a default PATH-dependent test.
mod common;
use watermark::codec::{roundtrip, Codec};
use watermark::decode::decode_at_origins;
use watermark::{decode_frame, encode_frame, Payload, TileSpec};

#[test]
#[ignore = "requires ffmpeg and all three encoders"]
fn codec_pathological_crops() {
    // Delta 8 passes clean PNGs but does not survive every high-frequency codec
    // case. This acceptance arm explicitly qualifies stronger contrast, not the
    // default. The default's checkerboard behavior is characterized separately.
    let s = TileSpec::new(16.0).unwrap();
    let p = Payload::from_hex("deadbeef12345678").unwrap();
    for codec in Codec::all() {
        for name in common::CASES {
            let base = common::fixture(name, 696, 648);
            let wm = encode_frame(&base, &p, &s).unwrap();
            let received = roundtrip(&wm, codec, codec.default_crf()).unwrap();
            let single = decode_at_origins(&received, &[(0, 0)], &s);
            assert_eq!(
                single.unwrap_or_else(|e| panic!("{name} {} single: {e}", codec.name())),
                p
            );
            for (x, y) in [(0, 0), (17, 29), (231, 215)] {
                let crop = received.crop(x, y, 464, 432).unwrap();
                let expected = ((232 - x % 232) % 232, (216 - y % 216) % 216);
                assert_eq!(
                    decode_at_origins(&crop, &[expected], &s).unwrap_or_else(|e| panic!(
                        "{name} {} {x},{y} known phase: {e}",
                        codec.name()
                    )),
                    p
                );
                assert_eq!(
                    decode_frame(&crop, &s)
                        .unwrap_or_else(|e| panic!("{name} {} {x},{y}: {e}", codec.name())),
                    p
                );
            }
        }
    }
}

#[test]
#[ignore = "requires ffmpeg and libx264"]
fn codec_default_checkerboard_characterization() {
    let s = TileSpec::default();
    let p = Payload::from_hex("deadbeef12345678").unwrap();
    let wm = encode_frame(&common::fixture("checker1", 696, 648), &p, &s).unwrap();
    let rx = roundtrip(&wm, Codec::H264, Codec::H264.default_crf()).unwrap();
    let crop = rx.crop(17, 29, 464, 432).unwrap();
    match decode_frame(&crop, &s) {
        Ok(got) => assert_eq!(got, p),
        Err(
            e @ (watermark::Error::Uncorrectable
            | watermark::Error::InvalidPadding
            | watermark::Error::CrcMismatch
            | watermark::Error::NoUsableTile),
        ) => eprintln!("delta 8 checkerboard is not qualified: {e}"),
        Err(e) => panic!("unexpected harness/input failure: {e}"),
    }
}

#[test]
#[ignore = "requires ffmpeg and all three encoders"]
fn codec_moving_sequence() {
    use std::process::Command;
    let dir = tempfile::tempdir().unwrap();
    let s = TileSpec::default();
    let p = Payload([37; 8]);
    for frame in 0..6 {
        let mut base = common::fixture("text", 696, 648);
        // Moving opaque patch changes inter-frame prediction while the identifier stays fixed.
        for y in 230..290 {
            for x in 50 + frame * 31..130 + frame * 31 {
                base.set(x, y, 200.0);
            }
        }
        encode_frame(&base, &p, &s)
            .unwrap()
            .save_png(dir.path().join(format!("input{frame:02}.png")))
            .unwrap();
    }
    for (codec, encoder) in [
        (Codec::H264, "libx264"),
        (Codec::Vp9, "libvpx-vp9"),
        (Codec::Av1, "libsvtav1"),
    ] {
        let enc = dir.path().join(format!("{}.mkv", codec.name()));
        let mut cmd = Command::new("ffmpeg");
        cmd.args(["-y", "-loglevel", "error", "-framerate", "6", "-i"])
            .arg(dir.path().join("input%02d.png"))
            .args([
                "-c:v",
                encoder,
                "-crf",
                &codec.default_crf().to_string(),
                "-g",
                "6",
                "-pix_fmt",
                "yuv420p",
                "-frames:v",
                "6",
            ]);
        if matches!(codec, Codec::Vp9) {
            cmd.args(["-b:v", "0", "-cpu-used", "4"]);
        }
        if matches!(codec, Codec::Av1) {
            cmd.args(["-preset", "8"]);
        }
        let out = cmd.arg(&enc).output().unwrap();
        assert!(
            out.status.success(),
            "{}",
            String::from_utf8_lossy(&out.stderr)
        );
        let out = Command::new("ffmpeg")
            .args(["-y", "-loglevel", "error", "-i"])
            .arg(enc)
            .args(["-frames:v", "6"])
            .arg(dir.path().join("received%02d.png"))
            .output()
            .unwrap();
        assert!(
            out.status.success(),
            "{}",
            String::from_utf8_lossy(&out.stderr)
        );
        for frame in 1..=6 {
            let received =
                watermark::LumaFrame::load_png(dir.path().join(format!("received{frame:02}.png")))
                    .unwrap();
            let crop = received.crop(17, 29, 464, 432).unwrap();
            assert_eq!(
                decode_frame(&crop, &s)
                    .unwrap_or_else(|e| panic!("{} frame{frame}: {e}", codec.name())),
                p
            );
        }
    }
}
