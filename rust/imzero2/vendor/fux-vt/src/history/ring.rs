//! History's ring of row records, oldest first, in fixed-size chunks made
//! as the last fills and freed as the first empties: the room taken is
//! what is kept plus at most two chunks, and no record ever moves
//! (`THREATS.md` B17).

use super::Kept;
use crate::Error;

#[derive(Debug, Default)]
pub(crate) struct Ring {
    /// The chunks before the last, each full, from `start` on: those
    /// before it are let go, and empty, until there are as many of them as
    /// of the rest, when they are dropped together.
    full: Vec<Vec<Kept>>,
    start: usize,
    /// The last chunk, which records fill; none yet if it holds no memory.
    last: Vec<Kept>,
    /// Where the first record is in the first chunk.
    head: usize,
    len: usize,
    shift: u32,
}

/// A copy keeps the last chunk's room, as a copy of a `Vec` would not: the
/// chunks hold `1 << shift` records each.
impl Clone for Ring {
    fn clone(&self) -> Self {
        let mut last = Vec::with_capacity(self.last.capacity());
        last.extend_from_slice(&self.last);
        Self {
            full: self.full.get(self.start..).unwrap_or_default().to_vec(),
            start: 0,
            last,
            head: self.head,
            len: self.len,
            shift: self.shift,
        }
    }
}

impl Ring {
    /// A ring of chunks of `1 << shift` records.
    pub(crate) fn new(shift: u32) -> Self {
        Self {
            shift,
            ..Self::default()
        }
    }
    pub(crate) fn chunk(&self) -> usize {
        1usize.checked_shl(self.shift).unwrap_or(1)
    }
    pub(crate) fn len(&self) -> usize {
        self.len
    }
    pub(crate) fn is_empty(&self) -> bool {
        self.len == 0
    }
    fn full(&self) -> &[Vec<Kept>] {
        self.full.get(self.start..).unwrap_or_default()
    }
    pub(crate) fn capacity(&self) -> usize {
        (self.full().len().saturating_mul(self.chunk())).saturating_add(self.last.capacity())
    }
    #[inline(always)]
    pub(crate) fn has_room(&self) -> bool {
        self.last.len() < self.last.capacity()
    }
    /// Starts a new last chunk; fails, with nothing changed, only when
    /// out of memory.
    pub(crate) fn grow(&mut self) -> Result<(), Error> {
        if !self.last.is_empty() {
            self.full.try_reserve(1).map_err(|_| Error::Capacity)?;
        }
        let mut chunk = Vec::new();
        chunk
            .try_reserve_exact(self.chunk())
            .map_err(|_| Error::Capacity)?;
        let last = std::mem::replace(&mut self.last, chunk);
        if !last.is_empty() {
            self.full.push(last);
        }
        Ok(())
    }
    /// Appends `kept`; the caller checks `has_room` first.
    #[inline(always)]
    pub(crate) fn push_back(&mut self, kept: Kept) {
        self.last.push(kept);
        self.len = self.len.saturating_add(1);
    }
    /// Takes the first record, letting its chunk go once it has none left;
    /// and whether it did.
    #[inline]
    pub(crate) fn pop_front(&mut self) -> Option<(Kept, bool)> {
        let kept = *self.front_run().first()?;
        Some((kept, self.advance(1)))
    }
    /// The first records, to the end of the first chunk.
    #[inline]
    pub(crate) fn front_run(&self) -> &[Kept] {
        let first = self.full().first().unwrap_or(&self.last);
        first.get(self.head..).unwrap_or_default()
    }
    /// Takes the first `n` records, of `front_run`, letting their chunk go
    /// if they were its last; and whether they did.
    #[inline]
    pub(crate) fn advance(&mut self, n: usize) -> bool {
        self.head = self.head.saturating_add(n);
        self.len = self.len.saturating_sub(n);
        let freed = self.head >= self.chunk() && !self.full().is_empty();
        if freed {
            self.let_first_go();
            self.head = 0;
        } else if self.len == 0 {
            self.last.clear();
            self.head = 0;
        }
        freed
    }
    /// Lets the first full chunk go; and the list's room for those let go,
    /// once they are as many as the rest.
    fn let_first_go(&mut self) {
        if let Some(first) = self.full.get_mut(self.start) {
            *first = Vec::new();
            self.start = self.start.saturating_add(1);
        }
        if self.start.saturating_mul(2) >= self.full.len() {
            let rest = std::mem::take(&mut self.full).into_iter().skip(self.start);
            self.full = rest.collect();
            self.start = 0;
        }
    }
    /// Takes the last record, its chunk going once it holds none; and
    /// whether the ring's room changed.
    pub(crate) fn pop_back(&mut self) -> Option<(Kept, bool)> {
        let freed = self.last.is_empty() && !self.full().is_empty();
        if freed && let Some(chunk) = self.full.pop() {
            self.last = chunk;
        }
        let kept = self.last.pop()?;
        self.len = self.len.saturating_sub(1);
        if self.len == 0 {
            self.last.clear();
            self.head = 0;
        }
        Some((kept, freed))
    }
    #[inline(always)]
    pub(crate) fn get(&self, index: usize) -> Option<&Kept> {
        if index >= self.len {
            return None;
        }
        let at = self.head.wrapping_add(index);
        let chunk = at.checked_shr(self.shift).unwrap_or(0);
        let place = at & self.chunk().wrapping_sub(1);
        match self.full.get(self.start.wrapping_add(chunk)) {
            Some(full) => full.get(place),
            None => self.last.get(place),
        }
    }
    pub(crate) fn get_mut(&mut self, index: usize) -> Option<&mut Kept> {
        if index >= self.len {
            return None;
        }
        let at = self.head.wrapping_add(index);
        let chunk = at.checked_shr(self.shift).unwrap_or(0);
        let place = at & self.chunk().wrapping_sub(1);
        match self.full.get_mut(self.start.wrapping_add(chunk)) {
            Some(full) => full.get_mut(place),
            None => self.last.get_mut(place),
        }
    }
    /// The last record, which `pop_back` may leave in a full chunk.
    pub(crate) fn back_mut(&mut self) -> Option<&mut Kept> {
        self.get_mut(self.len.checked_sub(1)?)
    }
    pub(crate) fn iter(&self) -> impl Iterator<Item = &Kept> {
        let head = self.head;
        (self.full().iter().chain([&self.last]).enumerate()).flat_map(move |(n, chunk)| {
            chunk
                .get(if n == 0 { head } else { 0 }..)
                .unwrap_or_default()
        })
    }
    pub(crate) fn iter_mut(&mut self) -> impl Iterator<Item = &mut Kept> {
        let head = self.head;
        let full = self.full.get_mut(self.start..).unwrap_or_default();
        (full.iter_mut().chain([&mut self.last]).enumerate()).flat_map(move |(n, chunk)| {
            chunk
                .get_mut(if n == 0 { head } else { 0 }..)
                .unwrap_or_default()
        })
    }
}
