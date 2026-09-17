//! Platform-independent input helpers for the `boxer.imzero2.v1` wire
//! (ADR-0024 SD7/SD8, ADR-0242 SD2, ADR-0243 SD2).
//!
//! Nothing here touches a Windows API type — every function takes plain
//! integers (a virtual-key code, a UTF-16 code unit, a modifier bitmask) so
//! it is usable from the native Windows shell ADR-0243 describes without
//! this crate depending on `windows-sys`/`winapi`, and is exercised by the
//! unit tests below on any host.
//!
//! Three independent concerns live here:
//! - [`Modifiers`] / [`MouseButton`]: the wire's small bitmask and button
//!   numbering, in both directions.
//! - [`key_name_from_vk`]: a Win32 virtual-key code → the wire's `key`
//!   string (the spelling `egui::Key::from_name` and the browser's
//!   `KeyboardEvent.key` already agree on — see `input.proto`'s `KeyEvent`
//!   doc comment). US-layout punctuation only; a mismapped OEM key on
//!   another layout degrades to no key event, matching how an unmapped
//!   browser key is already dropped host-side (`inputmap.rs`) — not a
//!   regression this client introduces.
//! - [`Utf16Assembler`]: combines the UTF-16 code units Windows delivers one
//!   `WM_CHAR` at a time (surrogate pairs for anything outside the BMP —
//!   emoji, rare CJK) into whole `char`s before they go on the wire as a
//!   [`crate::wire::pb::TextInput`] (a proto3 `string`, i.e. UTF-8).
//! - [`InputState`]: tracks held keys/buttons/modifiers/focus for one
//!   session and builds each wire [`pb::InputEvent`] from it, so the
//!   UI event loop only has to report raw transitions.
//!
//! ## Focus and cancellation (ADR-0242 SD2)
//!
//! Losing focus cancels held input *at the host*, not by this client
//! synthesizing button-up/key-up events onto the wire — the ADR's migration
//! note is explicit that an older host lacking that behaviour "should be
//! upgraded rather than relying on synthetic button releases". So
//! [`InputState::focus`] only clears local tracking (so a later refocus
//! does not believe stale keys/buttons are still down) and returns the one
//! [`pb::Focus`] event to send.
//!
//! ## Clipboard (ADR-0082 SD6, ADR-0242 SD2)
//!
//! Copy/cut shortcuts are translated to clipboard actions *at the host* from
//! the ordinary keydown — this client must not special-case them. Paste is
//! the one exception: contents cannot ride a keydown, so [`is_paste_shortcut`]
//! flags command+V so the caller can also send a
//! [`crate::wire::pb::ClipboardData`] alongside the unmodified keystroke.

use crate::wire::pb;

/// `egui::Modifiers`-shaped bitmask (`input.proto`'s header comment):
/// 1=alt, 2=ctrl, 4=shift, 8=mac_cmd, 16=command.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Modifiers {
    pub alt: bool,
    pub ctrl: bool,
    pub shift: bool,
    pub mac_cmd: bool,
    pub command: bool,
}

impl Modifiers {
    pub fn from_bits(bits: u32) -> Self {
        Self {
            alt: bits & 1 != 0,
            ctrl: bits & 2 != 0,
            shift: bits & 4 != 0,
            mac_cmd: bits & 8 != 0,
            command: bits & 16 != 0,
        }
    }

    pub fn to_bits(self) -> u32 {
        (self.alt as u32)
            | ((self.ctrl as u32) << 1)
            | ((self.shift as u32) << 2)
            | ((self.mac_cmd as u32) << 3)
            | ((self.command as u32) << 4)
    }

    /// Build the modifier set for a Windows keyboard, where there is no
    /// separate `mac_cmd` key and Ctrl is the platform's own "command"
    /// modifier — matching how `egui-winit` fills `command` on non-mac
    /// platforms (mirroring `ctrl`, not sourced from a distinct key).
    pub fn windows(alt: bool, ctrl: bool, shift: bool) -> Self {
        Self {
            alt,
            ctrl,
            shift,
            mac_cmd: false,
            command: ctrl,
        }
    }
}

/// Wire mouse-button numbering (`input.proto`'s `MouseButton.button` doc
/// comment) — `egui::PointerButton` order, frozen on the wire regardless of
/// any platform's own numbering.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum MouseButton {
    Primary = 0,
    Secondary = 1,
    Middle = 2,
    Extra1 = 3,
    Extra2 = 4,
}

impl MouseButton {
    pub fn to_wire(self) -> u32 {
        self as u32
    }

    pub fn from_wire(v: u32) -> Option<Self> {
        Some(match v {
            0 => Self::Primary,
            1 => Self::Secondary,
            2 => Self::Middle,
            3 => Self::Extra1,
            4 => Self::Extra2,
            _ => return None,
        })
    }
}

/// `AccessKitAction.action` wire codes (`input.proto`'s doc comment) — pinned
/// independently of `accesskit::Action`'s own numbering for the same reason
/// `CursorShape.shape` is (an upstream reorder must not silently renumber
/// the wire).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum AccessKitAction {
    Click = 0,
    Focus = 1,
    SetValue = 2,
    ScrollIntoView = 3,
}

impl AccessKitAction {
    pub fn to_wire(self) -> u32 {
        self as u32
    }
}

/// Whether this key-down is the client's cue to also send a
/// [`crate::wire::pb::ClipboardData`] paste (ADR-0082 SD6 / ADR-0242 SD2):
/// command+V (Ctrl+V on Windows, since [`Modifiers::windows`] mirrors `ctrl`
/// into `command`), without Alt (a common "paste as plain text" chord on
/// some apps is Ctrl+Shift+V, which this still matches — the host decides
/// what to do with the pasted text, this only decides whether to attach it).
pub fn is_paste_shortcut(key: &str, modifiers: Modifiers) -> bool {
    (key.eq_ignore_ascii_case("v"))
        && (modifiers.ctrl || modifiers.command || modifiers.mac_cmd)
        && !modifiers.alt
}

/// Win32 virtual-key code → the wire's `KeyEvent.key` spelling. US-layout
/// only for OEM punctuation (VK_OEM_*); named/control keys and letters/digits
/// are layout-independent. Returns `None` for keys with no established wire
/// spelling (dead keys, IME keys, media keys other than browser-back) — the
/// caller drops the key event and, for a printable key, still gets the
/// character through `WM_CHAR`/[`Utf16Assembler`] as `TextInput`.
#[allow(clippy::match_same_arms)] // one arm per VK constant reads clearer than merging ranges by hand
pub fn key_name_from_vk(vk: u32) -> Option<&'static str> {
    Some(match vk {
        0x08 => "Backspace",
        0x09 => "Tab",
        0x0D => "Enter",
        0x1B => "Escape",
        0x20 => "Space",
        0x21 => "PageUp",
        0x22 => "PageDown",
        0x23 => "End",
        0x24 => "Home",
        0x25 => "ArrowLeft",
        0x26 => "ArrowUp",
        0x27 => "ArrowRight",
        0x28 => "ArrowDown",
        0x2D => "Insert",
        0x2E => "Delete",
        0x30 => "0",
        0x31 => "1",
        0x32 => "2",
        0x33 => "3",
        0x34 => "4",
        0x35 => "5",
        0x36 => "6",
        0x37 => "7",
        0x38 => "8",
        0x39 => "9",
        0x41 => "A",
        0x42 => "B",
        0x43 => "C",
        0x44 => "D",
        0x45 => "E",
        0x46 => "F",
        0x47 => "G",
        0x48 => "H",
        0x49 => "I",
        0x4A => "J",
        0x4B => "K",
        0x4C => "L",
        0x4D => "M",
        0x4E => "N",
        0x4F => "O",
        0x50 => "P",
        0x51 => "Q",
        0x52 => "R",
        0x53 => "S",
        0x54 => "T",
        0x55 => "U",
        0x56 => "V",
        0x57 => "W",
        0x58 => "X",
        0x59 => "Y",
        0x5A => "Z",
        // Numpad digits/operators (VK_NUMPAD0..9, VK_MULTIPLY..VK_DIVIDE):
        // reported the same as their main-block equivalents — a browser
        // with NumLock on does the same (`key` names the digit, not the pad).
        0x60 => "0",
        0x61 => "1",
        0x62 => "2",
        0x63 => "3",
        0x64 => "4",
        0x65 => "5",
        0x66 => "6",
        0x67 => "7",
        0x68 => "8",
        0x69 => "9",
        0x6A => "*",
        0x6B => "+",
        0x6D => "-",
        0x6E => ".",
        0x6F => "/",
        0x70 => "F1",
        0x71 => "F2",
        0x72 => "F3",
        0x73 => "F4",
        0x74 => "F5",
        0x75 => "F6",
        0x76 => "F7",
        0x77 => "F8",
        0x78 => "F9",
        0x79 => "F10",
        0x7A => "F11",
        0x7B => "F12",
        0x7C => "F13",
        0x7D => "F14",
        0x7E => "F15",
        0x7F => "F16",
        0x80 => "F17",
        0x81 => "F18",
        0x82 => "F19",
        0x83 => "F20",
        0x84 => "F21",
        0x85 => "F22",
        0x86 => "F23",
        0x87 => "F24",
        // US-layout OEM punctuation (VK_OEM_1..VK_OEM_7 and friends).
        0xBA => ";",
        0xBB => "=",
        0xBC => ",",
        0xBD => "-",
        0xBE => ".",
        0xBF => "/",
        0xC0 => "`",
        0xDB => "[",
        0xDC => "\\",
        0xDD => "]",
        0xDE => "'",
        _ => return None,
    })
}

/// Assembles the UTF-16 code units a native Windows message loop delivers
/// one `WM_CHAR` at a time back into whole `char`s, so a surrogate pair
/// (anything outside the Basic Multilingual Plane — most emoji, some CJK)
/// becomes one [`pb::TextInput`] rather than two lone, invalid halves.
///
/// One assembler per input surface: `push` must see every code unit in
/// order, with nothing skipped between a high surrogate and its low half.
#[derive(Debug, Default)]
pub struct Utf16Assembler {
    pending_high: Option<u16>,
}

impl Utf16Assembler {
    pub fn new() -> Self {
        Self::default()
    }

    /// Feed one UTF-16 code unit. Returns the completed `char` once one is
    /// known — either immediately (an ordinary BMP unit) or on the second
    /// half of a surrogate pair — or `None` while a high surrogate awaits
    /// its low half. A low surrogate with no pending high half, or a second
    /// high surrogate before the first was completed, is invalid UTF-16;
    /// the stale/unpaired unit is reported as `'\u{FFFD}'` (Unicode's
    /// replacement character) rather than silently dropped or panicking.
    pub fn push(&mut self, unit: u16) -> Option<char> {
        if let Some(high) = self.pending_high.take() {
            if (0xDC00..=0xDFFF).contains(&unit) {
                return Some(combine_surrogates(high, unit));
            }
        }
        // Either there was no pending high surrogate, or `unit` did not
        // complete it (in which case the stale half above was already
        // dropped by `.take()`) — either way, classify `unit` fresh.
        if (0xD800..=0xDBFF).contains(&unit) {
            self.pending_high = Some(unit);
            None
        } else if (0xDC00..=0xDFFF).contains(&unit) {
            Some('\u{FFFD}') // lone low surrogate
        } else {
            // Every non-surrogate u16 is a valid Unicode scalar value.
            Some(char::from_u32(u32::from(unit)).unwrap_or('\u{FFFD}'))
        }
    }
}

fn combine_surrogates(high: u16, low: u16) -> char {
    let c = 0x1_0000 + ((u32::from(high) - 0xD800) << 10) + (u32::from(low) - 0xDC00);
    char::from_u32(c).unwrap_or('\u{FFFD}')
}

/// Per-session input tracking (ADR-0242 SD2) and the one place that builds
/// each wire [`pb::InputEvent`], so the event loop reports raw transitions
/// (`key_down("Enter")`, `button(Primary, true)`, …) and never hand-assembles
/// a modifiers bitmask or forgets to record what is now held.
#[derive(Debug, Default)]
pub struct InputState {
    modifiers: Modifiers,
    held_keys: std::collections::HashSet<String>,
    held_buttons: [bool; 5],
    focused: bool,
}

impl InputState {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn modifiers(&self) -> Modifiers {
        self.modifiers
    }

    pub fn set_modifiers(&mut self, modifiers: Modifiers) {
        self.modifiers = modifiers;
    }

    pub fn is_focused(&self) -> bool {
        self.focused
    }

    pub fn is_key_held(&self, key: &str) -> bool {
        self.held_keys.contains(key)
    }

    pub fn is_button_held(&self, button: MouseButton) -> bool {
        self.held_buttons[button as usize]
    }

    /// Build a [`pb::MouseMove`] event. Stateless (no modifiers/held-set on
    /// this wire message), kept as a method for a uniform call style with
    /// the rest of this type.
    pub fn mouse_move(&self, x: f32, y: f32) -> pb::InputEvent {
        event(pb::input_event::Event::MouseMove(pb::MouseMove { x, y }))
    }

    /// Build a [`pb::MouseButton`] event and record it in the held set —
    /// consulted by [`Self::focus`] on a later focus loss.
    pub fn mouse_button(
        &mut self,
        x: f32,
        y: f32,
        button: MouseButton,
        pressed: bool,
    ) -> pb::InputEvent {
        self.held_buttons[button as usize] = pressed;
        event(pb::input_event::Event::MouseButton(pb::MouseButton {
            x,
            y,
            button: button.to_wire(),
            pressed,
            modifiers: self.modifiers.to_bits(),
        }))
    }

    /// `unit`: 0=points, 1=lines, 2=pages (`MouseWheel.unit` doc comment).
    pub fn mouse_wheel(&self, dx: f32, dy: f32, unit: u32) -> pb::InputEvent {
        event(pb::input_event::Event::MouseWheel(pb::MouseWheel {
            dx,
            dy,
            unit,
            modifiers: self.modifiers.to_bits(),
        }))
    }

    /// Build a [`pb::KeyEvent`] and record the key in the held set (cleared,
    /// like every other held state, on [`Self::focus`] losing focus).
    /// `code` may be empty — the v1 host mapper reads only `key`
    /// (`input.proto`'s `KeyEvent.code` doc comment).
    pub fn key(
        &mut self,
        key: impl Into<String>,
        code: impl Into<String>,
        pressed: bool,
        repeat: bool,
    ) -> pb::InputEvent {
        let key = key.into();
        if pressed {
            self.held_keys.insert(key.clone());
        } else {
            self.held_keys.remove(&key);
        }
        event(pb::input_event::Event::Key(pb::KeyEvent {
            key,
            code: code.into(),
            pressed,
            repeat,
            modifiers: self.modifiers.to_bits(),
        }))
    }

    pub fn text(&self, text: impl Into<String>) -> pb::InputEvent {
        event(pb::input_event::Event::Text(pb::TextInput {
            text: text.into(),
        }))
    }

    pub fn pointer_gone(&self) -> pb::InputEvent {
        event(pb::input_event::Event::PointerGone(pb::PointerGone {}))
    }

    pub fn pinch_zoom(&self, factor: f32) -> pb::InputEvent {
        event(pb::input_event::Event::PinchZoom(pb::PinchZoom { factor }))
    }

    pub fn accesskit_action(
        &self,
        node_id: u64,
        action: AccessKitAction,
        value: impl Into<String>,
    ) -> pb::InputEvent {
        event(pb::input_event::Event::AccesskitAction(
            pb::AccessKitAction {
                node_id,
                action: action.to_wire(),
                value: value.into(),
            },
        ))
    }

    /// ADR-0242 SD2: report a focus transition. On losing focus, clears this
    /// session's locally tracked keys/buttons — *not* by emitting synthetic
    /// releases onto the wire (the host does the cancellation; see the
    /// module doc) — so a later refocus starts from a clean slate rather
    /// than believing a key released while unfocused is still held.
    pub fn focus(&mut self, focused: bool) -> pb::InputEvent {
        self.focused = focused;
        if !focused {
            self.held_keys.clear();
            self.held_buttons = [false; 5];
        }
        event(pb::input_event::Event::Focus(pb::Focus { focused }))
    }
}

fn event(ev: pb::input_event::Event) -> pb::InputEvent {
    pb::InputEvent { event: Some(ev) }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn modifiers_bits_round_trip() {
        let m = Modifiers {
            alt: true,
            ctrl: false,
            shift: true,
            mac_cmd: false,
            command: true,
        };
        assert_eq!(Modifiers::from_bits(m.to_bits()), m);
        assert_eq!(m.to_bits(), 1 | 4 | 16);
    }

    #[test]
    fn windows_modifiers_mirror_ctrl_into_command() {
        let m = Modifiers::windows(false, true, false);
        assert!(m.command);
        assert!(!m.mac_cmd);
    }

    #[test]
    fn mouse_button_wire_numbers_match_proto_doc() {
        assert_eq!(MouseButton::Primary.to_wire(), 0);
        assert_eq!(MouseButton::Secondary.to_wire(), 1);
        assert_eq!(MouseButton::Middle.to_wire(), 2);
        assert_eq!(MouseButton::from_wire(4), Some(MouseButton::Extra2));
        assert_eq!(MouseButton::from_wire(99), None);
    }

    #[test]
    fn key_name_from_vk_covers_letters_digits_and_named_keys() {
        assert_eq!(key_name_from_vk(0x41), Some("A"));
        assert_eq!(key_name_from_vk(0x30), Some("0"));
        assert_eq!(key_name_from_vk(0x0D), Some("Enter"));
        assert_eq!(key_name_from_vk(0x25), Some("ArrowLeft"));
        assert_eq!(key_name_from_vk(0x70), Some("F1"));
        assert_eq!(key_name_from_vk(0x87), Some("F24"));
        assert_eq!(key_name_from_vk(0x20), Some("Space"));
    }

    #[test]
    fn key_name_from_vk_unmapped_is_none() {
        assert_eq!(key_name_from_vk(0xFF00), None);
    }

    #[test]
    fn is_paste_shortcut_matches_ctrl_v_not_alt_v() {
        let ctrl_v = Modifiers::windows(false, true, false);
        assert!(is_paste_shortcut("v", ctrl_v));
        assert!(is_paste_shortcut("V", ctrl_v));
        let alt_v = Modifiers::windows(true, false, false);
        assert!(!is_paste_shortcut("v", alt_v));
        let ctrl_alt_v = Modifiers::windows(true, true, false);
        assert!(
            !is_paste_shortcut("v", ctrl_alt_v),
            "Alt held rules it out even with Ctrl"
        );
    }

    #[test]
    fn utf16_assembler_passes_through_bmp_chars() {
        let mut a = Utf16Assembler::new();
        assert_eq!(a.push(b'A' as u16), Some('A'));
        assert_eq!(a.push(0x00E9), Some('é'));
    }

    #[test]
    fn utf16_assembler_combines_surrogate_pair() {
        // U+1F600 GRINNING FACE = surrogate pair 0xD83D 0xDE00.
        let mut a = Utf16Assembler::new();
        assert_eq!(a.push(0xD83D), None, "high surrogate awaits its pair");
        assert_eq!(a.push(0xDE00), Some('\u{1F600}'));
    }

    #[test]
    fn utf16_assembler_reports_lone_low_surrogate_as_replacement() {
        let mut a = Utf16Assembler::new();
        assert_eq!(a.push(0xDE00), Some('\u{FFFD}'));
    }

    #[test]
    fn utf16_assembler_drops_stale_high_surrogate_and_resumes() {
        let mut a = Utf16Assembler::new();
        assert_eq!(a.push(0xD83D), None);
        // A second high surrogate arrives before the first's pair — the
        // first is stale; the second starts a fresh pending pair.
        assert_eq!(a.push(0xD83D), None);
        assert_eq!(a.push(0xDE00), Some('\u{1F600}'));
    }

    #[test]
    fn input_state_key_tracks_held_set() {
        let mut s = InputState::new();
        s.key("A", "KeyA", true, false);
        assert!(s.is_key_held("A"));
        s.key("A", "KeyA", false, false);
        assert!(!s.is_key_held("A"));
    }

    #[test]
    fn input_state_focus_loss_clears_held_state_without_synthetic_release() {
        let mut s = InputState::new();
        s.key("A", "KeyA", true, false);
        let _ = s.mouse_button(1.0, 2.0, MouseButton::Primary, true);
        assert!(s.is_key_held("A"));
        assert!(s.is_button_held(MouseButton::Primary));

        let ev = s.focus(false);
        assert_eq!(
            ev,
            pb::InputEvent {
                event: Some(pb::input_event::Event::Focus(pb::Focus { focused: false }))
            }
        );
        assert!(!s.is_key_held("A"), "held state is cleared locally");
        assert!(!s.is_button_held(MouseButton::Primary));
    }

    #[test]
    fn input_state_builds_expected_wire_shapes() {
        let mut s = InputState::new();
        s.set_modifiers(Modifiers::windows(false, true, false));
        match s.mouse_wheel(1.0, -2.0, 0).event.unwrap() {
            pb::input_event::Event::MouseWheel(w) => {
                assert_eq!(w.modifiers, s.modifiers().to_bits())
            }
            other => panic!("unexpected event {other:?}"),
        }
        match s.key("V", "KeyV", true, false).event.unwrap() {
            pb::input_event::Event::Key(k) => {
                assert_eq!(k.key, "V");
                assert!(k.pressed);
                assert!(!k.repeat);
            }
            other => panic!("unexpected event {other:?}"),
        }
    }
}
