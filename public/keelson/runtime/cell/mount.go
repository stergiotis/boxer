package cell

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// MountKey is the key the host's declaration is handed to an app under
// (app.MountValuesI): the one copy the host built and its services
// enforce, so an app labelling content by the declaration reads that copy
// and not the environment again.
type MountKey struct{}

// OfMount is the declaration ctx carries, ok false when the host hands
// none — a host without a cell, a test, a tab.
func OfMount(ctx app.MountContextI) (d Declaration, ok bool) {
	p, ok := app.MountValueOf(ctx, MountKey{}).(*Declaration)
	if !ok || p == nil {
		return Declaration{}, false
	}
	return *p, true
}
