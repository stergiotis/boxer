package chstore

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

// launchStatsRecord builds a result batch the way the server shapes one: the
// app as a string, the count and timestamp as integers, the score a float.
func launchStatsRecord(t *testing.T, fields []arrow.Field, fill func(b *array.RecordBuilder)) arrow.RecordBatch {
	t.Helper()
	b := array.NewRecordBuilder(memory.NewGoAllocator(), arrow.NewSchema(fields, nil))
	defer b.Release()
	fill(b)
	rec := b.NewRecordBatch()
	t.Cleanup(rec.Release)
	return rec
}

// TestLaunchStatOf_ReadsTheFourColumns is the always-running half of
// ADR-0214 §SD7's verification: the SQL needs a server, the row read does
// not. The widths are the server's choices, not this package's, so a
// narrower integer reads the same.
func TestLaunchStatOf_ReadsTheFourColumns(t *testing.T) {
	rec := launchStatsRecord(t, []arrow.Field{
		{Name: "app_id", Type: arrow.BinaryTypes.Binary},
		{Name: "opens", Type: arrow.PrimitiveTypes.Uint64},
		{Name: "last_ts", Type: arrow.PrimitiveTypes.Uint32},
		{Name: "score", Type: arrow.PrimitiveTypes.Float64},
	}, func(b *array.RecordBuilder) {
		b.Field(0).(*array.BinaryBuilder).AppendValues([][]byte{[]byte("github.com/x/play"), []byte("github.com/x/imztop")}, nil)
		b.Field(1).(*array.Uint64Builder).AppendValues([]uint64{12, 3}, nil)
		b.Field(2).(*array.Uint32Builder).AppendValues([]uint32{1756600000, 1756500000}, nil)
		b.Field(3).(*array.Float64Builder).AppendValues([]float64{7.5, 0.25}, nil)
	})
	var stats []factsstore.AppLaunchStat
	require.NoError(t, eachRow(rec, "app launch stats", 4, func(r row) { stats = append(stats, launchStatOf(r)) }))
	require.Len(t, stats, 2)
	assert.Equal(t, "github.com/x/play", string(stats[0].AppId))
	assert.Equal(t, uint64(12), stats[0].Opens)
	assert.Equal(t, time.Unix(1756600000, 0).UTC(), stats[0].LastTs)
	assert.InDelta(t, 7.5, stats[0].Score, 1e-9)
}

// TestEachRow_MalformedFailsTheRead: this feeds an ordering, and an ordering
// computed from a silently truncated trail is worse than one the caller
// knows it could not compute.
func TestEachRow_MalformedFailsTheRead(t *testing.T) {
	str := arrow.Field{Name: "app_id", Type: arrow.BinaryTypes.String}
	t.Run("too few columns", func(t *testing.T) {
		rec := launchStatsRecord(t, []arrow.Field{str}, func(b *array.RecordBuilder) {
			b.Field(0).(*array.StringBuilder).Append("app")
		})
		require.Error(t, eachRow(rec, "app launch stats", 4, func(r row) { _ = launchStatOf(r) }))
	})
	t.Run("a count that is not a number", func(t *testing.T) {
		rec := launchStatsRecord(t, []arrow.Field{str,
			{Name: "opens", Type: arrow.BinaryTypes.String},
			{Name: "last_ts", Type: arrow.PrimitiveTypes.Uint32},
			{Name: "score", Type: arrow.PrimitiveTypes.Float64},
		}, func(b *array.RecordBuilder) {
			b.Field(0).(*array.StringBuilder).Append("app")
			b.Field(1).(*array.StringBuilder).Append("many")
			b.Field(2).(*array.Uint32Builder).Append(1)
			b.Field(3).(*array.Float64Builder).Append(1)
		})
		require.Error(t, eachRow(rec, "app launch stats", 4, func(r row) { _ = launchStatOf(r) }))
	})
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
	assert.Contains(t, sql, "LW_GET('symbol', 'runtime-lifecycle-phase'", "the phase is read by name, not by lane position")
}
