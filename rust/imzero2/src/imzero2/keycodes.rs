// Key vocabulary for ADR-0177 focus-scoped capture.
//
// GENERATED from public/thestack/imzero2/egui2/keycodes/keycodes.go's Table.
// Do not edit by hand: definition/egui2_definition_d_keys_test.go rebuilds this
// from the Go table and fails if the two disagree, which is what stops the wire
// code, the Go constant and the egui variant drifting apart.

/// Map an egui key to its imzero2 wire code. 0 is "outside the vocabulary";
/// the capture mask cannot name it, so it is never captured.
pub fn imzero_key_code(k: egui::Key) -> u8 {
    match k {
        egui::Key::ArrowUp => 1,    // ArrowUp
        egui::Key::ArrowDown => 2,  // ArrowDown
        egui::Key::ArrowLeft => 3,  // ArrowLeft
        egui::Key::ArrowRight => 4, // ArrowRight
        egui::Key::Home => 5,       // Home
        egui::Key::End => 6,        // End
        egui::Key::PageUp => 7,     // PageUp
        egui::Key::PageDown => 8,   // PageDown
        egui::Key::Enter => 9,      // Enter
        egui::Key::Space => 10,     // Space
        egui::Key::Escape => 11,    // Escape
        egui::Key::Tab => 12,       // Tab
        egui::Key::Backspace => 13, // Backspace
        egui::Key::Delete => 14,    // Delete
        _ => 0,
    }
}
