package implot

import "testing"

// A released plot loses its retained state and nothing else's: a notebook
// that drops one cell's plot must not reset its neighbours' pans.
func TestReleaseDropsOnlyThatPlot(t *testing.T) {
	const kept, dropped = uint64(0x5eed_0001), uint64(0x5eed_0002)
	pool[kept] = &plotState{hidden: map[string]bool{}}
	pool[dropped] = &plotState{hidden: map[string]bool{}}
	t.Cleanup(func() { delete(pool, kept); delete(pool, dropped) })

	Release(dropped)

	if _, ok := pool[dropped]; ok {
		t.Error("the released plot's state is still retained")
	}
	if _, ok := pool[kept]; !ok {
		t.Error("releasing one plot dropped another's state")
	}
	Release(dropped)
}
