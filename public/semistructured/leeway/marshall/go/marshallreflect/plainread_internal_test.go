package marshallreflect

import (
	"reflect"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// The anchor schema's plain columns are fixed to id:uint64 + naturalKey:[]byte,
// so the numeric / bool / string / time / fixed-byte arms of readPlainArrow —
// and the ts / expiresAt entity-header roles — are unreachable through a
// marshal-then-unmarshal round-trip against it. These white-box tests drive
// the read side directly: building each Arrow array and reading it back is the
// read-side equivalent, with no DML/schema required. (A consumer schema whose
// entity setters take these types covers the matching write side; that lives
// in the per-kind keelson suites.)

// TestReadPlainArrow_AllArms exercises every arm of readPlainArrow's type
// switch (and, via goTypeReflect, the inverse name→reflect.Type mapping the
// accumulator allocation uses).
func TestReadPlainArrow_AllArms(t *testing.T) {
	mem := memory.NewGoAllocator()
	tsNs := time.Date(2020, 6, 1, 2, 3, 4, 0, time.UTC).UnixNano()

	var want16 [16]byte
	copy(want16[:], "0123456789abcdef")

	cases := []struct {
		goType string
		build  func() arrow.Array
		want   any
	}{
		{"uint8", func() arrow.Array { b := array.NewUint8Builder(mem); b.Append(8); return b.NewArray() }, uint8(8)},
		{"uint16", func() arrow.Array { b := array.NewUint16Builder(mem); b.Append(16); return b.NewArray() }, uint16(16)},
		{"uint32", func() arrow.Array { b := array.NewUint32Builder(mem); b.Append(32); return b.NewArray() }, uint32(32)},
		{"uint64", func() arrow.Array { b := array.NewUint64Builder(mem); b.Append(64); return b.NewArray() }, uint64(64)},
		{"int8", func() arrow.Array { b := array.NewInt8Builder(mem); b.Append(-8); return b.NewArray() }, int8(-8)},
		{"int16", func() arrow.Array { b := array.NewInt16Builder(mem); b.Append(-16); return b.NewArray() }, int16(-16)},
		{"int32", func() arrow.Array { b := array.NewInt32Builder(mem); b.Append(-32); return b.NewArray() }, int32(-32)},
		{"int64", func() arrow.Array { b := array.NewInt64Builder(mem); b.Append(-64); return b.NewArray() }, int64(-64)},
		{"float32", func() arrow.Array { b := array.NewFloat32Builder(mem); b.Append(1.5); return b.NewArray() }, float32(1.5)},
		{"float64", func() arrow.Array { b := array.NewFloat64Builder(mem); b.Append(2.5); return b.NewArray() }, float64(2.5)},
		{"bool", func() arrow.Array { b := array.NewBooleanBuilder(mem); b.Append(true); return b.NewArray() }, true},
		{"string", func() arrow.Array { b := array.NewStringBuilder(mem); b.Append("hi"); return b.NewArray() }, "hi"},
		{"[]byte", func() arrow.Array {
			b := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
			b.Append([]byte("blob"))
			return b.NewArray()
		}, []byte("blob")},
		{"time.Time", func() arrow.Array {
			b := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: arrow.Nanosecond})
			b.Append(arrow.Timestamp(tsNs))
			return b.NewArray()
		}, time.Unix(0, tsNs).UTC()},
		{"[16]byte", func() arrow.Array {
			b := array.NewFixedSizeBinaryBuilder(mem, &arrow.FixedSizeBinaryType{ByteWidth: 16})
			b.Append(want16[:])
			return b.NewArray()
		}, want16},
	}

	for _, c := range cases {
		t.Run(c.goType, func(t *testing.T) {
			arr := c.build()
			defer arr.Release()
			fld := reflect.New(goTypeReflect(c.goType)).Elem()
			require.NoError(t, readPlainArrow(fld, c.goType, arr, 0))
			if c.goType == "time.Time" {
				require.Equal(t, c.want.(time.Time).UnixNano(), fld.Interface().(time.Time).UnixNano())
				return
			}
			require.Equal(t, c.want, fld.Interface())
		})
	}
}

// TestReadPlainArrow_TimestampUnits pins the 2026-06-13 review fix: a plain
// time.Time column must be reconstructed honoring its declared Arrow TimeUnit,
// not as hardcoded nanoseconds. In-tree plain ts/expiresAt columns are
// millisecond-width (z32, "ts:ts:z32" with Unit: arrow.Millisecond), so the
// old `time.Unix(0, raw)` reader under-scaled them by 10^6. Every unit the
// generator can emit is exercised; before the fix the non-nanosecond arms
// reconstructed the wrong instant.
func TestReadPlainArrow_TimestampUnits(t *testing.T) {
	mem := memory.NewGoAllocator()
	// A wall-clock instant whose fractional part is below millisecond, to also
	// confirm sub-unit truncation matches the column's resolution rather than
	// silently surviving via a nanosecond round-trip.
	want := time.Date(2026, 6, 13, 8, 9, 10, 123_456_789, time.UTC)

	cases := []struct {
		name string
		unit arrow.TimeUnit
		raw  arrow.Timestamp
		want time.Time
	}{
		{"Second", arrow.Second, arrow.Timestamp(want.Unix()), time.Unix(want.Unix(), 0).UTC()},
		{"Millisecond", arrow.Millisecond, arrow.Timestamp(want.UnixMilli()), time.UnixMilli(want.UnixMilli()).UTC()},
		{"Microsecond", arrow.Microsecond, arrow.Timestamp(want.UnixMicro()), time.UnixMicro(want.UnixMicro()).UTC()},
		{"Nanosecond", arrow.Nanosecond, arrow.Timestamp(want.UnixNano()), time.Unix(0, want.UnixNano()).UTC()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: c.unit})
			b.Append(c.raw)
			arr := b.NewArray()
			defer arr.Release()

			fld := reflect.New(goTypeReflect("time.Time")).Elem()
			require.NoError(t, readPlainArrow(fld, "time.Time", arr, 0))
			got := fld.Interface().(time.Time)
			require.Truef(t, c.want.Equal(got), "unit %s: want %s, got %s", c.name, c.want, got)
			require.Equal(t, time.UTC, got.Location(), "reconstructed time must be UTC")
		})
	}
}

// plainRolesDTO declares all four entity-header roles. With no tagged fields
// the section readers are never consulted, so Unmarshal can be driven from
// hand-built plain Arrow arrays alone — covering unmarshalPlain's iteration
// over ts / expiresAt that the anchor (id + naturalKey only) round-trips skip.
type plainRolesDTO struct {
	_         struct{}  `kind:"plainRoles"`
	ID        uint64    `lw:",id"`
	NK        []byte    `lw:",naturalKey"`
	Ts        time.Time `lw:",ts"`
	ExpiresAt time.Time `lw:",expiresAt"`
}

func TestUnmarshalPlain_AllRoles_ReadOnly(t *testing.T) {
	mem := memory.NewGoAllocator()
	ts := []time.Time{
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 6, 1, 12, 30, 0, 0, time.UTC),
	}
	ex := []time.Time{
		time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2021, 6, 1, 12, 30, 0, 0, time.UTC),
	}

	idB := array.NewUint64Builder(mem)
	idB.Append(101)
	idB.Append(102)
	idArr := idB.NewArray()
	defer idArr.Release()

	nkB := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
	nkB.Append([]byte("nk-0"))
	nkB.Append([]byte("nk-1"))
	nkArr := nkB.NewArray()
	defer nkArr.Release()

	tsB := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: arrow.Nanosecond})
	tsB.Append(arrow.Timestamp(ts[0].UnixNano()))
	tsB.Append(arrow.Timestamp(ts[1].UnixNano()))
	tsArr := tsB.NewArray()
	defer tsArr.Release()

	exB := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: arrow.Nanosecond})
	exB.Append(arrow.Timestamp(ex[0].UnixNano()))
	exB.Append(arrow.Timestamp(ex[1].UnixNano()))
	exArr := exB.NewArray()
	defer exArr.Release()

	args := UnmarshalArgs{
		NumRows: 2,
		PlainCol: func(name string) any {
			switch name {
			case "id":
				return idArr
			case "naturalKey":
				return nkArr
			case "ts":
				return tsArr
			case "expiresAt":
				return exArr
			}
			return nil
		},
		// No tagged fields → SectionAttrs / SectionMembs are never called.
	}
	var got []plainRolesDTO
	require.NoError(t, Unmarshal(args, &got, nil))

	require.Len(t, got, 2)
	for i := range got {
		require.Equal(t, uint64(101+i), got[i].ID, "row %d id", i)
		require.Equal(t, []byte{'n', 'k', '-', byte('0' + i)}, got[i].NK, "row %d naturalKey", i)
		require.Equal(t, ts[i].UnixNano(), got[i].Ts.UnixNano(), "row %d ts", i)
		require.Equal(t, ex[i].UnixNano(), got[i].ExpiresAt.UnixNano(), "row %d expiresAt", i)
	}
}
