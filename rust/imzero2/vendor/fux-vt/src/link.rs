//! Hyperlinks (OSC 8, `references/modern/osc8_hyperlinks.md`), held in
//! runs per row beside the cells.
//!
//! A [`Hyperlink`] is the link itself, not a number to look up, so a cell
//! cannot name a link that is not there. Only a cell with contents has a
//! link: an erased cell keeps its stale link, unread, until a glyph is
//! printed there, which always writes its link; so a partial erase never
//! touches links. A whole erased row drops them (`Grid::erase`).
//!
//! A grid's links are bounded: each new link is counted (`Links::held`),
//! and past the bound the oldest history rows lose theirs
//! (`Grid::make_room`).

use std::collections::HashSet;
use std::ops::Range;
use std::sync::Arc;

/// The longest URI kept, as VTE and iTerm2 bound it (the spec's "Length
/// limits"). A longer one opens no link.
pub(crate) const URI_LIMIT: usize = 2083;
/// The longest `id` kept, as VTE bounds it (the spec's "Length limits"). A
/// link with a longer one is not opened.
pub(crate) const ID_LIMIT: usize = 250;
/// The most heap a grid's links hold at once: each link's string and `Arc`
/// counts, plus the `id` table as allocated, two tables while it grows.
pub(crate) const LINK_BYTES: usize = 4 << 20;
/// What an `Arc<str>` allocates beyond its text: its two counts.
const ARC_HEADER: usize = 2 * size_of::<usize>();
/// After making room fails (every link left is on the screen), how many
/// more links are refused before trying again.
const STALL: u32 = 256;

/// A cell's hyperlink: its URI, optional `id`, and a key that tells links
/// apart. Cheap to clone: URI and `id` share one allocation.
#[derive(Clone, PartialEq, Eq)]
pub struct Hyperlink {
    key: u64,
    /// The URI, after the `id` between two NULs if it has one: neither
    /// holds a NUL (`parse` keeps them printable ASCII), and a URI is never
    /// empty, so a link without an `id` costs no search to read its URI.
    text: Arc<str>,
}

impl Hyperlink {
    pub(crate) fn new(key: u64, uri: &str, id: Option<&str>) -> Self {
        let text = match id {
            Some(id) => {
                let len = id.len().saturating_add(uri.len()).saturating_add(2);
                let mut text = String::with_capacity(len);
                for part in ["\0", id, "\0", uri] {
                    text.push_str(part);
                }
                Arc::from(text)
            }
            None => Arc::from(uri),
        };
        Self { key, text }
    }
    fn parts(&self) -> (Option<&str>, &str) {
        match self.text.strip_prefix('\0') {
            Some(rest) => match rest.split_once('\0') {
                Some((id, uri)) => (Some(id), uri),
                None => (None, rest),
            },
            None => (None, &self.text),
        }
    }
    /// The URI: at most [`URI_LIMIT`] printable ASCII bytes.
    pub fn uri(&self) -> &str {
        self.parts().1
    }
    /// The nonempty `id` parameter, if given: at most [`ID_LIMIT`] printable
    /// ASCII bytes.
    pub(crate) fn id(&self) -> Option<&str> {
        self.parts().0
    }
    /// A unique key: cells with the same key are one link, as the spec
    /// groups them. An `OSC 8` without `id` opens a new link; one whose `id`
    /// and URI the screen still holds (history included) reuses that link.
    pub fn key(&self) -> u64 {
        self.key
    }
    /// What the link's string allocates, `Arc` counts included.
    fn cost(&self) -> usize {
        self.text.len().saturating_add(ARC_HEADER)
    }
}

impl std::fmt::Debug for Hyperlink {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Hyperlink")
            .field("key", &self.key())
            .field("uri", &self.uri())
            .field("id", &self.id())
            .finish()
    }
}

/// A link with an `id`, found by its `id` and URI (`Links::ids`).
#[derive(Clone, Debug)]
struct ById(Hyperlink);

impl PartialEq for ById {
    fn eq(&self, other: &Self) -> bool {
        self.0.text == other.0.text
    }
}

impl Eq for ById {}

impl std::hash::Hash for ById {
    fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
        self.0.text.hash(state);
    }
}

/// A row's links: runs of cells with one link each, sorted and disjoint.
/// A screen row keeps its list's memory for the slot's next row.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub(crate) struct RowLinks(Vec<Run>);

/// A history row's links, sized to fit: one run inline with no heap, more
/// in an exact-size list.
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) enum KeptLinks {
    One(Run),
    Many(Box<[Run]>),
}

#[derive(Clone, Copy, Debug)]
pub(crate) struct Runs<'a>(&'a [Run]);

/// Cells `start..end` of a row, and their link.
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct Run {
    start: u16,
    end: u16,
    link: Hyperlink,
}

// The memory budget is set for 64-bit targets; a 32-bit one (wasm32) has
// smaller pointers, so its runs are smaller still.
#[cfg(target_pointer_width = "64")]
const _: () = assert!(size_of::<Run>() == 32 && size_of::<KeptLinks>() == 32);

impl KeptLinks {
    /// Moves the links out of `links`, which keeps its memory.
    pub(crate) fn take(links: &mut RowLinks) -> Option<Self> {
        crate::budget::spend(links.0.len());
        match links.0.len() {
            0 => None,
            1 => links.0.pop().map(Self::One),
            n => {
                let mut runs = Vec::with_capacity(n);
                runs.extend(std::iter::from_fn(|| links.0.pop()));
                runs.reverse();
                Some(Self::Many(runs.into_boxed_slice()))
            }
        }
    }
    pub(crate) fn of(runs: Runs<'_>) -> Option<Self> {
        match runs.0 {
            [] => None,
            [run] => Some(Self::One(run.clone())),
            runs => Some(Self::Many(runs.into())),
        }
    }
    pub(crate) fn runs(&self) -> Runs<'_> {
        match self {
            Self::One(run) => Runs(std::slice::from_ref(run)),
            Self::Many(runs) => Runs(runs),
        }
    }
    /// The bytes its runs take beyond itself.
    pub(crate) fn heap(&self) -> usize {
        match self {
            Self::One(_) => 0,
            Self::Many(runs) => runs.len().saturating_mul(size_of::<Run>()),
        }
    }
}

impl RowLinks {
    #[inline]
    pub(crate) fn runs(&self) -> Runs<'_> {
        Runs(&self.0)
    }
    pub(crate) fn is_empty(&self) -> bool {
        self.0.is_empty()
    }
    /// Drops every link, keeping the memory for the slot's next row.
    pub(crate) fn clear(&mut self) {
        crate::budget::spend(self.0.len());
        self.0.clear();
    }
    /// Sets the links of cells `span` to `runs`, each inside it, in order
    /// and apart: a margin scroll's span copied, in one splice however many
    /// cells change. Whether any cell's link changed.
    #[inline(never)]
    pub(crate) fn replace(
        &mut self,
        span: Range<usize>,
        runs: &[(Range<usize>, Hyperlink)],
    ) -> bool {
        let (Ok(start), Ok(end)) = (u16::try_from(span.start), u16::try_from(span.end)) else {
            return false;
        };
        let (first, last) = self.overlapping(start, end);
        let held = self.0.get(first..last).unwrap_or_default();
        crate::budget::spend(held.len().saturating_add(runs.len()));
        let clipped = held.iter().map(|run| {
            let cells = usize::from(run.start.max(start))..usize::from(run.end.min(end));
            (cells, &run.link)
        });
        if clipped.eq(runs.iter().map(|(cells, link)| (cells.clone(), link))) {
            return false;
        }
        let runs = runs.iter().filter_map(|(cells, link)| {
            let (start, end) = (
                u16::try_from(cells.start).ok()?,
                u16::try_from(cells.end).ok()?,
            );
            Some(Run {
                start,
                end,
                link: link.clone(),
            })
        });
        self.splice(start, end, runs);
        true
    }
    /// The runs that reach into cells `start..end`, as a range of indices:
    /// the runs are in order and apart.
    fn overlapping(&self, start: u16, end: u16) -> (usize, usize) {
        let first = self.0.partition_point(|run| run.end <= start);
        let last = self.0.partition_point(|run| run.start < end).max(first);
        (first, last)
    }
    /// Puts `runs`, in order and apart within cells `start..end`, in place
    /// of what links those cells had: the runs reaching in are cut at the
    /// span's edges, and the new runs merged with those beside them, in one
    /// splice. Spends the runs looked at and those moved past it.
    fn splice(&mut self, start: u16, end: u16, runs: impl Iterator<Item = Run>) {
        let (first, last) = self.overlapping(start, end);
        // The runs beside, which may merge with what goes in.
        let (from, to) = (
            first.saturating_sub(1),
            last.saturating_add(1).min(self.0.len()),
        );
        let mut pieces: Vec<Run> = Vec::new();
        let mut put = |run: Run| match pieces.last_mut() {
            _ if run.start >= run.end => {}
            Some(kept) if kept.end == run.start && kept.link == run.link => kept.end = run.end,
            Some(_) | None => pieces.push(run),
        };
        if let Some(before) = self.0.get(from).filter(|_| from < first) {
            put(before.clone());
        }
        if let Some(run) = self.0.get(first).filter(|_| first < last) {
            put(Run {
                end: start,
                ..run.clone()
            });
        }
        runs.for_each(&mut put);
        if let Some(run) = last
            .checked_sub(1)
            .and_then(|i| self.0.get(i))
            .filter(|_| first < last)
        {
            put(Run {
                start: end,
                ..run.clone()
            });
        }
        if let Some(after) = self.0.get(last).filter(|_| last < to) {
            put(after.clone());
        }
        let moved = self.0.len().saturating_sub(to);
        crate::budget::spend(
            (to.saturating_sub(from))
                .saturating_add(pieces.len())
                .saturating_add(moved),
        );
        // `pieces` in place of `from..to`: written over it, then the rest
        // rotated in after it, or the surplus rotated past the tail and cut.
        let (old, new) = (to.saturating_sub(from), pieces.len());
        let mut pieces = pieces.into_iter();
        for (slot, piece) in self.0.iter_mut().skip(from).take(old).zip(&mut pieces) {
            *slot = piece;
        }
        let kept = from.saturating_add(old.min(new));
        if new > old {
            self.0.extend(pieces);
            if let Some(tail) = self.0.get_mut(kept..) {
                tail.rotate_right(new.saturating_sub(old).min(tail.len()));
            }
        } else if let Some(tail) = self.0.get_mut(kept..) {
            let surplus = old.saturating_sub(new).min(tail.len());
            tail.rotate_left(surplus);
            let len = self.0.len().saturating_sub(surplus);
            self.0.truncate(len);
        }
    }
    /// Sets the link of cells `span`; returns whether any cell's link
    /// changed. A span past what a row holds changes nothing. The left-to-right append
    /// is inlined; the rest is out of line (`overwrite`).
    #[inline]
    pub(crate) fn set(&mut self, span: Range<usize>, link: Option<&Hyperlink>) -> bool {
        let (Ok(start), Ok(end)) = (u16::try_from(span.start), u16::try_from(span.end)) else {
            return false;
        };
        if start >= end {
            return false;
        }
        if self.0.last().is_none_or(|last| last.end <= start) {
            let Some(link) = link else {
                return false;
            };
            self.push(start, end, link.clone());
            return true;
        }
        self.overwrite(start..end, link)
    }
    /// `set` over cells some run reaches.
    #[inline(never)]
    fn overwrite(&mut self, span: Range<u16>, link: Option<&Hyperlink>) -> bool {
        if self.has(span.clone(), link) {
            return false;
        }
        let Range { start, end } = span;
        let link = link.map(|link| Run {
            start,
            end,
            link: link.clone(),
        });
        self.splice(start, end, link.into_iter());
        true
    }
    /// Whether every cell of `span` has `link`.
    fn has(&self, span: Range<u16>, link: Option<&Hyperlink>) -> bool {
        let first = self.0.partition_point(|run| run.end <= span.start);
        let mut runs =
            (self.0.get(first..).unwrap_or_default().iter()).take_while(|run| run.start < span.end);
        match link {
            None => runs.next().is_none(),
            Some(link) => {
                let mut at = span.start;
                runs.all(|run| {
                    let joins = run.start <= at && run.link == *link;
                    at = run.end;
                    joins
                }) && at >= span.end
            }
        }
    }
    /// Appends a run, merged into the last if it touches with the same link.
    fn push(&mut self, start: u16, end: u16, link: Hyperlink) {
        match self.0.last_mut() {
            Some(last) if last.end == start && last.link == link => last.end = end,
            Some(_) | None => self.0.push(Run { start, end, link }),
        }
    }
    /// Replaces each run in place by the up to three pieces `parts` gives;
    /// extra pieces are appended, unsorted, for `tidy`.
    fn remake(&mut self, parts: impl Fn(&Run) -> [(u16, u16); 3]) {
        crate::budget::spend(self.0.len());
        for i in 0..self.0.len() {
            let Some(run) = self.0.get_mut(i) else {
                break;
            };
            let mut cells = parts(run)
                .into_iter()
                .filter(|(start, end)| start < end)
                .peekable();
            (run.start, run.end) = cells.next().unwrap_or((0, 0));
            if cells.peek().is_some() {
                let link = run.link.clone();
                for (start, end) in cells {
                    let link = link.clone();
                    self.0.push(Run { start, end, link });
                }
            }
        }
    }
    /// Restores order: drops empty runs, merges touching runs of one link.
    fn tidy(&mut self) {
        // A pass, a sort and a pass over the runs.
        let log = usize::try_from(self.0.len().checked_ilog2().unwrap_or(0)).unwrap_or(0);
        crate::budget::spend(self.0.len().saturating_mul(log.saturating_add(2)));
        self.0.retain(|run| run.start < run.end);
        self.0.sort_unstable_by_key(|run| run.start);
        self.0.dedup_by(|next, kept| {
            let joins = kept.end == next.start && kept.link == next.link;
            if joins {
                kept.end = next.end;
            }
            joins
        });
    }
    /// ICH and DCH: links of cells `col..end` move `count` places right
    /// (insert) or left (delete); what passes `end` is gone, vacated cells
    /// have none. Out of line, as only rows with links call it.
    #[inline(never)]
    pub(crate) fn shift(&mut self, col: usize, count: usize, insert: bool, end: usize) {
        let clamp = |n: usize| u16::try_from(n).unwrap_or(u16::MAX);
        let (col, end) = (clamp(col), clamp(end));
        let count = clamp(count).min(end.saturating_sub(col));
        self.remake(|run| {
            let turned = if insert {
                let start = run.start.max(col).saturating_add(count);
                (start, run.end.min(end).saturating_add(count).min(end))
            } else {
                let start = run.start.max(col.saturating_add(count));
                let end = run.end.min(end).saturating_sub(count);
                (start.saturating_sub(count), end)
            };
            [
                (run.start, run.end.min(col)),
                turned,
                (run.start.max(end), run.end),
            ]
        });
        self.tidy();
    }
}

impl<'a> Runs<'a> {
    pub(crate) fn len(self) -> usize {
        self.0.len()
    }
    pub(crate) fn get(self, col: usize) -> Option<&'a Hyperlink> {
        let col = u16::try_from(col).ok()?;
        let at = self.0.partition_point(|run| run.end <= col);
        self.0
            .get(at)
            .filter(|run| run.start <= col)
            .map(|run| &run.link)
    }
    /// The links within `cells`, with spans relative to `cells.start`.
    pub(crate) fn within(
        self,
        cells: Range<usize>,
    ) -> impl Iterator<Item = (Range<usize>, &'a Hyperlink)> {
        self.0.iter().filter_map(move |run| {
            let start = usize::from(run.start).max(cells.start);
            let end = usize::from(run.end).min(cells.end);
            let at = start.saturating_sub(cells.start);
            (start < end).then(|| (at..at.saturating_add(end.saturating_sub(start)), &run.link))
        })
    }
    /// Every link of the row, once per run.
    pub(crate) fn links(self) -> impl Iterator<Item = &'a Hyperlink> {
        self.0.iter().map(|run| &run.link)
    }
    /// The links of the cells before `width`.
    pub(crate) fn clipped(self, width: usize) -> RowLinks {
        let width = u16::try_from(width).unwrap_or(u16::MAX);
        let mut links = RowLinks(self.0.to_vec());
        links.remake(|run| [(run.start, run.end.min(width)), (0, 0), (0, 0)]);
        links.tidy();
        links
    }
}

/// The buckets hashbrown allocates for `capacity`: a power of two, 7/8
/// full at most, at least 4 once nonempty.
fn buckets(capacity: usize) -> usize {
    match capacity {
        0 => 0,
        1..8 => capacity.saturating_add(1).next_power_of_two().max(4),
        _ => (capacity.saturating_mul(8) / 7).next_power_of_two(),
    }
}

/// What a table of `buckets` allocates for `Links::ids`: a link and a
/// control byte per bucket, plus one group's control bytes.
fn table_bytes(buckets: usize) -> usize {
    match buckets {
        0 => 0,
        _ => (buckets.saturating_mul(size_of::<ById>().saturating_add(1))).saturating_add(16),
    }
}

/// A grid's links: their cost, those with an `id`, and the open link.
#[derive(Clone, Debug, Default)]
pub(crate) struct Links {
    /// What the links held cost: the strings the rows had at the last
    /// `sweep` plus every link made since, and `ids`'s table.
    held: usize,
    /// The buckets `ids`'s table has, as allocated: tracked here because
    /// removals leave the table as is while `capacity` excludes tombstones.
    buckets: usize,
    /// The links with an `id`, by `id` and URI, until a sweep finds no row
    /// with them.
    ids: HashSet<ById>,
    /// Links still to refuse before making room is tried again.
    stalled: u32,
    /// The link the program has open, as this grid has it.
    pub(crate) open: Held,
}

impl Links {
    /// Whether no row has a link: every link a row has is held, and
    /// costs something.
    #[inline]
    pub(crate) fn none(&self) -> bool {
        self.held == 0
    }
    /// The held link with this `id` and URI.
    pub(crate) fn find(&self, opened: &Hyperlink) -> Option<&Hyperlink> {
        let found = self.ids.get(&ById(opened.clone()));
        found.map(|link| &link.0)
    }
    /// Holds `opened`; `None` if it does not fit without making room. If
    /// it grows `ids`, old and new tables must both fit, as both are held
    /// while it grows.
    pub(crate) fn insert(&mut self, opened: &Hyperlink) -> Option<Hyperlink> {
        let link = opened.clone();
        let held = self.held.saturating_add(link.cost());
        // A full table is reallocated at up to twice the size, the old one
        // held until the new one is filled.
        let grows = link.id().is_some() && self.ids.len() >= self.ids.capacity();
        let grown = match grows {
            true => table_bytes(self.buckets.saturating_mul(2).max(4)),
            false => 0,
        };
        if held.saturating_add(grown) > LINK_BYTES {
            return None;
        }
        if grows {
            self.ids.try_reserve(1).ok()?;
            // A fresh table has no tombstones, so capacity gives its buckets.
            let made = buckets(self.ids.capacity());
            self.held =
                (held.saturating_sub(table_bytes(self.buckets))).saturating_add(table_bytes(made));
            self.buckets = made;
        } else {
            self.held = held;
        }
        if link.id().is_some() {
            self.ids.replace(ById(link.clone()));
        }
        Some(link)
    }
    /// Recounts the links `rows` have (history index of `len` rows, oldest
    /// first, or `None` for a screen row) and returns how many of the
    /// oldest history rows must lose their links for the rest to cost at
    /// most half the bound (all of them if none suffices). Drops `ids`
    /// entries no remaining row has.
    pub(crate) fn sweep<'a, I>(&mut self, rows: impl Fn() -> I, len: usize) -> usize
    where
        I: Iterator<Item = (Option<usize>, Runs<'a>)>,
    {
        // Each link once, with the newest row that has it (screen rows
        // newest). The list is sized up front, not grown, so it is never
        // held twice; rows and costs fit 32 bits (at most `grid::MAX_ROWS`
        // rows, a link under 4 KiB), keeping it small beside the links.
        let runs = rows().fold(0usize, |n, (_, links)| n.saturating_add(links.0.len()));
        let mut seen: Vec<(u64, u32, u32)> = Vec::with_capacity(runs);
        for (row, links) in rows() {
            let row = row.map_or(u32::MAX, |row| u32::try_from(row).unwrap_or(u32::MAX - 1));
            seen.extend(links.links().map(|link| {
                let cost = u32::try_from(link.cost()).unwrap_or(u32::MAX);
                (link.key(), row, cost)
            }));
        }
        // Each run, history row and `id`, plus two sorts at ⌈log2 runs⌉ ×
        // runs ÷ 16 each (THREATS.md B16).
        let log = usize::try_from(seen.len().checked_ilog2().unwrap_or(0)).unwrap_or(0);
        let sort = log.saturating_add(1).saturating_mul(seen.len()) / 16;
        crate::budget::spend(
            seen.len()
                .saturating_add(len)
                .saturating_add(self.ids.len())
                .saturating_add(sort.saturating_mul(2)),
        );
        seen.sort_unstable();
        seen.dedup_by(|next, kept| {
            let same = next.0 == kept.0;
            if same {
                kept.1 = next.1;
            }
            same
        });
        // History rows lose their links oldest first, each freeing the
        // links no newer row has, until the rest costs half the bound with
        // the table at its current size.
        let half = (LINK_BYTES / 2).saturating_sub(table_bytes(self.buckets));
        let wide = |n: u32| usize::try_from(n).unwrap_or(usize::MAX);
        let len = u32::try_from(len).unwrap_or(u32::MAX - 1);
        let mut live = (seen.iter()).fold(0usize, |live, &(_, _, c)| live.saturating_add(wide(c)));
        let mut lose = 0u32;
        let mut kept = seen.as_mut_slice();
        if live > half {
            lose = len;
            kept.sort_unstable_by_key(|&(_, row, _)| row);
            for leaving in kept.chunk_by(|a, b| a.1 == b.1) {
                let Some(&(_, row, _)) = leaving.first().filter(|(_, row, _)| *row < len) else {
                    break;
                };
                let cost =
                    (leaving.iter()).fold(0usize, |sum, &(_, _, c)| sum.saturating_add(wide(c)));
                live = live.saturating_sub(cost);
                if live <= half {
                    lose = row.saturating_add(1);
                    break;
                }
            }
            // The links rows from `lose` on keep, sorted by key again.
            let from = kept.partition_point(|&(_, row, _)| row < lose);
            kept = kept.get_mut(from..).unwrap_or_default();
            kept.sort_unstable();
        }
        self.ids.retain(|ById(link)| {
            let at = kept.binary_search_by_key(&link.key(), |&(key, _, _)| key);
            at.ok()
                .and_then(|at| kept.get(at))
                .is_some_and(|&(_, row, _)| row >= lose)
        });
        // Shrink a table over twice the needed size, if the smaller one
        // fits beside it while both are held.
        if self.ids.is_empty() {
            self.ids = HashSet::new();
            self.buckets = 0;
        } else if buckets(self.ids.len()).saturating_mul(2) < self.buckets {
            let smaller = table_bytes(buckets(self.ids.len()));
            if live
                .saturating_add(table_bytes(self.buckets))
                .saturating_add(smaller)
                <= LINK_BYTES
            {
                self.ids.shrink_to(self.ids.len());
                self.buckets = buckets(self.ids.capacity());
            }
        }
        self.held = live.saturating_add(table_bytes(self.buckets));
        wide(lose)
    }
    /// Whether making room may be tried now: after a failure, only once in
    /// [`STALL`] links.
    pub(crate) fn may_make_room(&mut self) -> bool {
        match self.stalled.checked_sub(1) {
            Some(left) => {
                self.stalled = left;
                false
            }
            None => true,
        }
    }
    /// Making room failed: refuse the next links without trying.
    pub(crate) fn stall(&mut self) {
        self.stalled = STALL;
    }
    /// The held cost, and what `live` costs counting each link once.
    #[cfg(test)]
    pub(crate) fn costs<'a>(&self, live: impl Iterator<Item = &'a Hyperlink>) -> (usize, usize) {
        let live: std::collections::HashMap<u64, usize> =
            live.map(|link| (link.key(), link.cost())).collect();
        (self.held, live.values().sum())
    }
}

/// The links' part of the invariant checker (THREATS.md section 6).
#[cfg(feature = "checks")]
impl Runs<'_> {
    /// 24: the runs are nonempty, sorted, disjoint, within `width`, with
    /// links within the spec's limits.
    pub(crate) fn check(self, width: usize) -> Result<(), String> {
        let mut end = 0u16;
        for run in self.0 {
            crate::check::ensure(
                end <= run.start && run.start < run.end && usize::from(run.end) <= width,
                || {
                    format!(
                        "run {}..{} after {end}, in {width} cells",
                        run.start, run.end
                    )
                },
            )?;
            end = run.end;
            let (uri, id) = (run.link.uri().len(), run.link.id().map_or(0, str::len));
            crate::check::ensure(uri <= URI_LIMIT && id <= ID_LIMIT, || {
                format!("a link of {uri} and {id} bytes")
            })?;
        }
        Ok(())
    }
}

#[cfg(feature = "checks")]
impl Links {
    /// 23 and 24: the held cost is at most the bound and at least what the
    /// rows' links cost; each link with an `id` is found by it.
    pub(crate) fn check<'a>(&self, rows: impl Iterator<Item = Runs<'a>>) -> Result<(), String> {
        let mut costs = std::collections::HashMap::new();
        for links in rows {
            for link in links.links() {
                costs.insert(link.key(), link.cost());
                if link.id().is_some() {
                    let found = self.find(link).map(Hyperlink::key);
                    crate::check::ensure(found == Some(link.key()), || {
                        format!("link {} with an id is not found by it", link.key())
                    })?;
                }
            }
        }
        let live = costs
            .values()
            .fold(0, |sum: usize, cost| sum.saturating_add(*cost));
        // Capacity excludes tombstones, so it only bounds `buckets` below.
        let map = table_bytes(self.buckets);
        let least = buckets(self.ids.capacity());
        crate::check::ensure(
            least <= self.buckets
                && live.saturating_add(map) <= self.held
                && self.held <= LINK_BYTES,
            || {
                format!(
                    "{} bytes of links held, the rows' cost {live} and the table's {map}",
                    self.held
                )
            },
        )
    }
}

/// What the open link is in one grid.
#[derive(Clone, Debug, Default)]
pub(crate) enum Held {
    /// Not printed there yet.
    #[default]
    Pending,
    At(Hyperlink),
    /// The grid had no room for it: cells printed there have no link.
    Refused,
}

/// Parses `OSC 8 ; params ; URI`: the link to open, or `None` to close.
/// A URI or `id` past its limit or with non-printable-ASCII bytes (left
/// undefined by the spec, and not passed on intact) opens nothing.
pub(crate) fn parse(payload: &[u8]) -> Option<(&str, Option<&str>)> {
    let (params, uri) = crate::split_semicolon(payload)?;
    // `id=value` among `:`-separated `key=value` pairs; empty means none.
    let id = params
        .split(|b| *b == b':')
        .find_map(|param| param.strip_prefix(b"id="))
        .filter(|id| !id.is_empty());
    let printable = |bytes: &[u8]| bytes.iter().all(|b| (0x20..=0x7e).contains(b));
    if uri.is_empty()
        || uri.len() > URI_LIMIT
        || !printable(uri)
        || id.is_some_and(|id| id.len() > ID_LIMIT || !printable(id))
    {
        return None;
    }
    let uri = str::from_utf8(uri).ok()?;
    let id = id.map(str::from_utf8).transpose().ok()?;
    Some((uri, id))
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A sweep that drops most `id` links shrinks the table and counts it
    /// as allocated, not by `capacity`, which excludes tombstones.
    #[test]
    fn the_id_table_is_counted_as_allocated_after_a_sweep() {
        let mut links = Links::default();
        let made: Vec<Hyperlink> = (0..896u64)
            .filter_map(|i| links.insert(&Hyperlink::new(i, "u", Some(&i.to_string()))))
            .collect();
        assert_eq!(made.len(), 896);
        let full = buckets(links.ids.capacity());
        assert_eq!(full, 1024, "the table after the 896th link");
        let mut row = RowLinks::default();
        for (i, link) in made.iter().take(200).enumerate() {
            assert!(row.set(i * 2..i * 2 + 1, Some(link)));
        }
        let lose = links.sweep(|| std::iter::once((None, row.runs())), 0);
        assert_eq!((lose, links.ids.len()), (0, 200));
        // Kept to 200, the table shrinks to 256 buckets, and held counts it.
        let now = buckets(links.ids.capacity());
        assert_eq!((now, links.ids.capacity()), (256, 224), "made smaller");
        let strings = made.iter().take(200).map(Hyperlink::cost).sum::<usize>();
        assert_eq!(links.held, strings + table_bytes(now));
    }

    #[test]
    fn a_payload_opens_its_link_or_closes() {
        assert_eq!(parse(b";http://a"), Some(("http://a", None)));
        assert_eq!(parse(b"id=x;http://a"), Some(("http://a", Some("x"))));
        assert_eq!(
            parse(b"foo=bar:id=x:baz=1;http://a;b"),
            Some(("http://a;b", Some("x")))
        );
        // An empty id is no id; an empty URI closes the link.
        assert_eq!(parse(b"id=;http://a"), Some(("http://a", None)));
        assert_eq!(parse(b";"), None);
        assert_eq!(parse(b"id=x;"), None);
        assert_eq!(parse(b""), None);
        // Past the limits, or not printable ASCII, nothing is opened.
        let long: Vec<u8> = std::iter::repeat_n(b'a', URI_LIMIT).collect();
        let mut payload = b";".to_vec();
        payload.extend_from_slice(&long);
        assert!(parse(&payload).is_some());
        payload.push(b'a');
        assert_eq!(parse(&payload), None);
        let mut payload = b"id=".to_vec();
        payload.extend(std::iter::repeat_n(b'i', ID_LIMIT));
        payload.extend_from_slice(b";u");
        assert!(parse(&payload).is_some());
        let mut payload = b"id=i".to_vec();
        payload.extend(std::iter::repeat_n(b'i', ID_LIMIT));
        payload.extend_from_slice(b";u");
        assert_eq!(parse(&payload), None);
        assert_eq!(parse(";http://é".as_bytes()), None);
        assert_eq!(parse(b";http://a\x07b"), None);
        assert_eq!(parse(b"id=\x01;http://a"), None);
    }
}

/// The links' checks, each shown links with their one fault.
#[cfg(all(test, feature = "checks"))]
mod faults {
    use super::*;

    fn link(key: u64, id: Option<&str>) -> Hyperlink {
        Hyperlink::new(key, "http://a", id)
    }

    #[test]
    fn runs_out_of_order_or_past_the_row_are_faults() {
        let mut row = RowLinks::default();
        assert!(row.set(0..2, Some(&link(1, None))));
        assert!(row.set(3..4, Some(&link(2, None))));
        assert_eq!(row.runs().check(4), Ok(()));
        assert!(row.runs().check(3).is_err(), "past the row");
        row.0.reverse();
        assert!(row.runs().check(4).is_err(), "out of order");
    }

    #[test]
    fn links_held_for_less_than_the_rows_cost_or_lost_ids_are_faults() {
        let mut links = Links::default();
        let opened = link(1, Some("i"));
        let Some(held) = links.insert(&opened) else {
            return;
        };
        let mut row = RowLinks::default();
        assert!(row.set(0..1, Some(&held)));
        assert_eq!(links.check([row.runs()].into_iter()), Ok(()));
        links.held = 0;
        assert!(
            links.check([row.runs()].into_iter()).is_err(),
            "held too little"
        );
        links.held = held.cost();
        links.ids.clear();
        assert!(
            links.check([row.runs()].into_iter()).is_err(),
            "an id not found"
        );
    }
}
