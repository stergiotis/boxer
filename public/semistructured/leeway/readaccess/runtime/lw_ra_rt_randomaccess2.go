package runtime

import (
	"iter"
)

func NewRandomAccessTwoLevelLookupAccel[F IndexConstraintI, B IndexConstraintI, I, I2 IndexConstraintI](estLength int) *RandomAccessTwoLevelLookupAccel[F, B, I, I2] {
	return &RandomAccessTwoLevelLookupAccel[F, B, I, I2]{
		accel:   NewRandomAccessLookupAccel[F, B](estLength),
		current: 0,
		cards:   nil,
		ranger:  nil,
		loaded:  false,
	}
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) SetCurrentEntityIdx(current I) {
	if inst.current == current && inst.loaded {
		return
	}
	inst.current = current
	b, e := inst.ranger.ValueOffsets(current)
	inst.accel.LoadCardinalities(inst.cards[b:e])
	inst.loaded = true
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) SetReleaser(releaser ReleasableI) {
	inst.releaser = releaser
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) SetRanger(ranger ValueOffsetI[I, I2]) {
	inst.ranger = ranger
}

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) LoadCardinalities(cards []uint64) {
	inst.cards = cards
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) LookupForward(i B) (beginIncl F, endExcl F) {
	return inst.accel.LookupForward(i)
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) LookupForwardRange(i B) (r Range[F]) {
	return inst.accel.LookupForwardRange(i)
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) LookupForwardIndexedRange(i B) (r IndexedRange[F, B]) {
	return inst.accel.LookupForwardIndexedRange(i)
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) LookupBackward(i F) (index B) {
	return inst.accel.LookupBackward(i)
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) GetCardinality(i B) (card uint64) {
	return inst.accel.GetCardinality(i)
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) IterateAllFwdIndexedRange() iter.Seq[IndexedRange[F, B]] {
	return inst.accel.IterateAllFwdIndexedRange()
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) IterateAllFwdRange() iter.Seq[Range[F]] {
	return inst.accel.IterateAllFwdRange()
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) Len() int {
	return len(inst.cards)
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) Release() {
	if inst.releaser != nil {
		inst.releaser.Release()
	}
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) Reset() {
	inst.accel.Reset()
	inst.cards = inst.cards[:0]
	inst.ranger = nil
	inst.releaser = nil
	inst.loaded = false
}
