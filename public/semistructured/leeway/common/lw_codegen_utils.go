package common

import "slices"

func IterateColumnPropsMultiIntermediatePairHolders(irhs ...*IntermediatePairHolder) IntermediateColumnIterator {
	return func(yield func(IntermediateColumnContext, *IntermediateColumnProps) bool) {
		for _, irh := range irhs {
			for cc, cp := range irh.IterateColumnProps() {
				if !yield(cc, cp) {
					return
				}
			}
		}
	}
}
func NewIntermediatePairHolder(nEst int) *IntermediatePairHolder {
	return &IntermediatePairHolder{
		ccs: make([]IntermediateColumnContext, 0, nEst),
		cps: make([]*IntermediateColumnProps, 0, nEst),
	}
}
func (inst *IntermediatePairHolder) Concat(other *IntermediatePairHolder) {
	inst.ccs = slices.Concat(inst.ccs, other.ccs)
	inst.cps = slices.Concat(inst.cps, other.cps)
}
func (inst *IntermediatePairHolder) Load(iter IntermediateColumnIterator) {
	for cc, cp := range iter {
		inst.Add(cc, cp)
	}
}
func (inst *IntermediatePairHolder) Add(cc IntermediateColumnContext, cp *IntermediateColumnProps) {
	inst.ccs = append(inst.ccs, cc)
	inst.cps = append(inst.cps, cp)
}
func (inst *IntermediatePairHolder) Length() int {
	return len(inst.ccs)
}
func (inst *IntermediatePairHolder) CountColumns() (nColumns int) {
	for _, cp := range inst.cps {
		nColumns += cp.Length()
	}
	return
}
func (inst *IntermediatePairHolder) IterateColumnProps() IntermediateColumnIterator {
	return func(yield func(IntermediateColumnContext, *IntermediateColumnProps) bool) {
		for i, cc := range inst.ccs {
			if !yield(cc, inst.cps[i]) {
				return
			}
		}
	}
}
func (inst *IntermediatePairHolder) DeriveSubHolder(filter func(cc IntermediateColumnContext) (keep bool)) (r *IntermediatePairHolder) {
	r = NewIntermediatePairHolder(inst.Length())
	for cc, cp := range inst.IterateColumnProps() {
		if filter(cc) {
			r.Add(cc, cp)
		}
	}
	return
}
func (inst *IntermediatePairHolder) Reset() {
	inst.ccs = inst.ccs[:0]
	clear(inst.cps)
	inst.cps = inst.cps[:0]
}
