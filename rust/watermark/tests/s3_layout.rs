//! Stage 3 acceptance — tile geometry and the interleaver's spatial spread.

use watermark::layout::{CellKind, TileSpec};

#[test]
fn containment_boundary() {
    let s = TileSpec::default();
    assert!(s.guaranteed_containment(464, 432)); // exactly 2×tile
    assert!(s.guaranteed_containment(465, 433));
    assert!(!s.guaranteed_containment(463, 431));
    assert!(!s.guaranteed_containment(464, 431)); // one axis short
    assert!(!s.guaranteed_containment(463, 432));
}

#[test]
fn cells_tile_active_area_without_overlap() {
    let s = TileSpec::default();
    // Paint each cell's 16×16 footprint; assert the 224×208 active area is
    // covered exactly once and nothing spills into the guard gutter.
    let aw = s.active_w() as usize;
    let ah = s.active_h() as usize;
    let mut cover = vec![0u8; aw * ah];
    for c in s.cells() {
        let (x0, y0) = s.cell_origin(c.col as u32, c.row as u32);
        for y in y0..y0 + s.cell_px {
            for x in x0..x0 + s.cell_px {
                assert!((x as usize) < aw && (y as usize) < ah, "cell spills guard");
                cover[y as usize * aw + x as usize] += 1;
            }
        }
    }
    assert!(
        cover.iter().all(|&n| n == 1),
        "active area must be tiled exactly once"
    );
}

#[test]
fn no_two_bits_of_one_word_are_neighbours() {
    // The burst-robustness property: 4-neighbours and diagonals of a data cell
    // must belong to a different Golay word.
    let s = TileSpec::default();
    let cols = s.cols as i32;
    let rows = s.rows as i32;
    let word_at = |col: i32, row: i32| -> Option<u8> {
        if col < 0 || row < 0 || col >= cols || row >= rows {
            return None;
        }
        match s.cells()[(row * cols + col) as usize].kind {
            CellKind::Data { word, .. } => Some(word),
            CellKind::Ref { .. } => None,
        }
    };
    let neigh = [
        (1, 0),
        (-1, 0),
        (0, 1),
        (0, -1),
        (1, 1),
        (1, -1),
        (-1, 1),
        (-1, -1),
    ];
    for row in 0..rows {
        for col in 0..cols {
            if let Some(w) = word_at(col, row) {
                for (dx, dy) in neigh {
                    if let Some(wn) = word_at(col + dx, row + dy) {
                        assert_ne!(
                            w,
                            wn,
                            "word {w} appears at adjacent cells ({col},{row}) and ({},{})",
                            col + dx,
                            row + dy
                        );
                    }
                }
            }
        }
    }
}

#[test]
fn worst_case_offset_exposes_exactly_one_tile() {
    let s = TileSpec::default();
    let (w, h) = (s.window_w(), s.window_h()); // 464×432

    // Phase 0 on both axes: the grid is aligned, so a 2×tile window holds 2×2.
    assert_eq!(s.complete_tile_origins(w, h, 0.0, 0.0).len(), 4);

    // Worst case — half a period offset on each axis — yields exactly one tile.
    let pworst_x = s.tile_w as f32 / 2.0;
    let pworst_y = s.tile_h as f32 / 2.0;
    assert_eq!(s.complete_tile_origins(w, h, pworst_x, pworst_y).len(), 1);

    // Any nonzero phase on both axes gives a single complete tile at this window
    // size; alignment on exactly one axis gives two.
    assert_eq!(s.complete_tile_origins(w, h, 1.0, 1.0).len(), 1);
    assert_eq!(s.complete_tile_origins(w, h, 0.0, 5.0).len(), 2);
    assert_eq!(s.complete_tile_origins(w, h, 5.0, 0.0).len(), 2);
}
