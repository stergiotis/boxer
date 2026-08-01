// Package providers implements the GUI-free introspection table
// providers — env, apps, build, sbom (ADR-0094 §SD8), sql_passes
// (ADR-0108 §SD5), extbin (ADR-0118), adr/subtask/coderef/adrcontent
// (ADR-0122 §SD4) — and registers them into an introspect.Registry.
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
// sbom, sql_passes, extbin, adr/subtask/coderef/adrcontent, components) into r
// (ADR-0094 §SD8, ADR-0108 §SD5, ADR-0118, ADR-0122 §SD4, ADR-0126 §SD5).
//
// The four ADR tables register unconditionally, like the rest: off-repo they
// are empty rather than absent, so the set of table names does not depend on
// where the process was started (see adr.go). Registering adrcontent costs
// nothing until it is named in a query — it reads the corpus on Snapshot, not
// here. The plane-fed topology tables (procs, sockets) are the exception —
// they need a consumer, so they register via RegisterTopology.
func RegisterStatic(r *introspect.Registry) (err error) {
	for _, p := range []introspect.Provider{
		envProvider{}, appsProvider{}, buildProvider{}, sbomProvider{}, sqlPassesProvider{}, extbinProvider{},
		adrProvider{}, subtaskProvider{}, coderefProvider{}, adrcontentProvider{}, componentsProvider{},
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
	rs := app.AllRegistrations() // sorted by Id
	return appsTable(rs).Build(proj, len(rs)), nil
}

// topicStrings renders a manifest's topics for the Arrow list column. The
// table speaks strings so a SQL predicate can name a topic without knowing
// the Go type (ADR-0158 §SD8).
func topicStrings(ts []app.TopicT) (out []string) {
	out = make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.String())
	}
	return
}

// appsTable renders registrations rather than bare manifests: how an app
// was registered decides whether two windows of it are independent, and
// the manifest cannot say.
func appsTable(rs []app.Registration) *introspect.Table {
	m := func(i int) app.Manifest { return rs[i].Manifest }
	caps := func(i int) (out []string) {
		out = make([]string, 0, len(m(i).Caps))
		for _, c := range m(i).Caps {
			out = append(out, c.Pattern+" ["+c.Direction.String()+"]")
		}
		return
	}
	registration := func(i int) string {
		if rs[i].Singleton {
			return "singleton"
		}
		return "factory"
	}
	return introspect.NewTable().
		String("id", func(i int) string { return string(m(i).Id) }).
		String("version", func(i int) string { return m(i).Version }).
		String("display", func(i int) string { return m(i).Display }).
		String("title", func(i int) string { return m(i).WindowTitle() }).
		String("icon", func(i int) string { return m(i).Icon }).
		StringList("topics", func(i int) []string { return topicStrings(m(i).Topics) }).
		StringList("keywords", func(i int) []string { return m(i).Keywords }).
		// Provenance, deliberately a column rather than a section (ADR-0158
		// §SD5): "show me every applet" is a filter, not a place apps live.
		String("kind", func(i int) string { return m(i).Kind.String() }).
		String("surface", func(i int) string { return m(i).Surface.String() }).
		Int32("preferred_width", func(i int) int32 { return int32(m(i).SurfaceHints.PreferredWidth) }).
		Int32("preferred_height", func(i int) int32 { return int32(m(i).SurfaceHints.PreferredHeight) }).
		Int32("background_tick_hz", func(i int) int32 { return int32(m(i).BackgroundTickHz) }).
		Bool("has_help", func(i int) bool { return m(i).Help != nil }).
		StringList("caps", caps).
		StringList("persisted_keys", func(i int) []string { return m(i).PersistedKeys }).
		// The two argument/state declarations (ADR-0135 §SD3, ADR-0148
		// §SD7). launch_kind answers what a caller must send to open this
		// app with arguments — empty means it accepts none, and an
		// argument-carrying open is refused at the host boundary.
		// workingset says whether the host pulls a record out of a closing
		// window and hands it back at the next plain open; it implies a
		// non-empty launch_kind, since the record is an instance of that
		// kind (manifest validation enforces the pair).
		String("launch_kind", func(i int) string { return m(i).LaunchKind }).
		Bool("workingset", func(i int) bool { return m(i).Workingset }).
		// How the app was registered: "factory" mints a fresh instance per
		// Open, "singleton" hands the same one to every window. It bounds
		// both of the columns above — a config, and therefore a restored
		// workingset, is delivered at the Mount that runs once per
		// instance, so a singleton app with a window open can consume
		// neither. `workingset AND registration = 'singleton'` is a
		// misdeclaration the host can otherwise only report at first close.
		String("registration", registration)
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
