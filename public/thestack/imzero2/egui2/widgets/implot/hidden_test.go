package implot

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// A label hidden through the API drops its series from the frame, as a
// click on its legend row does, and the flag is retained across frames.
func TestSetSeriesHiddenDropsSeries(t *testing.T) {
	messages, zero, reset := scenetest.InstallCounting()
	t.Cleanup(reset)
	ids := c.NewWidgetIdStack()
	xs := []float64{0, 1, 2}
	frame := func(set func(p *Plot)) (int, *Plot) {
		reset()
		zero()
		p := Begin(ids, "##hidden-test", 400, 300)
		p.SetupAxisLimits(AxisX1, 0, 2, CondAlways).SetupAxisLimits(AxisY1, 0, 2, CondAlways).NoLegend()
		set(p)
		p.Line("a", xs, xs)
		p.Line("b", xs, xs)
		p.End()
		return messages(), p
	}
	shown, p := frame(func(*Plot) {})
	t.Cleanup(func() { Release(p.ScopeId()) })
	hidden, p := frame(func(p *Plot) { p.SetSeriesHidden("b", true) })
	if hidden != shown-1 {
		t.Errorf("hiding b sent %d messages against %d shown, want one fewer", hidden, shown)
	}
	if !p.SeriesHidden("b") || p.SeriesHidden("a") {
		t.Errorf("hidden = a:%v b:%v, want b only", p.SeriesHidden("a"), p.SeriesHidden("b"))
	}
	retained, p := frame(func(*Plot) {})
	if retained != hidden || !p.SeriesHidden("b") {
		t.Errorf("the next frame sent %d messages (b hidden: %v), want %d and still hidden", retained, p.SeriesHidden("b"), hidden)
	}
	p.SetSeriesHidden("b", false)
	if len(p.st.hidden) != 0 {
		t.Errorf("showing b left %v in the hidden set", p.st.hidden)
	}
}
