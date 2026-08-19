package runtime

import (
	"bytes"
	"encoding/binary"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFffiCapture is a stub FffiCaptureI for tests that exercise
// DeferredBlockScope's Begin/End contract without standing up a real
// Fffi2 instance. It records BeginCapture / EndCapture / AppendRaw
// calls so tests can assert the routing invariants the scope relies
// on (specifically: ReleaseWithHint must restore Fffi2 routing if
// End() was skipped).
type fakeFffiCapture struct {
	captureBuf  *bytes.Buffer
	beginCalls  int
	endCalls    int
	appendCalls int
}

func (f *fakeFffiCapture) BeginCapture(buf *bytes.Buffer, _ binary.ByteOrder) {
	f.captureBuf = buf
	f.beginCalls++
}

func (f *fakeFffiCapture) EndCapture() {
	f.captureBuf = nil
	f.endCalls++
}

func (f *fakeFffiCapture) AppendRawToCapture(raw []byte) {
	if f.captureBuf != nil {
		f.captureBuf.Write(raw)
	}
	f.appendCalls++
}

func (f *fakeFffiCapture) IsCapturing() bool {
	return f.captureBuf != nil
}

// freshHint returns a *ScopeHint the test owns — including its own pool,
// so recycling in one test cannot hand a scope to another. Avoids the
// process-wide RegisterScopeHint registry so tests stay isolated from
// each other and from any binding-package init that may have already
// registered the same name.
func freshHint() *ScopeHint {
	return &ScopeHint{}
}

func TestRegisterScopeHint_DedupsByName(t *testing.T) {
	a := RegisterScopeHint("TestRegisterScopeHintDedup")
	b := RegisterScopeHint("TestRegisterScopeHintDedup")
	assert.Same(t, a, b, "second call with the same kind must return the same *ScopeHint")
}

func TestRegisterScopeHint_DistinctNamesAreDistinct(t *testing.T) {
	a := RegisterScopeHint("TestRegisterScopeHintDistinctA")
	b := RegisterScopeHint("TestRegisterScopeHintDistinctB")
	assert.NotSame(t, a, b, "distinct kind names must produce distinct atomics")
}

func TestRegisterScopeHint_ConcurrentSameNameStillDedups(t *testing.T) {
	const goroutines = 32
	const kind = "TestRegisterScopeHintConcurrent"
	var wg sync.WaitGroup
	wg.Add(goroutines)
	got := make([]*ScopeHint, goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			got[idx] = RegisterScopeHint(kind)
		}(i)
	}
	wg.Wait()
	for i := 1; i < goroutines; i++ {
		assert.Same(t, got[0], got[i], "all concurrent callers must observe the same hint pointer")
	}
}

func TestScopeHintsSnapshot_ContainsRegisteredKinds(t *testing.T) {
	h := RegisterScopeHint("TestScopeHintsSnapshotKind")
	h.hint.Store(12345)
	snap := ScopeHintsSnapshot()
	var found bool
	for _, s := range snap {
		if s.Kind == "TestScopeHintsSnapshotKind" {
			assert.Equal(t, uint64(12345), s.Bytes)
			found = true
		}
	}
	assert.True(t, found, "snapshot must include registered kind")
}

func TestScopeHintsSnapshot_IsSortedByKind(t *testing.T) {
	// Register three deterministically-ordered names. The snapshot must
	// return them in lexical order regardless of registration order so
	// downstream consumers (overlay rows, log lines, regression tests)
	// see a stable shape.
	RegisterScopeHint("TestScopeHintsSnapshotSortB")
	RegisterScopeHint("TestScopeHintsSnapshotSortA")
	RegisterScopeHint("TestScopeHintsSnapshotSortC")
	snap := ScopeHintsSnapshot()
	var prev string
	for _, s := range snap {
		assert.LessOrEqual(t, prev, s.Kind, "snapshot must be kind-asc sorted")
		prev = s.Kind
	}
}

func TestNewDeferredBlockScopeHinted_NilHintFallsBackToFloor(t *testing.T) {
	s := NewDeferredBlockScopeHinted(
		func() FffiCaptureI { return &fakeFffiCapture{} },
		binary.LittleEndian,
		nil,
	)
	assert.GreaterOrEqual(t, s.dataBuf.Cap(), deferredDataBufFloor,
		"nil hint must give at least the floor capacity")
	assert.Equal(t, 0, s.dataBuf.Len(), "fresh scope's dataBuf must be empty")
}

func TestNewDeferredBlockScopeHinted_SmallHintRoundsUpToFloor(t *testing.T) {
	hint := freshHint()
	hint.hint.Store(64) // below the 4 KiB floor
	s := NewDeferredBlockScopeHinted(
		func() FffiCaptureI { return &fakeFffiCapture{} },
		binary.LittleEndian,
		hint,
	)
	assert.GreaterOrEqual(t, s.dataBuf.Cap(), deferredDataBufFloor,
		"hint below the floor must be clamped up to the floor — keeps tiny scopes from re-growing on the first byte")
}

func TestNewDeferredBlockScopeHinted_LargeHintPresizesAccordingly(t *testing.T) {
	const want = 128 * 1024
	hint := freshHint()
	hint.hint.Store(want)
	s := NewDeferredBlockScopeHinted(
		func() FffiCaptureI { return &fakeFffiCapture{} },
		binary.LittleEndian,
		hint,
	)
	assert.GreaterOrEqual(t, s.dataBuf.Cap(), want,
		"hint above the floor must size dataBuf to at least the hint")
}

func TestReleaseWithHint_RatchetsUpOnOvershoot(t *testing.T) {
	hint := freshHint()
	hint.hint.Store(1000)
	s := NewDeferredBlockScopeHinted(
		func() FffiCaptureI { return &fakeFffiCapture{} },
		binary.LittleEndian,
		hint,
	)
	// Simulate a frame that wrote 5000 bytes into the slab.
	s.dataBuf.Write(make([]byte, 5000))
	s.ReleaseWithHint()
	assert.Equal(t, uint64(5000), hint.hint.Load(),
		"observed > old must ratchet the hint up to the observed high-water immediately")
}

func TestReleaseWithHint_DecaysSlowlyOnUndershoot(t *testing.T) {
	hint := freshHint()
	hint.hint.Store(32000)
	s := NewDeferredBlockScopeHinted(
		func() FffiCaptureI { return &fakeFffiCapture{} },
		binary.LittleEndian,
		hint,
	)
	// Observed undershoots the current hint — apply one decay step.
	// With deferredHintDecayShift=5: next = 32000 - ((32000-0) >> 5) = 32000 - 1000 = 31000.
	s.dataBuf.Write(make([]byte, 0))
	s.ReleaseWithHint()
	assert.Equal(t, uint64(31000), hint.hint.Load(),
		"observed < old must decay by (old-observed)>>decayShift, not snap down")
}

func TestReleaseWithHint_ConvergesAcrossManyFrames(t *testing.T) {
	hint := freshHint()
	hint.hint.Store(100000)
	// 200 frames each observing 1000 bytes: hint should drift from
	// 100000 toward 1000. Half-life at shift=5 is ~22 frames, so 200
	// frames is plenty of headroom.
	for range 200 {
		s := NewDeferredBlockScopeHinted(
			func() FffiCaptureI { return &fakeFffiCapture{} },
			binary.LittleEndian,
			hint,
		)
		s.dataBuf.Write(make([]byte, 1000))
		s.ReleaseWithHint()
	}
	got := hint.hint.Load()
	assert.Less(t, got, uint64(1500),
		"after 200 frames of observed=1000 the hint should converge near 1000, got %d", got)
}

func TestReleaseWithHint_RecyclesWithinCeiling(t *testing.T) {
	hint := freshHint()
	s := NewDeferredBlockScopeHinted(
		func() FffiCaptureI { return &fakeFffiCapture{} },
		binary.LittleEndian,
		hint,
	)
	s.dataBuf.Write(make([]byte, 8*1024))
	grown := s.dataBuf.Cap()
	s.ReleaseWithHint()

	// Recycled, not dropped: the buffers survive so the next scope of this
	// kind does not re-allocate. Emptied, so it cannot leak one frame's
	// bytes into the next.
	require.NotNil(t, s.dataBuf, "a scope within the ceiling must keep its buffers for reuse")
	require.NotNil(t, s.tempBuf)
	assert.Zero(t, s.dataBuf.Len(), "a recycled scope must be emptied")
	assert.Zero(t, s.tempBuf.Len())
	assert.Zero(t, len(s.entries))
	assert.Equal(t, grown, s.dataBuf.Cap(), "the grown capacity is what makes recycling worth it")
	assert.Nil(t, s.getFffi, "Release must drop the getFffi closure reference")
	assert.Nil(t, s.scope, "Release must drop the hint pointer (prevents accidental re-fold)")
	assert.True(t, s.released, "a recycled scope must be marked released so use-after-release is loud")
}

// TestReleaseWithHint_DropsOverCeiling pins the golang/go#23199 guard: one
// outlier frame must not pin an outsized buffer in the pool for the process
// lifetime. Asserting the drop predicate rather than a Put/Get round-trip
// keeps this deterministic — sync.Pool may discard entries at any GC.
func TestReleaseWithHint_DropsOverCeiling(t *testing.T) {
	hint := freshHint()
	s := NewDeferredBlockScopeHinted(
		func() FffiCaptureI { return &fakeFffiCapture{} },
		binary.LittleEndian,
		hint,
	)
	s.dataBuf.Grow(deferredPooledBufferCeiling + 1)
	require.Greater(t, s.dataBuf.Cap(), deferredPooledBufferCeiling)
	s.ReleaseWithHint()
	assert.Nil(t, s.dataBuf, "a buffer past the ceiling must be dropped for the collector, not pooled")
	assert.Nil(t, s.tempBuf)
	assert.True(t, s.released)
}

func TestUseAfterRelease_Panics(t *testing.T) {
	hint := freshHint()
	s := NewDeferredBlockScopeHinted(
		func() FffiCaptureI { return &fakeFffiCapture{} },
		binary.LittleEndian,
		hint,
	)
	s.ReleaseWithHint()
	assert.Panics(t, func() { s.Begin(uint64(1)) },
		"Begin after release must panic — the scope may already belong to another frame")
	assert.Panics(t, func() { s.End() },
		"End after release must panic for the same reason")
}

func TestReleaseWithHint_RestoresCaptureIfEndForgotten(t *testing.T) {
	fake := &fakeFffiCapture{}
	hint := freshHint()
	s := NewDeferredBlockScopeHinted(
		func() FffiCaptureI { return fake },
		binary.LittleEndian,
		hint,
	)
	// Simulate a caller that forgot End(): start a Begin but never close.
	s.Begin(uint64(1))
	require.True(t, s.capturing, "Begin must set capturing=true")
	require.Equal(t, 1, fake.beginCalls)
	require.Equal(t, 0, fake.endCalls)

	s.ReleaseWithHint()

	assert.Equal(t, 1, fake.endCalls,
		"Release on a still-capturing scope must call EndCapture to restore Fffi2 routing before handing buffers to GC")
}

func TestReleaseWithHint_NoHintIsSafe(t *testing.T) {
	// A scope built via the legacy NewDeferredBlockScope has scope == nil, so
	// there is no pool to recycle into; Release must collapse to a clean
	// teardown with no panic.
	s := NewDeferredBlockScope(func() FffiCaptureI { return &fakeFffiCapture{} }, binary.LittleEndian)
	s.dataBuf.Write(make([]byte, 256))
	require.NotPanics(t, func() { s.ReleaseWithHint() })
	assert.Nil(t, s.dataBuf)
	assert.Nil(t, s.scope)
}
