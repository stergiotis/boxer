// Package timescrubber is a time strip for a stepped series — forecast
// steps, snapshots, frames — drawn on the painter lane (ADR-0251): a control
// that shows where in time there is something to look at, and what moving
// there will cost, before the reader moves.
//
//	sc := timescrubber.New(ids, timescrubber.Options{ValueName: "speed", ValueUnit: "m/s"})
//	// every frame:
//	ev := sc.RenderFillWidth(steps, 960)
//	layer.SetStepPosition(sc.Transport.Pos)
//	layer.SetAhead(sc.Transport.Ahead(len(steps), 4, ahead)) // fetch before the playhead arrives
//	if ev.Settled {
//		query(ev.Step) // the position came to rest; a drag's every frame did not
//	}
//
// The caller hands it the steps as it knows them this frame — each step's
// instant, what its data is doing, optionally a value and a peak — and reads
// the position back as a fractional step index. The widget knows nothing of
// what the steps are steps of, and asks for nothing: hover, the snap preview
// and the drawing read what was handed in, so showing the strip never costs a
// fetch.
//
// # What it draws
//
// Steps sit at their own instants on a calendar axis, so a series that is
// hourly and then three-hourly looks it; an index slider draws that series
// evenly and misstates how fast time passes under the thumb. [Options.ByIndex]
// is the other choice, for a series whose dense block would otherwise be
// squeezed: every step as wide as the next, and the change of cadence no
// longer shown.
//
// Each step has a bar up to its value and a stem to its peak, scaled to the
// largest value — a peak past the top is drawn clipped, because a series whose
// gusts run well above its means would otherwise have its bars in the bottom
// of the strip. What a value is in absolute terms is the colour's to say,
// through [Scrubber.ValueColor], and the hover readout's. Because the colours
// are the data's, a step's state is told by shape: filled for held, hollow for
// loading, a cross for missing, a short tick for idle. Where steps outnumber
// pixel columns a column draws the largest value among its steps and the worst
// of their states; nothing is subsampled, so the one step a reader is scanning
// for is still there. A series that spans the wall clock is tinted before it
// and gets a line at now; days alternate in shade; [Scrubber.Marks] are drawn
// in the label row.
//
// # Input
//
// A drag that starts on the strip moves the playhead, marks the step a release
// would settle on and reads out that step's value; a click seeks. The band
// along the top belongs to the loop range alone — a drag there sets it, an
// edge or the body changes it, a double click clears it, Escape puts back what
// was there, and a click does not seek, so the double click collides with
// nothing. The press origin decides which gesture it is, not where the pointer
// had got to when the drag was recognised. In and Out set an end of the range
// at the playhead, which is the route that needs no dragging.
//
// Under focus the strip takes Space, the arrows (Shift for a stride, Ctrl for
// a day, Alt for a mark), PageUp and PageDown, Home and End, Shift+Home and
// Shift+End for the range's ends, Delete to clear it (ADR-0177). Every
// register read is one frame behind the host, as for every painter widget.
//
// # Playback
//
// [Transport] holds the position and the rules that move it, reads no clock
// and draws nothing. Playback enters the bracket between two steps only when
// both are there, and otherwise waits just inside it: on the far side of the
// step, so that whoever loads data for the position sees the bracket wanted,
// and close enough to it that the picture is the step's. It does not wait for
// a step the caller has declared missing, and it holds the last step for
// [Transport.Dwell] before a loop wraps or a bounce turns.
//
// [Transport.Ahead] lists the steps playback will reach next, in the order it
// will reach them, walking the same rules [Transport.Advance] does — so a
// loader fetching ahead and the playhead cannot disagree about where it is
// going.
//
// # Two clocks
//
// [Options.Now] is the wall clock: where now stands on the axis, and what an
// offset is measured from. It does not pace playback, which always runs on
// elapsed real time — a capture that fixes the wall clock to get the same
// picture every run still plays.
package timescrubber
