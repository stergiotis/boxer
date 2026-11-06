use super::*;

impl Grid {
    /// The cells retained, screen and history.
    pub(crate) fn storage_cells(&self) -> usize {
        self.cells
            .capacity()
            .saturating_add(self.history.kept_cells())
    }
}

/// Up, the departing rows going into history.
const UP: Scroll = Scroll::Up { history: true };

/// Heap bytes; a Vec's allocation cannot exceed `isize::MAX`, so no product
/// saturates.
fn heap(grid: &Grid) -> [usize; 4] {
    [
        grid.cells.capacity().saturating_mul(size_of::<Compact>()),
        grid.meta.capacity().saturating_mul(size_of::<Meta>()),
        grid.order.capacity().saturating_mul(size_of::<usize>()),
        grid.history.heap(),
    ]
}
/// A full history's storage, metadata included, stops growing: the
/// screen's as much after 20,000 scrolls as after 10,000, and history's
/// within its budget. A resize builds the new screen and history before
/// it lets the old go (`THREATS.md` B37): the new history within its
/// budget and a tenth more and 64 KiB (3.2's resize term), the new screens'
/// cells exactly their size, so the peak is the old and those together.
#[test]
fn storage_with_history_full_stops_growing_and_a_resize_holds_its_bound() -> Result<(), Error> {
    const BUDGET: usize = 10_000 * 24;
    let mut next = 0;
    let mut primary = Grid::new(Size::of(24, 80), BUDGET, &mut next, 0)?;
    let alternate = Grid::new(Size::of(24, 80), 0, &mut next, 0)?;
    for version in 1..=10_000 {
        primary.scroll(Size::of(24, 80).lines(), 1, UP, 0, &mut next, version);
    }
    let plateau = heap(&primary);
    for version in 10_001..=20_000 {
        primary.scroll(Size::of(24, 80).lines(), 1, UP, 0, &mut next, version);
    }
    // The screen's storage as it was, and history's within its budget.
    let now = heap(&primary);
    assert_eq!(now.get(..3), plateau.get(..3));
    assert!(now.iter().zip(plateau).skip(3).all(|(h, _)| *h <= BUDGET));
    // 240,000 bytes hold 19 of the ring's chunks of 512 empty rows (12,288
    // bytes each, THREATS.md B17): the newest of them filling, and the
    // oldest let go only once all its rows have gone.
    assert!((18 * 512 + 1..=19 * 512).contains(&primary.history_len()));
    let (mut resized_primary, kept) = primary.resized(Size::of(60, 120), &mut next, 20_001)?;
    let (resized_alternate, _) = alternate.resized(Size::of(60, 120), &mut next, 20_001)?;
    resized_primary.adopt_history(&mut primary, kept);
    let [cells, _, _, history] = heap(&resized_primary);
    assert!(
        history <= BUDGET + BUDGET / 10 + (64 << 10),
        "{history} bytes of new history"
    );
    assert_eq!(cells, 60 * 120 * size_of::<Compact>());
    assert_eq!(heap(&resized_alternate).first(), Some(&cells));
    Ok(())
}

// Narrowing a pane whose rows all stay live must not keep the old width in
// its storage: that multiplied every later copy of the grid by the widest
// width it ever had, and doubled the smoke's 200-pane `scale` run.
#[test]
fn narrowing_live_rows_uses_the_new_width() -> Result<(), Error> {
    let mut next = 0;
    let wide = Grid::new(Size::of(24, 400), 100, &mut next, 0)?;
    let (narrow, _) = wide.resized(Size::of(23, 10), &mut next, 1)?;
    assert_eq!(narrow.history_len(), 0);
    assert_eq!(narrow.cells.capacity(), 23 * 10);
    Ok(())
}

/// `move_row` is `remove(from)` then `insert(to)`, for every pair of rows,
/// in an order that is one slice and in one that has wrapped round into two;
/// and it moves nothing for an index out of range.
#[test]
fn moving_a_row_is_a_removal_then_an_insertion() -> Result<(), Error> {
    let mut next = 0;
    let contiguous = Grid::new(Size::of(5, 1), 0, &mut next, 0)?;
    let mut wrapped = Grid::new(Size::of(5, 1), 4, &mut next, 0)?;
    for version in 1..=11 {
        wrapped.scroll(Size::of(5, 1).lines(), 1, UP, 0, &mut next, version);
    }
    let order = &wrapped.order;
    assert!((1..order.len()).contains(&order.top), "the order wraps");
    moves_are_removals_then_insertions(&contiguous);
    moves_are_removals_then_insertions(&wrapped);
    Ok(())
}

fn moves_are_removals_then_insertions(grid: &Grid) {
    let rows: Vec<usize> = grid.order.iter().copied().collect();
    let len = rows.len();
    for from in 0..len {
        for to in 0..len {
            let mut moved = grid.clone();
            assert_eq!(moved.move_row(from, to), rows.get(from).copied());
            let rest = rows.iter().enumerate().filter(|(i, _)| *i != from);
            let mut expected: Vec<usize> = rest.clone().take(to).map(|(_, r)| *r).collect();
            expected.extend(rows.get(from));
            expected.extend(rest.skip(to).map(|(_, r)| *r));
            assert_eq!(
                moved.order.iter().copied().collect::<Vec<_>>(),
                expected,
                "{from} to {to}"
            );
        }
    }
    let mut untouched = grid.clone();
    assert_eq!(untouched.move_row(1, len), None);
    assert_eq!(untouched.move_row(len, 1), None);
    assert_eq!(untouched.order, grid.order);
}

/// What a reader sees of a row: its identity, version, wrap and prompt
/// flags, and each cell as stored, with its attributes, its text and its
/// link.
pub(crate) type Seen = (
    RowId,
    u64,
    bool,
    bool,
    Vec<(Compact, Attributes, String, Option<Hyperlink>)>,
);

impl Grid {
    /// Every link a row of the grid has, its history's and its screen's.
    pub(crate) fn held_links(&self) -> impl Iterator<Item = &Hyperlink> {
        let history = (0..self.history_len()).filter_map(|index| self.history.get(index));
        history
            .filter_map(|row| row.links)
            .chain(self.linked.iter().map(RowLinks::runs))
            .flat_map(Runs::links)
    }
    /// What a reader sees of every retained row, in order.
    pub(crate) fn seen(&self) -> Vec<Seen> {
        (0..self.retained_len())
            .filter_map(|i| self.row_at(i))
            .map(|row| {
                let cells = row
                    .cells
                    .iter()
                    .enumerate()
                    .map(|(col, c)| {
                        let link = row.links.and_then(|l| l.get(col)).cloned();
                        let read = c.read(row.text, row.grid.styles());
                        (*c, read.attributes(), read.contents().to_owned(), link)
                    })
                    .collect();
                (
                    row.id,
                    row.version,
                    row.wrapped(),
                    row.starts_prompt(),
                    cells,
                )
            })
            .collect()
    }
}

/// A live row's cells as a reader reads them: text, halves, attributes
/// and link.
fn read_row(grid: &Grid, y: u16) -> Vec<(String, bool, bool, Attributes, Option<String>)> {
    let Some(row) = grid.live_row(y) else {
        return Vec::new();
    };
    (0..row.width)
        .filter_map(|x| {
            let cell = row.cell(x)?;
            let link = row.link(x).map(|l| l.uri().to_owned());
            Some((
                cell.contents().to_owned(),
                cell.is_wide(),
                cell.is_wide_continuation(),
                cell.attributes(),
                link,
            ))
        })
        .collect()
}

/// Rows keep their text, halves, attributes and links while moving only
/// inside the selected region, in both directions and across the ring end.
#[test]
fn scrolling_moves_only_the_selected_rows() -> Result<(), Error> {
    let colour = Attributes::new(crate::Color::Idx(1), crate::Color::Idx(4));
    let blank = colour.inline_style().unwrap_or(0);
    for scrolls in [0, 5] {
        let mut p =
            crate::Parser::with_options(Size::of(4, 12), 0, crate::Feature::Hyperlinks.into())?;
        for y in 0..4 {
            let marks: String =
                std::iter::repeat_n("\u{301}", if y == 0 { 9 } else { 3 }).collect();
            p.process(
                format!(
                    "\x1b[{};1H\x1b[{}m{y}界e{marks}\x1b]8;;https://{y}\x1b\\link\x1b]8;;\x1b\\",
                    y + 1,
                    31 + y
                )
                .as_bytes(),
            )?;
        }
        let mut grid = p.screen().primary_grid().clone();
        grid.order.top = if scrolls == 0 { 0 } else { 2 };
        // Raw rows can hold an unpaired head or tail; copying a whole row
        // must preserve them too, alongside its short and long text.
        let slot = grid.slot(2).ok_or(Error::Capacity)?;
        for (x, cell) in [
            (8, Compact::continuation()),
            (10, Compact::glyph('界', 2, 0)),
        ] {
            *grid.slice_mut(slot).get_mut(x).ok_or(Error::Capacity)? = cell;
        }
        grid.meta.get_mut(slot).ok_or(Error::Capacity)?.used = 12;
        let before: Vec<_> = (0..4).map(|y| read_row(&grid, y)).collect();
        for top in 0..4 {
            for bottom in top..4 {
                for count in 0..=5 {
                    for up in [true, false] {
                        let mut moved = grid.clone();
                        moved.scroll_columns(Span::of(top, bottom), count, up, blank, 1_000);
                        for y in 0..4 {
                            let source = if y < top || y > bottom {
                                Some(y)
                            } else if up {
                                y.checked_add(count).filter(|&n| n <= bottom)
                            } else {
                                y.checked_sub(count).filter(|&n| n >= top)
                            };
                            if let Some(source) = source {
                                assert_eq!(
                                    Some(&read_row(&moved, y)),
                                    before.get(usize::from(source))
                                );
                            } else {
                                let row = moved.live_row(y).ok_or(Error::Capacity)?;
                                assert!((0..row.width).all(|x| row.cell(x).is_some_and(|c| {
                                    c.contents().is_empty() && c.attributes() == colour
                                })));
                            }
                        }
                        assert!(moved.blank_past_used());
                    }
                }
            }
        }
    }
    Ok(())
}

/// Turning rows that cross the ring's end turns those rows and no others,
/// as turning them where the ring starts at its top does, each way and
/// by every count.
#[test]
fn a_rotation_across_the_rings_end_turns_only_its_rows() {
    for top in 0..6 {
        for (low, high) in [(0, 5), (1, 4), (2, 5), (3, 4), (0, 0)] {
            for by in 0..=high - low + 1 {
                for left in [true, false] {
                    let mut order = Order {
                        slots: (0..6).collect(),
                        top,
                    };
                    let mut want: Vec<usize> = order.iter().copied().collect();
                    if let Some(run) = want.get_mut(low..=high) {
                        if left {
                            run.rotate_left(by);
                        } else {
                            run.rotate_right(by);
                        }
                    }
                    order.rotate((low, high), by, left);
                    let got: Vec<usize> = order.iter().copied().collect();
                    assert_eq!(
                        got, want,
                        "top {top}, rows {low}..={high}, by {by}, left {left}"
                    );
                }
            }
        }
    }
}

/// Copying the span of a row that holds the most text a row holds, 1,033
/// clusters of 127 bytes and 3,063 of 17, each cell its own link of two
/// alternating ids, over a row of other text and links, spends no more
/// than the row's share of the screen term (`THREATS.md` 4.2),
/// 12C + 2 × (49C + 128): its cells, its links and its text twice, and the
/// compaction when the clusters' lengths don't line up.
#[test]
fn copying_a_full_linked_row_spends_at_most_its_share() -> Result<(), Error> {
    const C: u16 = 4096;
    let c = u64::from(C);
    let share = 12 * c + 2 * (49 * c + 128);
    // The destination's long clusters where the source's are, or at its end.
    for long_from in [0, C - 1_033] {
        let mut p =
            crate::Parser::with_options(Size::of(2, C), 0, crate::Feature::Hyperlinks.into())?;
        for (row, base, ids, long) in [
            (1, 'e', ["a", "b"], long_from..long_from + 1_033),
            (2, 'f', ["c", "d"], 0..1_033),
        ] {
            let mut line = format!("\x1b[{row};1H");
            for col in 0..C {
                let id = ids.get(usize::from(col % 2)).ok_or(Error::Capacity)?;
                let marks = if long.contains(&col) { 63 } else { 8 };
                line.push_str(&format!("\x1b]8;id={id};u{id}\x1b\\{base}"));
                line.extend(std::iter::repeat_n('\u{301}', marks));
            }
            p.process(line.as_bytes())?;
        }
        let mut grid = p.screen().primary_grid().clone();
        let before = crate::budget::spent();
        grid.copy_span(1, 0, 1..usize::from(C - 1), 1_000);
        let spent = crate::budget::spent().wrapping_sub(before);
        assert!(spent <= share, "long from {long_from}: {spent} > {share}");
        let row = grid.live_row(0).ok_or(Error::Capacity)?;
        let text = |x| row.cell(x).map(|c| c.contents().len());
        assert_eq!([text(1), text(4094)], [Some(127), Some(17)]);
    }
    Ok(())
}
