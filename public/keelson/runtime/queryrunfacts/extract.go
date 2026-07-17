package queryrunfacts

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ScopeE is the capture-scope knob (ADR-0115 outline): all terminal
// events, only boxer-stamped ones, or capture off. "off" is decided by
// the service (it serves an empty stream); the extract composer rejects
// it so a caller cannot accidentally build an unbounded query for a
// scope that must not extract.
type ScopeE string

const (
	ScopeAll     ScopeE = "all"
	ScopeStamped ScopeE = "stamped"
	ScopeOff     ScopeE = "off"
)

// Self-identification tags: every query the pipeline itself issues is
// excluded from capture by log_comment, or the pipeline would feed on
// its own extract (each 5s tick minting one new fact, forever).
const (
	// ExtractTag marks the service's own extract SELECT.
	ExtractTag = "queryrunsd-extract"
	// RefreshTag marks the refreshable MV's tick queries via the
	// SETTINGS clause in the MV body.
	RefreshTag = "queryrunsd-refresh"
)

// Physical leeway wire-column names of runtime.facts the pipeline SQL
// references (the chstore composeLatestStateSql convention; the DDL
// parse test in mv_test.go asserts they exist in ddl.ColumnsSQL).
const (
	ColId       = "`id:id:u64:2k:0:0:`"
	ColTs       = "`ts:ts:z64:2k:0:0:`"
	ColSymbolLr = "`tv:symbol:lr:lr:u64:2q:0:0:0::data`"
)

// WatermarkOverlap is the lookback subtracted from the destination
// watermark. query_log buffers flush time-batched, so an event can
// surface with an event_time slightly older than the newest event
// already captured; re-reading the overlap costs nothing (the
// deterministic ids dedup in the MV anti-join) while a strict `>`
// watermark would drop such stragglers forever.
const WatermarkOverlap = "INTERVAL 60 SECOND"

// DefaultBatchCap bounds one extract (and hence one /pull response).
// First-boot backfill can face the source TTL's worth of query_log;
// ORDER BY event time ascending + this cap turns that into bounded
// batches that advance the watermark refresh by refresh.
const DefaultBatchCap = 10000

// ComposeExtractSql builds the extract SELECT over system.query_log:
// terminal events only, newer than the destination watermark (max
// KindQueryRun fact ts, minus WatermarkOverlap), excluding the
// pipeline's own queries — by tag, and by the pull URL appearing in the
// query text (the belt for the case where a ClickHouse version does not
// apply the MV body's SETTINGS log_comment to refresh queries).
//
// factsTable is the qualified destination ("runtime.facts"); pullURL is
// the endpoint the MV reads, e.g. "http://127.0.0.1:8127/pull";
// batchCap <= 0 applies DefaultBatchCap.
func ComposeExtractSql(factsTable string, pullURL string, scope ScopeE, batchCap int) (sql string, err error) {
	if factsTable == "" || pullURL == "" {
		err = eh.Errorf("queryrunfacts: extract needs factsTable + pullURL")
		return
	}
	if batchCap <= 0 {
		batchCap = DefaultBatchCap
	}
	var scopePredicate string
	switch scope {
	case ScopeAll:
		scopePredicate = ""
	case ScopeStamped:
		scopePredicate = "\n  AND JSONHas(log_comment, 'run_id')"
	default:
		err = eh.Errorf("queryrunfacts: extract not composable for scope %q", scope)
		return
	}
	sql = fmt.Sprintf(`SELECT
  type,
  toUnixTimestamp64Micro(event_time_microseconds) AS event_us,
  query_id,
  substring(query, 1, %d) AS query,
  normalized_query_hash,
  query_kind,
  query_duration_ms,
  read_rows, read_bytes, written_rows, written_bytes, result_rows, result_bytes,
  memory_usage,
  exception_code,
  exception,
  ProfileEvents,
  log_comment
FROM system.query_log
WHERE type != 'QueryStart'
  AND log_comment NOT IN (%s, %s)
  AND position(query, %s) = 0%s
  AND event_time_microseconds >= (
    SELECT max(%s) FROM %s WHERE has(%s, %d)
  ) - %s
ORDER BY event_time_microseconds
LIMIT %d
SETTINGS output_format_json_quote_64bit_integers=0, log_comment=%s
FORMAT JSONEachRow`,
		QueryTextCap,
		quoteSqlString(ExtractTag), quoteSqlString(RefreshTag),
		quoteSqlString(pullURL), scopePredicate,
		ColTs, factsTable, ColSymbolLr, vocab.MembKindQueryRun.GetId().Value(),
		WatermarkOverlap,
		batchCap,
		quoteSqlString(ExtractTag))
	return
}

// quoteSqlString single-quotes s for inline SQL, escaping single quotes
// by doubling (the chstore convention — these values are not amenable
// to parameter binding inside DDL / MV bodies).
func quoteSqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
