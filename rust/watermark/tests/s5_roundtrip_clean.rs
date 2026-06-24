//! Stage 5 acceptance — encode → decode is bit-exact on a clean array, both on
//! a flat mid-gray base and on non-flat synthetic "natural" content. This is the
//! golden test: if it fails, nothing downstream works.

use rand::rngs::StdRng;
use rand::{Rng, SeedableRng};

use watermark::decode::{decode_aligned, decode_at_origins};
use watermark::render::encode_frame;
use watermark::{LumaFrame, Payload, TileSpec};

fn roundtrip_over_base(base: &LumaFrame, seed: u64, n: usize) {
    let spec = TileSpec::default();
    let mut rng = StdRng::seed_from_u64(seed);
    for _ in 0..n {
        let payload = Payload(rng.gen());
        let wm = encode_frame(base, &payload, &spec);
        let got = decode_aligned(&wm, &spec).expect("clean decode must be CRC-clean");
        assert_eq!(got, payload, "bit-exact round-trip failed");
    }
}

#[test]
fn roundtrip_flat_midgray() {
    let spec = TileSpec::default();
    let base = LumaFrame::filled(spec.window_w(), spec.window_h(), 128.0);
    roundtrip_over_base(&base, 0xA11CE, 1000);
}

#[test]
fn roundtrip_synthetic_natural() {
    let spec = TileSpec::default();
    let base = LumaFrame::synthetic_natural(spec.window_w(), spec.window_h(), 5);
    roundtrip_over_base(&base, 0xB0B, 1000);
}

#[test]
fn single_tile_decode_clean() {
    // Prove the single-tile guarantee already holds on clean data: decode from
    // exactly one tile origin.
    let spec = TileSpec::default();
    let base = LumaFrame::synthetic_natural(spec.tile_w, spec.tile_h, 9);
    let mut rng = StdRng::seed_from_u64(123);
    for _ in 0..1000 {
        let payload = Payload(rng.gen());
        let wm = encode_frame(&base, &payload, &spec);
        let got = decode_at_origins(&wm, &[(0, 0)], &spec).expect("single tile must decode");
        assert_eq!(got, payload);
    }
}
