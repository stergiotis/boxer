package play

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

// TestHelpTrailSnippetsResolve asserts every help block that reads a trail
// view names only columns the view has. The pre-execute stage resolves
// handles best-effort: a handle no column matches ships verbatim and the
// server rejects it, so a view column renamed under the snippets would
// otherwise surface only when someone runs one.
func TestHelpTrailSnippetsResolve(t *testing.T) {
	views, err := trailviews.Compose("", "")
	require.NoError(t, err)
	schemas := map[string][]string{}
	for _, v := range views {
		cols := make([]string, 0, len(v.Columns))
		for _, c := range v.Columns {
			cols = append(cols, c.Physical)
		}
		schemas[factsschema.DatabaseName+"."+v.Name] = cols
	}
	resolver := lwsql.NewResolver(passes.NewStaticSchemaProvider(schemas))

	docs, err := fs.Glob(helpFS, "help/*.md")
	require.NoError(t, err)
	seen := 0
	for _, path := range docs {
		raw, readErr := fs.ReadFile(helpFS, path)
		require.NoError(t, readErr, "read %s", path)
		for _, blk := range sqlFences(string(raw)) {
			if !strings.Contains(blk.sql, factsschema.DatabaseName+".dm_trail_") && !strings.Contains(blk.sql, factsschema.DatabaseName+".agg_trail_") {
				continue
			}
			seen++
			residual, _, exErr := ExtractParams(blk.sql)
			require.NoErrorf(t, exErr, "%s:%d", path, blk.line)
			var diags []passes.ColumnDiagnostic
			out, resErr := passes.ResolveColumnNames(resolver, factsschema.DatabaseName, func(d passes.ColumnDiagnostic) {
				diags = append(diags, d)
			}).Apply(env.NewEnvironment(), residual)
			require.NoErrorf(t, resErr, "%s:%d", path, blk.line)
			require.Emptyf(t, diags, "%s:%d: a handle names no column of the view\n%s", path, blk.line, blk.sql)
			for line := range strings.SplitSeq(out, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "--") {
					continue
				}
				require.NotContainsf(t, line, "`opaque:", "%s:%d: a handle is left unresolved\n%s", path, blk.line, out)
				require.NotContainsf(t, line, "`timestamp:", "%s:%d: a handle is left unresolved\n%s", path, blk.line, out)
			}
		}
	}
	require.NotZero(t, seen, "the help corpus has trail snippets")
}
