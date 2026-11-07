// Demo-registry enrollment for appstate (ADR-0057, keelson-app A8): one
// Demo draws the window over a seeded read of keelson('app_state') — a few
// entries of each kind across three apps, one app selected — set on the
// instance directly. No bus or poller runs under the tour, so the picture
// is the same on every machine.

package appstate

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "appstate-entries",
		Category: "Runtime",
		Title:    icons.PhBroom + " App state — what apps keep",
		Stage:    [2]float32{1100, 700},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindUX,
		Description: "The app-state manager over a seeded read: the app list with entry counts and sizes, and the selected app's " +
			"persisted values, saved workingsets and dragged column widths, each with its Delete.",
		Init:           tourInit,
		RenderStateful: tourRenderStateful,
		SourceFunc:     (*App).render,
	})
}

const (
	tourPlay  = "github.com/example/apps/play"
	tourTally = "github.com/example/apps/tally"
	tourEdit  = "github.com/example/apps/mdedit"
)

// tourRows is the seeded read, in the table's order: app, kind, key.
func tourRows() (rows entryCols) {
	rows.Add(entryRow{Kind: persiststore.KindPersist, AppId: tourEdit, Key: "draft", EntityId: "state/mdedit/draft", PayloadBytes: 4_812,
		WrittenAt: "2026-09-22T09:41:07Z", RunId: "run-2b9d0e", InstanceKey: 2})
	rows.Add(entryRow{Kind: persiststore.KindColumnWidth, AppId: tourPlay, Key: "column//subject/text", EntityId: "cw/play/column//subject/text",
		Detail: "260 pt at font 12", WrittenAt: "2026-09-22T10:02:31Z", RunId: "run-8f3a1c", InstanceKey: 1})
	rows.Add(entryRow{Kind: persiststore.KindColumnWidth, AppId: tourPlay, Key: "instance/3/table/kind/text", EntityId: "cw/play/instance/3/table/kind/text",
		Detail: "150 pt at font 12", WrittenAt: "2026-09-22T10:02:31Z", RunId: "run-8f3a1c", InstanceKey: 3})
	rows.Add(entryRow{Kind: persiststore.KindPersist, AppId: tourPlay, Key: "session", EntityId: "state/play/session", PayloadBytes: 1_206,
		Detail: "", WrittenAt: "2026-09-22T10:05:12Z", RunId: "run-8f3a1c", InstanceKey: 3})
	rows.Add(entryRow{Kind: persiststore.KindPersist, AppId: tourPlay, Key: "tabs", EntityId: "state/play/tabs", PayloadBytes: 318,
		WrittenAt: "2026-09-22T10:05:12Z", RunId: "run-8f3a1c", InstanceKey: 3})
	rows.Add(entryRow{Kind: persiststore.KindWorkingset, AppId: tourPlay, Key: "default", EntityId: "ws/play/default", PayloadBytes: 2_044,
		Detail: "playLaunch · closed by the person", WrittenAt: "2026-09-21T18:30:00Z", RunId: "run-77c0aa", InstanceKey: 1})
	rows.Add(entryRow{Kind: persiststore.KindWorkingset, AppId: tourTally, Key: "default", EntityId: "ws/tally/default", PayloadBytes: 9_731,
		Detail: "tallyLaunch · closed by the person", WrittenAt: "2026-09-21T18:31:40Z", RunId: "run-77c0aa", InstanceKey: 1})
	return
}

// tourInit builds a window bound to the tour's id stack with the seeded
// read landed and the first app selected.
func tourInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	inst.ids = ids
	inst.selected = tourPlay
	inst.mu.Lock()
	inst.snap = snapshot{rows: tourRows(), reads: true, lastNote: "cleared persist scratch of play"}
	inst.mu.Unlock()
	state = inst
	return
}

// tourRenderStateful draws the seeded window. The read time is set to now
// on every frame, so the status row reads "read 0s ago" whenever the frame
// is captured.
func tourRenderStateful(ids *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok || inst == nil {
		return
	}
	inst.ids = ids
	inst.mu.Lock()
	inst.snap.refreshed = time.Now()
	inst.mu.Unlock()
	inst.render()
}
