package jackstay

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

// transitive checks that cmp orders every triple of ids consistently.
func transitive(t *testing.T, cmp func(a string, b string) int, ids []string) {
	t.Helper()
	for _, a := range ids {
		for _, b := range ids {
			assert.Equal(t, -sign(cmp(b, a)), sign(cmp(a, b)), "antisymmetric: %q %q", a, b)
			for _, c := range ids {
				if cmp(a, b) < 0 && cmp(b, c) < 0 {
					assert.Negative(t, cmp(a, c), "transitive: %q < %q < %q", a, b, c)
				}
			}
		}
	}
}

func sign(r int) (s int) {
	return min(max(r, -1), 1)
}

func TestCompareChunkIds_ByKind(t *testing.T) {
	// Hex partition ids that read as decimal indexes: compared by shape,
	// "99" < "1000" numerically, "1000" < "100A" and "100A" < "99" as text.
	ids := []string{"99", "1000", "100A", "0", "FF", "1"}
	part := &Chunking{Kind: ChunkingPartition}
	transitive(t, part.compareChunkIds, ids)
	sorted := slices.Clone(ids)
	slices.SortFunc(sorted, part.compareChunkIds)
	assert.Equal(t, []string{"0", "1", "1000", "100A", "99", "FF"}, sorted, "partition ids order as strings")

	rng := &Chunking{Kind: ChunkingRange}
	transitive(t, rng.compareChunkIds, ids)
	sorted = []string{"10", "9", "x", "0", "2"}
	slices.SortFunc(sorted, rng.compareChunkIds)
	assert.Equal(t, []string{"0", "2", "9", "10", "x"}, sorted, "range indexes numerically, anything else after")
}
