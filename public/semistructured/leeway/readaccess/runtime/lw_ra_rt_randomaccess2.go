package runtime

import (
	"iter"
)

func NewRandomAccessTwoLevelLookupAccel[F IndexConstraintI, B IndexConstraintI, I, I2 IndexConstraintI](estLength int) *RandomAccessTwoLevelLookupAccel[F, B, I, I2] {
	return &RandomAccessTwoLevelLookupAccel[F, B, I, I2]{
		accel:  NewRandomAccessLookupAccel[F, B](estLength),
		row:    0,
		cards:  nil,
		ranger: nil,
	}
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) SetCurrentEntityIdx(row I) {
	inst.row = row
	b, e := inst.ranger.ValueOffsets(row)
	inst.accel.LoadCardinalities(inst.cards[b:e])
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) SetReleaser(releaser ReleasableI) {
	inst.relaser = releaser
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
	if inst.relaser != nil {
		inst.relaser.Release()
	}
}
func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) Reset() {
	inst.accel.Reset()
	inst.cards = inst.cards[:0]
	inst.ranger = nil
	inst.relaser = nil
}
