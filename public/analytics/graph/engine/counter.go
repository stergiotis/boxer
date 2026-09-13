package engine

import "sync/atomic"

// atomicCounter hands out job indices to workers. Which worker gets which
// chunk is scheduling-dependent; the results are folded by chunk index, so
// that does not reach the output.
type atomicCounter struct {
	v atomic.Int64
}

func (c *atomicCounter) claim() int {
	return int(c.v.Add(1) - 1)
}
