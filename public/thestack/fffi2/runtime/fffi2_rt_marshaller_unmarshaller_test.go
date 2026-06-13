package runtime

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestPair() (*Marshaller, *Unmarshaller, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	m := NewMarshaller(buf, binary.LittleEndian, func(err error) { panic(err) })
	u := NewUnmarshaller(buf, binary.LittleEndian, func(err error) { panic(err) }, nil)
	return m, u, buf
}

func newTestPairBigEndian() (*Marshaller, *Unmarshaller, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	m := NewMarshaller(buf, binary.BigEndian, func(err error) { panic(err) })
	u := NewUnmarshaller(buf, binary.BigEndian, func(err error) { panic(err) }, nil)
	return m, u, buf
}

func TestRoundtrip_Uint8(t *testing.T) {
	m, u, _ := newTestPair()
	cases := []uint8{0, 1, 127, 128, 255}
	for _, v := range cases {
		m.WriteUint8(v)
	}
	for _, want := range cases {
		got := u.ReadUInt8()
		require.EqualValues(t, want, got)
	}
}

func TestRoundtrip_Bool(t *testing.T) {
	m, u, _ := newTestPair()
	m.WriteBool(true)
	m.WriteBool(false)
	require.True(t, u.ReadBool())
	require.False(t, u.ReadBool())
}

func TestRoundtrip_Uint16(t *testing.T) {
	m, u, _ := newTestPair()
	cases := []uint16{0, 1, 256, math.MaxUint16}
	for _, v := range cases {
		m.WriteUint16(v)
	}
	for _, want := range cases {
		got := u.ReadUInt16()
		require.EqualValues(t, want, got)
	}
}

func TestRoundtrip_Uint32(t *testing.T) {
	m, u, _ := newTestPair()
	cases := []uint32{0, 1, 65536, math.MaxUint32}
	for _, v := range cases {
		m.WriteUint32(v)
	}
	for _, want := range cases {
		got := u.ReadUInt32()
		require.EqualValues(t, want, got)
	}
}

func TestRoundtrip_Uint64(t *testing.T) {
	m, u, _ := newTestPair()
	cases := []uint64{0, 1, 1 << 32, math.MaxUint64}
	for _, v := range cases {
		m.WriteUint64(v)
	}
	for _, want := range cases {
		got := u.ReadUInt64()
		require.EqualValues(t, want, got)
	}
}

func TestRoundtrip_Int8(t *testing.T) {
	m, u, _ := newTestPair()
	cases := []int8{0, 1, -1, 127, -127, math.MinInt8}
	for _, v := range cases {
		m.WriteInt8(v)
	}
	for _, want := range cases {
		got := u.ReadInt8()
		require.EqualValues(t, want, got, "failed for %d", want)
	}
}

func TestRoundtrip_Int16(t *testing.T) {
	m, u, _ := newTestPair()
	cases := []int16{0, 1, -1, 127, -127, math.MaxInt16, math.MinInt16}
	for _, v := range cases {
		m.WriteInt16(v)
	}
	for _, want := range cases {
		got := u.ReadInt16()
		require.EqualValues(t, want, got, "failed for %d", want)
	}
}

func TestRoundtrip_Int32(t *testing.T) {
	m, u, _ := newTestPair()
	cases := []int32{0, 1, -1, 127, -127, math.MaxInt32, math.MinInt32}
	for _, v := range cases {
		m.WriteInt32(v)
	}
	for _, want := range cases {
		got := u.ReadInt32()
		require.EqualValues(t, want, got, "failed for %d", want)
	}
}

func TestRoundtrip_Int64(t *testing.T) {
	m, u, _ := newTestPair()
	cases := []int64{0, 1, -1, 127, -127, math.MaxInt64, math.MinInt64, math.MinInt64 + 1}
	for _, v := range cases {
		m.WriteInt64(v)
	}
	for _, want := range cases {
		got := u.ReadInt64()
		require.EqualValues(t, want, got, "failed for %d", want)
	}
}

func TestRoundtrip_Float32(t *testing.T) {
	m, u, _ := newTestPair()
	cases := []float32{0, 1.0, -1.0, math.MaxFloat32, math.SmallestNonzeroFloat32, float32(math.Inf(1)), float32(math.Inf(-1))}
	for _, v := range cases {
		m.WriteFloat32(v)
	}
	for _, want := range cases {
		got := u.ReadFloat32()
		require.EqualValues(t, want, got)
	}
	// NaN special case (NaN != NaN, so test bit pattern)
	m.WriteFloat32(float32(math.NaN()))
	got := u.ReadFloat32()
	require.True(t, math.IsNaN(float64(got)))
}

func TestRoundtrip_Float64(t *testing.T) {
	m, u, _ := newTestPair()
	cases := []float64{0, 1.0, -1.0, math.MaxFloat64, math.SmallestNonzeroFloat64, math.Inf(1), math.Inf(-1)}
	for _, v := range cases {
		m.WriteFloat64(v)
	}
	for _, want := range cases {
		got := u.ReadFloat64()
		require.EqualValues(t, want, got)
	}
	m.WriteFloat64(math.NaN())
	got := u.ReadFloat64()
	require.True(t, math.IsNaN(got))
}

func TestRoundtrip_Complex64(t *testing.T) {
	m, u, _ := newTestPair()
	cases := []complex64{0, 1 + 2i, -3 - 4i}
	for _, v := range cases {
		m.WriteComplex64(v)
	}
	for _, want := range cases {
		got := u.ReadComplex64()
		require.EqualValues(t, want, got)
	}
}

func TestRoundtrip_Complex128(t *testing.T) {
	m, u, _ := newTestPair()
	cases := []complex128{0, 1 + 2i, -3 - 4i, complex(math.MaxFloat64, math.SmallestNonzeroFloat64)}
	for _, v := range cases {
		m.WriteComplex128(v)
	}
	for _, want := range cases {
		got := u.ReadComplex128()
		require.EqualValues(t, want, got)
	}
}

func TestRoundtrip_String(t *testing.T) {
	m, u, _ := newTestPair()
	cases := []string{"", "hello", "日本語", "\x00\x01\x02"}
	for _, v := range cases {
		m.WriteString(v)
	}
	for _, want := range cases {
		got := u.ReadString()
		require.Equal(t, want, got)
	}
}

func TestRoundtrip_Bytes(t *testing.T) {
	m, u, _ := newTestPair()
	m.WriteBytes([]byte{1, 2, 3})
	m.WriteBytes([]byte{})
	m.WriteBytes(nil)
	require.Equal(t, []byte{1, 2, 3}, u.ReadBytes())
	require.Equal(t, []byte{}, u.ReadBytes())
	// nil writes MaxUint32 sentinel; ReadBytes should handle it
	got := u.ReadBytes()
	require.Nil(t, got)
}

func TestRoundtrip_SliceLength(t *testing.T) {
	m, u, _ := newTestPair()
	m.WriteSliceLength(0)
	m.WriteSliceLength(100)
	m.WriteSliceLength(math.MaxInt32)
	m.WriteNilSlice()

	l, isNil := u.ReadSliceLength()
	require.Equal(t, 0, l)
	require.False(t, isNil)

	l, isNil = u.ReadSliceLength()
	require.Equal(t, 100, l)
	require.False(t, isNil)

	l, isNil = u.ReadSliceLength()
	require.Equal(t, math.MaxInt32, l)
	require.False(t, isNil)

	l, isNil = u.ReadSliceLength()
	require.Equal(t, 0, l)
	require.True(t, isNil)
}

func TestRoundtrip_NilSlice(t *testing.T) {
	m, u, _ := newTestPair()
	PutUint32SliceArg[*Marshaller, uint32](m, nil)
	PutUint32SliceArg[*Marshaller, uint32](m, []uint32{})
	PutUint32SliceArg[*Marshaller, uint32](m, []uint32{1, 2, 3})

	r1 := GetUint32SliceRetr[*Unmarshaller, uint32](u)
	require.Nil(t, r1)

	r2 := GetUint32SliceRetr[*Unmarshaller, uint32](u)
	require.NotNil(t, r2)
	require.Empty(t, r2)

	r3 := GetUint32SliceRetr[*Unmarshaller, uint32](u)
	require.Equal(t, []uint32{1, 2, 3}, r3)
}

func TestRoundtrip_BigEndian(t *testing.T) {
	m, u, _ := newTestPairBigEndian()
	m.WriteUint32(0xDEADBEEF)
	m.WriteInt64(math.MinInt64)
	m.WriteString("big endian test")
	require.EqualValues(t, 0xDEADBEEF, u.ReadUInt32())
	require.EqualValues(t, math.MinInt64, u.ReadInt64())
	require.Equal(t, "big endian test", u.ReadString())
}

func TestRoundtrip_WrittenBytesCounter(t *testing.T) {
	buf := &bytes.Buffer{}
	m := NewMarshaller(buf, binary.LittleEndian, func(err error) { panic(err) })
	m.ResetWrittenBytes()
	m.WriteUint32(42)
	require.Equal(t, 4, m.GetWrittenBytes())
	m.WriteUint8(1)
	require.Equal(t, 5, m.GetWrittenBytes())
	m.ResetWrittenBytes()
	require.Equal(t, 0, m.GetWrittenBytes())
}
