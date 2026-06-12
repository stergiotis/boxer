package treemap

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
)

// =============================================================================
// labelMetrics — measured label-height gates (metrics.go)
// =============================================================================

func TestLabelMetrics_InitDerivesDensityTokens(t *testing.T) {
	cases := []struct {
		name        string
		d           styletokens.DensityE
		wantNamePt  float32
		wantValuePt float32
		wantGapY    float64
	}{
		{"tight", styletokens.DensityTight, 12, 10, 6},
		{"standard", styletokens.DensityStandard, 13, 11, 8},
		{"roomy", styletokens.DensityRoomy, 14, 12, 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m labelMetrics
			m.init("k", tc.d)
			if m.nameFontPt != tc.wantNamePt {
				t.Errorf("nameFontPt = %v, want %v", m.nameFontPt, tc.wantNamePt)
			}
			if m.valueFontPt != tc.wantValuePt {
				t.Errorf("valueFontPt = %v, want %v", m.valueFontPt, tc.wantValuePt)
			}
			if m.gapY != tc.wantGapY {
				t.Errorf("gapY = %v, want %v", m.gapY, tc.wantGapY)
			}
		})
	}
}

// The frame-0 seed must err toward suppressing labels: a seeded gate below
// the real row height would paint the overlap the gates exist to prevent.
// Real egui row heights run ≈1.2–1.3× pt, so the 1.45× seed stays above them.
func TestLabelMetrics_SeedIsGenerous(t *testing.T) {
	var m labelMetrics
	m.init("k", styletokens.DensityStandard)
	if got, min := m.nameRowH, float64(m.nameFontPt)*1.3; got < min {
		t.Errorf("seeded nameRowH = %v, want >= %v (1.3x pt)", got, min)
	}
	if got, min := m.valueRowH, float64(m.valueFontPt)*1.3; got < min {
		t.Errorf("seeded valueRowH = %v, want >= %v (1.3x pt)", got, min)
	}
}

func TestLabelMetrics_GateArithmetic(t *testing.T) {
	var m labelMetrics
	m.init("k", styletokens.DensityStandard)
	// Simulate the post-Sync state: measured row heights have replaced the
	// seeds (values representative of Body 13pt / Small 11pt).
	m.nameRowH = 16.25
	m.valueRowH = 13.75

	if got, want := m.nameMinH(zoomCellVSlack), 16.25+5.0; got != want {
		t.Errorf("nameMinH(zoom) = %v, want %v", got, want)
	}
	if got, want := m.nameMinH(previewCellVSlack), 16.25+3.0; got != want {
		t.Errorf("nameMinH(preview) = %v, want %v", got, want)
	}
	// name + item gap (GapItems standard = 8) + value + slack.
	if got, want := m.valueMinH(zoomCellVSlack), 16.25+8.0+13.75+5.0; got != want {
		t.Errorf("valueMinH(zoom) = %v, want %v", got, want)
	}
	if got, want := m.valueMinH(previewCellVSlack), 16.25+8.0+13.75+3.0; got != want {
		t.Errorf("valueMinH(preview) = %v, want %v", got, want)
	}

	// The measured two-line gate sits well above the old fixed 34/30 gates —
	// the band in which labels used to overflow their cells.
	if got := m.valueMinH(zoomCellVSlack); got <= 34 {
		t.Errorf("valueMinH(zoom) = %v, want > 34 (old fixed gate)", got)
	}
}

// A zeroed or garbage Sync write must not collapse the gates below the em
// box and silently reopen the overflow.
func TestLabelMetrics_RowFloorsAtFontPt(t *testing.T) {
	var m labelMetrics
	m.init("k", styletokens.DensityStandard)
	m.nameRowH = 0
	m.valueRowH = -3
	if got, want := m.nameMinH(zoomCellVSlack), float64(m.nameFontPt)+5.0; got != want {
		t.Errorf("nameMinH with zeroed row = %v, want floor %v", got, want)
	}
	if got, want := m.valueMinH(zoomCellVSlack), float64(m.nameFontPt)+8.0+float64(m.valueFontPt)+5.0; got != want {
		t.Errorf("valueMinH with garbage rows = %v, want floor %v", got, want)
	}
}

func TestLabelMetrics_MeasureIdsDistinct(t *testing.T) {
	var a, b labelMetrics
	a.init("scope-a", styletokens.DensityStandard)
	b.init("scope-b", styletokens.DensityStandard)

	seen := map[uint64]string{}
	for name, id := range map[string]uint64{
		"a.nameW": a.idNameW, "a.nameH": a.idNameH, "a.valueW": a.idValueW, "a.valueH": a.idValueH,
		"b.nameW": b.idNameW, "b.nameH": b.idNameH, "b.valueW": b.idValueW, "b.valueH": b.idValueH,
	} {
		if prev, dup := seen[id]; dup {
			t.Errorf("measure id collision: %s and %s share %#x", prev, name, id)
		}
		seen[id] = name
	}
}
