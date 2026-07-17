package queryrunfacts

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

func TestComposeHistorySql(t *testing.T) {
	sql, err := ComposeHistorySql("runtime.facts", 100)
	require.NoError(t, err)
	require.Contains(t, sql, fmt.Sprintf("has(%s, %d)", ColSymbolLr, vocab.MembKindQueryRun.GetId().Value()))
	require.Contains(t, sql, "ORDER BY "+ColTs+" DESC")
	require.Contains(t, sql, "LIMIT 100")
	require.Contains(t, sql, "FORMAT TabSeparated")
	// The SELECT arity is the parse contract.
	require.Equal(t, historyRowColumns, strings.Count(sql, " AS "),
		"compose and parse must agree on the column count")

	sql, err = ComposeHistorySql("runtime.facts", 0)
	require.NoError(t, err)
	require.Contains(t, sql, fmt.Sprintf("LIMIT %d", HistoryLimitCap))
	sql, err = ComposeHistorySql("runtime.facts", 10_000)
	require.NoError(t, err)
	require.Contains(t, sql, fmt.Sprintf("LIMIT %d", HistoryLimitCap))

	_, err = ComposeHistorySql("", 10)
	require.Error(t, err)
}

func TestParseHistoryRowsRoundTrip(t *testing.T) {
	line := strings.Join([]string{
		"9223372036854775809",       // id (band bit set)
		"1752700000",                // ts_sec
		"play-map-1234-7",           // query_id
		"QueryFinish",               // event
		"Select",                    // kind
		"map",                       // lane
		"data.play",                 // app
		"run-1",                     // run_id
		"42",                        // duration_ms
		"100",                       // read_rows
		"2048",                      // read_bytes
		"0",                         // written_rows
		"0",                         // written_bytes
		"1",                         // result_rows
		"64",                        // result_bytes
		"1048576",                   // memory_peak
		"18446744073709551615",      // normalized_hash (full range)
		"0",                         // exception_code
		"",                          // exception
		"SELECT 1\\nFROM t\\tWHERE", // query_text with escapes
	}, "\t")
	rows, err := ParseHistoryRows([]byte(line + "\n"))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	r := rows[0]
	require.Equal(t, uint64(1)<<63|uint64(1), r.Id)
	require.Equal(t, time.Unix(1752700000, 0).UTC(), r.Ts)
	require.Equal(t, "play-map-1234-7", r.QueryId)
	require.Equal(t, "QueryFinish", r.Event)
	require.Equal(t, "map", r.Lane)
	require.Equal(t, "data.play", r.App)
	require.Equal(t, uint64(42), r.DurationMs)
	require.Equal(t, uint64(18446744073709551615), r.NormalizedHash)
	require.Equal(t, "SELECT 1\nFROM t\tWHERE", r.QueryText)
	require.Empty(t, r.Exception)

	rows, err = ParseHistoryRows(nil)
	require.NoError(t, err)
	require.Empty(t, rows)

	_, err = ParseHistoryRows([]byte("only\tthree\tcolumns\n"))
	require.Error(t, err)
	bad := strings.Replace(line, "1752700000", "not-a-number", 1)
	_, err = ParseHistoryRows([]byte(bad))
	require.Error(t, err)
}

func TestComposeProfileEventsSql(t *testing.T) {
	sql, err := ComposeProfileEventsSql("runtime.facts", 123)
	require.NoError(t, err)
	require.Contains(t, sql, "ARRAY JOIN arrayZip(")
	require.Contains(t, sql, fmt.Sprintf("m = %d", vocab.MembQueryRunProfileEvent.GetId().Value()))
	require.Contains(t, sql, fmt.Sprintf("WHERE %s = 123", ColId))
	require.Contains(t, sql, "ORDER BY count DESC")

	_, err = ComposeProfileEventsSql("", 1)
	require.Error(t, err)
}
