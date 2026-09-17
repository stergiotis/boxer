//! Stage 2 acceptance — Golay [24,12,8] error correction.
//!
//! - ≤3 bit errors per word are always corrected exactly.
//! - Exactly four errors are detected. Larger error patterns can map to another
//!   valid message; CRC is not authentication.
//! - Full 80-bit info word survives encode → (corrupt) → decode.

use rand::rngs::StdRng;
use rand::{RngExt, SeedableRng};

use watermark::fec::{
    decode_payload, decode_word, encode_info, encode_word, BITS_PER_WORD, N_WORDS,
};
use watermark::payload::INFO_BITS;
use watermark::Payload;

/// Flip the bits set in `mask` (a 24-bit error pattern) into a codeword.
fn apply(cw: u32, mask: u32) -> u32 {
    cw ^ (mask & 0x00ff_ffff)
}

#[test]
fn corrects_all_weight_1_and_2_errors() {
    // Exhaustive over every info word × every 1- and 2-bit error pattern.
    for x in 0..4096u16 {
        let cw = encode_word(x);
        for i in 0..BITS_PER_WORD {
            let e1 = 1u32 << i;
            assert_eq!(decode_word(apply(cw, e1)), (x, 1), "weight-1 i={i} x={x}");
            for j in (i + 1)..BITS_PER_WORD {
                let e2 = e1 | (1u32 << j);
                assert_eq!(
                    decode_word(apply(cw, e2)),
                    (x, 2),
                    "weight-2 i={i} j={j} x={x}"
                );
            }
        }
    }
}

#[test]
fn corrects_random_weight_3_errors() {
    let mut rng = StdRng::seed_from_u64(0xC0DE_F00D);
    for _ in 0..200_000 {
        let x = rng.random_range(0..4096u16);
        let cw = encode_word(x);
        // Pick 3 distinct bit positions.
        let mut bits = [0u32; 3];
        bits[0] = rng.random_range(0..BITS_PER_WORD as u32);
        loop {
            bits[1] = rng.random_range(0..BITS_PER_WORD as u32);
            if bits[1] != bits[0] {
                break;
            }
        }
        loop {
            bits[2] = rng.random_range(0..BITS_PER_WORD as u32);
            if bits[2] != bits[0] && bits[2] != bits[1] {
                break;
            }
        }
        let mask = (1 << bits[0]) | (1 << bits[1]) | (1 << bits[2]);
        assert_eq!(
            decode_word(apply(cw, mask)),
            (x, 3),
            "x={x} mask={mask:#08x}"
        );
    }
}

#[test]
fn info_word_roundtrip_clean() {
    let mut rng = StdRng::seed_from_u64(11);
    for _ in 0..1000 {
        let mut bits = [false; INFO_BITS];
        for b in bits.iter_mut() {
            *b = rng.random();
        }
        let cws = encode_info(&bits);
        let (back, stats) = watermark::fec::decode_words(&cws);
        assert_eq!(back, bits);
        assert_eq!(stats.corrected, 0);
        assert_eq!(stats.uncorrectable, 0);
    }
}

#[test]
fn info_word_survives_3_errors_per_word() {
    // The design guarantee: each word tolerates 3 errors independently, so the
    // whole 80-bit word survives up to 3 errors in *every* one of the 7 words.
    let mut rng = StdRng::seed_from_u64(22);
    for _ in 0..2000 {
        let payload = Payload(rng.random());
        let info = payload.to_info_bits();
        let mut cws = encode_info(&info);
        for cw in cws.iter_mut() {
            // Inject exactly 3 distinct errors into this word.
            let mut mask = 0u32;
            while mask.count_ones() < 3 {
                mask |= 1u32 << rng.random_range(0..BITS_PER_WORD as u32);
            }
            *cw = apply(*cw, mask);
        }
        let got = decode_payload(&cws).expect("≤3 errors/word must recover CRC-clean");
        assert_eq!(got, payload);
    }
}

#[test]
fn every_weight_four_pattern_is_detected() {
    // Linearity makes one codeword sufficient for exhaustive error syndromes.
    let p = Payload([17; 8]);
    let original = encode_info(&p.to_info_bits());
    for i in 0..24 {
        for j in i + 1..24 {
            for k in j + 1..24 {
                for l in k + 1..24 {
                    let mask = (1 << i) | (1 << j) | (1 << k) | (1 << l);
                    assert_eq!(
                        decode_word(original[0] ^ mask).1,
                        watermark::fec::UNCORRECTABLE
                    );
                    let mut words = original;
                    words[0] ^= mask;
                    assert!(matches!(
                        decode_payload(&words),
                        Err(watermark::Error::Uncorrectable)
                    ));
                }
            }
        }
    }
}

#[test]
fn padding_is_checked_even_when_information_crc_is_valid() {
    let p = Payload([17; 8]);
    let mut words = encode_info(&p.to_info_bits());
    let (last, errors) = decode_word(words[N_WORDS - 1]);
    assert_eq!(errors, 0);
    words[N_WORDS - 1] = encode_word(last | 1);
    assert!(matches!(
        decode_payload(&words),
        Err(watermark::Error::InvalidPadding)
    ));
}

#[test]
fn checksum_failure_and_valid_replacement_are_distinct() {
    let p = Payload([17; 8]);
    let mut info = p.to_info_bits();
    info[0] = !info[0];
    assert!(matches!(
        decode_payload(&encode_info(&info)),
        Err(watermark::Error::CrcMismatch)
    ));
    let replacement = Payload([18; 8]);
    assert_eq!(
        decode_payload(&encode_info(&replacement.to_info_bits())).unwrap(),
        replacement
    );
}
