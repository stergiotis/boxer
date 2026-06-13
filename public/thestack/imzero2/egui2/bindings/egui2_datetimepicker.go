package bindings

import (
	"time"
)

// Hand-written helpers for the generated DateTimePickerButton opcode
// (see definition/egui2_definition_d_widgets.go and rust/src/imzero2/
// datetime_picker.rs). The wire format is a uint64 holding the bit
// pattern of an int64 representing milliseconds since the Unix epoch
// (UTC). Phase 1 of ADR-0016's port (doc/howto/imzero2-time-range-
// picker-port.md) intentionally reuses the r9_u64 register rather
// than adding I64 plumbing through StateManager + Fetcher; the int64
// epoch-ms semantics are preserved via PackDateTimeUtc /
// UnpackDateTimeUtc.

// PackDateTimeUtc converts a time.Time to the canonical wire uint64
// (bits of int64 milliseconds since the Unix epoch, in UTC). The
// time.Time is internally converted to UTC; sub-second components
// truncate to milliseconds.
func PackDateTimeUtc(t time.Time) uint64 {
	return uint64(t.UTC().UnixMilli())
}

// UnpackDateTimeUtc inverts PackDateTimeUtc. The returned time.Time
// is in UTC.
func UnpackDateTimeUtc(packed uint64) time.Time {
	return time.UnixMilli(int64(packed)).UTC()
}

// SendRespVal flushes the DateTimePickerButton opcode and registers
// an r9_u64 databinding so the next StateManager.Sync() writes the
// user-picked instant back into *val (packed as PackDateTimeUtc).
// Returns the widget's response flags. FFFI databindings reset each
// Sync; callers must call SendRespVal every frame for the binding
// to remain live.
//
// The value visible at *val reflects the user's pick from the
// previous frame, per the project's standard one-frame lag.
func (inst DateTimePickerButtonFluid) SendRespVal(val *uint64) ResponseFlagsE {
	inst.Send()
	s := CurrentApplicationState.StateManager
	id := inst.id
	s.AddR9U64Databinding(id, val)
	return s.GetResponseByIdRaw(id)
}
