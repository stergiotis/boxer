package bindings

// Hand-written helpers for the generated DatePickerButton opcode (see
// definition/egui2_definition_d_widgets.go and rust/src/imzero2/
// date_picker_button.rs). The wire format for the date is a packed
// uint64 of the form YYYY*10000 + MM*100 + DD (e.g. 20260425), routed
// through the standard r9_u64 register so the same SendRespVal +
// AddR9U64Databinding pipeline used by SliderF64 / DragValueU64 carries
// the value back to Go with the project's usual one-frame lag.

// PackDateYmd packs a Gregorian (year, month, day) triple into the
// canonical YYYYMMDD uint64 used as the DatePickerButton wire format.
// Inputs are not validated — pass values inside their natural range
// (year 1..=9999, month 1..=12, day 1..=31). Out-of-range or
// non-existent dates (e.g. Feb 30) decode to 1970-01-01 on the Rust
// side per date_picker_button::unpack_ymd.
func PackDateYmd(year, month, day int) uint64 {
	return uint64(year)*10000 + uint64(month)*100 + uint64(day)
}

// UnpackDateYmd splits a packed YYYYMMDD value back into (year, month,
// day). Mirrors PackDateYmd; round-trips for any value PackDateYmd can
// produce.
func UnpackDateYmd(packed uint64) (year, month, day int) {
	year = int(packed / 10000)
	month = int((packed / 100) % 100)
	day = int(packed % 100)
	return
}

// SendRespVal flushes the DatePickerButton opcode and registers an
// r9_u64 databinding so the next StateManager.Sync() writes the
// user-picked date back into *val (packed YYYYMMDD). Returns the
// widget's response flags. FFFI databindings reset each Sync; callers
// must call SendRespVal every frame for the binding to remain live.
//
// Per the project's standard one-frame lag, the value visible at *val
// reflects the user's pick from the previous frame.
func (inst DatePickerButtonFluid) SendRespVal(val *uint64) ResponseFlagsE {
	inst.Send()
	s := CurrentApplicationState.StateManager
	id := inst.id
	s.AddR9U64Databinding(id, val)
	return s.GetResponseByIdRaw(id)
}
