//! ADR-0334 §SD7: chord matching for the key-capture register (ADR-0177).
//!
//! A plain capture (`captureKeys`) matches a key regardless of modifiers and
//! reports them alongside — the SD5 rule, which keeps Shift+Down from being
//! missed. A chord capture (`captureKeyChords`) is the opposite contract: the
//! key is taken only while the platform command modifier is held, with Shift
//! required or forbidden exactly as the caller said, so Ctrl+B is a shortcut
//! and b stays a letter the editor types.
//!
//! The byte the caller passes is the modifier byte R26 reports (bit 0 shift,
//! bit 1 ctrl, bit 2 alt, bit 3 command). Only bits 0 and 3 are honoured:
//! `command` is already Ctrl on Linux and Windows and Cmd on macOS, which is
//! what every platform's shortcut convention means; Alt and bare Ctrl produce
//! `Event::Text` on some platforms (Option on macOS, AltGr), so a chord over
//! them would need text suppression, which ADR-0177 §SD10 keeps out of this
//! channel. A byte without the command bit matches nothing: there is no such
//! thing as a chord on a bare letter here.

/// The modifier byte's bits, as R26 reports them.
pub const MOD_SHIFT: u8 = 1 << 0;
pub const MOD_COMMAND: u8 = 1 << 3;

/// True when `m`, an event's own modifiers, is exactly the chord `want`
/// asks for: command held, shift as stated, alt not held.
pub fn chord_matches(m: &egui::Modifiers, want: u8) -> bool {
    if want & MOD_COMMAND == 0 {
        return false;
    }
    m.command && m.shift == (want & MOD_SHIFT != 0) && !m.alt
}

/// Packs an event's modifiers into the R26 byte, the same encoding the
/// per-frame snapshot uses (bit 0 shift, bit 1 ctrl, bit 2 alt, bit 3 command).
pub fn mods_byte(m: &egui::Modifiers) -> u8 {
    (m.shift as u8) | ((m.ctrl as u8) << 1) | ((m.alt as u8) << 2) | ((m.command as u8) << 3)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn mods(shift: bool, ctrl: bool, alt: bool, command: bool) -> egui::Modifiers {
        egui::Modifiers {
            alt,
            ctrl,
            shift,
            mac_cmd: false,
            command,
        }
    }

    #[test]
    fn a_bare_letter_never_matches_a_chord() {
        assert!(!chord_matches(
            &mods(false, false, false, false),
            MOD_COMMAND
        ));
        // and a chord byte without the command bit asks for nothing
        assert!(!chord_matches(&mods(false, false, false, false), 0));
        assert!(!chord_matches(&mods(true, false, false, false), MOD_SHIFT));
    }

    #[test]
    fn command_matches_and_shift_is_exact() {
        assert!(chord_matches(&mods(false, true, false, true), MOD_COMMAND));
        assert!(!chord_matches(&mods(true, true, false, true), MOD_COMMAND));
        assert!(chord_matches(
            &mods(true, true, false, true),
            MOD_COMMAND | MOD_SHIFT
        ));
        assert!(!chord_matches(
            &mods(false, true, false, true),
            MOD_COMMAND | MOD_SHIFT
        ));
    }

    #[test]
    fn alt_is_never_part_of_a_chord() {
        assert!(!chord_matches(&mods(false, true, true, true), MOD_COMMAND));
    }

    #[test]
    fn the_byte_round_trips() {
        let m = mods(true, false, true, true);
        assert_eq!(mods_byte(&m), MOD_SHIFT | (1 << 2) | MOD_COMMAND);
    }
}
