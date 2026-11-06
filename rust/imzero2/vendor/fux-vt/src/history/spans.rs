//! History's rows by identity (`THREATS.md` B40), kept as spans of
//! consecutive identities, each keyed by its first. Identities are never
//! reused, so spans never overlap whatever their order; a lookup reads
//! one row to make sure.

use std::collections::{BTreeMap, VecDeque};

/// What a span holds, at most: its entry in `by_id` (a key, a value and
/// its share of a B-tree node, counted at a node half full, and of the
/// nodes above), and its place in `order` (with room for as many more).
pub(crate) const SPAN_BYTES: usize = 80;
/// The spans a history holds free of its budget (20 KiB), more than an
/// ordinary history ever needs; past them each costs `SPAN_BYTES`.
pub(crate) const FREE_SPANS: usize = 256;

#[derive(Clone, Debug)]
pub(crate) struct Spans {
    /// The identity the newest span goes on with; `u64::MAX`, which no
    /// row has, while there is none.
    next: u64,
    /// Each span's first identity, and the number its first row came in
    /// history with.
    by_id: BTreeMap<u64, u64>,
    /// The spans' first numbers and identities, oldest first.
    order: VecDeque<(u64, u64)>,
}

impl Default for Spans {
    fn default() -> Self {
        Self {
            next: u64::MAX,
            by_id: BTreeMap::new(),
            order: VecDeque::new(),
        }
    }
}

impl Spans {
    /// Whether a row of identity `id` coming next begins a span; and the
    /// identity the next row goes on with.
    #[inline(always)]
    pub(crate) fn went_on(&mut self, id: u64) -> bool {
        let begins = id != self.next;
        self.next = id.wrapping_add(1);
        begins
    }
    /// The row of identity `id`, numbered `number`, begins a span; the
    /// spans of rows all let go before `first` go first.
    pub(crate) fn begin(&mut self, id: u64, number: u64, first: u64) {
        while self.order.get(1).is_some_and(|&(start, _)| start <= first) {
            if let Some((_, gone)) = self.order.pop_front() {
                self.by_id.remove(&gone);
            }
        }
        self.by_id.insert(id, number);
        self.order.push_back((number, id));
    }
    /// The row numbered `number`, the newest, went: the span it began
    /// goes with it, and the next row begins one.
    pub(crate) fn pop(&mut self, number: u64) {
        if let Some(&(start, id)) = self.order.back()
            && start == number
        {
            self.order.pop_back();
            self.by_id.remove(&id);
        }
        self.next = u64::MAX;
    }
    pub(crate) fn len(&self) -> usize {
        self.by_id.len()
    }
    /// What it costs history's budget: its spans past `FREE_SPANS`.
    pub(crate) fn charged(&self) -> usize {
        self.len()
            .saturating_sub(FREE_SPANS)
            .saturating_mul(SPAN_BYTES)
    }
    /// The number the row of identity `id` came with, if a span holds
    /// it; a row let go, or past its span's end, is not that row, which
    /// the caller reads to make sure.
    pub(crate) fn number_of(&self, id: u64) -> Option<u64> {
        let (&start, &number) = self.by_id.range(..=id).next_back()?;
        number.checked_add(id.checked_sub(start)?)
    }
}
