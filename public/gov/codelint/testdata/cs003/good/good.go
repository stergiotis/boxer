package good

import "sync"

type ByValue struct {
	mu sync.Mutex
	rw sync.RWMutex
}

type Embedded struct {
	sync.Mutex
}

func borrow(mu *sync.Mutex) { // pointer as param is fine
	mu.Lock()
	mu.Unlock()
}

func anonOK() {
	_ = struct {
		mu sync.Mutex
	}{}
}
