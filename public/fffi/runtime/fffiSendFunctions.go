package runtime

func AddBoolArg[T ~bool](inst *Fffi2, v T) {
	if v {
		inst.marshaller.WriteUInt8(1)
	} else {
		inst.marshaller.WriteUInt8(0)
	}
}
func AddRuneArg[T ~rune](inst *Fffi2, v T) {
	inst.marshaller.WriteInt32(int32(v))
}
func AddUintArg[T ~uint](inst *Fffi2, v T) {
	inst.marshaller.WriteUint(uint(v))
}
func AddUint8Arg[T ~uint8](inst *Fffi2, v T) {
	inst.marshaller.WriteUInt8(uint8(v))
}
func AddUint16Arg[T ~uint16](inst *Fffi2, v T) {
	inst.marshaller.WriteUInt16(uint16(v))
}
func AddUint32Arg[T ~uint32](inst *Fffi2, v T) {
	inst.marshaller.WriteUInt32(uint32(v))
}
func AddUint64Arg[T ~uint64](inst *Fffi2, v T) {
	inst.marshaller.WriteUInt64(uint64(v))
}
func AddInt64Arg[T ~int64](inst *Fffi2, v T) {
	inst.marshaller.WriteInt64(int64(v))
}
func AddStringArg[T ~string](inst *Fffi2, v T) {
	inst.marshaller.WriteString(string(v))
}
func AddBytesArg(inst *Fffi2, v []byte) {
	inst.marshaller.WriteBytes(v)
}
func AddIntArg[T ~int](inst *Fffi2, v T) {
	inst.marshaller.WriteInt(int(v))
}
func AddUintptrArg[T ~uintptr](inst *Fffi2, v T) {
	// FIXME pointer length
	inst.marshaller.WriteUInt64(uint64(uintptr(v)))
}
func AddIntArray2Arg[T ~int](inst *Fffi2, v [2]T) {
	inst.marshaller.WriteInt(int(v[0]))
	inst.marshaller.WriteInt(int(v[1]))
}
func AddIntArray3Arg[T ~int](inst *Fffi2, v [3]T) {
	inst.marshaller.WriteInt(int(v[0]))
	inst.marshaller.WriteInt(int(v[1]))
	inst.marshaller.WriteInt(int(v[2]))
}
func AddIntArray4Arg[T ~int](inst *Fffi2, v [4]T) {
	inst.marshaller.WriteInt(int(v[0]))
	inst.marshaller.WriteInt(int(v[1]))
	inst.marshaller.WriteInt(int(v[2]))
	inst.marshaller.WriteInt(int(v[3]))
}
func AddFloat32Arg[T ~float32](inst *Fffi2, v T) {
	inst.marshaller.WriteFloat32(float32(v))
}
func AddUint32SliceArg[T ~uint32](inst *Fffi2, vs []T) {
	m := inst.marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteUInt32(uint32(v))
	}
}
func AddRuneSliceArg[T ~rune](inst *Fffi2, vs []T) {
	m := inst.marshaller
	if vs == nil {
		m.WriteNilSlice()
		return
	}
	m.WriteSliceLength(len(vs))
	for _, v := range vs {
		m.WriteInt32(int32(v))
	}
}
func AddFloat32Array2Arg[T ~float32](inst *Fffi2, v [2]T) {
	inst.marshaller.WriteFloat32(float32(v[0]))
	inst.marshaller.WriteFloat32(float32(v[1]))
}
func AddFloat32Array3Arg[T ~float32](inst *Fffi2, v [3]T) {
	inst.marshaller.WriteFloat32(float32(v[0]))
	inst.marshaller.WriteFloat32(float32(v[1]))
	inst.marshaller.WriteFloat32(float32(v[2]))
}
func AddFloat32Array4Arg[T ~float32](inst *Fffi2, v [4]T) {
	inst.marshaller.WriteFloat32(float32(v[0]))
	inst.marshaller.WriteFloat32(float32(v[1]))
	inst.marshaller.WriteFloat32(float32(v[2]))
	inst.marshaller.WriteFloat32(float32(v[3]))
}
func AddFloat64Arg[T ~float64](inst *Fffi2, v T) {
	inst.marshaller.WriteFloat64(float64(v))
}
func AddComplex64Arg[T ~complex64](inst *Fffi2, v T) {
	inst.marshaller.WriteComplex64(complex64(v))
}
func AddComplex128Arg[T ~complex128](inst *Fffi2, v T) {
	inst.marshaller.WriteComplex128(complex128(v))
}
