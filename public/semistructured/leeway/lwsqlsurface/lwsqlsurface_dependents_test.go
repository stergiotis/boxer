package lwsqlsurface_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// A registered dependent is re-created by the install, after the surface
// has verified and after the decode views exist: its view inlines a surface
// function and reads leeway.columns, and both only work at that point. A
// dependent that returns nothing is skipped.
func TestInstallRecreatesDependents(t *testing.T) {
	conn := newLocalConn(t)
	ran := []string{}
	defer lwsqlsurface.RegisterDependent(lwsqlsurface.Dependent{
		Name: "test b: over the surface and its decode views",
		Statements: func(ctx context.Context, c lwsqlsurface.Conn) ([]string, error) {
			ran = append(ran, "b")
			return []string{
				"CREATE DATABASE IF NOT EXISTS dep",
				"CREATE OR REPLACE VIEW dep.probe AS SELECT " + lwsqlsurface.VersionFunctionName + "() AS v, (SELECT count() FROM leeway.columns) >= 0 AS decoded",
			}, nil
		},
	})()
	defer lwsqlsurface.RegisterDependent(lwsqlsurface.Dependent{
		Name: "test a: not on this server",
		Statements: func(ctx context.Context, c lwsqlsurface.Conn) ([]string, error) {
			ran = append(ran, "a")
			return nil, nil
		},
	})()
	require.Equal(t, []string{"test a: not on this server", "test b: over the surface and its decode views"}, lwsqlsurface.Dependents(),
		"re-created by name, not by registration order")

	require.NoError(t, lwsqlsurface.Install(t.Context(), conn))
	require.Equal(t, []string{"a", "b"}, ran)
	out, err := conn.run("SELECT v, decoded FROM dep.probe")
	require.NoError(t, err)
	require.Equal(t, strconv.Itoa(lwsqlsurface.Version)+"\t1", strings.TrimSpace(out))
}

// failingConn refuses one statement at Exec time. localConn buffers writes
// and reports a failure only at the next read, which is too late to show
// which step it belonged to.
type failingConn struct {
	*localConn
	refuse string
}

func (inst failingConn) Exec(ctx context.Context, sql string) (err error) {
	if strings.Contains(sql, inst.refuse) {
		return eh.Errorf("refused: %s", inst.refuse)
	}
	return inst.localConn.Exec(ctx, sql)
}

// A failing dependent fails the install with its name; the surface it
// depends on is in place by then.
func TestInstallNamesAFailingDependent(t *testing.T) {
	conn := newLocalConn(t)
	unregister := lwsqlsurface.RegisterDependent(lwsqlsurface.Dependent{
		Name: "test: broken statement",
		Statements: func(ctx context.Context, c lwsqlsurface.Conn) ([]string, error) {
			return []string{"CREATE OR REPLACE VIEW nowhere.v AS SELECT 1"}, nil
		},
	})
	err := lwsqlsurface.Install(t.Context(), failingConn{localConn: conn, refuse: "nowhere.v"})
	unregister()
	require.Error(t, err)
	require.Contains(t, err.Error(), "install dependent views")
	require.Contains(t, err.Error(), "refused: nowhere.v")
	require.True(t, functionExists(t, conn, lwsqlsurface.VersionFunctionName), "the surface installed before the dependent failed")

	unregister = lwsqlsurface.RegisterDependent(lwsqlsurface.Dependent{
		Name: "test: cannot decide",
		Statements: func(ctx context.Context, c lwsqlsurface.Conn) ([]string, error) {
			return nil, eh.Errorf("no answer")
		},
	})
	defer unregister()
	err = lwsqlsurface.Install(t.Context(), conn)
	require.ErrorContains(t, err, "no answer")
}

func TestRegisterDependentRefusesDuplicatesAndBlanks(t *testing.T) {
	d := lwsqlsurface.Dependent{Name: "test: once", Statements: func(context.Context, lwsqlsurface.Conn) ([]string, error) { return nil, nil }}
	defer lwsqlsurface.RegisterDependent(d)()
	require.Panics(t, func() { lwsqlsurface.RegisterDependent(d) })
	require.Panics(t, func() { lwsqlsurface.RegisterDependent(lwsqlsurface.Dependent{Name: "test: no statements"}) })
	require.NotContains(t, lwsqlsurface.Dependents(), "test: no statements")
}
