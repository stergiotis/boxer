package writingstylescope

import (
	"iter"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/stergiotis/boxer/public/analytics/similarity/compression"
	"github.com/stergiotis/boxer/public/analytics/similarity/compression/stylometry"
	"github.com/stergiotis/boxer/public/analytics/stats"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Caps (ADR-0175 §SD4). The sweep is quadratic in section count, so it is
// bounded on both axes and refuses rather than stalling a frame. At the
// ceiling — 128 × 128 cells over sections of a few KiB — a measured sweep runs
// in well under a second on a dev box.
const (
	maxPaneBytes      = 1 << 20 // per pasted document
	maxSectionsPerDoc = 128
	maxRankedPairs    = 25 // rows in the ranked-pairs table

	// defaultMinSectionBytes is the section-length floor (§SD1). Below it a
	// compression measurement says more about frame overhead than about
	// content. Adjustable in the app.
	defaultMinSectionBytes = 200
	minMinSectionBytes     = 0
	maxMinSectionBytes     = 4000

	// convWindow / convTolerance configure the streaming convergence detector
	// used by the document-level instance sweep. Mirrors the defaults the
	// stylometry package's own tests use.
	convWindow    = 16
	convTolerance = 1.0e-3
)

// Pair is one cell of the cross-matrix: section I of document A against
// section J of document B.
type Pair struct {
	I   int
	J   int
	Ncd float64
}

// Analysis is one completed sweep. It is immutable once returned and is held
// by the app until either pane's text changes.
type Analysis struct {
	// SecA / SecB are the sections that were actually swept — the ones that
	// cleared MinSectionBytes. DroppedA / DroppedB count those that did not.
	SecA     []Section
	SecB     []Section
	DroppedA int
	DroppedB int

	// Ncd is the cross-matrix, row-major: cell (i, j) is at i*len(SecB)+j.
	Ncd []float64
	// Sorted is Ncd ascending — the ECDF sample and the ranking source.
	Sorted []float64
	// Pairs is the closest maxRankedPairs cells, ascending by Ncd.
	Pairs []Pair

	// Document-level readings from stylometry's own measurement modes, taken
	// with document A (its kept sections, concatenated) as the fixed
	// reference. Profile mode truncates both sides to a common length;
	// instance mode streams B's sections past the reference and may stop
	// early — InstConverged and InstCount say whether it did.
	ProfileNcd    float64
	ProfileCcc    float64
	InstCount     int64
	InstMin       float64
	InstMean      float64
	InstMax       float64
	InstStdDev    float64
	InstConverged bool

	MinSectionBytes int
	Elapsed         time.Duration
}

// Rows and Cols are the matrix dimensions.
func (inst *Analysis) Rows() (n int) { return len(inst.SecA) }
func (inst *Analysis) Cols() (n int) { return len(inst.SecB) }

// At returns the NCD of A-section i against B-section j. Out-of-range
// coordinates return NaN rather than panicking — a hover readout computes
// cell coordinates from cursor position and may briefly be off the grid.
func (inst *Analysis) At(i int, j int) (ncd float64) {
	if i < 0 || j < 0 || i >= inst.Rows() || j >= inst.Cols() {
		return math.NaN()
	}
	return inst.Ncd[i*inst.Cols()+j]
}

// Min and Max bound the matrix. Both are NaN for an empty matrix.
func (inst *Analysis) Min() (v float64) {
	if len(inst.Sorted) == 0 {
		return math.NaN()
	}
	return inst.Sorted[0]
}

func (inst *Analysis) Max() (v float64) {
	if len(inst.Sorted) == 0 {
		return math.NaN()
	}
	return inst.Sorted[len(inst.Sorted)-1]
}

// Quantile returns the fraction of the matrix at or below ncd — where a cell
// sits in this document pair's own background distribution. This is the number
// the app reports instead of a threshold verdict (§SD3): "closer than all but
// 0.1% of pairs" is a statement about these two documents, where "below 0.4"
// would be a statement about a corpus nobody measured.
func (inst *Analysis) Quantile(ncd float64) (q float64) {
	n := len(inst.Sorted)
	if n == 0 || math.IsNaN(ncd) {
		return math.NaN()
	}
	idx := sort.SearchFloat64s(inst.Sorted, math.Nextafter(ncd, math.Inf(1)))
	return float64(idx) / float64(n)
}

// analyze splits both documents, sweeps the section cross-product, and takes
// the two document-level readings. It is pure with respect to the app: no
// widget state, no globals beyond the compressor it allocates and drops.
//
// Errors are the cap refusals and the degenerate inputs; a per-cell
// measurement failure aborts the sweep rather than leaving a hole in the
// matrix, because a hole would be indistinguishable from a low score.
func analyze(srcA string, srcB string, minSectionBytes int) (res *Analysis, err error) {
	if len(srcA) > maxPaneBytes || len(srcB) > maxPaneBytes {
		err = eh.Errorf("document too large: %d / %d bytes, limit is %d per document",
			len(srcA), len(srcB), maxPaneBytes)
		return
	}
	secA, dropA := keepAtLeast(splitSections(srcA), minSectionBytes)
	secB, dropB := keepAtLeast(splitSections(srcB), minSectionBytes)
	if len(secA) == 0 || len(secB) == 0 {
		err = eh.Errorf("nothing to compare: %d and %d sections reach the %d-byte floor (%d and %d were below it)",
			len(secA), len(secB), minSectionBytes, dropA, dropB)
		return
	}
	if len(secA) > maxSectionsPerDoc || len(secB) > maxSectionsPerDoc {
		err = eh.Errorf("too many sections: %d and %d, limit is %d per document — raise the section-length floor to merge short sections out of the sweep",
			len(secA), len(secB), maxSectionsPerDoc)
		return
	}

	started := time.Now()
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		err = eh.Errorf("unable to create zstd encoder: %w", err)
		return
	}
	defer func() { _ = enc.Close() }()

	res = &Analysis{
		SecA:            secA,
		SecB:            secB,
		DroppedA:        dropA,
		DroppedB:        dropB,
		MinSectionBytes: minSectionBytes,
	}
	err = res.sweep(enc)
	if err != nil {
		res = nil
		return
	}
	err = res.headline(enc)
	if err != nil {
		res = nil
		return
	}
	res.Elapsed = time.Since(started)
	return
}

// sweep fills the cross-matrix on the exact path (§SD5): one Similarity for
// the whole grid, C(a‖b) measured directly, C(b) cached once per column
// because it does not depend on the row.
func (inst *Analysis) sweep(enc compression.CompressorI) (err error) {
	// The reference text is irrelevant to MeasureCompressedLength, which is
	// the only method this sweep calls — the Similarity is used here purely as
	// the package's compressor harness. An empty reference keeps the
	// construction cheap and leaves the dictionary path (which needs a fixed
	// reference to be worth anything) out of it.
	sim, err := compression.NewSimilarity("", enc)
	if err != nil {
		err = eh.Errorf("unable to create similarity engine: %w", err)
		return
	}

	rows, cols := inst.Rows(), inst.Cols()
	colLen := make([]uint64, cols)
	for j, sb := range inst.SecB {
		colLen[j], err = sim.MeasureCompressedLength(sb.Text, "")
		if err != nil {
			err = eh.Errorf("unable to measure section B%d: %w", j, err)
			return
		}
	}

	inst.Ncd = make([]float64, rows*cols)
	for i, sa := range inst.SecA {
		var x uint64
		x, err = sim.MeasureCompressedLength(sa.Text, "")
		if err != nil {
			err = eh.Errorf("unable to measure section A%d: %w", i, err)
			return
		}
		for j, sb := range inst.SecB {
			var xy uint64
			xy, err = sim.MeasureCompressedLength(sa.Text, sb.Text)
			if err != nil {
				err = eh.Errorf("unable to measure section pair A%d/B%d: %w", i, j, err)
				return
			}
			inst.Ncd[i*cols+j] = compression.CalculateNormalizedCompressionDistance(xy, x, colLen[j])
		}
	}

	// The ECDF sample keeps only finite cells. NCD's denominator is a
	// compressed length and so is never zero for a section that cleared the
	// length floor, but ecdfbands rejects an unsorted sample and a stray NaN
	// would make slices.Sort produce one — cheaper to exclude than to debug.
	inst.Sorted = make([]float64, 0, len(inst.Ncd))
	for _, v := range inst.Ncd {
		if !math.IsInf(v, 0) && !math.IsNaN(v) {
			inst.Sorted = append(inst.Sorted, v)
		}
	}
	slices.Sort(inst.Sorted)
	inst.Pairs = rankPairs(inst.Ncd, cols, maxRankedPairs)
	return
}

// headline takes the two document-level readings on the dictionary-optimised
// path (§SD5), where the reference is fixed and the candidates stream past it
// — the shape stylometry's Analyzer is built for. The reference is A's kept
// sections concatenated, so both readings see the same text the matrix did.
func (inst *Analysis) headline(enc compression.CompressorI) (err error) {
	an, err := stylometry.NewAnalyzer(concatSections(inst.SecA),
		stats.NewConvergenceDetector(convWindow, convTolerance), enc)
	if err != nil {
		err = eh.Errorf("unable to create stylometry analyzer: %w", err)
		return
	}
	texts := sectionTexts(inst.SecB)

	_, _, inst.ProfileNcd, err = an.MeasureNcdProfile(texts)
	if err != nil {
		err = eh.Errorf("profile NCD: %w", err)
		return
	}
	_, _, inst.ProfileCcc, err = an.MeasureCccProfile(texts)
	if err != nil {
		err = eh.Errorf("profile CCC: %w", err)
		return
	}
	_, inst.InstCount, inst.InstMin, inst.InstMean, inst.InstMax, inst.InstStdDev,
		inst.InstConverged, err = an.MeasureNcdInstance(texts)
	if err != nil {
		err = eh.Errorf("instance NCD: %w", err)
		return
	}
	return
}

// rankPairs returns the keep closest cells, ascending by NCD. Ties keep
// row-major order, so the ranking is deterministic across runs.
func rankPairs(ncd []float64, cols int, keep int) (pairs []Pair) {
	if cols <= 0 {
		return
	}
	pairs = make([]Pair, 0, len(ncd))
	for k, v := range ncd {
		pairs = append(pairs, Pair{I: k / cols, J: k % cols, Ncd: v})
	}
	sort.SliceStable(pairs, func(a int, b int) bool { return pairs[a].Ncd < pairs[b].Ncd })
	if len(pairs) > keep {
		pairs = pairs[:keep]
	}
	return
}

// sectionTexts adapts a section slice to the iterator the stylometry
// measurement modes consume. The sequence is re-iterable — each mode ranges
// over it independently.
func sectionTexts(secs []Section) (seq iter.Seq[string]) {
	return func(yield func(string) bool) {
		for _, s := range secs {
			if !yield(s.Text) {
				return
			}
		}
	}
}

// concatSections joins the kept sections back into one document-shaped text.
func concatSections(secs []Section) (out string) {
	var sb strings.Builder
	for _, s := range secs {
		sb.WriteString(s.Text)
	}
	return sb.String()
}
