// DatePickerButton wraps egui_extras::DatePickerButton (jiff::civil::Date
// based as of egui_extras 0.34). egui_extras requires `&'a mut Date` at
// construction; the FFFI2 codegen template emits
// `let mut w = USER_CONSTRUCTION_CODE;` and we need a Date local at the
// same scope as `w` so its borrow lives across `apply_widget`.
// Construction therefore emits a plain DatePickerButtonRequest
// accumulator, builder methods set its fields, and the apply step calls
// Interpreter::apply_date_picker_button which owns the Date local and
// forwards the actual egui_extras builder call to apply_widget.
//
// Wire format for the date itself is a packed `u64` of the form
// YYYY*10000 + MM*100 + DD (e.g. 20260425). The packed form survives
// the existing r9_u64 register pipeline unchanged, so the Go side can
// use the same SendRespVal databinding pattern as other interactive
// widgets.

use jiff::civil::Date;

use super::interpreter::ImZeroFffi;

#[derive(Default)]
pub struct DatePickerButtonRequest {
    pub format: Option<String>,
    pub highlight_weekends: Option<bool>,
    pub show_icon: Option<bool>,
    pub calendar: Option<bool>,
    pub calendar_week: Option<bool>,
    pub start_end_years: Option<(i16, i16)>,
    pub arrows: Option<bool>,
}

pub fn unpack_ymd(packed: u64) -> Date {
    let y = (packed / 10000) as i16;
    let m = ((packed / 100) % 100) as i8;
    let d = (packed % 100) as i8;
    Date::new(y, m, d).unwrap_or_else(|_| Date::new(1970, 1, 1).expect("epoch is valid"))
}

pub fn pack_ymd(date: Date) -> u64 {
    (date.year() as u64) * 10000 + (date.month() as u64) * 100 + (date.day() as u64)
}

impl<'a, R: std::io::BufRead, W: std::io::Write> ImZeroFffi<'a, R, W> {
    pub fn apply_date_picker_button(
        &mut self,
        req: DatePickerButtonRequest,
        u: &mut Option<&mut egui::Ui>,
        f: &super::enums_out::FuncProcId,
        i: egui::Id,
        packed_ymd: u64,
    ) {
        let mut date = unpack_ymd(packed_ymd);
        let salt = format!("{:016x}", i.value());
        let mut w = egui_extras::DatePickerButton::new(&mut date).id_salt(&salt);
        if let Some(s) = req.format {
            w = w.format(s);
        }
        if let Some(b) = req.highlight_weekends {
            w = w.highlight_weekends(b);
        }
        if let Some(b) = req.show_icon {
            w = w.show_icon(b);
        }
        if let Some(b) = req.calendar {
            w = w.calendar(b);
        }
        if let Some(b) = req.calendar_week {
            w = w.calendar_week(b);
        }
        if let Some((s, e)) = req.start_end_years {
            w = w.start_end_years(s..=e);
        }
        if let Some(b) = req.arrows {
            w = w.arrows(b);
        }
        let resp = self.apply_widget(w, u, f, Some(i));
        if let Some(r) = resp
            && r.changed()
        {
            self.r9_u64_push(i.value(), pack_ymd(date));
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn pack_unpack_roundtrip() {
        let d = Date::new(2026, 4, 25).expect("valid date");
        assert_eq!(pack_ymd(d), 20260425);
        assert_eq!(unpack_ymd(20260425), d);
    }

    #[test]
    fn unpack_invalid_falls_back_to_epoch() {
        let epoch = Date::new(1970, 1, 1).expect("epoch is valid");
        assert_eq!(unpack_ymd(0), epoch);
        assert_eq!(unpack_ymd(20260230), epoch); // feb 30 invalid
    }
}
