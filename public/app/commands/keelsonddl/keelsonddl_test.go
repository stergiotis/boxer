package keelsonddl

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

func defaults() chstore.Config {
	def := chstore.Defaults()
	return chstore.Config{Database: def.Database, Table: def.Table}
}

// Without flags the command still prints exactly what first-run setup runs.
func TestDefaultIsFirstRunSetup(t *testing.T) {
	want, err := chstore.ComposeSetupSQL(defaults(), "")
	require.NoError(t, err)
	have, err := compose(options{cfg: defaults()})
	require.NoError(t, err)
	require.Equal(t, want, have)
}

// The views are appended, never interleaved: the facts DDL stays a
// byte-identical prefix, the surface precedes the views, and every
// statement of the trail views — their functions first — is present in
// creation order.
func TestTrailViewsAreAppended(t *testing.T) {
	setup, err := chstore.ComposeSetupSQL(defaults(), "")
	require.NoError(t, err)
	have, err := compose(options{cfg: defaults(), trailViews: true, withSurface: true})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(have, setup))

	marker := lwsqlsurface.MarkerStatement()
	require.Contains(t, have, marker)
	views, err := trailviews.Statements("", "")
	require.NoError(t, err)
	require.Equal(t, trailviews.FunctionStatements(), views[:len(trailviews.FunctionNames())], "the functions precede the views")
	at := strings.Index(have, marker)
	for _, v := range views {
		next := strings.Index(have, v)
		head, _, _ := strings.Cut(v, "\n")
		require.Greater(t, next, at, "in creation order, after the surface: %s", head)
		at = next
	}

	bare, err := compose(options{cfg: defaults(), trailViews: true})
	require.NoError(t, err)
	require.NotContains(t, bare, marker, "the surface is printed only when asked for")
}

// With a read table the views read it and stay in --database (ADR-0402
// §SD3).
func TestTrailViewsReadTheReadTable(t *testing.T) {
	have, err := compose(options{cfg: defaults(), trailViews: true, readTable: "carrier.facts_view"})
	require.NoError(t, err)
	require.Contains(t, have, "FROM carrier.facts_view\n")
	require.Contains(t, have, "CREATE OR REPLACE VIEW boxer.")
}

// Under an insert transform there is no facts DDL: the bare command
// refuses, and --trail-views prints the views without it (ADR-0402 §SD5).
func TestUnderATransformNoFactsDDL(t *testing.T) {
	cfg := chstore.Config{Database: "scratch_ddl", Table: "facts"}
	unregister := recordstore.RegisterInsertTransform("scratch_ddl.facts", recordstore.InsertTransformFunc(
		func(_ string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
			return "scratch_ddl.carrier", records, nil
		}))
	defer unregister()
	_, err := compose(options{cfg: cfg})
	require.ErrorIs(t, err, recordstore.ErrTransformedTable)
	have, err := compose(options{cfg: cfg, trailViews: true, readTable: "carrier.facts_view"})
	require.NoError(t, err)
	require.NotContains(t, have, "CREATE TABLE")
	require.Contains(t, have, "FROM carrier.facts_view\n")
}

func TestWithSurfaceNeedsTrailViews(t *testing.T) {
	_, err := compose(options{cfg: defaults(), withSurface: true})
	require.Error(t, err)
}

// The printed script is sufficient on a fresh server: run as one
// multi-statement file against an empty clickhouse-local, it creates the
// table, the surface and every view, for the default target and for one
// named by --database/--table.
func TestScriptAppliesToAFreshServer(t *testing.T) {
	for _, cfg := range []chstore.Config{defaults(), {Database: "audit", Table: "trail_facts"}} {
		t.Run(cfg.Database+"."+cfg.Table, func(t *testing.T) {
			script, err := compose(options{cfg: cfg, trailViews: true, withSurface: true})
			require.NoError(t, err)
			dir := t.TempDir()
			file := filepath.Join(t.TempDir(), "ddl.sql")
			require.NoError(t, os.WriteFile(file, []byte(script), 0o644))

			run := func(args ...string) string {
				cmd, cerr := extbin.ClickHouseLocal.Command(context.Background(), extbin.Opts{}, append([]string{"--path", dir}, args...)...)
				if cerr != nil {
					t.Skipf("clickhouse unavailable: %v", cerr)
				}
				var out, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &stderr
				require.NoError(t, cmd.Run(), stderr.String())
				return out.String()
			}
			run("--queries-file", file)

			names := run("--query", "SELECT name FROM system.tables WHERE database = '"+cfg.Database+
				"' AND comment = "+trailviews.Stamp().Literal()+" ORDER BY name FORMAT TSV")
			want := append([]string{}, trailviews.AllViewNames()...)
			require.ElementsMatch(t, want, strings.Fields(names))
			require.Equal(t, "0\n", run("--query", "SELECT count() FROM "+cfg.Database+"."+trailviews.ViewTimeline))
			require.Equal(t, strconv.Itoa(trailviews.ViewsVersion)+"\n", run("--query", "SELECT "+trailviews.FuncViewsVersion+"()"),
				"the script installs the trail functions with the views")
		})
	}
}
