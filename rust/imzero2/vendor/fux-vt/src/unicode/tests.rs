use super::tables::UNICODE_VERSION;
use super::*;
use unicode_segmentation::UnicodeSegmentation;

/// Where `text` breaks, as byte offsets inside it, by the state machine.
fn breaks(text: &str) -> impl Iterator<Item = usize> {
    let mut chars = text.char_indices();
    let mut cluster = chars.next().map(|(_, c)| Cluster::start(c));
    chars
        .filter(move |&(_, c)| cluster.as_mut().is_some_and(|k| !k.push(c)))
        .map(|(at, _)| at)
}

fn boundaries(text: &str) -> Vec<usize> {
    breaks(text).collect()
}

/// Where `text` breaks by unicode-segmentation, which implements UAX #29 as
/// written.
fn oracle(text: &str) -> impl Iterator<Item = usize> {
    text.grapheme_indices(true).map(|(at, _)| at).skip(1)
}

#[test]
fn the_tables_are_the_version_unicode_segmentation_implements() {
    assert_eq!(
        UNICODE_VERSION,
        (
            u8::try_from(unicode_segmentation::UNICODE_VERSION.0).unwrap_or(0),
            u8::try_from(unicode_segmentation::UNICODE_VERSION.1).unwrap_or(0),
            u8::try_from(unicode_segmentation::UNICODE_VERSION.2).unwrap_or(0),
        )
    );
}

/// Every case of the Unicode conformance test, GraphemeBreakTest.txt, as
/// UAX #29 has it: the file of the version the tables are generated at,
/// as its header names it.
#[test]
fn every_conformance_case_breaks_where_uax_29_says() {
    let cases = include_str!("../../tests/data/GraphemeBreakTest.txt");
    let (major, minor, update) = UNICODE_VERSION;
    let header = format!("# GraphemeBreakTest-{major}.{minor}.{update}.txt");
    assert!(
        cases.lines().next().is_some_and(|l| l.starts_with(&header)),
        "GraphemeBreakTest.txt is not {header}"
    );
    let mut checked = 0u32;
    for case in cases.lines().filter(|line| !line.starts_with('#')) {
        let mut text = String::new();
        let mut expected = Vec::new();
        let mut tokens = case.split_whitespace().peekable();
        while let Some(token) = tokens.next() {
            match token {
                "÷" if !text.is_empty() && tokens.peek().is_some() => expected.push(text.len()),
                "÷" | "×" => {}
                hex => text.extend(u32::from_str_radix(hex, 16).ok().and_then(char::from_u32)),
            }
        }
        assert_eq!(boundaries(&text), expected, "{case}");
        checked = checked.saturating_add(1);
    }
    assert_eq!(checked, 766);
}

/// Every assigned scalar value's properties, against unicode-segmentation:
/// each is put after and before characters whose rules single out one
/// property, so a code point classed wrongly breaks differently. Past plane 3
/// only the tags and variation selectors (E0000..E0FFF) are assigned, other
/// than private use; the rest is sampled every 61st code point.
#[test]
fn every_code_point_is_classed_as_unicode_segmentation_classes_it() {
    // What goes before the code point, and after it.
    let around: &[(&str, &str)] = &[
        ("a", ""),                     // Extend, ZWJ, SpacingMark
        ("\u{1100}", ""),              // Hangul L, V, LV, LVT
        ("\u{AC00}", ""),              // V, T after LV
        ("\u{AC01}", ""),              // T after LVT
        ("\u{1F1E6}", ""),             // Regional_Indicator
        ("\u{1F600}\u{200D}", ""),     // Extended_Pictographic
        ("\u{915}\u{94D}", ""),        // InCB Consonant
        ("\r", ""),                    // LF
        ("\u{600}", ""),               // what follows Prepend
        ("", "\u{301}"),               // Control, CR, LF
        ("", "a"),                     // Prepend
        ("", "\u{1161}"),              // L, LV, V before V
        ("", "\u{11A8}"),              // LV, LVT, V, T before T
        ("", "\u{1100}"),              // L before L
        ("", "\u{1F1E6}"),             // Regional_Indicator
        ("", "\u{200D}\u{1F600}"),     // Extended_Pictographic
        ("", "\u{94D}\u{915}"),        // InCB Consonant
        ("\u{915}", "\u{915}"),        // InCB Linker
        ("\u{915}", "\u{94D}\u{915}"), // InCB Extend
    ];
    let mut text = String::new();
    let mut checked = 0u32;
    let assigned = |p: &u32| *p < 0x4_0000 || (0xE_0000..0xE_1000).contains(p);
    let code_points = (0..=0x10_FFFF).filter(|p| assigned(p) || p % 61 == 0);
    for c in code_points.filter_map(char::from_u32) {
        for (before, after) in around {
            text.clear();
            text.push_str(before);
            text.push(c);
            text.push_str(after);
            if !breaks(&text).eq(oracle(&text)) {
                assert_eq!(
                    boundaries(&text),
                    oracle(&text).collect::<Vec<_>>(),
                    "{text:?}"
                );
            }
            checked = checked.saturating_add(1);
        }
    }
    assert!(checked > 5_000_000, "{checked}");
}

#[test]
fn a_cluster_rebuilt_from_its_text_continues_as_it_would_have() {
    for (text, next, joins) in [
        ("\u{1F469}\u{200D}", '\u{1F52C}', true),
        ("\u{1F469}", '\u{1F52C}', false),
        ("\u{1F1EF}", '\u{1F1F5}', true),
        ("\u{1F1EF}\u{1F1F5}", '\u{1F1E6}', false),
        ("\u{915}\u{94D}", '\u{937}', true),
        ("\u{915}", '\u{937}', false),
        ("\u{600}", 'a', true),
        ("\u{600}", '\r', false),
        ("", '\u{301}', true),
    ] {
        assert_eq!(Cluster::of(text).push(next), joins, "{text:?} {next:?}");
    }
}

/// `continues_cluster` is the state machine's answer, the fast path for
/// printable ASCII included.
#[test]
fn continues_cluster_answers_as_the_state_machine() {
    let clusters = [
        "",
        "a",
        " ",
        "\u{1F44D}",
        "\u{1F469}\u{200D}",
        "\u{1F1EF}",
        "\u{915}\u{94D}",
        "\u{600}",
        "\u{1100}",
        "\r",
        "\u{e4}\u{356}",
    ];
    // Every character of the planes in use, and the tags and variation
    // selectors: the shortcuts answer as the state machine does for each.
    let nexts = ('\0'..='\u{2FFFF}').chain('\u{E0000}'..='\u{E01EF}');
    for next in nexts {
        for cluster in clusters {
            assert_eq!(
                continues_cluster(cluster, next),
                Cluster::of(cluster).push(next),
                "{cluster:?} {next:?}"
            );
        }
    }
    assert!(continues_cluster("\u{1F44D}", '\u{1F3FD}'));
    assert!(continues_cluster("a", '\u{301}'));
    assert!(!continues_cluster("\u{1F44D}", 'x'));
}

/// Whether fux-vt's width rule gives `c` other columns than
/// unicode-width 0.2.2: U+17D8, and the spacing marks it makes 0.
fn departs(c: char) -> bool {
    use unicode_width::UnicodeWidthChar;
    crate::char_width(c) != c.width()
}

/// fux-vt's widths (THREATS.md B32): no character takes more than 2
/// columns, U+17D8 KHMER SIGN BEYYAL, the one unicode-width makes 3,
/// takes 1, as in xterm 411; the 34 spacing marks (Mc) of combining
/// class 0 it makes 0, as Grapheme_Extend, take 1, as in wcwidth 0.9.2;
/// every other is
/// unicode-width 0.2.2's, of the same Unicode version as the tables.
#[test]
fn every_character_is_as_wide_as_the_width_rule_says() {
    use unicode_width::UnicodeWidthChar;
    assert_eq!(unicode_width::UNICODE_VERSION, UNICODE_VERSION);
    let mut spacing = 0u32;
    for c in (0..=0x10_ffff).filter_map(char::from_u32) {
        let width = crate::char_width(c);
        assert!(width.is_none_or(|w| w <= 2), "{c:?} is {width:?} wide");
        if c != '\u{17d8}' && width != c.width() {
            // A spacing mark: Grapheme_Extend, so Extend to UAX #29.
            assert_eq!((width, c.width()), (Some(1), Some(0)), "{c:?}");
            assert_eq!(Properties::of(c).kind(), super::Break::Extend, "{c:?}");
            spacing = spacing.saturating_add(1);
        }
    }
    assert_eq!(spacing, 34);
    for mark in [
        '\u{9be}',
        '\u{bbe}',
        '\u{cc0}',
        '\u{d3e}',
        '\u{dcf}',
        '\u{1133e}',
    ] {
        assert_eq!(crate::char_width(mark), Some(1), "{mark:?}");
    }
    // Spacing marks of another class stay at none: viramas, the Hangul
    // tone marks and Vietnamese reading marks (East Asian Wide), the
    // musical stems.
    for other in ['\u{302e}', '\u{16ff0}', '\u{1d165}', '\u{1d16e}'] {
        assert_eq!(crate::char_width(other), Some(0), "{other:?}");
    }
    for virama in ['\u{a9c0}', '\u{1134d}'] {
        assert_eq!(crate::char_width(virama), Some(0), "{virama:?}");
    }
    assert_eq!(crate::char_width('\u{17d8}'), Some(1));
    assert_eq!(crate::str_width("\u{17d8}"), 1);
    assert_eq!(crate::str_width("a\u{17d8}b\u{17d8}"), 4);
    assert_eq!(crate::str_width("一e\u{301}"), 3);
}

/// The characters whose rules in unicode-width's string width look at a
/// neighbour: the variation selectors, ZWJ, the keycap, an emoji modifier,
/// a regional indicator, the tags, Kirat Rai's vowel signs, a combining
/// mark, the Tifinagh joiner and the Khmer coeng.
const CONTEXT: &[char] = &[
    '\u{fe00}',
    '\u{fe01}',
    '\u{fe0e}',
    '\u{fe0f}',
    '\u{200d}',
    '\u{20e3}',
    '\u{1f3fb}',
    '\u{1f3ff}',
    '\u{1f1e6}',
    '\u{1f1ef}',
    '\u{e0067}',
    '\u{e0031}',
    '\u{e007f}',
    '\u{16d63}',
    '\u{16d67}',
    '\u{16d68}',
    '\u{16d69}',
    '\u{301}',
    '\u{2d7f}',
    '\u{17d2}',
    '\u{a8fa}',
];

/// Whether `text` is one grapheme cluster, as fux-vt segments it.
fn one_cluster(text: &str) -> bool {
    breaks(text).next().is_none()
}

/// The cluster rule (`cluster_width`) is unicode-width 0.2.2's
/// string width of that cluster, but for a cluster holding U+17D8, a
/// spacing mark the rule departs on, or a Khmer coeng (whose rule fux-vt
/// does not take: `a_spacing_mark_and_a_coeng_conjunct_take_their_columns`):
/// for every character alone,
/// and before and after each of `CONTEXT`'s; for the ZWJ, keycap,
/// modifier, flag and tag sequences built of every emoji; and for every
/// cluster of the Unicode conformance test.
#[test]
fn a_cluster_is_as_wide_as_unicode_width_makes_it() {
    use unicode_width::UnicodeWidthStr;
    let mut checked = 0u32;
    let mut check = |text: &str| {
        if one_cluster(text) && !text.chars().any(|c| c == '\u{17d2}' || departs(c)) {
            assert_eq!(super::cluster_width(text), text.width(), "{text:?}");
            checked = checked.saturating_add(1);
        }
    };
    let all = || (0..=0x10_ffff).filter_map(char::from_u32);
    let mut text = String::new();
    for c in all() {
        // U+FE0F changes only an emoji's width, or what follows one.
        let emoji = bits(c) & (1 << 9 | 1 << 10 | 1 << 12) != 0;
        let selected = [&[c][..], &[c, '\u{fe0f}']];
        for chars in selected.iter().take(if emoji { 2 } else { 1 }) {
            for &k in CONTEXT {
                text.clear();
                text.extend(chars.iter().chain([&k]));
                check(&text);
                text.clear();
                text.extend([&k].into_iter().chain(chars.iter()));
                check(&text);
            }
        }
        check(c.encode_utf8(&mut [0; 4]));
    }
    let emoji: Vec<char> = all()
        .filter(|&c| bits(c) & (1 << 9 | 1 << 10 | 1 << 12) != 0)
        .collect();
    let tags = [
        "",
        "\u{e0067}\u{e0062}\u{e0065}\u{e006e}\u{e0067}",
        "\u{e0075}\u{e0073}\u{e0031}\u{e0032}",
    ];
    for &a in &emoji {
        for b in [
            '\u{1f468}',
            '\u{2764}',
            '\u{1f1fa}',
            '\u{1f3fd}',
            '#',
            '\u{1f3f4}',
        ] {
            check(&format!("{a}\u{200d}{b}"));
            check(&format!("{b}\u{200d}{a}"));
            check(&format!("{a}\u{fe0f}\u{200d}{b}\u{fe0f}"));
            check(&format!("{b}\u{fe0f}\u{200d}{a}\u{fe0f}\u{20e3}"));
            check(&format!("{a}\u{1f3fb}\u{200d}{b}\u{1f3ff}"));
        }
        for tag in tags {
            check(&format!("\u{1f3f4}{tag}\u{e007f}\u{200d}{a}"));
            check(&format!("{a}\u{200d}\u{1f3f4}{tag}\u{e007f}"));
        }
    }
    for flags in [
        "\u{1f1fa}",
        "\u{1f1fa}\u{1f1f8}",
        "\u{1f1fa}\u{1f1f8}\u{1f1fa}",
    ] {
        check(&format!("\u{1f468}\u{200d}{flags}"));
        check(&format!("{flags}\u{200d}\u{1f468}"));
    }
    // Conjuncts (GB9c): each consonant, a linker and a consonant, its own
    // and another script's, ZWJ or a mark between.
    let consonants: Vec<char> = all().filter(|&c| (bits(c) >> 5) & 3 == 1).collect();
    let linkers: Vec<char> = all().filter(|&c| (bits(c) >> 5) & 3 == 3).collect();
    for &c in &consonants {
        for &linker in &linkers {
            for d in [c, '\u{915}', '\u{1780}', '\u{17a0}', '\u{1a20}'] {
                check(&format!("{c}{linker}{d}"));
                check(&format!("{c}{linker}\u{200d}{d}"));
                check(&format!("{c}\u{17b4}{linker}{d}\u{fe0f}"));
            }
        }
    }
    for digit in ['0', '9', '#', '*'] {
        check(&format!("{digit}\u{fe0f}\u{20e3}"));
        check(&format!("\u{1f468}\u{200d}{digit}\u{fe0f}\u{20e3}"));
    }
    let cases = include_str!("../../tests/data/GraphemeBreakTest.txt");
    for case in cases.lines().filter(|line| !line.starts_with('#')) {
        for cluster in case.split('÷') {
            let text: String = (cluster.split_whitespace())
                .filter_map(|hex| u32::from_str_radix(hex, 16).ok().and_then(char::from_u32))
                .collect();
            check(&text);
        }
    }
    assert!(checked > 15_000_000, "{checked} clusters");
}

/// fux-vt's departures in a cluster: a consonant and a spacing vowel sign
/// take two columns, each one, as wcwidth 0.9.2 measures them (ucs-detect
/// measures Bengali, Kannada, Malayalam, Sinhala, Tamil and Grantha so); a
/// Khmer conjunct, two letters joined by a coeng (Mn, none), two; a spacing
/// virama none, so a Javanese consonant and its pangkon one.
#[test]
fn a_spacing_mark_and_a_coeng_conjunct_take_their_columns() {
    for (text, width) in [
        ("\u{995}\u{9be}", 2),
        ("\u{c95}\u{cc0}", 2),
        ("\u{d15}\u{d3e}", 2),
        ("\u{d9a}\u{dcf}", 2),
        ("\u{b95}\u{bbe}", 2),
        ("\u{11315}\u{1133e}", 2),
        ("\u{1780}\u{17d2}\u{1781}", 2),
        ("\u{1780}\u{17d2}\u{178a}\u{17b7}", 2),
        ("\u{a98f}\u{a9c0}", 1),
        ("\u{11315}\u{1134d}", 1),
        // Two-part vowel signs: as their composed forms.
        ("\u{995}\u{9c7}\u{9be}", 2),
        ("\u{9c7}\u{9be}", 1),
        ("\u{9cb}", 1),
        ("\u{d9a}\u{dd9}\u{dcf}\u{dca}", 2),
        ("\u{dd9}\u{dcf}\u{dca}", 1),
        ("\u{b92}\u{bd7}", 1),
        ("\u{1d158}\u{1d165}", 1),
        // A mark of another class between: canonical order and NFC.
        ("\u{1d15f}\u{334}", 1),
        ("\u{1d158}\u{334}\u{1d165}", 1),
        ("\u{1d158}\u{1d165}\u{334}", 1),
        ("\u{ac00}\u{302e}", 2),
        ("a\u{302e}", 1),
    ] {
        assert!(one_cluster(text), "{text:?}");
        assert_eq!(super::cluster_width(text), width, "{text:?}");
    }
}

/// `Cluster::start`'s ASCII shortcut is what the table gives.
#[test]
fn a_cluster_started_with_ascii_is_as_the_table_starts_it() {
    for c in ' '..='~' {
        assert_eq!(
            Cluster::start(c),
            Cluster::default().after(Properties::of(c), false),
            "{c:?}"
        );
    }
}

/// The table's bit for the characters the cluster width rule looks at
/// (`width::CONTEXT`) is set on exactly those its arms name: a line feed,
/// a spacing mark fux-vt widens, ZWJ, the keycap, the variation selectors, Kirat Rai's
/// vowel signs, the regional indicators, the emoji modifiers and the tags.
#[test]
fn the_width_rule_looks_at_the_characters_its_arms_name() {
    for c in (0..=0x10_ffffu32).filter_map(char::from_u32) {
        let named = matches!(
            c,
            '\n' | '\u{200d}'
                | '\u{20e3}'
                | '\u{fe00}'..='\u{fe0f}'
                | '\u{16d63}'..='\u{16d6a}'
                | '\u{1f1e6}'..='\u{1f1ff}'
                | '\u{1f3fb}'..='\u{1f3ff}'
                | '\u{e0020}'..='\u{e007f}'
        );
        let spacing = c != '\u{17d8}' && departs(c);
        assert_eq!(
            Properties::of(c).in_context(),
            named || spacing,
            "{:04X}",
            u32::from(c)
        );
    }
}

/// A flag, two regional indicators, is two columns wide by the cluster
/// rule, as `Screen::join` takes it without measuring: every pair.
#[test]
fn every_pair_of_regional_indicators_is_two_columns() {
    for a in '\u{1f1e6}'..='\u{1f1ff}' {
        for b in '\u{1f1e6}'..='\u{1f1ff}' {
            let flag: String = [a, b].into_iter().collect();
            assert_eq!(super::cluster_width(&flag), 2, "{flag}");
            assert!(Properties::of(a).regional_indicator());
        }
    }
}
