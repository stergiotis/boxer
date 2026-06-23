// Package providersgui implements the GUI-coupled v1 introspection
// providers — demos and windows (ADR-0094 §SD8). They live apart from
// the GUI-free providers package because the demo registry and the
// window host both pull in the egui2 bindings, so importing them from a
// headless context is undesirable. The runtime wiring registers these
// alongside the GUI-free set.
package providersgui

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	demoreg "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// RegisterDemos registers the demos provider into r.
func RegisterDemos(r *introspect.Registry) error { return r.Register(demosProvider{}) }

// RegisterWindows registers a windows provider bound to host into r.
func RegisterWindows(r *introspect.Registry, host *windowhost.Inst) error {
	return r.Register(windowsProvider{host: host})
}

// RegisterAll registers the GUI-coupled providers: demos (a process
// global) and, when host is non-nil, windows (bound to that host).
func RegisterAll(r *introspect.Registry, host *windowhost.Inst) (err error) {
	if err = RegisterDemos(r); err != nil {
		return
	}
	if host != nil {
		err = RegisterWindows(r, host)
	}
	return
}

// --- demos (ADR-0057 demo registry) ------------------------------------------

type demosProvider struct{}

func (demosProvider) Name() string                        { return "demos" }
func (demosProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (demosProvider) Schema() *arrow.Schema               { return demosTable(nil).Schema() }

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

type windowsProvider struct{ host *windowhost.Inst }

func (windowsProvider) Name() string                        { return "windows" }
func (windowsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (p windowsProvider) Schema() *arrow.Schema             { return windowsTable(nil).Schema() }

func (p windowsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var infos []windowhost.WindowInfo
	if p.host != nil {
		infos = p.host.WindowInfos()
	}
	return windowsTable(infos).Build(proj, len(infos)), nil
}

func windowsTable(ws []windowhost.WindowInfo) *introspect.Table {
	return introspect.NewTable().
		Int64("key", func(i int) int64 { return int64(ws[i].Key) }).
		String("app_id", func(i int) string { return string(ws[i].AppId) }).
		String("display", func(i int) string { return ws[i].Display }).
		String("title", func(i int) string { return ws[i].Title }).
		String("surface", func(i int) string { return ws[i].Surface.String() }).
		String("category", func(i int) string { return ws[i].Category }).
		String("stop_reason", func(i int) string { return ws[i].StopReason })
}
