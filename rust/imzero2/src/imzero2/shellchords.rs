// The shell's chords, ADR-0346 §SD5.
//
// GENERATED from public/thestack/imzero2/egui2/keycodes/shellchords.go's ShellChords.
// Do not edit by hand: definition/egui2_definition_d_keys_test.go rebuilds this
// from the Go table and fails if the two disagree.

/// The bit a key pressed with Alt (and Shift as stated) sets in the shell's
/// per-frame chord set; 0 for a key that is no chord.
pub fn shell_chord_bit(k: egui::Key, shift: bool) -> u64 {
    match (k, shift) {
        (egui::Key::J, false) => 1u64 << 0,          // FocusNext
        (egui::Key::K, false) => 1u64 << 1,          // FocusPrev
        (egui::Key::H, false) => 1u64 << 2,          // FocusLeft
        (egui::Key::L, false) => 1u64 << 3,          // FocusRight
        (egui::Key::ArrowLeft, false) => 1u64 << 2,  // FocusLeft
        (egui::Key::ArrowRight, false) => 1u64 << 3, // FocusRight
        (egui::Key::ArrowUp, false) => 1u64 << 4,    // FocusUp
        (egui::Key::ArrowDown, false) => 1u64 << 5,  // FocusDown
        (egui::Key::J, true) => 1u64 << 6,           // SwapNext
        (egui::Key::K, true) => 1u64 << 7,           // SwapPrev
        (egui::Key::ArrowLeft, true) => 1u64 << 8,   // SnapLeft
        (egui::Key::ArrowRight, true) => 1u64 << 9,  // SnapRight
        (egui::Key::ArrowUp, true) => 1u64 << 10,    // SnapUp
        (egui::Key::ArrowDown, true) => 1u64 << 11,  // SnapDown
        (egui::Key::Enter, false) => 1u64 << 12,     // Promote
        (egui::Key::Space, false) => 1u64 << 13,     // CycleLayout
        (egui::Key::Space, true) => 1u64 << 14,      // ToggleFloat
        (egui::Key::F, false) => 1u64 << 15,         // Monocle
        (egui::Key::Equals, false) => 1u64 << 16,    // RatioUp
        (egui::Key::Minus, false) => 1u64 << 17,     // RatioDown
        (egui::Key::Plus, false) => 1u64 << 18,      // CountUp
        (egui::Key::Plus, true) => 1u64 << 18,       // CountUp
        (egui::Key::Equals, true) => 1u64 << 18,     // CountUp
        (egui::Key::Minus, true) => 1u64 << 19,      // CountDown
        (egui::Key::Tab, false) => 1u64 << 20,       // PrevActive
        _ => 0,
    }
}
