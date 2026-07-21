package play

import (
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"
)

func TestArrowColumnType(t *testing.T) {
	cases := []struct {
		dt   arrow.DataType
		want string
	}{
		{arrow.FixedWidthTypes.Boolean, "Bool"},
		{arrow.PrimitiveTypes.Int8, "Int8"},
		{arrow.PrimitiveTypes.Int64, "Int64"},
		{arrow.PrimitiveTypes.Uint16, "UInt16"},
		{arrow.PrimitiveTypes.Uint64, "UInt64"},
		{arrow.PrimitiveTypes.Float32, "Float32"},
		{arrow.PrimitiveTypes.Float64, "Float64"},
		{arrow.BinaryTypes.String, "String"},
		{arrow.BinaryTypes.LargeString, "String"},
		{arrow.BinaryTypes.Binary, "String"},
		{&arrow.FixedSizeBinaryType{ByteWidth: 16}, "FixedString(16)"},
		{arrow.FixedWidthTypes.Date32, "Date32"},
		{&arrow.TimestampType{Unit: arrow.Second}, "DateTime64(0,'UTC')"},
		{&arrow.TimestampType{Unit: arrow.Millisecond}, "DateTime64(3,'UTC')"},
		{&arrow.TimestampType{Unit: arrow.Microsecond}, "DateTime64(6,'UTC')"},
		{&arrow.TimestampType{Unit: arrow.Nanosecond}, "DateTime64(9,'UTC')"},
		{arrow.ListOf(arrow.PrimitiveTypes.Uint64), "Array(UInt64)"},
		{arrow.ListOf(arrow.ListOf(arrow.BinaryTypes.String)), "Array(Array(String))"},
		{&arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String}, "String"},
	}
	for _, tc := range cases {
		got, err := arrowColumnType(tc.dt)
		require.NoError(t, err, "%s", tc.dt)
		require.Equal(t, tc.want, got, "%s", tc.dt)
	}

	_, err := arrowColumnType(arrow.StructOf(arrow.Field{Name: "x", Type: arrow.PrimitiveTypes.Int64}))
	require.Error(t, err, "structs must refuse, not mangle")
}

func TestComposePinTableDDL(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "count()", Type: arrow.PrimitiveTypes.Uint64},
		{Name: "id:id:u64:2k:0:0:", Type: arrow.PrimitiveTypes.Uint64},
		{Name: "note", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "tags", Type: arrow.ListOf(arrow.BinaryTypes.String), Nullable: true},
	}, nil)
	ddl, err := composePinTableDDL("boxer.pin_00ff", schema)
	require.NoError(t, err)
	require.Contains(t, ddl, "CREATE TABLE IF NOT EXISTS boxer.pin_00ff")
	require.Contains(t, ddl, "`count()` UInt64")
	require.Contains(t, ddl, "`id:id:u64:2k:0:0:` UInt64")
	require.Contains(t, ddl, "`note` Nullable(String)")
	require.Contains(t, ddl, "`tags` Array(String)", "arrays must not be wrapped Nullable")
	require.Contains(t, ddl, "ENGINE MergeTree() ORDER BY tuple()")

	_, err = composePinTableDDL("t", arrow.NewSchema([]arrow.Field{
		{Name: "bad`name", Type: arrow.PrimitiveTypes.Int64}}, nil))
	require.Error(t, err)
	_, err = composePinTableDDL("t", arrow.NewSchema(nil, nil))
	require.Error(t, err)
}

func TestPinDataTableName(t *testing.T) {
	require.Equal(t, "boxer.pin_00000000000000ff", pinDataTableName(0xff))
	require.Equal(t, "boxer.pin_ffffffffffffffff", pinDataTableName(^uint64(0)))
}

func TestParsePinRowsRoundTrip(t *testing.T) {
	line := strings.Join([]string{
		"18446744073709551615",
		"boxer.pin_ffffffffffffffff",
		"1752800000",
		"play-main-1-2",
		"run-1",
		"main",
		"42",
		"3",
		"SELECT 1\\nFROM t",
	}, "\t")
	rows, err := parsePinRows([]byte(line + "\n"))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	r := rows[0]
	require.Equal(t, ^uint64(0), r.Fingerprint)
	require.Equal(t, "boxer.pin_ffffffffffffffff", r.DataTable)
	require.Equal(t, time.Unix(1752800000, 0).UTC(), r.PinnedAt)
	require.EqualValues(t, 42, r.NumRows)
	require.Equal(t, "SELECT 1\nFROM t", r.Query)

	rows, err = parsePinRows(nil)
	require.NoError(t, err)
	require.Empty(t, rows)
	_, err = parsePinRows([]byte("too\tfew\n"))
	require.Error(t, err)
}

func TestComposePinBrowserSql(t *testing.T) {
	sql := composePinBrowserSql(0)
	require.Contains(t, sql, "FROM "+pinMetaTable)
	require.Contains(t, sql, "ORDER BY pinned_at DESC")
	require.Contains(t, sql, "LIMIT 100")
	require.Equal(t, pinRowColumns, strings.Count(strings.Split(sql, "FROM")[0], ",")+1,
		"browser SELECT arity must match the parse contract")
}

func TestPinDriverNilClientAndSingleFlight(t *testing.T) {
	d := newPinDriver(nil)
	d.pin(nil, pinMetaRow{}) // must not panic
	state, _, err := d.status()
	require.Equal(t, pinIdle, state)
	require.NoError(t, err)
}

func TestPinRowLabel(t *testing.T) {
	label := pinRowLabel(pinRow{
		PinnedAt: time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC),
		NumRows:  42, NumCols: 3,
		Query: "SELECT a\nFROM b",
	})
	require.Contains(t, label, "42×3")
	require.Contains(t, label, "SELECT a")
	require.NotContains(t, label, "FROM b")

	noQuery := pinRowLabel(pinRow{DataTable: "boxer.pin_ab", NumRows: 1, NumCols: 1})
	require.Contains(t, noQuery, "boxer.pin_ab")
}
