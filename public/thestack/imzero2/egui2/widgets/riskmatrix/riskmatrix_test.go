package riskmatrix

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// register is three risks, the second without an inherent rating.
func register() *Model {
	nan := math.NaN()
	return &Model{
		Ids:              []string{"R1", "R2", "R3"},
		Names:            []string{"Ransomware", "", "Warehouse fire"},
		PLo:              []float64{0.03, 0.2, 0.002},
		PHi:              []float64{0.15, 0.6, 0.01},
		ImpactLo:         []float64{800e3, 20e3, 2e6},
		ImpactHi:         []float64{12e6, 400e3, 40e6},
		InherentPLo:      []float64{0.10, nan, 0.004},
		InherentPHi:      []float64{0.35, nan, 0.02},
		InherentImpactLo: []float64{1.5e6, nan, 3e6},
		InherentImpactHi: []float64{25e6, nan, 60e6},
	}
}

// One frame renders in either reading from a zero State.
func TestRendersAFrame(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	var st State
	res := Render(Input{Ids: ids, Model: register(), State: &st})
	require.NoError(t, res.Err)
	assert.EqualValues(t, defaultWidth, res.Width)
	assert.Equal(t, -1, res.Clicked)

	st.Mode, st.ShowInherent = ModeContinuous, true
	res = Render(Input{Ids: ids, ScopeKey: "narrow", Model: register(), State: &st, Width: 400, Height: 300})
	require.NoError(t, res.Err)
	assert.EqualValues(t, 400, res.Width)
}

// A matrix that cannot be drawn says why instead of drawing nothing.
func TestBrokenInputIsReported(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	var st State
	assert.Error(t, Render(Input{Model: register(), State: &st}).Err, "no id stack")
	assert.Error(t, Render(Input{Ids: ids, Model: register()}).Err, "no state")
	assert.Error(t, Render(Input{Ids: ids, Model: &Model{}, State: &st}).Err, "no risks")

	m := register()
	m.PLo[1] = 0
	assert.ErrorContains(t, Render(Input{Ids: ids, Model: m, State: &st}).Err, "R2")

	m = register()
	m.InherentPHi = nil
	assert.Error(t, m.Validate(), "inherent columns are all or none")

	assert.Error(t, Render(Input{Ids: ids, Model: register(), State: &st,
		Bands: Bands{LikelihoodEdges: []float64{0.1, 0.01}}}).Err, "descending edges")
}

// The default bands carry the geometric labels a register is read by.
func TestDefaultBandLabels(t *testing.T) {
	b, err := Bands{}.resolved()
	require.NoError(t, err)
	assert.Equal(t, []string{"< 1 in 100 yr", "1 in 30–100 yr", "1 in 10–30 yr", "1 in 3–10 yr", "> 1 in 3 yr"},
		b.likelihoodLabels())
	assert.Equal(t, []string{"< €100k", "€100k–500k", "€500k–2.5m", "€2.5m–12.5m", "> €12.5m"},
		b.impactLabels(FormatEuro))
	assert.Equal(t, []string{"< €3k", "€3k–20k", "€20k–100k", "€100k–500k", "> €500k"},
		b.shadeLabels(FormatEuro))

	b.LikelihoodLabels = []string{"rare", "unlikely", "possible", "likely", "almost certain"}
	assert.Equal(t, "rare", b.likelihoodLabels()[0], "labels of the right length replace the derived ones")
	b.ImpactLabels = []string{"too", "few"}
	assert.Equal(t, "< €100k", b.impactLabels(FormatEuro)[0], "labels of the wrong length are ignored")
}

// A cell's shade is the expected loss at its centre, so the grid darkens
// toward the high-risk corner and never by adding ranks.
func TestCellShadeIsExpectedLoss(t *testing.T) {
	b, err := Bands{}.resolved()
	require.NoError(t, err)
	assert.Equal(t, 0, b.cellShade(0, 0))
	assert.Equal(t, 4, b.cellShade(4, 4))
	for li := range 5 {
		for ii := range 4 {
			assert.LessOrEqual(t, b.cellShade(li, ii), b.cellShade(li, ii+1), "monotone in impact")
		}
	}
	// (likelihood 1 in 3–10, impact < €100k): 0.17 × 31.6k ≈ 5.5k a year.
	assert.Equal(t, 1, b.cellShade(3, 0))
}

func TestFormatCompact(t *testing.T) {
	for v, want := range map[float64]string{0: "0", 950: "950", 20e3: "20k", 2.5e6: "2.5m", 12.5e6: "12.5m", 3e9: "3bn", 1.25e3: "1.25k", 999.6e3: "1m", 0.1234: "0.123"} {
		assert.Equal(t, want, FormatCompact(v), "%v", v)
	}
	assert.Equal(t, "€500k–2.5m", amountBandLabel(500e3, 2.5e6, false, false, FormatEuro))
}

// A pointer over a dot comes back as its hit region's hover, a click on it
// as the region's click; the canvas itself is no longer read.
func TestHoverAndClickOnADot(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	var st State
	m := register()
	in := Input{Ids: ids, ScopeKey: "m", Model: m, State: &st}
	var dot widgethandle.WidgetHandle
	for range c.IdScope(ids.PrepareStr("m")) {
		for range c.IdScope(ids.PrepareStr("risks")) {
			dot = widgethandle.Make(ids.PrepareSeq(2).Derive())
		}
	}
	sm := c.CurrentApplicationState.StateManager
	sm.ScriptResponse(dot, c.HoveredResponseFlags|c.PrimaryClickedResponseFlags)
	res := Render(in)
	require.NoError(t, res.Err)
	assert.Equal(t, 2, res.Hovered)
	assert.Equal(t, 2, res.Clicked)

	sm.ScriptReset()
	res = Render(in)
	assert.Equal(t, -1, res.Hovered, "no region hovered, nothing hovered")
	assert.Equal(t, -1, res.Clicked)

	st.SetSelected(1)
	assert.Equal(t, 1, st.Selected())
	st.SetSelected(-1)
	assert.Equal(t, -1, st.Selected())
}

// Every dot and every cell carries the name a reader and a scene find it by.
func TestRegionsAreNamed(t *testing.T) {
	f := newFrame(Input{Model: register(), State: &State{}}, DefaultBands())
	var dots []string
	cells := 0
	for _, mk := range f.marks {
		assert.NotEmpty(t, mk.label)
		if mk.risk >= 0 {
			dots = append(dots, mk.label)
		} else {
			cells++
		}
	}
	assert.Equal(t, []string{"R1 · Ransomware", "R2", "R3 · Warehouse fire"}, dots)
	assert.Equal(t, 25, cells)
}
