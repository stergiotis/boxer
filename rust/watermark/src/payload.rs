//! Stage 1 — the 64-bit payload and its CRC-16 integrity tail.
//!
//! A [`Payload`] is 8 bytes. We append a 16-bit CRC to form the **80-bit info
//! word** that the FEC layer protects. The CRC is the final clean/not-clean
//! gate after Golay decoding (Golay can *mis-correct* >3-error words into a
//! valid-but-wrong codeword; only the CRC catches that), with a 1/65536
//! missed-detection floor.

use crate::Error;

/// Payload size in bytes.
pub const PAYLOAD_BYTES: usize = 8;
/// Payload size in bits.
pub const PAYLOAD_BITS: usize = PAYLOAD_BYTES * 8; // 64
/// CRC tail width in bits.
pub const CRC_BITS: usize = 16;
/// Info-word width: payload + CRC. This is what the FEC layer encodes.
pub const INFO_BITS: usize = PAYLOAD_BITS + CRC_BITS; // 80

/// The 64-bit watermark payload.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Hash)]
pub struct Payload(pub [u8; PAYLOAD_BYTES]);

impl Payload {
    /// CRC-16 over the 8 payload bytes.
    pub fn crc(&self) -> u16 {
        crc16_ccitt(&self.0)
    }

    /// The 80-bit info word: 64 payload bits (MSB-first per byte) followed by
    /// 16 CRC bits (MSB-first).
    pub fn to_info_bits(&self) -> [bool; INFO_BITS] {
        let mut bits = [false; INFO_BITS];
        for (i, &byte) in self.0.iter().enumerate() {
            for b in 0..8 {
                bits[i * 8 + b] = (byte >> (7 - b)) & 1 == 1;
            }
        }
        let crc = self.crc();
        for b in 0..CRC_BITS {
            bits[PAYLOAD_BITS + b] = (crc >> (CRC_BITS - 1 - b)) & 1 == 1;
        }
        bits
    }

    /// Parse an 80-bit info word back into a payload, verifying the CRC.
    ///
    /// Returns [`Error::CrcMismatch`] if the recomputed CRC over the payload
    /// bits disagrees with the carried CRC bits — the decoder must treat this as
    /// "not clean" and never surface the bytes.
    pub fn from_info_bits(bits: &[bool; INFO_BITS]) -> Result<Payload, Error> {
        let mut bytes = [0u8; PAYLOAD_BYTES];
        for (i, byte) in bytes.iter_mut().enumerate() {
            for b in 0..8 {
                if bits[i * 8 + b] {
                    *byte |= 1 << (7 - b);
                }
            }
        }
        let mut crc_rx: u16 = 0;
        for b in 0..CRC_BITS {
            if bits[PAYLOAD_BITS + b] {
                crc_rx |= 1 << (CRC_BITS - 1 - b);
            }
        }
        let p = Payload(bytes);
        if p.crc() == crc_rx {
            Ok(p)
        } else {
            Err(Error::CrcMismatch)
        }
    }

    /// Hex string of the payload bytes (for CLI display).
    pub fn to_hex(&self) -> String {
        self.0.iter().map(|b| format!("{b:02x}")).collect()
    }

    /// Parse a 16-hex-digit string into a payload.
    pub fn from_hex(s: &str) -> Result<Payload, Error> {
        let s = s.trim().trim_start_matches("0x");
        if s.len() != PAYLOAD_BYTES * 2 {
            return Err(Error::BadDimensions(format!(
                "payload hex must be {} chars, got {}",
                PAYLOAD_BYTES * 2,
                s.len()
            )));
        }
        let mut bytes = [0u8; PAYLOAD_BYTES];
        for (i, byte) in bytes.iter_mut().enumerate() {
            *byte = u8::from_str_radix(&s[i * 2..i * 2 + 2], 16)
                .map_err(|e| Error::BadDimensions(format!("bad hex: {e}")))?;
        }
        Ok(Payload(bytes))
    }
}

/// CRC-16/CCITT-FALSE: polynomial `0x1021`, init `0xFFFF`, no input/output
/// reflection, no final XOR. MSB-first. This exact variant is internal-only
/// (encode and decode both use this routine), so interop naming is moot; it is
/// pinned here for reproducibility.
pub fn crc16_ccitt(data: &[u8]) -> u16 {
    let mut crc: u16 = 0xFFFF;
    for &byte in data {
        crc ^= (byte as u16) << 8;
        for _ in 0..8 {
            if crc & 0x8000 != 0 {
                crc = (crc << 1) ^ 0x1021;
            } else {
                crc <<= 1;
            }
        }
    }
    crc
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn crc_known_vector() {
        // CRC-16/CCITT-FALSE of "123456789" is the canonical 0x29B1.
        assert_eq!(crc16_ccitt(b"123456789"), 0x29B1);
    }

    #[test]
    fn info_bits_roundtrip() {
        let p = Payload([0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef]);
        let bits = p.to_info_bits();
        assert_eq!(Payload::from_info_bits(&bits).unwrap(), p);
    }

    #[test]
    fn hex_roundtrip() {
        let p = Payload([0xde, 0xad, 0xbe, 0xef, 0x00, 0x11, 0x22, 0x33]);
        assert_eq!(Payload::from_hex(&p.to_hex()).unwrap(), p);
    }
}
