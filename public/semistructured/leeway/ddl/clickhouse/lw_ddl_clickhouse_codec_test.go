package clickhouse

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	encodingaspects "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stretchr/testify/require"
)

func codecFor(t *testing.T, ct canonicaltypes.PrimitiveAstNodeI, list bool, aspects ...encodingaspects.AspectE) string {
	t.Helper()
	gen := NewTechnologySpecificCodeGenerator()
	b := &strings.Builder{}
	gen.SetCodeBuilder(b)
	hints := encodingaspects.EmptyAspectSet
	if len(aspects) > 0 {
		var err error
		hints, err = encodingaspects.EncodeAspects(aspects...)
		require.NoError(t, err)
	}
	require.NoError(t, gen.generateTypeAndCodec(ct, hints, list))
	return b.String()
}

var (
	u64ct = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 64}
	f64ct = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericFloat, Width: 64}
)

// Regression for review D-3: a hint-less column emitted an explicit CODEC(NONE),
// disabling the server-default compression; and a transform-only chain emitted
// CODEC(Delta,NONE), which default ClickHouse rejects. A hint-less column must
// now emit no CODEC clause, and a transform must be followed by a real compressor.
func TestCodec_NoUnconditionalNone(t *testing.T) {
	// Hint-less: no CODEC clause at all (inherits server default).
	require.NotContains(t, codecFor(t, u64ct, false), "CODEC", "hint-less column must not emit a CODEC clause")

	// Transform-only (Delta, no compression aspect): must get a real compressor,
	// never NONE.
	out := codecFor(t, u64ct, false, encodingaspects.AspectDeltaEncoding)
	require.Contains(t, out, "Delta")
	require.NotContains(t, out, "NONE", "a transform chain must not append NONE")
	require.Contains(t, out, "LZ4", "a transform-only chain must be followed by a real compressor")
}

// Regression for review D-4: Gorilla/FPC are float-only and T64 is integer-only;
// applying them to the wrong type produces DDL ClickHouse rejects. The generator
// must skip the inapplicable codec rather than emit it.
func TestCodec_HintTypeGating(t *testing.T) {
	// SlowlyChangingFloat on an integer column → no Gorilla/FPC.
	intFloatHint := codecFor(t, u64ct, false, encodingaspects.AspectLightSlowlyChangingFloat)
	require.NotContains(t, intFloatHint, "Gorilla")
	require.NotContains(t, intFloatHint, "FPC")

	// BiasSmallInteger on a float column → no T64.
	floatBiasHint := codecFor(t, f64ct, false, encodingaspects.AspectLightBiasSmallInteger)
	require.NotContains(t, floatBiasHint, "T64")

	// The matching combinations still emit their codec.
	require.Contains(t, codecFor(t, f64ct, false, encodingaspects.AspectLightSlowlyChangingFloat), "FPC")
	require.Contains(t, codecFor(t, u64ct, false, encodingaspects.AspectLightBiasSmallInteger), "T64")
}
