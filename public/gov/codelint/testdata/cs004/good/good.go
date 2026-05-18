package good

import (
	"sync/atomic"
)

type Counter struct {
	n atomic.Int64
	p atomic.Pointer[string]
	b atomic.Bool
}

func (inst *Counter) use() {
	inst.n.Store(1)
	_ = inst.n.Load()
	inst.n.Add(1)
	_ = inst.n.Swap(2)
	_ = inst.n.CompareAndSwap(0, 1)

	s := "hi"
	inst.p.Store(&s)
	_ = inst.p.Load()

	inst.b.Store(true)
	_ = inst.b.Load()
}
