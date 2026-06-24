//! Stage 3 — tile geometry, the cell interleaver, and reference-cell placement.
//!
//! A tile is a `cols × rows` grid of `cell_px`-square cells plus an 8-px guard
//! gutter, repeated periodically across the frame. Of the 182 grid cells, 168
//! carry Golay-coded data bits and 14 are reference cells at known luma levels
//! for per-crop calibration.
//!
//! ## Interleaver
//!
//! Each cell is assigned a **word class** `(col + 3·row) mod 7`. Because the grid
//! is 14 = 2×7 columns wide, every class holds exactly 26 cells (2 per row × 13
//! rows). Reserving **2 reference cells per class** leaves exactly **24 data
//! cells per class = one Golay word**. The `+3·row` term means 4-neighbours
//! (Δ=±1 horizontally, ±3 vertically) and diagonals (Δ=±2,±4) are all in
//! *different* classes — so a localized codec burst damages a few bits across
//! many words, never several bits of one word (`EXPLANATION.md` §Guardrails 5).

/// Reference-cell luma levels (sRGB-gamma, 0..255), kept clear of 0/255 clipping.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum RefLevel {
    Black,
    Mid,
    White,
}

impl RefLevel {
    /// Nominal luma value this reference level renders to.
    pub fn luma(self) -> f32 {
        match self {
            RefLevel::Black => 16.0,
            RefLevel::Mid => 128.0,
            RefLevel::White => 240.0,
        }
    }
}

/// What a grid cell carries.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum CellKind {
    /// One coded bit: `bit` (0..24) of Golay `word` (0..7).
    Data { word: u8, bit: u8 },
    /// A calibration reference held at a fixed level.
    Ref { level: RefLevel },
}

/// A grid cell at `(col, row)` within a tile, with its role.
#[derive(Clone, Copy, Debug)]
pub struct Cell {
    pub col: u16,
    pub row: u16,
    pub kind: CellKind,
}

/// Tile geometry + the fixed cell assignment. Construct via [`TileSpec::new`] or
/// [`TileSpec::default`].
#[derive(Clone, Debug)]
pub struct TileSpec {
    pub cell_px: u32,
    pub inner_px: u32,
    pub cols: u32,
    pub rows: u32,
    pub guard: u32,
    pub tile_w: u32,
    pub tile_h: u32,
    /// Luma delta applied to data cells (±delta on the inner block).
    pub delta: f32,
    cells: Vec<Cell>,
}

/// Default luma delta (0..255 scale) — subtle but codec-survivable.
pub const DEFAULT_DELTA: f32 = 8.0;

impl Default for TileSpec {
    fn default() -> Self {
        TileSpec::new(DEFAULT_DELTA)
    }
}

impl TileSpec {
    /// Build the spec'd 14×13 grid (cell 16, inner 8, guard 8 → tile 232×216)
    /// with the given luma delta.
    pub fn new(delta: f32) -> Self {
        const COLS: u32 = 14;
        const ROWS: u32 = 13;
        const CELL: u32 = 16;
        const INNER: u32 = 8;
        const GUARD: u32 = 8;

        let cells = build_cells(COLS, ROWS);
        TileSpec {
            cell_px: CELL,
            inner_px: INNER,
            cols: COLS,
            rows: ROWS,
            guard: GUARD,
            tile_w: COLS * CELL + GUARD, // 232
            tile_h: ROWS * CELL + GUARD, // 216
            delta,
            cells,
        }
    }

    /// All 182 cells in raster (row-major) order.
    pub fn cells(&self) -> &[Cell] {
        &self.cells
    }

    /// Active (modulated) area width: `cols × cell_px` (224). The guard gutter
    /// sits to the right of and below this.
    pub fn active_w(&self) -> u32 {
        self.cols * self.cell_px
    }

    /// Active area height: `rows × cell_px` (208).
    pub fn active_h(&self) -> u32 {
        self.rows * self.cell_px
    }

    /// The guaranteed-recovery window size: `2 × tile` (464×432).
    pub fn window_w(&self) -> u32 {
        2 * self.tile_w
    }
    pub fn window_h(&self) -> u32 {
        2 * self.tile_h
    }

    /// Top-left pixel of cell `(col,row)` within a tile.
    pub fn cell_origin(&self, col: u32, row: u32) -> (u32, u32) {
        (col * self.cell_px, row * self.cell_px)
    }

    /// Inner block rectangle `(x0, y0, size)` of cell `(col,row)` within a tile —
    /// the `inner_px`-square centre that is modulated and sampled.
    pub fn inner_rect(&self, col: u32, row: u32) -> (u32, u32, u32) {
        let inset = (self.cell_px - self.inner_px) / 2; // 4
        (
            col * self.cell_px + inset,
            row * self.cell_px + inset,
            self.inner_px,
        )
    }

    /// Whether a window of `window_w × window_h` is guaranteed to fully contain a
    /// tile at *every* offset. True iff `W ≥ 2·tile_w` and `H ≥ 2·tile_h`.
    pub fn guaranteed_containment(&self, window_w: u32, window_h: u32) -> bool {
        window_w >= 2 * self.tile_w && window_h >= 2 * self.tile_h
    }

    /// Pixel top-left origins of every tile fully contained in a `win_w × win_h`
    /// region, given the tile-grid `phase` (position of the first tile boundary,
    /// in pixels, within the region) on each axis.
    pub fn complete_tile_origins(
        &self,
        win_w: u32,
        win_h: u32,
        phase_x: f32,
        phase_y: f32,
    ) -> Vec<(u32, u32)> {
        let xs = axis_origins(win_w, self.tile_w, phase_x);
        let ys = axis_origins(win_h, self.tile_h, phase_y);
        let mut out = Vec::with_capacity(xs.len() * ys.len());
        for &y in &ys {
            for &x in &xs {
                out.push((x, y));
            }
        }
        out
    }
}

/// Tile-origin x (or y) coordinates fully inside `[0, win)` for period `period`
/// and phase `phase ∈ [0, period)`.
fn axis_origins(win: u32, period: u32, phase: f32) -> Vec<u32> {
    let p = (phase as f64).rem_euclid(period as f64);
    let mut out = Vec::new();
    let mut x = p;
    let win_f = win as f64;
    let per = period as f64;
    while x + per <= win_f + 0.5 {
        // The +0.5 above tolerates float drift at the W=2w boundary; this integer
        // check is the real containment contract. Rounding the origin first means
        // a fractional phase can't admit a tile that overhangs the window edge.
        let o = x.round() as u32;
        if o + period <= win {
            out.push(o);
        }
        x += per;
    }
    out
}

/// Assign every cell of a `cols × rows` grid to a word class `(col+3row) mod 7`,
/// reserve 2 reference cells per class, and number the remaining 24 data cells.
fn build_cells(cols: u32, rows: u32) -> Vec<Cell> {
    let n = (cols * rows) as usize;
    let mut kinds: Vec<Option<CellKind>> = vec![None; n];
    let mut global_ref: u32 = 0;

    for class in 0..7u32 {
        // This class's members, in raster order.
        let members: Vec<(u32, u32)> = (0..rows)
            .flat_map(|row| (0..cols).map(move |col| (col, row)))
            .filter(|&(col, row)| (col + 3 * row) % 7 == class)
            .collect();
        assert_eq!(members.len(), 26, "class {class} must hold 26 cells");

        // Two reference cells per class, spread ~1/3 and ~2/3 down the member
        // list (which is itself spread diagonally across the tile).
        let ref_local = [members.len() / 3, (2 * members.len()) / 3]; // [8, 17]
        let mut data_bit: u8 = 0;
        for (local, &(col, row)) in members.iter().enumerate() {
            let raster = (row * cols + col) as usize;
            if ref_local.contains(&local) {
                let level = match global_ref % 3 {
                    0 => RefLevel::Black,
                    1 => RefLevel::Mid,
                    _ => RefLevel::White,
                };
                global_ref += 1;
                kinds[raster] = Some(CellKind::Ref { level });
            } else {
                kinds[raster] = Some(CellKind::Data {
                    word: class as u8,
                    bit: data_bit,
                });
                data_bit += 1;
            }
        }
        assert_eq!(data_bit, 24, "class {class} must yield 24 data cells");
    }

    (0..rows)
        .flat_map(|row| (0..cols).map(move |col| (col, row)))
        .map(|(col, row)| Cell {
            col: col as u16,
            row: row as u16,
            kind: kinds[(row * cols + col) as usize].expect("every cell assigned"),
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn geometry_constants() {
        let s = TileSpec::default();
        assert_eq!((s.tile_w, s.tile_h), (232, 216));
        assert_eq!((s.active_w(), s.active_h()), (224, 208));
        assert_eq!((s.window_w(), s.window_h()), (464, 432));
        assert_eq!(s.tile_w - s.active_w(), s.guard);
        assert_eq!(s.cells().len(), 182);
    }

    #[test]
    fn interleaver_balance_and_levels() {
        let s = TileSpec::default();
        let mut data = 0;
        let mut per_word = [0u32; 7];
        let mut per_level = [0u32; 3];
        for c in s.cells() {
            match c.kind {
                CellKind::Data { word, bit } => {
                    data += 1;
                    per_word[word as usize] += 1;
                    assert!(bit < 24);
                }
                CellKind::Ref { level } => {
                    per_level[match level {
                        RefLevel::Black => 0,
                        RefLevel::Mid => 1,
                        RefLevel::White => 2,
                    }] += 1;
                }
            }
        }
        assert_eq!(data, 168);
        assert_eq!(per_word, [24; 7], "each Golay word needs exactly 24 cells");
        assert!(per_level.iter().all(|&n| n >= 1), "all 3 levels present");
        assert_eq!(per_level.iter().sum::<u32>(), 14);
    }

    #[test]
    fn each_word_uses_every_bit_once() {
        let s = TileSpec::default();
        let mut seen = [[false; 24]; 7];
        for c in s.cells() {
            if let CellKind::Data { word, bit } = c.kind {
                assert!(!seen[word as usize][bit as usize], "dup (word,bit)");
                seen[word as usize][bit as usize] = true;
            }
        }
        assert!(
            seen.iter().all(|w| w.iter().all(|&b| b)),
            "all bits covered"
        );
    }

    #[test]
    fn fractional_phase_never_overhangs() {
        // Every enumerated origin must yield a tile fully inside the window, even
        // for sub-pixel phases (complete_tile_origins is public and takes f32).
        let s = TileSpec::default();
        for &win in &[
            (s.tile_w, s.tile_h),
            (s.window_w(), s.window_h()),
            (700, 650),
        ] {
            for &px in &[0.0f32, 0.5, 1.0, 115.9, 116.0, 116.5, 231.5] {
                for &py in &[0.0f32, 0.5, 107.5, 215.5] {
                    for (ox, oy) in s.complete_tile_origins(win.0, win.1, px, py) {
                        assert!(
                            ox + s.tile_w <= win.0,
                            "x overhang: {ox}+{} > {}",
                            s.tile_w,
                            win.0
                        );
                        assert!(
                            oy + s.tile_h <= win.1,
                            "y overhang: {oy}+{} > {}",
                            s.tile_h,
                            win.1
                        );
                    }
                }
            }
        }
    }
}
