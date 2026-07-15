// Package providers implements the GUI-free introspection table
// providers — env, apps, build, sbom (ADR-0094 §SD8), sql_passes
// (ADR-0108 §SD5), extbin (ADR-0118), package_capabilities (ADR-0120) —
// and registers them into an introspect.Registry.
// The two GUI-coupled providers (demos, windows) live with the runtime
// wiring, where the egui2 host and its window-host instance exist, so
// this package stays importable from headless contexts.
package providers

import (
	"sort"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
)

// RegisterStatic registers the GUI-free providers (env, apps, build,
// sbom, sql_passes, extbin, package_capabilities) into r (ADR-0094 §SD8,
// ADR-0108 §SD5, ADR-0118, ADR-0120).
//
// package_capabilities is registered unconditionally; the
// boxer_disable_packagecaps build tag empties it rather than removing it, so
// the set of table names does not depend on build flags.
func RegisterStatic(r *introspect.Registry) (err error) {
	for _, p := range []introspect.Provider{
		envProvider{}, appsProvider{}, buildProvider{}, sbomProvider{}, sqlPassesProvider{}, extbinProvider{},
		packageCapsProvider{},
	} {
		if err = r.Register(p); err != nil {
			return
		}
	}
	return
}

// RegisterDefaults registers the GUI-free providers into the
// process-wide introspect.Default registry.
func RegisterDefaults() error { return RegisterStatic(introspect.Default) }

// --- env (ADR-0009 registry) -------------------------------------------------

// envProvider exposes the env-var registry as keelson.env. Specs are
// stable but live values are read per query, so the table is Live.
// Sensitive values are redacted via env.FormatValue before leaving the
// process.
type envProvider struct{}

func (envProvider) Name() string                         { return "env" }
func (envProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (envProvider) Schema() *arrow.Schema                { return envTable(nil).Schema() }

func (envProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	specs := env.All()
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return envTable(specs).Build(proj, len(specs)), nil
}

func envTable(specs []env.Spec) *introspect.Table {
	value := func(i int) (string, bool) {
		s := specs[i]
		if v, ok := env.LookupVar(s.Name); ok {
			if raw, set := v.Lookup(); set {
				return raw, true
			}
		}
		return s.Default, false
	}
	return introspect.NewTable().
		String("name", func(i int) string { return specs[i].Name }).
		String("type", func(i int) string { return string(specs[i].Type) }).
		String("category", func(i int) string { return string(specs[i].Category) }).
		String("value", func(i int) string { v, _ := value(i); return env.FormatValue(specs[i], v) }).
		Bool("is_set", func(i int) bool { _, set := value(i); return set }).
		String("default", func(i int) string { return env.FormatValue(specs[i], specs[i].Default) }).
		Bool("sensitive", func(i int) bool { return specs[i].Sensitive }).
		String("description", func(i int) string { return specs[i].Description }).
		String("cli_flag", func(i int) string { return specs[i].CliFlagName }).
		String("origin_module", func(i int) string { return specs[i].Origin.Module }).
		String("origin_package", func(i int) string { return specs[i].Origin.Package }).
		StringList("allowed", func(i int) []string { return specs[i].Allowed })
}

// --- apps (runtime app registry) ---------------------------------------------

// appsProvider exposes the registered app manifests as keelson.apps.
type appsProvider struct{}

func (appsProvider) Name() string                         { return "apps" }
func (appsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (appsProvider) Schema() *arrow.Schema                { return appsTable(nil).Schema() }

func (appsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	ms := app.AllManifests() // sorted by Id
	return appsTable(ms).Build(proj, len(ms)), nil
}

func appsTable(ms []app.Manifest) *introspect.Table {
	caps := func(i int) (out []string) {
		out = make([]string, 0, len(ms[i].Caps))
		for _, c := range ms[i].Caps {
			out = append(out, c.Pattern+" ["+c.Direction.String()+"]")
		}
		return
	}
	return introspect.NewTable().
		String("id", func(i int) string { return string(ms[i].Id) }).
		String("version", func(i int) string { return ms[i].Version }).
		String("display", func(i int) string { return ms[i].Display }).
		String("title", func(i int) string { return ms[i].WindowTitle() }).
		String("icon", func(i int) string { return ms[i].Icon }).
		String("category", func(i int) string { return ms[i].Category }).
		String("surface", func(i int) string { return ms[i].Surface.String() }).
		Int32("preferred_width", func(i int) int32 { return int32(ms[i].SurfaceHints.PreferredWidth) }).
		Int32("preferred_height", func(i int) int32 { return int32(ms[i].SurfaceHints.PreferredHeight) }).
		Int32("background_tick_hz", func(i int) int32 { return int32(ms[i].BackgroundTickHz) }).
		Bool("has_help", func(i int) bool { return ms[i].Help != nil }).
		StringList("caps", caps).
		StringList("persisted_keys", func(i int) []string { return ms[i].PersistedKeys })
}

// --- build (runinfo + vcs) ---------------------------------------------------

// buildProvider exposes the process run identity + build metadata as
// keelson.build (one row). When runinfo.Init has not run the table is
// empty rather than erroring.
type buildProvider struct{}

func (buildProvider) Name() string                         { return "build" }
func (buildProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (buildProvider) Schema() *arrow.Schema                { return buildTable(nil).Schema() }

func (buildProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []*runinfo.Inst
	if inst, err := runinfo.Get(); err == nil {
		rows = []*runinfo.Inst{inst}
	}
	return buildTable(rows).Build(proj, len(rows)), nil
}

func buildTable(rows []*runinfo.Inst) *introspect.Table {
	return introspect.NewTable().
		String("run_id", func(i int) string { return rows[i].RunId }).
		String("hostname", func(i int) string { return rows[i].Hostname }).
		Int32("pid", func(i int) int32 { return int32(rows[i].Pid) }).
		String("started_at", func(i int) string { return rows[i].StartedAt.Format(time.RFC3339) }).
		String("go_version", func(i int) string { return rows[i].GoVersion }).
		String("vcs_revision", func(i int) string { return rows[i].VcsRevision }).
		Bool("vcs_modified", func(i int) bool { return rows[i].VcsModified }).
		String("vcs_build_info", func(i int) string { return rows[i].VcsBuildInfo }).
		String("module_path", func(i int) string { return rows[i].ModulePath })
}
