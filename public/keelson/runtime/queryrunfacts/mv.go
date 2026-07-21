package queryrunfacts

import (
	"fmt"
	"strings"

	factsddl "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ddl"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// MvBaseName is the unqualified name of the refreshable materialized
// view; the reconciler qualifies it with the destination's database.
const MvBaseName = "mv_queryruns"

// AntiJoinWindow is the recent-destination window the MV body anti-joins
// (ADR-0115 SD2): url() read amplification and watermark overlap re-serve
// rows; any re-served id already appended within this window is dropped
// by the MV, so duplicates need adversarial timing beyond a day to land.
const AntiJoinWindow = "INTERVAL 1 DAY"

// ddlColumns parses the generated boxer.facts DDL column block into
// (name, ClickHouse type) pairs, codecs stripped. The parse is strict so
// a DDL-shape change fails loudly here instead of misdeclaring the wire.
func ddlColumns() (cols [][2]string, err error) {
	lines := strings.Split(factsddl.ColumnsSQL, "\n")
	cols = make([][2]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSuffix(strings.TrimSpace(line), ",")
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, `"`) {
			err = eh.Errorf("queryrunfacts: ddl column line without leading quote: %q", line)
			return
		}
		name, rest, found := strings.Cut(line[1:], `"`)
		if !found {
			err = eh.Errorf("queryrunfacts: ddl column line without closing quote: %q", line)
			return
		}
		chType, _, found := strings.Cut(strings.TrimSpace(rest), " CODEC(")
		if !found {
			err = eh.Errorf("queryrunfacts: ddl column %q without CODEC clause: %q", name, line)
			return
		}
		chType = strings.TrimSpace(chType)
		if chType == "" {
			err = eh.Errorf("queryrunfacts: ddl column %q with empty type: %q", name, line)
			return
		}
		cols = append(cols, [2]string{name, chType})
	}
	if len(cols) == 0 {
		err = eh.Errorf("queryrunfacts: ddl column block parsed to zero columns")
		return
	}
	return
}

// DdlColumnNames returns the wire-column names of the current facts
// schema — the reconciler compares them against a live destination to
// detect an older schema generation before the pipeline references a
// column that is not there.
func DdlColumnNames() (names []string, err error) {
	cols, err := ddlColumns()
	if err != nil {
		return
	}
	names = make([]string, len(cols))
	for i, col := range cols {
		names[i] = col[0]
	}
	return
}

// UrlStructure derives the url() structure clause from the generated
// boxer.facts DDL — every leeway wire column with its ClickHouse type,
// names backtick-quoted: "`id:id:u64:2k:0:0:` UInt64, …". Deriving
// (rather than duplicating) the list keeps the pipeline DDL moving with
// the schema.
func UrlStructure() (structure string, err error) {
	cols, err := ddlColumns()
	if err != nil {
		return
	}
	parts := make([]string, len(cols))
	for i, col := range cols {
		parts[i] = "`" + col[0] + "` " + col[1]
	}
	structure = strings.Join(parts, ", ")
	return
}

// ComposeMvSql builds the refreshable materialized view that drives the
// pipeline (ADR-0115 SD2): ClickHouse owns the schedule and the write —
// every cadenceSeconds it GETs pullURL as ArrowStream and appends any
// row whose deterministic id is not already in the recent destination
// window. The SETTINGS clause tags the refresh queries so the extract
// can exclude them (see RefreshTag).
//
// mvName and factsTable are qualified ("boxer.mv_queryruns",
// "boxer.facts") so tests can point the pipeline at a scratch
// database.
func ComposeMvSql(mvName string, factsTable string, pullURL string, cadenceSeconds int) (sql string, err error) {
	if mvName == "" || factsTable == "" || pullURL == "" {
		err = eh.Errorf("queryrunfacts: mv needs mvName + factsTable + pullURL")
		return
	}
	if cadenceSeconds < 1 {
		err = eh.Errorf("queryrunfacts: mv cadence %d < 1s", cadenceSeconds)
		return
	}
	var structure string
	structure, err = UrlStructure()
	if err != nil {
		return
	}
	// The url() structure instantiates the leeway column types, whose
	// LowCardinality(UInt64) members sit behind the same suspicious-type
	// gate the facts DDL unlocks (factsddl.SettingsClause) — the setting
	// rides the view's stored SELECT so both CREATE and every refresh
	// carry it. http_max_tries=1 disables url()'s intra-query HTTP retry
	// backoff: under pull-shape the NEXT refresh is the retry, and a
	// dead endpoint must fail the tick fast instead of holding the
	// refresh slot through a backoff loop (which blocks the boot
	// reconciler's DROP for that long).
	sql = fmt.Sprintf(`CREATE MATERIALIZED VIEW IF NOT EXISTS %s
REFRESH EVERY %d SECOND APPEND TO %s
AS SELECT * FROM url(%s, 'ArrowStream', %s)
WHERE %s NOT IN (
  SELECT %s FROM %s
  WHERE %s > now64(9) - %s AND has(%s, %d)
)
SETTINGS log_comment=%s, %s, http_max_tries=1`,
		mvName,
		cadenceSeconds, factsTable,
		quoteSqlString(pullURL), quoteSqlString(structure),
		ColId,
		ColId, factsTable,
		ColTs, AntiJoinWindow, ColSymbolLr, vocab.MembKindQueryRun.GetId().Value(),
		quoteSqlString(RefreshTag), factsddl.SettingsClause)
	return
}

// ComposeDropMvSql removes the view. The reconciler drops and recreates
// unconditionally at boot — cheaper and drift-proof compared to
// normalizing create_table_query for comparison; the refresh schedule
// simply restarts (DROP TABLE works on materialized views everywhere).
func ComposeDropMvSql(mvName string) (sql string, err error) {
	if mvName == "" {
		err = eh.Errorf("queryrunfacts: drop needs mvName")
		return
	}
	sql = "DROP TABLE IF EXISTS " + mvName
	return
}
