package runtime

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/unsafeperf"
)

// elementOnly hides the bulk half of a marshaller: embedding the interface
// promotes MarshallWriterI's methods and nothing else, so the Put helpers
// write element by element through it — the path every writer had before the
// bulk one existed, and the definition of the wire format.
type elementOnly struct{ MarshallWriterI }

type (
	namedU8  uint8
	namedI8  int8
	namedU16 uint16
	namedI16 int16
	namedU32 uint32
	namedI32 int32
	namedF32 float32
	namedU64 uint64
	namedI64 int64
	namedF64 float64
)

// both writes with put through a bulk-capable marshaller and through the
// element-wise one, and returns what each wrote.
func both(order binary.ByteOrder, put func(m MarshallWriterI)) (bulk, elementwise []byte, bulkWritten, elementWritten int) {
	var a, b bytes.Buffer
	fail := func(err error) { panic(err) }
	ma := NewMarshaller(&a, order, fail)
	mb := NewMarshaller(&b, order, fail)
	put(ma)
	put(elementOnly{mb})
	return a.Bytes(), b.Bytes(), ma.GetWrittenBytes(), mb.GetWrittenBytes()
}

func bits32(ws []uint32) (out []namedF32) {
	if ws == nil {
		return nil
	}
	out = make([]namedF32, len(ws))
	for i, w := range ws {
		out[i] = namedF32(math.Float32frombits(w)) // any bit pattern, NaN payloads included
	}
	return
}

func bits64(ws []uint64) (out []namedF64) {
	if ws == nil {
		return nil
	}
	out = make([]namedF64, len(ws))
	for i, w := range ws {
		out[i] = namedF64(math.Float64frombits(w))
	}
	return
}

func convert[To, From any](vs []From, f func(From) To) (out []To) {
	if vs == nil {
		return nil
	}
	out = make([]To, len(vs))
	for i, v := range vs {
		out[i] = f(v)
	}
	return
}

// The bulk path is an optimisation and nothing else: for every slice helper,
// every byte order and every content it writes exactly the bytes the
// element-wise path writes, and counts them the same.
func TestBulkSliceWritesTheSameBytes(t *testing.T) {
	orders := map[string]binary.ByteOrder{"little": binary.LittleEndian, "big": binary.BigEndian}
	for name, order := range orders {
		t.Run(name, func(t *testing.T) {
			rapid.Check(t, func(t *rapid.T) {
				// Long enough, sometimes, to span several chunks of the
				// encoding path.
				n := rapid.SampledFrom([]int{0, 1, 2, 7, 1023, 1024, 1025, 5000}).Draw(t, "n")
				u8 := rapid.SliceOfN(rapid.Uint8(), n, n).Draw(t, "u8")
				u16 := rapid.SliceOfN(rapid.Uint16(), n, n).Draw(t, "u16")
				u32 := rapid.SliceOfN(rapid.Uint32(), n, n).Draw(t, "u32")
				u64 := rapid.SliceOfN(rapid.Uint64(), n, n).Draw(t, "u64")

				puts := map[string]func(m MarshallWriterI){
					"uint8": func(m MarshallWriterI) { PutUint8SliceArg(m, convert(u8, func(v uint8) namedU8 { return namedU8(v) })) },
					"int8":  func(m MarshallWriterI) { PutInt8SliceArg(m, convert(u8, func(v uint8) namedI8 { return namedI8(v) })) },
					"uint16": func(m MarshallWriterI) {
						PutUint16SliceArg(m, convert(u16, func(v uint16) namedU16 { return namedU16(v) }))
					},
					"int16": func(m MarshallWriterI) {
						PutInt16SliceArg(m, convert(u16, func(v uint16) namedI16 { return namedI16(v) }))
					},
					"uint32": func(m MarshallWriterI) {
						PutUint32SliceArg(m, convert(u32, func(v uint32) namedU32 { return namedU32(v) }))
					},
					"int32": func(m MarshallWriterI) {
						PutInt32SliceArg(m, convert(u32, func(v uint32) namedI32 { return namedI32(v) }))
					},
					"rune":    func(m MarshallWriterI) { PutRuneSliceArg(m, convert(u32, func(v uint32) rune { return rune(v) })) },
					"float32": func(m MarshallWriterI) { PutFloat32SliceArg(m, bits32(u32)) },
					"uint64": func(m MarshallWriterI) {
						PutUint64SliceArg(m, convert(u64, func(v uint64) namedU64 { return namedU64(v) }))
					},
					"int64": func(m MarshallWriterI) {
						PutInt64SliceArg(m, convert(u64, func(v uint64) namedI64 { return namedI64(v) }))
					},
					"float64": func(m MarshallWriterI) { PutFloat64SliceArg(m, bits64(u64)) },
				}
				for kind, put := range puts {
					bulk, elementwise, bw, ew := both(order, put)
					if !bytes.Equal(bulk, elementwise) {
						t.Fatalf("%s, %d elements: the bulk path wrote different bytes", kind, n)
					}
					if bw != ew || bw != len(bulk) {
						t.Fatalf("%s, %d elements: written counts %d and %d for %d bytes", kind, n, bw, ew, len(bulk))
					}
				}
			})
		})
	}
}

type countingWriter struct {
	bytes.Buffer
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(p)
}

// The point of the bulk path is the number of writes, so that is what says it
// was taken: the length and the elements, against the length and one write
// per element. In the wire's own byte order the elements are one write; in the
// other they are a write per chunk.
func TestBulkSliceIsTaken(t *testing.T) {
	vs := make([]float32, 5000)
	fail := func(err error) { panic(err) }

	var native, foreign, elementwise countingWriter
	nativeOrder, foreignOrder := binary.ByteOrder(binary.LittleEndian), binary.ByteOrder(binary.BigEndian)
	if !nativeLittleEndian {
		nativeOrder, foreignOrder = foreignOrder, nativeOrder
	}
	PutFloat32SliceArg(NewMarshaller(&native, nativeOrder, fail), vs)
	PutFloat32SliceArg(NewMarshaller(&foreign, foreignOrder, fail), vs)
	PutFloat32SliceArg(MarshallWriterI(elementOnly{NewMarshaller(&elementwise, nativeOrder, fail)}), vs)

	require.Equal(t, 1+len(vs), elementwise.writes)
	if _, views := unsafeSliceViewAvailable(); !views {
		// Under extrasafe a []float32 cannot be seen as its bit patterns, so
		// there is nothing to hand the bulk half: every path is element-wise.
		require.Equal(t, elementwise.writes, native.writes)
		require.Equal(t, elementwise.writes, foreign.writes)
		return
	}
	require.Equal(t, 2, native.writes)
	require.Equal(t, 1+(4*len(vs)+chunkBytes-1)/chunkBytes, foreign.writes)
}

// A nil slice and an empty one are different things on the wire, and stay so.
func TestBulkSliceKeepsNilApartFromEmpty(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		nilBytes, nilElementwise, _, _ := both(order, func(m MarshallWriterI) { PutFloat32SliceArg(m, []float32(nil)) })
		emptyBytes, emptyElementwise, _, _ := both(order, func(m MarshallWriterI) { PutFloat32SliceArg(m, []float32{}) })
		require.Equal(t, nilElementwise, nilBytes)
		require.Equal(t, emptyElementwise, emptyBytes)
		require.NotEqual(t, nilBytes, emptyBytes)

		nilBytes, nilElementwise, _, _ = both(order, func(m MarshallWriterI) { PutUint8SliceArg(m, []uint8(nil)) })
		emptyBytes, emptyElementwise, _, _ = both(order, func(m MarshallWriterI) { PutUint8SliceArg(m, []uint8{}) })
		require.Equal(t, nilElementwise, nilBytes)
		require.Equal(t, emptyElementwise, emptyBytes)
		require.NotEqual(t, nilBytes, emptyBytes)
	}
}

// The view a bulk write takes of a slice is read and let go: the caller may
// change the slice afterwards without changing what was written.
func TestBulkSliceDoesNotKeepTheCallersMemory(t *testing.T) {
	var buf bytes.Buffer
	m := NewMarshaller(&buf, binary.LittleEndian, func(err error) { panic(err) })
	vs := []float32{1, 2, 3}
	PutFloat32SliceArg(m, vs)
	written := bytes.Clone(buf.Bytes())
	vs[0], vs[1], vs[2] = 9, 9, 9
	require.Equal(t, written, buf.Bytes())
	require.Equal(t, uint32(3), binary.LittleEndian.Uint32(written))
	require.Equal(t, float32(1), math.Float32frombits(binary.LittleEndian.Uint32(written[4:])))
}

// unsafeSliceViewAvailable reports whether this build views slices as raw
// memory (it does not under the extrasafe tag).
func unsafeSliceViewAvailable() (b []byte, ok bool) {
	return unsafeperf.UnsafeSliceToBytes([]uint32{1})
}

func BenchmarkPutFloat32Slice(b *testing.B) {
	vs := make([]float32, 100_000)
	for i := range vs {
		vs[i] = float32(i)
	}
	var buf bytes.Buffer
	run := func(b *testing.B, m MarshallWriterI) {
		b.SetBytes(int64(4 * len(vs)))
		for range b.N {
			buf.Reset()
			PutFloat32SliceArg(m, vs)
		}
	}
	fail := func(err error) { panic(err) }
	b.Run("bulk", func(b *testing.B) { run(b, NewMarshaller(&buf, binary.LittleEndian, fail)) })
	b.Run("bulk-foreign-order", func(b *testing.B) { run(b, NewMarshaller(&buf, binary.BigEndian, fail)) })
	b.Run("elementwise", func(b *testing.B) { run(b, elementOnly{NewMarshaller(&buf, binary.LittleEndian, fail)}) })
}
