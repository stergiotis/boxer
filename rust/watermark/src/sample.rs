//! Per-cell soft sampling.
//!
//! A data cell's soft value is `mean(inner block) − mean(outer ring)`. The inner
//! block carries `±delta`; the ring is unmodulated base image, so it estimates
//! the local DC. Because inner and ring are *concentric*, any locally-linear
//! image gradient contributes equally to both means and **cancels** — leaving
//! `≈ ±delta` even over non-flat content. The sign is the bit.

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
    let (ix, iy, isz) = spec.inner_rect(col, row);
    let (x0, y0) = (tx + ix, ty + iy);
    if x0 + isz > frame.w || y0 + isz > frame.h {
        return None;
    }
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
    let cell = spec.cell_px;
    let (cx0, cy0) = (tx + col * cell, ty + row * cell);
    if cx0 + cell > frame.w || cy0 + cell > frame.h {
        return None;
    }
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
