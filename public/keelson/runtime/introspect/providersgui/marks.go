package providersgui

import (
	"strconv"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/inscribe"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// TableMarks is keelson('inscribe_marks'): the marks agents put on screen
// (ADR-0297), one row per mark, read from the host's scene.
const TableMarks = "inscribe_marks"

// RegisterMarks registers a marks provider bound to host into r. A nil
// host answers with an empty table.
func RegisterMarks(r *introspect.Registry, host *windowhost.Inst) error {
	return r.Register(marksProvider{host: host})
}

type marksProvider struct{ host *windowhost.Inst }

func (marksProvider) Name() string                         { return TableMarks }
func (marksProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (marksProvider) Schema() *arrow.Schema                { return marksTable(nil).Schema() }

// Snapshot reads the scene, which is safe off the render goroutine: the
// agent service writes it from the bus.
func (inst marksProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var items []inscribe.Item
	if inst.host != nil {
		items = inst.host.Marks().Snapshot()
	}
	return marksTable(items).Build(proj, len(items)), nil
}

// targetOf spells an anchor: "window:<key>" for a window, with
// "@x,y,w,h" for a rect within it, and "viewport@x,y,w,h" for a rect of
// the viewport.
func targetOf(a inscribe.Anchor) (s string) {
	rect := func(r *inscribe.Rect) string {
		f := func(v float32) string { return strconv.FormatFloat(float64(v), 'f', -1, 32) }
		return "@" + f(r.X) + "," + f(r.Y) + "," + f(r.W) + "," + f(r.H)
	}
	if a.Viewport != nil {
		return "viewport" + rect(a.Viewport)
	}
	s = "window:" + strconv.FormatUint(a.Window, 10)
	if a.Local != nil {
		s += rect(a.Local)
	}
	return
}

func marksTable(items []inscribe.Item) *introspect.Table {
	r := func(i int) *inscribe.Item { return &items[i] }
	return introspect.NewTable().
		String("task", func(i int) string { return r(i).Task }).
		String("id", func(i int) string { return r(i).Id }).
		// highlight, callout, arrow, step or spotlight.
		String("op", func(i int) string { return r(i).Op.String() }).
		String("text", func(i int) string { return r(i).Text }).
		Int32("hue", func(i int) int32 { return int32(r(i).Hue) }).
		// The step's number, zero for any other op.
		Int32("step", func(i int) int32 { return int32(r(i).Step) }).
		// Arrival order of the mark's latest put.
		Uint64("seq", func(i int) uint64 { return r(i).Seq }).
		// The window each target names, zero for a viewport rect: for
		// joining against keelson('windows').
		Uint64List("windows", func(i int) (ws []uint64) {
			for _, a := range r(i).Targets {
				ws = append(ws, a.Window)
			}
			return
		}).
		StringList("targets", func(i int) (ts []string) {
			for _, a := range r(i).Targets {
				ts = append(ts, targetOf(a))
			}
			return
		})
}
