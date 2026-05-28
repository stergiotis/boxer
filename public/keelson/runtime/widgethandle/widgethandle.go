//go:build llm_generated_opus47

// Package widgethandle provides an opaque, runtime-scoped handle for widget IDs.
//
// A WidgetHandle is an XOR-obfuscated wrapper around an internal uint64 widget ID.
// The obfuscation secret is randomly generated at program startup, which means:
//   - Handles from one program run cannot be used in another.
//   - Persisting a handle to disk and reloading it will produce an invalid lookup
//     (silent miss), not a stale hit against an unrelated widget.
//
// This package is internal to thestack — only packages under thestack/ can import it.
package widgethandle

import (
	"math/rand/v2"
)

// secret is generated once at program startup. It changes every run,
// invalidating any handles that might have been persisted to disk.
var secret = rand.Uint64()

// WidgetHandle is an opaque reference to a widget, valid only during the
// current program run. It cannot be used to recover the raw widget ID
// outside of the thestack package tree.
type WidgetHandle uint64

// Make creates a WidgetHandle from a raw widget ID.
func Make(id uint64) WidgetHandle {
	return WidgetHandle(id ^ secret)
}

// Resolve recovers the raw widget ID from a WidgetHandle.
func (inst WidgetHandle) Resolve() uint64 {
	return uint64(inst) ^ secret
}

// IsZero reports whether inst is the zero-value handle (no widget).
func (inst WidgetHandle) IsZero() bool {
	return inst == 0
}

// NoWidget is the zero-value WidgetHandle, representing "no widget".
// Note: because of the XOR obfuscation, Make(0) != NoWidget.
var NoWidget = WidgetHandle(0)
