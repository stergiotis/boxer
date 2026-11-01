package impacttolerance

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// services is four services under three scenarios, one estimate missing.
func services() *Model {
	return &Model{
		Services:       []string{"Order intake", "Warehouse dispatch", "Payroll", "Customer support"},
		ToleranceHours: []float64{24, 12, 72, 48},
		Scenarios:      []string{"Ransomware", "Cloud region outage", "Key supplier failure"},
		RecoveryHours: []float64{
			72, 10, 4,
			48, 6, 30,
			60, 8, math.NaN(),
			30, 20, 1,
		},
	}
}

// One frame renders from a zero State, its height following the rows.
func TestRendersAFrame(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	var st State
	res := Render(Input{Ids: ids, Model: services(), State: &st})
	require.NoError(t, res.Err)
	assert.EqualValues(t, defaultWidth, res.Width)
	assert.Greater(t, res.Height, 4*defaultRowHeight)
	assert.Equal(t, NoPoint, res.Clicked)

	narrow := Render(Input{Ids: ids, ScopeKey: "narrow", Model: services(), State: &st, Width: 360, HideLegend: true})
	require.NoError(t, narrow.Err)
	assert.Less(t, narrow.Height, res.Height, "no legend row")
}

// A chart that cannot be drawn says why instead of drawing nothing.
func TestBrokenInputIsReported(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	var st State
	assert.Error(t, Render(Input{Model: services(), State: &st}).Err, "no id stack")
	assert.Error(t, Render(Input{Ids: ids, Model: services()}).Err, "no state")
	assert.Error(t, Render(Input{Ids: ids, Model: &Model{}, State: &st}).Err, "no services")

	m := services()
	m.RecoveryHours = m.RecoveryHours[:5]
	assert.Error(t, Render(Input{Ids: ids, Model: m, State: &st}).Err, "a ragged matrix")

	m = services()
	m.ToleranceHours[2] = 0
	assert.ErrorContains(t, Render(Input{Ids: ids, Model: m, State: &st}).Err, "Payroll")

	assert.Error(t, Render(Input{Ids: ids, Model: services(), State: &st, MinHours: 10, MaxHours: 5}).Err)
}

// The breach count is per service and leaves out a scenario with no estimate.
func TestBreaches(t *testing.T) {
	m := services()
	over, of := m.breaches(0)
	assert.Equal(t, [2]int{1, 3}, [2]int{over, of})
	over, of = m.breaches(1)
	assert.Equal(t, [2]int{2, 3}, [2]int{over, of})
	over, of = m.breaches(2)
	assert.Equal(t, [2]int{0, 2}, [2]int{over, of})
	assert.Equal(t, "all within", breachText(0, 2))
	assert.Equal(t, "2 of 3 breach", breachText(2, 3))
}

// The axis runs from a duration rung at or below the data to one well past it.
func TestAxisRange(t *testing.T) {
	lo, hi := axisRange(1, 72)
	assert.Equal(t, [2]float64{1, 336}, [2]float64{lo, hi})
	lo, hi = axisRange(1.0/7200, 20000)
	assert.Equal(t, [2]float64{1.0 / 7200, 20000}, [2]float64{lo, hi}, "data past the ladder widens it")
	lo, _ = axisRange(0.1, 1)
	assert.Equal(t, 5.0/60, lo, "the rungs reach below an hour, for a column that declares minutes or seconds")
	assert.Equal(t, "30 s", FormatHours(30.0/3600))
	assert.Equal(t, "1 min", FormatHours(1.0/60))
	assert.Equal(t, "15 min", FormatHours(0.25))
	assert.Equal(t, "4 h", FormatHours(4))
	assert.Equal(t, "3 d", FormatHours(72))
	assert.Equal(t, "1.5 d", FormatHours(36))
}

// A pointer over an estimate comes back as its hit region's hover, a click
// on it as the region's click.
func TestHoverAndClickOnAPoint(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	var st State
	in := Input{Ids: ids, ScopeKey: "t", Model: services(), State: &st}
	var pt widgethandle.WidgetHandle
	for range c.IdScope(ids.PrepareStr("t")) {
		for range c.IdScope(ids.PrepareStr("estimates")) {
			for range c.IdScope(ids.PrepareSeq(1)) {
				pt = widgethandle.Make(ids.PrepareSeq(2).Derive())
			}
		}
	}
	sm := c.CurrentApplicationState.StateManager
	sm.ScriptResponse(pt, c.HoveredResponseFlags|c.PrimaryClickedResponseFlags)
	res := Render(in)
	require.NoError(t, res.Err)
	assert.Equal(t, Point{Service: 1, Scenario: 2}, res.Hovered)
	assert.Equal(t, Point{Service: 1, Scenario: 2}, res.Clicked)

	sm.ScriptReset()
	res = Render(in)
	assert.Equal(t, NoPoint, res.Hovered)
	assert.Equal(t, NoPoint, res.Clicked)

	st.SetSelected(Point{Service: 1, Scenario: 2})
	assert.Equal(t, Point{Service: 1, Scenario: 2}, st.Selected())
	st.SetSelected(NoPoint)
	assert.Equal(t, NoPoint, st.Selected())
}

// Every estimate and tolerance line carries the name a reader and a scene
// find it by; the missing estimate declares nothing.
func TestRegionsAreNamed(t *testing.T) {
	f := newFrame(Input{Model: services(), State: &State{}})
	var labels []string
	for _, mk := range f.marks {
		labels = append(labels, mk.label)
	}
	assert.Len(t, labels, 4+11)
	assert.Contains(t, labels, "Warehouse dispatch · Key supplier failure")
	assert.Contains(t, labels, "Payroll · tolerance")
	assert.NotContains(t, labels, "Payroll · Key supplier failure")
}
