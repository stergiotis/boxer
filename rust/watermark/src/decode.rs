//! Stage 5 — the decode chain: sample → soft-combine → threshold →
//! reassemble codewords → Golay-decode → CRC.
//!
//! Soft values are averaged across **whatever complete tiles are present** (one
//! in the worst case) *before* thresholding, so multi-tile crops get a free
//! SNR boost while the guarantee still rests on single-tile Golay decoding
//! (`EXPLANATION.md` §Guardrails 4).

use crate::calibrate::Calibration;
use crate::fec::{self, BITS_PER_WORD, N_WORDS};
use crate::frame::LumaFrame;
use crate::layout::{CellKind, RefLevel, TileSpec};
use crate::{sample, Error, Payload};

/// The thresholded codewords (pre-Golay) recovered from a set of tiles, plus how
/// many cells contributed (for diagnostics / pre-FEC BER).
pub struct Recovered {
    pub words: [u32; N_WORDS],
    pub tiles: usize,
}

/// Soft-combine the given tile origins and threshold to 7 codewords.
pub fn recover_words(frame: &LumaFrame, origins: &[(u32, u32)], spec: &TileSpec) -> Recovered {
    let mut soft = [[0f32; BITS_PER_WORD]; N_WORDS];
    let mut count = [[0u32; BITS_PER_WORD]; N_WORDS];
    let mut tiles = 0;

    for &(tx, ty) in origins {
        // Fit this tile's brightness/gamma transfer from its reference cells.
        let mut refs: Vec<(f32, RefLevel)> = Vec::with_capacity(14);
        for cell in spec.cells() {
            if let CellKind::Ref { level } = cell.kind {
                if let Some(m) =
                    sample::inner_mean(frame, tx, ty, cell.col as u32, cell.row as u32, spec)
                {
                    refs.push((m, level));
                }
            }
        }
        let calib = Calibration::fit(&refs);

        let mut used = false;
        for cell in spec.cells() {
            if let CellKind::Data { word, bit } = cell.kind {
                if let Some((inner, ring)) =
                    sample::inner_and_ring(frame, tx, ty, cell.col as u32, cell.row as u32, spec)
                {
                    let s = match &calib {
                        Some(c) => c.soft(inner, ring),
                        None => inner - ring,
                    };
                    soft[word as usize][bit as usize] += s;
                    count[word as usize][bit as usize] += 1;
                    used = true;
                }
            }
        }
        if used {
            tiles += 1;
        }
    }

    let mut words = [0u32; N_WORDS];
    for w in 0..N_WORDS {
        for b in 0..BITS_PER_WORD {
            // Sign of the combined soft value is the bit (1 → +delta).
            if count[w][b] > 0 && soft[w][b] > 0.0 {
                words[w] |= 1 << (BITS_PER_WORD as u32 - 1 - b as u32);
            }
        }
    }
    Recovered { words, tiles }
}

/// Decode from explicit tile origins. `Err(NoCompleteTile)` if none are usable.
pub fn decode_at_origins(
    frame: &LumaFrame,
    origins: &[(u32, u32)],
    spec: &TileSpec,
) -> Result<Payload, Error> {
    let rec = recover_words(frame, origins, spec);
    if rec.tiles == 0 {
        return Err(Error::NoCompleteTile);
    }
    fec::decode_payload(&rec.words)
}

/// Decode a frame whose tile grid is aligned to the top-left (phase 0), using
/// every complete tile. This is the in-memory / pre-locator path.
pub fn decode_aligned(frame: &LumaFrame, spec: &TileSpec) -> Result<Payload, Error> {
    let origins = spec.complete_tile_origins(frame.w, frame.h, 0.0, 0.0);
    decode_at_origins(frame, &origins, spec)
}

/// The top-level decode for an arbitrary frame or crop: locate the tile grid,
/// enumerate the complete tiles, then soft-combine and decode. This is the API
/// the CLI `decode` and the crop constraint use.
pub fn decode_frame(frame: &LumaFrame, spec: &TileSpec) -> Result<Payload, Error> {
    let loc = crate::locate::locate(frame, spec);
    let origins = spec.complete_tile_origins(frame.w, frame.h, loc.phase_x, loc.phase_y);
    decode_at_origins(frame, &origins, spec)
}
