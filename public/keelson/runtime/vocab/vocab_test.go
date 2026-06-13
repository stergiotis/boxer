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
