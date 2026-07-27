package regex_explorer

// Embedded entry point — hosts the regex explorer inside another widget's
// UI scope (typically an inspector window) without going through the
// runtimeapp registry. Mirrors [AppInstance.Frame]'s setup, plus an
// instance-unique [c.WidgetIdStack] salt pushed via [c.IdScope] so
// several embedded explorers can share a screen, and the same idempotent
// [App.RunTripwire] kick. ADR-0026 amendment 2026-05-12 already designed
// [App.RenderWindow]'s body so it works inside any caller-owned
// [c.Window] using only `*Inside` panel variants, so no panel-layout
// refactor is needed.
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
	inst.state.setBus(&runtimeapp.NoopBus{})
	return
}

// SetBus attaches the clickhouse-local-capable BusI to the embedded
// explorer. Typically the host's MountContext.Bus() — a per-app
// inprocbus client wired to the chlocalbroker service. Passing nil
// reverts to [runtimeapp.NoopBus] (CH-backed queries fail with a clear
// error; Go-side preview still works).
//
// Safe to call from the render thread at any time, including while
// queries are in flight: the swap goes through [App.setBus] under the
// state lock, and a query that has already started keeps the transport
// [App.busSnapshot] handed it. Hosts that re-push a bus every frame
// (regexsummary does) therefore cost one uncontended lock per frame and
// nothing else.
func (inst *EmbeddedApp) SetBus(bus runtimeapp.BusI) {
	if bus == nil {
		inst.state.setBus(&runtimeapp.NoopBus{})
		return
	}
	inst.state.setBus(bus)
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

// Render renders the regex explorer body into the current UI scope.
// The caller must wrap this in a parent scope that owns layout (e.g.
// a `c.Window` body or a `c.Vertical` block) — per ADR-0026 the body
// uses only `*Inside` panel variants and does not own its own window
// chrome.
//
// Pushes the per-instance salt onto the state's [c.WidgetIdStack] via
// [c.IdScope] before calling [App.RenderWindow], so nested hosts (one
// EmbeddedApp inside another's inspector window) each draw under their
// own id namespace.
//
// Kicks off the SD1 engine-fidelity tripwire on the first call
// (coalesced by [App.tripwireRan] on the per-instance state) so the
// status bar's "SD1: ✓" / "SD1: DRIFT" indicator reflects the
// embedded explorer just like the standalone window does.
func (inst *EmbeddedApp) Render() {
	inst.state.RunTripwire(context.Background())
	inst.state.ids.Reset()
	for range c.IdScope(inst.state.ids.PrepareSeq(inst.seed)) {
		inst.state.RenderWindow()
	}
}

// Close abandons in-flight queries and retracts anything the embedded
// explorer published through the ADR-0017 extraction hand-off. Hosts that
// know when their embedded explorer dies should call it.
//
// The gap, stated plainly (ADR-0017 §SD5): there is no teardown hook on
// the embedded path, and nothing in-tree calls this today —
// [regexsummary] keeps its embedded explorers alive for the widget's
// lifetime, which in practice is the process's. So an embedded explorer
// that published holds its handles until exit. The exposure is bounded:
// at most two handles per instance (they are reused on republish) and the
// store is ephemeral.
//
// In practice the embedded case degrades earlier and more visibly than
// that gap suggests. The host's bus client carries the *host app's*
// manifest caps, which will not include adhoc.publish, so the publish is
// refused with a reason in the status line rather than silently doing
// nothing.
//
// Safe to call more than once, and safe on an explorer that never
// published.
func (inst *EmbeddedApp) Close() {
	if inst.state == nil {
		return
	}
	inst.state.cancelQueries()
	inst.state.retractEvalDatasets()
}
