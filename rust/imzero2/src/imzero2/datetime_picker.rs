// DateTimePickerButton wraps egui_extras::DatePickerButton plus three
// egui::DragValue widgets (h:m:s) into a horizontal compound widget.
// egui_extras::DatePickerButton requires `&'a mut Date` at construction;
// the FFFI2 codegen template puts the construction code on the RHS of
// `let mut w = ...`, leaving no outer-scope room for a Date local. So
// construction emits a plain DateTimePickerButtonRequest accumulator,
// builder methods set its fields, and apply hands it to the
// hand-written self.apply_date_time_picker_button which owns the Date,
// hour, minute and second locals across self.apply_widget() and pushes
// a packed u64 (bits of i64 epoch milliseconds, UTC) back via r9_u64
// when any sub-widget reports .changed().
//
// Wire format: a u64 register carrying the bit pattern of an i64
// representing milliseconds since the Unix epoch (UTC). Phase 1 of
// ADR-0016's port (see doc/howto/imzero2-time-range-picker-port.md)
// intentionally reuses the r9_u64 channel rather than adding I64
// plumbing through StateManager and the Fetcher; the int64 epoch-ms
// semantics are preserved via the Go-side helpers PackDateTimeUtc /
// UnpackDateTimeUtc.

use jiff::Timestamp;
use jiff::civil::Date;
use jiff::tz::TimeZone;

use super::interpreter::ImZeroFffi;

#[derive(Default)]
pub struct DateTimePickerButtonRequest {
    pub format: Option<String>,
    pub highlight_weekends: Option<bool>,
    pub show_icon: Option<bool>,
    pub calendar: Option<bool>,
    pub calendar_week: Option<bool>,
    pub start_end_years: Option<(i16, i16)>,
    pub arrows: Option<bool>,
}

/// Pack a (date, hour, minute, second) tuple into the wire u64. The
/// u64 carries the bit pattern of an i64 = milliseconds since the
/// Unix epoch (UTC). On any construction error, returns the bits of
/// 0 (Unix epoch). Hour, minute and second are clamped to their
/// natural ranges before packing.
pub fn pack_dt(date: Date, hour: u8, minute: u8, second: u8) -> u64 {
    let h = (hour.min(23)) as i8;
    let m = (minute.min(59)) as i8;
    let s = (second.min(59)) as i8;
    let dt = date.at(h, m, s, 0);
    match dt.to_zoned(TimeZone::UTC) {
        Ok(z) => z.timestamp().as_millisecond() as u64,
        Err(_) => 0,
    }
}

/// Inverse of pack_dt. On any decode error, falls back to the Unix
/// epoch in UTC, mirroring date_picker_button::unpack_ymd so a zero
/// wire value renders as 1970-01-01 00:00:00 UTC.
pub fn unpack_dt(packed: u64) -> (Date, u8, u8, u8) {
    let ms = packed as i64;
    let ts = Timestamp::from_millisecond(ms).unwrap_or(Timestamp::UNIX_EPOCH);
    let dt = ts.to_zoned(TimeZone::UTC).datetime();
    (
        dt.date(),
        dt.hour() as u8,
        dt.minute() as u8,
        dt.second() as u8,
    )
}

struct DateTimePickerWidget<'a> {
    date: &'a mut Date,
    hour: &'a mut u8,
    minute: &'a mut u8,
    second: &'a mut u8,
    req: DateTimePickerButtonRequest,
    salt: String,
}

impl<'a> egui::Widget for DateTimePickerWidget<'a> {
    fn ui(self, ui: &mut egui::Ui) -> egui::Response {
        ui.horizontal(|ui| {
            let mut dpb = egui_extras::DatePickerButton::new(self.date).id_salt(&self.salt);
            if let Some(s) = self.req.format {
                dpb = dpb.format(s);
            }
            if let Some(b) = self.req.highlight_weekends {
                dpb = dpb.highlight_weekends(b);
            }
            if let Some(b) = self.req.show_icon {
                dpb = dpb.show_icon(b);
            }
            if let Some(b) = self.req.calendar {
                dpb = dpb.calendar(b);
            }
            if let Some(b) = self.req.calendar_week {
                dpb = dpb.calendar_week(b);
            }
            if let Some((s, e)) = self.req.start_end_years {
                dpb = dpb.start_end_years(s..=e);
            }
            if let Some(b) = self.req.arrows {
                dpb = dpb.arrows(b);
            }
            let r_d = ui.add(dpb);
            let r_h = ui.add(egui::DragValue::new(self.hour).range(0u8..=23u8).suffix("h"));
            let r_m = ui.add(egui::DragValue::new(self.minute).range(0u8..=59u8).suffix("m"));
            let r_s = ui.add(egui::DragValue::new(self.second).range(0u8..=59u8).suffix("s"));
            r_d | r_h | r_m | r_s
        })
        .inner
    }
}

impl<'a, R: std::io::BufRead, W: std::io::Write> ImZeroFffi<'a, R, W> {
    pub fn apply_date_time_picker_button(
        &mut self,
        req: DateTimePickerButtonRequest,
        u: &mut Option<&mut egui::Ui>,
        f: &super::enums_out::FuncProcId,
        i: egui::Id,
        packed_epoch_ms: u64,
    ) {
        let (mut date, mut hour, mut minute, mut second) = unpack_dt(packed_epoch_ms);
        let salt = format!("{:016x}", i.value());
        let widget = DateTimePickerWidget {
            date: &mut date,
            hour: &mut hour,
            minute: &mut minute,
            second: &mut second,
            req,
            salt,
        };
        let resp = self.apply_widget(widget, u, f, Some(i));
        if let Some(r) = resp
            && r.changed()
        {
            self.r9_u64_push(i.value(), pack_dt(date, hour, minute, second));
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn pack_unpack_roundtrip_present() {
        let d = Date::new(2026, 4, 25).expect("valid date");
        let packed = pack_dt(d, 14, 30, 45);
        let (d2, h, m, s) = unpack_dt(packed);
        assert_eq!(d, d2);
        assert_eq!((h, m, s), (14, 30, 45));
    }

    #[test]
    fn unpack_zero_is_epoch_midnight() {
        let (d, h, m, s) = unpack_dt(0);
        assert_eq!(d, Date::new(1970, 1, 1).expect("epoch is valid"));
        assert_eq!((h, m, s), (0, 0, 0));
    }

    #[test]
    fn pack_clamps_out_of_range_time() {
        let d = Date::new(2026, 1, 1).expect("valid");
        let packed = pack_dt(d, 24, 60, 60);
        let (d2, h, m, s) = unpack_dt(packed);
        assert_eq!(d, d2);
        assert_eq!((h, m, s), (23, 59, 59));
    }

    #[test]
    fn pre_epoch_round_trip() {
        // 1969-12-31 23:59:59 UTC = -1000 ms
        let d = Date::new(1969, 12, 31).expect("valid");
        let packed = pack_dt(d, 23, 59, 59);
        assert_eq!(packed as i64, -1000);
        let (d2, h, m, s) = unpack_dt(packed);
        assert_eq!(d, d2);
        assert_eq!((h, m, s), (23, 59, 59));
    }
}
