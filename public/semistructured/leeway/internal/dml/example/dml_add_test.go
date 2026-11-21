package example

import (
	"bytes"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// ipcBytes serialises one record to an Arrow IPC stream — the strict wire-byte
// check (array.RecordEqual is only logical equality).
func ipcBytes(t *testing.T, rec arrow.RecordBatch) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(rec.Schema()), ipc.WithAllocator(memory.NewGoAllocator()))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// The named value-struct Add(<Section>Attr{...}) must produce a BYTE-IDENTICAL
// Arrow record to the positional BeginAttribute + AddToCoContainersP loop it
// lowers to — binding sub-columns by name instead of by position. The `special`
// section is a scalar (spc) + two co-containers (ary1, ary2) with a mixed
// membership; membership stays chained on the cursor Add returns.
func TestAdd_ByteIdenticalToPositional(t *testing.T) {
	pool := memory.NewGoAllocator()
	ts := time.Unix(1700000000, 0).UTC()

	// Positional: two co-container elements per attribute.
	eA := NewInEntityTesttable(pool, 1)
	eA.BeginEntity().SetId(42).SetTimestamp(ts)
	{
		a := eA.GetSectionSpecial().BeginAttribute("hello")
		a.AddToCoContainersP(7, 9)
		a.AddToCoContainersP(8, 10)
		a.AddMembershipMixedLowCardRefP(5, []byte("params"))
		a.EndAttributeP()
	}
	require.NoError(t, eA.CommitEntity())
	recsA, err := eA.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsA)

	// Named: the same attribute via Add(SpecialAttr{…}); membership chained.
	eB := NewInEntityTesttable(pool, 1)
	eB.BeginEntity().SetId(42).SetTimestamp(ts)
	{
		a := eB.GetSectionSpecial().Add(InEntityTesttableSectionSpecialAttr{
			Spc:  "hello",
			Ary1: []uint32{7, 8},
			Ary2: []uint32{9, 10},
		})
		a.AddMembershipMixedLowCardRefP(5, []byte("params"))
		a.EndAttributeP()
	}
	require.NoError(t, eB.CommitEntity())
	recsB, err := eB.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsB)

	assertEquivalent(t, recsA, recsB)
	require.Equal(t, ipcBytes(t, recsA[0]), ipcBytes(t, recsB[0]),
		"Add is not byte-identical to the positional loop")
}

// Add records an error (surfaced via CheckErrors) when its co-container slices
// have unequal length, rather than panicking on the zip.
func TestAdd_CoContainerLengthMismatch(t *testing.T) {
	pool := memory.NewGoAllocator()
	e := NewInEntityTesttable(pool, 1)
	e.BeginEntity().SetId(1).SetTimestamp(time.Unix(1700000000, 0).UTC())
	sec := e.GetSectionSpecial()
	sec.Add(InEntityTesttableSectionSpecialAttr{
		Spc:  "x",
		Ary1: []uint32{1, 2, 3},
		Ary2: []uint32{9}, // unequal length
	})
	require.Error(t, sec.CheckErrors(), "unequal co-container lengths must record an error")
}
