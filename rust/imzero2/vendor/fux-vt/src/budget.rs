//! The work one call may do before it stops (THREATS.md 4).
//!
//! A few bytes of input can ask for work in proportion to the screen, to
//! history or to a picture (a rectangle edit, a scroll, REP, a sweep). fux
//! reads every pane on one thread, so all of fux-vt's work is spent here as
//! it is done, one unit an element visited (a cell, a row, a row slot, a
//! text byte, a pixel, a style or link entry), and
//! [`Parser::process_until_stop`](crate::Parser::process_until_stop) stops
//! at the next place it looks once [`LIMIT`] is spent
//! ([`Stop::Budget`](crate::Stop::Budget)). Spending is kept per thread,
//! and a call compares before and after, so several parsers on one thread
//! don't mix.

use std::cell::Cell;

thread_local! {
    /// What this thread's calls into fux-vt have spent.
    static SPENT: Cell<u64> = const { Cell::new(0) };
}

/// The most a call of `process_until_stop` spends before it stops, over
/// as many calls as it takes: past it, by at most the one indivisible
/// operation that crossed it (THREATS.md 4).
pub(crate) const LIMIT: u64 = 1_000_000;

/// Spends `n` units. The count wraps rather than saturates, so a spend is
/// one add: a thread visits fewer than 2^64 elements in centuries.
#[inline(always)]
pub(crate) fn spend(n: impl TryInto<u64>) {
    let n = n.try_into().unwrap_or(u64::MAX);
    SPENT.with(|spent| spent.set(spent.get().wrapping_add(n)));
}

/// The most one call of `process_until_stop` spends at a screen of `size`
/// with `history` bytes of history: the quota and the largest indivisible
/// operation, every term summed (THREATS.md 4.2). A sixel picture's growth
/// (twice `MAX_PIXELS`) and its covering (`COVER_WORDS`); a placement's
/// rows, at most 683 and 8 graphics new lines, each 3C + 6 (B05); a screen-wide
/// function, 12 units a cell for a copy (its cells, compaction and links)
/// and each row's text, at most 49C + 128 bytes, twice (copied up front and
/// stored); a sweep of
/// history, its cells, 8 bytes each, twice, and its rows, 24 bytes each;
/// the link sweep's runs, `ids` and sorts, 2^20.
#[cfg(feature = "checks")]
pub(crate) fn call_most(size: crate::Size, history: usize) -> u64 {
    let (r, c) = (u64::from(size.rows()), u64::from(size.cols()));
    let h = u64::try_from(history).unwrap_or(u64::MAX);
    let picture = 2 * 4096 * 2160 + 4_000_000;
    let placement = c.saturating_mul(3).saturating_add(6).saturating_mul(691);
    let text = r.saturating_mul(c.saturating_mul(49).saturating_add(128));
    let screen = (r.saturating_mul(c).saturating_mul(12)).saturating_add(text.saturating_mul(2));
    let cells = (h / 8).min(1 << 26).saturating_mul(2);
    [
        LIMIT,
        picture,
        placement,
        screen,
        cells,
        (h / 24).min(1 << 20),
        1 << 20,
    ]
    .into_iter()
    .fold(0, u64::saturating_add)
}

pub(crate) fn spent() -> u64 {
    SPENT.with(Cell::get)
}

/// Sets what this thread has spent back to `n`: for the checker, whose work
/// is no call's (`check::unseen`).
#[cfg(feature = "checks")]
pub(crate) fn rewind(n: u64) {
    SPENT.with(|spent| spent.set(n));
}
