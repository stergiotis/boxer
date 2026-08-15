package implot

import "testing"

// The axis scale is Setup state: it holds for the frame that declares it, and
// an axis nobody re-declares is linear again. A plot whose caller offers a log
// toggle depends on exactly this — the caller states the CHOICE each frame and
// never has to state the change, so unchecking the toggle is simply a frame
// that says nothing.
//
// Before this, scale sat in the retained half of the state with no other
// writer, so the last SetupAxisScale stood forever: play's Chart panel put its
// checkbox back to unchecked and kept drawing a log axis.
func TestAxisScaleIsPerFrameSetupState(t *testing.T) {
	st := &plotState{}
	st.x.scale, st.y.scale = ScaleTime, ScaleLog10

	st.beginFrame()

	if st.x.scale != ScaleLinear {
		t.Errorf("x scale = %v after a frame that did not declare it, want %v", st.x.scale, ScaleLinear)
	}
	if st.y.scale != ScaleLinear {
		t.Errorf("y scale = %v after a frame that did not declare it, want %v", st.y.scale, ScaleLinear)
	}
}

// The other half of the same rule: the reset is scoped to Setup state and must
// not touch the interaction state a plot carries BETWEEN frames. A pan that
// survived only until the next frame would be no pan at all.
func TestBeginFrameKeepsRetainedInteractionState(t *testing.T) {
	st := &plotState{hidden: map[string]bool{"errors": true}}
	st.x.rng, st.y.rng = Range{2, 40}, Range{1, 1000}
	st.x.hasRange, st.y.hasRange = true, true
	st.y.touched = true
	st.initialized = true
	st.legendHover = "errors"

	st.beginFrame()

	if st.x.rng != (Range{2, 40}) || st.y.rng != (Range{1, 1000}) {
		t.Errorf("ranges = %v / %v, want them carried across the frame", st.x.rng, st.y.rng)
	}
	if !st.x.hasRange || !st.y.hasRange || !st.initialized {
		t.Error("range/initialized bookkeeping was reset with the Setup state")
	}
	if !st.y.touched {
		t.Error("a gesture's touched mark was reset, which would resume auto-fit under the reader")
	}
	if !st.hidden["errors"] || st.legendHover != "errors" {
		t.Error("legend state was reset with the Setup state")
	}
}
