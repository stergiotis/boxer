package play

// ADR-0181 §SD8 M3: the write gate's detection and the FORMAT step's
// statement-kind awareness. The gate itself is a two-line branch on these.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunIsInsertWrapper(t *testing.T) {
	require.True(t, runIsInsertWrapper("INSERT INTO t SELECT 1"))
	require.True(t, runIsInsertWrapper("SET param_event = 'DDOS';\nINSERT INTO db.t (a) SELECT a FROM src WHERE x = {event:String}"),
		"the SET prelude is harvested before the single-statement parse")
	require.False(t, runIsInsertWrapper("SELECT 1"))
	require.False(t, runIsInsertWrapper("INSERT INTO t VALUES (1)"),
		"a VALUES source is outside the wrapper's scope and outside grammar1")
	require.False(t, runIsInsertWrapper("DROP TABLE t"))
}

// TestBuildStatementFormatIsStatementKindAware pins the M3 FORMAT rule: a
// read gets `FORMAT ArrowStream` appended as always, the INSERT wrapper
// ships without one — the appended FORMAT is exactly why non-SELECT
// statements from play used to fail at the server.
func TestBuildStatementFormatIsStatementKindAware(t *testing.T) {
	c := NewClient(ClientConfig{URL: "http://example.invalid"}, nil)

	body, _ := c.BuildStatement("SELECT 1")
	require.Contains(t, body, "FORMAT ArrowStream")

	body, _ = c.BuildStatement("INSERT INTO t SELECT 1")
	require.NotContains(t, strings.ToUpper(body), "FORMAT")
	require.Contains(t, body, "INSERT INTO")
}
