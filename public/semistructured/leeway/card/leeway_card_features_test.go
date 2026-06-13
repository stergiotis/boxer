package card

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// Regression for review G-1: the FeatureExtractor's zstd-backed
// compressionRatio always returned 0 because zstd.NewWriter captured a value
// copy of the SizeMeasureWriter (the encoder wrote to the copy; the struct
// read its own never-written field). F13/F14 were permanently zero, and
// PreprocessFeatureMatrix then dropped them as constant columns — UMAP silently
// ran on a 14-dim space. The fix stores a *SizeMeasureWriter and resets/closes
// the encoder around each measurement.

func TestFeatureExtractorCompressionRatioNonZero(t *testing.T) {
	fe, err := NewFeatureExtractor()
	require.NoError(t, err)

	// Highly compressible: 12 KiB of a single byte (the review's probe).
	data := bytes.Repeat([]byte("a"), 12*1024)
	ratio := fe.compressionRatio(data)
	require.Greater(t, ratio, 0.0, "compression ratio of a non-empty buffer must be > 0")
	require.Less(t, ratio, 1.0, "highly compressible data must shrink (ratio < 1)")
}

func TestFeatureExtractorCompressionRatioRepeatable(t *testing.T) {
	fe, err := NewFeatureExtractor()
	require.NoError(t, err)

	// computeFeatures calls compressionRatio twice per entity (topology then
	// value) and the extractor is reused across entities — the measurement
	// must be self-contained and repeatable, not leak state between calls.
	data := bytes.Repeat([]byte("abcd"), 4*1024)
	r1 := fe.compressionRatio(data)
	r2 := fe.compressionRatio(data)
	require.Greater(t, r1, 0.0)
	require.InDelta(t, r1, r2, 1e-9, "same input must yield the same ratio across calls")

	// An empty buffer is defined to return 0.
	require.Equal(t, 0.0, fe.compressionRatio(nil))
}

func TestComputeFeaturesCompressionRatiosNonZero(t *testing.T) {
	fe, err := NewFeatureExtractor()
	require.NoError(t, err)

	// White-box: populate the per-entity buffers computeFeatures reads, then
	// assert F13/F14 are actually computed rather than stuck at 0.
	fe.topoBuf = bytes.Repeat([]byte{0x01, 0x02, 0x03}, 4096)
	fe.valueBuf = bytes.Repeat([]byte("hello world"), 2048)

	feat := fe.computeFeatures()
	require.Greater(t, feat.TopologyCompressionRatio, 0.0, "F13 (topology) must be non-zero for a non-empty buffer")
	require.Greater(t, feat.ValueCompressionRatio, 0.0, "F14 (value) must be non-zero for a non-empty buffer")
}
