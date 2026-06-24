//! Stage 1 acceptance: payload <-> info-word round-trip is bit-exact; the CRC
//! detects any single-bit corruption of the 80-bit info word.

use rand::rngs::StdRng;
use rand::{Rng, SeedableRng};
use watermark::payload::INFO_BITS;
use watermark::Payload;

#[test]
fn roundtrip_bit_exact_1000() {
    let mut rng = StdRng::seed_from_u64(0x5151_5151);
    for _ in 0..1000 {
        let mut b = [0u8; 8];
        rng.fill(&mut b[..]);
        let p = Payload(b);
        let bits = p.to_info_bits();
        let q = Payload::from_info_bits(&bits).expect("clean CRC must verify");
        assert_eq!(p, q);
    }
}

#[test]
fn single_bit_corruption_always_detected() {
    let mut rng = StdRng::seed_from_u64(7);
    for _ in 0..2000 {
        let mut b = [0u8; 8];
        rng.fill(&mut b[..]);
        let p = Payload(b);
        let mut bits = p.to_info_bits();
        // Flip exactly one bit anywhere in the 80-bit info word.
        let i = rng.gen_range(0..INFO_BITS);
        bits[i] = !bits[i];
        assert!(
            Payload::from_info_bits(&bits).is_err(),
            "single-bit flip at index {i} must be caught by CRC"
        );
    }
}
