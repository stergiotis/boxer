package axisruler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLabelAnchorsBottomMiddle: an interior tick on a bottom axis is centered
// under its mark, top-anchored.
func TestLabelAnchorsBottomMiddle(t *testing.T) {
	st := DefaultStyle()
	h, v := labelAnchors(SideBottom, 100, 0, 200, st)
	require.Equal(t, anchorCenter, h)
	require.Equal(t, anchorTop, v)
}

// TestLabelAnchorsBottomEnds: the first/last ticks anchor inward (left at the
// left end, right at the right end) so labels do not overhang the rect.
func TestLabelAnchorsBottomEnds(t *testing.T) {
	st := DefaultStyle()
	h, _ := labelAnchors(SideBottom, 2, 0, 200, st)
	require.Equal(t, anchorLeft, h, "tick near the left edge left-anchors")
	h, _ = labelAnchors(SideBottom, 198, 0, 200, st)
	require.Equal(t, anchorRight, h, "tick near the right edge right-anchors")
}

// TestLabelAnchorsBottomEdgeDisabled: with EdgeAnchor off, end ticks stay
// centered (the timeline's legacy behavior, preserved on retrofit).
func TestLabelAnchorsBottomEdgeDisabled(t *testing.T) {
	st := DefaultStyle()
	st.EdgeAnchor = false
	h, _ := labelAnchors(SideBottom, 2, 0, 200, st)
	require.Equal(t, anchorCenter, h)
	h, _ = labelAnchors(SideBottom, 198, 0, 200, st)
	require.Equal(t, anchorCenter, h)
}

// TestLabelAnchorsLeftMiddle: an interior tick on a left axis is right-anchored,
// vertically centered against its mark.
func TestLabelAnchorsLeftMiddle(t *testing.T) {
	st := DefaultStyle()
	h, v := labelAnchors(SideLeft, 100, 0, 200, st)
	require.Equal(t, anchorRight, h)
	require.Equal(t, anchorVCenter, v)
}

// TestLabelAnchorsLeftEnds: the top/bottom ticks anchor inward so a label at the
// gutter's top/bottom edge does not clip vertically; horizontal stays right.
func TestLabelAnchorsLeftEnds(t *testing.T) {
	st := DefaultStyle()
	h, v := labelAnchors(SideLeft, 3, 0, 200, st)
	require.Equal(t, anchorRight, h)
	require.Equal(t, anchorTop, v, "tick near the top edge top-anchors")
	_, v = labelAnchors(SideLeft, 197, 0, 200, st)
	require.Equal(t, anchorBottom, v, "tick near the bottom edge bottom-anchors")
}

// TestEdgePadBoundary: EdgePad is the inclusive-exclusive threshold — a tick
// exactly EdgePad from the end is still centered.
func TestEdgePadBoundary(t *testing.T) {
	st := DefaultStyle() // EdgePad 12
	h, _ := labelAnchors(SideBottom, 12, 0, 200, st)
	require.Equal(t, anchorCenter, h, "exactly EdgePad away is not an end")
	h, _ = labelAnchors(SideBottom, 11.9, 0, 200, st)
	require.Equal(t, anchorLeft, h, "just inside EdgePad anchors left")
}
