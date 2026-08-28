package runtime

import (
	"bytes"
	"encoding/hex"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// encBytes runs f against a fresh writer and returns what it wrote. It is the
// testing.T-free form of enc, usable inside a rapid property.
func encBytes(f func(c *CborWriter)) (b []byte, err error) {
	var buf bytes.Buffer
	c, err := NewCborWriter(&buf)
	if err != nil {
		return
	}
	f(c)
	if err = c.Err(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// rd returns a reader over the item spelled as hex.
func rd(t *testing.T, s string) *CborReader {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return NewCborReader(b)
}

// Heads at every argument width, read back as the values the writer wrote.
func TestReadHeadWidths(t *testing.T) {
	cases := []struct {
		name string
		item string
		mt   MajorType
		arg  uint64
	}{
		{"0", "00", MajorUint, 0},
		{"23 immediate", "17", MajorUint, 23},
		{"24 one byte", "1818", MajorUint, 24},
		{"255", "18ff", MajorUint, math.MaxUint8},
		{"256 two bytes", "190100", MajorUint, 256},
		{"65535", "19ffff", MajorUint, math.MaxUint16},
		{"65536 four bytes", "1a00010000", MajorUint, 65536},
		{"2^32-1", "1affffffff", MajorUint, math.MaxUint32},
		{"2^32 eight bytes", "1b0000000100000000", MajorUint, 1 << 32},
		{"MaxUint64", "1bffffffffffffffff", MajorUint, math.MaxUint64},
		{"negative", "3818", MajorNeg, 24},
		{"bytes head", "43", MajorBytes, 3},
		{"text head", "63", MajorText, 3},
		{"array head", "9818", MajorArray, 24},
		{"map head", "a2", MajorMap, 2},
		{"tag 258", "d90102", MajorTag, TagSet},
		{"tag 1001", "d903e9", MajorTag, TagExtendedTime},
	}
	for _, tc := range cases {
		r := rd(t, tc.item)
		mt, arg := r.ReadHead()
		require.NoError(t, r.Err(), tc.name)
		require.Equal(t, tc.mt, mt, tc.name)
		require.Equal(t, tc.arg, arg, tc.name)
		require.Equal(t, len(tc.item)/2, r.Pos(), tc.name)
		require.Zero(t, r.Remaining(), tc.name)
		require.True(t, r.Done(), tc.name)
	}
}

// An argument that would fit a shorter head, an indefinite length and the
// reserved additional information values are all refused: they are encodings
// a core-deterministic writer never produces.
func TestReadHeadRejects(t *testing.T) {
	nonShortest := []struct {
		name string
		item string
	}{
		{"5 in a one-byte argument", "1805"},
		{"23 in a one-byte argument", "1817"},
		{"5 in a two-byte argument", "190005"},
		{"255 in a two-byte argument", "1900ff"},
		{"5 in a four-byte argument", "1a00000005"},
		{"65535 in a four-byte argument", "1a0000ffff"},
		{"5 in an eight-byte argument", "1b0000000000000005"},
		{"2^32-1 in an eight-byte argument", "1b00000000ffffffff"},
		{"a negative in a wide argument", "3900ff"},
		{"a byte-string length in a wide argument", "580341"},
		{"an array length in a wide argument", "9801"},
		{"simple value 24 in the one-byte form", "f818"},
		{"reserved additional information 28", "1c"},
		{"reserved additional information 29", "1d"},
		{"reserved additional information 30", "9e"},
	}
	for _, tc := range nonShortest {
		r := rd(t, tc.item)
		r.ReadHead()
		require.ErrorIs(t, r.Err(), ErrNonShortest, tc.name)
	}
	for _, item := range []string{"5f", "7f", "9f", "bf", "ff", "df"} {
		r := rd(t, item)
		r.ReadHead()
		require.ErrorIs(t, r.Err(), ErrIndefinite, item)
	}
	for _, item := range []string{"", "18", "1900", "1a000000", "1b00000000000000"} {
		r := rd(t, item)
		r.ReadHead()
		require.ErrorIs(t, r.Err(), ErrTruncated, item)
	}
	// The payload must be there too, not only the head.
	r := rd(t, "43ab")
	r.ReadBytes()
	require.ErrorIs(t, r.Err(), ErrTruncated)
}

// The first error sticks: every later call is a no-op returning zero values
// and the position does not move.
func TestReaderErrIsSticky(t *testing.T) {
	r := rd(t, "1805"+"03")
	r.ReadUint()
	require.ErrorIs(t, r.Err(), ErrNonShortest)
	first := r.Err()
	pos := r.Pos()
	require.Zero(t, r.ReadUint())
	require.Zero(t, r.ReadInt())
	require.Nil(t, r.ReadBytes())
	require.Equal(t, "", r.ReadTextString())
	require.Zero(t, r.ReadArrayHead())
	require.False(t, r.ReadBool())
	r.Skip()
	require.Same(t, first, r.Err(), "the first error is the one kept")
	require.Equal(t, pos, r.Pos())
	require.True(t, r.Done(), "a reader in error is done")
	_, ok := r.PeekMajor()
	require.False(t, ok)
	require.False(t, r.IsNull())

	r.Reset([]byte{0x03})
	require.NoError(t, r.Err())
	require.EqualValues(t, 3, r.ReadUint())
}

func TestReadIntRanges(t *testing.T) {
	require.Equal(t, uint64(math.MaxUint64), rd(t, "1bffffffffffffffff").ReadUint())
	require.EqualValues(t, math.MaxInt64, rd(t, "1b7fffffffffffffff").ReadInt())
	require.EqualValues(t, math.MinInt64, rd(t, "3b7fffffffffffffff").ReadInt())
	require.EqualValues(t, -1, rd(t, "20").ReadInt())
	require.EqualValues(t, 0, rd(t, "00").ReadInt())

	// One past int64 in either direction.
	r := rd(t, "1b8000000000000000")
	r.ReadInt()
	require.ErrorIs(t, r.Err(), ErrOutOfRange)
	r = rd(t, "3b8000000000000000")
	r.ReadInt()
	require.ErrorIs(t, r.Err(), ErrOutOfRange)

	// A signed read of a text string is a major-type mismatch, not a range error.
	r = rd(t, "63616263")
	r.ReadInt()
	require.ErrorIs(t, r.Err(), ErrUnexpectedMajor)
	r = rd(t, "20")
	r.ReadUint()
	require.ErrorIs(t, r.Err(), ErrUnexpectedMajor)
}

func TestReadSizedInts(t *testing.T) {
	require.Equal(t, uint8(math.MaxUint8), rd(t, "18ff").ReadU8())
	require.Equal(t, uint16(math.MaxUint16), rd(t, "19ffff").ReadU16())
	require.Equal(t, uint32(math.MaxUint32), rd(t, "1affffffff").ReadU32())
	require.Equal(t, uint64(math.MaxUint64), rd(t, "1bffffffffffffffff").ReadU64())
	require.EqualValues(t, math.MaxInt8, rd(t, "187f").ReadI8())
	require.EqualValues(t, math.MinInt8, rd(t, "387f").ReadI8())
	require.EqualValues(t, math.MaxInt16, rd(t, "197fff").ReadI16())
	require.EqualValues(t, math.MinInt16, rd(t, "397fff").ReadI16())
	require.EqualValues(t, math.MaxInt32, rd(t, "1a7fffffff").ReadI32())
	require.EqualValues(t, math.MinInt32, rd(t, "3a7fffffff").ReadI32())
	require.EqualValues(t, math.MinInt64, rd(t, "3b7fffffffffffffff").ReadI64())

	overflow := []struct {
		item string
		read func(*CborReader)
	}{
		{"190100", func(r *CborReader) { r.ReadU8() }},
		{"1a00010000", func(r *CborReader) { r.ReadU16() }},
		{"1b0000000100000000", func(r *CborReader) { r.ReadU32() }},
		{"1880", func(r *CborReader) { r.ReadI8() }},
		{"3880", func(r *CborReader) { r.ReadI8() }},
		{"198000", func(r *CborReader) { r.ReadI16() }},
		{"1a80000000", func(r *CborReader) { r.ReadI32() }},
	}
	for _, tc := range overflow {
		r := rd(t, tc.item)
		tc.read(r)
		require.ErrorIs(t, r.Err(), ErrOutOfRange, tc.item)
	}
}

func TestReadStrings(t *testing.T) {
	require.Equal(t, []byte("abc"), rd(t, "43616263").ReadBytes())
	require.Equal(t, []byte("abc"), rd(t, "63616263").ReadText())
	require.Equal(t, "abc", rd(t, "63616263").ReadTextString())
	require.Equal(t, "", rd(t, "60").ReadTextString())
	require.Empty(t, rd(t, "40").ReadBytes())
	require.Equal(t, "héllo ☃", rd(t, "6a68c3a96c6c6f20e29883").ReadTextString())

	// A text string is checked for UTF-8; a byte string is not.
	r := rd(t, "62fffe")
	r.ReadText()
	require.ErrorIs(t, r.Err(), ErrOutOfRange)
	require.Equal(t, []byte{0xff, 0xfe}, rd(t, "42fffe").ReadBytes())

	// The view aliases the reader's buffer rather than copying it.
	src, err := hex.DecodeString("43616263")
	require.NoError(t, err)
	view := NewCborReader(src).ReadBytes()
	src[1] = 'z'
	require.Equal(t, []byte("zbc"), view)
}

func TestReadContainersAndTags(t *testing.T) {
	require.Equal(t, 0, rd(t, "80").ReadArrayHead())
	require.Equal(t, 3, rd(t, "83010203").ReadArrayHead())
	require.Equal(t, 1, rd(t, "a10100").ReadMapHead())
	require.EqualValues(t, TagSet, rd(t, "d9010280").ReadTag())

	r := rd(t, "d9010280")
	r.ExpectTag(TagSet)
	require.NoError(t, r.Err())
	require.Equal(t, 0, r.ReadArrayHead())

	r = rd(t, "d834")
	r.ExpectTag(TagIPv6)
	require.ErrorIs(t, r.Err(), ErrOutOfRange)

	// A count larger than the bytes left is truncation, whatever the caller
	// intends to do with it: an item is at least one byte.
	r = rd(t, "8302")
	r.ReadArrayHead()
	require.ErrorIs(t, r.Err(), ErrTruncated)
	r = rd(t, "a20100")
	r.ReadMapHead()
	require.ErrorIs(t, r.Err(), ErrTruncated)
}

func TestReadSimpleValues(t *testing.T) {
	require.True(t, rd(t, "f5").ReadBool())
	require.False(t, rd(t, "f4").ReadBool())
	require.True(t, rd(t, "f6").IsNull())
	require.False(t, rd(t, "f4").IsNull())

	r := rd(t, "f6")
	r.ReadNull()
	require.NoError(t, r.Err())

	r = rd(t, "f4")
	r.ReadNull()
	require.ErrorIs(t, r.Err(), ErrUnexpectedMajor)

	r = rd(t, "f6")
	r.ReadBool()
	require.ErrorIs(t, r.Err(), ErrOutOfRange)

	r = rd(t, "f820")
	r.ReadBool()
	require.ErrorIs(t, r.Err(), ErrUnexpectedMajor)

	mt, ok := rd(t, "d90102").PeekMajor()
	require.True(t, ok)
	require.Equal(t, MajorTag, mt)
}

// Floats are accepted only in the shortest width that preserves the value —
// the width WriteFloatShortest picks — so decoding a canonical item and
// re-encoding it cannot move the bytes.
func TestReadFloatShortestEnforced(t *testing.T) {
	accepted := []struct {
		item string
		want float64
	}{
		{"f90000", 0},
		{"f98000", math.Copysign(0, -1)},
		{"f93c00", 1},
		{"f93e00", 1.5},
		{"f9c000", -2},
		{"f90001", math.Ldexp(1, -24)},    // the smallest binary16 subnormal
		{"f903ff", math.Ldexp(1023, -24)}, // the largest binary16 subnormal
		{"f90400", math.Ldexp(1, -14)},    // the smallest binary16 normal
		{"f97bff", 65504},                 // the largest finite binary16
		{"f97c00", math.Inf(1)},
		{"f9fc00", math.Inf(-1)},
		{"fa3dcccccd", float64(float32(0.1))},
		{"fa5f800000", math.Pow(2, 64)},
		{"fb3fb999999999999a", 0.1},
		{"fb4415af1d78b58c40", 1e20},
	}
	for _, tc := range accepted {
		r := rd(t, tc.item)
		got := r.ReadFloat64()
		require.NoError(t, r.Err(), tc.item)
		require.Equal(t, math.Float64bits(tc.want), math.Float64bits(got), tc.item)
	}
	require.True(t, math.IsNaN(rd(t, "f97e00").ReadFloat64()))

	rejected := []struct {
		name string
		item string
	}{
		{"1.5 as binary32", "fa3fc00000"},
		{"1.5 as binary64", "fb3ff8000000000000"},
		{"-0.0 as binary64", "fb8000000000000000"},
		{"65504 as binary32", "fa477fe000"},
		{"+Inf as binary32", "fa7f800000"},
		{"+Inf as binary64", "fb7ff0000000000000"},
		{"-Inf as binary64", "fbfff0000000000000"},
		{"NaN as binary32", "fa7fc00000"},
		{"NaN as binary64", "fb7ff8000000000000"},
		{"a NaN carrying a payload", "f97e01"},
		{"a signaling NaN", "f97c01"},
		{"0.1f32 as binary64", "fb3fb99999a0000000"},
	}
	for _, tc := range rejected {
		r := rd(t, tc.item)
		r.ReadFloat64()
		require.ErrorIs(t, r.Err(), ErrNonShortest, tc.name)
	}

	r := rd(t, "03")
	r.ReadFloat64()
	require.ErrorIs(t, r.Err(), ErrUnexpectedMajor, "an integer is not a float: SD3 applies no numeric reduction")
	r = rd(t, "f5")
	r.ReadFloat64()
	require.ErrorIs(t, r.Err(), ErrUnexpectedMajor)
}

func TestReadFloat32(t *testing.T) {
	require.Equal(t, float32(1.5), rd(t, "f93e00").ReadFloat32())
	require.Equal(t, float32(0.1), rd(t, "fa3dcccccd").ReadFloat32())
	require.True(t, math.IsInf(float64(rd(t, "f97c00").ReadFloat32()), 1))
	require.True(t, math.IsNaN(float64(rd(t, "f97e00").ReadFloat32())))
	require.Equal(t, float32(math.Copysign(0, -1)), rd(t, "f98000").ReadFloat32())

	r := rd(t, "fb3fb999999999999a") // 0.1 needs binary64
	r.ReadFloat32()
	require.ErrorIs(t, r.Err(), ErrOutOfRange)
}

// The reader's shortest-width rule must be the writer's: the width
// canonicalFloatAI predicts is the width WriteFloatShortest emits.
func TestFloatWidthRuleMatchesTheWriter(t *testing.T) {
	check := func(t *rapid.T, f float64) {
		b, err := encBytes(func(c *CborWriter) { c.WriteFloatShortest(f) })
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		if got, want := b[0]&0x1f, canonicalFloatAI(f); got != want {
			t.Fatalf("f=%v (%016x): the writer used additional information %d, the reader expects %d", f, math.Float64bits(f), got, want)
		}
		r := NewCborReader(b)
		back := r.ReadFloat64()
		if err := r.Err(); err != nil {
			t.Fatalf("f=%v: reading back what the writer wrote failed: %v", f, err)
		}
		if math.IsNaN(f) {
			if !math.IsNaN(back) {
				t.Fatalf("f=%v: NaN did not survive", f)
			}
			return
		}
		if math.Float64bits(back) != math.Float64bits(f) {
			t.Fatalf("f=%v: read back %v", f, back)
		}
	}
	fixed := []float64{
		0, math.Copysign(0, -1), 1, -1, 1.5, 0.1, float64(float32(0.1)),
		math.Inf(1), math.Inf(-1), math.NaN(), math.MaxFloat64, math.SmallestNonzeroFloat64,
		float64(math.MaxFloat32), float64(math.SmallestNonzeroFloat32),
		math.Ldexp(1, -24), math.Ldexp(1023, -24), math.Ldexp(1, -25), 65504, 65505,
		float64(float32(math.Ldexp(1, -25))),
	}
	rapid.Check(t, func(rt *rapid.T) {
		which := rapid.IntRange(0, len(fixed)).Draw(rt, "which")
		if which < len(fixed) {
			check(rt, fixed[which])
			return
		}
		// Random bit patterns cover the binary64-only widths; random float32
		// bit patterns cover the binary16 and binary32 ones.
		check(rt, math.Float64frombits(rapid.Uint64().Draw(rt, "bits64")))
		check(rt, float64(math.Float32frombits(rapid.Uint32().Draw(rt, "bits32"))))
	})
}

func TestSkipAndItemBytes(t *testing.T) {
	// [1, [2, 3], {1: "a"}, h'ff', 258(1.5), null]
	const item = "86" + "01" + "820203" + "a1016161" + "41ff" + "d90102f93e00" + "f6"
	r := rd(t, item)
	require.Equal(t, 6, r.ReadArrayHead())
	for range 6 {
		r.Skip()
		require.NoError(t, r.Err())
	}
	require.True(t, r.Done())

	r = rd(t, item)
	require.Equal(t, 6, r.ReadArrayHead())
	want := []string{"01", "820203", "a1016161", "41ff", "d90102f93e00", "f6"}
	for _, w := range want {
		require.Equal(t, w, hex.EncodeToString(r.ReadItemBytes()))
	}
	require.NoError(t, r.Err())

	// Skip validates the heads it passes.
	for _, bad := range []string{"9f00ff", "821805", "a1011c"} {
		r := rd(t, bad)
		r.Skip()
		require.Error(t, r.Err(), bad)
	}
	r = rd(t, "8201")
	r.Skip()
	require.ErrorIs(t, r.Err(), ErrTruncated)

	// A nest deeper than the limit is an error, not a stack overflow.
	deep := make([]byte, maxNestingDepth+2)
	for i := range deep {
		deep[i] = byte(MajorArray) | 1
	}
	deep[len(deep)-1] = 0x00
	r = NewCborReader(deep)
	r.Skip()
	require.ErrorIs(t, r.Err(), ErrOutOfRange)
}

// A CBOR sequence (RFC 8742) is read by looping until Done.
func TestReadSequence(t *testing.T) {
	r := rd(t, "01"+"63616263"+"f93e00"+"820102")
	n := 0
	for !r.Done() {
		r.Skip()
		n++
	}
	require.NoError(t, r.Err())
	require.Equal(t, 4, n)
}

// Every integer the writer emits reads back as itself, and the reader refuses
// what the writer never emits.
func TestIntegerRoundTripProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		u := rapid.Uint64().Draw(rt, "u")
		i := rapid.Int64().Draw(rt, "i")
		b, err := encBytes(func(c *CborWriter) {
			c.WriteUint(u)
			c.WriteInt(i)
		})
		if err != nil {
			rt.Fatalf("write: %v", err)
		}
		r := NewCborReader(b)
		if got := r.ReadUint(); got != u {
			rt.Fatalf("uint: got %d want %d", got, u)
		}
		if got := r.ReadInt(); got != i {
			rt.Fatalf("int: got %d want %d", got, i)
		}
		if err := r.Err(); err != nil {
			rt.Fatalf("read: %v", err)
		}
		if !r.Done() {
			rt.Fatalf("%d bytes left over", r.Remaining())
		}
	})
}
