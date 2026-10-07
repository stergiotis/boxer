package introspectengine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/trivialsql"
)

// A statement the trivial evaluator accepts answers the same through it as
// through clickhouse-local (ADR-0290: one statement text in every host).
func TestTrivialSQL_AnswersAsClickHouseDoes(t *testing.T) {
	e := newEngineWithBroker(t)
	require.NoError(t, e.reg.Register(seqProvider{}))
	params := map[string]string{"k": "4"}
	for _, sql := range []string{
		"SELECT * FROM keelson('seq', n = 3) FORMAT TabSeparated",
		"SELECT * FROM keelson('seq', n = {k:UInt64}) LIMIT 2 OFFSET 1 FORMAT TabSeparated",
		"WITH vf AS (SELECT * FROM keelson('seq', n = {k:UInt64})) SELECT * FROM vf LIMIT 1, 2 FORMAT TabSeparated",
		"SELECT * FROM keelson('seq', n = 2) FORMAT JSONEachRow",
	} {
		want, _, err := e.QueryParams(context.Background(), sql, "", params)
		require.NoError(t, err, sql)
		got, err := trivialsql.Run(context.Background(), e.reg, sql, params)
		require.NoError(t, err, sql)
		assert.Equal(t, string(want), string(got), sql)
	}
}
