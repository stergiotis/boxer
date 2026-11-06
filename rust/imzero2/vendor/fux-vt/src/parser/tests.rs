use super::*;
use crate::test_rng::Rng;

/// B20: the OSC buffer has room for the limit the features set and the
/// byte that marks a string overflowed, and no more, however a string is
/// split: 65 bytes with prompt marks alone, 1 with no feature reading OSC.
#[test]
fn the_osc_buffer_has_room_for_its_limit_alone() -> Result<(), Error> {
    for (options, room) in [
        (Options::from(Feature::PromptMarks), 65),
        (Options::new(), 1),
    ] {
        let mut p = Parser::with_options(Size::of(2, 10), 0, options)?;
        p.process(b"\x1b]133;")?;
        for _ in 0..40 {
            p.process(b"aa")?;
        }
        assert!(p.osc.capacity() <= room, "{} for {room}", p.osc.capacity());
    }
    Ok(())
}

/// Non-ASCII runs keep the printing context on every glyph, including the
/// glyphs after the first that take Screen::run. Dots below are blank cells;
/// 界 and a Prepend joined to Ж each occupy two columns. DECSCA is checked
/// by selective erase.
#[test]
fn text_runs_keep_their_printing_context() -> Result<(), Error> {
    type Case<'a> = (&'a str, &'a [&'a str], (u16, u16), bool, bool, bool);
    let cases: &[Case<'_>] = &[
        ("", &["Ж Ш 界"], (0, 4), false, false, false),
        (
            "\x1b[?69h\x1b[3;8s\x1b[1;5H",
            &["· · · · Ж Ш 界"],
            (0, 7),
            true,
            false,
            false,
        ),
        (
            "\x1b[?69h\x1b[3;8s\x1b[?6h",
            &["· · Ж Ш 界"],
            (0, 6),
            false,
            false,
            false,
        ),
        ("\x1b#6", &["Ж Ш 界"], (0, 4), false, false, false),
        (
            "\x1b[2;3H界界界界\x1b[2;3H",
            &["", "· · Ж Ш 界 界 界"],
            (1, 6),
            false,
            false,
            false,
        ),
        (
            "\x1b[2;3H界a界界\x1b[2;3H",
            &["", "· · Ж Ш 界 · 界"],
            (1, 6),
            false,
            false,
            false,
        ),
        (
            "\x1b[2;3H界界界界\x1b[2;4H",
            &["", "· · · Ж Ш 界 · 界"],
            (1, 7),
            false,
            false,
            false,
        ),
        (
            "\x1b[2;3H界界界界\x1b[2;3Hx",
            &["", "· · x Ж Ш 界 · 界"],
            (1, 7),
            false,
            false,
            false,
        ),
        (
            "\x1b[1;11Hx",
            &["· · · · · · · · · · x Ж", "Ш 界"],
            (1, 3),
            false,
            false,
            false,
        ),
        (
            "\x1b[?7l\x1b[1;9H",
            &["· · · · · · · · Ж Ш 界"],
            (0, 11),
            false,
            false,
            false,
        ),
        ("\x1b[1\"q", &["Ж Ш 界"], (0, 4), false, false, true),
        ("\x1b[?1049h", &["Ж Ш 界"], (0, 4), false, false, false),
        (
            "abcdefghij\r\x1b[4h",
            &["Ж Ш 界 a b c d e f g h"],
            (0, 4),
            false,
            false,
            false,
        ),
        (
            "\x1b]8;id=1;http://x\x1b\\",
            &["Ж Ш 界"],
            (0, 4),
            false,
            true,
            false,
        ),
        ("\u{600}", &["\u{600}Ж Ш 界"], (0, 5), false, false, false),
        ("\x1b(0", &["Ж Ш 界"], (0, 4), false, false, false),
        (
            "\x1b[2;3r\x1b[3;1H",
            &["", "", "Ж Ш 界"],
            (2, 4),
            false,
            false,
            false,
        ),
        ("AAШA\r", &["Ж Ш 界"], (0, 4), false, false, false),
        (
            "\x1b]8;id=1;http://x\x1b\\AAШA\r",
            &["Ж Ш 界"],
            (0, 4),
            false,
            true,
            false,
        ),
    ];
    for &(setup, rows, cursor, pending, linked, protected) in cases {
        let mut parser =
            Parser::with_options(Size::of(4, 12), 1024, Feature::ALL.into_iter().collect())?;
        parser.process(setup.as_bytes())?;
        parser.process("ЖШ界".as_bytes())?;
        let check_cells = |p: &Parser| {
            for y in 0..4u16 {
                let mut expected = Vec::new();
                for text in rows.get(usize::from(y)).unwrap_or(&"").split_whitespace() {
                    let wide = matches!(text, "界" | "\u{600}Ж");
                    expected.push((if text == "·" { "" } else { text }, wide, false));
                    if wide {
                        expected.push(("", false, true));
                    }
                }
                expected.resize(12, ("", false, false));
                for (x, &(text, wide, tail)) in (0..12).zip(&expected) {
                    let cell = p.screen().cell(y, x);
                    assert_eq!(
                        cell.map(|c| (c.contents(), c.is_wide(), c.is_wide_continuation())),
                        Some((text, wide, tail)),
                        "{setup:?}: ({y},{x})"
                    );
                    assert_eq!(
                        cell.map(|c| c.attributes()),
                        Some(crate::Attributes::default()),
                        "{setup:?}: ({y},{x})"
                    );
                    let link =
                        (linked && (!text.is_empty() || tail)).then_some(("http://x", Some("1")));
                    assert_eq!(
                        p.screen().link(y, x).map(|l| (l.uri(), l.id())),
                        link,
                        "{setup:?}: ({y},{x})"
                    );
                }
            }
        };
        check_cells(&parser);
        assert_eq!(
            (
                parser.screen().cursor_position(),
                parser.screen().pending_wrap()
            ),
            (cursor, pending),
            "{setup:?}"
        );
        if protected {
            parser.process(b"\x1b[0\"q\x1b[H\x1b[?2K")?;
            check_cells(&parser);
        }
    }
    // A glyph already equal to the middle of a run must not stop updates
    // after it, or the link coverage of the whole run.
    for link in ["", "\x1b]8;id=1;http://x\x1b\\"] {
        let mut parser =
            Parser::with_options(Size::of(4, 12), 1024, Feature::ALL.into_iter().collect())?;
        parser.process(format!("{link}AAШA\r").as_bytes())?;
        parser.process("ЖШШШ".as_bytes())?;
        for (x, text) in ["Ж", "Ш", "Ш", "Ш"].into_iter().enumerate() {
            let x = u16::try_from(x).map_err(|_| Error::Capacity)?;
            assert_eq!(parser.screen().cell(0, x).map(|c| c.contents()), Some(text));
            assert_eq!(
                parser.screen().link(0, x).map(|l| l.uri()),
                (!link.is_empty()).then_some("http://x")
            );
        }
        assert_eq!(parser.screen().cursor_position(), (0, 4));
        assert!(!parser.screen().pending_wrap());
    }
    Ok(())
}

/// Each of `rows` rows of `cols` columns with a wide glyph before its last
/// column, then a run of ASCII over the whole row.
fn wide_glyph_rows(rows: u16, cols: u16) -> Vec<u8> {
    let mut text = Vec::new();
    for row in 1..=rows {
        text.extend_from_slice(
            format!("\x1b[{row};{}H\u{4e2d}\r", cols.saturating_sub(1)).as_bytes(),
        );
        text.extend(std::iter::repeat_n(b'a', usize::from(cols)));
    }
    text
}

/// The ASCII run path writes up to a half of a wide glyph and goes on past
/// it: a run over a row that holds one costs the row, not a retry of the
/// rest of the run for every byte.
#[test]
fn a_run_over_a_wide_glyph_costs_its_length() -> Result<(), Error> {
    let mut p = Parser::new(Size::of(40, 8000), 0)?;
    let text = wide_glyph_rows(40, 8000);
    let start = std::time::Instant::now();
    p.process(&text)?;
    let spent = start.elapsed();
    assert!(spent < std::time::Duration::from_secs(1), "{spent:?}");
    let row = p.screen().row_text(39);
    assert_eq!(row, std::iter::repeat_n('a', 8000).collect::<String>());
    Ok(())
}

/// A line feed carried out in text is a place `process_until_stop` looks
/// at the budget, as its scroll can move a row's text into history: with
/// the budget all but spent, a run of LF, VT or FF at the bottom stops
/// after the first, and one of CR, which moves no row, does not stop.
#[test]
fn a_line_feed_stops_at_a_spent_budget() -> Result<(), Error> {
    let mut parser = Parser::new(Size::of(2, 4), 8)?;
    parser.process(b"\x1b[2;1H")?;
    for feeds in [b"\n\n\n", b"\x0b\x0b\x0b", b"\x0c\x0c\x0c"] {
        parser.spent = crate::budget::LIMIT - 1;
        let stop = parser.process_until_stop(feeds, &mut ())?;
        assert_eq!(stop, Some((1, Stop::Budget)), "{}", feeds.escape_ascii());
    }
    parser.spent = crate::budget::LIMIT - 1;
    assert_eq!(parser.process_until_stop(b"\r\r\r", &mut ())?, None);
    Ok(())
}

/// REP's copies a budget stop left after the last byte are made by the
/// next call, even one of no bytes, before any later text: here in insert
/// mode, where each copy moves the row, so a call stops between copies,
/// and each call that takes no byte still makes some.
#[test]
fn copies_left_at_the_budget_are_made_before_later_text() -> Result<(), Error> {
    let size = Size::of(1, 4096);
    let first = b"\x1b[4hx\x1b[65535b";
    for following in ["", "c"] {
        let mut whole = Parser::new(size, 0)?;
        whole.process(first)?;
        whole.process(following.as_bytes())?;
        let mut parser = Parser::new(size, 0)?;
        let stop = parser.process_until_stop(first, &mut ())?;
        assert_eq!(stop, Some((first.len(), Stop::Budget)));
        assert!(parser.has_work_left());
        let (mut rest, mut calls) = (following.as_bytes(), 0);
        loop {
            let before = crate::budget::spent();
            let Some((taken, stop)) = parser.process_until_stop(rest, &mut ())? else {
                break;
            };
            assert_eq!(stop, Stop::Budget);
            assert!(taken > 0 || crate::budget::spent() > before, "no progress");
            rest = rest.get(taken..).unwrap_or_default();
            calls += 1;
        }
        assert!(calls >= 2, "{calls} calls");
        assert!(!parser.has_work_left());
        assert_eq!(
            parser.screen().cursor_position(),
            whole.screen().cursor_position()
        );
        assert_eq!(parser.screen().row_text(0), whole.screen().row_text(0));
        assert_eq!(parser.process_until_stop(&[], &mut ())?, None);
    }
    Ok(())
}

/// Printing that never wraps, and each control, is a place the budget is
/// looked at too: `x` and CR over and over on a 2×2 screen, whole or two
/// bytes a call; zero-width spaces after a row of long clusters, a byte a
/// call (each completed in the parser's UTF-8 state); SUB under IRM and a
/// tab after TBC 3, each before a CR, at 4,096 columns; SUB alone; BS
/// under extended reverse wraparound, alone or before a CR: all stop at
/// the budget, spending little past it between stops.
#[test]
fn printing_that_never_wraps_stops_at_the_budget() -> Result<(), Error> {
    let crs: Vec<u8> = std::iter::repeat_n(&b"x\r"[..], 1_100_000)
        .flatten()
        .copied()
        .collect();
    // B31's row: the cell printed last a long cluster, each mark dropped.
    let cluster = |marks| {
        format!(
            "e{}",
            std::iter::repeat_n("\u{301}", marks).collect::<String>()
        )
    };
    let mut row = (std::iter::repeat_n(cluster(50), 64).chain([cluster(22), cluster(8)]))
        .collect::<String>()
        .into_bytes();
    row.push(0x08);
    let marks: Vec<u8> = std::iter::repeat_n("\u{200b}", 100_000)
        .collect::<String>()
        .into_bytes();
    // A SUB under IRM, which prints and moves the row, and a tab after
    // TBC 3, which looks for a stop across the row, each before a CR.
    let subs: Vec<u8> = std::iter::repeat_n(&b"\x1a\r"[..], 8190)
        .flatten()
        .copied()
        .collect();
    let plain: Vec<u8> = std::iter::repeat_n(&b"\x1a\r"[..], 1_100_000)
        .flatten()
        .copied()
        .collect();
    // A BS under extended reverse wraparound, which wraps back a row.
    let bss = vec![0x08; 1_100_000];
    let bs_crs: Vec<u8> = std::iter::repeat_n(&b"\x08\r"[..], 1_100_000)
        .flatten()
        .copied()
        .collect();
    let tabs: Vec<u8> = std::iter::repeat_n(&b"\t\r"[..], 8190)
        .flatten()
        .copied()
        .collect();
    for (size, setup, input, piece) in [
        (Size::of(2, 2), &b""[..], &crs, crs.len()),
        (Size::of(2, 2), &b""[..], &crs, 2),
        (Size::of(1, 200), &row[..], &marks, 1),
        (Size::of(1, 4096), &b"\x1b[4h"[..], &subs, subs.len()),
        (Size::of(2, 2), &b""[..], &plain, plain.len()),
        (Size::of(1, 1), &b"\x1b[?1045h"[..], &bss, bss.len()),
        (Size::of(1, 1), &b"\x1b[?1045h"[..], &bss, 3),
        (Size::of(1, 2), &b"\x1b[?1045h"[..], &bs_crs, bs_crs.len()),
        (Size::of(1, 4096), &b"\x1b[3g"[..], &tabs, tabs.len()),
    ] {
        let mut parser = Parser::new(size, 0)?;
        parser.process(setup)?;
        let (mut stops, mut since) = (0, 0u64);
        let mut left = &input[..];
        while let Some((mut rest, after)) =
            (!left.is_empty()).then(|| left.split_at_checked(piece).unwrap_or((left, &[])))
        {
            left = after;
            while !rest.is_empty() || parser.has_work_left() {
                let before = crate::budget::spent();
                let stop = parser.process_until_stop(rest, &mut ())?;
                since = since.saturating_add(crate::budget::spent().saturating_sub(before));
                assert!(
                    since <= crate::budget::LIMIT + 10_000,
                    "{since} between stops"
                );
                let Some((taken, stop)) = stop else { break };
                assert_eq!(stop, Stop::Budget);
                (stops, since) = (stops + 1, 0);
                rest = rest.get(taken..).unwrap_or_default();
            }
        }
        assert!(stops >= 1, "no stop at {size:?}, {piece} bytes a call");
    }
    Ok(())
}

/// The alternate screen holds no cells until a program shows it: not as
/// made, nor resized, reset (RIS, DECSTR) or left, its modes saved and
/// restored unset; then as many as the screen.
#[test]
fn the_alternate_screen_holds_no_cells_until_shown() -> Result<(), Error> {
    let mut parser = Parser::new(Size::of(50, 200), 0)?;
    assert_eq!(parser.screen().storage_cells(), 50 * 200);
    parser.resize(Size::of(40, 120))?;
    assert_eq!(parser.screen().storage_cells(), 40 * 120);
    parser.process(b"\x1bc\x1b[!p\x1b[?47l\x1b[?1047l\x1b[?1049l\x1b[?1049s\x1b[?1049r")?;
    parser.resize(Size::of(30, 100))?;
    parser.process(b"\x1bc")?;
    assert_eq!(parser.screen().storage_cells(), 30 * 100);
    parser.process(b"\x1b[?1049h")?;
    assert_eq!(parser.screen().storage_cells(), 2 * 30 * 100);
    // RIS with history makes both grids afresh, the alternate unmade: a
    // history the style table leaves room in for rows.
    let mut parser = Parser::new(Size::of(5, 10), 64 << 10)?;
    parser.process(b"\x1b[?1049h\x1b[?1049l")?;
    let lines: Vec<u8> = std::iter::repeat_n(*b"x\n", 20).flatten().collect();
    parser.process(&lines)?;
    parser.process(b"\x1bc")?;
    assert_eq!(parser.screen().storage_cells(), 5 * 10);
    Ok(())
}

/// The printable run, as the general path counts it: a byte at a time.
fn printable_slowly(bytes: &[u8]) -> usize {
    bytes
        .iter()
        .take_while(|b| (0x20..=0x7e).contains(*b))
        .count()
}

/// `printable`, found a word at a time, is the run counted a byte at a
/// time: every byte at every place of runs up to 24 long, after every
/// printable byte and before, and random bytes, mostly printable.
#[test]
fn printable_runs_are_found_a_word_at_a_time_as_a_byte_at_a_time() {
    for len in 1..=24usize {
        for at in 1..len {
            for byte in 0..=u8::MAX {
                for fill in *b" a~" {
                    let mut bytes = vec![fill; len];
                    if let Some(slot) = bytes.get_mut(at) {
                        *slot = byte;
                    }
                    assert_eq!(
                        printable(&bytes),
                        printable_slowly(&bytes),
                        "{len} bytes, {byte:#x} at {at}"
                    );
                }
            }
        }
    }
    let mut r = Rng(7);
    for _ in 0..20_000 {
        let len = r.below(40).saturating_add(1);
        let mut bytes: Vec<u8> = (0..len)
            .map(|_| {
                if r.chance(95) {
                    r.byte_in(0x20, 0x7e)
                } else {
                    r.byte_in(0, 0xff)
                }
            })
            .collect();
        if let Some(first) = bytes.first_mut() {
            *first = r.byte_in(0x20, 0x7e);
        }
        assert_eq!(printable(&bytes), printable_slowly(&bytes), "{bytes:?}");
    }
}

/// A scan and a streamed read stop at the first reset, not a later reset in
/// the same input. Saving 2026 while a frame is held changes what restore
/// means; cancelled sequences and ESC-terminated strings keep their rules.
#[test]
fn a_frame_scan_stops_at_the_first_synchronized_output_reset() -> Result<(), Error> {
    for saved in [false, true] {
        for (end, ends) in [
            (&b"\x1b[?2026l"[..], true),
            (b"\x1b[?25;2026l", true),
            (b"\x1b[?20\r26l", true),
            (b"\x1bc", true),
            (b"\x1b[!p", true),
            (b"\x1b[61;1\"p", true),
            (b"\x1b[62\"p", true),
            (b"\x1b[63\"p", false),
            (b"\x1b]2;\x1b[?2026l", true),
            (b"\x1bP1;2|x\x1b[?2026l", true),
            (b"\x1bPq#0~\x1b[?2026l", true),
            (b"\x1b_x\x1b[?2026l", true),
            (b"\x1b[?2026r", !saved),
            (b"\x1b[?2026s\x1b[?2026r", false),
            (b"\x1b[?25;2026s\x1b[?2026r", false),
            (b"\x1b[?25s\x1b[?2026r", !saved),
            (b"\x1b[?20\x1826l", false),
            (b"\x1b[?2026:1l", false),
            (b"\x1b[?2026h", false),
            (b"\x1b]2;hello h\x07", false),
            (b"\x1b[?2026", false),
        ] {
            let mut parser = Parser::new(Size::of(4, 10), 8)?;
            if saved {
                parser.process(b"\x1b[?2026h")?;
            }
            parser.process(b"\x1b[?2026s\x1b[?2026h")?;
            for suffix in [b"".as_slice(), b"x\x1b[?2026l"] {
                let input = [b"ab".as_slice(), end, suffix].concat();
                let expected = if ends {
                    Some(2 + end.len())
                } else {
                    (!suffix.is_empty()).then_some(input.len())
                };
                for chunk in [1, 2, input.len()] {
                    let mut scan = parser.frame_scan();
                    let (mut held, mut found) = (0usize, None);
                    while held < input.len() && found.is_none() {
                        held = (held + chunk).min(input.len());
                        found = scan.end(input.get(..held).ok_or(Error::Capacity)?);
                        assert_eq!(
                            found,
                            expected.filter(|&at| at <= held),
                            "{end:?}, saved {saved}, held {held}"
                        );
                    }
                    assert_eq!(found, expected);
                    let mut streamed = parser.clone();
                    let (mut offset, mut first) = (0, None);
                    let mut rest = input.as_slice();
                    while !rest.is_empty() {
                        let (mut piece, tail) = rest.split_at_checked(chunk).unwrap_or((rest, &[]));
                        rest = tail;
                        while !piece.is_empty() {
                            let taken = match streamed.process_until_stop(piece, &mut ())? {
                                Some((taken, stop)) => {
                                    first.get_or_insert((offset + taken, stop));
                                    taken
                                }
                                None => piece.len(),
                            };
                            assert!(taken > 0, "a small frame must make progress");
                            piece = piece.get(taken..).ok_or(Error::Capacity)?;
                            offset += taken;
                        }
                    }
                    assert_eq!(
                        first,
                        expected.map(|at| (at, Stop::Ended)),
                        "{end:?}, saved {saved}, chunk {chunk}"
                    );
                }
            }
        }
    }
    Ok(())
}

/// `esc_in` finds the first ESC at every place in and past a word, and
/// none where there is none.
#[test]
fn esc_in_finds_the_first_esc() {
    for len in 0..20 {
        for at in 0..len {
            let mut bytes = vec![b'x'; len];
            for (i, b) in bytes.iter_mut().enumerate() {
                if i == at || i == at.saturating_add(3) {
                    *b = 0x1b;
                } else if i % 3 == 0 {
                    *b = 0x9b;
                }
            }
            assert_eq!(esc_in(&bytes), Some(at), "{len} {at}");
        }
        assert_eq!(esc_in(&vec![0x1a; len]), None);
    }
}

/// The word scans for strings, `control_in` and `string_end`, find the
/// byte a byte-at-a-time scan finds, at every place in and past a word,
/// among bytes of every kind.
#[test]
fn the_string_scans_find_what_a_byte_at_a_time_finds() {
    let mut r = Rng(7);
    for _ in 0..20_000 {
        let bytes: Vec<u8> = (0..r.below(40))
            .map(|_| match r.below(6) {
                0 => r.byte_in(0, 0x1f),
                1 => 0x7f,
                _ => r.byte_in(0, 0xff),
            })
            .collect();
        let control = bytes.iter().position(|&b| b < 0x20 || b == 0x7f);
        let end = bytes.iter().position(|&b| matches!(b, 0x18 | 0x1a | 0x1b));
        assert_eq!(control_in(&bytes), control, "{bytes:?}");
        assert_eq!(string_end(&bytes), end, "{bytes:?}");
    }
}

/// A scroll of many rows inside a region spends what it does: the region's
/// slots turned once, not once a row, and the rows brought in. At 4,095
/// rows of one column an IL 4095 is linear in the region, at most 8 units
/// a row; charged a slot a row a row, each was 16.8 M.
#[test]
fn a_region_scroll_spends_its_slots_once() -> Result<(), Error> {
    let mut parser = Parser::new(Size::of(4095, 1), 0)?;
    let bytes: Vec<u8> = std::iter::repeat_n(&b"\x1b[4095L"[..], 100)
        .flatten()
        .copied()
        .collect();
    let start = crate::budget::spent();
    parser.process(&bytes)?;
    let spent = crate::budget::spent().saturating_sub(start);
    assert!(spent <= 100 * 8 * 4095, "{spent}");
    Ok(())
}
