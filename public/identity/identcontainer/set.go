package identcontainer

import (
	"bytes"
	"io"

	"iter"

	"github.com/RoaringBitmap/roaring/roaring64"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
)

type TaggedIdSet struct {
	bm *roaring64.Bitmap
}

var _ Roaring64Serializable = (*TaggedIdSet)(nil)

func (inst *TaggedIdSet) Iterate() iter.Seq[identifier.TaggedId] {
	return func(yield func(identifier.TaggedId) bool) {
		l := inst.bm.Iterator()
		for l.HasNext() {
			n := l.Next()
			if !yield(identifier.TaggedId(n)) {
				return
			}
		}
	}
}

var _ commonInterface = (*TaggedIdSet)(nil)

func NewTaggedIdSet() *TaggedIdSet {
	bm := roaring64.New()
	return &TaggedIdSet{bm: bm}
}

func (inst *TaggedIdSet) Intersect(other *TaggedIdSet) {
	inst.bm.And(other.bm)
}

func (inst *TaggedIdSet) IntersectionCardinality(other *TaggedIdSet) (card uint64) {
	return inst.bm.AndCardinality(other.bm)
}

func (inst *TaggedIdSet) Diff(other *TaggedIdSet) {
	inst.bm.AndNot(other.bm)
}

func (inst *TaggedIdSet) DifferenceCardinality(other *TaggedIdSet) (card uint64) {
	return inst.bm.GetCardinality() - inst.bm.AndCardinality(other.bm)
}

func (inst *TaggedIdSet) Union(set *TaggedIdSet) {
	inst.bm.Or(set.bm)
}

func (inst *TaggedIdSet) UnionCard(set *TaggedIdSet) (card uint64) {
	return inst.bm.OrCardinality(set.bm)
}

func (inst *TaggedIdSet) AddMember(id identifier.TaggedId) {
	inst.bm.Add(uint64(id))
}

func (inst *TaggedIdSet) RemoveMember(id identifier.TaggedId) {
	inst.bm.Remove(uint64(id))
}

func (inst *TaggedIdSet) Optimize() {
	inst.bm.RunOptimize()
}

func (inst *TaggedIdSet) Clear() {
	inst.bm.Clear()
}

func (inst *TaggedIdSet) Clone() *TaggedIdSet {
	return &TaggedIdSet{bm: inst.bm.Clone()}
}

func (inst *TaggedIdSet) IsEmpty() bool {
	return inst.bm.IsEmpty()
}

func (inst *TaggedIdSet) Cardinality() uint64 {
	return inst.bm.GetCardinality()
}

func (inst *TaggedIdSet) Length() uint64 {
	return inst.bm.GetCardinality()
}

func (inst *TaggedIdSet) Min() identifier.TaggedId {
	return identifier.TaggedId(inst.bm.Minimum())
}

func (inst *TaggedIdSet) Max() identifier.TaggedId {
	return identifier.TaggedId(inst.bm.Maximum())
}

func (inst *TaggedIdSet) Rank(id identifier.TaggedId) uint64 {
	return inst.bm.Rank(uint64(id))
}

func (inst *TaggedIdSet) WriteTo(out io.Writer) (int64, error) {
	return inst.bm.WriteTo(out)
}

func (inst *TaggedIdSet) WriteRoaring64(dest io.Writer) (n int64, err error) {
	return inst.bm.WriteTo(dest)
}

func (inst *TaggedIdSet) ReadFrom(in io.Reader) (int64, error) {
	return inst.bm.ReadFrom(in)
}

func (inst *TaggedIdSet) Serialize() (r Roaring64Bytes, err error) {
	bm := inst.bm
	buf := bytes.NewBuffer(make([]byte, 0, bm.GetSizeInBytes()+128))
	_, err = inst.WriteRoaring64(buf)
	if err != nil {
		err = eh.Errorf("unable to serialize roaring64 bitmap: %w", err)
		return
	}
	r = buf.Bytes()
	return
}
