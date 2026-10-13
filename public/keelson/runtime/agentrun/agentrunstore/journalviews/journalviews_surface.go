package journalviews

import (
	"context"
	"io"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// SurfaceDependentName is the name the views are registered under with the
// read-surface install.
const SurfaceDependentName = "keelson journal views"

// The views inline the read surface, so every surface install re-creates
// them, last. Linking this package is what opts a binary in.
func init() {
	lwsqlsurface.RegisterDependent(lwsqlsurface.Dependent{
		Name:       SurfaceDependentName,
		Statements: surfaceStatements,
	})
}

// surfaceStatements is the views over the ledger in the default database,
// or nothing where the server holds no ledger; a ledger elsewhere gets its
// views from [Statements].
func surfaceStatements(ctx context.Context, conn lwsqlsurface.Conn) (stmts []string, err error) {
	for _, table := range []string{agentrunstore.TableNameRun, agentrunstore.TableNameEntry} {
		present, perr := tableExists(ctx, conn, agentrunstore.DatabaseName, table)
		if perr != nil || !present {
			return nil, perr
		}
	}
	return Statements(agentrunstore.DatabaseName)
}

func tableExists(ctx context.Context, conn lwsqlsurface.Conn, database string, table string) (exists bool, err error) {
	body, err := conn.Query(ctx, "EXISTS TABLE "+database+"."+table)
	if err != nil {
		return false, eb.Build().Str("database", database).Str("table", table).Errorf("journalviews: ask whether the table exists: %w", err)
	}
	defer func() {
		_ = body.Close()
	}()
	b, err := io.ReadAll(body)
	if err != nil {
		return false, eb.Build().Str("database", database).Str("table", table).Errorf("journalviews: read whether the table exists: %w", err)
	}
	return strings.TrimSpace(string(b)) == "1", nil
}
