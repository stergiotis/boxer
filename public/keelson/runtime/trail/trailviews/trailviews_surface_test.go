package trailviews_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// localConn is lwsqlsurface.Conn over clickhouse-local, so an install runs
// for real against a directory. Writes are buffered and flushed as one
// multi-statement script before the next read — a process per statement
// put three installs at half a minute — which gives up attributing an Exec
// error to its own call; nothing here asserts that.
type localConn struct {
	t       *testing.T
	path    string
	pending *[]string
}

func newLocalConn(t *testing.T) localConn {
	return localConn{t: t, path: t.TempDir(), pending: &[]string{}}
}

func (inst localConn) script(script string) (out string, err error) {
	cmd, err := extbin.ClickHouseLocal.Command(context.Background(), extbin.Opts{}, "--path", inst.path, "--multiquery", "--output-format", "TSV")
	if err != nil {
		inst.t.Skipf("clickhouse unavailable: %v", err)
	}
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err = cmd.Run(); err != nil {
		return "", &runError{sql: script, stderr: stderr.String()}
	}
	return stdout.String(), nil
}

func (inst localConn) run(sql string) (out string, err error) {
	if len(*inst.pending) > 0 {
		writes := strings.Join(*inst.pending, ";\n") + ";\n"
		*inst.pending = (*inst.pending)[:0]
		if _, err = inst.script(writes); err != nil {
			return "", err
		}
	}
	return inst.script(sql)
}

type runError struct{ sql, stderr string }

func (inst *runError) Error() string { return inst.stderr + "\n" + inst.sql }

func (inst localConn) Exec(_ context.Context, sql string) (err error) {
	*inst.pending = append(*inst.pending, sql)
	return nil
}

func (inst localConn) Query(_ context.Context, sql string) (body io.ReadCloser, err error) {
	out, err := inst.run(sql)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(out)), nil
}

func TestRegisteredWithTheSurfaceInstall(t *testing.T) {
	require.Contains(t, lwsqlsurface.Dependents(), trailviews.SurfaceDependentName)
}

// The surface install owns the views' refresh: on a server without the
// facts table it leaves them out; once the table is there it creates every
// view, stamped; and a stale view — one created from an older body — is
// replaced by the next install rather than left answering.
func TestSurfaceInstallRecreatesTheViews(t *testing.T) {
	conn := newLocalConn(t)
	ctx := context.Background()
	stamped := func() []string {
		out, err := conn.run("SELECT name FROM system.tables WHERE database = 'boxer' AND comment = " +
			trailviews.Stamp().Literal() + " ORDER BY name")
		require.NoError(t, err)
		return strings.Fields(out)
	}

	require.NoError(t, lwsqlsurface.Install(ctx, conn))
	out, err := conn.run("SELECT count() FROM system.tables WHERE database = 'boxer'")
	require.NoError(t, err)
	require.Equal(t, "0", strings.TrimSpace(out), "no facts table, no trail views")

	setup, err := chstore.ComposeSetupSQL(chstore.Config{Database: factsschema.DatabaseName, Table: factsschema.TableName}, "")
	require.NoError(t, err)
	for stmt := range strings.SplitSeq(setup, ";") {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			require.NoError(t, conn.Exec(ctx, stmt))
		}
	}
	require.NoError(t, lwsqlsurface.Install(ctx, conn))
	require.ElementsMatch(t, trailviews.AllViewNames(), stamped())

	// A view left behind by an older build: different body, older stamp.
	require.NoError(t, conn.Exec(ctx, "CREATE OR REPLACE VIEW boxer."+trailviews.ViewTimeline+" AS SELECT 1 AS stale COMMENT 'keelson trail views v0'"))
	require.NotContains(t, stamped(), trailviews.ViewTimeline)
	require.NoError(t, lwsqlsurface.Install(ctx, conn))
	require.ElementsMatch(t, trailviews.AllViewNames(), stamped())
	out, err = conn.run("SELECT count() FROM boxer." + trailviews.ViewTimeline)
	require.NoError(t, err)
	require.Equal(t, "0", strings.TrimSpace(out), "the re-created timeline reads the (empty) trail again")
}
