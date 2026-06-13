package factsschema_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	factsddl "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ddl"
)

// TestComposeCreateTableSql_AppliesToLiveClickHouse exercises the leeway DDL
// emitter against the project's localhost ClickHouse (per
// reference_clickhouse_localhost_defaults). Validates that:
//
//   - the emitted CREATE TABLE statement parses,
//   - ClickHouse accepts every leeway-encoded physical column,
//   - the table is queryable after the DDL applies.
//
// Skips when the server is unreachable so the suite stays green offline.
// Uses a per-test database so it doesn't collide with any other agent's
// runtime data.
func TestComposeCreateTableSql_AppliesToLiveClickHouse(t *testing.T) {
	ctx := context.Background()
	cli := chclient.New(chclient.Defaults(), nil)
	if err := cli.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", chclient.Defaults().URL, err)
	}

	const dbName = "runtime_factsschema_test"
	require.NoError(t, cli.Exec(ctx, "DROP DATABASE IF EXISTS "+dbName))
	defer func() {
		_ = cli.Exec(ctx, "DROP DATABASE IF EXISTS "+dbName)
	}()
	require.NoError(t, cli.Exec(ctx, "CREATE DATABASE "+dbName))

	// The engine clause must reference physical column names — leeway
	// encodes them as e.g. "id:id:u64:2k:0:0:" (the physical name for the
	// plain `id` column). Production hosts pick a real PARTITION BY /
	// ORDER BY against the physical names; the smoke test only validates
	// the schema applies, so ORDER BY tuple() suffices.
	ddl, err := factsddl.ComposeCreateTableSql(`MergeTree() ORDER BY tuple()`)
	require.NoError(t, err)
	require.Contains(t, ddl, "runtime.facts")

	// Retarget the table onto our test database.
	ddl = strings.ReplaceAll(ddl, "runtime.facts", dbName+".facts")

	// ClickHouse HTTP rejects multi-statement bodies. Split on top-level
	// `;` and Exec each piece individually, dropping the bootstrap CREATE
	// DATABASE — we already created our own.
	for _, stmt := range splitStatements(ddl) {
		trim := strings.TrimSpace(stmt)
		if trim == "" || strings.HasPrefix(trim, "CREATE DATABASE") {
			continue
		}
		err = cli.Exec(ctx, trim)
		require.NoError(t, err, "leeway DDL statement must apply against live CH; stmt=%q", trim[:min(120, len(trim))])
	}

	// Sanity-check the table is queryable.
	body, err := cli.Query(ctx, "SELECT count() FROM "+dbName+".facts FORMAT TabSeparated")
	require.NoError(t, err)
	defer body.Close()
	out, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, "0\n", string(out))
}

// splitStatements breaks a multi-statement SQL string on top-level semicolons.
// Naive — does not parse strings, comments, or nested constructs. Adequate
// for the boxer-emitted DDL which has no embedded `;`.
func splitStatements(sql string) (out []string) {
	for _, s := range strings.Split(sql, ";") {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
