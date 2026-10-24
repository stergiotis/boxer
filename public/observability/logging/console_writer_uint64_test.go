package logging

import (
	"bytes"
	"math"
	"strconv"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// Test_NewFormatWriter_consolePrintsUint64 guards the upper half of the
// uint64 range on the console format. Under binary_log, zerolog's own CBOR
// decoder reads every integer into an int64 (zerolog v1.35.1,
// internal/cbor/decode_stream.go), so an id at or above 2⁶³ printed as itself
// minus 2⁶⁴. The JSON build never had the fault; the test runs in both lanes
// and is the one that pins it in the binary_log lane.
func Test_NewFormatWriter_consolePrintsUint64(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewFormatWriter("console", &buf, true)
	require.NoError(t, err)
	logger := zerolog.New(w)

	for _, v := range []uint64{math.MaxInt64, math.MaxInt64 + 1, 13835058055282180229, math.MaxUint64} {
		buf.Reset()
		logger.Info().Uint64("id", v).Int64("neg", math.MinInt64).Msg("console")
		out := buf.String()
		require.Containsf(t, out, "id="+strconv.FormatUint(v, 10)+" ", "got: %q", out)
		require.Containsf(t, out, "neg="+strconv.FormatInt(math.MinInt64, 10), "got: %q", out)
	}
}
