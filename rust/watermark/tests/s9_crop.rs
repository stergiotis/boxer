//! Stage 9 acceptance — THE requirement.
//!
//! After codec compression, crop to 464×432 at a random offset and decode *only*
//! that crop. Over ≥500 random offsets per codec — including the single-tile
//! worst case — every crop must yield all 64 bits, CRC-clean.
//!
//! ffmpeg is expensive, cropping is cheap: we encode a handful of oversized
//! frames per codec, then sweep many crop offsets over each decoded frame.
//! Skipped (not failed) when ffmpeg is absent.

use rand::rngs::StdRng;
use rand::{Rng, SeedableRng};

use watermark::codec::{ffmpeg_available, roundtrip, Codec};
use watermark::decode::decode_frame;
use watermark::locate::locate;
use watermark::render::encode_frame;
use watermark::{LumaFrame, Payload, TileSpec};

struct Outcome {
    crops: u32,
    failures: u32,
    singles: u32,
}

fn sweep_codec(codec: Codec, crf: u32, frames: u32, crops_per_frame: u32, seed: u64) -> Outcome {
    let spec = TileSpec::default();
    // Oversized so any 464×432 crop with offset up to a full period fits.
    let (fw, fh) = (spec.window_w() + spec.tile_w, spec.window_h() + spec.tile_h); // 696×648
    let mut rng = StdRng::seed_from_u64(seed);
    let mut out = Outcome {
        crops: 0,
        failures: 0,
        singles: 0,
    };

    for f in 0..frames {
        let payload = Payload(rng.gen());
        let base = LumaFrame::synthetic_natural(fw, fh, seed.wrapping_add(f as u64));
        let wm = encode_frame(&base, &payload, &spec);
        let dec_full = roundtrip(&wm, codec, crf).expect("ffmpeg round-trip");

        for _ in 0..crops_per_frame {
            let ox = rng.gen_range(0..=spec.tile_w);
            let oy = rng.gen_range(0..=spec.tile_h);
            let crop = dec_full.crop(ox, oy, spec.window_w(), spec.window_h());

            // Track whether this offset exposes a single tile (the worst case).
            let loc = locate(&crop, &spec);
            if spec
                .complete_tile_origins(crop.w, crop.h, loc.phase_x, loc.phase_y)
                .len()
                == 1
            {
                out.singles += 1;
            }

            out.crops += 1;
            if decode_frame(&crop, &spec).ok() != Some(payload) {
                out.failures += 1;
            }
        }
    }
    out
}

#[test]
fn every_crop_recovers_payload() {
    if !ffmpeg_available() {
        eprintln!("ffmpeg not found on PATH — skipping Stage 9 crop test");
        return;
    }
    // 5 frames × 110 crops = 550 crops per codec (> 500), 3 codecs.
    for codec in Codec::all() {
        let crf = codec.default_crf();
        let o = sweep_codec(codec, crf, 5, 110, 0xC0FFEE ^ crf as u64);
        println!(
            "{:5} crf {crf}: {} crops, {} single-tile, {} failures",
            codec.name(),
            o.crops,
            o.singles,
            o.failures
        );
        assert!(o.crops >= 500, "need ≥500 crops, got {}", o.crops);
        assert!(
            o.singles > o.crops / 2,
            "expected the single-tile worst case to dominate, got {}/{}",
            o.singles,
            o.crops
        );
        assert_eq!(
            o.failures,
            0,
            "{}: {} crops failed to recover CRC-clean",
            codec.name(),
            o.failures
        );
    }
}
