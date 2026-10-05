package runtimestatus

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stretchr/testify/assert"
)

// TestRenderNilSnapshotNoPanic covers the nil-safety guard: no snapshot,
// nothing drawn, nothing reported.
func TestRenderNilSnapshotNoPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		res := Render(Input{})
		assert.Equal(t, "", res.Clicked)
	})
}

// TestRenderOneFrameHeadless draws a zero-value Snapshot (every backend
// inactive) plain and clickable under the discard channel (ADR-0267 W19).
func TestRenderOneFrameHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	s := &Snapshot{}
	assert.NotPanics(t, func() {
		_ = Render(Input{Snapshot: s})
		res := Render(Input{Ids: c.NewWidgetIdStack(), ScopeKey: "t", Snapshot: s, Clickable: true})
		assert.Equal(t, "", res.Clicked, "nothing is clicked without input")
	})
}
