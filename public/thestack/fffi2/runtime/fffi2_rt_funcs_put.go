package runtime

import "unsafe"

// FFFI2 wire format encodes uintptr in a fixed 8-byte slot (PutUintptrArg
// writes 8, ReadUintptr reads 8 and casts back). Correct iff the platform's
// uintptr is at most 8 bytes wide. The constant below evaluates to 1/0 —
// a compile-time error — on hypothetical >64-bit platforms.
const _ = 1 / (8 / unsafe.Sizeof(uintptr(0)))

func PutBoolArg[D MarshallWriterI, T ~bool](marshaller D, v T) {
	if v {
		marshaller.WriteUint8(1)
	} else {
		marshaller.WriteUint8(0)
	}
}

func PutRuneArg[D MarshallWriterI, T ~rune](marshaller D, v T) {
	marshaller.WriteInt32(int32(v))
}

func PutUint8Arg[D MarshallWriterI, T ~uint8](marshaller D, v T) {
	marshaller.WriteUint8(uint8(v))
}

func PutUint16Arg[D MarshallWriterI, T ~uint16](marshaller D, v T) {
	marshaller.WriteUint16(uint16(v))
}

func PutUint32Arg[D MarshallWriterI, T ~uint32](marshaller D, v T) {
	marshaller.WriteUint32(uint32(v))
}

func PutUint64Arg[D MarshallWriterI, T ~uint64](marshaller D, v T) {
	marshaller.WriteUint64(uint64(v))
}

func PutInt8Arg[D MarshallWriterI, T ~int8](marshaller D, v T) {
	marshaller.WriteInt8(int8(v))
}

func PutInt16Arg[D MarshallWriterI, T ~int16](marshaller D, v T) {
	marshaller.WriteInt16(int16(v))
}

func PutInt32Arg[D MarshallWriterI, T ~int32](marshaller D, v T) {
	marshaller.WriteInt32(int32(v))
}

func PutInt64Arg[D MarshallWriterI, T ~int64](marshaller D, v T) {
	marshaller.WriteInt64(int64(v))
}

func PutStringArg[D MarshallWriterI, T ~string](marshaller D, v T) {
	marshaller.WriteString(string(v))
}

func PutBytesArg[D MarshallWriterI](marshaller D, v []byte) {
	marshaller.WriteBytes(v)
}

func PutStringsArg[D MarshallWriterI, T ~string](marshaller D, vs []T) {
	m := marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteString(string(v))
	}
}

func PutUintptrArg[D MarshallWriterI, T ~uintptr](marshaller D, v T) {
	marshaller.WriteUint64(uint64(uintptr(v)))
}

func PutFloat32Arg[D MarshallWriterI, T ~float32](marshaller D, v T) {
	marshaller.WriteFloat32(float32(v))
}

func PutFloat64Array4Arg[D MarshallWriterI, T ~float64](marshaller D, v [4]T) {
	marshaller.WriteFloat64(float64(v[0]))
	marshaller.WriteFloat64(float64(v[1]))
	marshaller.WriteFloat64(float64(v[2]))
	marshaller.WriteFloat64(float64(v[3]))
}

func PutBoolSliceArg[D MarshallWriterI, T ~bool](marshaller D, vs []T) {
	m := marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteBool(bool(v))
	}
}

func PutUint8SliceArg[D MarshallWriterI, T ~uint8](marshaller D, vs []T) {
	m := marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteUint8(uint8(v))
	}
}

func PutUint16SliceArg[D MarshallWriterI, T ~uint16](marshaller D, vs []T) {
	m := marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteUint16(uint16(v))
	}
}

func PutUint32SliceArg[D MarshallWriterI, T ~uint32](marshaller D, vs []T) {
	m := marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteUint32(uint32(v))
	}
}

func PutInt8SliceArg[D MarshallWriterI, T ~int8](marshaller D, vs []T) {
	m := marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteInt8(int8(v))
	}
}

func PutInt16SliceArg[D MarshallWriterI, T ~int16](marshaller D, vs []T) {
	m := marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteInt16(int16(v))
	}
}

func PutInt32SliceArg[D MarshallWriterI, T ~int32](marshaller D, vs []T) {
	m := marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteInt32(int32(v))
	}
}

func PutFloat32SliceArg[D MarshallWriterI, T ~float32](marshaller D, vs []T) {
	m := marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteFloat32(float32(v))
	}
}

func PutFloat64SliceArg[D MarshallWriterI, T ~float64](marshaller D, vs []T) {
	m := marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteFloat64(float64(v))
	}
}

func PutUint64SliceArg[D MarshallWriterI, T ~uint64](marshaller D, vs []T) {
	m := marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteUint64(uint64(v))
	}
}

func PutInt64SliceArg[D MarshallWriterI, T ~int64](marshaller D, vs []T) {
	m := marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteInt64(int64(v))
	}
}

func PutStringSliceArg[D MarshallWriterI, T ~string](marshaller D, vs []T) {
	m := marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteString(string(v))
	}
}

func PutRuneSliceArg[D MarshallWriterI, T ~rune](marshaller D, vs []T) {
	m := marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteInt32(int32(v))
	}
}

func PutFloat32Array2Arg[D MarshallWriterI, T ~float32](marshaller D, v [2]T) {
	marshaller.WriteFloat32(float32(v[0]))
	marshaller.WriteFloat32(float32(v[1]))
}

func PutFloat32Array3Arg[D MarshallWriterI, T ~float32](marshaller D, v [3]T) {
	marshaller.WriteFloat32(float32(v[0]))
	marshaller.WriteFloat32(float32(v[1]))
	marshaller.WriteFloat32(float32(v[2]))
}

func PutFloat32Array4Arg[D MarshallWriterI, T ~float32](marshaller D, v [4]T) {
	marshaller.WriteFloat32(float32(v[0]))
	marshaller.WriteFloat32(float32(v[1]))
	marshaller.WriteFloat32(float32(v[2]))
	marshaller.WriteFloat32(float32(v[3]))
}

func PutFloat64Arg[D MarshallWriterI, T ~float64](marshaller D, v T) {
	marshaller.WriteFloat64(float64(v))
}

func PutComplex64Arg[D MarshallWriterI, T ~complex64](marshaller D, v T) {
	marshaller.WriteComplex64(complex64(v))
}

func PutComplex128Arg[D MarshallWriterI, T ~complex128](marshaller D, v T) {
	marshaller.WriteComplex128(complex128(v))
}

func PutComplex64Array2Arg[D MarshallWriterI, T ~complex64](marshaller D, v [2]T) {
	marshaller.WriteComplex64(complex64(v[0]))
	marshaller.WriteComplex64(complex64(v[1]))
}
