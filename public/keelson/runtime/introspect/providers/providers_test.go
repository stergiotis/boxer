package providers

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

func TestRegisterStatic(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, RegisterStatic(r))
	assert.Equal(t, []string{"adr", "adrcontent", "adrsections", "apps", "build",
		"coderef", "competence", "competencerelation", "competencesection",
		"components", "env", "extbin", "go_modules", "go_symbols", "helpsections",
		"panel_shapes", "sbom", "sql_passes", "subtask"},
		r.Names())
}

func TestProvidersSnapshotWell(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, RegisterStatic(r))
	for _, p := range r.Providers() {
		t.Run(p.Name(), func(t *testing.T) {
			require.Greater(t, p.Schema().NumFields(), 0)
			rec, err := p.Snapshot(introspect.AllColumns())
			require.NoError(t, err)
			defer rec.Release()
			assert.EqualValues(t, p.Schema().NumFields(), rec.NumCols(),
				"an AllColumns snapshot must carry every schema column")
		})
	}
}

func TestEnvProviderHasRows(t *testing.T) {
	// config/env and runinfo register at least PEBBLE2_RUN_ID and
	// KEELSON_INTROSPECT_SBOM_PATH, so the live registry is non-empty.
	rec, err := envProvider{}.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	assert.Positive(t, rec.NumRows())
}

func TestEnvProviderRedactsSensitive(t *testing.T) {
	// envTable takes specs directly, so this exercises redaction without
	// mutating the process-wide registry.
	specs := []env.Spec{{Name: "INTROSPECT_TEST_SECRET", Sensitive: true, Default: "hunter2", Type: env.TypeString}}
	rec := envTable(specs).Build(introspect.AllColumns(), len(specs))
	defer rec.Release()
	assert.Equal(t, "<redacted>", firstString(t, rec, "value"))
	assert.Equal(t, "<redacted>", firstString(t, rec, "default"))
}

func TestEnvProviderProjection(t *testing.T) {
	rec, err := envProvider{}.Snapshot(introspect.Columns("name"))
	require.NoError(t, err)
	defer rec.Release()
	require.EqualValues(t, 1, rec.NumCols())
	assert.Equal(t, "name", rec.Schema().Field(0).Name)
}

func TestExtbinProviderHasRows(t *testing.T) {
	// extbin's package init declares the central host programs (git, scc, …),
	// so the live registry is non-empty.
	rec, err := extbinProvider{}.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	assert.Positive(t, rec.NumRows())
	for _, col := range []string{"name", "kind", "module", "override_env", "install_hint", "available", "resolved_path", "blake3"} {
		require.NotEmpty(t, rec.Schema().FieldIndices(col), "missing column %q", col)
	}
}

func TestExtbinTableRendersKindAndPath(t *testing.T) {
	// Drive the table directly with fixed rows (no dependency on the host's
	// installed binaries); blake3 is best-effort and unread here.
	rows := []extbinRow{
		{prog: &extbin.Program{Name: "git", Kind: extbin.Host, InstallHint: "install git"}, resolved: "/usr/bin/git", available: true},
		{prog: &extbin.Program{Name: "some-artifact", Kind: extbin.Local}, resolved: "", available: false},
	}
	rec := extbinTable(rows).Build(introspect.AllColumns(), len(rows))
	defer rec.Release()
	require.EqualValues(t, 2, rec.NumRows())
	assert.Equal(t, "git", firstString(t, rec, "name"))
	assert.Equal(t, "host", firstString(t, rec, "kind"))
	assert.Equal(t, "/usr/bin/git", firstString(t, rec, "resolved_path"))
}

// firstString returns the row-0 value of the named Utf8 column.
func firstString(t *testing.T, rec arrow.RecordBatch, col string) string {
	t.Helper()
	idx := rec.Schema().FieldIndices(col)
	require.NotEmpty(t, idx, "column %q not found", col)
	return rec.Column(idx[0]).(*array.String).Value(0)
}

// TestAppsTableRendersLaunchAndWorkingset covers the two declaration
// columns the launch/workingset contracts added (ADR-0135 §SD3, ADR-0148
// §SD7): together they answer "what can I open this app with, and does it
// remember anything" without reading Go source.
func TestAppsTableRendersLaunchAndWorkingset(t *testing.T) {
	rs := []app.Registration{
		{Manifest: app.Manifest{
			Id: "test.plain", Display: "Plain", Surface: app.SurfaceWindowed,
			Topics: []app.TopicT{app.TopicRuntime},
		}},
		{Manifest: app.Manifest{
			Id: "test.stateful", Display: "Stateful", Surface: app.SurfaceWindowed,
			Topics: []app.TopicT{app.TopicRuntime}, LaunchKind: "testLaunch", Workingset: true,
		}},
	}
	for _, r := range rs {
		require.NoError(t, r.Manifest.Validate(), "the fixtures must be manifests the registry would accept")
	}
	rec := appsTable(rs).Build(introspect.AllColumns(), len(rs))
	defer rec.Release()
	require.EqualValues(t, 2, rec.NumRows())

	kinds := rec.Schema().FieldIndices("launch_kind")
	require.NotEmpty(t, kinds)
	kindCol := rec.Column(kinds[0]).(*array.String)
	assert.Empty(t, kindCol.Value(0), "an app that accepts no arguments reads as empty, not absent")
	assert.Equal(t, "testLaunch", kindCol.Value(1))

	ws := rec.Schema().FieldIndices("workingset")
	require.NotEmpty(t, ws)
	wsCol := rec.Column(ws[0]).(*array.Boolean)
	assert.False(t, wsCol.Value(0))
	assert.True(t, wsCol.Value(1))
}

// TestAppsTableRendersRegistrationMode pins the column that bounds the two
// above: a workingset over a singleton registration is a misdeclaration,
// and this is what makes it a query instead of a host warning at the first
// window close.
func TestAppsTableRendersRegistrationMode(t *testing.T) {
	rs := []app.Registration{
		{Manifest: app.Manifest{
			Id: "test.factory", Display: "Factory", Surface: app.SurfaceWindowed,
			LaunchKind: "testLaunch", Workingset: true,
		}},
		{Manifest: app.Manifest{
			Id: "test.singleton", Display: "Singleton", Surface: app.SurfaceWindowed,
			LaunchKind: "testLaunch", Workingset: true,
		}, Singleton: true},
	}
	rec := appsTable(rs).Build(introspect.AllColumns(), len(rs))
	defer rec.Release()

	idx := rec.Schema().FieldIndices("registration")
	require.NotEmpty(t, idx)
	col := rec.Column(idx[0]).(*array.String)
	assert.Equal(t, "factory", col.Value(0))
	assert.Equal(t, "singleton", col.Value(1), "the row a workingset audit would flag")
}

// TestAppsTableRendersClassification covers the ADR-0158 columns: the
// subject axis as a list, the free keyword layer, and provenance as a
// column rather than a section.
func TestAppsTableRendersClassification(t *testing.T) {
	rs := []app.Registration{
		{Manifest: app.Manifest{
			Id: "test.multi", Display: "Multi", Surface: app.SurfaceWindowed,
			Topics:   []app.TopicT{app.TopicCode, app.TopicTopology},
			Keywords: []string{"deps", "imports"},
			Kind:     app.KindApplet,
		}},
	}
	for _, r := range rs {
		require.NoError(t, r.Manifest.Validate(), "the fixtures must be manifests the registry would accept")
	}
	rec := appsTable(rs).Build(introspect.AllColumns(), len(rs))
	defer rec.Release()
	require.EqualValues(t, 1, rec.NumRows())

	assert.Equal(t, []string{"code", "topology"}, firstStringList(t, rec, "topics"),
		"topics is a list so a predicate can ask has(topics, 'code')")
	assert.Equal(t, []string{"deps", "imports"}, firstStringList(t, rec, "keywords"))
	assert.Equal(t, "applet", firstString(t, rec, "kind"))
}

// firstStringList returns the row-0 value of the named List<Utf8> column.
func firstStringList(t *testing.T, rec arrow.RecordBatch, col string) (out []string) {
	t.Helper()
	idx := rec.Schema().FieldIndices(col)
	require.NotEmpty(t, idx, "column %q not found", col)
	lst := rec.Column(idx[0]).(*array.List)
	vals := lst.ListValues().(*array.String)
	start, end := lst.ValueOffsets(0)
	out = make([]string, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, vals.Value(int(i)))
	}
	return
}
