//! Stage 2 — extended binary Golay [24,12,8] FEC, ported in-crate.
//!
//! This is a direct port of the IRIG-106 (Std 106-15, Appendix Q) construction
//! used elsewhere in this repo:
//!
//! - `doc/golay24/encoding.c` — generator parity sub-matrix `G_P`, encode LUT
//! - `doc/golay24/decoding.c` — parity-check sub-matrix `H_P`, syndrome /
//!   coset-leader / error-weight LUTs
//! - `public/fec/code/golay24` — the Go package this mirrors bit-for-bit
//!
//! Codeword layout is **info in the high 12 bits**: `codeword = info<<12 |
//! parity`. Decoding is syndrome → coset-leader table lookup. The four 4096-entry
//! tables are built once on first use (`LazyLock`); no GF(256), no
//! Berlekamp–Massey, no external FEC crate — exactly what `EXPLANATION.md` mandates.
//!
//! Each word corrects ≤3 bit errors and *detects* 4 (reported as
//! `n_errors == UNCORRECTABLE`). A word hit by >3 errors can also be
//! *mis-corrected* into a valid-but-wrong codeword with a low error count — that
//! is caught downstream by the payload CRC (`EXPLANATION.md` §Guardrails 9), never
//! surfaced as a clean decode.

use std::sync::LazyLock;

use crate::payload::INFO_BITS;
use crate::{Error, Payload};

/// Golay words needed for the 80-bit info word: ⌈80/12⌉ = 7 (4 pad bits in the
/// last word's low positions).
pub const N_WORDS: usize = 7;
/// Info bits carried per Golay word.
pub const INFO_PER_WORD: usize = 12;
/// Coded bits per Golay word.
pub const BITS_PER_WORD: usize = 24;
/// Total coded bits = data cells = 7 × 24.
pub const CODED_BITS: usize = N_WORDS * BITS_PER_WORD; // 168
/// Error-table sentinel: syndrome unreachable by ≤3 errors ⇒ uncorrectable
/// (detected). The matching coset-leader entry is the `0xfff` garbage default.
pub const UNCORRECTABLE: u8 = 4;

// Generator parity sub-matrix P (doc/golay24/encoding.c, G_P[12]).
const G_P: [u16; 12] = [
    0xc75, 0x63b, 0xf68, 0x7b4, 0x3da, 0xd99, 0x6cd, 0x367, 0xdc6, 0xa97, 0x93e, 0x8eb,
];
// Parity-check sub-matrix (doc/golay24/decoding.c, H_P[12]).
const H_P: [u16; 12] = [
    0xa4f, 0xf68, 0x7b4, 0x3da, 0x1ed, 0xab9, 0xf13, 0xdc6, 0x6e3, 0x93e, 0x49f, 0xc75,
];

struct Tables {
    encode: [u32; 4096],   // info12 -> 24-bit codeword
    syndrome: [u16; 4096], // low-12 of codeword -> partial syndrome
    correct: [u16; 4096],  // syndrome -> coset-leader high-12 (error in info bits)
    errors: [u8; 4096],    // syndrome -> min error weight (4 == uncorrectable)
}

static TABLES: LazyLock<Tables> = LazyLock::new(build_tables);

fn build_tables() -> Tables {
    // Encode LUT: codeword = (info<<12) ^ Σ G_P[i] over set info bits (MSB-first).
    let mut encode = [0u32; 4096];
    for (x, slot) in encode.iter_mut().enumerate() {
        let mut e = (x as u32) << 12;
        for (i, &g) in G_P.iter().enumerate() {
            if (x >> (11 - i)) & 1 == 1 {
                e ^= g as u32;
            }
        }
        *slot = e;
    }

    // Syndrome LUT over the low 12 bits.
    let mut syndrome = [0u16; 4096];
    for (x, slot) in syndrome.iter_mut().enumerate() {
        let mut s = 0u16;
        for (i, &h) in H_P.iter().enumerate() {
            if (x >> (11 - i)) & 1 == 1 {
                s ^= h;
            }
        }
        *slot = s;
    }

    // Coset-leader / error-weight LUTs: default to the uncorrectable sentinel,
    // then fill every syndrome reachable by an error of weight ≤3.
    let mut correct = [0x0fffu16; 4096];
    let mut errors = [UNCORRECTABLE; 4096];
    errors[0] = 0;
    correct[0] = 0;
    for i in 0..24u32 {
        for j in 0..24u32 {
            for k in 0..24u32 {
                let error = (1u32 << i) | (1u32 << j) | (1u32 << k);
                let syn = (syndrome[(error & 0xfff) as usize] ^ ((error >> 12) as u16)) as usize;
                correct[syn] = ((error >> 12) & 0xfff) as u16;
                errors[syn] = error.count_ones() as u8;
            }
        }
    }

    Tables {
        encode,
        syndrome,
        correct,
        errors,
    }
}

/// Encode a 12-bit info word into its 24-bit Golay codeword.
pub fn encode_word(info12: u16) -> u32 {
    TABLES.encode[(info12 & 0x0fff) as usize]
}

/// Decode a (possibly corrupted) 24-bit codeword.
///
/// Returns `(corrected_info12, n_errors)`. `n_errors == UNCORRECTABLE` (4) flags
/// a word that cannot be trusted; `0..=3` is the corrected bit-error count.
pub fn decode_word(codeword: u32) -> (u16, u8) {
    let v1 = ((codeword >> 12) & 0x0fff) as u16;
    let v2 = (codeword & 0x0fff) as usize;
    let syn = (TABLES.syndrome[v2] ^ v1) as usize;
    (v1 ^ TABLES.correct[syn], TABLES.errors[syn])
}

/// Pack the 80-bit info word into 7 × 12-bit words (MSB-first; the last word's
/// low 4 bits are zero pad).
fn pack_info(info: &[bool; INFO_BITS]) -> [u16; N_WORDS] {
    let mut words = [0u16; N_WORDS];
    for (w, slot) in words.iter_mut().enumerate() {
        let mut v = 0u16;
        for b in 0..INFO_PER_WORD {
            let idx = w * INFO_PER_WORD + b;
            let bit = idx < INFO_BITS && info[idx];
            v = (v << 1) | bit as u16;
        }
        *slot = v;
    }
    words
}

/// Inverse of [`pack_info`] (pad bits discarded).
fn unpack_info(words: &[u16; N_WORDS]) -> [bool; INFO_BITS] {
    let mut info = [false; INFO_BITS];
    for (w, &word) in words.iter().enumerate() {
        for b in 0..INFO_PER_WORD {
            let idx = w * INFO_PER_WORD + b;
            if idx < INFO_BITS {
                info[idx] = (word >> (INFO_PER_WORD - 1 - b)) & 1 == 1;
            }
        }
    }
    info
}

/// Encode an 80-bit info word into 7 Golay codewords.
pub fn encode_info(info: &[bool; INFO_BITS]) -> [u32; N_WORDS] {
    let words = pack_info(info);
    let mut cws = [0u32; N_WORDS];
    for (cw, &w) in cws.iter_mut().zip(words.iter()) {
        *cw = encode_word(w);
    }
    cws
}

/// Aggregate decode diagnostics over the 7 words.
#[derive(Debug, Clone, Copy, Default)]
pub struct DecodeStats {
    /// Total bit errors corrected across all correctable words.
    pub corrected: u32,
    /// Number of words flagged uncorrectable (≥4 errors detected).
    pub uncorrectable: usize,
}

/// Decode 7 received codewords back to the 80-bit info word, with diagnostics.
pub fn decode_words(words: &[u32; N_WORDS]) -> ([bool; INFO_BITS], DecodeStats) {
    let mut info12 = [0u16; N_WORDS];
    let mut stats = DecodeStats::default();
    for (slot, &cw) in info12.iter_mut().zip(words.iter()) {
        let (v, e) = decode_word(cw);
        *slot = v;
        if e == UNCORRECTABLE {
            stats.uncorrectable += 1;
        } else {
            stats.corrected += e as u32;
        }
    }
    (unpack_info(&info12), stats)
}

/// Decode 7 received codewords straight to a CRC-verified [`Payload`].
///
/// Returns [`Error::CrcMismatch`] when the recovered info word fails its CRC —
/// the only honest outcome when Golay may have mis-corrected a burst-damaged
/// word (`EXPLANATION.md` §Guardrails 9).
pub fn decode_payload(words: &[u32; N_WORDS]) -> Result<Payload, Error> {
    let (info, _stats) = decode_words(words);
    Payload::from_info_bits(&info)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn golden_matches_go_encoding_table() {
        // Spot values lifted from public/fec/code/golay24/encoding_table.go.
        // Proves the ported tables are byte-identical to the established impl.
        const GOLDEN: &[(u16, u32)] = &[
            (0, 0x000000),
            (1, 0x0018eb),
            (2, 0x00293e),
            (3, 0x0031d5),
            (7, 0x007b42),
            (12, 0x00c751),
            (255, 0x0ffd6d),
            (256, 0x1007b4),
            (1000, 0x3e8d94),
            (2048, 0x800c75),
            (4094, 0xffe714),
            (4095, 0xffffff),
        ];
        for &(info, cw) in GOLDEN {
            assert_eq!(encode_word(info), cw, "encode_word({info}) mismatch");
        }
    }

    #[test]
    fn clean_codewords_decode_to_themselves() {
        for x in 0..4096u16 {
            let cw = encode_word(x);
            assert_eq!(decode_word(cw), (x, 0));
        }
    }
}
