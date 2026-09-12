package card

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"gonum.org/v1/gonum/mat"
)

// NumFeatures is the dimensionality of EntityFeatures.
const NumFeatures = 16

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
		err = eb.Build().Int("want", NumFeatures).Int("got", nCols).Errorf("unexpected feature column count")
		return
	}
	data := raw.Data
	stride := raw.Stride

	col := make([]float64, nRows)

	for fi := range int32(NumFeatures) {
		doLog := LogTransformFeature[fi]

		for ri := range nRows {
			v := data[ri*stride+int(fi)]
			if math.IsNaN(v) || math.IsInf(v, 0) {
				err = eb.Build().Int32("feature", fi).Int("row", ri).Errorf("feature has a NaN or Inf value")
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
			for ri := range nRows {
				data[ri*stride+int(fi)] = 0
			}
			continue
		}
		invStd := 1.0 / std
		for ri := range nRows {
			data[ri*stride+int(fi)] = (col[ri] - mean) * invStd
		}
	}
	return
}
