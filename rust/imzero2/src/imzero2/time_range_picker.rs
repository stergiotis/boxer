// TimeRangePicker is a compound FFFI2 widget for ADR-0016. The widget
// surfaces a compact trigger button in the host layout; clicking it
// opens a Grafana-style popover anchored below the button:
//
//   trigger:   [ Last 1 hour  ·  UTC                       ▾ ]
//
//   popover:
//   ┌──────────────────────────────┬───────────────────────┐
//   │ Absolute time range          │ Quick ranges          │
//   │   From                       │ Last 5 minutes        │
//   │   [_________________]        │ Last 15 minutes       │
//   │   [📅 ymd] [h] [m] [s]        │ Last 30 minutes       │
//   │   To                         │ Last 1 hour           │
//   │   [_________________]        │ Last 6 hours          │
//   │   [📅 ymd] [h] [m] [s]        │ Today                 │
//   │   [Apply time range] [Cancel]│ Yesterday             │
//   │ ─────────────────            │ This week             │
//   │ Change time settings         │ … (scroll)            │
//   │   tz: [UTC                ▾] │                       │
//   │   refresh: NNNN ms           │                       │
//   └──────────────────────────────┴───────────────────────┘
//
// State model:
//   - The IDL inputs `from_initial` and `to_initial` set the
//     first-frame draft contents per widget id; afterwards drafts
//     persist in egui memory across frames so the user can type
//     freely without external pushes clobbering keystrokes.
//   - Each calendar pop carries a jiff::civil::Date plus three u8s
//     (h:m:s) in the draft state. On ANY change among those four
//     sub-widgets, the corresponding text draft is overwritten with
//     a ClickHouse SQL string literal `'YYYY-MM-DD HH:MM:SS'`.
//   - The tz dropdown picks from a small hardcoded set of IANA zones
//     plus the configured Tz() builder value (so the configured tz is
//     always reachable). The picked tz is part of the wire payload.
//   - Apply button click pushes `tz\x1efrom\x1eto` via r9_s, closes
//     the popover, and refreshes the Cancel snapshot. Preset row
//     buttons act as "instant apply" — set both drafts to the preset
//     SQL and immediately push + close.
//   - Cancel button reverts draft.from / draft.to / draft.tz to the
//     last-applied snapshot and closes the popover. The snapshot is
//     seeded with the picker's initial values at first render and
//     refreshed on every Apply / preset click.
//   - Click outside the popover (or Esc) closes without reverting:
//     edits persist so the user can re-open and continue.
//
// Wire format (Phase 4b): a single r9_s push of a packed
// `tz_name\x1efrom_expression\x1eto_expression` string (\x1e = ASCII
// record separator). Phase 3's 2-segment shape is decoded as
// (from, to) with empty tz by timerangepicker.UnpackRange — kept for
// interoperability while no other consumer survives.

use egui::{ComboBox, DragValue, Id, Popup, PopupCloseBehavior, Response, TextEdit, Ui, Widget};
use jiff::Timestamp;
use jiff::civil::Date;
use jiff::tz::TimeZone;

use super::interpreter::ImZeroFffi;

const RANGE_DELIMITER: char = '\x1e';

const TZ_CANDIDATES: &[&str] = &[
    "UTC",
    "System",
    "Europe/Berlin",
    "Europe/London",
    "America/New_York",
    "America/Los_Angeles",
    "Asia/Tokyo",
    "Asia/Shanghai",
    "Asia/Kolkata",
    "Australia/Sydney",
];

// Popover layout budgets. Width is split between the left (absolute /
// tz) column and the right (presets) column with a separator between.
const POPUP_WIDTH: f32 = 560.0;
const LEFT_COL_WIDTH: f32 = 300.0;
const RIGHT_COL_WIDTH: f32 = 230.0;
const TEXT_EDIT_WIDTH: f32 = 280.0;
const PRESET_LIST_MAX_HEIGHT: f32 = 360.0;
const TRIGGER_MIN_WIDTH: f32 = 240.0;
// Trigger label truncation — keep the button readable at minimum width.
const TRIGGER_EXPR_MAX: usize = 22;

#[derive(Default)]
pub struct TimeRangePickerRequest {
    pub presets: Vec<PresetEntry>,
    /// IANA timezone the picker advertises (Phase 4 of ADR-0016).
    /// Seeds the in-widget dropdown's initial selection; the user can
    /// pick a different zone from the dropdown, in which case the
    /// new selection rides the wire on the next Apply / preset click.
    pub tz: Option<String>,
    /// Auto-refresh interval in milliseconds the picker advertises
    /// (Phase 4 of ADR-0016). The picker only displays the value;
    /// the auto-refresh runner that drives re-Apply on a timer is
    /// out of scope for the widget.
    pub refresh_interval_ms: Option<u32>,
    /// From bound of the most recently chlocalbroker-evaluated range,
    /// in epoch milliseconds. Paired with `evaluated_to_ms`; both
    /// arrive together via the `evaluatedBounds` builder. When Some,
    /// the trigger button renders human wall-clock time instead of
    /// the raw SQL expression — but only when no preset matches the
    /// last-applied draft (preset labels are more informative than
    /// dates for canned ranges).
    pub evaluated_from_ms: Option<i64>,
    /// To bound; see `evaluated_from_ms`.
    pub evaluated_to_ms: Option<i64>,
}

#[derive(Clone)]
pub struct PresetEntry {
    pub label: String,
    pub from_sql: String,
    pub to_sql: String,
}

#[derive(Clone)]
struct DraftState {
    from: String,
    to: String,
    tz: String,
    from_date: Date,
    to_date: Date,
    from_h: u8,
    from_m: u8,
    from_s: u8,
    to_h: u8,
    to_m: u8,
    to_s: u8,
    // Last-applied snapshot for Cancel. Seeded with the initial values
    // at first render and refreshed on every Apply / preset click.
    last_from: String,
    last_to: String,
    last_tz: String,
}

/// Today in UTC. Used to seed the calendar pop's initial display.
fn default_today() -> Date {
    Timestamp::now().to_zoned(TimeZone::UTC).datetime().date()
}

/// Render the calendar pop button followed by three h:m:s DragValues.
/// On any change among the four sub-widgets, `text` is overwritten
/// with a ClickHouse SQL string literal `'YYYY-MM-DD HH:MM:SS'`.
#[allow(clippy::too_many_arguments)]
fn render_calendar_pop(
    ui: &mut Ui,
    text: &mut String,
    date: &mut Date,
    h: &mut u8,
    m: &mut u8,
    s: &mut u8,
    salt: &str,
) {
    let r_d = ui.add(egui_extras::DatePickerButton::new(date).id_salt(salt));
    let r_h = ui.add(DragValue::new(h).range(0u8..=23u8).suffix("h"));
    let r_m = ui.add(DragValue::new(m).range(0u8..=59u8).suffix("m"));
    let r_s = ui.add(DragValue::new(s).range(0u8..=59u8).suffix("s"));
    if r_d.changed() || r_h.changed() || r_m.changed() || r_s.changed() {
        *text = format!(
            "'{:04}-{:02}-{:02} {:02}:{:02}:{:02}'",
            date.year(),
            date.month(),
            date.day(),
            *h,
            *m,
            *s,
        );
    }
}

/// Compact a free-form expression for the trigger button. Keeps the
/// button readable when expressions are long SQL strings.
fn compact(s: &str, max: usize) -> String {
    let count = s.chars().count();
    if count <= max {
        s.to_string()
    } else {
        let head: String = s.chars().take(max.saturating_sub(1)).collect();
        format!("{head}…")
    }
}

/// Resolve our dropdown's tz name to a jiff::tz::TimeZone. "System"
/// maps to the host's local zone via TZ env / /etc/localtime; "UTC"
/// to the constant; anything else is an IANA lookup falling back to
/// UTC on failure (consistent with the picker's own initial-tz
/// default).
fn resolve_tz(name: &str) -> jiff::tz::TimeZone {
    match name {
        "System" => jiff::tz::TimeZone::system(),
        "UTC" => jiff::tz::TimeZone::UTC,
        other => jiff::tz::TimeZone::get(other).unwrap_or(jiff::tz::TimeZone::UTC),
    }
}

/// Format an evaluated (from, to) epoch-millisecond pair as wall-clock
/// time in the named zone. Collapses to `"YYYY-MM-DD HH:MM → HH:MM"`
/// when both bounds fall on the same calendar day; otherwise renders
/// both dates in full. Minute precision matches Grafana's trigger
/// (seconds remain visible inside the popover's h:m:s drag values).
fn format_bounds(from_ms: i64, to_ms: i64, tz_name: &str) -> String {
    let tz = resolve_tz(tz_name);
    let from_ts = jiff::Timestamp::from_millisecond(from_ms).unwrap_or(jiff::Timestamp::UNIX_EPOCH);
    let to_ts = jiff::Timestamp::from_millisecond(to_ms).unwrap_or(jiff::Timestamp::UNIX_EPOCH);
    let from_d = from_ts.to_zoned(tz.clone()).datetime();
    let to_d = to_ts.to_zoned(tz).datetime();
    if from_d.date() == to_d.date() {
        format!(
            "{:04}-{:02}-{:02} {:02}:{:02} → {:02}:{:02}",
            from_d.year(),
            from_d.month(),
            from_d.day(),
            from_d.hour(),
            from_d.minute(),
            to_d.hour(),
            to_d.minute(),
        )
    } else {
        format!(
            "{:04}-{:02}-{:02} {:02}:{:02} → {:04}-{:02}-{:02} {:02}:{:02}",
            from_d.year(),
            from_d.month(),
            from_d.day(),
            from_d.hour(),
            from_d.minute(),
            to_d.year(),
            to_d.month(),
            to_d.day(),
            to_d.hour(),
            to_d.minute(),
        )
    }
}

/// Compute the trigger button's label using the *last-applied* draft
/// snapshot (so unapplied edits made inside an open popover don't
/// mislead a glance at the closed trigger).
///
/// Priority:
///   1. Preset whose (from_sql, to_sql) matches `(last_from, last_to)`
///      → return the preset label (more informative than dates).
///   2. Evaluated bounds present → render formatted wall-clock time.
///   3. Otherwise → truncated `"last_from → last_to"` SQL fallback.
fn compute_trigger_label(
    draft: &DraftState,
    presets: &[PresetEntry],
    evaluated: Option<(i64, i64)>,
) -> String {
    for p in presets {
        if p.from_sql == draft.last_from && p.to_sql == draft.last_to {
            return p.label.clone();
        }
    }
    if let Some((from_ms, to_ms)) = evaluated {
        return format_bounds(from_ms, to_ms, &draft.last_tz);
    }
    let from_short = compact(&draft.last_from, TRIGGER_EXPR_MAX);
    let to_short = compact(&draft.last_to, TRIGGER_EXPR_MAX);
    format!("{from_short} → {to_short}")
}

struct TimeRangePickerWidget<'a> {
    presets: Vec<PresetEntry>,
    initial_tz: String,
    refresh_interval_ms: Option<u32>,
    /// Latest evaluated bounds from Go (paired Some/Some or None).
    /// Drives the trigger button's wall-clock label when no preset
    /// matches the last-applied draft.
    evaluated_bounds: Option<(i64, i64)>,
    from_initial: String,
    to_initial: String,
    salt_id: Id,
    apply_signal: &'a mut Option<String>,
}

impl<'a> Widget for TimeRangePickerWidget<'a> {
    fn ui(self, ui: &mut Ui) -> Response {
        let mem_id = self.salt_id.with("time_range_picker_drafts");
        let mut draft =
            ui.memory_mut(|m| m.data.get_temp::<DraftState>(mem_id)).unwrap_or_else(|| {
                let today = default_today();
                DraftState {
                    from: self.from_initial.clone(),
                    to: self.to_initial.clone(),
                    tz: self.initial_tz.clone(),
                    from_date: today,
                    to_date: today,
                    from_h: 0,
                    from_m: 0,
                    from_s: 0,
                    to_h: 0,
                    to_m: 0,
                    to_s: 0,
                    last_from: self.from_initial.clone(),
                    last_to: self.to_initial.clone(),
                    last_tz: self.initial_tz.clone(),
                }
            });
        let salt_hex = format!("{:016x}", self.salt_id.value());
        let from_cal_salt = format!("{salt_hex}-from-cal");
        let to_cal_salt = format!("{salt_hex}-to-cal");
        let tz_combo_salt = format!("{salt_hex}-tz");

        let trigger_label = compute_trigger_label(&draft, &self.presets, self.evaluated_bounds);
        let trigger_text = format!("{trigger_label}  ·  {}  ▾", draft.last_tz);
        let mut btn_resp =
            ui.add(egui::Button::new(trigger_text).min_size(egui::vec2(TRIGGER_MIN_WIDTH, 0.0)));
        let popup_id = Popup::default_response_id(&btn_resp);

        let mut applied = false;
        let mut cancelled = false;

        Popup::from_toggle_button_response(&btn_resp)
            .close_behavior(PopupCloseBehavior::CloseOnClickOutside)
            .width(POPUP_WIDTH)
            .show(|ui| {
                ui.horizontal_top(|ui| {
                    // Left column: absolute time range + tz / refresh.
                    ui.vertical(|ui| {
                        ui.set_max_width(LEFT_COL_WIDTH);
                        ui.strong("Absolute time range");
                        ui.separator();
                        ui.label("From");
                        ui.add(
                            TextEdit::singleline(&mut draft.from).desired_width(TEXT_EDIT_WIDTH),
                        );
                        ui.horizontal(|ui| {
                            render_calendar_pop(
                                ui,
                                &mut draft.from,
                                &mut draft.from_date,
                                &mut draft.from_h,
                                &mut draft.from_m,
                                &mut draft.from_s,
                                &from_cal_salt,
                            );
                        });
                        ui.add_space(4.0);
                        ui.label("To");
                        ui.add(TextEdit::singleline(&mut draft.to).desired_width(TEXT_EDIT_WIDTH));
                        ui.horizontal(|ui| {
                            render_calendar_pop(
                                ui,
                                &mut draft.to,
                                &mut draft.to_date,
                                &mut draft.to_h,
                                &mut draft.to_m,
                                &mut draft.to_s,
                                &to_cal_salt,
                            );
                        });
                        ui.add_space(6.0);
                        ui.horizontal(|ui| {
                            if ui.button("Apply time range").clicked() {
                                applied = true;
                            }
                            if ui.button("Cancel").clicked() {
                                cancelled = true;
                            }
                        });
                        ui.add_space(8.0);
                        ui.separator();
                        ui.strong("Change time settings");
                        ui.horizontal(|ui| {
                            ui.label("tz:");
                            ComboBox::from_id_salt(&tz_combo_salt)
                                .selected_text(&draft.tz)
                                .show_ui(ui, |ui| {
                                    let mut shown = std::collections::HashSet::new();
                                    for cand in TZ_CANDIDATES {
                                        ui.selectable_value(
                                            &mut draft.tz,
                                            (*cand).to_string(),
                                            *cand,
                                        );
                                        shown.insert(*cand);
                                    }
                                    if !shown.contains(self.initial_tz.as_str()) {
                                        let cur = self.initial_tz.clone();
                                        ui.selectable_value(&mut draft.tz, cur.clone(), cur);
                                    }
                                });
                        });
                        if let Some(ms) = self.refresh_interval_ms {
                            ui.weak(format!("auto-refresh: {ms} ms"));
                        }
                    });
                    ui.separator();
                    // Right column: scrollable preset list.
                    ui.vertical(|ui| {
                        ui.set_max_width(RIGHT_COL_WIDTH);
                        ui.strong("Quick ranges");
                        ui.separator();
                        if self.presets.is_empty() {
                            ui.weak("(no presets configured)");
                        } else {
                            egui::ScrollArea::vertical().max_height(PRESET_LIST_MAX_HEIGHT).show(
                                ui,
                                |ui| {
                                    for p in &self.presets {
                                        let r = ui.add_sized(
                                            egui::vec2(RIGHT_COL_WIDTH, 0.0),
                                            egui::Button::new(&p.label).frame(false),
                                        );
                                        if r.clicked() {
                                            draft.from = p.from_sql.clone();
                                            draft.to = p.to_sql.clone();
                                            applied = true;
                                        }
                                    }
                                },
                            );
                        }
                    });
                });
            });

        if cancelled {
            draft.from = draft.last_from.clone();
            draft.to = draft.last_to.clone();
            draft.tz = draft.last_tz.clone();
        }
        if applied {
            *self.apply_signal = Some(format!(
                "{}{}{}{}{}",
                draft.tz, RANGE_DELIMITER, draft.from, RANGE_DELIMITER, draft.to,
            ));
            draft.last_from = draft.from.clone();
            draft.last_to = draft.to.clone();
            draft.last_tz = draft.tz.clone();
            btn_resp.mark_changed();
        }
        if applied || cancelled {
            Popup::close_id(ui.ctx(), popup_id);
        }
        ui.memory_mut(|m| m.data.insert_temp(mem_id, draft));
        btn_resp
    }
}

impl<'a, R: std::io::BufRead, W: std::io::Write> ImZeroFffi<'a, R, W> {
    pub fn apply_time_range_picker(
        &mut self,
        req: TimeRangePickerRequest,
        u: &mut Option<&mut egui::Ui>,
        f: &super::enums_out::FuncProcId,
        i: egui::Id,
        from_initial: String,
        to_initial: String,
    ) {
        let mut apply_signal: Option<String> = None;
        let initial_tz = req.tz.unwrap_or_else(|| "UTC".to_string());
        let evaluated_bounds = match (req.evaluated_from_ms, req.evaluated_to_ms) {
            (Some(f), Some(t)) => Some((f, t)),
            _ => None,
        };
        let widget = TimeRangePickerWidget {
            presets: req.presets,
            initial_tz,
            refresh_interval_ms: req.refresh_interval_ms,
            evaluated_bounds,
            from_initial,
            to_initial,
            salt_id: i,
            apply_signal: &mut apply_signal,
        };
        let _resp = self.apply_widget(widget, u, f, Some(i));
        if let Some(packed) = apply_signal {
            self.r9_s_push(i.value(), packed);
        }
    }
}
