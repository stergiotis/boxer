//! Stage 6 acceptance — calibration + perturbation robustness, all from a
//! SINGLE tile (the worst case; no multi-tile averaging crutch).
//!
//! - Global brightness offset and gamma shift do not break decode.
//! - A Gaussian-noise σ-sweep records pre-Golay BER and proves CRC-clean
//!   recovery up to a documented σ.

use rand::rngs::StdRng;
use rand::{Rng, SeedableRng};

use watermark::decode::{decode_at_origins, recover_words};
use watermark::fec::{encode_info, BITS_PER_WORD, N_WORDS};
use watermark::render::encode_frame;
use watermark::{LumaFrame, Payload, TileSpec};

fn add_gaussian(frame: &mut LumaFrame, sigma: f32, rng: &mut StdRng) {
    if sigma == 0.0 {
        return;
    }
    for v in frame.y.iter_mut() {
        let u1: f32 = rng.gen::<f32>().max(1e-7);
        let u2: f32 = rng.gen::<f32>();
        let z = (-2.0 * u1.ln()).sqrt() * (2.0 * std::f32::consts::PI * u2).cos();
        *v = (*v + sigma * z).clamp(0.0, 255.0);
    }
}

fn apply_gamma(frame: &mut LumaFrame, gamma: f32) {
    for v in frame.y.iter_mut() {
        *v = 255.0 * (*v / 255.0).powf(gamma);
    }
}

fn add_brightness(frame: &mut LumaFrame, off: f32) {
    for v in frame.y.iter_mut() {
        *v = (*v + off).clamp(0.0, 255.0);
    }
}

fn single_tile_base() -> (TileSpec, LumaFrame) {
    let spec = TileSpec::default();
    let base = LumaFrame::filled(spec.tile_w, spec.tile_h, 128.0);
    (spec, base)
}

#[test]
fn brightness_offsets_decode_clean() {
    let (spec, base) = single_tile_base();
    let mut rng = StdRng::seed_from_u64(1);
    for off in [-60.0, -30.0, 30.0, 60.0, 90.0] {
        for _ in 0..200 {
            let payload = Payload(rng.gen());
            let mut wm = encode_frame(&base, &payload, &spec);
            add_brightness(&mut wm, off);
            let got = decode_at_origins(&wm, &[(0, 0)], &spec)
                .unwrap_or_else(|_| panic!("brightness {off} must decode"));
            assert_eq!(got, payload);
        }
    }
}

#[test]
fn gamma_shifts_decode_clean() {
    // Decode on a natural base too, to exercise calibration over content.
    let spec = TileSpec::default();
    let base = LumaFrame::synthetic_natural(spec.tile_w, spec.tile_h, 4);
    let mut rng = StdRng::seed_from_u64(2);
    for gamma in [0.5f32, 0.7, 1.4, 2.0] {
        for _ in 0..200 {
            let payload = Payload(rng.gen());
            let mut wm = encode_frame(&base, &payload, &spec);
            apply_gamma(&mut wm, gamma);
            let got = decode_at_origins(&wm, &[(0, 0)], &spec)
                .unwrap_or_else(|_| panic!("gamma {gamma} must decode"));
            assert_eq!(got, payload);
        }
    }
}

#[test]
fn noise_sigma_sweep_single_tile() {
    let (spec, base) = single_tile_base();
    let mut rng = StdRng::seed_from_u64(0x0153);
    let trials = 300;

    // σ at which we still require 100% CRC-clean recovery from one tile.
    const GUARANTEED_SIGMA: f32 = 12.0;

    println!("sigma  pre_golay_BER  payload_ok/{trials}");
    for &sigma in &[0.0f32, 2.0, 4.0, 6.0, 8.0, 10.0, 12.0, 16.0, 20.0, 24.0] {
        let mut bit_errors = 0u64;
        let mut total_bits = 0u64;
        let mut ok = 0u32;
        for _ in 0..trials {
            let payload = Payload(rng.gen());
            let truth = encode_info(&payload.to_info_bits());
            let mut wm = encode_frame(&base, &payload, &spec);
            add_gaussian(&mut wm, sigma, &mut rng);

            let rec = recover_words(&wm, &[(0, 0)], &spec);
            for (r, t) in rec.words.iter().zip(truth.iter()) {
                bit_errors += (r ^ t).count_ones() as u64;
            }
            total_bits += (N_WORDS * BITS_PER_WORD) as u64;
            if decode_at_origins(&wm, &[(0, 0)], &spec).ok() == Some(payload) {
                ok += 1;
            }
        }
        let ber = bit_errors as f64 / total_bits as f64;
        println!("{sigma:5.1}  {ber:13.6}  {ok}");
        if sigma <= GUARANTEED_SIGMA {
            assert_eq!(
                ok, trials,
                "σ={sigma}: every single-tile decode must be CRC-clean"
            );
        }
    }
}
