//! The unit tests' generator: splitmix64, seeded by each test.

pub(crate) struct Rng(pub(crate) u64);

impl Rng {
    pub(crate) fn next(&mut self) -> u64 {
        self.0 = self.0.wrapping_add(0x9e3779b97f4a7c15);
        let mut x = self.0;
        x = (x ^ (x >> 30)).wrapping_mul(0xbf58476d1ce4e5b9);
        x = (x ^ (x >> 27)).wrapping_mul(0x94d049bb133111eb);
        x ^ (x >> 31)
    }
    /// Below `n`, or 0 for an `n` of 0.
    pub(crate) fn below(&mut self, n: usize) -> usize {
        let n = u64::try_from(n).unwrap_or(u64::MAX);
        usize::try_from(self.next().checked_rem(n).unwrap_or(0)).unwrap_or(0)
    }
    /// From 1 to `n`.
    pub(crate) fn upto(&mut self, n: usize) -> usize {
        self.below(n).saturating_add(1)
    }
    pub(crate) fn pick<T: Copy>(&mut self, items: &[T]) -> Option<T> {
        items.get(self.below(items.len())).copied()
    }
    pub(crate) fn chance(&mut self, percent: usize) -> bool {
        self.below(100) < percent
    }
    /// From `low` to `high`, both included.
    pub(crate) fn byte_in(&mut self, low: u8, high: u8) -> u8 {
        let span = usize::from(high.saturating_sub(low)).saturating_add(1);
        low.saturating_add(u8::try_from(self.below(span)).unwrap_or(0))
    }
}
