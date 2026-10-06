package keelsonddl

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
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
// byte-identical prefix, the surface precedes the views, and every view
// statement is present in creation order.
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
	at := strings.Index(have, marker)
	for _, v := range views {
		next := strings.Index(have, v)
		require.Greater(t, next, at, "in creation order, after the surface: %s", v[:60])
		at = next
	}

	bare, err := compose(options{cfg: defaults(), trailViews: true})
	require.NoError(t, err)
	require.NotContains(t, bare, marker, "the surface is printed only when asked for")
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
		})
	}
}
