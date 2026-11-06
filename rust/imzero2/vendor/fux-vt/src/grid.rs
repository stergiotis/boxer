use std::collections::VecDeque;
use std::ops::Range;
use std::sync::Arc;

use crate::compact::{BLANK, Compact, Line, Text};
use crate::geometry::{Size, Span};
use crate::history::{Arriving, ENDS, History, PROMPT, RENDITION_SHIFT, WRAPPED, trimmed};
use crate::link::{Held, Hyperlink, KeptLinks, Links, RowLinks, Runs};
use crate::screen::Saved;
use crate::style::Styles;
use crate::{Attributes, CellRef, Error, LineRendition, Row, RowId, reserve};

/// The cursor, and what DECSC saves with it. Only the grid's movements set
/// its row and column, each keeping them on the grid: a glyph that reaches
/// the last column leaves the cursor there with `pending_wrap` set.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub(crate) struct Cursor {
    row: u16,
    col: u16,
    /// A glyph went into the last column and the cursor waits past it.
    /// With autowrap on this is DEC STD 070's Last Column Flag (Appendix
    /// D.6.1): the next glyph first wraps. With it off, the next glyph
    /// overwrites the last column and `Screen::pending_wrap` is not set.
    pub pending_wrap: bool,
    /// `pending_wrap` came back with DECRC: the Last Column Flag, which
    /// setting autowrap keeps (DEC STD 070, Appendix D.6.1), even if it
    /// was off. Meaningful only while `pending_wrap` is set; read through
    /// both (`Screen::last_column_flag`). Only `advance_within` (clearing
    /// this) and DECRC (setting it) set `pending_wrap`.
    pub restored: bool,
    /// Origin mode (DECOM).
    pub origin: bool,
}

impl Cursor {
    pub fn row(&self) -> u16 {
        self.row
    }
    pub fn col(&self) -> u16 {
        self.col
    }
    /// Its row and column.
    pub fn at(&self) -> (u16, u16) {
        (self.row, self.col)
    }
    /// This cursor at `row` and `col`, clamped to `size`.
    fn placed(self, (row, col): (u16, u16), size: Size) -> Self {
        Self {
            row: row.min(size.lines().last()),
            col: col.min(size.columns().last()),
            ..self
        }
    }
}

/// How many styles of the table a grid keeps at hand (`Grid::recent`).
const RECENT: usize = 32;

/// The slot of `attributes` among the styles at hand: a hash of them.
#[inline]
fn recent(attributes: Attributes) -> usize {
    let (a, b) = attributes.bits();
    let hash = (a ^ b.rotate_left(17)).wrapping_mul(0x9e37_79b9_7f4a_7c15);
    usize::try_from(hash >> 59).unwrap_or(0)
}

/// Takes the row in `slot` into `history` if it has neither text nor
/// links, blanking its cells for the slot's next row; whether it did.
/// Takes the grid's parts so they borrow apart.
#[inline(always)]
fn take_row(
    history: &mut History,
    cells: &mut [Compact],
    meta: &mut [Meta],
    (texts, links): (&[Text], Option<&[RowLinks]>),
    (slot, width): (usize, u16),
) -> Result<bool, Error> {
    let Some(m) = meta.get_mut(slot) else {
        return Ok(false);
    };
    if texts.get(slot).is_some_and(|text| !text.is_empty())
        || links.is_some_and(|links| links.get(slot).is_some_and(|l| !l.is_empty()))
    {
        return Ok(false);
    }
    // Past its `used` mark the row is blank. A slot's cells are within
    // `cells`, so these do not wrap.
    let start = slot.wrapping_mul(usize::from(width));
    let row = cells
        .get_mut(start..start.wrapping_add(usize::from(m.used)))
        .unwrap_or_default();
    let len = trimmed(row).len();
    // The row's cells, those trimmed too, and the row: once.
    crate::budget::spend(row.len().saturating_add(1));
    let kept = row.get_mut(..len).unwrap_or_default();
    history.push(m.arriving(kept, width))?;
    // Blanked now, while the cells are at hand.
    match kept {
        [cell] => *cell = BLANK,
        _ => kept.fill(BLANK),
    }
    m.used = 0;
    Ok(true)
}

/// `Grid::narrow`'s bits.
const LR: u8 = 1;
const DOUBLED: u8 = 2;

/// The most cells a screen holds, and its history (512 MiB at 8 bytes a
/// cell), whatever history's budget.
pub(crate) const MAX_CELLS: usize = 64 * 1024 * 1024;
/// The most rows history holds, whatever its budget.
pub(crate) const MAX_ROWS: usize = 1_048_576;

/// A row of the screen, in its slot: what it is, besides its cells.
#[derive(Clone, Copy, Debug)]
struct Meta {
    id: RowId,
    version: u64,
    /// Every cell from here to the grid's width is blank, so recycling the
    /// slot clears only the cells before it, and history looks no further.
    used: u16,
    /// `WRAPPED`, `PROMPT` (OSC 133 ; A) and the line rendition above
    /// them, in history's layout, so a row hands them on as they are.
    flags: u8,
}

// `flags` fits in padding: a row costs no more for it.
const _: () = assert!(size_of::<Meta>() == 24);

impl Meta {
    fn arriving(self, cells: &[Compact], width: u16) -> Arriving<'_> {
        Arriving {
            id: self.id,
            version: self.version,
            flags: self.flags,
            cells,
            width,
        }
    }
    fn new(id: RowId, version: u64, wrapped: bool, used: u16) -> Self {
        Self {
            id,
            version,
            used,
            flags: if wrapped { WRAPPED } else { 0 },
        }
    }
    fn wrapped(&self) -> bool {
        self.flags & WRAPPED != 0
    }
    fn rendition(&self) -> LineRendition {
        LineRendition::from_bits(self.flags >> RENDITION_SHIFT)
    }
    /// Sets `flag`; setting or clearing `WRAPPED` also clears `ENDS`.
    fn set(&mut self, flag: u8, on: bool) {
        let ends = if flag & WRAPPED != 0 { ENDS } else { 0 };
        self.flags = (self.flags & !flag & !ends) | if on { flag } else { 0 };
    }
    /// Whether its line goes on in the row below: soft-wrapped, and that
    /// row not erased whole since (`ENDS`).
    fn continues(&self) -> bool {
        self.flags & (WRAPPED | ENDS) == WRAPPED
    }
    fn with_rendition(mut self, rendition: LineRendition) -> Self {
        self.flags = (self.flags & (WRAPPED | PROMPT | ENDS)) | rendition.bits() << RENDITION_SHIFT;
        self
    }
}

/// The slot of each screen row, top to bottom: a ring with the top row's
/// at `top`, so scrolling the whole screen turns it and moves no slot.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
struct Order {
    slots: Vec<usize>,
    top: usize,
}

impl Order {
    fn len(&self) -> usize {
        self.slots.len()
    }
    #[cfg(test)]
    fn capacity(&self) -> usize {
        self.slots.capacity()
    }
    fn try_reserve_exact(&mut self, more: usize) -> Result<(), Error> {
        self.slots
            .try_reserve_exact(more)
            .map_err(|_| Error::Capacity)
    }
    #[inline]
    fn at(&self, row: usize) -> Option<usize> {
        if row >= self.slots.len() {
            return None;
        }
        // Both are below the length, so this does not wrap.
        let at = self.top.wrapping_add(row);
        Some(at.checked_sub(self.slots.len()).unwrap_or(at))
    }
    #[inline]
    fn get(&self, row: usize) -> Option<&usize> {
        self.slots.get(self.at(row)?)
    }
    fn get_mut(&mut self, row: usize) -> Option<&mut usize> {
        let at = self.at(row)?;
        self.slots.get_mut(at)
    }
    fn push_back(&mut self, slot: usize) {
        if self.top != 0 {
            if let Some(slots) = self.slots.get_mut(..) {
                slots.rotate_left(self.top);
            }
            self.top = 0;
        }
        self.slots.push(slot);
    }
    /// Rotates the slots of rows `low..=high` by `by`, left (up) or right,
    /// in one pass whatever `by`, touching only those rows (THREATS.md B36).
    fn rotate(&mut self, (low, high): (usize, usize), by: usize, left: bool) {
        let len = high.wrapping_sub(low).wrapping_add(1);
        if high < low || high >= self.slots.len() || by > len {
            return;
        }
        let (front, _) = self.as_mut_slices();
        if low < front.len() && high >= front.len() {
            // Across the ring's end: three reversals in place.
            crate::budget::spend(len.saturating_mul(2));
            let by = if left { by } else { len.saturating_sub(by) };
            let (mid, end) = (low.saturating_add(by), high.saturating_add(1));
            self.reverse(low, mid);
            self.reverse(mid, end);
            self.reverse(low, end);
            return;
        }
        let (front, back) = self.as_mut_slices();
        let split = front.len();
        let run = match (low.checked_sub(split), high.checked_sub(split)) {
            (Some(low), Some(high)) => back.get_mut(low..=high),
            _ => front.get_mut(low..=high),
        };
        if let Some(run) = run {
            crate::budget::spend(run.len());
            if left {
                run.rotate_left(by);
            } else {
                run.rotate_right(by);
            }
        }
    }
    /// Reverses the slots of rows `low..end`.
    fn reverse(&mut self, mut low: usize, mut end: usize) {
        while low < end {
            end = end.wrapping_sub(1);
            if let (Some(a), Some(b)) = (self.at(low), self.at(end))
                && let Ok([a, b]) = self.slots.get_disjoint_mut([a, b])
            {
                std::mem::swap(a, b);
            }
            low = low.wrapping_add(1);
        }
    }
    /// The top row's slot becomes the bottom row's.
    #[inline]
    fn turn(&mut self) {
        let top = self.top.wrapping_add(1);
        self.top = if top >= self.slots.len() { 0 } else { top };
    }
    fn as_mut_slices(&mut self) -> (&mut [usize], &mut [usize]) {
        let top = self.top.min(self.slots.len());
        match self.slots.split_at_mut_checked(top) {
            Some((before, from)) => (from, before),
            None => (&mut [], &mut []),
        }
    }
    fn iter(&self) -> impl Iterator<Item = &usize> {
        (0..self.len()).filter_map(|row| self.get(row))
    }
}

closed! {
    /// What ED and EL erase: from the cursor to the end, from the start to
    /// the cursor, or all.
    #[derive(Clone, Copy, Debug, PartialEq, Eq)]
    pub(crate) enum Erase {
        /// Its parameter.
        pub(crate) fn number(self) -> u16;
        /// The cursor's cell and what follows.
        Below => 0,
        /// What comes before, and the cursor's cell.
        Above => 1,
        /// All of it.
        All => 2,
    }
}

/// The most flag sets a kitty keyboard stack holds; a push onto a full stack
/// drops the oldest, as kitty does, so a runaway program cannot grow it.
const KEYBOARD_STACK_LIMIT: usize = 32;

/// One screen's kitty keyboard protocol flag stack. The primary and
/// alternate screens each keep one, so a program that pushes flags on the
/// alternate screen and exits without popping them leaves the shell's alone.
#[derive(Clone, Debug, Default)]
pub(crate) struct KeyboardStack(VecDeque<u8>);

impl KeyboardStack {
    pub(crate) fn top(&self) -> u8 {
        self.0.back().copied().unwrap_or(0)
    }
    pub(crate) fn push(&mut self, flags: u8) {
        if self.0.len() >= KEYBOARD_STACK_LIMIT {
            self.0.pop_front();
        }
        self.0.push_back(flags);
    }
    pub(crate) fn pop(&mut self, count: u16) {
        self.0
            .truncate(self.0.len().saturating_sub(usize::from(count)));
    }
    /// `CSI = flags ; mode u`: mode 1 (the default) replaces the top's
    /// flags, 2 adds to them and 3 removes from them; an empty stack's are 0.
    pub(crate) fn set(&mut self, flags: u8, mode: u16) {
        let top = self.top();
        self.pop(1);
        self.push(match mode {
            2 => top | flags,
            3 => top & !flags,
            _ => flags,
        });
    }
}

/// A grid: its screen, rows of cells in slots ordered by `order`, and its
/// history (`history.rs`). Retained rows are indexed history first, oldest
/// first, then the screen from its top.
#[derive(Clone, Debug)]
pub(crate) struct Grid {
    cells: Vec<Compact>,
    meta: Vec<Meta>,
    /// Each slot's text too long for its cells to hold inline.
    texts: Vec<Text>,
    /// Each slot's cells' links: none for most, which costs no memory.
    linked: Vec<RowLinks>,
    /// The cells' links that have an `id`, and what the links cost.
    pub links: Links,
    /// The cells' styles: shared with the grid a resize makes from this
    /// one, until either adds a style.
    styles: Arc<Styles>,
    /// Styles found lately, by a hash of their attributes (`recent`), so a
    /// program switching between colours skips the table search. An empty
    /// entry holds the default attributes, which are no style of the table.
    recent: [(Attributes, u32); RECENT],
    /// How many sweeps have renumbered the styles: a number found before
    /// one may name another style after it.
    sweeps: u64,
    order: Order,
    history: History,
    size: Size,
    pub cursor: Cursor,
    /// What DECSC saved, which DECRC restores.
    pub saved: Saved,
    /// The top and bottom margins (DECSTBM): the scrolling region.
    pub lines: Span,
    /// The left and right margins (DECSLRM, with DECLRMM set), set with
    /// `set_columns` alone, which keeps `lr`.
    columns: Span,
    /// What may end a line before the screen's edge: `LR`, `columns` is
    /// narrower than the screen; `DOUBLED`, a row may be double width
    /// (cleared when every row is single width again). One test covers
    /// both on every operation they bound.
    narrow: u8,
    /// The screen's cells are yet to be made (`Grid::unmade`).
    unmade: bool,
    /// The kitty keyboard flags the program on this screen pushed.
    pub keyboard: KeyboardStack,
}

/// Which way a scroll moves rows; going up, rows leaving the top go into
/// history if `history` and the region is the whole screen.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum Scroll {
    Up { history: bool },
    Down,
}

/// Takes the next identity: 64 bits counted up by one do not run out, so
/// none is ever reused.
pub(crate) fn next_id(next: &mut u64) -> RowId {
    let id = *next;
    *next = next.wrapping_add(1);
    RowId(id)
}

impl Grid {
    pub(crate) fn history_bytes(&self) -> usize {
        self.history.budget()
    }

    pub(crate) fn clear_history(&mut self) {
        self.history.clear();
    }

    pub(crate) fn set_history_bytes(&mut self, bytes: usize) {
        self.history.set_budget(bytes);
    }

    /// Whether a screen of `size` is more than a grid holds.
    pub fn check_size(size: Size) -> Result<(), Error> {
        (size.cells() <= MAX_CELLS)
            .then_some(())
            .ok_or(Error::Capacity)
    }

    pub fn new(
        size: Size,
        history_bytes: usize,
        next: &mut u64,
        version: u64,
    ) -> Result<Self, Error> {
        Self::made(size, history_bytes, next, version, false)
    }

    /// `new`, but with no storage for the screen's cells until `make`.
    /// Rows read as blank, and resizing and clearing keep it unmade;
    /// anything else needs a made grid. The alternate screen starts so, as
    /// most programs never show it.
    pub fn unmade(size: Size, next: &mut u64, version: u64) -> Result<Self, Error> {
        Self::made(size, 0, next, version, true)
    }

    /// Gives an unmade grid its blank cells; on failure it stays unmade.
    pub fn make(&mut self) -> Result<(), Error> {
        if !self.unmade {
            return Ok(());
        }
        let size = self.size.cells();
        crate::budget::spend(size);
        reserve(&mut self.cells, size)?;
        self.cells.resize(size, BLANK);
        self.unmade = false;
        Ok(())
    }

    fn made(
        size: Size,
        history_bytes: usize,
        next: &mut u64,
        version: u64,
        unmade: bool,
    ) -> Result<Self, Error> {
        Self::check_size(size)?;
        let mut grid = Self {
            unmade,
            ..Self::bare(size, history_bytes)
        };
        grid.reserve_screen()?;
        for _ in 0..size.rows() {
            let id = next_id(next);
            grid.push_screen_row(Meta::new(id, version, false, 0), &[], None, None);
        }
        Ok(grid)
    }

    fn bare(size: Size, history_bytes: usize) -> Self {
        Self {
            cells: Vec::new(),
            meta: Vec::new(),
            texts: Vec::new(),
            linked: Vec::new(),
            links: Links::default(),
            styles: Arc::default(),
            recent: [(Attributes::default(), 0); RECENT],
            sweeps: 0,
            order: Order::default(),
            history: History::new(history_bytes),
            size,
            cursor: Cursor::default(),
            saved: Saved::default(),
            lines: size.lines(),
            columns: size.columns(),
            narrow: 0,
            unmade: false,
            keyboard: KeyboardStack::default(),
        }
    }

    /// An empty grid to take this one's place, sharing its styles and their
    /// numbers; unmade if this one is.
    fn successor(&self, size: Size) -> Self {
        let mut next = Self {
            styles: Arc::clone(&self.styles),
            recent: self.recent,
            unmade: self.unmade,
            ..Self::bare(size, self.history_bytes())
        };
        // Its history is charged for the shared styles (B11).
        let cells = if self.unmade { 0 } else { size.cells() };
        next.charge_styles(cells);
        next
    }

    /// Reserves exactly the screen's rows; their cells only once made.
    fn reserve_screen(&mut self) -> Result<(), Error> {
        // Within the limits: `check_size` came first.
        let rows = usize::from(self.size.rows());
        let cells = if self.unmade { 0 } else { self.size.cells() };
        reserve(&mut self.cells, cells)?;
        reserve(&mut self.meta, rows)?;
        reserve(&mut self.texts, rows)?;
        reserve(&mut self.linked, rows)?;
        self.order
            .try_reserve_exact(rows.saturating_sub(self.order.len()))?;
        Ok(())
    }

    /// Adds a row as a resize rebuilds the grid: into history if `history`,
    /// else onto the screen. Its cells past `written` are blank.
    fn keep_row(
        &mut self,
        meta: Meta,
        (cells, written): (&[Compact], usize),
        (text, links): (Option<Text>, Option<RowLinks>),
        history: bool,
    ) -> Result<(), Error> {
        if !history {
            self.push_screen_row(meta, cells, text, links);
            return Ok(());
        }
        let width = u16::try_from(cells.len()).map_err(|_| Error::Capacity)?;
        let kept = trimmed(cells.get(..written).unwrap_or(cells));
        crate::budget::spend(kept.len().saturating_add(1));
        self.history.push(meta.arriving(kept, width))?;
        let text = text.filter(|text| !text.is_empty());
        let links = links.and_then(|mut links| KeptLinks::take(&mut links));
        if text.is_some() || links.is_some() {
            self.history.attach(text.map(|text| text.exact()), links);
        }
        self.history.trim();
        Ok(())
    }

    /// Adds a row below the screen's rows in a new slot, its cells past
    /// `cells` blank. Room was reserved (`reserve_screen`).
    fn push_screen_row(
        &mut self,
        meta: Meta,
        cells: &[Compact],
        text: Option<Text>,
        links: Option<RowLinks>,
    ) {
        let slot = self.meta.len();
        let cols = usize::from(self.size.cols());
        crate::budget::spend(if self.unmade {
            1
        } else {
            cols.saturating_add(1)
        });
        if !self.unmade {
            let start = self.cells.len();
            self.cells
                .extend_from_slice(cells.get(..cells.len().min(cols)).unwrap_or_default());
            self.cells.resize(start.saturating_add(cols), BLANK);
        }
        if meta.rendition().is_double() {
            self.narrow |= DOUBLED;
        }
        self.meta.push(meta);
        self.texts.push(text.unwrap_or_default());
        self.linked.push(links.unwrap_or_default());
        self.order.push_back(slot);
    }

    pub fn size(&self) -> Size {
        self.size
    }
    /// The attributes of the cells' styles.
    pub fn styles(&self) -> &Styles {
        &self.styles
    }
    pub fn history_len(&self) -> usize {
        self.history.len()
    }
    /// The cursor's row, counted from the oldest row history keeps.
    pub(crate) fn cursor_index(&self) -> Option<usize> {
        self.history_len()
            .checked_add(usize::from(self.cursor.row()))
    }
    pub fn retained_len(&self) -> usize {
        self.history.len().saturating_add(self.order.len())
    }

    fn cells_of(&self, slot: usize) -> Option<Range<usize>> {
        let cols = usize::from(self.size.cols());
        let start = slot.checked_mul(cols)?;
        Some(start..start.checked_add(cols)?)
    }
    fn slice(&self, slot: usize) -> &[Compact] {
        self.cells_of(slot)
            .and_then(|cells| self.cells.get(cells))
            .unwrap_or(&[])
    }
    fn slice_mut(&mut self, slot: usize) -> &mut [Compact] {
        match self.cells_of(slot) {
            Some(cells) => self.cells.get_mut(cells).unwrap_or(&mut []),
            None => &mut [],
        }
    }
    fn line(&mut self, slot: usize) -> Option<Line<'_>> {
        let cells = self.cells_of(slot)?;
        Some(Line {
            cells: self.cells.get_mut(cells)?,
            text: self.texts.get_mut(slot)?,
        })
    }
    fn slot_row(&self, slot: usize, row: usize) -> Option<Row<'_>> {
        let m = self.meta.get(slot)?;
        Some(Row {
            grid: self,
            // Exact: rows are far fewer than a usize holds.
            index: self.history.len().saturating_add(row),
            id: m.id,
            version: m.version,
            flags: m.flags,
            cells: self.slice(slot),
            width: usize::from(self.size.cols()),
            text: self.texts.get(slot)?,
            links: (self.linked.get(slot).filter(|links| !links.is_empty())).map(RowLinks::runs),
        })
    }
    pub fn row_at(&self, index: usize) -> Option<Row<'_>> {
        crate::budget::spend(1);
        let Some(row) = index.checked_sub(self.history.len()) else {
            let found = self.history.get(index)?;
            return Some(Row {
                grid: self,
                index,
                id: found.kept.id,
                version: found.kept.version,
                flags: found.flags,
                cells: found.cells,
                width: usize::from(found.kept.width()),
                text: found.text,
                links: found.links,
            });
        };
        self.slot_row(*self.order.get(row)?, row)
    }
    /// The retained index of the row with identity `id`, searching the
    /// screen first, then history.
    pub fn index_of(&self, id: RowId) -> Option<usize> {
        self.screen_index_of(id)
            .or_else(|| self.history.position(id))
    }
    /// `index_of`, for the screen's rows only.
    pub(crate) fn screen_index_of(&self, id: RowId) -> Option<usize> {
        let row =
            (self.order.iter()).position(|slot| self.meta.get(*slot).is_some_and(|m| m.id == id));
        crate::budget::spend(row.map_or(self.order.len(), |row| row.saturating_add(1)));
        row.and_then(|row| row.checked_add(self.history.len()))
    }
    /// See `Screen::track_pictures`.
    pub(crate) fn followed(&self) -> &crate::history::Noted {
        self.history.followed()
    }
    /// The numbers of history's oldest row kept and of the next to come.
    pub(crate) fn history_numbers(&self) -> (u64, u64) {
        self.history.numbers()
    }
    /// The index of history row `number`, if it is kept and is row `id`.
    pub(crate) fn index_of_number(&self, number: u64, id: RowId) -> Option<usize> {
        crate::budget::spend(1);
        let index = self.history.index_of_number(number)?;
        (self.history.kept(index)?.id == id).then_some(index)
    }
    pub(crate) fn id_of_number(&self, number: u64) -> Option<RowId> {
        let index = self.history.index_of_number(number)?;
        Some(self.history.kept(index)?.id)
    }
    /// Whether screen row `row` is soft-wrapped, without making its `Row`.
    pub fn live_wrapped(&self, row: u16) -> bool {
        self.slot(row)
            .and_then(|slot| self.meta.get(slot))
            .is_some_and(Meta::wrapped)
    }
    pub fn live_row(&self, row: u16) -> Option<Row<'_>> {
        self.slot_row(self.slot(row)?, usize::from(row))
    }
    #[inline]
    fn slot(&self, row: u16) -> Option<usize> {
        self.order.get(usize::from(row)).copied()
    }
    /// A live row's cell, without making its `Row`.
    pub fn cell(&self, row: u16, col: u16) -> Option<CellRef<'_>> {
        let slot = self.slot(row)?;
        let cell = self.slice(slot).get(usize::from(col))?;
        Some(cell.read(self.texts.get(slot)?, &self.styles))
    }
    /// A live row's cells as stored.
    pub fn live_cells(&self, row: u16) -> &[Compact] {
        self.slot(row).map_or(&[], |slot| self.slice(slot))
    }
    /// A live row's cell as stored.
    #[inline]
    pub fn stored(&self, row: u16, col: u16) -> Option<&Compact> {
        self.slice(self.slot(row)?).get(usize::from(col))
    }
    /// Edits a live row's cells with `f`, which says whether it changed
    /// any; only then does the row take `version`. Every non-blank cell
    /// `f` makes is before `end`, which the row's `used` mark is raised to.
    pub fn mutate_row(
        &mut self,
        row: u16,
        version: u64,
        end: u16,
        f: impl FnOnce(&mut [Compact]) -> bool,
    ) {
        self.mutate_row_to(row, version, |cells| f(cells).then_some(end));
    }
    /// `mutate_row` where `f` returns the end, or `None` if it changed
    /// nothing.
    #[inline]
    pub fn mutate_row_to(
        &mut self,
        row: u16,
        version: u64,
        f: impl FnOnce(&mut [Compact]) -> Option<u16>,
    ) {
        if let Some(slot) = self.slot(row)
            && let Some(end) = f(self.slice_mut(slot))
        {
            self.changed(slot, end.into(), version);
        }
    }
    /// The row in `slot` takes `version` and its `used` mark is raised to
    /// `end`, capped at the width.
    #[inline]
    fn changed(&mut self, slot: usize, end: usize, version: u64) {
        let end = u16::try_from(end).unwrap_or(u16::MAX).min(self.size.cols());
        if let Some(m) = self.meta.get_mut(slot) {
            m.version = version;
            m.used = m.used.max(end);
        }
    }
    /// Writes the ASCII `run` at `col` of live row `row` in `style`, taking
    /// `version` only if a cell changed. A wide glyph half
    /// overwritten loses its other half (`put`).
    #[inline]
    pub fn write_ascii(&mut self, row: u16, col: u16, run: &[u8], style: u32, version: u64) {
        let Some(slot) = self.slot(row) else {
            return;
        };
        crate::budget::spend(run.len());
        let start = usize::from(col);
        let end = start.saturating_add(run.len());
        let Some(dst) = self.slice_mut(slot).get_mut(start..end) else {
            return;
        };
        // Check a short run cell by cell; fold the half flags of a long run
        // together, whose set-up a short one would not repay.
        let halves = if run.len() < LONG_RUN {
            dst.iter().any(|c| c.is_wide() || c.is_wide_continuation())
        } else {
            Compact::any_halves(dst)
        };
        if halves {
            let cells = self.slice_mut(slot);
            let mut changed = false;
            for (i, &byte) in (start..).zip(run) {
                changed |= put(cells, i, Compact::ascii(byte, style), 1);
            }
            if changed {
                self.changed(slot, end, version);
            }
            return;
        }
        // A redraw of the same cells leaves the row's version alone; new
        // text usually differs at the first cell.
        let first = dst.first().zip(run.first());
        if first.is_some_and(|(c, b)| c.is_ascii(*b, style)) && unchanged(dst, run, style) {
            return;
        }
        for (cell, byte) in dst.iter_mut().zip(run) {
            *cell = Compact::ascii(*byte, style);
        }
        self.changed(slot, end, version);
    }
    /// `mutate_row` with the row's text too, for edits that store clusters.
    pub fn mutate_line(
        &mut self,
        row: u16,
        version: u64,
        end: u16,
        f: impl FnOnce(&mut Line<'_>) -> bool,
    ) {
        if let Some(slot) = self.slot(row)
            && let Some(mut line) = self.line(slot)
            && f(&mut line)
        {
            self.changed(slot, end.into(), version);
        }
    }
    /// Gives the cells `span` of live row `row` the open link `pen`, or
    /// none, interning it the first time; with no room for it, the cells
    /// get none. Out of line: printing calls it per glyph once a link is
    /// open.
    #[inline(never)]
    pub(crate) fn print_link(
        &mut self,
        row: u16,
        span: Range<usize>,
        pen: Option<&Hyperlink>,
        version: u64,
    ) {
        let link = match pen {
            None => None,
            Some(pen) => {
                if let Held::Pending = self.links.open {
                    self.open(pen, version);
                }
                match &self.links.open {
                    Held::At(link) => Some(link),
                    Held::Pending | Held::Refused => None,
                }
            }
        };
        let slot = self
            .slot(row)
            .filter(|_| span.end <= usize::from(self.size.cols()));
        set_link(&mut self.meta, &mut self.linked, slot, span, link, version);
    }
    #[cold]
    #[inline(never)]
    fn open(&mut self, pen: &Hyperlink, version: u64) {
        let link = self.intern(pen, version);
        self.links.open = link.map_or(Held::Refused, Held::At);
    }
    pub(crate) fn set_link(
        &mut self,
        row: u16,
        span: Range<usize>,
        link: Option<&Hyperlink>,
        version: u64,
    ) {
        let slot = self
            .slot(row)
            .filter(|_| span.end <= usize::from(self.size.cols()));
        set_link(&mut self.meta, &mut self.linked, slot, span, link, version);
    }

    /// The style number for `attributes`, adding it to the table if needed.
    /// Adding may sweep and renumber every cell's style: numbers got before
    /// this call are invalid after it.
    pub fn style(&mut self, attributes: Attributes) -> u32 {
        match attributes.inline_style() {
            Some(style) => style,
            None => self.table_style(attributes),
        }
    }

    /// `style` for attributes with no inline number
    /// (`Attributes::inline_style`), checking the recent styles first.
    #[inline]
    pub fn table_style(&mut self, attributes: Attributes) -> u32 {
        let at = recent(attributes);
        if let Some(&(seen, id)) = self.recent.get(at)
            && seen == attributes
        {
            return id;
        }
        self.find_table_style(attributes, at)
    }

    /// `table_style`'s slow path, out of line so `table_style` stays small
    /// where inlined.
    #[inline(never)]
    fn find_table_style(&mut self, attributes: Attributes, at: usize) -> u32 {
        crate::budget::spend(1);
        let id = match self.styles.find(attributes) {
            Some(id) => id,
            None => {
                crate::budget::spend(1);
                let cells = self.cells.len().saturating_add(self.history.kept_cells());
                let mut swept = self.styles.wants_sweep(cells);
                if swept {
                    self.sweep();
                }
                // The table's bytes change only as it grows, sweeps, or is
                // copied from a shared clone: history makes room first, and
                // if it can't, the styles only it held are swept too.
                let copied = Arc::get_mut(&mut self.styles).is_none();
                Arc::make_mut(&mut self.styles);
                let grows = self.styles.full();
                if grows && !self.make_style_room() {
                    self.sweep();
                    swept = true;
                }
                // After a sweep there is room: the styles in use are at
                // most the cells, far fewer than the table holds.
                let id = Arc::make_mut(&mut self.styles)
                    .insert(attributes)
                    .unwrap_or(0);
                if grows || swept || copied {
                    self.charge_styles(self.cells.len());
                }
                id
            }
        };
        if let Some(slot) = self.recent.get_mut(at) {
            *slot = (attributes, id);
        }
        id
    }

    /// Charges the style table's bytes beyond the share of a screen of
    /// `cells` cells to history's budget (`THREATS.md` B11).
    fn charge_styles(&mut self, cells: usize) {
        let share = crate::style::screen_share(cells);
        self.history
            .charge(self.styles.bytes().saturating_sub(share));
    }

    /// `charge_styles` for the table with one more style (`THREATS.md`
    /// B11); whether it fits the budget, even with no rows.
    fn make_style_room(&mut self) -> bool {
        let share = crate::style::screen_share(self.cells.len());
        let after = self.styles.bytes_after_insert().saturating_sub(share);
        self.history.charge(after);
        after <= self.history.budget()
    }

    /// Keeps only the styles cells use, renumbering them and every cell
    /// (`style.rs`).
    #[cold]
    #[inline(never)]
    pub(crate) fn sweep(&mut self) {
        // Every cell and history row is read twice: to mark its style, then
        // to renumber it; and each style kept or let go, twice.
        let cells = (self.cells.len())
            .saturating_add(self.history.kept_cells())
            .saturating_add(self.history.len())
            .saturating_add(self.styles.len());
        crate::budget::spend(cells.saturating_mul(2));
        let mut used = vec![false; self.styles.len()];
        let mut mark = |cell: &Compact| {
            if let Some(mark) = Styles::place(cell.style()).and_then(|i| used.get_mut(i)) {
                *mark = true;
            }
        };
        self.cells.iter().for_each(&mut mark);
        self.history.each_cell_mut(|cell| mark(cell));
        let renumber = Arc::make_mut(&mut self.styles).retain(&used);
        let mut renumbered = |cell: &mut Compact| {
            if let Some(&new) = Styles::place(cell.style()).and_then(|i| renumber.get(i)) {
                cell.set_style(new);
            }
        };
        self.cells.iter_mut().for_each(&mut renumbered);
        self.history.each_cell_mut(renumbered);
        self.recent = [(Attributes::default(), 0); RECENT];
        self.sweeps = self.sweeps.wrapping_add(1);
    }

    pub(crate) fn sweeps(&self) -> u64 {
        self.sweeps
    }

    #[cfg(test)]
    pub(crate) fn style_count(&self) -> usize {
        self.styles.len()
    }

    /// Forgets every link: RIS.
    pub fn reset_links(&mut self) {
        crate::budget::spend(self.linked.len());
        self.linked.iter_mut().for_each(RowLinks::clear);
        self.history.reset_links();
        self.links = Links::default();
    }

    /// The grid's link for `opened`, made if no row has it. With no room,
    /// room is made (`make_room`), rate-limited after a failure; `None` if
    /// there is still none.
    pub fn intern(&mut self, opened: &Hyperlink, version: u64) -> Option<Hyperlink> {
        if opened.id().is_some()
            && let Some(link) = self.links.find(opened)
        {
            return Some(link.clone());
        }
        if let Some(link) = self.links.insert(opened) {
            return Some(link);
        }
        if !self.links.may_make_room() {
            return None;
        }
        self.make_room(version);
        let link = self.links.insert(opened);
        if link.is_none() {
            self.links.stall();
        }
        link
    }

    /// Makes room for links (`Links::sweep`): if they cost more than half
    /// the bound, the oldest history rows lose theirs, taking `version`,
    /// until the rest cost at most half. Screen rows never lose links.
    fn make_room(&mut self, version: u64) {
        // Oldest first: history, then the screen top to bottom.
        let (linked, history, order) = (&self.linked, &self.history, &self.order);
        let rows = || {
            let history = history.links().map(|(index, links)| (Some(index), links));
            let screen = (order.iter()).filter_map(|slot| Some((None, linked.get(*slot)?.runs())));
            history.chain(screen)
        };
        // The screen's rows, walked twice; history's charge themselves.
        crate::budget::spend(self.order.len().saturating_mul(2));
        let lose = self.links.sweep(rows, self.history.len());
        for index in 0..lose {
            self.history.unlink(index, version);
        }
    }

    /// Marks live row `row` as where a prompt starts (OSC 133 ; A), or
    /// not. A change gives the row `version` (`THREATS.md` section 6, 28),
    /// so a host redraws what shows the marks.
    pub fn set_prompt(&mut self, row: u16, prompt: bool, version: u64) {
        self.set_flag(row, PROMPT, prompt, version);
    }

    /// Sets or clears `flag` on live row `row` (`Meta::set`), giving the
    /// row `version` if that changes it.
    fn set_flag(&mut self, row: u16, flag: u8, on: bool, version: u64) {
        if let Some(slot) = self.slot(row)
            && let Some(m) = self.meta.get_mut(slot)
            && (m.flags & flag != 0) != on
        {
            m.version = version;
            m.set(flag, on);
        }
    }

    /// Ends live row `row`'s line though it stays soft-wrapped (`ENDS`).
    fn end_line(&mut self, row: u16, version: u64) {
        if let Some(slot) = self.slot(row)
            && let Some(m) = self.meta.get_mut(slot)
            && m.continues()
        {
            m.version = version;
            m.flags |= ENDS;
        }
    }

    /// `wrap` for a glyph wrapping off live row `row`: also clears `ENDS`.
    pub(crate) fn wrap_onto(&mut self, row: u16, wrapped: bool, version: u64) {
        if let Some(slot) = self.slot(row)
            && let Some(m) = self.meta.get_mut(slot)
        {
            let flags = (m.flags & !(WRAPPED | ENDS)) | if wrapped { WRAPPED } else { 0 };
            if flags != m.flags {
                m.version = version;
                m.flags = flags;
            }
        }
    }

    pub fn wrap(&mut self, row: u16, wrapped: bool, version: u64) {
        self.set_flag(row, WRAPPED, wrapped, version);
    }
    /// Blanks columns `start` to `end` of live row `row` in `style`, with
    /// the other half of a wide glyph the span splits. The row takes
    /// `version` only if a cell changed.
    ///
    /// Cells past the `used` mark are already default blanks, so a default
    /// erase (tmux sends an EL for nearly every line) reads only cells
    /// before it, and one reaching it lowers the mark to `start`: erasing
    /// or recycling the row again costs nothing.
    ///
    /// A whole row erased ends the line above (or history's newest, above
    /// the top row), which no longer goes on in it, so a line editor
    /// redrawing its prompt there (bash after a resize) starts a line of
    /// its own. The row above keeps its soft wrap, as xterm's `ClearRight`
    /// and `ScrnClearLines` do; its line just ends (`ENDS`,
    /// `Row::continues`).
    pub fn erase(&mut self, row: u16, start: u16, end: u16, style: u32, version: u64) {
        if self.blank(row, start, end, style, version) {
            match row.checked_sub(1) {
                Some(above) => self.end_line(above, version),
                None => self.history.end_newest_line(version),
            }
        }
    }

    /// `erase`, but a line going on into a whole row still does: whether
    /// a whole row changed.
    fn blank(&mut self, row: u16, start: u16, end: u16, style: u32, version: u64) -> bool {
        let (cols, last) = (self.size.cols(), self.size.columns().last());
        let mut clears_edge = end >= cols;
        let Some(slot) = self.slot(row) else {
            return false;
        };
        let Some(&Meta { used, .. }) = self.meta.get(slot) else {
            return false;
        };
        let plain = style == 0;
        let span_end = usize::from(end.min(cols));
        let first = usize::from(start);
        // In the default style, none past the mark needs blanking.
        let reach = if plain {
            span_end.min(usize::from(used))
        } else {
            span_end
        };
        let blank = Compact::blank(style);
        crate::budget::spend(reach.saturating_sub(first));
        let cells = self.slice_mut(slot);
        // Already all blank: the row is unchanged, and a blank is never
        // half a wide glyph, so nothing needs repair.
        let differs = cells
            .get(first..reach)
            .and_then(|run| run.iter().position(|c| !c.is_blank(style)));
        let changed = differs.is_some();
        if let Some(differs) = differs {
            // A wide glyph across either end loses its other half too,
            // which keeps its attributes. Halves are always adjacent
            // (`repair_wide`).
            if let Some(cell) = cells.get(first)
                && !cell.is_wide()
                && cell.is_wide_continuation()
                && let Some(other) = cells.get_mut(first.wrapping_sub(1))
            {
                *other = other.blanked();
            }
            if cells
                .get(reach.wrapping_sub(1))
                .is_some_and(Compact::is_wide)
            {
                if let Some(other) = cells.get_mut(reach) {
                    *other = other.blanked();
                }
                // The glyph's second half is in the last column.
                clears_edge |= reach == usize::from(last);
            }
            if let Some(run) = cells.get_mut(first.saturating_add(differs)..reach) {
                run.fill(blank);
            }
        }
        if let Some(m) = self.meta.get_mut(slot) {
            if changed {
                m.version = version;
                if !plain {
                    m.used = m.used.max(end.min(cols));
                }
            }
            if plain && span_end >= usize::from(m.used) {
                m.used = m.used.min(start);
            }
            if clears_edge && m.wrapped() {
                m.set(WRAPPED, false);
                m.version = version;
            }
        }
        // A whole row erased keeps no text or links.
        let whole = start == 0 && end >= cols;
        if whole {
            if let Some(text) = self.texts.get_mut(slot) {
                text.release();
            }
            if !self.links.none()
                && let Some(rows) = self.size.lines().starting_at(row)
            {
                self.unlink_rows(rows, 1, false);
            }
        }
        whole && changed
    }

    /// ED (`display`) or EL of `part`, ending a pending wrap. `erase` erases
    /// each span and returns whether a whole line may become single width.
    /// Whole rows ED erases lose their prompt marks, as in Ghostty; EL and
    /// the cursor's row keep them (a shell redrawing its prompt).
    #[inline]
    pub fn erase_in(
        &mut self,
        display: bool,
        part: Erase,
        version: u64,
        mut erase: impl FnMut(&mut Self, u16, u16, u16) -> bool,
    ) {
        self.cursor.pending_wrap = false;
        let ((row, col), cols) = (self.cursor.at(), self.size.cols());
        let mut kept = false;
        if display {
            // ED spends a unit a row; each row's erase, its cells.
            crate::budget::spend(self.size.rows());
            for y in 0..self.size.rows() {
                let whole = match part {
                    Erase::Below => y > row,
                    Erase::Above => y < row,
                    Erase::All => true,
                };
                if whole {
                    let reset = erase(self, y, 0, cols);
                    kept |= !reset;
                    self.set_prompt(y, false, version);
                    if reset && self.doubled() {
                        self.set_rendition(y, LineRendition::Single, version);
                    }
                }
            }
        }
        let (start, end) = match part {
            Erase::Below => (col, cols),
            Erase::Above => (0, col.saturating_add(1).min(cols)),
            Erase::All => (0, cols),
        };
        let reset = erase(self, row, start, end);
        // ED makes a line it erases whole single width (DEC STD 070, ED,
        // note 1, and its algorithm); EL leaves it as it is.
        if display && reset && self.doubled() && start == 0 && end >= self.row_end(row) {
            self.set_rendition(row, LineRendition::Single, version);
            if part == Erase::All && !kept {
                self.narrow &= !DOUBLED;
            }
        }
    }

    /// `erase`, skipping glyphs guarded by SPA, as xterm's `ClearInLine2`
    /// does. A wide glyph's second half is guarded with its first. Out of
    /// line, as protection is rare.
    #[inline(never)]
    pub fn erase_unprotected(&mut self, row: u16, start: u16, end: u16, style: u32, version: u64) {
        let end = end.min(self.size.cols());
        let (cells, styles) = (self.live_cells(row), &self.styles);
        let kept = |c: &Compact| styles.guarded(c.style());
        let protected = |at: usize| {
            cells.get(at).is_some_and(|c| {
                kept(c)
                    || c.is_wide_continuation() && cells.get(at.wrapping_sub(1)).is_some_and(kept)
            })
        };
        crate::budget::spend(end.saturating_sub(start));
        let mut runs = Vec::new();
        let mut from = start;
        for col in start..end {
            if protected(usize::from(col)) {
                if from < col {
                    runs.push((from, col));
                }
                from = col.saturating_add(1);
            }
        }
        if from < end {
            runs.push((from, end));
        }
        for (from, to) in runs {
            self.erase(row, from, to, style, version);
        }
    }

    /// DECFRA: fills columns `start` to `end` of live row `row` with the
    /// narrow glyph `c` in `style`, with no link, blanking first so a
    /// wide glyph the span cuts is blanked. It changes no line (DEC STD
    /// 070, 5.12.1): one going on into the row still does.
    pub fn fill(&mut self, row: u16, (start, end): (u16, u16), c: char, style: u32, version: u64) {
        self.blank(row, start, end, style, version);
        let span = usize::from(start)..usize::from(end);
        crate::budget::spend(span.len());
        let glyph = Compact::glyph(c, 1, style);
        self.mutate_row(row, version, end, |cells| {
            cells
                .get_mut(span.clone())
                .map(|run| run.fill(glyph))
                .is_some()
        });
        self.set_link(row, span, None, version);
    }

    /// DECSERA, DECSED and DECSEL: each glyph DECSCA does not protect in
    /// columns `start` to `end` of live row `row` becomes a space, keeping
    /// its attributes; empty cells stay empty (DEC STD 070, each of them).
    /// With `all`, DECERA: every glyph becomes a space in that style,
    /// "clearing all character attributes", links too, and a wide glyph
    /// two spaces. A wide glyph the span cuts is blanked whole.
    pub fn erase_to_spaces(
        &mut self,
        row: u16,
        (start, end): (u16, u16),
        all: Option<u32>,
        version: u64,
    ) {
        let span = usize::from(start)..usize::from(end);
        crate::budget::spend(span.len());
        self.mutate_row(row, version, end, |cells| {
            // A protected glyph just before keeps its second half.
            let before = span.start.checked_sub(1).and_then(|i| cells.get(i));
            let mut kept = all.is_none() && before.is_some_and(Compact::is_protected);
            let mut changed = false;
            for cell in cells.get_mut(span.clone()).into_iter().flatten() {
                if cell.is_wide_continuation() {
                    if !kept {
                        *cell = all.map_or(cell.blanked(), |style| Compact::ascii(b' ', style));
                        changed = true;
                    }
                    continue;
                }
                kept = all.is_none() && cell.is_protected();
                if cell.has_contents() && !kept {
                    *cell = Compact::ascii(b' ', all.unwrap_or(cell.style()));
                    changed = true;
                }
            }
            repair_wide(cells);
            changed
        });
        if all.is_some() {
            self.set_link(row, usize::from(start)..usize::from(end), None, version);
        }
    }

    /// DECCARA and DECRARA on cells `start..end` of screen row `row`: each
    /// occupied cell takes the style `restyle` maps its own to, and with
    /// `fill` (DECSACE 2) each unoccupied one becomes a space in it first
    /// (DEC STD 070, DECSACE). Text, protection and links stay. `restyle`
    /// may sweep the table, so each cell is read only when its turn comes.
    pub fn restyle(
        &mut self,
        row: u16,
        (start, end): (u16, u16),
        fill: bool,
        version: u64,
        restyle: &mut impl FnMut(&mut Self, u32) -> u32,
    ) {
        let Some(slot) = self.slot(row) else {
            return;
        };
        let end = end.min(self.size.cols());
        crate::budget::spend(end.saturating_sub(start));
        let mut changed = false;
        for col in usize::from(start)..usize::from(end) {
            let Some(&cell) = self.slice(slot).get(col) else {
                break;
            };
            let occupied = cell.has_contents() || cell.is_wide_continuation();
            if !occupied && !fill {
                continue;
            }
            let style = restyle(self, cell.style());
            let Some(at) = self.slice_mut(slot).get_mut(col) else {
                break;
            };
            let new = if occupied {
                let mut new = *at;
                new.set_style(style);
                new
            } else {
                Compact::ascii(b' ', style)
            };
            if new != *at {
                *at = new;
                changed = true;
            }
        }
        if changed {
            self.changed(slot, usize::from(end), version);
        }
    }

    /// DECCRA: copies cells `columns` of rows `lines` to `to`, clipped to
    /// the screen (DEC STD 070, DECCRA); source and target may overlap. A
    /// wide glyph the copy cuts is blanked whole; copied cells get no link.
    pub fn copy_rectangle(&mut self, lines: Span, columns: Span, to: (u16, u16), version: u64) {
        let span = usize::from(columns.first())..usize::from(columns.end());
        let (row, col) = to;
        let end = col.saturating_add(columns.len()).min(self.size.cols());
        // Each source row is read before it is written over: a copy down
        // goes bottom up, otherwise top down.
        let mut rows: Vec<(u16, u16)> = lines.each().zip(row..self.size.rows()).collect();
        if row > lines.first() {
            rows.reverse();
        }
        for (from, y) in rows {
            // To a double-width line's end (DEC STD 070, 5.6.1).
            let end = end.min(self.row_end(y)).max(col);
            let to = usize::from(col)..usize::from(end);
            // Each cell read, then written.
            crate::budget::spend(to.len().saturating_mul(2));
            if let (Some(source), Some(target)) = (self.slot(from), self.slot(y)) {
                self.copy_cells((source, target), span.clone(), to.clone(), true, version);
            }
            self.set_link(y, to, None, version);
        }
    }

    /// Copies cells `span` of slot `source` over columns `to` of slot
    /// `target`, as many as fit, with their text. With `cuts` (DECCRA),
    /// wide glyphs cut at either edge are blanked; a margin scroll splits
    /// them first. If each cluster replaces one as long, its text is
    /// written in place, needing no room (B35); else the columns are
    /// cleared first (`clear_for`). The target takes `version` if a cell
    /// changed.
    fn copy_cells(
        &mut self,
        (source, target): (usize, usize),
        span: Range<usize>,
        to: Range<usize>,
        cuts: bool,
        version: u64,
    ) {
        let mut cells = self.slice(source).get(span).unwrap_or_default().to_vec();
        // The source's cells, however few of them land.
        crate::budget::spend(cells.len());
        // Cut where it lands too: at the screen's edge.
        cells.truncate(to.len());
        if cuts {
            cut(&mut cells);
        }
        unpicture(&mut cells);
        let text = self.texts.get(source);
        let texts: Vec<String> = (cells.iter())
            .map(|c| match (c.is_spilled(), text) {
                (true, Some(text)) => text.of(c).to_owned(),
                _ => String::new(),
            })
            .collect();
        let bytes = texts
            .iter()
            .map(String::len)
            .fold(0usize, usize::saturating_add);
        // The text copied, spent here up front.
        crate::budget::spend(bytes);
        let columns = to.start..to.start.saturating_add(cells.len());
        let mut changed = false;
        if let Some(mut line) = self.line(target) {
            let olds = line.cells.get(columns.clone()).unwrap_or_default();
            changed = (cells.iter().zip(olds).zip(&texts))
                .any(|((cell, old), text)| !old.holds(line.text, cell, text));
            let in_place = (cells.iter().zip(&texts).zip(columns.clone()))
                .all(|((cell, text), at)| !cell.is_spilled() || line.replaces(at, text.len()));
            if changed && !in_place {
                line.clear_for(columns.clone());
            }
            for ((at, cell), text) in columns.clone().zip(&cells).zip(&texts).filter(|_| changed) {
                if !cell.is_spilled() {
                    if let Some(slot) = line.cells.get_mut(at) {
                        *slot = *cell;
                    }
                } else if in_place {
                    line.replace(at, *cell, text);
                } else {
                    line.set_compacted(at, *cell, text);
                }
            }
            // The columns written and their neighbours, each read with its
            // own: a glyph beside them keeps the half past them.
            let edges = columns.start.saturating_sub(1)..columns.end.saturating_add(1);
            if changed && cuts {
                repair_wide_in(line.cells, edges);
            }
        }
        if changed {
            self.changed(target, columns.end, version);
        }
    }

    /// ICH and DCH at the cursor, which stays; they end a pending wrap
    /// (DEC STD 070, Appendix D.6.1). DCH ends the row's soft wrap; ICH and
    /// IRM insertion keep it, as xterm does. With left and right margins
    /// they edit up to the right margin, and outside them do nothing, the
    /// pending wrap staying too, as in xterm (`InsertChar`, `DeleteChar`;
    /// DEC STD 070, 5.4.3).
    pub fn edit_cells(&mut self, count: u16, insert: bool, blank: u32, version: u64) {
        if self.lr() && !self.in_columns() {
            return;
        }
        self.cursor.pending_wrap = false;
        let (row, col) = self.cursor.at();
        self.edit_row(row, col, count, insert, blank, version);
    }

    /// ICH or DCH of `count` cells at column `col` (between the margins)
    /// of live row `row`: for `edit_cells`, and DECIC and DECDC per row.
    pub fn edit_row(
        &mut self,
        row: u16,
        col: u16,
        count: u16,
        insert: bool,
        blank: u32,
        version: u64,
    ) {
        // The edit ends at the right margin, or a double-width row's end
        // (ICH and DCH, their algorithms).
        let end = self.columns.end().min(self.row_end(row));
        let count = usize::from(count.min(end.saturating_sub(col)));
        if count == 0 {
            return;
        }
        let cols = self.size.cols();
        let end = usize::from(end);
        crate::budget::spend(end.saturating_sub(usize::from(col)));
        let blank = Compact::blank(blank);
        self.mutate_row(row, version, cols, |row_cells| {
            let col = usize::from(col);
            // A wide glyph across the right margin loses both halves: the
            // edit would move one and not the other.
            split_wide(row_cells, end);
            let Some(cells) = row_cells.get_mut(..end) else {
                return false;
            };
            let len = cells.len();
            // Where the cells shifted out start.
            let shifted = if insert {
                len.checked_sub(count)
            } else {
                col.checked_add(count)
            };
            let Some(shifted) = shifted.filter(|_| count <= len) else {
                return false;
            };
            for boundary in [col, shifted] {
                split_wide(cells, boundary);
            }
            shift(cells, col, count, insert, blank);
            unpicture(cells.get_mut(col..).unwrap_or_default());
            repair_wide(row_cells);
            // Inserting and deleting always count as a change.
            true
        });
        if let Some(slot) = self.slot(row)
            && let Some(links) = self.linked.get_mut(slot)
            && !links.is_empty()
        {
            links.shift(usize::from(col), count, insert, end);
        }
        if !insert {
            self.wrap(row, false, version);
        }
    }

    /// Gives a slot a new blank row `id`, clearing only cells before its
    /// `used` mark. Callers drop its links, off the scroll's hot path.
    #[inline]
    fn recycle(&mut self, slot: usize, id: RowId, version: u64) {
        let mut used = usize::from(self.size.cols());
        if let Some(m) = self.meta.get_mut(slot) {
            used = usize::from(m.used);
            *m = Meta::new(id, version, false, 0);
        }
        crate::budget::spend(used);
        // A row taken into history leaves its slot blank (`take_row`).
        if used > 0 {
            let cells = self.slice_mut(slot);
            let used = used.min(cells.len());
            cells.get_mut(..used).unwrap_or_default().fill(BLANK);
        }
        if let Some(text) = self.texts.get_mut(slot) {
            text.empty();
        }
    }
    /// Drops the links of the `count` rows at the top of `region`, or its
    /// bottom if `bottom`. Out of line: most grids hold no links.
    #[cold]
    #[inline(never)]
    fn unlink_rows(&mut self, region: Span, count: u16, bottom: bool) {
        let (top, end) = (region.first(), region.last().saturating_add(1));
        let rows = if bottom {
            end.saturating_sub(count)..end
        } else {
            top..top.saturating_add(count)
        };
        for row in rows {
            if let Some(links) = self.slot(row).and_then(|slot| self.linked.get_mut(slot)) {
                links.clear();
            }
        }
    }

    /// Gives a new row's blank cells style `blank` (non-zero). Rows are
    /// first blanked in the all-zero default style, a memset; this second
    /// pass stays out of line so the two are never fused into one slower
    /// loop.
    #[inline(never)]
    fn colour(&mut self, slot: usize, blank: u32) {
        crate::budget::spend(self.size.cols());
        for cell in self.slice_mut(slot) {
            cell.set_style(blank);
        }
        if let Some(m) = self.meta.get_mut(slot) {
            m.used = self.size.cols();
        }
    }

    /// Scrolls by moving slots, not cells. Scrolling up a region at the
    /// top of the screen takes its rows into history, as xterm does (a
    /// status line below the region stays). New rows are blank in `blank`.
    #[inline]
    pub fn scroll(
        &mut self,
        region: Span,
        count: u16,
        direction: Scroll,
        blank: u32,
        next: &mut u64,
        version: u64,
    ) {
        if direction == (Scroll::Up { history: true })
            && region.first() == 0
            && self.history_bytes() > 0
        {
            let count = count.min(region.len());
            for done in 0..count {
                if self
                    .scroll_into_history(region.last(), blank, next, version)
                    .is_err()
                {
                    // No memory for history: scroll as without it
                    // (THREATS.md 1.3), counted.
                    crate::degraded();
                    let rest = count.saturating_sub(done);
                    let up = Scroll::Up { history: false };
                    self.scroll_region(region, rest, up, blank, next, version);
                    break;
                }
            }
            return;
        }
        self.scroll_region(region, count, direction, blank, next, version);
    }

    /// A scroll inside left and right margins (DEC STD 070, 5.4.3; xterm's
    /// `scrollInMargins`): the cells between the margins of `region` move
    /// `count` rows, those left behind blank in `blank`. Rows keep their
    /// identity, soft wrap and prompt mark, as xterm keeps a row's flags;
    /// only cells, text, links and versions change. A wide glyph across
    /// either margin loses both halves first, as in xterm. Nothing goes
    /// into history.
    pub fn scroll_columns(&mut self, region: Span, count: u16, up: bool, blank: u32, version: u64) {
        let (top, bottom, height) = (region.first(), region.last(), region.len());
        let count = count.min(height);
        if count == 0 {
            return;
        }
        let left = usize::from(self.columns.first());
        let end = usize::from(self.columns.end());
        crate::budget::spend(usize::from(height).saturating_mul(2));
        for y in top..=bottom {
            self.mutate_row(y, version, 0, |cells| {
                let mut changed = false;
                for edge in [left, end] {
                    changed |= edge > 0 && split_wide(cells, edge);
                }
                changed
            });
        }
        let span = left..end;
        // Copy in the order that reads each source before overwriting it.
        let moved = height.saturating_sub(count);
        for i in 0..moved {
            let (to, from) = if up {
                (
                    top.saturating_add(i),
                    top.saturating_add(i).saturating_add(count),
                )
            } else {
                let to = bottom.saturating_sub(i);
                (to, to.saturating_sub(count))
            };
            self.copy_span(from, to, span.clone(), version);
        }
        for i in 0..count {
            let y = if up {
                bottom.saturating_sub(i)
            } else {
                top.saturating_add(i)
            };
            self.blank_span(y, span.clone(), blank, version);
        }
    }

    /// Copies cells `span` of live row `from` into `to`, with text and
    /// links.
    fn copy_span(&mut self, from: u16, to: u16, span: Range<usize>, version: u64) {
        let (Some(source), Some(target)) = (self.slot(from), self.slot(to)) else {
            return;
        };
        // Each cell read, compared and written.
        crate::budget::spend(span.len().saturating_mul(3));
        self.copy_cells((source, target), span.clone(), span.clone(), false, version);
        // The source's links in the span, absolute, set in one go: the
        // row's others stay where they are.
        let runs: Vec<(Range<usize>, Hyperlink)> = (self.linked.get(source))
            .map(|links| {
                crate::budget::spend(links.runs().len());
                (links.runs().within(span.clone()))
                    .map(|(cells, link)| {
                        let start = cells.start.saturating_add(span.start);
                        (start..cells.end.saturating_add(span.start), link.clone())
                    })
                    .collect()
            })
            .unwrap_or_default();
        if let Some(links) = self.linked.get_mut(target)
            && (!runs.is_empty() || !links.is_empty())
            && links.replace(span, &runs)
            && let Some(m) = self.meta.get_mut(target)
        {
            m.version = version;
        }
    }

    /// Blanks cells `span` of live row `row` in style `blank`, with no
    /// links, keeping the row's soft wrap.
    fn blank_span(&mut self, row: u16, span: Range<usize>, blank: u32, version: u64) {
        let cell = Compact::blank(blank);
        let end = u16::try_from(span.end).unwrap_or(u16::MAX);
        crate::budget::spend(span.len());
        self.mutate_row(row, version, end, |cells| {
            let Some(run) = cells.get_mut(span.clone()) else {
                return false;
            };
            if run.iter().all(|c| *c == cell) {
                return false;
            }
            run.fill(cell);
            true
        });
        self.set_link(row, span, None, version);
    }

    /// `scroll` within the margins, or down, or without history.
    ///
    /// A soft wrap is the row's own, as in xterm (no standard has one): it
    /// moves with the row, new rows have none, and the row above the
    /// region keeps its own (`replay --engines xterm --size 2x1
    /// '\e[999Cab\e[L'`, and `--size 6x1 '~!@#\e[3;5r\e[10H\e[2T'`).
    /// Scrolling up, a line wrapping at the bottom margin goes on in the
    /// new row (`Screen::wrap` marks it before the scroll).
    #[inline]
    fn scroll_region(
        &mut self,
        region: Span,
        count: u16,
        direction: Scroll,
        blank: u32,
        next: &mut u64,
        version: u64,
    ) {
        let up = direction != Scroll::Down;
        let (top, bottom) = (region.first(), region.last());
        let count = count.min(region.len());
        if count == 0 {
            return;
        }
        if count == 1 {
            let id = next_id(next);
            let (from, to) = if up { (top, bottom) } else { (bottom, top) };
            if let Some(slot) = self.move_row(usize::from(from), usize::from(to)) {
                self.recycle(slot, id, version);
                if blank != 0 {
                    self.colour(slot, blank);
                }
            }
        } else {
            // Rotate once by `count` (THREATS.md B36), then make the new
            // rows in the order moving them one at a time would, so their
            // identities match.
            let (low, high) = (usize::from(top), usize::from(bottom));
            self.order.rotate((low, high), usize::from(count), up);
            for k in 0..count {
                let row = if up {
                    bottom
                        .saturating_add(1)
                        .saturating_sub(count)
                        .saturating_add(k)
                } else {
                    top.saturating_add(count)
                        .saturating_sub(1)
                        .saturating_sub(k)
                };
                let id = next_id(next);
                if let Some(slot) = self.slot(row) {
                    self.recycle(slot, id, version);
                    if blank != 0 {
                        self.colour(slot, blank);
                    }
                }
            }
        }
        if !self.links.none() {
            self.unlink_rows(region, count, up);
        }
    }

    /// Moves the screen's top row into history and puts a new blank row,
    /// in style `blank`, at row `bottom`, the rows between moving up.
    /// Nothing moves if history has no memory for it.
    #[inline]
    fn scroll_into_history(
        &mut self,
        bottom: u16,
        blank: u32,
        next: &mut u64,
        version: u64,
    ) -> Result<(), Error> {
        let Some(&slot) = self.order.get(0) else {
            return Ok(());
        };
        let taken = take_row(
            &mut self.history,
            self.cells.as_mut_slice(),
            self.meta.as_mut_slice(),
            (&self.texts, (!self.links.none()).then_some(&self.linked)),
            (slot, self.size.cols()),
        )?;
        if !taken {
            self.take_row_with_extras(slot)?;
        }
        self.history.trim();
        let id = next_id(next);
        if taken {
            // Already blank with no text or links: only a new identity.
            if let Some(m) = self.meta.get_mut(slot) {
                *m = Meta::new(id, version, false, 0);
            }
        } else {
            self.recycle(slot, id, version);
        }
        if blank != 0 {
            self.colour(slot, blank);
        }
        if bottom == self.size.lines().last() {
            self.order.turn();
        } else {
            self.move_row(0, usize::from(bottom));
        }
        Ok(())
    }

    /// `take_row` for a row with text or links: they go with it.
    #[cold]
    #[inline(never)]
    fn take_row_with_extras(&mut self, slot: usize) -> Result<(), Error> {
        let Some(&m) = self.meta.get(slot) else {
            return Ok(());
        };
        let start = slot.saturating_mul(usize::from(self.size.cols()));
        let used = start.saturating_add(usize::from(m.used));
        let cells = trimmed(self.cells.get(start..used).unwrap_or_default());
        crate::budget::spend(usize::from(m.used).saturating_add(1));
        self.history.push(m.arriving(cells, self.size.cols()))?;
        // A copy of the text, and the links themselves, the slot keeping
        // their list's memory for its next row.
        let text = self.texts.get(slot).map(Text::exact);
        let links = self.linked.get_mut(slot).and_then(KeptLinks::take);
        self.history.attach(text, links);
        Ok(())
    }

    /// Moves screen row `from` to `to`, the rows between closing up; its
    /// slot, or `None`, with nothing moved, if either is off the screen.
    fn move_row(&mut self, from: usize, to: usize) -> Option<usize> {
        let slot = *self.order.get(from)?;
        if to >= self.order.len() {
            return None;
        }
        // Usually the rows lie in one of the ring's two slices: shift that
        // run by one.
        let (low, high) = (from.min(to), from.max(to));
        crate::budget::spend(high.saturating_sub(low).saturating_add(1));
        let (front, back) = self.order.as_mut_slices();
        let split = front.len();
        let run = if high < split {
            front.get_mut(low..=high)
        } else {
            match (low.checked_sub(split), high.checked_sub(split)) {
                (Some(low), Some(high)) => back.get_mut(low..=high),
                _ => None,
            }
        };
        if let Some(run) = run {
            // The ranges are the run's own, so `copy_within` cannot panic:
            // the one call outside `bytes::copy_within` (clippy.toml).
            let end = run.len().saturating_sub(1);
            if from < to {
                run.copy_within(1.., 0);
                *run.last_mut()? = slot;
            } else {
                run.copy_within(..end, 1);
                *run.first_mut()? = slot;
            }
            return Some(slot);
        }
        // Across the two slices, one row at a time.
        let mut at = from;
        while at != to {
            let next = if at < to {
                at.checked_add(1)?
            } else {
                at.checked_sub(1)?
            };
            let row = *self.order.get(next)?;
            *self.order.get_mut(at)? = row;
            at = next;
        }
        *self.order.get_mut(to)? = slot;
        Some(slot)
    }

    /// This grid resized without reflow: rows are cut or padded to the new
    /// width (`reflowed` rewraps them). The new grid is built before
    /// anything changes, so a failed allocation leaves this one as it was.
    /// History's rows are not copied: the returned count says how many of
    /// this one's the new grid takes later (`adopt_history`).
    pub fn resized(
        &self,
        size: Size,
        next: &mut u64,
        version: u64,
    ) -> Result<(Self, usize), Error> {
        Self::check_size(size)?;
        let (rows, cols) = (size.rows(), size.cols());
        let history = self.history_len();
        // The cursor's line stays in view. A shrink drops rows below the
        // cursor first, then scrolls rows above it into history. A grow
        // pulls rows back from history, as xterm does, then pads with
        // blank rows below.
        let old_retained = self.retained_len();
        let live_top = match rows.checked_sub(self.size.rows()) {
            Some(grown) if grown > 0 => history.saturating_sub(usize::from(grown)),
            Some(_) | None => {
                let through_cursor = usize::from(self.cursor.row)
                    .checked_add(1)
                    .ok_or(Error::Capacity)?;
                history
                    .checked_add(through_cursor.saturating_sub(usize::from(rows)))
                    .ok_or(Error::Capacity)?
            }
        };
        let end = live_top
            .checked_add(usize::from(rows))
            .ok_or(Error::Capacity)?;
        // Both cursors move with their rows and keep their columns as far
        // as the width allows, and a pending wrap, as xterm keeps it (DEC
        // STD 070, Appendix D.6.1: a resize is no movement that ends it).
        // The scroll region is reset, as xterm does.
        let shifted = |cursor: Cursor| {
            let index = history.checked_add(usize::from(cursor.row));
            let row = index.map_or(usize::MAX, |i| i.saturating_sub(live_top));
            let row = u16::try_from(row).unwrap_or(u16::MAX);
            cursor.placed((row, cursor.col), size)
        };
        let mut replacement = Self {
            cursor: shifted(self.cursor),
            saved: Saved {
                cursor: shifted(self.saved.cursor),
                ..self.saved
            },
            ..self.successor(size)
        };
        replacement.reserve_screen()?;
        let mut row = Vec::new();
        let same_width = self.size.cols() == cols;
        let kept = history.min(live_top);
        for source in kept..end {
            let old = self.row_at(source).filter(|_| source < old_retained);
            let is_history = source < live_top;
            let id = old.map_or_else(|| next_id(next), |r| r.id);
            let width = match old {
                // A row is never wider than the u16 grid it was made in.
                Some(r) if is_history => u16::try_from(r.width).map_err(|_| Error::Capacity)?,
                Some(_) | None => cols,
            };
            // A row cut or padded to a new width no longer continues.
            let wrapped = old.is_some_and(|r| r.wrapped()) && (is_history || same_width);
            let row_version = old.filter(|_| is_history).map_or(version, |r| r.version);
            let mut meta = Meta::new(id, row_version, wrapped, width)
                .with_rendition(old.map_or(LineRendition::Single, |r| r.rendition()));
            meta.set(PROMPT, old.is_some_and(|r| r.starts_prompt()));
            if wrapped && old.is_some_and(|r| !r.continues()) {
                meta.flags |= ENDS;
            }
            row.clear();
            row.resize(usize::from(width), BLANK);
            crate::budget::spend(width);
            // The links that fit, to a double-width row's new end.
            let fits = match old.is_some_and(|r| r.rendition().is_double()) {
                true => (width / 2).max(1),
                false => width,
            };
            let links = old
                .and_then(|r| r.links)
                .map(|links| links.clipped(usize::from(fits)));
            let mut text = None;
            if let Some(old) = old {
                let len = old.cells.len().min(usize::from(width));
                if let (Some(dst), Some(src)) = (row.get_mut(..len), old.cells.get(..len)) {
                    crate::copy_from(dst, src);
                }
                // A double-width row loses what is past its new end, as
                // DECDWL loses it.
                if old.rendition().is_double()
                    && let Some(past) = row.get_mut(usize::from(fits)..)
                {
                    past.fill(BLANK);
                }
                repair_wide(&mut row);
                // Text is stored again within the new width's budget.
                text = Some(Line::rebuilt(&mut row, old.text));
            }
            replacement.keep_row(meta, (&row, row.len()), (text, links), is_history)?;
        }
        replacement.fit_cursor();
        Ok((replacement, kept))
    }

    /// After a resize, moves `old`'s first `kept` history rows in without
    /// copying, before the rows this grid's history was given. A row with
    /// no memory for it goes, counted (THREATS.md 1.3).
    pub(crate) fn adopt_history(&mut self, old: &mut Self, kept: usize) {
        if kept == 0 {
            return;
        }
        let mut history = std::mem::take(&mut old.history);
        history.truncate(kept);
        let given = std::mem::replace(&mut self.history, history);
        if self.history.take(&given).is_err() {
            crate::degraded();
        }
        self.charge_styles(if self.unmade { 0 } else { self.size.cells() });
    }

    /// Starts the grid again, as `new` makes it, reusing its storage when
    /// `recyclable`.
    pub fn clear(&mut self, next: &mut u64, version: u64) -> Result<(), Error> {
        if !self.recyclable() {
            // The links' table is kept: it counts the link the program has
            // open (`Screen::link_cells`) and finds it by its `id`.
            match Self::made(self.size, self.history_bytes(), next, version, self.unmade) {
                Ok(mut grid) => {
                    grid.links = std::mem::take(&mut self.links);
                    *self = grid;
                    return Ok(());
                }
                // Out of memory: history goes and the rows are blanked in
                // place (THREATS.md 1.3), counted.
                Err(refused) => {
                    if !self.holds_its_rows() {
                        return Err(refused);
                    }
                    self.history = History::new(self.history_bytes());
                    self.charge_styles(self.cells.len());
                    crate::degraded();
                }
            }
        }
        self.renew(next, version);
        Ok(())
    }

    /// Whether storage holds a full-width slot for each live row, as
    /// `renew` needs.
    fn holds_its_rows(&self) -> bool {
        let rows = usize::from(self.size.rows());
        let cells = if self.unmade { 0 } else { self.size.cells() };
        self.order.len() == rows && self.meta.len() == rows && self.cells.len() >= cells
    }

    /// Whether `clear` can reuse the storage: it holds only the live rows,
    /// sized exactly as `new` would make them.
    pub fn recyclable(&self) -> bool {
        let rows = usize::from(self.size.rows());
        let cells = if self.unmade { 0 } else { self.size.cells() };
        self.history.is_bare()
            && self.order.len() == rows
            && self.meta.len() == rows
            && self.cells.capacity() == cells
    }

    /// Blanks every row of a recyclable grid with new identities, top to
    /// bottom, and resets cursors, margins and keyboard stack.
    fn renew(&mut self, next: &mut u64, version: u64) {
        // Each row renewed and its links cleared, blank or not.
        crate::budget::spend(self.order.len().saturating_add(self.linked.len()));
        for index in 0..self.order.len() {
            let id = next_id(next);
            if let Some(&slot) = self.order.get(index) {
                self.recycle(slot, id, version);
            }
        }
        self.linked.iter_mut().for_each(RowLinks::clear);
        // No cell has a style but the default now.
        self.styles = Arc::default();
        self.recent = [(Attributes::default(), 0); RECENT];
        self.cursor = Cursor::default();
        self.saved = Saved::default();
        self.keyboard = KeyboardStack::default();
        self.narrow &= !DOUBLED;
        self.reset_margins();
    }
    /// Whether every slot's cells past `used` are blank, as recycling
    /// relies on.
    #[cfg(any(test, feature = "checks"))]
    pub fn blank_past_used(&self) -> bool {
        self.unmade
            || (0..self.meta.len()).all(|slot| {
                let used = self.meta.get(slot).map_or(0, |m| usize::from(m.used));
                self.slice(slot)
                    .get(used..)
                    .is_some_and(|tail| tail.iter().all(|c| *c == BLANK))
            })
    }
    /// Whether left and right margins narrower than the screen are set
    /// (DECSLRM).
    #[inline]
    pub fn lr(&self) -> bool {
        self.narrow & LR != 0
    }
    /// Whether a screen row may be double width.
    #[inline]
    pub(crate) fn doubled(&self) -> bool {
        self.narrow & DOUBLED != 0
    }
    /// The left and right margins.
    pub fn columns(&self) -> Span {
        self.columns
    }
    /// Sets the left and right margins.
    pub fn set_columns(&mut self, columns: Span) {
        self.columns = columns;
        let full = columns == self.size.columns();
        self.narrow = (self.narrow & !LR) | if full { 0 } else { LR };
    }
    /// The margins at the screen's edges, as resets put them.
    pub fn reset_margins(&mut self) {
        self.lines = self.size.lines();
        self.set_columns(self.size.columns());
    }
    /// Whether the cursor is between the left and right margins.
    #[inline]
    pub fn in_columns(&self) -> bool {
        self.columns.contains(self.cursor.col)
    }
    /// One past the last column a glyph printed now may take: the right
    /// margin, unless the cursor is past it (DEC STD 070, 5.4.3; xterm's
    /// `dotext`), and a double-width row's end (`row_end`).
    #[inline]
    pub fn line_end(&self) -> u16 {
        if self.narrow() {
            return self.line_end_in_margins();
        }
        self.size.cols()
    }
    /// `line_end`'s margin case, out of line like every margin case.
    #[cold]
    #[inline(never)]
    fn line_end_in_margins(&self) -> u16 {
        let end = if self.lr() && self.cursor.col <= self.columns.last() {
            self.columns.end()
        } else {
            self.size.cols()
        };
        end.min(self.row_end(self.cursor.row))
    }
    /// Whether a line may end before the screen's edge (`lr` or `doubled`).
    #[inline]
    pub fn narrow(&self) -> bool {
        self.narrow != 0
    }
    /// The rendition of the screen's row `row` (DECDWL, DECDHL).
    pub fn rendition(&self, row: u16) -> LineRendition {
        self.slot(row)
            .and_then(|slot| self.meta.get(slot))
            .map_or(LineRendition::Single, Meta::rendition)
    }
    /// One past the last column of screen row `row`: the width, or half of
    /// it (at least one) on a double-width row (DEC STD 070, 5.14.1,
    /// `end_of_line`).
    #[inline]
    pub fn row_end(&self, row: u16) -> u16 {
        if !self.doubled() {
            return self.size.cols();
        }
        self.halved_end(row)
    }
    #[cold]
    #[inline(never)]
    fn halved_end(&self, row: u16) -> u16 {
        let cols = self.size.cols();
        if self.rendition(row).is_double() {
            (cols / 2).max(1)
        } else {
            cols
        }
    }
    /// Keeps the cursor within a double-width row (DEC STD 070, 5.6.1;
    /// IND, LF, CUU, CUD, CUF, CUP and HT, their algorithms).
    #[inline]
    pub(crate) fn fit_cursor(&mut self) {
        if self.doubled() {
            self.fit_to_row();
        }
    }
    #[cold]
    #[inline(never)]
    fn fit_to_row(&mut self) {
        let last = self.halved_end(self.cursor.row).saturating_sub(1);
        self.cursor.col = self.cursor.col.min(last);
    }
    /// Sets screen row `row`'s rendition (DECSWL, DECDWL, DECDHL). A row
    /// made double width loses its cells past its new end and the cursor
    /// is kept within it (DECDWL and DECDHL, their notes and algorithms);
    /// one made single width keeps every cell (DECSWL, note 2).
    pub fn set_rendition(&mut self, row: u16, rendition: LineRendition, version: u64) {
        let Some(m) = self.slot(row).and_then(|slot| self.meta.get_mut(slot)) else {
            return;
        };
        if m.rendition() == rendition {
            return;
        }
        *m = m.with_rendition(rendition);
        m.version = version;
        if rendition.is_double() {
            self.narrow |= DOUBLED;
            let (end, cols) = (self.halved_end(row), self.size.cols());
            self.erase(row, end, cols, 0, version);
            self.set_link(row, usize::from(end)..usize::from(cols), None, version);
            if self.cursor.row == row {
                self.fit_to_row();
            }
        }
    }
    /// Makes every row of the screen single width, as setting DECLRMM does
    /// (DEC STD 070, DECLRMM, 5-29).
    pub fn single_width(&mut self, version: u64) {
        if !self.doubled() {
            return;
        }
        crate::budget::spend(self.size.rows());
        for row in 0..self.size.rows() {
            self.set_rendition(row, LineRendition::Single, version);
        }
        self.narrow &= !DOUBLED;
    }
    /// Where the next glyph goes, before any wrap: the cursor's column, or
    /// one past the last column while a wrap is pending.
    pub fn next_column(&self) -> u16 {
        past(self.cursor.col, self.cursor.pending_wrap)
    }
    /// Puts the cursor in column `col`; at or past `line_end` it stays on
    /// the last column with a wrap pending.
    #[inline]
    pub fn advance_to(&mut self, col: u16) {
        let end = self.line_end();
        self.advance_within(col, end);
    }
    /// `advance_to` with `line_end` already found (at most the width).
    #[inline]
    pub fn advance_within(&mut self, col: u16, end: u16) {
        (self.cursor.pending_wrap, self.cursor.restored) = (col >= end, false);
        self.cursor.col = col.min(end.saturating_sub(1));
    }
    /// Moves the cursor to row `row`, the last if past it.
    #[inline]
    pub fn set_row(&mut self, row: u16) {
        self.cursor.row = row.min(self.size.lines().last());
        self.fit_cursor();
    }
    /// Moves the cursor to column `col`, the last if past it.
    #[inline]
    /// Callers bring a column past a double-width row's end back
    /// (`fit_cursor`): most move left.
    pub fn set_col(&mut self, col: u16) {
        self.cursor.col = col.min(self.size.columns().last());
    }
    /// The cursor's line as CUP addresses it: from the top margin in
    /// origin mode.
    pub fn cursor_line(&self) -> u16 {
        self.cursor.row.saturating_sub(self.addressed().0.first())
    }
    /// CR: to the left margin, or to the first column if left of the
    /// margin outside origin mode (xterm's `CarriageReturn`; DEC STD 070
    /// leaves CR at the margin).
    #[inline]
    pub fn carriage_return(&mut self) {
        self.cursor.col = if self.lr() {
            self.carriage_in_margins()
        } else {
            0
        };
    }
    #[cold]
    #[inline(never)]
    fn carriage_in_margins(&self) -> u16 {
        if self.cursor.origin || self.cursor.col >= self.columns.first() {
            self.columns.first()
        } else {
            0
        }
    }
    /// CUP, relative to the margins in origin mode. Ends a pending wrap.
    #[inline]
    pub fn position(&mut self, row: u16, col: u16) {
        self.cursor.pending_wrap = false;
        let (lines, columns) = self.addressed();
        (self.cursor.row, self.cursor.col) = (lines.nth(row), columns.nth(col));
        self.fit_cursor();
    }
    /// The rows and columns CUP addresses: the margins in origin mode, else
    /// the whole screen.
    #[inline]
    pub fn addressed(&self) -> (Span, Span) {
        if self.cursor.origin {
            (self.lines, self.columns)
        } else {
            (self.size.lines(), self.size.columns())
        }
    }
    /// CUF, or HPR (`absolute`): the cursor `n` columns right, bounded by
    /// the right margin (CUF while not past it, HPR in origin mode; xterm's
    /// `CursorForward` and `CASE_HPR`), else by the last column.
    #[inline]
    pub fn forward(&mut self, n: u16, absolute: bool) {
        let col = self.cursor.col.saturating_add(n);
        self.cursor.col = if self.narrow() {
            self.forward_in_margins(col, absolute)
        } else {
            col.min(self.size.columns().last())
        };
    }
    /// `forward`'s margin and double-width case.
    #[cold]
    #[inline(never)]
    fn forward_in_margins(&self, col: u16, absolute: bool) -> u16 {
        let end = self.row_end(self.cursor.row).saturating_sub(1);
        if !self.lr() {
            return col.min(end);
        }
        let bounded = if absolute {
            self.cursor.origin
        } else {
            self.cursor.col <= self.columns.last()
        };
        col.min(if bounded {
            self.columns.last()
        } else {
            self.size.columns().last()
        })
        .min(end)
    }
    /// CUB: the cursor `n` columns left, no further than the left margin,
    /// unless it is left of it already (xterm's `CursorBack`).
    #[inline]
    pub fn backward(&mut self, n: u16) {
        let col = self.cursor.col.saturating_sub(n);
        let left = self.columns.first();
        self.cursor.col = if self.lr() && self.cursor.col >= left {
            col.max(left)
        } else {
            col
        };
    }
    /// CHA and HPA: the cursor to column `col`, counting from the left
    /// margin in origin mode, no further than the right one.
    #[inline]
    pub fn column(&mut self, col: u16) {
        self.cursor.col = if self.narrow() {
            self.column_in_margins(col)
        } else {
            col.min(self.size.columns().last())
        };
    }
    /// `column`'s margin and double-width case.
    #[cold]
    #[inline(never)]
    fn column_in_margins(&self, col: u16) -> u16 {
        let col = if self.lr() && self.cursor.origin {
            self.columns.nth(col)
        } else {
            col.min(self.size.columns().last())
        };
        col.min(self.row_end(self.cursor.row).saturating_sub(1))
    }
    /// VPA and VPR, as xterm addresses them: line `line` as CUP does,
    /// keeping the column (within the right margin in origin mode).
    #[inline]
    pub fn position_line(&mut self, line: u16) {
        let col = self.cursor.col;
        let (lines, columns) = self.addressed();
        self.cursor.pending_wrap = false;
        (self.cursor.row, self.cursor.col) = (lines.nth(line), col.min(columns.last()));
        self.fit_cursor();
    }
}

fn past(col: u16, pending_wrap: bool) -> u16 {
    col.saturating_add(u16::from(pending_wrap))
}

/// Blanks the pictures' cells among cells copied or moved: a cell copy
/// carries a cell's character and attributes (DEC STD 070, DECCRA; ICH,
/// DCH), and a picture's cell has none, its placeholder read as a blank.
/// Only placing a picture fills its cells (`Screen::place_picture`), so
/// no copy or move makes another picture's, or gives back one that text
/// or an erase took.
fn unpicture(cells: &mut [Compact]) {
    let placeholder = crate::PLACEHOLDER.chars().next();
    for cell in cells.iter_mut().filter(|c| c.first() == placeholder) {
        *cell = cell.blanked();
    }
}

/// Blanks the halves of wide glyphs cut by the edges of a DECCRA span.
fn cut(cells: &mut [Compact]) {
    if let Some(cell) = cells.first_mut().filter(|c| c.is_wide_continuation()) {
        *cell = cell.blanked();
    }
    if let Some(cell) = cells.last_mut().filter(|c| c.is_wide()) {
        *cell = cell.blanked();
    }
}

/// Gives cells `span` of slot `slot` link `link`, or none; the row takes
/// `version` if a link changed. Takes the grid's parts so they borrow
/// apart.
#[inline(always)]
fn set_link(
    meta: &mut [Meta],
    links: &mut [RowLinks],
    slot: Option<usize>,
    span: Range<usize>,
    link: Option<&Hyperlink>,
    version: u64,
) {
    // No link into a row with none, the common case, is one test.
    if let Some(slot) = slot
        && let Some(links) = links.get_mut(slot)
        && (link.is_some() || !links.is_empty())
        && links.set(span, link)
        && let Some(m) = meta.get_mut(slot)
    {
        m.version = version;
    }
}

/// The run length from which `write_ascii` folds the wide-glyph half flags.
const LONG_RUN: usize = 8;

/// Whether `cells` already hold the ASCII `run` in `style`. Out of line,
/// so the write that usually follows compiles as if it were not there.
#[inline(never)]
fn unchanged(cells: &[Compact], run: &[u8], style: u32) -> bool {
    cells.iter().zip(run).all(|(c, b)| c.is_ascii(*b, style))
}

/// ICH and DCH on a row's cells: shifts from `col` on by `count`, right
/// to insert or left to delete, filling the gap with `blank`.
fn shift<T: Copy>(cells: &mut [T], col: usize, count: usize, insert: bool, blank: T) {
    let Some(tail) = cells.get_mut(col..) else {
        return;
    };
    // `rotate_*` panics past the end: the clamp is why it is allowed here
    // (clippy.toml).
    let count = count.min(tail.len());
    let fill = if insert {
        tail.rotate_right(count);
        0..count
    } else {
        tail.rotate_left(count);
        tail.len().saturating_sub(count)..tail.len()
    };
    tail.get_mut(fill).unwrap_or_default().fill(blank);
}

/// If cell `at` is the second half of a wide glyph, blanks it and the
/// first half before it; whether it did.
fn split_wide(cells: &mut [Compact], at: usize) -> bool {
    let Some(pair) = cells.get_mut(at.saturating_sub(1)..=at) else {
        return false;
    };
    if !pair.last().is_some_and(Compact::is_wide_continuation) {
        return false;
    }
    for cell in pair {
        *cell = cell.blanked();
    }
    true
}

/// Whether a wide glyph at `i` has its second half after it.
fn whole(cells: &[Compact], i: usize, width: u16) -> bool {
    width != 2
        || i.checked_add(1)
            .and_then(|j| cells.get(j))
            .is_some_and(|c| *c == Compact::continuation())
}

/// Writes `glyph`, `width` columns wide, at cell `i`, as every printing
/// path does: a wide glyph half overwritten loses its other half, blanked
/// in the glyph's style. Whether a cell changed.
#[inline]
pub(crate) fn put(cells: &mut [Compact], i: usize, glyph: Compact, width: u16) -> bool {
    if cells.get(i) == Some(&glyph) && whole(cells, i, width) {
        return false;
    }
    let blank = Compact::blank(glyph.style());
    if cells.get(i).is_some_and(Compact::is_wide_continuation)
        && let Some(other) = i.checked_sub(1).and_then(|j| cells.get_mut(j))
    {
        *other = blank;
    }
    let last = i.saturating_add(usize::from(width)).saturating_sub(1);
    if cells.get(last).is_some_and(Compact::is_wide)
        && let Some(other) = cells.get_mut(last.saturating_add(1))
    {
        *other = blank;
    }
    if let Some(cell) = cells.get_mut(i) {
        *cell = glyph;
    }
    if width == 2
        && let Some(cell) = cells.get_mut(i.saturating_add(1))
    {
        *cell = Compact::continuation();
    }
    true
}

pub(crate) fn repair_wide(cells: &mut [Compact]) {
    repair_wide_in(cells, 0..cells.len());
}

/// `repair_wide` for cells `range` of `cells`, each half's other read
/// wherever it is in `cells`.
fn repair_wide_in(cells: &mut [Compact], range: Range<usize>) {
    let range = range.start..range.end.min(cells.len());
    crate::budget::spend(range.len());
    for i in range {
        let invalid = cells.get(i).is_some_and(|c| {
            c.is_wide()
                && !i
                    .checked_add(1)
                    .and_then(|j| cells.get(j))
                    .is_some_and(Compact::is_wide_continuation)
                || c.is_wide_continuation()
                    && !i
                        .checked_sub(1)
                        .and_then(|j| cells.get(j))
                        .is_some_and(Compact::is_wide)
        });
        if invalid && let Some(cell) = cells.get_mut(i) {
            *cell = cell.blanked();
        }
    }
}

mod reflow;
#[cfg(test)]
pub(crate) mod tests;

/// The grid's part of the invariant checker (THREATS.md section 6).
#[cfg(feature = "checks")]
impl Grid {
    pub(crate) fn check(&self, which: &str, version: u64, report: &mut crate::Checked) {
        report.record("grid.order", which, self.check_order());
        report.record("grid.slots", which, self.check_slots());
        report.record("grid.blank_past_used", which, self.check_blank_past_used());
        report.record("grid.blank_past_end", which, self.check_blank_past_end());
        report.record("grid.wide_pairs", which, self.check_wide_pairs());
        report.record("grid.text", which, self.check_text());
        report.record("grid.style_numbers", which, self.check_style_numbers());
        report.record("grid.recent", which, self.check_recent());
        report.record("grid.charged", which, self.check_charged());
        report.record("styles.table", which, self.styles.check());
        let keyed = |attributes: &Attributes| {
            std::hash::BuildHasher::hash_one(&crate::style::Keyed, attributes)
        };
        report.record("styles.spread", which, self.styles.check_spread(keyed));
        report.record("grid.versions", which, self.check_versions(version));
        report.record("grid.cursor", which, self.check_cursor());
        report.record("grid.margins", which, self.check_margins());
        report.record("grid.keyboard", which, self.check_keyboard());
        report.record("links.runs", which, self.check_link_runs());
        report.record("links.held", which, self.check_links_held());
        self.history.check(which, report);
    }

    /// Every row, history then screen: cells, width, text and links,
    /// read without the work counters seeing them.
    fn checked_rows(
        &self,
    ) -> impl Iterator<Item = (&[Compact], usize, &Text, Option<Runs<'_>>)> + '_ {
        let history = (0..self.history.len()).filter_map(|i| {
            let found = self.history.get(i)?;
            let width = usize::from(found.kept.width());
            Some((found.cells, width, found.text, found.links))
        });
        let cols = usize::from(self.size.cols());
        let screen = (self.order.iter()).filter_map(move |&slot| {
            let links = self.linked.get(slot).filter(|l| !l.is_empty());
            Some((
                self.slice(slot),
                cols,
                self.texts.get(slot)?,
                links.map(RowLinks::runs),
            ))
        });
        history.chain(screen)
    }

    /// 7: the order is a permutation of the slots, one a row.
    fn check_order(&self) -> Result<(), String> {
        let rows = usize::from(self.size.rows());
        crate::check::ensure(self.order.len() == rows, || {
            format!("{} rows ordered, {rows} rows", self.order.len())
        })?;
        crate::check::ensure(self.order.top < rows.max(1), || {
            format!("the order's top {} past {rows} rows", self.order.top)
        })?;
        let mut seen = vec![false; self.meta.len()];
        for &slot in &self.order.slots {
            match seen.get_mut(slot) {
                Some(seen) if !*seen => *seen = true,
                Some(_) => return Err(format!("slot {slot} ordered twice")),
                None => return Err(format!("slot {slot} past {} slots", self.meta.len())),
            }
        }
        crate::check::ensure(seen.iter().all(|s| *s), || "a slot no row has".into())
    }

    /// 8 and 10: one record, text and links a slot; the screen's cells, a
    /// row of the grid's width a slot, once made.
    fn check_slots(&self) -> Result<(), String> {
        let slots = self.meta.len();
        crate::check::ensure(
            self.texts.len() == slots && self.linked.len() == slots,
            || {
                format!(
                    "{slots} slots, {} texts, {} links",
                    self.texts.len(),
                    self.linked.len()
                )
            },
        )?;
        let cells = if self.unmade {
            0
        } else {
            slots.saturating_mul(usize::from(self.size.cols()))
        };
        crate::check::ensure(self.cells.len() == cells, || {
            format!("{} cells for {slots} slots", self.cells.len())
        })
    }

    /// 9: every cell past a slot's `used` mark is blank.
    fn check_blank_past_used(&self) -> Result<(), String> {
        crate::check::ensure(self.blank_past_used(), || {
            "a cell past a row's used mark is not blank".into()
        })
    }

    /// 9a: on a double-width row, every cell past `row_end` is empty, with
    /// no wide half and no link.
    fn check_blank_past_end(&self) -> Result<(), String> {
        for row in 0..self.size.rows() {
            if !self.rendition(row).is_double() {
                continue;
            }
            let end = usize::from(self.row_end(row));
            let cells = self.live_cells(row);
            let held = (cells.iter().enumerate().skip(end))
                .find(|(_, c)| c.has_contents() || c.is_wide() || c.is_wide_continuation());
            if let Some((col, _)) = held {
                return Err(format!(
                    "row {row}, column {col}: a cell past a double-width line's end holds something"
                ));
            }
            if let Some(links) = self.slot(row).and_then(|slot| self.linked.get(slot)) {
                links
                    .runs()
                    .check(end)
                    .map_err(|e| format!("row {row}, past a double-width line's end: {e}"))?;
            }
        }
        Ok(())
    }

    /// 11: a wide glyph's halves are side by side, on the screen and in
    /// history.
    fn check_wide_pairs(&self) -> Result<(), String> {
        for (row, (cells, width, _, _)) in self.checked_rows().enumerate() {
            let cell = |i: usize| cells.get(i).copied().unwrap_or(BLANK);
            for i in 0..width {
                let here = cell(i);
                let paired = if here.is_wide() {
                    i.checked_add(1)
                        .filter(|j| *j < width)
                        .is_some_and(|j| cell(j).is_wide_continuation())
                } else if here.is_wide_continuation() {
                    i.checked_sub(1).is_some_and(|j| cell(j).is_wide())
                } else {
                    true
                };
                if !paired {
                    return Err(format!("row {row}, column {i}: half a wide glyph"));
                }
            }
        }
        Ok(())
    }

    /// 13: each row's text within its limits, and every cell's text where
    /// its locator says.
    fn check_text(&self) -> Result<(), String> {
        for (row, (cells, width, text, _)) in self.checked_rows().enumerate() {
            text.check(cells, width)
                .map_err(|e| format!("row {row}: {e}"))?;
        }
        Ok(())
    }

    /// 14: every style of the table a cell has, on the screen and in
    /// history, is in the grid's table.
    fn check_style_numbers(&self) -> Result<(), String> {
        let len = self.styles.len();
        for (row, (cells, _, _, _)) in self.checked_rows().enumerate() {
            for (i, cell) in cells.iter().enumerate() {
                if Styles::place(cell.style()).is_some_and(|place| place >= len) {
                    return Err(format!(
                        "row {row}, column {i}: style {:#x} past {len} styles",
                        cell.style()
                    ));
                }
            }
        }
        Ok(())
    }

    /// The styles found lately are the table's, with their attributes.
    fn check_recent(&self) -> Result<(), String> {
        for (at, &(attributes, style)) in self.recent.iter().enumerate() {
            if attributes == Attributes::default() {
                continue;
            }
            crate::check::ensure(
                self.styles.get(style) == attributes.with_guard(false),
                || format!("style {style:#x} found lately at {at} has other attributes"),
            )?;
        }
        Ok(())
    }

    /// 21a: history is charged the style table's bytes beyond the
    /// screen's share (B11).
    fn check_charged(&self) -> Result<(), String> {
        let share = crate::style::screen_share(self.cells.len());
        let want = self.styles.bytes().saturating_sub(share);
        let charged = self.history.charged();
        crate::check::ensure(charged == want, || {
            format!("history charged {charged} bytes for styles of {want}")
        })
    }

    /// 20: every row's version is at most the screen's, `version`.
    fn check_versions(&self, version: u64) -> Result<(), String> {
        let screen = self.meta.iter().map(|m| m.version);
        let history =
            (0..self.history.len()).filter_map(|i| self.history.kept(i).map(|k| k.version));
        match history.chain(screen).find(|v| *v > version) {
            Some(v) => Err(format!("a row's version {v} past the screen's {version}")),
            None => Ok(()),
        }
    }

    /// 25: the cursor and the saved cursor are on the grid.
    fn check_cursor(&self) -> Result<(), String> {
        let (rows, cols) = (self.size.rows(), self.size.cols());
        for (name, cursor) in [("cursor", self.cursor), ("saved cursor", self.saved.cursor)] {
            crate::check::ensure(cursor.row < rows && cursor.col < cols, || {
                format!("{name} at {:?} off {rows}x{cols}", cursor.at())
            })?;
        }
        let end = self.row_end(self.cursor.row);
        crate::check::ensure(self.cursor.col < end, || {
            format!("cursor at {:?} past its line's end {end}", self.cursor.at())
        })
    }

    /// 26: the margins are inside the grid, each the whole of it or two
    /// lines or columns apart at least, and `lr` says whether the left and
    /// right ones are narrower than the grid.
    fn check_margins(&self) -> Result<(), String> {
        let (rows, cols) = (self.size.rows(), self.size.cols());
        let inside = |span: Span, n: u16| span.first() <= span.last() && span.last() < n;
        crate::check::ensure(
            inside(self.lines, rows) && inside(self.columns, cols),
            || {
                format!(
                    "margins {:?} {:?} off {rows}x{cols}",
                    self.lines, self.columns
                )
            },
        )?;
        let set = |span: Span, whole: Span| span == whole || span.first() < span.last();
        crate::check::ensure(
            set(self.lines, self.size.lines()) && set(self.columns, self.size.columns()),
            || {
                format!(
                    "margins {:?} {:?} one line or column",
                    self.lines, self.columns
                )
            },
        )?;
        crate::check::ensure(self.lr() == (self.columns != self.size.columns()), || {
            format!("lr {} with columns {:?}", self.lr(), self.columns)
        })?;
        // A double-width row's end bounds the cursor and printing only
        // while `doubled` is set.
        let halved = (0..rows).any(|row| self.rendition(row).is_double());
        crate::check::ensure(self.doubled() || !halved, || {
            "a row is half as long, unmarked".into()
        })
    }

    /// 36: the kitty keyboard stack holds at most its limit.
    fn check_keyboard(&self) -> Result<(), String> {
        crate::check::ensure(self.keyboard.0.len() <= KEYBOARD_STACK_LIMIT, || {
            format!("{} kitty keyboard flags held", self.keyboard.0.len())
        })
    }

    /// 24: each row's link runs inside its width, in order and apart.
    fn check_link_runs(&self) -> Result<(), String> {
        for (row, (_, width, _, links)) in self.checked_rows().enumerate() {
            if let Some(links) = links {
                links.check(width).map_err(|e| format!("row {row}: {e}"))?;
            }
        }
        Ok(())
    }

    /// 23 and 24: what the links held cost covers the links the rows have,
    /// within the bound, and each with an `id` is found by it.
    fn check_links_held(&self) -> Result<(), String> {
        let links = self.checked_rows().filter_map(|(_, _, _, links)| links);
        self.links.check(links)
    }
}

/// The grid's checks, each shown a grid with its one fault: a
/// check that stops checking fails here.
#[cfg(all(test, feature = "checks"))]
mod faults {
    use super::*;

    /// A grid of 3 rows of 4, with a few rows of history.
    fn grid() -> Result<Grid, Error> {
        let size = Size::new(3, 4).map_err(|_| Error::Capacity)?;
        let mut next = 1;
        let mut grid = Grid::new(size, 1 << 12, &mut next, 1)?;
        for _ in 0..5 {
            grid.write_ascii(0, 0, b"ab", 0, 1);
            grid.scroll(
                size.lines(),
                1,
                Scroll::Up { history: true },
                0,
                &mut next,
                1,
            );
        }
        Ok(grid)
    }

    /// The checks that found a fault in `grid`.
    fn faulted(grid: &Grid) -> Vec<&'static str> {
        let mut report = crate::Checked::default();
        grid.check("grid", 1, &mut report);
        report.faults().iter().map(|f| f.check).collect()
    }

    /// Whether `grid` is clean, and `fault` makes it fail `check` alone.
    fn shows(check: &str, fault: impl FnOnce(&mut Grid)) -> Result<(), Error> {
        let mut grid = grid()?;
        assert_eq!(faulted(&grid), Vec::<&str>::new());
        fault(&mut grid);
        let found = faulted(&grid);
        assert!(found.contains(&check), "{check}: {found:?}");
        Ok(())
    }

    #[test]
    fn each_check_names_its_fault() -> Result<(), Error> {
        shows("grid.order", |g| {
            if let [first, second, ..] = g.order.slots.as_mut_slice() {
                *first = *second;
            }
        })?;
        shows("grid.slots", |g| {
            g.texts.pop();
        })?;
        shows("grid.blank_past_used", |g| {
            if let Some(cell) = g.cells.get_mut(1) {
                *cell = Compact::ascii(b'x', 0);
            }
            g.meta.iter_mut().for_each(|m| m.used = 0);
        })?;
        shows("grid.blank_past_end", |g| {
            g.set_rendition(0, LineRendition::DoubleWidth, 1);
            let at = g.order.get(0).map(|slot| slot * 4 + 3);
            if let Some(cell) = at.and_then(|at| g.cells.get_mut(at)) {
                *cell = Compact::ascii(b'x', 0);
            }
            g.meta.iter_mut().for_each(|m| m.used = 4);
        })?;
        shows("grid.wide_pairs", |g| {
            if let Some(cell) = g.cells.get_mut(0) {
                *cell = Compact::glyph('一', 2, 0);
            }
            g.meta.iter_mut().for_each(|m| m.used = 4);
        })?;
        shows("grid.text", |g| {
            let long = "e\u{301}\u{301}\u{301}\u{301}\u{301}\u{301}\u{301}\u{301}\u{301}";
            g.mutate_line(0, 1, 1, |line| line.set(0, Compact::glyph('e', 1, 0), long));
            if let Some(&slot) = g.order.get(0)
                && let Some(text) = g.texts.get_mut(slot)
            {
                *text = Text::default();
            }
        })?;
        shows("grid.style_numbers", |g| {
            if let Some(cell) = g.cells.get_mut(0) {
                cell.set_style(crate::style::TABLE | 5);
            }
            g.meta.iter_mut().for_each(|m| m.used = 4);
        })?;
        shows("grid.recent", |g| {
            let bold = Attributes::default().with_bold(true);
            if let Some(slot) = g.recent.get_mut(0) {
                *slot = (bold, crate::style::TABLE | 3);
            }
        })?;
        shows("grid.charged", |g| g.history.charge(1))?;
        // A run past the row's 4 cells, and a link the links held never
        // counted (the grid's calls to each check).
        shows("links.runs", |g| {
            if let Some(&slot) = g.order.get(0)
                && let Some(row) = g.linked.get_mut(slot)
            {
                let _ = row.set(3..5, Some(&Hyperlink::new(9, "http://a", None)));
            }
        })?;
        shows("links.held", |g| {
            if let Some(&slot) = g.order.get(0)
                && let Some(row) = g.linked.get_mut(slot)
            {
                let _ = row.set(0..1, Some(&Hyperlink::new(9, "http://a", None)));
            }
        })?;
        shows("grid.versions", |g| {
            g.meta.iter_mut().for_each(|m| m.version = 2);
        })?;
        shows("grid.cursor", |g| g.cursor.row = 3)?;
        shows("grid.cursor", |g| {
            g.set_rendition(0, LineRendition::DoubleWidth, 1);
            (g.cursor.row, g.cursor.col) = (0, g.size.cols().saturating_sub(1));
        })?;
        shows("grid.margins", |g| g.narrow = LR)?;
        shows("grid.margins", |g| {
            g.set_rendition(0, LineRendition::DoubleWidth, 1);
            g.narrow = 0;
        })?;
        shows("grid.keyboard", |g| g.keyboard.0.extend([1; 33]))?;
        Ok(())
    }
}

/// What a fault test needs of a grid it breaks on purpose.
#[cfg(all(test, feature = "checks"))]
impl Grid {
    /// Gives live row `row` identity `id`, another row's maybe.
    pub(crate) fn give_id(&mut self, row: u16, id: RowId) {
        if let Some(m) = self.slot(row).and_then(|slot| self.meta.get_mut(slot)) {
            m.id = id;
        }
    }
}
