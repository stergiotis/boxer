//go:build llm_generated_opus47

package regex_explorer

// Embedded entry point — hosts the regex explorer inside another widget's
// UI scope (typically an inspector window) without going through the
// runtimeapp registry. Mirrors [AppInstance.Frame]'s setup exactly:
// per-render package-`app`-pointer swap, instance-unique
// [c.WidgetIdStack] salt pushed via [c.IdScope], and the same
// idempotent [App.RunTripwire] kick. ADR-0026 amendment 2026-05-12
// already designed [RenderWindow]'s body so it works inside any
// caller-owned [c.Window] using only `*Inside` panel variants, so no
// panel-layout refactor is needed.
//
// One [EmbeddedApp] per host widget instance; reuse across frames so
// pattern / haystack / replacement state persists. The embedded app
// does not register itself with [runtimeapp.DefaultRegistry] — it
// lives entirely inside the host widget's lifetime.

import (
	"context"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// EmbeddedApp wraps a per-host *App and an instance-unique salt for
// rendering the regex explorer inside another widget's UI scope. The
// salt feeds [c.IdScope] before each [RenderWindow] call so the
// explorer's derived widget ids cannot collide with the host's other
// widgets or with another embedded regex_explorer in the same frame.
//
// Lifecycle:
//
//  1. host constructs once: e := [NewEmbedded](seed)
//  2. host optionally calls [EmbeddedApp.SetBus] with the BusI from its
//     MountContext to enable clickhouse-local queries (omit for
//     Go-side preview only — the inspector still works, CH-backed tabs
//     surface a clear "no bus attached" error)
//  3. host calls [EmbeddedApp.SetPattern] when the embedded pattern
//     should mirror a host source (e.g. on each false→true open of an
//     inspector toggle); subsequent edits inside the inspector stay
//     local to the EmbeddedApp until bidirectional inspector bridging
//     lands
//  4. host calls [EmbeddedApp.Render] each frame inside its parent UI
//     scope (e.g. inside a `c.Window` body)
type EmbeddedApp struct {
	state *App
	seed  uint64
}

// NewEmbedded constructs an EmbeddedApp with a fresh *App state and a
// caller-supplied salt. The state starts with a [runtimeapp.NoopBus];
// clickhouse-local queries return the noop's "no bus attached" error
// until [EmbeddedApp.SetBus] is called. The Go-side highlight preview,
// compile-error labels, and the cheatsheet panel all work without a bus.
//
// seed must be stable across frames for a given host-widget instance —
// typically the host derives it from its own scoped widget id (e.g.
// `uint64(c.MakeAbsoluteIdStr(scope))`) so two embedded explorers on
// the same screen draw under independent id namespaces.
func NewEmbedded(seed uint64) (inst *EmbeddedApp) {
	inst = &EmbeddedApp{
		state: newApp(),
		seed:  seed,
	}
	inst.state.bus = &runtimeapp.NoopBus{}
	return
}

// SetBus attaches the clickhouse-local-capable BusI to the embedded
// explorer. Typically the host's MountContext.Bus() — a per-app
// inprocbus client wired to the chlocalbroker service. Passing nil
// reverts to [runtimeapp.NoopBus] (CH-backed queries fail with a clear
// error; Go-side preview still works).
//
// Safe to call between frames; in-flight goroutines spawned before the
// swap retain their captured *App pointer and continue using the
// previous bus value, so the swap takes effect from the next
// [App.RunMatch] / [App.RunExtractAll] / etc. dispatch.
func (inst *EmbeddedApp) SetBus(bus runtimeapp.BusI) {
	if bus == nil {
		inst.state.bus = &runtimeapp.NoopBus{}
		return
	}
	inst.state.bus = bus
}

// SetPattern seeds the embedded explorer's pattern field. The caller
// typically calls this once on each open of the inspector window
// (false→true toggle transition) so the inspector starts mirrored to
// the host's source pattern; subsequent edits inside the inspector are
// local to the EmbeddedApp and do not flow back. Bidirectional
// propagation is deferred until inspector bridging lands.
func (inst *EmbeddedApp) SetPattern(p string) {
	inst.state.mu.Lock()
	inst.state.pattern = p
	inst.state.mu.Unlock()
}

// Pattern returns the embedded explorer's current pattern. Useful for
// hosts that want to surface "user has changed the pattern in the
// inspector" feedback (e.g. a small dirty marker) in their level-1
// summary row.
func (inst *EmbeddedApp) Pattern() (p string) {
	inst.state.mu.RLock()
	p = inst.state.pattern
	inst.state.mu.RUnlock()
	return
}

// Render renders the regex explorer body into the current UI scope.
// The caller must wrap this in a parent scope that owns layout (e.g.
// a `c.Window` body or a `c.Vertical` block) — per ADR-0026 the body
// uses only `*Inside` panel variants and does not own its own window
// chrome.
//
// Internally performs the same package-level `app` pointer swap that
// [AppInstance.Frame] does, then pushes the per-instance salt onto
// the per-state [c.WidgetIdStack] via [c.IdScope] before calling
// [RenderWindow]. The swap is safe under the single-threaded Go render
// loop; defer restores the previous pointer on return so nested hosts
// (one EmbeddedApp inside another's inspector window) still see the
// correct state when control returns to them.
//
// Kicks off the SD1 engine-fidelity tripwire on the first call
// (coalesced by [App.tripwireRan] on the per-instance state) so the
// status bar's "SD1: ✓" / "SD1: DRIFT" indicator reflects the
// embedded explorer just like the standalone window does.
func (inst *EmbeddedApp) Render() {
	prev := app
	app = inst.state
	defer func() { app = prev }()

	inst.state.RunTripwire(context.Background())
	inst.state.ids.Reset()
	for range c.IdScope(inst.state.ids.PrepareSeq(inst.seed)) {
		RenderWindow()
	}
}
