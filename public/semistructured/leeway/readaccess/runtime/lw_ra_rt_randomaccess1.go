package runtime

import (
	"iter"
	"slices"
)


func NewRandomAccessLookupAccel[F IndexConstraintI, B IndexConstraintI](estLength int) *RandomAccessLookupAccel[F, B] {
	return &RandomAccessLookupAccel[F, B]{
		forwardBeginIncl: make([]F, 0, estLength),
		forwardEndExcl:   make([]F, 0, estLength),
		backward:         make([]B, 0, estLength),
		len:              0,
	}
}

func (inst *RandomAccessLookupAccel[F, B]) LookupForward(i B) (beginIncl F, endExcl F) {
	beginIncl = inst.forwardBeginIncl[i]
	endExcl = inst.forwardEndExcl[i]
	return
}
func (inst *RandomAccessLookupAccel[F, B]) LookupForwardRange(i B) (r Range[F]) {
	r.BeginIncl = inst.forwardBeginIncl[i]
	r.EndExcl = inst.forwardEndExcl[i]
	return
}
func (inst *RandomAccessLookupAccel[F, B]) LookupForwardIndexedRange(i B) (r IndexedRange[F, B]) {
	r.BeginIncl = inst.forwardBeginIncl[i]
	r.EndExcl = inst.forwardEndExcl[i]
	r.Index = i
	r.Length = inst.len
	return
}
func (inst *RandomAccessLookupAccel[F, B]) LookupBackward(i F) (index B) {
	index = inst.backward[i]
	return
}
func (inst *RandomAccessLookupAccel[F, B]) GetCardinality(i B) (card uint64) {
	card = uint64(inst.forwardEndExcl[i] - inst.forwardBeginIncl[i])
	return
}
func (inst *RandomAccessLookupAccel[F, B]) IterateAllFwdIndexedRange() iter.Seq[IndexedRange[F, B]] {
	return func(yield func(IndexedRange[F, B]) bool) {
		var r IndexedRange[F, B]
		r.Length = len(inst.forwardBeginIncl)
		exs := inst.forwardEndExcl
		for i, b := range inst.forwardBeginIncl {
			r.BeginIncl = b
			r.EndExcl = exs[i]
			r.Index = B(i)
			if !yield(r) {
				break
			}
		}
	}
}
func (inst *RandomAccessLookupAccel[F, B]) IterateAllFwdRange() iter.Seq[Range[F]] {
	return func(yield func(Range[F]) bool) {
		exs := inst.forwardEndExcl
		for i, b := range inst.forwardBeginIncl {
			var r Range[F]
			r.BeginIncl = b
			r.EndExcl = exs[i]
			if !yield(r) {
				break
			}
		}
	}
}
func (inst *RandomAccessLookupAccel[F, B]) LoadCardinalities(cards []uint64) {
	var pfxSum F
	l := len(cards)
	{
		fwdBeginIncl := slices.Grow(inst.forwardBeginIncl[:0], l)
		fwdEndExcl := slices.Grow(inst.forwardEndExcl[:0], l)

		for _, card := range cards {
			fwdBeginIncl = append(fwdBeginIncl, pfxSum)
			pfxSum += F(card)
			fwdEndExcl = append(fwdEndExcl, pfxSum)
		}
		inst.forwardBeginIncl = fwdBeginIncl
		inst.forwardEndExcl = fwdEndExcl
	}
	{
		bwd := slices.Grow(inst.backward[:0], int(pfxSum))
		for i, card := range cards {
			for j := 0; j < int(card); j++ {
				bwd = append(bwd, B(i))
			}
		}
		inst.backward = bwd
	}
	inst.len = l
}
