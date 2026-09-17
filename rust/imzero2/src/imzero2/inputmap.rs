//! Protobuf input events → `egui::Event` translation (ADR-0024 SD8).
//!
//! Lives at the headless-host edge: the FFFI2 interpreter sees the exact
//! same `egui::RawInput` events as under the desktop host and never
//! learns that input is remote. Modifier state is tracked per session
//! from the modifier bitmask each event carries (1=alt, 2=ctrl, 4=shift,
//! `8=mac_cmd`, 16=command — mirrors `egui::Modifiers`).
//!
//! Two wire events are not keystrokes or pointer samples but *state* about the
//! session, and both land here (ADR-0242 SD2): a focus transition, which egui
//! takes as an ordered event plus a `RawInput::focused` flag, and — not a wire
//! event at all — the host's [`InputTranslator::cancel`], called when input
//! stops belonging to the connection that produced it.
//!
//! The one *output*-side translation riding the same edge lives here too:
//! [`cursor_shape_code`] maps egui's per-frame `CursorIcon` onto the wire's
//! cursor-shape code (ADR-0024 Update 2026-07-28), the mirror of what
//! `egui-winit`'s `translate_cursor` does for the desktop host.

use crate::imzero2::inputproto as pb;

/// egui's per-frame cursor request → the wire's shape code
/// (`CursorShape.shape`; ADR-0024 Update 2026-07-28).
///
/// Deliberately an explicit exhaustive match rather than `icon as u32`:
/// `egui::CursorIcon` has no `#[repr]` and no declared ordering stability, so
/// a variant inserted upstream would silently renumber every shape on the
/// wire — a class of bug that would only surface after a routine dependency
/// bump, as a browser showing the wrong cursor. This way an egui that gains a
/// variant fails the build here, where the numbering is decided. The codes are
/// documented (with their CSS keywords) in `input.proto`.
pub fn cursor_shape_code(icon: egui::CursorIcon) -> u32 {
    use egui::CursorIcon as C;
    match icon {
        C::Default => 0,
        C::None => 1,
        C::ContextMenu => 2,
        C::Help => 3,
        C::PointingHand => 4,
        C::Progress => 5,
        C::Wait => 6,
        C::Cell => 7,
        C::Crosshair => 8,
        C::Text => 9,
        C::VerticalText => 10,
        C::Alias => 11,
        C::Copy => 12,
        C::Move => 13,
        C::NoDrop => 14,
        C::NotAllowed => 15,
        C::Grab => 16,
        C::Grabbing => 17,
        C::AllScroll => 18,
        C::ResizeHorizontal => 19,
        C::ResizeNeSw => 20,
        C::ResizeNwSe => 21,
        C::ResizeVertical => 22,
        C::ResizeEast => 23,
        C::ResizeSouthEast => 24,
        C::ResizeSouth => 25,
        C::ResizeSouthWest => 26,
        C::ResizeWest => 27,
        C::ResizeNorthWest => 28,
        C::ResizeNorth => 29,
        C::ResizeNorthEast => 30,
        C::ResizeColumn => 31,
        C::ResizeRow => 32,
        C::ZoomIn => 33,
        C::ZoomOut => 34,
    }
}

fn modifiers_from_bits(bits: u32) -> egui::Modifiers {
    egui::Modifiers {
        alt: bits & 1 != 0,
        ctrl: bits & 2 != 0,
        shift: bits & 4 != 0,
        mac_cmd: bits & 8 != 0,
        command: bits & 16 != 0,
    }
}

/// Pointer-button slots (`egui::PointerButton`), indexed by wire number.
const NUM_POINTER_BUTTONS: usize = 5;

/// How far [`InputTranslator::cancel`] nudges the pointer, in points, before
/// releasing a held button. It only has to beat egui's `max_click_dist` (a
/// handful of points by default, raisable through `Options`), and the nudge is
/// undone before the release, so being generous costs nothing.
const CANCEL_NUDGE: f32 = 64.0;

fn pointer_button(button: u32) -> Option<egui::PointerButton> {
    // Wire numbering is `egui::PointerButton`'s own order, NOT the DOM's
    // (ADR-0242 SD2): `MouseEvent.button` numbers the middle button 1 and the
    // secondary 2, the opposite way round. The viewer translates; the host
    // takes the wire numbers at face value. The .proto documents the contract.
    Some(match button {
        0 => egui::PointerButton::Primary,
        1 => egui::PointerButton::Secondary,
        2 => egui::PointerButton::Middle,
        3 => egui::PointerButton::Extra1,
        4 => egui::PointerButton::Extra2,
        _ => return None,
    })
}

/// Is this keydown a cut? Mirrors `egui-winit`'s predicate of the same name,
/// minus its `cfg!(target_os = "windows")` shift+Delete arm: that arm asks
/// which OS the *integration* runs on, which for a remote session is the wrong
/// end of the wire — the shortcut belongs to whoever is typing, and the host
/// cannot see their platform. `Modifiers::command` is the platform-folded
/// modifier the viewer already sends (bit 16, set for ctrl and for meta).
fn is_cut_command(modifiers: egui::Modifiers, key: egui::Key) -> bool {
    key == egui::Key::Cut || (modifiers.command && key == egui::Key::X)
}

/// Is this keydown a copy? See [`is_cut_command`].
fn is_copy_command(modifiers: egui::Modifiers, key: egui::Key) -> bool {
    key == egui::Key::Copy || (modifiers.command && key == egui::Key::C)
}

/// Per-session translation state. `modifiers` holds the last seen
/// modifier set so `RawInput::modifiers` can be kept coherent between
/// events (egui reads it for hover/shortcut state outside event dispatch).
/// The rest is what [`InputTranslator::cancel`] needs in order to undo input
/// that is still held when its owner goes away (ADR-0242 SD2).
pub struct InputTranslator {
    pub modifiers: egui::Modifiers,
    focused: bool,
    keys_down: Vec<egui::Key>,
    buttons_down: [bool; NUM_POINTER_BUTTONS],
    /// Last position forwarded, kept across `PointerGone` — egui draws the
    /// same distinction (`latest_pos` vs `latest_pos_ignoring_gone`), and a
    /// drag that left the canvas still has to be released somewhere.
    last_pos: Option<egui::Pos2>,
}

impl Default for InputTranslator {
    fn default() -> Self {
        Self {
            modifiers: egui::Modifiers::default(),
            // As `egui::RawInput` itself defaults: integrations opt in to focus
            // tracking, and a host with no carrier — or with a sender that
            // never reports focus — must not look unfocused.
            focused: true,
            keys_down: Vec::new(),
            buttons_down: [false; NUM_POINTER_BUTTONS],
            last_pos: None,
        }
    }
}

impl InputTranslator {
    /// What to put in `egui::RawInput::focused` for the next pass. Starts
    /// focused and only a `Focus` wire event moves it.
    pub fn focused(&self) -> bool {
        self.focused
    }

    /// Restore the default focus state, emitting nothing.
    ///
    /// A focus interval belongs to one owner (ADR-0242 SD2), so the host ends
    /// it along with the ownership rather than carrying a departed owner's
    /// blur into the next one: the next owner may be a driver or an
    /// un-reloaded page that never sends `Focus`, and a session stuck
    /// unfocused has no keyboard at all — `RawInput::focused` gates
    /// `Response::has_focus` and the text caret.
    pub fn reset_focus(&mut self) {
        self.focused = true;
    }

    /// Undo input the host can no longer attribute to a live owner — an owner
    /// change or a disconnect (ADR-0242 SD2), which is server-side and cannot
    /// wait for a departing browser to tidy up.
    ///
    /// Appends releases for everything this translator saw pressed and leaves
    /// no pointer on the canvas, so the next owner starts from rest. Focus is
    /// deliberately untouched (see [`Self::reset_focus`]). Emits nothing when
    /// nothing is held and no position was ever seen, so the host can call it
    /// on every ownership change.
    pub fn cancel(&mut self, out: &mut Vec<egui::Event>) {
        self.cancel_held(out);
    }

    /// The release sequence shared by [`Self::cancel`] and losing focus.
    ///
    /// The pointer part is a nudge away and straight back before the release:
    /// egui decides click-versus-drag at release time from
    /// `has_moved_too_much_for_a_click`, which only a `PointerMoved` past
    /// `max_click_dist` can set, and it does not reconsider the release
    /// position — so a plain synthetic release *completes the click* on
    /// whatever was pressed, which is the thing SD2 forbids. Moving back
    /// before releasing keeps the net pointer delta at zero, so a drag that is
    /// being cancelled stops where it actually was rather than 64 points away.
    fn cancel_held(&mut self, out: &mut Vec<egui::Event>) {
        self.modifiers = egui::Modifiers::default();
        for key in self.keys_down.drain(..) {
            out.push(egui::Event::Key {
                key,
                physical_key: None,
                pressed: false,
                repeat: false,
                modifiers: self.modifiers,
            });
        }
        if let Some(pos) = self.last_pos {
            let held = self.buttons_down;
            if held.iter().any(|down| *down) {
                out.push(egui::Event::PointerMoved(
                    pos + egui::vec2(CANCEL_NUDGE, CANCEL_NUDGE),
                ));
                out.push(egui::Event::PointerMoved(pos));
                for (i, _) in held.iter().enumerate().filter(|(_, down)| **down) {
                    if let Some(button) = pointer_button(i as u32) {
                        out.push(egui::Event::PointerButton {
                            pos,
                            button,
                            pressed: false,
                            modifiers: self.modifiers,
                        });
                    }
                }
            }
            out.push(egui::Event::PointerGone);
        }
        self.buttons_down = [false; NUM_POINTER_BUTTONS];
        self.last_pos = None;
    }

    /// Translate one wire event into zero or more egui events, appended
    /// to `out`.
    pub fn translate(&mut self, ev: pb::input_event::Event, out: &mut Vec<egui::Event>) {
        use pb::input_event::Event as E;
        if !self.focused && !matches!(ev, E::Focus(_) | E::PointerGone(_)) {
            return; // a trailing event cannot reopen a cancelled focus interval
        }
        match ev {
            E::MouseMove(m) => {
                // SD8 trust edge (ADR-0082 hostile model): drop non-finite
                // coordinates from the wire — e.g. a viewer dividing by a
                // zero-width canvas sends NaN. NaN/Inf in egui's pointer state
                // corrupts hit-testing until the next valid move; the PinchZoom
                // arm already guards this way, the pointer/wheel arms did not.
                if m.x.is_finite() && m.y.is_finite() {
                    let pos = egui::pos2(m.x, m.y);
                    self.last_pos = Some(pos);
                    out.push(egui::Event::PointerMoved(pos));
                } else {
                    tracing::debug!(x = m.x, y = m.y, "dropping non-finite pointer move");
                }
            }
            E::MouseButton(b) => {
                self.modifiers = modifiers_from_bits(b.modifiers);
                if !(b.x.is_finite() && b.y.is_finite()) {
                    tracing::debug!(x = b.x, y = b.y, "dropping non-finite pointer button");
                } else if let Some(button) = pointer_button(b.button) {
                    let pos = egui::pos2(b.x, b.y);
                    self.last_pos = Some(pos);
                    self.buttons_down[button as usize] = b.pressed;
                    out.push(egui::Event::PointerButton {
                        pos,
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
                let Some(key) = egui::Key::from_name(&name) else {
                    tracing::debug!(key = %k.key, code = %k.code, "no egui key mapping");
                    return;
                };
                // ADR-0242 SD2: copy/cut are clipboard *actions* to egui, not
                // key presses. Every widget that can copy (TextEdit, selectable
                // labels, and app code) listens for egui::Event::Copy / ::Cut
                // and nothing listens for command+C, so forwarding the keystroke
                // alone made the remote shortcut a no-op. Translating it here —
                // the same place `egui-winit` does it for the desktop host, on
                // the same `Key`/`Modifiers` predicates — keeps the decision at
                // the one input boundary instead of in each widget.
                //
                // Paste keeps its own path and is deliberately untouched: the
                // browser cannot hand us clipboard *contents* from a keydown, so
                // the viewer answers command+V with a ClipboardData message and
                // the host injects egui::Event::Paste when it arrives. The V
                // keystroke still travels as an ordinary Key event, as before.
                if k.pressed {
                    if is_cut_command(self.modifiers, key) {
                        out.push(egui::Event::Cut);
                        return;
                    }
                    if is_copy_command(self.modifiers, key) {
                        out.push(egui::Event::Copy);
                        return;
                    }
                }
                if k.pressed {
                    if !self.keys_down.contains(&key) {
                        self.keys_down.push(key);
                    }
                } else {
                    self.keys_down.retain(|held| *held != key);
                }
                out.push(egui::Event::Key {
                    key,
                    physical_key: None,
                    pressed: k.pressed,
                    repeat: k.repeat,
                    modifiers: self.modifiers,
                });
            }
            E::Text(t) => {
                if !t.text.is_empty() {
                    out.push(egui::Event::Text(t.text));
                }
            }
            E::PointerGone(_) => {
                // `last_pos` deliberately survives: a button held while the
                // pointer leaves the canvas is still draggable in egui, and a
                // later cancellation needs somewhere to release it.
                out.push(egui::Event::PointerGone);
            }
            E::Focus(f) => {
                // ADR-0242 SD2: a focus transition is an ordered input event,
                // so it is translated in place — keystrokes ahead of it belong
                // to the focus interval that is ending. What it does mirrors
                // eframe's web backend on blur/focus: forget the modifier set
                // (an alt-tab never delivers the keyup) and tell egui, which
                // drops its own held keys on `WindowFocused(false)`. Losing
                // focus also cancels held input rather than letting the next
                // release complete a click.
                self.focused = f.focused;
                self.modifiers = egui::Modifiers::default();
                out.push(egui::Event::WindowFocused(f.focused));
                if !f.focused {
                    self.cancel_held(out);
                }
            }
            E::PinchZoom(z) => {
                // Touch pinch; sanitize against zero/NaN from a misbehaving
                // client (egui multiplies zoom state by this factor).
                if z.factor.is_finite() && z.factor > 0.0 {
                    out.push(egui::Event::Zoom(z.factor.clamp(0.2, 5.0)));
                }
            }
            E::AccesskitAction(a) => {
                // ADR-0154 SD3: actuate by node instead of by position. egui
                // honours an injected action request for Click, Focus,
                // SetValue and ScrollIntoView, so a driver targeting a node
                // needs no coordinates at all — and cannot land on whatever
                // moved into a stale position. Painter-only widgets have no
                // node; those still take synthetic pointer events.
                let Some(action) = super::treemap::action_from_code(a.action) else {
                    tracing::debug!(action = a.action, "ignoring unknown accesskit action");
                    return;
                };
                let data = (action == egui::accesskit::Action::SetValue)
                    .then(|| egui::accesskit::ActionData::Value(a.value.clone().into()));
                out.push(egui::Event::AccessKitActionRequest(
                    egui::accesskit::ActionRequest {
                        action,
                        // egui builds its whole tree under the root tree id
                        // (context.rs stamps `TreeId::ROOT` on every update),
                        // so that is the only id a request can target.
                        target_tree: egui::accesskit::TreeId::ROOT,
                        target_node: egui::accesskit::NodeId(a.node_id),
                        data,
                    },
                ));
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

    /// The wire numbering is frozen in `input.proto` and read by a
    /// hand-maintained table in the browser viewer, so it must not drift.
    /// The match itself is exhaustive — an egui that adds a `CursorIcon`
    /// fails the build rather than renumbering silently — but these anchors
    /// catch a *reordering* of the arms, which would still compile.
    #[test]
    fn cursor_shape_codes_are_pinned() {
        use egui::CursorIcon as C;
        for (icon, code) in [
            (C::Default, 0),
            (C::None, 1),
            (C::PointingHand, 4),
            (C::Text, 9),
            (C::Grabbing, 17),
            (C::ResizeHorizontal, 19),
            (C::ResizeVertical, 22),
            (C::ResizeRow, 32),
            (C::ZoomOut, 34),
        ] {
            assert_eq!(cursor_shape_code(icon), code, "wire code for {icon:?}");
        }
    }

    /// Wire modifier bits for "the clipboard modifier", as the viewer sends
    /// them for a ctrl press: 2=ctrl and 16=command (see the .proto).
    const COMMAND: u32 = 2 | 16;

    fn key(name: &str, modifiers: u32, pressed: bool) -> E {
        E::Key(pb::KeyEvent {
            key: name.to_owned(),
            code: String::new(),
            pressed,
            repeat: false,
            modifiers,
        })
    }

    /// ADR-0242 SD2: command+C/X are clipboard actions, not key presses —
    /// nothing in egui listens for the keystroke.
    #[test]
    fn clipboard_shortcuts_become_clipboard_actions() {
        assert!(matches!(
            run(key("c", COMMAND, true)).as_slice(),
            [egui::Event::Copy]
        ));
        assert!(matches!(
            run(key("x", COMMAND, true)).as_slice(),
            [egui::Event::Cut]
        ));
        // A keyboard with dedicated clipboard keys sends them by name, with no
        // modifier at all (egui::Key::Copy / ::Cut).
        assert!(matches!(
            run(key("Copy", 0, true)).as_slice(),
            [egui::Event::Copy]
        ));
        assert!(matches!(
            run(key("Cut", 0, true)).as_slice(),
            [egui::Event::Cut]
        ));
    }

    /// Everything else about the key path is unchanged: plain letters, the
    /// other command shortcuts (select-all is the one this file used to break
    /// if it swallowed too much), and the paste keystroke, whose contents
    /// arrive out of band on `ClipboardData`. Only keydown is translated, as in
    /// `egui-winit`, so the keyup still balances.
    #[test]
    fn normal_key_semantics_are_preserved() {
        for (name, modifiers, pressed) in [
            ("c", 0, true),
            ("a", COMMAND, true),
            ("v", COMMAND, true),
            ("c", COMMAND, false),
            ("x", COMMAND, false),
            ("Enter", 0, true),
        ] {
            let out = run(key(name, modifiers, pressed));
            assert!(
                matches!(out.as_slice(), [egui::Event::Key { .. }]),
                "{name} (modifiers {modifiers}, pressed {pressed}) => {out:?}"
            );
        }
    }

    /// The clipboard predicates read the *tracked* modifier state, so the
    /// bitmask on the copy keydown itself is what decides — a stale ctrl from
    /// an earlier event must not turn a later plain "c" into a copy.
    #[test]
    fn clipboard_modifier_is_taken_from_the_event() {
        let mut t = InputTranslator::default();
        let mut out = Vec::new();
        t.translate(key("c", COMMAND, true), &mut out);
        t.translate(key("c", 0, true), &mut out);
        assert!(
            matches!(out.as_slice(), [egui::Event::Copy, egui::Event::Key { .. }]),
            "{out:?}"
        );
    }

    fn screen() -> egui::Rect {
        egui::Rect::from_min_size(egui::Pos2::ZERO, egui::vec2(320.0, 120.0))
    }

    fn raw(time: f64, events: Vec<egui::Event>) -> egui::RawInput {
        egui::RawInput {
            screen_rect: Some(screen()),
            time: Some(time),
            events,
            ..Default::default()
        }
    }

    fn copied_text(output: &egui::PlatformOutput) -> Vec<&str> {
        output
            .commands
            .iter()
            .filter_map(|c| match c {
                egui::OutputCommand::CopyText(text) => Some(text.as_str()),
                _ => None,
            })
            .collect()
    }

    /// The translation unit tests above pin the event; this pins the part that
    /// motivated ADR-0242 SD2 — that a real, focused `TextEdit` acts on it. The
    /// same session also sends command+A first, so the run covers a shortcut
    /// that must stay an ordinary key event to work.
    #[test]
    fn copy_shortcut_copies_from_a_real_text_edit() {
        let ctx = egui::Context::default();
        let id = egui::Id::new("remote-text-edit");
        let mut text = "hello".to_owned();
        let mut t = InputTranslator::default();

        let pass = |time: f64, events: Vec<egui::Event>, text: &mut String| {
            ctx.run_ui(raw(time, events), |ui| {
                ui.add(egui::TextEdit::singleline(text).id(id));
            })
        };

        pass(0.0, Vec::new(), &mut text);
        ctx.memory_mut(|m| m.request_focus(id));

        let mut events = Vec::new();
        t.translate(key("a", COMMAND, true), &mut events);
        pass(0.1, events, &mut text);

        let mut events = Vec::new();
        t.translate(key("c", COMMAND, true), &mut events);
        let out = pass(0.2, events, &mut text);
        assert_eq!(
            copied_text(&out.platform_output),
            ["hello"],
            "commands: {:?}",
            out.platform_output.commands
        );
        assert_eq!(text, "hello", "copy must not mutate the buffer");

        let mut events = Vec::new();
        t.translate(key("x", COMMAND, true), &mut events);
        let out = pass(0.3, events, &mut text);
        assert_eq!(copied_text(&out.platform_output), ["hello"]);
        assert_eq!(text, "", "cut takes the selection with it");
    }

    fn button_event(x: f32, y: f32, button: u32, pressed: bool) -> E {
        E::MouseButton(pb::MouseButton {
            x,
            y,
            button,
            pressed,
            modifiers: 0,
        })
    }

    /// ADR-0242 SD2: cancellation releases what it saw held — keys first, then
    /// the pointer, which leaves the canvas afterwards.
    #[test]
    fn cancel_releases_held_keys_and_buttons() {
        let mut t = InputTranslator::default();
        let mut out = Vec::new();
        t.translate(key("a", 4, true), &mut out);
        t.translate(button_event(10.0, 10.0, 0, true), &mut out);
        t.translate(button_event(10.0, 10.0, 1, true), &mut out);
        out.clear();

        t.cancel(&mut out);
        assert!(
            matches!(
                out.as_slice(),
                [
                    egui::Event::Key { pressed: false, .. },
                    egui::Event::PointerMoved(_),
                    egui::Event::PointerMoved(_),
                    egui::Event::PointerButton {
                        button: egui::PointerButton::Primary,
                        pressed: false,
                        ..
                    },
                    egui::Event::PointerButton {
                        button: egui::PointerButton::Secondary,
                        pressed: false,
                        ..
                    },
                    egui::Event::PointerGone,
                ]
            ),
            "{out:?}"
        );
        assert!(!t.modifiers.shift, "modifiers are dropped too");
        assert!(t.focused(), "cancellation is not a focus change");

        // Nothing is held any more, so a second call is silent — the host may
        // cancel on every ownership change without thinking about it.
        out.clear();
        t.cancel(&mut out);
        assert!(out.is_empty(), "{out:?}");
    }

    /// A translator that never saw input has nothing to cancel.
    #[test]
    fn cancel_is_silent_when_nothing_was_seen() {
        let mut out = Vec::new();
        InputTranslator::default().cancel(&mut out);
        assert!(out.is_empty(), "{out:?}");
    }

    /// The focus flag starts set (a host with no carrier is not unfocused),
    /// follows `Focus` events, and an ownership change puts it back.
    #[test]
    fn focus_state_tracks_focus_events_and_resets_with_the_owner() {
        let mut t = InputTranslator::default();
        assert!(t.focused());

        let mut out = Vec::new();
        t.translate(E::Focus(pb::Focus { focused: false }), &mut out);
        assert!(!t.focused());
        assert!(
            matches!(out.as_slice(), [egui::Event::WindowFocused(false)]),
            "{out:?}"
        );

        out.clear();
        t.translate(E::Focus(pb::Focus { focused: true }), &mut out);
        assert!(t.focused());
        assert!(
            matches!(out.as_slice(), [egui::Event::WindowFocused(true)]),
            "{out:?}"
        );

        // A blur from a departing owner must not outlive it: the next owner
        // may never report focus at all.
        t.translate(E::Focus(pb::Focus { focused: false }), &mut Vec::new());
        t.reset_focus();
        assert!(t.focused());
    }

    /// Focus is an ordered event: the keystroke ahead of the blur is still
    /// delivered, the cancellation follows the blur, and trailing input is
    /// ignored until focus returns.
    #[test]
    fn focus_is_translated_in_batch_order() {
        let mut t = InputTranslator::default();
        let mut out = Vec::new();
        t.translate(button_event(4.0, 4.0, 0, true), &mut out);
        t.translate(key("A", 0, true), &mut out);
        t.translate(E::Focus(pb::Focus { focused: false }), &mut out);
        t.translate(
            E::Text(pb::TextInput {
                text: "ignored".into(),
            }),
            &mut out,
        );
        t.translate(E::Focus(pb::Focus { focused: true }), &mut out);
        t.translate(E::Text(pb::TextInput { text: "b".into() }), &mut out);
        assert!(
            matches!(
                out.as_slice(),
                [
                    egui::Event::PointerButton { pressed: true, .. },
                    egui::Event::Key { pressed: true, .. },
                    egui::Event::WindowFocused(false),
                    egui::Event::Key { pressed: false, .. },
                    egui::Event::PointerMoved(_),
                    egui::Event::PointerMoved(_),
                    egui::Event::PointerButton { pressed: false, .. },
                    egui::Event::PointerGone,
                    egui::Event::WindowFocused(true),
                    egui::Event::Text(_),
                ]
            ),
            "{out:?}"
        );
    }

    /// Runs press → (optional cancellation) → release against a real button,
    /// as the host would: the release is the stale event of a departed owner.
    fn press_then_release(cancel: bool) -> bool {
        let ctx = egui::Context::default();
        let clicked = std::cell::Cell::new(false);
        let rect = std::cell::Cell::new(egui::Rect::NOTHING);
        let draw = |ui: &mut egui::Ui| {
            let response = ui.add(egui::Button::new("go"));
            rect.set(response.rect);
            if response.clicked() {
                clicked.set(true);
            }
        };

        let _ = ctx.run_ui(raw(0.0, Vec::new()), draw);
        let at = rect.get().center();
        let mut t = InputTranslator::default();

        let mut events = Vec::new();
        t.translate(
            E::MouseMove(pb::MouseMove { x: at.x, y: at.y }),
            &mut events,
        );
        t.translate(button_event(at.x, at.y, 0, true), &mut events);
        let _ = ctx.run_ui(raw(0.05, events), draw);

        if cancel {
            let mut events = Vec::new();
            t.cancel(&mut events);
            let _ = ctx.run_ui(raw(0.10, events), draw);
        }

        let mut events = Vec::new();
        t.translate(button_event(at.x, at.y, 0, false), &mut events);
        let _ = ctx.run_ui(raw(0.15, events), draw);
        clicked.get()
    }

    /// ADR-0242 SD2 against real egui: a cancelled press must not come back as
    /// a click when the release arrives. The uncancelled run is the control —
    /// without it the test would also pass if the press never landed.
    #[test]
    fn cancelled_press_does_not_complete_a_click() {
        assert!(
            press_then_release(false),
            "press and release must still click"
        );
        assert!(
            !press_then_release(true),
            "a cancelled press must not click on release"
        );
    }

    /// The existing `PinchZoom` guard stays in force (regression anchor).
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
