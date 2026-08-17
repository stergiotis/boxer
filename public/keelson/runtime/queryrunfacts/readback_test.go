package queryrunfacts

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// physical strips the backticks off one of extract.go's hardcoded wire-column
// names, so a test can assert that a handle resolved to that same name — the
// two halves of this package name the same column two ways, and nothing else
// checks that they agree.
func physical(quoted string) (name string) {
	return strings.Trim(quoted, "`")
}

func TestComposeHistorySql(t *testing.T) {
	sql, err := ComposeHistorySql("boxer.facts", 100)
	require.NoError(t, err)

	// The kind test survives expansion as a has() over the membership lane:
	// it is the only term that prunes granules through the skip index, so it
	// must stay a has() and must name the lane the extract path hardcodes.
	require.Contains(t, sql, fmt.Sprintf("has(%q, %d)",
		physical(ColSymbolLr), vocab.MembKindQueryRun.GetId().Value()))
	require.Contains(t, sql, fmt.Sprintf("ORDER BY %q DESC", physical(ColTs)))
	require.Contains(t, sql, fmt.Sprintf("%q AS id", physical(ColId)))
	require.Contains(t, sql, "LIMIT 100")
	require.Contains(t, sql, "FORMAT TabSeparated")

	sql, err = ComposeHistorySql("boxer.facts", 0)
	require.NoError(t, err)
	require.Contains(t, sql, fmt.Sprintf("LIMIT %d", HistoryLimitCap))
	sql, err = ComposeHistorySql("boxer.facts", 10_000)
	require.NoError(t, err)
	require.Contains(t, sql, fmt.Sprintf("LIMIT %d", HistoryLimitCap))

	_, err = ComposeHistorySql("", 10)
	require.Error(t, err)
	// The database half is what resolves the unqualified references inside,
	// so an unqualified table cannot be prepared.
	_, err = ComposeHistorySql("facts", 10)
	require.Error(t, err)
}

// TestComposeHistorySqlExpandsFully pins that what ships carries no
// client-side name: play's History tab posts this over plain HTTP, outside
// the pass pipeline, so an unexpanded LW_GET would reach the server as an
// unknown function.
func TestComposeHistorySqlExpandsFully(t *testing.T) {
	sql, err := ComposeHistorySql("boxer.facts", 10)
	require.NoError(t, err)
	require.False(t, constructsql.HasExtractMarker(sql),
		"the posted query still carries an unexpanded extraction call")
	require.NotContains(t, sql, "`", "handles must have resolved to physical names")

	// What it expanded INTO is the read-back family — the scalar section
	// through LW_VALUE_BY_TAG_EQUAL, the array sections through
	// LW_LIST_BY_TAG_EQUAL. The second is the call the jsonbench trial found
	// the hand-written gather silently truncating against.
	require.Contains(t, sql, "LW_VALUE_BY_TAG_EQUAL(")
	require.Contains(t, sql, "LW_LIST_BY_TAG_EQUAL(")
}

// TestComposeHistoryAuthoredArity checks the SELECT arity on the authored
// form rather than the expanded one: the expansion emits calls of its own, so
// counting aliases after it would count the wrong thing.
func TestComposeHistoryAuthoredArity(t *testing.T) {
	sql, err := composeHistoryAuthored("boxer.facts", 100)
	require.NoError(t, err)
	require.Equal(t, historyRowColumns, strings.Count(sql, " AS "),
		"compose and parse must agree on the column count")

	// Every membership the row model carries a column for is named, not
	// numbered — the registry's own folded spelling, which the expansion
	// resolves back to an id (ADR-0171 §SD4).
	for _, m := range []registry.RegisteredNaturalKey{
		vocab.MembQueryRunEventType,
		vocab.MembQueryRunQueryKind,
		vocab.MembQueryRunLane,
		vocab.MembRuntimeApp,
		vocab.MembRuntimeRun,
		vocab.MembQueryRunDurationMs,
		vocab.MembQueryRunNormalizedHash,
		vocab.MembQueryRunExceptionCode,
		vocab.MembQueryRunQueryText,
	} {
		require.Contains(t, sql, fmt.Sprintf("'%s'", m.GetNaturalKey()))
	}
	// The kind test is the exception, and deliberately: has() takes a
	// literal, and it is the term that prunes.
	require.Contains(t, sql, fmt.Sprintf("has(%s, %d)", hSymLr, vocab.MembKindQueryRun.GetId().Value()))
	// The app and run stamps ride the mixed channel; everything else is the
	// ordinary one-membership-per-attribute one.
	require.Contains(t, sql, mixedChannel)
	require.Contains(t, sql, plainChannel)
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

// TestComposeProfileEventsSql pins the drill-down's authored shape. Unlike
// the history query this one is deliberately NOT expanded: it goes into
// play's editor, where the pipeline expands it and the reader sees the
// vocabulary rather than the arithmetic it stands for.
func TestComposeProfileEventsSql(t *testing.T) {
	sql, err := ComposeProfileEventsSql("boxer.facts", 123)
	require.NoError(t, err)
	pe := vocab.MembQueryRunProfileEvent.GetNaturalKey()

	require.Contains(t, sql, "ARRAY JOIN arrayZip(")
	// Names off the parameter lane, counts off the value lane, selected by
	// two co-indexed selectors over the same membership — and the membership
	// named rather than numbered, since this is the query a person reads.
	require.Contains(t, sql, fmt.Sprintf("LW_SEL('u64Array', '%s', '%s')", pe, mixedChannel))
	require.Contains(t, sql, fmt.Sprintf("LW_SEL_ATTRS('u64Array', '%s', '%s')", pe, mixedChannel))
	require.NotContains(t, sql, fmt.Sprintf("%d", vocab.MembQueryRunProfileEvent.GetId().Value()),
		"the editor-facing query should carry no raw membership id")
	require.Contains(t, sql, "LW_CO_GATHER(")
	// The value gather goes through the raggedness rather than assuming one
	// value per attribute — the assumption the previous form baked in.
	require.Contains(t, sql, "LW_RAGGED_ELEM(")
	require.Contains(t, sql, fmt.Sprintf("WHERE %s = 123", hId))
	require.Contains(t, sql, "ORDER BY count DESC")

	_, err = ComposeProfileEventsSql("", 1)
	require.Error(t, err)
}

// TestComposeProfileEventsSqlIsExpandable is the other half of leaving the
// drill-down authored: a query play hands to the editor has to be one the
// editor's pipeline can actually expand, and this asserts it against the same
// passes without needing play.
func TestComposeProfileEventsSqlIsExpandable(t *testing.T) {
	sql, err := ComposeProfileEventsSql("boxer.facts", 123)
	require.NoError(t, err)
	expanded, err := prepare(sql, "boxer.facts")
	require.NoError(t, err)
	require.False(t, constructsql.HasExtractMarker(expanded))
	require.Contains(t, expanded, physical(ColId))
}

func TestCheckSurfaceVersion(t *testing.T) {
	require.NoError(t, CheckSurfaceVersion(fmt.Appendf(nil, "%d\n", lwsqlsurface.Version), nil))

	// An unprovisioned server answers with an unknown-function error; the
	// message has to name the fix rather than repeat the server's.
	err := CheckSurfaceVersion(nil, fmt.Errorf("Unknown function LW_SURFACE_VERSION"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "sqlsurface install")

	// A revision this build does not emit against is refused, not attempted.
	err = CheckSurfaceVersion(fmt.Appendf(nil, "%d\n", lwsqlsurface.Version+1), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "reconcile")

	err = CheckSurfaceVersion([]byte("not-a-version"), nil)
	require.Error(t, err)
}

// TestSurfaceVersionSql keeps the probe a single statement the raw HTTP path
// can post as-is.
func TestSurfaceVersionSql(t *testing.T) {
	require.Contains(t, SurfaceVersionSql(), lwsqlsurface.VersionFunctionName+"()")
	require.Contains(t, SurfaceVersionSql(), "FORMAT TabSeparated")
}
