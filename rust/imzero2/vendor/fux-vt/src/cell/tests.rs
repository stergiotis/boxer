use super::*;

/// A cell stored in `Cells` reads back as it was made, whichever part of
/// the text keeps it.
#[test]
fn stored_cells_read_back_as_they_were_made() {
    let attributes = Attributes {
        foreground: Packed::new(Color::Idx(9)),
        background: Packed::new(Color::Rgb([1, 2, 3].into())),
        underline_color: Packed::new(Color::Idx(4)),
        flags: (0x1ff & !Attributes::RAPID_BLINK) | Attributes::UNDERLINE,
    };
    let seventeen = "a\u{301}\u{301}\u{301}\u{301}\u{301}\u{301}\u{301}\u{301}";
    let mut cells = Cells::new(3).unwrap_or_default();
    for (i, text) in ["é", seventeen, &format!("{seventeen}{seventeen}")]
        .into_iter()
        .enumerate()
    {
        assert!(cells.set(i, CellRef::new(text, true, attributes)));
        let view = cells.get(i).unwrap_or_default();
        assert_eq!(view.contents(), text);
        assert!(view.is_wide() && !view.contents().is_empty() && !view.is_wide_continuation());
        assert_eq!(view.attributes(), attributes);
    }
}

/// Past the 16 MiB a cell's locator reaches, a cluster is not kept whole,
/// and `set` says so: every cell `set` says it kept reads back whole.
#[test]
fn cells_past_a_locator_s_reach_say_their_text_was_not_kept() -> Result<(), Error> {
    let cluster: String = std::iter::once('e')
        .chain(std::iter::repeat_n('\u{301}', 15))
        .collect();
    let mut cells = Cells::new(1 << 20)?;
    let set = |cells: &mut Cells, i: usize| {
        cells.set(i, CellRef::new(&cluster, false, Attributes::default()))
    };
    let mut i = 0;
    while set(&mut cells, i) {
        assert_eq!(cells.get(i).map(|c| c.contents()), Some(cluster.as_str()));
        i = i.saturating_add(1);
    }
    assert!(i > 500_000 && cells.text.long_len() <= 1 << 24, "{i} kept");
    assert!(!set(&mut cells, i.saturating_add(1)) && !set(&mut cells, i.saturating_add(2)));
    Ok(())
}

/// The underline's style takes three bits of the flags, apart from every
/// other style's: each style, set over any other attributes, reads back
/// alone, and setting it changes nothing else.
#[test]
fn underline_styles_keep_to_their_own_bits() {
    use super::UnderlineStyle;
    let others = Attributes::new(Color::Idx(1), Color::Rgb([1, 2, 3].into()))
        .with_bold(true)
        .with_dim(true)
        .with_italic(true)
        .with_inverse(true)
        .with_blink(Blink::Rapid)
        .with_hidden(true)
        .with_strikeout(true)
        .with_underline_color(Color::Idx(5));
    for n in 0..=5 {
        let style = UnderlineStyle::from_number(n).unwrap_or_default();
        assert_eq!(style.number(), n);
        for base in [Attributes::default(), others] {
            let a = base
                .with_underline_style(UnderlineStyle::Dashed)
                .with_underline_style(style);
            assert_eq!(a.underline_style(), style);
            assert_eq!(a.underline(), n != 0);
            assert_eq!(a.with_underline_style(UnderlineStyle::None), base);
        }
    }
    assert_eq!(UnderlineStyle::from_number(6), None);
    assert_eq!(
        Attributes::default().with_underline(true).underline_style(),
        UnderlineStyle::Single
    );
}

#[test]
fn owned_cells_copy_text_from_wherever_it_is_and_compare_by_it() {
    let family = "\u{1F468}\u{200D}\u{1F469}\u{200D}\u{1F467}\u{200D}\u{1F466}";
    let mut a = Cells::new(4).unwrap_or_default();
    assert!(a.set(1, CellRef::new(family, true, Attributes::default())));
    a.set(2, CellRef::wide_continuation());
    // Copied cell by cell, through a third run, in another order.
    let mut c = Cells::new(4).unwrap_or_default();
    for i in [3, 2, 1, 0] {
        if let Some(cell) = a.get(i) {
            c.set(i, cell);
        }
    }
    let mut b = Cells::new(4).unwrap_or_default();
    for i in 0..4 {
        if let Some(cell) = c.get(i) {
            b.set(i, cell);
        }
    }
    assert_eq!(a, b);
    assert_eq!(b.get(1).map(|c| c.contents()), Some(family));
    b.set_attributes(1, Attributes::default().with_bold(true));
    assert_ne!(a, b);
    assert!(
        b.get(1)
            .is_some_and(|c| c.attributes().bold() && c.contents() == family)
    );
    b.fill(0..4, CellRef::default());
    assert_eq!(b, Cells::new(4).unwrap_or_default());
    assert_eq!(b.range(2..9).len(), 2);
}

/// Equality reads text, width and attributes across inline, short and long
/// storage; unused text and ranges outside the cells do not differ.
#[test]
fn range_comparisons_ignore_storage_and_detect_cell_changes() -> Result<(), Error> {
    let long = "a\u{301}\u{301}\u{301}\u{301}\u{301}\u{301}\u{301}\u{301}\u{301}";
    let short = "a\u{301}\u{301}\u{301}\u{301}\u{301}\u{301}\u{301}\u{301}";
    for text in ["a", "é", "界", short, long, "👨‍👩‍👧‍👦"] {
        let mut a = Cells::new(6)?;
        let mut b = Cells::new(6)?;
        a.set(1, CellRef::new(text, false, Attributes::default()));
        b.set(1, CellRef::new(long, false, Attributes::default()));
        b.set(1, CellRef::new(text, false, Attributes::default()));
        assert_eq!(a, b, "unused stored text must not affect equality");
        for changed in [
            CellRef::new(text, false, Attributes::default().with_bold(true)),
            CellRef::new(text, true, Attributes::default()),
            CellRef::new("different", false, Attributes::default()),
            CellRef::wide_continuation(),
        ] {
            b.set(1, changed);
            assert_ne!(a, b);
            for start in 0..=8 {
                for end in start..=10 {
                    assert_eq!(
                        a.range_eq(&b, start..end),
                        !(start <= 1 && end > 1),
                        "{text:?}, {start}..{end}"
                    );
                }
            }
        }
    }
    Ok(())
}

/// The SGR change from one pen to another, read by a terminal whose pen
/// is the first, leaves it the second, and is no longer than a reset and
/// the second.
#[test]
fn an_sgr_change_takes_a_pen_from_one_to_the_other() -> Result<(), Error> {
    let plain = Attributes::default();
    let pens = [
        plain,
        plain.with_bold(true).with_dim(true),
        plain.with_dim(true).with_italic(true),
        Attributes::new(Color::Idx(1), Color::Rgb([1, 2, 3].into()))
            .with_underline_style(UnderlineStyle::Curly)
            .with_blink(Blink::Rapid),
        Attributes::new(Color::Idx(12), Color::Default)
            .with_underline_color(Color::Idx(200))
            .with_inverse(true)
            .with_strikeout(true),
    ];
    for (from, to) in pens.iter().flat_map(|a| pens.iter().map(move |b| (*a, *b))) {
        let (mut set, mut change, mut full) = (b"\x1b[0".to_vec(), Vec::new(), b"\x1b[0".to_vec());
        from.write_sgr(&mut set, SgrForm::Paint { styles: true });
        set.push(b'm');
        to.write_sgr_change(from, &mut change, true);
        to.write_sgr(&mut full, SgrForm::Paint { styles: true });
        let mut parser =
            crate::Parser::new(crate::Size::new(1, 2).map_err(|_| Error::Capacity)?, 0)?;
        parser.process(&[set, change.clone(), b"x".to_vec()].concat())?;
        let got = parser.screen().window().cell(0, 0).map(|c| c.attributes());
        assert_eq!(got, Some(to), "{:?}", change.escape_ascii().to_string());
        assert!(change.len() <= full.len().saturating_add(1) && (from != to || change.is_empty()));
    }
    // SPA's guard is no SGR: attributes that differ only by it need none.
    // Read from guarded cells they don't differ (`Styles::get` drops it);
    // made here, they do.
    let mut parser = crate::Parser::new(crate::Size::new(1, 2).map_err(|_| Error::Capacity)?, 0)?;
    parser.process(b"\x1b[1m\x1bVa\x1bWb")?;
    let pen = |x| parser.screen().window().cell(0, x).map(|c| c.attributes());
    let a = pen(0).unwrap_or_default();
    let guarded = Attributes {
        flags: a.flags | Attributes::GUARDED,
        ..a
    };
    for (to, from) in [(pen(1).unwrap_or_default(), a), (a, guarded), (guarded, a)] {
        let mut change = Vec::new();
        to.write_sgr_change(from, &mut change, true);
        assert!(
            to.bold() && change.is_empty(),
            "{:?}",
            change.escape_ascii().to_string()
        );
    }
    Ok(())
}
