package bad

import "context"

func wrongOrder(x int, ctx context.Context) (err error) { // want CS002 here
	_ = ctx
	_ = x
	return
}

type T struct{}

func (inst *T) methodWrong(name string, ctx context.Context) (err error) { // want CS002 here
	_ = ctx
	_ = name
	return
}

type Doer interface {
	Do(payload []byte, ctx context.Context) (err error) // want CS002 here
}

func anonCtxWrong(x int, _ context.Context) (err error) { // want CS002 here
	_ = x
	return
}

func suppressed(x int, ctx context.Context) (err error) { //boxer:lint disable=CS002 reason="testdata coverage of suppression"
	_ = ctx
	_ = x
	return
}
