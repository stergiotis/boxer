package test

import (
	"bytes"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml/example"
	"github.com/stretchr/testify/require"
)

func TestRuntimeSmoke(t *testing.T) {
	ent := example.NewInEntityJson(memory.DefaultAllocator, 128)
	require.NoError(t, ent.CheckErrors())
	ent.BeginEntity()
	require.NoError(t, ent.CheckErrors())
	ent.SetId([]byte{0, 1, 2, 4, 5, 6})
	require.NoError(t, ent.CheckErrors())

	boolSec := ent.GetSectionBool()
	float64Sec := ent.GetSectionFloat64()
	int64Sec := ent.GetSectionInt64()
	nullSec := ent.GetSectionNull()
	stringSec := ent.GetSectionString()
	undefinedSec := ent.GetSectionUndefined()
	symbolSec := ent.GetSectionSymbol()
	var _ = boolSec
	var _ = float64Sec
	var _ = int64Sec
	var _ = nullSec
	var _ = stringSec
	var _ = undefinedSec
	var _ = symbolSec

	stringSec.BeginAttribute("hello").AddMembershipMixedLowCardVerbatim([]byte("/a/_"), []byte("0")).EndAttribute()
	require.NoError(t, stringSec.CheckErrors())
	stringSec.BeginAttribute(", world!").AddMembershipMixedLowCardVerbatim([]byte("/b/_"), []byte("1")).EndAttribute()
	require.NoError(t, stringSec.CheckErrors())

	err := ent.CommitEntity()
	require.NoError(t, err)
	var records []arrow.Record
	records, err = ent.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.EqualValues(t, 1, records[0].NumRows())

	{
		buf := bytes.NewBuffer(make([]byte, 0, 4096))
		w := ipc.NewWriter(buf, ipc.WithDictionaryDeltas(true))
		err = w.Write(records[0])
		require.NoError(t, err)
		err = w.Close()
		require.NoError(t, err)
		s := buf.String()
		require.Contains(t, s, "hello")
		require.Contains(t, s, ", world!")
		require.Contains(t, s, "/a/_")
		require.Contains(t, s, "/b/_")
		require.Contains(t, s, "0")
		require.Contains(t, s, "1")
	}
}
func TestStateChecks(t *testing.T) {
	ent := example.NewInEntityJson(memory.DefaultAllocator, 128)
	require.NoError(t, ent.CheckErrors())
	ent.BeginEntity()
	require.NoError(t, ent.CheckErrors())
	ent.SetId([]byte{0, 1, 2, 4, 5, 6})
	require.NoError(t, ent.CheckErrors())

	boolSec := ent.GetSectionBool()
	float64Sec := ent.GetSectionFloat64()
	int64Sec := ent.GetSectionInt64()
	nullSec := ent.GetSectionNull()
	stringSec := ent.GetSectionString()
	undefinedSec := ent.GetSectionUndefined()
	symbolSec := ent.GetSectionSymbol()
	var _ = boolSec
	var _ = float64Sec
	var _ = int64Sec
	var _ = nullSec
	var _ = stringSec
	var _ = undefinedSec
	var _ = symbolSec

	stringSec.BeginAttribute("hello").AddMembershipMixedLowCardVerbatim([]byte("/a/_"), []byte("0")).EndAttribute()
	require.NoError(t, stringSec.CheckErrors())
	stringSec.BeginAttribute(", world!").AddMembershipMixedLowCardVerbatim([]byte("/b/_"), []byte("1")) //.EndAttribute()
	require.NoError(t, stringSec.CheckErrors())

	err := ent.CommitEntity()
	require.ErrorContains(t, err, "wrong state")
}
