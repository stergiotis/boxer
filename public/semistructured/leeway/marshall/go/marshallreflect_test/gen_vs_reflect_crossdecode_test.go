package marshallreflect_test

import (
	"bytes"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor/codecdemo"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// ipcBytes serialises a single record batch to an Arrow IPC stream so two
// independently-produced batches can be byte-compared.
func ipcBytes(t *testing.T, rec arrow.RecordBatch) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(rec.Schema()), ipc.WithAllocator(memory.NewGoAllocator()))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// TestGenVsReflect_ByteEqualAndCrossDecode closes the load-bearing invariant
// that marshallreflect exists to satisfy (review E-3, marshallreflect/doc.go):
// the bytes marshallreflect.Marshal emits must equal what marshallgen's
// BuildEntities emits for the same rows. Both front-ends target the same
// anchor.InEntityTestTable with the same membership ids (kindStatus=1 /
// "droneStatus"->1, kindBattery=2 / "battery"->2), so the produced records must
// be identical — and a record written by one front-end must decode through the
// other (gen-write -> reflect-read).
func TestGenVsReflect_ByteEqualAndCrossDecode(t *testing.T) {
	drones := []reflectDrone{
		{ID: 1001, Tracking: []byte("TRK-A"), Status: "IN_TRANSIT", Battery: 8500},
		{ID: 1002, Tracking: []byte("TRK-B"), Status: "DELIVERED", Battery: 7200},
		{ID: 1003, Tracking: []byte("TRK-C"), Status: "DELIVERED", Battery: 6100},
	}
	lookup := marshallreflect.MapLookup{"droneStatus": 1, "battery": 2}

	allocator := memory.NewGoAllocator()

	// --- gen front-end (marshallgen BuildEntities) ---
	var cols codecdemo.DroneMissionColumns
	for _, d := range drones {
		cols.Append(codecdemo.DroneMission{ID: d.ID, Tracking: d.Tracking, Status: d.Status, Battery: d.Battery})
	}
	genTable := anchor.NewInEntityTestTable(allocator, len(drones))
	require.NoError(t, codecdemo.DroneMissionBuildEntities(genTable, &cols))
	genRecs, err := genTable.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, genRecs)
	defer func() {
		for _, r := range genRecs {
			r.Release()
		}
	}()

	// --- reflect front-end (marshallreflect.Marshal) ---
	reflectTable := anchor.NewInEntityTestTable(allocator, len(drones))
	require.NoError(t, marshallreflect.Marshal(reflectTable, drones, lookup))
	reflectRecs, err := reflectTable.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, reflectRecs)
	defer func() {
		for _, r := range reflectRecs {
			r.Release()
		}
	}()

	// Both front-ends must batch the rows identically.
	require.Equal(t, len(genRecs), len(reflectRecs), "record-batch count must match")
	for i := range genRecs {
		require.Truef(t, array.RecordEqual(genRecs[i], reflectRecs[i]),
			"record %d differs between gen and reflect front-ends:\ngen=%s\nreflect=%s", i, genRecs[i], reflectRecs[i])
		// Stronger: the serialised wire bytes must match too.
		require.Equalf(t, ipcBytes(t, genRecs[i]), ipcBytes(t, reflectRecs[i]),
			"record %d IPC bytes differ between gen and reflect front-ends", i)
	}

	// Cross-decode: a record written by the gen front-end must decode through
	// the reflect front-end back to the original rows.
	rec := genRecs[0]

	idReader := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idReader.SetColumnIndices(idReader.GetColumnIndices())
	require.NoError(t, idReader.LoadFromRecord(rec))
	defer idReader.Release()

	symbolReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symbolReader.SetColumnIndices(symbolReader.GetColumnIndices())
	require.NoError(t, symbolReader.LoadFromRecord(rec))
	defer symbolReader.Release()

	u64ArrayReader := anchor.NewReadAccessTestTableTaggedU64Array()
	u64ArrayReader.SetColumnIndices(u64ArrayReader.GetColumnIndices())
	require.NoError(t, u64ArrayReader.LoadFromRecord(rec))
	defer u64ArrayReader.Release()

	args := marshallreflect.UnmarshalArgs{
		NumRows: idReader.Len(),
		PlainCol: func(name string) any {
			switch name {
			case "id":
				return idReader.ValueId
			case "naturalKey":
				return idReader.ValueNaturalKey
			}
			return nil
		},
		SectionAttrs: func(name string) any {
			switch name {
			case "symbol":
				return symbolReader.GetAttributes()
			case "u64Array":
				return u64ArrayReader.GetAttributes()
			}
			return nil
		},
		SectionMembs: func(name string) any {
			switch name {
			case "symbol":
				return symbolReader.GetMemberships()
			case "u64Array":
				return u64ArrayReader.GetMemberships()
			}
			return nil
		},
	}
	var got []reflectDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))
	require.Equal(t, len(drones), len(got))
	for i := range drones {
		require.Equal(t, drones[i].ID, got[i].ID, "row %d ID", i)
		require.Equal(t, drones[i].Tracking, got[i].Tracking, "row %d Tracking", i)
		require.Equal(t, drones[i].Status, got[i].Status, "row %d Status", i)
		require.Equal(t, drones[i].Battery, got[i].Battery, "row %d Battery", i)
	}
}
