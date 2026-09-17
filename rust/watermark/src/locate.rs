//! Stage 7 — tile locator: recover the tile-grid phase from a translated frame.
//!
//! The tile **period** is fixed by the spec (we control the watermark), and
//! Stage 8 verifies the codec preserves resolution, so there is no rescale to
//! detect — the locator's job is to recover the **phase** on each axis, then
//! enumerate the fully-contained tiles.
//!
//! We score a candidate phase by the **covariance between the reference cells'
//! measured inner means and their nominal levels**. At the true phase the bold
//! black/mid/white reference cells land exactly on their known levels, so this
//! covariance tends to be positive. Content can also correlate with this pattern;
//! the score is not a detection probability. A coarse sweep and local refinement
//! estimate phase; calibration, FEC and CRC subsequently check the evidence.

use crate::frame::LumaFrame;
use crate::layout::{CellKind, TileSpec};
use crate::sample;

/// Recovered tile-grid geometry. `period_*` are echoed from the spec; `phase_*`
/// is the position (px, in `[0, period)`) of the tile origin within the frame.
#[derive(Clone, Copy, Debug)]
pub struct Locate {
    pub period_x: f32,
    pub period_y: f32,
    pub phase_x: f32,
    pub phase_y: f32,
    /// Raw reference covariance; diagnostic only, not a probability.
    pub score: f32,
}

/// Locate the tile grid in `frame`.
pub fn locate(frame: &LumaFrame, spec: &TileSpec) -> Result<Locate, crate::Error> {
    frame.validate()?;
    if frame.w() < spec.tile_w() || frame.h() < spec.tile_h() {
        return Err(crate::Error::NoCompleteTile);
    }
    // Reference cells: (col, row, nominal level).
    let refs: Vec<(u32, u32, f32)> = spec
        .cells()
        .iter()
        .filter_map(|c| match c.kind {
            CellKind::Ref { level } => Some((c.col as u32, c.row as u32, level.luma())),
            CellKind::Data { .. } => None,
        })
        .collect();
    let nom_mean = refs.iter().map(|r| r.2).sum::<f32>() / refs.len() as f32;

    // Score = covariance of the reference cells' measured means with their
    // nominal levels. Since Σ(nom − nom_mean) = 0, the measured mean drops out:
    // cov = Σ measured·(nom − nom_mean), a single pass with no per-call storage.
    let score = |px: u32, py: u32| -> f32 {
        if px as u64 + spec.tile_w() as u64 > frame.w() as u64
            || py as u64 + spec.tile_h() as u64 > frame.h() as u64
        {
            return f32::NEG_INFINITY;
        }
        let mut cov = 0f32;
        for &(col, row, nom) in &refs {
            match sample::inner_mean(frame, px, py, col, row, spec) {
                Some(m) => cov += m * (nom - nom_mean),
                None => return f32::NEG_INFINITY, // tile doesn't fit here
            }
        }
        cov
    };

    // Coarse sweep over the full period on each axis.
    const COARSE: u32 = 4;
    let (mut bx, mut by, mut bs) = (0u32, 0u32, f32::NEG_INFINITY);
    let mut py = 0;
    while py < spec.tile_h() {
        let mut px = 0;
        while px < spec.tile_w() {
            let s = score(px, py);
            if s > bs {
                bs = s;
                bx = px;
                by = py;
            }
            px += COARSE;
        }
        py += COARSE;
    }

    // 1-px refinement around the coarse peak. The phase is circular, so the
    // search must wrap: a true phase just below the period (e.g. ox=2 → 230) can
    // make the coarse winner land at 0, a couple px away on the *other* side.
    let (rx, ry) = refine(&score, bx, by, COARSE + 1, spec);

    Ok(Locate {
        period_x: spec.tile_w() as f32,
        period_y: spec.tile_h() as f32,
        phase_x: rx as f32,
        phase_y: ry as f32,
        score: score(rx, ry),
    })
}

fn refine(
    score: &impl Fn(u32, u32) -> f32,
    cx: u32,
    cy: u32,
    radius: u32,
    spec: &TileSpec,
) -> (u32, u32) {
    let r = radius as i32;
    let (pw, ph) = (spec.tile_w() as i32, spec.tile_h() as i32);
    let (mut bx, mut by, mut bs) = (cx, cy, f32::NEG_INFINITY);
    for dy in -r..=r {
        let py = (cy as i32 + dy).rem_euclid(ph) as u32;
        for dx in -r..=r {
            let px = (cx as i32 + dx).rem_euclid(pw) as u32;
            let s = score(px, py);
            if s > bs {
                bs = s;
                bx = px;
                by = py;
            }
        }
    }
    (bx, by)
}

/// Convenience: the pixel origins of every complete tile in `frame`.
pub fn complete_tiles(frame: &LumaFrame, spec: &TileSpec) -> Result<Vec<(u32, u32)>, crate::Error> {
    let loc = locate(frame, spec)?;
    Ok(spec.complete_tile_origins(frame.w(), frame.h(), loc.phase_x, loc.phase_y))
}
