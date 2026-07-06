//! Protobuf input events → `egui::Event` translation (ADR-0024 SD8).
//!
//! Lives at the headless-host edge: the FFFI2 interpreter sees the exact
//! same `egui::RawInput` events as under the desktop host and never
//! learns that input is remote. Modifier state is tracked per session
//! from the modifier bitmask each event carries (1=alt, 2=ctrl, 4=shift,
//! 8=mac_cmd, 16=command — mirrors `egui::Modifiers`).

use crate::imzero2::inputproto as pb;

fn modifiers_from_bits(bits: u32) -> egui::Modifiers {
    egui::Modifiers {
        alt: bits & 1 != 0,
        ctrl: bits & 2 != 0,
        shift: bits & 4 != 0,
        mac_cmd: bits & 8 != 0,
        command: bits & 16 != 0,
    }
}

fn pointer_button(button: u32) -> Option<egui::PointerButton> {
    // Matches browser MouseEvent.button ordering (the .proto documents it).
    Some(match button {
        0 => egui::PointerButton::Primary,
        1 => egui::PointerButton::Secondary,
        2 => egui::PointerButton::Middle,
        3 => egui::PointerButton::Extra1,
        4 => egui::PointerButton::Extra2,
        _ => return None,
    })
}

/// Per-session translation state. `modifiers` holds the last seen
/// modifier set so `RawInput::modifiers` can be kept coherent between
/// events (egui reads it for hover/shortcut state outside event dispatch).
#[derive(Default)]
pub struct InputTranslator {
    pub modifiers: egui::Modifiers,
}

impl InputTranslator {
    /// Translate one wire event into zero or more egui events, appended
    /// to `out`.
    pub fn translate(&mut self, ev: pb::input_event::Event, out: &mut Vec<egui::Event>) {
        use pb::input_event::Event as E;
        match ev {
            E::MouseMove(m) => {
                // SD8 trust edge (ADR-0082 hostile model): drop non-finite
                // coordinates from the wire — e.g. a viewer dividing by a
                // zero-width canvas sends NaN. NaN/Inf in egui's pointer state
                // corrupts hit-testing until the next valid move; the PinchZoom
                // arm already guards this way, the pointer/wheel arms did not.
                if m.x.is_finite() && m.y.is_finite() {
                    out.push(egui::Event::PointerMoved(egui::pos2(m.x, m.y)));
                } else {
                    tracing::debug!(x = m.x, y = m.y, "dropping non-finite pointer move");
                }
            }
            E::MouseButton(b) => {
                self.modifiers = modifiers_from_bits(b.modifiers);
                if !(b.x.is_finite() && b.y.is_finite()) {
                    tracing::debug!(x = b.x, y = b.y, "dropping non-finite pointer button");
                } else if let Some(button) = pointer_button(b.button) {
                    out.push(egui::Event::PointerButton {
                        pos: egui::pos2(b.x, b.y),
                        button,
                        pressed: b.pressed,
                        modifiers: self.modifiers,
                    });
                } else {
                    tracing::debug!(button = b.button, "ignoring unknown pointer button");
                }
            }
            E::MouseWheel(w) => {
                self.modifiers = modifiers_from_bits(w.modifiers);
                if w.dx.is_finite() && w.dy.is_finite() {
                    let unit = match w.unit {
                        1 => egui::MouseWheelUnit::Line,
                        2 => egui::MouseWheelUnit::Page,
                        _ => egui::MouseWheelUnit::Point,
                    };
                    out.push(egui::Event::MouseWheel {
                        unit,
                        delta: egui::vec2(w.dx, w.dy),
                        // Browser wheel events carry no gesture phase; egui
                        // documents Move as the value for unknown.
                        phase: egui::TouchPhase::Move,
                        modifiers: self.modifiers,
                    });
                } else {
                    tracing::debug!(dx = w.dx, dy = w.dy, "dropping non-finite wheel delta");
                }
            }
            E::Key(k) => {
                self.modifiers = modifiers_from_bits(k.modifiers);
                // Browser KeyboardEvent.key names are accepted by
                // egui::Key::from_name ("ArrowDown", "Enter", "A", ...);
                // single letters arrive lowercase from the browser and
                // egui names them uppercase, so normalize. Unmapped keys
                // (IME intermediates, media keys) are dropped — TextInput
                // carries the printable characters.
                let name = if k.key.chars().count() == 1 {
                    k.key.to_uppercase()
                } else {
                    k.key.clone()
                };
                if let Some(key) = egui::Key::from_name(&name) {
                    out.push(egui::Event::Key {
                        key,
                        physical_key: None,
                        pressed: k.pressed,
                        repeat: k.repeat,
                        modifiers: self.modifiers,
                    });
                } else {
                    tracing::debug!(key = %k.key, code = %k.code, "no egui key mapping");
                }
            }
            E::Text(t) => {
                if !t.text.is_empty() {
                    out.push(egui::Event::Text(t.text));
                }
            }
            E::PointerGone(_) => {
                out.push(egui::Event::PointerGone);
            }
            E::PinchZoom(z) => {
                // Touch pinch; sanitize against zero/NaN from a misbehaving
                // client (egui multiplies zoom state by this factor).
                if z.factor.is_finite() && z.factor > 0.0 {
                    out.push(egui::Event::Zoom(z.factor.clamp(0.2, 5.0)));
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use pb::input_event::Event as E;

    fn run(ev: E) -> Vec<egui::Event> {
        let mut out = Vec::new();
        InputTranslator::default().translate(ev, &mut out);
        out
    }

    #[test]
    fn finite_pointer_move_passes() {
        assert!(matches!(
            run(E::MouseMove(pb::MouseMove { x: 10.0, y: 20.0 })).as_slice(),
            [egui::Event::PointerMoved(_)]
        ));
    }

    /// L1: NaN/Inf pointer coordinates (e.g. a viewer's zero-width-canvas
    /// divide) must not reach egui, where they corrupt hit-testing.
    #[test]
    fn nonfinite_pointer_move_dropped() {
        assert!(
            run(E::MouseMove(pb::MouseMove {
                x: f32::NAN,
                y: 0.0
            }))
            .is_empty()
        );
        assert!(
            run(E::MouseMove(pb::MouseMove {
                x: 0.0,
                y: f32::INFINITY
            }))
            .is_empty()
        );
    }

    #[test]
    fn finite_wheel_passes() {
        let ev = E::MouseWheel(pb::MouseWheel {
            dx: -3.0,
            dy: 5.0,
            unit: 0,
            modifiers: 0,
        });
        assert!(matches!(
            run(ev).as_slice(),
            [egui::Event::MouseWheel { .. }]
        ));
    }

    /// L1: a non-finite scroll delta would persist as a NaN scroll offset and
    /// wedge the affected scroll area — drop it.
    #[test]
    fn nonfinite_wheel_dropped() {
        let ev = E::MouseWheel(pb::MouseWheel {
            dx: 1.0,
            dy: f32::INFINITY,
            unit: 0,
            modifiers: 0,
        });
        assert!(run(ev).is_empty());
    }

    #[test]
    fn finite_button_passes() {
        assert!(matches!(
            run(E::MouseButton(pb::MouseButton {
                x: 1.0,
                y: 2.0,
                button: 0,
                pressed: true,
                modifiers: 0
            }))
            .as_slice(),
            [egui::Event::PointerButton { .. }]
        ));
    }

    /// A non-finite button position is dropped, but the modifier bitmask (which
    /// can never be non-finite) is still tracked, matching the prior contract.
    #[test]
    fn nonfinite_button_dropped_but_modifiers_tracked() {
        let mut t = InputTranslator::default();
        let mut out = Vec::new();
        t.translate(
            E::MouseButton(pb::MouseButton {
                x: f32::NAN,
                y: 0.0,
                button: 0,
                pressed: true,
                modifiers: 4,
            }),
            &mut out,
        );
        assert!(out.is_empty(), "non-finite button position is dropped");
        assert!(
            t.modifiers.shift,
            "modifier state (bit 4 = shift) is still tracked"
        );
    }

    /// The existing PinchZoom guard stays in force (regression anchor).
    #[test]
    fn nonfinite_pinch_dropped() {
        assert!(run(E::PinchZoom(pb::PinchZoom { factor: f32::NAN })).is_empty());
        assert!(run(E::PinchZoom(pb::PinchZoom { factor: 0.0 })).is_empty());
        assert!(matches!(
            run(E::PinchZoom(pb::PinchZoom { factor: 1.5 })).as_slice(),
            [egui::Event::Zoom(_)]
        ));
    }
}
