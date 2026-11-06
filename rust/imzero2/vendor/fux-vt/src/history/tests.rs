use super::*;

/// A row of `len` cells, each a letter from `first` on, in style `style`.
fn row(len: usize, first: u8, style: u32) -> Vec<Compact> {
    (0..len)
        .map(|i| {
            let letter = u8::try_from(i % 26).unwrap_or(0);
            Compact::ascii(first.wrapping_add(letter), style)
        })
        .collect()
}

fn arriving(id: u64, cells: &[Compact], width: u16) -> Arriving<'_> {
    Arriving {
        id: RowId(id),
        version: id.wrapping_mul(3),
        flags: u8::from(id % 2 == 1)
            | u8::from(id.is_multiple_of(3)) << 1
            | u8::from(id % 5 == 4) << RENDITION_SHIFT,
        cells,
        width,
    }
}

/// What a reader finds of each row: its identity, version, flags (whether
/// it is soft-wrapped; whether a prompt starts on it, and whether it is
/// double width), width and cells.
type Found = (u64, u64, bool, (bool, bool), u16, Vec<Compact>);
fn found(history: &History) -> Vec<Found> {
    (0..history.len())
        .filter_map(|index| history.get(index))
        .map(|row| {
            (
                row.kept.id.0,
                row.kept.version,
                row.kept.wrapped(),
                (row.flags & PROMPT != 0, row.flags >> RENDITION_SHIFT != 0),
                row.kept.width(),
                row.cells.to_vec(),
            )
        })
        .collect()
}

/// A row's cells are those before its blank tail; a coloured blank is no
/// part of the tail.
#[test]
fn a_row_keeps_the_cells_before_its_blank_tail() {
    let mut cells = row(5, b'a', 0);
    cells.extend([BLANK; 3]);
    assert_eq!(trimmed(&cells).len(), 5);
    cells.push(Compact::blank(7));
    cells.push(BLANK);
    assert_eq!(trimmed(&cells).len(), 9);
    assert!(trimmed(&[BLANK; 4]).is_empty());
    assert!(trimmed(&[]).is_empty());
}

/// Rows of every length, none, one, many, and longer than a block, read
/// back as they came, in order, through block after block, as the oldest
/// go past the budget; and the bytes counted are those the rows keep: 24
/// a row, 8 a cell.
#[test]
fn rows_read_back_as_they_came_while_the_oldest_go() -> Result<(), Error> {
    let budget = 64 << 10;
    let mut history = History::new(budget);
    let mut expected = std::collections::VecDeque::new();
    for id in 0..3000u64 {
        // Rows end anywhere in a block, now and then one longer than a
        // block.
        let len = match id % 11 {
            0 => 0,
            1 => 1,
            2 if id % 33 == 2 => BLOCK.saturating_add(5),
            n => usize::try_from(n).unwrap_or(0).saturating_mul(31),
        };
        let first = b'a'.wrapping_add(u8::try_from(id % 26).unwrap_or(0));
        let cells = row(len, first, u32::try_from(id % 5).unwrap_or(0));
        let width = u16::try_from(len.max(80)).unwrap_or(u16::MAX);
        history.push(arriving(id, &cells, width))?;
        expected.push_back((
            id,
            id.wrapping_mul(3),
            id % 2 == 1,
            (id.is_multiple_of(3), id % 5 == 4),
            width,
            cells,
        ));
        history.trim();
        while expected.len() > history.len() {
            expected.pop_front();
        }
        assert_eq!(
            found(&history),
            Vec::from(expected.clone()),
            "after row {id}"
        );
        let kept: usize = expected.iter().map(|row| row.5.len()).sum();
        assert_eq!(history.kept_cells(), kept);
        // What it counts is what it holds, and within the budget.
        assert_eq!(history.bytes(), history.heap());
        assert!(history.bytes() <= budget);
    }
    let last = history.len().saturating_sub(1);
    assert_eq!(history.position(RowId(2999)), Some(last));
    assert_eq!(history.position(RowId(0)), None);
    Ok(())
}

/// Blocks the rows have left are let go: the history's memory stops
/// growing at its budget, and fills most of it.
#[test]
fn memory_stops_growing_at_the_budget() -> Result<(), Error> {
    let budget = 256 << 10;
    let mut history = History::new(budget);
    for round in 0..4 {
        for id in 0..2000u64 {
            let cells = row(usize::try_from(id % 120).unwrap_or(0), b'a', 0);
            history.push(arriving(id, &cells, 120))?;
            history.trim();
            assert!(
                history.heap() <= budget,
                "round {round}: {}",
                history.heap()
            );
        }
        // Less than two blocks and the ring's doubling short of it.
        assert!(
            history.heap() > budget / 2,
            "round {round}: {}",
            history.heap()
        );
    }
    Ok(())
}

/// Block numbers are 16 bits and wrap (`BLOCKS`): a long-lived
/// pane makes 65,536 blocks in about 268 M cells. Numbered from just below
/// the wrap, rows of a block each read back as they came across it, while
/// the oldest go past the budget, and the bytes counted stay those held.
#[test]
fn block_numbers_wrap() -> Result<(), Error> {
    let budget = 256 << 10;
    let mut history = History::new(budget);
    history.first_block = u16::MAX - 3;
    history.newest_block = u16::MAX - 3;
    let mut expected = std::collections::VecDeque::new();
    for id in 0..200u64 {
        // Each row more than half a block: a block of its own.
        let len = (BLOCK / 2).saturating_add(usize::try_from(id % 7).unwrap_or(0));
        let first = b'a'.wrapping_add(u8::try_from(id % 26).unwrap_or(0));
        let cells = row(len, first, 0);
        let width = u16::try_from(len).unwrap_or(u16::MAX);
        history.push(arriving(id, &cells, width))?;
        expected.push_back((
            id,
            id.wrapping_mul(3),
            id % 2 == 1,
            (id.is_multiple_of(3), id % 5 == 4),
            width,
            cells,
        ));
        history.trim();
        while expected.len() > history.len() {
            expected.pop_front();
        }
        assert_eq!(
            found(&history),
            Vec::from(expected.clone()),
            "after row {id}"
        );
        assert_eq!(history.bytes(), history.heap());
        assert!(history.bytes() <= budget);
    }
    assert!(
        history.first_block < 1000,
        "never wrapped: {}",
        history.first_block
    );
    assert!(history.len() > 1);
    Ok(())
}

/// A row with one link keeps its run in place: its links cost history
/// nothing beyond its share of the map (`THREATS.md` B15), however the row
/// came by them; one with two links costs the room of its runs.
#[test]
fn a_row_with_one_link_holds_no_memory_for_it() -> Result<(), Error> {
    use crate::link::{Hyperlink, KeptLinks, RowLinks, Run};
    let (a, b) = (
        Hyperlink::new(1, "http://a", None),
        Hyperlink::new(2, "http://b", Some("b")),
    );
    let cells = row(3, b'a', 0);
    let mut history = History::new(1 << 20);
    // Printed in one run; and in two runs of one link, which join.
    for (id, ends) in [(0, &[3][..]), (1, &[1, 3][..])] {
        let mut links = RowLinks::default();
        let mut start = 0;
        for &end in ends {
            links.set(start..end, Some(&a));
            start = end;
        }
        history.push(arriving(id, &cells, 4))?;
        history.attach(None, KeptLinks::take(&mut links));
        assert_eq!(history.runs, 0, "row {id}");
    }
    let mut links = RowLinks::default();
    links.set(0..1, Some(&a));
    links.set(2..3, Some(&b));
    history.push(arriving(2, &cells, 4))?;
    history.attach(None, KeptLinks::take(&mut links));
    assert_eq!(history.runs, 2 * size_of::<Run>());
    Ok(())
}

/// The spans of rows let go while history is made anew (a new budget, as a
/// resize makes it) go with them: what the spans cost stays what the rows
/// kept need, however often history is made anew (`THREATS.md` B40).
#[test]
fn spans_of_rows_let_go_while_history_is_made_anew_go_too() -> Result<(), Error> {
    let budget = 64 << 10;
    let cells = row(8, b'a', 0);
    let mut history = History::new(budget);
    // Identities two apart: each row begins a span of its own.
    let mut id = 0u64;
    let mut push = |history: &mut History| {
        id = id.wrapping_add(2);
        let pushed = history.push(arriving(id, &cells, 8));
        history.trim();
        pushed
    };
    for round in 0..8 {
        history.set_budget(budget);
        for _ in 0..2000 {
            push(&mut history)?;
        }
        // Made anew in half the room: half the rows go as they come in.
        history.set_budget(budget / 2);
        // A span begun lets go of those whose rows all went.
        push(&mut history)?;
        assert!(
            history.spans.len() <= history.len().saturating_add(1),
            "round {round}: {} spans for {} rows",
            history.spans.len(),
            history.len()
        );
    }
    Ok(())
}

/// The room history's deques keep: up to four times their entries and 64,
/// and no more, so one that grew by doubling and lost entries gives room
/// back only past that (`shrink`, `THREATS.md` B17).
#[test]
fn a_deque_keeps_room_for_four_times_its_entries_and_64() {
    let mut deque = VecDeque::<u64>::with_capacity(128);
    let room = deque.capacity();
    assert!(room >= 64 && room.is_multiple_of(4), "{room}");
    deque.extend(0..u64::try_from(room.saturating_sub(64) / 4).unwrap_or(0));
    shrink(&mut deque);
    assert_eq!(deque.capacity(), room, "at the most it keeps");
    deque.pop_back();
    shrink(&mut deque);
    assert!(deque.capacity() < room, "past it");
}
