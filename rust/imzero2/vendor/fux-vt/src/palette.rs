//! The colours a program sets, queries and resets, with
//! [`crate::Feature::Palette`]: the 256-colour palette (OSC 4, OSC 104),
//! xterm's special colours (OSC 5, OSC 105, and OSC 4 past the palette) and
//! the dynamic colours (OSC 10 to 19, OSC 110 to 119), as xterm's ctlseqs
//! give them ("Operating System Commands") and xterm 411 reads them.
//!
//! A colour is kept as xterm keeps it, 8 bits a channel (what its 24-bit
//! visual holds), and reported as xterm reports it, each channel's byte
//! twice: `rgb:cdcd/0000/0000`. A specification is read as X11's
//! XParseColor reads it, in the two forms ctlseqs names: `rgb:R/G/B`, each
//! channel one to four hex digits scaled to 16 bits (`f` is `ffff`), and
//! `#RGB`, `#RRGGBB`, `#RRRGGGBBB` and `#RRRRGGGGBBBB`, the digits the
//! channel's high bits (`#fff` is `f000`); of the 16 bits the high byte is
//! kept, so `#fff` reports `f0f0`, as xterm does. XParseColor's
//! device-independent spaces (`rgbi:`, `CIEXYZ:` and the rest), which
//! need libX11's colour management, and colour names are not read: a
//! specification fux-vt cannot read changes nothing.
//!
//! What fux-vt keeps is what the program set, and what the host says its
//! terminal shows for palette entries 0 to 15 ([`crate::Parser::set_host_palette`]):
//! the colours themselves are the terminal's. A palette entry the program
//! has not set is answered with the host's colour for it, if the host gave
//! one, else not at all: what the terminal shows for it is not known, and
//! xterm's default is no answer for another terminal; a dynamic colour it
//! has not set is asked of the host ([`crate::Event::ColorQuery`], as
//! without the palette), which knows its terminal's; a special colour it
//! has not set is not answered either.
use crate::parser::{Event, Sink};
use crate::{Reply, Rgb};

/// How many special colours xterm has (ctlseqs, OSC 5): bold, underline,
/// blink, reverse and italic.
const SPECIALS: u16 = 5;
/// The first OSC 4 colour number past the palette: the special colours.
const PAST_PALETTE: u16 = 256;

/// The colours a program set, each `None` while it is the terminal's own,
/// and the host's colours for palette entries 0 to 15.
#[derive(Clone, Debug)]
pub(crate) struct Colours {
    palette: [Option<Rgb>; 256],
    special: [Option<Rgb>; 5],
    /// OSC 10 to 19.
    dynamic: [Option<Rgb>; 10],
    /// What the host's terminal shows for palette entries 0 to 15, the
    /// ones themes change: what a query of an entry the program has not
    /// set is answered with. No reset of the program's colours touches
    /// them.
    pub(crate) host: [Option<Rgb>; 16],
}

impl Default for Colours {
    fn default() -> Self {
        Self {
            palette: [None; 256],
            special: [None; 5],
            dynamic: [None; 10],
            host: [None; 16],
        }
    }
}

impl Colours {
    /// Palette entry `index`, if the program set it.
    pub(crate) fn palette(&self, index: u8) -> Option<Rgb> {
        self.palette.get(usize::from(index)).copied().flatten()
    }
    /// Dynamic colour `number` (10 to 19), if the program set it.
    pub(crate) fn dynamic(&self, number: u8) -> Option<Rgb> {
        let i = usize::from(number.checked_sub(10)?);
        self.dynamic.get(i).copied().flatten()
    }
    /// Whether the program set a palette entry or a dynamic colour: what
    /// a host that draws the screen needs to know of.
    pub(crate) fn changed(&self) -> bool {
        self.palette
            .iter()
            .chain(&self.dynamic)
            .any(Option::is_some)
    }
    /// The palette back to the terminal's own (OSC 104 alone, DECSTR and
    /// RIS, as xterm resets it).
    pub(crate) fn reset_palette(&mut self) {
        self.palette = [None; 256];
    }
    /// The slot of OSC 4's colour `number`: a palette entry, or a special
    /// colour past them.
    fn slot(&mut self, number: u16) -> Option<&mut Option<Rgb>> {
        match number.checked_sub(PAST_PALETTE) {
            None => self.palette.get_mut(usize::from(number)),
            Some(special) => self.special.get_mut(usize::from(special)),
        }
    }
}

/// `text` as C's `atoi` reads it, as xterm reads OSC 4's colour numbers:
/// leading white space, a sign, and the digits that follow, 0 if none; a
/// number too large for an `i32` is as large as one.
fn atoi(text: &[u8]) -> i32 {
    let mut i = 0usize;
    while text.get(i).is_some_and(|b| b" \t\n\x0b\x0c\r".contains(b)) {
        i = i.saturating_add(1);
    }
    let negative = text.get(i) == Some(&b'-');
    if matches!(text.get(i), Some(b'-' | b'+')) {
        i = i.saturating_add(1);
    }
    let mut magnitude = 0i32;
    while let Some(d) = text.get(i).filter(|b| b.is_ascii_digit()) {
        magnitude = magnitude
            .saturating_mul(10)
            .saturating_add(i32::from(d.saturating_sub(b'0')));
        i = i.saturating_add(1);
    }
    if negative {
        magnitude.saturating_neg()
    } else {
        magnitude
    }
}

/// A channel of `digits` hex digits (at most four) read as XParseColor
/// reads `rgb:`, scaled to 16 bits.
fn scaled(digits: &[u8]) -> Option<u16> {
    if digits.is_empty() || digits.len() > 4 {
        return None;
    }
    let value = u32::from_str_radix(str::from_utf8(digits).ok()?, 16).ok()?;
    let bits = u32::try_from(digits.len()).ok()?.checked_mul(4)?;
    let largest = 1u32.checked_shl(bits)?.checked_sub(1)?;
    u16::try_from(value.checked_mul(0xffff)?.checked_div(largest)?).ok()
}

impl Rgb {
    /// A colour specification, as xterm reads a program's and fux reads a
    /// terminal's answer (see the module's documentation); `None` for one
    /// fux-vt does not read.
    pub(crate) fn parse(spec: &[u8]) -> Option<Self> {
        let sixteen: [u16; 3] = if let Some(hex) = spec.strip_prefix(b"#") {
            // Three channels of 1 to 4 digits each, the digits the high bits.
            let n = hex.len() / 3;
            if n == 0 || hex.len() % 3 != 0 || n > 4 {
                return None;
            }
            let shift = u32::try_from(n).ok()?.checked_mul(4)?;
            let channel = |i: usize| -> Option<u16> {
                let digits = hex.get(i.checked_mul(n)?..i.checked_add(1)?.checked_mul(n)?)?;
                let value = u16::from_str_radix(str::from_utf8(digits).ok()?, 16).ok()?;
                value.checked_shl(16u32.checked_sub(shift)?)
            };
            [channel(0)?, channel(1)?, channel(2)?]
        } else if spec
            .get(..4)
            .is_some_and(|p| p.eq_ignore_ascii_case(b"rgb:"))
        {
            // `rgb:`, its prefix in either case.
            let mut channels = spec.get(4..)?.split(|b| *b == b'/');
            let colour = [
                scaled(channels.next()?)?,
                scaled(channels.next()?)?,
                scaled(channels.next()?)?,
            ];
            if channels.next().is_some() {
                return None;
            }
            colour
        } else {
            return None;
        };
        Some(Rgb::from(sixteen.map(|c| {
            let [high, _] = c.to_be_bytes();
            high
        })))
    }
}

/// xterm's answer: `OSC command ; index ; rgb:RRRR/GGGG/BBBB` (no index for
/// a dynamic colour), each channel's byte twice, ended as the query was.
/// Built byte by byte rather than formatted: zellij asks all 256 entries
/// as it starts, and formatting cost more than the rest of the work.
#[inline]
pub(crate) fn answer(command: u16, index: Option<u16>, colour: Rgb, bel: bool) -> Reply {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut out = Reply::default();
    out.push(b"\x1b]");
    decimal(&mut out, command);
    if let Some(index) = index {
        out.push(b";");
        decimal(&mut out, index);
    }
    out.push(b";rgb:");
    for (i, channel) in <[u8; 3]>::from(colour).into_iter().enumerate() {
        if i > 0 {
            out.push(b"/");
        }
        let high = HEX.get(usize::from(channel >> 4)).copied().unwrap_or(b'0');
        let low = HEX.get(usize::from(channel & 0xf)).copied().unwrap_or(b'0');
        out.push(&[high, low, high, low]);
    }
    out.push(if bel { b"\x07" } else { b"\x1b\\" });
    out
}

/// `n` in decimal: at most three digits, as every colour number is.
fn decimal(out: &mut Reply, n: u16) {
    let digit = |d: u16| b'0'.saturating_add(u8::try_from(d % 10).unwrap_or(0));
    if n >= 100 {
        out.push(&[digit(n / 100)]);
    }
    if n >= 10 {
        out.push(&[digit(n / 10)]);
    }
    out.push(&[digit(n)]);
}

/// Carries out the colour OSC `command` with `rest` after it, if it is
/// one, ended by BEL if `bel`; whether it was. `colours` is the screen's,
/// made when a colour is first set; `events` says whether a dynamic colour
/// the program has not set may be asked of the host.
pub(crate) fn osc(
    colours: &mut Option<Box<Colours>>,
    command: &[u8],
    rest: &[u8],
    bel: bool,
    events: bool,
    sink: &mut impl Sink,
) -> bool {
    match command {
        b"4" => pairs(colours, rest, 0, bel, sink),
        b"5" => pairs(colours, rest, PAST_PALETTE, bel, sink),
        b"104" => reset(colours, rest, 0),
        b"105" => reset(colours, rest, PAST_PALETTE),
        // OSC 10 to 19, and 110 to 119 with no parameter (with one, nothing,
        // as in xterm and Ghostty): dynamic colour `d`, 0 to 9 from 10.
        [b'1', d @ b'0'..=b'9'] => {
            dynamic(colours, d.saturating_sub(b'0'), rest, bel, events, sink)
        }
        [b'1', b'1', d @ b'0'..=b'9'] => {
            if rest.is_empty()
                && let Some(slot) = colours
                    .as_mut()
                    .and_then(|c| c.dynamic.get_mut(usize::from(d.saturating_sub(b'0'))))
            {
                *slot = None;
            }
        }
        _ => return false,
    }
    true
}

/// OSC 4 (`offset` 0) and OSC 5 (`offset` 256): pairs of a colour number
/// and a specification, `?` asking for the colour, as xterm's
/// `ChangeAnsiColorRequest` reads them: the number as `atoi` reads it, and
/// the first pair without a specification, with a number out of range or
/// a specification fux-vt cannot read ends them.
fn pairs(
    colours: &mut Option<Box<Colours>>,
    rest: &[u8],
    offset: u16,
    bel: bool,
    sink: &mut impl Sink,
) {
    let limit = PAST_PALETTE.saturating_add(SPECIALS);
    let mut params = rest.split(|b| *b == b';');
    while let (Some(number), Some(spec)) = (params.next(), params.next()) {
        let Some(n) = u16::try_from(atoi(number))
            .ok()
            .and_then(|n| n.checked_add(offset))
            .filter(|n| *n < limit)
        else {
            return;
        };
        if spec == b"?" {
            let set = colours.as_mut().and_then(|c| *c.slot(n)?);
            // The program's colour, else the host's terminal's, if it said;
            // else none is known, and none is answered.
            let host = |c: &Colours| c.host.get(usize::from(n)).copied().flatten();
            let Some(colour) = set.or_else(|| colours.as_deref().and_then(host)) else {
                continue;
            };
            // Answered in the form it was asked: OSC 5 by its own number.
            let asked = n.saturating_sub(offset);
            let command = if offset == 0 { 4 } else { 5 };
            sink.reply(answer(command, Some(asked), colour, bel).as_bytes());
            continue;
        }
        let Some(colour) = Rgb::parse(spec) else {
            return;
        };
        if let Some(slot) = colours.get_or_insert_default().slot(n) {
            *slot = Some(colour);
        }
    }
}

/// OSC 104 (`offset` 0) and OSC 105 (`offset` 256): the colours listed back
/// to the terminal's own, read as xterm's `ResetAnsiColorRequest` reads
/// them, up to the first that is no number or is followed by anything but
/// `;`. With none listed, OSC 104 resets every palette entry and OSC 105
/// every special colour, as ctlseqs says (xterm 411 resets no special
/// colour).
fn reset(colours: &mut Option<Box<Colours>>, rest: &[u8], offset: u16) {
    let Some(c) = colours else {
        return;
    };
    if rest.is_empty() {
        if offset == 0 {
            c.reset_palette();
        } else {
            c.special = [None; 5];
        }
        return;
    }
    for item in rest.split(|b| *b == b';') {
        // `strtol`: white space, a sign and digits, and nothing after.
        let digits = item.trim_ascii_start();
        let digits = digits.strip_prefix(b"+").unwrap_or(digits);
        if digits.is_empty() || !digits.iter().all(u8::is_ascii_digit) {
            return;
        }
        let n = atoi(digits);
        if let Some(slot) = u16::try_from(n)
            .ok()
            .and_then(|n| n.checked_add(offset))
            .and_then(|n| c.slot(n))
        {
            *slot = None;
        }
    }
}

/// OSC 10 to 19: each parameter the next dynamic colour from `first`'s
/// (0 to 9, numbered from 10),
/// `?` asking for it, as xterm's `ChangeColorsRequest` reads them; a
/// specification fux-vt cannot read leaves its colour and goes on to the
/// next, as in xterm. A colour the program set is answered here; one it has
/// not is asked of the host, with `events`.
pub(crate) fn dynamic(
    colours: &mut Option<Box<Colours>>,
    first: u8,
    rest: &[u8],
    bel: bool,
    events: bool,
    sink: &mut impl Sink,
) {
    for (i, parameter) in (first..10).zip(rest.split(|b| *b == b';')) {
        let number = i.saturating_add(10);
        if parameter == b"?" {
            match colours.as_ref().and_then(|c| c.dynamic(number)) {
                Some(colour) => sink.reply(answer(u16::from(number), None, colour, bel).as_bytes()),
                None if events => sink.event(Event::ColorQuery { number, bel }),
                None => {}
            }
        } else if let Some(colour) = Rgb::parse(parameter)
            && let Some(slot) = colours
                .get_or_insert_default()
                .dynamic
                .get_mut(usize::from(i))
        {
            *slot = Some(colour);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::atoi;
    use crate::Rgb;

    #[test]
    fn specifications_are_read_as_xterm_reads_them() {
        let parse = |spec: &[u8]| Rgb::parse(spec).map(<[u8; 3]>::from);
        // XParseColor's rgb: scales each channel to 16 bits; # forms give
        // the high bits; the high byte is kept (xterm 411, `OSC 4 ; n ; ?`
        // after each).
        assert_eq!(parse(b"rgb:f0f0/f0f0/f0f0"), Some([0xf0, 0xf0, 0xf0]));
        assert_eq!(parse(b"rgb:ff00/8/80"), Some([0xff, 0x88, 0x80]));
        assert_eq!(parse(b"rgb:1/22/333"), Some([0x11, 0x22, 0x33]));
        assert_eq!(parse(b"RGB:ff/00/00"), Some([0xff, 0, 0]));
        assert_eq!(parse(b"#fff"), Some([0xf0, 0xf0, 0xf0]));
        assert_eq!(parse(b"#888"), Some([0x80, 0x80, 0x80]));
        assert_eq!(parse(b"#f0f0f0"), Some([0xf0, 0xf0, 0xf0]));
        assert_eq!(parse(b"#f00f00f00"), Some([0xf0, 0xf0, 0xf0]));
        assert_eq!(parse(b"#aaaabbbbcccc"), Some([0xaa, 0xbb, 0xcc]));
        for bad in [
            &b""[..],
            b"#",
            b"#f",
            b"#ffff",
            b"#fffffffffffffff",
            b"#ggg",
            b"  #102030",
            b"rgb:",
            b"rgb:1/2",
            b"rgb:1/2/3/4",
            b"rgb:12345/0/0",
            b"rgb:/0/0",
            b"rgbi:1/1/1",
            b"CIEXYZ:1/1/1",
            b"red",
            b"?",
        ] {
            assert_eq!(parse(bad), None, "{:?}", String::from_utf8_lossy(bad));
        }
    }

    #[test]
    fn colour_numbers_are_read_as_atoi_reads_them() {
        assert_eq!(atoi(b"12"), 12);
        assert_eq!(atoi(b"1x"), 1);
        assert_eq!(atoi(b"x"), 0);
        assert_eq!(atoi(b" 3"), 3);
        assert_eq!(atoi(b"+5"), 5);
        assert_eq!(atoi(b"-1"), -1);
        assert_eq!(atoi(b"99999999999"), i32::MAX);
    }
}
