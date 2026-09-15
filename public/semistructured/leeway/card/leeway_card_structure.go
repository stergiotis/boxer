package card

import (
	"hash/fnv"
)

// StructureDims is the default width of a structure vector.
const StructureDims = 128

// StructureMatrix turns item sets into a row-major float32 matrix of dims
// columns: each entity's structural items — its sections, co-section
// groups and memberships, never its values — are feature-hashed
// (Weinberger et al. 2009) into a signed presence vector: the item's
// name picks a column and a sign, and the column accumulates the sign.
// Two entities with the same sections and attribute names get the same
// row whatever their values or sizes, which is what a distance over the
// rows should mean when the question is what kind of record this is; the
// sixteen shape features of EntityFeatures answer how big and how skewed
// it is instead. Cosine is the distance the vectors are meant for, since
// the sign trick makes collisions cancel in expectation rather than pile
// up. Deterministic: the hash is FNV-1a over the item name.
func StructureMatrix(sets ItemSets, dims int) (x []float32) {
	if dims <= 0 {
		dims = StructureDims
	}
	col := make([]int, len(sets.Items))
	sign := make([]float32, len(sets.Items))
	for i, it := range sets.Items {
		col[i] = -1
		switch it.Kind {
		case ItemKindSection, ItemKindCoGroup, ItemKindTagRef, ItemKindTagVerbatim:
		default:
			continue
		}
		h := fnv.New64a()
		_, _ = h.Write([]byte(it.Name))
		v := h.Sum64()
		col[i] = int(v % uint64(dims))
		sign[i] = 1
		if (v>>63)&1 == 1 {
			sign[i] = -1
		}
	}
	x = make([]float32, len(sets.Rows)*dims)
	for r, row := range sets.Rows {
		base := r * dims
		for _, it := range row {
			if c := col[it]; c >= 0 {
				x[base+c] += sign[it]
			}
		}
	}
	return
}
