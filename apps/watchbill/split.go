package watchbill

import (
	"strconv"
	"strings"
)

// The split between the list and the detail is the app's, not egui's.
// egui's side panel stores the width it drew every frame; a window that
// shrinks clamps that stored width, and a window that grows back leaves it
// clamped — the split is lost on every resize. So the window keeps the
// width the user chose, tells the resize from the drag by whether the
// window's own width moved, re-asserts the kept width through an exact
// size for the frames after a resize, and persists it under a declared key
// so it survives the process as well.

const (
	// defaultSplit is the list's width before anyone drags it.
	defaultSplit = 640.0
	// minSplit and minDetail keep both panes reachable: a split narrower
	// than the first, or leaving the detail less than the second, is
	// clamped.
	minSplit  = 200.0
	minDetail = 320.0
	// reassertFrames is how many frames after a window resize the kept
	// width is forced: the panel's own report lags a frame, so one is not
	// enough to see the result of the first.
	reassertFrames = 2
	// splitKey is the persisted key (the manifest declares it).
	splitKey = "split"
)

// splitState is the state machine behind the split.
type splitState struct {
	// Width is the kept split, in panel width, as the user left it; the
	// drawn width is this clamped to the body.
	Width float32
	// bodyW and obsW are last frame's window body width and the panel's
	// observed inner width; ok says whether each has reported yet.
	bodyW, obsW   float32
	bodyOK, obsOK bool
	reassert      int
	dirty         bool
}

func newSplitState() (s splitState) {
	s = splitState{Width: defaultSplit}
	return
}

// frame takes this frame's reports — the body width the panels share and
// the panel's own inner width, each with whether it reported — and says
// whether the panel is to be drawn at an exact width this frame, and at
// what. A drag moves Width by the observed delta; a window resize does
// not, and instead arms the re-assertion.
func (inst *splitState) frame(bodyW float32, bodyOK bool, obsW float32, obsOK bool) (exact bool, width float32) {
	resized := bodyOK && inst.bodyOK && bodyW != inst.bodyW
	if resized {
		inst.reassert = reassertFrames
	}
	if obsOK && inst.obsOK && !resized && inst.reassert == 0 && obsW != inst.obsW {
		// The window stood still and the panel moved: the user's drag.
		inst.Width += obsW - inst.obsW
		inst.dirty = true
	}
	if bodyOK {
		inst.bodyW, inst.bodyOK = bodyW, true
	}
	if obsOK {
		inst.obsW, inst.obsOK = obsW, true
	}
	// The kept width is not clamped, only the drawn one: a window that
	// shrinks narrows the panel for as long as it is narrow, and one that
	// grows back finds the split where the user left it.
	width = inst.Width
	if inst.bodyOK {
		width = clampSplit(width, inst.bodyW)
	}
	if inst.reassert > 0 {
		inst.reassert--
		return true, width
	}
	return false, width
}

// clampSplit keeps both panes reachable in a body of bodyW.
func clampSplit(w float32, bodyW float32) (out float32) {
	out = w
	if bodyW > 0 && out > bodyW-minDetail {
		out = bodyW - minDetail
	}
	if out < minSplit {
		out = minSplit
	}
	return
}

// takeDirty reports and clears whether the width changed by a drag since
// the last call — what the persist write waits for.
func (inst *splitState) takeDirty() (dirty bool) {
	dirty, inst.dirty = inst.dirty, false
	return
}

// encodeSplit and decodeSplit are the stored spelling: the width as text.
func encodeSplit(w float32) (b []byte) {
	return []byte(strconv.FormatFloat(float64(w), 'f', 1, 32))
}

func decodeSplit(b []byte) (w float32, ok bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 32)
	if err != nil || f <= 0 {
		return 0, false
	}
	return float32(f), true
}
