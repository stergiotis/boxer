package good

import (
	"context"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// Fetch takes its ctx from the caller.
func Fetch(ctx context.Context, bus app.BusI) (reply []byte, err error) {
	return bus.Request(ctx, "s", nil)
}

// fetch is unexported: the exported surface is what the rule holds.
func fetch(bus app.BusI) (reply []byte, err error) {
	return bus.Request(context.Background(), "s", nil)
}

// Root is a documented root.
func Root() (ctx context.Context) {
	return context.Background() //boxer:lint disable=CS020 reason="the documented root of a test fixture"
}

func init() {
	_, _ = fetch(nil)
	_ = context.Background()
}
