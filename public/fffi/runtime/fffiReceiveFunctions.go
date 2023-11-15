package runtime

func GetBoolRetr[T ~bool](inst *Fffi2) (r T) {
	r = GetUint8Retr[uint8](inst) != 0
	return
}
func GetUint8Retr[T ~uint8](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadUInt8())
	return
}
func GetUint16Retr[T ~uint16](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadUInt16())
	return
}
func GetUint32Retr[T ~uint32](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadUInt32())
	return
}
func GetUint64Retr[T ~uint64](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadUInt64())
	return
}
func GetStringRetr[T ~string](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadString())
	return
}
func GetIntRetr[T ~int](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadInt())
	return
}
func GetInt8Retr[T ~int8](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadInt8())
	return
}
func GetInt16Retr[T ~int16](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadInt16())
	return
}
func GetInt32Retr[T ~int32](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadInt32())
	return
}
func GetInt64Retr[T ~int64](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadInt64())
	return
}
func GetFloat32Retr[T ~float32](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadFloat32())
	return
}
func GetFloat32Array3Retr[T ~float32](inst *Fffi2) (r [3]T) {
	r[0] = T(inst.unmarshaller.ReadFloat32())
	r[1] = T(inst.unmarshaller.ReadFloat32())
	r[2] = T(inst.unmarshaller.ReadFloat32())
	return
}
func GetFloat32Array4Retr[T ~float32](inst *Fffi2) (r [4]T) {
	r[0] = T(inst.unmarshaller.ReadFloat32())
	r[1] = T(inst.unmarshaller.ReadFloat32())
	r[2] = T(inst.unmarshaller.ReadFloat32())
	r[3] = T(inst.unmarshaller.ReadFloat32())
	return
}
func GetFloat32Array2Retr[T ~float32](inst *Fffi2) (r [2]T) {
	r[0] = T(inst.unmarshaller.ReadFloat32())
	r[1] = T(inst.unmarshaller.ReadFloat32())
	return
}
func GetIntArray2Retr[T ~int](inst *Fffi2) (r [2]T) {
	r[0] = T(inst.unmarshaller.ReadInt())
	r[1] = T(inst.unmarshaller.ReadInt())
	return
}
func GetIntArray4Retr[T ~int](inst *Fffi2) (r [4]T) {
	r[0] = T(inst.unmarshaller.ReadInt())
	r[1] = T(inst.unmarshaller.ReadInt())
	r[2] = T(inst.unmarshaller.ReadInt())
	r[3] = T(inst.unmarshaller.ReadInt())
	return
}
func GetFloat64Retr[T ~float64](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadFloat64())
	return
}
func GetComplex64Retr[T ~complex64](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadComplex64())
	return
}
func GetComplex128Retr[T ~complex128](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadComplex128())
	return
}
func GetUintptrRetr[T ~uintptr](inst *Fffi2) (r T) {
	r = T(inst.unmarshaller.ReadUintptr())
	return
}

func GetFloat32SliceRetr[T ~float32](inst *Fffi2) (r []T) {
	l := inst.getSliceLength()
	r = make([]T, 0, l)
	u := inst.unmarshaller
	for i := uint32(0); i < l; i++ {
		r = append(r, T(u.ReadFloat32()))
	}
	return
}
func GetFloat64SliceRetr[T ~float64](inst *Fffi2) (r []T) {
	l := inst.getSliceLength()
	r = make([]T, 0, l)
	u := inst.unmarshaller
	for i := uint32(0); i < l; i++ {
		r = append(r, T(u.ReadFloat64()))
	}
	return
}
func GetUint8SliceRetr[T ~uint8](inst *Fffi2) (r []T) {
	l := inst.getSliceLength()
	r = make([]T, 0, l)
	u := inst.unmarshaller
	for i := uint32(0); i < l; i++ {
		r = append(r, T(u.ReadUInt8()))
	}
	return
}
func GetUint16SliceRetr[T ~uint16](inst *Fffi2) (r []T) {
	l := inst.getSliceLength()
	r = make([]T, 0, l)
	u := inst.unmarshaller
	for i := uint32(0); i < l; i++ {
		r = append(r, T(u.ReadUInt16()))
	}
	return
}
func GetUint32SliceRetr[T ~uint32](inst *Fffi2) (r []T) {
	l := inst.getSliceLength()
	r = make([]T, 0, l)
	u := inst.unmarshaller
	for i := uint32(0); i < l; i++ {
		r = append(r, T(u.ReadUInt32()))
	}
	return
}
func GetUint64SliceRetr[T ~uint64](inst *Fffi2) (r []T) {
	l := inst.getSliceLength()
	r = make([]T, 0, l)
	u := inst.unmarshaller
	for i := uint32(0); i < l; i++ {
		r = append(r, T(u.ReadUInt64()))
	}
	return
}
func GetInt8SliceRetr[T ~int8](inst *Fffi2) (r []T) {
	l := inst.getSliceLength()
	r = make([]T, 0, l)
	u := inst.unmarshaller
	for i := uint32(0); i < l; i++ {
		r = append(r, T(u.ReadInt8()))
	}
	return
}
func GetInt16SliceRetr[T ~int16](inst *Fffi2) (r []T) {
	l := inst.getSliceLength()
	r = make([]T, 0, l)
	u := inst.unmarshaller
	for i := uint32(0); i < l; i++ {
		r = append(r, T(u.ReadInt16()))
	}
	return
}
func GetInt32SliceRetr[T ~int32](inst *Fffi2) (r []T) {
	l := inst.getSliceLength()
	r = make([]T, 0, l)
	u := inst.unmarshaller
	for i := uint32(0); i < l; i++ {
		r = append(r, T(u.ReadInt32()))
	}
	return
}
func GetInt64SliceRetr[T ~int64](inst *Fffi2) (r []T) {
	l := inst.getSliceLength()
	r = make([]T, 0, l)
	u := inst.unmarshaller
	for i := uint32(0); i < l; i++ {
		r = append(r, T(u.ReadInt64()))
	}
	return
}
func GetIntSliceRetr[T ~int](inst *Fffi2) (r []T) {
	l := inst.getSliceLength()
	r = make([]T, 0, l)
	u := inst.unmarshaller
	for i := uint32(0); i < l; i++ {
		r = append(r, T(u.ReadInt()))
	}
	return
}
func (inst *Fffi2) getSliceLength() (r uint32) {
	r = inst.unmarshaller.ReadUInt32()
	return
}
