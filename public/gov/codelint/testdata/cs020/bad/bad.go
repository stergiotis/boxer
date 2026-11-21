package bad

import (
	"context"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// Fetch roots a context of its own.
func Fetch(bus app.BusI) (reply []byte, err error) {
	return bus.Request(context.Background(), "s", nil) // want a CS020 finding here
}

type Store struct {
	ctx context.Context
	st  app.StorageI
}

// Save hides a storage round trip behind a ctx-less signature.
func (inst *Store) Save(key string, v []byte) (err error) {
	return inst.st.Set(inst.ctx, key, v) // want a CS020 finding here
}

// Later puts the root off.
func Later() (ctx context.Context) {
	return context.TODO() // want a CS020 finding here
}
