package utils

import (
	"runtime"
)

// FinalizeWrapper
// Will run fin as finalizer when the wrapper object becomes unreachable _and_ is garbage collected.
// Note that using finalizers to manage resource heavy (e.g. memory regions) or scarce objects (e.g. file handles) is
// generally considered to be an anti-pattern (i.e. bad idea).
type FinalizeWrapper[T any] struct {
	obj T
	fin func(obj T)
}

func finalizeFunc[T any](obj *FinalizeWrapper[T]) {
	obj.fin(obj.obj)
}

func NewFinalizeWrapper[T any](obj T, fin func(obj T)) *FinalizeWrapper[T] {
	t := &FinalizeWrapper[T]{
		obj: obj,
		fin: fin,
	}
	runtime.SetFinalizer(t, finalizeFunc[T])
	return t
}

// Get May return a stale/invalidated obj if ForceFinalize() has been called before.
func (inst *FinalizeWrapper[T]) Get() (obj T) {
	return inst.obj
}
func (inst *FinalizeWrapper[T]) ForceFinalize() {
	fin := inst.fin
	runtime.SetFinalizer(inst, nil)
	// run in go routine like a true finalizer
	go func() {
		fin(inst.obj)
	}()
	return
}
