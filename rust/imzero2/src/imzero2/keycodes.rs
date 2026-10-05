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
        egui::Key::ArrowUp => 1,       // ArrowUp
        egui::Key::ArrowDown => 2,     // ArrowDown
        egui::Key::ArrowLeft => 3,     // ArrowLeft
        egui::Key::ArrowRight => 4,    // ArrowRight
        egui::Key::Home => 5,          // Home
        egui::Key::End => 6,           // End
        egui::Key::PageUp => 7,        // PageUp
        egui::Key::PageDown => 8,      // PageDown
        egui::Key::Enter => 9,         // Enter
        egui::Key::Space => 10,        // Space
        egui::Key::Escape => 11,       // Escape
        egui::Key::Tab => 12,          // Tab
        egui::Key::Backspace => 13,    // Backspace
        egui::Key::Delete => 14,       // Delete
        egui::Key::Num0 => 15,         // Digit0
        egui::Key::Num1 => 16,         // Digit1
        egui::Key::Num2 => 17,         // Digit2
        egui::Key::Num3 => 18,         // Digit3
        egui::Key::Num4 => 19,         // Digit4
        egui::Key::Num5 => 20,         // Digit5
        egui::Key::Num6 => 21,         // Digit6
        egui::Key::Num7 => 22,         // Digit7
        egui::Key::Num8 => 23,         // Digit8
        egui::Key::Num9 => 24,         // Digit9
        egui::Key::A => 25,            // KeyA
        egui::Key::B => 26,            // KeyB
        egui::Key::C => 27,            // KeyC
        egui::Key::D => 28,            // KeyD
        egui::Key::E => 29,            // KeyE
        egui::Key::F => 30,            // KeyF
        egui::Key::G => 31,            // KeyG
        egui::Key::H => 32,            // KeyH
        egui::Key::I => 33,            // KeyI
        egui::Key::J => 34,            // KeyJ
        egui::Key::K => 35,            // KeyK
        egui::Key::L => 36,            // KeyL
        egui::Key::M => 37,            // KeyM
        egui::Key::N => 38,            // KeyN
        egui::Key::O => 39,            // KeyO
        egui::Key::P => 40,            // KeyP
        egui::Key::Q => 41,            // KeyQ
        egui::Key::R => 42,            // KeyR
        egui::Key::S => 43,            // KeyS
        egui::Key::T => 44,            // KeyT
        egui::Key::U => 45,            // KeyU
        egui::Key::V => 46,            // KeyV
        egui::Key::W => 47,            // KeyW
        egui::Key::X => 48,            // KeyX
        egui::Key::Y => 49,            // KeyY
        egui::Key::Z => 50,            // KeyZ
        egui::Key::Plus => 51,         // Plus
        egui::Key::Minus => 52,        // Minus
        egui::Key::Equals => 53,       // Equals
        egui::Key::Period => 54,       // Period
        egui::Key::Comma => 55,        // Comma
        egui::Key::Slash => 56,        // Slash
        egui::Key::Colon => 57,        // Colon
        egui::Key::Quote => 58,        // Quote
        egui::Key::OpenBracket => 59,  // OpenBracket
        egui::Key::CloseBracket => 60, // CloseBracket
        _ => 0,
    }
}
