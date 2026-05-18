package good

import "context"

func freeFunc(ctx context.Context, x int) (err error) {
	_ = ctx
	_ = x
	return
}

type T struct{}

func (inst *T) method(ctx context.Context, name string) (err error) {
	_ = ctx
	_ = name
	return
}

type Doer interface {
	Do(ctx context.Context, payload []byte) (err error)
}

type Handler = func(ctx context.Context, msg string) (err error)

func noCtx(x int, y int) (n int) {
	n = x + y
	return
}

func anonCtxFirst(_ context.Context, x int) (err error) {
	_ = x
	return
}
