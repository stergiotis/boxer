package runtime

func (inst *Fffi2) GetBoolRetr() (r bool) {
	r = inst.GetUint8Retr() != 0
	return
}
func (inst *Fffi2) GetUint8Retr() (r uint8) {
	r = inst.unmarshaller.ReadUInt8()
	return
}
func (inst *Fffi2) GetUint16Retr() (r uint16) {
	r = inst.unmarshaller.ReadUInt16()
	return
}
func (inst *Fffi2) GetUint32Retr() (r uint32) {
	r = inst.unmarshaller.ReadUInt32()
	return
}
func (inst *Fffi2) GetUint64Retr() (r uint64) {
	r = inst.unmarshaller.ReadUInt64()
	return
}
func (inst *Fffi2) GetStringRetr() (r string) {
	r = inst.unmarshaller.ReadString()
	return
}
func (inst *Fffi2) GetIntRetr() (r int) {
	r = inst.unmarshaller.ReadInt()
	return
}
func (inst *Fffi2) GetFloat32Retr() (r float32) {
	r = inst.unmarshaller.ReadFloat32()
	return
}
func (inst *Fffi2) GetFloat32Array4Retr() (r [4]float32) {
	r[0] = inst.unmarshaller.ReadFloat32()
	r[1] = inst.unmarshaller.ReadFloat32()
	r[2] = inst.unmarshaller.ReadFloat32()
	r[3] = inst.unmarshaller.ReadFloat32()
	return
}
func (inst *Fffi2) GetFloat64Retr() (r float64) {
	r = inst.unmarshaller.ReadFloat64()
	return
}
func (inst *Fffi2) GetComplex64Retr() (r complex64) {
	r = inst.unmarshaller.ReadComplex64()
	return
}
func (inst *Fffi2) GetComplex128Retr() (r complex128) {
	r = inst.unmarshaller.ReadComplex128()
	return
}
func (inst *Fffi2) GetUintptrRetr() (r uintptr) {
	r = inst.unmarshaller.ReadUintptr()
	return
}
