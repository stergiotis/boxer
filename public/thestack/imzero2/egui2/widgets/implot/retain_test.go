package implot

import (
	"strconv"
	"testing"
)

// A plot or grid no frame has drawn for retainFrames is dropped at the next
// sweep; one drawn since stays.
func TestSweepDropsStateNoFrameTouched(t *testing.T) {
	const stale, fresh = uint64(0x5eed_1001), uint64(0x5eed_1002)
	prevSweep := lastSweep
	t.Cleanup(func() {
		delete(pool, stale)
		delete(pool, fresh)
		delete(subplotPool, stale)
		lastSweep = prevSweep
	})
	lastSweep = 0
	now := uint64(retainFrames + sweepEvery + 10)
	pool[stale] = &plotState{lastSeen: 1}
	pool[fresh] = &plotState{lastSeen: now - 1}
	subplotPool[stale] = &subplotState{lastSeen: 1}

	sweepPools(sweepEvery - 1)
	if _, ok := pool[stale]; !ok {
		t.Fatal("swept before sweepEvery frames passed")
	}
	sweepPools(now)
	if _, ok := pool[stale]; ok {
		t.Error("a plot no frame drew for retainFrames is still retained")
	}
	if _, ok := subplotPool[stale]; ok {
		t.Error("a grid no frame drew for retainFrames is still retained")
	}
	if _, ok := pool[fresh]; !ok {
		t.Error("a plot drawn recently was dropped")
	}
}

// A heatmap whose label changes every frame keeps the cache bounded; the
// textures one frame draws are all kept.
func TestHeatTextureCacheIsBounded(t *testing.T) {
	st := &plotState{}
	for frame := range 100 {
		st.begins++
		st.heatTexFor("heat-" + strconv.Itoa(frame))
	}
	if n := len(st.heatCache); n > maxHeatTextures {
		t.Errorf("cache holds %d textures, bound is %d", n, maxHeatTextures)
	}
	st.begins++
	for i := range 2 * maxHeatTextures {
		st.heatTexFor("same-frame-" + strconv.Itoa(i))
	}
	for i := range 2 * maxHeatTextures {
		if _, ok := st.heatCache["same-frame-"+strconv.Itoa(i)]; !ok {
			t.Fatalf("a texture drawn this frame was dropped: %d", i)
		}
	}
}
