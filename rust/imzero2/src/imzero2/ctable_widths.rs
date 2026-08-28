//! Column-width apply for the `egui_extras` table surfaces (ADR-0151 §SD4).
//!
//! `egui_table` lets the binding seed its stored widths directly, so the
//! etable apply is a write (see [`super::etable_widths`]). `egui_extras`
//! does not: its `TableState` is private and its `column_widths` are
//! positional, with no accessor. The only lever it exposes is
//! `TableBuilder::reset`, which drops the stored state so the next layout
//! rebuilds from each column's `Column::initial` width.
//!
//! That makes the apply a *reset*, and makes two facts worth pinning
//! against the real crate, because the whole design rests on them and
//! neither is visible from the binding's own code:
//!
//! 1. stored state beats `initial()` on later frames — without this there
//!    would be nothing to reset and Go could simply pass new widths; and
//! 2. `reset()` before `header()`/`body()` restores `initial()`.
//!
//! Both are third-party behaviour that a version bump could change.

#[cfg(test)]
mod tests {
    /// Runs one frame of a single-column table and returns the width the
    /// column was actually allocated.
    ///
    /// Measured from the cell `Ui`'s `max_rect`, deliberately not from the
    /// `Rect` that `TableRow::col` returns: that one is the *used* rect —
    /// egui_extras feeds it straight into `max_used_widths` — so it reports
    /// the width of the cell's content, and a test built on it reads the
    /// same small number no matter what the column does. `TableState`
    /// itself is private, so the cell `Ui` is the observable.
    fn frame_width(ctx: &egui::Context, salt: egui::Id, initial: f32, do_reset: bool) -> f32 {
        use egui::{Pos2, Rect, Vec2};

        let raw = egui::RawInput {
            screen_rect: Some(Rect::from_min_size(Pos2::ZERO, Vec2::new(800.0, 600.0))),
            ..Default::default()
        };
        let mut observed = f32::NAN;
        let _ = ctx.run_ui(raw, |ui| {
            ui.push_id(salt, |ui| {
                let builder = egui_extras::TableBuilder::new(ui)
                    .column(egui_extras::Column::initial(initial).resizable(true));
                if do_reset {
                    builder.reset();
                }
                builder.body(|body| {
                    body.rows(16.0, 1, |mut row| {
                        row.col(|ui| {
                            observed = ui.max_rect().width();
                            ui.label("x");
                        });
                    });
                });
            });
        });
        observed
    }

    /// The premise of the reset-based design: once egui_extras has stored a
    /// width, a different `initial()` on a later frame does not take. If
    /// this stops being true, the binding should pass widths directly and
    /// drop the reset — along with the in-flight drag the reset costs.
    #[test]
    fn stored_state_beats_initial() {
        let ctx = egui::Context::default();
        let salt = egui::Id::new("ctable-widths-premise");

        let first = frame_width(&ctx, salt, 100.0, false);
        assert!(
            (first - 100.0).abs() < 1.0,
            "the first frame must honour initial() (got {first}) — if it does \
             not, this test proves nothing about later frames"
        );

        let second = frame_width(&ctx, salt, 240.0, false);
        assert!(
            (second - 100.0).abs() < 1.0,
            "a later initial() must NOT take while state is stored \
             (first {first}, second {second})"
        );
    }

    /// And the fix: a reset before the body drops the stored width, so the
    /// new `initial()` applies.
    #[test]
    fn reset_restores_initial() {
        let ctx = egui::Context::default();
        let salt = egui::Id::new("ctable-widths-reset");

        let first = frame_width(&ctx, salt, 100.0, false);
        assert!((first - 100.0).abs() < 1.0, "setup: got {first}");

        let after_reset = frame_width(&ctx, salt, 240.0, true);
        assert!(
            (after_reset - 240.0).abs() < 1.0,
            "reset() before body() must drop the stored width so the new \
             initial() applies (before {first}, after {after_reset})"
        );
    }
}
