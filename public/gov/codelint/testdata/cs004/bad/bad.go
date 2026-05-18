package bad

import (
	"sync/atomic"
	"unsafe"
)

var counter int64
var u32 uint32
var p unsafe.Pointer

func ops() {
	atomic.StoreInt64(&counter, 1)             // want CS004 here
	_ = atomic.LoadInt64(&counter)             // want CS004 here
	_ = atomic.AddInt64(&counter, 1)           // want CS004 here
	_ = atomic.SwapInt64(&counter, 2)          // want CS004 here
	_ = atomic.CompareAndSwapInt64(&counter, 0, 1) // want CS004 here

	atomic.StoreUint32(&u32, 7)                // want CS004 here
	_ = atomic.LoadUint32(&u32)                // want CS004 here

	_ = atomic.LoadPointer(&p)                 // want CS004 here
}

func suppressed() {
	_ = atomic.LoadInt64(&counter) //boxer:lint disable=CS004 reason="testdata coverage of suppression"
}
