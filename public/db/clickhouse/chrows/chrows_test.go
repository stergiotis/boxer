package chrows

import (
	"bytes"
	"math"
	"os"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// The golden bodies are clickhouse-local's answer to one statement, under
// the default Arrow output settings and under
// output_format_arrow_string_as_string=0,
// output_format_arrow_low_cardinality_as_dictionary=1:
//
//	SELECT toString(number) AS name, toInt64(number) - 1 AS i,
//	       toUInt64(number) * 1000000000000 AS u, toUInt8(number % 2) AS flag,
//	       number % 2 = 0 AS b, toDecimal64(number, 2) / 4 AS dec,
//	       toDateTime('2026-09-24 10:00:00', 'UTC') + number AS dt,
//	       toDateTime64('2026-09-24 10:00:00.123', 3, 'UTC') AS dt64,
//	       toDate('2026-09-24') AS d,
//	       toLowCardinality(concat('lc', toString(number % 2))) AS lc,
//	       arrayMap(x -> toString(x), range(number)) AS arr,
//	       if(number = 1, NULL, number * 1.5) AS maybe
//	FROM numbers(3) FORMAT ArrowStream

type goldenCols struct {
	Name       []string    `ch:"name"`
	I          []int32     `ch:"i"`
	U          []uint64    `ch:"u"`
	Flag       []bool      `ch:"flag"`
	B          []bool      `ch:"b"`
	Dec        []float64   `ch:"dec"`
	Dt         []time.Time `ch:"dt"`
	Dt64       []time.Time `ch:"dt64"`
	D          []time.Time `ch:"d"`
	Lc         []lcName    `ch:"lc"`
	Arr        [][]string  `ch:"arr"`
	Maybe      []float64   `ch:"maybe"`
	MaybeValid []bool      `ch:"maybe,valid"`
	Absent     []string    `ch:"absent,optional"`
}

// lcName is a nominal type over string, read like its underlying kind.
type lcName string

func TestDecodeClickHouseGolden(t *testing.T) {
	day := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	for _, file := range []string{"testdata/defaults.arrows", "testdata/binary_dict.arrows"} {
		t.Run(file, func(t *testing.T) {
			body, err := os.ReadFile(file)
			require.NoError(t, err)
			var cols goldenCols
			n, err := DecodeBytes(&cols, body)
			require.NoError(t, err)
			require.Equal(t, 3, n)
			assert.Equal(t, []string{"0", "1", "2"}, cols.Name)
			assert.Equal(t, []int32{-1, 0, 1}, cols.I)
			assert.Equal(t, []uint64{0, 1e12, 2e12}, cols.U)
			assert.Equal(t, []bool{false, true, false}, cols.Flag)
			assert.Equal(t, []bool{true, false, true}, cols.B)
			assert.Equal(t, []float64{0, 0.25, 0.5}, cols.Dec)
			assert.Equal(t, []time.Time{day, day.Add(time.Second), day.Add(2 * time.Second)}, cols.Dt)
			assert.Equal(t, day.Add(123*time.Millisecond), cols.Dt64[0])
			assert.Equal(t, time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC), cols.D[2])
			assert.Equal(t, []lcName{"lc0", "lc1", "lc0"}, cols.Lc)
			assert.Equal(t, [][]string{{}, {"0"}, {"0", "1"}}, cols.Arr)
			assert.Equal(t, []bool{true, false, true}, cols.MaybeValid)
			assert.Equal(t, []float64{0, 0, 3}, cols.Maybe)
			assert.Equal(t, []string{"", "", ""}, cols.Absent)

			// A second decode appends.
			n, err = DecodeBytes(&cols, body)
			require.NoError(t, err)
			assert.Equal(t, 3, n)
			assert.Len(t, cols.Name, 6)
			assert.Len(t, cols.MaybeValid, 6)
		})
	}
}

func TestDecodeRefusals(t *testing.T) {
	body, err := os.ReadFile("testdata/defaults.arrows")
	require.NoError(t, err)

	var missing struct {
		X []string `ch:"no_such_column"`
	}
	_, err = DecodeBytes(&missing, body)
	require.Error(t, err, "a required column the result lacks")

	var nulls struct {
		Maybe []float64 `ch:"maybe"`
	}
	_, err = DecodeBytes(&nulls, body)
	require.Error(t, err, "NULLs without a validity field")

	var narrow struct {
		U []uint8 `ch:"u"`
	}
	_, err = DecodeBytes(&narrow, body)
	require.Error(t, err, "a value above the field's range")

	var negative struct {
		I []uint64 `ch:"i"`
	}
	_, err = DecodeBytes(&negative, body)
	require.Error(t, err, "a negative value into an unsigned field")

	var wrongType struct {
		Name []int64 `ch:"name"`
	}
	_, err = DecodeBytes(&wrongType, body)
	require.Error(t, err, "a text column into an integer field")

	var uneven struct {
		Name []string `ch:"name"`
		I    []int64  `ch:"i"`
	}
	uneven.Name = []string{"pre-existing"}
	_, err = DecodeBytes(&uneven, body)
	require.Error(t, err, "column slices of unequal length on entry")

	_, err = DecodeBytes(missing, body)
	require.Error(t, err, "a struct value, not a pointer")

	var untagged struct{ X []string }
	_, err = DecodeBytes(&untagged, body)
	require.Error(t, err, "no tagged fields")
}

type roundTrip struct {
	S      []string    `ch:"s"`
	By     [][]byte    `ch:"by"`
	Bo     []bool      `ch:"bo"`
	I8     []int8      `ch:"i8"`
	I      []int       `ch:"i"`
	U16    []uint16    `ch:"u16"`
	U64    []uint64    `ch:"u64"`
	F32    []float32   `ch:"f32"`
	F64    []float64   `ch:"f64"`
	T      []time.Time `ch:"t"`
	L      [][]int64   `ch:"l"`
	LS     [][]string  `ch:"ls"`
	N      []int32     `ch:"n"`
	NValid []bool      `ch:"n,valid"`
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 40).Draw(t, "rows")
		var in roundTrip
		for range n {
			in.S = append(in.S, rapid.String().Draw(t, "s"))
			in.By = append(in.By, rapid.SliceOf(rapid.Byte()).Draw(t, "by"))
			in.Bo = append(in.Bo, rapid.Bool().Draw(t, "bo"))
			in.I8 = append(in.I8, rapid.Int8().Draw(t, "i8"))
			in.I = append(in.I, rapid.Int().Draw(t, "i"))
			in.U16 = append(in.U16, rapid.Uint16().Draw(t, "u16"))
			in.U64 = append(in.U64, rapid.Uint64().Draw(t, "u64"))
			in.F32 = append(in.F32, rapid.Float32().Draw(t, "f32"))
			in.F64 = append(in.F64, rapid.Float64().Draw(t, "f64"))
			in.T = append(in.T, time.Unix(0, rapid.Int64().Draw(t, "t")).UTC())
			in.L = append(in.L, rapid.SliceOf(rapid.Int64()).Draw(t, "l"))
			in.LS = append(in.LS, rapid.SliceOf(rapid.String()).Draw(t, "ls"))
			valid := rapid.Bool().Draw(t, "valid")
			in.NValid = append(in.NValid, valid)
			v := int32(0)
			if valid {
				v = rapid.Int32().Draw(t, "n")
			}
			in.N = append(in.N, v)
		}
		var buf bytes.Buffer
		require.NoError(t, EncodeStream(&buf, &in))
		var out roundTrip
		m, err := DecodeBytes(&out, buf.Bytes())
		require.NoError(t, err)
		require.Equal(t, n, m)
		// Empty and nil slices decode alike; compare element-wise where
		// the distinction is not the point.
		assert.Equal(t, len(in.S), len(out.S))
		for i := range n {
			assert.Equal(t, in.S[i], out.S[i])
			assert.True(t, bytes.Equal(in.By[i], out.By[i]))
			assert.Equal(t, in.Bo[i], out.Bo[i])
			assert.Equal(t, in.I8[i], out.I8[i])
			assert.Equal(t, in.I[i], out.I[i])
			assert.Equal(t, in.U16[i], out.U16[i])
			assert.Equal(t, in.U64[i], out.U64[i])
			assert.Equal(t, math.Float32bits(in.F32[i]), math.Float32bits(out.F32[i]))
			assert.Equal(t, math.Float64bits(in.F64[i]), math.Float64bits(out.F64[i]))
			assert.True(t, in.T[i].Equal(out.T[i]))
			assert.Equal(t, len(in.L[i]), len(out.L[i]))
			for j := range in.L[i] {
				assert.Equal(t, in.L[i][j], out.L[i][j])
			}
			assert.Equal(t, len(in.LS[i]), len(out.LS[i]))
			for j := range in.LS[i] {
				assert.Equal(t, in.LS[i][j], out.LS[i][j])
			}
			assert.Equal(t, in.NValid[i], out.NValid[i])
			assert.Equal(t, in.N[i], out.N[i])
		}
	})
}

func TestCellReaders(t *testing.T) {
	var in roundTrip
	in.S = []string{"x"}
	in.By = [][]byte{{1}}
	in.Bo = []bool{true}
	in.I8 = []int8{-3}
	in.I = []int{7}
	in.U16 = []uint16{9}
	in.U64 = []uint64{math.MaxUint64}
	in.F32 = []float32{1.5}
	in.F64 = []float64{math.NaN()}
	in.T = []time.Time{time.Unix(1, 5e8).UTC()}
	in.L = [][]int64{{1}}
	in.LS = [][]string{{"a"}}
	in.N = []int32{0}
	in.NValid = []bool{false}
	var b bytes.Buffer
	require.NoError(t, EncodeStream(&b, in))
	rec := recordOf(t, b.Bytes())
	col := func(name string) int { return rec.Schema().FieldIndices(name)[0] }

	v, ok := Int64(rec.Column(col("i8")), 0)
	assert.True(t, ok)
	assert.Equal(t, int64(-3), v)
	_, ok = Int64(rec.Column(col("u64")), 0)
	assert.False(t, ok, "a UInt64 above MaxInt64 is not an int64")
	u, ok := Uint64(rec.Column(col("u64")), 0)
	assert.True(t, ok)
	assert.Equal(t, uint64(math.MaxUint64), u)
	_, ok = Uint64(rec.Column(col("i8")), 0)
	assert.False(t, ok, "a negative value is not a uint64")
	f, ok := Float64(rec.Column(col("f64")), 0)
	assert.True(t, ok, "a NaN is a value")
	assert.True(t, math.IsNaN(f))
	_, ok = Float64(rec.Column(col("s")), 0)
	assert.False(t, ok)
	s, ok := String(rec.Column(col("by")), 0)
	assert.True(t, ok, "binary reads as a string")
	assert.Equal(t, "\x01", s)
	bo, ok := Bool(rec.Column(col("u16")), 0)
	assert.True(t, ok)
	assert.True(t, bo)
	ms, ok := EpochMillis(rec.Column(col("t")), 0)
	assert.True(t, ok)
	assert.Equal(t, int64(1500), ms)
	_, ok = EpochMillis(rec.Column(col("i")), 0)
	assert.False(t, ok, "an integer is not a moment for EpochMillis")
	_, ok = Int64(rec.Column(col("n")), 0)
	assert.False(t, ok, "NULL")
	_, ok = Int64(rec.Column(col("n")), 5)
	assert.False(t, ok, "out of range")
}

func TestTimestampToEpochMillis(t *testing.T) {
	for _, tt := range []struct {
		v    int64
		unit arrow.TimeUnit
	}{
		{1700000000, arrow.Second},
		{1700000000000, arrow.Millisecond},
		{1700000000000000, arrow.Microsecond},
		{1700000000000000000, arrow.Nanosecond},
	} {
		assert.Equal(t, int64(1700000000000), TimestampToEpochMillis(tt.v, tt.unit), tt.unit.String())
	}
}
