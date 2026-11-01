package riskmatrix

import (
	"math"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// Model is a risk register, columnar: one entry per risk in every slice.
//
// Likelihood is a rate per year and impact an amount, both as an interval
// (lo, hi) — the register's 90% range, say. A point estimate is an interval
// with lo == hi. The matrix places a risk at the geometric midpoint of its two
// intervals, so every value must be positive; Validate says which is not.
type Model struct {
	// Ids label the marks; Names title the key and the tooltip. Names may be
	// nil, and then the id is the name.
	Ids   []string
	Names []string

	// PLo/PHi is the residual likelihood per year, ImpactLo/ImpactHi the
	// residual impact.
	PLo, PHi           []float64
	ImpactLo, ImpactHi []float64

	// The Inherent* slices are the same pair before controls. Either all four
	// are nil — a register that keeps no inherent rating — or all four carry
	// one entry per risk, NaN for a risk that has none.
	InherentPLo, InherentPHi           []float64
	InherentImpactLo, InherentImpactHi []float64
}

// Len is the number of risks.
func (inst *Model) Len() int {
	if inst == nil {
		return 0
	}
	return len(inst.Ids)
}

// HasInherent reports whether the register carries inherent ratings at all.
func (inst *Model) HasInherent() bool {
	return inst != nil && inst.InherentPLo != nil
}

// name is risk i's display name.
func (inst *Model) name(i int) string {
	if i < len(inst.Names) && inst.Names[i] != "" {
		return inst.Names[i]
	}
	return inst.Ids[i]
}

// inherent reports risk i's inherent intervals; ok is false when it has none.
func (inst *Model) inherent(i int) (p, imp [2]float64, ok bool) {
	if !inst.HasInherent() {
		return
	}
	p = [2]float64{inst.InherentPLo[i], inst.InherentPHi[i]}
	imp = [2]float64{inst.InherentImpactLo[i], inst.InherentImpactHi[i]}
	ok = positive(p[0]) && positive(p[1]) && positive(imp[0]) && positive(imp[1])
	return
}

// Validate checks the slices agree in length and every residual value is
// positive and finite. It returns the first problem.
func (inst *Model) Validate() (err error) {
	n := inst.Len()
	if inst == nil || n == 0 {
		return eh.Errorf("riskmatrix: the model holds no risks")
	}
	for _, s := range [][]float64{inst.PLo, inst.PHi, inst.ImpactLo, inst.ImpactHi} {
		if len(s) != n {
			return eh.Errorf("riskmatrix: a residual column has %d entries for %d risks", len(s), n)
		}
	}
	if inst.Names != nil && len(inst.Names) != n {
		return eh.Errorf("riskmatrix: Names has %d entries for %d risks", len(inst.Names), n)
	}
	inh := [][]float64{inst.InherentPLo, inst.InherentPHi, inst.InherentImpactLo, inst.InherentImpactHi}
	for _, s := range inh {
		if (s == nil) != (inh[0] == nil) || (s != nil && len(s) != n) {
			return eh.Errorf("riskmatrix: the inherent columns must be all absent or all of length %d", n)
		}
	}
	for i := range n {
		for _, v := range []float64{inst.PLo[i], inst.PHi[i], inst.ImpactLo[i], inst.ImpactHi[i]} {
			if !positive(v) {
				return eh.Errorf("riskmatrix: risk %q has a residual value %v; likelihood and impact must be positive", inst.Ids[i], v)
			}
		}
	}
	return nil
}

func positive(v float64) bool { return v > 0 && !math.IsInf(v, 1) && !math.IsNaN(v) }

// gmean is the geometric midpoint of an interval, the place the matrix draws
// a range that spans orders of magnitude.
func gmean(lo, hi float64) float64 { return math.Sqrt(lo * hi) }

// Bands is how the banded view bins the two axes, and how it shades a cell.
// The bands are a rendering choice over the stored intervals, not data: the
// same register reads in any binning.
type Bands struct {
	// LikelihoodEdges and ImpactEdges are ascending band edges, n+1 for n
	// bands. The outermost two bound the axis; the inner ones are the band
	// boundaries.
	LikelihoodEdges []float64
	ImpactEdges     []float64
	// LikelihoodLabels and ImpactLabels name the bands; a slice whose length
	// is not the band count is ignored and the labels are derived from the
	// edges ("1 in 10–30 yr", "€500k–2.5m").
	LikelihoodLabels []string
	ImpactLabels     []string
	// ShadeEdges are the expected-annual-loss boundaries of the cell shades,
	// ascending, n-1 for n shades. A cell takes the shade of the expected
	// loss at its geometric centre — likelihood times impact in the units
	// they are stored in, never an ordinal score.
	ShadeEdges []float64
}

// DefaultBands is a 5×5 on geometric edges: likelihood at 1%, 3%, 10% and 30%
// a year, impact in steps of five from 100k, five shades from 3k to 500k of
// expected loss a year.
func DefaultBands() Bands {
	return Bands{
		LikelihoodEdges: []float64{0.001, 0.01, 0.03, 0.1, 0.3, 1},
		ImpactEdges:     []float64{10e3, 100e3, 500e3, 2.5e6, 12.5e6, 100e6},
		ShadeEdges:      []float64{3e3, 20e3, 100e3, 500e3},
	}
}

// resolved returns the bands to draw with: the defaults for a zero value, and
// an error for edges that are not ascending and positive.
func (inst Bands) resolved() (b Bands, err error) {
	b = inst
	d := DefaultBands()
	if len(b.LikelihoodEdges) == 0 {
		b.LikelihoodEdges = d.LikelihoodEdges
	}
	if len(b.ImpactEdges) == 0 {
		b.ImpactEdges = d.ImpactEdges
	}
	if len(b.ShadeEdges) == 0 {
		b.ShadeEdges = d.ShadeEdges
	}
	for _, e := range [][]float64{b.LikelihoodEdges, b.ImpactEdges} {
		if len(e) < 2 {
			return b, eh.Errorf("riskmatrix: a band axis needs at least two edges")
		}
	}
	for _, e := range [][]float64{b.LikelihoodEdges, b.ImpactEdges, b.ShadeEdges} {
		for i, v := range e {
			if !positive(v) || (i > 0 && v <= e[i-1]) {
				return b, eh.Errorf("riskmatrix: band edges must be positive and ascending, got %v", e)
			}
		}
	}
	return b, nil
}

// bandIndex is the band v falls in; values outside the axis fall into the
// outermost band on their side.
func bandIndex(edges []float64, v float64) int {
	for i := 0; i < len(edges)-2; i++ {
		if v < edges[i+1] {
			return i
		}
	}
	return len(edges) - 2
}

// shadeIndex is the shade class of an expected annual loss.
func shadeIndex(edges []float64, el float64) (k int) {
	for _, e := range edges {
		if el < e {
			return
		}
		k++
	}
	return
}

// cellShade is the shade class of the cell at likelihood band li, impact band
// ii: the expected loss at the cell's geometric centre.
func (inst Bands) cellShade(li, ii int) int {
	pc := gmean(inst.LikelihoodEdges[li], inst.LikelihoodEdges[li+1])
	ic := gmean(inst.ImpactEdges[ii], inst.ImpactEdges[ii+1])
	return shadeIndex(inst.ShadeEdges, pc*ic)
}

// likelihoodLabels names the likelihood bands.
func (inst Bands) likelihoodLabels() []string {
	n := len(inst.LikelihoodEdges) - 1
	if len(inst.LikelihoodLabels) == n {
		return inst.LikelihoodLabels
	}
	out := make([]string, n)
	for i := range n {
		out[i] = rateBandLabel(inst.LikelihoodEdges[i], inst.LikelihoodEdges[i+1], i == 0, i == n-1)
	}
	return out
}

// impactLabels names the impact bands.
func (inst Bands) impactLabels(format func(float64) string) []string {
	n := len(inst.ImpactEdges) - 1
	if len(inst.ImpactLabels) == n {
		return inst.ImpactLabels
	}
	out := make([]string, n)
	for i := range n {
		out[i] = amountBandLabel(inst.ImpactEdges[i], inst.ImpactEdges[i+1], i == 0, i == n-1, format)
	}
	return out
}

// shadeLabels names the shade classes.
func (inst Bands) shadeLabels(format func(float64) string) []string {
	n := len(inst.ShadeEdges) + 1
	out := make([]string, n)
	for i := range n {
		var lo, hi float64
		if i > 0 {
			lo = inst.ShadeEdges[i-1]
		}
		if i < n-1 {
			hi = inst.ShadeEdges[i]
		}
		out[i] = amountBandLabel(lo, hi, i == 0, i == n-1, format)
	}
	return out
}

// rateBandLabel names a likelihood band [lo, hi) by its return period —
// "1 in 10–30 yr" — which reads where a percentage does not. The open-ended
// first and last bands name only their inner edge. A band reaching past once
// a year has no return period of a year or more, and names its rate instead.
func rateBandLabel(lo, hi float64, first, last bool) string {
	if hi > 1 {
		switch {
		case last:
			return "> " + rateText(lo) + " / yr"
		default:
			return rateText(lo) + "–" + rateText(hi) + " / yr"
		}
	}
	short, long := roundSig(1/hi), roundSig(1/lo)
	if short == long {
		short, long = roundSig2(1/hi), roundSig2(1/lo)
	}
	switch {
	case first:
		return "< 1 in " + num(short) + " yr"
	case last:
		return "> 1 in " + num(long) + " yr"
	}
	return "1 in " + num(short) + "–" + num(long) + " yr"
}

// rateText is a rate per year as a percentage below once a year and as a
// count above it.
func rateText(p float64) string {
	if p >= 1 {
		return num(roundSig2(p)) + "×"
	}
	return num(roundSig2(p*100)) + "%"
}

// amountBandLabel names [lo, hi) with the caller's formatter. The second
// amount drops the leading symbol the first one carries, so a range reads
// "€500k–2.5m" rather than "€500k–€2.5m".
func amountBandLabel(lo, hi float64, first, last bool, format func(float64) string) string {
	switch {
	case first:
		return "< " + format(hi)
	case last:
		return "> " + format(lo)
	}
	a, b := format(lo), format(hi)
	return a + "–" + trimCommonSymbol(a, b)
}

// trimCommonSymbol drops from b the non-digit prefix a and b share.
func trimCommonSymbol(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] && (a[i] < '0' || a[i] > '9') {
		i++
	}
	if i < len(b) && b[i] >= '0' && b[i] <= '9' {
		return b[i:]
	}
	return b
}

// FormatEuro is the default impact formatter: a compact euro amount, "€12k",
// "€2.5m", "€40m".
func FormatEuro(v float64) string { return "€" + FormatCompact(v) }

// FormatCompact is an amount to three significant figures with a k / m / bn
// suffix: "950", "1.25k", "12.5m", "3bn".
func FormatCompact(v float64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	// Round first, then pick the suffix, so 999.6k reads "1m" and not "1000k".
	r := roundSigN(v, 3)
	for _, s := range []struct {
		div    float64
		suffix string
	}{{1e9, "bn"}, {1e6, "m"}, {1e3, "k"}} {
		if r >= s.div {
			return sign + num(roundSigN(r/s.div, 3)) + s.suffix
		}
	}
	return sign + num(r)
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// roundSig rounds to one significant figure: 33.3 → 30, 3.33 → 3.
func roundSig(v float64) float64 { return roundSigN(v, 1) }

// roundSig2 rounds to two significant figures.
func roundSig2(v float64) float64 { return roundSigN(v, 2) }

// roundSigN rounds a positive v to n significant figures; the division by a
// power of ten keeps 12.5 from printing as 12.500000000000002.
func roundSigN(v float64, n int) float64 {
	if v <= 0 || math.IsInf(v, 0) || math.IsNaN(v) {
		return v
	}
	e := math.Floor(math.Log10(v)) - float64(n-1)
	if e < 0 {
		m := math.Pow(10, -e)
		return math.Round(v*m) / m
	}
	m := math.Pow(10, e)
	return math.Round(v/m) * m
}

// pctText is a likelihood for a tooltip: "3%", "0.2%".
func pctText(p float64) string {
	return num(roundSig2(p*100)) + "%"
}
