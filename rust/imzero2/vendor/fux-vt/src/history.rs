//! A grid's history: the rows scrolled off the top of its screen, oldest
//! first, as many as fit its budget of bytes.
//!
//! A row keeps only its cells before its blank tail, packed into shared
//! blocks that rows fill in order and leave oldest first. Beside them go
//! its identity, version, width and flags, and, for rows that have any, its
//! text and links, keyed by the row's number.
//!
//! Taking a row in is the hot path, so it is a few loads and stores; the
//! rest is out of line. Once a row is in, the grid trims history to its
//! budget (`trim`), counting the memory actually taken (`THREATS.md` B17):
//! the ring of records and every block at capacity, the rows' text and the
//! maps. The oldest rows go a run at a time (`pop_run`); the ring frees a
//! chunk at a time, a block as its last row goes, and a map gives back its
//! room past four times its entries and 64.

use std::collections::VecDeque;

mod ring;
use ring::Ring;
mod spans;
use spans::Spans;
use std::ops::Range;
use std::sync::atomic::{AtomicU64, Ordering};

use crate::compact::{BLANK, Compact, NO_TEXT, Text};
use crate::grid::{MAX_CELLS, MAX_ROWS};
use crate::link::{KeptLinks, Runs};
use crate::{Error, LineRendition, RowId};

/// The cells a block holds, unless one row needs more. Small, as the
/// partly filled newest and partly left oldest blocks cost their whole
/// size; large enough that a row left at a block's end costs little.
const BLOCK: usize = 1 << 12;
/// The most blocks (16-bit numbers). More than a grid can fill
/// (`grid::MAX_CELLS`): a block made is half full, or holds a row half its
/// size or more.
const BLOCKS: usize = 1 << 16;

/// Refused metadata is lost, separately from ordinary history eviction.
fn room_for_metadata<T>(rows: &mut VecDeque<T>) -> bool {
    #[cfg(feature = "checks")]
    if crate::check::refusing() {
        crate::degraded();
        return false;
    }
    if rows.try_reserve(1).is_err() {
        crate::degraded();
        return false;
    }
    true
}

/// Values kept for some rows, by row number, oldest first: rows come and
/// go in order, so lookup is a binary search and removal is from an end.
#[derive(Clone, Debug)]
struct ByRow<T>(VecDeque<(u64, T)>);

impl<T> Default for ByRow<T> {
    fn default() -> Self {
        Self(VecDeque::new())
    }
}

impl<T> ByRow<T> {
    fn at(&self, number: u64) -> Result<usize, usize> {
        self.0.binary_search_by_key(&number, |(n, _)| *n)
    }
    fn get(&self, number: u64) -> Option<&T> {
        let at = self.at(number).ok()?;
        self.0.get(at).map(|(_, value)| value)
    }
    /// Keeps `value` for the newest row; out of memory keeps none.
    fn insert(&mut self, number: u64, value: T) -> bool {
        let kept = room_for_metadata(&mut self.0);
        if kept {
            self.0.push_back((number, value));
        }
        kept
    }
    fn remove(&mut self, number: u64) -> Option<T> {
        let at = self.at(number).ok()?;
        self.0.remove(at).map(|(_, value)| value)
    }
    #[cfg(feature = "checks")]
    fn len(&self) -> usize {
        self.0.len()
    }
    fn is_empty(&self) -> bool {
        self.0.is_empty()
    }
    /// The bytes it takes, at capacity.
    fn bytes(&self) -> usize {
        self.0.capacity().saturating_mul(size_of::<(u64, T)>())
    }
    fn drop_before(&mut self, first: u64) {
        while self.0.front().is_some_and(|(n, _)| *n < first) {
            self.0.pop_front();
        }
    }
    fn drop_from(&mut self, from: u64) {
        while self.0.back().is_some_and(|(n, _)| *n >= from) {
            self.0.pop_back();
        }
    }
    fn shrink(&mut self) {
        shrink(&mut self.0);
    }
}

/// A row of history: what it is, and where its cells are.
#[derive(Clone, Copy, Debug)]
pub(crate) struct Kept {
    pub(crate) id: RowId,
    pub(crate) version: u64,
    /// Packed: block number (16 bits, wrapping), start in the block (12:
    /// a row sharing a block starts before `BLOCK`), cells kept and width
    /// (16 each), and flags (4).
    place: u64,
}

const _: () = assert!(size_of::<Kept>() == 24);

/// Budget bytes per row of a ring chunk: a chunk holds a sixteenth of the
/// most rows the budget holds (`History::shift_for`).
const CHUNKS_PER_BUDGET: usize = size_of::<Kept>() * 16;
const _: () = assert!(BLOCK <= 1 << 12);

/// `Kept`'s flags, the first two the screen's rows keep too (`Meta`).
pub(crate) const WRAPPED: u8 = 1;
pub(crate) const PROMPT: u8 = 2;
/// Where a screen row's flags keep its rendition (`LineRendition::bits`),
/// above `WRAPPED` and `PROMPT`; history keeps it apart (`renditions`).
pub(crate) const RENDITION_SHIFT: u8 = 2;
const LINKED: u8 = 4;
const TEXT: u8 = 8;
/// A screen row's flag, past its rendition's bits: soft-wrapped as xterm
/// keeps it, but its line ends here since the row below was erased whole
/// (`Row::continues`). History keeps it apart (`ended`).
pub(crate) const ENDS: u8 = 16;

impl Kept {
    #[inline]
    fn new(row: &Arriving<'_>, (block, start): (u16, u16)) -> Self {
        let flags = row.flags & (WRAPPED | PROMPT);
        let len = u16::try_from(row.cells.len()).unwrap_or(u16::MAX);
        Self {
            id: row.id,
            version: row.version,
            place: u64::from(block)
                | u64::from(start & 0xfff) << 16
                | u64::from(len) << 28
                | u64::from(row.width) << 44
                | u64::from(flags & 0xf) << 60,
        }
    }
    #[inline]
    fn block(&self) -> u16 {
        u16::try_from(self.place & 0xffff).unwrap_or(0)
    }
    #[inline]
    fn start(&self) -> usize {
        usize::try_from((self.place >> 16) & 0xfff).unwrap_or(0)
    }
    #[inline]
    pub(crate) fn len(&self) -> usize {
        usize::try_from((self.place >> 28) & 0xffff).unwrap_or(0)
    }
    pub(crate) fn width(&self) -> u16 {
        u16::try_from((self.place >> 44) & 0xffff).unwrap_or(0)
    }
    #[inline]
    fn flags(&self) -> u8 {
        u8::try_from(self.place >> 60).unwrap_or(0)
    }
    fn set(&mut self, flag: u8, on: bool) {
        let bit = u64::from(flag) << 60;
        self.place = (self.place & !bit) | if on { bit } else { 0 };
    }
    pub(crate) fn wrapped(&self) -> bool {
        self.flags() & WRAPPED != 0
    }
}

/// The most room a map holds for `len` entries before giving some back.
fn most_room(len: usize) -> usize {
    len.saturating_mul(4).saturating_add(64)
}

/// Gives back `deque`'s room past [`most_room`] for its entries, so that
/// one growing by doubling doesn't shrink back at once (`THREATS.md` B17).
fn shrink<T>(deque: &mut VecDeque<T>) {
    if deque.capacity() > most_room(deque.len()) {
        deque.shrink_to(deque.len());
    }
}

/// The cells of a row before its blank tail: those history keeps.
#[inline]
pub(crate) fn trimmed(cells: &[Compact]) -> &[Compact] {
    let len = cells
        .iter()
        .rposition(|cell| *cell != BLANK)
        .map_or(0, |last| last.saturating_add(1));
    cells.get(..len).unwrap_or_default()
}

/// What a row brings into history, besides its text and links
/// (`History::attach`).
#[derive(Clone, Copy)]
pub(crate) struct Arriving<'a> {
    pub(crate) id: RowId,
    pub(crate) version: u64,
    /// `WRAPPED`, `PROMPT` and the rendition above them, as the screen's
    /// row kept them.
    pub(crate) flags: u8,
    /// Its cells before its blank tail (`trimmed`), of `width`.
    pub(crate) cells: &'a [Compact],
    pub(crate) width: u16,
}

/// A row of history as a reader finds it.
pub(crate) struct Found<'a> {
    pub(crate) kept: &'a Kept,
    /// Its flags as a screen row's (`WRAPPED`, `PROMPT`, its rendition
    /// from `RENDITION_SHIFT`).
    pub(crate) flags: u8,
    pub(crate) cells: &'a [Compact],
    pub(crate) text: &'a Text,
    pub(crate) links: Option<Runs<'a>>,
}

/// An `Option<u64>` set through `&self` while reading
/// (`Screen::track_pictures`): atomic, as a `Screen` is `Sync` and may be
/// read from several threads at once. `None` is `u64::MAX`, no row's
/// number.
#[derive(Debug)]
pub(crate) struct Noted(AtomicU64);

impl Noted {
    pub(crate) const fn none() -> Self {
        Self(AtomicU64::new(u64::MAX))
    }
    pub(crate) fn get(&self) -> Option<u64> {
        Some(self.0.load(Ordering::Relaxed)).filter(|n| *n != u64::MAX)
    }
    pub(crate) fn set(&self, n: Option<u64>) {
        self.0.store(n.unwrap_or(u64::MAX), Ordering::Relaxed);
    }
}

impl Default for Noted {
    fn default() -> Self {
        Self::none()
    }
}

impl Clone for Noted {
    fn clone(&self) -> Self {
        Self(AtomicU64::new(self.0.load(Ordering::Relaxed)))
    }
}

#[derive(Debug, Default)]
pub(crate) struct History {
    rows: Ring,
    /// The number of rows that came before the oldest.
    first_row: u64,
    /// The blocks before the newest, oldest first, and the newest, which
    /// rows fill; none yet if it holds no memory.
    older: VecDeque<Vec<Compact>>,
    newest: Vec<Compact>,
    /// `older`'s first block number (wrapping); `newest_block` is the one
    /// after `older`'s last.
    first_block: u16,
    newest_block: u16,
    /// The cells all blocks have room for.
    room_cells: usize,
    /// The bytes the ring, blocks and maps take at capacity, all but the
    /// rows' text (`recount`); and the maps' share of it.
    held: usize,
    maps: usize,
    /// The bytes the rows' links' runs take (`THREATS.md` B15).
    runs: usize,
    texts: ByRow<Text>,
    linked: ByRow<KeptLinks>,
    /// Rows not single width: rare, and `Kept` has no bit to spare.
    renditions: ByRow<LineRendition>,
    /// Soft-wrapped rows whose line ends with them (`ENDS`), as rare.
    ended: VecDeque<u64>,
    /// Rows by identity (`position`): the first `spans::FREE_SPANS` spans
    /// are free so what is kept does not depend on how identities run; the
    /// rest cost `spans::SPAN_BYTES` each (`THREATS.md` B40).
    spans: Spans,
    /// The cells the rows keep (`kept_cells`).
    cells: usize,
    /// The bytes of the rows' text.
    text: usize,
    /// The most bytes history holds (`bytes`) once trimmed.
    budget: usize,
    /// Bytes counted in the budget beside the rows': the grid's style
    /// table's beyond the screen's share (`THREATS.md` B11).
    charged: usize,
    /// The budget less what is charged.
    room: usize,
    /// The next row's number when the screen's pictures were last followed
    /// into history (`Screen::track_pictures`).
    followed: Noted,
}

/// A copy keeps each block's capacity, as rows fill the newest within it,
/// and recounts what its ring and maps take, whose capacities may differ
/// from the original's.
impl Clone for History {
    fn clone(&self) -> Self {
        let block = |b: &Vec<Compact>| {
            let mut copy = Vec::with_capacity(b.capacity());
            copy.extend_from_slice(b);
            copy
        };
        let mut copy = Self {
            rows: self.rows.clone(),
            first_row: self.first_row,
            older: self.older.iter().map(block).collect(),
            newest: block(&self.newest),
            first_block: self.first_block,
            newest_block: self.newest_block,
            room_cells: self.room_cells,
            held: 0,
            maps: 0,
            runs: self.runs,
            texts: self.texts.clone(),
            linked: self.linked.clone(),
            renditions: self.renditions.clone(),
            ended: self.ended.clone(),
            spans: self.spans.clone(),
            cells: self.cells,
            text: self.text,
            budget: self.budget,
            charged: self.charged,
            room: self.room,
            followed: self.followed.clone(),
        };
        copy.recount();
        copy
    }
}

impl History {
    pub(crate) fn new(budget: usize) -> Self {
        Self {
            rows: Ring::new(Self::shift_for(budget)),
            budget,
            room: budget,
            ..Self::default()
        }
    }
    /// The ring's chunk size for `budget`: a sixteenth of the most rows it
    /// holds, a power of two from 1 to 512 rows (12 KiB), so an unused
    /// chunk is little of the budget.
    fn shift_for(budget: usize) -> u32 {
        let rows = (budget.checked_div(CHUNKS_PER_BUDGET))
            .unwrap_or(0)
            .clamp(1, 512);
        rows.ilog2()
    }
    pub(crate) fn len(&self) -> usize {
        self.rows.len()
    }
    pub(crate) fn followed(&self) -> &Noted {
        &self.followed
    }
    pub(crate) fn budget(&self) -> usize {
        self.budget
    }
    /// The bytes the rows hold plus the bytes charged to history.
    #[cfg(any(test, feature = "checks"))]
    pub(crate) fn bytes(&self) -> usize {
        self.rows_bytes().saturating_add(self.charged)
    }
    /// The ring, blocks and maps at capacity, and the rows' text.
    #[inline(always)]
    fn rows_bytes(&self) -> usize {
        self.held.saturating_add(self.text)
    }
    /// Recounts `held`: after the ring, a block or a map is made, grows,
    /// shrinks or goes.
    fn recount(&mut self) {
        self.maps = self.maps_bytes();
        self.held = (self.rows.capacity().saturating_mul(size_of::<Kept>()))
            .saturating_add(self.room_cells.saturating_mul(size_of::<Compact>()))
            .saturating_add(self.maps)
            .saturating_add(self.runs);
    }
    fn maps_bytes(&self) -> usize {
        (self.texts.bytes())
            .saturating_add(self.linked.bytes())
            .saturating_add(self.renditions.bytes())
            .saturating_add(self.ended.capacity().saturating_mul(size_of::<u64>()))
            .saturating_add(self.spans.charged())
    }
    /// The cells a block holds: `BLOCK`, or less for a small budget, so a
    /// block is never most of what history may hold.
    fn block_cells(&self) -> usize {
        (self.budget / 64).clamp(1, BLOCK)
    }
    #[cfg(feature = "checks")]
    pub(crate) fn charged(&self) -> usize {
        self.charged
    }
    /// Charges `bytes` to the budget in place of the old charge, letting
    /// the oldest rows go to fit.
    pub(crate) fn charge(&mut self, bytes: usize) {
        self.charged = bytes;
        self.room = self.budget.saturating_sub(bytes);
        self.trim();
    }
    /// Lets the oldest rows go while history holds more than the budget
    /// (`make_room` bounds rows and cells to `grid::MAX_ROWS` and
    /// `MAX_CELLS`). What it holds changes only out of line, so a row
    /// arriving usually lets none go.
    #[inline]
    pub(crate) fn trim(&mut self) {
        if self.over() {
            self.trim_over();
        }
    }
    #[inline(always)]
    fn over(&self) -> bool {
        self.rows_bytes() > self.room
    }
    /// With no rows left and still over, all storage goes.
    #[cold]
    #[inline(never)]
    fn trim_over(&mut self) {
        while !self.rows.is_empty() && self.over() {
            self.pop_run();
        }
        if self.rows.is_empty() && self.over() {
            self.release();
        }
    }
    fn release(&mut self) {
        self.runs = 0;
        self.rows = Ring::new(Self::shift_for(self.budget));
        self.older.clear();
        self.newest = Vec::new();
        self.newest_block = self.first_block;
        self.room_cells = 0;
        self.texts = ByRow::default();
        self.linked = ByRow::default();
        self.renditions = ByRow::default();
        self.ended = VecDeque::new();
        self.spans = Spans::default();
        self.recount();
    }
    /// Lets every row go, keeping the budget, the charge and the row
    /// numbering.
    pub(crate) fn clear(&mut self) {
        let mut fresh = Self::new(self.budget);
        fresh.first_row = self.numbers().1;
        fresh.followed = self.followed.clone();
        fresh.charge(self.charged);
        *self = fresh;
    }
    /// Sets the budget, copying the rows that fit into storage sized for
    /// it, numbers kept.
    pub(crate) fn set_budget(&mut self, budget: usize) {
        let mut fresh = Self::new(budget);
        fresh.first_row = self.first_row;
        fresh.followed = self.followed.clone();
        fresh.charge(self.charged);
        if fresh.take(self).is_ok() {
            *self = fresh;
        } else {
            // Out of memory: trim in place.
            self.budget = budget;
            self.room = budget.saturating_sub(self.charged);
            self.trim();
        }
    }
    /// Takes in `old`'s rows, trimming as it fills; fails only when out
    /// of memory.
    pub(crate) fn take(&mut self, old: &Self) -> Result<(), Error> {
        for found in (0..old.len()).filter_map(|index| old.get(index)) {
            let kept = found.kept;
            crate::budget::spend(found.cells.len().saturating_add(1));
            self.push(Arriving {
                id: kept.id,
                version: kept.version,
                flags: found.flags,
                cells: found.cells,
                width: kept.width(),
            })?;
            let text = (!found.text.is_empty()).then(|| found.text.exact());
            if text.is_some() || found.links.is_some() {
                self.attach(text, found.links.and_then(KeptLinks::of));
            }
            self.trim();
        }
        Ok(())
    }
    pub(crate) fn kept_cells(&self) -> usize {
        self.cells
    }
    /// Whether no row and no storage is held.
    pub(crate) fn is_bare(&self) -> bool {
        self.rows.capacity() == 0 && self.older.is_empty() && self.newest.capacity() == 0
    }
    pub(crate) fn kept(&self, index: usize) -> Option<&Kept> {
        self.rows.get(index)
    }
    fn number(&self, index: usize) -> Option<u64> {
        self.first_row.checked_add(u64::try_from(index).ok()?)
    }
    /// The numbers of the oldest row kept and of the next to come.
    pub(crate) fn numbers(&self) -> (u64, u64) {
        let next = self.number(self.rows.len()).unwrap_or(u64::MAX);
        (self.first_row, next)
    }
    pub(crate) fn index_of_number(&self, number: u64) -> Option<usize> {
        let index = usize::try_from(number.checked_sub(self.first_row)?).ok()?;
        (index < self.rows.len()).then_some(index)
    }
    /// Block `block`'s index in `older`, or `older`'s length for the
    /// newest, if it is held.
    fn block_at(first: u16, older: usize, block: u16) -> Option<usize> {
        let at = usize::from(block.wrapping_sub(first));
        (at <= older).then_some(at)
    }
    fn block(&self, block: u16) -> Option<&Vec<Compact>> {
        let at = Self::block_at(self.first_block, self.older.len(), block)?;
        Some(self.older.get(at).unwrap_or(&self.newest))
    }
    fn cells_of(kept: &Kept) -> Option<Range<usize>> {
        let start = kept.start();
        Some(start..start.checked_add(kept.len())?)
    }
    #[inline(always)]
    pub(crate) fn get(&self, index: usize) -> Option<Found<'_>> {
        let kept = self.kept(index)?;
        let cells = if kept.len() == 0 {
            &[][..]
        } else {
            self.block(kept.block())?.get(Self::cells_of(kept)?)?
        };
        let number = self.number(index)?;
        let text = (kept.flags() & TEXT != 0).then(|| self.texts.get(number));
        let text = text.flatten().unwrap_or(&NO_TEXT);
        let links = (kept.flags() & LINKED != 0).then(|| self.linked.get(number));
        let links = links.flatten().map(KeptLinks::runs);
        let mut flags = kept.flags() & (WRAPPED | PROMPT);
        if !self.renditions.is_empty() {
            flags |= self.rendition_of(number).bits() << RENDITION_SHIFT;
        }
        if !self.ended.is_empty() && self.ended.binary_search(&number).is_ok() {
            flags |= ENDS;
        }
        Some(Found {
            kept,
            flags,
            cells,
            text,
            links,
        })
    }
    /// The index of the row with identity `id`, found by its span with one
    /// row read (`THREATS.md` B40).
    pub(crate) fn position(&self, id: RowId) -> Option<usize> {
        let index = self.index_of_number(self.spans.number_of(id.0)?)?;
        crate::budget::spend(1);
        (self.rows.get(index)?.id == id).then_some(index)
    }
    /// The links of each row that has any, with its index, oldest first.
    pub(crate) fn links(&self) -> impl Iterator<Item = (usize, Runs<'_>)> {
        crate::budget::spend(self.rows.len());
        let linked = self.rows.iter().enumerate();
        linked
            .filter(|(_, kept)| kept.flags() & LINKED != 0)
            .filter_map(|(index, _)| Some((index, self.linked.get(self.number(index)?)?.runs())))
    }
    /// Calls `f` with every cell the rows keep (not the stale cells of rows
    /// gone from a block still held), to mark and renumber styles.
    pub(crate) fn each_cell_mut(&mut self, mut f: impl FnMut(&mut Compact)) {
        let (older, newest) = (&mut self.older, &mut self.newest);
        for kept in self.rows.iter() {
            let at = Self::block_at(self.first_block, older.len(), kept.block());
            let block = at.map(|at| older.get_mut(at).unwrap_or(&mut *newest));
            if let Some(cells) =
                Self::cells_of(kept).and_then(|cells| block.and_then(|b| b.get_mut(cells)))
            {
                cells.iter_mut().for_each(&mut f);
            }
        }
    }

    /// Whether `len` cells fit after the newest block's.
    #[inline]
    fn fits(&self, len: usize) -> bool {
        let end = self.newest.len().saturating_add(len);
        len == 0 || end <= BLOCK && end <= self.newest.capacity()
    }

    /// Makes room for a row of `len` cells: a ring chunk if needed, and
    /// room for its cells. The newest block grows by doubling up to
    /// `block_cells`; else a new block starts, of `block_cells`, or just the
    /// row's size for a row over half a block or for the first block. Fails
    /// only when out of memory. Out of line: a row usually fits.
    #[cold]
    #[inline(never)]
    fn make_room(&mut self, len: usize) -> Result<(), Error> {
        let block = self.block_cells();
        if !self.rows.has_room() {
            // Keep to `MAX_ROWS` once the chunk is made.
            let most = MAX_ROWS.saturating_sub(self.rows.chunk());
            while self.rows.len() > most {
                self.pop();
            }
            self.rows.grow()?;
            self.recount();
        }
        if self.fits(len) {
            return Ok(());
        }
        // Keep to `BLOCKS` block numbers and `MAX_CELLS` cells.
        let most = MAX_CELLS.saturating_sub(len.max(block));
        while !self.rows.is_empty()
            && (self.older.len() >= BLOCKS.saturating_sub(2) || self.room_cells > most)
        {
            self.pop();
        }
        let end = self.newest.len().saturating_add(len);
        if self.newest.capacity() > 0 && end <= block {
            let before = self.newest.capacity();
            let capacity = before.saturating_mul(2).clamp(end, block);
            crate::reserve(&mut self.newest, capacity)?;
            self.room_cells = (self.room_cells)
                .saturating_add(self.newest.capacity())
                .saturating_sub(before);
            self.recount();
            return Ok(());
        }
        let first = self.newest.capacity() == 0 && self.older.is_empty();
        let capacity = if first || len > block / 2 { len } else { block };
        if self.older.len() >= BLOCKS.saturating_sub(2) {
            return Err(Error::Capacity);
        }
        let mut cells = Vec::new();
        crate::reserve(&mut cells, capacity)?;
        self.start_block(cells);
        Ok(())
    }

    /// Appends `row`, its cells already `trimmed`; the caller spends the
    /// row (its cells, trimmed or not, and one). Fails, with nothing
    /// changed, only when out of memory. Inlined: it is the hot path of
    /// `Grid::scroll_into_history`.
    #[inline(always)]
    pub(crate) fn push(&mut self, row: Arriving<'_>) -> Result<(), Error> {
        let len = row.cells.len();
        if !self.rows.has_room() || !self.fits(len) {
            self.make_room(len)?;
        }
        let start = u16::try_from(self.newest.len()).unwrap_or(0);
        // One cell, common for short lines, is stored without a copy call.
        match row.cells {
            [cell] => self.newest.push(*cell),
            cells => self.newest.extend_from_slice(cells),
        }
        self.cells = self.cells.saturating_add(len);
        if self.spans.went_on(row.id.0) {
            self.begin_span(row.id);
        }
        self.rows
            .push_back(Kept::new(&row, (self.newest_block, start)));
        if (row.flags >> RENDITION_SHIFT) & 3 != 0 {
            self.keep_rendition(LineRendition::from_bits(row.flags >> RENDITION_SHIFT));
        }
        if row.flags & ENDS != 0 {
            let _ = self.keep_ended();
        }
        Ok(())
    }

    /// The row coming next, `id`, begins a span. Out of line, as scrolled
    /// rows usually have consecutive identities.
    #[cold]
    #[inline(never)]
    fn begin_span(&mut self, id: RowId) {
        let number = self.number(self.rows.len()).unwrap_or(u64::MAX);
        let before = self.spans.charged();
        self.spans.begin(id.0, number, self.first_row);
        // Only the spans' cost changed: adjust by the difference.
        let after = self.spans.charged();
        self.maps = self.maps.saturating_sub(before).saturating_add(after);
        self.held = self.held.saturating_sub(before).saturating_add(after);
    }

    /// Notes the last row's non-single-width rendition; out of line, as
    /// rare.
    #[cold]
    #[inline(never)]
    fn keep_rendition(&mut self, rendition: LineRendition) {
        let Some(number) = self.rows.len().checked_sub(1).and_then(|i| self.number(i)) else {
            return;
        };
        // No memory for it: the row is kept single width.
        if self.renditions.insert(number, rendition) {
            self.recount();
        }
    }
    /// Notes that the row pushed last, soft-wrapped, ends its line (`ENDS`).
    #[cold]
    #[inline(never)]
    fn keep_ended(&mut self) -> bool {
        let Some(number) = self.rows.len().checked_sub(1).and_then(|i| self.number(i)) else {
            return false;
        };
        // No memory for it: the line goes on, as xterm's would.
        let new = self.ended.back() != Some(&number) && room_for_metadata(&mut self.ended);
        if new {
            self.ended.push_back(number);
            self.recount();
        }
        new
    }
    pub(crate) fn has_renditions(&self) -> bool {
        !self.renditions.is_empty()
    }
    #[cold]
    #[inline(never)]
    fn rendition_of(&self, number: u64) -> LineRendition {
        self.renditions
            .get(number)
            .copied()
            .unwrap_or(LineRendition::Single)
    }
    /// Drops renditions and line ends of rows let go.
    #[cold]
    #[inline(never)]
    fn drop_renditions(&mut self) {
        self.renditions.drop_before(self.first_row);
        while self.ended.front().is_some_and(|&n| n < self.first_row) {
            self.ended.pop_front();
        }
        self.maps_changed();
    }

    /// Lets the oldest rows go up to and including the first whose going
    /// may change storage: one with text or links, the last in its block,
    /// one in a later block, or the last of its chunk. Same as repeated
    /// `pop`, reading each row once.
    fn pop_run(&mut self) {
        let first_block = self.first_block;
        let first_end = self.older.front().map(Vec::len);
        let (mut n, mut cells, mut leave) = (0usize, 0usize, None);
        for kept in self.rows.front_run() {
            if kept.flags() & (TEXT | LINKED) != 0 {
                break;
            }
            n = n.saturating_add(1);
            cells = cells.saturating_add(kept.len());
            // A row with no cells is in no block; its block number is stale.
            if kept.len() == 0 {
                continue;
            }
            if kept.block() != first_block {
                leave = Some(kept.block());
                break;
            }
            if first_end == Some(kept.start().saturating_add(kept.len())) {
                leave = Some(first_block.wrapping_add(1));
                break;
            }
        }
        if n == 0 {
            self.pop();
            return;
        }
        crate::budget::spend(n);
        self.first_row = self
            .first_row
            .wrapping_add(u64::try_from(n).unwrap_or(u64::MAX));
        self.cells = self.cells.saturating_sub(cells);
        if !self.renditions.is_empty() || !self.ended.is_empty() {
            self.drop_renditions();
        }
        let freed = self.rows.advance(n);
        match leave {
            Some(block) => self.leave_blocks(block),
            None if freed => self.recount(),
            None => {}
        }
    }

    /// Lets the oldest row go, with its links and any blocks no row's
    /// cells are in now.
    #[inline]
    fn pop(&mut self) {
        let Some((gone, freed)) = self.rows.pop_front() else {
            return;
        };
        crate::budget::spend(1);
        let number = self.first_row;
        self.first_row = self.first_row.wrapping_add(1);
        self.cells = self.cells.saturating_sub(gone.len());
        if gone.flags() & (TEXT | LINKED) != 0 {
            self.forget(number, gone.flags());
        }
        if !self.renditions.is_empty() || !self.ended.is_empty() {
            self.drop_renditions();
        }
        let ended = (gone.len() > 0)
            .then(|| gone.start().saturating_add(gone.len()))
            .is_some_and(|end| {
                gone.block() == self.first_block
                    && self.older.front().is_some_and(|first| first.len() == end)
            });
        if ended {
            self.leave_blocks(gone.block().wrapping_add(1));
        } else if gone.len() > 0 && gone.block() != self.first_block {
            self.leave_blocks(gone.block());
        } else if freed {
            self.recount();
        }
    }

    /// Lets the newest rows go until `len` remain: rows a resize takes
    /// back onto the screen.
    pub(crate) fn truncate(&mut self, len: usize) {
        while self.rows.len() > len {
            let Some((gone, freed)) = self.rows.pop_back() else {
                return;
            };
            let number = self.number(self.rows.len()).unwrap_or(u64::MAX);
            crate::budget::spend(1);
            self.cells = self.cells.saturating_sub(gone.len());
            if gone.flags() & (TEXT | LINKED) != 0 {
                self.forget(number, gone.flags());
            }
            self.renditions.drop_from(number);
            while self.ended.back().is_some_and(|&n| n >= number) {
                self.ended.pop_back();
            }
            let end = gone.start().saturating_add(gone.len());
            if gone.len() > 0 && gone.block() == self.newest_block && end == self.newest.len() {
                self.newest.truncate(gone.start());
            }
            self.spans.pop(number);
            if freed {
                self.recount();
            }
        }
        self.maps_changed();
    }

    /// Starts `block`, empty, as the newest (`make_room`).
    fn start_block(&mut self, block: Vec<Compact>) {
        self.room_cells = self.room_cells.saturating_add(block.capacity());
        // If the newest held no memory, the new block takes its number,
        // which rows with no cells have.
        let last = std::mem::replace(&mut self.newest, block);
        if last.capacity() > 0 {
            self.older.push_back(last);
            self.newest_block = self.newest_block.wrapping_add(1);
        }
        self.recount();
    }

    /// Drops row `number`'s text and links, as `flags` says it has.
    #[cold]
    #[inline(never)]
    fn forget(&mut self, number: u64, flags: u8) {
        if flags & TEXT != 0
            && let Some(text) = self.texts.remove(number)
        {
            self.text = self.text.saturating_sub(text.bytes());
        }
        if flags & LINKED != 0
            && let Some(links) = self.linked.remove(number)
        {
            self.runs = self.runs.saturating_sub(links.heap());
        }
        self.maps_changed();
    }

    /// After a map changed: shrinks the maps (`ByRow::shrink`) and
    /// recounts.
    fn maps_changed(&mut self) {
        self.texts.shrink();
        self.linked.shrink();
        self.renditions.shrink();
        shrink(&mut self.ended);
        self.recount();
    }

    /// Lets the blocks before `block` go: rows go oldest first, so no row
    /// is in them.
    #[cold]
    #[inline(never)]
    fn leave_blocks(&mut self, block: u16) {
        let left = usize::from(block.wrapping_sub(self.first_block));
        if left > self.older.len() {
            return;
        }
        for _ in 0..left {
            if let Some(block) = self.older.pop_front() {
                self.first_block = self.first_block.wrapping_add(1);
                self.room_cells = self.room_cells.saturating_sub(block.capacity());
            }
        }
        self.recount();
    }

    /// Gives the last row pushed its text and links, if any.
    #[cold]
    #[inline(never)]
    pub(crate) fn attach(&mut self, text: Option<Text>, links: Option<KeptLinks>) {
        let last = self.rows.len().checked_sub(1);
        let Some(number) = last.and_then(|last| self.number(last)) else {
            return;
        };
        let Some(kept) = self.rows.back_mut() else {
            return;
        };
        if let Some(text) = text.filter(|text| !text.is_empty()) {
            let bytes = text.bytes();
            if self.texts.insert(number, text) {
                self.text = self.text.saturating_add(bytes);
                kept.set(TEXT, true);
            }
        }
        if let Some(links) = links {
            let heap = links.heap();
            if self.linked.insert(number, links) {
                self.runs = self.runs.saturating_add(heap);
                kept.set(LINKED, true);
            }
        }
        self.recount();
    }

    /// Whether row `index`'s line goes on in the row after: soft-wrapped,
    /// and not noted ended (`ENDS`).
    pub(crate) fn continues(&self, index: usize) -> bool {
        let wrapped = self.kept(index).is_some_and(Kept::wrapped);
        wrapped
            && (self.ended.is_empty()
                || self
                    .number(index)
                    .is_none_or(|n| self.ended.binary_search(&n).is_err()))
    }
    /// Ends the newest row's line, giving it `version`, as the screen's top
    /// row it went on in was erased whole. It stays soft-wrapped, as xterm
    /// keeps it (`ENDS`).
    pub(crate) fn end_newest_line(&mut self, version: u64) {
        if self
            .rows
            .len()
            .checked_sub(1)
            .and_then(|i| self.rows.get(i))
            .is_some_and(Kept::wrapped)
            && self.keep_ended()
            && let Some(kept) = self.rows.back_mut()
        {
            kept.version = version;
            // The note counts in the budget.
            self.trim();
        }
    }

    /// Drops row `index`'s links, if any, giving it `version`.
    pub(crate) fn unlink(&mut self, index: usize, version: u64) {
        if let Some(number) = self.number(index)
            && let Some(kept) = self.rows.get_mut(index)
            && kept.flags() & LINKED != 0
        {
            kept.set(LINKED, false);
            kept.version = version;
            self.forget(number, LINKED);
        }
    }

    /// The heap bytes held, counted from the storage itself, to check
    /// `rows_bytes`.
    #[cfg(any(test, feature = "checks"))]
    pub(crate) fn heap(&self) -> usize {
        let cell = std::mem::size_of::<Compact>();
        self.older
            .iter()
            .chain([&self.newest])
            .map(|block| block.capacity().saturating_mul(cell))
            .sum::<usize>()
            .saturating_add(
                self.rows
                    .capacity()
                    .saturating_mul(std::mem::size_of::<Kept>()),
            )
            .saturating_add(self.maps_bytes())
            .saturating_add(self.linked.0.iter().map(|(_, l)| l.heap()).sum::<usize>())
            .saturating_add(self.text)
    }

    /// Forgets every row's links: RIS.
    pub(crate) fn reset_links(&mut self) {
        self.linked = ByRow::default();
        self.runs = 0;
        for kept in self.rows.iter_mut() {
            kept.set(LINKED, false);
        }
        self.maps_changed();
    }
}

/// History's part of the invariant checker (THREATS.md section 6).
#[cfg(feature = "checks")]
impl History {
    pub(crate) fn check(&self, which: &str, report: &mut crate::Checked) {
        report.record("history.counts", which, self.check_counts());
        report.record("history.blocks", which, self.check_blocks());
        report.record("history.flags", which, self.check_flags());
        report.record("history.budget", which, self.check_budget());
        report.record("history.capacities", which, self.check_capacities());
    }

    /// 17: the counts of cells, text and storage match what is held.
    fn check_counts(&self) -> Result<(), String> {
        crate::check::ensure(self.rows_bytes() == self.heap(), || {
            format!("{} bytes counted, {} held", self.rows_bytes(), self.heap())
        })?;
        let cells = self
            .rows
            .iter()
            .map(Kept::len)
            .fold(0, usize::saturating_add);
        let text = (self.texts.0.iter())
            .map(|(_, text)| text.bytes())
            .fold(0, usize::saturating_add);
        crate::check::ensure(cells == self.cells && text == self.text, || {
            format!(
                "{} cells and {} bytes of text counted, {cells} and {text} kept",
                self.cells, self.text
            )
        })
    }

    /// 10 and 18: each row's cells are in a block held, within it, and no
    /// more than its width.
    fn check_blocks(&self) -> Result<(), String> {
        for (i, kept) in self.rows.iter().enumerate() {
            crate::check::ensure(kept.len() <= usize::from(kept.width()), || {
                format!("row {i} keeps {} cells of {}", kept.len(), kept.width())
            })?;
            if kept.len() == 0 {
                continue;
            }
            let block = self.block(kept.block());
            let fits =
                Self::cells_of(kept).is_some_and(|c| block.is_some_and(|b| c.end <= b.len()));
            crate::check::ensure(fits, || {
                format!("row {i}'s cells are not in block {}", kept.block())
            })?;
        }
        Ok(())
    }

    /// 19: a row's text and link flags say whether the maps have it.
    fn check_flags(&self) -> Result<(), String> {
        let (mut texts, mut linked) = (0usize, 0usize);
        for (i, kept) in self.rows.iter().enumerate() {
            let number = self.number(i).unwrap_or(u64::MAX);
            let (text, links) = (kept.flags() & TEXT != 0, kept.flags() & LINKED != 0);
            crate::check::ensure(
                text == self.texts.get(number).is_some()
                    && links == self.linked.get(number).is_some(),
                || format!("row {i}'s flags {:#x} disagree with the maps", kept.flags()),
            )?;
            texts = texts.saturating_add(usize::from(text));
            linked = linked.saturating_add(usize::from(links));
        }
        crate::check::ensure(
            texts == self.texts.len() && linked == self.linked.len(),
            || {
                format!(
                    "{} texts and {} links kept for {texts} and {linked} rows",
                    self.texts.len(),
                    self.linked.len()
                )
            },
        )?;
        // Ended lines are of rows kept, in order, each soft-wrapped.
        let (first, next) = self.numbers();
        let mut after = first;
        for &number in &self.ended {
            let wrapped = number
                .checked_sub(first)
                .and_then(|i| usize::try_from(i).ok())
                .and_then(|i| self.rows.get(i))
                .is_some_and(Kept::wrapped);
            crate::check::ensure((after..next).contains(&number) && wrapped, || {
                format!("a line noted ended at row {number} of {first}..{next}")
            })?;
            after = number.saturating_add(1);
        }
        // Renditions are of rows kept, in order, none single width.
        let (first, next) = self.numbers();
        let mut after = first;
        for &(number, rendition) in &self.renditions.0 {
            crate::check::ensure(
                (after..next).contains(&number) && rendition.is_double(),
                || format!("a rendition noted for row {number} of {first}..{next}"),
            )?;
            after = number.saturating_add(1);
        }
        // 20: each row is found by its identity where it is (`position`).
        for (i, kept) in self.rows.iter().enumerate() {
            let found = self.spans.number_of(kept.id.0);
            crate::check::ensure(found == self.number(i), || {
                format!("row {i}, {:?}, is found as number {found:?}", kept.id)
            })?;
        }
        Ok(())
    }

    /// 21: within the budget, and the most rows and cells a grid holds.
    fn check_budget(&self) -> Result<(), String> {
        crate::check::ensure(
            self.bytes() <= self.budget && self.rows.len() <= MAX_ROWS && self.cells <= MAX_CELLS,
            || {
                format!(
                    "{} bytes of {}, {} rows, {} cells",
                    self.bytes(),
                    self.budget,
                    self.rows.len(),
                    self.cells
                )
            },
        )
    }

    /// 21: the ring holds at most two spare chunks, and the maps at most
    /// four times their entries and 64 (THREATS.md B17).
    fn check_capacities(&self) -> Result<(), String> {
        let ring = self
            .rows
            .len()
            .saturating_add(self.rows.chunk().saturating_mul(2));
        crate::check::ensure(self.rows.capacity() < ring, || {
            format!(
                "the ring holds room for {}, keeping {}",
                self.rows.capacity(),
                self.rows.len()
            )
        })?;
        for (name, capacity, len) in [
            ("ended lines", self.ended.capacity(), self.ended.len()),
            ("texts", self.texts.0.capacity(), self.texts.len()),
            ("links", self.linked.0.capacity(), self.linked.len()),
            (
                "renditions",
                self.renditions.0.capacity(),
                self.renditions.len(),
            ),
        ] {
            crate::check::ensure(capacity <= most_room(len), || {
                format!("the {name} holds room for {capacity}, keeping {len}")
            })?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests;

/// History's checks, each shown a history with its one fault.
#[cfg(all(test, feature = "checks"))]
mod faults {
    use super::*;

    /// A history of six rows of three cells.
    fn history() -> History {
        let mut history = History::new(1 << 12);
        let cells = [Compact::ascii(b'x', 0); 3];
        for id in 0..6u64 {
            let row = Arriving {
                id: RowId(id),
                version: 1,
                flags: 0,
                cells: &cells,
                width: 4,
            };
            assert!(history.push(row).is_ok());
            history.trim();
        }
        history
    }

    #[test]
    fn refused_history_metadata_is_counted_once_and_eviction_is_not() {
        let mut history = history();
        let mut text = Text::default();
        let mut cells = [Compact::ascii(b'x', 0)];
        assert!(
            crate::compact::Line {
                cells: &mut cells,
                text: &mut text
            }
            .set(
                0,
                Compact::ascii(b'x', 0),
                &format!(
                    "x{}",
                    std::iter::repeat_n("\u{301}", 12).collect::<String>()
                )
            )
        );
        let link = crate::Hyperlink::new(1, "https://example.test", None);
        let mut links = crate::link::RowLinks::default();
        assert!(links.set(0..1, Some(&link)));
        let links = KeptLinks::take(&mut links);
        assert!(links.is_some() && !text.is_empty());
        let before = crate::degrades();
        crate::refuse_next(2);
        history.attach(Some(text), links);
        assert_eq!(crate::degrades().saturating_sub(before), 2);
        crate::refuse_next(1);
        history.keep_rendition(LineRendition::DoubleWidth);
        assert_eq!(crate::degrades().saturating_sub(before), 3);
        crate::refuse_next(1);
        assert!(!history.keep_ended());
        assert_eq!(crate::degrades().saturating_sub(before), 4);
        assert!(history.keep_ended());
        assert!(!history.keep_ended());
        history.budget = 0;
        history.trim();
        assert_eq!(crate::degrades().saturating_sub(before), 4);
    }

    fn faulted(history: &History) -> Vec<&'static str> {
        let mut report = crate::Checked::default();
        history.check("history", &mut report);
        report.faults().iter().map(|f| f.check).collect()
    }

    fn shows(check: &str, fault: impl FnOnce(&mut History)) {
        let mut history = history();
        assert_eq!(faulted(&history), Vec::<&str>::new());
        fault(&mut history);
        let found = faulted(&history);
        assert!(found.contains(&check), "{check}: {found:?}");
    }

    #[test]
    fn each_check_names_its_fault() {
        shows("history.counts", |h| h.cells = h.cells.saturating_add(1));
        shows("history.blocks", |h| {
            if let Some(kept) = h.rows.back_mut() {
                kept.place = (kept.place & !0xffff) | 0x7fff;
            }
        });
        shows("history.flags", |h| {
            if let Some(kept) = h.rows.back_mut() {
                kept.set(TEXT, true);
            }
        });
        shows("history.flags", |h| {
            (h.renditions.0).push_back((u64::MAX, LineRendition::DoubleWidth));
        });
        shows("history.flags", |h| {
            if let Some(kept) = h.rows.back_mut() {
                kept.id = RowId(1 << 40);
            }
        });
        shows("history.budget", |h| h.budget = 0);
        shows("history.capacities", |h| h.texts.0.reserve(1000));
    }
}
