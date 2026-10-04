// Package providersgui implements the GUI-coupled v1 introspection
// providers — demos, windows (ADR-0094 §SD8) and frame times (ADR-0261). They live apart from
// the GUI-free providers package because the demo registry and the
// window host both pull in the egui2 bindings, so importing them from a
// headless context is undesirable. The runtime wiring registers these
// alongside the GUI-free set.
package providersgui

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	demoreg "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// RegisterDemos registers the demos provider into r.
func RegisterDemos(r *introspect.Registry) error { return r.Register(demosProvider{}) }

// TasksOfI says which agent tasks hold each window (ADR-0276 §SD1): window
// key to task ids. The agent service's read side answers it.
type TasksOfI func() (tasks map[uint64][]string)

// RegisterWindows registers a windows provider bound to host into r.
// tasksOf may be nil: every window then reads as held by no task.
func RegisterWindows(r *introspect.Registry, host *windowhost.Inst, tasksOf TasksOfI) error {
	return r.Register(windowsProvider{host: host, tasksOf: tasksOf})
}

// RegisterDesktop registers a desktop provider bound to host into r.
// A nil host answers with an empty table.
func RegisterDesktop(r *introspect.Registry, host *windowhost.Inst) error {
	return r.Register(desktopProvider{host: host})
}

// RegisterFrameTimes registers a frame-time provider bound to host into r.
// A nil host answers with an empty table.
func RegisterFrameTimes(r *introspect.Registry, host *windowhost.Inst) error {
	return r.Register(frameTimesProvider{host: host})
}

// RegisterAll registers the GUI-coupled providers: demos (a process
// global) and, when host is non-nil, windows, the desktop and frame times
// (bound to that host).
func RegisterAll(r *introspect.Registry, host *windowhost.Inst, tasksOf TasksOfI) (err error) {
	if err = RegisterDemos(r); err != nil {
		return
	}
	if host != nil {
		if err = RegisterWindows(r, host, tasksOf); err != nil {
			return
		}
		if err = RegisterDesktop(r, host); err != nil {
			return
		}
		err = RegisterFrameTimes(r, host)
	}
	return
}

// --- demos (ADR-0057 demo registry) ------------------------------------------

type demosProvider struct{}

func (demosProvider) Name() string                         { return "demos" }
func (demosProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (demosProvider) Schema() *arrow.Schema                { return demosTable(nil).Schema() }

func (demosProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	ds := demoreg.All() // sorted by Name
	return demosTable(ds).Build(proj, len(ds)), nil
}

func demosTable(ds []demoreg.Demo) *introspect.Table {
	flags := func(i int) (out []string) {
		f := ds[i].Flags
		if f&demoreg.DemoFlagNeedsLargeArea != 0 {
			out = append(out, "needs_large_area")
		}
		if f&demoreg.DemoFlagSkipInTour != 0 {
			out = append(out, "skip_in_tour")
		}
		if f&demoreg.DemoFlagNeedsNetwork != 0 {
			out = append(out, "needs_network")
		}
		if f&demoreg.DemoFlagNonDeterministic != 0 {
			out = append(out, "non_deterministic")
		}
		return
	}
	return introspect.NewTable().
		String("name", func(i int) string { return ds[i].Name }).
		String("category", func(i int) string { return ds[i].Category }).
		String("title", func(i int) string { return ds[i].Title }).
		String("kind", func(i int) string { return ds[i].Kind.String() }).
		String("description", func(i int) string { return ds[i].Description }).
		Int32("stage_w", func(i int) int32 { return int32(ds[i].Stage[0]) }).
		Int32("stage_h", func(i int) int32 { return int32(ds[i].Stage[1]) }).
		Bool("stateful", func(i int) bool { return ds[i].RenderStateful != nil }).
		StringList("flags", flags).
		String("source_file", func(i int) string { return ds[i].SourceFile }).
		Int32("source_line", func(i int) int32 { return int32(ds[i].SourceLine) })
}

// --- windows (window host) ---------------------------------------------------

type windowsProvider struct {
	host    *windowhost.Inst
	tasksOf TasksOfI
}

func (windowsProvider) Name() string                         { return "windows" }
func (windowsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (p windowsProvider) Schema() *arrow.Schema              { return windowsTable(nil, nil).Schema() }

func (p windowsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var infos []windowhost.WindowInfo
	if p.host != nil {
		infos = p.host.WindowInfos()
	}
	var tasks map[uint64][]string
	if p.tasksOf != nil {
		tasks = p.tasksOf()
	}
	return windowsTable(infos, tasks).Build(proj, len(infos)), nil
}

func windowsTable(ws []windowhost.WindowInfo, tasks map[uint64][]string) *introspect.Table {
	return introspect.NewTable().
		Int64("key", func(i int) int64 { return int64(ws[i].Key) }).
		String("app_id", func(i int) string { return string(ws[i].AppId) }).
		String("display", func(i int) string { return ws[i].Display }).
		String("title", func(i int) string { return ws[i].Title }).
		String("surface", func(i int) string { return ws[i].Surface.String() }).
		StringList("topics", func(i int) []string {
			out := make([]string, 0, len(ws[i].Topics))
			for _, t := range ws[i].Topics {
				out = append(out, t.String())
			}
			return out
		}).
		String("kind", func(i int) string { return ws[i].Kind.String() }).
		String("stop_reason", func(i int) string { return ws[i].StopReason }).
		// How this window came to hold what it holds (ADR-0148 §SD5):
		// "plain" (nobody delivered a config), "caller" (another app opened
		// it with arguments), "restore" (the host handed back the app's own
		// stored workingset). config_kind / config_bytes describe the
		// payload that arrived; the payload itself stays in the window.
		String("launch_reason", func(i int) string { return ws[i].LaunchReason.String() }).
		String("config_kind", func(i int) string { return ws[i].ConfigKind }).
		Int64("config_bytes", func(i int) int64 { return int64(ws[i].ConfigBytes) }).
		// True when another open window shares this one's app instance
		// (a singleton-registered app shown twice): such a window takes no
		// config and saves no workingset.
		Bool("shares_instance", func(i int) bool { return ws[i].SharesInstance }).
		// The window's state as of the last completed frame (ADR-0276 §SD1).
		// shown is false for a window that has not drawn yet; its geometry
		// columns are zero then. Coordinates are logical points, viewport
		// top-left origin; x, y, w, h is the outer rect.
		Bool("shown", func(i int) bool { return ws[i].Geom.Shown }).
		Float64("x", func(i int) float64 { return float64(ws[i].Geom.Rect.MinX) }).
		Float64("y", func(i int) float64 { return float64(ws[i].Geom.Rect.MinY) }).
		Float64("w", func(i int) float64 { return float64(ws[i].Geom.Rect.W()) }).
		Float64("h", func(i int) float64 { return float64(ws[i].Geom.Rect.H()) }).
		// The outer size the content needed as laid out (ADR-0275 §SD4):
		// larger than w, h where the content overflowed the window.
		Float64("need_w", func(i int) float64 { return float64(ws[i].Geom.NeedW) }).
		Float64("need_h", func(i int) float64 { return float64(ws[i].Geom.NeedH) }).
		// Stacking rank: larger is further front, 0 unknown.
		Uint64("stack", func(i int) uint64 { return uint64(ws[i].Geom.Stack) }).
		Bool("collapsed", func(i int) bool { return ws[i].Geom.Collapsed }).
		Bool("maximized", func(i int) bool { return ws[i].Geom.Maximized }).
		// The shell's active window: the one process-global input goes to.
		Bool("active", func(i int) bool { return ws[i].Geom.Active }).
		// The agent tasks whose grant holds the window (ADR-0269), the
		// task column of keelson('agent_grants'); empty when none.
		StringList("agent_tasks", func(i int) []string { return tasks[uint64(ws[i].Key)] })
}

// --- desktop (window host) ---------------------------------------------------

// TableDesktop is the table's name, keelson('desktop').
const TableDesktop = "desktop"

type desktopProvider struct{ host *windowhost.Inst }

func (desktopProvider) Name() string                         { return TableDesktop }
func (desktopProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (desktopProvider) Schema() *arrow.Schema                { return desktopTable(nil).Schema() }

func (p desktopProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []windowhost.DesktopInfo
	if p.host != nil {
		rows = append(rows, p.host.DesktopInfo())
	}
	return desktopTable(rows).Build(proj, len(rows)), nil
}

// desktopTable is one row: the desktop as of the last completed frame
// (ADR-0276 §SD1).
func desktopTable(rows []windowhost.DesktopInfo) *introspect.Table {
	return introspect.NewTable().
		// The work area: what the shell's panels leave free, what a
		// maximized window fills and arrangements lay out into. work_shown
		// is false until a frame has shown a window; the rect is zero then.
		Bool("work_shown", func(i int) bool { return rows[i].WorkShown }).
		Float64("work_x", func(i int) float64 { return float64(rows[i].Work.MinX) }).
		Float64("work_y", func(i int) float64 { return float64(rows[i].Work.MinY) }).
		Float64("work_w", func(i int) float64 { return float64(rows[i].Work.W()) }).
		Float64("work_h", func(i int) float64 { return float64(rows[i].Work.H()) }).
		// The active window's key in keelson('windows'), 0 when none.
		Int64("active_key", func(i int) int64 { return int64(rows[i].ActiveKey) }).
		// The arrangement in progress ("Tile", "Cascade", …), empty when none.
		String("arranging", func(i int) string {
			if rows[i].Arranging == windowhost.ArrangeNone {
				return ""
			}
			return rows[i].Arranging.String()
		})
}

// --- frame times (ADR-0261) --------------------------------------------------

// TableFrameTimes is the table's name, keelson('frame_times').
const TableFrameTimes = "frame_times"

type frameTimesProvider struct{ host *windowhost.Inst }

func (frameTimesProvider) Name() string                         { return TableFrameTimes }
func (frameTimesProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (frameTimesProvider) Schema() *arrow.Schema                { return frameTimesTable(nil).Schema() }

func (p frameTimesProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []windowhost.FrameTimeInfo
	if p.host != nil {
		rows = p.host.FrameTimes()
	}
	return frameTimesTable(rows).Build(proj, len(rows)), nil
}

// frameTimesTable is one row for the render loop and one per open window
// that has drawn. Durations are microseconds: a frame's parts are well under
// a millisecond, and a column a reader subtracts should not need parsing.
func frameTimesTable(rows []windowhost.FrameTimeInfo) *introspect.Table {
	us := func(d time.Duration) int64 { return d.Microseconds() }
	return introspect.NewTable().
		// "loop": the render loop's period, start of one frame to the next —
		// the budget the window rows spend from. "window": one window's
		// Frame call on the render goroutine.
		String("scope", func(i int) string { return rows[i].Scope.String() }).
		Uint64("instance_key", func(i int) uint64 { return uint64(rows[i].Key) }).
		String("app_id", func(i int) string { return string(rows[i].AppId) }).
		// frames counts every sample since the window opened; samples those
		// the quantiles and means are over, the most recent ones.
		Uint64("frames", func(i int) uint64 { return rows[i].Frames }).
		Int64("samples", func(i int) int64 { return int64(rows[i].Samples) }).
		Int64("total_us", func(i int) int64 { return us(rows[i].Total) }).
		Int64("last_us", func(i int) int64 { return us(rows[i].Last) }).
		Int64("mean_us", func(i int) int64 { return us(rows[i].Mean) }).
		Int64("p50_us", func(i int) int64 { return us(rows[i].P50) }).
		Int64("p95_us", func(i int) int64 { return us(rows[i].P95) }).
		Int64("max_us", func(i int) int64 { return us(rows[i].Max) }).
		// FFFI messages the sample produced, captured or sent.
		Uint64("messages_last", func(i int) uint64 { return rows[i].MessagesLast }).
		Float64("messages_mean", func(i int) float64 { return rows[i].MessagesMean }).
		// Mount on the render goroutine; 0 for a window sharing an instance
		// already mounted, and for the loop row.
		Int64("mount_us", func(i int) int64 { return us(rows[i].Mount) })
}
