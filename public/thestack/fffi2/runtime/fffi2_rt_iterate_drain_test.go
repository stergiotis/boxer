package runtime

// Regression suite for the Iterate*SliceRetr drain-on-early-termination
// off-by-one (found 2026-07-05 during the containers review): when the
// consumer stopped iteration at element k, the drain loop restarted at
// index k — whose wire bytes the aborted yield had already consumed —
// and thereby read one element past the slice payload, desynchronising
// the FFI channel. The tests pin wire alignment by writing a sentinel
// value directly after the slice and requiring it to read back intact
// after every termination pattern.

import (
	"iter"
	"testing"

	"github.com/stretchr/testify/require"
)

type iterDrainOps[T comparable] struct {
	name     string
	vals     []T // len 4, pairwise distinct
	sentinel T
	writeOne func(m *Marshaller, v T)
	readOne  func(u *Unmarshaller) T
	iterate  func(u *Unmarshaller) iter.Seq[T]
}

func runIterateDrainCases[T comparable](t *testing.T, ops iterDrainOps[T]) {
	t.Helper()
	require.Len(t, ops.vals, 4, "fixture invariant")

	type tc struct {
		name       string
		slice      []T
		nilSlice   bool
		breakAtIdx int // yield returns false at this index; -1 = consume fully
	}
	cases := []tc{
		{"break_at_first", ops.vals, false, 0},
		{"break_mid", ops.vals, false, 1},
		{"break_at_last", ops.vals, false, len(ops.vals) - 1},
		{"consume_fully", ops.vals, false, -1},
		{"empty_slice", nil, false, -1},
		{"nil_slice", nil, true, -1},
	}
	for _, c := range cases {
		t.Run(ops.name+"/"+c.name, func(t *testing.T) {
			m, u, _ := newTestPair()
			if c.nilSlice {
				m.WriteNilSlice()
			} else {
				m.WriteSliceLength(len(c.slice))
				for _, v := range c.slice {
					ops.writeOne(m, v)
				}
			}
			ops.writeOne(m, ops.sentinel)

			var got []T
			for v := range ops.iterate(u) {
				got = append(got, v)
				if c.breakAtIdx >= 0 && len(got) == c.breakAtIdx+1 {
					break
				}
			}

			wantYielded := c.slice
			if c.breakAtIdx >= 0 {
				wantYielded = c.slice[:c.breakAtIdx+1]
			}
			require.Equal(t, wantYielded, got, "yielded prefix")
			require.Equal(t, ops.sentinel, ops.readOne(u),
				"wire misaligned after the slice: the drain consumed the wrong number of elements")
		})
	}
}

func TestIterateSliceRetr_DrainKeepsWireAligned(t *testing.T) {
	runIterateDrainCases(t, iterDrainOps[uint64]{
		name:     "uint64",
		vals:     []uint64{10, 20, 30, 40},
		sentinel: 0xA5A5A5A5A5A5A5A5,
		writeOne: func(m *Marshaller, v uint64) { m.WriteUint64(v) },
		readOne:  func(u *Unmarshaller) uint64 { return u.ReadUInt64() },
		iterate: func(u *Unmarshaller) iter.Seq[uint64] {
			return IterateUint64SliceRetr[*Unmarshaller, uint64](u)
		},
	})
	runIterateDrainCases(t, iterDrainOps[uint32]{
		name:     "uint32",
		vals:     []uint32{11, 21, 31, 41},
		sentinel: 0xA5A5A5A5,
		writeOne: func(m *Marshaller, v uint32) { m.WriteUint32(v) },
		readOne:  func(u *Unmarshaller) uint32 { return u.ReadUInt32() },
		iterate: func(u *Unmarshaller) iter.Seq[uint32] {
			return IterateUint32SliceRetr[*Unmarshaller, uint32](u)
		},
	})
	runIterateDrainCases(t, iterDrainOps[float64]{
		name:     "float64",
		vals:     []float64{1.5, 2.5, 3.5, 4.5},
		sentinel: -123.25,
		writeOne: func(m *Marshaller, v float64) { m.WriteFloat64(v) },
		readOne:  func(u *Unmarshaller) float64 { return u.ReadFloat64() },
		iterate: func(u *Unmarshaller) iter.Seq[float64] {
			return IterateFloat64SliceRetr[*Unmarshaller, float64](u)
		},
	})
	runIterateDrainCases(t, iterDrainOps[float32]{
		name:     "float32",
		vals:     []float32{1.25, 2.25, 3.25, 4.25},
		sentinel: -321.5,
		writeOne: func(m *Marshaller, v float32) { m.WriteFloat32(v) },
		readOne:  func(u *Unmarshaller) float32 { return u.ReadFloat32() },
		iterate: func(u *Unmarshaller) iter.Seq[float32] {
			return IterateFloat32SliceRetr[*Unmarshaller, float32](u)
		},
	})
	runIterateDrainCases(t, iterDrainOps[int64]{
		name:     "int64",
		vals:     []int64{-1, -2, -3, -4},
		sentinel: -0x5A5A5A5A5A5A5A5A,
		writeOne: func(m *Marshaller, v int64) { m.WriteInt64(v) },
		readOne:  func(u *Unmarshaller) int64 { return u.ReadInt64() },
		iterate: func(u *Unmarshaller) iter.Seq[int64] {
			return IterateInt64SliceRetr[*Unmarshaller, int64](u)
		},
	})
	runIterateDrainCases(t, iterDrainOps[string]{
		name:     "string",
		vals:     []string{"a", "bb", "ccc", "dddd"},
		sentinel: "SENTINEL",
		writeOne: func(m *Marshaller, v string) { m.WriteString(v) },
		readOne:  func(u *Unmarshaller) string { return u.ReadString() },
		iterate: func(u *Unmarshaller) iter.Seq[string] {
			return IterateStringSliceRetr[*Unmarshaller, string](u)
		},
	})
}
