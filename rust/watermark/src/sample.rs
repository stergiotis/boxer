//! Per-cell soft sampling.
//!
//! A data cell's soft value is `mean(inner block) − mean(outer ring)`. The
//! recovery-first encoder establishes this contrast, moving the ring only when
//! luminance headroom is needed. Concentric regions cancel a locally linear
//! gradient; nonlinear channel transforms of textured regions can still change
//! their mean difference. The sign is the bit.

use crate::frame::LumaFrame;
use crate::layout::TileSpec;

/// Mean luma of the inner block of cell `(col,row)` for a tile at pixel origin
/// `(tx,ty)`. `None` if the block is not fully inside the frame.
pub fn inner_mean(
    frame: &LumaFrame,
    tx: u32,
    ty: u32,
    col: u32,
    row: u32,
    spec: &TileSpec,
) -> Option<f32> {
    if col >= spec.cols() || row >= spec.rows() {
        return None;
    }
    let (ix, iy, isz) = spec.inner_rect(col, row);
    if tx as u64 + ix as u64 + isz as u64 > frame.w() as u64
        || ty as u64 + iy as u64 + isz as u64 > frame.h() as u64
    {
        return None;
    }
    let (x0, y0) = (tx + ix, ty + iy);
    let mut s = 0.0;
    for y in y0..y0 + isz {
        for x in x0..x0 + isz {
            s += frame.at(x, y);
        }
    }
    Some(s / (isz * isz) as f32)
}

/// Mean luma of the inner block and of the outer ring of cell `(col,row)`,
/// returned as `(inner, ring)`. `None` if the full 16×16 cell is not inside the
/// frame. Both means feed calibration (each is mapped to nominal space before
/// differencing).
pub fn inner_and_ring(
    frame: &LumaFrame,
    tx: u32,
    ty: u32,
    col: u32,
    row: u32,
    spec: &TileSpec,
) -> Option<(f32, f32)> {
    if col >= spec.cols() || row >= spec.rows() {
        return None;
    }
    let cell = spec.cell_px();
    if tx as u64 + (col as u64 + 1) * cell as u64 > frame.w() as u64
        || ty as u64 + (row as u64 + 1) * cell as u64 > frame.h() as u64
    {
        return None;
    }
    let (cx0, cy0) = (tx + col * cell, ty + row * cell);
    let (ix, iy, isz) = spec.inner_rect(col, row);
    let (ix0, iy0) = (tx + ix, ty + iy);

    let mut inner_sum = 0.0;
    let mut inner_n = 0u32;
    let mut ring_sum = 0.0;
    let mut ring_n = 0u32;
    for y in cy0..cy0 + cell {
        for x in cx0..cx0 + cell {
            let v = frame.at(x, y);
            let in_inner = x >= ix0 && x < ix0 + isz && y >= iy0 && y < iy0 + isz;
            if in_inner {
                inner_sum += v;
                inner_n += 1;
            } else {
                ring_sum += v;
                ring_n += 1;
            }
        }
    }
    Some((inner_sum / inner_n as f32, ring_sum / ring_n as f32))
}

/// Soft value of a data cell: `inner_mean − ring_mean` (`≈ ±delta`), without
/// calibration. `None` if the full 16×16 cell is not inside the frame.
pub fn cell_soft(
    frame: &LumaFrame,
    tx: u32,
    ty: u32,
    col: u32,
    row: u32,
    spec: &TileSpec,
) -> Option<f32> {
    inner_and_ring(frame, tx, ty, col, row, spec).map(|(i, r)| i - r)
}
