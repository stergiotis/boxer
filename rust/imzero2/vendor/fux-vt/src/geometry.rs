//! A screen's size, and the runs of its rows or columns its margins bound.
use std::fmt;
use std::num::NonZeroU16;

/// A screen's size: how many rows and columns it has, never none of either,
/// so a screen always has a last row and a last column.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub struct Size {
    rows: NonZeroU16,
    cols: NonZeroU16,
}

impl Size {
    /// `rows` by `cols`; an error that says which is zero if either is.
    /// A size past the bound on cells is refused later, where a screen of
    /// it would be made ([`crate::fits`]).
    pub fn new(rows: u16, cols: u16) -> Result<Self, SizeError> {
        match (NonZeroU16::new(rows), NonZeroU16::new(cols)) {
            (Some(rows), Some(cols)) => Ok(Self { rows, cols }),
            _ => Err(SizeError { rows, cols }),
        }
    }
    /// How many rows.
    pub fn rows(self) -> u16 {
        self.rows.get()
    }
    /// How many columns.
    pub fn cols(self) -> u16 {
        self.cols.get()
    }
    /// Rows and columns, as the numbers they are: never zero.
    pub fn nonzero(self) -> (NonZeroU16, NonZeroU16) {
        (self.rows, self.cols)
    }
    pub(crate) fn cells(self) -> usize {
        usize::from(self.rows()).saturating_mul(usize::from(self.cols()))
    }
    /// Every row.
    pub(crate) fn lines(self) -> Span {
        Span::whole(self.rows)
    }
    /// Every column.
    pub(crate) fn columns(self) -> Span {
        Span::whole(self.cols)
    }
}

/// Why [`Size::new`] made no size: no rows, or no columns.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct SizeError {
    rows: u16,
    cols: u16,
}

impl fmt::Display for SizeError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let needs = match (self.rows, self.cols) {
            (0, 0) => "one row and one column",
            (0, _) => "one row",
            _ => "one column",
        };
        let (rows, cols) = (self.rows, self.cols);
        write!(
            f,
            "{rows} rows by {cols} columns: a screen needs at least {needs}"
        )
    }
}

impl std::error::Error for SizeError {}

impl From<(NonZeroU16, NonZeroU16)> for Size {
    fn from((rows, cols): (NonZeroU16, NonZeroU16)) -> Self {
        Self { rows, cols }
    }
}

impl From<Size> for (u16, u16) {
    fn from(size: Size) -> Self {
        (size.rows(), size.cols())
    }
}

/// Rows, or columns, `first` to `last` of a screen, both included: a pair
/// of margins, or the screen's edges. `first` is never past `last`, nor
/// `last` off the screen the span was made for.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) struct Span {
    first: u16,
    last: u16,
}

impl Span {
    fn whole(extent: NonZeroU16) -> Self {
        // Exact: an extent is at least one.
        let last = extent.get().saturating_sub(1);
        Self { first: 0, last }
    }
    /// Margins within this span, the screen's, as DECSTBM and DECSLRM set
    /// them: `last` past the span is its last, as xterm takes it, and then
    /// `None` unless `first` is before `last` (DEC STD 070, DECSTBM and
    /// DECSLRM, note 2).
    pub fn margins(self, first: u16, last: u16) -> Option<Self> {
        let last = last.min(self.last);
        (first < last).then_some(Self { first, last })
    }
    /// The part `first` to `last` of this span, `None` if `first` is past
    /// `last` or `last` past this span: a side of a rectangle (DEC STD 070,
    /// 5.12.1).
    pub fn part(self, first: u16, last: u16) -> Option<Self> {
        (first <= last && last <= self.last).then_some(Self { first, last })
    }
    pub fn each(self) -> std::ops::RangeInclusive<u16> {
        self.first..=self.last
    }
    /// The span from `first` on, if it is in this one.
    pub fn starting_at(self, first: u16) -> Option<Self> {
        self.contains(first).then_some(Self { first, ..self })
    }
    pub fn first(self) -> u16 {
        self.first
    }
    pub fn last(self) -> u16 {
        self.last
    }
    /// One past the last: exact, as the last is below a u16 extent.
    pub fn end(self) -> u16 {
        self.last.saturating_add(1)
    }
    /// How many rows or columns: at least one.
    pub fn len(self) -> u16 {
        self.end().saturating_sub(self.first)
    }
    pub fn contains(self, n: u16) -> bool {
        (self.first..=self.last).contains(&n)
    }
    /// Its `n`th row or column, from 0, as origin mode addresses them; its
    /// last past it.
    pub fn nth(self, n: u16) -> u16 {
        self.first.saturating_add(n).min(self.last)
    }
}

#[cfg(test)]
impl Size {
    /// `rows` by `cols`, for a test, which never asks for none.
    pub(crate) fn of(rows: u16, cols: u16) -> Self {
        let at_least_one = |n| NonZeroU16::new(n).unwrap_or(NonZeroU16::MIN);
        Self::from((at_least_one(rows), at_least_one(cols)))
    }
}

#[cfg(test)]
impl Span {
    /// `first` to `last`, for a test, which never asks for one off the screen.
    pub(crate) fn of(first: u16, last: u16) -> Self {
        let last = last.max(first);
        Self { first, last }
    }
}
