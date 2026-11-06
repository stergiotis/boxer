//! DEC ANSI parser after Paul Williams's transition model (attribution in
//! the crate README), with UTF-8 decoding in ground. Ignored control
//! strings retain no payload.

use crate::mode::{Mode, Modes};
use crate::{
    Error, Graphics, Rgb, Screen, Size,
    screen::{Dispatch, Setting},
};

#[derive(Clone, Debug)]
pub(crate) struct Parameters {
    values: [u16; 32],
    /// Bit `i` set when value `i` continues the previous colon-delimited
    /// group: none until a `:` is read, and each value a group of its own.
    sub: u32,
    len: usize,
    /// Whether a separator came after the last slot: the parameters past
    /// it are ignored, as DEC STD 070 (3.5.3.1) lets parameters "beyond the
    /// maximum number supported" be.
    full: bool,
    /// After a private marker, whether a digit or a separator was read:
    /// XTMODKEYS tells `CSI > m` from `CSI > 0 m`.
    pub(crate) given: bool,
}
impl Default for Parameters {
    fn default() -> Self {
        Self {
            values: [0; 32],
            sub: 0,
            len: 1,
            full: false,
            given: false,
        }
    }
}
impl Parameters {
    fn digit(&mut self, byte: u8) {
        // A value too large for a u16 stays at its largest.
        if !self.full
            && let Some(n) = self.len.checked_sub(1).and_then(|i| self.values.get_mut(i))
            && let Some(digit) = byte.checked_sub(b'0')
        {
            *n = n.saturating_mul(10).saturating_add(u16::from(digit));
        }
    }
    fn separator(&mut self, colon: bool) {
        let Some(len) = self.len.checked_add(1).filter(|n| *n <= self.values.len()) else {
            self.full = true;
            return;
        };
        // The new parameter starts empty: `clear` left what a sequence
        // before put there.
        if let Some(value) = self.values.get_mut(self.len) {
            *value = 0;
        }
        self.continue_group(self.len, colon);
        self.len = len;
    }
    #[inline]
    fn continue_group(&mut self, index: usize, colon: bool) {
        if colon && let Ok(index) = u32::try_from(index) {
            self.sub |= 1u32.wrapping_shl(index);
        }
    }
    fn continues(&self, index: usize) -> bool {
        u32::try_from(index)
            .ok()
            .and_then(|i| self.sub.checked_shr(i))
            .is_some_and(|bits| bits & 1 != 0)
    }
    /// `separator` for a reader that keeps the current value and count
    /// itself (`Parser::csi_sequence`). The new count, or `None`, with nothing
    /// changed, where there is no slot for another.
    #[inline]
    fn next(&mut self, len: usize, value: u16, colon: bool) -> Option<usize> {
        let more = len.checked_add(1).filter(|n| *n <= self.values.len())?;
        self.end(len, value);
        self.continue_group(len, colon);
        Some(more)
    }
    /// Stores such a reader's count and last value.
    #[inline]
    fn end(&mut self, len: usize, value: u16) {
        if let Some(slot) = len.checked_sub(1).and_then(|i| self.values.get_mut(i)) {
            *slot = value;
        }
        self.len = len;
    }
    /// Back to one empty parameter, touching only what `separator` and
    /// `digit` read, as a CSI or DCS is begun often.
    fn clear(&mut self) {
        self.len = 1;
        self.sub = 0;
        self.full = false;
        if let Some(value) = self.values.first_mut() {
            *value = 0;
        }
    }
    pub fn groups(&self) -> impl Iterator<Item = &[u16]> + use<'_> {
        let mut start = 0;
        std::iter::from_fn(move || {
            if start >= self.len {
                return None;
            }
            let mut end = start.checked_add(1)?;
            while self.sub != 0 && end < self.len && self.continues(end) {
                end = end.checked_add(1)?;
            }
            let result = self.values.get(start..end);
            start = end;
            result
        })
    }
    /// Whether no parameter was given (or only 0, which reads the same).
    pub(crate) fn is_empty(&self) -> bool {
        self.len == 1 && self.values.first() == Some(&0)
    }
    /// The first value of the group `index`, or `default` if it is 0 or
    /// there is no such group. Without colons the group is the value, a
    /// plain load; with colons `grouped` is called out of line.
    #[inline(always)]
    pub fn first(&self, index: usize, default: u16) -> u16 {
        let value = if self.sub != 0 {
            self.grouped(index)
        } else {
            self.values
                .get(..self.len)
                .and_then(|v| v.get(index))
                .copied()
        };
        match value {
            Some(0) | None => default,
            Some(value) => value,
        }
    }
    #[inline(never)]
    fn grouped(&self, index: usize) -> Option<u16> {
        self.groups().nth(index).and_then(|g| g.first()).copied()
    }
}

/// The non-ASCII character a valid UTF-8 sequence at the start of `bytes`
/// encodes, and its length; `None` for ASCII, and for a sequence invalid or
/// cut short, which the byte-by-byte path reads.
fn decode(bytes: &[u8]) -> Option<(char, usize)> {
    let &first = bytes.first()?;
    let (length, low, high) = lead(first)?;
    // The lead byte's own bits: 5, 4 or 3.
    let mut code = u32::from(first) & 0x7f_u32.checked_shr(u32::try_from(length).ok()?)?;
    let continuation = bytes.get(1..length)?;
    for (i, &byte) in continuation.iter().enumerate() {
        let (low, high) = if i == 0 { (low, high) } else { (0x80, 0xbf) };
        if !(low..=high).contains(&byte) {
            return None;
        }
        code = code.checked_shl(6)? | u32::from(byte & 0x3f);
    }
    Some((char::from_u32(code)?, length))
}

/// The non-ASCII characters of the valid UTF-8 `bytes` begins with, and how
/// many bytes they took.
pub(crate) struct Chars<'a> {
    bytes: &'a [u8],
    taken: usize,
}

impl Iterator for Chars<'_> {
    type Item = char;
    #[inline]
    fn next(&mut self) -> Option<char> {
        let (c, length) = self.bytes.get(self.taken..).and_then(decode)?;
        self.taken = self.taken.saturating_add(length);
        Some(c)
    }
}

/// Whether an OSC's payload keeps `byte`, one that does not end it: a
/// command string holds 00/08 to 00/13 and 02/00 to 07/14 (ECMA-48 8.3.89,
/// 5.6), and here the bytes of UTF-8 too; the other controls are left out,
/// as Williams's DEC parser model ignores them in an OSC string.
const fn in_command_string(byte: u8) -> bool {
    !matches!(byte, 0x00..=0x07 | 0x0e..=0x1f | 0x7f)
}

/// What a UTF-8 lead byte begins: the sequence's length, and the range its
/// second byte is in, which rules out overlong forms, surrogates and code
/// points past U+10FFFF (Unicode 17, table 3-7); `None` for a byte that
/// leads none. Every later byte is 0x80 to 0xbf.
#[inline(always)]
const fn lead(first: u8) -> Option<(usize, u8, u8)> {
    Some(match first {
        0xc2..=0xdf => (2, 0x80, 0xbf),
        0xe0 => (3, 0xa0, 0xbf),
        0xed => (3, 0x80, 0x9f),
        0xe1..=0xef => (3, 0x80, 0xbf),
        0xf0 => (4, 0x90, 0xbf),
        0xf4 => (4, 0x80, 0x8f),
        0xf1..=0xf3 => (4, 0x80, 0xbf),
        _ => return None,
    })
}

/// How many bytes `bytes` begins with are printable ASCII, 0x20 to 0x7e,
/// its first being one (its caller checks; else the count is off, not a
/// panic). Eight at a time, as a word: a byte below 0x20 borrows when 0x20
/// is taken from it, setting its top bit where the byte's own is clear; one
/// from 0x7f up has its top bit set once 1 is added to it, or before. A
/// borrow or carry runs only into later bytes, so the first byte flagged is
/// the first that is not printable.
fn printable(bytes: &[u8]) -> usize {
    if !bytes.get(1).is_some_and(|b| (0x20..=0x7e).contains(b)) {
        return 1;
    }
    let (words, _) = bytes.as_chunks::<8>();
    let mut length = 0usize;
    for word in words {
        let x = u64::from_le_bytes(*word);
        let below = x.wrapping_sub(ONES.wrapping_mul(0x20)) & !x;
        let above = x.wrapping_add(ONES) | x;
        let flagged = (below | above) & TOPS;
        if flagged != 0 {
            let first = usize::try_from(flagged.trailing_zeros() / 8).unwrap_or(0);
            return length.saturating_add(first);
        }
        length = length.saturating_add(8);
    }
    let tail = bytes.get(length..).unwrap_or_default();
    length.saturating_add(
        tail.iter()
            .take_while(|b| (0x20..=0x7e).contains(*b))
            .count(),
    )
}

/// Where the first ESC in `bytes` is, a word at a time: XOR with ESC makes
/// an ESC a zero byte, which `zeros` flags.
fn esc_in(bytes: &[u8]) -> Option<usize> {
    first_flagged(bytes, |x| zeros(x ^ ONES.wrapping_mul(0x1b)), |b| b == 0x1b)
}

/// Where the first ESC, CAN or SUB in `bytes` is, a word at a time: where an
/// ignored string may end.
fn string_end(bytes: &[u8]) -> Option<usize> {
    let flag = |x: u64| {
        zeros(x ^ ONES.wrapping_mul(0x1b))
            | zeros(x ^ ONES.wrapping_mul(0x18))
            | zeros(x ^ ONES.wrapping_mul(0x1a))
    };
    first_flagged(bytes, flag, |b| matches!(b, 0x18 | 0x1a | 0x1b))
}

/// Where the first C0 control or DEL in `bytes` is, a word at a time: a
/// byte below 0x20 borrows when 0x20 is taken from it (as in `printable`),
/// and DEL is a zero byte once XORed with DEL.
fn control_in(bytes: &[u8]) -> Option<usize> {
    let flag = |x: u64| {
        (x.wrapping_sub(ONES.wrapping_mul(0x20)) & !x & TOPS) | zeros(x ^ ONES.wrapping_mul(0x7f))
    };
    first_flagged(bytes, flag, |b| b < 0x20 || b == 0x7f)
}

const ONES: u64 = u64::from_le_bytes([0x01; 8]);
const TOPS: u64 = u64::from_le_bytes([0x80; 8]);

/// The top bit of each zero byte of `x`, and perhaps of bytes after one: a
/// zero byte alone borrows when 1 is taken from it with its top bit clear,
/// and a borrow runs only into the bytes after it.
const fn zeros(x: u64) -> u64 {
    x.wrapping_sub(ONES) & !x & TOPS
}

/// Where the first byte of `bytes` is that `flag` flags in its word (exact
/// for the first one flagged), then the tail by `byte`.
#[inline(always)]
fn first_flagged(
    bytes: &[u8],
    flag: impl Fn(u64) -> u64,
    byte: impl Fn(u8) -> bool,
) -> Option<usize> {
    let (words, tail) = bytes.as_chunks::<8>();
    for (i, word) in words.iter().enumerate() {
        let flagged = flag(u64::from_le_bytes(*word));
        if flagged != 0 {
            let first = usize::try_from(flagged.trailing_zeros() / 8).ok()?;
            return i.checked_mul(8)?.checked_add(first);
        }
    }
    let at = tail.iter().position(|&b| byte(b))?;
    words.len().checked_mul(8)?.checked_add(at)
}

/// Where the parser is in what it reads, and what it keeps of that: each
/// state holds exactly what is read in it, and nothing outlives it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
enum State {
    #[default]
    Ground,
    /// A character's first bytes.
    Utf8(Bytes<4>),
    Escape(Intermediates),
    /// An escape sequence with more intermediates than are kept: read to
    /// its final byte, and not carried out.
    EscapeIgnore,
    Csi(Phase),
    CsiIgnore,
    /// A DCS before its final byte.
    Dcs(Phase),
    /// A string read to its end and dropped: a DCS fux-vt does not
    /// answer, SOS, PM or APC.
    IgnoreString,
    /// Sixel data, decoded as it comes; its end places the picture.
    Sixel,
    /// A DECRQSS (`DCS $ q Pt ST`): its request so far, `None` once it ran
    /// past any setting fux-vt knows.
    Request(Option<Bytes<REQUEST_LIMIT>>),
    /// `whole` until it ran past what is kept of one.
    Osc {
        whole: bool,
    },
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
struct Bytes<const N: usize> {
    bytes: [u8; N],
    len: u8,
}

/// The intermediates (and private marker) a sequence keeps: xterm's
/// longest are two (`?$`).
type Intermediates = Bytes<2>;

impl<const N: usize> Default for Bytes<N> {
    fn default() -> Self {
        Self {
            bytes: [0; N],
            len: 0,
        }
    }
}

impl<const N: usize> Bytes<N> {
    fn of(byte: u8) -> Self {
        Self::default().push(byte).unwrap_or_default()
    }
    /// `None` if there is no room for `byte`.
    fn push(mut self, byte: u8) -> Option<Self> {
        *self.bytes.get_mut(usize::from(self.len))? = byte;
        self.len = self.len.checked_add(1)?;
        Some(self)
    }
    fn as_slice(&self) -> &[u8] {
        self.bytes.get(..usize::from(self.len)).unwrap_or_default()
    }
}

/// How far a CSI or DCS has come, and its intermediates.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Phase {
    /// Nothing read after its introducer.
    Entry,
    /// After a private marker if there is one.
    Param(Intermediates),
    Intermediate(Intermediates),
}

/// Finds where a frame drawn in synchronized output ends in the bytes a
/// host holds after its BSU ([`Parser::process_until_stop`]), reading them
/// as the parser will, nothing carried out. ESC begins a sequence anywhere,
/// in a string too, so only what follows each ESC is read. Made by
/// [`Parser::frame_scan`]; it goes on from where it stopped, so each byte
/// is read once however the frame comes split.
#[derive(Clone, Debug)]
pub struct FrameScan {
    from: usize,
    /// Where it is in a sequence the bytes so far end in the middle of,
    /// so that no byte is read twice (`THREATS.md` B19).
    pending: Pending,
    params: Parameters,
    options: Options,
    /// Whether XTSAVE holds synchronized output set, for XTRESTORE.
    saved: bool,
}

/// Where a frame scan is in the sequence it reads.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
enum Pending {
    #[default]
    Ground,
    Escape {
        /// No intermediate yet: `[` begins a CSI, `c` is RIS.
        plain: bool,
    },
    Csi(Phase),
    /// Read to its final byte and not carried out (`State::CsiIgnore`).
    CsiIgnore,
}

impl FrameScan {
    /// Whether synchronized output is reset by default, as RIS, and DECSTR
    /// for the modes it names, leave it.
    const RESET: bool = !Modes::DEFAULT.contains(Mode::SynchronizedOutput);
    /// Whether a soft reset (DECSTR, DECSCL) leaves it reset.
    const SOFT: bool = Self::RESET && Modes::SOFT_RESET.contains(Mode::SynchronizedOutput);
    /// Reads `held`, the frame's bytes so far, from where it stopped: how
    /// many of them go up to and through the sequence that ends
    /// synchronized output (DECRST naming it, XTRESTORE of it saved reset,
    /// DECSTR, DECSCL, RIS); `None` if none does.
    pub fn end(&mut self, held: &[u8]) -> Option<usize> {
        loop {
            if self.pending == Pending::Ground {
                let rest = held.get(self.from..)?;
                let Some(esc) = esc_in(rest) else {
                    crate::budget::spend(rest.len());
                    self.from = held.len();
                    return None;
                };
                crate::budget::spend(esc);
                self.from = self.from.checked_add(esc)?;
            }
            let &byte = held.get(self.from)?;
            crate::budget::spend(1);
            self.from = self.from.checked_add(1)?;
            if self.step(byte) {
                return Some(self.from);
            }
        }
    }

    /// Reads `byte` of a sequence, as `Parser::byte` does: CAN and SUB
    /// cancel it, ESC begins another; whether it ends synchronized output.
    fn step(&mut self, byte: u8) -> bool {
        let (next, ended) = match (self.pending, byte) {
            (_, 0x1b) => (Pending::Escape { plain: true }, false),
            (Pending::Ground, _) | (_, 0x18 | 0x1a) => (Pending::Ground, false),
            (Pending::Escape { .. }, 0x20..=0x2f) => (Pending::Escape { plain: false }, false),
            (Pending::Escape { plain: true }, b'[') => {
                self.params.clear();
                (Pending::Csi(Phase::Entry), false)
            }
            (Pending::Escape { plain: true }, b'c') => (Pending::Ground, Self::RESET),
            // A final byte, or a control string's introducer among them.
            (Pending::Escape { .. }, 0x30..=0x7e) => (Pending::Ground, false),
            (Pending::Escape { .. }, _) => (self.pending, false),
            (Pending::CsiIgnore, 0x40..=0x7e) => (Pending::Ground, false),
            (Pending::CsiIgnore, _) => (self.pending, false),
            (Pending::Csi(phase), _) => match phase.next(byte, &mut self.params) {
                Next::Phase(phase) => (Pending::Csi(phase), false),
                Next::Ignore => (Pending::CsiIgnore, false),
                Next::Control => (self.pending, false),
                Next::Final(read) => (Pending::Ground, self.ends(read.as_slice(), byte)),
            },
        };
        self.pending = next;
        ended
    }

    /// Whether the CSI just read ends synchronized output, as `Screen::csi`
    /// carries it out: DECRST (`CSI ? Pm l`) naming it; XTRESTORE (`CSI ? Pm
    /// r`) of it while XTSAVE holds it reset; DECSTR (`CSI ! p`) and DECSCL
    /// (`CSI 61 " p`, `CSI 62 " p`), which do a soft reset.
    fn ends(&mut self, intermediates: &[u8], byte: u8) -> bool {
        let ours = |n: &u16| Mode::of(*n, true, &self.options) == Some(Mode::SynchronizedOutput);
        let named = || self.params.groups().any(|g| matches!(g, [n] if ours(n)));
        match (intermediates, byte) {
            (b"?", b'l') => named(),
            (b"?", b'r') => !self.saved && named(),
            // The mode is set while the frame lasts.
            (b"?", b's') => {
                self.saved = self.saved || named();
                false
            }
            (b"!", b'p') => Self::SOFT,
            (b"\"", b'p') => Self::SOFT && matches!(self.params.first(0, 0), 61 | 62),
            _ => false,
        }
    }
}

/// The frame scan's part of the invariant checker (THREATS.md section 6).
#[cfg(feature = "checks")]
impl FrameScan {
    /// 5: where the scan goes on is within `held`, the frame's bytes it
    /// was given.
    pub fn check(&self, held: &[u8]) -> crate::Checked {
        let mut report = crate::Checked::default();
        let found = crate::check::ensure(self.from <= held.len(), || {
            format!("the scan goes on at {} of {} bytes", self.from, held.len())
        });
        report.record("frame_scan.from", "", found);
        report
    }
}

/// What a byte makes of a CSI or DCS in a [`Phase`].
enum Next {
    Phase(Phase),
    /// Malformed or past the bounds kept: read to its end, not carried out.
    Ignore,
    /// A C0 control, executed within a CSI.
    Control,
    Final(Intermediates),
}

impl Phase {
    /// Digits and separators are read into `params`.
    fn next(self, byte: u8, params: &mut Parameters) -> Next {
        let read = match self {
            Self::Entry => Intermediates::default(),
            Self::Param(read) | Self::Intermediate(read) => read,
        };
        match byte {
            0x00..=0x1f => Next::Control,
            0x20..=0x2f => read
                .push(byte)
                .map_or(Next::Ignore, |read| Next::Phase(Self::Intermediate(read))),
            0x30..=0x3f if matches!(self, Self::Intermediate(_)) => Next::Ignore,
            b'0'..=b'9' => {
                params.digit(byte);
                params.given |= !read.as_slice().is_empty();
                Next::Phase(Self::Param(read))
            }
            b';' | b':' => {
                params.separator(byte == b':');
                params.given |= !read.as_slice().is_empty();
                Next::Phase(Self::Param(read))
            }
            0x3c..=0x3f if self == Self::Entry => {
                params.given = false;
                Next::Phase(Self::Param(Intermediates::of(byte)))
            }
            0x3c..=0x3f => Next::Ignore,
            0x40..=0x7e => Next::Final(read),
            0x7f..=0xff => Next::Phase(self),
        }
    }
}

/// The most OSC payload bytes retained for [`Event`] delivery, hyperlinks
/// and the palette. Of a longer OSC string the rest is consumed and
/// dropped, its event `cut` ([`Kept`]); nothing beyond this is ever
/// buffered.
pub const OSC_PAYLOAD_LIMIT: usize = 64 * 1024;

/// The most OSC payload bytes retained with [`Feature::PromptMarks`] alone:
/// enough for any mark's command and the options fux-vt reads of it,
/// `133;D;-2147483648` the longest.
const OSC_PREFIX: usize = 64;

/// The most bytes of a DECRQSS request kept: xterm's longest are two
/// (` q`, `$|`, `"p`). A longer request is no setting fux-vt knows.
const REQUEST_LIMIT: usize = 4;

closed! {
    /// A piece of opt-in behaviour that needs the host's cooperation, turned on
    /// in [`Options`].
    #[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
    pub enum Feature {
        /// Deliver OSC 0/1/2 (icon name / window title), OSC 52 (clipboard),
        /// BEL, and the dynamic colours' queries (OSC 10 to 19, as
        /// [`Event::ColorQuery`]) as [`Event`]s. OSC payloads are buffered up
        /// to [`OSC_PAYLOAD_LIMIT`].
        Events,
        /// Also answer DECRQM (`CSI ? Ps $ p` and `CSI Ps $ p`), DECXCPR
        /// (`CSI ? 6 n`) and secondary device attributes (`CSI > c`).
        ExtendedReplies,
        /// Answer DECRQM (`CSI ? Ps $ p` and `CSI Ps $ p`) alone, without the
        /// rest of [`Feature::ExtendedReplies`]: how programs learn which
        /// modes, such as synchronized output (2026), the terminal knows.
        ModeReports,
        /// Mode 2048, in-band resize (`references/modern/mode_2048_in_band_resize.md`):
        /// report the size as `CSI 48 ; rows ; cols ; 0 ; 0 t` when a program
        /// sets it and after a resize ([`Parser::resize_report`]). Pixel sizes
        /// are 0, as the spec allows. Off, the mode is not recognized.
        InBandResize,
        /// Answer xterm's text-area size query (`CSI 18 t`, ctlseqs' window
        /// manipulation) with `CSI 8 ; rows ; cols t`.
        SizeReports,
        /// Mode 2031, colour-scheme change reports
        /// (`references/modern/mode_2031_color_scheme_updates.md`): track it
        /// and report it to DECRQM. State only: the host knows the colour
        /// scheme, sends the reports (`CSI ? 997 ; 1 n` dark, `2` light) and
        /// answers `CSI ? 996 n`. Off, the mode is not recognized.
        ColorSchemeUpdates,
        /// Track the kitty keyboard protocol's flag stacks (`CSI > u`, `CSI < u`,
        /// `CSI = u`) and xterm's modifyOtherKeys (`CSI > 4 ; Pv m`), and answer
        /// the flag query `CSI ? u`. State only: the host encodes keys, and
        /// must leave this off if it cannot, or programs will believe it can.
        KittyKeyboard,
        /// Re-wrap the primary screen and its history at the new width on
        /// resize. The alternate screen is not reflowed: its programs redraw.
        Reflow,
        /// Keep hyperlinks (`OSC 8 ; params ; URI ST`), read with
        /// [`crate::Row::link`]; each screen holds up to 4 MiB of links. Off,
        /// OSC 8 is ignored.
        Hyperlinks,
        /// Keep prompt marks (`OSC 133 ; A`, semantic prompts): the row a prompt
        /// starts on is marked, and `A` and `L` start a fresh line. Off, OSC
        /// 133 is ignored.
        PromptMarks,
        /// Answer DECRQCRA, a checksum of a rectangle of the screen, with
        /// DECCKSR, as xterm and the VT520 sum it. It lets a program read what
        /// its screen shows, so xterm refuses it by default.
        RectangleChecksums,
        /// Answer DECRQSS (`DCS $ q Pt ST`, xterm's ctlseqs; DECRPSS in the
        /// VT510 manual) for SGR (`m`), DECSCUSR (` q`), DECSTBM (`r`),
        /// DECSLRM (`s`), DECSCA (`"q`), DECSCL (`"p`) and DECSACE (`*x`), as
        /// xterm 411 answers: `DCS 1 $ r Pt ST`, `Pt` the sequence that sets
        /// it: the cursor shape the terminal starts with as `7 SP q`, which
        /// DECSCUSR 7 restores, and DECSCUSR 0's as `1 SP q`. Another request
        /// is answered as invalid, `DCS 0 $ r ST`. XTGETTCAP (`DCS + q
        /// Pt ST`) is answered as invalid, `DCS 0 + r ST`. Programs learn
        /// from the pen what the terminal draws: neovim draws its diagnostics
        /// curly only if `4:3` comes back. Off, both are ignored.
        SettingReports,
        /// Keep the colours a program sets and answer its queries of them, as
        /// xterm does: the palette (OSC 4, 104), the special colours (OSC 5,
        /// 105) and the dynamic colours (OSC 10 to 19, 110 to 119). Off, these
        /// OSCs are ignored, and OSC 10 to 19 queries are events, with
        /// [`Feature::Events`].
        Palette,
        /// Decode sixel graphics (`DCS P1 ; P2 ; P3 q … ST`) into pictures,
        /// read with [`Screen::pictures`]: each takes the cells it covers,
        /// which hold [`crate::PLACEHOLDER`], up to
        /// [`Parser::set_picture_bytes`] of pixels in all. Off, sixel is
        /// ignored, and neither DA1 nor XTSMGRAPHICS says the terminal has it.
        Pictures,
    }
}

/// How a parser behaves: the [`Feature`]s on, and who it answers as. The
/// default (no feature, no identity) causes no side effects, retains no
/// OSC payload, and answers only DSR 5n/6n and primary DA.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Options {
    /// Whether each feature is on, by its discriminant.
    on: [bool; Feature::ALL.len()],
    identity: Option<Identity>,
}

impl Options {
    /// Nothing on, as [`Options::default`], in a `const` too.
    pub const fn new() -> Self {
        Self {
            on: [false; Feature::ALL.len()],
            identity: None,
        }
    }
    /// These options with `feature` on.
    pub const fn with(self, feature: Feature) -> Self {
        self.set(feature, true)
    }
    /// These options with `feature` as `on` says.
    pub const fn set(mut self, feature: Feature, on: bool) -> Self {
        if let Some((_, [slot, ..])) = self.on.split_at_mut_checked(feature as usize) {
            *slot = on;
        }
        self
    }
    /// Whether `feature` is on.
    pub(crate) const fn has(&self, feature: Feature) -> bool {
        matches!(
            self.on.split_at_checked(feature as usize),
            Some((_, [true, ..]))
        )
    }
    /// These options answering as `identity`, or as a bare VT100 if `None`.
    pub const fn with_identity(mut self, identity: Option<Identity>) -> Self {
        self.identity = identity;
        self
    }
    /// Who the terminal answers as: see [`Identity`].
    pub(crate) const fn identity(&self) -> Option<Identity> {
        self.identity
    }
}

impl From<Feature> for Options {
    fn from(feature: Feature) -> Self {
        Self::new().with(feature)
    }
}

impl FromIterator<Feature> for Options {
    fn from_iter<I: IntoIterator<Item = Feature>>(features: I) -> Self {
        features.into_iter().fold(Self::new(), Self::with)
    }
}

/// Why [`Parser::process_until_stop`] stopped before the end of its bytes:
/// a sequence began or ended a frame drawn in synchronized output
/// ([`Mode::SynchronizedOutput`]), or the call spent its work budget.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Stop {
    /// It set the mode while it was reset (BSU, `CSI ? 2026 h`, or
    /// XTRESTORE of it saved set): a frame begins.
    Begun,
    /// It reset the mode while it was set (ESU, `CSI ? 2026 l`, XTRESTORE,
    /// DECSTR or RIS): the frame ends.
    Ended,
    /// The call did as much work as one call may (THREATS.md 4). Nothing
    /// is lost: the parser stopped between two bytes, and goes on from the
    /// next byte when it is given it, later or at once as the host likes.
    Budget,
}

/// Who the terminal says it is. With [`Options::identity`] set, primary DA
/// (`CSI c`) answers as level 2, the VT220's, with its extensions;
/// secondary DA (`CSI > c`) answers `CSI > 1 ; Pv ; 0 c` with `version`
/// encoded as `major * 10000 + minor * 100 + patch`; XTVERSION (`CSI > q`)
/// answers `DCS > | name version ST`; and cursor reports give a cursor
/// waiting to wrap at the last column, as xterm does, not one past it.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Identity {
    /// The terminal's name, as XTVERSION reports it.
    pub name: &'static str,
    /// Its `major.minor.patch` version; a pre-release suffix is ignored in
    /// DA2. XTVERSION goes unanswered if name and version exceed
    /// [`Identity::MAX_LEN`] bytes together.
    pub version: &'static str,
}

impl Identity {
    /// The most bytes of name and version XTVERSION reports.
    pub(crate) const MAX_LEN: usize = 48;

    /// The version as DA2's firmware field: its parts, at most three, as
    /// digits in base 100, anything past a `-` or `+` dropped: `0.5.0` is
    /// 500, `1.2` is 102.
    pub(crate) fn encoded_version(&self) -> u32 {
        let release = self.version.split(['-', '+']).next().unwrap_or_default();
        release.split('.').take(3).fold(0u32, |sum, part| {
            let part = part.parse::<u32>().unwrap_or(0).min(99);
            sum.saturating_mul(100).saturating_add(part)
        })
    }
}

/// An OSC string's text as it was kept: raw bytes, all of them, or the
/// first of a string longer than [`OSC_PAYLOAD_LIMIT`], `cut` saying the
/// rest was dropped.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Kept<'a> {
    /// The bytes kept.
    pub bytes: &'a [u8],
    /// Whether the string went on past them.
    pub cut: bool,
}

/// A side effect requested by child output. Only delivered with
/// [`Feature::Events`]; payloads are raw bytes, bounded by [`OSC_PAYLOAD_LIMIT`].
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Event<'a> {
    /// OSC 0 or OSC 2.
    Title(Kept<'a>),
    /// OSC 0 or OSC 1.
    IconName(Kept<'a>),
    /// BEL executed outside a control string.
    Bell,
    /// A desktop notification: OSC 9 (iTerm2), OSC 777 (`notify;title;body`)
    /// or OSC 99 (kitty).
    Notification(Kept<'a>),
    /// OSC 52 set request; a `?` query is not an event.
    Clipboard {
        /// The selection parameter, `Pc`: `c`, `p`, `s` and so on, or empty.
        selection: &'a [u8],
        /// The data, `Pd`, as sent (normally base64).
        data: Kept<'a>,
    },
    /// A query of one of xterm's dynamic colours, `OSC Ps ; ?` with `Ps`
    /// from 10 to 19 (ctlseqs, "Operating System Commands"). One OSC can ask
    /// for several, each `?` the next colour (`OSC 10 ; ? ; ?` asks 10 and
    /// 11), an event each, in order. The host answers, as xterm does, `OSC
    /// Ps ; rgb:RRRR/GGGG/BBBB`, ended as the query was. With
    /// [`Feature::Palette`], a query of a colour the program set is no
    /// event: fux-vt answers it itself.
    ColorQuery {
        /// The colour asked for, 10 to 19.
        number: u8,
        /// Whether the query ended with BEL rather than ST.
        bel: bool,
    },
    /// A shell command began (OSC 133 `C` with none running; a `C` while
    /// one runs moves its start and begins none).
    CommandStarted,
    /// A shell command ended (OSC 133 `D`, or `N`, ending one that ran),
    /// with the status the shell gave, if any.
    CommandEnded(Option<i32>),
}

/// A complete sequence fux-vt parsed but does not implement, so a host can
/// log or answer it. Sequences cut short by their bounds (too many
/// parameters or intermediates) are dropped without being reported.
#[derive(Clone, Copy, Debug)]
pub enum Unhandled<'a> {
    /// `CSI`, its private marker and intermediates, parameters and final byte.
    Csi {
        /// Its parameters.
        params: Params<'a>,
        /// Its private marker and intermediate bytes.
        intermediates: &'a [u8],
        /// Its final byte.
        action: u8,
    },
    /// `ESC`, its intermediates and final byte.
    Escape {
        /// Its intermediate bytes.
        intermediates: &'a [u8],
        /// Its final byte.
        action: u8,
    },
}

/// A CSI sequence's parameters.
#[derive(Clone, Copy, Debug)]
pub struct Params<'a>(&'a Parameters);

impl<'a> Params<'a> {
    /// Each parameter with its colon-separated subparameters; an empty
    /// parameter is 0.
    pub fn groups(&self) -> impl Iterator<Item = &'a [u16]> + use<'a> {
        self.0.groups()
    }
}

/// Receives terminal query replies, with [`Feature::Events`] events, and
/// sequences fux-vt does not implement. All default to discarding, so an
/// implementation handles only what it needs.
pub trait Sink {
    /// A reply to a query, to send back to the program.
    fn reply(&mut self, _bytes: &[u8]) {}
    /// An event, with [`Feature::Events`].
    fn event(&mut self, _event: Event<'_>) {}
    /// A complete sequence fux-vt does not implement.
    fn unhandled(&mut self, _sequence: Unhandled<'_>) {}
    /// A sixel picture, with [`Feature::Pictures`], ended: placed whole,
    /// or not: begun while the host drew no sixel (`set_host_graphics`),
    /// and not kept; ended once it had no cell size; lifted past a scroll
    /// region's top, or cut by a double-width line's end, as it was
    /// placed; or cut, placed or not: what it drew
    /// past what a picture may keep ([`crate::sixel::MAX_SIDE`] a side,
    /// [`crate::sixel::MAX_PIXELS`] in all, and the pictures' budget,
    /// [`Parser::set_picture_bytes`]) is not kept. One that drew nothing,
    /// or that CAN ended, is neither. Told in order, as each ends; one
    /// placed whole and let go later, the oldest first as newer ones need
    /// the room, is not told again, as history let go is not.
    fn picture(&mut self, _placed: bool) {}
}

/// Discards everything: the sink of a host with no program to answer, as
/// one replaying recorded output.
impl Sink for () {}

/// Incremental byte parser and its terminal state. Replies and events are
/// delivered synchronously to a caller-supplied sink; no unbounded queue exists.
#[derive(Clone, Debug)]
pub struct Parser {
    screen: Screen,
    options: Options,
    /// At most `osc_limit` bytes; emptied as an OSC begins, its allocation
    /// kept.
    osc: Vec<u8>,
    /// Set once from the options, as it is asked for every byte of an OSC.
    osc_limit: usize,
    state: State,
    params: Parameters,
    sixel: Option<Box<crate::sixel::Decoder>>,
    /// The string being ignored is an XTGETTCAP (`DCS + q Pt ST`) with
    /// [`Feature::SettingReports`], answered at its end as invalid.
    termcap: bool,
    /// The work `process_until_stop` spent since it last stopped at the
    /// budget, over as many calls as that took: so a host that cuts its
    /// reads small still stops at the budget.
    spent: u64,
    /// Where this thread's spending reaches the budget in the call of
    /// `process_until_stop` under way; `u64::MAX` between calls.
    end: u64,
    refused: u64,
}
#[cfg(test)]
mod tests;

impl Parser {
    /// A parser with a screen of `size` keeping up to `history_bytes` of
    /// history (24 bytes a row, 8 a cell, and its text). More cells than a
    /// grid may hold are refused.
    pub fn with_options(size: Size, history_bytes: usize, options: Options) -> Result<Self, Error> {
        Ok(Self {
            screen: Screen::new(size, history_bytes)?,
            options,
            osc: Vec::new(),
            osc_limit: if options.has(Feature::Events)
                || options.has(Feature::Hyperlinks)
                || options.has(Feature::Palette)
            {
                OSC_PAYLOAD_LIMIT
            } else if options.has(Feature::PromptMarks) {
                OSC_PREFIX
            } else {
                0
            },
            state: State::Ground,
            params: Parameters::default(),
            sixel: None,
            termcap: false,
            spent: 0,
            end: u64::MAX,
            refused: 0,
        })
    }
    /// Checks every invariant of the parser and its screen that the state
    /// alone shows (THREATS.md section 6): which checks ran, and the faults
    /// found. The work counters do not see it.
    #[cfg(feature = "checks")]
    pub fn check(&self) -> crate::Checked {
        crate::check::unseen(|| {
            let mut report = crate::Checked::default();
            report.record("parser.osc", "", self.check_osc());
            report.record("parser.params", "", self.check_params());
            report.record("parser.sixel", "", self.check_sixel());
            report.record("parser.utf8", "", self.check_utf8());
            report.record("parser.termcap", "", self.check_termcap());
            report.wait("parser.refused", "B44: Parser::refused");
            report.merge(self.screen.check());
            report
        })
    }
    /// 1: the OSC payload held is within its limit, one byte past it
    /// inside an OSC string, in room for the limit and that byte alone.
    #[cfg(feature = "checks")]
    fn check_osc(&self) -> Result<(), String> {
        let inside = matches!(self.state, State::Osc { .. });
        let most = self.osc_limit.saturating_add(usize::from(inside));
        crate::check::ensure(self.osc.len() <= most, || {
            format!("{} OSC bytes held, {most} at most", self.osc.len())
        })?;
        let room = self.osc_limit.saturating_add(1);
        crate::check::ensure(self.osc.capacity() <= room, || {
            format!(
                "room for {} OSC bytes, {room} at most (B20)",
                self.osc.capacity()
            )
        })
    }
    /// 2: one parameter at least, and no more than are kept.
    #[cfg(feature = "checks")]
    fn check_params(&self) -> Result<(), String> {
        let len = self.params.len;
        crate::check::ensure((1..=self.params.values.len()).contains(&len), || {
            format!("{len} parameters")
        })
    }
    /// 3: a decoder exactly while in sixel data, and its picture within
    /// what one may be; 31: its pixels and the pictures kept within their
    /// budget.
    #[cfg(feature = "checks")]
    fn check_sixel(&self) -> Result<(), String> {
        let sixel = matches!(self.state, State::Sixel);
        crate::check::ensure(sixel == self.sixel.is_some(), || {
            format!("in sixel {sixel}, with a decoder {}", self.sixel.is_some())
        })?;
        let Some(decoder) = &self.sixel else {
            return Ok(());
        };
        decoder.check()?;
        self.screen.check_picture_bytes(decoder.bytes())
    }
    /// 3a: an XTGETTCAP is being answered only while its string is read
    /// (`State::IgnoreString`), so that no other string's end answers it.
    #[cfg(feature = "checks")]
    fn check_termcap(&self) -> Result<(), String> {
        let string = matches!(self.state, State::IgnoreString);
        crate::check::ensure(!self.termcap || string, || {
            format!("an XTGETTCAP answered in {:?}", self.state)
        })
    }
    /// 4: a character's first bytes are fewer than its lead byte says.
    #[cfg(feature = "checks")]
    fn check_utf8(&self) -> Result<(), String> {
        let State::Utf8(read) = self.state else {
            return Ok(());
        };
        let [first, ..] = read.bytes;
        let need = lead(first).map_or(0, |(need, _, _)| need);
        crate::check::ensure(read.len >= 1 && usize::from(read.len) < need, || {
            format!("{} bytes of a character of {need}", read.len)
        })
    }
    #[cfg(test)]
    pub(crate) fn screen_mut(&mut self) -> &mut Screen {
        &mut self.screen
    }
    /// Sets the most bytes of pixels its pictures may hold together, the
    /// one being drawn included ([`crate::PICTURE_BYTES`] to begin with),
    /// and lets pictures go to fit it: those no longer shown first, then
    /// the oldest.
    pub fn set_picture_bytes(&mut self, bytes: usize) {
        let drawing = self.sixel.as_ref().map_or(0, |d| d.bytes());
        if !self.screen.set_picture_bytes(bytes, drawing)
            && let Some(decoder) = &mut self.sixel
        {
            decoder.clear();
        }
    }
    #[cfg(test)]
    pub(crate) fn picture_bytes(&self) -> usize {
        let drawing = self.sixel.as_ref().map_or(0, |d| d.bytes());
        self.screen.picture_bytes().saturating_add(drawing)
    }
    /// The terminal's state: what the screen shows, the cursor and the modes.
    pub fn screen(&self) -> &Screen {
        &self.screen
    }
    /// Drops the scrollback, as `CSI 3 J` does; the screen stays.
    pub fn clear_history(&mut self) {
        self.screen.clear_history();
    }
    /// Tells the parser what the host's terminal shows for palette entries
    /// 0 to 15 (`None`: unknown). With `Feature::Palette`, a program's query
    /// of an entry it has not set (`OSC 4 ; index ; ?`) is answered with it,
    /// and not at all where it is unknown. A colour the program set wins,
    /// and no reset (OSC 104, DECSTR, RIS) clears the host's. It changes no
    /// colour drawn.
    pub fn set_host_palette(&mut self, palette: [Option<Rgb>; 16]) {
        self.screen.set_host_palette(palette);
    }
    /// Tells the parser what the host's terminal draws: the size of a cell
    /// in pixels, which `CSI 16 t` and `CSI 14 t` are answered with, and
    /// whether it shows sixel graphics, which primary device attributes
    /// then say (`4`) with [`Feature::Pictures`]; without it, sixel is not
    /// shown ([`Graphics`]).
    pub fn set_host_graphics(&mut self, graphics: Graphics) {
        let shown = match graphics {
            Graphics::Sixel(cell) if !self.options.has(Feature::Pictures) => Graphics::Cell(cell),
            Graphics::Text | Graphics::Cell(_) | Graphics::Sixel(_) => graphics,
        };
        self.screen.set_graphics(shown);
    }
    /// Resizes the terminal, reflowing the primary screen with
    /// [`Feature::Reflow`]. A resize refused room leaves the screen as it
    /// was, and history a resize could not keep is let go; each counts in
    /// [`Parser::refused`].
    pub fn resize(&mut self, size: Size) -> Result<(), Error> {
        let degrades = crate::degrades();
        let resized = self.screen.resize(size, self.options.has(Feature::Reflow));
        self.count_degrades(degrades);
        if resized.is_err() {
            self.refused = self.refused.saturating_add(1);
        }
        resized
    }
    /// The primary screen's current history budget, in bytes.
    pub fn history_bytes(&self) -> usize {
        self.screen.history_bytes()
    }

    /// Keeps at most `bytes` of the primary screen's history from now on:
    /// lowering it lets the oldest rows go at once.
    pub fn set_history_bytes(&mut self, bytes: usize) {
        self.screen.set_history_bytes(bytes);
    }
    /// The size report to send a program that set in-band resize (mode
    /// 2048) once its terminal has the new size, as the spec requires:
    /// `CSI 48 ; rows ; cols ; 0 ; 0 t`, or `None` if the mode is not set.
    pub fn resize_report(&self) -> Option<Vec<u8>> {
        self.screen
            .mode(Mode::InBandResize)
            .then(|| self.screen.size_report().as_bytes().to_vec())
    }
    /// How many times carrying out output, or a resize, was refused room
    /// (THREATS.md 1.3). Each refusal loses only what it was for.
    pub fn refused(&self) -> u64 {
        self.refused
    }
    /// Process output, delivering replies and (with [`Feature::Events`])
    /// events to `sink`, and stop right after a sequence that begins or ends a frame drawn in synchronized output
    /// ([`Stop::Begun`], [`Stop::Ended`]), or at the byte where the call has
    /// spent its work budget ([`Stop::Budget`]), and say how many bytes that
    /// took and why; `None` if every byte was processed. A frame's start or
    /// end is found exactly as the parser reads it, split across calls or
    /// beside other modes in one sequence. A host that holds a program's
    /// frames holds the rest from a [`Stop::Begun`] on, unread, until
    /// [`Parser::frame_scan`] finds its end. After a [`Stop::Budget`], the
    /// rest is given again in a later call, made even if no byte is left
    /// while [`Parser::has_work_left`]: the next call finishes what the
    /// last left (REP's copies) before reading on; replies keep their order.
    ///
    /// The budget (THREATS.md 4) is spent by the work the bytes ask for,
    /// a unit an element visited: a cell, a row, a text byte, a pixel. It
    /// runs over calls, from one budget stop to the next, so a host that
    /// cuts its reads small is stopped as well. It is looked at after each
    /// sequence or string, each run of text, each character read alone, each
    /// control, each sixel byte, each wrap, each glyph inserted and each
    /// glyph that joins or does not fit a run, so a call overruns it by at
    /// most the one indivisible operation that crossed it (THREATS.md 4). A
    /// call that ends a frame ([`Stop::Begun`], [`Stop::Ended`]) past the
    /// budget leaves its stop to the next, which makes it before any byte.
    ///
    /// A refusal of room is counted ([`Parser::refused`]) and the rest
    /// read; `Err(Error::Capacity)` says
    /// one happened in a call that read every byte.
    pub fn process_until_stop(
        &mut self,
        bytes: &[u8],
        sink: &mut impl Sink,
    ) -> Result<Option<(usize, Stop)>, Error> {
        self.run(bytes, sink)
    }
    /// Reads every byte of `bytes`, calling [`Parser::process_until_stop`]
    /// again after each stop until none is left and no work is owed: for
    /// a host that holds no frames and spreads no work over turns, as a
    /// replay, a converter or a test does. It gives how many times room was
    /// refused on the way ([`Parser::refused`] counts them all), most often
    /// 0: every byte is read all the same, and each refusal lost only what
    /// it was for (a row of history, a picture, a resize; THREATS.md 1.3),
    /// so the screen is good to read whatever the count.
    #[must_use = "a refusal lost what it was for (a row of history, a picture): count it, or say why not"]
    pub fn process_all(&mut self, bytes: &[u8], sink: &mut impl Sink) -> u64 {
        let refused = self.refused;
        let mut rest = bytes;
        // `run` errs only at the end of `bytes`, every byte read.
        while let Ok(Some((taken, _))) = self.run(rest, sink) {
            rest = rest.get(taken..).unwrap_or_default();
        }
        self.refused.saturating_sub(refused)
    }
    /// Whether the last call of [`Parser::process_until_stop`] stopped at
    /// its budget with work left to do: another call is owed, of no bytes
    /// if none wait.
    pub fn has_work_left(&self) -> bool {
        self.screen.left_over()
    }
    /// A [`FrameScan`] for the frame [`Parser::process_until_stop`] just
    /// stopped at the start of ([`Stop::Begun`]).
    pub fn frame_scan(&self) -> FrameScan {
        FrameScan {
            from: 0,
            pending: Pending::Ground,
            params: Parameters::default(),
            options: self.options,
            saved: self.screen.saved_mode(Mode::SynchronizedOutput),
        }
    }
    /// Processes `bytes`, stopping as `process_until_stop` does.
    fn run(&mut self, bytes: &[u8], sink: &mut impl Sink) -> Result<Option<(usize, Stop)>, Error> {
        let ran = self.run_on(bytes, sink);
        // Every byte is read but those after a stop.
        let read = match ran {
            Ok(Some((taken, _))) => taken,
            Ok(None) | Err(_) => bytes.len(),
        };
        self.screen.read(read);
        ran
    }
    /// `run`, but for counting the bytes read.
    fn run_on(
        &mut self,
        bytes: &[u8],
        sink: &mut impl Sink,
    ) -> Result<Option<(usize, Stop)>, Error> {
        // No bytes: only what the last call left at its budget, if any.
        if bytes.is_empty() && !self.screen.left_over() {
            return Ok(None);
        }
        self.screen.begin();
        let (start, refused) = (crate::budget::spent(), self.refused);
        let degrades = crate::degrades();
        // Every call leaves `end` and `stop_at` at u64::MAX: no budget.
        // A call that ended a frame past the budget owes its stop: this one
        // makes it before anything, so a frame's boundary is no way past
        // the budget.
        if self.spent >= crate::budget::LIMIT {
            self.spent = 0;
            return Ok(Some((0, Stop::Budget)));
        }
        self.end = start.saturating_add(crate::budget::LIMIT.saturating_sub(self.spent));
        (self.screen.stop_at, self.screen.stopped) = (self.end, false);
        // What the last call left at its budget goes first.
        if self.screen.left_over() && self.resume() {
            (self.screen.stop_at, self.screen.stopped) = (u64::MAX, false);
            self.spend_since(start, Stop::Budget);
            self.count_degrades(degrades);
            return Ok(Some((0, Stop::Budget)));
        }
        let mut remaining = bytes;
        while let Some((&byte, tail)) = remaining.split_first() {
            let ground = matches!(self.state, State::Ground);
            // A refusal degrades what it was for, and the rest is read
            // (`count_degrades`, THREATS.md 1.3, B44).
            let framed = if ground && (0x20..=0x7e).contains(&byte) {
                let length = printable(remaining);
                let (run, rest) = remaining
                    .split_at_checked(length)
                    .unwrap_or((remaining, &[]));
                let taken = self.screen.ascii(run);
                remaining = rest;
                // A wrap scrolled inside margins past the budget.
                if self.screen.stopped {
                    remaining = (bytes.len().saturating_sub(remaining.len()))
                        .checked_sub(length)
                        .and_then(|at| bytes.get(at.saturating_add(taken)..))
                        .unwrap_or_default();
                    self.screen.stopped = false;
                    Some(Stop::Budget)
                } else {
                    // A run that ends before the margin printed too.
                    self.over()
                }
            } else if ground && byte < 0x20 && byte != 0x1b {
                // CR LF, which ends most lines, as one.
                let fed = if let [b'\r', b'\n', after @ ..] = remaining {
                    self.screen.carriage_return_line_feed();
                    remaining = after;
                    true
                } else {
                    self.control(byte, sink);
                    remaining = tail;
                    matches!(byte, 0x0a..=0x0c)
                };
                self.screen.forget_repeat();
                // A line feed can move a row's text into history; a SUB
                // prints and a tab looks for stops, which look themselves.
                self.over_after(false, fed)
            } else if ground
                && byte >= 0x80
                && let Some(length) = self.text(remaining)
            {
                remaining = remaining.get(length..).unwrap_or_default();
                self.screen.stopped = false;
                self.over()
            } else if ground && byte == 0x1b && remaining.get(1) == Some(&b'[') {
                self.screen.cancel_single_shift();
                let (length, framed) = self.csi_sequence(remaining, sink);
                remaining = remaining.get(length..).unwrap_or_default();
                framed.or_else(|| self.over_after(true, false))
            } else if let Some(rest) = self.string(remaining) {
                remaining = rest;
                None
            } else {
                remaining = tail;
                let fed = matches!(byte, 0x0a..=0x0c) && !self.in_string();
                let framed = self.byte(byte, sink);
                // Whatever left the parser in ground: a sequence's end, a
                // character completed or printed alone.
                framed.or_else(|| self.over_after(true, fed))
            };
            if let Some(stop) = framed {
                (self.screen.stop_at, self.screen.stopped) = (u64::MAX, false);
                self.spend_since(start, stop);
                self.count_degrades(degrades);
                let taken = bytes.len().saturating_sub(remaining.len());
                return Ok(Some((taken, stop)));
            }
        }
        (self.screen.stop_at, self.screen.stopped) = (u64::MAX, false);
        self.spend_since(start, Stop::Ended);
        self.count_degrades(degrades);
        match self.refused == refused {
            true => Ok(None),
            false => Err(Error::Capacity),
        }
    }
    /// Counts in [`Parser::refused`] the refusals this call went on past by
    /// degrading (`crate::degraded`) since `degrades`; out of line, as a
    /// call seldom meets one.
    #[inline]
    fn count_degrades(&mut self, degrades: u64) {
        if crate::degrades() != degrades {
            self.count_degrades_met(degrades);
        }
    }
    #[cold]
    #[inline(never)]
    fn count_degrades_met(&mut self, degrades: u64) {
        let met = crate::degrades().saturating_sub(degrades);
        self.refused = self.refused.saturating_add(met);
    }
    /// Spending carries over calls until a stop at the budget.
    fn spend_since(&mut self, start: u64, stop: Stop) {
        self.end = u64::MAX;
        self.spent = match stop {
            Stop::Budget => 0,
            Stop::Begun | Stop::Ended => {
                (self.spent).saturating_add(crate::budget::spent().saturating_sub(start))
            }
        };
    }
    /// `over`, only where the parser came back to ground after a byte it
    /// read alone or a sequence (`sequence`), a line feed was carried out
    /// (`fed`), a sixel byte was read, or printing, SUB or a tab found the
    /// budget spent (`Screen::check_budget`): where the work a few bytes can multiply is
    /// done. A rule of states, so the fast paths look where `byte` alone
    /// would.
    #[inline]
    fn over_after(&mut self, sequence: bool, fed: bool) -> Option<Stop> {
        let printed = self.screen.stopped;
        let at = fed || printed || sequence && matches!(self.state, State::Ground | State::Sixel);
        if !at {
            return None;
        }
        self.screen.stopped = false;
        self.over()
    }
    /// Whether the parser is in a string or a DCS, where a C0 control is
    /// not carried out.
    fn in_string(&self) -> bool {
        matches!(
            self.state,
            State::Dcs(_)
                | State::IgnoreString
                | State::Sixel
                | State::Request(_)
                | State::Osc { .. }
        )
    }
    /// Makes the copies a REP the last call stopped in left; whether the
    /// call stops at once, its budget spent again (`Screen::check_budget`).
    #[cold]
    #[inline(never)]
    pub(super) fn resume(&mut self) -> bool {
        self.screen.resume_repeat();
        let stopped = std::mem::take(&mut self.screen.stopped);
        stopped && self.over().is_some()
    }
    #[inline]
    fn over(&self) -> Option<Stop> {
        (crate::budget::spent() >= self.end).then_some(Stop::Budget)
    }
    /// Prints the run of valid non-ASCII UTF-8 `bytes` begins with; how many
    /// bytes it took, or `None` for invalid or incomplete UTF-8, left to the
    /// byte-by-byte path.
    fn text(&mut self, bytes: &[u8]) -> Option<usize> {
        let mut chars = Chars { bytes, taken: 0 };
        self.screen.text(&mut chars);
        (chars.taken > 0).then_some(chars.taken)
    }
    /// The string a DCS with final byte `byte` and intermediates `read`
    /// begins: a DECRQSS, a sixel picture, or one that is dropped (an
    /// XTGETTCAP, answered at its end).
    #[inline(never)]
    fn dcs_string(&mut self, read: Intermediates, byte: u8) -> State {
        // DECRQSS: `$ q` with no parameters; XTGETTCAP, `+ q`.
        let request = |marker: &[u8]| {
            self.options.has(Feature::SettingReports)
                && byte == b'q'
                && read.as_slice() == marker
                && self.params.is_empty()
        };
        let (decrqss, termcap) = (request(b"$"), request(b"+"));
        // Sixel: `q` with no intermediates, with `Feature::Pictures`; kept
        // only while the host draws sixel, as DA1 says (with a cell size to
        // lay it out in, `set_host_graphics`), else read only to tell
        // whether it drew anything, which its end counts.
        let sixel = byte == b'q' && read.as_slice().is_empty();
        if decrqss {
            State::Request(Some(Bytes::default()))
        } else if termcap {
            self.termcap = true;
            State::IgnoreString
        } else if sixel && self.options.has(Feature::Pictures) {
            self.sixel = Some(crate::sixel::Decoder::new(
                self.params.first(0, 0),
                self.params.first(1, 0),
                self.screen.sixel(),
            ));
            State::Sixel
        } else {
            State::IgnoreString
        }
    }
    /// Reads the CSI `bytes` begins with, `ESC [` read in ground state, as
    /// far as it is the common shape: a private marker, digits, `;` and
    /// `:`, and a final byte. Each byte is done exactly as `byte` does it;
    /// it stops before any other byte with the parser in the state `byte`
    /// would have left, for `byte` to go on from. Says how many bytes it
    /// took, and whether the sequence began or ended a frame.
    fn csi_sequence(&mut self, bytes: &[u8], sink: &mut impl Sink) -> (usize, Option<Stop>) {
        self.params.clear();
        // The parameter being read and how many there are, kept here until
        // the sequence ends. The value saturates at u16::MAX as
        // `Parameters::digit` does: `(v * 10).min(MAX) + d` saturated is
        // `(v * 10 + d).min(MAX)`.
        let (mut len, mut value) = (1usize, 0u32);
        let max = u32::from(u16::MAX);
        let mut marker = Intermediates::default();
        let mut at = 2usize;
        while let Some(&byte) = bytes.get(at) {
            let next = at.wrapping_add(1);
            match byte {
                b'0'..=b'9' => {
                    let digit = u32::from(byte.wrapping_sub(b'0'));
                    value = value.wrapping_mul(10).wrapping_add(digit).min(max);
                }
                b';' | b':' => {
                    let ended = u16::try_from(value).unwrap_or(u16::MAX);
                    match self.params.next(len, ended, byte == b':') {
                        Some(more) => (len, value) = (more, 0),
                        // Past the slots: the rest goes byte by byte.
                        None => {
                            self.params.end(len, ended);
                            self.params.full = true;
                            self.state = State::Csi(Phase::Param(marker));
                            return (next, None);
                        }
                    }
                }
                0x3c..=0x3f if at == 2 => {
                    marker = Intermediates::of(byte);
                    // A parameter follows, or `digit` will say so.
                    self.params.given = matches!(bytes.get(next), Some(b'0'..=b';'));
                }
                0x40..=0x7e => {
                    self.params
                        .end(len, u16::try_from(value).unwrap_or(u16::MAX));
                    self.state = State::Ground;
                    let framed = self.csi_dispatch(marker.as_slice(), byte, sink);
                    self.screen.forget_repeat();
                    return (next, framed);
                }
                _ => break,
            }
            at = next;
        }
        self.params
            .end(len, u16::try_from(value).unwrap_or(u16::MAX));
        self.state = State::Csi(if at == 2 {
            Phase::Entry
        } else {
            Phase::Param(marker)
        });
        (at, None)
    }
    /// Takes the OSC string's bytes `bytes` begins with, up to the BEL,
    /// CAN, SUB or ESC that ends it, keeping what `byte` would keep of
    /// them; how many it took.
    #[inline(never)]
    fn osc_more(&mut self, bytes: &[u8], whole: bool) -> usize {
        // The byte past the limit marks the string overflowed, and one
        // that is not whole holds as many as the limit already.
        let room = self.osc_limit.saturating_add(1);
        let mut length = 0;
        // Each run up to a control is copied whole, with the control if
        // the string holds it.
        loop {
            let rest = bytes.get(length..).unwrap_or_default();
            let run = control_in(rest).unwrap_or(rest.len());
            let control = rest.get(run).copied();
            let ends = matches!(control, Some(0x07 | 0x18 | 0x1a | 0x1b) | None);
            let held = control.is_some_and(in_command_string);
            let kept = run.saturating_add(usize::from(held));
            let kept = kept.min(room.saturating_sub(self.osc.len()));
            self.osc_room(kept);
            self.osc
                .extend_from_slice(rest.get(..kept).unwrap_or_default());
            length = length.saturating_add(run);
            if ends {
                break;
            }
            length = length.saturating_add(1);
        }
        self.state = State::Osc {
            whole: whole && self.osc.len() <= self.osc_limit,
        };
        self.osc.truncate(self.osc_limit);
        length
    }
    /// In an OSC or an ignored string, takes the bytes `bytes` begins with
    /// that go on it, as `byte` would; the rest of `bytes`, or `None` if it
    /// took none.
    #[inline(never)]
    fn string<'a>(&mut self, bytes: &'a [u8]) -> Option<&'a [u8]> {
        let taken = match self.state {
            State::Osc { whole } => self.osc_more(bytes, whole),
            State::IgnoreString => string_end(bytes).unwrap_or(bytes.len()),
            State::Ground
            | State::Utf8(_)
            | State::Escape(_)
            | State::EscapeIgnore
            | State::Csi(_)
            | State::CsiIgnore
            | State::Dcs(_)
            | State::Sixel
            | State::Request(_) => 0,
        };
        (taken > 0).then(|| bytes.get(taken..).unwrap_or_default())
    }

    /// Room in the OSC buffer for `more` bytes: grown by doubling up to 16
    /// KiB, then at once to the limit the features set and the byte that
    /// marks it overflowed, never past them (THREATS.md B20).
    #[inline]
    fn osc_room(&mut self, more: usize) {
        let need = self.osc.len().saturating_add(more);
        if need > self.osc.capacity() {
            self.grow_osc(need);
        }
    }
    #[cold]
    #[inline(never)]
    fn grow_osc(&mut self, need: usize) {
        const DOUBLING: usize = 16 << 10;
        let doubled = need.max(self.osc.capacity().saturating_mul(2));
        let most = self.osc_limit.saturating_add(1);
        let room = match doubled > DOUBLING {
            true => most,
            false => doubled.min(most),
        };
        self.osc.reserve_exact(room.saturating_sub(self.osc.len()));
    }

    fn ground(&mut self, byte: u8, sink: &mut impl Sink) {
        match byte {
            0x1b => {
                self.screen.cancel_single_shift();
                self.state = State::Escape(Intermediates::default());
            }
            0x00..=0x1f | 0x7f => {
                self.control(byte, sink);
                self.screen.forget_repeat();
            }
            0x20..=0x7e => self.screen.print(char::from(byte)),
            0xc2..=0xf4 => self.state = State::Utf8(Bytes::of(byte)),
            // A byte that starts no UTF-8 sequence, a continuation byte
            // alone among them: U+FFFD (Unicode 3.9; C10). xterm reads a
            // continuation byte alone as Latin-1.
            _ => self.screen.print(char::REPLACEMENT_CHARACTER),
        }
    }

    fn continuation(&mut self, read: Bytes<4>, byte: u8, sink: &mut impl Sink) {
        let [first, ..] = read.bytes;
        // Only a lead byte begins a character's bytes (`ground`).
        let (need, low, high) = lead(first).unwrap_or((1, 0xff, 0));
        let (low, high) = if read.len == 1 {
            (low, high)
        } else {
            (0x80, 0xbf)
        };
        let valid = (low..=high).contains(&byte);
        self.state = State::Ground;
        match read.push(byte).filter(|_| valid) {
            Some(read) if usize::from(read.len) < need => self.state = State::Utf8(read),
            Some(read) => {
                if let Some((c, _)) = decode(read.as_slice()) {
                    self.screen.print(c);
                }
            }
            // An invalid sequence: what came of it is one U+FFFD (Unicode
            // 17, 3.9, "U+FFFD Substitution of Maximal Subparts"), as in
            // xterm. Then this byte is read again; it can be ESC.
            None => {
                self.screen.print(char::REPLACEMENT_CHARACTER);
                self.ground(byte, sink);
            }
        }
    }

    fn control(&mut self, byte: u8, sink: &mut impl Sink) {
        if byte == 7 && self.options.has(Feature::Events) {
            sink.event(Event::Bell);
        }
        self.screen.control(byte)
    }

    /// BEL inside a string that rings it (sixel): as in text.
    #[cold]
    #[inline(never)]
    fn bell(&mut self, sink: &mut impl Sink) {
        self.control(0x07, sink);
    }

    /// CAN or SUB, `byte`, in `state`, any but ground: what is in progress
    /// is cancelled, and SUB then shows its glyph (DEC STD 070, p. 5-132,
    /// note 2; and in a control string, README, "SUB shows its error
    /// character"); inside sixel SUB is a blank sixel instead and the
    /// picture goes on, and CAN ends it unplaced (the sixel chapter, 9.1).
    #[cold]
    #[inline(never)]
    fn cancel(&mut self, byte: u8, state: State) {
        if byte == 0x1a
            && matches!(state, State::Sixel)
            && let Some(decoder) = &mut self.sixel
        {
            let screen = &mut self.screen;
            decoder.push(b'?', &mut |bytes, evict| screen.make_room(bytes, evict));
            return;
        }
        self.state = State::Ground;
        self.sixel = None;
        self.termcap = false;
        if byte == 0x1a && !matches!(state, State::Sixel) {
            self.screen.substitute();
        }
        self.screen.forget_repeat();
    }

    /// Reads one byte; whether it ended a frame-starting or -ending sequence.
    fn byte(&mut self, byte: u8, sink: &mut impl Sink) -> Option<Stop> {
        let state = self.state;
        if matches!(state, State::Ground) {
            self.ground(byte, sink);
            return None;
        }
        if let State::Utf8(read) = state {
            self.continuation(read, byte, sink);
            return None;
        }
        // Anywhere transitions: CAN/SUB cancel, ESC starts a new sequence.
        // ESC also begins ST, so it completes a pending string.
        if matches!(byte, 0x18 | 0x1a) {
            self.cancel(byte, state);
            return None;
        }
        if byte == 0x1b {
            // The ESC begins a sequence whatever carrying out the string
            // does.
            self.state = State::Escape(Intermediates::default());
            match state {
                State::Osc { whole } => self.dispatch_osc(whole, false, sink),
                State::Request(request) => self.dispatch_request(request, sink),
                // ctlseqs, XTGETTCAP: "DCS 0 + r ST for invalid requests";
                // fux-vt knows no capability names.
                State::IgnoreString if std::mem::take(&mut self.termcap) => {
                    sink.reply(b"\x1bP0+r\x1b\\");
                }
                State::Sixel => {
                    let screen = &mut self.screen;
                    let keeps = self.sixel.as_ref().is_some_and(|d| d.keeps());
                    let (picture, whole) = (self.sixel.take()).map_or((None, true), |decoder| {
                        decoder.finish(&mut |bytes, evict| screen.make_room(bytes, evict))
                    });
                    // One begun while the host drew no sixel, ended once it
                    // drew none (or had no cell to lay it out in), the
                    // host's having gone as it was read, or that lost bands
                    // as it was placed, is not placed; one cut is counted
                    // too, placed or not: each once.
                    let drawn = keeps && self.screen.sixel();
                    let placed = picture.map(|p| drawn && self.screen.place_picture(p));
                    if !whole || placed == Some(false) {
                        sink.picture(false);
                    } else if placed == Some(true) {
                        sink.picture(true);
                    }
                }
                State::Ground
                | State::Utf8(_)
                | State::Escape(_)
                | State::EscapeIgnore
                | State::Csi(_)
                | State::CsiIgnore
                | State::Dcs(_)
                | State::IgnoreString => {}
            }
            return None;
        }
        let mut framed = None;
        match state {
            State::Ground | State::Utf8(_) => {}
            State::Osc { whole } => match byte {
                0x07 => {
                    self.state = State::Ground;
                    self.dispatch_osc(whole, true, sink);
                }
                _ if !whole || !in_command_string(byte) => return None,
                _ if self.osc.len() < self.osc_limit => {
                    self.osc_room(1);
                    self.osc.push(byte);
                    return None;
                }
                // What came first stays: a prompt mark, and an OSC 8 too
                // long to keep, which closes the link, are told by it.
                _ => {
                    self.state = State::Osc { whole: false };
                    return None;
                }
            },
            // In UTF-8 a string ends only at ESC (ST is ESC \, ECMA-48
            // 8.3.143): the byte 0x9c, 8-bit ST, is part of a character
            // there, as in `\u{271c}` (e2 9c 9c).
            // C0 controls are no part of a request: "all other control
            // codes not used within a command string" are ignored (DEC STD
            // 070, 3.5.4.4).
            State::Request(_) if byte < 0x20 => {}
            State::Request(request) => {
                self.state = State::Request(request.and_then(|r| r.push(byte)));
                return None;
            }
            State::IgnoreString => return None,
            State::Sixel => {
                // BEL rings as in text (DEC STD 070, sixel graphics, 9.1).
                if byte == 0x07 {
                    self.bell(sink);
                    return None;
                }
                if let Some(decoder) = &mut self.sixel {
                    let screen = &mut self.screen;
                    decoder.push(byte, &mut |bytes, evict| screen.make_room(bytes, evict));
                }
                return None;
            }
            State::Escape(read) => match byte {
                0x00..=0x1f => self.control(byte, sink),
                0x20..=0x2f => {
                    self.state = read.push(byte).map_or(State::EscapeIgnore, State::Escape);
                }
                0x30..=0x7e => match (read.as_slice(), byte) {
                    ([], b'[') => {
                        self.params.clear();
                        self.state = State::Csi(Phase::Entry);
                    }
                    ([], b'P') => {
                        self.params.clear();
                        self.state = State::Dcs(Phase::Entry);
                    }
                    ([], b']') => {
                        self.osc.clear();
                        self.state = State::Osc { whole: true };
                    }
                    ([], b'X' | b'^' | b'_') => self.state = State::IgnoreString,
                    (intermediates, _) => {
                        self.state = State::Ground;
                        framed = self.escape_dispatch(intermediates, byte, sink);
                    }
                },
                _ => {}
            },
            State::EscapeIgnore => match byte {
                0x00..=0x1f => self.control(byte, sink),
                0x30..=0x7e => self.state = State::Ground,
                _ => {}
            },
            State::Csi(phase) => match phase.next(byte, &mut self.params) {
                Next::Phase(phase) => self.state = State::Csi(phase),
                Next::Ignore => self.state = State::CsiIgnore,
                Next::Control => self.control(byte, sink),
                Next::Final(read) => {
                    self.state = State::Ground;
                    framed = self.csi_dispatch(read.as_slice(), byte, sink);
                }
            },
            State::CsiIgnore => match byte {
                0x00..=0x1f => self.control(byte, sink),
                0x40..=0x7e => self.state = State::Ground,
                _ => {}
            },
            State::Dcs(phase) => match phase.next(byte, &mut self.params) {
                Next::Phase(phase) => self.state = State::Dcs(phase),
                Next::Ignore => self.state = State::IgnoreString,
                Next::Control => {}
                Next::Final(read) => self.state = self.dcs_string(read, byte),
            },
        }
        // After a sequence or string, REP has no character to repeat until
        // one is printed.
        if matches!(self.state, State::Ground) {
            self.screen.forget_repeat();
        }
        framed
    }

    /// Carries out an escape sequence; whether it ended a frame (RIS).
    fn escape_dispatch(
        &mut self,
        intermediates: &[u8],
        byte: u8,
        sink: &mut impl Sink,
    ) -> Option<Stop> {
        let framing = self.screen.mode(Mode::SynchronizedOutput);
        // DECID (ctlseqs: "Obsolete form of CSI c"): answered as DA1 is.
        if intermediates.is_empty() && byte == b'Z' {
            let reply = self.screen.primary_attributes(&self.options);
            sink.reply(reply.as_bytes());
        } else if !self.screen.escape(intermediates, byte) {
            sink.unhandled(Unhandled::Escape {
                intermediates,
                action: byte,
            });
        }
        let ended = framing && !self.screen.mode(Mode::SynchronizedOutput);
        ended.then_some(Stop::Ended)
    }

    /// Carries out a CSI sequence; whether it began or ended a frame.
    fn csi_dispatch(
        &mut self,
        intermediates: &[u8],
        byte: u8,
        sink: &mut impl Sink,
    ) -> Option<Stop> {
        // Only SM and RM, XTRESTORE, DECSTR and DECSCL change synchronized
        // output (`Screen::csi` keeps resizes out; `FrameScan::ends`
        // restates the rest): the mode is read around those alone.
        if !matches!(byte, b'h' | b'l' | b'r' | b'p') {
            let dispatch = (self.screen).csi(&self.params, intermediates, byte, &self.options);
            self.carry_out(dispatch, intermediates, byte, sink);
            return None;
        }
        let (begun, framing) = (
            self.screen.frames_begun(),
            self.screen.mode(Mode::SynchronizedOutput),
        );
        let dispatch = (self.screen).csi(&self.params, intermediates, byte, &self.options);
        // A frame begun or ended is told even if room was refused on the
        // way: the refusal loses only what it was for.
        let framed = if self.screen.frames_begun() != begun {
            Some(Stop::Begun)
        } else {
            let ended = framing && !self.screen.mode(Mode::SynchronizedOutput);
            ended.then_some(Stop::Ended)
        };
        self.carry_out(dispatch, intermediates, byte, sink);
        framed
    }

    /// The rest of a CSI `Screen::csi` carried out as `dispatch` says: the
    /// sequences it left to `other_csi`, a reply, or an unhandled one.
    #[inline]
    fn carry_out(
        &mut self,
        dispatch: Dispatch,
        intermediates: &[u8],
        byte: u8,
        sink: &mut impl Sink,
    ) {
        let dispatch = match dispatch {
            Dispatch::Unhandled => (self.screen)
                .other_csi(&self.params, intermediates, byte, &self.options)
                .unwrap_or(Dispatch::Unhandled),
            done @ (Dispatch::Done | Dispatch::Reply(_)) => done,
        };
        match dispatch {
            Dispatch::Done => {}
            Dispatch::Reply(reply) => sink.reply(reply.as_bytes()),
            Dispatch::Unhandled => sink.unhandled(Unhandled::Csi {
                params: Params(&self.params),
                intermediates,
                action: byte,
            }),
        }
    }

    /// Carries out the completed OSC string, cut short unless `whole`,
    /// ended by BEL if `bel`, else by ST. An overflowed string's event is
    /// `cut`; it sets no colour, and closes any link. Out of line, so
    /// `byte` stays small enough to inline into the parser's loop.
    #[inline(never)]
    fn dispatch_osc(&mut self, whole: bool, bel: bool, sink: &mut impl Sink) {
        let payload = std::mem::take(&mut self.osc);
        self.osc_command(&payload, whole, bel, sink);
        self.osc = payload;
        self.osc.clear();
    }

    /// Answers the DECRQSS whose string just ended.
    #[inline(never)]
    fn dispatch_request(&mut self, request: Option<Bytes<REQUEST_LIMIT>>, sink: &mut impl Sink) {
        let mut setting = String::new();
        let status = match request.map(|r| self.screen.setting_report(r.as_slice(), &mut setting)) {
            Some(Setting::Written) => b'1',
            Some(Setting::Unknown) | None => b'0',
        };
        sink.reply(format!("\x1bP{}$r{setting}\x1b\\", char::from(status)).as_bytes());
    }

    fn osc_command(&mut self, payload: &[u8], whole: bool, bel: bool, sink: &mut impl Sink) {
        let (command, rest) = crate::split_semicolon(payload).unwrap_or((payload, &[]));
        match command {
            b"133" if self.options.has(Feature::PromptMarks) => {
                let before = self.screen.commands();
                self.screen.prompt_osc(rest);
                let commands = self.screen.commands();
                if self.options.has(Feature::Events) {
                    if commands.started != before.started {
                        sink.event(Event::CommandStarted);
                    }
                    if commands.finished != before.finished {
                        sink.event(Event::CommandEnded(commands.last_status));
                    }
                }
                return;
            }
            // A link too long to keep is no link.
            b"8" if self.options.has(Feature::Hyperlinks) => {
                self.screen.hyperlink_osc(if whole { rest } else { &[] });
                return;
            }
            _ => {}
        }
        if self.options.has(Feature::Palette)
            && whole
            && crate::palette::osc(
                self.screen.colours_mut(),
                command,
                rest,
                bel,
                self.options.has(Feature::Events),
                sink,
            )
        {
            return;
        }
        if !self.options.has(Feature::Events) {
            return;
        }
        // What the string's start holds, the rest cut if it overflowed.
        let kept = |bytes| Kept { bytes, cut: !whole };
        match command {
            b"0" => {
                self.screen.set_title(kept(rest));
                sink.event(Event::IconName(kept(rest)));
                sink.event(Event::Title(kept(rest)));
            }
            b"1" => sink.event(Event::IconName(kept(rest))),
            b"2" => {
                self.screen.set_title(kept(rest));
                sink.event(Event::Title(kept(rest)));
            }
            // OSC 9 ; 4 is ConEmu's progress report, not a notification.
            b"9" if !rest.starts_with(b"4;") => sink.event(Event::Notification(kept(rest))),
            b"777" => {
                if let Some(text) = rest.strip_prefix(b"notify;") {
                    sink.event(Event::Notification(kept(text)));
                }
            }
            b"99" => {
                let text = crate::split_semicolon(rest).map(|(_, text)| text);
                sink.event(Event::Notification(kept(text.unwrap_or_default())));
            }
            b"52" => {
                if let Some((selection, data)) = crate::split_semicolon(rest)
                    && data != b"?"
                {
                    let data = kept(data);
                    sink.event(Event::Clipboard { selection, data });
                }
            }
            _ if !whole => {}
            // OSC 10 to 19 the palette did not read (it is off, or the
            // number is written `010`) still ask the host for their
            // colours; what they set is kept nowhere.
            _ => {
                let number = str::from_utf8(command).ok();
                let number = number.and_then(|n| n.parse::<u8>().ok());
                if let Some(first) = number.and_then(|n| n.checked_sub(10)).filter(|n| *n < 10) {
                    crate::palette::dynamic(&mut None, first, rest, bel, true, sink);
                }
            }
        }
    }
}

/// The parser's checks, each shown a parser with its one fault,
/// and the whole checker's report of what ran.
#[cfg(all(test, feature = "checks"))]
mod faults {
    use super::*;

    fn parser() -> Result<Parser, Error> {
        let options = Feature::ALL.into_iter().collect();
        let size = Size::new(4, 8).map_err(|_| Error::Capacity)?;
        Parser::with_options(size, 1 << 12, options)
    }

    fn shows(check: &str, fault: impl FnOnce(&mut Parser)) -> Result<(), Error> {
        let mut parser = parser()?;
        assert!(parser.check().is_clean(), "{}", parser.check());
        fault(&mut parser);
        let report = parser.check();
        assert!(
            report.faults_of(check).next().is_some(),
            "{check}: {report}"
        );
        Ok(())
    }

    #[test]
    fn each_check_names_its_fault() -> Result<(), Error> {
        shows("parser.osc", |p| {
            p.osc = vec![b'x'; OSC_PAYLOAD_LIMIT.saturating_add(2)]
        })?;
        shows("parser.params", |p| p.params.len = 0)?;
        shows("parser.sixel", |p| p.state = State::Sixel)?;
        shows("parser.termcap", |p| p.termcap = true)?;
        shows("parser.utf8", |p| {
            p.state = State::Utf8(Bytes {
                bytes: [0xc3, 0xa9, 0, 0],
                len: 2,
            });
        })?;
        Ok(())
    }

    /// Every check of the checker runs on a parser, and is named in its
    /// report: one dropped from `check` would not be.
    #[test]
    fn every_check_is_reported() -> Result<(), Error> {
        let mut parser = parser()?;
        parser.process(b"hello\r\n\x1bPq#1~~\x1b\\")?;
        let report = parser.check();
        let every = [
            "parser.osc",
            "parser.params",
            "parser.sixel",
            "parser.utf8",
            "parser.termcap",
            "grid.order",
            "grid.slots",
            "grid.blank_past_used",
            "grid.blank_past_end",
            "grid.wide_pairs",
            "grid.text",
            "grid.style_numbers",
            "grid.recent",
            "grid.charged",
            "styles.table",
            "styles.spread",
            "grid.versions",
            "grid.cursor",
            "grid.margins",
            "grid.keyboard",
            "links.runs",
            "links.held",
            "history.counts",
            "history.blocks",
            "history.flags",
            "history.budget",
            "history.capacities",
            "screen.ids",
            "screen.structural",
            "screen.tabs",
            "pictures.count",
            "pictures.bytes",
            "pictures.shown",
        ];
        assert_eq!(report.ran(), every.as_slice(), "{report}");
        assert!(report.is_clean(), "{report}");
        let waiting: Vec<&str> = report.waiting().iter().map(|(check, _)| *check).collect();
        assert!(waiting.contains(&"parser.refused"));
        Ok(())
    }

    /// The checker does no work the counters see.
    #[test]
    fn the_checker_is_no_work() -> Result<(), Error> {
        let mut parser = parser()?;
        parser.process(b"hello\r\nworld")?;
        let before = crate::budget::spent();
        let report = parser.check();
        assert!(report.is_clean(), "{report}");
        assert_eq!(crate::budget::spent(), before);
        Ok(())
    }

    /// The frame scan goes on within what it was given.
    #[test]
    fn a_frame_scan_past_its_bytes_is_a_fault() -> Result<(), Error> {
        let parser = parser()?;
        let mut scan = parser.frame_scan();
        assert_eq!(scan.end(b"\x1b[1"), None);
        assert!(scan.check(b"\x1b[1").is_clean());
        scan.from = 99;
        assert!(!scan.check(b"\x1b[1").is_clean());
        Ok(())
    }
}
