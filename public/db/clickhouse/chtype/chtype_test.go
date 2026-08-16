package chtype_test

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/chtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseShapes(t *testing.T) {
	cases := []struct {
		in       string
		name     string
		nargs    int
		round    string // canonical one-line spelling; "" means equal to in
		elements []string
		enums    []string
	}{
		{in: "UInt8", name: "UInt8"},
		{in: "String", name: "String"},
		{in: "Bool", name: "Bool"},
		{in: "Dynamic", name: "Dynamic"},
		{in: "JSON", name: "JSON"},
		{in: "IPv6", name: "IPv6"},
		{in: "UUID", name: "UUID"},
		{in: "Date32", name: "Date32"},
		{in: "Point", name: "Point"},
		{in: "Nothing", name: "Nothing"},
		{in: "Nullable(Float32)", name: "Nullable", nargs: 1},
		{in: "Nullable(Nothing)", name: "Nullable", nargs: 1},
		{in: "LowCardinality(Nullable(String))", name: "LowCardinality", nargs: 1},
		{in: "Array(String)", name: "Array", nargs: 1},
		{in: "Array(Array(Nullable(UInt8)))", name: "Array", nargs: 1},
		{in: "Map(String, UInt64)", name: "Map", nargs: 2},
		{in: "Map(LowCardinality(String), Array(UInt8))", name: "Map", nargs: 2},
		{in: "FixedString(16)", name: "FixedString", nargs: 1},
		{in: "Decimal(18, 4)", name: "Decimal", nargs: 2},
		{in: "Decimal(38,10)", name: "Decimal", nargs: 2, round: "Decimal(38, 10)"},
		{in: "Decimal64(4)", name: "Decimal64", nargs: 1},
		{in: "DateTime", name: "DateTime"},
		{in: "DateTime('UTC')", name: "DateTime", nargs: 1},
		{in: "DateTime64(3)", name: "DateTime64", nargs: 1},
		{in: "DateTime64(9, 'UTC')", name: "DateTime64", nargs: 2},
		{in: "DateTime64(9,'Europe/Zurich')", name: "DateTime64", nargs: 2, round: "DateTime64(9, 'Europe/Zurich')"},
		{in: "Object('json')", name: "Object", nargs: 1},
		{in: "Variant(String, UInt64)", name: "Variant", nargs: 2},
		{in: "SimpleAggregateFunction(sum, UInt64)", name: "SimpleAggregateFunction", nargs: 2},
		{in: "AggregateFunction(uniq, String)", name: "AggregateFunction", nargs: 2},
		{in: "AggregateFunction(quantiles(0.5, 0.9), UInt64)", name: "AggregateFunction", nargs: 2},
		{in: "AggregateFunction(quantilesTiming(0.5), Float64)", name: "AggregateFunction", nargs: 2},
		{
			in: "Tuple(a UInt8, b String)", name: "Tuple", nargs: 2,
			elements: []string{"a", "b"},
		},
		{
			in:   "Tuple(Id UInt64, NaturalKey String, Ts DateTime64(9, 'UTC'), UsageWatts Nullable(Float32), ActiveCPUs Array(Int32))",
			name: "Tuple", nargs: 5,
			elements: []string{"Id", "NaturalKey", "Ts", "UsageWatts", "ActiveCPUs"},
		},
		{in: "Tuple(UInt8, String)", name: "Tuple", nargs: 2},
		{
			in: "Nested(x UInt8, y String)", name: "Nested", nargs: 2,
			elements: []string{"x", "y"},
		},
		{
			in: "Array(Tuple(name String, type String))", name: "Array", nargs: 1,
		},
		{
			in: "Enum8('a' = 1, 'b' = 2)", name: "Enum8", nargs: 2,
			enums: []string{"a", "b"},
		},
		{
			in: "Enum16('FILTERED_LIST' = 0, 'MULTI_READ' = 1)", name: "Enum16", nargs: 2,
			enums: []string{"FILTERED_LIST", "MULTI_READ"},
		},
		{
			in: "Enum8('SHOW COLUMNS' = -3, 'x' = 0)", name: "Enum8", nargs: 2,
			enums: []string{"SHOW COLUMNS", "x"},
		},
		{
			in: "Nullable(Enum8('a' = 1))", name: "Nullable", nargs: 1,
			enums: []string{"a"},
		},
		{
			in: "Tuple(`a b` UInt8, `c` String)", name: "Tuple", nargs: 2,
			elements: []string{"a b", "c"},
		},
		{
			// Whitespace and newlines: what DESCRIBE pretty-prints in TSV.
			in: "Tuple(\n    a UInt8,\n    b String)", name: "Tuple", nargs: 2,
			round:    "Tuple(a UInt8, b String)",
			elements: []string{"a", "b"},
		},
		{
			in: "  LowCardinality( String )  ", name: "LowCardinality", nargs: 1,
			round: "LowCardinality(String)",
		},
	}
	for _, c := range cases {
		t.Run(strings.ReplaceAll(c.in, "\n", "\\n"), func(t *testing.T) {
			ty, err := chtype.Parse(c.in)
			require.NoError(t, err)
			assert.Equal(t, c.name, ty.Name)
			assert.Len(t, ty.Args, c.nargs)

			want := c.round
			if want == "" {
				want = c.in
			}
			assert.Equal(t, want, ty.String(), "canonical spelling")

			// Round-trip: re-parsing the canonical spelling yields the same tree.
			again, err := chtype.Parse(ty.String())
			require.NoError(t, err)
			assert.Equal(t, ty, again)

			names, ok := ty.ElementNames()
			if len(c.elements) == 0 {
				assert.False(t, ok, "expected no named elements")
			} else {
				require.True(t, ok)
				assert.Equal(t, c.elements, names)
			}

			vals, ok := ty.EnumValues()
			if len(c.enums) == 0 {
				assert.False(t, ok, "expected not an enum")
			} else {
				require.True(t, ok)
				assert.Equal(t, c.enums, vals)
			}
		})
	}
}

func TestUnwrap(t *testing.T) {
	cases := []struct{ in, want string }{
		{"UInt8", "UInt8"},
		{"Nullable(UInt8)", "UInt8"},
		{"LowCardinality(String)", "String"},
		{"LowCardinality(Nullable(String))", "String"},
		{"Nullable(Tuple(a UInt8))", "Tuple(a UInt8)"},
		{"Array(Nullable(UInt8))", "Array(Nullable(UInt8))"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			ty, err := chtype.Parse(c.in)
			require.NoError(t, err)
			assert.Equal(t, c.want, ty.Unwrap().String())
		})
	}
}

func TestElementLookup(t *testing.T) {
	ty, err := chtype.Parse("Nullable(Tuple(Id UInt64, Ts DateTime64(9, 'UTC'), `odd name` String))")
	require.NoError(t, err)

	a, ok := ty.Element("Ts")
	require.True(t, ok)
	require.NotNil(t, a.Type)
	assert.Equal(t, "DateTime64(9, 'UTC')", a.Type.String())

	a, ok = ty.Element("odd name")
	require.True(t, ok)
	require.NotNil(t, a.Type)
	assert.Equal(t, "String", a.Type.String())

	_, ok = ty.Element("Nonesuch")
	assert.False(t, ok)
}

// A positional tuple has no names to offer, so Elements refuses rather than
// answering with blanks.
func TestPositionalTupleHasNoElements(t *testing.T) {
	ty, err := chtype.Parse("Tuple(UInt64, UInt64, UUID)")
	require.NoError(t, err)
	_, ok := ty.Elements()
	assert.False(t, ok)
}

func TestParseRefusals(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"Tuple(",
		"Tuple()",
		"Array(String)) ",
		"Array(String) extra",
		"(String)",
		"Tuple(a UInt8,)",
		"Enum8('a' = )",
		"DateTime64(9, 'UTC)",
		"1234",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, err := chtype.Parse(c)
			assert.Error(t, err)
		})
	}
}

func TestUnescape(t *testing.T) {
	cases := []struct{ in, want string }{
		{`Tuple(Ts DateTime64(9,\'UTC\'))`, `Tuple(Ts DateTime64(9,'UTC'))`},
		{`a''b`, `a'b`},
		{`plain`, `plain`},
		{`a\nb`, "a\nb"},
		{`a\\b`, `a\b`},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			assert.Equal(t, c.want, chtype.Unescape(c.in))
		})
	}
}

// The corpus is a capture of `SELECT DISTINCT type FROM system.columns WHERE
// database = 'system'` from a ClickHouse 26.7 endpoint: the server's own type
// vocabulary, which is wider than anything this repo writes.
func TestParseSystemColumnsCorpus(t *testing.T) {
	f, err := os.Open("testdata/system_columns_types.txt")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		n++
		ty, err := chtype.Parse(line)
		if !assert.NoErrorf(t, err, "type %q", line) {
			continue
		}
		assert.Equalf(t, line, ty.String(), "canonical spelling of %q", line)
	}
	require.NoError(t, sc.Err())
	assert.Greater(t, n, 100, "corpus should not have been truncated")
}
