package play

import (
	"bytes"
	"image"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imagedecode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ADR-0245 §SD7: the fixture's rows are what their notes say they are — the
// awkward ones in particular, since a fixture that stopped being awkward
// would stop testing anything.
func TestCardgridFixtureRows(t *testing.T) {
	rows, err := cardgridFixtureRows()
	require.NoError(t, err)
	again, err := cardgridFixtureRows()
	require.NoError(t, err)
	require.Equal(t, cardgridFixtureNames(rows), cardgridFixtureNames(again))
	byName := make(map[string]cardgridFixtureRow, len(rows))
	for i, r := range rows {
		byName[r.name] = r
		assert.Equal(t, r.content, again[i].content, "%s is not deterministic", r.name)
	}

	for _, name := range []string{"Harbour at dusk", "Panorama", "Strip", "Icon", "Large photograph", "A JPEG among the PNGs", "Animated GIF"} {
		r := byName[name]
		cfg, _, cErr := image.DecodeConfig(bytes.NewReader(r.content))
		require.NoError(t, cErr, name)
		assert.Equal(t, r.width, int64(cfg.Width), name)
		assert.Equal(t, r.height, int64(cfg.Height), name)
	}
	_, err = imagedecode.DecodeThumbnailRGBA8(byName["Over the pixel budget"].content, richMaxImagePixels, cardgridThumbMaxSide)
	assert.ErrorContains(t, err, "pixel budget", "refused from the header")
	assert.Less(t, len(byName["Over the pixel budget"].content), 64, "and the file is only a header")
	_, err = imagedecode.DecodeThumbnailRGBA8(byName["Truncated PNG"].content, richMaxImagePixels, cardgridThumbMaxSide)
	assert.Error(t, err)
	assert.Nil(t, byName["No hero on this card"].content)
	assert.Equal(t, "image/pgn", byName["Misspelt media type"].mime)

	for _, name := range []string{"Sine sweep", "Gated tone, stereo", "Silence", "Eight-bit telephone tone", "Float samples"} {
		r := byName[name]
		f, wErr := wavfile.NewReaderE(bytes.NewReader(r.content), int64(len(r.content)))
		require.NoError(t, wErr, name)
		assert.Equal(t, r.sampleRate, int64(f.Format().SampleRate), name)
		assert.Equal(t, r.channels, int64(f.Format().Channels), name)
		assert.InDelta(t, r.lengthMS, f.Format().FramesToDuration(f.Frames()).Milliseconds(), 1, name)
		assert.False(t, f.IsTruncated(), name)
	}
}

// The published stream reads back as the scaffold's columns, and folds.
func TestCardgridFixtureEncodesAndFolds(t *testing.T) {
	rows, err := cardgridFixtureRows()
	require.NoError(t, err)
	data, err := encodeCardgridFixture(rows, memory.NewGoAllocator())
	require.NoError(t, err)
	rd, err := ipc.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer rd.Release()
	require.True(t, rd.Next())
	rec := rd.RecordBatch()
	assert.Equal(t, int64(len(rows)), rec.NumRows())
	for _, col := range []string{"name", "kind", "content", "mime", "note", "tags", "tone", "recorded", "bytes", "length_ms", "width", "height", "sample_rate", "channels"} {
		assert.NotEmpty(t, rec.Schema().FieldIndices(col), col)
	}
}
