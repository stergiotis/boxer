package runtime

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// enc runs f against a fresh writer and returns the hex of what it wrote.
func enc(t *testing.T, f func(c *CborWriter)) string {
	t.Helper()
	var b bytes.Buffer
	c, err := NewCborWriter(&b)
	require.NoError(t, err)
	f(c)
	require.NoError(t, c.Err())
	return hex.EncodeToString(b.Bytes())
}

// Heads take the shortest argument encoding (RFC 8949 §4.2.1), at every
// width boundary.
func TestHeadShortestArgument(t *testing.T) {
	cases := []struct {
		name string
		n    uint64
		want string
	}{
		{"0", 0, "00"},
		{"23 is still immediate", 23, "17"},
		{"24 takes one byte", 24, "1818"},
		{"255", math.MaxUint8, "18ff"},
		{"256 takes two bytes", 256, "190100"},
		{"65535", math.MaxUint16, "19ffff"},
		{"65536 takes four bytes", 65536, "1a00010000"},
		{"2^32-1", math.MaxUint32, "1affffffff"},
		{"2^32 takes eight bytes", 1 << 32, "1b0000000100000000"},
		{"MaxUint64", math.MaxUint64, "1bffffffffffffffff"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, enc(t, func(c *CborWriter) { c.WriteUint(tc.n) }), tc.name)
	}
	// The same argument encoding under the other major types.
	require.Equal(t, "80", enc(t, func(c *CborWriter) { c.ArrayHead(0) }))
	require.Equal(t, "9818", enc(t, func(c *CborWriter) { c.ArrayHead(24) }))
	require.Equal(t, "a2", enc(t, func(c *CborWriter) { c.MapHead(2) }))
	require.Equal(t, "d834", enc(t, func(c *CborWriter) { c.Tag(TagIPv4) }))
	require.Equal(t, "d836", enc(t, func(c *CborWriter) { c.Tag(TagIPv6) }))
	require.Equal(t, "d90102", enc(t, func(c *CborWriter) { c.Tag(TagSet) }))
	require.Equal(t, "d903e9", enc(t, func(c *CborWriter) { c.Tag(TagExtendedTime) }))
	require.Equal(t, "c0", enc(t, func(c *CborWriter) { c.Head(MajorTypeTag, 0) }))
}

func TestWriteInt(t *testing.T) {
	cases := []struct {
		name string
		n    int64
		want string
	}{
		{"0", 0, "00"},
		{"3", 3, "03"},
		{"-1", -1, "20"},
		{"-4", -4, "23"},
		{"-24 is still immediate", -24, "37"},
		{"-25 takes one byte", -25, "3818"},
		{"-256", -256, "38ff"},
		{"-257", -257, "390100"},
		{"MaxInt64", math.MaxInt64, "1b7fffffffffffffff"},
		{"MinInt64", math.MinInt64, "3b7fffffffffffffff"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, enc(t, func(c *CborWriter) { c.WriteInt(tc.n) }), tc.name)
	}
}

func TestSimpleValues(t *testing.T) {
	require.Equal(t, "f5", enc(t, func(c *CborWriter) { c.WriteBool(true) }))
	require.Equal(t, "f4", enc(t, func(c *CborWriter) { c.WriteBool(false) }))
	require.Equal(t, "f6", enc(t, func(c *CborWriter) { c.WriteNull() }))
	require.Equal(t, "f5f4f6", enc(t, func(c *CborWriter) {
		c.WriteBool(true)
		c.WriteBool(false)
		c.WriteNull()
	}))
}

func TestStrings(t *testing.T) {
	const long = "abcdefghijklmnopqrstuvwx" // 24 bytes: the length crosses into a one-byte argument
	require.Equal(t, "60", enc(t, func(c *CborWriter) { c.WriteTextString("") }))
	require.Equal(t, "40", enc(t, func(c *CborWriter) { c.WriteBytes(nil) }))
	require.Equal(t, "63616263", enc(t, func(c *CborWriter) { c.WriteTextString("abc") }))
	require.Equal(t, "63616263", enc(t, func(c *CborWriter) { c.WriteText([]byte("abc")) }))
	require.Equal(t, "43616263", enc(t, func(c *CborWriter) { c.WriteBytes([]byte("abc")) }))
	require.Equal(t, "43616263", enc(t, func(c *CborWriter) { c.WriteBytesString("abc") }))
	require.Equal(t, "78186162636465666768696a6b6c6d6e6f707172737475767778",
		enc(t, func(c *CborWriter) { c.WriteTextString(long) }))
	// Write passes an already-encoded item through verbatim.
	require.Equal(t, "6361626303", enc(t, func(c *CborWriter) {
		c.Write([]byte{0x63, 'a', 'b', 'c'})
		c.WriteUint(3)
	}))
}

// The shortest value-preserving float width, with no numeric reduction: an
// integer-valued float stays a float and -0.0 keeps its sign. The dCBOR §2.5
// reduction is a form's rule, not the writer's (canonform applies it).
func TestWriteFloatShortest(t *testing.T) {
	cases := []struct {
		name string
		f    float64
		want string
	}{
		{"1.5 fits float16", 1.5, "f93e00"},
		{"f32 0.1 fits float32", float64(float32(0.1)), "fa3dcccccd"},
		{"f64 0.1 needs float64", 0.1, "fb3fb999999999999a"},
		{"3.0 stays a float", 3.0, "f94200"},
		{"-0.0 keeps its sign", math.Copysign(0, -1), "f98000"},
		{"0.0", 0.0, "f90000"},
		{"NaN is the quiet one", math.NaN(), "f97e00"},
		{"+Inf", math.Inf(1), "f97c00"},
		{"-Inf", math.Inf(-1), "f9fc00"},
		{"2^64 fits float32", math.Pow(2, 64), "fa5f800000"},
		{"1e20 needs float64", 1e20, "fb4415af1d78b58c40"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, enc(t, func(c *CborWriter) { c.WriteFloatShortest(tc.f) }), tc.name)
	}
	// A zero-value writer has no float encoder: the error is reported, not a
	// panic or silent wrong bytes.
	var b bytes.Buffer
	var zero CborWriter
	zero.Reset(&b)
	zero.WriteFloatShortest(1.5)
	require.Error(t, zero.Err())
	require.Zero(t, b.Len())
}

type failWriter struct{ n int }

var errFail = errors.New("failWriter")

func (w *failWriter) Write(p []byte) (int, error) {
	w.n++
	return 0, errFail
}

// The first error sticks and every later call is a no-op, so callers check
// once per item instead of once per head.
func TestErrIsSticky(t *testing.T) {
	fw := &failWriter{}
	c, err := NewCborWriter(fw)
	require.NoError(t, err)
	c.WriteUint(1)
	c.WriteTextString("abc")
	c.WriteFloatShortest(1.5)
	require.ErrorIs(t, c.Err(), errFail)
	require.Equal(t, 1, fw.n, "the writer is touched once, then not again")

	var b bytes.Buffer
	c.Reset(&b)
	require.NoError(t, c.Err())
	c.WriteUint(1)
	require.NoError(t, c.Err())
	require.Equal(t, "01", hex.EncodeToString(b.Bytes()))
}

// Text strings must be valid UTF-8 (RFC 8949 §2); the writer refuses what
// ReadText refuses, so the wire stays writer/reader symmetric.
func TestWriteTextRefusesInvalidUtf8(t *testing.T) {
	var b bytes.Buffer
	c, err := NewCborWriter(&b)
	require.NoError(t, err)
	c.WriteText([]byte{0x61, 0xff, 0x62})
	require.ErrorIs(t, c.Err(), ErrOutOfRange)
	require.Equal(t, 0, b.Len())

	c.Reset(&b)
	c.WriteTextString("aµb")
	require.NoError(t, c.Err())
}
