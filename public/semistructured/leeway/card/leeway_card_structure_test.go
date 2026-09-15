package card

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStructureMatrixIdentityNotValues(t *testing.T) {
	ie := driveItems(t, []itemEntity{
		{sections: []itemSection{{name: "geo", column: "lat", values: []string{"1", "2"}, refs: []uint64{7}, numeric: true}}},
		{sections: []itemSection{{name: "geo", column: "lat", values: []string{"3"}, refs: []uint64{7}, numeric: true}}},
		{sections: []itemSection{{name: "geo", column: "lat", values: []string{"3"}, refs: []uint64{7, 8}, numeric: true}}},
		{sections: []itemSection{{name: "sym", column: "value", values: []string{"a"}}}},
	})
	sets := ie.Results()
	const dims = 32
	x := StructureMatrix(sets, dims)
	require.Len(t, x, 4*dims)
	row := func(r int) []float32 { return x[r*dims : (r+1)*dims] }
	require.Equal(t, row(0), row(1), "same sections and tags, different values: the same row")
	require.NotEqual(t, row(1), row(2), "one more tag: a different row")
	require.NotEqual(t, row(0), row(3))
	// Every row is non-zero (it has at least its section) and made of
	// signed unit steps.
	for r := range 4 {
		nz := 0
		for _, v := range row(r) {
			if v != 0 {
				nz++
				require.Equal(t, float32(int(v)), v)
			}
		}
		require.Greater(t, nz, 0)
	}
	// Deterministic.
	require.Equal(t, x, StructureMatrix(sets, dims))
	require.Len(t, StructureMatrix(sets, 0), 4*StructureDims)
}
