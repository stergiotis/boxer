package play

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// The scope gate (ADR-0340 §SD3): an assertion names only tables the
// source statement read, its own CTEs aside; keelson tables count; a
// statement naming nothing is refused.
func TestACheckStaysWithinTheResultsTables(t *testing.T) {
	source := "SELECT region, sum(revenue) AS r FROM sales.orders WHERE month = 3 GROUP BY region"
	assert.NoError(t, checkScope("SELECT sum(revenue) FROM sales.orders WHERE month = 3", source))
	err := checkScope("SELECT sum(revenue) FROM orders WHERE month = 3", source)
	require.Error(t, err, "a table the source qualified is named as the source named it")
	assert.Contains(t, err.Error(), "name tables as that statement did")
	assert.NoError(t, checkScope("SELECT count() FROM keelson('sales')", "SELECT * FROM keelson('sales')"), "a bound dataset's alias meets itself, since both texts are read as authored")
	err = checkScope("SELECT sum(x) FROM archive.orders", "SELECT sum(x) FROM orders")
	require.Error(t, err, "another database's table of the same name does not pass")
	assert.NoError(t, checkScope("WITH m AS (SELECT revenue FROM sales.orders) SELECT count() FROM m", source), "the assertion's own CTE is no table")
	err = checkScope("SELECT count() FROM sales.customers", source)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customers, which the result's statement did not read")
	assert.NoError(t, checkScope("SELECT sum(revenue) FROM sales.orders WHERE month = 3", "WITH m AS (SELECT 1) SELECT * FROM sales.orders, m"), "the source's CTE is not a table the assertion may name, but its tables are")
	err = checkScope("SELECT count() FROM crm.orders", source)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crm.orders")
	err = checkScope("SELECT true", source)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no table")
	assert.NoError(t, checkScope("SELECT count() FROM keelson('windows')", "SELECT key FROM keelson('windows')"))
	err = checkScope("SELECT count() FROM keelson('apps')", "SELECT key FROM keelson('windows')")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keelson('apps')")
	err = checkScope("SELEC", source)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse")
}

// Two spellings of one statement digest alike; a statement that does not
// parse digests as written.
func TestAStatementDigestIsCanonical(t *testing.T) {
	a := statementDigest("select  count() from t where x=1")
	b := statementDigest("SELECT count() FROM t WHERE x = 1")
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, statementDigest("SELECT count() FROM t WHERE x = 2"))
	assert.Len(t, a, 64)
	assert.Empty(t, statementDigest("  "))
	assert.NotEmpty(t, statementDigest("not sql at all"))
}

// A check against no result, or another result than the window holds, is
// refused before anything is sent.
func TestACheckNeedsTheResultItRestsOn(t *testing.T) {
	ctx := context.Background()
	_, err := runCheck(ctx, nil, checkSnap{}, nil, CheckArgs{ResultId: 7, Statement: "SELECT 1 FROM t"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "holds no result")
	held := checkSnap{checkSource: checkSource{resultId: 8, source: "SELECT x FROM t"}}
	_, err = runCheck(ctx, nil, held, nil, CheckArgs{ResultId: 7, Statement: "SELECT 1 FROM t"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "another result is held now: 8")
	assert.Contains(t, err.Error(), "result 7 is not among the last")
	_, err = runCheck(ctx, nil, held, nil, CheckArgs{ResultId: 8, Statement: ""})
	require.Error(t, err)
	var refused *app.OperationRefusal
	assert.ErrorAs(t, err, &refused, "an empty statement is a refusal, not a failure")
}

// A check rests on the result the window holds or on one of the results
// before it, each with its own statement and parameters; an older one is
// forgotten.
func TestACheckRestsOnAnEarlierResultTheWindowRemembers(t *testing.T) {
	var history []checkSource
	for id := uint64(1); id <= checkHistoryMax+2; id++ {
		history = rememberCheckSource(history, checkSource{resultId: id, source: "SELECT " + strconv.FormatUint(id, 10) + " FROM t",
			sigParams: map[string]string{"x": strconv.FormatUint(id, 10)}})
		history = rememberCheckSource(history, checkSource{resultId: id, source: "a second snapshot of the same result"})
	}
	history = rememberCheckSource(history, checkSource{resultId: 0, source: "nothing ran"})
	history = rememberCheckSource(history, checkSource{resultId: 99, source: ""})
	require.Len(t, history, checkHistoryMax)
	assert.Equal(t, uint64(3), history[0].resultId, "the oldest two are forgotten")

	held := checkSnap{checkSource: history[len(history)-1], earlier: history[:len(history)-1]}
	src, superseded, ok := held.lookup(checkHistoryMax + 2)
	require.True(t, ok)
	assert.False(t, superseded)
	assert.Equal(t, "SELECT 18 FROM t", src.source)

	src, superseded, ok = held.lookup(5)
	require.True(t, ok)
	assert.True(t, superseded)
	assert.Equal(t, "SELECT 5 FROM t", src.source, "an earlier result keeps its own statement")
	assert.Equal(t, "5", src.sigParams["x"], "and the parameters it shipped")

	_, _, ok = held.lookup(2)
	assert.False(t, ok)
	_, _, ok = held.lookup(0)
	assert.False(t, ok)
}

func TestCellTextPrintsAsClickHouseDoes(t *testing.T) {
	assert.Equal(t, "12.5", cellText(12.5))
	assert.Equal(t, "3", cellText(float64(3)))
	assert.Equal(t, "true", cellText(true))
	assert.Equal(t, "x", cellText("x"))
	assert.Equal(t, "[1,2]", cellText([]any{float64(1), float64(2)}))
}
