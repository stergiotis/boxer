//! Sixel graphics (DEC STD 070, the VT340's; xterm's ctlseqs, "Sixel
//! Graphics"): the data of a `DCS P1 ; P2 ; P3 q … ST`, decoded as it
//! comes into a picture of RGBA pixels, one kept for each position of the
//! sixel grid, and shown as square ones at the grid's aspect. Pixels no
//! sixel paints stay clear, or take the background, as P2 says.

use std::io::Write;
use std::sync::Arc;

/// The most pixels across or down a picture may show, and keep: a 4K
/// screen's.
pub(crate) const MAX_SIDE: u32 = 4096;
/// The most pixels a picture may keep in all, one a position of its grid,
/// which bounds its memory.
pub(crate) const MAX_PIXELS: usize = 4096 * 2160;
/// The pixel aspect ratio, tall to wide in hundredths, each macro parameter
/// P1 selects (DEC STD 070, 7.1).
const MACRO_ASPECT: [u32; 10] = [200, 200, 450, 300, 250, 200, 150, 130, 112, 100];

/// A picture a program drew: its size as shown, in square pixels, and the
/// pixels of its grid, RGBA, row by row from the top, four bytes each
/// (red, green, blue and alpha, 0 clear), each shown over as many square
/// pixels as the grid's aspect makes it. Only what was drawn is kept: the
/// rest of its extent reads as its background, or clear; a pixel a newer
/// picture covers reads clear.
#[derive(Clone, Debug)]
pub struct Picture {
    width: u32,
    height: u32,
    /// The columns and rows of the grid drawn, a pixel kept each.
    across: u32,
    down: u32,
    /// A grid position's height to its width.
    aspect: (u32, u32),
    /// What a pixel no sixel set reads as, if not clear.
    background: Option<[u8; 4]>,
    /// The square pixel row the decoder's last sixel band ends on: where
    /// its vertical motion left it (DEC STD 070-9, 11.3), past the picture
    /// if a graphics new line came last.
    ended: u32,
    /// Shared with the picture it was before newer ones covered it.
    rgba: Arc<Vec<u8>>,
    /// The pixels newer pictures cover, once one does.
    covered: Option<Covered>,
}

impl Picture {
    /// The square pixel row its last sixel band ends on: the vertical
    /// motion its data made, down from its top; a picture made whole, its
    /// last row.
    pub(crate) fn ended(&self) -> u32 {
        self.ended
    }
    /// Its width in pixels.
    pub fn width(&self) -> u32 {
        self.width
    }
    /// Its height in pixels.
    pub fn height(&self) -> u32 {
        self.height
    }
    /// The bytes its pixels take, and those of what covers them.
    pub(crate) fn bytes(&self) -> usize {
        self.rgba.capacity().saturating_add(self.covered_bytes())
    }
    /// The RGBA of the pixel at `x`, `y`, if it is in the picture.
    pub(crate) fn pixel(&self, x: u32, y: u32) -> Option<[u8; 4]> {
        if x >= self.width || y >= self.height {
            return None;
        }
        if self.covered.as_ref().is_some_and(|c| c.has(x, y)) {
            return Some([0; 4]);
        }
        let (x, y) = grid_of(self.aspect, x, y);
        let drawn = (x < self.across && y < self.down)
            .then(|| offset(x, y, self.across))
            .flatten()
            .and_then(|at| self.rgba.get(at..at.checked_add(4)?))
            .and_then(|bytes| <[u8; 4]>::try_from(bytes).ok())
            .filter(|pixel| pixel[3] != 0);
        Some(drawn.or(self.background).unwrap_or([0; 4]))
    }
}

impl Picture {
    /// The pixels of row `y` left of `right` that it draws opaque, as
    /// bits, the first pixel the lowest bit of the first word: all of its
    /// extent with a background (P2 0 or 2), else those drawn, read a grid
    /// position at a time. What newer pictures cover of it is not read:
    /// `Screen::cover` reads the newest, which nothing covers.
    pub(crate) fn opaque_row(&self, y: u32, right: u32, bits: &mut Vec<u64>) {
        let right = right.min(self.width);
        bits.clear();
        bits.resize(usize::try_from(right.div_ceil(64)).unwrap_or(0), 0);
        if y >= self.height {
            return;
        }
        if self.background.is_some() {
            return set_bits(bits, 0, right);
        }
        let (_, row) = grid_of(self.aspect, 0, y);
        if row >= self.down {
            return;
        }
        // A grid position read each, which a few bytes may ask for again
        // on each placement (B46).
        crate::budget::spend(self.across);
        for x in (0..self.across).take_while(|x| shown_at(self.aspect, *x, 0).0 < right) {
            let alpha =
                offset(x, row, self.across).and_then(|at| self.rgba.get(at.checked_add(3)?));
            if alpha.is_some_and(|a| *a != 0) {
                let (start, _) = shown_at(self.aspect, x, 0);
                let (end, _) = shown_at(self.aspect, x.saturating_add(1), 0);
                set_bits(bits, start, end.min(right));
            }
        }
    }

    /// The grid row its square pixel row `y` shows: rows showing the same
    /// one have the same pixels.
    pub(crate) fn grid_row(&self, y: u32) -> u32 {
        grid_of(self.aspect, 0, y).1
    }

    /// The bytes it takes for newer pictures to cover `rows` of its rows,
    /// of it the screen showing `shown` (`coverable`): a list of the rows
    /// shown, if none is covered yet, which `coverable` makes, and the
    /// words of each row covered, made as they are covered.
    pub(crate) fn cover_bytes(
        &self,
        (right, top, bottom): (u32, u32, u32),
        rows: usize,
    ) -> (usize, usize) {
        // Rows are made as wide as the first cover found it shown.
        let (list, stride) = match &self.covered {
            Some(covered) => (0, covered.stride()),
            None => (
                usize::try_from(bottom.min(self.height).saturating_sub(top)).unwrap_or(usize::MAX),
                usize::try_from(right.min(self.width).div_ceil(64)).unwrap_or(usize::MAX),
            ),
        };
        (
            list.saturating_mul(Covered::ROW),
            rows.saturating_mul(stride).saturating_mul(8),
        )
    }

    /// The bytes what newer pictures cover of it takes.
    pub(crate) fn covered_bytes(&self) -> usize {
        self.covered.as_ref().map_or(0, Covered::bytes)
    }

    /// Makes it ready for newer pictures to cover, if none covers it yet:
    /// of its pixels, those left of `right` on rows `top` to `bottom` are
    /// what the screen shows, which they may cover; the others never show
    /// again. False if there is no memory for it.
    pub(crate) fn coverable(&mut self, (right, top, bottom): (u32, u32, u32)) -> bool {
        if self.covered.is_none() {
            let rows = bottom.min(self.height).saturating_sub(top);
            self.covered = Covered::new(right.min(self.width), top, rows);
        }
        self.covered.is_some()
    }

    /// Covers its pixels on row `y` from column `x` (either before its
    /// own: a newer picture's that overlaps it) where `bits` are set, as
    /// `opaque_row` gives them, the words past its sides left unread;
    /// nothing unless `coverable`.
    pub(crate) fn cover_row(&mut self, y: i64, x: i64, bits: &[u64]) {
        let (Some(covered), Ok(y)) = (self.covered.as_mut(), u32::try_from(y)) else {
            return;
        };
        // The first word that reaches its first pixel, and the first past
        // its shown pixels.
        let first = u64::try_from(x.saturating_neg()).map_or(0, |back| back / 64);
        let end = (i64::from(covered.right).saturating_sub(x)).max(0);
        let end = u64::try_from(end).map_or(0, |end| end.div_ceil(64));
        let (first, end) = (
            usize::try_from(first).unwrap_or(usize::MAX),
            usize::try_from(end).unwrap_or(0),
        );
        let words = bits.get(first..end.min(bits.len())).unwrap_or_default();
        for (at, word) in (i64::try_from(first).unwrap_or(i64::MAX)..).zip(words) {
            covered.put(y, x.saturating_add(at.saturating_mul(64)), *word);
        }
    }

    /// Whether newer pictures cover all it showed when one first did.
    pub(crate) fn covered_whole(&self) -> bool {
        self.covered.as_ref().is_some_and(|c| c.left == 0)
    }
}

/// Sets bits `start` to `end` of `bits`, the first the lowest of the
/// first word, a word at a time; those past its words dropped.
fn set_bits(bits: &mut [u64], start: u32, end: u32) {
    let mut at = start;
    while at < end {
        let (word, bit) = (at / 64, at % 64);
        let n = end.saturating_sub(at).min(64_u32.saturating_sub(bit));
        let run = u64::MAX.checked_shr(64_u32.saturating_sub(n)).unwrap_or(0) << bit;
        if let Some(slot) = usize::try_from(word).ok().and_then(|w| bits.get_mut(w)) {
            *slot |= run;
        }
        at = at.saturating_add(n.max(1));
    }
}

/// The pixels of a picture that newer ones cover, a bit each, of those
/// the screen showed when the first did: left of `right` on the rows from
/// `top`, each row's words, once one of its pixels is covered, holding its
/// pixels from the left, lowest bit first; and how many of those are left
/// uncovered.
#[derive(Clone, Debug)]
struct Covered {
    rows: Vec<Option<Box<[u64]>>>,
    right: u32,
    top: u32,
    left: u64,
    /// The words of its rows made, so that its bytes are read at once,
    /// however many rows it has.
    words: usize,
}

impl Covered {
    /// The bytes it takes a row, before any of its pixels is covered.
    const ROW: usize = size_of::<Option<Box<[u64]>>>();

    /// None of `right` pixels by `rows` rows from row `top` covered; none
    /// if there is no memory for it.
    fn new(right: u32, top: u32, rows: u32) -> Option<Self> {
        let n = usize::try_from(rows).ok()?;
        let mut list = Vec::new();
        list.try_reserve_exact(n).ok()?;
        list.resize(n, None);
        let left = u64::from(right).saturating_mul(u64::from(rows));
        Some(Self {
            rows: list,
            right,
            top,
            left,
            words: 0,
        })
    }

    /// The words of a row once one of its pixels is covered.
    fn stride(&self) -> usize {
        usize::try_from(self.right.div_ceil(64)).unwrap_or(0)
    }

    /// Row `y`'s place in `rows`.
    fn row(&self, y: u32) -> Option<usize> {
        usize::try_from(y.checked_sub(self.top)?).ok()
    }

    /// Whether pixel `x`, `y` is covered.
    fn has(&self, x: u32, y: u32) -> bool {
        let row = self.row(y).and_then(|y| self.rows.get(y)?.as_ref());
        let word = row
            .zip(usize::try_from(x / 64).ok())
            .and_then(|(row, at)| row.get(at));
        word.is_some_and(|word| word >> (x % 64) & 1 == 1)
    }

    /// Covers row `y`'s pixels from `x` on where `bits` are set, those
    /// outside what was shown dropped.
    fn put(&mut self, y: u32, x: i64, bits: u64) {
        let (start, bits) = match u32::try_from(x) {
            Ok(start) => (start, bits),
            Err(_) if x <= -64 => return,
            Err(_) => (0, bits >> x.unsigned_abs()),
        };
        let (right, stride) = (self.right, self.stride());
        let row = (self.row(y))
            .filter(|_| start < right && bits != 0)
            .and_then(|y| self.rows.get_mut(y));
        let Some(row) = row else {
            return;
        };
        let row = match row {
            Some(row) => row,
            None => {
                self.words = self.words.saturating_add(stride);
                row.insert(vec![0; stride].into_boxed_slice())
            }
        };
        let shift = start % 64;
        let carried = bits
            .checked_shr(64_u32.saturating_sub(shift))
            .filter(|_| shift > 0);
        let first = start.saturating_sub(shift);
        for (x, word) in [
            (first, bits << shift),
            (first.saturating_add(64), carried.unwrap_or(0)),
        ] {
            // The bits of this word's pixels left of `right`.
            let shown = match right.saturating_sub(x) {
                n if n >= 64 => !0,
                n => !(!0_u64 << n),
            };
            if let Some(slot) = usize::try_from(x / 64).ok().and_then(|at| row.get_mut(at)) {
                let new = word & shown & !*slot;
                *slot |= new;
                self.left = self.left.saturating_sub(u64::from(new.count_ones()));
            }
        }
    }

    /// The bytes it takes.
    fn bytes(&self) -> usize {
        (self.rows.capacity().saturating_mul(Self::ROW))
            .saturating_add(self.words.saturating_mul(8))
    }
}

/// Two pictures are alike when they show alike: the same size, and the
/// same pixels.
impl PartialEq for Picture {
    fn eq(&self, other: &Self) -> bool {
        (self.width, self.height) == (other.width, other.height)
            && (0..self.height)
                .all(|y| (0..self.width).all(|x| self.pixel(x, y) == other.pixel(x, y)))
    }
}

impl Eq for Picture {}

/// Where grid position `x`, `y` shows in square pixels, at `aspect`: a
/// position taller than wide takes as many rows, one wider as many columns.
fn shown_at((tall, wide): (u32, u32), x: u32, y: u32) -> (u32, u32) {
    let by = |n: u32, times: u32, over: u32| {
        let n = u64::from(n).saturating_mul(u64::from(times));
        let n = n.checked_div(u64::from(over)).unwrap_or(0);
        u32::try_from(n).unwrap_or(u32::MAX)
    };
    if tall >= wide {
        (x, by(y, tall, wide))
    } else {
        (by(x, wide, tall), y)
    }
}

/// The grid position square pixel `x`, `y` shows, at `aspect`: the last
/// whose square pixels begin at or before it.
fn grid_of((tall, wide): (u32, u32), x: u32, y: u32) -> (u32, u32) {
    // The last g with g × over ÷ by at or before n: ⌈(n + 1) × by ÷ over⌉ − 1.
    let back = |n: u32, by: u32, over: u32| {
        let n = u64::from(n).saturating_add(1).saturating_mul(u64::from(by));
        let g = n.div_ceil(u64::from(over.max(1))).saturating_sub(1);
        u32::try_from(g).unwrap_or(u32::MAX)
    };
    if tall >= wide {
        (x, back(y, wide, tall))
    } else {
        (back(x, tall, wide), y)
    }
}

/// The colour cube a picture of more than 256 colours is drawn in: 6
/// reds, 7 greens and 6 blues, numbered red first, then green.
const CUBE: (usize, usize, usize) = (6, 7, 6);

/// Where the pixel at `x`, `y` of a picture `width` wide starts in its RGBA
/// bytes.
fn offset(x: u32, y: u32, width: u32) -> Option<usize> {
    let at = u64::from(y).checked_mul(u64::from(width))?;
    usize::try_from(at.checked_add(u64::from(x))?.checked_mul(4)?).ok()
}

/// A side's room as a picture grows to `needed`: `now` if that holds it,
/// else twice `now`, or `needed` if more, but never past `most`
/// (`THREATS.md` B03).
fn next_side(now: u32, needed: u32, most: u32) -> u32 {
    if needed <= now {
        return now;
    }
    let doubled = now.saturating_mul(2).max(needed);
    if doubled > most { most } else { doubled }
}

/// The largest picture the decoder keeps whole within a window of `width`
/// by `height` pixels: its sides at most [`MAX_SIDE`], its height within
/// [`MAX_PIXELS`] beside that width. What XTSMGRAPHICS reports (ctlseqs:
/// "the minimum of the window size and the graphic size").
pub(crate) fn largest(width: u32, height: u32) -> (u32, u32) {
    let width = width.min(MAX_SIDE);
    (width, height.min(most_beside(width)))
}

/// The most rows (or columns) a picture may keep beside `other` columns
/// (or rows): within [`MAX_PIXELS`] and [`MAX_SIDE`].
fn most_beside(other: u32) -> u32 {
    let most = MAX_PIXELS
        .checked_div(usize::try_from(other.max(1)).unwrap_or(usize::MAX))
        .unwrap_or(0);
    u32::try_from(most).unwrap_or(u32::MAX).min(MAX_SIDE)
}

/// Where the decoder is in its data: a run of digits after `!`, `#` or
/// `"`, or between commands.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Command {
    None,
    /// `! Pn ch`: the next sixel `Pn` times.
    Repeat,
    /// `# Pc` selects a colour; `# Pc ; Pu ; Px ; Py ; Pz` defines it.
    Colour,
    /// `" Pan ; Pad ; Ph ; Pv`: the picture's size, which it may draw past.
    Raster,
}

/// Sixel data, decoded as it comes.
#[derive(Clone, Debug)]
pub(crate) struct Decoder {
    /// The colour registers: the VT340's sixteen to begin with.
    palette: [[u8; 3]; 256],
    colour: usize,
    command: Command,
    /// The numbers of the command being read, at most five.
    numbers: [u32; 5],
    count: usize,
    /// Where the next sixel goes, in grid positions: its column, and the
    /// top of its band.
    x: u32,
    y: u32,
    /// The grid's size drawn so far, and the room its pixels have: `room`
    /// columns by rows, `pixels` holding them, `room.0` across.
    across: u32,
    down: u32,
    room: (u32, u32),
    pixels: Vec<u8>,
    /// The raster attributes' extent, in grid positions: the least size,
    /// which takes no pixels of its own.
    extent: (u32, u32),
    /// A grid position's height to its width.
    aspect: (u32, u32),
    /// Pixels no sixel sets take the background (P2 0 or 2), else stay clear.
    background: bool,
    /// A sixel or a positioning command came: raster attributes are ignored.
    begun: bool,
    /// Some of what it drew was not kept: past what a picture may keep, or
    /// refused room.
    cut: bool,
    /// Whether its pixels may be given room: one no host draws is read
    /// only to tell whether it drew anything, and keeps none.
    keeps: bool,
}

impl Decoder {
    /// A decoder for the data of `DCS P1 ; P2 ; P3 q`. P3, the grid size,
    /// gets the device's grid that fits it best (DEC STD 070, 7.3 and its
    /// terminology): a screen has one, its pixel, so P3 changes nothing.
    /// It `keeps` its pixels, or reads them only (`Self::keeps`).
    pub(crate) fn new(p1: u16, p2: u16, keeps: bool) -> Box<Self> {
        let tall = MACRO_ASPECT.get(usize::from(p1)).copied().unwrap_or(200);
        Box::new(Self {
            palette: vt340_palette(),
            // "Sixel color: Contrasting to background" (DEC STD 070-9, 10,
            // the initial states), the background being register 0: xterm
            // 411 starts at register 3 (`graphics_sixel.c`), the VT340's
            // green, and so does this.
            colour: 3,
            command: Command::None,
            numbers: [0; 5],
            count: 0,
            x: 0,
            y: 0,
            across: 0,
            down: 0,
            room: (0, 0),
            pixels: Vec::new(),
            extent: (0, 0),
            aspect: (tall, 100),
            background: p2 != 1,
            begun: false,
            cut: false,
            keeps,
        })
    }

    /// Whether it keeps its pixels: else its picture is not placed.
    pub(crate) fn keeps(&self) -> bool {
        self.keeps
    }

    /// Reads one byte of the data. Format effectors, SPACE and DELETE are
    /// ignored in it (DEC STD 070, 8.1 and its coding section): one inside a
    /// command does not end it. Before the picture's pixels grow, `room`
    /// is asked for the bytes they then hold at once, the old and the new
    /// (`THREATS.md` B01), and whether it may let pictures go for them:
    /// false keeps them as they are.
    pub(crate) fn push(&mut self, byte: u8, room: &mut impl FnMut(usize, bool) -> bool) {
        // C0 controls, space and DEL change nothing: 0x08 to 0x0d have "no
        // effect", and the rest "are ignored" (DEC STD 070, sixel graphics,
        // 9.1), BEL ringing as in text (the parser's) and CAN, SUB and ESC
        // the parser's too.
        // GR codes are errors read as GL, "the 8th bit ... set to zero" (DEC
        // STD 070-9, 9.4), a byte at a time, a UTF-8 character's bytes too
        // (as xterm 411 reads it; README, 'Sixel data is read a byte at a
        // time'). C1 codes, which there end sixel mode (9.3), are no
        // controls in UTF-8, as fux-vt reads its input, and are ignored
        // as C0 controls are (9.1); so are SPACE and DELETE, in GL or GR.
        let byte = match byte {
            0x21..=0x7e => byte,
            0xa1..=0xfe => byte & 0x7f,
            _ => return,
        };
        match byte {
            b'0'..=b'9' if self.command != Command::None => {
                let digit = u32::from(byte.wrapping_sub(b'0'));
                if let Some(n) = self.numbers.get_mut(self.count) {
                    *n = n.saturating_mul(10).saturating_add(digit).min(1 << 20);
                }
                return;
            }
            b';' if self.command != Command::None => {
                self.count = self.count.saturating_add(1);
                return;
            }
            _ => {}
        }
        let repeat = self.end_command();
        match byte {
            b'!' => self.begin(Command::Repeat),
            b'#' => self.begin(Command::Colour),
            b'"' => self.begin(Command::Raster),
            b'$' => self.x = 0,
            b'-' => {
                self.x = 0;
                self.y = self.y.saturating_add(6);
            }
            0x3f..=0x7e => self.sixel(byte.wrapping_sub(0x3f), repeat.unwrap_or(1).max(1), room),
            _ => {}
        }
        self.begun |= matches!(byte, b'$' | b'-' | 0x3f..=0x7e);
    }

    fn begin(&mut self, command: Command) {
        self.command = command;
        self.numbers = [0; 5];
        self.count = 0;
    }

    /// Carries out the command whose numbers were read: a repeat's count is
    /// for the sixel that follows.
    fn end_command(&mut self) -> Option<u32> {
        let command = std::mem::replace(&mut self.command, Command::None);
        let [a, b, c, d, e] = self.numbers;
        match command {
            Command::None => None,
            Command::Raster => {
                // Omitted aspect numbers are 1; the extent, in grid
                // positions, is the least size, not a clip, and "does not
                // limit the number of pixels a device may image" (DEC STD
                // 070, 8.1.2): past what a picture may keep, it is cut to
                // that as it ends. It is kept as numbers: what no sixel
                // draws is background, read when shown.
                if !self.begun {
                    self.aspect = (a.max(1), b.max(1));
                    self.extent = (c, d);
                }
                None
            }
            // What it paints past a picture's width is cut as it is
            // painted (`sixel`): a blank one cuts nothing.
            Command::Repeat => Some(a),
            Command::Colour => {
                // A definition in no coordinate system DEC STD 070 (8.1.3)
                // lists, or past its ranges, is ignored whole, the colour's
                // selection too; coordinates left out are 0.
                let rgb = match (self.count, b) {
                    (0, _) => None,
                    (_, 1) if c <= 360 && d <= 100 && e <= 100 => Some(hls(c, d, e)),
                    (_, 2) if c <= 100 && d <= 100 && e <= 100 => {
                        Some([percent(c), percent(d), percent(e)])
                    }
                    _ => return None,
                };
                self.colour = usize::try_from(a).unwrap_or(0).min(255);
                if let Some(rgb) = rgb
                    && let Some(slot) = self.palette.get_mut(self.colour)
                {
                    *slot = rgb;
                }
                None
            }
        }
    }

    /// Paints `bits`, a sixel's six pixels from the top, `count` times
    /// across, in the current colour: a run along each row a bit sets, of
    /// the positions the picture may keep (DEC STD 070, 11.2.2: sixels
    /// past its edge "are not printed").
    fn sixel(&mut self, bits: u8, count: u32, room: &mut impl FnMut(usize, bool) -> bool) {
        let right = self.x.saturating_add(count);
        // Down to its lowest pixel painted: the highest bit set.
        let lowest = 6u32.saturating_sub(bits.leading_zeros().saturating_sub(2));
        let (most, bottom) = self.kept(right, self.y.saturating_add(lowest));
        // Some of it is kept: the grid grows for it.
        if bits != 0 && self.x < most && self.y < bottom {
            let (across, down) = self.grow(right, bottom, room);
            let [r, g, b] = self.palette.get(self.colour).copied().unwrap_or([0; 3]);
            let (left, end) = (self.x.min(across), right.min(across));
            let run = usize::try_from(end.saturating_sub(left)).unwrap_or(0);
            for bit in 0..6u32 {
                let y = self.y.saturating_add(bit);
                if bits & (1 << bit) == 0 || y >= down {
                    continue;
                }
                crate::budget::spend(run);
                let at = offset(left, y, self.room.0).unwrap_or(usize::MAX);
                let span = at.saturating_add(run.saturating_mul(4));
                if let Some(row) = self.pixels.get_mut(at..span) {
                    row.as_chunks_mut::<4>().0.fill([r, g, b, 255]);
                }
            }
        }
        // A pixel it sets past the grid kept is not painted.
        let below = self.y.saturating_add(lowest);
        self.cut |= bits != 0 && (self.across < right || self.down < below);
        self.x = right.min(MAX_SIDE);
    }

    /// Grows the grid to `right` columns and `bottom` rows, cut to what a
    /// picture may keep, and makes room for its pixels, keeping those
    /// drawn: each side doubling, within its limits (`THREATS.md` B03).
    /// It stays as it is when `room` hasn't the bytes for the old pixels
    /// and the new at once, or the memory isn't there. Its size after.
    fn grow(
        &mut self,
        right: u32,
        bottom: u32,
        room: &mut impl FnMut(usize, bool) -> bool,
    ) -> (u32, u32) {
        let (right, bottom) = self.kept(right, bottom);
        // The pixels it has hold the grid.
        if !self.needs_growth(right, bottom) {
            (self.across, self.down) = (right, bottom);
            return (right, bottom);
        }
        let (cols, rows) = (
            next_side(self.room.0, right, MAX_SIDE),
            next_side(self.room.1, bottom, MAX_SIDE),
        );
        // Both doubled may be past the pixels: then each in turn as far as
        // fits beside the other, else neither.
        let tries = [
            (cols, rows),
            (cols, rows.min(most_beside(cols)).max(bottom)),
            (cols.min(most_beside(rows)).max(right), rows),
            (right, bottom),
        ];
        let mut tries = tries.into_iter().filter(|&(c, r)| {
            usize::try_from(u64::from(c).saturating_mul(u64::from(r)))
                .is_ok_and(|pixels| pixels <= MAX_PIXELS)
        });
        // The first that doubles and fits beside the pictures kept, else
        // the first that fits once pictures are let go: none is let go for
        // a try not taken (B01). Grown just to what it needs beside them,
        // a picture drawn a column at a time would copy all it has for
        // each (B03).
        let kept = (tries.clone())
            .filter(|&(c, r)| (c, r) != (right, bottom) || (c, r) == (cols, rows))
            .find(|&(c, r)| self.room_for(c, r, false, room));
        let chosen = kept.or_else(|| tries.find(|&(c, r)| self.room_for(c, r, true, room)));
        let Some((cols, rows)) = chosen else {
            return (self.across, self.down);
        };
        let pixels = usize::try_from(u64::from(cols).saturating_mul(u64::from(rows))).unwrap_or(0);
        let mut grown = Vec::new();
        if grown.try_reserve_exact(pixels.saturating_mul(4)).is_err() {
            return (self.across, self.down);
        }
        grown.resize(pixels.saturating_mul(4), 0);
        // The new pixels zeroed, and the old ones copied into them.
        let (old, new) = (
            usize::try_from(self.across).unwrap_or(0).saturating_mul(4),
            usize::try_from(cols).unwrap_or(0).saturating_mul(4),
        );
        let drawn = usize::try_from(self.down).unwrap_or(0);
        crate::budget::spend(pixels.saturating_add(drawn.saturating_mul(old) / 4));
        let stride = usize::try_from(self.room.0).unwrap_or(0).saturating_mul(4);
        for row in 0..drawn {
            let (from, to) = (row.saturating_mul(stride), row.saturating_mul(new));
            let source = self.pixels.get(from..from.saturating_add(old));
            if let (Some(source), Some(target)) =
                (source, grown.get_mut(to..to.saturating_add(old)))
            {
                crate::copy_from(target, source);
            }
        }
        (self.room, self.pixels) = ((cols, rows), grown);
        (self.across, self.down) = (right, bottom);
        (right, bottom)
    }

    /// Whether a grid of `right` columns and `bottom` rows needs more room
    /// than its pixels have.
    fn needs_growth(&self, right: u32, bottom: u32) -> bool {
        right > self.room.0 || bottom > self.room.1
    }

    /// The grid's size grown to `right` columns and `bottom` rows, as much
    /// of it as a picture may keep: each side, and the pixels in all, the
    /// rows taken first, as a band paints nothing without its rows.
    fn kept(&self, right: u32, bottom: u32) -> (u32, u32) {
        let bottom = bottom.min(most_beside(self.across)).max(self.down);
        (right.min(most_beside(bottom)).max(self.across), bottom)
    }

    /// Whether `room` has the bytes for pixels `cols` by `rows` beside
    /// those held now.
    fn room_for(
        &self,
        cols: u32,
        rows: u32,
        evict: bool,
        room: &mut impl FnMut(usize, bool) -> bool,
    ) -> bool {
        let bytes = u64::from(cols)
            .saturating_mul(u64::from(rows))
            .saturating_mul(4);
        let bytes = usize::try_from(bytes).unwrap_or(usize::MAX);
        self.keeps && room(self.bytes().saturating_add(bytes), evict)
    }

    /// The bytes of pixels drawn so far.
    pub(crate) fn bytes(&self) -> usize {
        self.pixels.capacity()
    }

    /// Lets go of the pixels drawn so far: the picture begins again, cut.
    pub(crate) fn clear(&mut self) {
        self.cut |= self.bytes() > 0;
        (self.across, self.down, self.room, self.pixels) = (0, 0, (0, 0), Vec::new());
    }

    /// The picture drawn, unless it is empty, and whether it is whole: all
    /// it drew kept, its extent and its shown size too. `room` as for
    /// [`Self::push`], for its pixels cut to their size beside those held,
    /// without which it ends with nothing, not whole.
    pub(crate) fn finish(
        mut self: Box<Self>,
        room: &mut impl FnMut(usize, bool) -> bool,
    ) -> (Option<Arc<Picture>>, bool) {
        self.end_command();
        let (cols, rows) = self.kept(self.extent.0, self.extent.1);
        self.cut |= cols < self.extent.0 || rows < self.extent.1;
        if cols == 0 || rows == 0 {
            return (None, !self.cut);
        }
        let rgba = if self.room == (self.across, self.down) {
            std::mem::take(&mut self.pixels)
        } else {
            match self.exact(room) {
                Some(rgba) => rgba,
                None => return (None, false),
            }
        };
        // The background is colour register 0, as it ends (DEC STD 070,
        // 7.2, leaves it to the device).
        let background = self.background.then(|| {
            let [r, g, b] = self.palette.first().copied().unwrap_or([0; 3]);
            [r, g, b, 255]
        });
        let (width, height) = shown_at(self.aspect, cols, rows);
        let whole = !self.cut && width <= MAX_SIDE && height <= MAX_SIDE;
        let picture = Arc::new(Picture {
            width: width.min(MAX_SIDE),
            height: height.min(MAX_SIDE),
            across: self.across,
            down: self.down,
            aspect: self.aspect,
            background,
            ended: shown_at(self.aspect, 0, self.y.saturating_add(6))
                .1
                .saturating_sub(1),
            rgba: Arc::new(rgba),
            covered: None,
        });
        (Some(picture), whole)
    }

    /// The pixels drawn, a copy just their size, if `room` has the bytes.
    fn exact(&self, room: &mut impl FnMut(usize, bool) -> bool) -> Option<Vec<u8>> {
        if !self.room_for(self.across, self.down, true, room) {
            return None;
        }
        let (row, stride) = (
            usize::try_from(self.across).ok()?.checked_mul(4)?,
            usize::try_from(self.room.0).ok()?.checked_mul(4)?,
        );
        let rows = usize::try_from(self.down).ok()?;
        let mut rgba = Vec::new();
        rgba.try_reserve_exact(row.checked_mul(rows)?).ok()?;
        crate::budget::spend(row.saturating_mul(rows) / 4);
        for y in 0..rows {
            let from = y.checked_mul(stride)?;
            rgba.extend_from_slice(self.pixels.get(from..from.checked_add(row)?)?);
        }
        Some(rgba)
    }
}

/// The decoder's part of the invariant checker (THREATS.md section 6).
#[cfg(feature = "checks")]
impl Decoder {
    /// 31: the picture being drawn is no larger than a picture may keep,
    /// within the room its pixels have, exactly that many.
    pub(crate) fn check(&self) -> Result<(), String> {
        let (cols, rows) = self.room;
        let pixels = u64::from(cols).saturating_mul(u64::from(rows));
        crate::check::ensure(
            self.across <= cols
                && self.down <= rows
                && cols <= MAX_SIDE
                && rows <= MAX_SIDE
                && usize::try_from(pixels).is_ok_and(|p| p <= MAX_PIXELS)
                && u64::try_from(self.pixels.len())
                    .is_ok_and(|len| len == pixels.saturating_mul(4)),
            || {
                format!(
                    "a {}x{} picture in room for {cols}x{rows}, of {} bytes, being drawn",
                    self.across,
                    self.down,
                    self.pixels.len()
                )
            },
        )
    }
}

/// A sixel colour's share, 0 to 100, as a byte.
fn percent(n: u32) -> u8 {
    let scaled = n.saturating_mul(255) / 100;
    u8::try_from(scaled).unwrap_or(u8::MAX)
}

/// A colour given as hue (0 to 360, blue at 0), lightness and saturation
/// (0 to 100), as the VT340 reads them, worked in thousandths.
fn hls(hue: u32, lightness: u32, saturation: u32) -> [u8; 3] {
    let (l, s) = (i64::from(lightness), i64::from(saturation));
    // The VT340's hue 0 is blue; the usual wheel's 0 is red, 240° on.
    let h = i64::from(hue % 360).saturating_add(240) % 360;
    // Chroma, and the second-largest channel, both of 0 to 100_000.
    let chroma = 100i64
        .saturating_sub(l.saturating_mul(2).saturating_sub(100).abs())
        .saturating_mul(s)
        .saturating_mul(10);
    let sector = h / 60;
    let within = (h % 120).saturating_mul(1000) / 60;
    let second =
        chroma.saturating_mul(1000i64.saturating_sub(within.saturating_sub(1000).abs())) / 1000;
    let (r, g, b) = match sector {
        0 => (chroma, second, 0),
        1 => (second, chroma, 0),
        2 => (0, chroma, second),
        3 => (0, second, chroma),
        4 => (second, 0, chroma),
        _ => (chroma, 0, second),
    };
    let base = l.saturating_mul(1000).saturating_sub(chroma / 2);
    let byte = |c: i64| {
        let v = c.saturating_add(base).saturating_mul(255) / 100_000;
        u8::try_from(v.clamp(0, 255)).unwrap_or(0)
    };
    [byte(r), byte(g), byte(b)]
}

/// The VT340's sixteen colour registers as it starts, the rest black.
fn vt340_palette() -> [[u8; 3]; 256] {
    const SIXTEEN: [[u32; 3]; 16] = [
        [0, 0, 0],
        [20, 20, 80],
        [80, 13, 13],
        [20, 80, 20],
        [80, 20, 80],
        [20, 80, 80],
        [80, 80, 20],
        [53, 53, 53],
        [26, 26, 26],
        [33, 33, 60],
        [60, 26, 26],
        [33, 60, 33],
        [60, 33, 60],
        [33, 60, 60],
        [60, 60, 33],
        [80, 80, 80],
    ];
    let mut palette = [[0u8; 3]; 256];
    for (slot, [r, g, b]) in palette.iter_mut().zip(SIXTEEN) {
        *slot = [percent(r), percent(g), percent(b)];
    }
    palette
}

/// A picture as sixel data, to show it on a terminal that draws sixel:
/// its pixels from `x`, `y`, `width` by `height` of them, in at most 256
/// colours; clear pixels left alone.
pub fn encode(picture: &Picture, (x, y, width, height): (u32, u32, u32, u32), out: &mut Vec<u8>) {
    let width = width.min(picture.width.saturating_sub(x));
    let height = height.min(picture.height.saturating_sub(y));
    if width == 0 || height == 0 {
        return;
    }
    // Colours as they are if there are few enough, else to the cube.
    let at = |px: u32, py: u32| picture.pixel(x.saturating_add(px), y.saturating_add(py));
    let mut seen = std::collections::BTreeSet::new();
    let mut exact = true;
    // Neighbours often share a colour: the last one seen needs no search.
    let mut last = None;
    'scan: for py in 0..height {
        for px in 0..width {
            if let Some([r, g, b, a]) = at(px, py)
                && a >= 128
                && last.replace([r, g, b]) != Some([r, g, b])
                && seen.insert([r, g, b])
                && seen.len() > 256
            {
                exact = false;
                break 'scan;
            }
        }
    }
    // Read at most once each.
    crate::budget::spend(u64::from(width).saturating_mul(u64::from(height)));
    let (reds, greens, blues) = CUBE;
    let steps = |c: u8, of: usize| usize::from(c).saturating_mul(of) >> 8;
    let cube = |[r, g, b]: [u8; 3]| {
        let (r, g, b) = (steps(r, reds), steps(g, greens), steps(b, blues));
        (r.saturating_mul(greens).saturating_add(g))
            .saturating_mul(blues)
            .saturating_add(b)
    };
    let level = |n: usize, of: usize| {
        let n = n
            .saturating_mul(255)
            .checked_div(of.saturating_sub(1))
            .unwrap_or(0);
        u8::try_from(n).unwrap_or(u8::MAX)
    };
    // In order, for `index` to find a colour by halves.
    let palette: Vec<[u8; 3]> = if exact {
        seen.into_iter().collect()
    } else {
        // In `cube`'s order: blue fastest, then green, then red.
        let mut levels = Vec::new();
        for r in 0..reds {
            for g in 0..greens {
                for b in 0..blues {
                    levels.push([level(r, reds), level(g, greens), level(b, blues)]);
                }
            }
        }
        levels
    };
    let index = |rgb: [u8; 3]| -> usize {
        if exact {
            palette.binary_search(&rgb).unwrap_or(0)
        } else {
            cube(rgb)
        }
    };
    // P2 = 1: clear pixels stay as they are. The raster attributes give
    // the size, square pixels.
    let _ = write!(out, "\x1bP0;1;0q\"1;1;{width};{height}");
    for (i, [r, g, b]) in palette.iter().enumerate() {
        // The nearest percent: rounded down, a colour read back is darker.
        let share = |c: u8| (u32::from(c).saturating_mul(100).saturating_add(127)) / 255;
        let _ = write!(out, "#{i};2;{};{};{}", share(*r), share(*g), share(*b));
    }
    // Each colour's sixels of a band, in one walk over its pixels: for each
    // colour, the columns it has a sixel in and the sixel's bits, in order,
    // so that a colour costs its own sixels, never the band's width.
    let mut sixels: Vec<Vec<(u32, u8)>> = vec![Vec::new(); palette.len()];
    let mut used = Vec::new();
    let mut last = None;
    let mut top = 0;
    while top < height {
        let rows = height.saturating_sub(top).min(6);
        for px in 0..width {
            for bit in 0..rows {
                let Some([r, g, b, a]) = at(px, top.saturating_add(bit)) else {
                    continue;
                };
                let colour = match last {
                    Some((rgb, colour)) if rgb == [r, g, b] => colour,
                    _ => {
                        let colour = index([r, g, b]);
                        last = Some(([r, g, b], colour));
                        colour
                    }
                };
                let Some(columns) = sixels.get_mut(colour).filter(|_| a >= 128) else {
                    continue;
                };
                match columns.last_mut() {
                    Some((column, sixel)) if *column == px => *sixel |= 1 << bit,
                    _ => {
                        if columns.is_empty() {
                            used.push(colour);
                        }
                        columns.push((px, 1 << bit));
                    }
                }
            }
        }
        crate::budget::spend(u64::from(width).saturating_mul(u64::from(rows)));
        // Each colour of the band in turn, from its left, `$` between: its
        // sixels, the blanks between them run together, and the blanks to
        // the band's right.
        used.sort_unstable();
        for (n, &colour) in used.iter().enumerate() {
            let Some(columns) = sixels.get_mut(colour) else {
                continue;
            };
            crate::budget::spend(columns.len());
            if n > 0 {
                out.push(b'$');
            }
            let _ = write!(out, "#{colour}");
            let mut next = 0;
            let mut run = (0u8, 0u32);
            for &(column, sixel) in columns.iter() {
                if column > next {
                    sixels_run(out, &mut run, 0, column.saturating_sub(next));
                }
                sixels_run(out, &mut run, sixel, 1);
                next = column.saturating_add(1);
            }
            sixels_run(out, &mut run, 0, width.saturating_sub(next));
            sixels_run(out, &mut run, 0, 0);
            columns.clear();
        }
        used.clear();
        out.push(b'-');
        top = top.saturating_add(6);
    }
    out.extend_from_slice(b"\x1b\\");
}

/// Adds `n` of `sixel` to `run`, the sixels of one value written last but
/// not yet put in `out`: a different value puts `run` in `out` first, three
/// or fewer as themselves, more as a repeat. `n` 0 of a value never in a run
/// (0 after 0 is) ends one: `sixels_run(out, &mut run, 0, 0)` after the
/// last puts the last in.
fn sixels_run(out: &mut Vec<u8>, run: &mut (u8, u32), sixel: u8, n: u32) {
    let (value, count) = *run;
    if value == sixel && n > 0 {
        *run = (value, count.saturating_add(n));
        return;
    }
    let ch = value.saturating_add(0x3f);
    match count {
        0 => {}
        1..=3 => out.extend(std::iter::repeat_n(ch, usize::try_from(count).unwrap_or(0))),
        _ => {
            let _ = write!(out, "!{count}");
            out.push(ch);
        }
    }
    *run = (sixel, n);
}

#[cfg(test)]
mod tests {
    use super::*;

    impl Picture {
        /// A picture `width` by `height` of the pixels `rgba`, if they are
        /// exactly that many.
        pub(crate) fn new(width: u32, height: u32, rgba: Vec<u8>) -> Option<Self> {
            let bytes = (u64::from(width).checked_mul(u64::from(height)))
                .and_then(|pixels| pixels.checked_mul(4))
                .and_then(|bytes| usize::try_from(bytes).ok());
            (bytes == Some(rgba.len())).then(|| Self {
                width,
                height,
                across: width,
                down: height,
                aspect: (1, 1),
                background: None,
                ended: height.saturating_sub(1),
                rgba: Arc::new(rgba),
                covered: None,
            })
        }
    }

    fn decode(data: &[u8]) -> Option<Arc<Picture>> {
        // P1 9: square pixels; P2 1: pixels no sixel sets stay clear.
        let mut decoder = Decoder::new(9, 1, true);
        data.iter().for_each(|b| decoder.push(*b, &mut |_, _| true));
        decoder.finish(&mut |_, _| true).0
    }

    /// GR codes are errors whose 8th bit "is set to zero and the code is
    /// then processed as a GL code" (DEC STD 070-9, 9.4): `!3` then 0xFE
    /// is three `~`. C1 codes, which DEC has end sixel mode (9.3), are no
    /// controls in UTF-8 (README), and are ignored as C0 controls are
    /// (9.1): a repeat count goes on past one.
    #[test]
    fn gr_codes_are_read_as_gl_and_c1_codes_ignored() {
        let pixels = |data: &[u8]| decode(data).map(|p| (p.width(), p.height(), p.rgba.to_vec()));
        let three = pixels(b"#1;2;100;0;0!3~");
        assert!(three.is_some());
        assert_eq!(pixels(b"#1;2;100;0;0!3\xfe"), three, "GR");
        assert_eq!(pixels(b"#1;2;100;0;0!\x853~"), three, "C1");
        // A UTF-8 character's bytes are GR codes each: `é` is `C` then `)`,
        // which ends a command and does nothing (as xterm 411 reads it;
        // README, 'Sixel data is read a byte at a time').
        assert_eq!(
            pixels("#1;2;100;0;0\u{e9}".as_bytes()),
            pixels(b"#1;2;100;0;0C)"),
            "UTF-8"
        );
        assert!(pixels("#1;2;100;0;0\u{e9}".as_bytes()).is_some());
    }

    /// A sixel before any colour is selected is drawn in a colour that
    /// contrasts with the background (DEC STD 070-9, 10: "Sixel color:
    /// Contrasting to background"): register 3, the VT340's green, 20% red,
    /// 80% green and 20% blue, as xterm 411 starts, not register 0, the
    /// background's black.
    #[test]
    fn the_first_sixel_colour_contrasts_with_the_background() -> Result<(), String> {
        let picture = decode(b"~").ok_or("no picture")?;
        assert_eq!(picture.pixel(0, 0), Some([51, 204, 51, 255]));
        let mut decoder = Decoder::new(0, 0, true);
        b"~".iter().for_each(|b| decoder.push(*b, &mut |_, _| true));
        let picture = decoder.finish(&mut |_, _| true).0.ok_or("no picture")?;
        assert_ne!(picture.pixel(0, 0), picture.background);
        Ok(())
    }

    /// Colours defined and selected, sixels repeated, bands below one
    /// another; and the picture encoded back decodes to itself.
    #[test]
    fn sixels_decode_and_encode_back() -> Result<(), String> {
        // Red 2 wide by 6 high, then a band below of one blue column 3 high.
        let data = b"#1;2;100;0;0#2;2;0;0;100#1!2~-#2F";
        let picture = decode(data).ok_or("no picture")?;
        assert_eq!((picture.width, picture.height), (2, 9));
        assert_eq!(picture.pixel(1, 5), Some([255, 0, 0, 255]));
        assert_eq!(picture.pixel(0, 8), Some([0, 0, 255, 255]));
        assert_eq!(
            picture.pixel(1, 8),
            Some([0, 0, 0, 0]),
            "clear where nothing was drawn"
        );
        let mut encoded = Vec::new();
        encode(&picture, (0, 0, 2, 9), &mut encoded);
        let body = encoded
            .strip_prefix(b"\x1bP0;1;0q")
            .and_then(|b| b.strip_suffix(b"\x1b\\"))
            .unwrap_or_default();
        assert_eq!(decode(body).as_deref(), Some(&*picture));
        Ok(())
    }

    /// A picture of more than 256 colours is drawn in a cube of 6 reds, 7
    /// greens and 6 blues: each channel to its nearest step below, which
    /// the colour definitions give in percent.
    #[test]
    fn many_colours_are_drawn_in_the_cube() -> Result<(), String> {
        let mut rgba = Vec::new();
        for c in [[0, 0, 0], [255, 255, 255], [128, 128, 128], [200, 100, 50]] {
            rgba.extend(c);
            rgba.push(255);
        }
        for i in 4..300u32 {
            let [low, high, ..] = i.to_le_bytes();
            rgba.extend([low, high.saturating_mul(128), 7, 255]);
        }
        let picture = Picture::new(300, 1, rgba).ok_or("not 300 pixels")?;
        let mut encoded = Vec::new();
        encode(&picture, (0, 0, 300, 1), &mut encoded);
        let body = encoded
            .strip_prefix(b"\x1bP0;1;0q")
            .and_then(|b| b.strip_suffix(b"\x1b\\"))
            .unwrap_or_default();
        let back = decode(body);
        let pixel = |x| back.as_ref().and_then(|p| p.pixel(x, 0));
        assert_eq!(pixel(0), Some([0, 0, 0, 255]));
        assert_eq!(pixel(1), Some([255, 255, 255, 255]));
        // 128: red and blue 3 of 5 steps, 153, 60%; green 3 of 6, 127,
        // 50% (not 49%, rounded down, read back as 124).
        assert_eq!(pixel(2), Some([153, 127, 153, 255]));
        // 200 is red's step 4 (204, 80%); 100 green's 2 (85, 33%: 84); 50
        // blue's 1 (51, 20%).
        assert_eq!(pixel(3), Some([204, 84, 51, 255]));
        Ok(())
    }

    /// The colour at the picture's top left, of `data`.
    fn first(data: &[u8]) -> Option<[u8; 4]> {
        decode(data).and_then(|p| p.pixel(0, 0))
    }

    /// DEC STD 070, 8.1.3: a Pu other than 1 (HLS) or 2 (RGB), or a
    /// coordinate past its range (hue 360, the rest 100), makes "the entire
    /// color specifier command" ignored, "including the color select
    /// parameter, Pc"; omitted coordinates are 0.
    #[test]
    fn a_colour_specifier_out_of_range_is_ignored_whole() {
        let red = Some([255, 0, 0, 255]);
        let blue = Some([0, 0, 255, 255]);
        // Colour 1 red; then ignored, so it stays red.
        for bad in [
            "#1;0;0;0;100",
            "#1;3;0;0;100",
            "#1;2;0;0;101",
            "#1;1;361;50;100",
        ] {
            assert_eq!(
                first(format!("#1;2;100;0;0{bad}#1~").as_bytes()),
                red,
                "{bad}"
            );
        }
        // Ignored, it selects nothing: the colour selected before paints.
        assert_eq!(first(b"#2;2;0;0;100#2#3;2;200;0;0~"), blue);
        // Hue 360 and an omitted blue are in range.
        assert_eq!(first(b"#1;2;100;0#1~"), red);
        assert_eq!(first(b"#1;1;360;50;100#1~"), blue);
    }

    /// DEC STD 070, 8.1: "Format Effectors (0/8 to 0/13) and SPACE (2/0)
    /// are ignored within sixel control strings", and DELETE too (its
    /// coding section): one inside a colour command does not end it.
    #[test]
    fn format_effectors_space_and_delete_are_ignored() {
        let blue = Some([0, 0, 255, 255]);
        for gap in ["\n", "\r", "\t", "\x08", "\x0b", "\x0c", " ", "\x7f"] {
            let data = format!("#1;2;0;{gap}0;100#1~");
            assert_eq!(first(data.as_bytes()), blue, "{gap:?}");
        }
    }

    /// DEC STD 070, 8.1.2: the raster attributes' extent "does not limit
    /// the number of pixels a device may image". One past what a picture
    /// may hold, a side or the pixels, spoils none: the data decides.
    #[test]
    fn an_extent_past_the_limits_spoils_no_picture() {
        let red = Some([255, 0, 0, 255]);
        for extent in ["\"1;1;5000;10", "\"1;1;10;5000", "\"1;1;4000;4000"] {
            let data = format!("{extent}#1;2;100;0;0~");
            assert_eq!(first(data.as_bytes()), red, "{extent}");
        }
    }

    /// Data that would draw past what a picture may keep is cut to it, as
    /// sixels past the right margin "are not printed" (DEC STD 070, 11.2.2):
    /// 4096 pixels a side, and 4096 by 2160 of the grid in all.
    #[test]
    fn a_picture_past_its_limits_is_clipped() {
        let size = |data: &[u8]| decode(data).map(|p| (p.width(), p.height()));
        assert_eq!(size(b"!4096~!4096~"), Some((4096, 6)));
        assert_eq!(size(b"#1;2;100;0;0!4097~"), Some((4096, 6)));
        let mut tall = b"!4096~".to_vec();
        tall.extend(std::iter::repeat_n(b'-', 400));
        tall.extend(b"!4096~");
        // Rows 0 to 2159 kept: 2,160 is the most beside 4,096 columns.
        assert_eq!(size(&tall), Some((4096, 6)), "the band past 2,160 rows");
        let mut narrow = b"~".to_vec();
        narrow.extend(std::iter::repeat_n(b'-', 400));
        narrow.extend(b"!4096~");
        // A band 2,406 rows down beside one column: then at most 3,677 columns.
        let most = u32::try_from(4096 * 2160 / 2406).unwrap_or(0);
        assert_eq!(size(&narrow), Some((most, 2406)));
        assert_eq!(size(b""), None);
    }

    /// A picture grows each side by doubling within what a picture may
    /// keep (`THREATS.md` B03): a legal 2,049 by 4,096 one gets room for no
    /// more than `MAX_PIXELS`, never 4,096 by 4,096.
    #[test]
    fn growth_stays_within_what_a_picture_may_keep() {
        let mut decoder = Decoder::new(9, 1, true);
        // A sixel at the last column, its lowest bit on row 4,095.
        let mut data = b"!2048?".to_vec();
        data.extend(std::iter::repeat_n(b'-', 682));
        data.push(0x3f + (1 << 3));
        for byte in data {
            decoder.push(byte, &mut |_, _| true);
        }
        assert_eq!((decoder.across, decoder.down), (1, 4096));
        for byte in b"$!2048?~" {
            decoder.push(*byte, &mut |_, _| true);
        }
        assert_eq!((decoder.across, decoder.down), (2049, 4096));
        assert!(decoder.bytes() <= MAX_PIXELS * 4, "{}", decoder.bytes());
        let (picture, whole) = decoder.finish(&mut |_, _| true);
        assert_eq!(picture.map(|p| (p.width(), p.height())), Some((2049, 4096)));
        // The `~`'s two lowest pixels, on rows 4,096 and 4,097, were cut.
        assert!(!whole);
    }

    /// What a picture drew past what one may keep is not kept, and the
    /// picture says it is not whole: a sixel past `MAX_SIDE` columns, an
    /// extent past it, or pixels refused room. One that drew nothing is
    /// empty, not cut, a blank repeat past `MAX_SIDE` too.
    #[test]
    fn a_picture_cut_says_so() {
        let ended = |data: &[u8], room: bool| {
            let mut decoder = Decoder::new(9, 1, true);
            data.iter().for_each(|b| decoder.push(*b, &mut |_, _| room));
            let (picture, whole) = decoder.finish(&mut |_, _| room);
            (picture.map(|p| (p.width(), p.height())), whole)
        };
        assert_eq!(ended(b"!4096~", true), (Some((4096, 6)), true));
        assert_eq!(ended(b"!4097~", true), (Some((4096, 6)), false));
        assert_eq!(ended(b"!5000?", true), (None, true));
        assert_eq!(ended(b"!5000?~", true), (None, false));
        assert_eq!(ended(b"\"1;1;4097;6~", true), (Some((4096, 6)), false));
        assert_eq!(ended(b"~", false), (None, false));
        assert_eq!(ended(b"", true), (None, true));
        assert_eq!(ended(b"?", false), (None, true));
    }

    /// The grid is kept at its own resolution and shown at its aspect: at
    /// 2:1 each sixel row shows as two, so six rows of 4,096 sixels keep
    /// 4,096 by 6 pixels and show 4,096 by 12 (THREATS.md B04).
    #[test]
    fn a_grid_is_kept_at_its_resolution_and_shown_at_its_aspect() {
        let mut decoder = Decoder::new(0, 1, true);
        for byte in b"#1;2;100;0;0!4096~" {
            decoder.push(*byte, &mut |_, _| true);
        }
        assert_eq!(decoder.bytes(), 4096 * 6 * 4);
        let (picture, whole) = decoder.finish(&mut |_, _| true);
        assert!(whole);
        let picture = picture.as_deref();
        assert_eq!(picture.map(|p| (p.width(), p.height())), Some((4096, 12)));
        let red = Some([255, 0, 0, 255]);
        assert_eq!(picture.and_then(|p| p.pixel(4095, 11)), red);
        assert_eq!(picture.and_then(|p| p.pixel(0, 12)), None);
    }

    /// The README states what a picture may keep as `MAX_SIDE` and
    /// `MAX_PIXELS` have it: the pixels in all, as `MAX_SIDE` by the other
    /// side, and their RGBA bytes.
    #[test]
    fn the_readme_states_what_a_picture_keeps() {
        // Its words, a blank between each, as its lines wrap them.
        let readme = include_str!("../README.md").split_whitespace();
        let readme = readme.collect::<Vec<_>>().join(" ");
        let side = usize::try_from(MAX_SIDE).unwrap_or_default();
        assert_eq!(MAX_PIXELS % side, 0);
        let other = MAX_PIXELS / side;
        // 35,389,440: four bytes a pixel, thousands grouped.
        let digits = (MAX_PIXELS * 4).to_string();
        let mut bytes = String::new();
        for (i, digit) in digits.chars().enumerate() {
            if i > 0 && (digits.len() - i).is_multiple_of(3) {
                bytes.push(',');
            }
            bytes.push(digit);
        }
        for said in [
            format!("({side} positions a side, {side} by {other} in all,"),
            format!("({side} a side, {side} by {other} in all;"),
            format!("at most {side} by {other} positions"),
            format!("take at most {bytes} bytes"),
        ] {
            assert!(readme.contains(&said), "README: {said}");
        }
    }
}

/// The decoder's check, shown a picture past what one may be.
#[cfg(all(test, feature = "checks"))]
mod faults {
    #[test]
    fn a_picture_past_what_one_may_be_is_a_fault() {
        let mut decoder = super::Decoder::new(0, 0, true);
        for byte in b"#1~~" {
            decoder.push(*byte, &mut |_, _| true);
        }
        assert_eq!(decoder.check(), Ok(()));
        decoder.across = super::MAX_SIDE.saturating_add(1);
        assert!(decoder.check().is_err());
    }
}
