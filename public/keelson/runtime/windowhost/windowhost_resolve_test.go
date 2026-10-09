package windowhost

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/inscribe"
)

func geom(key WindowKeyT, x, y, w, h float32, stack uint32) GeomEntry {
	return GeomEntry{Key: key, Geom: WindowGeom{Shown: true, Rect: Rect{MinX: x, MinY: y, MaxX: x + w, MaxY: y + h}, Stack: stack}}
}

func TestResolveFollowsTheWindowAndSaysHowItStands(t *testing.T) {
	local := &inscribe.Rect{X: 10, Y: 20, W: 30, H: 10}
	geoms := []GeomEntry{geom(1, 100, 50, 400, 300, 1), geom(2, 600, 50, 200, 200, 2)}

	r, v := Resolve(inscribe.Anchor{Window: 1, Local: local}, geoms)
	assert.Equal(t, inscribe.Rect{X: 110, Y: 70, W: 30, H: 10}, r)
	assert.Equal(t, inscribe.VisibilityShown, v)

	// The window moves; the mark goes with it.
	geoms[0] = geom(1, 0, 0, 400, 300, 1)
	r, _ = Resolve(inscribe.Anchor{Window: 1, Local: local}, geoms)
	assert.Equal(t, inscribe.Rect{X: 10, Y: 20, W: 30, H: 10}, r)

	// A window ranked further front comes over the target.
	geoms[1] = geom(2, 0, 0, 200, 200, 2)
	_, v = Resolve(inscribe.Anchor{Window: 1, Local: local}, geoms)
	assert.Equal(t, inscribe.VisibilityBehind, v)
	_, v = Resolve(inscribe.Anchor{Window: 2}, geoms)
	assert.Equal(t, inscribe.VisibilityShown, v, "the front window is not behind itself")

	collapsed := geoms[0]
	collapsed.Geom.Collapsed = true
	collapsed.Geom.Rect.MaxY = 24
	r, v = Resolve(inscribe.Anchor{Window: 1, Local: local}, []GeomEntry{collapsed})
	assert.Equal(t, inscribe.VisibilityCollapsed, v)
	assert.Equal(t, inscribe.Rect{X: 0, Y: 0, W: 400, H: 24}, r, "a collapsed window's title bar")

	_, v = Resolve(inscribe.Anchor{Window: 3}, geoms)
	assert.Equal(t, inscribe.VisibilityGone, v)
	_, v = Resolve(inscribe.Anchor{Window: 4}, []GeomEntry{{Key: 4}})
	assert.Equal(t, inscribe.VisibilityPending, v)

	vp := &inscribe.Rect{X: 5, Y: 5, W: 10, H: 10}
	r, v = Resolve(inscribe.Anchor{Viewport: vp}, nil)
	assert.Equal(t, *vp, r)
	assert.Equal(t, inscribe.VisibilityShown, v)
}
