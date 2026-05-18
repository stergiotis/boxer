package bad

import "sync"

type Named struct {
	mu *sync.Mutex   // want CS003 here
	rw *sync.RWMutex // want CS003 here
}

type Embedded struct {
	*sync.Mutex // want CS003 here
}

func anonBad() {
	_ = struct {
		mu *sync.Mutex // want CS003 here
	}{}
}

type Suppressed struct {
	mu *sync.Mutex //boxer:lint disable=CS003 reason="testdata coverage of suppression"
}
