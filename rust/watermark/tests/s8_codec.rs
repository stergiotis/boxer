//! Stage 8 acceptance — codec round-trip. Render → ffmpeg encode (h264/vp9/av1)
//! → decode first frame → decode the watermark from a SINGLE full-frame tile.
//! Reports pre-Golay BER and asserts CRC-clean recovery at the chosen quality.
//!
//! Skipped (not failed) when `ffmpeg` is absent.

use rand::rngs::StdRng;
use rand::{Rng, SeedableRng};

use watermark::codec::{ffmpeg_available, roundtrip, Codec};
use watermark::decode::{decode_at_origins, recover_words};
use watermark::fec::{encode_info, BITS_PER_WORD, N_WORDS};
use watermark::render::encode_frame;
use watermark::{LumaFrame, Payload, TileSpec};

/// Encode `n` random payloads through `codec` at `crf`, decoding each from one
/// full-frame tile. Returns `(pre_golay_ber, payload_ok_count)`.
fn measure(codec: Codec, crf: u32, n: u32, seed: u64) -> (f64, u32) {
    let spec = TileSpec::default();
    let base = LumaFrame::synthetic_natural(spec.window_w(), spec.window_h(), seed);
    let mut rng = StdRng::seed_from_u64(seed ^ crf as u64);
    let mut bit_errors = 0u64;
    let mut total_bits = 0u64;
    let mut ok = 0u32;

    for _ in 0..n {
        let payload = Payload(rng.gen());
        let truth = encode_info(&payload.to_info_bits());
        let wm = encode_frame(&base, &payload, &spec);
        let dec = roundtrip(&wm, codec, crf).expect("ffmpeg round-trip");

        let rec = recover_words(&dec, &[(0, 0)], &spec);
        for (r, t) in rec.words.iter().zip(truth.iter()) {
            bit_errors += (r ^ t).count_ones() as u64;
        }
        total_bits += (N_WORDS * BITS_PER_WORD) as u64;
        if decode_at_origins(&dec, &[(0, 0)], &spec).ok() == Some(payload) {
            ok += 1;
        }
    }
    (bit_errors as f64 / total_bits as f64, ok)
}

#[test]
fn codec_roundtrip_single_tile() {
    if !ffmpeg_available() {
        eprintln!("ffmpeg not found on PATH — skipping Stage 8 codec test");
        return;
    }
    const N: u32 = 30;
    println!("codec  crf  pre_golay_BER  payload_ok/{N}");
    for codec in Codec::all() {
        let crf = codec.default_crf();
        let (ber, ok) = measure(codec, crf, N, 0x5EED);
        println!("{:5}  {crf:3}  {ber:13.5}  {ok}", codec.name());
        assert_eq!(
            ok,
            N,
            "{} at crf {crf} must recover every payload",
            codec.name()
        );
    }
}

/// Opt-in quality sweep (`cargo test -- --ignored`) — documents where each codec
/// starts failing. No hard assertions beyond what Stage 8 already guarantees.
#[test]
#[ignore]
fn codec_quality_sweep() {
    if !ffmpeg_available() {
        eprintln!("ffmpeg not found — skipping sweep");
        return;
    }
    const N: u32 = 12;
    println!("codec  crf  pre_golay_BER  payload_ok/{N}");
    for codec in Codec::all() {
        for crf in crf_ladder(codec) {
            let (ber, ok) = measure(codec, crf, N, 0xABCD);
            println!("{:5}  {crf:3}  {ber:13.5}  {ok}", codec.name());
        }
    }
}

fn crf_ladder(codec: Codec) -> Vec<u32> {
    match codec {
        Codec::H264 => vec![20, 23, 27, 30, 33, 36],
        Codec::Vp9 => vec![28, 31, 35, 40, 45, 50],
        Codec::Av1 => vec![24, 30, 36, 42, 48, 52],
    }
}
