package bindings

// Hand-written helpers for the generated TimeRangePicker opcode.
// See definition/egui2_definition_d_widgets.go and
// rust/src/imzero2/time_range_picker.rs.
//
// Wire format (Phase 3 of ADR-0016): the picker emits a single r9_s
// payload of the form `from_expression\x1eto_expression`. Use the
// PackRange / UnpackRange helpers in the timerangepicker package to
// translate between the wire string and the (from, to) pair. The
// demo applies the unpacked expressions to the evaluator (Phase 2)
// to resolve concrete epoch-millisecond bounds.

// SendRespVal flushes the TimeRangePicker opcode and registers an
// r9_s databinding so the next StateManager.Sync() writes the
// user-applied range string back into *val (packed as
// `from\x1eto`). Returns the widget's response flags.
//
// FFFI databindings reset each Sync; callers must call SendRespVal
// every frame for the binding to remain live. The string at *val
// reflects the user's pick from the previous frame, per the
// project's standard one-frame lag.
func (inst TimeRangePickerFluid) SendRespVal(val *string) ResponseFlagsE {
	inst.Send()
	s := CurrentApplicationState.StateManager
	id := inst.id
	s.AddR9SDatabinding(id, val)
	return s.GetResponseByIdRaw(id)
}
