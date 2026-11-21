package stats

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// RowSpan is the pixels of one row from column Min up to but not including
// column Max: what a shape covers on that row (ADR-0399 §SD3).
type RowSpan struct {
	Row, Min, Max int
}

// OfSpans counts the samples of a plane in the pixels spans name, at full
// resolution, as [OfRegion] counts a rectangle's (ADR-0399 §SD7): one bin per
// code for a kind with codes, [FloatBins] bins over the counted samples' own
// range otherwise, invalid samples counted apart. The spans are in row order
// and, within a row, in column order without overlap, each non-empty and
// inside the plane, so that no pixel is counted twice; anything else is an
// error. No spans is the histogram of nothing of the plane's kind.
func OfSpans(plane imaging.Plane, spans []RowSpan) (h Histogram, err error) {
	if err = checkSpans(plane, spans); err != nil {
		return
	}
	if len(spans) == 0 {
		return Histogram{kind: plane.Kind(), transform: plane.Transform()}, nil
	}
	return count(plane, runs{spans: spans}, 0, 0, false)
}

func checkSpans(plane imaging.Plane, spans []RowSpan) error {
	w, h := plane.Width(), plane.Height()
	for i, s := range spans {
		if s.Row < 0 || s.Row >= h || s.Min < 0 || s.Max > w || s.Min >= s.Max {
			return eb.Build().Int("span", i).Int("row", s.Row).Int("min", s.Min).Int("max", s.Max).
				Int("width", w).Int("height", h).Errorf("span is empty or not inside the plane")
		}
		if i > 0 {
			p := spans[i-1]
			if s.Row < p.Row || (s.Row == p.Row && s.Min < p.Max) {
				return eb.Build().Int("span", i).Int("row", s.Row).Int("min", s.Min).
					Int("previousRow", p.Row).Int("previousMax", p.Max).
					Errorf("spans are not in row and column order without overlap")
			}
		}
	}
	return nil
}
