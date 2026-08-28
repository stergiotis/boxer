//! Column-width seeding and read-back for `egui_table` (ADR-0151 §SD4).
//!
//! The interpreter's `endETable` apply code seeds `egui_table::TableState`
//! before `Table::show` and reads it back after. Both halves depend on
//! agreeing with `egui_table` about two things it never states in its API:
//! how the state slot is addressed, and how a column's entry inside it is
//! keyed. Neither is type-checked — a mismatch compiles cleanly and
//! silently does nothing — so the agreement is asserted here against the
//! real crate rather than trusted.
//!
//! The helpers exist to be tested. The apply code inlines the same two
//! expressions because it must run inside the closure that owns
//! `child_ui`; if that ever changes, it should call these.

/// Addresses a table's `TableState` slot exactly as `Table::show` does.
///
/// `Table::id_salt(v)` stores `IdSalt::new(v)` and `show` then derives
/// `TableState::id(ui, salt)`, so seeding from a different `Ui` — the
/// parent rather than the one `show` receives — addresses a slot nothing
/// reads.
pub fn table_state_id(ui: &egui::Ui, id_salt: egui::Id) -> egui::Id {
    egui_table::TableState::id(ui, egui::IdSalt::new(id_salt))
}

/// Keys one column's entry exactly as `Column::id_for` does for a column
/// that carries no explicit id — `egui::Id::new(col_idx)` over a `usize`.
///
/// Take the `usize` from the crate rather than from the integer that
/// happens to be in hand. `Id::new` hashes via ahash, whose `write_usize`
/// folds into `write_u64` on a 64-bit target, so a `u64` key does in fact
/// collide with the `usize` one here — but that is an ahash implementation
/// detail on one pointer width, not a property of `Id`, and it is not
/// something this binding should depend on.
pub fn column_width_key(col_idx: usize) -> egui::Id {
    egui::Id::new(col_idx)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Our key must equal what the crate itself computes for an
    /// implicitly-keyed column. This is the contract that matters: if it
    /// ever fails, seeding writes to slots `egui_table` never reads and
    /// widths silently stop applying.
    #[test]
    fn column_key_matches_egui_table() {
        for i in 0..4usize {
            let col = egui_table::Column::new(100.0);
            assert_eq!(column_width_key(i), col.id_for(i));
        }
    }

    /// End to end against the real crate: seed widths, show the table,
    /// read them back. This is the mechanism the apply code performs, and
    /// it covers the two claims the IDL comments make — that a seeded
    /// state overrides the Go-supplied column width, and that storing a
    /// state at all suppresses the first-show force-autofit that would
    /// otherwise overwrite the seed on the very first frame.
    #[test]
    fn seeded_widths_survive_first_show() {
        use egui::{Pos2, Rect, Vec2};

        struct NullDelegate;
        impl egui_table::TableDelegate for NullDelegate {
            fn header_cell_ui(&mut self, _ui: &mut egui::Ui, _cell: &egui_table::HeaderCellInfo) {}
            fn cell_ui(&mut self, _ui: &mut egui::Ui, _cell: &egui_table::CellInfo) {}
        }

        let ctx = egui::Context::default();
        let raw = egui::RawInput {
            screen_rect: Some(Rect::from_min_size(Pos2::ZERO, Vec2::new(800.0, 600.0))),
            ..Default::default()
        };

        let salt = egui::Id::new("etable-widths-test");
        // Deliberately unlike the seeded widths, so a read-back that
        // matched these would prove the seed did nothing.
        let supplied = [100.0f32, 100.0, 100.0];
        let seeded = [137.0f32, 42.0, 211.0];

        let mut read_back: Vec<f32> = Vec::new();
        let _ = ctx.run_ui(raw, |ui| {
            let bounds = Vec2::new(600.0, 400.0);
            let layout = *ui.layout();
            ui.allocate_ui_with_layout(bounds, layout, |child_ui| {
                let state_id = table_state_id(child_ui, salt);

                let mut st = egui_table::TableState::load(child_ui, state_id).unwrap_or_default();
                for (idx, w) in seeded.iter().enumerate() {
                    st.col_widths.insert(column_width_key(idx), *w);
                }
                st.store(child_ui.ctx(), state_id);

                let table = egui_table::Table::new().id_salt(salt).num_rows(4).columns(
                    supplied.iter().map(|w| egui_table::Column::new(*w)).collect::<Vec<_>>(),
                );
                table.show(child_ui, &mut NullDelegate);

                let after = egui_table::TableState::load(child_ui, state_id);
                for (idx, initial) in supplied.iter().enumerate() {
                    let w = after
                        .as_ref()
                        .and_then(|s| s.col_widths.get(&column_width_key(idx)).copied())
                        .unwrap_or(*initial);
                    read_back.push(w);
                }
            });
        });

        assert_eq!(
            read_back.len(),
            seeded.len(),
            "one width per column must be read back"
        );
        for (idx, want) in seeded.iter().enumerate() {
            assert!(
                (read_back[idx] - want).abs() < 0.001,
                "column {idx}: seeded {want}, read back {} — the seed was \
                 overwritten (first-show autofit not suppressed, or the \
                 state slot / column key does not match egui_table's)",
                read_back[idx]
            );
        }
    }
}
