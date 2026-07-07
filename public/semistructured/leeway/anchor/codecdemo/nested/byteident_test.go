package nested

import (
	"bytes"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// ipcBytes serialises a record to an Arrow IPC stream — the strict wire-byte
// check (array.RecordEqual is only logical equality).
func ipcBytes(t *testing.T, rec arrow.RecordBatch) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(rec.Schema()), ipc.WithAllocator(memory.NewGoAllocator()))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// assertGenEqualsReflect marshals data through BOTH front-ends against a fresh
// anchor table and asserts the records are byte-identical — the load-bearing
// invariant. The GENERATED codec (buildGen) drives the DML directly; the REFLECT
// codec walks the same DTO with a lookup that maps the static membership name to
// the same kind id the generated code hardcodes.
func assertGenEqualsReflect[T any](t *testing.T, data []T, buildGen func(*anchor.InEntityTestTable) error, lookup marshallreflect.MapLookup) {
	t.Helper()
	pool := memory.NewGoAllocator()

	genTable := anchor.NewInEntityTestTable(pool, len(data))
	require.NoError(t, buildGen(genTable))
	genRecs, err := genTable.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, genRecs)
	defer releaseAll(genRecs)

	reflectTable := anchor.NewInEntityTestTable(pool, len(data))
	require.NoError(t, marshallreflect.Marshal(reflectTable, data, lookup))
	reflectRecs, err := reflectTable.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, reflectRecs)
	defer releaseAll(reflectRecs)

	require.Equal(t, len(genRecs), len(reflectRecs), "record-batch count")
	for i := range genRecs {
		require.Truef(t, array.RecordEqual(genRecs[i], reflectRecs[i]),
			"record %d differs:\ngen=%s\nreflect=%s", i, genRecs[i], reflectRecs[i])
		require.Equalf(t, ipcBytes(t, genRecs[i]), ipcBytes(t, reflectRecs[i]),
			"record %d IPC bytes differ (gen vs reflect)", i)
	}
}

func TestTextDocNested_GenEqualsReflect(t *testing.T) {
	data := []TextDocNested{
		{ID: 1, Tracking: []byte("A"), Body: proseAttrs{Text: "hello world", WordLength: []uint32{5, 5}, WordBag: []string{"hello", "world"}}},
		{ID: 2, Tracking: []byte("B"), Body: proseAttrs{Text: "solo", WordLength: []uint32{4}, WordBag: []string{"solo"}}},
	}
	cols := &TextDocNestedColumns{}
	for _, d := range data {
		cols.Append(d)
	}
	assertGenEqualsReflect(t, data,
		func(tbl *anchor.InEntityTestTable) error { return TextDocNestedBuildEntities(tbl, cols) },
		marshallreflect.MapLookup{"prose": kindProse})
}

func TestManyTagsDoc_GenEqualsReflect(t *testing.T) {
	data := []ManyTagsDoc{
		{ID: 10, Tracking: []byte("M1"), Blocks: []symBlock{{Val: "a"}, {Val: "b"}, {Val: "c"}}},
		{ID: 11, Tracking: []byte("M2"), Blocks: []symBlock{{Val: "x"}}},
		{ID: 12, Tracking: []byte("M3")},
	}
	cols := &ManyTagsDocColumns{}
	for _, d := range data {
		cols.Append(d)
	}
	assertGenEqualsReflect(t, data,
		func(tbl *anchor.InEntityTestTable) error { return ManyTagsDocBuildEntities(tbl, cols) },
		marshallreflect.MapLookup{"tags": kindTags})
}

func TestOptNoteDoc_GenEqualsReflect(t *testing.T) {
	data := []OptNoteDoc{
		{ID: 20, Tracking: []byte("O1"), Note: option.Option[noteAttr]{Has: true, Val: noteAttr{Val: "present"}}},
		{ID: 21, Tracking: []byte("O2")},
		{ID: 22, Tracking: []byte("O3"), Note: option.Option[noteAttr]{Has: true, Val: noteAttr{Val: "here"}}},
	}
	cols := &OptNoteDocColumns{}
	for _, d := range data {
		cols.Append(d)
	}
	assertGenEqualsReflect(t, data,
		func(tbl *anchor.InEntityTestTable) error { return OptNoteDocBuildEntities(tbl, cols) },
		marshallreflect.MapLookup{"note": kindNote})
}
