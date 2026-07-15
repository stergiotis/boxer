package providers

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

func TestRegisterStatic(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, RegisterStatic(r))
	assert.Equal(t, []string{"apps", "build", "env", "extbin", "package_capabilities", "sbom", "sql_passes"}, r.Names())
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
