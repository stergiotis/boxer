//! Pictures a program draws with sixel, placed on the screen: each takes the
//! cells it covers, which hold [`PLACEHOLDER`] while it is there. Text or
//! an erase over any of them replaces the placeholder, as a terminal's
//! text replaces the pixels below it, and the picture is gone; scrolling
//! takes the cells, and the picture, along.

use super::Screen;
use crate::RowId;
use crate::sixel::Picture;
use std::sync::Arc;

/// What a picture's cells hold while it is there, one cell wide: U+0080,
/// a C1 control with no function in ECMA-48, which printed text never
/// stores (a control has no width), so text can't forge a picture's
/// cells. Read as a blank.
pub const PLACEHOLDER: &str = "\u{80}";

/// The most pictures kept, on both screens together; a new one past it
/// lets one go, those no longer shown first (`Screen::victim`).
const MAX_PICTURES: usize = 64;

/// The most words of 64 pixels one placement covers of the pictures under
/// it, the room to note them included (`Screen::cover`, `THREATS.md` B46):
/// 256 million pixels, a 1920 by 1080 screen's over a hundred times; past
/// it, one under it goes whole.
const COVER_WORDS: usize = 4_000_000;

/// The most bytes of pixels a terminal's pictures hold together by
/// default ([`crate::Parser::set_picture_bytes`]), however large their data
/// says they are: those kept on either screen, the one being drawn, and its
/// old pixels while it grows (`THREATS.md` B01). Past it, pictures no
/// longer shown go first, then the oldest. It holds the largest picture,
/// 4096 by 2160 (`sixel::MAX_SIDE`), twice.
pub const PICTURE_BYTES: usize = 96 << 20;

/// What the host's terminal draws, for the pictures a program asks about
/// and draws. A picture is laid out in cells, so sixel comes with a cell
/// size: with none known ([`Graphics::Text`]), no picture is placed, and
/// `CSI 14 t` and `CSI 16 t` are not answered.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Graphics {
    /// Text alone, in cells of no known size.
    #[default]
    Text,
    /// Text alone, in cells of this size in pixels.
    Cell(CellSize),
    /// Sixel too, in cells of this size in pixels.
    Sixel(CellSize),
}

impl Graphics {
    /// Its cell's size in pixels, if known.
    pub fn cell(self) -> Option<CellSize> {
        match self {
            Self::Text => None,
            Self::Cell(cell) | Self::Sixel(cell) => Some(cell),
        }
    }
}

/// The size of a cell in pixels, as the host's terminal says it: at least
/// a sixel's band (`LEAST`), so that placing a picture costs a bounded
/// number of cells (`THREATS.md` B05). A smaller one is no size a picture
/// can be laid out in, and is refused, not replaced.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct CellSize {
    width: u16,
    height: u16,
}

impl CellSize {
    /// The smallest cell, width and height in pixels: a sixel band, 2 by 6.
    pub const LEAST: (u16, u16) = (2, 6);
    /// A cell `width` by `height` pixels, unless smaller than a sixel band.
    pub fn new(width: u16, height: u16) -> Option<Self> {
        (width >= Self::LEAST.0 && height >= Self::LEAST.1).then_some(Self { width, height })
    }
    /// Its width in pixels.
    pub fn width(self) -> u16 {
        self.width
    }
    /// Its height in pixels.
    pub fn height(self) -> u16 {
        self.height
    }
}

/// A picture placed on a grid.
#[derive(Clone, Debug)]
pub(crate) struct Placement {
    picture: Arc<Picture>,
    /// Its top row, which it moves with, and its first column; and its
    /// bottom row.
    top: RowId,
    bottom: RowId,
    /// The top row's number in the order rows came into the main screen's
    /// history, once it has (`Screen::track_pictures`).
    number: crate::history::Noted,
    col: u16,
    /// The cells it covers, the columns those it was placed in (clipped
    /// at the margin or edge it met), and the cell size it was laid out in.
    cols: u16,
    rows: u16,
    cell: (u16, u16),
    /// On the alternate screen.
    alternate: bool,
    /// The version it was last found whole at: a row of it no newer holds
    /// the placeholder still, as a row's version changes with its cells,
    /// while its bottom row is where it was. Scrolls keep the order of the
    /// rows they don't make anew, so no older row is among its rows then.
    whole_at: u64,
    /// Its first rows that are gone, scrolled off the top of a region or
    /// a screen that keeps no history (DEC STD 070-9, 11.2.4: what scrolls
    /// off is gone, the rest stays), noted by the scroll that took them
    /// (`crop_pictures`), whatever margins are set later.
    cropped: u16,
    /// Its number, in the order placed: how the tests tell pictures apart.
    #[cfg(test)]
    pub(super) id: u64,
}

/// A picture as the screen shows it now: its top row on the screen, which
/// is above the screen's top (negative) once that has scrolled away, its
/// first column, the cells it covers, and its pixel size per cell.
#[derive(Clone, Copy, Debug)]
pub struct Shown<'a> {
    /// Its pixels.
    pub picture: &'a Arc<Picture>,
    /// Its top row, from the screen's top.
    pub row: i64,
    /// Its first column.
    pub col: u16,
    /// The columns its cells hold: those it was placed in, clipped at the
    /// right margin or the screen's edge it met then, and at the edge now.
    pub cols: u16,
    /// The rows it covers.
    pub rows: u16,
    /// Its first rows that are gone: scrolled off the top of a screen or
    /// a region that keeps no history (DEC STD 070-9, 11.2.4), the rest
    /// shown below them. Above the screen's top (`row` negative) or on
    /// it, above a region's top margin, those rows hold none of it.
    pub cropped: u16,
    /// The width and height in pixels of the cells it was laid out in.
    pub cell: (u16, u16),
}

impl Screen {
    /// What the host's terminal draws, as [`crate::Parser::set_host_graphics`]
    /// last said: its cell size, if known, and whether it shows sixel.
    pub fn graphics(&self) -> Graphics {
        self.graphics
    }

    /// Whether the host draws sixel, as DA1 says.
    pub(crate) fn sixel(&self) -> bool {
        matches!(self.graphics, Graphics::Sixel(_))
    }

    /// What the host's terminal draws, as [`crate::Parser::set_host_graphics`]
    /// says.
    pub(crate) fn set_graphics(&mut self, graphics: Graphics) {
        self.graphics = graphics;
    }

    /// Places a picture's clipped placeholder rectangle and leaves the
    /// cursor in its first column, on the last band or graphics new line.
    /// Covers older pictures and drops those no longer shown. Refuses
    /// without a cell size or when margin scrolling loses bands.
    pub(crate) fn place_picture(&mut self, picture: Arc<Picture>) -> bool {
        let Some(cell) = self.graphics.cell() else {
            return false;
        };
        if self.pictures.is_empty() {
            // Nothing to follow: its rows are followed from here.
            self.primary
                .followed()
                .set(Some(self.primary.history_numbers().1));
        }
        let cell = (cell.width(), cell.height());
        self.track_pictures();
        // Drop hidden pictures before new placeholders can look like theirs.
        let alternate = self.alternate_active;
        self.let_go_hidden();
        let cells = |pixels: u32, per: u16| {
            u16::try_from(pixels.div_ceil(u32::from(per)))
                .unwrap_or(u16::MAX)
                .max(1)
        };
        let (cols, mut rows) = (
            cells(picture.width(), cell.0),
            cells(picture.height(), cell.1),
        );
        // DECSDM: top left, clipped at the bottom, with the cursor restored.
        let display = self
            .mode(crate::Mode::SixelDisplay)
            .then(|| self.grid().cursor);
        if display.is_some() {
            let g = self.grid_mut();
            (g.cursor.pending_wrap, g.cursor.restored) = (false, false);
            g.set_row(0);
            g.set_col(0);
            rows = rows.min(self.size().rows());
        }
        let col = self.grid().cursor.col();
        // Clip as text would at this column (DEC STD 070-9, 11.2.2).
        let across = cols.min(self.grid().line_end().saturating_sub(col));
        let Some(top) = self.cursor_mark().map(|mark| mark.row) else {
            return false;
        };
        // Fill independently of text modes and the pen (DEC STD 070-9, 11.1).
        self.break_cluster();
        let placeholder = PLACEHOLDER.chars().next().unwrap_or(' ');
        let span = (col, col.saturating_add(across));
        // A unit a row placed: its fill, scrolls and history copies spend
        // as they go.
        crate::budget::spend(rows);
        // Graphics new lines may take the cursor below the last band, by
        // at most eight rows (DEC STD 070-9, 11.3; THREATS.md B05).
        let down = picture.ended().checked_div(u32::from(cell.1)).unwrap_or(0);
        let bands = u32::from(rows.saturating_sub(1));
        let extra = down.saturating_sub(bands).min(8);
        let g = self.grid();
        let (start, region) = (u32::from(g.cursor.row()), g.lines);
        let margin = display.is_none() && g.lr();
        let inside = margin && start <= u32::from(region.last());
        let limit = u32::from(if inside {
            region.last()
        } else {
            self.size().rows().saturating_sub(1)
        });
        let narrow = |row: u32| u16::try_from(row.min(limit)).unwrap_or(u16::MAX);
        let steps = bands.saturating_add(extra);
        let room = limit.saturating_sub(start);
        let scrolls = if inside && g.in_columns() {
            steps.saturating_sub(room)
        } else {
            0
        };
        let lifted = scrolls > 0 || (inside && bands > room);
        if scrolls > 0 {
            self.let_go_scrolled();
            let count = u16::try_from(scrolls).unwrap_or(u16::MAX);
            self.scroll_columns(region, count, crate::grid::Scroll::Up { history: true });
        }
        let placed_top = if lifted {
            start
                .checked_sub(scrolls)
                .filter(|first| scrolls > 0 && *first >= u32::from(region.first()))
                .map(narrow)
                .and_then(|first| self.grid().live_row(first).map(|r| r.id()))
        } else {
            Some(top)
        };
        let tail_scrolls = extra.saturating_sub(room.saturating_sub(bands));
        let (mut bottom, mut cut) = (top, false);
        for row in 0..u32::from(rows) {
            let logical = start.saturating_add(row);
            let moved = scrolls > 0 && logical >= u32::from(region.first());
            let at = if moved {
                logical
                    .checked_sub(scrolls)
                    .filter(|y| *y >= u32::from(region.first()))
            } else if margin || display.is_some() {
                Some(logical)
            } else {
                if row > 0 {
                    self.linefeed();
                }
                Some(u32::from(self.grid().cursor.row()))
            };
            let Some(y) = at.map(narrow) else {
                continue;
            };
            // A refused placement keeps copied bands blank and only its
            // unmoved bands as placeholders. Rows above the region stay.
            let whole = !moved || placed_top.is_some() || (row == bands && tail_scrolls == 0);
            self.grid_mut().set_col(col);
            self.grid_mut().cursor.pending_wrap = false;
            self.with_grid(|g, _, v| {
                let wrapped = g.live_wrapped(y);
                // To a double-width line's end (DEC STD 070, 5.6.1),
                // which cuts the picture.
                let end = span.1.min(g.row_end(y));
                cut |= end < span.1;
                // NUL fills empty cells, clearing links and keeping the used mark.
                let c = if whole { placeholder } else { '\0' };
                g.fill(y, (span.0.min(end), end), c, 0, v);
                g.wrap(y, wrapped, v);
            });
            bottom = self.grid().live_row(y).map_or(top, |r| r.id());
        }
        // DEC STD 070-9 (11.3) leaves the row open; follow xterm 411's last band.
        if let Some(cursor) = display {
            self.grid_mut().cursor = cursor;
        } else {
            if margin {
                self.grid_mut().set_row(narrow(start.saturating_add(steps)));
                self.grid_mut().cursor.pending_wrap = false;
            } else {
                // Ordinary line feeds retain every intermediate history row.
                for _ in 0..extra {
                    self.linefeed();
                }
            }
            self.grid_mut().set_col(col);
            self.grid_mut().fit_cursor();
        }
        let Some(top) = placed_top.filter(|_| !cut) else {
            return false;
        };
        #[cfg(test)]
        {
            self.picture_ids = self.picture_ids.wrapping_add(1);
        }
        self.pictures.push_back(Placement {
            picture,
            top,
            bottom,
            number: crate::history::Noted::none(),
            col,
            cols: across,
            rows,
            cell,
            alternate,
            whole_at: self.version,
            cropped: 0,
            #[cfg(test)]
            id: self.picture_ids,
        });
        // Its rows, and others', may have gone into history.
        self.track_pictures();
        // Crop rows lost before this placement was kept for `crop_pictures`.
        if !self.keeps_rows(region)
            && let Some(lost) = (self.pictures.back()).and_then(|p| self.lost_off(p, region))
            && let Some(p) = self.pictures.back_mut()
        {
            p.cropped = lost;
        }
        self.cover();
        // Later changes take a newer version than these whole pictures.
        let whole_at = self.version;
        (self.pictures.iter_mut())
            .filter(|p| p.alternate == alternate)
            .for_each(|p| p.whole_at = whole_at);
        self.begin();
        // Its pixels had room as it grew: only the count can be past.
        while self.pictures.len() > MAX_PICTURES || self.over_budget(0) {
            let Some(victim) = self.victim() else { break };
            self.pictures.remove(victim);
        }
        true
    }

    /// The newest picture lets go of those of its screen no longer shown
    /// (text or an erase over them, their rows gone from history), whose
    /// pixels nothing can show again, and covers those it lies on, laid
    /// out in its cell size: of each, the pixels under its opaque ones
    /// read clear from now on, whatever later shows or hides it, as on a
    /// terminal's one plane of pixels, where a sixel paints over those
    /// below and P2 1 leaves the pixels it doesn't set "unchanged" (DEC
    /// STD 070-9, 7.2), and as xterm 411 draws pictures, in order, a clear
    /// pixel showing those below (`graphics.c`). One covered whole goes;
    /// so does one with no room to note what is covered, a bit for each
    /// pixel of a row of it covered and 16 bytes a row it shows, inside
    /// the budget (B01), or past the words a placement may cover
    /// (`COVER_WORDS`, B46), the newest covered first. Pictures laid out in
    /// other cell sizes are never shown together, and cover none of each
    /// other.
    fn cover(&mut self) {
        // Itself as shown: none if it is not, which covers nothing.
        let over = (self.pictures.back()).and_then(|p| self.shown(p));
        let over = over.map(|s| {
            (
                Arc::clone(s.picture),
                s.cell,
                s.area(),
                s.origin(),
                s.pixels(),
            )
        });
        let alternate = self.alternate_active;
        let placed = self.pictures.len().saturating_sub(1);
        // Those of its screen no longer shown go: nothing can show their
        // pixels again. Those under it, newest first, each with its top
        // left's pixel and what of it the screen shows.
        let (mut gone, mut under) = (Vec::new(), Vec::new());
        for (at, p) in (self.pictures.iter().enumerate()).take(placed).rev() {
            match self.shown(p).filter(|_| p.alternate == alternate) {
                Some(s)
                    if over
                        .as_ref()
                        .is_some_and(|o| p.cell == o.1 && overlaps(s.area(), o.2)) =>
                {
                    under.push((at, s.origin(), s.pixels()));
                }
                None if p.alternate == alternate => gone.push(at),
                Some(_) | None => {}
            }
        }
        let Some((picture, _, _, origin, (right, top, bottom))) = over else {
            return self.let_go(&gone);
        };
        let bottom = bottom.min(picture.height());
        // The words promised to those taken, not yet made.
        let (mut coverable, mut words, mut promised) = (Vec::new(), 0_usize, 0_usize);
        for (at, at_origin, shown) in under {
            let Some(p) = self.pictures.get(at) else {
                continue;
            };
            // The rows and columns of it it lies on; the room to note them,
            // a copy of what is noted if the host holds it too, and the
            // words read and written, two a row for each it lies on, as the
            // words a row cuts across may be one more.
            let (x, y) = (
                at_origin.0.saturating_sub(origin.0),
                at_origin.1.saturating_sub(origin.1),
            );
            let rows = span(y, shown.1, shown.2, top, bottom);
            let cols = span(x, 0, shown.0, 0, right);
            let (list, row_words) = p.picture.cover_bytes(shown, rows);
            let copied = match Arc::strong_count(&p.picture) {
                1 => 0,
                _ => p.picture.covered_bytes(),
            };
            let cost = (list.saturating_add(row_words).saturating_add(copied) / 8)
                .saturating_add(rows.saturating_mul(cols.div_ceil(64).saturating_add(1)));
            let ready = words.saturating_add(cost) <= COVER_WORDS
                && !self.over_budget(promised.saturating_add(list).saturating_add(row_words))
                && (self.pictures.get_mut(at))
                    .is_some_and(|p| Arc::make_mut(&mut p.picture).coverable(shown));
            if ready {
                words = words.saturating_add(cost);
                promised = promised.saturating_add(row_words);
                // Its rows among the newest's.
                let first = i64::from(shown.1).saturating_add(y).max(i64::from(top));
                let first = u32::try_from(first).unwrap_or(u32::MAX);
                let end = first.saturating_add(u32::try_from(rows).unwrap_or(u32::MAX));
                coverable.push((at, at_origin, first..end));
            } else {
                gone.push(at);
            }
        }
        crate::budget::spend(words);
        // A row of its pixels read once for each row of the grid it was
        // drawn in, on the rows some picture under it has, and laid on
        // each picture with that row: those under it in the order their
        // rows begin, each walked on its own rows alone, a gap between
        // them skipped.
        coverable.sort_by_key(|c| c.2.start);
        let end = coverable.iter().map(|c| c.2.end).max().unwrap_or(0);
        let (mut bits, mut read) = (Vec::new(), None);
        let (mut next, mut active) = (0, Vec::new());
        let mut y = coverable.first().map_or(end, |c| c.2.start);
        while y < end {
            while let Some(c) = coverable.get(next).filter(|c| c.2.start <= y) {
                active.push((c.0, c.1, c.2.end));
                next = next.saturating_add(1);
            }
            active.retain(|a| a.2 > y);
            if active.is_empty() {
                y = coverable.get(next).map_or(end, |c| c.2.start);
                continue;
            }
            let row = Some(picture.grid_row(y));
            if read != row {
                picture.opaque_row(y, right, &mut bits);
                read = row;
            }
            for &(at, (x, y_at), _) in &active {
                let dy = origin.1.saturating_add(i64::from(y)).saturating_sub(y_at);
                if let Some(p) = self.pictures.get_mut(at) {
                    Arc::make_mut(&mut p.picture).cover_row(dy, origin.0.saturating_sub(x), &bits);
                }
            }
            y = y.saturating_add(1);
        }
        gone.extend(
            coverable
                .iter()
                .map(|(at, _, _)| *at)
                .filter(|at| (self.pictures.get(*at)).is_some_and(|p| p.picture.covered_whole())),
        );
        self.let_go(&gone);
    }

    /// Lets go of the pictures of the screen in use no longer shown.
    fn let_go_hidden(&mut self) {
        let alternate = self.alternate_active;
        let hidden: Vec<usize> = (self.pictures.iter().enumerate())
            .filter(|(_, p)| p.alternate == alternate && self.shown(p).is_none())
            .map(|(at, _)| at)
            .collect();
        self.let_go(&hidden);
    }

    /// Lets go, at a placement's first scroll inside left and right
    /// margins, of the pictures of the screen in use with a cell in the
    /// region it scrolls: their cells move, and land blank
    /// (`grid::unpicture`), and the placement's fills, which follow, could
    /// make them read as theirs again.
    fn let_go_scrolled(&mut self) {
        let g = self.grid();
        let region = (
            i64::from(g.lines.first()),
            g.columns().first(),
            g.columns().len(),
            g.lines.len(),
        );
        let alternate = self.alternate_active;
        let gone: Vec<usize> = (self.pictures.iter().enumerate())
            .filter(|(_, p)| p.alternate == alternate)
            .filter(|(_, p)| self.shown(p).is_none_or(|s| overlaps(s.area(), region)))
            .map(|(at, _)| at)
            .collect();
        self.let_go(&gone);
    }

    /// Lets go of the pictures at `gone`.
    fn let_go(&mut self, gone: &[usize]) {
        let mut at = (0..self.pictures.len()).map(|at| gone.contains(&at));
        self.pictures.retain(|_| !at.next().unwrap_or(false));
    }

    /// The bytes of pixels the screen's pictures hold, on either grid.
    pub(crate) fn picture_bytes(&self) -> usize {
        (self.pictures.iter())
            .map(|p| p.picture.bytes())
            .fold(0, usize::saturating_add)
    }

    /// Whether the pictures kept and `extra` bytes more are past the budget.
    fn over_budget(&self, extra: usize) -> bool {
        self.picture_bytes().saturating_add(extra) > self.picture_budget
    }

    /// The picture to let go first: the oldest no longer shown (on the
    /// other screen, covered, or its rows gone), else the oldest.
    fn victim(&self) -> Option<usize> {
        let hidden = (self.pictures.iter())
            .position(|p| p.alternate != self.alternate_active || self.shown(p).is_none());
        hidden.or_else(|| (!self.pictures.is_empty()).then_some(0))
    }

    /// Whether `bytes` more fit in the budget beside the pictures kept; if
    /// `evict`, letting pictures go, [`Self::victim`] first, until they do.
    /// False, with none let go, if they don't with none kept.
    pub(crate) fn make_room(&mut self, bytes: usize, evict: bool) -> bool {
        if !evict || bytes > self.picture_budget {
            return !self.over_budget(bytes);
        }
        self.track_pictures();
        while self.over_budget(bytes) {
            let Some(victim) = self.victim() else {
                return false;
            };
            self.pictures.remove(victim);
        }
        true
    }

    /// Sets the bytes the pictures may hold, the one being drawn's
    /// `drawing` included, and lets pictures go to fit; false if `drawing`
    /// alone is past it.
    pub(crate) fn set_picture_bytes(&mut self, bytes: usize, drawing: usize) -> bool {
        self.picture_budget = bytes;
        let kept = self.pictures.len();
        // The one being drawn past it alone goes (the parser's), and the
        // pictures kept still fit the budget without it.
        let fits = self.make_room(drawing, true) || {
            let _ = self.make_room(0, true);
            false
        };
        // A picture let go changed what the screen shows: so do its counts.
        if self.pictures.len() != kept {
            self.version = self.version.saturating_add(1);
            self.revise(1);
        }
        fits
    }

    /// The pictures the screen shows: those of the screen in use whose
    /// every cell holds the placeholder, but the rows cropped off the top
    /// of a screen or region that keeps no history (`shown`).
    pub fn pictures(&self) -> impl Iterator<Item = Shown<'_>> {
        self.track_pictures();
        self.pictures
            .iter()
            .filter(|p| p.alternate == self.alternate_active)
            .filter_map(|p| self.shown(p))
    }

    /// Follows the pictures whose top rows have gone into the main screen's
    /// history since last followed, so that each is found there by its
    /// number, not by a search of history (`THREATS.md` B09): the rows
    /// come since are read once, each looked for among those tops, with
    /// nothing to do if none came or no picture's top is still to follow.
    /// Rows go into history only from the screen's top and stay in the
    /// order they came, so a top not on the screen is there or gone.
    pub(crate) fn track_pictures(&self) {
        let (first, next) = self.primary.history_numbers();
        let tracked = self.primary.followed().get().unwrap_or(first);
        if tracked == next || self.pictures.is_empty() {
            return;
        }
        let from = tracked.max(first);
        self.primary.followed().set(Some(next));
        let mut wanted: Vec<(RowId, usize)> = (self.pictures.iter().enumerate())
            .filter(|(_, p)| !p.alternate && p.number.get().is_none())
            .map(|(at, p)| (p.top, at))
            .collect();
        if wanted.is_empty() {
            return;
        }
        wanted.sort_unstable();
        for number in from..next {
            crate::budget::spend(1);
            let Some(id) = self.primary.id_of_number(number) else {
                continue;
            };
            if let Ok(found) = wanted.binary_search_by_key(&id, |(top, _)| *top)
                && let Some(&(_, at)) = wanted.get(found)
                && let Some(placement) = self.pictures.get(at)
            {
                placement.number.set(Some(number));
            }
        }
    }

    /// Follows the pictures into a history made anew (a resize): each is
    /// looked for in all of it once.
    pub(crate) fn retrack_pictures(&mut self) {
        self.pictures.iter().for_each(|p| p.number.set(None));
        self.primary
            .followed()
            .set(Some(self.primary.history_numbers().0));
        self.track_pictures();
    }

    /// Where `placement`'s top row is, counted from the oldest row history
    /// keeps: by its number in the main screen's history, if followed
    /// there, else among the screen's rows.
    fn top_index(&self, placement: &Placement) -> Option<usize> {
        let grid = self.grid();
        let followed = (placement.number.get())
            .filter(|_| !placement.alternate)
            .and_then(|number| grid.index_of_number(number, placement.top));
        followed.or_else(|| grid.screen_index_of(placement.top))
    }

    /// `placement` as the screen shows it, if it is whole: its top row
    /// found by `top_index`, or, that row gone (scrolled off a screen or a
    /// region that keeps no history, as a picture taller than the rows
    /// below the cursor scrolls its own top off), its rows from its bottom
    /// row up, those gone cropped (DEC STD 070-9, 11.2.4: what scrolls off
    /// is gone, the rest stays): those above the oldest row kept, or above
    /// the top margin, which the region's scrolls took; and the cells read
    /// of its rows changed since it was last found whole (`THREATS.md`
    /// B09), and, its top gone, of its first row left, which a row edit
    /// rather than a scroll may have made another's.
    fn shown<'a>(&'a self, placement: &'a Placement) -> Option<Shown<'a>> {
        let grid = self.grid();
        let last = usize::from(placement.rows.saturating_sub(1));
        let (first, cropped) = match self.top_index(placement) {
            Some(first) => (i64::try_from(first).ok()?, None),
            None => {
                let bottom = i64::try_from(grid.screen_index_of(placement.bottom)?).ok()?;
                let first = bottom.checked_sub(i64::try_from(last).ok()?)?;
                // The rows its scrolls took, and any above the oldest row
                // kept.
                let kept = first
                    .saturating_add(i64::from(placement.cropped))
                    .max(first.max(0));
                (
                    first,
                    Some(usize::try_from(kept.saturating_sub(first)).ok()?),
                )
            }
        };
        let at = |offset: usize| {
            let index = first.checked_add(i64::try_from(offset).ok()?)?;
            grid.row_at(usize::try_from(index).ok()?)
        };
        let width = self.size().cols();
        let across = placement.cols.min(width.saturating_sub(placement.col));
        let since = match at(last) {
            Some(row) if row.id() == placement.bottom => Some(placement.whole_at),
            Some(_) | None => None,
        };
        let skip = cropped.unwrap_or(0);
        for offset in skip..usize::from(placement.rows) {
            // Gone past the bottom: not shown.
            let row = at(offset)?;
            let unsure = cropped.is_some() && offset == skip;
            if !unsure && since.is_some_and(|since| row.version() <= since) {
                continue;
            }
            crate::budget::spend(across);
            let whole = (placement.col..placement.col.saturating_add(across)).all(|c| {
                row.cell(usize::from(c))
                    .is_some_and(|cell| cell.contents() == PLACEHOLDER)
            });
            if !whole {
                return None;
            }
        }
        let history = i64::try_from(grid.history_len()).ok()?;
        Some(Shown {
            picture: &placement.picture,
            row: first.saturating_sub(history),
            col: placement.col,
            cols: across,
            rows: placement.rows,
            cropped: u16::try_from(skip).ok()?,
            cell: placement.cell,
        })
    }

    /// Notes, after a scroll up of `region` that kept none of the rows it
    /// took, the rows each picture of the screen in use whose top row it
    /// took lost off the region's top: those of its rows above the top
    /// margin now, which were the region's and are others' (a picture
    /// that went on above the region, its top row there, was cut by the
    /// scroll and is not shown after).
    #[cold]
    #[inline(never)]
    pub(crate) fn crop_pictures(
        &mut self,
        region: crate::geometry::Span,
        direction: crate::grid::Scroll,
    ) {
        use crate::grid::Scroll;
        let kept = direction == (Scroll::Up { history: true }) && self.keeps_rows(region);
        if kept || direction == Scroll::Down {
            return;
        }
        let lost: Vec<Option<u16>> = (self.pictures.iter())
            .map(|p| self.lost_off(p, region))
            .collect();
        for (p, lost) in self.pictures.iter_mut().zip(lost) {
            p.cropped = p.cropped.max(lost.unwrap_or(0));
        }
    }

    /// Whether a scroll up of `region` that may keep its rows takes them
    /// into history (`Grid::scroll`), not off the screen.
    fn keeps_rows(&self, region: crate::geometry::Span) -> bool {
        region.first() == 0 && self.grid().history_bytes() > 0
    }

    /// The rows of `p`, its top row gone, above `region`'s top margin:
    /// those a scroll up of the region that kept none took off it.
    fn lost_off(&self, p: &Placement, region: crate::geometry::Span) -> Option<u16> {
        if p.alternate != self.alternate_active || self.top_index(p).is_some() {
            return None;
        }
        let history = i64::try_from(self.grid().history_len()).ok()?;
        let at = i64::try_from(self.grid().screen_index_of(p.bottom)?).ok()?;
        let first = at
            .saturating_sub(i64::from(p.rows.saturating_sub(1)))
            .saturating_sub(history);
        let above = i64::from(region.first()).saturating_sub(first);
        Some(u16::try_from(above.clamp(0, i64::from(p.rows))).unwrap_or(p.rows))
    }

    /// The pixel size of the text area, for a program's `CSI 14 t`.
    pub(crate) fn pixel_size(&self) -> Option<(u32, u32)> {
        let cell = self.graphics.cell()?;
        let (rows, cols) = self.size().into();
        Some((
            u32::from(rows).saturating_mul(u32::from(cell.height())),
            u32::from(cols).saturating_mul(u32::from(cell.width())),
        ))
    }
}

/// The pictures' part of the invariant checker (THREATS.md section 6).
#[cfg(feature = "checks")]
impl Screen {
    /// 30: at most [`MAX_PICTURES`] on both screens together.
    pub(super) fn check_picture_count(&self) -> Result<(), String> {
        let kept = self.pictures.len();
        crate::check::ensure(kept <= MAX_PICTURES, || format!("{kept} pictures kept"))
    }

    /// 31: the pictures kept, and `drawing` bytes of the one being drawn,
    /// hold at most the budget.
    pub(crate) fn check_picture_bytes(&self, drawing: usize) -> Result<(), String> {
        let bytes = self.picture_bytes().saturating_add(drawing);
        crate::check::ensure(bytes <= self.picture_budget, || {
            format!("{bytes} bytes of pictures, {} at most", self.picture_budget)
        })
    }

    /// 32: each picture shown has its rows, every cell it covers there
    /// holding the placeholder, on the screen in use, but those cropped off
    /// the top of a screen or region that kept no history (DEC STD 070-9,
    /// 11.2.4).
    pub(super) fn check_pictures_shown(&self) -> Result<(), String> {
        let grid = self.grid();
        // What is cropped is what the scrolls noted on the placement, and
        // what lies above the screen's top: no row is cropped by a margin
        // set since.
        for (p, shown) in (self.pictures.iter())
            .filter(|p| p.alternate == self.alternate_active)
            .filter_map(|p| Some((p, self.shown(p)?)))
        {
            // Above the oldest row kept: `row` counts from the screen's top,
            // history's rows above it.
            let history = i64::try_from(grid.history_len()).unwrap_or(i64::MAX);
            let above = shown.row.saturating_add(history).saturating_neg().max(0);
            let above = u16::try_from(above).unwrap_or(u16::MAX);
            let noted = match self.top_index(p) {
                Some(_) => 0,
                None => p.cropped.max(above.min(p.rows)),
            };
            crate::check::ensure(shown.cropped == noted, || {
                format!(
                    "the picture at {}, {} shown with {} rows cropped, {noted} noted",
                    shown.row, shown.col, shown.cropped
                )
            })?;
        }
        for shown in self.pictures() {
            let top = i64::try_from(grid.history_len())
                .unwrap_or(i64::MAX)
                .saturating_add(shown.row);
            let across = shown.cols.min(self.size().cols().saturating_sub(shown.col));
            for offset in i64::from(shown.cropped)..i64::from(shown.rows) {
                let index = top.saturating_add(offset);
                let row = usize::try_from(index).ok().and_then(|i| grid.row_at(i));
                let whole = row.is_some_and(|row| {
                    (shown.col..shown.col.saturating_add(across)).all(|c| {
                        row.cell(usize::from(c))
                            .is_some_and(|cell| cell.contents() == PLACEHOLDER)
                    })
                });
                crate::check::ensure(whole, || {
                    let (row, col) = (shown.row, shown.col);
                    format!("the picture at {row}, {col} shown without its cells")
                })?;
            }
        }
        Ok(())
    }
}

impl Shown<'_> {
    /// The cells it covers: its top row left, first column, columns and
    /// rows left, its cropped rows being others'.
    fn area(&self) -> (i64, u16, u16, u16) {
        let row = self.row.saturating_add(i64::from(self.cropped));
        let rows = self.rows.saturating_sub(self.cropped);
        (row, self.col, self.cols, rows)
    }

    /// Where its top left pixel is on the screen, in pixels of its cells
    /// from the screen's top left: across, then down.
    fn origin(&self) -> (i64, i64) {
        let (width, height) = self.cell;
        (
            i64::from(self.col).saturating_mul(i64::from(width)),
            self.row.saturating_mul(i64::from(height)),
        )
    }

    /// The pixels of it the screen shows: those left of the first, on the
    /// rows from the second to the third, its cropped rows being others'.
    fn pixels(&self) -> (u32, u32, u32) {
        let (width, height) = (u32::from(self.cell.0), u32::from(self.cell.1));
        (
            u32::from(self.cols).saturating_mul(width),
            u32::from(self.cropped).saturating_mul(height),
            u32::from(self.rows).saturating_mul(height),
        )
    }
}

/// How many of the pixels `start` to `end` of a picture whose first is at
/// `at` lie on `from` to `to` of another's.
fn span(at: i64, start: u32, end: u32, from: u32, to: u32) -> usize {
    let (start, end) = (
        at.saturating_add(i64::from(start)),
        at.saturating_add(i64::from(end)),
    );
    let n = end
        .min(i64::from(to))
        .saturating_sub(start.max(i64::from(from)));
    usize::try_from(n.max(0)).unwrap_or(0)
}

/// Whether two areas of cells, each its top row, first column, columns and
/// rows, share a cell.
fn overlaps(
    (row, col, cols, rows): (i64, u16, u16, u16),
    (r, c, cs, rs): (i64, u16, u16, u16),
) -> bool {
    let (end, e) = (
        row.saturating_add(i64::from(rows)),
        r.saturating_add(i64::from(rs)),
    );
    let (right, ri) = (col.saturating_add(cols), c.saturating_add(cs));
    row < e && r < end && col < ri && c < right
}

#[cfg(test)]
impl Screen {
    /// The pictures the screen shows, each with its number.
    pub(crate) fn numbered_pictures(&self) -> impl Iterator<Item = (u64, Shown<'_>)> {
        self.track_pictures();
        (self.pictures.iter())
            .filter(|p| p.alternate == self.alternate_active)
            .filter_map(|p| Some((p.id, self.shown(p)?)))
    }
}

/// The pictures' checks, each shown pictures with their one fault.
#[cfg(all(test, feature = "checks"))]
mod faults {
    use super::*;
    use crate::{Error, Size};

    /// A placement of one pixel, its bytes in room for `room`.
    fn placed(room: usize, top: RowId) -> Option<Placement> {
        let mut rgba = Vec::with_capacity(room);
        rgba.extend([0; 4]);
        Some(Placement {
            picture: Arc::new(Picture::new(1, 1, rgba)?),
            top,
            bottom: top,
            number: crate::history::Noted::none(),
            col: 0,
            cols: 1,
            rows: 1,
            cell: (10, 20),
            alternate: false,
            whole_at: 0,
            cropped: 0,
            id: 0,
        })
    }

    /// A placement found whole at the newest version, whose cell holds no
    /// placeholder: `shown` reads none of its rows, and check 32 reads
    /// them all (`check_pictures_shown`).
    #[test]
    fn a_picture_shown_without_its_cells_is_a_fault() -> Result<(), Error> {
        let size = Size::new(3, 6).map_err(|_| Error::Capacity)?;
        let mut screen = Screen::new(size, 0)?;
        let top = screen.primary.live_row(0).map_or(RowId(0), |r| r.id());
        assert!(screen.check_pictures_shown().is_ok());
        let stale = placed(4, top).map(|p| Placement {
            whole_at: u64::MAX,
            ..p
        });
        screen.pictures.extend(stale);
        assert_eq!(screen.pictures().count(), 1);
        assert!(screen.check_pictures_shown().is_err());
        Ok(())
    }

    #[test]
    fn too_many_pictures_or_too_large_are_faults() -> Result<(), Error> {
        let size = Size::new(3, 6).map_err(|_| Error::Capacity)?;
        let screen = Screen::new(size, 0)?;
        let top = screen.primary.live_row(0).map_or(RowId(0), |r| r.id());
        assert!(screen.check_picture_count().is_ok());
        let mut many = screen.clone();
        for _ in 0..65 {
            many.pictures.extend(placed(4, top));
        }
        assert!(many.check_picture_count().is_err(), "65 pictures");
        // 33 on each screen: 66 in all.
        let mut both = screen.clone();
        for id in 0..66 {
            let mut picture = placed(4, top);
            if let Some(p) = picture.as_mut() {
                p.alternate = id % 2 == 1;
            }
            both.pictures.extend(picture);
        }
        assert!(both.check_picture_count().is_err(), "33 pictures a screen");
        let mut large = screen.clone();
        large
            .pictures
            .extend(placed(PICTURE_BYTES.saturating_add(1), top));
        assert!(large.check_picture_bytes(0).is_err(), "past the bytes");
        let mut small = screen;
        small.picture_budget = 0;
        assert!(small.check_picture_bytes(0).is_ok());
        assert!(small.check_picture_bytes(1).is_err(), "the drawing past it");
        Ok(())
    }
}

/// A private observer of actual margin-scroll calls, including those that
/// clear the whole region and copy no spans.
#[cfg(test)]
pub(super) mod placement_test {
    use crate::{CellSize, Feature, Graphics, Parser, Size};
    use std::cell::Cell;

    thread_local! {
        pub(in crate::screen) static SCROLLS: Cell<u16> = const { Cell::new(0) };
    }

    #[test]
    fn one_picture_placement_scrolls_a_margin_region_at_most_once()
    -> Result<(), Box<dyn std::error::Error>> {
        for (rows, trailing, scrolls) in
            [(1_usize, 0, 0), (2, 0, 1), (4, 0, 1), (2, 2, 1), (12, 0, 1)]
        {
            let mut p =
                Parser::with_options(Size::new(8, 6)?, 1024, Feature::ALL.into_iter().collect())?;
            p.set_host_graphics(Graphics::Sixel(CellSize::new(2, 6).ok_or("cell size")?));
            p.process(b"\x1b]8;;https://example.test\x1b\\")?;
            for y in 1..=8 {
                p.process(format!("\x1b[{y};1H一ab一").as_bytes())?;
                if y < 8 {
                    p.process(b"x")?;
                }
            }
            p.process(b"\x1b]8;;\x1b\\\x1b[?69h\x1b[2;5s\x1b[8;2H")?;
            let ids: Vec<_> = (0..8)
                .filter_map(|y| {
                    p.screen()
                        .window()
                        .row(y)
                        .map(|row| (row.id(), row.wrapped()))
                })
                .collect();
            let bands: String = std::iter::repeat_n("~-", rows.saturating_sub(1)).collect();
            let tail: String = std::iter::repeat_n('-', trailing).collect();
            SCROLLS.set(0);
            p.process(format!("\x1bPq\"1;1{bands}~{tail}\x1b\\").as_bytes())?;
            assert_eq!(SCROLLS.get(), scrolls, "{rows} bands, {trailing} new lines");
            let after: Vec<_> = (0..8)
                .filter_map(|y| {
                    p.screen()
                        .window()
                        .row(y)
                        .map(|row| (row.id(), row.wrapped()))
                })
                .collect();
            assert_eq!(
                after, ids,
                "margin scrolls preserve row identities and soft wraps"
            );
            for y in 0..8 {
                let row = p.screen().window().row(y).ok_or("row")?;
                if row
                    .cell(1)
                    .is_some_and(|c| c.contents() == crate::PLACEHOLDER)
                {
                    assert!(row.link(1).is_none(), "a placeholder clears its link");
                }
            }
            assert_eq!(p.screen().history_len(), 0);
            #[cfg(feature = "checks")]
            {
                let report = p.check();
                assert!(report.is_clean(), "{report}");
            }
        }
        for region in ["", "\x1b[65533;65535r"] {
            let mut p = Parser::with_options(
                Size::new(u16::MAX, 3)?,
                0,
                Feature::ALL.into_iter().collect(),
            )?;
            p.set_host_graphics(Graphics::Sixel(CellSize::new(2, 6).ok_or("cell size")?));
            p.process(format!("\x1b[?69h\x1b[1;2s{region}\x1b[65535;1H").as_bytes())?;
            SCROLLS.set(0);
            p.process(b"\x1bPq\"1;1~-~-~\x1b\\")?;
            assert_eq!(SCROLLS.get(), 1, "one scroll at the row limit");
        }
        Ok(())
    }
}
