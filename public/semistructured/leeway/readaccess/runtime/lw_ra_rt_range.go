package runtime

func (inst Range[T]) ToRange() (r Range[T]) {
	r = inst
	return
}
func (inst IndexedRange[R, I]) ToRange() (r Range[R]) {
	r.BeginIncl = inst.BeginIncl
	r.EndExcl = inst.EndExcl
	return
}
func (inst Range[T]) IsEmpty() bool {
	return inst.EndExcl == inst.BeginIncl
}
func (inst IndexedRange[R, I]) IsEmpty() bool {
	return inst.EndExcl == inst.BeginIncl
}
func (inst Range[T]) CalcCardinality() (card uint64) {
	card = uint64(inst.EndExcl - inst.BeginIncl)
	return
}
func (inst IndexedRange[R, I]) CalcCardinality() (card uint64) {
	card = uint64(inst.EndExcl - inst.BeginIncl)
	return
}
