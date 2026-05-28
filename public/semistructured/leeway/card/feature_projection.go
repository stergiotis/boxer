//go:build llm_generated_opus47

package card

import (
	"math"

	umap "github.com/nozzle/umap-go"
	"github.com/stergiotis/boxer/public/observability/eh"
	"gonum.org/v1/gonum/mat"
)

// NumFeatures is the dimensionality of EntityFeatures.
const NumFeatures = 16

// Default UMAP hyperparameters tuned for 1k–10k entity batches. These
// match the Python umap-learn defaults that nozzle/umap-go targets parity
// with — n_neighbors=15 trades local detail vs global structure, min_dist=0.1
// keeps clusters reasonably tight, n_epochs=0 lets the library auto-pick
// (500 for n<10k, 200 for larger).
const (
	DefaultUMAPNNeighbors = 15
	DefaultUMAPMinDist    = 0.1
	DefaultUMAPNEpochs    = 0 // 0 = auto (umap-learn convention)
	DefaultUMAPDimsOut    = 2
)

// LogTransformFeature flags features that are log1p-transformed before
// z-score standardisation. Right-skewed / unbounded features span orders
// of magnitude; bounded [0,1] features stay linear.
var LogTransformFeature = [NumFeatures]bool{
	true,  // 0:  F01 TotalAttributeCount
	true,  // 1:  F02 TotalValueBytes
	false, // 2:  F03 GiniAttrsPerSection      (bounded [0,1])
	true,  // 3:  F04 MaxToMeanAttrRatio       (≥1, can be very large)
	false, // 4:  F05 MeanTagsPerAttr
	true,  // 5:  F06 TagCountVariance
	false, // 6:  F07 UntaggedAttrFraction     (bounded [0,1])
	false, // 7:  F08 MembershipRoleEntropy
	false, // 8:  F09 NonScalarValueFraction   (bounded [0,1])
	true,  // 9:  F10 MeanNonScalarCard
	true,  // 10: F11 EffectiveSectionCount
	false, // 11: F12 CoGroupSectionFraction   (bounded [0,1])
	false, // 12: F13 TopologyCompressionRatio (bounded [0,1])
	false, // 13: F14 ValueCompressionRatio    (bounded [0,1])
	true,  // 14: F15 MeanValueLength
	false, // 15: F16 ValueRepetitionRatio     (bounded [0,1])
}

// UMAPOptions configures RunUMAP. Zero values pick sensible defaults.
type UMAPOptions struct {
	NNeighbors int32   // 0 → DefaultUMAPNNeighbors
	MinDist    float64 // 0 → DefaultUMAPMinDist
	NEpochs    int32   // 0 → auto (umap-learn convention)
	DimsOut    int32   // 0 → DefaultUMAPDimsOut
	Verbose    bool
}

// BuildFeatureMatrix copies features into a (nRows × NumFeatures) row-major
// dense matrix. Direct backing-slice writes avoid the per-row allocation that
// EntityFeatures.AsSlice would incur.
func BuildFeatureMatrix(features []EntityFeatures) (m *mat.Dense) {
	nRows := len(features)
	m = mat.NewDense(nRows, NumFeatures, nil)
	data := m.RawMatrix().Data
	for ri, f := range features {
		base := ri * NumFeatures
		data[base+0] = f.TotalAttributeCount
		data[base+1] = f.TotalValueBytes
		data[base+2] = f.GiniAttrsPerSection
		data[base+3] = f.MaxToMeanAttrRatio
		data[base+4] = f.MeanTagsPerAttr
		data[base+5] = f.TagCountVariance
		data[base+6] = f.UntaggedAttrFraction
		data[base+7] = f.MembershipRoleEntropy
		data[base+8] = f.NonScalarValueFraction
		data[base+9] = f.MeanNonScalarCard
		data[base+10] = f.EffectiveSectionCount
		data[base+11] = f.CoGroupSectionFraction
		data[base+12] = f.TopologyCompressionRatio
		data[base+13] = f.ValueCompressionRatio
		data[base+14] = f.MeanValueLength
		data[base+15] = f.ValueRepetitionRatio
	}
	return
}

// PreprocessFeatureMatrix applies log1p (per LogTransformFeature) then
// z-score standardises each column in-place. Constant columns (std < 1e-12)
// are zeroed out. Returns an error if any input value is NaN/Inf.
func PreprocessFeatureMatrix(m *mat.Dense) (err error) {
	raw := m.RawMatrix()
	nRows := raw.Rows
	nCols := raw.Cols
	if nCols != NumFeatures {
		err = eh.Errorf("expected %d feature columns, got %d", NumFeatures, nCols)
		return
	}
	data := raw.Data
	stride := raw.Stride

	col := make([]float64, nRows)

	for fi := int32(0); fi < int32(NumFeatures); fi++ {
		doLog := LogTransformFeature[fi]

		for ri := 0; ri < nRows; ri++ {
			v := data[ri*stride+int(fi)]
			if math.IsNaN(v) || math.IsInf(v, 0) {
				err = eh.Errorf("feature %d has NaN/Inf at row %d", fi, ri)
				return
			}
			if doLog {
				if v < 0 {
					v = 0
				}
				v = math.Log1p(v)
			}
			col[ri] = v
		}

		sum := 0.0
		for _, v := range col {
			sum += v
		}
		mean := sum / float64(nRows)

		variance := 0.0
		for _, v := range col {
			d := v - mean
			variance += d * d
		}
		std := math.Sqrt(variance / float64(nRows))

		if std < 1e-12 {
			for ri := 0; ri < nRows; ri++ {
				data[ri*stride+int(fi)] = 0
			}
			continue
		}
		invStd := 1.0 / std
		for ri := 0; ri < nRows; ri++ {
			data[ri*stride+int(fi)] = (col[ri] - mean) * invStd
		}
	}
	return
}

// RunUMAP projects an (nRows × NumFeatures) preprocessed matrix down to 2-D
// via UMAP. Returns an nRows-long slice of (x, y) pairs.
//
// Performance: nozzle/umap-go is pure Go with parallel workers; memory is
// O(nRows × NNeighbors), much cheaper than t-SNE's O(nRows²). Unlike t-SNE,
// UMAP has no per-epoch callback in the upstream library — callers wanting
// to reflect progress in a UI must rely on phase boundaries (extraction,
// preprocess, fit) and elapsed time, or vendor + patch the package.
//
// Below n=2 the result is all-zero coords (UMAP NNeighbors needs ≥2 points
// and the embedding is meaningless anyway). NNeighbors is clamped to nRows−1
// so small batches don't fail validation.
func RunUMAP(m *mat.Dense, opts UMAPOptions) (coords [][2]float64, err error) {
	nRows, _ := m.Dims()
	if nRows < 2 {
		coords = make([][2]float64, nRows)
		return
	}

	X := denseToRows(m)

	uopts := umap.DefaultOptions()
	if opts.NNeighbors > 0 {
		uopts.NNeighbors = int(opts.NNeighbors)
	}
	if uopts.NNeighbors > nRows-1 {
		uopts.NNeighbors = nRows - 1
	}
	if uopts.NNeighbors < 2 {
		uopts.NNeighbors = 2
	}
	if opts.MinDist > 0 {
		uopts.MinDist = opts.MinDist
	}
	if opts.NEpochs > 0 {
		uopts.NEpochs = int(opts.NEpochs)
	}
	if opts.DimsOut > 0 {
		uopts.NComponents = int(opts.DimsOut)
	}
	uopts.Verbose = opts.Verbose

	model := umap.New(uopts)
	emb, ferr := model.FitTransform(X, nil)
	if ferr != nil {
		err = eh.Errorf("umap fit: %w", ferr)
		return
	}
	if len(emb) != nRows {
		err = eh.Errorf("umap returned %d rows, expected %d", len(emb), nRows)
		return
	}

	dims := uopts.NComponents
	coords = make([][2]float64, nRows)
	for i := 0; i < nRows; i++ {
		row := emb[i]
		if len(row) < 2 || dims < 2 {
			coords[i] = [2]float64{row[0], 0}
			continue
		}
		coords[i] = [2]float64{row[0], row[1]}
	}
	return
}

// denseToRows reshapes a row-major mat.Dense into a slice of independent
// row slices, the input shape umap-go expects. Rows alias the dense's
// backing array via gonum's RawRowView, so this is allocation-free per row
// (only the outer slice header is allocated).
func denseToRows(m *mat.Dense) (rows [][]float64) {
	nRows, _ := m.Dims()
	rows = make([][]float64, nRows)
	for i := 0; i < nRows; i++ {
		rows[i] = m.RawRowView(i)
	}
	return
}
