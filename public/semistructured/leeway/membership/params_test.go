package membership

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParamsCanonicalForm(t *testing.T) {
	for _, tc := range []struct {
		name string
		idx  []uint64
		want string
	}{
		{"no elided index", nil, ""},
		{"first element", []uint64{0}, "0000"},
		{"single index", []uint64{1}, "0001"},
		{"two levels", []uint64{12, 3}, "000c.0003"},
		{"widest index", []uint64{MaxParamsIndex}, "ffff"},
		{"three levels", []uint64{1, 0, 255}, "0001.0000.00ff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := EncodeParams(tc.idx...)
			require.NoError(t, err)
			require.Equal(t, tc.want, string(raw))

			back, err := DecodeParams(raw)
			require.NoError(t, err)
			require.Equal(t, tc.idx, back)

			require.Equal(t, len(tc.idx), ParamsArity(raw),
				"arity must be readable from the length alone")
		})
	}
}

// The fixed width is what buys ordering: a SQL range predicate over the blob
// must agree with a range predicate over the decoded indices, or the format's
// stated property is false.
func TestParamsLexicographicOrderIsNumeric(t *testing.T) {
	var prev string
	for i := range uint64(4096) {
		raw, err := EncodeParams(i)
		require.NoError(t, err)
		if i > 0 {
			require.Less(t, prev, string(raw), "index %d must sort after %d", i, i-1)
		}
		prev = string(raw)
	}
}

func TestParamsIndexOutOfRange(t *testing.T) {
	_, err := EncodeParams(MaxParamsIndex + 1)
	require.ErrorIs(t, err, ErrParamsIndexOutOfRange)

	// A rejected tuple must not append a partial encoding, or a scratch buffer
	// carries half an index into the next attribute.
	dst := []byte("keep")
	out, err := AppendParams(dst, 1, MaxParamsIndex+1)
	require.ErrorIs(t, err, ErrParamsIndexOutOfRange)
	require.Nil(t, out)
	require.Equal(t, "keep", string(dst))
}

func TestParamsDecodeRejectsMalformed(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"short index", "001"},
		{"unpadded", "1"},
		{"wrong separator", "0001,0002"},
		{"trailing separator", "0001."},
		{"non-hex digit", "00g1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, err := DecodeParams([]byte(tc.raw))
			require.ErrorIs(t, err, ErrParamsMalformed)
			require.Nil(t, idx)
		})
	}
}

func TestParamsDecodeAcceptsUpperCase(t *testing.T) {
	idx, err := DecodeParams([]byte("000C.00FF"))
	require.NoError(t, err)
	require.Equal(t, []uint64{12, 255}, idx)
}

// The default renderer is `string(raw)` (ADR-0072's representation plane), so
// the encoding has to survive it unchanged.
func TestParamsRenderThroughDefaultFormatter(t *testing.T) {
	raw, err := EncodeParams(12, 3)
	require.NoError(t, err)
	require.Equal(t, "000c.0003", DefaultRenderer().RenderParams(string(raw)))
}
