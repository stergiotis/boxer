package trailviews

import (
	"context"
	"io"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// SurfaceDependentName is the name the views are registered under with the
// read-surface install (lwsqlsurface.RegisterDependent).
const SurfaceDependentName = "keelson trail views"

// The views inline the read surface, so they are a snapshot of it: every
// surface install re-creates them, last, once the functions they inline have
// verified. Linking this package is what opts a binary in — play links it,
// so its startup reconcile refreshes them, and so does `leeway sqlsurface
// install` in any binary that links play.
func init() {
	lwsqlsurface.RegisterDependent(lwsqlsurface.Dependent{
		Name:       SurfaceDependentName,
		Statements: surfaceStatements,
	})
}

// surfaceStatements is the views over the facts table, or nothing when the
// server being installed holds no facts table — most leeway endpoints are
// not keelson's, and a family over a table that is absent does not belong
// there.
//
// The facts table is the fixed one (factsschema), as chstore.ConfigFromEnv
// keeps it; an installation at another location re-creates its views with
// `keelsonddl --trail-views --database … --table …`.
func surfaceStatements(ctx context.Context, conn lwsqlsurface.Conn) (stmts []string, err error) {
	present, err := tableExists(ctx, conn, factsschema.DatabaseName, factsschema.TableName)
	if err != nil || !present {
		return nil, err
	}
	return Statements(factsschema.DatabaseName, factsschema.TableName)
}

// tableExists asks the server whether database.table exists.
func tableExists(ctx context.Context, conn lwsqlsurface.Conn, database string, table string) (exists bool, err error) {
	body, err := conn.Query(ctx, "EXISTS TABLE "+database+"."+table)
	if err != nil {
		return false, eh.Errorf("trailviews: ask whether %s.%s exists: %w", database, table, err)
	}
	defer func() {
		_ = body.Close()
	}()
	b, err := io.ReadAll(body)
	if err != nil {
		return false, eh.Errorf("trailviews: read whether %s.%s exists: %w", database, table, err)
	}
	return strings.TrimSpace(string(b)) == "1", nil
}
