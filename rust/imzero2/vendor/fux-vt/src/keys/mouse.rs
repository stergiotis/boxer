//! Mouse events and their reports, as xterm sends them
//! (`references/xterm/ctlseqs.html`, "Mouse Tracking"): what a user's
//! terminal reports is a [`MouseEvent`], and
//! [`crate::Screen::encode_mouse`] reports one to a program in the mode
//! and encoding it asked for.
//!
//! A report's button code is the button (0 left, 1 middle, 2 right, 3 a
//! release in the encodings that do not name the button; 64 to 67 the
//! wheel; 128 and 129 the back and forward buttons), plus 4 for Shift, 8 for
//! Alt and 16 for Ctrl, plus 32 for motion. Positions count from one.
use crate::keys::encode::key_bytes;
use crate::keys::{Direction, Key, KeyPress, Modifiers};
use crate::{Mode, MouseProtocolEncoding, MouseProtocolMode};
use std::io::Write;

closed! {
    /// A mouse button.
    #[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
    pub enum MouseButton {
        /// The button's part of a report's button code.
        fn code(self) -> u32;
        /// The left button (button 1).
        Left => 0,
        /// The middle button (button 2).
        Middle => 1,
        /// The right button (button 3).
        Right => 2,
        /// The wheel turned up (button 4).
        WheelUp => 64,
        /// The wheel turned down (button 5).
        WheelDown => 65,
        /// The wheel tilted left (button 6).
        WheelLeft => 66,
        /// The wheel tilted right (button 7).
        WheelRight => 67,
        /// The back button (button 8).
        Back => 128,
        /// The forward button (button 9).
        Forward => 129,
    }
}

impl MouseButton {
    /// Whether this is the wheel, whose turns are presses never released:
    /// its codes, and buttons 6 and 7's, add 64 (ctlseqs).
    pub fn is_wheel(self) -> bool {
        (64..128).contains(&self.code())
    }

}

/// What a mouse event is, with its button.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum MouseAction {
    /// A button pressed, or the wheel turned.
    Press(MouseButton),
    /// A button released; `None` where the terminal did not say which (the
    /// default and UTF-8 encodings never do).
    Release(Option<MouseButton>),
    /// The pointer moved to another cell, with the button held, if any.
    Motion(Option<MouseButton>),
}

/// A mouse event over a screen.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct MouseEvent {
    /// What happened.
    pub action: MouseAction,
    /// The modifiers held.
    pub mods: Modifiers,
    /// The cell's row, from zero at the top of the screen.
    pub row: u16,
    /// The cell's column, from zero at the left.
    pub col: u16,
}

/// Where a report's button code holds the modifiers' bits
/// (`Modifiers::bits`): Shift 4, Alt 8, Ctrl 16.
const MODIFIERS_AT: u32 = 2;
/// The motion bit of a report's button code.
const MOTION: u32 = 32;
/// The button code of a release in the encodings that do not name the
/// button, and of motion with no button held.
const NO_BUTTON: u32 = 3;
/// The largest position the default encoding carries: 32 more is the
/// largest byte.
const DEFAULT_LIMIT: u32 = 223;
/// The largest position the UTF-8 encoding carries, as xterm has it: 32
/// more is the largest two-byte character.
const UTF8_LIMIT: u32 = 2015;

/// Checked ordinary wheel distance, bounded like Ctrl's half-page step.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct ScrollLines(u16);

impl ScrollLines {
    /// A distance of 0 to 32,767 arrows, or a refusal.
    pub fn new(lines: usize) -> Option<Self> {
        u16::try_from(lines).ok().filter(|n| *n <= 32767).map(Self)
    }
    /// The number of arrows.
    pub fn get(self) -> u16 {
        self.0
    }
}

impl crate::Screen {
    /// Encodes `event` as the program asked, in chunks of at most 64 bytes.
    /// With mouse reporting enabled, yields its report ([`mouse_bytes`]),
    /// or nothing if the mode does not report this event. With no reporting,
    /// alternate scroll (`CSI ? 1007 h`) on the alternate screen sends Up
    /// or Down through the negotiated key encoder: `lines` keys, or half the
    /// screen's rows with Ctrl. Modifiers choose the scroll distance and
    /// are not sent with the arrows (xterm 411's `AlternateScroll`).
    ///
    /// `lines` is the ordinary wheel step, at most 32,767 arrows.
    /// Returns `None` when the event is not reported, and `Some` of an
    /// empty iterator for an accepted zero-distance scroll. Chunks keep
    /// whole keys in order, at most 32,767 arrows in an expansion. Callers
    /// must admit the whole expansion as one input action or refuse it.
    pub fn encode_mouse(
        &self,
        event: MouseEvent,
        lines: ScrollLines,
    ) -> Option<impl Iterator<Item = Vec<u8>> + use<>> {
        let mode = self.mouse_protocol_mode();
        let mut unit = Vec::new();
        let count = if mode == MouseProtocolMode::None
            && let Some((key, lines)) = self.alternate_scroll(event, lines.get())
        {
            key_bytes(KeyPress::plain(key).into(), self.key_mode(), &mut unit);
            lines
        } else if mouse_bytes(event, mode, self.mouse_protocol_encoding(), &mut unit) {
            1
        } else {
            return None;
        };
        // B45 bounds each unit at 64 bytes; a step always advances.
        let per = 64usize.checked_div(unit.len()).unwrap_or(1).max(1);
        let count = usize::from(count);
        Some((0..count.div_ceil(per)).map(move |chunk| {
            let left = count.saturating_sub(chunk.saturating_mul(per));
            std::iter::repeat_n(unit.as_slice(), per.min(left))
                .flatten()
                .copied()
                .collect()
        }))
    }

    /// A wheel turn's key and distance, while alternate scrolling applies.
    fn alternate_scroll(&self, event: MouseEvent, lines: u16) -> Option<(Key, u16)> {
        let direction = match event.action {
            MouseAction::Press(MouseButton::WheelUp) => Direction::Up,
            MouseAction::Press(MouseButton::WheelDown) => Direction::Down,
            MouseAction::Press(_) | MouseAction::Release(_) | MouseAction::Motion(_) => {
                return None;
            }
        };
        if !self.mode(Mode::AlternateScroll) || !self.mode(Mode::AlternateScreenSaveCursor) {
            return None;
        }
        let lines = if event.mods.ctrl {
            self.size().rows() / 2
        } else {
            lines
        };
        Some((Key::Arrow(direction), lines))
    }
}

/// Appends `event`'s report to `out` in `mode` and `encoding`, as xterm
/// reports it, and returns whether there was one:
/// - with no mode, nothing is reported;
/// - X10 (`Press`, mode 9) reports presses of the left, middle and right
///   buttons alone, without modifiers;
/// - `PressRelease` (1000) reports presses and releases, the wheel's turns
///   as presses and never released;
/// - `ButtonMotion` (1002) also reports motion while a button is held;
/// - `AnyMotion` (1003) also reports motion with none held;
/// - a press names its button; motion over a cell is the host's to tell
///   from motion within it (xterm reports a move to another cell only);
/// - highlight tracking (1001) reports as `PressRelease` does;
/// - the default encoding sends `CSI M` and three bytes, each value plus
///   32, and a 0 byte for a position past 223; UTF-8 (1005) sends each
///   value plus 32 as a UTF-8 character, the button code too, and a 0 byte
///   for a position past 2015 (xterm 411's past-end marker); SGR (1006) sends
///   `CSI < code ; column ; row M`, or `m` for a release, which names its
///   button, for any position; urxvt (1015) `CSI code ; column ; row M`, the
///   code plus 32 and a release unnamed (xterm 411's `EditorButton`).
pub(crate) fn mouse_bytes(
    event: MouseEvent,
    mode: MouseProtocolMode,
    encoding: MouseProtocolEncoding,
    out: &mut Vec<u8>,
) -> bool {
    let reported = match (mode, event.action) {
        (MouseProtocolMode::None, _) => false,
        // X10 knew three buttons: "Cb is button−1, where button is 1, 2 or 3".
        (MouseProtocolMode::Press, MouseAction::Press(button)) => button.code() < 3,
        (_, MouseAction::Press(_)) => true,
        (MouseProtocolMode::Press, _) => false,
        (_, MouseAction::Release(button)) => !button.is_some_and(MouseButton::is_wheel),
        (
            MouseProtocolMode::PressRelease | MouseProtocolMode::Highlight,
            MouseAction::Motion(_),
        ) => false,
        (MouseProtocolMode::ButtonMotion, MouseAction::Motion(held)) => {
            held.is_some_and(|b| !b.is_wheel())
        }
        (MouseProtocolMode::AnyMotion, MouseAction::Motion(held)) => {
            !held.is_some_and(MouseButton::is_wheel)
        }
    };
    if !reported {
        return false;
    }
    // The button the report names, its motion bit, and SGR's final byte.
    let names_release = encoding == MouseProtocolEncoding::Sgr;
    let (button, motion, last) = match event.action {
        MouseAction::Press(button) => (Some(button), 0, 'M'),
        MouseAction::Release(button) => (button.filter(|_| names_release), 0, 'm'),
        MouseAction::Motion(held) => (held, MOTION, 'M'),
    };
    let mut code = button.map_or(NO_BUTTON, MouseButton::code) | motion;
    if mode != MouseProtocolMode::Press {
        code |= u32::from(event.mods.bits()) << MODIFIERS_AT;
    }
    let (x, y) = (u32::from(event.col), u32::from(event.row));
    let (x, y) = (x.saturating_add(1), y.saturating_add(1));
    match encoding {
        // Writing to a Vec cannot fail.
        MouseProtocolEncoding::Sgr => {
            let _ = write!(out, "\x1b[<{code};{x};{y}{last}");
        }
        MouseProtocolEncoding::Urxvt => {
            let code = code.saturating_add(32);
            let _ = write!(out, "\x1b[{code};{x};{y}M");
        }
        // A position past the encoding's limit is clamped to it and sent as
        // a 0 byte, "a past-end marker", as xterm 411 does
        // (`EmitMousePosition`, `button.c`): ctlseqs gives the limits only.
        MouseProtocolEncoding::Default => {
            let byte = |n: u32| u8::try_from(n.saturating_add(32)).unwrap_or(u8::MAX);
            let place = |n: u32| if n > DEFAULT_LIMIT { 0 } else { byte(n) };
            out.extend_from_slice(b"\x1b[M");
            out.extend_from_slice(&[byte(code), place(x), place(y)]);
        }
        MouseProtocolEncoding::Utf8 => {
            out.extend_from_slice(b"\x1b[M");
            for (n, place) in [(code, false), (x, true), (y, true)] {
                if place && n > UTF8_LIMIT {
                    out.push(0);
                    continue;
                }
                let c = char::from_u32(n.saturating_add(32)).unwrap_or(char::MAX);
                let mut utf8 = [0; 4];
                out.extend_from_slice(c.encode_utf8(&mut utf8).as_bytes());
            }
        }
    }
    true
}

#[cfg(test)]
mod tests;
