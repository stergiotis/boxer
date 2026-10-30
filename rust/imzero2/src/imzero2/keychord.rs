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

/// Drains the shell's chords (ADR-0346 §SD5) from the frame's input: every
/// key press whose own modifiers are Alt — with Shift as the chord table
/// states, and neither Ctrl nor the command key — that names a chord leaves
/// the queue, and so does every `Text` event of a frame in which a chord
/// fired, since that text is the chord's letter as the platform spells it
/// (Option+J on macOS types a character; X11 and Windows type the plain
/// letter). Returns the bit set over the table's bits; 0 when nothing fired,
/// and then the queue is untouched.
pub fn drain_shell_chords(ctx: &egui::Context) -> u64 {
    let mut chords: u64 = 0;
    ctx.input_mut(|inp| {
        inp.events.retain(|ev| {
            if let egui::Event::Key {
                key,
                pressed: true,
                modifiers,
                ..
            } = ev
            {
                if modifiers.alt && !modifiers.ctrl && !modifiers.command && !modifiers.mac_cmd {
                    let bit = crate::imzero2::shellchords::shell_chord_bit(*key, modifiers.shift);
                    if bit != 0 {
                        chords |= bit;
                        return false;
                    }
                }
            }
            true
        });
        if chords != 0 {
            inp.events.retain(|ev| !matches!(ev, egui::Event::Text(_)));
        }
    });
    if chords != 0 {
        ctx.request_repaint();
    }
    chords
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

    fn key(k: egui::Key, modifiers: egui::Modifiers) -> egui::Event {
        egui::Event::Key {
            key: k,
            physical_key: None,
            pressed: true,
            repeat: false,
            modifiers,
        }
    }

    #[test]
    fn a_chord_leaves_the_queue_with_its_text_and_the_rest_stays() {
        let ctx = egui::Context::default();
        let alt = mods(false, false, true, false);
        let mut raw = egui::RawInput::default();
        raw.events = vec![
            key(egui::Key::J, alt),
            egui::Event::Text("j".to_owned()),
            key(egui::Key::A, egui::Modifiers::NONE),
            key(egui::Key::J, mods(false, true, false, true)),
        ];
        ctx.begin_pass(raw);
        let set = drain_shell_chords(&ctx);
        assert_eq!(
            set,
            crate::imzero2::shellchords::shell_chord_bit(egui::Key::J, false)
        );
        let left: Vec<egui::Event> = ctx.input(|i| i.events.clone());
        assert_eq!(
            left.len(),
            2,
            "the plain letter and the Ctrl chord stay: {left:?}"
        );
        assert!(matches!(
            left[0],
            egui::Event::Key {
                key: egui::Key::A,
                ..
            }
        ));
        let mut out = ctx.end_pass();
        out.textures_delta.clear();
    }

    #[test]
    fn without_a_chord_nothing_is_touched() {
        let ctx = egui::Context::default();
        let mut raw = egui::RawInput::default();
        raw.events = vec![
            key(egui::Key::A, mods(false, false, true, false)),
            egui::Event::Text("a".to_owned()),
        ];
        ctx.begin_pass(raw);
        assert_eq!(drain_shell_chords(&ctx), 0);
        assert_eq!(ctx.input(|i| i.events.len()), 2);
        let mut out = ctx.end_pass();
        out.textures_delta.clear();
    }
}
