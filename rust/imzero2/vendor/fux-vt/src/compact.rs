//! How cells are stored, eight bytes each: text, halves and style number
//! (`style.rs`). Readers see a [`CellRef`] instead.
//!
//! Up to four bytes of text are inline. A longer cluster goes in its
//! row's [`Text`]: short ones (5 to 17 bytes) in a part that always has
//! room, long ones in a part bounded at 32 bytes a cell plus one cluster.

use crate::CellRef;
use crate::cell::{ASCII, CLUSTER_CAPACITY, floor};
use crate::style::Styles;

/// A stored cell. Inline, `text` is the text then zeros (text never holds
/// NUL). Spilled, it locates the text in its row's [`Text`]: a 24-bit
/// little-endian offset, then a length (over 17 means long). `word` is the
/// 28-bit style number and the flags; in a `Cells` the style is 0. All
/// zeros is a blank cell in the default attributes.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub(crate) struct Compact {
    text: [u8; 4],
    word: u32,
}

const _: () = assert!(size_of::<Compact>() == 8);

/// What a row is past the cells it keeps.
pub(crate) const BLANK: Compact = Compact {
    text: [0; 4],
    word: 0,
};

/// The text of a row that keeps none.
pub(crate) static NO_TEXT: Text = Text {
    long: Vec::new(),
    short: Vec::new(),
};

const STYLE: u32 = (1 << 28) - 1;
/// Wide-glyph halves; shifted down 22, they are [`CellRef`]'s bits.
const CONTINUATION: u32 = 1 << 28;
const WIDE: u32 = 1 << 29;
const HALVES: u32 = WIDE | CONTINUATION;
/// The text is in the row's text.
const SPILLED: u32 = 1 << 30;
/// The glyph is protected from selective erase (DECSCA 1, or SPA). A
/// blank, and a wide glyph's second half, never have it (its first half
/// says).
pub(crate) const PROTECTED: u32 = 1 << 31;
/// What a cell keeps of its word when its text changes.
const KEPT: u32 = STYLE | HALVES | PROTECTED;
const _: () = assert!(HALVES >> 22 == CellRef::HALVES as u32);
const _: () = assert!(SHORT == 17 && SPILLED == 1 << 30);

/// The longest short cluster; a long one without room is cut to this.
const SHORT: usize = 17;
/// The bytes a row's short text first takes room for.
const FIRST_SHORT: usize = 256;
/// The bytes of a row's text a locator's 24 bits reach.
const LOCATED: usize = 1 << 24;

impl Compact {
    #[inline]
    pub(crate) fn blank(style: u32) -> Self {
        Self {
            text: [0; 4],
            word: style & STYLE,
        }
    }
    /// The glyph `c`, `width` columns wide; `style` may carry [`PROTECTED`].
    #[inline]
    pub(crate) fn glyph(c: char, width: usize, style: u32) -> Self {
        // A char is at most four UTF-8 bytes; the rest stay zeros.
        let mut text = [0; 4];
        c.encode_utf8(&mut text);
        Self {
            text,
            word: (style & (STYLE | PROTECTED)) | if width == 2 { WIDE } else { 0 },
        }
    }
    #[inline]
    pub(crate) fn ascii(byte: u8, style: u32) -> Self {
        Self {
            text: [byte, 0, 0, 0],
            word: style & (STYLE | PROTECTED),
        }
    }
    /// Whether any of `cells` is either half of a wide glyph.
    pub(crate) fn any_halves(cells: &[Self]) -> bool {
        cells.iter().fold(0, |any, cell| any | cell.word) & HALVES != 0
    }
    /// A blank cell in style 0, the halves `halves` says ([`CellRef`]'s).
    #[inline]
    pub(crate) fn shaped(halves: u8) -> Self {
        Self {
            text: [0; 4],
            word: (u32::from(halves) << 22) & HALVES,
        }
    }
    /// The second half of a wide glyph: no text, the default style.
    pub(crate) fn continuation() -> Self {
        Self {
            text: [0; 4],
            word: CONTINUATION,
        }
    }
    #[inline]
    pub(crate) fn style(&self) -> u32 {
        self.word & STYLE
    }
    #[inline]
    pub(crate) fn set_style(&mut self, style: u32) {
        self.word = (self.word & !STYLE) | (style & STYLE);
    }
    /// The cell with no text and neither half, in its style.
    #[inline]
    pub(crate) fn blanked(&self) -> Self {
        Self::blank(self.style())
    }
    /// Neither blank nor the second half of a wide glyph.
    #[inline]
    pub(crate) fn has_contents(&self) -> bool {
        let [first, ..] = self.text;
        first != 0 || self.is_spilled()
    }
    #[inline]
    pub(crate) fn is_wide(&self) -> bool {
        self.word & WIDE != 0
    }
    /// Whether the cell is the second half of the wide glyph before it.
    #[inline]
    pub(crate) fn is_wide_continuation(&self) -> bool {
        self.word & CONTINUATION != 0
    }
    pub(crate) fn widen(&mut self) {
        self.word |= WIDE;
    }
    /// Whether the cell's text is in its row's text.
    #[inline]
    pub(crate) fn is_spilled(&self) -> bool {
        self.word & SPILLED != 0
    }
    /// Whether the glyph is protected from selective erase (DECSCA, SPA).
    #[inline]
    pub(crate) fn is_protected(&self) -> bool {
        self.word & PROTECTED != 0
    }
    /// Whether the cell (row text `text`) equals `other` (text `held`).
    pub(crate) fn holds(&self, text: &Text, other: &Self, held: &str) -> bool {
        if !self.is_spilled() && !other.is_spilled() {
            return self == other;
        }
        self.word == other.word && text.of(self) == held
    }
    #[inline]
    pub(crate) fn is_blank(&self, style: u32) -> bool {
        *self == Self::blank(style)
    }
    #[inline]
    pub(crate) fn is_ascii(&self, byte: u8, style: u32) -> bool {
        *self == Self::ascii(byte, style)
    }
    /// The halves, as [`CellRef`]'s.
    #[inline]
    fn halves(&self) -> u8 {
        let [halves, ..] = ((self.word & HALVES) >> 22).to_le_bytes();
        halves
    }
    /// The inline text; empty for a spilled cell.
    #[inline]
    fn inline(&self) -> &str {
        if self.is_spilled() {
            return "";
        }
        let word = u32::from_le_bytes(self.text);
        // One ASCII byte or none: skip UTF-8 validation, which readers
        // that walk every cell (copy, search) would pay on each.
        if word < 0x80 {
            let [at, ..] = self.text;
            let at = usize::from(at);
            let len = usize::from(at != 0);
            return ASCII.get(at..at.saturating_add(len)).unwrap_or("");
        }
        str::from_utf8(self.inline_bytes()).unwrap_or("")
    }
    /// The inline text's bytes, unread as UTF-8; none for a spilled cell.
    #[inline]
    fn inline_bytes(&self) -> &[u8] {
        if self.is_spilled() {
            return &[];
        }
        // The bytes up to the last that is not zero: text has none inside.
        let word = u32::from_le_bytes(self.text);
        let len = 32u32.saturating_sub(word.leading_zeros()).div_ceil(8);
        usize::try_from(len)
            .ok()
            .and_then(|len| self.text.get(..len))
            .unwrap_or_default()
    }
    /// The cell with `c` after its inline text, if both fit inline. A cell
    /// with no text has none to follow (`Line::append` gives it a space).
    #[inline]
    fn inline_with(self, c: char) -> Option<Self> {
        // Full already, as a character of four bytes leaves it.
        let [.., last] = self.text;
        if last != 0 || self.is_spilled() || !self.has_contents() {
            return None;
        }
        let word = u32::from_le_bytes(self.text);
        let used = 32u32.saturating_sub(word.leading_zeros()).div_ceil(8);
        let mut encoded = [0; 4];
        let length = u32::try_from(c.encode_utf8(&mut encoded).len()).ok()?;
        if used.checked_add(length)? > 4 {
            return None;
        }
        let added = u32::from_le_bytes(encoded).checked_shl(used.checked_mul(8)?)?;
        Some(Self {
            text: (word | added).to_le_bytes(),
            word: self.word & KEPT,
        })
    }
    /// The first character of the inline text; none for a spilled cell.
    #[inline]
    pub(crate) fn first(&self) -> Option<char> {
        self.inline().chars().next()
    }
    /// Where a spilled cell's text is in its row's text, and its length.
    #[inline]
    fn locator(&self) -> Option<(usize, usize)> {
        if !self.is_spilled() {
            return None;
        }
        let [a, b, c, len] = self.text;
        let start = usize::try_from(u32::from_le_bytes([a, b, c, 0])).ok()?;
        Some((start, usize::from(len)))
    }
    /// The range of a long (`long`) or short cluster's text in its part.
    fn spilled(&self, long: bool) -> Option<std::ops::Range<usize>> {
        let (start, len) = self.locator().filter(|(_, len)| (*len > SHORT) == long)?;
        Some(start..start.checked_add(len)?)
    }
    /// The cell, keeping its halves and style, holding `text` inline, if
    /// it fits.
    #[inline]
    pub(crate) fn holding(self, text: &[u8]) -> Option<Self> {
        let mut bytes = [0; 4];
        match (text, &mut bytes) {
            ([byte], [first, ..]) => *first = *byte,
            (text, bytes) => crate::copy_from(bytes.get_mut(..text.len())?, text)?,
        }
        Some(Self {
            text: bytes,
            word: self.word & KEPT,
        })
    }
    /// The cell, keeping its halves and style, locating `len` bytes of its
    /// row's text from `start`; blank if `start` takes more than 24 bits.
    fn with_spilled(self, start: u32, len: u8) -> Self {
        match start.to_le_bytes() {
            [a, b, c, 0] => Self {
                text: [a, b, c, len],
                word: (self.word & KEPT) | SPILLED,
            },
            _ => self.blanked(),
        }
    }
    /// The cell as a reader sees it, with its row's text and `styles`.
    #[inline]
    pub(crate) fn read<'a>(&'a self, text: &'a Text, styles: &Styles) -> CellRef<'a> {
        self.read_as(text, styles.get(self.style()))
    }
    /// `read`, the cell's style's attributes being `attributes`.
    #[inline]
    pub(crate) fn read_as<'a>(
        &'a self,
        text: &'a Text,
        attributes: crate::Attributes,
    ) -> CellRef<'a> {
        CellRef::of(text.of(self), attributes, self.halves())
    }
}

/// A row's spilled text, long and short clusters each in their own part.
/// Overwritten clusters leave their text behind until the row runs out of
/// room and is compacted.
#[derive(Clone, Debug, Default)]
pub(crate) struct Text {
    long: Vec<u8>,
    short: Vec<u8>,
}

impl Text {
    /// The text of `cell`, a cell of this row.
    #[inline]
    pub(crate) fn of<'a>(&'a self, cell: &'a Compact) -> &'a str {
        match cell.locator() {
            None => cell.inline(),
            Some(_) => str::from_utf8(self.bytes_of(cell)).unwrap_or(""),
        }
    }
    /// `of`'s bytes, unread as UTF-8: what an append copies.
    #[inline]
    pub(crate) fn bytes_of<'a>(&'a self, cell: &'a Compact) -> &'a [u8] {
        let Some((start, len)) = cell.locator() else {
            return cell.inline_bytes();
        };
        let held = self.part(len > SHORT);
        start
            .checked_add(len)
            .and_then(|end| held.get(start..end))
            .unwrap_or_default()
    }
    /// What an append to `cell` follows: its text, or a space if empty.
    #[inline]
    fn followed<'a>(&'a self, cell: &'a Compact) -> &'a [u8] {
        if cell.has_contents() {
            self.bytes_of(cell)
        } else {
            b" "
        }
    }
    /// The bytes of text kept, long clusters and short.
    pub(crate) fn bytes(&self) -> usize {
        self.long.len().saturating_add(self.short.len())
    }
    pub(crate) fn is_empty(&self) -> bool {
        self.long.len() == 0 && self.short.is_empty()
    }
    /// Forgets all the text, releasing its memory.
    pub(crate) fn release(&mut self) {
        for part in [&mut self.long, &mut self.short] {
            if part.capacity() != 0 {
                *part = Vec::new();
            }
        }
    }
    /// Forgets all the text, keeping its memory for the slot's next row.
    pub(crate) fn empty(&mut self) {
        self.long.clear();
        self.short.clear();
    }
    /// A copy, in no more memory than it needs: what history keeps.
    pub(crate) fn exact(&self) -> Text {
        crate::budget::spend(self.bytes());
        Text {
            long: self.long.as_slice().to_vec(),
            short: self.short.as_slice().to_vec(),
        }
    }
    /// The most bytes of long clusters `cells` cells keep: 32 a cell, plus
    /// one whole cluster so even a one-column row holds its longest, and
    /// no more than a locator's 24 bits reach.
    pub(crate) fn long_limit(cells: usize) -> usize {
        cells
            .saturating_mul(32)
            .saturating_add(CLUSTER_CAPACITY)
            .min(LOCATED)
    }
    /// The most bytes of short clusters `cells` cells keep: one each, and
    /// no more than a locator reaches. A cluster grown in place may pass
    /// it until the next store compacts.
    fn short_limit(cells: usize) -> usize {
        cells.saturating_mul(SHORT).min(LOCATED)
    }
    fn part(&self, long: bool) -> &Vec<u8> {
        if long { &self.long } else { &self.short }
    }
    fn part_mut(&mut self, long: bool) -> &mut Vec<u8> {
        if long {
            &mut self.long
        } else {
            &mut self.short
        }
    }
}

/// The text's part of the invariant checker (THREATS.md section 6, 13).
#[cfg(feature = "checks")]
impl Text {
    /// Whether the text is within its limits for `width` cells, and each
    /// of `cells` locates whole text of at most [`CLUSTER_CAPACITY`] bytes.
    pub(crate) fn check(&self, cells: &[Compact], width: usize) -> Result<(), String> {
        let (long, short) = (Self::long_limit(width), Self::short_limit(width));
        crate::check::ensure(
            self.long.len() <= long && self.short.len() <= short.saturating_add(CLUSTER_CAPACITY),
            || {
                format!(
                    "{} long and {} short bytes, limits {long} and {short}",
                    self.long.len(),
                    self.short.len()
                )
            },
        )?;
        for (i, cell) in cells.iter().enumerate() {
            let Some((start, len)) = cell.locator() else {
                continue;
            };
            let text = self.of(cell);
            crate::check::ensure(!text.is_empty() && text.len() <= CLUSTER_CAPACITY, || {
                format!("column {i}: text at {start} of {len} bytes is not there")
            })?;
        }
        Ok(())
    }
}

impl AsRef<Compact> for Compact {
    fn as_ref(&self) -> &Compact {
        self
    }
}
impl AsMut<Compact> for Compact {
    fn as_mut(&mut self) -> &mut Compact {
        self
    }
}

/// A row's cells with their text, to write into.
pub(crate) struct Line<'a, C = Compact> {
    pub(crate) cells: &'a mut [C],
    pub(crate) text: &'a mut Text,
}

impl<C: AsRef<Compact> + AsMut<Compact>> Line<'_, C> {
    /// Adds `c` to the cluster in cell `i` (after a space if empty).
    /// Dropped, returning false, at [`CLUSTER_CAPACITY`] or out of room.
    pub(crate) fn append(&mut self, i: usize, c: char) -> bool {
        let Some(&cell) = self.cells.get(i).map(AsRef::as_ref) else {
            return false;
        };
        let Some(end) = (self.text.followed(&cell))
            .len()
            .checked_add(c.len_utf8())
            .filter(|end| *end <= CLUSTER_CAPACITY)
        else {
            return false;
        };
        // The cluster is read and joined again whole.
        crate::budget::spend(end);
        if let Some(joined) = cell.inline_with(c)
            && let Some(slot) = self.cells.get_mut(i).map(AsMut::as_mut)
        {
            *slot = joined;
            return true;
        }
        let mut encoded = [0; 4];
        let encoded = c.encode_utf8(&mut encoded).as_bytes();
        let long = end > SHORT;
        let limit = Text::long_limit(self.cells.len());
        // In place, when the cluster is last in its part. A short cluster
        // is never out of room.
        let (store, room) = if long {
            (&mut self.text.long, limit)
        } else {
            (&mut self.text.short, usize::MAX)
        };
        if let Some(range) = cell.spilled(long)
            && range.end == store.len()
            && store.len().saturating_add(encoded.len()) <= room
            && let (Ok(start), Ok(len)) = (u32::try_from(range.start), u8::try_from(end))
        {
            push_utf8(store, encoded);
            if let Some(slot) = self.cells.get_mut(i).map(AsMut::as_mut) {
                *slot = cell.with_spilled(start, len);
            }
            return true;
        }
        // Else joined in a buffer: whole UTF-8 then a character is whole.
        let current = self.text.followed(&cell);
        if !long {
            let mut buffer = [0u8; SHORT];
            let Some(joined) = join(&mut buffer, current, encoded) else {
                return false;
            };
            return self.store_short(i, cell, joined, false);
        }
        let mut buffer = [0u8; CLUSTER_CAPACITY];
        let Some(joined) = join(&mut buffer, current, encoded) else {
            return false;
        };
        // Unlike `set`, the cell keeps what it has if the row has no room,
        // even compacted: an append never loses text.
        self.store_long(i, cell, joined, false, false)
    }

    /// Sets cell `i` to `cell`'s halves and style holding `text`. A cluster
    /// longer than [`CLUSTER_CAPACITY`] is cut there; a long one the row
    /// has no room for, even compacted, is cut to a short one. Whether the
    /// text was kept whole.
    pub(crate) fn set(&mut self, i: usize, cell: Compact, text: &str) -> bool {
        let kept = floor(text, CLUSTER_CAPACITY);
        self.store(i, cell, kept, false) && kept.len() == text.len()
    }

    /// `set` on a row `clear_for` compacted, as a bulk copy fills it: a
    /// cluster with no room is cut at once, as compacting again frees
    /// nothing, so a copy of many reads the row once, not once a cluster.
    pub(crate) fn set_compacted(&mut self, i: usize, cell: Compact, text: &str) -> bool {
        let kept = floor(text, CLUSTER_CAPACITY);
        self.store(i, cell, kept, true) && kept.len() == text.len()
    }

    fn store(&mut self, i: usize, cell: Compact, text: &str, compacted: bool) -> bool {
        if i >= self.cells.len() {
            return false;
        }
        if text.len() <= SHORT {
            return self.store_short(i, cell, text.as_bytes(), compacted);
        }
        if self.store_long(i, cell, text.as_bytes(), true, compacted) {
            return true;
        }
        let _ = self.store_short(i, cell, floor(text, SHORT).as_bytes(), compacted);
        false
    }

    /// Stores long cluster `text` in cell `i`, compacting first if out of
    /// room, unless `compacted` says the text is already. If `replace`, the
    /// cell's old text goes first. Whether the row had room.
    fn store_long(
        &mut self,
        i: usize,
        cell: Compact,
        text: &[u8],
        replace: bool,
        compacted: bool,
    ) -> bool {
        let limit = Text::long_limit(self.cells.len());
        if self.text.long.len().saturating_add(text.len()) > limit {
            if replace && let Some(slot) = self.cells.get_mut(i).map(AsMut::as_mut) {
                *slot = slot.blanked();
            }
            // Compacted, the text is what the cells hold, end to end: if
            // that leaves no room either, compacting would copy it all for
            // nothing (THREATS.md B31).
            if !compacted && self.live_long().saturating_add(text.len()) <= limit {
                self.compact(true, 0..0);
            }
        }
        let long = &mut self.text.long;
        let (Ok(start), Ok(len)) = (u32::try_from(long.len()), u8::try_from(text.len())) else {
            return false;
        };
        let room = long.len().saturating_add(text.len()) <= limit;
        match self.cells.get_mut(i).map(AsMut::as_mut) {
            Some(slot) if room => {
                crate::budget::spend(text.len());
                long.extend_from_slice(text);
                *slot = cell.with_spilled(start, len);
                true
            }
            _ => false,
        }
    }

    /// Stores `text`, whole UTF-8 of at most [`SHORT`] bytes, in cell `i`:
    /// inline, or in the short text, compacted first if out of room.
    /// Whether it had room: a row wider than a locator reaches may not,
    /// and the cell keeps what of `text` fits inline. `compacted`: as
    /// `store_long`'s.
    fn store_short(&mut self, i: usize, cell: Compact, text: &[u8], compacted: bool) -> bool {
        if let Some(inline) = cell.holding(text) {
            if let Some(slot) = self.cells.get_mut(i).map(AsMut::as_mut) {
                *slot = inline;
            }
            return true;
        }
        let limit = Text::short_limit(self.cells.len());
        if self.text.short.len().saturating_add(text.len()) > limit {
            // The text the cell had goes: it is being replaced.
            if let Some(slot) = self.cells.get_mut(i).map(AsMut::as_mut) {
                *slot = slot.blanked();
            }
            if !compacted {
                self.compact(false, 0..0);
            }
        }
        let short = &mut self.text.short;
        let Some(slot) = self.cells.get_mut(i).map(AsMut::as_mut) else {
            return false;
        };
        let (Ok(start), Ok(len)) = (u32::try_from(short.len()), u8::try_from(text.len())) else {
            return false;
        };
        if short.len().saturating_add(text.len()) > limit {
            let head = floor(str::from_utf8(text).unwrap_or_default(), 4);
            *slot = cell
                .holding(head.as_bytes())
                .unwrap_or_else(|| cell.blanked());
            return false;
        }
        if short.capacity() == 0 {
            // A row with one short cluster usually has more.
            short.reserve(FIRST_SHORT.min(limit));
        }
        crate::budget::spend(text.len());
        short.extend_from_slice(text);
        *slot = cell.with_spilled(start, len);
        true
    }

    /// The bytes of long text the cells hold: what `compact(true)` keeps.
    fn live_long(&self) -> usize {
        crate::budget::spend(self.cells.len());
        (self.cells.iter())
            .filter_map(|cell| cell.as_ref().spilled(true))
            .map(|range| range.len())
            .fold(0, usize::saturating_add)
    }

    /// Whether cell `i` spills exactly `len` bytes, so `replace` can reuse
    /// them.
    pub(crate) fn replaces(&self, i: usize, len: usize) -> bool {
        (self.cells.get(i))
            .and_then(|c| c.as_ref().locator())
            .is_some_and(|(_, held)| held == len)
    }
    /// Writes `text` over cell `i`'s spilled text of the same length
    /// (`replaces`): no room taken, so no compaction (B35).
    pub(crate) fn replace(&mut self, i: usize, cell: Compact, text: &str) {
        let Some((start, len)) = (self.cells.get(i)).and_then(|c| c.as_ref().locator()) else {
            return;
        };
        let store = self.text.part_mut(len > SHORT);
        let held = start
            .checked_add(len)
            .and_then(|end| store.get_mut(start..end));
        if let (Some(held), Ok(at), Ok(n)) = (held, u32::try_from(start), u8::try_from(len))
            && held.len() == text.len()
            && let Some(slot) = self.cells.get_mut(i).map(AsMut::as_mut)
        {
            crate::budget::spend(text.len());
            held.copy_from_slice(text.as_bytes());
            *slot = cell.with_spilled(at, n);
        }
    }
    /// Blanks cells `span` and keeps only the others' text, so writing the
    /// span cell by cell never compacts the row again (DECCRA, B35).
    pub(crate) fn clear_for(&mut self, span: std::ops::Range<usize>) {
        let cells = self.cells.get_mut(span.clone()).unwrap_or_default();
        for cell in cells.iter_mut().map(AsMut::as_mut) {
            *cell = cell.blanked();
        }
        for long in [true, false] {
            if !self.text.part(long).is_empty() {
                self.compact(long, span.clone());
            }
        }
    }

    /// Keeps only the text of long clusters (`long`), or of short ones, of
    /// the cells outside `skip`, relocating their cells.
    fn compact(&mut self, long: bool, skip: std::ops::Range<usize>) {
        let store = self.text.part_mut(long);
        let old = std::mem::take(store);
        let (head, rest) = (self.cells)
            .split_at_mut_checked(skip.start.min(self.cells.len()))
            .unwrap_or_default();
        let tail = rest.get_mut(skip.len()..).unwrap_or_default();
        crate::budget::spend(head.len().saturating_add(tail.len()));
        for cell in head.iter_mut().chain(tail).map(AsMut::as_mut) {
            let Some(range) = cell.spilled(long) else {
                continue;
            };
            let [.., len] = cell.text;
            let (Some(text), Ok(start)) = (old.get(range), u32::try_from(store.len())) else {
                *cell = cell.blanked();
                continue;
            };
            store.extend_from_slice(text);
            *cell = cell.with_spilled(start, len);
        }
        // What it copied: the text the cells hold.
        crate::budget::spend(store.len());
    }

    /// `cells`' text from `old`, stored again within budget; `cells` are
    /// relocated into it.
    pub(crate) fn rebuilt(cells: &mut [C], old: &Text) -> Text {
        let mut text = Text::default();
        if old.is_empty() {
            return text;
        }
        let spilled: Vec<(usize, Compact)> = (cells.iter().map(|c| *c.as_ref()).enumerate())
            .filter(|(_, cell)| cell.is_spilled())
            .collect();
        let mut line = Line {
            cells,
            text: &mut text,
        };
        line.store_again(spilled.into_iter(), old);
        text
    }

    /// Stores the `spilled` cells again, left to right, their text in
    /// `old`. All are blanked first: until stored again, none may locate
    /// text in this store, which a compaction would misread.
    pub(crate) fn store_again(
        &mut self,
        spilled: impl Iterator<Item = (usize, Compact)> + Clone,
        old: &Text,
    ) {
        for (i, cell) in spilled.clone() {
            if let Some(stored) = self.cells.get_mut(i) {
                *stored.as_mut() = cell.blanked();
            }
        }
        for (i, cell) in spilled {
            self.set(i, cell, old.of(&cell));
        }
    }

    pub(crate) fn text(&self, i: usize) -> &str {
        self.cells
            .get(i)
            .map_or("", |cell| self.text.of(cell.as_ref()))
    }
}

/// Pushes `bytes`, a character's UTF-8, as a copy of known length.
#[inline]
fn push_utf8(store: &mut Vec<u8>, bytes: &[u8]) {
    match *bytes {
        [a] => store.push(a),
        [a, b] => store.extend_from_slice(&[a, b]),
        [a, b, c] => store.extend_from_slice(&[a, b, c]),
        [a, b, c, d] => store.extend_from_slice(&[a, b, c, d]),
        _ => store.extend_from_slice(bytes),
    }
}

/// `head` then `tail` in `buffer`, if they fit: the bytes it holds.
fn join<'a>(buffer: &'a mut [u8], head: &[u8], tail: &[u8]) -> Option<&'a [u8]> {
    let end = head.len().checked_add(tail.len())?;
    let (first, second) = buffer.get_mut(..end)?.split_at_mut_checked(head.len())?;
    crate::copy_from(first, head)?;
    crate::copy_from(second, tail)?;
    buffer.get(..end)
}

#[cfg(test)]
mod tests;
