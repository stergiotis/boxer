package chstore

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseAppLaunchStats_ReadsTheFourColumns is the always-running half of
// ADR-0214 §SD7's verification: the SQL needs a server, the parse does not.
func TestParseAppLaunchStats_ReadsTheFourColumns(t *testing.T) {
	raw := []byte("github.com/x/play\t12\t1756600000\t7.5\n" +
		"github.com/x/imztop\t3\t1756500000\t0.25\n")
	stats, err := parseAppLaunchStats(raw)
	require.NoError(t, err)
	require.Len(t, stats, 2)
	assert.Equal(t, "github.com/x/play", string(stats[0].AppId))
	assert.Equal(t, uint64(12), stats[0].Opens)
	assert.Equal(t, time.Unix(1756600000, 0).UTC(), stats[0].LastTs)
	assert.InDelta(t, 7.5, stats[0].Score, 1e-9)
}

// TestParseAppLaunchStats_EmptyIsEmptyNotNil keeps "no history" a slice a
// caller can range over without a nil check, matching the store's other
// readers.
func TestParseAppLaunchStats_EmptyIsEmptyNotNil(t *testing.T) {
	stats, err := parseAppLaunchStats(nil)
	require.NoError(t, err)
	assert.NotNil(t, stats)
	assert.Empty(t, stats)

	stats, err = parseAppLaunchStats([]byte("\n"))
	require.NoError(t, err)
	assert.Empty(t, stats)
}

// TestParseAppLaunchStats_MalformedFailsTheRead: this feeds an ordering, and an
// ordering computed from a silently truncated trail is worse than one the
// caller knows it could not compute.
func TestParseAppLaunchStats_MalformedFailsTheRead(t *testing.T) {
	for name, raw := range map[string]string{
		"too few columns": "app\t1\t2\n",
		"bad opens":       "app\tmany\t1756600000\t1.0\n",
		"bad ts":          "app\t1\tyesterday\t1.0\n",
		"bad score":       "app\t1\t1756600000\tlots\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseAppLaunchStats([]byte(raw))
			require.Error(t, err)
		})
	}
}

// TestComposeAppLaunchStatsSql_ShapeIsTheDecision pins the three choices in the
// query that are not obvious from reading it, each of which would be a silent
// wrong answer rather than an error:
//
//   - only `started` rows, because a close is not a launch and counting both
//     double-weights every finished session;
//   - the decay against the SERVER's clock, because the rows were written with
//     the server's now() and a skewed client would otherwise see the future;
//   - grouped by app, ordered by score, and bounded.
func TestComposeAppLaunchStatsSql_ShapeIsTheDecision(t *testing.T) {
	sql := composeAppLaunchStatsSql("boxer.facts", 336*time.Hour, 100)
	assert.Contains(t, sql, "= 'started'", "a close is not a launch")
	assert.NotContains(t, sql, "'stopped'")
	assert.Contains(t, sql, "toUnixTimestamp(now())", "decay against the server clock")
	assert.Contains(t, sql, "GROUP BY app_id")
	assert.Contains(t, sql, "ORDER BY score DESC")
	assert.Contains(t, sql, "LIMIT 100")
	assert.Contains(t, sql, "1209600.000000", "the half-life reaches the query in seconds")
	assert.Contains(t, sql, "app_id != ''", "a row with no app reference is not an app's launch")
	assert.Equal(t, 1, strings.Count(sql, "FORMAT TabSeparated"))
}
