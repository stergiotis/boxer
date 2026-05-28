package identcontainer

import (
	"iter"
	"slices"

	"github.com/stergiotis/boxer/public/identity/identifier"
)

type TaggedIdSeq struct {
	seq []identifier.TaggedId
}

func NewTaggedIdSeq(estLength int) *TaggedIdSeq {
	return &TaggedIdSeq{
		seq: make([]identifier.TaggedId, 0, estLength),
	}
}

func (inst *TaggedIdSeq) Iterate() iter.Seq[identifier.TaggedId] {
	return slices.Values(inst.seq)
}

func (inst *TaggedIdSeq) Clear() {
	inst.seq = inst.seq[:0]
}

func (inst *TaggedIdSeq) Push(id identifier.TaggedId) {
	inst.seq = append(inst.seq, id)
}

func (inst *TaggedIdSeq) Pop() {
	if len(inst.seq) == 0 {
		return
	}
	inst.seq = inst.seq[:len(inst.seq)-1]
}

func (inst *TaggedIdSeq) Last() (last identifier.TaggedId) {
	if len(inst.seq) == 0 {
		return
	}
	last = inst.seq[len(inst.seq)-1]
	return
}

func (inst *TaggedIdSeq) ReplaceLastUnchecked(id identifier.TaggedId) {
	inst.seq[len(inst.seq)-1] = id
}

func (inst *TaggedIdSeq) GetUnchecked(i int) (element identifier.TaggedId) {
	return inst.seq[i]
}

func (inst *TaggedIdSeq) SetUnchecked(i int, id identifier.TaggedId) {
	inst.seq[i] = id
}

func (inst *TaggedIdSeq) Reserve(length int) {
	if cap(inst.seq) < length {
		t := inst.seq
		n := make([]identifier.TaggedId, 0, length)
		n = append(n, t...)
		inst.seq = n
	}
}

func (inst *TaggedIdSeq) Capacity() int {
	return cap(inst.seq)
}

func (inst *TaggedIdSeq) ResliceUnchecked(fromIncl int, toExcl int) {
	inst.seq = inst.seq[fromIncl:toExcl]
}

func (inst *TaggedIdSeq) Length() uint64 {
	return uint64(len(inst.seq))
}

func (inst *TaggedIdSeq) Optimize() {
	// no-op
}

func (inst *TaggedIdSeq) IsEmpty() bool {
	return len(inst.seq) == 0
}

func (inst *TaggedIdSeq) Clone() *TaggedIdSeq {
	s := make([]identifier.TaggedId, 0, cap(inst.seq))
	s = append(s, inst.seq...)
	return &TaggedIdSeq{
		seq: s,
	}
}

var _ commonInterface = (*TaggedIdSeq)(nil)
