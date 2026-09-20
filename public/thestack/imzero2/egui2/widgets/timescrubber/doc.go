// Package timescrubber is a time strip for a stepped series — forecast
// steps, snapshots, frames — drawn on the painter lane (ADR-0251): a control
// that shows where in time there is something to look at, and what moving
// there will cost, before the reader moves.
//
//	sc := timescrubber.New(ids, timescrubber.Options{ValueName: "speed", ValueUnit: "m/s"})
//	// every frame:
//	sc.RenderFillWidth(steps, 960)
//	layer.SetStepPosition(sc.Transport.Pos)
//
// The caller hands it the steps as it knows them this frame — each step's
// instant, what its data is doing, optionally a value and a peak — and reads
// the position back as a fractional step index. The widget knows nothing of
// what the steps are steps of.
//
// # What it draws
//
// Steps sit at their own instants on a calendar axis, so a series that is
// hourly and then three-hourly looks it; an index slider draws that series
// evenly and misstates how fast time passes under the thumb. Each step has a
// notch coloured by its state, a bar up to its value and a stem from there to
// its peak. Heights are scaled to the series and say where it is strong; what
// a value is in absolute terms is the colour's to say, through
// Scrubber.ValueColor, and the hover readout's. A series that spans the wall
// clock gets a line at now.
//
// # Input
//
// A drag that starts on the strip moves the playhead continuously and lets go
// of it on the nearest step; a click seeks to a step. A drag that starts on
// the band along the top sets the range playback is limited to, and a double
// click clears it. The press origin decides which, not where the pointer had
// got to when the drag was recognised. Under focus the strip takes Space, the
// arrows (Shift for a longer stride), Home and End (ADR-0177). Every register
// read is one frame behind the host, as for every painter widget.
//
// # Playback
//
// [Transport] holds the position and the rules that move it, reads no clock
// and draws nothing. Playback enters the bracket between two steps only when
// both are there, and otherwise waits just inside it: on the far side of the
// step, so that whoever loads data for the position sees the bracket wanted,
// and close enough to it that the picture is the step's.
package timescrubber
