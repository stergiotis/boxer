// Package waveform is the audio waveform player of ADR-0208: the waveform of
// a [track.Track] drawn on the painter lane with a playhead, click-to-seek,
// drag-to-pan, wheel and pinch zoom, a duration ruler and a hover readout.
// It imports track and nothing below it; every sample it draws comes from
// the track's peaks pyramid or its raw-window read.
//
// # Drawing (ADR-0208 SD2, SD3)
//
// At every zoom the widget picks one of two paths and there is no third. When
// a screen column covers at least one frame, each column is a min/max pair —
// from the pyramid when the column spans a base bin or more, reduced from a
// raw window otherwise — and a channel is one paintRectsFilled with a colour
// per column (progress colour up to the playhead, wave colour past it). When a
// column covers less than a frame the raw window is a polyline through the
// samples, with a marker on each once a sample is four pixels wide.
//
// # Input (ADR-0204 SD6, ADR-0140, ADR-0177)
//
// The canvas senses click and hover and captures the wheel; a sense region
// emitted last owns the drag. A drag pans from the press origin plus the
// offset, never by summing deltas. A click seeks. The wheel pans; pinch and
// Ctrl+wheel zoom about the hovered time. A key-capturing Frame around the
// canvas takes Space (play/pause), the arrows (seek; Shift for a longer
// step), Home/End and PageUp/PageDown (zoom); a press on the canvas gives
// it focus. Every register read is one frame behind the host, as for every
// painter widget.
//
// # View
//
// The view is a leftmost frame and a frames-per-pixel ratio, both float64 so
// a zoom anchored on the pointer stays put across many steps. It is clamped
// to the track: it never starts before frame 0, never runs past the end
// unless the whole track fits, and never zooms out past fitting the whole
// track or in past 64 pixels per sample.
package waveform
