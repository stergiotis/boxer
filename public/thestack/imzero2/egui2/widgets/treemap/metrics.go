package treemap

import (
	"hash/fnv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Measured label-height gates. A cell's Frame sizes to its content with only
// a minimum (UiSetMinWidth/UiSetMinHeight), and the allocated cell Ui is not
// clipped, so a label block taller than the cell's content box grows the
// Frame past the cell rect and paints over whatever lies below — the parent's
// bottom inset, the parent's border, ultimately the container edge. Squarify
// packs the smallest (shortest) cells into the last strips, so the overflow
// clusters at the bottom of every container.
//
// The previous fixed gates (name at r.H > 18/14, value at r.H > 34/30) were
// calibrated for egui's default typography; under the IDS overlay (ADR-0030
// type scale + ADR-0032 GapItems item spacing) a two-line block is ~10 px
// taller, so cells in the gap rendered overflowing labels. labelMetrics
// derives the gates from the actual font row heights instead: measured via
// MeasureTextSizeBind (one-frame lag, the gauge fit.go / colorscale
// cachingMeasurer idiom), seeded analytically for frame 0.
//
// A single non-wrapped line's galley height is the font's row height,
// independent of the string, so one short probe per text style suffices and
// the measured values are constants after the first Sync — the one-frame lag
// degenerates to a one-time warm-up. The seed deliberately over-estimates
// (rowHSeedFactor) so the warm-up frame suppresses a borderline label rather
// than painting the overlap this exists to prevent. The measurement inputs
// (probe, font size) do not depend on what the treemap drew, so the
// measure→gate loop cannot oscillate.

const (
	// rowHSeedFactor converts a font pt size into the frame-0 row-height
	// estimate. Real egui row heights run ≈1.2–1.3× the pt size; 1.45 is
	// deliberately generous (see package comment above on seed direction).
	rowHSeedFactor float64 = 1.45
	// rowProbeText is the measurement probe; any non-empty string yields
	// the same single-line galley height.
	rowProbeText = "Mg"

	// zoomCellVSlack / previewCellVSlack tie the gates to the cell Frame
	// geometry in renderZoom / renderLeafChildren: a cell's content box is
	// cellH - slack tall (UiSetMinHeight), so a label block fits without
	// growing the Frame iff blockH <= r.H - slack. Untyped so the same
	// constant serves the float32 UiSetMinHeight call and float64 gates.
	zoomCellVSlack    = 5
	previewCellVSlack = 3
)

// labelMetrics holds the per-instance measured row heights and the derived
// label gates. nameRowH/valueRowH are pointer-bound: Sync overwrites them
// with the real row heights one frame after renewBindings first runs.
type labelMetrics struct {
	nameFontPt  float32 // Body at the active density — cell name line
	valueFontPt float32 // Small (Caption) at the active density — value line
	gapY        float64 // item_spacing.y between the two lines (GapItems)
	nameRowH    float64
	valueRowH   float64

	idNameW, idNameH   uint64
	idValueW, idValueH uint64
}

// init seeds the metrics analytically and derives the measurement ids.
// scopeKey is the owning Treemap's instance key, so concurrent instances
// keep distinct databinding slots.
func (m *labelMetrics) init(scopeKey string, d styletokens.DensityE) {
	m.nameFontPt = styletokens.ScaledPt(styletokens.BodyPt, d)
	m.valueFontPt = styletokens.ScaledPt(styletokens.CaptionPt, d)
	m.gapY = float64(styletokens.GapItems(d))
	m.nameRowH = float64(m.nameFontPt) * rowHSeedFactor
	m.valueRowH = float64(m.valueFontPt) * rowHSeedFactor
	m.idNameW = metricsMeasureId(scopeKey, "name-row-w")
	m.idNameH = metricsMeasureId(scopeKey, "name-row-h")
	m.idValueW = metricsMeasureId(scopeKey, "value-row-w")
	m.idValueH = metricsMeasureId(scopeKey, "value-row-h")
}

// renewBindings re-emits the probe measurements; call once per Render so the
// bindings survive Sync's databind-reset semantics (the colorscale
// RenewBindings pattern). Widths are pushed by the node but not bound — only
// the heights matter here.
func (m *labelMetrics) renewBindings() {
	c.MeasureTextSizeBind(m.idNameW, m.idNameH, rowProbeText, m.nameFontPt, false, nil, &m.nameRowH)
	c.MeasureTextSizeBind(m.idValueW, m.idValueH, rowProbeText, m.valueFontPt, false, nil, &m.valueRowH)
}

// nameMinH is the cell-rect height (exclusive) a single name line needs to
// fit the Frame's content box without growing it past the cell rect.
func (m *labelMetrics) nameMinH(vSlack float64) float64 {
	return m.nameRow() + vSlack
}

// valueMinH is the two-line variant: name line + item gap + value line.
func (m *labelMetrics) valueMinH(vSlack float64) float64 {
	return m.nameRow() + m.gapY + m.valueRow() + vSlack
}

// nameRow / valueRow floor the bound fields at the font pt size: a row is
// never shorter than its em box, so a zeroed or garbage Sync write cannot
// collapse the gates and reopen the overflow.
func (m *labelMetrics) nameRow() float64 {
	if m.nameRowH < float64(m.nameFontPt) {
		return float64(m.nameFontPt)
	}
	return m.nameRowH
}

func (m *labelMetrics) valueRow() float64 {
	if m.valueRowH < float64(m.valueFontPt) {
		return float64(m.valueFontPt)
	}
	return m.valueRowH
}

// metricsMeasureId derives a stable databinding id from the instance scope
// key and a slot salt (the gauge readoutMeasureId idiom).
func metricsMeasureId(scopeKey string, salt string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(scopeKey))
	_, _ = h.Write([]byte("#treemap-metrics-"))
	_, _ = h.Write([]byte(salt))
	return h.Sum64()
}
