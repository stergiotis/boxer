package jackstay

import (
	"context"
	"encoding/hex"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLeavesFor_Clamped(t *testing.T) {
	opts := ChunkingOptions{TargetLeafRows: 1, MaxLeaves: math.MaxUint32}
	assert.Equal(t, uint32(1)<<31, leavesFor(1<<33, opts), "a want past 2^31 clamps rather than shifting to zero")
	assert.Equal(t, uint32(1)<<31, leavesFor(math.MaxUint64, opts))
	opts.MaxLeaves = 3000
	assert.Equal(t, uint32(2048), leavesFor(1<<20, opts), "the cap is a power of two")
	assert.Equal(t, uint32(2048), leavesFor(2048, opts))
	assert.Equal(t, uint32(1024), leavesFor(1000, opts))
	opts.MaxLeaves = 0
	assert.Equal(t, uint32(1), leavesFor(1<<20, opts))
}

func TestBoundOrderOf(t *testing.T) {
	for typ, want := range map[string]boundOrderE{
		"Int8": boundOrderInt, "UInt256": boundOrderInt, "Int128": boundOrderInt,
		"Float32": boundOrderFloat, "Float64": boundOrderFloat,
		"Decimal(18, 4)": boundOrderDecimal, "Decimal128(3)": boundOrderDecimal,
		"Date": boundOrderText, "Date32": boundOrderText, "DateTime('UTC')": boundOrderText, "DateTime64(3, 'UTC')": boundOrderText, "Bool": boundOrderText,
		"String": boundOrderBytes, "LowCardinality(String)": boundOrderBytes, "FixedString(16)": boundOrderBytes,
		"UUID": boundOrderNone, "IPv4": boundOrderNone, "IPv6": boundOrderNone,
		"Enum8('a' = 1, 'b' = 2)": boundOrderNone, "Enum16('a' = 1)": boundOrderNone,
		"IntervalDay": boundOrderNone, "Nullable(Int64)": boundOrderNone, "BFloat16": boundOrderNone,
	} {
		assert.Equal(t, want, boundOrderOf(typ), typ)
		assert.Equal(t, want != boundOrderNone, orderableType(typ), typ)
	}
}

func TestChunkingValidate_Types(t *testing.T) {
	ok := func(bt string, bounds ...string) {
		c := Chunking{Kind: ChunkingRange, Exprs: []string{"k"}, BoundType: bt, Bounds: bounds, Leaves: 1}
		assert.NoError(t, c.validate(), "%s %v", bt, bounds)
	}
	bad := func(bt string, bounds ...string) {
		c := Chunking{Kind: ChunkingRange, Exprs: []string{"k"}, BoundType: bt, Bounds: bounds, Leaves: 1}
		assert.Error(t, c.validate(), "%s %v", bt, bounds)
	}
	// Past 2^53 a float64 cannot tell these apart.
	ok("Int64", "9007199254740992", "9007199254740993")
	ok("Int64", "-5", "-1", "0", "10")
	ok("UInt256", "1", "115792089237316195423570985008687907853269984665640564039457584007913129639935")
	bad("UInt64", "10", "9")
	bad("Int64", "1.5")
	ok("Float64", "-inf", "-1.5", "0", "1e300", "inf")
	bad("Float64", "-0", "0")
	bad("Float64", "1", "nan")
	ok("Decimal(10, 2)", "-1.50", "0.10", "10.00")
	bad("Decimal(10, 2)", "10.00", "9.99")
	ok("Date", "1999-12-31", "2000-01-01")
	ok("DateTime64(3, 'UTC')", "1969-12-31 23:59:59.999", "1970-01-01 00:00:00.000")
	ok("String", "B", "a", "é")
	bad("String", "b", "a")
	bad("IPv4", "9.0.0.1", "10.0.0.2")
	bad("UUID", "00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002")
	bad("Enum8('b' = 1, 'a' = 2)", "a")
}

func TestRangeChunking(t *testing.T) {
	c, ok := rangeChunking("k", "UInt64", []string{"1", "2", "3", "4", "5", "6", "7", "8"}, 4)
	require.True(t, ok)
	assert.Equal(t, []string{"3", "5", "7"}, c.Bounds)
	assert.NoError(t, c.validate())

	// Integers sorted by the server compare as numbers, past float64's
	// precision too.
	c, ok = rangeChunking("k", "Int64", []string{"9007199254740992", "9007199254740992", "9007199254740993", "9007199254740993"}, 2)
	require.True(t, ok)
	assert.Equal(t, []string{"9007199254740993"}, c.Bounds)

	_, ok = rangeChunking("k", "Float64", []string{"1", "2", "3", "nan"}, 2)
	assert.True(t, ok, "a NaN not picked is harmless")
	_, ok = rangeChunking("k", "Float64", []string{"1", "nan", "nan", "nan"}, 2)
	assert.False(t, ok, "a NaN bound falls back to one chunk")

	c, ok = rangeChunking("k", "Float64", []string{"-1", "-0", "0", "1"}, 4)
	require.True(t, ok)
	assert.Equal(t, []string{"0", "1"}, c.Bounds, "-0 and 0 are one bound")

	_, ok = rangeChunking("k", "UInt64", []string{"9", "10", "2", "3"}, 2)
	assert.True(t, ok)
	_, ok = rangeChunking("k", "UInt64", []string{"1", "10", "2", "3"}, 3)
	assert.False(t, ok, "a sample out of the type's order gives no layout")

	_, ok = rangeChunking("k", "UInt64", []string{"1", "1"}, 1)
	assert.False(t, ok, "one chunk needs no bounds")
}

func hexes(ss ...string) (out []string) {
	for _, s := range ss {
		out = append(out, strings.ToUpper(hex.EncodeToString([]byte(s))))
	}
	return
}

func TestRangeChunking_Strings(t *testing.T) {
	c, ok := rangeChunking("s", "String", hexes("a", "b", "c", "it's"), 2)
	require.True(t, ok)
	assert.Equal(t, []string{"c"}, c.Bounds, "the hex sample is decoded")

	_, ok = rangeChunking("s", "String", hexes("a", "\xff\xfe", "\xff\xff", "z"), 2)
	assert.False(t, ok, "bytes that are not UTF-8 would not survive the plan file")
	_, ok = rangeChunking("s", "String", hexes("a", "b\x00", "c", "d"), 2)
	assert.False(t, ok, "NUL is refused")
	_, ok = rangeChunking("s", "String", []string{"zz", "41"}, 2)
	assert.False(t, ok, "not hex")

	c, ok = rangeChunking("s", "FixedString(4)", hexes("ab\x00\x00", "ab\x00\x00", "abc\x00", "abcd"), 2)
	require.True(t, ok)
	assert.Equal(t, []string{"abc"}, c.Bounds, "the zero padding CAST adds back is dropped")
}

// fakeSampleQuery answers DeriveChunking's queries with canned rows and keeps
// the SQL it saw.
type fakeSampleQuery struct {
	typ    string
	sample string
	seen   []string
}

func (inst *fakeSampleQuery) Query(_ context.Context, sql string) (io.ReadCloser, error) {
	inst.seen = append(inst.seen, sql)
	body := ""
	switch {
	case strings.Contains(sql, "toTypeName"):
		body = `{"type":"` + inst.typ + `"}` + "\n"
	case strings.Contains(sql, "groupArraySample"):
		body = `{"sample":` + inst.sample + `}` + "\n"
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

func TestDeriveChunking_Sample(t *testing.T) {
	opts := DefaultChunkingOptions()
	opts.TargetChunkRows = 10

	q := &fakeSampleQuery{typ: "Float64", sample: `["1","2","3","4"]`}
	c, err := DeriveChunking(context.Background(), q, ref("d", "t"), "v", "", 40, opts)
	require.NoError(t, err)
	assert.Equal(t, ChunkingRange, c.Kind)
	require.Len(t, q.seen, 2)
	assert.Contains(t, q.seen[1], " WHERE NOT isNaN(v)")
	assert.Contains(t, q.seen[1], "arrayMap(v -> toString(v)")

	q = &fakeSampleQuery{typ: "LowCardinality(String)", sample: `["61","62","63","64"]`}
	c, err = DeriveChunking(context.Background(), q, ref("d", "t"), "s", "", 40, opts)
	require.NoError(t, err)
	assert.Contains(t, q.seen[1], "arrayMap(v -> hex(v)")
	assert.Equal(t, ChunkingRange, c.Kind)
	assert.Equal(t, "String", c.BoundType)

	q = &fakeSampleQuery{typ: "String", sample: `["61","FF","FE","64"]`}
	c, err = DeriveChunking(context.Background(), q, ref("d", "t"), "s", "", 40, opts)
	require.NoError(t, err)
	assert.Equal(t, ChunkingSingle, c.Kind, "a key that is not UTF-8 is one chunk")
	assert.Equal(t, leavesFor(40, opts), c.Leaves)

	q = &fakeSampleQuery{typ: "IPv4"}
	c, err = DeriveChunking(context.Background(), q, ref("d", "t"), "ip", "", 40, opts)
	require.NoError(t, err)
	assert.Equal(t, ChunkingSingle, c.Kind)
	assert.Len(t, q.seen, 1, "no sample for a type that is not range-chunked")
}
