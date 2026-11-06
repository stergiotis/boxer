use crate::compact::{BLANK, Compact, PROTECTED};
use crate::keys::KITTY_FLAGS_KEPT;
use crate::keys::encode::OtherKeysLevel;
use crate::link::Held;
use crate::mode::{Kind, Mode, ModeStatus, Modes, RESET_FOR_GOOD, SET_FOR_GOOD, Switch};
use crate::parser::Chars;
use crate::unicode::{Cluster, Properties};
use crate::{
    Attributes, Blink, Color, Error, Feature, Hyperlink, Identity, LineRendition, Mark, Options,
    Reply, Rgb, RowId, Rows, SgrForm, UnderlineStyle, Window,
    geometry::{Size, Span},
    grid::{Cursor, Erase, Grid, Scroll, put},
    palette::Colours,
    parser::Parameters,
};
use std::{collections::VecDeque, mem, ops::Range};

/// The blank style (`Screen::blank_style`) not asked for since the pen
/// changed. The pen's own style is found at once (`pen_changed`).
const UNKNOWN: u32 = u32::MAX;
/// A pen style that is no number of its own: the grid's table has it.
const TABLED: u32 = u32::MAX - 1;

/// The mouse reporting a program asked for, the latest set winning
/// (`CSI ? 9 / 1000 / 1001 / 1002 / 1003 h`); `Screen::encode_mouse` reads it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum MouseProtocolMode {
    /// No reporting.
    #[default]
    None,
    /// Mode 9, X10: button presses.
    Press,
    /// Mode 1000: presses and releases.
    PressRelease,
    /// Mode 1001, highlight tracking (ctlseqs): reported as 1000 is; the
    /// highlight itself is the host's to drive.
    Highlight,
    /// Mode 1002: also motion while a button is down.
    ButtonMotion,
    /// Mode 1003: also motion with no button down.
    AnyMotion,
}
closed! {
    /// The cursor's style, as DECSCUSR sets it (`CSI Ps SP q`). Ps 0 is
    /// kept apart from 1, as many terminals read it as their own shape.
    #[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
    pub enum CursorShape {
        /// Its Ps.
        pub fn number(self) -> u16;
        /// The terminal's own, which fux-vt does not know: before any
        /// DECSCUSR, after RIS, and as DECSCUSR 7 restores it.
        #[default]
        Initial => 7,
        /// A blinking block, as Ps 0 (or none) names it.
        Zero => 0,
        /// A blinking block (Ps 1).
        BlinkingBlock => 1,
        /// A steady block.
        SteadyBlock => 2,
        /// A blinking underline.
        BlinkingUnderline => 3,
        /// A steady underline.
        SteadyUnderline => 4,
        /// A blinking bar.
        BlinkingBar => 5,
        /// A steady bar.
        SteadyBar => 6,
    }
}

impl CursorShape {
    /// Whether it blinks, as mode 12 reports it: the terminal's own is
    /// read as reset, as xterm's escape state starts.
    pub(crate) const fn blinks(self) -> bool {
        matches!(
            self,
            Self::Zero | Self::BlinkingBlock | Self::BlinkingUnderline | Self::BlinkingBar
        )
    }

    /// The same form, blinking or steady, as mode 12 sets it. The
    /// terminal's own form is unknown: set, it is xterm's block; reset, it
    /// stays as it is, which mode 12 already reads reset.
    pub(crate) const fn with_blink(self, on: bool) -> Self {
        match self {
            Self::Initial if !on => Self::Initial,
            Self::Zero if on => Self::Zero,
            Self::Initial | Self::Zero | Self::BlinkingBlock | Self::SteadyBlock if on => {
                Self::BlinkingBlock
            }
            Self::Initial | Self::Zero | Self::BlinkingBlock | Self::SteadyBlock => {
                Self::SteadyBlock
            }
            Self::BlinkingUnderline | Self::SteadyUnderline if on => Self::BlinkingUnderline,
            Self::BlinkingUnderline | Self::SteadyUnderline => Self::SteadyUnderline,
            Self::BlinkingBar | Self::SteadyBar if on => Self::BlinkingBar,
            Self::BlinkingBar | Self::SteadyBar => Self::SteadyBar,
        }
    }
}

/// How mouse reports are to be encoded (`CSI ? 1005 / 1006 / 1015 h`),
/// the latest set winning, as xterm 411's `extend_coords`.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum MouseProtocolEncoding {
    /// The legacy encoding: coordinates as single bytes.
    #[default]
    Default,
    /// Mode 1005: coordinates in UTF-8.
    Utf8,
    /// Mode 1006, SGR: `CSI < b ; x ; y M`, or `m` for a release.
    Sgr,
    /// Mode 1015, urxvt: `CSI b ; x ; y M`, `b` the code plus 32 and a
    /// release unnamed, as the default encoding has them.
    Urxvt,
}

/// The cell printed last, the state of its grapheme cluster, and what a
/// join knows of its text.
#[derive(Clone, Copy, Debug)]
struct Printed {
    at: (u16, u16),
    cluster: Cluster,
    kept: Kept,
}

/// What a join knows of the cell printed last, so it can tell whether the
/// text grew wide without reading it; and whether the cluster is full.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Kept {
    /// The character just printed there, alone.
    Alone,
    /// Text whose characters' columns sum to 1, none of them one the
    /// cluster width rule looks at, so `cluster_width` says 1 of it.
    One,
    /// Text whose width only `cluster_width` says.
    Unknown,
    /// The cluster is full: once a character of it is dropped, so is every
    /// one after, so a cell always holds a start of its cluster.
    Full,
}

impl Printed {
    /// The cell `at`, which holds the ASCII character just printed.
    fn new(at: (u16, u16), c: char) -> Self {
        Self {
            at,
            cluster: Cluster::start(c),
            kept: Kept::Alone,
        }
    }

    /// The cell `at`, which holds alone the character just printed, whose
    /// properties are `properties`.
    fn of(at: (u16, u16), properties: Properties) -> Self {
        Self {
            at,
            cluster: Cluster::starting(properties),
            kept: Kept::Alone,
        }
    }

    fn full(self) -> bool {
        self.kept == Kept::Full
    }
}

/// The character sets a program designates and shifts between (ECMA-35;
/// DEC STD 070, ch. 3; the VT520 manual's SCS): G0 to G3, each ASCII, DEC
/// Special Graphics or DEC Supplemental Graphics, and the one in GL; a
/// single shift is no part of them, nor of what DECSC saves
/// (`Screen::single`). The national sets and the other sets ctlseqs lists
/// are not kept: each is ASCII, as xterm reads one in UTF-8; a designation
/// of no set is ignored (DEC STD 070, 3.5.1.3: "Unimplemented functions
/// shall be ignored as if they were not received"). At level 2, G2 and G3
/// are DEC Supplemental at power-up and soft reset (DEC STD 070, 3.6.4.2),
/// G0 and G1 ASCII.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(C, align(4))]
pub(crate) struct Charsets {
    /// Which sets are not ASCII, a bit each (`Set::bit`), in the low four
    /// bits; which of those are DEC Supplemental rather than DEC Special
    /// Graphics, in the high four.
    graphics: u8,
    /// The set in GL: G0 after SI, G1 after SO, G2 after LS2, G3 after LS3.
    gl: Set,
}

/// One of the four sets a program designates (ECMA-35, 6.3).
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
enum Set {
    #[default]
    G0,
    G1,
    G2,
    G3,
}

impl Set {
    fn bit(self) -> u8 {
        1 << self as u8
    }
    /// The set an SCS's first intermediate names: `(` G0, `)` G1, `*` G2
    /// and `+` G3.
    fn of(intermediate: u8) -> Self {
        match intermediate {
            b')' => Set::G1,
            b'*' => Set::G2,
            b'+' => Set::G3,
            _ => Set::G0,
        }
    }
}

/// What SCS designates a set as.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Designated {
    Ascii,
    /// DEC Special Graphics (`0`).
    Graphics,
    /// DEC Supplemental Graphics (`% 5`), and the user-preferred
    /// supplemental set (`<`), which is DEC Supplemental (DEC STD 070,
    /// SCS and 3.6.3).
    Supplemental,
}

impl Designated {
    /// The 94-character set an SCS's final `byte` designates, after the
    /// intermediate naming G0 to G3 and `intermediate`, its own if it has
    /// one (ctlseqs, "Designate G0 Character Set"): `0` DEC Special
    /// Graphics, `<` and `% 5` DEC Supplemental, the other sets listed
    /// there ASCII; none for a combination that names no set.
    fn of(intermediate: &[u8], byte: u8) -> Option<Self> {
        match (intermediate, byte) {
            ([], b'0') => Some(Self::Graphics),
            ([], b'<') | (b"%", b'5') => Some(Self::Supplemental),
            (
                [],
                b'4'..=b'7' | b'9' | b'=' | b'>' | b'A'..=b'C' | b'E' | b'H' | b'I' | b'J' | b'K',
            )
            | ([], b'Q' | b'R' | b'Y' | b'Z' | b'`' | b'f')
            | (b"\"", b'4' | b'>' | b'?')
            | (b"%", b'0' | b'2' | b'3' | b'6' | b'=')
            | (b"&", b'4' | b'5') => Some(Self::Ascii),
            _ => None,
        }
    }
}

impl Default for Charsets {
    fn default() -> Self {
        Self::at(Level::Two)
    }
}

impl Charsets {
    /// The sets a soft reset designates at `level`: ASCII in all four at
    /// level 1 (DEC STD 070, 3.6.4.1), DEC Supplemental in G2 and G3 at
    /// level 2 (3.6.4.2); G0 in GL.
    fn at(level: Level) -> Self {
        let supplemental = match level {
            Level::One => 0,
            Level::Two => Set::G2.bit() | Set::G3.bit(),
        };
        Self {
            graphics: supplemental | supplemental << 4,
            gl: Set::G0,
        }
    }
    /// Whether GL's set is not ASCII.
    fn graphics(self) -> bool {
        self.graphics & self.gl.bit() != 0
    }
    /// The character `raw` prints as in `set`.
    fn map(self, set: Set, raw: char) -> char {
        if self.graphics & set.bit() == 0 {
            raw
        } else if self.graphics & set.bit() << 4 != 0 {
            supplemental(raw)
        } else {
            special_graphics(raw)
        }
    }
    fn designate(&mut self, set: Set, kind: Designated) {
        let bits = match kind {
            Designated::Ascii => 0,
            Designated::Graphics => set.bit(),
            Designated::Supplemental => set.bit() | set.bit() << 4,
        };
        self.graphics = (self.graphics & !(set.bit() | set.bit() << 4)) | bits;
    }
}

/// The DEC Supplemental Graphics character for `c`, 0x21 to 0x7e (the VT330
/// and VT340 manual's DEC Multinational set, Figure 2-5, its right half;
/// DEC STD 070 leaves the set to DEC STD 169): Latin-1's character 0x80
/// above, but for ¤ at 0x28, Œ at 0x57, Ÿ at 0x5d, œ at 0x77 and ÿ at
/// 0x7d, and the positions the set leaves empty, which show U+2426, as
/// DEC terminals and xterm do. 0x5f is ß, as the manual has it, where
/// xterm has `_`.
fn supplemental(c: char) -> char {
    match c {
        '\x24'
        | '\x26'
        | '\x2c'..='\x2f'
        | '\x34'
        | '\x38'
        | '\x3e'
        | '\x50'
        | '\x5e'
        | '\x70'
        | '\x7e' => '\u{2426}',
        '\x28' => '\u{a4}',
        '\x57' => '\u{152}',
        '\x5d' => '\u{178}',
        '\x77' => '\u{153}',
        '\x7d' => '\u{ff}',
        '\x21'..='\x7d' => char::from_u32(u32::from(c) | 0x80).unwrap_or(c),
        _ => c,
    }
}

/// What DECSC saves and DECRC restores: the cursor, with its pending wrap
/// and origin mode (DEC STD 070, Appendix D.6.1), the pen, the character
/// sets and DECSCA's attribute (VT420 manual). Each grid keeps its own,
/// as each of xterm's screens does.
#[derive(Clone, Copy, Debug, Default)]
pub(crate) struct Saved {
    pub(crate) cursor: Cursor,
    pub(crate) attributes: Attributes,
    pub(crate) charsets: Charsets,
    pub(crate) protect: u32,
}

/// The DEC Special Graphics character for `c`, 0x5f to 0x7e, as xterm
/// draws it (VT520 manual, Special Graphics set).
fn special_graphics(c: char) -> char {
    const GRAPHICS: [char; 32] = [
        ' ', '◆', '▒', '␉', '␌', '␍', '␊', '°', '±', '␤', '␋', '┘', '┐', '┌', '└', '┼', '⎺', '⎻',
        '─', '⎼', '⎽', '├', '┤', '┴', '┬', '│', '≤', '≥', 'π', '≠', '£', '·',
    ];
    u32::from(c)
        .checked_sub(0x5f)
        .and_then(|i| usize::try_from(i).ok())
        .and_then(|i| GRAPHICS.get(i))
        .copied()
        .unwrap_or(c)
}

/// ESC, which a DECRQCRA checksum sums for a character it has no code for.
const ESC: u16 = 0x1b;

/// The DEC code xterm 411 sums for a Special Graphics or DEC Supplemental
/// glyph past Latin-1 (its charsets.h): the glyph's code in DEC's internal
/// set, not the byte it was drawn with.
fn dec_code(c: char) -> Option<u8> {
    Some(match c {
        '◆' => 0x14,
        '▒' => 0x78,
        '␉' => 0x0a,
        '␌' => 0x0d,
        '␍' => 0x0e,
        '␊' => 0x0b,
        '␤' => 0x15,
        '␋' => 0x0c,
        '┘' => 0x16,
        '┐' => 0x17,
        '┌' => 0x18,
        '└' => 0x19,
        '┼' => 0x1a,
        '⎺' => 0x1b,
        '⎻' => 0x1c,
        '─' => 0x1d,
        '⎼' => 0x1e,
        '⎽' => 0x1f,
        '├' => 0x80,
        '┤' => 0x81,
        '┴' => 0x82,
        '┬' => 0x83,
        '│' => 0x84,
        '≤' => 0x85,
        '≥' => 0x86,
        '≠' => 0x87,
        'Œ' => 0x97,
        'Ÿ' => 0x98,
        'œ' => 0x99,
        'π' => 0xc6,
        _ => return None,
    })
}

/// What a cell's VT100 attributes add to a DECRQCRA checksum.
fn rendition_of(a: Attributes) -> u16 {
    [
        (a.hidden(), 0x08),
        (a.underline(), 0x10),
        (a.inverse(), 0x20),
        (a.blink() != Blink::None, 0x40),
        (a.bold(), 0x80),
    ]
    .iter()
    .filter(|(on, _)| *on)
    .fold(0u16, |sum, (_, bit)| sum | bit)
}

/// The tab stops, one set for both screens, as in xterm: one every eight
/// columns at first and after RIS. Kept for every column the terminal has
/// had, so a resize keeps them; past those, a column is a stop if it is a
/// multiple of eight and no TBC 3 came since the last reset.
#[derive(Clone, Debug)]
struct TabStops {
    stops: Vec<bool>,
    /// Whether the columns past `stops` have the stops of a reset.
    beyond: bool,
}

impl Default for TabStops {
    fn default() -> Self {
        Self {
            stops: Vec::new(),
            beyond: true,
        }
    }
}

impl TabStops {
    fn is_stop(&self, col: u16) -> bool {
        match self.stops.get(usize::from(col)) {
            Some(stop) => *stop,
            None => self.beyond && col.is_multiple_of(8),
        }
    }
    fn set(&mut self, col: u16, stop: bool) {
        let (col, beyond) = (usize::from(col), self.beyond);
        let made = self.stops.len()..=col;
        // Each stop made: TBC 3 then HTS far right makes the row's again.
        crate::budget::spend(col.saturating_add(1).saturating_sub(self.stops.len()));
        (self.stops).extend(made.map(|at| beyond && at.is_multiple_of(8)));
        if let Some(slot) = self.stops.get_mut(col) {
            *slot = stop;
        }
    }
    /// TBC 3: no stops anywhere.
    fn clear(&mut self) {
        self.stops.clear();
        self.beyond = false;
    }
    /// The first stop after `col`, or `last`, the last column, if none
    /// comes before it.
    fn next(&self, col: u16, last: u16) -> u16 {
        if self.stops.is_empty() && self.beyond {
            return (col / 8).saturating_add(1).saturating_mul(8).min(last);
        }
        let after = col.saturating_add(1)..=last;
        after
            .into_iter()
            .find(|&at| self.is_stop(at))
            .unwrap_or(last)
    }
    /// The last stop before `col` from `first` on, or `first`, the first
    /// column the cursor may go back to.
    fn previous(&self, col: u16, first: u16) -> u16 {
        (first..col)
            .rev()
            .find(|&at| self.is_stop(at))
            .unwrap_or(first)
    }
}

/// How DECRQSS answers a request (`Screen::setting_report`).
pub(crate) enum Setting {
    /// The setting, written.
    Written,
    /// A request fux-vt does not know, which xterm answers as invalid.
    Unknown,
}

/// What a CSI sequence came to.
pub(crate) enum Dispatch {
    /// Carried out, with nothing to answer.
    Done,
    /// A query, and its answer.
    Reply(Reply),
    /// Not a sequence the screen implements.
    Unhandled,
}

/// What DSR 75 reports of the data integrity flag (DEC STD 070, DSR):
/// power-up (73) until reported once after power-up or RIS, then 70.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
#[repr(u32)]
pub(crate) enum Integrity {
    #[default]
    PowerUp,
    Reported,
}

impl Integrity {
    /// The report's number.
    pub(crate) fn report(self) -> u8 {
        match self {
            Self::PowerUp => 73,
            Self::Reported => 70,
        }
    }
}

/// The cold state, which RIS starts again as it was made, so a field of
/// it cannot be left out of either.
#[derive(Clone, Debug, Default)]
struct Terminal {
    /// Left by 1047 with this pen: the alternate screen is cleared in its
    /// colours when shown again, keeping what the program drew till then.
    clear_on_show: Option<Attributes>,
    tabs: TabStops,
    cursor_shape: CursorShape,
    mouse: MouseProtocolMode,
    encoding: MouseProtocolEncoding,
    modify_other_keys: Option<OtherKeysLevel>,
    /// What DSR 75 reports of the data integrity flag.
    integrity: Integrity,
    /// The conformance level DECSCL selected last, level 2 until then.
    level: Level,
    /// DECSACE's Ps as set, 0 until then: 0 and 1 have DECCARA and DECRARA
    /// change a stream of positions, 2 a rectangle (DEC STD 070, DECSACE).
    extent: u8,
}

/// A conformance level DECSCL selects (DEC STD 070, DECSCL). The level
/// gates no function, a departure from note 3: a stray `CSI 61 " p` must
/// not disable half the terminal. It decides what DECSTR designates
/// (3.6.4.1, 3.6.4.2) and what DECRQSS reports.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
enum Level {
    One,
    #[default]
    Two,
}

/// Terminal state. Reading a window never changes where subsequent output lands.
#[derive(Clone, Debug)]
pub struct Screen {
    primary: Grid,
    alternate: Grid,
    /// Where the call under way stops at its budget (`Parser::run`'s
    /// `end`; `u64::MAX` for none), and whether printing found it spent
    /// (`check_budget`): printing stops after the glyph it is on, and a REP
    /// keeps the copies it has left in `left`.
    pub(crate) stop_at: u64,
    pub(crate) stopped: bool,
    /// The character and the copies of it a REP stopped at the budget
    /// left, made first by the next call (`resume_repeat`).
    left: Option<(char, usize)>,
    /// Pictures placed on either grid (`pictures`), and what the host's
    /// terminal draws.
    pictures: VecDeque<pictures::Placement>,
    /// The last picture's number (`Placement::id`).
    #[cfg(test)]
    picture_ids: u64,
    graphics: Graphics,
    /// The most bytes the pictures may hold (`PICTURE_BYTES`).
    picture_budget: usize,
    /// The shell's command marks (`shell`).
    commands: Commands,
    next_id: u64,
    version: u64,
    structural: u64,
    /// The rows scrolled up off the top of the screen shown (`scrolled_since`).
    scrolled: u64,
    /// `revision`'s count, and whether a byte read is held from it while
    /// its work is left over.
    revision: u64,
    held: bool,
    alternate_active: bool,
    attributes: Attributes,
    /// The pen's style, and the style of its colours alone (what erasing
    /// fills with), if each is a style that is its own number (`style.rs`),
    /// the same in either grid; else `TABLED`, which the grid looks up
    /// (`Grid::table_style`). The pen's is found when the pen changes
    /// (`pen_changed`); the colours' is `UNKNOWN` until next asked for.
    pen_style: u32,
    blank_style: u32,
    charsets: Charsets,
    /// G2 after SS2, G3 after SS3: the set of the next graphic character,
    /// which spends it. Any other function but SO and SI cancels it (DEC
    /// STD 070, 3.8.3.2), so none is pending as a control or sequence runs
    /// (`cancel_single_shift`).
    single: Option<Set>,
    /// The modes kept as bits (`Kind::Flag`, `Margins`, `Frame`,
    /// `SizeReport`) that are set.
    modes: Modes,
    /// The modes XTSAVE saved set. One never saved is reset, as in xterm,
    /// and RIS and DECSTR keep them, as xterm 411 does.
    saved_modes: Modes,
    /// [`PROTECTED`] while DECSCA 1 protects the glyphs printed from
    /// DECSED and DECSEL (DEC STD 070, 5.11.1.2), else 0.
    protect: u32,
    /// `TABLED` while the glyphs printed are in SPA's guarded area
    /// (ECMA-48 8.3.140), else 0: or-ed into the pen's style, it sends the
    /// pen to the table, which keeps `Attributes::GUARDED`.
    guard: u32,
    /// Whether a glyph either protects may be on a grid: erases look for
    /// them only then. Set at DECSCA 1 and SPA; RIS clears it.
    protected: bool,
    /// How many times synchronized output has been set, for
    /// `Parser::process_until_stop`.
    frames_begun: u64,
    /// What RIS starts again.
    terminal: Terminal,
    /// The cell the last glyph was printed in, and its cluster's state,
    /// while the cursor has not moved nor the row been edited since.
    last_print: Option<Printed>,
    /// The character REP (`CSI b`) repeats: the last one printed that took
    /// a cell of its own, as long as nothing but printing came after it.
    repeat: Option<char>,
    /// The hyperlink the program opened (OSC 8), which glyphs printed take.
    link: Option<Hyperlink>,
    /// Whether a link was opened since the screen was made or reset: until
    /// one is, printing leaves the links be.
    links_seen: bool,
    /// The key the next link opened takes (`Hyperlink::key`).
    next_link: u64,
    /// The colours the program set, with `Feature::Palette`: made when
    /// the first is set, so a screen whose program sets none keeps none.
    colours: Option<Box<Colours>>,
    /// The window title the program set last, with `Feature::Events`
    /// (OSC 0 or 2), and whether its string went on past it.
    title: (Vec<u8>, bool),
}

/// The screen's part of the invariant checker (THREATS.md section 6).
#[cfg(feature = "checks")]
impl Screen {
    /// Checks every invariant its state alone shows, and says which checks
    /// ran and the faults found. The work counters do not see it.
    pub(crate) fn check(&self) -> crate::Checked {
        crate::check::unseen(|| {
            let mut report = crate::Checked::default();
            for (which, grid) in [("primary", &self.primary), ("alternate", &self.alternate)] {
                grid.check(which, self.version, &mut report);
            }
            report.record("screen.ids", "", self.check_ids());
            report.record("screen.structural", "", self.check_structural());
            report.record("screen.tabs", "", self.check_tabs());
            report.record("pictures.count", "", self.check_picture_count());
            report.record("pictures.bytes", "", self.check_picture_bytes(0));
            report.record("pictures.shown", "", self.check_pictures_shown());
            report
        })
    }

    /// 20a: no row identity is two rows', and the shown rows and history's
    /// ends are found by their identity where they are.
    fn check_ids(&self) -> Result<(), String> {
        let mut seen = std::collections::HashSet::new();
        for grid in [&self.primary, &self.alternate] {
            for row in Rows::of(grid) {
                crate::check::ensure(seen.insert(row.id()), || {
                    format!("row {:?} is held twice", row.id())
                })?;
            }
        }
        let grid = self.grid();
        let history = grid.history_len();
        let mut places: Vec<usize> = (history..grid.retained_len()).collect();
        places.extend(
            [0, history.saturating_sub(1)]
                .into_iter()
                .filter(|i| *i < history),
        );
        for index in places {
            let Some(row) = grid.row_at(index) else {
                return Err(format!("no row {index}"));
            };
            crate::check::ensure(grid.index_of(row.id()) == Some(index), || {
                format!(
                    "row {:?} at {index} is found at {:?}",
                    row.id(),
                    grid.index_of(row.id())
                )
            })?;
        }
        Ok(())
    }

    /// 20: a structural change is no later than the screen's version.
    fn check_structural(&self) -> Result<(), String> {
        crate::check::ensure(self.structural <= self.version, || {
            format!(
                "structural {} past version {}",
                self.structural, self.version
            )
        })
    }

    /// 36: tab stops are kept for no more columns than a `u16` counts.
    fn check_tabs(&self) -> Result<(), String> {
        let kept = self.terminal.tabs.stops.len();
        crate::check::ensure(kept <= usize::from(u16::MAX).saturating_add(1), || {
            format!("tab stops kept for {kept} columns")
        })
    }
}

mod pictures;
mod shell;
pub use pictures::{CellSize, Graphics, PICTURE_BYTES, PLACEHOLDER, Shown};
pub use shell::{Commands, Mark as CommandMark};
#[cfg(test)]
mod tests;

/// Palette colour `n - from`, for an SGR parameter `n` of a range that
/// starts at colour `from`'s.
fn indexed(n: u16, from: u16) -> Color {
    Color::Idx(u8::try_from(n.saturating_sub(from)).unwrap_or(0))
}

/// `attributes` with DECCARA's renditions `ops` set in turn (DEC STD 070,
/// DECCARA: 0 normal intensity, positive image, not underlined, not
/// blinking; 1 bold, 4 underscore, 5 blink, 7 negative; 22, 24, 25 and 27
/// each off; 30 to 37, 39, 40 to 47 and 49 the colours, the Color Text
/// Extension's (5.6.3.1)), or with `reverse` DECRARA's reversed (0 all
/// four, 1 bold, 4 underscore, 5 blink, 7 negative). Each is cumulative,
/// as if one function of each were carried out in turn.
fn rendition_change(mut a: Attributes, ops: &[u16], reverse: bool) -> Attributes {
    let flip = |a: Attributes, op: u16| match op {
        1 => a.with_bold(!a.bold()),
        4 => a.with_underline(!a.underline()),
        5 => a.with_blink(if a.blink() == Blink::None {
            Blink::Slow
        } else {
            Blink::None
        }),
        7 => a.with_inverse(!a.inverse()),
        _ => a,
    };
    for &op in ops {
        a = match (reverse, op) {
            (true, 0) => [1, 4, 5, 7].into_iter().fold(a, flip),
            (true, _) => flip(a, op),
            (false, 0) => a
                .with_bold(false)
                .with_dim(false)
                .with_underline(false)
                .with_blink(Blink::None)
                .with_inverse(false),
            (false, 1) => a.with_bold(true),
            (false, 4) => a.with_underline_style(UnderlineStyle::Single),
            (false, 5) => a.with_blink(Blink::Slow),
            (false, 7) => a.with_inverse(true),
            (false, 22) => a.with_bold(false).with_dim(false),
            (false, 24) => a.with_underline(false),
            (false, 25) => a.with_blink(Blink::None),
            (false, 27) => a.with_inverse(false),
            (false, 39) => a.with_foreground(Color::Default),
            (false, 49) => a.with_background(Color::Default),
            (false, n @ 30..=37) => a.with_foreground(indexed(n, 30)),
            (false, n @ 40..=47) => a.with_background(indexed(n, 40)),
            (false, _) => a,
        };
    }
    a
}

/// The colour of 38, 48 or 58, `rest` being what follows the selector:
/// `5:index`, `2:r:g:b`, or ITU-T T.416's `2:space:r:g:b`, its colour
/// space ignored and empty, as xterm reads it, with anything after the
/// blue ignored. `None` if the colour is out of range or of another kind.
fn colour_of(rest: &[u16]) -> Option<Color> {
    match rest {
        [5, index, ..] => u8::try_from(*index).ok().map(Color::Idx),
        [2, r, g, b] | [2, _, r, g, b, ..] => Some(Color::Rgb(Rgb {
            r: u8::try_from(*r).ok()?,
            g: u8::try_from(*g).ok()?,
            b: u8::try_from(*b).ok()?,
        })),
        _ => None,
    }
}

/// Sets a mouse mode or encoding `value` in `slot`, the latest set
/// winning; reset, it is the default again if it is the one set.
fn latest<T: Copy + Default + PartialEq>(slot: &mut T, value: T, on: bool) {
    if on {
        *slot = value;
    } else if *slot == value {
        *slot = T::default();
    }
}

/// The columns a glyph takes in `Screen::text`'s run at column `col` of a
/// line ending at `end`, after a cluster in state `cluster`, if the run
/// may write it: one or two, before the line's end, joining no cluster.
#[inline]
fn run_width(properties: Properties, col: u16, end: u16, cluster: Cluster) -> Option<u16> {
    let width = match properties.width() {
        Some(width @ 1..=2) => u16::try_from(width).ok()?,
        _ => return None,
    };
    (cluster.never_joins(properties) && col.checked_add(width)? <= end).then_some(width)
}

impl Screen {
    pub(crate) fn new(size: Size, history: usize) -> Result<Self, Error> {
        let mut next_id = 1;
        Ok(Self {
            stop_at: u64::MAX,
            stopped: false,
            left: None,
            primary: Grid::new(size, history, &mut next_id, 1)?,
            // Its cells are made when a program first shows it.
            alternate: Grid::unmade(size, &mut next_id, 1)?,
            pictures: VecDeque::new(),
            #[cfg(test)]
            picture_ids: 0,
            graphics: Graphics::default(),
            picture_budget: PICTURE_BYTES,
            commands: Commands::default(),
            next_id,
            version: 1,
            structural: 1,
            scrolled: 0,
            revision: 0,
            held: false,
            alternate_active: false,
            attributes: Attributes::default(),
            pen_style: 0,
            blank_style: 0,
            charsets: Charsets::default(),
            single: None,
            modes: Modes::DEFAULT,
            saved_modes: Modes::default(),
            protect: 0,
            guard: 0,
            protected: false,
            frames_begun: 0,
            terminal: Terminal::default(),
            last_print: None,
            repeat: None,
            link: None,
            links_seen: false,
            next_link: 1,
            colours: None,
            title: (Vec::new(), false),
        })
    }
    /// The primary screen's grid, as a test starts from it.
    #[cfg(test)]
    pub(crate) fn primary_grid(&self) -> &Grid {
        &self.primary
    }
    fn grid(&self) -> &Grid {
        if self.alternate_active {
            &self.alternate
        } else {
            &self.primary
        }
    }
    /// Drops the main screen's scrollback; the screen stays (ctlseqs, ED 3).
    pub(crate) fn clear_history(&mut self) {
        self.primary.clear_history();
        // Rows left history: every reader reads again.
        self.version = self.version.saturating_add(1);
        self.structural = self.version;
        self.revise(1);
    }

    fn grid_mut(&mut self) -> &mut Grid {
        if self.alternate_active {
            &mut self.alternate
        } else {
            &mut self.primary
        }
    }
    /// The grid shown, with the next row id and the version, for `f`.
    #[inline(always)]
    fn with_grid<T>(&mut self, f: impl FnOnce(&mut Grid, &mut u64, u64) -> T) -> T {
        let grid = if self.alternate_active {
            &mut self.alternate
        } else {
            &mut self.primary
        };
        f(grid, &mut self.next_id, self.version)
    }
    /// Finds the pen's style again and forgets its colours' style: every
    /// change of `attributes` is followed by this.
    fn pen_changed(&mut self) {
        self.pen_style = self.attributes.inline_style().unwrap_or(TABLED) | self.guard;
        self.blank_style = UNKNOWN;
    }
    /// The number of the pen's style in the grid shown.
    #[inline]
    fn pen_style(&mut self) -> u32 {
        let style = self.pen_style;
        if style < TABLED {
            return style;
        }
        let attributes = self.attributes.with_guard(self.guard != 0);
        self.grid_mut().table_style(attributes)
    }
    /// The number, in the grid shown, of the style of the pen's colours
    /// alone, which erasing, scrolling and inserting fill cells with (`bce`).
    #[inline]
    fn blank_style(&mut self) -> u32 {
        let style = self.blank_style;
        if style < TABLED {
            return style;
        }
        let attributes = self.attributes.erased();
        if style == UNKNOWN {
            self.blank_style = attributes.inline_style().unwrap_or(TABLED);
            if self.blank_style < TABLED {
                return self.blank_style;
            }
        }
        self.grid_mut().table_style(attributes)
    }
    /// A new version, for what is processed next: like identities, 64 bits
    /// counted up by one do not run out.
    pub(crate) fn begin(&mut self) {
        self.version = self.version.saturating_add(1);
    }
    /// Counts `bytes` read by a call in `revision`, which holds one byte
    /// read back while work is left over: each call that reads moves it on,
    /// and once none is left it counts every byte.
    pub(crate) fn read(&mut self, bytes: usize) {
        let bytes = u64::try_from(bytes).unwrap_or(u64::MAX);
        let read = bytes.saturating_add(u64::from(self.held));
        self.held = self.left_over() && read > 0;
        self.revise(read.saturating_sub(u64::from(self.held)));
    }
    /// `revision` moves on by `n`. A u64 of bytes read is never reached,
    /// so it saturates rather than wrap.
    fn revise(&mut self, n: u64) {
        self.revision = self.revision.saturating_add(n);
    }
    /// The screen's rows and columns.
    pub fn size(&self) -> Size {
        self.grid().size()
    }
    /// The cursor's row and column, always on the screen. A glyph printed
    /// in the last column (or at the right margin) leaves the cursor on
    /// it, with [`pending_wrap`](Self::pending_wrap) set while autowrap is
    /// on.
    pub fn cursor_position(&self) -> (u16, u16) {
        self.grid().cursor.at()
    }
    /// DEC STD 070's Last Column Flag: a glyph went into the last column,
    /// or the right margin's, with autowrap on, and the next one first
    /// moves to the start of the next line. Cursor movements, line feeds,
    /// edits and resetting autowrap end it.
    pub fn pending_wrap(&self) -> bool {
        self.grid().cursor.pending_wrap && self.mode(Mode::Autowrap)
    }
    /// The Last Column Flag, autowrap on or off: a glyph's, set with it on,
    /// or one DECRC restored.
    fn last_column_flag(&self) -> bool {
        let cursor = self.grid().cursor;
        cursor.pending_wrap && (cursor.restored || self.mode(Mode::Autowrap))
    }
    /// Whether `mode` is set, as DECRQM reports it. The alternate screen's
    /// three modes are set together, while it is shown; DECARM and 1048
    /// never are.
    #[inline]
    pub fn mode(&self, mode: Mode) -> bool {
        match mode.kind() {
            Kind::Flag | Kind::Margins | Kind::Frame | Kind::SizeReport => {
                self.modes.contains(mode)
            }
            Kind::Origin => self.grid().cursor.origin,
            Kind::CursorBlink => self.terminal.cursor_shape.blinks(),
            Kind::Screen(_) => self.alternate_active,
            Kind::SaveCursor => false,
            Kind::Permanent(set) => set,
            Kind::Mouse(mouse) => self.terminal.mouse == mouse,
            Kind::Encoding(encoding) => self.terminal.encoding == encoding,
        }
    }
    /// The in-band resize report of the current size, pixels unknown.
    pub(crate) fn size_report(&self) -> Reply {
        let (rows, cols) = self.size().into();
        reply!("\x1b[48;{rows};{cols};0;0t")
    }
    /// Whether XTSAVE holds `mode` set.
    pub(crate) fn saved_mode(&self, mode: Mode) -> bool {
        self.saved_modes.contains(mode)
    }
    pub(crate) fn frames_begun(&self) -> u64 {
        self.frames_begun
    }
    /// The cursor shape last set with DECSCUSR (`CSI Ps SP q`). State
    /// only: fux-vt draws no cursor.
    pub fn cursor_shape(&self) -> CursorShape {
        self.terminal.cursor_shape
    }
    /// The top and bottom margins (DECSTBM), zero-based and inclusive.
    pub(crate) fn scroll_region(&self) -> (u16, u16) {
        let lines = self.grid().lines;
        (lines.first(), lines.last())
    }
    /// The left and right margins (DECSLRM, with DECLRMM set), zero-based
    /// and inclusive.
    pub(crate) fn left_right_margins(&self) -> (u16, u16) {
        let columns = self.grid().columns();
        (columns.first(), columns.last())
    }
    /// The mouse reporting the program asked for.
    pub fn mouse_protocol_mode(&self) -> MouseProtocolMode {
        self.terminal.mouse
    }
    /// How the program asked for mouse reports to be encoded.
    pub fn mouse_protocol_encoding(&self) -> MouseProtocolEncoding {
        self.terminal.encoding
    }
    /// The kitty keyboard protocol flags in force, 0 (legacy) when none.
    /// Always 0 without [`Feature::KittyKeyboard`].
    pub fn kitty_keyboard_flags(&self) -> u8 {
        self.grid().keyboard.top()
    }
    /// The xterm modifyOtherKeys level set by `CSI > 4 ; Pv m`, `None` when
    /// it is off (`Pv` 0 or absent, or past 3, which ctlseqs gives no keys).
    /// Always `None` without [`Feature::KittyKeyboard`].
    pub(crate) fn modify_other_keys(&self) -> Option<OtherKeysLevel> {
        self.terminal.modify_other_keys
    }
    /// The one-based cursor position a DSR 6n or DECXCPR reports, counted
    /// as CUP addresses it: from the margins with DECOM set (DEC STD 070,
    /// CPR and DECXCPR). A cursor waiting to wrap is reported at its
    /// column: the Last Column Flag is a flag (DEC STD 070, D.6.1).
    pub(crate) fn reported_cursor(&self) -> (u32, u32) {
        let g = self.grid();
        let col = g.cursor.col();
        // In origin mode from the left margin, as xterm reports it.
        let col = col.saturating_sub(g.addressed().1.first());
        (u32::from(g.cursor_line()) + 1, u32::from(col) + 1)
    }

    /// The window title the program set last (OSC 0 or OSC 2), with
    /// `Feature::Events`, which reads them; empty until one is set. Its
    /// raw bytes, at most [`crate::OSC_PAYLOAD_LIMIT`], `cut` if the
    /// string went on past them, as [`crate::Event::Title`] gives it.
    pub fn title(&self) -> crate::Kept<'_> {
        crate::Kept {
            bytes: &self.title.0,
            cut: self.title.1,
        }
    }
    /// Keeps `title` as the window title, or, refused room, an empty one
    /// cut (THREATS.md 1.3).
    pub(crate) fn set_title(&mut self, title: crate::Kept<'_>) {
        self.title.0.clear();
        let kept = crate::reserve(&mut self.title.0, title.bytes.len()).is_ok();
        if kept {
            self.title.0.extend_from_slice(title.bytes);
        } else {
            crate::degraded();
        }
        self.title.1 = title.cut || !kept;
    }
    /// The colour palette entry `index` shows if the program changed it
    /// (OSC 4, with `Feature::Palette`); `None` while it is the terminal's.
    pub fn palette_color(&self, index: u8) -> Option<Rgb> {
        self.colours.as_ref()?.palette(index)
    }
    /// Sets the host's colours for palette entries 0 to 15 ([`crate::Parser::set_host_palette`]).
    pub(crate) fn set_host_palette(&mut self, palette: [Option<Rgb>; 16]) {
        if palette != [None; 16] || self.colours.is_some() {
            self.colours.get_or_insert_default().host = palette;
        }
    }
    /// The colour dynamic colour `number` shows if the program set it (OSC
    /// 10 to 19, with `Feature::Palette`): 10 the foreground, 11 the
    /// background, 12 the cursor. `None` while it is the terminal's own.
    pub fn dynamic_color(&self, number: u8) -> Option<Rgb> {
        self.colours.as_ref()?.dynamic(number)
    }
    /// Whether the program changed a palette entry or a dynamic colour.
    pub fn colors_changed(&self) -> bool {
        self.colours.as_ref().is_some_and(|c| c.changed())
    }
    /// The colours the program set, for the parser's OSC handling.
    pub(crate) fn colours_mut(&mut self) -> &mut Option<Box<Colours>> {
        &mut self.colours
    }

    /// OSC 8: opens the link `payload` names, or closes the open one
    /// (`link::parse`). A link without an `id` is a link of its own each
    /// time it is opened, as VTE makes it.
    pub(crate) fn hyperlink_osc(&mut self, payload: &[u8]) {
        self.link = crate::link::parse(payload).and_then(|(uri, id)| {
            let key = self.next_link;
            // Keys never run out in practice: one a link opened.
            self.next_link = key.checked_add(1)?;
            Some(Hyperlink::new(key, uri, id))
        });
        // Neither grid has made a link of the one open now.
        self.primary.links.open = Held::Pending;
        self.alternate.links.open = Held::Pending;
        self.links_seen |= self.link.is_some();
    }

    /// Gives the cells `span` of row `row` the open link, if `open`, or none.
    #[inline(never)]
    fn link_cells(&mut self, row: u16, span: Range<usize>, open: bool) {
        let version = self.version;
        let pen = self.link.as_ref().filter(|_| open);
        let grid = if self.alternate_active {
            &mut self.alternate
        } else {
            &mut self.primary
        };
        grid.print_link(row, span, pen, version);
    }

    /// OSC 133, semantic prompts (`references/modern/osc133_*`): `A`
    /// marks the row a prompt starts on, after a fresh line, and `L` is the
    /// fresh line alone: a new line unless the cursor is in the first
    /// column, as the semantic prompt proposal and Ghostty have it. `N` is
    /// `A` that first ends the command running, as a `D` with no status.
    /// `C` and `D` mark a command (`command_mark`), `P` a prompt where it
    /// is; the rest change nothing. Every mark counts, whoever sent it.
    pub(crate) fn prompt_osc(&mut self, payload: &[u8]) {
        let mut parts = payload.split(|b| *b == b';');
        let command = parts.next().unwrap_or_default();
        if matches!(command, b"C" | b"D") {
            self.command_mark(command, parts.next());
            return;
        }
        // P (explicit start of prompt) of the primary kind, `k=i` or none,
        // marks where it is without a fresh line: shells send it in A's
        // place. Right-side and continuation prompts start none.
        if command == b"P" {
            let kind = parts.find_map(|option| option.strip_prefix(b"k="));
            if kind.is_none_or(|k| k == b"i") {
                self.prompted();
            }
            return;
        }
        if !matches!(command, b"A" | b"N" | b"L") {
            return;
        }
        // N "may first implicitly terminate a previous command".
        if command == b"N" {
            self.command_mark(b"D", None);
        }
        // CR, then IND, as Ghostty does it.
        if self.grid().cursor.col() != 0 {
            self.control(b'\r');
            self.linefeed();
        }
        if command != b"L" {
            self.prompted();
        }
    }
    /// The line rendition of row `row` of the screen (DECDWL, DECDHL).
    pub fn line_rendition(&self, row: u16) -> LineRendition {
        let g = self.grid();
        if !g.doubled() {
            return LineRendition::Single;
        }
        g.rendition(row)
    }
    /// How many rows of history the screen keeps now.
    pub fn history_len(&self) -> usize {
        self.grid().history_len()
    }
    /// The rows retained, history's oldest first, then the screen's.
    pub fn rows(&self) -> Rows<'_> {
        Rows::of(self.grid())
    }
    /// The primary screen's rows, its history first, whichever screen is in
    /// use: what a program printed before it took the alternate screen.
    pub fn main_rows(&self) -> Rows<'_> {
        Rows::of(&self.primary)
    }
    /// The numbers of the rows the primary screen's history keeps, oldest
    /// first, whichever screen is in use: each row that scrolls into it
    /// takes the next number, never reused, so `end` moves on by every row
    /// it takes, and `start` by every row let go, to its budget or
    /// [`crate::Parser::clear_history`]. The history rows of
    /// [`Screen::main_rows`] are these, in order. A host that noted `end`
    /// finds the rows taken since numbered from it on, and, if `start` has
    /// passed it, that some went before it read them. A resize lays
    /// history out again and may number it afresh: numbers compare only
    /// between resizes.
    pub fn history_numbers(&self) -> Range<u64> {
        let (start, end) = self.primary.history_numbers();
        start..end
    }
    /// The alternate screen's rows, whichever screen is in use: what a
    /// full-screen program drew last, kept until it is shown again.
    pub fn alternate_rows(&self) -> Rows<'_> {
        Rows::of(&self.alternate)
    }
    /// Where the retained row `id` is, from the oldest, if it is kept.
    pub fn row_index(&self, id: RowId) -> Option<usize> {
        self.grid().index_of(id)
    }
    /// The screen's rows: the window no rows up into history.
    pub fn window(&self) -> Window<'_> {
        let grid = self.grid();
        Window {
            grid,
            start: grid.history_len(),
        }
    }
    /// A mark of the screen as it is now, to ask later what changed.
    pub fn mark(&self) -> Mark {
        Mark {
            version: self.version,
            scrolled: self.scrolled,
        }
    }
    /// The screen's revision: the bytes of output read, one held back
    /// while a call has stopped at its budget with work left, plus one for
    /// each resize to another size and each change to its history
    /// (`clear_history`, also `CSI 3 J`; `set_history_bytes` when it evicts
    /// rows). With no work left it is the same however the output was cut
    /// into calls; it moves on with each call that reads output, and after
    /// anything else that changes the screen.
    pub fn revision(&self) -> u64 {
        self.revision
    }
    /// Whether a reader of `mark` must read every row again: a scroll,
    /// resize, reset or switch of screens happened since.
    pub fn full_refresh_since(&self, mark: Mark) -> bool {
        mark.version < self.structural || mark.version > self.version
    }
    /// How many rows scrolled up off the top of the screen shown since
    /// `mark`, into history or not, on either screen: a scroll up of rows
    /// from the top (LF or IND at the bottom margin, SU, DL on the top row),
    /// by as many rows as the scroll moved. A scroll region that starts at
    /// the top row is counted though it ends above the last: the rows below
    /// it stay, so a host that moved them too sends them again. A host
    /// mirroring the screen moves the rows it sent up by this many, then
    /// sends those whose [`crate::Row::id`] or [`crate::Row::version`]
    /// differ from what it now holds. Other moves (a scroll down or inside
    /// left and right margins, a resize, a switch of screens) are not
    /// counted; [`Screen::full_refresh_since`] tells of them.
    pub fn scrolled_since(&self, mark: Mark) -> u64 {
        self.scrolled.saturating_sub(mark.scrolled)
    }
    /// How many style sweeps both grids have made, with the `checks`
    /// feature: for the bounds tests.
    #[cfg(feature = "checks")]
    pub fn style_sweeps(&self) -> u64 {
        (self.primary.sweeps()).wrapping_add(self.alternate.sweeps())
    }

    pub(crate) fn resize(&mut self, size: Size, reflow: bool) -> Result<(), Error> {
        // A frame drawn for the old size is no frame for the new one: as in
        // Ghostty, any resize ends synchronized output.
        if self.size() == size {
            self.modes.set(Mode::SynchronizedOutput, false);
            return Ok(());
        }
        let version = self.version.saturating_add(1);
        let mut next = self.next_id;
        // The command marks go with their characters when the lines
        // rewrap; without reflow, they stay with their rows.
        let (mut primary, kept, marks) = if reflow {
            self.primary
                .reflowed(size, &mut next, version, self.commands.marks())?
        } else {
            let (grid, kept) = self.primary.resized(size, &mut next, version)?;
            (grid, kept, self.commands.marks())
        };
        let (mut alternate, _) = self.alternate.resized(size, &mut next, version)?;
        // Only now, with both grids built, does the resize happen: a refused
        // one leaves the screen whole, the next print still joining.
        self.last_print = None;
        primary.adopt_history(&mut self.primary, kept);
        // The links' tables and keyboard stacks go along.
        primary.links = mem::take(&mut self.primary.links);
        alternate.links = mem::take(&mut self.alternate.links);
        primary.keyboard = mem::take(&mut self.primary.keyboard);
        alternate.keyboard = mem::take(&mut self.alternate.keyboard);
        self.primary = primary;
        self.alternate = alternate;
        self.commands = self.commands.moved(marks);
        self.modes.set(Mode::SynchronizedOutput, false);
        self.next_id = next;
        self.version = version;
        self.structural = version;
        self.revise(1);
        // History is made anew: the pictures are found in it again.
        self.retrack_pictures();
        Ok(())
    }

    pub(crate) fn history_bytes(&self) -> usize {
        self.primary.history_bytes()
    }

    pub(crate) fn set_history_bytes(&mut self, bytes: usize) {
        if self.primary.history_bytes() == bytes {
            return;
        }
        let rows = self.primary.history_len();
        self.primary.set_history_bytes(bytes);
        if self.primary.history_len() == rows {
            return;
        }
        // Rows left history: every reader reads again.
        self.version = self.version.saturating_add(1);
        self.structural = self.version;
        self.revise(1);
    }

    fn scroll(&mut self, region: Span, count: u16, direction: Scroll) {
        if self.grid().lr() {
            self.scroll_columns(region, count, direction);
            return;
        }
        self.scroll_rows(region, count, direction);
    }
    /// `scroll` without left and right margins: the rows move, and every
    /// reader's window is invalid, not just its newly blank rows.
    fn scroll_rows(&mut self, region: Span, count: u16, direction: Scroll) {
        self.structural = self.version;
        if direction != Scroll::Down && region.first() == 0 {
            let moved = count.min(region.len());
            self.scrolled = self.scrolled.saturating_add(u64::from(moved));
        }
        // The rows brought in take the pen's colours (`bce`), as in xterm.
        let blank = self.blank_style();
        self.with_grid(|g, next, version| {
            g.scroll(region, count, direction, blank, next, version);
        });
        // A scroll up that keeps none of the rows it takes crops the
        // pictures whose top rows they were (DEC STD 070-9, 11.2.4).
        if !self.pictures.is_empty() {
            self.crop_pictures(region, direction);
        }
    }
    /// A scroll inside left and right margins (`Grid::scroll_columns`),
    /// its blanks in the pen's colours.
    #[inline(never)]
    fn scroll_columns(&mut self, region: Span, count: u16, direction: Scroll) {
        #[cfg(test)]
        pictures::placement_test::SCROLLS.with(|n| n.set(n.get().saturating_add(1)));
        let blank = self.blank_style();
        let up = direction != Scroll::Down;
        self.with_grid(|g, _, version| g.scroll_columns(region, count, up, blank, version));
    }
    /// IND, and LF, VT and FF: down a line, scrolling at the bottom margin;
    /// with left and right margins only while the cursor is between them,
    /// and outside them the cursor stays at the margin, as DEC STD 070
    /// (5.4.3) and xterm's `xtermIndex` have it.
    fn linefeed(&mut self) {
        self.grid_mut().cursor.pending_wrap = false;
        let g = self.grid();
        if g.cursor.row() == g.lines.last() {
            if g.lr() {
                if g.in_columns() {
                    self.scroll_columns(g.lines, 1, Scroll::Up { history: true });
                }
                return;
            }
            self.scroll_rows(g.lines, 1, Scroll::Up { history: true });
        } else {
            let g = self.grid_mut();
            g.set_row(g.cursor.row().saturating_add(1));
        }
    }
    /// A line feed and a carriage return, NEL's and LNM's. With left and
    /// right margins the line feed goes first, as the column says whether
    /// it scrolls (xterm's `CASE_NEL`).
    fn new_line(&mut self) {
        let g = self.grid_mut();
        if g.lr() {
            return self.new_line_in_margins();
        }
        g.set_col(0);
        self.linefeed()
    }
    /// `new_line` with left and right margins: the line feed, then the
    /// carriage to the left margin.
    #[inline(never)]
    fn new_line_in_margins(&mut self) {
        self.linefeed();
        self.grid_mut().carriage_return();
    }
    /// RI: up a line, scrolling at the top margin, as `linefeed` goes down.
    fn reverse_index(&mut self) {
        self.grid_mut().cursor.pending_wrap = false;
        let g = self.grid();
        if g.cursor.row() == g.lines.first() {
            if !g.lr() || g.in_columns() {
                self.scroll(g.lines, 1, Scroll::Down);
            }
        } else {
            let g = self.grid_mut();
            g.set_row(g.cursor.row().saturating_sub(1));
        }
    }
    /// Moves the cursor to where a glyph `width` wide goes: on to the next
    /// line with autowrap on (DEC STD 070, Appendix D.6.1), or back to the
    /// last column it fits in with autowrap off. The end of the cursor's
    /// line after it, or none for a glyph not printed (a wide one with
    /// autowrap off, as xterm drops it).
    #[inline]
    fn wrap_for(&mut self, width: u16) -> Option<u16> {
        let g = self.grid();
        let end = g.line_end();
        // The last column a glyph this wide can start in.
        let Some(room) = end.checked_sub(width) else {
            return self.too_wide(end, width);
        };
        // A pending wrap is carried out wherever it waits, even where a
        // reset right margin was, as xterm and Ghostty do.
        if !g.cursor.pending_wrap && g.cursor.col() <= room {
            return Some(end);
        }
        let line = self.wrap(end, room, width);
        self.check_budget();
        line
    }
    /// Notes whether the call under way has spent its budget, as printing
    /// does at each wrap, each glyph inserted and each glyph text prints
    /// one at a time, and SUB, a tab and a BS that wraps back do: printing
    /// then stops after the
    /// glyph it is on (`stopped`), and the parser at that byte.
    #[inline]
    pub(crate) fn check_budget(&mut self) {
        self.stopped |= crate::budget::spent() >= self.stop_at;
    }
    /// `wrap_for` for a glyph wider than the line: printed between margins
    /// narrower than it, but not on a halved line (DEC STD 070, 5.6.1); a
    /// pending wrap is carried out first, as xterm does.
    #[cold]
    #[inline(never)]
    fn too_wide(&mut self, end: u16, width: u16) -> Option<u16> {
        let g = self.grid();
        if width <= g.row_end(g.cursor.row()) {
            return Some(end);
        }
        if g.cursor.pending_wrap && self.mode(Mode::Autowrap) {
            self.wrap(end, 0, width);
        }
        None
    }
    /// `wrap_for` where the glyph does not fit: the cursor goes to the next
    /// line, or back to `room`, the last column the glyph fits in.
    #[inline(never)]
    fn wrap(&mut self, end: u16, room: u16, width: u16) -> Option<u16> {
        let g = self.grid();
        let wrap = self.mode(Mode::Autowrap);
        // xterm writes a wide glyph over none before it, unless it fits:
        // the flag a glyph left at an old right margin is no wrap.
        if !wrap && width > 1 && g.cursor.col() > room {
            return None;
        }
        if !wrap {
            // A wrap left pending where a right margin was overwrites
            // there, as xterm does.
            let g = self.grid_mut();
            g.set_col(g.cursor.col().min(room));
            g.cursor.pending_wrap = false;
            return Some(end);
        }
        let row = g.cursor.row();
        // Without margins or a halved row: wrap onto the next row, or
        // scroll at the bottom margin.
        if !g.narrow() {
            let (bottom, last, version) = (g.lines.last(), g.size().lines().last(), self.version);
            let g = self.grid_mut();
            if row != bottom {
                g.wrap_onto(row, row < last, version);
                g.cursor.pending_wrap = false;
                g.set_col(0);
                g.set_row(row.saturating_add(1));
                return Some(end);
            }
            g.wrap_onto(row, true, version);
            g.set_col(0);
            self.linefeed();
            return Some(end);
        }
        // The row is soft-wrapped, whatever its last column holds, unless
        // the line feed leaves the cursor on it (the last row below the
        // scroll region; the bottom margin, outside the side margins).
        let wrapped = if g.lr() && row == g.lines.last() {
            g.in_columns()
        } else {
            row < g.size().lines().last() || row == g.lines.last()
        };
        // Set before scrolling so a departing row carries its soft-wrap into history.
        self.with_grid(|g, _, v| g.wrap_onto(row, wrapped, v));
        // With margins, the line feed goes from the column the glyph did
        // not fit in (xterm's `WrapLine`).
        self.new_line();
        let g = self.grid();
        if !g.narrow() {
            return Some(end);
        }
        // The line it wrapped onto may be halved to less than the glyph:
        // it is not printed there (`too_wide`).
        let end = g.line_end();
        (width <= end || width <= g.row_end(g.cursor.row())).then_some(end)
    }

    /// SUB (DEC STD 070, p. 5-132): the error character (U+2426), printed
    /// in no character set.
    #[cold]
    #[inline(never)]
    pub(crate) fn substitute(&mut self) {
        self.print_as('\u{2426}', Properties::of('\u{2426}'));
        self.check_budget();
    }

    /// Cancels a pending single shift, as a function other than a graphic
    /// character, SO or SI begins (NUL and DEL are none, `control`): "If
    /// the next character in the received data stream is not a graphic
    /// character [...], the single shift function is ignored" (DEC STD 070,
    /// 3.8.3.2, SS2 and SS3). SS2 and SS3 then set it again.
    pub(crate) fn cancel_single_shift(&mut self) {
        self.single = None;
    }

    #[inline(never)]
    pub(crate) fn print(&mut self, raw: char) {
        let set = self.single.take().unwrap_or(self.charsets.gl);
        let c = self.charsets.map(set, raw);
        self.print_as(c, Properties::of(c))
    }

    /// `print` of `c`, already in the character set's mapping, whose
    /// properties are `properties`.
    fn print_as(&mut self, c: char, properties: Properties) {
        // A control character (C0, DEL or C1, the only ones of no width)
        // is not printed.
        let Some(width) = properties.width().and_then(|w| u16::try_from(w).ok()) else {
            return;
        };
        // Nor where it fits nowhere, but for a pending wrap, which xterm
        // still carries out. REP still repeats it.
        if width > self.grid().size().cols() {
            self.repeat = Some(c);
            if self.grid().cursor.pending_wrap && self.mode(Mode::Autowrap) {
                let end = self.grid().line_end();
                self.wrap(end, 0, width);
            }
            return;
        }
        if self.extend_cluster(c, properties) {
            // A Prepend character of no width joins nothing after it
            // (README): what follows is segmented afresh.
            if width == 0
                && properties.is_prepend()
                && let Some(printed) = &mut self.last_print
            {
                printed.cluster = Cluster::ended();
            }
            return;
        }
        if width == 0 {
            if let Some((row, col)) = self.before_cursor() {
                let g = self.grid();
                // A picture's cell holds a control, which ends a cluster
                // (UAX #29 GB4): the mark has no cell, as at a row's start.
                if g.cell(row, col)
                    .is_some_and(|c| c.contents() == PLACEHOLDER)
                {
                    return;
                }
                // A blank cell takes a space for the mark to follow, and
                // with it the open link, as a glyph printed there would.
                let blank = g.stored(row, col).is_some_and(|c| !c.has_contents());
                let end = col.saturating_add(1);
                // A full cell takes no more (`append`), nor the cell
                // printed last once it has dropped a character, so it
                // keeps a start of its text.
                let full = self
                    .last_print
                    .is_some_and(|p| p.at == (row, col) && p.full());
                let mut joined = false;
                self.with_grid(|g, _, v| {
                    g.mutate_line(row, v, end, |line| {
                        joined = !full && line.append(usize::from(col), c);
                        joined
                    })
                });
                // A mark dropped leaves the cell blank, its link unread.
                if blank && self.links_seen {
                    let at = usize::from(col);
                    self.link_cells(row, at..at.saturating_add(1), true);
                }
                // What follows is segmented after the mark, as UAX #29
                // segments the cell's text; after a Prepend of no width,
                // which joins nothing here (README), afresh. A mark dropped
                // from a full cell is still where segmenting goes on from.
                if self.last_print.is_some() {
                    self.last_print = Some(Printed {
                        at: (row, col),
                        cluster: if properties.is_prepend() {
                            Cluster::ended()
                        } else {
                            Cluster::starting(properties)
                        },
                        kept: if joined { Kept::Unknown } else { Kept::Full },
                    });
                }
            }
            return;
        }
        // What REP repeats: the last glyph printed, not a mark.
        self.repeat = Some(c);
        // Spent whether or not it fits: REP may try many that do not.
        crate::budget::spend(width);
        let Some(end) = self.wrap_for(width) else {
            return;
        };
        if self.mode(Mode::Insert) {
            let blank = self.blank_style();
            self.with_grid(|g, _, v| g.edit_cells(width, true, blank, v));
            self.check_budget();
        }
        let (row, col) = self.grid().cursor.at();
        let glyph = Compact::glyph(c, usize::from(width), self.pen_style() | self.protect);
        self.with_grid(|g, _, version| {
            g.mutate_row(row, version, col.saturating_add(width), |cells| {
                put(cells, usize::from(col), glyph, width)
            });
            // Past the glyph; in the last column it waits there to wrap.
            g.advance_within(col.saturating_add(width), end);
        });
        if self.links_seen {
            let at = usize::from(col);
            self.link_cells(row, at..at.saturating_add(usize::from(width)), true);
        }
        self.last_print = Some(Printed::of((row, col), properties));
    }

    /// Prints the characters `chars` yields, as `print` prints each. A
    /// glyph one or two columns wide that joins no cluster and fits before
    /// the line's end without a wrap is written straight into its row
    /// (`run`); anything else, and every character under IRM, takes
    /// `print`. The first takes `print` too, which spends a single shift.
    pub(crate) fn text(&mut self, chars: &mut Chars<'_>) {
        let general = self.mode(Mode::Insert);
        let Some(first) = chars.next() else {
            return;
        };
        self.print(first);
        while !self.stopped
            && let Some(next) = chars.next()
        {
            // A glyph begins a run; a mark takes the general path.
            let (mut c, mut properties) = (next, Properties::of(next));
            if !general && matches!(properties.width(), Some(1 | 2)) {
                match self.run(c, properties, chars) {
                    Some(stopped) => (c, properties) = stopped,
                    None => continue,
                }
            }
            // A mark can rewrite its row's text, a few bytes asking for it.
            self.print_as(c, properties);
            self.check_budget();
        }
    }

    /// `text`'s run: writes `first` and the glyphs `chars` yields after it,
    /// while each fits and joins nothing, and says which it stopped at,
    /// unwritten, if any.
    fn run(
        &mut self,
        first: char,
        properties: Properties,
        chars: &mut Chars<'_>,
    ) -> Option<(char, Properties)> {
        let Some((row, start, end, width)) = self.run_start(properties) else {
            return Some((first, properties));
        };
        let style = self.pen_style() | self.protect;
        let mut last = None;
        let mut stopped = None;
        let mut col = start;
        self.with_grid(|g, _, version| {
            g.mutate_row_to(row, version, |cells| {
                let mut changed = None;
                let mut glyph = Some((first, properties, width));
                while let Some((c, properties, width)) = glyph {
                    let cell = Compact::glyph(c, usize::from(width), style);
                    if put(cells, usize::from(col), cell, width) {
                        changed = col.checked_add(width);
                    }
                    last = Some((col, c, properties));
                    col = col.saturating_add(width);
                    glyph = chars.next().and_then(|c| {
                        let properties = Properties::of(c);
                        match run_width(properties, col, end, Cluster::default()) {
                            Some(width) => Some((c, properties, width)),
                            None => {
                                stopped = Some((c, properties));
                                None
                            }
                        }
                    });
                }
                changed
            });
            g.advance_within(col, end);
        });
        crate::budget::spend(col.saturating_sub(start));
        if self.links_seen {
            self.link_cells(row, usize::from(start)..usize::from(col), true);
        }
        if let Some((at, c, properties)) = last {
            self.repeat = Some(c);
            self.last_print = Some(Printed::of((row, at), properties));
        }
        stopped
    }

    /// Where `run` and `fill` write on the cursor's row: its row, the
    /// cursor's column and the line's end, with the width of a glyph of
    /// `properties` there, if it fits and joins nothing.
    #[inline]
    fn run_start(&self, properties: Properties) -> Option<(u16, u16, u16, u16)> {
        let g = self.grid();
        let (row, start) = g.cursor.at();
        let end = g.line_end();
        let cluster = self.last_print.map(|p| p.cluster).unwrap_or_default();
        let fits = (!g.cursor.pending_wrap).then(|| run_width(properties, start, end, cluster));
        Some((row, start, end, fits.flatten()?))
    }

    /// REP's copies of `c`, as `run` writes text: up to `copies` along the
    /// cursor's row while each fits and joins nothing; says how many, none
    /// where `print` must write the next.
    fn fill(&mut self, c: char, properties: Properties, copies: usize) -> usize {
        let Some((row, start, end, width)) = self.run_start(properties) else {
            return 0;
        };
        let room = usize::from(end.saturating_sub(start).checked_div(width).unwrap_or(0));
        let made = copies.min(room);
        let Some(last) = made.checked_sub(1) else {
            return 0;
        };
        let cell = Compact::glyph(c, usize::from(width), self.pen_style() | self.protect);
        let at = move |k: usize| {
            u16::try_from(k)
                .ok()
                .and_then(|k| k.checked_mul(width))
                .map_or(end, |cols| start.saturating_add(cols))
        };
        self.with_grid(|g, _, version| {
            g.mutate_row_to(row, version, |cells| {
                let mut changed = None;
                for k in 0..made {
                    if put(cells, usize::from(at(k)), cell, width) {
                        changed = Some(at(k.saturating_add(1)));
                    }
                }
                changed
            });
            g.advance_within(at(made), end);
        });
        crate::budget::spend(at(made).saturating_sub(start));
        if self.links_seen {
            self.link_cells(row, usize::from(start)..usize::from(at(made)), true);
        }
        self.repeat = Some(c);
        self.last_print = Some(Printed::of((row, at(last)), properties));
        made
    }

    /// The cell before the cursor, which a mark joins: the one left of it,
    /// or else the last of the soft-wrapped row above; of a wide glyph, its
    /// first half.
    fn before_cursor(&self) -> Option<(u16, u16)> {
        let g = self.grid();
        let (row, col) = (g.cursor.row(), g.next_column());
        let (row, col) = match col.checked_sub(1) {
            Some(left) => (row, left),
            None => {
                let above = row.checked_sub(1).filter(|&above| g.live_wrapped(above))?;
                // Its last column: a halved line's ends at half the screen.
                (above, g.row_end(above).saturating_sub(1))
            }
        };
        let second = g.stored(row, col)?.is_wide_continuation();
        Some((row, if second { col.saturating_sub(1) } else { col }))
    }

    /// Joins `c` to the cell printed last, if the cursor is just past it and
    /// `c` continues its grapheme cluster (UAX #29, see `unicode.rs`): a
    /// spacing vowel sign, a variation selector, a ZWJ sequence, a flag's
    /// second regional indicator. A narrow cell whose cluster becomes two
    /// columns wide is widened, as programs laid out with unicode-width
    /// expect and kitty and Ghostty (mode 2027) do, or in the last column
    /// moved as a wide glyph (`relocate`). A zero-width mark joins the cell
    /// before the cursor, even after a cursor move. Whether `c` was taken:
    /// joined, or dropped because the cluster is full.
    fn extend_cluster(&mut self, c: char, properties: Properties) -> bool {
        let anchor = match self.last_print {
            Some(printed) => printed,
            None if properties.width() == Some(0) => match self.mark_anchor() {
                Some(printed) => printed,
                None => return false,
            },
            None => return false,
        };
        let mut cluster = anchor.cluster;
        if !cluster.push_properties(properties) {
            return false;
        }
        self.join(c, properties, Printed { cluster, ..anchor })
    }

    /// What a mark joins with nothing printed last: the cell before the
    /// cursor, its cluster's state read from its text.
    fn mark_anchor(&self) -> Option<Printed> {
        let at = self.before_cursor()?;
        let cluster = Cluster::of(self.grid().cell(at.0, at.1)?.contents());
        Some(Printed {
            at,
            cluster,
            kept: Kept::Unknown,
        })
    }

    /// `extend_cluster` once `c` continues `anchor`'s cluster: joins it, if
    /// the cursor is just past the anchor's cell on its row.
    fn join(&mut self, c: char, properties: Properties, anchor: Printed) -> bool {
        let g = self.grid();
        let (row, col) = (g.cursor.row(), g.next_column());
        let Printed {
            at: (anchor_row, anchor_col),
            cluster,
            kept: known,
        } = anchor;
        let Some(cell) = g.stored(anchor_row, anchor_col) else {
            return false;
        };
        let narrow = !cell.is_wide();
        let next = anchor_col.checked_add(if narrow { 1 } else { 2 });
        if anchor_row != row
            || next != Some(col)
            || !cell.has_contents()
            || cell.is_wide_continuation()
        {
            return false;
        }
        let at = usize::from(anchor_col);
        let cursor = usize::from(col);
        crate::budget::spend(2);
        let fits = col < g.line_end();
        // Whether the text is one column, summed, before `c`; and whether
        // it is a lone regional indicator, which a second makes a flag.
        let first = matches!(known, Kept::Alone).then(|| cell.first()).flatten();
        let one = match known {
            Kept::Alone => {
                narrow
                    && first.is_some_and(|first| {
                        first.is_ascii() || !Properties::of(first).in_context()
                    })
            }
            Kept::One => true,
            Kept::Unknown | Kept::Full => false,
        };
        let flag = properties.regional_indicator()
            && first.is_some_and(|first| Properties::of(first).regional_indicator());
        // And after: summed still if `c` too is none the rule looks at.
        let summed = one && !properties.in_context();
        let still_one = summed && properties.width() == Some(0);
        let mut grows = false;
        let mut kept = known != Kept::Full;
        self.with_grid(|g, _, v| {
            g.mutate_line(row, v, col.saturating_add(1), |line| {
                kept = kept && line.append(at, c);
                // Width is kept once wide.
                grows = kept
                    && narrow
                    && if summed {
                        !still_one
                    } else {
                        flag || crate::unicode::cluster_width(line.text(at)) >= 2
                    };
                kept
            });
        });
        let at = match (grows, fits) {
            (true, true) => {
                self.widen(row, at, cursor);
                Some((anchor_row, anchor_col))
            }
            (true, false) => self.relocate(row, anchor_col),
            (false, _) => Some((anchor_row, anchor_col)),
        };
        let known = match (kept, still_one) {
            (false, _) => Kept::Full,
            (true, true) => Kept::One,
            (true, false) => Kept::Unknown,
        };
        self.last_print = at.map(|at| Printed {
            at,
            cluster,
            kept: known,
        });
        true
    }

    /// Moves the cluster in cell `at` of `row`, the line's last, grown two
    /// columns wide, to where a wide glyph goes (`wrap_for`), and says
    /// where it went. Mode 2027's spec ("Variation Selector 16"): the
    /// cluster's width is 2, "which may possibly cause reflowing of that
    /// symbol to the next line if on right margin with AutoWrap mode set";
    /// so it is placed as a wide glyph printed there is, the line feed
    /// scrolling at the bottom margin, under IRM inserted. With autowrap
    /// off the spec leaves it undefined ("Margins and AutoWrap with
    /// Emoji"), and it is not printed, as a wide glyph with no room is not
    /// (xterm, the DECAWM row of the README). It keeps its style,
    /// protection and link. The column it leaves is blank in the default
    /// attributes and unlinked, as the one a wide glyph did not fit in is
    /// before anything is printed there: wrapped, a spacer
    /// (`Row::ends_in_spacer`).
    #[cold]
    #[inline(never)]
    fn relocate(&mut self, row: u16, at: u16) -> Option<(u16, u16)> {
        let g = self.grid();
        let (Some(&cell), Some(text)) = (g.stored(row, at), g.cell(row, at)) else {
            return None;
        };
        let (attributes, text) = (text.attributes(), text.contents().to_owned());
        let link = (g.live_row(row))
            .and_then(|r| r.link(usize::from(at)))
            .cloned();
        let mut glyph = cell.holding(b" ")?;
        glyph.widen();
        let links = self.links_seen;
        self.with_grid(|g, _, v| {
            let at = usize::from(at);
            g.mutate_row(row, v, 0, |cells| {
                cells.get_mut(at).map(|c| *c = BLANK).is_some()
            });
            if links {
                g.set_link(row, at..at.saturating_add(1), None, v);
            }
        });
        let end = self.wrap_for(2)?;
        crate::budget::spend(2);
        if self.mode(Mode::Insert) {
            let blank = self.blank_style();
            self.with_grid(|g, _, v| g.edit_cells(2, true, blank, v));
        }
        let (row, col) = self.grid().cursor.at();
        let to = usize::from(col);
        self.with_grid(|g, _, v| {
            // Its style is found again: no cell holds its number once it
            // is blanked, so the wrap's or the edit's styles may sweep it.
            glyph.set_style(g.style(attributes));
            g.mutate_line(row, v, col.saturating_add(2), |line| {
                put(line.cells, to, glyph, 2);
                line.set(to, glyph, &text);
                true
            });
            if links {
                g.set_link(row, to..to.saturating_add(2), link.as_ref(), v);
            }
            g.advance_within(col.saturating_add(2), end);
        });
        Some((row, col))
    }

    /// Widens cell `at` of `row` over the cell under the cursor, which
    /// becomes its second half as a glyph's would: under IRM what is there
    /// moves right first (ECMA-48 7.2.10).
    #[cold]
    #[inline(never)]
    fn widen(&mut self, row: u16, at: usize, cursor: usize) {
        if self.mode(Mode::Insert) {
            let blank = self.blank_style();
            self.with_grid(|g, _, v| g.edit_cells(1, true, blank, v));
        }
        let style = self.pen_style();
        let end = u16::try_from(cursor).unwrap_or(u16::MAX).saturating_add(1);
        self.with_grid(|g, _, v| {
            g.mutate_line(row, v, end, |line| {
                if let Some(cell) = line.cells.get_mut(at) {
                    cell.widen();
                }
                // The cell under the cursor becomes the second half; if it
                // led a wide glyph, that glyph's half is left blank.
                if line.cells.get(cursor).is_some_and(Compact::is_wide)
                    && let Some(orphan) = cursor.checked_add(1).and_then(|i| line.cells.get_mut(i))
                {
                    *orphan = Compact::blank(style);
                }
                if let Some(cell) = line.cells.get_mut(cursor) {
                    *cell = Compact::continuation();
                }
                true
            });
            g.advance_to(end);
        });
        if self.links_seen {
            self.link_cells(row, cursor..cursor.saturating_add(1), true);
        }
    }

    /// Ends the cluster being printed: the next character starts a cell of
    /// its own. Anything that moves the cursor or edits a row does.
    pub(crate) fn break_cluster(&mut self) {
        self.last_print = None;
    }

    /// Forgets the character REP repeats, as xterm does whenever its parser
    /// returns to the ground state without printing.
    pub(crate) fn forget_repeat(&mut self) {
        self.repeat = None;
    }

    /// REP (ECMA-48 8.3.103): the preceding graphic character printed
    /// `count` more times, as if it had been sent again. Nothing if anything
    /// but printing came after that character, where ECMA-48 leaves REP
    /// undefined, as in xterm.
    ///
    /// Copies past those that fill every row they can reach, on the screen
    /// and in history, change nothing, and whole rows' worth of them are
    /// skipped (`copies_needed`).
    fn repeat(&mut self, count: u16) {
        let Some(c) = self.repeat else {
            return;
        };
        let count = self.copies_needed(c, usize::from(count));
        self.copies(c, count)
    }

    /// How many of `asked` copies of `c` REP must print to leave what all
    /// of them would (`repeat`).
    fn copies_needed(&self, c: char, asked: usize) -> usize {
        let g = self.grid();
        let width = crate::char_width(c).unwrap_or(1).max(1);
        if width > usize::from(g.size().cols()) {
            return asked.min(1);
        }
        if !self.mode(Mode::Autowrap) {
            return asked.min(usize::from(g.size().cols()).saturating_add(1));
        }
        // Below the scroll region the copies go round the cursor's row.
        let below = g.cursor.row() > g.lines.last();
        let line = match below {
            true => usize::from(g.row_end(g.cursor.row()).min(g.columns().end()))
                .saturating_sub(usize::from(g.columns().first())),
            false => usize::from(g.columns().len()),
        };
        // Copies to a row; a wide glyph leaves an odd last column blank.
        let Some(per_row) = line.checked_div(width).filter(|n| *n > 0) else {
            return asked.min(1);
        };
        // History's rows of copies, when a scroll at the top feeds it: at
        // least 32 bytes a row, and no more than a grid may have
        // (THREATS.md B33).
        let feeds = !g.lr() && g.lines.first() == 0 && !below;
        let kept = match feeds {
            true => (g.history_bytes() / 32).min(crate::grid::MAX_ROWS),
            false => 0,
        };
        let lines = usize::from(g.size().rows())
            .saturating_add(kept)
            .saturating_add(1);
        let full = lines.saturating_mul(per_row);
        match asked.checked_sub(full) {
            Some(beyond) => full.saturating_add(beyond.checked_rem(per_row).unwrap_or(0)),
            None => asked,
        }
    }

    /// The copies a REP stopped at the budget left, made first by the
    /// call after it.
    pub(crate) fn resume_repeat(&mut self) {
        if let Some((c, count)) = self.left {
            self.left = None;
            self.copies(c, count);
        }
    }

    /// Whether a REP stopped at the budget left copies to make: another
    /// call is owed.
    #[inline]
    pub(crate) fn left_over(&self) -> bool {
        self.left.is_some()
    }

    /// Prints `count` copies of `c`, REP's, stopping where a wrap scrolled
    /// inside margins past the budget, the copies left kept for the next
    /// call.
    fn copies(&mut self, c: char, mut count: usize) {
        if let Ok(byte) = u8::try_from(c)
            && (0x20..=0x7e).contains(&byte)
        {
            const RUN: usize = 128;
            let run = [byte; RUN];
            while count > 0 {
                if self.stopped {
                    self.left = Some((c, count));
                    return;
                }
                let n = count.min(RUN);
                let taken = self.ascii(run.get(..n).unwrap_or_default());
                if taken == 0 && !self.stopped {
                    break;
                }
                count = count.saturating_sub(taken);
            }
            return;
        }
        let properties = Properties::of(c);
        let insert = self.mode(Mode::Insert);
        while count > 0 {
            if self.stopped {
                self.left = Some((c, count));
                return;
            }
            // In insert mode each copy moves what is there, as `print` does.
            let filled = match insert {
                true => 0,
                false => self.fill(c, properties, count),
            };
            count = match filled.checked_sub(1) {
                Some(_) => count.saturating_sub(filled),
                None => {
                    self.print_as(c, properties);
                    count.saturating_sub(1)
                }
            };
            // Once a self-joining glyph's cluster is full, the rest change
            // nothing and are skipped (THREATS.md B34a).
            if self.last_print.is_some_and(Printed::full) {
                break;
            }
        }
    }

    /// Prints a run of ASCII straight into cells, wrapping at the right
    /// margin itself (`wrap_for`).
    pub(crate) fn ascii(&mut self, mut bytes: &[u8]) -> usize {
        let all = bytes.len();
        // Character sets and insert mode take `print`, a glyph at a time,
        // each a place to look: a glyph can clear a linked cell.
        if self.single.is_some() || self.charsets.graphics() || self.mode(Mode::Insert) {
            for (i, &byte) in bytes.iter().enumerate() {
                if self.stopped {
                    return i;
                }
                self.print(char::from(byte));
                self.check_budget();
            }
            return all;
        }
        // The first joins a Prepend character printed last (UAX #29, GB9b).
        if self.last_print.is_some_and(|p| p.cluster.prepended())
            && let Some((&first, tail)) = bytes.split_first()
        {
            self.print(char::from(first));
            bytes = tail;
        }
        let taken = |bytes: &[u8]| all.saturating_sub(bytes.len());
        while let Some((&first, tail)) = bytes.split_first() {
            if self.stopped {
                return taken(bytes);
            }
            let Some(line) = self.wrap_for(1) else {
                return all;
            };
            // A wrap that scrolled past the budget: the glyph it was for is
            // written, and the rest left for the parser.
            if self.stopped {
                self.print(char::from(first));
                return taken(tail);
            }
            let (row, col) = self.grid().cursor.at();
            let Some(room) = line.checked_sub(col).filter(|r| *r > 0) else {
                return all;
            };
            // A run too long for a u16 still stops at the margin.
            let count = u16::try_from(bytes.len()).map_or(room, |n| n.min(room));
            let style = self.pen_style() | self.protect;
            let run = bytes.get(..usize::from(count)).unwrap_or_default();
            self.with_grid(|g, _, v| g.write_ascii(row, col, run, style, v));
            let end = col.saturating_add(count);
            self.grid_mut().advance_within(end, line);
            if self.links_seen {
                self.link_cells(row, usize::from(col)..usize::from(end), true);
            }
            // The run's last glyph, which a mark may join and REP repeats.
            if let Some(&last) = run.last() {
                self.last_print =
                    Some(Printed::new((row, end.saturating_sub(1)), char::from(last)));
                self.repeat = Some(char::from(last));
            }
            bytes = bytes.get(run.len()..).unwrap_or_default();
        }
        all
    }

    /// CR then LF, as `control` carries out the one and then the other:
    /// the pair that ends most lines, in one call.
    pub(crate) fn carriage_return_line_feed(&mut self) {
        self.break_cluster();
        self.cancel_single_shift();
        let new_line = self.mode(Mode::NewLine);
        let g = self.grid_mut();
        g.cursor.pending_wrap = false;
        g.carriage_return();
        match new_line {
            true => self.new_line(),
            false => self.linefeed(),
        }
    }

    pub(crate) fn control(&mut self, byte: u8) {
        if (8..=13).contains(&byte) {
            self.break_cluster();
        }
        // NUL and DEL are "Ignored on input (not stored in input buffer)"
        // (DEC STD 070, 5.9.1): no function, as SO and SI cancel none.
        if !matches!(byte, 0x00 | 0x0e | 0x0f | 0x7f) {
            self.cancel_single_shift();
        }
        if byte == 8 && self.reverse_wraps() {
            let pending = self.grid().cursor.pending_wrap;
            self.cursor_back(1, pending);
            return;
        }
        let new_line = self.mode(Mode::NewLine);
        let g = self.grid_mut();
        // BS, LF, VT, FF and CR end a pending wrap (DEC STD 070, Appendix
        // D.6.1); HT too, in `tab`.
        if matches!(byte, 8 | 13) {
            g.cursor.pending_wrap = false;
        }
        match byte {
            8 => g.backward(1),
            9 => self.forward_tab(1),
            10..=12 if new_line => self.new_line(),
            10..=12 => self.linefeed(),
            13 => g.carriage_return(),
            // SO puts G1 in GL, SI G0.
            14 => self.charsets.gl = Set::G1,
            15 => self.charsets.gl = Set::G0,
            0x1a => self.substitute(),
            _ => {}
        }
    }
    /// Moves the cursor `count` tab stops forward or back. Forward stops at
    /// the right margin, but from right of it or from a line outside the
    /// scrolling region at the last column (DEC STD 070, HT, note 1). Back,
    /// in origin mode, stops at the left margin. The work is the line's
    /// width at most, whatever the count.
    ///
    /// It ends a pending wrap (DEC STD 070, Appendix D.6.1, lists HT; xterm
    /// keeps it).
    fn tab(&mut self, count: u16, forward: bool) {
        let g = self.grid();
        let (mut col, mut last) = (g.cursor.col(), g.columns().last());
        if col > last || !g.lines.contains(g.cursor.row()) {
            last = g.size().columns().last();
        }
        let first = g.addressed().1.first();
        let from = col;
        for _ in 0..count {
            let to = if forward {
                self.terminal.tabs.next(col, last)
            } else {
                self.terminal.tabs.previous(col, first)
            };
            if to == col {
                break;
            }
            col = to;
        }
        // Each column the stops were looked for in.
        crate::budget::spend(col.abs_diff(from));
        self.check_budget();
        let g = self.grid_mut();
        g.set_col(col);
        g.fit_cursor();
        g.cursor.pending_wrap = false;
    }
    /// HT and CHT: `tab` forward, after the wrap mode 41 carries out
    /// (`more_fix`).
    #[inline(never)]
    fn forward_tab(&mut self, count: u16) {
        if self.mode(Mode::MoreFix) && self.grid().cursor.pending_wrap {
            self.more_fix();
        }
        self.tab(count, true);
    }
    /// `tab`'s wrap carried out with mode 41, with autowrap on.
    #[cold]
    #[inline(never)]
    fn more_fix(&mut self) {
        if self.mode(Mode::Autowrap) {
            self.linefeed();
            self.grid_mut().set_col(0);
        }
    }
    /// Whether BS and CUB wrap back: reverse wraparound, either kind, with
    /// DECAWM, as xterm has it.
    fn reverse_wraps(&self) -> bool {
        self.mode(Mode::Autowrap)
            && (self.mode(Mode::ReverseWrap) || self.mode(Mode::ExtendedReverseWrap))
    }

    /// BS and CUB with reverse wraparound, as xterm 411 moves (`CursorBack`;
    /// ctlseqs, `CSI ? 45 h` and `CSI ? 1045 h`): back `count` columns,
    /// going on from the last column of the line before. With 45 that is
    /// only a soft-wrapped row above; with 1045 any row above, and from the
    /// top margin the bottom margin. A cursor waiting to wrap counts as one
    /// past the last column. Where xterm 411 crashes or leaves the screen,
    /// the cursor stops at the first row, as Ghostty's does.
    fn cursor_back(&mut self, count: u16, pending: bool) {
        let extended = self.mode(Mode::ExtendedReverseWrap);
        let g = self.grid_mut();
        g.cursor.pending_wrap = false;
        let mut count = count.saturating_sub(u16::from(pending));
        let (lines, columns) = (g.lines, g.columns());
        let left = columns.first();
        let first = if g.cursor.col() < left { 0 } else { left };
        while count > 0 {
            let moved = g.cursor.col().saturating_sub(first).min(count);
            g.set_col(g.cursor.col().saturating_sub(moved));
            count = count.saturating_sub(moved);
            if count == 0 {
                break;
            }
            let row = g.cursor.row();
            let before = if extended {
                if row == lines.first() {
                    Some(lines.last())
                } else {
                    row.checked_sub(1)
                }
            } else {
                row.checked_sub(1).filter(|above| g.live_wrapped(*above))
            };
            let Some(before) = before else {
                break;
            };
            // A row stepped: under 1045 a CUB can go round the region.
            crate::budget::spend(1);
            g.set_row(before);
            g.set_col(columns.last());
            g.fit_cursor();
            count = count.saturating_sub(1);
        }
        // BS is a control, which looks only where it spends.
        self.check_budget();
    }

    /// DECSC, into the grid shown (`Saved`).
    fn save(&mut self) {
        let (attributes, charsets, protect) = (self.attributes, self.charsets, self.protect);
        let pending_wrap = self.last_column_flag();
        let g = self.grid_mut();
        let mut cursor = g.cursor;
        cursor.pending_wrap = pending_wrap;
        g.saved = Saved {
            cursor,
            attributes,
            charsets,
            protect,
        };
    }
    /// DECRC. With origin mode restored, the cursor comes back inside the
    /// margins, all four (DEC STD 070, DECRC, note 3 and its algorithm).
    fn restore(&mut self) {
        let g = self.grid_mut();
        let saved = g.saved;
        g.cursor = saved.cursor;
        g.cursor.restored = saved.cursor.pending_wrap;
        let (lines, columns) = g.addressed();
        g.set_row(g.cursor.row().clamp(lines.first(), lines.last()));
        g.set_col(g.cursor.col().clamp(columns.first(), columns.last()));
        g.fit_cursor();
        self.attributes = saved.attributes;
        self.pen_changed();
        self.charsets = saved.charsets;
        self.protect = saved.protect;
    }
    /// Carries out an escape sequence; whether fux-vt implements it.
    pub(crate) fn escape(&mut self, intermediates: &[u8], byte: u8) -> bool {
        // SCS (DEC STD 070, SCS): ESC ( F designates G0, ESC ) F G1, ESC * F
        // G2 and ESC + F G3; F `0` is DEC Special Graphics, `<` (the
        // user-preferred supplemental set) and `% 5` DEC Supplemental, any
        // other set ctlseqs lists is ASCII here, and no set is ignored.
        match intermediates {
            [g @ (b'(' | b')' | b'*' | b'+'), set @ ..] => match Designated::of(set, byte) {
                Some(kind) => self.charsets.designate(Set::of(*g), kind),
                None => return false,
            },
            b"#" if byte == b'8' => {
                self.alignment();
                return true;
            }
            b"#" => return self.set_line_rendition(byte),
            // S7C1T and S8C1T (DEC STD 070, 1.7.2): replies stay 7-bit, as
            // UTF-8 has no C1 bytes (ECMA-35, 15.4).
            b" " if matches!(byte, b'F' | b'G') => return true,
            [] => {}
            _ => return false,
        }
        if !intermediates.is_empty() {
            return true;
        }
        if matches!(byte, b'6' | b'7' | b'8' | b'9' | b'D' | b'E' | b'M' | b'c') {
            self.break_cluster();
        }
        match byte {
            b'6' | b'9' => self.index_column(byte == b'9'),
            b'7' => self.save(),
            b'8' => self.restore(),
            // LS2 and LS3 (ECMA-35, 9.3.1): G2 or G3 into GL; SS2 and SS3
            // (9.4): either for the next graphic character alone.
            b'n' => self.charsets.gl = Set::G2,
            b'o' => self.charsets.gl = Set::G3,
            b'N' => self.single = Some(Set::G2),
            b'O' => self.single = Some(Set::G3),
            // LS1R, LS2R and LS3R (ECMA-35, 9.3.2): GR, which no byte
            // reaches in UTF-8 (15.4), so no state is kept.
            b'~' | b'}' | b'|' => {}
            b'=' => self.modes.set(Mode::ApplicationKeypad, true),
            b'>' => self.modes.set(Mode::ApplicationKeypad, false),
            b'D' => self.linefeed(),
            b'E' => self.new_line(),
            // SPA and EPA (ECMA-48 8.3.140, 8.3.49): a guarded area, which
            // ED, EL and ECH leave.
            b'V' | b'W' => {
                let guarded = byte == b'V';
                self.guard = if guarded { TABLED } else { 0 };
                self.protected |= guarded;
                self.pen_changed();
            }
            // HTS (ECMA-48 8.3.62): a tab stop at the cursor's column.
            b'H' => {
                let col = self.grid().cursor.col();
                self.terminal.tabs.set(col, true);
            }
            b'M' => self.reverse_index(),
            // RIS: it resets synchronized output too, which
            // `parser::FrameScan::ends` restates.
            b'c' => {
                let size = self.size();
                let mut next = self.next_id;
                // Both grids start again: in their storage if they can,
                // else afresh, else cleared in place (`Grid::clear`).
                let recycle = [&self.primary, &self.alternate]
                    .iter()
                    .all(|g| g.recyclable() && g.size() == size);
                let history = self.primary.history_bytes();
                let fresh = (!recycle)
                    .then(|| {
                        let primary = Grid::new(size, history, &mut next, self.version).ok()?;
                        Some((primary, Grid::unmade(size, &mut next, self.version).ok()?))
                    })
                    .flatten();
                if let Some((primary, alternate)) = fresh {
                    self.primary = primary;
                    self.alternate = alternate;
                } else {
                    // A grid that cannot be blanked stays; the rest resets.
                    for g in [&mut self.primary, &mut self.alternate] {
                        if g.clear(&mut next, self.version).is_err() {
                            crate::degraded();
                        }
                    }
                }
                self.next_id = next;
                // No cell has a link now, and none is open.
                self.link = None;
                self.links_seen = false;
                self.primary.reset_links();
                self.alternate.reset_links();
                self.pictures.clear();
                self.alternate_active = false;
                // What DECSTR resets; the grids are new already.
                self.terminal = Terminal::default();
                self.soft_reset();
                self.modes = Modes::DEFAULT;
                self.protected = false;
                self.structural = self.version;
            }
            // ST (ECMA-48 8.3.143) is done too: it ended the string before
            // it, which the parser has dispatched, or ends nothing.
            _ => return byte == b'\\',
        }
        true
    }

    /// DECSWL (`ESC # 5`), DECDWL (`ESC # 6`) and DECDHL (`ESC # 3`, `ESC #
    /// 4`): the cursor's line takes the rendition. DECDWL and DECDHL do
    /// nothing while DECLRMM is set (DEC STD 070, DECDWL, note 3). Whether
    /// the sequence is one of the four.
    fn set_line_rendition(&mut self, byte: u8) -> bool {
        let rendition = match byte {
            b'3' => LineRendition::DoubleHeightTop,
            b'4' => LineRendition::DoubleHeightBottom,
            b'5' => LineRendition::Single,
            b'6' => LineRendition::DoubleWidth,
            _ => return false,
        };
        if rendition.is_double() && self.mode(Mode::LeftRightMargins) {
            return true;
        }
        self.break_cluster();
        let version = self.version;
        let g = self.grid_mut();
        g.cursor.pending_wrap = false;
        let row = g.cursor.row();
        g.set_rendition(row, rendition, version);
        true
    }

    /// DECIC (`insert`) and DECDC: `count` columns inserted or deleted at
    /// the cursor's column in every line of the scrolling region, between
    /// the left and right margins (DEC STD 070, 5.4.3). Nothing outside
    /// the margins. The cursor and a pending wrap stay, as in xterm.
    #[inline(never)]
    fn edit_columns(&mut self, count: u16, insert: bool) {
        let g = self.grid();
        if !g.lines.contains(g.cursor.row()) || !g.in_columns() {
            return;
        }
        self.shift_columns(g.cursor.col(), count, insert);
    }

    /// DECBI (`ESC 6`) and DECFI (`forward`, `ESC 9`; DEC STD 070, Back
    /// Index and Forward Index): the cursor a column back or forward. At
    /// the margin it would cross, the columns between the margins shift
    /// the other way instead, and the cursor stays.
    #[inline(never)]
    fn index_column(&mut self, forward: bool) {
        let g = self.grid_mut();
        g.cursor.pending_wrap = false;
        let (col, columns) = (g.cursor.col(), g.columns());
        let margin = if forward {
            columns.last()
        } else {
            columns.first()
        };
        if col == margin {
            self.shift_columns(columns.first(), 1, !forward);
        } else if forward {
            g.set_col(col.saturating_add(1));
            g.fit_cursor();
        } else {
            g.set_col(col.saturating_sub(1));
        }
    }

    /// The columns from `from` to the right margin of every line of the
    /// scrolling region moved `count` right, or left; the cursor stays.
    fn shift_columns(&mut self, from: u16, count: u16, right: bool) {
        let blank = self.blank_style();
        self.with_grid(|g, _, version| {
            // A unit a row, as a double-width one it leaves is still read.
            crate::budget::spend(g.lines.len());
            for y in g.lines.first()..=g.lines.last() {
                g.edit_row(y, from, count, right, blank, version);
            }
        });
    }

    /// DECSLRM (`CSI Pl ; Pr s`, DEC STD 070, 5-27), while DECLRMM is set:
    /// margins with the left one left of the right are set, and the cursor
    /// goes home, obeying DECOM; others are ignored (note 2). A right
    /// margin past the screen is its last column, as xterm takes it.
    #[inline(never)]
    fn set_left_right_margins(&mut self, p: &Parameters) {
        let cols = self.grid().size().cols();
        let right = p.first(1, cols).saturating_sub(1);
        let left = p.first(0, 1).saturating_sub(1);
        if let Some(columns) = self.grid().size().columns().margins(left, right) {
            let g = self.grid_mut();
            g.set_columns(columns);
            g.position(0, 0);
        }
    }

    /// ECH while glyphs may be protected: the columns `start` to `end` of
    /// row `row` erased but for their guarded glyphs (ISO's).
    #[inline(never)]
    fn erase_kept(&mut self, row: u16, start: u16, end: u16) {
        let blank = self.blank_style();
        self.with_grid(|g, _, v| g.erase_unprotected(row, start, end, blank, v));
    }

    /// ED or EL (`display`) while glyphs may be protected, or DECSED or
    /// DECSEL (`private`), in `mode`. DECSED and DECSEL make each glyph
    /// DECSCA does not protect a space in its own attributes, and leave
    /// empty positions empty (DEC STD 070, 5.11.1.2). ED and EL leave the
    /// guarded glyphs (ECMA-48, SPA with ERM reset). An erase that finds no
    /// protected glyph leaves the protection as it is (DEC STD 070,
    /// 5.11.1.2; ECMA-48 8.3.140), where xterm ends it.
    #[inline(never)]
    fn erase_protected(&mut self, private: bool, display: bool, part: Erase) {
        let (blank, version, cols) = (self.blank_style(), self.version, self.size().cols());
        // DECSED leaves every line's rendition (DEC STD 070, DECSED, note
        // 3); ED makes single width (ED, note 1) each line it spared no
        // glyph of.
        self.grid_mut()
            .erase_in(display, part, version, |g, y, start, end| {
                let reset = if private {
                    g.erase_to_spaces(y, (start, end), None, version);
                    if end >= cols {
                        g.wrap(y, false, version);
                    }
                    !g.rendition(y).is_double()
                } else {
                    g.erase_unprotected(y, start, end, blank, version);
                    !(display && g.rendition(y).is_double() && {
                        crate::budget::spend(cols);
                        let styles = g.styles();
                        g.live_cells(y).iter().any(|c| styles.guarded(c.style()))
                    })
                };
                let whole = display && start == 0 && end >= g.row_end(y);
                if !reset && whole && g.rendition(y).is_double() {
                    // A kept doubled line still loses its tail style and wrap.
                    g.erase(y, g.row_end(y), cols, 0, version);
                }
                reset
            });
    }

    /// DECALN (`ESC # 8`; DEC STD 070, Appendix D, Screen Alignment; VT520
    /// manual, 5-17): the screen filled with E, the margins the whole
    /// screen, origin mode off, the cursor home with no wrap pending, and
    /// the pen's rendition off, as DEC STD 070's algorithm has it. As in
    /// xterm 411, the pen keeps its colours, and the E's have neither
    /// colours nor attributes. Each row's soft wrap ends, as in Ghostty, and
    /// its prompt mark goes.
    fn alignment(&mut self) {
        self.break_cluster();
        self.attributes = self.attributes.erased();
        self.pen_changed();
        let version = self.version;
        let g = self.grid_mut();
        // Home, and the side margins reset as xterm does; DECLRMM stays.
        g.cursor = Cursor::default();
        g.reset_margins();
        // The default attributes, style 0 (`style.rs`).
        let plain = 0;
        let cols = g.size().cols();
        for y in 0..g.size().rows() {
            let filled = !g.live_row(y).is_some_and(|r| r.has_links())
                && g.live_cells(y).iter().all(|c| c.is_ascii(b'E', plain));
            crate::budget::spend(cols);
            if !filled {
                // Erased first, so its clusters' text and links go.
                g.erase(y, 0, cols, plain, version);
                g.mutate_row(y, version, cols, |cells| {
                    cells.fill(Compact::ascii(b'E', plain));
                    true
                });
            }
            g.wrap(y, false, version);
            g.set_prompt(y, false, version);
        }
        // Every line single width (DEC STD 070, DECALN, its algorithm).
        g.single_width(version);
    }

    /// DECSTR (`soft`), or DECSCL (`CSI Pl ; Pc " p`; DEC STD 070, DECSCL):
    /// level 1 or 2, with a soft reset (note 3); another level is ignored
    /// (note 4). Replies stay 7-bit whatever Pc asks, as UTF-8 has no C1
    /// bytes (ECMA-35, 15.4).
    #[inline(never)]
    fn soft_reset_or_level(&mut self, p: &Parameters, soft: bool) {
        if !soft {
            match p.first(0, 0) {
                61 => self.terminal.level = Level::One,
                62 => self.terminal.level = Level::Two,
                _ => return,
            }
        }
        self.soft_reset();
    }

    /// DECSTR (`CSI ! p`), as xterm does it: what the VT520 manual's table
    /// (p. 5-150) and DEC STD 070's Soft Terminal Reset (p. 4-37) list goes
    /// back to its default (`Modes::SOFT_RESET` has the modes), with the
    /// pen, the character sets, the margins and the shown screen's saved
    /// cursor. The screen, the cursor, a pending wrap, the alternate
    /// screen, mouse modes and kitty keyboard flags stay, as in xterm. It
    /// resets synchronized output (2026), which `parser::FrameScan::ends`
    /// restates: the two change together.
    fn soft_reset(&mut self) {
        // DECAWM comes back on with no wrap pending if it was off, but
        // the Last Column Flag DECRC restored.
        if !self.last_column_flag() {
            self.grid_mut().cursor.pending_wrap = false;
        }
        self.modes.reset(Modes::SOFT_RESET);
        // DECSCA (VT520 manual's table) and SPA's area, as xterm ends it.
        self.protect = 0;
        self.guard = 0;
        self.attributes = Attributes::default();
        self.pen_changed();
        self.charsets = Charsets::at(self.terminal.level);
        // The left and right margins, in the VT520 manual's table too.
        for g in [&mut self.primary, &mut self.alternate] {
            g.cursor.origin = false;
            g.reset_margins();
        }
        self.grid_mut().saved = Saved::default();
        // The palette, as xterm 411 resets it; dynamic colours stay.
        if let Some(colours) = &mut self.colours {
            colours.reset_palette();
        }
    }

    /// Clears the alternate screen in `pen`'s colours (`bce`), as xterm
    /// does for 1047 and 1049. The cursor, margins and saved cursor stay;
    /// a pending wrap ends, as ED's does.
    fn clear_alternate(&mut self, pen: Attributes) -> Result<(), Error> {
        let g = &mut self.alternate;
        let (cursor, saved, lines, columns) = (g.cursor, g.saved, g.lines, g.columns());
        let keyboard = mem::take(&mut g.keyboard);
        self.alternate.clear(&mut self.next_id, self.version)?;
        let blank = self.alternate.style(pen.erased());
        let g = &mut self.alternate;
        (g.cursor, g.saved, g.lines, g.keyboard) = (cursor, saved, lines, keyboard);
        g.set_columns(columns);
        g.cursor.pending_wrap = false;
        if blank != 0 {
            let cols = g.size().cols();
            for y in 0..g.size().rows() {
                g.erase(y, 0, cols, blank, self.version);
            }
        }
        Ok(())
    }

    /// Shows the alternate screen, or the primary. The cursor and margins
    /// go along, as in xterm, where they are the terminal's rather than
    /// either screen's; each screen keeps its own saved cursor.
    fn switch_screen(&mut self, alternate: bool) {
        if self.alternate_active != alternate {
            let (from, to) = if alternate {
                (&self.primary, &mut self.alternate)
            } else {
                (&self.alternate, &mut self.primary)
            };
            (to.cursor, to.lines) = (from.cursor, from.lines);
            to.set_columns(from.columns());
            to.fit_cursor();
        }
        self.alternate_active = alternate;
        self.structural = self.version;
    }

    /// DECRQCRA (`CSI Pi ; Pp ; Pt ; Pl ; Pb ; Pr * y`, VT420 and up), with
    /// [`Feature::RectangleChecksums`]: the checksum of the rectangle's
    /// cells, as DECCKSR (`DCS Pi ! ~ xxxx ST`, four upper-case hex
    /// digits). The sum is xterm's default, which matches the VT520's
    /// (`xtermCheckRect`, ctlseqs' XTCHECKSUM): each cell's character, plus
    /// 0x08 hidden, 0x10 underlined, 0x20 inverse, 0x40 blinking and 0x80
    /// bold, summed in 16 bits and negated. A character past Latin-1 or
    /// below a space, a picture's cell (a C1 control, `PLACEHOLDER`) and a
    /// wide glyph's second half count as ESC, but for
    /// a DEC Special Graphics or Supplemental glyph (`dec_code`); a
    /// cluster's combining marks are added as they are, as xterm counts
    /// them. An empty cell counts as a space, as on DEC's terminals. The
    /// page `Pp` is ignored, as xterm ignores it. The rectangle is read as
    /// `rectangle` reads it; an inverted one has no reply (DEC STD 070,
    /// DECRQCRA).
    pub(crate) fn rectangle_checksum(&mut self, p: &Parameters) -> Option<Reply> {
        let (lines, columns) = self.rectangle(p, 2)?;
        let id = p.first(0, 0);
        let g = self.grid();
        let mut sum = 0u16;
        crate::budget::spend(lines.len());
        for y in lines.each() {
            // A double-width line holds half the columns (DEC STD 070,
            // 5.6.1): the rectangle stops at its end.
            let row = columns.each().take_while(|x| *x < g.row_end(y));
            for cell in row.filter_map(|x| g.cell(y, x)) {
                // The cell, and its text read.
                crate::budget::spend(cell.contents().len().saturating_add(1));
                let mut chars = cell.contents().chars();
                let base = match chars.next() {
                    None if cell.is_wide_continuation() => ESC,
                    None => 0x20,
                    Some(c) => match u16::try_from(u32::from(c)) {
                        Ok(code @ (0x20..=0x7f | 0xa0..=0xff)) => code,
                        _ => dec_code(c).map_or(ESC, u16::from),
                    },
                };
                let marks = chars.fold(0u16, |sum, c| {
                    sum.wrapping_add(u16::try_from(u32::from(c) & 0xffff).unwrap_or(0))
                });
                sum = sum
                    .wrapping_add(base)
                    .wrapping_add(marks)
                    .wrapping_add(rendition_of(cell.attributes()));
            }
        }
        let sum = sum.wrapping_neg();
        Some(reply!("\x1bP{id}!~{sum:04X}\x1b\\"))
    }

    /// The rectangle `Pt ; Pl ; Pb ; Pr` of the parameters from `at` on, as
    /// the rectangular area operations and DECRQCRA take it (DEC STD 070,
    /// 5.12.1, 5.12.2): one-based, from the top and left margins in origin
    /// mode but otherwise not bound by them, and clamped to the page;
    /// absent or 0 its whole extent. `None` if its top is below its bottom
    /// or its left past its right, which has the function ignored.
    fn rectangle(&self, p: &Parameters, at: usize) -> Option<(Span, Span)> {
        let size = self.size();
        let (top, left) = self.page_position(p, at, (1, 1));
        let (bottom, right) = self.page_position(p, at.saturating_add(2), (u16::MAX, u16::MAX));
        Some((
            size.lines().part(top, bottom)?,
            size.columns().part(left, right)?,
        ))
    }

    /// The line and column the parameters at `at` and `at + 1` name, read
    /// as `rectangle` reads a corner; `default` if absent or 0.
    fn page_position(&self, p: &Parameters, at: usize, default: (u16, u16)) -> (u16, u16) {
        let (lines, columns) = self.grid().addressed();
        let (page_lines, page_columns) = (self.size().lines(), self.size().columns());
        let on = |page: Span, from: Span, i: usize, default: u16| {
            let n = p.first(at.saturating_add(i), default).saturating_sub(1);
            page.nth(from.first().saturating_add(n))
        };
        (
            on(page_lines, lines, 0, default.0),
            on(page_columns, columns, 1, default.1),
        )
    }

    /// DEC STD 070's rectangular area operations (5.12.1), by final byte:
    /// DECFRA (`x`), DECERA (`z`), DECSERA (`{`) and DECCRA (`v`), on the
    /// rectangle their parameters name; the cursor stays.
    #[inline(never)]
    fn rectangle_area(&mut self, p: &Parameters, byte: u8) {
        // DECFRA's rectangle follows its character.
        let Some((lines, columns)) = self.rectangle(p, usize::from(byte == b'x')) else {
            return;
        };
        self.break_cluster();
        // Stopping at a double-width line's end (DEC STD 070, 5.6.1).
        let span = |g: &Grid, y: u16| {
            let end = columns.end().min(g.row_end(y)).max(columns.first());
            (columns.first(), end)
        };
        match byte {
            // DECFRA: Pch 32 to 126 in GL's set, 160 to 255 Latin-1;
            // another is ignored.
            b'x' => {
                let c = match u8::try_from(p.first(0, 32)) {
                    Ok(byte @ 32..=126) => self.charsets.map(self.charsets.gl, char::from(byte)),
                    Ok(byte @ 160..=255) => char::from(byte),
                    _ => return,
                };
                let style = self.pen_style() | self.protect;
                self.each_row(lines, |g, y, v| g.fill(y, span(g, y), c, style, v));
            }
            // DECERA: blanks in the pen's colours, as ED's are.
            b'z' => {
                let blank = self.blank_style();
                self.each_row(lines, |g, y, v| {
                    g.erase_to_spaces(y, span(g, y), Some(blank), v);
                });
            }
            // DECSERA: unprotected glyphs made spaces in their attributes.
            b'{' => self.each_row(lines, |g, y, v| g.erase_to_spaces(y, span(g, y), None, v)),
            // DECCRA: to `Ptd ; Pld`, addressed as the rectangle is.
            _ => {
                let to = self.page_position(p, 5, (1, 1));
                self.with_grid(|g, _, v| g.copy_rectangle(lines, columns, to, v));
            }
        }
    }

    /// DECCARA (`CSI Pt ; Pl ; Pb ; Pr ; Ps … $ r`) and DECRARA (`… $ t`,
    /// `reverse`; DEC STD 070, 5.12.1): the renditions the parameters name
    /// (`rendition_change`) changed, or reversed, in the area: the
    /// rectangle with DECSACE 2, else the stream of positions from its
    /// first corner to its second.
    #[inline(never)]
    fn change_attributes(&mut self, p: &Parameters, reverse: bool) {
        let Some((lines, columns)) = self.rectangle(p, 0) else {
            return;
        };
        let valid: &[u16] = if reverse {
            &[0, 1, 4, 5, 7]
        } else {
            &[
                0, 1, 4, 5, 7, 22, 24, 25, 27, 30, 31, 32, 33, 34, 35, 36, 37, 39, 40, 41, 42, 43,
                44, 45, 46, 47, 49,
            ]
        };
        let mut ops = [0u16; 32];
        let mut count = 0usize;
        for group in p.groups().skip(4) {
            if let [n] = group
                && valid.contains(n)
                && let Some(op) = ops.get_mut(count)
            {
                *op = *n;
                count = count.saturating_add(1);
            }
        }
        // With no valid parameter DECCARA clears (0), DECRARA is ignored.
        if count == 0 && reverse {
            return;
        }
        let ops = ops.get(..count.max(1)).unwrap_or_default();
        self.break_cluster();
        let rectangle = self.terminal.extent == 2;
        let cols = self.size().cols();
        let (top, bottom) = (lines.first(), lines.last());
        let span = |y: u16| match rectangle {
            true => (columns.first(), columns.end()),
            false => (
                if y == top { columns.first() } else { 0 },
                if y == bottom { columns.end() } else { cols },
            ),
        };
        // The last few styles mapped; a sweep renumbers styles, so it ends.
        let mut memo = [(0u32, 0u32); 16];
        let (mut kept, mut sweeps) = (0usize, None);
        let mut restyle = |g: &mut Grid, old: u32| {
            if sweeps != Some(g.sweeps()) {
                (kept, sweeps) = (0, Some(g.sweeps()));
            }
            let seen = memo.get(..kept.min(memo.len())).unwrap_or_default();
            if let Some(&(_, new)) = seen.iter().find(|(from, _)| *from == old) {
                return new;
            }
            let styles = g.styles();
            let guarded = styles.guarded(old);
            let changed = rendition_change(styles.get(old), ops, reverse).with_guard(guarded);
            let new = g.style(changed);
            if let Some(slot) = memo.get_mut(kept % 16) {
                *slot = (old, new);
            }
            kept = kept.saturating_add(1);
            new
        };
        self.with_grid(|g, _, v| {
            crate::budget::spend(lines.len());
            for y in lines.each() {
                let (start, end) = span(y);
                let end = end.min(g.row_end(y)).max(start);
                g.restyle(y, (start, end), rectangle, v, &mut restyle);
            }
        });
    }

    /// `f` on each of the rows `lines`, each keeping its soft wrap: a
    /// rectangular area operation changes none (DEC STD 070, 5.12.1).
    fn each_row(&mut self, lines: Span, mut f: impl FnMut(&mut Grid, u16, u64)) {
        self.with_grid(|g, _, v| {
            crate::budget::spend(lines.len());
            for y in lines.each() {
                let wrapped = g.live_wrapped(y);
                f(g, y, v);
                g.wrap(y, wrapped, v);
            }
        });
    }

    /// The primary device attributes (DA1 and DECID): a level 2 terminal
    /// with an identity, else a VT100 with advanced video.
    pub(crate) fn primary_attributes(&self, options: &Options) -> Reply {
        // 4: sixel graphics, when the host's terminal shows them.
        let sixel = if self.sixel() { ";4" } else { "" };
        if options.identity().is_some() {
            // Level 2 (DEC STD 070, DA, Table 1): selective erase (6),
            // horizontal scrolling (21), colour text (22) and rectangular
            // editing (28).
            reply!("\x1b[?62{sixel};6;21;22;28c")
        } else {
            reply!("\x1b[?1;2{sixel}c")
        }
    }

    /// SM and RM, DECSET and DECRST (`on` for `h`), and XTRESTORE
    /// (`on` `None`: each mode as XTSAVE saved it, but 1048 and DECARM,
    /// which it does not save). Of the ANSI modes, a sequence that names
    /// none fux-vt keeps is unhandled.
    #[inline(always)]
    fn set_modes(
        &mut self,
        p: &Parameters,
        private: bool,
        on: Option<bool>,
        options: &Options,
    ) -> Dispatch {
        let (mut handled, mut report) = (private, false);
        for group in p.groups() {
            let [n] = group else { continue };
            let Some(mode) = Mode::of(*n, private, options) else {
                continue;
            };
            handled = true;
            let on = match on {
                Some(on) => on,
                None if mode.fixed().is_none() => self.saved_modes.contains(mode),
                None => continue,
            };
            // A mode refused room is left as it was; the rest are set.
            match self.set_mode(mode, on) {
                Ok(reports) => report |= reports,
                Err(_) => crate::degraded(),
            }
        }
        match (handled, report) {
            (_, true) => Dispatch::Reply(self.size_report()),
            (true, false) => Dispatch::Done,
            (false, false) => Dispatch::Unhandled,
        }
    }

    /// XTSAVE (`CSI ? Pm s`, with `save`): each mode saved as it is; and
    /// XTRESTORE (`CSI ? Pm r`): each set as it was saved.
    #[inline(never)]
    fn save_or_restore_modes(&mut self, p: &Parameters, save: bool, options: &Options) -> Dispatch {
        if !save {
            return self.set_modes(p, true, None, options);
        }
        for group in p.groups() {
            if let [n] = group
                && let Some(mode) = Mode::of(*n, true, options).filter(|m| m.fixed().is_none())
            {
                self.saved_modes.set(mode, self.mode(mode));
            }
        }
        Dispatch::Done
    }

    /// Sets or resets `mode`; whether a size report is due (setting
    /// in-band resize reports at once, however often it is set).
    #[inline(never)]
    fn set_mode(&mut self, mode: Mode, on: bool) -> Result<bool, Error> {
        match mode.kind() {
            Kind::Flag => {
                // Resetting DECAWM ends a pending wrap (DEC STD 070,
                // Appendix D.6.1), and setting it again starts with none
                // but the Last Column Flag DECRC restored.
                if mode == Mode::Autowrap && !(on && self.last_column_flag()) {
                    self.grid_mut().cursor.pending_wrap = false;
                }
                self.modes.set(mode, on);
            }
            // DECLRMM (DEC STD 070, 5-29): reset, the margins go back to the
            // screen's edges; set, every line is single width.
            Kind::Margins => {
                self.modes.set(mode, on);
                let version = self.version;
                for g in [&mut self.primary, &mut self.alternate] {
                    if on {
                        g.single_width(version);
                    } else {
                        g.set_columns(g.size().columns());
                    }
                }
            }
            // Which sequences reset it, `parser::FrameScan::ends` restates:
            // it changes with this arm, DECSTR, RIS and XTRESTORE.
            Kind::Frame => {
                // A frame begins only when the mode is set from reset.
                if on && !self.modes.contains(mode) {
                    self.frames_begun = self.frames_begun.wrapping_add(1);
                }
                self.modes.set(mode, on);
            }
            Kind::SizeReport => {
                self.modes.set(mode, on);
                return Ok(on);
            }
            Kind::Origin => {
                let g = self.grid_mut();
                g.cursor.origin = on;
                g.position(0, 0);
            }
            // The alternate screen is made before anything changes, so that
            // failing to make it changes nothing.
            Kind::Screen(switch) => {
                if on {
                    self.alternate.make()?;
                }
                self.break_cluster();
                match switch {
                    Switch::Plain => self.switch_screen(on),
                    // Cleared on leaving, as xterm does it, but only once
                    // shown again: till then, what the program drew is
                    // kept to read.
                    Switch::ClearedOnLeaving => {
                        if !on && self.alternate_active {
                            self.terminal.clear_on_show = Some(self.attributes);
                            self.alternate.cursor.pending_wrap = false;
                        }
                        self.switch_screen(on);
                    }
                    Switch::SavingCursor if on => {
                        self.save();
                        self.switch_screen(true);
                        self.terminal.clear_on_show = None;
                        self.clear_alternate(self.attributes)?;
                    }
                    Switch::SavingCursor => {
                        self.switch_screen(false);
                        self.restore();
                    }
                }
                // The clear 1047 deferred keeps a wrap pending since, as
                // xterm's clear on leaving came before it.
                if let Some(pen) = self.terminal.clear_on_show.take_if(|_| on) {
                    let pending = self.alternate.cursor.pending_wrap;
                    self.clear_alternate(pen)?;
                    self.alternate.cursor.pending_wrap = pending;
                }
            }
            Kind::CursorBlink => {
                let shape = &mut self.terminal.cursor_shape;
                *shape = shape.with_blink(on);
            }
            Kind::SaveCursor if on => self.save(),
            Kind::SaveCursor => self.restore(),
            Kind::Permanent(_) => {}
            Kind::Mouse(mouse) => latest(&mut self.terminal.mouse, mouse, on),
            Kind::Encoding(encoding) => latest(&mut self.terminal.encoding, encoding, on),
        }
        Ok(false)
    }

    /// DECRQSS (`DCS $ q Pt ST`): the setting `request` names, written to
    /// `out` as the control function that sets it, in the form xterm
    /// answers (`do_dcs` and `xtermFormatSGR`, xterm 411 misc.c).
    pub(crate) fn setting_report(&self, request: &[u8], out: &mut String) -> Setting {
        use std::fmt::Write;
        match request {
            b"m" => {
                let mut sgr = b"0".to_vec();
                self.attributes.write_sgr(&mut sgr, SgrForm::Report);
                out.push_str(&String::from_utf8_lossy(&sgr));
                out.push('m');
            }
            b" q" => {
                // 0 as the blinking block it is, as xterm answers it; the
                // terminal's own shape as 7, xterm's "initial resources"
                // (ctlseqs, DECSCUSR), which DECSCUSR 7 restores.
                let ps = self.terminal.cursor_shape.number().max(1);
                out.extend(char::from_digit(ps.into(), 10));
                out.push_str(" q");
            }
            b"r" => {
                // Exact: the margins are rows of a screen a u16 counts.
                let (top, bottom) = self.scroll_region();
                let (top, bottom) = (top.saturating_add(1), bottom.saturating_add(1));
                let _ = write!(out, "{top};{bottom}r");
            }
            // DECSLRM's margins, the screen's edges without DECLRMM.
            b"s" => {
                let (left, right) = self.left_right_margins();
                let (left, right) = (left.saturating_add(1), right.saturating_add(1));
                let _ = write!(out, "{left};{right}s");
            }
            b"*x" => {
                let _ = write!(out, "{}*x", self.terminal.extent);
            }
            // DECSCL's level, and 7-bit C1 replies (1), as fux-vt sends.
            b"\"p" => {
                let level = match self.terminal.level {
                    Level::One => 61,
                    Level::Two => 62,
                };
                let _ = write!(out, "{level};1\"p");
            }
            // DECSCA's selective erase attribute, which SPA is not.
            b"\"q" => {
                let on = self.protect != 0;
                let _ = write!(out, "{}\"q", u8::from(on));
            }
            _ => return Setting::Unknown,
        }
        Setting::Written
    }

    /// The kitty keyboard protocol and modifyOtherKeys sequences, with
    /// [`Feature::KittyKeyboard`]; `None` for any other sequence.
    fn keyboard_protocol(
        &mut self,
        p: &Parameters,
        intermediates: &[u8],
        byte: u8,
    ) -> Option<Dispatch> {
        // Flags fux-vt does not honour are dropped.
        let flags = |n: u16| u8::try_from(n & u16::from(KITTY_FLAGS_KEPT)).unwrap_or(0);
        match (intermediates, byte) {
            (b">", b'u') => self.grid_mut().keyboard.push(flags(p.first(0, 0))),
            (b"<", b'u') => self.grid_mut().keyboard.pop(p.first(0, 1)),
            (b"=", b'u') => {
                let (set, mode) = (flags(p.first(0, 0)), p.first(1, 1));
                self.grid_mut().keyboard.set(set, mode);
            }
            (b"?", b'u') => {
                let flags = self.kitty_keyboard_flags();
                return Some(Dispatch::Reply(reply!("\x1b[?{flags}u")));
            }
            // XTMODKEYS with no parameters resets every resource,
            // modifyOtherKeys too (ctlseqs); 0 alone is modifyKeyboard's.
            (b">", b'm') if !p.given => self.terminal.modify_other_keys = None,
            // Only resource 4 is modifyOtherKeys; other resources are
            // unhandled. A mask of modifiers to factor out, a subparameter
            // of the level (`CSI > 4 ; 2 : 1 m`, xterm 411), is ignored.
            (b">", b'm') if p.groups().next() == Some(&[4][..]) => {
                self.terminal.modify_other_keys = match p.first(1, 0) {
                    1 => Some(OtherKeysLevel::Usual),
                    2 => Some(OtherKeysLevel::All),
                    3 => Some(OtherKeysLevel::Unmodified),
                    // 0 or none is off; ctlseqs knows no level past 3.
                    _ => None,
                };
            }
            // XTQMODKEYS (ctlseqs, `CSI ? Pp m`): answered as XTMODKEYS,
            // 0 when off. Only modifyOtherKeys (4) is kept and answered.
            (b"?", b'm') if p.groups().next() == Some(&[4][..]) => {
                let level = match self.terminal.modify_other_keys {
                    None => 0,
                    Some(OtherKeysLevel::Usual) => 1,
                    Some(OtherKeysLevel::All) => 2,
                    Some(OtherKeysLevel::Unmodified) => 3,
                };
                return Some(Dispatch::Reply(reply!("\x1b[>4;{level}m")));
            }
            // Disables it (ctlseqs, `CSI > Ps n`).
            (b">", b'n') if p.groups().next() == Some(&[4][..]) => {
                self.terminal.modify_other_keys = None;
            }
            _ => return None,
        }
        Some(Dispatch::Done)
    }

    /// The CSI sequences with an intermediate `csi` carries out first:
    /// DECSCUSR, the kitty keyboard protocol's and XTMODKEYS, DECSTR and
    /// DECSCL; `None` for the rest. DECSTR and DECSCL are here, not in
    /// `other_csi`, as the parser asks whether synchronized output ended as
    /// `csi` returns.
    #[inline(never)]
    fn csi_intermediate(
        &mut self,
        p: &Parameters,
        intermediates: &[u8],
        byte: u8,
        options: &Options,
    ) -> Option<Dispatch> {
        if intermediates == b" " && byte == b'q' {
            let ps = p.first(0, 0);
            if let Some(&shape) = CursorShape::ALL.iter().find(|s| s.number() == ps) {
                self.terminal.cursor_shape = shape;
            }
            return Some(Dispatch::Done);
        }
        if options.has(Feature::KittyKeyboard)
            && let Some(dispatch) = self.keyboard_protocol(p, intermediates, byte)
        {
            return Some(dispatch);
        }
        if byte == b'p' && matches!(intermediates, b"!" | b"\"") {
            self.soft_reset_or_level(p, intermediates == b"!");
            return Some(Dispatch::Done);
        }
        None
    }

    pub(crate) fn csi(
        &mut self,
        p: &Parameters,
        intermediates: &[u8],
        byte: u8,
        options: &Options,
    ) -> Dispatch {
        let private = intermediates == b"?";
        // A sequence with an intermediate; of the private ones only the
        // kitty flags' and XTQMODKEYS's queries, not the modes.
        if !intermediates.is_empty() && (!private || matches!(byte, b'u' | b'm')) {
            if let Some(dispatch) = self.csi_intermediate(p, intermediates, byte, options) {
                return dispatch;
            }
            if !private {
                return Dispatch::Unhandled;
            }
        }
        if matches!(byte, b'h' | b'l') {
            return self.set_modes(p, private, Some(byte == b'h'), options);
        }
        // Every sequence that moves the cursor or edits a row.
        if matches!(
            byte,
            b'@'..=b'M' | b'P' | b'S' | b'T' | b'X' | b'Z' | b'`' | b'a' | b'd'..=b'f' | b'r' | b's' | b'u'
        ) {
            self.break_cluster();
        }
        // XTSAVE and XTRESTORE (ctlseqs): the private marker tells them
        // from DECSLRM, SCOSC and DECSTBM.
        if private && matches!(byte, b's' | b'r') {
            return self.save_or_restore_modes(p, byte == b's', options);
        }
        if private && !matches!(byte, b'J' | b'K') {
            return Dispatch::Unhandled;
        }
        let n = p.first(0, 1);
        let (row, col) = self.grid().cursor.at();
        let pending = self.grid().cursor.pending_wrap;
        // Every cursor movement, erase and edit ends a pending wrap (DEC STD
        // 070, Appendix D.6.1); some below, once carried out. SU and SD
        // keep it, as in xterm.
        if matches!(byte, b'A'..=b'H' | b'X' | b'`' | b'a' | b'd' | b'e' | b'f') {
            self.grid_mut().cursor.pending_wrap = false;
        }
        match byte {
            // CUU, CUD, CNL and CPL, each stopping at the margin it comes
            // to, else at the screen's edge (xterm's `CursorUp`). CNL and
            // CPL then return the carriage.
            b'A' | b'B' | b'E' | b'F' => {
                let g = self.grid_mut();
                let lines = g.lines;
                g.set_row(if matches!(byte, b'A' | b'F') {
                    let stop = if row >= lines.first() {
                        lines.first()
                    } else {
                        0
                    };
                    row.saturating_sub(n).max(stop)
                } else if row <= lines.last() {
                    row.saturating_add(n).min(lines.last())
                } else {
                    row.saturating_add(n)
                });
                if matches!(byte, b'E' | b'F') {
                    g.carriage_return();
                }
            }
            // CUF stops at the right margin, unless already past it (DEC
            // STD 070, 5.4.3).
            b'C' => self.grid_mut().forward(n, false),
            // HPR sets a position as CUP does (VT520 manual), so unlike CUF
            // it passes the right margin outside origin mode.
            b'a' => self.grid_mut().forward(n, true),
            // CUB; with reverse wraparound, back over line ends too.
            b'D' if self.reverse_wraps() => self.cursor_back(n, pending),
            // CUB stops at the left margin, unless already left of it.
            b'D' => self.grid_mut().backward(n),
            // CHA and HPA address the column as CUP does.
            b'G' | b'`' => self.grid_mut().column(n.saturating_sub(1)),
            b'H' | b'f' => self
                .grid_mut()
                .position(n.saturating_sub(1), p.first(1, 1).saturating_sub(1)),
            b's' if self.mode(Mode::LeftRightMargins) => self.set_left_right_margins(p),
            // SCOSC and SCORC share DECSC's slot, attributes too.
            b's' => self.save(),
            b'u' => self.restore(),
            // VPA and VPR address lines as CUP does (DEC STD 070, DECOM),
            // so unlike CUD, VPR passes the bottom margin with DECOM reset.
            b'd' | b'e' => {
                let g = self.grid_mut();
                let line = if byte == b'd' {
                    n.saturating_sub(1)
                } else {
                    g.cursor_line().saturating_add(n)
                };
                g.position_line(line);
            }
            b'@' | b'P' => {
                let blank = self.blank_style();
                self.with_grid(|g, _, v| g.edit_cells(n, byte == b'@', blank, v));
            }
            // Erased cells take the pen's colours; guarded glyphs stay.
            b'X' => {
                let a = self.blank_style();
                let end = col.saturating_add(n);
                if self.protected {
                    self.erase_kept(row, col, end);
                } else {
                    self.with_grid(|g, _, v| g.erase(row, col, end, a, v));
                }
            }
            // ED, EL, DECSED and DECSEL carry out each parameter in turn
            // (DEC STD 070, chapter 5's revision note 12).
            b'J' | b'K' => {
                // ED is screen-wide: it may be left for the next call.
                let mut known = false;
                for group in p.groups() {
                    let mode = group.first().copied().unwrap_or(0);
                    // ED 3 (xterm's): the scrollback goes; the screen stays.
                    if byte == b'J' && mode == 3 && !private {
                        self.clear_history();
                        known = true;
                        continue;
                    }
                    let Some(part) = Erase::ALL.into_iter().find(|e| e.number() == mode) else {
                        continue;
                    };
                    known = true;
                    // DECSED and DECSEL (`CSI ? Ps J`, `CSI ? Ps K`) leave
                    // DECSCA's glyphs and every rendition, ED and EL SPA's.
                    if private || self.protected {
                        self.erase_protected(private, byte == b'J', part);
                    } else {
                        let a = self.blank_style();
                        self.with_grid(|g, _, v| {
                            g.erase_in(byte == b'J', part, v, |g, y, start, end| {
                                g.erase(y, start, end, a, v);
                                true
                            });
                        });
                    }
                }
                if !known {
                    return Dispatch::Unhandled;
                }
            }
            // IL and DL, ignored outside the margins, leave the cursor at
            // the left margin (DEC STD 070, IL and DL, note 2).
            b'L' | b'M' => {
                let g = self.grid_mut();
                if let Some(region) = g.lines.starting_at(row)
                    && g.in_columns()
                {
                    g.cursor.pending_wrap = false;
                    g.set_col(g.columns().first());
                    let direction = match byte {
                        b'M' => Scroll::Up { history: false },
                        _ => Scroll::Down,
                    };
                    self.scroll(region, n, direction);
                }
            }
            // Five parameters: XTHIMOUSE (ctlseqs), ignored, not SD.
            b'T' if p.groups().nth(4).is_some() => {}
            b'S' | b'T' => {
                let lines = self.grid().lines;
                let direction = match byte {
                    b'S' => Scroll::Up { history: true },
                    _ => Scroll::Down,
                };
                self.scroll(lines, n, direction);
                // A double-width line moved under the cursor holds it.
                self.grid_mut().fit_cursor();
            }
            // DECSTBM (DEC STD 070, 5-25): a bottom past the screen is the
            // last line, as xterm takes it; margins with the top above the
            // bottom are set and the cursor goes home, others are ignored.
            b'r' => {
                let lines = self.grid().size().lines();
                let bottom = p.first(1, lines.end()).saturating_sub(1);
                if let Some(lines) = lines.margins(n.saturating_sub(1), bottom) {
                    let g = self.grid_mut();
                    g.lines = lines;
                    g.position(0, 0);
                }
            }
            b'm' => {
                self.sgr(p);
                self.pen_changed();
            }
            b'b' => self.repeat(n),
            // CHT (ECMA-48 8.3.10): HT n times.
            b'I' => self.forward_tab(n),
            // CBT (ECMA-48 8.3.7): back n tab stops.
            b'Z' => self.tab(n, false),
            // TBC (ECMA-48 8.3.154; DEC STD 070, p. 5-66): each parameter
            // in turn, 0 clearing the stop at the cursor and 3 every stop.
            b'g' => {
                for group in p.groups() {
                    match group.first() {
                        Some(0) => {
                            let col = self.grid().cursor.col();
                            self.terminal.tabs.set(col, false);
                        }
                        // 2 and 5 clear them all too, stops being every
                        // line's (ECMA-48 8.3.154).
                        Some(2 | 3 | 5) => self.terminal.tabs.clear(),
                        _ => {}
                    }
                }
            }
            b'n' => match p.first(0, 0) {
                5 => return Dispatch::Reply(reply!("\x1b[0n")),
                6 => {
                    let (row, col) = self.reported_cursor();
                    let reply = reply!("\x1b[{row};{col}R");
                    return Dispatch::Reply(reply);
                }
                _ => return Dispatch::Unhandled,
            },
            b'c' if p.first(0, 0) == 0 => {
                return Dispatch::Reply(self.primary_attributes(options));
            }
            _ => return Dispatch::Unhandled,
        }
        Dispatch::Done
    }

    /// The CSI sequences `csi` leaves unhandled, the parser asks here then
    /// (`Parser::csi_dispatch`): DECSCA, DECIC, DECDC, SL and SR and the
    /// rectangular area operations, carried out, and the queries' replies,
    /// some enabled by [`Feature::ExtendedReplies`] and
    /// [`Options::identity`]; `None` for the rest, unhandled.
    #[inline(never)]
    pub(crate) fn other_csi(
        &mut self,
        p: &Parameters,
        intermediates: &[u8],
        byte: u8,
        options: &Options,
    ) -> Option<Dispatch> {
        let n = p.first(0, 0);
        let extended = options.has(Feature::ExtendedReplies);
        let modes = extended || options.has(Feature::ModeReports);
        let identity = options.identity();
        let reply = match (intermediates, byte) {
            // HPB (ECMA-48 8.3.58) and VPB (8.3.159): back n columns, or up
            // n lines, as VPR goes down: with origin mode set "the Active
            // Position cannot be moved above the Top Margin" (DEC STD 070,
            // DECOM, p. 5-31).
            (b"", b'j' | b'k') => {
                self.break_cluster();
                let n = p.first(0, 1).max(1);
                let g = self.grid_mut();
                g.cursor.pending_wrap = false;
                if byte == b'j' {
                    g.backward(n);
                } else {
                    g.position_line(g.cursor_line().saturating_sub(n));
                }
                return Some(Dispatch::Done);
            }
            // CTC (ECMA-48 8.3.17, `CSI Ps W`), each in turn: 0 sets a stop
            // at the cursor's column, 2 clears it, 4 and 5 clear them all.
            (b"", b'W') => {
                let col = self.grid().cursor.col();
                for group in p.groups() {
                    match group.first().copied().unwrap_or(0) {
                        0 => self.terminal.tabs.set(col, true),
                        2 => self.terminal.tabs.set(col, false),
                        4 | 5 => self.terminal.tabs.clear(),
                        _ => {}
                    }
                }
                return Some(Dispatch::Done);
            }
            // DECST8C (ctlseqs, `CSI ? 5 W`): a stop every 8 columns again.
            (b"?", b'W') if n == 5 => {
                self.terminal.tabs = TabStops::default();
                return Some(Dispatch::Done);
            }
            // TSR (ECMA-48 8.3.156): the stop at column n cleared, if given.
            (b" ", b'd') => {
                if let Some(col) = n.checked_sub(1) {
                    self.terminal.tabs.set(col, false);
                }
                return Some(Dispatch::Done);
            }
            // DECSCA (DEC STD 070, 5.11.1.2): 1 protects the glyphs printed
            // next from DECSED and DECSEL, 0 and 2 end it, as xterm reads it.
            (b"\"", b'q') => {
                match n {
                    0 | 2 => self.protect = 0,
                    1 => {
                        self.protect = PROTECTED;
                        self.protected = true;
                    }
                    _ => {}
                }
                return Some(Dispatch::Done);
            }
            (b"'", b'}' | b'~') => {
                self.break_cluster();
                self.edit_columns(p.first(0, 1), byte == b'}');
                return Some(Dispatch::Done);
            }
            // SL and SR (ECMA-48 8.3.121, 8.3.135): the data moved Pn
            // columns, within the margins, as xterm bounds them.
            (b" ", b'@' | b'A') => {
                self.break_cluster();
                let g = self.grid();
                if g.lines.contains(g.cursor.row()) && g.in_columns() {
                    self.shift_columns(g.columns().first(), p.first(0, 1), byte == b'A');
                }
                return Some(Dispatch::Done);
            }
            (b"?", b'n') if extended => match n {
                6 => {
                    let (row, col) = self.reported_cursor();
                    Some(reply!("\x1b[?{row};{col}R"))
                }
                // The DSRs of DEC's other devices, which fux-vt has none
                // of (DEC STD 070, DSR): no printer, no UDK extension, a
                // keyboard of unknown language (as a VT220, which DA2 says
                // fux-vt is, reports it), no locator, no macro space and
                // so DECCKSR's checksum of no macros, labelled as asked.
                15 => Some(reply!("\x1b[?13n")),
                25 => Some(reply!("\x1b[?23n")),
                26 => Some(reply!("\x1b[?27;0n")),
                55 => Some(reply!("\x1b[?53n")),
                56 => Some(reply!("\x1b[?57;0n")),
                62 => Some(reply!("\x1b[0*{{")),
                63 => {
                    let label = p.first(1, 0);
                    Some(reply!("\x1bP{label}!~0000\x1b\\"))
                }
                // The data integrity flag (`Integrity`).
                75 => {
                    let integrity = &mut self.terminal.integrity;
                    let status = mem::replace(integrity, Integrity::Reported).report();
                    Some(reply!("\x1b[?{status}n"))
                }
                // Not configured for multiple sessions.
                85 => Some(reply!("\x1b[?83n")),
                _ => None,
            },
            (b">", b'c') if n == 0 => match identity {
                Some(identity) => {
                    let version = identity.encoded_version();
                    Some(reply!("\x1b[>1;{version};0c"))
                }
                None if extended => Some(reply!("\x1b[>1;10;0c")),
                None => None,
            },
            (b">", b'q') if n == 0 => {
                let identity = identity?;
                let length = identity.name.len().checked_add(identity.version.len())?;
                if length > Identity::MAX_LEN {
                    return None;
                }
                let (name, version) = (identity.name, identity.version);
                Some(reply!("\x1bP>|{name} {version}\x1b\\"))
            }
            (b"", b't') if n == 18 && options.has(Feature::SizeReports) => {
                let (rows, cols) = self.size().into();
                Some(reply!("\x1b[8;{rows};{cols}t"))
            }
            // Cell and text area sizes in pixels, from the host's terminal.
            (b"", b't') if n == 16 => {
                let cell = self.graphics.cell()?;
                Some(reply!("\x1b[6;{};{}t", cell.height(), cell.width()))
            }
            (b"", b't') if n == 14 => {
                let (height, width) = self.pixel_size()?;
                Some(reply!("\x1b[4;{height};{width}t"))
            }
            // XTSMGRAPHICS (ctlseqs, `CSI ? Pi ; Pa ; Pv S`), answered as
            // xterm 411 answers it: Ps 0 for success with the values, 1 for
            // an unknown item, 2 for an unknown action, 3 for a failure.
            // Read, read the maximum or reset: 256 colour registers, and
            // pictures as large as the screen, while the host shows sixel;
            // setting either fails, as they are fixed, and so does every
            // request without sixel shown, and ReGIS's (3).
            (b"?", b'S') if matches!(p.groups().count(), 2 | 3) => {
                let action = p.first(1, 0);
                let status = match (n, action) {
                    (1..=3, 1..=4) => 3,
                    (1..=3, _) => 2,
                    _ => 1,
                };
                let read = self.sixel() && matches!(action, 1 | 2 | 4);
                match n {
                    1 if read => Some(reply!("\x1b[?1;0;256S")),
                    // The screen's pixels, but no more than the decoder keeps.
                    2 if read && let Some((height, width)) = self.pixel_size() => {
                        let (width, height) = crate::sixel::largest(width, height);
                        Some(reply!("\x1b[?2;0;{width};{height}S"))
                    }
                    _ => Some(reply!("\x1b[?{n};{status}S")),
                }
            }
            (b"$", b'x' | b'z' | b'{' | b'v') => {
                self.rectangle_area(p, byte);
                return Some(Dispatch::Done);
            }
            (b"$", b'r' | b't') => {
                self.change_attributes(p, byte == b't');
                return Some(Dispatch::Done);
            }
            // DECREQTPARM (`CSI Ps x`; VT100 User Guide, chapter 3), which
            // the VT100 DA claims without an identity answers, with xterm's
            // fixed parameters. With an identity, a VT220, unanswered.
            (b"", b'x') if identity.is_none() && n <= 1 => {
                Some(reply!("\x1b[{};1;1;128;128;1;0x", n.saturating_add(2)))
            }
            // DECSACE: 0 and 1 a stream, 2 a rectangle; others ignored.
            (b"*", b'x') => {
                if n <= 2 {
                    self.terminal.extent = u8::try_from(n).unwrap_or(0);
                }
                return Some(Dispatch::Done);
            }
            (b"*", b'y') if options.has(Feature::RectangleChecksums) => {
                let Some(reply) = self.rectangle_checksum(p) else {
                    return Some(Dispatch::Done);
                };
                Some(reply)
            }
            (b"?$" | b"$", b'p') if modes => {
                let private = intermediates == b"?$";
                let mode = Mode::of(n, private, options);
                let status = match mode {
                    None if !private && RESET_FOR_GOOD.contains(&n) => ModeStatus::PermanentlyReset,
                    None if !private && SET_FOR_GOOD.contains(&n) => ModeStatus::PermanentlySet,
                    None => ModeStatus::NotRecognized,
                    Some(mode) => mode.fixed().unwrap_or(match self.mode(mode) {
                        true => ModeStatus::Set,
                        false => ModeStatus::Reset,
                    }),
                };
                let (marker, status) = (if private { "?" } else { "" }, status.number());
                Some(reply!("\x1b[{marker}{n};{status}$y"))
            }
            _ => None,
        };
        reply.map(Dispatch::Reply)
    }

    fn sgr(&mut self, p: &Parameters) {
        let mut groups = p.groups().peekable();
        while let Some(group) = groups.next() {
            match group {
                [0] => self.attributes = Attributes::default(),
                // The flags' rows (`Attributes::FLAGS`); a test holds these
                // arms to them. Bold and dim can both be on, as in xterm; 22
                // ends both (ECMA-48 8.3.117).
                [1] => self.attributes.flags |= Attributes::BOLD,
                [2] => self.attributes.flags |= Attributes::DIM,
                [3] => self.attributes.flags |= Attributes::ITALIC,
                [7] => self.attributes.flags |= Attributes::INVERSE,
                [8] => self.attributes.flags |= Attributes::HIDDEN,
                [9] => self.attributes.flags |= Attributes::STRIKEOUT,
                [22] => self.attributes.flags &= !(Attributes::BOLD | Attributes::DIM),
                [23] => self.attributes.flags &= !Attributes::ITALIC,
                [27] => self.attributes.flags &= !Attributes::INVERSE,
                [28] => self.attributes.flags &= !Attributes::HIDDEN,
                [29] => self.attributes.flags &= !Attributes::STRIKEOUT,
                [4] => self.underline(UnderlineStyle::Single),
                // Doubly underlined (ECMA-48 8.3.117, xterm's ctlseqs).
                [21] => self.underline(UnderlineStyle::Double),
                // Slow and rapid blink replace one another.
                [5] => self.attributes = self.attributes.with_blink(Blink::Slow),
                [6] => self.attributes = self.attributes.with_blink(Blink::Rapid),
                [24] => self.underline(UnderlineStyle::None),
                [25] => self.attributes.flags &= !Attributes::BLINK,
                [39] => self.attributes = self.attributes.with_foreground(Color::Default),
                [49] => self.attributes = self.attributes.with_background(Color::Default),
                [59] => self.attributes = self.attributes.with_underline_color(Color::Default),
                // Colours 0–7, and 8–15 as 90–97 and 100–107.
                [n @ 30..=37] => self.set_colour(38, indexed(*n, 30)),
                [n @ 40..=47] => self.set_colour(48, indexed(*n, 40)),
                [n @ 90..=97] => self.set_colour(38, indexed(*n, 90 - 8)),
                [n @ 100..=107] => self.set_colour(48, indexed(*n, 100 - 8)),
                // Kitty's underline styles: `4:0` ends the underline, 1 to 5
                // are single, double, curly, dotted and dashed.
                [4, n, ..] => {
                    if let Some(style) = UnderlineStyle::from_number(*n) {
                        self.underline(style);
                    }
                }
                // ITU-T T.416, 13.1.8, and ctlseqs. An invalid colour is
                // skipped, and the rest of the SGR goes on.
                // Its colour in the parameters after it, or in its own.
                [selector @ (38 | 48 | 58), rest @ ..] => {
                    let colour = match rest {
                        [] => Self::colour_after(&mut groups),
                        _ => colour_of(rest),
                    };
                    if let Some(colour) = colour {
                        self.set_colour(*selector, colour);
                    }
                }

                _ => {}
            }
        }
    }

    fn underline(&mut self, style: UnderlineStyle) {
        self.attributes = self.attributes.with_underline_style(style);
    }

    fn set_colour(&mut self, selector: u16, colour: Color) {
        self.attributes = match selector {
            38 => self.attributes.with_foreground(colour),
            48 => self.attributes.with_background(colour),
            _ => self.attributes.with_underline_color(colour),
        };
    }

    /// The colour of 38, 48 or 58 in their semicolon form, taken from the
    /// parameters after it, as xterm reads it: `5;index` or `2;r;g;b`, a
    /// missing value 0. `None` if the colour is out of range.
    fn colour_after<'a>(
        groups: &mut std::iter::Peekable<impl Iterator<Item = &'a [u16]>>,
    ) -> Option<Color> {
        let kind = match groups.peek() {
            Some([kind]) => *kind,
            _ => return None,
        };
        groups.next();
        let count = match kind {
            2 => 3,
            5 => 1,
            _ => return None,
        };
        // Read as the colon form is: `5:index` or `2:r:g:b`.
        let mut values = [kind, 0, 0, 0];
        for value in values.iter_mut().skip(1).take(count) {
            let Some([n]) = groups.peek() else {
                break;
            };
            *value = *n;
            groups.next();
        }
        colour_of(&values)
    }
}

/// The screen's checks, each shown a screen with its one fault.
#[cfg(all(test, feature = "checks"))]
mod faults {
    use super::*;

    fn screen() -> Result<Screen, Error> {
        let size = Size::new(3, 6).map_err(|_| Error::Capacity)?;
        let mut screen = Screen::new(size, 1 << 12)?;
        screen.ascii(b"hello");
        Ok(screen)
    }

    fn shows(check: &str, fault: impl FnOnce(&mut Screen)) -> Result<(), Error> {
        let mut screen = screen()?;
        assert!(screen.check().is_clean(), "{}", screen.check());
        fault(&mut screen);
        let report = screen.check();
        assert!(
            report.faults_of(check).next().is_some(),
            "{check}: {report}"
        );
        Ok(())
    }

    #[test]
    fn each_check_names_its_fault() -> Result<(), Error> {
        shows("screen.ids", |s| {
            let id = s.primary.live_row(0).map(|r| r.id());
            if let Some(id) = id {
                s.alternate.give_id(0, id);
            }
        })?;
        shows("screen.structural", |s| {
            s.structural = s.version.saturating_add(1)
        })?;
        shows("screen.tabs", |s| {
            s.terminal.tabs.stops = vec![false; 70_000]
        })?;
        Ok(())
    }
}
