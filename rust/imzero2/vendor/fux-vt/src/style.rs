//! Styles: a cell holds a 28-bit style number, not its attributes.
//!
//! The styles programs mostly use encode themselves in the number
//! (`Attributes::inline_style`): default and indexed colours, at most a
//! single underline with no colour, no rapid blink; or a direct foreground
//! on the default background, bold and/or italic, as syntax highlighting
//! prints. They cost no table search. Style 0 is the default, so a blank
//! cell is all zeros.
//!
//! Every other style is kept once in a grid's table, its number being its
//! place there with [`TABLE`] set. Cells are not reference counted.
//! Instead, once the table has grown to twice the styles in use at the
//! last sweep, and past a floor that grows with the grid, the grid sweeps
//! it (`Grid::sweep`), keeping the styles its cells use, in order, and
//! renumbering its cells. After a sweep that keeps L styles the next comes
//! only after max(floor − L, L) new ones, at least half the floor, which is
//! an eighth of the grid's cells or more, so a style costs a few cell reads
//! however many there are (`THREATS.md` B13). No reader sees a style's
//! number.

use std::collections::HashMap;
use std::hash::{BuildHasher, Hasher, RandomState};

use crate::Attributes;

/// The bit of a style's number that says it is in the table.
pub(crate) const TABLE: u32 = 1 << 27;

/// The most styles a table holds: the numbers below [`TABLE`]. More than a
/// grid's cells (`grid::MAX_CELLS`), so there is always room after a sweep.
pub(crate) const MAX_STYLES: usize = 1 << 27;

/// The fewest styles a table holds before it is swept.
const FLOOR: usize = 4096;

/// A grid's table of styles: their attributes by place, and each one's
/// place by its attributes, found by a keyed hash ([`Keyed`]), so that a
/// program can't choose styles that collide (`THREATS.md` B12). Probes per
/// insert are expected O(1); the ceiling is the table's length, which a
/// sweep bounds (B13).
#[derive(Clone, Default)]
pub(crate) struct Styles {
    table: Vec<Attributes>,
    places: HashMap<Attributes, u32, Keyed>,
    /// How many styles were in use when the table was last swept.
    live: usize,
}

/// The tables' hasher. Zero-sized: its keys are drawn once a process from
/// std's random keys, so a program sees neither them nor any hash. Each
/// word is folded in by one 128-bit multiply by a key, as foldhash does.
#[derive(Clone, Copy, Default)]
pub struct Keyed;

/// The process's keys: the seed, and the key, odd so that no word folds
/// to nothing.
fn keys() -> (u64, u64) {
    static KEYS: std::sync::OnceLock<(u64, u64)> = std::sync::OnceLock::new();
    *KEYS.get_or_init(|| {
        let random = RandomState::new();
        (random.hash_one(0u64), random.hash_one(1u64) | 1)
    })
}

impl BuildHasher for Keyed {
    type Hasher = KeyedHasher;
    fn build_hasher(&self) -> KeyedHasher {
        let (state, key) = keys();
        KeyedHasher { state, key }
    }
}

pub struct KeyedHasher {
    state: u64,
    key: u64,
}

/// The halves of `x × y`, folded: every bit of both reaches each.
fn folded(x: u64, y: u64) -> u64 {
    let full = u128::from(x).wrapping_mul(u128::from(y));
    let [low, high] = [full, full >> 64].map(|h| u64::try_from(h & u128::from(u64::MAX)));
    low.unwrap_or(0) ^ high.unwrap_or(0)
}

impl Hasher for KeyedHasher {
    fn finish(&self) -> u64 {
        self.state
    }
    fn write(&mut self, bytes: &[u8]) {
        for byte in bytes {
            self.write_u64(u64::from(*byte));
        }
    }
    fn write_u64(&mut self, n: u64) {
        self.state = folded(self.state ^ n, self.key);
    }
    /// Both halves at once, each masked by a key, folded in one multiply.
    fn write_u128(&mut self, n: u128) {
        let [high, low] = [n >> 64, n].map(|h| u64::try_from(h & u128::from(u64::MAX)));
        self.state = folded(high.unwrap_or(0) ^ self.state, low.unwrap_or(0) ^ self.key);
    }
}

/// Leaves out `places`, whose order depends on the keys.
impl std::fmt::Debug for Styles {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Styles")
            .field("table", &self.table)
            .field("live", &self.live)
            .finish_non_exhaustive()
    }
}

impl Styles {
    /// The attributes of style `style`; the default for a number with no
    /// style.
    #[inline]
    pub(crate) fn get(&self, style: u32) -> Attributes {
        if style & TABLE == 0 {
            return Attributes::from_inline_style(style);
        }
        Self::place(style)
            .and_then(|i| self.table.get(i))
            .map_or_else(Attributes::default, |a| a.with_guard(false))
    }

    /// Whether style `style` is a guarded glyph's (`Attributes::GUARDED`).
    pub(crate) fn guarded(&self, style: u32) -> bool {
        Self::place(style)
            .and_then(|i| self.table.get(i))
            .is_some_and(|a| a.flags & Attributes::GUARDED != 0)
    }

    #[inline]
    pub(crate) fn place(style: u32) -> Option<usize> {
        if style & TABLE == 0 {
            return None;
        }
        usize::try_from(style & !TABLE).ok()
    }

    pub(crate) fn len(&self) -> usize {
        self.table.len()
    }

    /// The bytes the table takes: its entries' room and its map's buckets.
    pub(crate) fn bytes(&self) -> usize {
        table_bytes(self.table.capacity(), self.places.capacity())
    }

    /// Whether one more style grows its room, the entries' or the map's.
    pub(crate) fn full(&self) -> bool {
        self.table.len() == self.table.capacity() || self.places.len() == self.places.capacity()
    }

    /// The bytes it takes once one more style is in: each room doubled if
    /// full.
    pub(crate) fn bytes_after_insert(&self) -> usize {
        let grown = |len: usize, room: usize| match len < room {
            true => room,
            false => room.saturating_mul(2).max(4),
        };
        table_bytes(
            grown(self.table.len(), self.table.capacity()),
            grown(self.places.len(), self.places.capacity()),
        )
    }

    pub(crate) fn find(&self, attributes: Attributes) -> Option<u32> {
        self.places.get(&attributes).map(|place| place | TABLE)
    }

    /// Whether the table is to be swept before another style is added: it
    /// is full, or has twice the styles in use at the last sweep and the
    /// floor, at least an eighth of the grid's `cells`.
    pub(crate) fn wants_sweep(&self, cells: usize) -> bool {
        let floor = FLOOR.max(cells / 8);
        let len = self.table.len();
        len >= MAX_STYLES || (len >= floor && len >= self.live.saturating_mul(2))
    }

    /// Adds a style with `attributes`, which the table does not have; its
    /// number, or `None` if the table is full.
    pub(crate) fn insert(&mut self, attributes: Attributes) -> Option<u32> {
        if self.table.len() >= MAX_STYLES {
            return None;
        }
        let place = u32::try_from(self.table.len()).ok()?;
        self.table.push(attributes);
        self.places.insert(attributes, place);
        Some(place | TABLE)
    }

    /// Keeps the styles `used` marks, by place, in their order. What each
    /// style's number becomes; a style let go becomes 0.
    pub(crate) fn retain(&mut self, used: &[bool]) -> Vec<u32> {
        let table = std::mem::take(&mut self.table);
        // Room for twice the styles kept, as many as the next sweep lets in
        // past the floor: a map with more is made anew, as one cleared
        // keeps its buckets, and its bytes count (B11).
        let kept = used.iter().filter(|used| **used).count();
        let room = kept.saturating_mul(2).max(FLOOR);
        if self.places.capacity() > room {
            self.places = HashMap::with_capacity_and_hasher(room, Keyed);
        } else {
            self.places.clear();
        }
        let used = used.iter().chain(std::iter::repeat(&false));
        let renumber = (table.iter().zip(used))
            .map(|(attributes, used)| match used {
                true => self.insert(*attributes).unwrap_or(0),
                false => 0,
            })
            .collect();
        self.live = self.table.len();
        renumber
    }
}

/// The bytes of a table with room for `entries` and a map with room for
/// `room`: hashbrown allocates the next power of two past 8 buckets per 7
/// of room, each holding an entry and a control byte.
fn table_bytes(entries: usize, room: usize) -> usize {
    let buckets = match room {
        0 => 0,
        1..=7 => room.saturating_add(1).next_power_of_two(),
        _ => (room.saturating_mul(8) / 7).next_power_of_two(),
    };
    let bucket = size_of::<(Attributes, u32)>().saturating_add(1);
    (entries.saturating_mul(size_of::<Attributes>())).saturating_add(buckets.saturating_mul(bucket))
}

/// The bytes a screen of `cells` cells may keep in its style table beside
/// history's budget: a table of twice its cells' styles, or the floor's
/// (`THREATS.md` B11, S(2N)).
pub(crate) fn screen_share(cells: usize) -> usize {
    let entries = cells.saturating_mul(2).max(FLOOR).next_power_of_two();
    table_bytes(entries, entries.saturating_mul(7) / 8)
}

/// The table's part of the invariant checker (THREATS.md section 6).
#[cfg(feature = "checks")]
impl Styles {
    /// 15 and 16: each style is found at its place, no other, and the
    /// table holds at most [`MAX_STYLES`].
    pub(crate) fn check(&self) -> Result<(), String> {
        crate::check::ensure(
            self.places.len() == self.table.len() && self.table.len() <= MAX_STYLES,
            || {
                format!(
                    "{} places for {} styles",
                    self.places.len(),
                    self.table.len()
                )
            },
        )?;
        for (place, attributes) in self.table.iter().enumerate() {
            let found = self
                .places
                .get(attributes)
                .and_then(|p| usize::try_from(*p).ok());
            crate::check::ensure(found == Some(place), || {
                format!("style {place} is found at {found:?}")
            })?;
        }
        crate::check::ensure(self.live <= self.table.len(), || {
            format!("{} styles live of {}", self.live, self.table.len())
        })
    }

    /// 15: no more than [`SPREAD`] styles share a place in `places` by
    /// `hash` (the table's own, [`Keyed`]): a program that can't see the
    /// keys can't make styles collide (`THREATS.md` B12).
    pub(crate) fn check_spread(&self, hash: impl Fn(&Attributes) -> u64) -> Result<(), String> {
        // The places a table of them holds: 8 for each 7 it has room for.
        let places = (self.places.capacity().saturating_mul(8) / 7)
            .max(1)
            .next_power_of_two();
        let mask = u64::try_from(places.saturating_sub(1)).unwrap_or(u64::MAX);
        let mut shared = vec![0u32; places];
        let mut most = 0;
        for attributes in &self.table {
            let at = usize::try_from(hash(attributes) & mask).unwrap_or(0);
            if let Some(count) = shared.get_mut(at) {
                *count = count.saturating_add(1);
                most = most.max(*count);
            }
        }
        crate::check::ensure(most <= SPREAD, || {
            format!("{most} styles share a place of {places}")
        })
    }
}

/// The most styles a place of the table may get by the hash
/// (`Styles::check_spread`): random hashes of a table's styles give a few,
/// and the chance of more than 64 is nil.
#[cfg(feature = "checks")]
const SPREAD: u32 = 64;

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{Blink, Color, UnderlineStyle};

    #[derive(Default)]
    struct Recording(Vec<u64>);

    impl std::hash::Hasher for Recording {
        fn finish(&self) -> u64 {
            0
        }
        fn write(&mut self, bytes: &[u8]) {
            self.0.extend(bytes.iter().map(|b| u64::from(*b)));
        }
        fn write_u64(&mut self, n: u64) {
            self.0.push(n);
        }
        fn write_u128(&mut self, n: u128) {
            let [high, low] = [n >> 64, n].map(|h| u64::try_from(h & u128::from(u64::MAX)));
            self.0.extend([high.unwrap_or(0), low.unwrap_or(0)]);
        }
    }

    fn written(attributes: Attributes) -> Vec<u64> {
        let mut hasher = Recording::default();
        std::hash::Hash::hash(&attributes, &mut hasher);
        hasher.0
    }

    /// Attributes write both their words, whole, to the hasher, so two that
    /// differ write differently, and a keyed hash leaves no family a
    /// program could choose to collide (`THREATS.md` B12).
    #[test]
    fn attributes_hash_both_their_words() {
        for (a, b) in [(direct(1), direct(2)), (direct(7), direct(200))] {
            assert_eq!(written(a), [a.bits().0, a.bits().1]);
            assert_ne!(written(a), written(b));
        }
    }

    /// A style no number holds: a direct colour.
    fn direct(n: u8) -> Attributes {
        Attributes::new(Color::Idx(n), Color::Rgb([n, 0, n.wrapping_mul(3)].into()))
    }

    #[test]
    fn styles_of_the_table_are_found_by_their_attributes_and_numbered_in_order() {
        let mut styles = Styles::default();
        for n in 0..=255u8 {
            assert_eq!(styles.find(direct(n)), None);
            assert_eq!(styles.insert(direct(n)), Some(u32::from(n) | TABLE));
        }
        for n in 0..=255u8 {
            assert_eq!(styles.find(direct(n)), Some(u32::from(n) | TABLE));
            assert_eq!(styles.get(u32::from(n) | TABLE), direct(n));
        }
        assert_eq!(styles.get(9999 | TABLE), Attributes::default());
        // Keeping the odd ones renumbers them in order.
        let used: Vec<bool> = (0..styles.len()).map(|place| place % 2 == 1).collect();
        let renumber = styles.retain(&used);
        assert_eq!(styles.len(), 128);
        for n in 0..=255u8 {
            let place = usize::from(n);
            let new = renumber.get(place).copied().unwrap_or(u32::MAX);
            if place % 2 == 1 {
                assert_eq!(new, u32::try_from(place / 2).unwrap_or(0) | TABLE);
                assert_eq!(styles.find(direct(n)), Some(new));
            } else {
                assert_eq!((new, styles.find(direct(n))), (0, None));
            }
        }
    }

    /// Every style a number holds reads back as itself, and only those
    /// styles have numbers.
    #[test]
    fn a_style_of_its_own_number_reads_back_as_itself() {
        let styles = Styles::default();
        assert_eq!(Attributes::default().inline_style(), Some(0));
        let colours = [
            Color::Default,
            Color::Idx(0),
            Color::Idx(7),
            Color::Idx(255),
            Color::Rgb([0, 0, 0].into()),
            Color::Rgb([1, 2, 3].into()),
            Color::Rgb([255, 255, 255].into()),
        ];
        let blinks = [Blink::None, Blink::Slow, Blink::Rapid];
        let underlines = [
            UnderlineStyle::None,
            UnderlineStyle::Single,
            UnderlineStyle::Double,
            UnderlineStyle::Curly,
            UnderlineStyle::Dotted,
            UnderlineStyle::Dashed,
        ];
        let mut numbers = std::collections::HashSet::new();
        for (fg, bg) in colours
            .iter()
            .flat_map(|f| colours.iter().map(move |b| (*f, *b)))
        {
            for bits in 0..64u8 {
                for blink in blinks {
                    for underline in underlines {
                        let on = |bit: u8| bits & (1 << bit) != 0;
                        let a = Attributes::new(fg, bg)
                            .with_bold(on(0))
                            .with_dim(on(1))
                            .with_italic(on(2))
                            .with_inverse(on(3))
                            .with_hidden(on(4))
                            .with_strikeout(on(5))
                            .with_blink(blink)
                            .with_underline_style(underline);
                        let direct = |c: Color| matches!(c, Color::Rgb(..));
                        let own = if direct(fg) || direct(bg) {
                            direct(fg)
                                && bg == Color::Default
                                && underline == UnderlineStyle::None
                                && blink == Blink::None
                                && bits & 0b11_1010 == 0
                        } else {
                            matches!(underline, UnderlineStyle::None | UnderlineStyle::Single)
                                && blink != Blink::Rapid
                        };
                        let code = a.inline_style();
                        assert_eq!(code.is_some(), own, "{a:?}");
                        if let Some(code) = code {
                            assert!(code < TABLE, "{a:?}");
                            assert_eq!(styles.get(code), a, "{a:?}");
                            assert!(numbers.insert(code), "{a:?}");
                        }
                        assert_eq!(a.with_underline_color(Color::Idx(1)).inline_style(), None);
                        assert_eq!(
                            a.with_background(Color::Rgb([0, 0, 0].into()))
                                .inline_style(),
                            None
                        );
                    }
                }
            }
        }
    }

    #[test]
    fn a_sweep_is_wanted_past_twice_the_styles_in_use_and_the_floor() {
        let mut styles = Styles::default();
        let mut n = 0u32;
        while !styles.wants_sweep(0) {
            n = n.wrapping_add(1);
            let [_, r, g, b] = n.to_be_bytes();
            let attributes = Attributes::new(Color::Rgb([r, g, b].into()), Color::Default);
            assert!(styles.insert(attributes).is_some());
        }
        assert_eq!(styles.len(), FLOOR);
        // A bigger grid waits for more.
        assert!(!styles.wants_sweep(FLOOR * 16));
        let used = vec![true; styles.len()];
        styles.retain(&used);
        assert!(!styles.wants_sweep(0));
    }
}

/// The table's check, shown a table with its one fault.
#[cfg(all(test, feature = "checks"))]
mod faults {
    use super::*;
    use crate::Color;

    #[test]
    fn a_style_found_at_another_place_is_a_fault() {
        let mut styles = Styles::default();
        for n in 0..4u8 {
            let attributes = Attributes::new(Color::Default, Color::Rgb([n, 1, 2].into()));
            assert!(styles.insert(attributes).is_some());
        }
        assert_eq!(styles.check(), Ok(()));
        if let Some(place) = styles.places.values_mut().next() {
            *place = place.wrapping_add(1) % 4;
        }
        assert!(styles.check().is_err());
        styles.places.clear();
        assert!(styles.check().is_err());
    }

    /// Styles that hash alike are a fault of the spread: 65 at one place.
    #[test]
    fn styles_that_hash_alike_are_a_fault() {
        let mut styles = Styles::default();
        for n in 0..65u8 {
            let attributes = Attributes::new(Color::Default, Color::Rgb([n, 1, 2].into()));
            assert!(styles.insert(attributes).is_some());
        }
        assert_eq!(styles.check_spread(|a| Keyed.hash_one(a)), Ok(()));
        assert!(styles.check_spread(|_| 7).is_err());
    }
}
