package marshallreflect_test

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/example"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// A write that fails mid-entity must leave the DML ready for the next entity:
// the membership lookup miss on the first call is the only error, and a later
// write of valid rows to the same DML commits.
func TestMarshal_FailedRowRollsBack(t *testing.T) {
	dml := example.NewInEntityFixedTable(memory.NewGoAllocator(), 2)
	rows := []fixedHashRow{{Id: 1, H: [4]byte{1, 2, 3, 4}}}
	require.Error(t, marshallreflect.Marshal(dml, rows, marshallreflect.MapLookup{}))
	require.NoError(t, marshallreflect.Marshal(dml, rows, marshallreflect.MapLookup{"m": 7}))
	recs, err := dml.TransferRecords(nil)
	require.NoError(t, err)
	var n int64
	for _, r := range recs {
		n += r.NumRows()
		r.Release()
	}
	require.EqualValues(t, 1, n, "only the committed entity is written")
}

func TestRowComposer_FailedCommitRollsBack(t *testing.T) {
	dml := example.NewInEntityFixedTable(memory.NewGoAllocator(), 2)
	row := fixedHashRow{Id: 1, H: [4]byte{1, 2, 3, 4}}

	bad := marshallreflect.NewRowComposer(dml, marshallreflect.MapLookup{})
	require.NoError(t, bad.BeginRow(row))
	require.Error(t, bad.CommitRow())

	good := marshallreflect.NewRowComposer(dml, marshallreflect.MapLookup{"m": 7})
	require.NoError(t, good.BeginRow(row))
	require.NoError(t, good.CommitRow())
	recs, err := dml.TransferRecords(nil)
	require.NoError(t, err)
	var n int64
	for _, r := range recs {
		n += r.NumRows()
		r.Release()
	}
	require.EqualValues(t, 1, n, "only the committed entity is written")
}

// ghostSectionRow names a section the DML does not have, so the write reaches
// a missing GetSection method after BeginEntity: a contract violation raised
// mid-row, which must roll the entity back like any other failed write.
type ghostSectionRow struct {
	_  struct{} `kind:"ghostSectionRow"`
	Id uint64   `lw:",id"`
	H  [4]byte  `lw:"m,hash"`
	G  [4]byte  `lw:"m,ghost"`
}

func TestMarshal_ContractViolationMidRowRollsBack(t *testing.T) {
	dml := example.NewInEntityFixedTable(memory.NewGoAllocator(), 2)
	require.Error(t, marshallreflect.Marshal(dml, []ghostSectionRow{{Id: 1}}, marshallreflect.MapLookup{"m": 7}))
	requireCommitsAfter(t, dml)
}

func TestRowComposer_ContractViolationMidRowRollsBack(t *testing.T) {
	dml := example.NewInEntityFixedTable(memory.NewGoAllocator(), 2)
	bad := marshallreflect.NewRowComposer(dml, marshallreflect.MapLookup{"m": 7})
	require.NoError(t, bad.BeginRow(ghostSectionRow{Id: 1}))
	require.Error(t, bad.CommitRow())
	requireCommitsAfter(t, dml)
}

// requireCommitsAfter writes one valid entity to dml and checks it is the only
// one written: the failed entity before it must have been rolled back.
func requireCommitsAfter(t *testing.T, dml *example.InEntityFixedTable) {
	t.Helper()
	rows := []fixedHashRow{{Id: 2, H: [4]byte{1, 2, 3, 4}}}
	require.NoError(t, marshallreflect.Marshal(dml, rows, marshallreflect.MapLookup{"m": 7}))
	recs, err := dml.TransferRecords(nil)
	require.NoError(t, err)
	var n int64
	for _, r := range recs {
		n += r.NumRows()
		r.Release()
	}
	require.EqualValues(t, 1, n, "only the committed entity is written")
}
