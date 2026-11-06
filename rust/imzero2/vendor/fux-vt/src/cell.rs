//! A cell's text, halves and [`Attributes`], read through a [`CellRef`].
//! Cells are stored one way only (`compact.rs`), in rows and [`Cells`] alike.

use crate::Error;
use crate::compact::{Compact, Line, Text};

/// A colour: red, green and blue, 8 bits each, as xterm keeps every colour.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Rgb {
    /// Red.
    pub r: u8,
    /// Green.
    pub g: u8,
    /// Blue.
    pub b: u8,
}

impl From<[u8; 3]> for Rgb {
    fn from([r, g, b]: [u8; 3]) -> Self {
        Self { r, g, b }
    }
}

impl From<Rgb> for [u8; 3] {
    fn from(Rgb { r, g, b }: Rgb) -> Self {
        [r, g, b]
    }
}

/// A default, indexed, or true-colour terminal colour.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Color {
    /// The terminal's default colour, foreground or background as it is used.
    #[default]
    Default,
    /// An indexed colour: 0 to 7 the standard colours, 8 to 15 their bright
    /// forms, 16 to 255 xterm's 256-colour palette.
    Idx(u8),
    /// A direct colour.
    Rgb(Rgb),
}

/// How a cell blinks: SGR 5 (slow) and 6 (rapid) replace one another, and
/// SGR 25 stops either.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Blink {
    /// Not blinking.
    #[default]
    None,
    /// Blinking slowly (SGR 5).
    Slow,
    /// Blinking rapidly (SGR 6).
    Rapid,
}

/// How a cell is underlined: SGR 4 and 24, 21 (doubly underlined, ECMA-48
/// 8.3.117), and kitty's styles `4:0` to `4:5`
/// (`references/modern/kitty_underlines.html`), numbered as they are there.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
#[repr(u16)]
pub enum UnderlineStyle {
    /// Not underlined (SGR 24, `4:0`).
    #[default]
    None,
    /// A straight underline (SGR 4, `4:1`).
    Single,
    /// A double underline (SGR 21, `4:2`).
    Double,
    /// A curly underline (`4:3`): neovim's diagnostics.
    Curly,
    /// A dotted underline (`4:4`).
    Dotted,
    /// A dashed underline (`4:5`).
    Dashed,
}

impl UnderlineStyle {
    /// The style kitty's `4:n` names; `None` past 5.
    pub const fn from_number(n: u16) -> Option<Self> {
        Some(match n {
            0 => Self::None,
            1 => Self::Single,
            2 => Self::Double,
            3 => Self::Curly,
            4 => Self::Dotted,
            5 => Self::Dashed,
            _ => return None,
        })
    }
    /// Its number in kitty's `4:n`, 0 to 5.
    pub const fn number(self) -> u16 {
        self as u16
    }
}

/// How [`Attributes::write_sgr`] writes attributes, for different readers.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SgrForm {
    /// As xterm reports the pen to DECRQSS (`xtermFormatSGR`), in its order
    /// and colour forms, with what xterm lacks (rapid blink, `4:n`, SGR 58)
    /// in the same places. A curly underline alone is `0;4:3`, which neovim
    /// looks for to learn the terminal draws styles.
    Report,
    /// To paint on a terminal: colours in their semicolon forms.
    Paint {
        /// Whether the terminal draws underline styles (kitty's `4:n`).
        /// If not, they go as a plain underline: a terminal that does not
        /// know `4:3` draws none or reads it as underline and italic, and
        /// 21 may read as bold off.
        styles: bool,
    },
}

fn write_numbers(out: &mut Vec<u8>, numbers: impl IntoIterator<Item = (u16, &'static str)>) {
    use std::io::Write;
    for (number, _) in numbers {
        let _ = write!(out, ";{number}");
    }
}

/// Writes colour `c`: `base` is 30, 40 or 50 (underline), so every sum
/// stays under 255. A colon form has ITU-T T.416's empty colour-space slot.
fn write_colour(out: &mut Vec<u8>, c: Color, base: u8, colon: bool) {
    use std::io::Write;
    let short = base < 50;
    let long = base.saturating_add(8);
    let _ = match c {
        Color::Default => Ok(()),
        Color::Idx(n) if short && n < 8 => write!(out, ";{}", base.saturating_add(n)),
        Color::Idx(n) if short && n < 16 => {
            write!(out, ";{}", base.saturating_add(52).saturating_add(n))
        }
        Color::Idx(n) if colon => write!(out, ";{long}:5:{n}"),
        Color::Idx(n) => write!(out, ";{long};5;{n}"),
        Color::Rgb(Rgb { r, g, b }) if colon => write!(out, ";{long}:2::{r}:{g}:{b}"),
        Color::Rgb(Rgb { r, g, b }) => write!(out, ";{long};2;{r};{g};{b}"),
    };
}

/// Every ASCII character, in order: the text of a cell holding one.
pub(crate) const ASCII: &str = "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f\x20\x21\x22\x23\x24\x25\x26\x27\x28\x29\x2a\x2b\x2c\x2d\x2e\x2f\x30\x31\x32\x33\x34\x35\x36\x37\x38\x39\x3a\x3b\x3c\x3d\x3e\x3f\x40\x41\x42\x43\x44\x45\x46\x47\x48\x49\x4a\x4b\x4c\x4d\x4e\x4f\x50\x51\x52\x53\x54\x55\x56\x57\x58\x59\x5a\x5b\x5c\x5d\x5e\x5f\x60\x61\x62\x63\x64\x65\x66\x67\x68\x69\x6a\x6b\x6c\x6d\x6e\x6f\x70\x71\x72\x73\x74\x75\x76\x77\x78\x79\x7a\x7b\x7c\x7d\x7e\x7f";

/// A colour as stored: a tag (0 default, 1 indexed, 2 RGB) and its bytes,
/// all always set, so attributes compare, copy and clear as plain bytes.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
struct Packed([u8; 4]);

impl Packed {
    const fn new(color: Color) -> Self {
        Self(match color {
            Color::Default => [0; 4],
            Color::Idx(i) => [1, i, 0, 0],
            Color::Rgb(Rgb { r, g, b }) => [2, r, g, b],
        })
    }
    const fn get(self) -> Color {
        match self.0 {
            [1, i, _, _] => Color::Idx(i),
            [2, r, g, b] => Color::Rgb(Rgb { r, g, b }),
            _ => Color::Default,
        }
    }
}

/// Attributes are independent of glyph storage. An erase fills cells with
/// the pen's colours alone (`erased`).
#[derive(Clone, Copy, Default, PartialEq, Eq)]
pub struct Attributes {
    foreground: Packed,
    background: Packed,
    underline_color: Packed,
    pub(crate) flags: u16,
}

/// Hashed as `bits`, so different attributes write different words and a
/// keyed hasher gives a program no way to choose colliding styles
/// (`style.rs`; `THREATS.md` B12).
impl std::hash::Hash for Attributes {
    fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
        let (a, b) = self.bits();
        state.write_u128(u128::from(a) << 64 | u128::from(b));
    }
}

impl std::fmt::Debug for Attributes {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Attributes")
            .field("foreground", &self.foreground())
            .field("background", &self.background())
            .field("underline_color", &self.underline_color())
            .field("flags", &self.flags)
            .finish()
    }
}

/// Expands each `get with BIT on off;` to a getter and builder of that flag
/// bit, and `Attributes::FLAGS`, the rows in their order.
macro_rules! flags {
    ($($get:ident $with:ident $bit:ident $on:literal $off:literal;)*) => {
        /// Each flag's bit, SGR set and clear numbers, and name, in the
        /// order xterm reports them.
        pub(crate) const FLAGS: [(u16, u16, u16, &'static str); [$($on),*].len()] =
            [$((Self::$bit, $on, $off, stringify!($get)),)*];
        $(
            #[doc = concat!("Whether ", stringify!($get), " (SGR ", $on, ") is on.")]
            pub fn $get(self) -> bool {
                self.flags & Self::$bit != 0
            }
            #[doc = concat!("These attributes with ", stringify!($get), " on or off.")]
            #[must_use]
            pub const fn $with(self, on: bool) -> Self {
                self.with_flag(Self::$bit, on)
            }
        )*
    };
}

impl Attributes {
    // The bits of `flags`: one for each style, but the underline, whose
    // style's number (`UnderlineStyle`, 0 for none) takes three bits from
    // `UNDERLINE_SHIFT`, and `GUARDED`.
    pub(crate) const BOLD: u16 = 1;
    pub(crate) const DIM: u16 = 2;
    pub(crate) const ITALIC: u16 = 4;
    /// A glyph in SPA's guarded area (ECMA-48 8.3.140), which ED, EL and
    /// ECH leave: kept in its style, as a cell's word has no bit to spare,
    /// and never read out (`Styles::get` drops it). No pen has it.
    pub(crate) const GUARDED: u16 = 8;
    pub(crate) const INVERSE: u16 = 16;
    pub(crate) const SLOW_BLINK: u16 = 32;
    pub(crate) const RAPID_BLINK: u16 = 64;
    pub(crate) const HIDDEN: u16 = 128;
    pub(crate) const STRIKEOUT: u16 = 256;
    pub(crate) const BLINK: u16 = Self::SLOW_BLINK | Self::RAPID_BLINK;
    const UNDERLINE_SHIFT: u32 = 9;
    pub(crate) const UNDERLINE: u16 = 0b111 << Self::UNDERLINE_SHIFT;

    /// Plain attributes with the given colours; add styles with `with_*`.
    pub const fn new(foreground: Color, background: Color) -> Self {
        Self {
            foreground: Packed::new(foreground),
            background: Packed::new(background),
            underline_color: Packed::new(Color::Default),
            flags: 0,
        }
    }
    /// The foreground colour.
    pub const fn foreground(self) -> Color {
        self.foreground.get()
    }
    /// The renditions an indexed style's own number holds
    /// (`inline_style`): every flag but rapid blink, and a single underline.
    const INDEXED: u16 = Self::BOLD
        | Self::DIM
        | Self::ITALIC
        | Self::INVERSE
        | Self::SLOW_BLINK
        | Self::HIDDEN
        | Self::STRIKEOUT
        | 1 << Self::UNDERLINE_SHIFT;
    /// The renditions a direct style's own number holds: bold and italic,
    /// as syntax highlighting draws keywords and comments.
    const DIRECT: u16 = Self::BOLD | Self::ITALIC;
    /// These attributes as a 27-bit style that is its own number
    /// (`style.rs`), if they are one of the two kinds programs mostly print
    /// in. Indexed (`INDEXED` renditions, default or indexed colours): fg
    /// and bg 9 bits each (0 default, index + 1), then the rendition, 8
    /// bits. Direct (`DIRECT` renditions, direct fg, default bg): bit 26
    /// set, the fg's RGB below it, then bold and italic.
    #[inline]
    pub(crate) fn inline_style(self) -> Option<u32> {
        // A colour is [0, 0, 0, 0] (the default), [1, index, 0, 0] or
        // [2, red, green, blue]: as a little-endian word, its tag in the
        // low byte, the rest above.
        let (fg, bg) = (
            u32::from_le_bytes(self.foreground.0),
            u32::from_le_bytes(self.background.0),
        );
        if self.underline_color.0 != [0; 4] {
            return None;
        }
        let flags = u32::from(self.flags);
        if (fg | bg) & 0xffff_00fe == 0 && self.flags & !Self::INDEXED == 0 {
            let colour = |w: u32| ((w >> 8).wrapping_add(1)) & (w & 1).wrapping_neg();
            let rendition =
                (flags & 0b111) | ((flags >> 1) & 0b1_1000) | ((flags >> 2) & 0b1110_0000);
            return Some(colour(fg) | colour(bg) << 9 | rendition << 18);
        }
        if fg & 0xff == 2 && bg == 0 && self.flags & !Self::DIRECT == 0 {
            let bold = flags & u32::from(Self::BOLD);
            let italic = (flags & u32::from(Self::ITALIC)) >> 2;
            return Some(1 << 26 | fg >> 8 | bold << 24 | italic << 25);
        }
        None
    }
    /// The inverse of `inline_style`; out of line, as readers look a style
    /// up once per run and keep their loop small.
    #[inline(never)]
    pub(crate) fn from_inline_style(code: u32) -> Self {
        fn colour(c: u32) -> Packed {
            match c.checked_sub(1).and_then(|i| u8::try_from(i).ok()) {
                Some(i) => Packed([1, i, 0, 0]),
                None => Packed([0; 4]),
            }
        }
        if code & 1 << 26 != 0 {
            let [r, g, b, _] = code.to_le_bytes();
            let flags = (code >> 24) & 1 | ((code >> 25) & 1) << 2;
            return Self {
                foreground: Packed([2, r, g, b]),
                background: Packed([0; 4]),
                underline_color: Packed([0; 4]),
                flags: u16::try_from(flags).unwrap_or(0),
            };
        }
        let rendition = (code >> 18) & 0xff;
        let flags =
            (rendition & 0b111) | ((rendition & 0b1_1000) << 1) | ((rendition & 0b1110_0000) << 2);
        Self {
            foreground: colour(code & 0x1ff),
            background: colour((code >> 9) & 0x1ff),
            underline_color: Packed([0; 4]),
            flags: u16::try_from(flags).unwrap_or(0),
        }
    }
    /// The attributes as two words, which differ where the attributes do:
    /// what they hash as.
    pub(crate) fn bits(self) -> (u64, u64) {
        let word = |p: Packed| u64::from(u32::from_le_bytes(p.0));
        (
            word(self.foreground) | word(self.background) << 32,
            word(self.underline_color) | u64::from(self.flags) << 32,
        )
    }
    pub(crate) const fn with_guard(self, on: bool) -> Self {
        self.with_flag(Self::GUARDED, on)
    }
    /// What an erase, a scroll or an insertion fills cells with while these
    /// attributes are the pen: its colours alone, as xterm fills them (the
    /// `bce` terminfo capability, background colour erase).
    pub(crate) const fn erased(self) -> Self {
        Self {
            underline_color: Packed([0; 4]),
            flags: 0,
            ..self
        }
    }
    /// The background colour.
    pub const fn background(self) -> Color {
        self.background.get()
    }
    /// SGR 58; `Default` draws underlines in the foreground colour.
    pub const fn underline_color(self) -> Color {
        self.underline_color.get()
    }
    /// These attributes with `color` as the foreground.
    #[must_use]
    pub const fn with_foreground(mut self, color: Color) -> Self {
        self.foreground = Packed::new(color);
        self
    }
    /// These attributes with `color` as the background.
    #[must_use]
    pub const fn with_background(mut self, color: Color) -> Self {
        self.background = Packed::new(color);
        self
    }
    const fn with_flag(mut self, bit: u16, on: bool) -> Self {
        self.flags = if on {
            self.flags | bit
        } else {
            self.flags & !bit
        };
        self
    }
    flags! {
        bold with_bold BOLD 1 22;
        inverse with_inverse INVERSE 7 27;
        hidden with_hidden HIDDEN 8 28;
        dim with_dim DIM 2 22;
        italic with_italic ITALIC 3 23;
        strikeout with_strikeout STRIKEOUT 9 29;
    }
    /// The SGR number and name of each flag that is on, in xterm's order.
    pub fn sgr_flags(self) -> impl Iterator<Item = (u16, &'static str)> {
        Self::FLAGS
            .into_iter()
            .filter(move |(bit, ..)| self.flags & bit != 0)
            .map(|(_, on, _, name)| (on, name))
    }
    /// Writes the SGR parameters that set these attributes after a reset
    /// (`0`, which the caller writes), each after its `;`: `;1;4:3;58:5:5`
    /// (`SgrForm::Report`), `;1;4;58:5:5` (`SgrForm::Paint` without styles).
    pub fn write_sgr(self, out: &mut Vec<u8>, form: SgrForm) {
        let mut flags = self.sgr_flags();
        let colon = match form {
            SgrForm::Report => {
                write_numbers(out, flags.by_ref().take(usize::from(self.bold())));
                self.write_underline_and_blink(out, form);
                write_numbers(out, flags);
                if self.underline_style() == UnderlineStyle::Double {
                    out.extend_from_slice(b";21");
                }
                true
            }
            SgrForm::Paint { .. } => {
                write_numbers(out, flags);
                self.write_underline_and_blink(out, form);
                // The underline colour always in ITU-T T.416's colon form
                // (§13.1.8): a terminal without SGR 58 skips the whole
                // parameter, where the semicolon form's numbers would read
                // as attributes (`58;2;…` would be dim). xterm skips SGR
                // parameters with subparameters other than 38's and 48's.
                write_colour(out, self.underline_color(), 50, true);
                false
            }
        };
        write_colour(out, self.foreground(), 30, colon);
        write_colour(out, self.background(), 40, colon);
        match form {
            SgrForm::Report => write_colour(out, self.underline_color(), 50, true),
            SgrForm::Paint { .. } => {}
        }
    }
    /// Writes the SGR sequence (`CSI … m`) that takes a terminal's pen
    /// from `from` to these attributes, painted as [`SgrForm::Paint`]
    /// with `styles` paints them: the shorter of the change alone and a
    /// reset then these ([`Attributes::write_sgr`]), and nothing if they
    /// are the same. Bold and dim go off together (22): one that stays
    /// is set again.
    pub fn write_sgr_change(self, from: Self, out: &mut Vec<u8>, styles: bool) {
        // SPA's guard is no SGR: it neither shows nor goes in one.
        let shown = |a: Self| Self {
            flags: a.flags & !Self::GUARDED,
            ..a
        };
        let (to, from) = (shown(self), shown(from));
        if to == from {
            return;
        }
        let form = SgrForm::Paint { styles };
        let mut change = Vec::new();
        let (gone, mut set) = (from.flags & !to.flags, to.flags & !from.flags);
        let mut ended = false;
        for (_, _, off, _) in Self::FLAGS.into_iter().filter(|(bit, ..)| gone & bit != 0) {
            // 22 ends bold and dim both, once.
            if off == 22 && std::mem::replace(&mut ended, true) {
                continue;
            }
            if off == 22 {
                set |= to.flags & (Self::BOLD | Self::DIM);
            }
            write_numbers(&mut change, [(off, "")]);
        }
        let on = Self::FLAGS.into_iter().filter(|(bit, ..)| set & bit != 0);
        write_numbers(&mut change, on.map(|(_, on, _, name)| (on, name)));
        let (underline, blink) = (to.underline_style(), to.blink());
        if underline == UnderlineStyle::None && from.underline_style() != underline {
            change.extend_from_slice(b";24");
        } else if underline != from.underline_style() {
            let underlined = Self::default().with_underline_style(underline);
            underlined.write_underline_and_blink(&mut change, form);
        }
        if blink == Blink::None && from.blink() != blink {
            change.extend_from_slice(b";25");
        } else if blink != from.blink() {
            Self::default()
                .with_blink(blink)
                .write_underline_and_blink(&mut change, form);
        }
        let colours = [
            (to.foreground(), from.foreground(), 30u8, false),
            (to.background(), from.background(), 40, false),
            (to.underline_color(), from.underline_color(), 50, true),
        ];
        for (to, _, base, colon) in colours.into_iter().filter(|(to, was, ..)| to != was) {
            if to == Color::Default {
                write_numbers(&mut change, [(u16::from(base.saturating_add(9)), "")]);
            } else {
                write_colour(&mut change, to, base, colon);
            }
        }
        let mut full = b";0".to_vec();
        to.write_sgr(&mut full, form);
        let shorter = if change.len() <= full.len() {
            change
        } else {
            full
        };
        out.extend_from_slice(b"\x1b[");
        out.extend_from_slice(shorter.get(1..).unwrap_or_default());
        out.push(b'm');
    }
    fn write_underline_and_blink(self, out: &mut Vec<u8>, form: SgrForm) {
        let style = self.underline_style();
        match style {
            UnderlineStyle::None => {}
            UnderlineStyle::Single => out.extend_from_slice(b";4"),
            // 21, after the other flags.
            UnderlineStyle::Double if form == SgrForm::Report => {}
            UnderlineStyle::Double
            | UnderlineStyle::Curly
            | UnderlineStyle::Dotted
            | UnderlineStyle::Dashed => match form {
                SgrForm::Report | SgrForm::Paint { styles: true } => {
                    let _ = std::io::Write::write_fmt(out, format_args!(";4:{}", style.number()));
                }
                SgrForm::Paint { styles: false } => out.extend_from_slice(b";4"),
            },
        }
        match self.blink() {
            Blink::Slow => out.extend_from_slice(b";5"),
            Blink::Rapid => out.extend_from_slice(b";6"),
            Blink::None => {}
        }
    }
    /// These attributes with a single underline (SGR 4), or none.
    #[must_use]
    pub const fn with_underline(self, on: bool) -> Self {
        self.with_underline_style(if on {
            UnderlineStyle::Single
        } else {
            UnderlineStyle::None
        })
    }
    /// These attributes underlined in `style` (SGR 4, 21, 24 and `4:n`).
    #[must_use]
    pub const fn with_underline_style(mut self, style: UnderlineStyle) -> Self {
        self.flags = (self.flags & !Self::UNDERLINE) | (style.number() << Self::UNDERLINE_SHIFT);
        self
    }
    /// These attributes blinking as `blink` says (SGR 5, 6 and 25).
    #[must_use]
    pub const fn with_blink(self, blink: Blink) -> Self {
        let bit = match blink {
            Blink::None => 0,
            Blink::Slow => Self::SLOW_BLINK,
            Blink::Rapid => Self::RAPID_BLINK,
        };
        self.with_flag(Self::BLINK, false).with_flag(bit, true)
    }
    /// These attributes with `color` as the underline colour (SGR 58).
    #[must_use]
    pub const fn with_underline_color(mut self, color: Color) -> Self {
        self.underline_color = Packed::new(color);
        self
    }
    /// Whether the text is underlined, in any style.
    pub fn underline(self) -> bool {
        self.flags & Self::UNDERLINE != 0
    }
    /// How the text is underlined.
    pub fn underline_style(self) -> UnderlineStyle {
        UnderlineStyle::from_number((self.flags & Self::UNDERLINE) >> Self::UNDERLINE_SHIFT)
            .unwrap_or(UnderlineStyle::None)
    }
    /// How the text blinks.
    pub fn blink(self) -> Blink {
        // Both bits, which `with_blink` never sets, read as slow.
        match self.flags & Self::BLINK {
            0 => Blink::None,
            Self::RAPID_BLINK => Blink::Rapid,
            _ => Blink::Slow,
        }
    }
}

/// The most UTF-8 bytes of one grapheme cluster a cell keeps. Characters
/// past it still belong to the cluster, and its cell, but are dropped: a
/// cluster is never split, so what follows it is always where a program
/// laid out with UAX #29 expects. 128 is the Unicode stream-safe limit of a
/// starter and 30 combining marks, and holds every emoji sequence.
pub(crate) const CLUSTER_CAPACITY: usize = 128;

/// The longest start of `text` of at most `max` bytes that ends on a char
/// boundary.
pub(crate) fn floor(text: &str, max: usize) -> &str {
    let mut end = text.len().min(max);
    while !text.is_char_boundary(end) {
        end = end.saturating_sub(1);
    }
    text.get(..end).unwrap_or("")
}

/// A cell as it is on the screen: its halves, attributes and text, which it
/// borrows from wherever the cell keeps it.
#[derive(Clone, Copy, Default)]
pub struct CellRef<'a> {
    text: &'a str,
    attributes: Attributes,
    /// `WIDE` and `CONTINUATION`.
    halves: u8,
}

/// Expands each `name of Type, "doc";` to `name(&self)`, returning `of()`.
impl<'a> CellRef<'a> {
    /// The second half of a wide glyph; and its first.
    pub(crate) const CONTINUATION: u8 = 0b0100_0000;
    pub(crate) const WIDE: u8 = 0b1000_0000;
    pub(crate) const HALVES: u8 = Self::WIDE | Self::CONTINUATION;

    /// A cell holding `text`, two columns wide if `wide`, for a host to
    /// store in [`Cells`]. Text holds no NUL, as a screen's never does.
    pub fn new(text: &'a str, wide: bool, attributes: Attributes) -> Self {
        let halves = if wide { Self::WIDE } else { 0 };
        Self::of(text, attributes, halves)
    }
    /// The second half of a wide glyph: empty, default attributes.
    pub fn wide_continuation() -> Self {
        Self::of("", Attributes::default(), Self::CONTINUATION)
    }
    #[inline]
    pub(crate) fn of(text: &'a str, attributes: Attributes, halves: u8) -> Self {
        Self {
            text,
            attributes,
            halves: halves & Self::HALVES,
        }
    }
    pub(crate) fn halves(&self) -> u8 {
        self.halves
    }
    /// The cell's grapheme cluster; empty for a blank cell or the second
    /// half of a wide glyph.
    pub fn contents(&self) -> &'a str {
        self.text
    }
    /// Its text as a reader sees it: a space for a blank cell, or one
    /// holding a picture's [`PLACEHOLDER`](crate::PLACEHOLDER).
    pub fn shown(&self) -> &'a str {
        match self.text {
            "" | crate::PLACEHOLDER => " ",
            text => text,
        }
    }
    /// Whether the cell holds a glyph two columns wide.
    pub fn is_wide(&self) -> bool {
        self.halves & Self::WIDE != 0
    }
    /// Whether the cell is the second half of the wide glyph before it.
    pub fn is_wide_continuation(&self) -> bool {
        self.halves & Self::CONTINUATION != 0
    }
    /// The cell's colours and rendition.
    pub fn attributes(&self) -> Attributes {
        self.attributes
    }
}

/// Equal if they look the same, wherever each keeps its text.
impl PartialEq for CellRef<'_> {
    fn eq(&self, other: &Self) -> bool {
        self.halves == other.halves
            && self.attributes == other.attributes
            && self.contents() == other.contents()
    }
}
impl Eq for CellRef<'_> {}

impl std::fmt::Debug for CellRef<'_> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("CellRef")
            .field("contents", &self.contents())
            .field("wide", &self.is_wide())
            .field("continuation", &self.is_wide_continuation())
            .field("attributes", &self.attributes)
            .finish()
    }
}

/// An owned run of cells, for a host that stores screen contents. It keeps
/// cells as a row does (`compact.rs`), attributes in place of a style, with
/// the same text budget: 32 bytes a cell of long clusters, and one more.
#[derive(Clone, Debug, Default)]
pub struct Cells {
    cells: Vec<Stored>,
    text: Text,
}

/// A cell of [`Cells`]: its style number unused (always 0).
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
struct Stored {
    cell: Compact,
    attributes: Attributes,
}

impl AsRef<Compact> for Stored {
    fn as_ref(&self) -> &Compact {
        &self.cell
    }
}
impl AsMut<Compact> for Stored {
    fn as_mut(&mut self) -> &mut Compact {
        &mut self.cell
    }
}

impl Stored {
    #[inline]
    fn read<'a>(&'a self, text: &'a Text) -> CellRef<'a> {
        self.cell.read_as(text, self.attributes)
    }
    /// For cells either of which keeps its text in its row's.
    #[cold]
    fn read_eq(a: &Stored, text: &Text, b: &Stored, b_text: &Text) -> bool {
        a.read(text) == b.read(b_text)
    }
}

impl Cells {
    /// `len` blank cells; `Error::Capacity` past `MAX_CELLS` (64 Mi, as a
    /// screen holds at most) or if there is no memory for them
    /// (`THREATS.md` B39).
    pub fn new(len: usize) -> Result<Self, Error> {
        let mut cells = Self::default();
        cells.room_for(len)?;
        cells.cells.resize(len, Stored::default());
        Ok(cells)
    }
    fn room_for(&mut self, len: usize) -> Result<(), Error> {
        if len > crate::grid::MAX_CELLS {
            return Err(Error::Capacity);
        }
        crate::reserve(&mut self.cells, len)
    }
    /// How many cells there are.
    pub fn len(&self) -> usize {
        self.cells.len()
    }
    /// Whether there are no cells.
    pub fn is_empty(&self) -> bool {
        self.cells.is_empty()
    }
    /// The cell at `i`.
    pub fn get(&self, i: usize) -> Option<CellRef<'_>> {
        Some(self.cells.get(i)?.read(&self.text))
    }
    /// The cells in `range`, clipped to those there are.
    pub fn range(
        &self,
        range: std::ops::Range<usize>,
    ) -> impl DoubleEndedIterator<Item = CellRef<'_>> + ExactSizeIterator + Clone {
        let text = &self.text;
        let end = range.end.min(self.cells.len());
        let start = range.start.min(end);
        let cells = self.cells.get(start..end).unwrap_or_default();
        cells.iter().map(move |cell| cell.read(text))
    }
    /// `self.range(range).eq(other.range(range))`, faster: cells are held
    /// one way only, so two with inline text are equal if their bytes are.
    pub fn range_eq(&self, other: &Cells, range: std::ops::Range<usize>) -> bool {
        let (Some(mine), Some(theirs)) = (
            self.cells.get(range.clone()),
            other.cells.get(range.clone()),
        ) else {
            return self.range(range.clone()).eq(other.range(range));
        };
        mine.iter().zip(theirs).all(|(a, b)| {
            if !a.cell.is_spilled() && !b.cell.is_spilled() {
                a == b
            } else {
                Stored::read_eq(a, &self.text, b, &other.text)
            }
        })
    }
    /// Sets cell `i` to a copy of `cell`, from wherever it keeps its text.
    /// A cluster longer than [`CLUSTER_CAPACITY`] is cut there, and one the
    /// cells have no room left for to its first 17 bytes; whether the text
    /// was kept whole.
    pub fn set(&mut self, i: usize, cell: CellRef<'_>) -> bool {
        let Some(slot) = self.cells.get_mut(i) else {
            return false;
        };
        slot.attributes = cell.attributes;
        let shape = Compact::shaped(cell.halves());
        if let Some(inline) = shape.holding(cell.text.as_bytes()) {
            slot.cell = inline;
            return true;
        }
        self.line().set(i, shape, cell.text)
    }
    /// Gives cell `i` new attributes, keeping its text.
    pub fn set_attributes(&mut self, i: usize, attributes: Attributes) {
        if let Some(slot) = self.cells.get_mut(i) {
            slot.attributes = attributes;
        }
    }
    /// Sets the cells in `range`, clipped to those there are, to `cell`.
    pub fn fill(&mut self, range: std::ops::Range<usize>, cell: CellRef<'_>) {
        let end = range.end.min(self.cells.len());
        let start = range.start.min(end);
        if start == 0 && end == self.cells.len() {
            self.text.release();
        }
        let inline = Compact::shaped(cell.halves()).holding(cell.text.as_bytes());
        match (inline, self.cells.get_mut(start..end)) {
            (Some(inline), Some(run)) => run.fill(Stored {
                cell: inline,
                attributes: cell.attributes,
            }),
            _ => {
                for i in start..end {
                    self.set(i, cell);
                }
            }
        }
    }
    /// Replaces the run with `len` blank cells, reusing its allocation.
    /// Refusal leaves it unchanged; success drops all spilled text without
    /// allocating to preserve it, even when the run shrinks.
    pub fn reset(&mut self, len: usize) -> Result<(), Error> {
        self.room_for(len)?;
        self.cells.clear();
        self.text = Text::default();
        self.cells.resize(len, Stored::default());
        Ok(())
    }
    fn line(&mut self) -> Line<'_, Stored> {
        Line {
            cells: &mut self.cells,
            text: &mut self.text,
        }
    }
}

/// Equal if every cell looks the same (see [`CellRef`]'s `==`).
impl PartialEq for Cells {
    fn eq(&self, other: &Self) -> bool {
        self.len() == other.len() && self.range_eq(other, 0..self.len())
    }
}
impl Eq for Cells {}

#[cfg(test)]
mod tests;
