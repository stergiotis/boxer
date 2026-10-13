package chat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

// The conversation's trail opens on the timeline with every handle it names
// resolving against the timeline's own columns, as play's pre-execute stage
// resolves them; a handle the view does not have would ship verbatim and
// fail on the server.
func TestTrailSqlResolvesOverTheTimeline(t *testing.T) {
	sql := trailSql("conv-1'x")
	assert.Contains(t, sql, "= 'conv-1\\'x'")
	_, err := nanopass.Parse(sql)
	require.NoError(t, err)

	views, err := trailviews.Compose("", "")
	require.NoError(t, err)
	var physical []string
	for _, v := range views {
		if v.Name == trailviews.ViewTimeline {
			for _, c := range v.Columns {
				physical = append(physical, c.Physical)
			}
		}
	}
	require.NotEmpty(t, physical)
	table := factsschema.DatabaseName + "." + trailviews.ViewTimeline
	resolver := lwsql.NewResolver(passes.NewStaticSchemaProvider(map[string][]string{table: physical}))
	var diags []passes.ColumnDiagnostic
	out, err := passes.ResolveColumnNames(resolver, factsschema.DatabaseName, func(d passes.ColumnDiagnostic) {
		diags = append(diags, d)
	}).Apply(env.NewEnvironment(), sql)
	require.NoError(t, err)
	assert.Empty(t, diags)
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "--") {
			continue
		}
		for _, handle := range []string{"`timestamp:", "`opaque:"} {
			assert.NotContains(t, line, handle, "a handle is left unresolved")
		}
	}
}
