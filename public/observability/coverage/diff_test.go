package coverage

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
)

// A function that ran before and ran again, one that ran for the first
// time, and one untouched: only the first two contribute, and only the
// units whose counter moved.
func TestDiffCountersChangedUnitsOnly(t *testing.T) {
	meta := synthMeta()
	before := synthSnap(meta,
		covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 0, Counters: []uint32{5, 0, 1}},    // A: g0, g2 covered
		covsnap.FuncCounters{PkgIdx: 1, FuncIdx: 0, Counters: []uint32{1, 1, 1, 1}}, // C: all covered
	)
	after := synthSnap(meta,
		covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 0, Counters: []uint32{6, 0, 1}},    // A: g0 moved again
		covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 1, Counters: []uint32{2, 0}},       // B: new, g3 ran
		covsnap.FuncCounters{PkgIdx: 1, FuncIdx: 0, Counters: []uint32{1, 1, 1, 1}}, // C: untouched
	)
	d, err := DiffCounters(meta, before, after)
	require.NoError(t, err)
	require.Equal(t, []uint32{0, 3}, d.Units.ToArray())
	require.Equal(t, uint32(2), d.Funcs)
}

func TestDiffCountersEmptyWhenNothingMoved(t *testing.T) {
	meta := synthMeta()
	snap := synthSnap(meta, covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 0, Counters: []uint32{5, 0, 1}})
	d, err := DiffCounters(meta, snap, snap)
	require.NoError(t, err)
	require.True(t, d.Units.IsEmpty())
	require.Zero(t, d.Funcs)
}

func TestDiffCountersRejectsForeignSnapshots(t *testing.T) {
	meta := synthMeta()
	foreign := &covsnap.CounterSnapshot{MetaHash: [16]byte{0x01}}
	_, err := DiffCounters(meta, foreign, synthSnap(meta))
	require.Error(t, err)
	_, err = DiffCounters(meta, synthSnap(meta), foreign)
	require.Error(t, err)
}

// ReadCounters fails cleanly in a test binary, which is not an atomic-mode
// cover build.
func TestReadCountersErrsInTestBinaries(t *testing.T) {
	var buf bytes.Buffer
	_, err := ReadCounters(&buf)
	require.Error(t, err)
}

// BenchmarkReadCounters is ADR-0343 §SD7's figure: one live counter read
// and decode, the cost a bracket pays twice per agent command. It only
// measures on an instrumented binary:
//
//	go test -cover -covermode=atomic -run XXX -bench ReadCounters ./public/observability/coverage/
func BenchmarkReadCounters(b *testing.B) {
	if err := ProbeRuntimeSupport(); err != nil {
		b.Skip("needs -cover -covermode=atomic:", err)
	}
	var buf bytes.Buffer
	b.ReportAllocs()
	for b.Loop() {
		snap, err := ReadCounters(&buf)
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(buf.Len()))
		_ = snap
	}
}
