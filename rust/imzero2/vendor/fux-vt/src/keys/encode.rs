//! Byte encodings for keys, pastes and focus changes delivered to a
//! program, in the modes it asked for (application cursor keys, the kitty
//! keyboard protocol, modifyOtherKeys, bracketed paste, focus reporting):
//! [`key_bytes`] and [`paste`] take them given, as
//! [`crate::Screen::key_mode`] and [`crate::Screen::mode`] read them from
//! the program's screen, and [`crate::Screen::focus_report`] reads its
//! own. Mouse reports are [`crate::keys::mouse`]'s.
use crate::keys::{
    CAPS_LOCK_BIT, DISAMBIGUATE, Direction, Key, KeyPress, Keystroke, Modifiers,
    NUM_LOCK_BIT, SHIFT_BIT,
};

/// How a pane's program asked for its keys: one of the protocols fux
/// speaks, with only the options that protocol reads.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum KeyMode {
    /// xterm's bytes, which every key has.
    Legacy {
        /// Application cursor keys (DECCKM): the arrows, Home and End as SS3.
        application: bool,
        /// The application keypad (DECKPAM, DECNKM): the keypad keys a
        /// terminal speaking the kitty protocol names send SS3 sequences.
        keypad: bool,
        /// xterm's modifyOtherKeys, if set: the keys it applies to are
        /// `CSI 27 ; mods ; code ~`, the others keep their legacy bytes.
        other_keys: Option<OtherKeysLevel>,
        /// DECBKM (`CSI ? 67 h`): Backspace sends BS, and DEL with Ctrl;
        /// reset, DEL, and BS with Ctrl.
        backarrow: bool,
    },
    /// The kitty keyboard protocol (`references/modern/kitty_keyboard_protocol.html`),
    /// which leaves application cursor keys and modifyOtherKeys unread.
    Kitty {
        /// Report alternate keys (4): a text key's shifted and base-layout keys.
        alternate: bool,
        /// Which keys are escapes.
        report: Report,
        /// DECBKM, which Backspace's legacy byte follows.
        backarrow: bool,
    },
}

/// xterm's modifyOtherKeys levels (`CSI > 4 ; level m`; ctlseqs, "Alt and
/// Meta Keys").
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum OtherKeysLevel {
    /// Level 1: "the usual shift- and control-modifiers work as expected".
    Usual,
    /// Level 2: "all of the modifiers apply".
    All,
    /// Level 3: as 2, and "unmodified keys also are sent as escape
    /// sequences".
    Unmodified,
}

/// Which keys the kitty protocol escapes, as its flags ask.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Report {
    /// Disambiguate (1): keys that type text, with no modifier but Shift,
    /// send their text, and Enter, Tab and Backspace their legacy byte.
    Disambiguated,
    /// Report all keys (8): every key is an escape.
    AllKeys,
    /// Report all keys with their text (8 and 16): a key that types text
    /// carries it as `CSI code ; mods ; text u`.
    AllKeysWithText,
}

impl crate::Screen {
    /// How the program on this screen asked for its keys: the kitty
    /// protocol, if its flags disambiguate or report all keys; else legacy
    /// keys, in the cursor mode and modifyOtherKeys level it set. The other
    /// flags alone change nothing.
    pub fn key_mode(&self) -> KeyMode {
        let flags = self.kitty_keyboard_flags();
        let report = match (flags & ALL_KEYS != 0, flags & TEXT != 0) {
            (true, true) => Report::AllKeysWithText,
            (true, false) => Report::AllKeys,
            (false, _) if flags & DISAMBIGUATE != 0 => Report::Disambiguated,
            (false, _) => {
                return KeyMode::Legacy {
                    application: self.mode(crate::Mode::ApplicationCursor),
                    keypad: self.mode(crate::Mode::ApplicationKeypad),
                    other_keys: self.modify_other_keys(),
                    backarrow: self.mode(crate::Mode::BackarrowSendsBackspace),
                };
            }
        };
        KeyMode::Kitty {
            alternate: flags & ALTERNATE_KEYS != 0,
            report,
            backarrow: self.mode(crate::Mode::BackarrowSendsBackspace),
        }
    }

    /// A focus change as the program on this screen asked for them
    /// (`CSI ? 1004 h`): `CSI I` gained, `CSI O` lost; else nothing.
    pub fn focus_report(&self, focused: bool) -> Option<&'static [u8]> {
        let report: &[u8] = if focused { b"\x1b[I" } else { b"\x1b[O" };
        self.mode(crate::Mode::FocusReporting).then_some(report)
    }
}

/// The kitty protocol's progressive enhancements, as a screen holds them.
const ALTERNATE_KEYS: u8 = 4;
const ALL_KEYS: u8 = 8;
const TEXT: u8 = 16;
/// The kitty protocol's lock modifiers, Caps Lock and Num Lock.
const LOCKS: u8 = CAPS_LOCK_BIT | NUM_LOCK_BIT;

/// What a bracketed paste begins with (`CSI 200 ~`).
pub(crate) const PASTE_START: &[u8] = b"\x1b[200~";
/// What a bracketed paste ends with (`CSI 201 ~`).
pub(crate) const PASTE_END: &[u8] = b"\x1b[201~";

/// Appends a paste as the pane should receive it to `out`: each line end
/// (`\n` or `\r\n`) one CR, as xterm 411 sends a `\n` (`button.c`, its
/// `pasteLiteralNL` resource off by default), though xterm sends `\r\n` as
/// two, which a shell runs as two Enters; framed if
/// it asked for bracketed paste, with every end marker inside the text
/// removed (`CSI 201 ~`, ESC `[` or the C1 CSI, U+009B), as often as
/// removing one makes another, so the text cannot end the paste early.
/// Unbracketed, the program asked for no frame to break.
pub fn paste(text: &str, bracketed: bool, out: &mut Vec<u8>) {
    let crs;
    let text = if text.contains('\n') {
        crs = text.replace("\r\n", "\r").replace('\n', "\r");
        crs.as_str()
    } else {
        text
    };
    if !bracketed {
        return out.extend_from_slice(text.as_bytes());
    }
    out.extend_from_slice(PASTE_START);
    // A marker can only be made by removing one, so text with none goes as
    // it is.
    if !text.contains("201~") {
        out.extend_from_slice(text.as_bytes());
        return out.extend_from_slice(PASTE_END);
    }
    let start = out.len();
    for c in text.chars() {
        out.extend_from_slice(c.encode_utf8(&mut [0; 4]).as_bytes());
        // A marker can only end where a character was just added; what is
        // left after removing one is checked again by the characters after.
        if c == '~' {
            let text = out.get(start..).unwrap_or_default();
            for marker in [PASTE_END, C1_PASTE_END] {
                if text.ends_with(marker) {
                    out.truncate(out.len().saturating_sub(marker.len()));
                    break;
                }
            }
        }
    }
    out.extend_from_slice(PASTE_END);
}

/// `CSI 201 ~` with the C1 CSI, U+009B, in UTF-8.
const C1_PASTE_END: &[u8] = b"\xc2\x9b201~";

/// Appends a key's bytes, as the pane asked for them in `mode`, to `out`:
/// the kitty keyboard protocol's; or xterm's modifyOtherKeys, if it is set
/// and applies to the key, else legacy xterm bytes, which every key has.
pub fn key_bytes(stroke: Keystroke, mode: KeyMode, out: &mut Vec<u8>) {
    match mode {
        KeyMode::Kitty {
            alternate,
            report,
            backarrow,
        } => kitty(stroke, alternate, report, backarrow, out),
        KeyMode::Legacy {
            application,
            keypad,
            other_keys: level,
            backarrow,
        } => {
            if keypad && application_keypad(stroke, out) {
                return;
            }
            if !level.is_some_and(|keys| other_keys(stroke, keys, backarrow, out)) {
                legacy(stroke.press, application, backarrow, out);
            }
        }
    }
}

/// A keypad key in application mode: `SS3 final`, or `SS3 modifier final`
/// modified, the final from ctlseqs' "VT220-Style Function Keys" table, for
/// a key the terminal named by its kitty keypad code; Num Lock held
/// overrides the mode ("PC-Style Function Keys": "Use the NumLock key to
/// override the application mode"). Whether it was one.
fn application_keypad(stroke: Keystroke, out: &mut Vec<u8>) -> bool {
    let Some(kitty) = stroke.kitty else {
        return false;
    };
    if kitty.mods & NUM_LOCK_BIT != 0 {
        return false;
    }
    let final_byte = match kitty.code {
        // KP_0 to KP_9: SS3 p to SS3 y.
        Some(code @ 57399..=57408) => {
            let digit = u8::try_from(code.saturating_sub(57399)).unwrap_or(0);
            b'p'.saturating_add(digit)
        }
        Some(57409) => b'n',
        Some(57410) => b'o',
        Some(57411) => b'j',
        Some(57412) => b'm',
        Some(57413) => b'k',
        Some(57414) => b'M',
        Some(57415) => b'X',
        Some(57416) => b'l',
        _ => return false,
    };
    let held = kitty.mods & !LOCKS;
    out.extend_from_slice(b"\x1bO");
    if held != 0 {
        decimal(out, u32::from(held).saturating_add(1));
    }
    out.push(final_byte);
    true
}

/// The kitty keyboard protocol's bytes for a key, its keys escaped as
/// `report` says (`references/modern/kitty_keyboard_protocol.html`):
/// - a key that types text, with no modifier but Shift, sends its text,
///   unless all keys are reported; then `CSI code ; mods u`, the code the
///   unshifted key's, with the text as its third parameter if asked;
/// - any other text key is `CSI code ; mods u`, with its shifted and
///   base-layout keys if `alternate` and known;
/// - Enter, Tab and Backspace send their legacy byte (Backspace's BS with
///   DECBKM set, `backarrow`) unless modified (or all keys are reported),
///   Escape is `CSI 27 u`;
/// - the functional keys are `CSI 1 ; mods X` or `CSI X` (arrows, Home, End,
///   F1, F2, F4), or `CSI n ; mods ~` (Insert, Delete, the page keys, F3 as
///   13, F5 to F12), never SS3; a keypad key is its own `CSI n u`.
///
/// The modifiers are the terminal's, Super, Hyper, Meta and the locks
/// among them, if it spoke the protocol; else the press's, a capital letter
/// counting as shifted. Only presses are sent: fux reports no repeats or
/// releases (2), which the spec lets a terminal leave out.
fn kitty(stroke: Keystroke, alternate: bool, report: Report, backarrow: bool, out: &mut Vec<u8>) {
    let KeyPress { key, mods } = stroke.press;
    let exact = stroke.kitty;
    let all = report != Report::Disambiguated;
    let capital = matches!(key, Key::Char(c) if c.is_ascii_uppercase()) && exact.is_none();
    let bits = exact.map_or_else(|| mods.bits() | (SHIFT_BIT * u8::from(capital)), |k| k.mods);
    let held = bits & !LOCKS;
    let modifier = u16::from(bits).saturating_add(1);
    // `;mods`, left out when there are none; empty if a parameter follows,
    // as the spec's examples have it (`CSI 0 ; ; 229 u`).
    let mods_param = |out: &mut Vec<u8>, more: bool| {
        if bits != 0 {
            out.push(b';');
            decimal(out, modifier.into());
        } else if more {
            out.push(b';');
        }
    };
    let csi_u = |out: &mut Vec<u8>, code: u32| {
        out.extend_from_slice(b"\x1b[");
        decimal(out, code);
        mods_param(out, false);
        out.push(b'u');
    };
    // A keypad key the terminal named is sent as it named it.
    if let Some(code @ 57399..=57426) = exact.and_then(|k| k.code) {
        return csi_u(out, code);
    }
    match Form::from(key) {
        Form::Text(c) => {
            let types_text = held & !SHIFT_BIT == 0;
            if types_text && !all {
                return text(out, c);
            }
            let unshifted = if capital { c.to_ascii_lowercase() } else { c };
            let code = exact.and_then(|k| k.code).unwrap_or(u32::from(unshifted));
            let shifted = exact.and_then(|k| k.shifted).or_else(|| {
                let shifted = if unshifted.is_ascii_lowercase() {
                    unshifted.to_ascii_uppercase()
                } else {
                    c
                };
                (u32::from(shifted) != code).then_some(u32::from(shifted))
            });
            out.extend_from_slice(b"\x1b[");
            decimal(out, code);
            if alternate {
                let shifted = shifted.filter(|_| bits & SHIFT_BIT != 0);
                let base = exact.and_then(|k| k.base);
                if shifted.is_some() || base.is_some() {
                    out.push(b':');
                    if let Some(shifted) = shifted {
                        decimal(out, shifted);
                    }
                }
                if let Some(base) = base {
                    out.push(b':');
                    decimal(out, base);
                }
            }
            let text = (report == Report::AllKeysWithText && types_text).then_some(u32::from(c));
            mods_param(out, text.is_some());
            if let Some(text) = text {
                out.push(b';');
                decimal(out, text);
            }
            out.push(b'u');
        }
        // Enter, Tab and Backspace: the legacy byte, which is also the code
        // but for Backspace with DECBKM set.
        Form::Control(Control::Backspace) if held == 0 && !all => {
            out.push(backspace(false, backarrow));
        }
        Form::Control(control) if control != Control::Escape && held == 0 && !all => {
            out.push(control.code());
        }
        Form::Control(control) => csi_u(out, u32::from(control.code())),
        // F3 is `CSI 13 ~`: the protocol leaves `CSI R` to cursor position reports.
        Form::Numbered(_, 'R') => numbered(out, 13, modifier, '~'),
        Form::Numbered(number, final_byte) => numbered(out, number, modifier, final_byte),
        // F13 to F35 are numbered from 57376.
        Form::Unnumbered(n) => csi_u(
            out,
            57376_u32.saturating_add(u32::from(n.saturating_sub(13))),
        ),
    }
}

/// A key as both protocols start from xterm's bytes for it.
#[derive(Clone, Copy)]
enum Form {
    /// A key that types a character.
    Text(char),
    /// Enter, Tab, Escape and Backspace.
    Control(Control),
    /// `CSI number ; mods final` ([`numbered`]): the arrows, Home, End and
    /// F1 to F4 numbered 1, the others with xterm's numbers and gaps.
    Numbered(u8, char),
    /// A function key past F12, which xterm has no number for.
    Unnumbered(u8),
}

impl From<Key> for Form {
    #[inline]
    fn from(key: Key) -> Form {
        match key {
            Key::Char(c) => Form::Text(c),
            Key::Enter => Form::Control(Control::Enter),
            Key::Tab => Form::Control(Control::Tab),
            Key::Escape => Form::Control(Control::Escape),
            Key::Backspace => Form::Control(Control::Backspace),
            Key::Arrow(Direction::Up) => Form::Numbered(1, 'A'),
            Key::Arrow(Direction::Down) => Form::Numbered(1, 'B'),
            Key::Arrow(Direction::Right) => Form::Numbered(1, 'C'),
            Key::Arrow(Direction::Left) => Form::Numbered(1, 'D'),
            Key::Home => Form::Numbered(1, 'H'),
            Key::End => Form::Numbered(1, 'F'),
            Key::Insert => Form::Numbered(2, '~'),
            Key::Delete => Form::Numbered(3, '~'),
            Key::PageUp => Form::Numbered(5, '~'),
            Key::PageDown => Form::Numbered(6, '~'),
            Key::F(f) => match f.number() {
                1 => Form::Numbered(1, 'P'),
                2 => Form::Numbered(1, 'Q'),
                3 => Form::Numbered(1, 'R'),
                4 => Form::Numbered(1, 'S'),
                n => match usize::from(n).checked_sub(5).and_then(|i| F_CODES.get(i)) {
                    Some(&number) => Form::Numbered(number, '~'),
                    None => Form::Unnumbered(n),
                },
            },
        }
    }
}

closed! {
    /// A key whose legacy bytes are a C0 control or DEL.
    #[derive(Clone, Copy, Debug, PartialEq, Eq)]
    pub(crate) enum Control {
        /// Its byte unmodified, which is also its code in `CSI code u` and
        /// in modifyOtherKeys's `CSI 27 ; mods ; code ~`.
        pub(crate) fn code(self) -> u8;
        Enter => 0x0d,
        Tab => 0x09,
        Escape => 0x1b,
        Backspace => 0x7f,
    }
}

/// Ctrl-Backspace's byte, BS, as xterm, kitty's legacy encoding, VTE and
/// Alacritty send it, and its code in modifyOtherKeys.
pub(crate) const CTRL_BACKSPACE: u8 = 0x08;

/// Backspace's byte, and its code in modifyOtherKeys: DEL, and BS with
/// Ctrl, the reverse with DECBKM set (`backarrow`), as xterm's backarrow
/// toggle swaps them.
fn backspace(ctrl: bool, backarrow: bool) -> u8 {
    if ctrl == backarrow {
        Control::Backspace.code()
    } else {
        CTRL_BACKSPACE
    }
}

/// `CSI number ; modifier final`, the modifier left out when it is 1, none
/// held, and then the number too if it is 1 (`CSI A`).
fn numbered(out: &mut Vec<u8>, number: u8, modifier: u16, final_byte: char) {
    out.extend_from_slice(b"\x1b[");
    if number != 1 || modifier >= 2 {
        decimal(out, number.into());
    }
    if modifier >= 2 {
        out.push(b';');
        decimal(out, modifier.into());
    }
    text(out, final_byte);
}

/// Appends `c` in UTF-8, an ASCII one as its byte.
fn text(out: &mut Vec<u8>, c: char) {
    match u8::try_from(c) {
        Ok(b) if b.is_ascii() => out.push(b),
        _ => out.extend_from_slice(c.encode_utf8(&mut [0; 4]).as_bytes()),
    }
}

/// Appends `n` in decimal, as `write!` would, without the formatter, which
/// cost a key's legacy bytes most of their work (`encode/legacy`).
fn decimal(out: &mut Vec<u8>, n: u32) {
    let mut place = 1_000_000_000;
    while place > 1 {
        if n >= place {
            out.push(digit(n.checked_div(place).unwrap_or(0)));
        }
        place = place.checked_div(10).unwrap_or(0);
    }
    out.push(digit(n));
}

/// The ASCII digit of `n`'s units.
fn digit(n: u32) -> u8 {
    b'0'.saturating_add(u8::try_from(n.checked_rem(10).unwrap_or(0)).unwrap_or(0))
}

/// F5 to F12's numbers, with xterm's gaps.
const F_CODES: [u8; 8] = [15, 17, 18, 19, 20, 21, 23, 24];

/// xterm's modifyOtherKeys bytes for a key, `CSI 27 ; mods ; code ~`
/// (formatOtherKeys 0, xterm's default), if the key is an ordinary one (a
/// character, Enter, Tab, Escape, Backspace) and xterm sends it so at
/// `level`; else nothing, and false, for the legacy bytes.
///
/// ctlseqs ("Alt and Meta Keys") gives the outline: at level 1 "the usual
/// shift- and control-modifiers work as expected", at level 2 "all of the
/// modifiers apply". The details are xterm's `input.c` (patch 412), which
/// this follows: `ModifyOtherKeys` decides whether a key is sent so
/// (`modified`), and `allowedCharModifiers` with `filterAltMeta` which
/// modifiers it carries (`allowed`). xterm is taken as X keyboards set it
/// up: the Alt key is xterm's Meta modifier (`mod1` holds `Alt_L` and
/// `Meta_L`; ctlseqs: "Common keyboard configurations assign the Meta
/// modifier to an 'Alt' key"), metaSendsEscape is off, the backarrow key
/// sends DEL (BS with DECBKM set, `backarrow`), and Shift-Tab is Tab with
/// Shift.
/// So, at level 1:
/// - Alt alone leaves a key its legacy bytes, Escape and all ("A bare
///   meta-modifier is independent of modifyOtherKeys"): emacs, which sets
///   level 1, reads `M-x` as `ESC x`. ctlseqs's alt-Tab example is of an
///   Alt key that is not Meta;
/// - so do Ctrl, Shift, or both, on a key that Ctrl makes a control (a
///   letter, Space, `2`, `/`, Escape), Shift on a key that types text, and
///   Shift-Tab; Ctrl-Alt on a letter, and Alt with Ctrl on Enter and Tab,
///   are legacy too, which emacs reads as `ESC C-x`; Alt with Shift drops
///   Alt on Enter and Tab;
/// - any other modifiers apply: Ctrl-1, Ctrl-Alt-Space, Shift-Enter,
///   Ctrl-Tab; Backspace's never.
///
/// At level 2 every modifier applies, but Ctrl alone on Backspace, and
/// Shift alone on a character Ctrl makes no control of (`!`, `)`, `+`),
/// which types its text, as real xterm 411 sends them
/// (tests/data/keys/xterm-keys.txt): Shift-a is `CSI 27 ; 2 ; 65 ~`,
/// Shift-2 `CSI 27 ; 2 ; 64 ~`, Shift-1 `!`. Shift-Tab is `CSI Z`. Level
/// 3 is 2, Ctrl-Backspace and every shifted character escaped too, and an
/// unmodified ordinary key is `CSI 27 ; 1 ; code ~` (ctlseqs: "space sends
/// CSI 2 7 ; 1 ; 3 2 ~"). A capital letter shows its Shift, but a symbol
/// does not (`@` is Shift-2 on one layout, a key of its own on another):
/// unless the terminal reported Shift (kitty), it is sent without Shift
/// (at level 3 `CSI 27 ; 1 ; 64 ~`), where xterm, which knows the keyboard,
/// sends Shift with it.
///
/// The code is the key's character, Shift applied (Ctrl-Shift-a is 65), as
/// xterm sends the keysym; Backspace's is 127, or 8 with Ctrl, which the
/// backarrow toggle leaves BS (the reverse with DECBKM set).
fn other_keys(
    stroke: Keystroke,
    level: OtherKeysLevel,
    backarrow: bool,
    out: &mut Vec<u8>,
) -> bool {
    let KeyPress { key, mods } = stroke.press;
    let kitty_shift = stroke.kitty.is_some_and(|k| k.mods & SHIFT_BIT != 0);
    let shift = mods.shift || kitty_shift || matches!(key, Key::Char(c) if c.is_ascii_uppercase());
    let state = Modifiers { shift, ..mods };
    let (sym, code) = match Form::from(key) {
        Form::Control(Control::Backspace) => {
            let sym = if mods.ctrl {
                Sym::BackSpace
            } else {
                Sym::Control(Control::Backspace)
            };
            (sym, backspace(mods.ctrl, backarrow).into())
        }
        Form::Control(control) => (Sym::Control(control), control.code().into()),
        Form::Text(c) => {
            let c = if shift { c.to_ascii_uppercase() } else { c };
            (Sym::Char(c), u32::from(c))
        }
        Form::Numbered(..) | Form::Unnumbered(_) => return false,
    };
    if !modified(sym, state, level) {
        return false;
    }
    let sent = match level {
        OtherKeysLevel::Usual => allowed(sym, state),
        OtherKeysLevel::All | OtherKeysLevel::Unmodified => state,
    };
    // Level 1 may leave the key no modifier to send: it is legacy then.
    if sent.is_empty() && !state.is_empty() {
        return false;
    }
    let mods = u16::from(sent.bits()).saturating_add(1);
    out.extend_from_slice(b"\x1b[27;");
    decimal(out, mods.into());
    out.push(b';');
    decimal(out, code);
    out.push(b'~');
    true
}

/// An ordinary key as xterm's `input.c` sees it: by its keysym, a control
/// key's being `Return`, `Tab`, `Escape` and, for Backspace, `Delete`.
/// Backspace is `BackSpace` with Ctrl, as xterm's backarrow toggle
/// (`IsBackarrowToggle`) makes it when the key sends DEL.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Sym {
    Char(char),
    Control(Control),
    BackSpace,
}

impl Sym {
    /// `IsControlInput`: a character that Ctrl makes a control of, `@` to
    /// DEL.
    fn control_input(self) -> bool {
        matches!(self, Sym::Char('\u{40}'..='\u{7f}'))
    }

    /// `IsControlOutput`: a control character itself.
    fn control_output(self) -> bool {
        matches!(self, Sym::Char('\0'..='\u{1f}' | '\u{7f}'..='\u{9f}'))
    }

    /// `IsControlAlias`: the key's one byte, as X looks it up with the
    /// modifiers held, is a control. With Ctrl, `@` to `~`, Space, `2` to
    /// `8` and `/` are (Xlib's control mapping); Enter, Tab, Escape and
    /// Backspace always are.
    fn control_alias(self, ctrl: bool) -> bool {
        match self {
            Sym::Char(c) => {
                ctrl && matches!(c, '@'..='~' | ' ' | '2'..='8' | '/')
                    || matches!(c, '\0'..='\u{1f}' | '\u{7f}')
            }
            Sym::Control(_) | Sym::BackSpace => true,
        }
    }
}

/// Whether xterm sends `sym` with `state` as a modifyOtherKeys sequence at
/// `level`: `input.c`'s `ModifyOtherKeys`, and its `Input`'s Shift-Tab,
/// which at level 1 becomes `ISO_Left_Tab`, a function key, `CSI Z`. At
/// level 2, Shift alone on a character other than Space types its text;
/// level 3 sends an unmodified key too.
fn modified(sym: Sym, state: Modifiers, level: OtherKeysLevel) -> bool {
    // At level 1, a key with no modifier ends `false` below: `allowed`
    // leaves none.
    match level {
        OtherKeysLevel::Usual => {}
        OtherKeysLevel::All if state.is_empty() => return false,
        OtherKeysLevel::Unmodified if state.is_empty() => return true,
        // At level 2 Shift-Tab alone is still CSI Z, as real xterm 411 sends it.
        OtherKeysLevel::All if sym == Sym::Control(Control::Tab) && state == Modifiers::SHIFT => {
            return false;
        }
        OtherKeysLevel::All | OtherKeysLevel::Unmodified => {
            // Every modifier; at level 2 Shift alone on a character types
            // its text unless the character is one Ctrl makes a control of
            // (`@` to DEL, capitals among them) or Space, which real xterm
            // 411 sends as `CSI 27 ; 2 ; code ~`, the shifted character's
            // code. Level 3 sends every key so, and Ctrl alone on
            // Backspace too (tests/data/keys/xterm-keys.txt).
            let unmodified = level == OtherKeysLevel::Unmodified;
            return match sym {
                Sym::BackSpace => state != Modifiers::CTRL || unmodified,
                Sym::Control(_) => true,
                Sym::Char(c) => {
                    unmodified || state != Modifiers::SHIFT || c == ' ' || sym.control_input()
                }
            };
        }
    }
    // Shift-Tab is ISO_Left_Tab at level 1, a function key: CSI Z, with
    // Ctrl or Alt held too, as real xterm 411 sends it.
    if sym == Sym::Control(Control::Tab) && state.shift {
        return false;
    }
    // A character's modifiers are filtered first, a predefined key's not.
    let st = match sym {
        Sym::Char(_) => allowed(sym, state),
        Sym::Control(_) | Sym::BackSpace => state,
    };
    let control = sym.control_input() || sym.control_alias(state.ctrl);
    match sym {
        _ if st.is_empty() => false,
        Sym::BackSpace | Sym::Control(Control::Backspace) => false,
        Sym::Control(Control::Enter | Control::Tab) => true,
        Sym::Char(_) | Sym::Control(Control::Escape) => {
            !control || st != Modifiers::CTRL && st != Modifiers::SHIFT
        }
    }
}

/// The modifiers xterm sends with `sym` at level 1: `input.c`'s
/// `allowedCharModifiers`, and its `filterAltMeta` for the Alt key as Meta.
fn allowed(sym: Sym, state: Modifiers) -> Modifiers {
    let mut m = state;
    let text = matches!(sym, Sym::Char(_)) && !sym.control_output();
    if sym.control_input() && !m.shift && !m.alt {
        // Ctrl makes these a control already, which it is left to.
    } else if matches!(sym, Sym::Control(Control::Enter | Control::Tab)) {
        // Enter and Tab keep Ctrl and Shift.
    } else if sym.control_alias(state.ctrl) {
        // Ctrl and Shift on a control are its own.
        if !m.alt {
            m = Modifiers::NONE;
        }
    } else if text && !m.ctrl {
        // "Printable keys are already associated with the shift-key".
        m.shift = false;
    }
    if m.alt {
        // "A bare meta-modifier is independent of modifyOtherKeys."
        if !m.ctrl && !m.shift {
            m.alt = false;
        }
        // "special cases of control+meta which are used by some
        // applications, e.g., emacs", and Enter and Tab.
        let control = (sym.control_input() || sym.control_output()) && m.ctrl;
        if control || matches!(sym, Sym::Control(Control::Enter | Control::Tab)) {
            m.alt = false;
            m.ctrl = false;
        }
    }
    m
}

/// Legacy xterm bytes for a key: what xterm sends with its default
/// resources, for a pane that asked for neither the kitty protocol nor
/// modifyOtherKeys. Every key has an encoding but a function key past
/// F12, which xterm has no bytes for: it sends nothing, where an ESC
/// would be read as Escape.
fn legacy(press: KeyPress, application: bool, backarrow: bool, out: &mut Vec<u8>) {
    let KeyPress { key, mods } = press;
    let Modifiers { ctrl, alt, shift } = mods;
    // xterm's modifier parameter: 1 plus a bit for each, so at most 8.
    let modifier = u16::from(mods.bits()).saturating_add(1);
    let start = out.len();
    match Form::from(key) {
        // SS3, for F1 to F4 and in application mode the other keys numbered
        // 1, unmodified.
        Form::Numbered(1, final_byte)
            if modifier == 1 && (application || matches!(key, Key::F(_))) =>
        {
            out.extend_from_slice(b"\x1bO");
            text(out, final_byte);
        }
        Form::Numbered(number, final_byte) => numbered(out, number, modifier, final_byte),
        Form::Control(control) => match control {
            // Shift-Tab is CSI Z, ISO_Left_Tab's, with Ctrl or Alt held too:
            // real xterm 411 sends neither (tests/data/keys/xterm-keys.txt).
            Control::Tab if shift => return out.extend_from_slice(b"\x1b[Z"),
            Control::Backspace => out.push(backspace(ctrl, backarrow)),
            Control::Enter | Control::Tab | Control::Escape => out.push(control.code()),
        },
        Form::Unnumbered(_) => return,
        // Ctrl-? is DEL by xterm's own translation, which Alt passes by:
        // Ctrl-Alt-? is ESC `?` (tests/data/keys/xterm-keys.txt).
        Form::Text(c) if ctrl && c.is_ascii() => out.push(match c {
            '?' if alt => b'?',
            _ => control_byte(c).unwrap_or(c as u8),
        }),
        Form::Text(c) => text(out, c),
    }
    // Alt is an Escape before the key, unless the key is a sequence, which
    // carries Alt in its parameter; a lone ESC (Escape, Ctrl-[) takes one
    // too, as xterm sends Alt-Escape: ESC ESC.
    let escape = Control::Escape.code();
    if alt && !(out.get(start) == Some(&escape) && out.len() > start.saturating_add(1)) {
        out.push(escape);
        // The key's bytes and the Escape after them: at least one to turn.
        if let Some(key) = out.get_mut(start..) {
            key.rotate_right(1);
        }
    }
}

/// xterm's control-key byte, Xlib's control mapping: `@` to `~` masked to
/// a C0 control, Space and `2` NUL, `3` to `7` ESC to US, `/` US, and `8`
/// and `?` DEL (`?` as kitty's table and xterm's own translations have it).
/// Ctrl leaves any other character untouched, as it does in both: `Ctrl-;`
/// is `;`, never the ESC masking would make of it.
fn control_byte(c: char) -> Option<u8> {
    let b = u8::try_from(c).ok()?;
    Some(match b {
        // Masking drops the case bit too: `a` and `A` are both 0x01.
        b'@'..=b'~' => b & 0x1f,
        b' ' | b'2' => 0,
        b'3'..=b'7' => b.wrapping_sub(b'3').wrapping_add(0x1b),
        b'/' => 0x1f,
        b'8' | b'?' => 0x7f,
        _ => return None,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::keys::Function;

    /// The bytes for a named key, or a stroke, in `mode`, as text, appended
    /// after others, which stay as they were.
    fn named(name: &str, mode: KeyMode) -> String {
        let press = name.parse::<KeyPress>().unwrap_or(KeyPress::char('?'));
        stroke(press.into(), mode)
    }
    fn stroke(stroke: Keystroke, mode: KeyMode) -> String {
        let mut out = b"before".to_vec();
        key_bytes(stroke, mode, &mut out);
        assert!(out.starts_with(b"before"), "{stroke:?}");
        String::from_utf8_lossy(out.get(6..).unwrap_or_default()).into_owned()
    }
    const LEGACY: KeyMode = KeyMode::Legacy {
        application: false,
        keypad: false,
        other_keys: None,
        backarrow: false,
    };
    /// Legacy keys in application cursor mode (DECCKM).
    const APPLICATION: KeyMode = KeyMode::Legacy {
        application: true,
        keypad: false,
        other_keys: None,
        backarrow: false,
    };
    const fn kitty_mode(alternate: bool, report: Report) -> KeyMode {
        KeyMode::Kitty {
            alternate,
            report,
            backarrow: false,
        }
    }
    const DISAMBIGUATED: KeyMode = kitty_mode(false, Report::Disambiguated);
    const ALTERNATE: KeyMode = kitty_mode(true, Report::Disambiguated);
    /// A key as a kitty-protocol terminal reports it.
    fn reported(
        name: &str,
        code: u32,
        shifted: Option<u32>,
        base: Option<u32>,
        mods: u8,
    ) -> Keystroke {
        Keystroke {
            press: name.parse().unwrap_or(KeyPress::char('?')),
            kitty: Some(crate::keys::Kitty {
                code: Some(code),
                shifted,
                base,
                mods,
            }),
        }
    }

    /// With disambiguate (1), as the kitty spec's "Disambiguate escape
    /// codes" and "Functional key definitions" say: text keys send their
    /// text; Escape and keys with Ctrl or Alt are `CSI code ; mods u`;
    /// Enter, Tab and Backspace stay legacy unless modified; the functional
    /// keys take the `CSI 1 ; mods X` and `CSI n ; mods ~` forms, never SS3,
    /// even in application cursor mode; F3 is `CSI 13 ~`.
    #[test]
    fn disambiguated_keys_follow_the_kitty_spec() {
        for (name, bytes) in [
            ("a", "a"),
            ("A", "A"),
            ("界", "界"),
            ("Escape", "\x1b[27u"),
            ("M-Escape", "\x1b[27;3u"),
            ("Enter", "\r"),
            ("S-Enter", "\x1b[13;2u"),
            ("C-Enter", "\x1b[13;5u"),
            ("Tab", "\t"),
            ("BTab", "\x1b[9;2u"),
            ("BSpace", "\x7f"),
            ("C-BSpace", "\x1b[127;5u"),
            ("M-BSpace", "\x1b[127;3u"),
            ("C-a", "\x1b[97;5u"),
            ("C-i", "\x1b[105;5u"),
            ("C-m", "\x1b[109;5u"),
            ("C-[", "\x1b[91;5u"),
            ("M-[", "\x1b[91;3u"),
            ("M-a", "\x1b[97;3u"),
            ("C-M-a", "\x1b[97;7u"),
            ("M-A", "\x1b[97;4u"),
            ("C-Space", "\x1b[32;5u"),
            ("M-!", "\x1b[33;3u"),
            ("Up", "\x1b[A"),
            ("C-Up", "\x1b[1;5A"),
            ("S-Left", "\x1b[1;2D"),
            ("Home", "\x1b[H"),
            ("End", "\x1b[F"),
            ("F1", "\x1b[P"),
            ("F2", "\x1b[Q"),
            ("F3", "\x1b[13~"),
            ("S-F3", "\x1b[13;2~"),
            ("F4", "\x1b[S"),
            ("F5", "\x1b[15~"),
            ("C-F12", "\x1b[24;5~"),
            ("Insert", "\x1b[2~"),
            ("C-Delete", "\x1b[3;5~"),
            ("PageUp", "\x1b[5~"),
            ("PageDown", "\x1b[6~"),
        ] {
            assert_eq!(named(name, DISAMBIGUATED), bytes, "{name}");
        }
    }

    /// What a kitty-protocol terminal said beyond the press is given back:
    /// Shift with Ctrl, which the press folds away (ctrl+shift+i is
    /// `CSI 105 ; 6 u`, the spec's "Legacy text keys" table), Super and the
    /// locks, a keypad key's own number; with alternate keys (4), the
    /// shifted and base-layout keys ("Key codes").
    #[test]
    fn a_reported_key_is_given_back_as_reported() {
        let ctrl_shift_i = reported("C-i", 105, Some(73), None, 5);
        assert_eq!(stroke(ctrl_shift_i, DISAMBIGUATED), "\x1b[105;6u");
        assert_eq!(stroke(ctrl_shift_i, ALTERNATE), "\x1b[105:73;6u");
        let super_a = reported("a", 97, None, None, 8);
        assert_eq!(stroke(super_a, DISAMBIGUATED), "\x1b[97;9u");
        // Ctrl and С on a Cyrillic layout, whose base-layout key is c.
        let cyrillic = reported("C-c", 1089, None, Some(99), 4);
        assert_eq!(stroke(cyrillic, DISAMBIGUATED), "\x1b[1089;5u");
        assert_eq!(stroke(cyrillic, ALTERNATE), "\x1b[1089::99;5u");
        let keypad_enter = reported("Enter", 57414, None, None, 0);
        assert_eq!(stroke(keypad_enter, DISAMBIGUATED), "\x1b[57414u");
        assert_eq!(stroke(keypad_enter, LEGACY), "\r");
        let caps_ctrl_a = reported("C-a", 97, None, None, 4 | 64);
        assert_eq!(stroke(caps_ctrl_a, DISAMBIGUATED), "\x1b[97;69u");
        // A legacy pane gets the press's bytes alone, what was reported
        // beside them unused.
        assert_eq!(stroke(ctrl_shift_i, LEGACY), "\t");
        assert_eq!(stroke(cyrillic, LEGACY), "\x03");
        // Alternate keys from a legacy press: a capital's shifted key.
        assert_eq!(named("M-A", ALTERNATE), "\x1b[97:65;4u");
    }

    /// Report all keys (8): text keys too are `CSI code ; mods u`, with the
    /// text as a third parameter if asked (16; the spec's "Text as code
    /// points": shift+a is `CSI 97 ; 2 ; 65 u`); Enter, Tab and Backspace
    /// are escapes.
    #[test]
    fn reporting_all_keys_escapes_text_keys() {
        let (all, text) = (Report::AllKeys, Report::AllKeysWithText);
        for (alternate, report, name, bytes) in [
            (false, all, "a", "\x1b[97u"),
            (false, all, "A", "\x1b[97;2u"),
            (true, all, "A", "\x1b[97:65;2u"),
            (false, text, "a", "\x1b[97;;97u"),
            (false, text, "A", "\x1b[97;2;65u"),
            (false, text, "C-a", "\x1b[97;5u"),
            (false, all, "Enter", "\x1b[13u"),
            (false, all, "Tab", "\x1b[9u"),
            (false, all, "BSpace", "\x1b[127u"),
            (false, all, "Escape", "\x1b[27u"),
            (false, all, "Up", "\x1b[A"),
        ] {
            let mode = kitty_mode(alternate, report);
            assert_eq!(named(name, mode), bytes, "{mode:?} {name}");
        }
    }

    const fn other_keys_level(level: OtherKeysLevel) -> KeyMode {
        KeyMode::Legacy {
            application: false,
            keypad: false,
            other_keys: Some(level),
            backarrow: false,
        }
    }
    const USUAL: KeyMode = other_keys_level(OtherKeysLevel::Usual);
    const ALL_MODIFIERS: KeyMode = other_keys_level(OtherKeysLevel::All);
    const UNMODIFIED: KeyMode = other_keys_level(OtherKeysLevel::Unmodified);

    /// modifyOtherKeys level 1, as xterm's input.c (patch 412) sends it with
    /// the Alt key as its Meta modifier. Alone, Alt leaves a key its legacy
    /// bytes (`filterAltMeta`: "A bare meta-modifier is independent of
    /// modifyOtherKeys"), as do Ctrl-Alt on a letter and Alt on Enter and
    /// Tab with Ctrl ("special cases of control+meta ... e.g., emacs"), Ctrl
    /// and Shift on a key Ctrl makes a control (`allowedCharModifiers`,
    /// `IsControlAlias`), Shift on a key that types text, Shift-Tab
    /// (`ISO_Left_Tab`), and Backspace (`ModifyOtherKeys`, `mokUser`).
    #[test]
    fn modify_other_keys_level_1_follows_xterm() {
        for name in [
            "M-x",
            "M-v",
            "M-X",
            "M-1",
            "M-<",
            "M-Space",
            "M-Escape",
            "M-Tab",
            "M-Enter",
            "M-BSpace",
            "C-M-x",
            "C-M-a",
            "C-M-Enter",
            "C-M-Tab",
            "C-a",
            "C-Space",
            "C-2",
            "C-/",
            "C-Escape",
            "S-Escape",
            "C-S-Escape",
            "BTab",
            "a",
            "A",
            "!",
            "Enter",
            "Tab",
            "Escape",
            "BSpace",
            "C-BSpace",
            "S-BSpace",
            "C-Up",
            "F5",
        ] {
            assert_eq!(named(name, USUAL), named(name, LEGACY), "{name}");
        }
        assert_eq!(named("M-x", USUAL), "\x1bx");
        assert_eq!(named("C-M-x", USUAL), "\x1b\x18");
        for (name, bytes) in [
            ("C-1", "\x1b[27;5;49~"),
            ("C-M-1", "\x1b[27;7;49~"),
            ("C-M-Space", "\x1b[27;7;32~"),
            ("C-M-/", "\x1b[27;7;47~"),
            ("C-M-%", "\x1b[27;7;37~"),
            ("C-M-Escape", "\x1b[27;7;27~"),
            ("M-S-Escape", "\x1b[27;4;27~"),
            ("S-Enter", "\x1b[27;2;13~"),
            ("C-Enter", "\x1b[27;5;13~"),
            ("C-S-Enter", "\x1b[27;6;13~"),
            ("M-S-Enter", "\x1b[27;2;13~"),
            ("C-Tab", "\x1b[27;5;9~"),
            ("C-BTab", "\x1b[Z"),
            ("M-BTab", "\x1b[Z"),
        ] {
            assert_eq!(named(name, USUAL), bytes, "{name}");
        }
        // Ctrl-Shift-a, which only a kitty-protocol terminal tells from
        // Ctrl-a, is a control too.
        let ctrl_shift_a = reported("C-a", 97, Some(65), None, 5);
        assert_eq!(stroke(ctrl_shift_a, USUAL), "\x01");
    }

    /// modifyOtherKeys level 2, as xterm's input.c sends it: every modifier
    /// applies (`ModifyOtherKeys`, case 2), and the Alt key is filtered no
    /// more; but Ctrl alone on Backspace, which xterm's backarrow toggle
    /// makes BS, and Shift alone on a character that types text, which is
    /// its text: xterm's for `!`, fux's departure for a letter. Shift-Tab is
    /// `CSI 27 ; 2 ; 9 ~`, as ctlseqs says. Other keys keep their legacy
    /// bytes.
    #[test]
    fn modify_other_keys_level_2_follows_xterm() {
        for (name, bytes) in [
            ("M-x", "\x1b[27;3;120~"),
            ("M-X", "\x1b[27;4;88~"),
            ("C-M-x", "\x1b[27;7;120~"),
            ("M-1", "\x1b[27;3;49~"),
            ("C-1", "\x1b[27;5;49~"),
            ("C-a", "\x1b[27;5;97~"),
            ("C-i", "\x1b[27;5;105~"),
            ("C-Space", "\x1b[27;5;32~"),
            ("BTab", "\x1b[Z"),
            ("M-Tab", "\x1b[27;3;9~"),
            ("S-Enter", "\x1b[27;2;13~"),
            ("C-Enter", "\x1b[27;5;13~"),
            ("M-Enter", "\x1b[27;3;13~"),
            ("M-Escape", "\x1b[27;3;27~"),
            ("S-Escape", "\x1b[27;2;27~"),
            ("M-BSpace", "\x1b[27;3;127~"),
            ("S-BSpace", "\x1b[27;2;127~"),
            ("C-S-BSpace", "\x1b[27;6;8~"),
            ("C-M-BSpace", "\x1b[27;7;8~"),
            ("C-BSpace", "\x08"),
            ("a", "a"),
            ("A", "\x1b[27;2;65~"),
            ("!", "!"),
            ("Enter", "\r"),
            ("Escape", "\x1b"),
            ("C-Up", "\x1b[1;5A"),
            ("F5", "\x1b[15~"),
        ] {
            assert_eq!(named(name, ALL_MODIFIERS), bytes, "{name}");
        }
        let ctrl_shift_a = reported("C-a", 97, Some(65), None, 5);
        assert_eq!(stroke(ctrl_shift_a, ALL_MODIFIERS), "\x1b[27;6;65~");
        // Shift-Space, which only a kitty-protocol terminal tells from
        // Space, is xterm's one character modified by Shift alone.
        let shift_space = reported("Space", 32, None, None, 1);
        assert_eq!(stroke(shift_space, ALL_MODIFIERS), "\x1b[27;2;32~");
        assert_eq!(stroke(shift_space, USUAL), " ");
    }

    /// modifyOtherKeys level 3 (ctlseqs, "Alt and Meta Keys": "unmodified
    /// keys also are sent as escape sequences. For example, space sends
    /// CSI 2 7 ; 1 ; 3 2 ~"): an ordinary key with no modifier is `CSI 27 ;
    /// 1 ; code ~`, a modified one as at level 2; the other keys keep their
    /// legacy bytes.
    #[test]
    fn modify_other_keys_level_3_sends_unmodified_keys_too() {
        for (name, bytes) in [
            ("Space", "\x1b[27;1;32~"),
            ("a", "\x1b[27;1;97~"),
            // No Shift is known with a symbol (`other_keys`).
            ("!", "\x1b[27;1;33~"),
            ("Enter", "\x1b[27;1;13~"),
            ("Tab", "\x1b[27;1;9~"),
            ("Escape", "\x1b[27;1;27~"),
            ("BSpace", "\x1b[27;1;127~"),
            ("C-a", "\x1b[27;5;97~"),
            ("M-x", "\x1b[27;3;120~"),
            ("A", "\x1b[27;2;65~"),
            ("Up", "\x1b[A"),
            ("F5", "\x1b[15~"),
        ] {
            assert_eq!(named(name, UNMODIFIED), bytes, "{name}");
        }
    }

    /// DECBKM (`CSI ? 67 h`; ctlseqs: "Backarrow key sends backspace"):
    /// set, Backspace sends BS, and Ctrl-Backspace, which xterm's
    /// backarrow toggle makes the other, DEL; reset, the reverse. Through
    /// modifyOtherKeys the codes swap the same way. The kitty protocol's
    /// Backspace "still generate[s] the same bytes as in legacy mode"
    /// ("Disambiguate escape codes"), and its codes stay its own.
    #[test]
    fn decbkm_makes_backspace_send_bs() -> Result<(), crate::Error> {
        const BS: KeyMode = KeyMode::Legacy {
            application: false,
            keypad: false,
            other_keys: None,
            backarrow: true,
        };
        const BS_ALL: KeyMode = KeyMode::Legacy {
            application: false,
            keypad: false,
            other_keys: Some(OtherKeysLevel::All),
            backarrow: true,
        };
        let kitty_bs = KeyMode::Kitty {
            alternate: false,
            report: Report::Disambiguated,
            backarrow: true,
        };
        for (name, mode, bytes) in [
            ("BSpace", BS, "\x08"),
            ("C-BSpace", BS, "\x7f"),
            ("M-BSpace", BS, "\x1b\x08"),
            ("BSpace", LEGACY, "\x7f"),
            ("C-BSpace", LEGACY, "\x08"),
            ("M-BSpace", BS_ALL, "\x1b[27;3;8~"),
            ("C-S-BSpace", BS_ALL, "\x1b[27;6;127~"),
            ("BSpace", BS_ALL, "\x08"),
            ("C-BSpace", BS_ALL, "\x7f"),
            ("BSpace", kitty_bs, "\x08"),
            ("C-BSpace", kitty_bs, "\x1b[127;5u"),
            ("BSpace", DISAMBIGUATED, "\x7f"),
        ] {
            assert_eq!(named(name, mode), bytes, "{name} {mode:?}");
        }
        let options = crate::Feature::KittyKeyboard.into();
        let mut parser = crate::Parser::with_options(crate::Size::of(4, 10), 0, options)?;
        parser.process(b"\x1b[?67h\x1b[>1u")?;
        assert_eq!(named("BSpace", parser.screen().key_mode()), "\x08");
        Ok(())
    }

    /// A screen's kitty flags pick the protocol: disambiguate or all keys
    /// make it kitty's, whatever the cursor mode and modifyOtherKeys; the
    /// other flags alone leave legacy keys. A level past 3 changes no key.
    #[test]
    fn a_screen_s_flags_pick_its_key_protocol() -> Result<(), crate::Error> {
        for (setup, mode) in [
            ("", LEGACY),
            ("\x1b[?1h\x1b[>2u", APPLICATION),
            ("\x1b[>4;1m\x1b[>16u", USUAL),
            ("\x1b[>4;3m", UNMODIFIED),
            ("\x1b[>4;4m", LEGACY),
            (
                "\x1b[?67h",
                KeyMode::Legacy {
                    application: false,
                    keypad: false,
                    other_keys: None,
                    backarrow: true,
                },
            ),
            ("\x1b[?1h\x1b[>4;2m\x1b[>5u", ALTERNATE),
            ("\x1b[>8u", kitty_mode(false, Report::AllKeys)),
            ("\x1b[>25u", kitty_mode(false, Report::AllKeysWithText)),
        ] {
            let options = crate::Feature::KittyKeyboard.into();
            let mut parser = crate::Parser::with_options(crate::Size::of(4, 10), 0, options)?;
            parser.process(setup.as_bytes())?;
            assert_eq!(parser.screen().key_mode(), mode, "{setup:?}");
        }
        Ok(())
    }

    fn pasted(text: &str, bracketed: bool) -> Vec<u8> {
        let mut out = Vec::new();
        paste(text, bracketed, &mut out);
        out
    }

    /// Legacy keys: SS3 for F1 to F4, and in application mode for the
    /// cursor keys, unless modified; Alt an ESC before a key's bytes, a lone
    /// ESC's too (xterm's Alt-Escape is ESC ESC), but not before a sequence,
    /// which carries it.
    /// DECKPAM (`ESC =`, DECNKM): the keypad keys a terminal speaking the
    /// kitty protocol names (`Kitty::code`, its keypad codes 57399 to 57416)
    /// send their application sequences, ctlseqs' "VT220-Style Function
    /// Keys" table: Enter `SS3 M`, 0 to 9 `SS3 p` to `SS3 y`, `*` `SS3 j`,
    /// `+` `SS3 k`, `,` `SS3 l`, `-` `SS3 m`, `.` `SS3 n`, `/` `SS3 o`, `=`
    /// `SS3 X`; modified, the modifier after SS3 (`SS3 5 q`), as Ghostty's
    /// `key_encode.zig` has it. Num Lock held overrides the application mode
    /// (ctlseqs, "PC-Style Function Keys": "Use the NumLock key to override
    /// the application mode"), and a keypad key in numeric mode, or named by
    /// a legacy terminal, sends what it types.
    #[test]
    fn the_application_keypad_sends_its_ss3_sequences() -> Result<(), crate::Error> {
        use crate::keys::{CTRL_BIT, Kitty};
        let keypad = |code: u32, mods: u8| Keystroke {
            press: KeyPress::char('1'),
            kitty: Some(Kitty {
                code: Some(code),
                mods,
                ..Kitty::default()
            }),
        };
        let mode = |keypad: bool| KeyMode::Legacy {
            application: false,
            keypad,
            other_keys: None,
            backarrow: false,
        };
        for (code, want) in [
            (57399, "\x1bOp"),
            (57400, "\x1bOq"),
            (57408, "\x1bOy"),
            (57409, "\x1bOn"),
            (57410, "\x1bOo"),
            (57411, "\x1bOj"),
            (57412, "\x1bOm"),
            (57413, "\x1bOk"),
            (57414, "\x1bOM"),
            (57415, "\x1bOX"),
            (57416, "\x1bOl"),
        ] {
            assert_eq!(stroke(keypad(code, 0), mode(true)), want, "{code}");
        }
        assert_eq!(stroke(keypad(57400, CTRL_BIT), mode(true)), "\x1bO5q");
        assert_eq!(stroke(keypad(57400, NUM_LOCK_BIT), mode(true)), "1");
        assert_eq!(stroke(keypad(57400, 0), mode(false)), "1");
        assert_eq!(stroke(KeyPress::char('1').into(), mode(true)), "1");
        // ESC = sets it, ESC > resets it, as `Screen::key_mode` reads them.
        let mut p = crate::Parser::new(crate::Size::of(2, 4), 0)?;
        p.process(b"\x1b=")?;
        assert_eq!(stroke(keypad(57400, 0), p.screen().key_mode()), "\x1bOq");
        p.process(b"\x1b>")?;
        assert_eq!(stroke(keypad(57400, 0), p.screen().key_mode()), "1");
        Ok(())
    }

    fn c_is_capital(key: Key) -> bool {
        matches!(key, Key::Char(c) if c.is_ascii_uppercase())
    }

    #[test]
    fn legacy_keys_follow_xterm() {
        let application = APPLICATION;
        for (name, mode, bytes) in [
            ("F1", LEGACY, "\x1bOP"),
            ("C-M-S-F1", LEGACY, "\x1b[1;8P"),
            ("C-S-F1", LEGACY, "\x1b[1;6P"),
            ("C-M-S-F4", LEGACY, "\x1b[1;8S"),
            ("F5", LEGACY, "\x1b[15~"),
            ("C-M-S-F5", LEGACY, "\x1b[15;8~"),
            ("M-F12", LEGACY, "\x1b[24;3~"),
            ("C-M-S-Insert", LEGACY, "\x1b[2;8~"),
            ("Home", LEGACY, "\x1b[H"),
            ("C-M-S-Home", LEGACY, "\x1b[1;8H"),
            ("Left", application, "\x1bOD"),
            ("C-Left", application, "\x1b[1;5D"),
            ("M-Up", application, "\x1b[1;3A"),
            ("C-c", LEGACY, "\x03"),
            ("M-a", LEGACY, "\x1ba"),
            ("M-Escape", LEGACY, "\x1b\x1b"),
            ("C-M-[", LEGACY, "\x1b\x1b"),
            ("C-M-3", LEGACY, "\x1b\x1b"),
            ("M-Delete", LEGACY, "\x1b[3;3~"),
            ("Tab", LEGACY, "\t"),
            ("M-Tab", LEGACY, "\x1b\t"),
            ("BTab", LEGACY, "\x1b[Z"),
            ("M-BTab", LEGACY, "\x1b[Z"),
            ("C-S-Tab", LEGACY, "\x1b[Z"),
        ] {
            assert_eq!(named(name, mode), bytes, "{name}");
        }
    }

    /// F13 to F35 are kitty's 57376 to 57398 (the kitty spec's
    /// "Functional key definitions").
    #[test]
    fn function_keys_past_f12_are_numbered_from_57376() {
        assert_eq!(named("F13", DISAMBIGUATED), "\x1b[57376u");
        assert_eq!(named("C-F35", DISAMBIGUATED), "\x1b[57398;5u");
    }

    /// A legacy pane gets Ctrl-Backspace as BS, as xterm, kitty's legacy
    /// encoding, VTE and Alacritty send it, whatever terminal it was typed
    /// in; and nothing for a function key past F12, which xterm has no
    /// bytes for, rather than an ESC a program reads as Escape.
    #[test]
    fn legacy_ctrl_backspace_is_bs_and_an_unnumbered_key_is_nothing() {
        for (name, bytes) in [
            ("BSpace", "\x7f"),
            ("M-BSpace", "\x1b\x7f"),
            ("C-BSpace", "\x08"),
            ("C-M-BSpace", "\x1b\x08"),
        ] {
            assert_eq!(named(name, LEGACY), bytes, "{name}");
        }
        for mods in [Modifiers::NONE, Modifiers::CTRL, Modifiers::ALT] {
            for f in [13, Function::LAST].into_iter().filter_map(Function::new) {
                let press = KeyPress::new(Key::F(f), mods);
                assert_eq!(stroke(press.into(), LEGACY), "", "{f:?} {mods:?}");
            }
        }
    }

    #[test]
    fn control_bytes_follow_xterm_for_every_c0_control() {
        for (c, byte) in [
            (' ', 0x00),
            ('2', 0x00),
            ('a', 0x01),
            ('Z', 0x1a),
            ('3', 0x1b),
            ('[', 0x1b),
            ('4', 0x1c),
            ('\\', 0x1c),
            ('5', 0x1d),
            (']', 0x1d),
            ('6', 0x1e),
            ('^', 0x1e),
            ('7', 0x1f),
            ('_', 0x1f),
            ('8', 0x7f),
            ('?', 0x7f),
            ('0', b'0'),
            ('9', b'9'),
            ('/', 0x1f),
            ('`', 0x00),
            ('~', 0x1e),
            // What Xlib does not map, Ctrl leaves alone: masking made
            // `;` ESC and `-` CR.
            (';', b';'),
            ('-', b'-'),
            (',', b','),
            ('.', b'.'),
            ('\'', b'\''),
            ('=', b'='),
            ('1', b'1'),
            ('!', b'!'),
        ] {
            let ctrl = KeyPress::new(Key::Char(c), Modifiers::CTRL);
            assert_eq!(stroke(ctrl.into(), LEGACY), char::from(byte).to_string());
        }
    }

    /// Every key, under all eight modifier sets and in every key mode, has a
    /// non-empty encoding, and a plain character is its own UTF-8 but where
    /// all keys are escapes.
    #[test]
    fn key_bytes_is_total_and_never_empty() {
        let mut keys = vec![
            Key::Enter,
            Key::Tab,
            Key::Escape,
            Key::Backspace,
            Key::Delete,
            Key::Insert,
            Key::Home,
            Key::End,
            Key::PageUp,
            Key::PageDown,
        ];
        keys.extend(Direction::ALL.map(Key::Arrow));
        // F13 and up send nothing in the legacy modes (xterm has no bytes
        // for them).
        keys.extend(Function::up_to(12));
        for code in [
            0x20u32, 0x61, 0x5a, 0x30, 0x7e, 0xe9, 0x203c, 0x1f600, 0x300,
        ] {
            if let Some(c) = char::from_u32(code) {
                keys.push(Key::Char(c));
            }
        }
        for &key in &keys {
            for bits in 0u8..8 {
                let mods = Modifiers {
                    ctrl: bits & 1 != 0,
                    alt: bits & 2 != 0,
                    shift: bits & 4 != 0,
                };
                for mode in [
                    LEGACY,
                    APPLICATION,
                    USUAL,
                    ALL_MODIFIERS,
                    DISAMBIGUATED,
                    ALTERNATE,
                    kitty_mode(true, Report::AllKeysWithText),
                ] {
                    let mut bytes = Vec::new();
                    key_bytes(KeyPress { key, mods }.into(), mode, &mut bytes);
                    assert!(!bytes.is_empty(), "{key:?} {mods:?} {mode:?}");
                    // A capital is Shift with its letter, which modifyOtherKeys
                    // 2 sends as an escape, as xterm does.
                    let shifted = c_is_capital(key) && mode == ALL_MODIFIERS;
                    if let Key::Char(c) = key
                        && mods.is_empty()
                        && !shifted
                        && !matches!(mode, KeyMode::Kitty { report, .. } if report != Report::Disambiguated)
                    {
                        let mut buffer = [0u8; 4];
                        assert_eq!(bytes, c.encode_utf8(&mut buffer).as_bytes());
                    }
                }
            }
        }
    }

    #[test]
    fn a_bracketed_paste_cannot_end_itself_early() {
        assert_eq!(pasted("a\x1b[201~b", false), b"a\x1b[201~b");
        assert_eq!(pasted("a\x1b[201~b", true), b"\x1b[200~ab\x1b[201~");
        // A marker that removing another makes is removed too, however
        // deep: a single pass left `\x1b[201~` here.
        assert_eq!(pasted("\x1b[20\x1b[201~1~x", true), b"\x1b[200~x\x1b[201~");
        let mut nested = String::from("x");
        for _ in 0..5 {
            nested = nested.replacen('x', "\x1b[20x1~", 1);
        }
        let nested = nested.replacen('x', "\x1b[201~", 1);
        assert_eq!(pasted(&nested, true), b"\x1b[200~\x1b[201~");
        // The C1 CSI, U+009B, which a terminal may read as ESC [.
        assert_eq!(pasted("a\u{9b}201~b", true), b"\x1b[200~ab\x1b[201~");
        assert_eq!(pasted("\u{9b}20\x1b[201~1~", true), b"\x1b[200~\x1b[201~");
        // Text that only looks alike stays.
        for text in [
            "\x1b[200~",
            "\x1b[2011~",
            "201~",
            "\x1b[201",
            "~~\x1b[20~1~",
            "é~界",
        ] {
            let mut expected = PASTE_START.to_vec();
            expected.extend_from_slice(text.as_bytes());
            expected.extend_from_slice(PASTE_END);
            assert_eq!(pasted(text, true), expected, "{text:?}");
        }
        let pieces = ["\x1b", "[", "2", "0", "1", "~", "\u{9b}", "x", "\x1b[201~"];
        // Every text of up to four pieces.
        let mut texts = vec![String::new()];
        let mut last = texts.clone();
        for _ in 0..4 {
            last = (last.iter())
                .flat_map(|t| pieces.iter().map(move |p| format!("{t}{p}")))
                .collect();
            texts.extend_from_slice(&last);
        }
        for text in texts {
            let out = pasted(&text, true);
            let inner = out
                .strip_prefix(PASTE_START)
                .and_then(|o| o.strip_suffix(PASTE_END))
                .unwrap_or_default();
            let holds = |marker: &[u8]| {
                (0..inner.len()).any(|i| inner.get(i..).is_some_and(|t| t.starts_with(marker)))
            };
            assert!(
                !holds(PASTE_END) && !holds(C1_PASTE_END),
                "{text:?} gave {out:?}"
            );
            let kept = text.matches('x').count();
            assert_eq!(
                inner.iter().filter(|&&b| b == b'x').count(),
                kept,
                "{text:?}"
            );
        }
    }

    /// Each line end, `\n` or `\r\n`, goes as one CR, bracketed or not.
    #[test]
    fn a_pastes_line_ends_go_as_cr() {
        for bracketed in [false, true] {
            let mut out = Vec::new();
            paste("a\nb\r\nc\rd\n", bracketed, &mut out);
            let text: &[u8] = b"a\rb\rc\rd\r";
            let expected = match bracketed {
                true => [PASTE_START, text, PASTE_END].concat(),
                false => text.to_vec(),
            };
            assert_eq!(out, expected, "bracketed {bracketed}");
        }
    }

    #[test]
    fn a_screen_reports_focus_as_its_program_asked() -> Result<(), crate::Error> {
        let mut parser = crate::Parser::new(crate::Size::of(4, 10), 0)?;
        let screen = parser.screen();
        assert_eq!(
            (screen.focus_report(true), screen.focus_report(false)),
            (None, None)
        );
        parser.process(b"\x1b[?1004h")?;
        let screen = parser.screen();
        assert_eq!(screen.focus_report(true), Some(&b"\x1b[I"[..]));
        assert_eq!(screen.focus_report(false), Some(&b"\x1b[O"[..]));
        Ok(())
    }
}
