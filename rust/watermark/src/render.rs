//! Establish inner-minus-ring contrast, including on saturated and textured
//! content (ADR-0241). Delta is target contrast, not maximum pixel alteration.

use crate::fec::{self, N_WORDS};
use crate::layout::{CellKind, TileSpec};
use crate::{Error, LumaFrame, Payload};

/// Move toward black or white to attain a target mean without clipping.
/// The same affine map applies to each sample in the region.
pub(crate) fn move_mean(value: f32, mean: f32, target: f32) -> f32 {
    let out = if target < mean {
        value * (target / mean)
    } else if target > mean {
        value + (255.0 - value) * ((target - mean) / (255.0 - mean))
    } else {
        value
    };
    out.clamp(0.0, 255.0) // floating-point endpoint drift only
}

/// Render complete cells, including cells in partial edge tiles. Incomplete
/// edge cells carry no usable symbol and remain unchanged.
pub fn render_into(
    frame: &mut LumaFrame,
    words: &[u32; N_WORDS],
    spec: &TileSpec,
) -> Result<(), Error> {
    frame.validate()?;
    if frame.w() < spec.tile_w() || frame.h() < spec.tile_h() {
        return Err(Error::NoCompleteTile);
    }
    for ty in (0..frame.h()).step_by(spec.tile_h() as usize) {
        for tx in (0..frame.w()).step_by(spec.tile_w() as usize) {
            for cell in spec.cells() {
                let col = cell.col as u32;
                let row = cell.row as u32;
                let Some((inner, ring)) =
                    crate::sample::inner_and_ring(frame, tx, ty, col, row, spec)
                else {
                    continue;
                };
                let (ix, iy, size) = spec.inner_rect(col, row);
                let (ix, iy) = (tx + ix, ty + iy);
                let (inner_target, ring_target) = match cell.kind {
                    CellKind::Data { word, bit } => {
                        let one =
                            words[word as usize] >> (fec::BITS_PER_WORD as u8 - 1 - bit) & 1 == 1;
                        let ring_target = ring.clamp(spec.delta(), 255.0 - spec.delta());
                        (
                            ring_target + if one { spec.delta() } else { -spec.delta() },
                            ring_target,
                        )
                    }
                    CellKind::Ref { level } => (level.luma(), ring),
                };
                let cx = tx + col * spec.cell_px();
                let cy = ty + row * spec.cell_px();
                for y in cy..cy + spec.cell_px() {
                    for x in cx..cx + spec.cell_px() {
                        let inside = x >= ix && x < ix + size && y >= iy && y < iy + size;
                        let old = frame.at(x, y);
                        let value = if inside {
                            match cell.kind {
                                CellKind::Ref { .. } => inner_target,
                                CellKind::Data { .. } => move_mean(old, inner, inner_target),
                            }
                        } else {
                            move_mean(old, ring, ring_target)
                        };
                        frame.set(x, y, value);
                    }
                }
            }
        }
    }
    Ok(())
}

/// Encode into a copy of `base`, retaining the fixed on-image bit layout.
pub fn encode_frame(
    base: &LumaFrame,
    payload: &Payload,
    spec: &TileSpec,
) -> Result<LumaFrame, Error> {
    base.validate()?;
    if base.w() < spec.tile_w() || base.h() < spec.tile_h() {
        return Err(Error::NoCompleteTile);
    }
    let words = fec::encode_info(&payload.to_info_bits());
    let mut frame = base.clone();
    render_into(&mut frame, &words, spec)?;
    Ok(frame)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn mean_move_has_headroom_at_both_rails() {
        assert_eq!(move_mean(0.0, 0.0, 8.0), 8.0);
        assert_eq!(move_mean(255.0, 255.0, 247.0), 247.0);
        assert_eq!(move_mean(128.0, 128.0, 128.0), 128.0);
    }

    #[test]
    fn reference_cells_read_back_nominal() {
        let spec = TileSpec::default();
        let base = LumaFrame::filled(spec.tile_w(), spec.tile_h(), 128.0).unwrap();
        let wm = encode_frame(&base, &Payload([1; 8]), &spec).unwrap();
        for c in spec.cells() {
            if let CellKind::Ref { level } = c.kind {
                assert_eq!(
                    crate::sample::inner_mean(&wm, 0, 0, c.col as u32, c.row as u32, &spec),
                    Some(level.luma())
                );
            }
        }
    }
}
