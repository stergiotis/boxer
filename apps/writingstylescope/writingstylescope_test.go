package writingstylescope

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdf"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// ---------------------------------------------------------------------------
// Section splitting (ADR-0175 §SD1)
// ---------------------------------------------------------------------------

// labelsOf collapses a section slice to its titles, for readable assertions.
func labelsOf(secs []Section) (out []string) {
	out = make([]string, len(secs))
	for i, s := range secs {
		out[i] = s.Label()
	}
	return
}

// findSection returns the index of the section whose title equals want.
func findSection(t *testing.T, secs []Section, want string) (idx int) {
	t.Helper()
	for i, s := range secs {
		if s.Title == want {
			return i
		}
	}
	t.Fatalf("section %q not found in %v", want, labelsOf(secs))
	return -1
}

func TestSplitSectionsFlatNotSubtree(t *testing.T) {
	// The load-bearing property: a nesting heading owns only its own lead-in,
	// never its children's text. Slicing by subtree instead would put "intro
	// text" *and* both child bodies into section "A".
	src := "# A\nintro text\n\n## B\nbody of b\n\n## C\nbody of c\n"
	secs := splitSections(src)

	require.Equal(t, []string{"A", "B", "C"}, labelsOf(secs))
	assert.Equal(t, "# A\nintro text\n\n", secs[0].Text)
	assert.Equal(t, "## B\nbody of b\n\n", secs[1].Text)
	assert.Equal(t, "## C\nbody of c\n", secs[2].Text)

	// Contiguous and non-overlapping: reassembling the sections reproduces
	// the source exactly, which is what "nothing dropped, nothing doubled"
	// means operationally.
	var sb strings.Builder
	for _, s := range secs {
		sb.WriteString(s.Text)
	}
	assert.Equal(t, src, sb.String())
}

func TestSplitSectionsKeepsHeadingLine(t *testing.T) {
	// A section starts at its heading's line start, not at the heading text,
	// so the `##` marker never trails into the previous section's body.
	src := "# One\nalpha\n## Two\nbeta\n"
	secs := splitSections(src)
	require.Len(t, secs, 2)
	assert.True(t, strings.HasPrefix(secs[0].Text, "# One\n"))
	assert.True(t, strings.HasPrefix(secs[1].Text, "## Two\n"))
	assert.False(t, strings.Contains(secs[0].Text, "##"))
}

func TestSplitSectionsPreamble(t *testing.T) {
	src := "some words before any heading\n\n# First\nbody\n"
	secs := splitSections(src)
	require.Equal(t, []string{"(preamble)", "First"}, labelsOf(secs))
	assert.Equal(t, uint8(0), secs[0].Level)
	assert.Equal(t, uint8(1), secs[1].Level)
}

func TestSplitSectionsBlankPreambleOmitted(t *testing.T) {
	secs := splitSections("\n\n   \n# First\nbody\n")
	require.Equal(t, []string{"First"}, labelsOf(secs))
}

func TestSplitSectionsFrontmatterExcluded(t *testing.T) {
	// Frontmatter is document metadata, not authored prose: two repo docs
	// sharing `type: adr` are not sharing writing.
	src := "---\ntype: adr\nstatus: proposed\n---\n\n# Body\nprose\n"
	secs := splitSections(src)
	require.Equal(t, []string{"Body"}, labelsOf(secs))
	for _, s := range secs {
		assert.NotContains(t, s.Text, "type: adr")
	}
}

func TestSplitSectionsNoHeadings(t *testing.T) {
	src := "just a paragraph of prose with no headings at all.\n"
	secs := splitSections(src)
	require.Equal(t, []string{"(preamble)"}, labelsOf(secs))
	assert.Equal(t, src, secs[0].Text)
}

func TestSplitSectionsIgnoresHashInFencedCode(t *testing.T) {
	// The reason the splitter goes through a real Markdown parser rather than
	// scanning for lines starting with '#'.
	src := "# Real\ntext\n\n```sh\n# not a heading\necho hi\n```\n\n## Also real\nmore\n"
	secs := splitSections(src)
	assert.Equal(t, []string{"Real", "Also real"}, labelsOf(secs))
}

func TestSplitSectionsDegenerateHeading(t *testing.T) {
	// `##` alone has no text lines, so goldmark reports no byte offset. The
	// slicer must produce a section rather than panicking or inverting a
	// slice.
	src := "# One\nalpha\n##\n### Deep\nbeta\n"
	secs := splitSections(src)
	require.NotEmpty(t, secs)
	for i, s := range secs {
		assert.LessOrEqual(t, s.Start, s.End, "section %d has inverted bounds", i)
		assert.Equal(t, s.End-s.Start, len(s.Text), "section %d text/bounds disagree", i)
	}
}

func TestSplitSectionsStripsHeadingAnchor(t *testing.T) {
	secs := splitSections("## Creating a table {#creating-a-table}\nbody\n")
	require.Len(t, secs, 1)
	assert.Equal(t, "Creating a table", secs[0].Title)
}

func TestKeepAtLeast(t *testing.T) {
	secs := []Section{
		{Title: "short", Text: "tiny"},
		{Title: "long", Text: strings.Repeat("x", 300)},
		{Title: "exact", Text: strings.Repeat("y", 200)},
	}
	kept, dropped := keepAtLeast(secs, 200)
	assert.Equal(t, 1, dropped)
	assert.Equal(t, []string{"long", "exact"}, labelsOf(kept))
}

// ---------------------------------------------------------------------------
// The sweep, and the claim the app actually makes
// ---------------------------------------------------------------------------

func TestAnalyzeSampleFindsTheSharedSection(t *testing.T) {
	// This is the whole app's claim: the section that appears in both
	// documents is the matrix minimum, and it is separated from the
	// background rather than merely somewhere near the bottom of it.
	res, err := analyze(sampleDocA, sampleDocB, defaultMinSectionBytes)
	require.NoError(t, err)
	require.NotNil(t, res)

	ai := findSection(t, res.SecA, "Retry budgets")
	bj := findSection(t, res.SecB, "Retry budgets")

	require.NotEmpty(t, res.Pairs)
	top := res.Pairs[0]
	assert.Equal(t, ai, top.I, "shared section is not the closest row")
	assert.Equal(t, bj, top.J, "shared section is not the closest column")
	assert.InDelta(t, res.Min(), res.At(ai, bj), 1e-12)

	// Separated from the bulk, not merely at its edge.
	q := res.Quantile(res.At(ai, bj))
	assert.Less(t, q, 0.05, "shared pair should sit in the extreme left tail")
	median := quantileOf(res.Sorted, 0.5)
	assert.Less(t, res.At(ai, bj), median-0.1,
		"shared pair should be well clear of the median pair")
}

func TestAnalyzeSameTopicIsNotFlaggedLikeACopy(t *testing.T) {
	// The distractor: "Timeouts" is written independently in both documents
	// on the same subject. It should score below the median (same vocabulary)
	// but stay far above the copied pair — which is exactly the case a fixed
	// threshold gets wrong, and the reason the app reports a quantile instead.
	res, err := analyze(sampleDocA, sampleDocB, defaultMinSectionBytes)
	require.NoError(t, err)

	ti := findSection(t, res.SecA, "Timeouts")
	tj := findSection(t, res.SecB, "Timeouts")
	ri := findSection(t, res.SecA, "Retry budgets")
	rj := findSection(t, res.SecB, "Retry budgets")

	timeouts := res.At(ti, tj)
	copied := res.At(ri, rj)
	assert.Greater(t, timeouts, copied+0.1,
		"same-topic pair should be nowhere near the copied pair")
	assert.Less(t, timeouts, quantileOf(res.Sorted, 0.5),
		"same-topic pair should still read as closer than a random pair")
}

func TestAnalyzeMatrixShape(t *testing.T) {
	res, err := analyze(sampleDocA, sampleDocB, defaultMinSectionBytes)
	require.NoError(t, err)

	assert.Equal(t, res.Rows()*res.Cols(), len(res.Ncd))
	assert.Equal(t, len(res.Ncd), len(res.Sorted))
	for k, v := range res.Ncd {
		require.Falsef(t, math.IsNaN(v) || math.IsInf(v, 0), "cell %d is not finite", k)
	}
	// Sorted is ascending — ecdfbands rejects an unsorted sample outright.
	for k := 1; k < len(res.Sorted); k++ {
		require.LessOrEqual(t, res.Sorted[k-1], res.Sorted[k])
	}
	// Ranked pairs index real cells and are ascending.
	for k, p := range res.Pairs {
		assert.InDelta(t, res.At(p.I, p.J), p.Ncd, 1e-12, "pair %d points at the wrong cell", k)
		if k > 0 {
			assert.LessOrEqual(t, res.Pairs[k-1].Ncd, p.Ncd)
		}
	}
}

func TestAnalyzeIsDeterministic(t *testing.T) {
	a, err := analyze(sampleDocA, sampleDocB, defaultMinSectionBytes)
	require.NoError(t, err)
	b, err := analyze(sampleDocA, sampleDocB, defaultMinSectionBytes)
	require.NoError(t, err)
	assert.Equal(t, a.Ncd, b.Ncd)
	assert.Equal(t, a.Pairs, b.Pairs)
	assert.Equal(t, a.ProfileNcd, b.ProfileNcd)
}

func TestAnalyzeIdenticalDocumentsPutDiagonalFirst(t *testing.T) {
	res, err := analyze(sampleDocA, sampleDocA, defaultMinSectionBytes)
	require.NoError(t, err)
	require.Equal(t, res.Rows(), res.Cols())
	// Every diagonal cell is a section against itself, so the whole diagonal
	// should occupy the lowest ranks.
	for i := 0; i < res.Rows(); i++ {
		for j := 0; j < res.Cols(); j++ {
			if i == j {
				continue
			}
			assert.Less(t, res.At(i, i), res.At(i, j),
				"section %d is closer to section %d than to itself", i, j)
		}
	}
}

func TestAnalyzeHeadlineModesPopulated(t *testing.T) {
	res, err := analyze(sampleDocA, sampleDocB, defaultMinSectionBytes)
	require.NoError(t, err)
	assert.False(t, math.IsNaN(res.ProfileNcd))
	assert.Greater(t, res.ProfileNcd, 0.0)
	assert.Greater(t, res.ProfileCcc, 0.0)
	assert.Equal(t, int64(res.Cols()), res.InstCount,
		"instance mode should have seen every B section without stopping early")
	assert.GreaterOrEqual(t, res.InstMean, res.InstMin)
	assert.LessOrEqual(t, res.InstMean, res.InstMax)
}

func TestAnalyzeRefusalsAreExplicit(t *testing.T) {
	big := strings.Repeat("x", maxPaneBytes+1)
	_, err := analyze(big, sampleDocB, defaultMinSectionBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")

	_, err = analyze("tiny", "tiny", defaultMinSectionBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nothing to compare")

	var many strings.Builder
	for i := 0; i <= maxSectionsPerDoc; i++ {
		many.WriteString("## Section\n")
		many.WriteString(strings.Repeat("filler prose. ", 30))
		many.WriteString("\n\n")
	}
	_, err = analyze(many.String(), sampleDocB, defaultMinSectionBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many sections")
}

func TestAnalysisQuantile(t *testing.T) {
	a := &Analysis{Sorted: []float64{0.1, 0.2, 0.2, 0.9}}
	assert.InDelta(t, 0.25, a.Quantile(0.1), 1e-12)
	assert.InDelta(t, 0.75, a.Quantile(0.2), 1e-12)
	assert.InDelta(t, 1.0, a.Quantile(0.9), 1e-12)
	assert.InDelta(t, 0.0, a.Quantile(0.0), 1e-12)
	assert.True(t, math.IsNaN(a.Quantile(math.NaN())))
	assert.True(t, math.IsNaN((&Analysis{}).Quantile(0.5)))
}

func TestAnalysisAtOutOfRange(t *testing.T) {
	a := &Analysis{SecA: make([]Section, 2), SecB: make([]Section, 3), Ncd: make([]float64, 6)}
	assert.False(t, math.IsNaN(a.At(1, 2)))
	assert.True(t, math.IsNaN(a.At(-1, 0)))
	assert.True(t, math.IsNaN(a.At(2, 0)))
	assert.True(t, math.IsNaN(a.At(0, 3)))
}

// ---------------------------------------------------------------------------
// Render-side pure helpers
// ---------------------------------------------------------------------------

func TestCellAtRowZeroIsTop(t *testing.T) {
	const rows, cols = 4, 3
	// implot draws heatmap row 0 at the TOP edge (y1 == rows), so the topmost
	// band of the plot must map back to A-section 0.
	j, i := cellAt(0.5, float64(rows)-0.5, rows, cols)
	assert.Equal(t, 0, i)
	assert.Equal(t, 0, j)

	j, i = cellAt(2.5, 0.5, rows, cols)
	assert.Equal(t, rows-1, i)
	assert.Equal(t, cols-1, j)

	for _, pos := range [][2]float64{{-0.1, 1}, {1, -0.1}, {float64(cols), 1}, {1, float64(rows)}} {
		j, i = cellAt(pos[0], pos[1], rows, cols)
		assert.Equal(t, -1, i, "pos %v should be off-grid", pos)
		assert.Equal(t, -1, j, "pos %v should be off-grid", pos)
	}
}

func TestTickValuesLineUpWithCells(t *testing.T) {
	// Row tick k must sit in the band that cellAt maps back to row k.
	const rows, cols = 5, 5
	ys := rowTickValues(rows)
	for k, y := range ys {
		_, i := cellAt(0.5, y, rows, cols)
		assert.Equalf(t, k, i, "row tick %d does not sit over row %d", k, k)
	}
	xs := colTickValues(cols)
	for k, x := range xs {
		j, _ := cellAt(x, 0.5, rows, cols)
		assert.Equalf(t, k, j, "column tick %d does not sit over column %d", k, k)
	}
}

func TestSectionLabelsSuppressedWhenCrowded(t *testing.T) {
	few := make([]Section, 3)
	assert.Len(t, sectionLabels(few, len(few)), 3)
	many := make([]Section, maxTickLabels+1)
	assert.Nil(t, sectionLabels(many, len(many)))
}

func TestEcdfGridIsMonotoneAndBounded(t *testing.T) {
	sorted := []float64{0.1, 0.2, 0.2, 0.4, 0.9}
	xs, fn := ecdfGrid(sorted, 32)
	require.Len(t, xs, 32)
	require.Len(t, fn, 32)
	for k := range xs {
		assert.GreaterOrEqual(t, fn[k], 0.0)
		assert.LessOrEqual(t, fn[k], 1.0)
		if k > 0 {
			assert.LessOrEqual(t, xs[k-1], xs[k])
			assert.LessOrEqual(t, fn[k-1], fn[k])
		}
	}
	assert.InDelta(t, 1.0, fn[len(fn)-1], 1e-12)

	// Degenerate samples produce no grid rather than an unsorted one.
	xs, fn = ecdfGrid([]float64{0.5, 0.5}, 32)
	assert.Nil(t, xs)
	assert.Nil(t, fn)
	xs, _ = ecdfGrid(nil, 32)
	assert.Nil(t, xs)
}

func TestElide(t *testing.T) {
	assert.Equal(t, "short", elide("short", 10))
	assert.Equal(t, "abcd…", elide("abcdefgh", 5))
	assert.Equal(t, "…", elide("abcdefgh", 1))
	// Rune-counting, not byte-counting: a multi-byte title must not be cut
	// mid-character.
	assert.Equal(t, "ααα…", elide("ααααα", 4))
}

func TestReversedPalette(t *testing.T) {
	in := []uint32{1, 2, 3}
	out := reversedPalette(in)
	assert.Equal(t, []uint32{3, 2, 1}, out)
	assert.Equal(t, []uint32{1, 2, 3}, in, "input must not be mutated")
}

func TestPercentTextKeepsTailResolution(t *testing.T) {
	assert.Equal(t, "0%", percentText(0))
	assert.Equal(t, "0.006%", percentText(0.00006))
	assert.Equal(t, "0.50%", percentText(0.005))
	assert.Equal(t, "42.0%", percentText(0.42))
	assert.Equal(t, "—", percentText(math.NaN()))
}

func TestClampFloor(t *testing.T) {
	assert.Equal(t, minMinSectionBytes, clampFloor(-5))
	assert.Equal(t, minMinSectionBytes, clampFloor(math.NaN()))
	assert.Equal(t, maxMinSectionBytes, clampFloor(1e9))
	assert.Equal(t, 250, clampFloor(250.7))
}

func TestQuantileOf(t *testing.T) {
	s := []float64{1, 2, 3, 4}
	assert.Equal(t, 1.0, quantileOf(s, 0))
	assert.Equal(t, 3.0, quantileOf(s, 0.5))
	assert.Equal(t, 4.0, quantileOf(s, 1.0))
	assert.True(t, math.IsNaN(quantileOf(nil, 0.5)))
}

func TestMarkValuesDeduplicates(t *testing.T) {
	pairs := []Pair{{Ncd: 0.1}, {Ncd: 0.1}, {Ncd: 0.2}}
	assert.Equal(t, []float64{0.1, 0.2}, markValues(pairs))
	assert.Empty(t, markValues(nil))
}

// ---------------------------------------------------------------------------
// App wiring
// ---------------------------------------------------------------------------

func TestNewAppDefersTheSweep(t *testing.T) {
	// Opening a window must not pay for a sweep; the first frame does.
	inst := newApp()
	require.True(t, inst.pending)
	assert.Nil(t, inst.res)

	inst.runPending()
	require.False(t, inst.pending)
	require.NoError(t, inst.err)
	require.NotNil(t, inst.res)
	assert.False(t, inst.stale())
}

func TestStaleTracksEveryInput(t *testing.T) {
	inst := newApp()
	inst.runPending()
	require.False(t, inst.stale())

	inst.docA += "\n## Extra\n" + strings.Repeat("prose. ", 40)
	assert.True(t, inst.stale())
	inst.run()
	assert.False(t, inst.stale())

	inst.minSectionBytes = 400
	assert.True(t, inst.stale())
}

func TestBandJobKeysAreDistinctPerInstance(t *testing.T) {
	assert.NotEqual(t, newApp().bandKey, newApp().bandKey)
}

func TestRunRecordsFailureWithoutStaleResult(t *testing.T) {
	inst := newApp()
	inst.runPending()
	require.NotNil(t, inst.res)

	inst.docA = "tiny"
	inst.run()
	require.Error(t, inst.err)
	assert.Nil(t, inst.res, "a failed sweep must not leave the previous matrix behind")
}

func TestColorRangeWidensDegenerateMatrix(t *testing.T) {
	// colormap.NewConfig panics unless min < max.
	inst := &App{res: &Analysis{Sorted: []float64{0.4, 0.4}}}
	lo, hi := inst.colorRange()
	assert.Less(t, lo, hi)

	inst = &App{}
	lo, hi = inst.colorRange()
	assert.Less(t, lo, hi)
}

func TestManifestRegisters(t *testing.T) {
	// RegisterFactory only logs a warning when Validate rejects a manifest, so
	// a malformed one would leave the app silently missing from the launcher.
	require.NoError(t, manifest.Validate())
	got, ok := app.DefaultRegistry.LookupManifest(app.AppIdT(manifest.Id))
	require.True(t, ok, "app is not in the default registry")
	assert.Equal(t, manifest.Display, got.Display)
	assert.Equal(t, app.SurfaceWindowed, got.Surface)
}

// ---------------------------------------------------------------------------
// Plot declaration path (headless, via implot's detached plot handle)
// ---------------------------------------------------------------------------

// TestEcdfDeclaresWithoutError drives the Distribution tab's plot declarations
// against a detached plot: no canvas, no frame, but the same band solve and
// the same geometry emission. It is the cheapest way to catch the failures
// that would otherwise be silent on screen — an unsorted sample rejected by
// ecdfbands, or a grid the band solver will not accept.
func TestEcdfDeclaresWithoutError(t *testing.T) {
	res, err := analyze(sampleDocA, sampleDocB, defaultMinSectionBytes)
	require.NoError(t, err)

	xs, fnAt := ecdfGrid(res.Sorted, ecdfGridN)
	require.NotEmpty(t, xs)
	n := len(res.Sorted)

	rr := ecdf.New().SeriesName("NCD, all section pairs")
	p := implot.NewDetached()
	require.NoError(t, rr.RenderGridPreview(p, xs, fnAt, n), "DKW preview band")
	require.NoError(t, rr.RenderGrid(p, xs, fnAt, n), "exact band")

	// The crosshair readers must tolerate "no plot has rendered yet".
	assert.False(t, rr.AtGrid(p, xs, fnAt, n).Valid)
	rr.PaintCrosshair(p, rr.AtGridPreview(p, xs, fnAt, n))
}

// TestHeatmapDeclaresOverTheWholeMatrix checks the Matrix tab's declaration
// against the same detached handle, including the colormap construction that
// panics when its range is degenerate.
func TestHeatmapDeclaresOverTheWholeMatrix(t *testing.T) {
	inst := newApp()
	inst.runPending()
	require.NoError(t, inst.err)
	r := inst.res

	lo, hi := inst.colorRange()
	inst.cmap = colormap.NewConfig(reversedPalette(colormap.Viridis8), lo, hi)
	p := implot.NewDetached()
	p.SetupAxisTicks(implot.AxisX1, colTickValues(r.Cols()), sectionLabels(r.SecB, r.Cols()))
	p.SetupAxisTicks(implot.AxisY1, rowTickValues(r.Rows()), sectionLabels(r.SecA, r.Rows()))
	p.Heatmap("NCD", r.Ncd, r.Rows(), r.Cols(), inst.cmap, 0, 0, float64(r.Cols()), float64(r.Rows()))

	// Every cell must land inside the colour range, or it renders as the
	// (transparent) underflow/overflow colour instead of a palette entry.
	for k, v := range r.Ncd {
		require.GreaterOrEqualf(t, v, lo, "cell %d underflows the colour range", k)
		require.LessOrEqualf(t, v, hi, "cell %d overflows the colour range", k)
	}
}

// ---------------------------------------------------------------------------
// Pane-following plot sizing
// ---------------------------------------------------------------------------

func TestBoxSizeFollowsThePane(t *testing.T) {
	// Unmeasured pane: the fixed preference, so the first frame draws something
	// sane rather than a floor-height sliver.
	w, h := boxSize([2]float32{}, ecdfPrefH, ecdfChromeBelow)
	assert.Equal(t, stageW, w)
	assert.Equal(t, ecdfPrefH, h)

	// A tall pane does not stretch the box past its preference.
	w, h = boxSize([2]float32{1600, 2000}, ecdfPrefH, ecdfChromeBelow)
	assert.Equal(t, float32(1600)-paneSlack, w)
	assert.Equal(t, ecdfPrefH, h)

	// A short pane shrinks the box, so implot keeps its bottom gutter — the x
	// tick labels — inside the pane.
	_, h = boxSize([2]float32{900, ecdfChromeBelow + 200}, ecdfPrefH, ecdfChromeBelow)
	assert.Equal(t, float32(200), h)
}

func TestBoxSizeNeverGoesUnderImplotsFloor(t *testing.T) {
	// Under the floor implot's gutters exceed the canvas and the box clips its
	// own tick labels — a smaller box is not a smaller plot, only a broken one.
	for _, paneH := range []float32{1, 40, ecdfChromeBelow, ecdfChromeBelow + 10} {
		_, h := boxSize([2]float32{900, paneH}, ecdfPrefH, ecdfChromeBelow)
		assert.GreaterOrEqualf(t, h, plotMinH, "pane height %v fell under the floor", paneH)
	}
	assert.Greater(t, plotMinH, float32(0), "MinBoxHeight should be a real bound")
}

func TestBoxSizeWidthFloor(t *testing.T) {
	// A very narrow pane degrades to the minimum width rather than an
	// unreadable sliver; the tab's ScrollArea takes the overflow.
	w, _ := boxSize([2]float32{60, 900}, ecdfPrefH, ecdfChromeBelow)
	assert.Equal(t, plotMinW, w)
}

// ---------------------------------------------------------------------------
// play handover
// ---------------------------------------------------------------------------

func TestPairsArrowPassesThePublishGate(t *testing.T) {
	// StructureFor is the ADR-0134 §SD1 bounded-type gate: a column type
	// outside the set is refused at publish, which would surface as a runtime
	// failure behind the button rather than at build time.
	structure, err := adhocdata.StructureFor(pairsSchema)
	require.NoError(t, err)
	for _, col := range []string{"a_section", "b_section", "ncd", "quantile", "a_bytes", "b_bytes"} {
		assert.Contains(t, structure, col)
	}
}

func TestPairsArrowCarriesEveryCell(t *testing.T) {
	res, err := analyze(sampleDocA, sampleDocB, defaultMinSectionBytes)
	require.NoError(t, err)

	stream, err := pairsArrow(res)
	require.NoError(t, err)
	require.NotEmpty(t, stream)

	rdr, err := ipc.NewReader(bytes.NewReader(stream))
	require.NoError(t, err)
	defer rdr.Release()
	require.True(t, rdr.Next(), "stream carries no record")
	rec := rdr.RecordBatch()

	require.Equal(t, int64(res.Rows()*res.Cols()), rec.NumRows())
	require.Equal(t, int64(len(pairsSchema.Fields())), rec.NumCols())

	aSec := rec.Column(2).(*array.String)
	bSec := rec.Column(3).(*array.String)
	ncd := rec.Column(8).(*array.Float64)
	quant := rec.Column(9).(*array.Float64)

	// Row-major, matching Analysis.Ncd, with the titles and the quantile the
	// panel shows carried alongside.
	for i := range res.Rows() {
		for j := range res.Cols() {
			k := i*res.Cols() + j
			assert.Equal(t, res.SecA[i].Label(), aSec.Value(k))
			assert.Equal(t, res.SecB[j].Label(), bSec.Value(k))
			assert.InDelta(t, res.At(i, j), ncd.Value(k), 1e-12)
			assert.InDelta(t, res.Quantile(res.At(i, j)), quant.Value(k), 1e-12)
		}
	}
	assert.False(t, rdr.Next(), "expected exactly one record")
}

func TestPairsArrowRefusesAnEmptyMatrix(t *testing.T) {
	_, err := pairsArrow(nil)
	require.Error(t, err)
	_, err = pairsArrow(&Analysis{})
	require.Error(t, err)
}

func TestHandoverSqlNamesTheHandle(t *testing.T) {
	// Handle form, not alias form: the opened window inherits no alias binding.
	sql := handoverSql("adhoc_deadbeef")
	assert.Contains(t, sql, "keelson('adhoc_deadbeef')")
	assert.NotContains(t, sql, datasetAlias+"'")
	assert.Contains(t, sql, "ORDER BY ncd ASC")
	assert.Contains(t, sql, fmt.Sprintf("LIMIT %d", maxRankedPairs))
}

func TestHandoverWithoutABusFailsCleanly(t *testing.T) {
	inst := newApp()
	inst.runPending()
	require.NoError(t, inst.err)

	inst.requestHandover()
	busy, note, errText := inst.handoverState()
	assert.False(t, busy, "the busy flag must clear even on the failure path")
	assert.Empty(t, note)
	assert.Contains(t, errText, "no bus")
	// Nothing was published, so nothing is left to retract.
	assert.Empty(t, inst.handle)
	inst.retractHandover()
}

func TestManifestDeclaresTheHandoverCaps(t *testing.T) {
	// The three caps the handover actually exercises, and no others — an app
	// that declares a cap it never uses is the §SD10 gate's other failure mode.
	want := map[string]bool{
		adhocdata.SubjectPublish: false,
		adhocdata.SubjectRetract: false,
		windowhost.OpenSubject:   false,
	}
	for _, cap := range manifest.Caps {
		_, known := want[cap.Pattern]
		require.Truef(t, known, "undeclared-in-test cap %q", cap.Pattern)
		want[cap.Pattern] = true
		assert.Equal(t, app.CapDirectionPub, cap.Direction, "cap %q", cap.Pattern)
		assert.NotEmpty(t, cap.Reason, "cap %q needs a reason", cap.Pattern)
	}
	for pattern, seen := range want {
		assert.Truef(t, seen, "manifest is missing cap %q", pattern)
	}
}
