package chrows

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

type goldenRow struct {
	Name       string    `ch:"name"`
	I          int32     `ch:"i"`
	U          uint64    `ch:"u"`
	Flag       bool      `ch:"flag"`
	Dec        float64   `ch:"dec"`
	Dt         time.Time `ch:"dt"`
	Lc         lcName    `ch:"lc"`
	Arr        []string  `ch:"arr"`
	Maybe      float64   `ch:"maybe"`
	MaybeValid bool      `ch:"maybe,valid"`
	Absent     string    `ch:"absent,optional"`
	Computed   string
}

func TestDecodeRowsClickHouseGolden(t *testing.T) {
	day := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	for _, file := range []string{"testdata/defaults.arrows", "testdata/binary_dict.arrows"} {
		t.Run(file, func(t *testing.T) {
			body, err := os.ReadFile(file)
			require.NoError(t, err)
			rows, err := DecodeRowsBytes[goldenRow](body)
			require.NoError(t, err)
			require.Len(t, rows, 3)
			assert.Equal(t, goldenRow{Name: "1", I: 0, U: 1e12, Flag: true, Dec: 0.25, Dt: day.Add(time.Second), Lc: "lc1",
				Arr: []string{"0"}, MaybeValid: false}, rows[1])
			assert.Equal(t, 3.0, rows[2].Maybe)
			assert.True(t, rows[2].MaybeValid)
			assert.Equal(t, []string{}, rows[0].Arr)
		})
	}
}

func TestDecodeRowsRefusals(t *testing.T) {
	body, err := os.ReadFile("testdata/defaults.arrows")
	require.NoError(t, err)
	type missing struct {
		X string `ch:"no_such_column"`
	}
	_, err = DecodeRowsBytes[missing](body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no_such_column", "the message names the column")
	assert.Contains(t, err.Error(), "name, i, u", "and what the result has")
	type nulls struct {
		Maybe float64 `ch:"maybe"`
	}
	_, err = DecodeRowsBytes[nulls](body)
	require.Error(t, err)
	type columnsAsRow struct {
		Name []int64 `ch:"name"`
	}
	_, err = DecodeRowsBytes[columnsAsRow](body)
	require.Error(t, err, "a slice field in a row struct is an Array column, and name is not one")
}

type rtRow struct {
	S      string    `ch:"s"`
	By     []byte    `ch:"by"`
	I      int       `ch:"i"`
	U64    uint64    `ch:"u64"`
	T      time.Time `ch:"t"`
	LS     []string  `ch:"ls"`
	N      int32     `ch:"n"`
	NValid bool      `ch:"n,valid"`
}

// The two destination shapes read one body identically.
func TestRowsAgreeWithColumns(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 30).Draw(t, "rows")
		var in roundTrip
		for range n {
			in.S = append(in.S, rapid.String().Draw(t, "s"))
			in.By = append(in.By, rapid.SliceOf(rapid.Byte()).Draw(t, "by"))
			in.Bo = append(in.Bo, false)
			in.I8 = append(in.I8, 0)
			in.I = append(in.I, rapid.Int().Draw(t, "i"))
			in.U16 = append(in.U16, 0)
			in.U64 = append(in.U64, rapid.Uint64().Draw(t, "u64"))
			in.F32 = append(in.F32, 0)
			in.F64 = append(in.F64, 0)
			in.T = append(in.T, time.Unix(0, rapid.Int64().Draw(t, "t")).UTC())
			in.L = append(in.L, nil)
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
		var cols roundTrip
		_, err := DecodeBytes(&cols, buf.Bytes())
		require.NoError(t, err)
		rows, err := DecodeRowsBytes[rtRow](buf.Bytes())
		require.NoError(t, err)
		require.Len(t, rows, n)
		for i, r := range rows {
			assert.Equal(t, cols.S[i], r.S)
			assert.True(t, bytes.Equal(cols.By[i], r.By))
			assert.Equal(t, cols.I[i], r.I)
			assert.Equal(t, cols.U64[i], r.U64)
			assert.True(t, cols.T[i].Equal(r.T))
			assert.Equal(t, len(cols.LS[i]), len(r.LS))
			assert.Equal(t, cols.N[i], r.N)
			assert.Equal(t, cols.NValid[i], r.NValid)
		}
	})
}
