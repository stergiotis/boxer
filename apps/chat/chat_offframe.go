package chat

import "context"

// offFrame runs f off the render goroutine, counted into the window's
// lifetime scope: the host waits for it at close before it closes the bus
// (ADR-0322). Without a host the window has no scope and f runs on a
// goroutine of its own. Once the scope retired nothing starts — the window
// is closing — and started says so.
func (inst *App) offFrame(f func(ctx context.Context)) (started bool) {
	if inst.scope == nil {
		go f(context.Background())
		return true
	}
	return inst.scope.Go(f)
}
