package vocab

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllMembs_Unique(t *testing.T) {
	seen := make(map[uint64]struct{}, len(AllMembs))
	for i, m := range AllMembs {
		id := m.GetId().Value()
		_, dup := seen[id]
		assert.False(t, dup, "duplicate id %d at index %d", id, i)
		seen[id] = struct{}{}
	}
	assert.Len(t, seen, len(AllMembs))
}

func TestMembRuntimeApp_StableId(t *testing.T) {
	// Repeated lookup must return the same id — the constants are package
	// globals registered at init time; downstream code stores them in
	// long-lived rows and must rely on stable mapping.
	a := MembRuntimeApp.GetId().Value()
	b := MembRuntimeApp.GetId().Value()
	assert.Equal(t, a, b)
}

// AllMembs lists every membership NkRegistry registers: a membership left
// out would escape the invariants the tests above hold and the
// cross-vocabulary disjointness check that iterates this list.
func TestAllMembsCoversTheRegistry(t *testing.T) {
	registered := map[uint64]string{}
	for name, nk := range NkRegistry.IterateAll() {
		registered[nk.GetId().Value()] = name.String()
	}
	listed := map[uint64]struct{}{}
	for _, m := range AllMembs {
		id := m.GetId().Value()
		_, known := registered[id]
		assert.True(t, known, "AllMembs lists id %d, which NkRegistry does not register", id)
		listed[id] = struct{}{}
	}
	for id, name := range registered {
		_, ok := listed[id]
		assert.True(t, ok, "NkRegistry registers %s (id %d), which AllMembs leaves out", name, id)
	}
}
