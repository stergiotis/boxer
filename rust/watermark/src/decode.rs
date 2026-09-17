//! Calibrate usable complete tiles, combine soft contrast, then check Golay,
//! reserved padding and CRC. These are noise checks, not authentication.

use crate::calibrate::Calibration;
use crate::fec::{self, BITS_PER_WORD, N_WORDS};
use crate::layout::{CellKind, RefLevel, TileSpec};
use crate::{sample, Error, LumaFrame, Payload};

#[derive(Debug)]
pub struct Recovered {
    pub words: [u32; N_WORDS],
    /// Complete supplied tiles, including rejected reference evidence.
    pub complete_tiles: usize,
    /// Tiles whose reference calibration contributed to the result.
    pub tiles: usize,
    /// Smallest absolute combined cell contrast, in nominal luminance units.
    pub min_contrast: f32,
}

/// Diagnostics survive FEC/CRC failure. Invalid input is an outer error.
#[derive(Debug)]
pub struct DecodeReport {
    pub payload: Result<Payload, Error>,
    pub recovered: Recovered,
    pub fec: fec::DecodeStats,
    pub location: Option<crate::locate::Locate>,
}

pub fn recover_words(
    frame: &LumaFrame,
    origins: &[(u32, u32)],
    spec: &TileSpec,
) -> Result<Recovered, Error> {
    frame.validate()?;
    if origins.iter().any(|&(x, y)| {
        x as u64 + spec.tile_w() as u64 > frame.w() as u64
            || y as u64 + spec.tile_h() as u64 > frame.h() as u64
    }) {
        return Err(Error::BadDimensions("tile origin outside frame".into()));
    }
    let mut soft = [[0f64; BITS_PER_WORD]; N_WORDS];
    let mut tiles = 0;
    for &(tx, ty) in origins {
        let refs: Vec<(f32, RefLevel)> = spec
            .cells()
            .iter()
            .filter_map(|cell| {
                if let CellKind::Ref { level } = cell.kind {
                    sample::inner_mean(frame, tx, ty, cell.col as u32, cell.row as u32, spec)
                        .map(|m| (m, level))
                } else {
                    None
                }
            })
            .collect();
        let Some(calib) = Calibration::fit(&refs) else {
            continue;
        };
        for cell in spec.cells() {
            if let CellKind::Data { word, bit } = cell.kind {
                let (inner, ring) =
                    sample::inner_and_ring(frame, tx, ty, cell.col as u32, cell.row as u32, spec)
                        .expect("validated complete tile");
                soft[word as usize][bit as usize] += calib.soft(inner, ring) as f64;
            }
        }
        tiles += 1;
    }
    let mut words = [0u32; N_WORDS];
    let mut min_contrast = f64::INFINITY;
    for (word, values) in words.iter_mut().zip(soft.iter()) {
        for (b, &sum) in values.iter().enumerate() {
            let value = if tiles == 0 { 0.0 } else { sum / tiles as f64 };
            min_contrast = min_contrast.min(value.abs());
            if value > 0.0 {
                *word |= 1 << (BITS_PER_WORD - 1 - b);
            }
        }
    }
    Ok(Recovered {
        words,
        complete_tiles: origins.len(),
        tiles,
        min_contrast: min_contrast as f32,
    })
}

pub fn decode_report_at_origins(
    frame: &LumaFrame,
    origins: &[(u32, u32)],
    spec: &TileSpec,
) -> Result<DecodeReport, Error> {
    let recovered = recover_words(frame, origins, spec)?;
    let (info, fec) = fec::decode_words(&recovered.words);
    let payload = if recovered.complete_tiles == 0 {
        Err(Error::NoCompleteTile)
    } else if recovered.tiles == 0 {
        Err(Error::NoUsableTile)
    } else {
        fec::checked_payload(&info, &fec)
    };
    Ok(DecodeReport {
        payload,
        recovered,
        fec,
        location: None,
    })
}

pub fn decode_at_origins(
    frame: &LumaFrame,
    origins: &[(u32, u32)],
    spec: &TileSpec,
) -> Result<Payload, Error> {
    decode_report_at_origins(frame, origins, spec)?.payload
}

pub fn decode_aligned(frame: &LumaFrame, spec: &TileSpec) -> Result<Payload, Error> {
    let origins = spec.complete_tile_origins(frame.w(), frame.h(), 0.0, 0.0);
    decode_at_origins(frame, &origins, spec)
}

pub fn decode_report(frame: &LumaFrame, spec: &TileSpec) -> Result<DecodeReport, Error> {
    let loc = crate::locate::locate(frame, spec)?;
    let origins = spec.complete_tile_origins(frame.w(), frame.h(), loc.phase_x, loc.phase_y);
    let mut report = decode_report_at_origins(frame, &origins, spec)?;
    report.location = Some(loc);
    Ok(report)
}

pub fn decode_frame(frame: &LumaFrame, spec: &TileSpec) -> Result<Payload, Error> {
    decode_report(frame, spec)?.payload
}
