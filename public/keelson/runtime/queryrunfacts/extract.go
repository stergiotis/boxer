package queryrunfacts

import (
	"fmt"
	"slices"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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

// AllScopes lists every scope, in the order the knob documents them — the
// one list the env registry's allowed values and Valid read.
func AllScopes() (scopes []ScopeE) {
	return []ScopeE{ScopeAll, ScopeStamped, ScopeOff}
}

// Valid says inst is one of AllScopes.
func (inst ScopeE) Valid() (ok bool) {
	return slices.Contains(AllScopes(), inst)
}

// Self-identification tags: every query the pipeline itself issues is
// excluded from capture by log_comment, or the pipeline would feed on
// its own extract (each 5s tick minting one new fact, forever).
const (
	// ExtractTag marks the service's own extract SELECT.
	ExtractTag = "queryrunsd-extract"
	// RefreshTag marks the refreshable MV's tick queries via the
	// SETTINGS clause in the MV body.
	RefreshTag = "queryrunsd-refresh"
	// ReconcileTag marks everything else the service sends: the boot
	// reconciliation's DDL, flush and schema checks.
	ReconcileTag = "queryrunsd-reconcile"
)

// Physical leeway wire-column names of boxer.facts the pipeline SQL
// references (the chstore composeLatestWorkingsetSql convention; the DDL
// parse test in mv_test.go asserts they exist in ddl.ColumnsSQL).
const (
	ColId         = "`id:id:u64:47::0:`"
	ColNaturalKey = "`id:naturalKey:y:4::0:`"
	ColTs         = "`ts:ts:z64:47::0:`"
	ColSymbolLr   = "`tv:symbol:lr:lr:u64:1247:::0::data`"
)

// WatermarkOverlap is the lookback subtracted from the destination
// watermark. query_log buffers flush time-batched, so an event can
// surface with an event_time slightly older than the newest event
// already captured; re-reading the overlap costs nothing (the
// deterministic ids dedup in the MV anti-join) while a strict `>`
// watermark would drop such stragglers forever.
const WatermarkOverlap = "INTERVAL 60 SECOND"

// watermarkSql is the destination watermark as a scalar subquery: the
// newest KindQueryRun fact ts, or the DateTime64 zero on an empty
// destination. The extract and the MV anti-join both bound their windows
// by it, so the two agree on which rows can be re-served.
func watermarkSql(factsTable string) string {
	return fmt.Sprintf("(SELECT max(%s) FROM %s WHERE has(%s, %d))",
		ColTs, factsTable, ColSymbolLr, vocab.MembKindQueryRun.GetId().Value())
}

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
// Events already captured inside the overlap are dropped here, before the
// LIMIT, by (query_id, event microsecond) against the destination. Without
// that, a window holding batchCap or more events would fill every batch
// with rows the MV then discards: the watermark never advances and capture
// stalls while each refresh reports success. The MV's id anti-join stays
// as the exact backstop; this pre-filter only has to be cheap and never
// drop an uncaptured event (a query has one terminal event, so the pair
// identifies it).
//
// factsTable is the qualified destination ("boxer.facts"); pullURL is
// the endpoint the MV reads, e.g. "http://127.0.0.1:8127/pull";
// batchCap <= 0 applies DefaultBatchCap.
//
// backfillFrom bounds a FIRST-BOOT backfill: with an empty destination
// there is no watermark to be newer than, so the extract otherwise reaches
// the source's whole retention — on a busy server that is a long silent
// catch-up before recent queries appear, bounded only by batchCap per
// refresh. A non-zero backfillFrom starts there instead. The zero value
// keeps the original unbounded behaviour.
//
// It applies ONLY while the destination is empty. Once it holds facts the
// watermark governs, so a restart after downtime still catches up over the
// gap — a floor that applied unconditionally would skip exactly that window,
// which is the property the pipeline exists to guarantee.
func ComposeExtractSql(factsTable string, pullURL string, scope ScopeE, batchCap int, backfillFrom time.Time) (sql string, err error) {
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
		err = eb.Build().Str("scope", string(scope)).Errorf("queryrunfacts: extract not composable for scope")
		return
	}
	// The watermark is read once into a scalar so the emptiness test and the
	// overlap subtraction cannot disagree, and the resulting lower bound lo
	// is shared by the source filter and the already-captured pre-filter. An
	// empty destination yields the DateTime64 zero, which is what selects the
	// floor below.
	floor := "toDateTime64(0, 9, 'UTC')"
	if !backfillFrom.IsZero() {
		floor = fmt.Sprintf("toDateTime64(%d, 9, 'UTC')", backfillFrom.UTC().Unix())
	}
	sql = fmt.Sprintf(`WITH %s AS watermark,
  if(watermark = toDateTime64(0, 9, 'UTC'), %s, watermark - %s) AS lo
SELECT
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
  AND log_comment NOT IN (%s, %s, %s)
  AND position(query, %s) = 0%s
  AND event_time_microseconds >= lo
  AND (query_id, toUnixTimestamp64Micro(event_time_microseconds)) NOT IN (
    SELECT %s, toUnixTimestamp64Micro(%s) FROM %s
    WHERE %s >= lo AND has(%s, %d)
  )
ORDER BY event_time_microseconds
LIMIT %d
SETTINGS output_format_json_quote_64bit_integers=0, log_comment=%s
FORMAT JSONEachRow`,
		watermarkSql(factsTable), floor, WatermarkOverlap,
		QueryTextCap,
		quoteLiteral(ExtractTag), quoteLiteral(RefreshTag), quoteLiteral(ReconcileTag),
		quoteLiteral(pullURL), scopePredicate,
		ColNaturalKey, ColTs, factsTable,
		ColTs, ColSymbolLr, vocab.MembKindQueryRun.GetId().Value(),
		batchCap,
		quoteLiteral(ExtractTag))
	return
}
