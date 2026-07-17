package queryrunfacts

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// readback.go is the S2 read side: the history SELECT that pivots
// KindQueryRun facts back into flat rows (play's History tab consumes
// it through whatever ClickHouse endpoint the user is on), and the
// per-run ProfileEvents drill-down composed as an ordinary query — the
// detail pane hands that one to the editor instead of rendering a
// second table, so the drill-down is itself visible, editable SQL.
//
// The membership algebra mirrors chstore's RecentLogs: LCR values are
// located by mapping the membership's lr-position through
// arrayCumSum(lrcard) to an attribute position. Indexing the value
// array by attribute position additionally relies on every QueryRun
// attribute being single-element (EncodeEntity uses
// BeginAttributeSingle throughout), which keeps the flat value array
// parallel to the attribute axis; the mixed-tagged ProfileEvents
// attributes carry lrcard=0 and so never shift the LCR mapping.

// HistoryRow is one captured run, flat. Zero values mean the fact did
// not carry the attribute (absent-when-zero counters, unstamped
// identity).
type HistoryRow struct {
	Id             uint64
	Ts             time.Time
	QueryId        string
	Event          string
	Kind           string
	Lane           string
	App            string
	RunId          string
	DurationMs     uint64
	ReadRows       uint64
	ReadBytes      uint64
	WrittenRows    uint64
	WrittenBytes   uint64
	ResultRows     uint64
	ResultBytes    uint64
	MemoryPeak     uint64
	NormalizedHash uint64
	ExceptionCode  int64
	Exception      string
	QueryText      string
}

// historyRowColumns is the SELECT-list arity ParseHistoryRows expects;
// compose and parse must move together.
const historyRowColumns = 20

// HistoryLimitCap bounds one history read the same way RecentLogs caps
// its window — an operator pane, not an export path.
const HistoryLimitCap = 500

// Wire names of the section columns the pivots touch (the symbol and
// string constants complement ColId/ColTs/ColSymbolLr in extract.go).
const (
	colSymbolValue  = "`tv:symbol:value:val:s:m:0:24:0::data`"
	colSymbolLrCard = "`tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::data`"
	colSymbolLmr    = "`tv:symbol:lmr:lmr:u64:2q:0:0:0::data`"
	colSymbolMrhp   = "`tv:symbol:mrhp:mrhp:y:g:0:0:0::data`"
	colStringValue  = "`tv:stringArray:value:val:sh:g:0:0:0::data`"
	colStringLr     = "`tv:stringArray:lr:lr:u64:2q:0:0:0::data`"
	colStringLrCard = "`tv:stringArray:lrcard:lrcard:u64:4gw:0:0:0::data`"
	colU64Value     = "`tv:u64Array:value:val:u64h:g:0:0:0::data`"
	colU64Lr        = "`tv:u64Array:lr:lr:u64:2q:0:0:0::data`"
	colU64LrCard    = "`tv:u64Array:lrcard:lrcard:u64:4gw:0:0:0::data`"
	colU64Lmr       = "`tv:u64Array:lmr:lmr:u64:2q:0:0:0::data`"
	colU64Mrhp      = "`tv:u64Array:mrhp:mrhp:y:g:0:0:0::data`"
	colU64LmrCard   = "`tv:u64Array:lmrcard:lmrcard:u64:4gw:0:0:0::data`"
	colI64Value     = "`tv:i64Array:value:val:i64h:g:0:0:0::data`"
	colI64Lr        = "`tv:i64Array:lr:lr:u64:2q:0:0:0::data`"
	colI64LrCard    = "`tv:i64Array:lrcard:lrcard:u64:4gw:0:0:0::data`"
	colNaturalKey   = "`id:naturalKey:y:g:0:0:`"
)

// pickLcr maps membershipId's lr-position to an attribute position via
// the cumulative lrcard sum and returns the value there, or def when
// the row does not carry the membership (indexOf = 0 would otherwise
// make arrayElement misbehave). See the package comment for why value
// indexing by attribute position is valid here.
func pickLcr(valueArr, lrArr, lrCardArr string, membershipId uint64, def string) (expr string) {
	idxInLr := fmt.Sprintf("indexOf(%s, %d)", lrArr, membershipId)
	expr = fmt.Sprintf("if(%s > 0, arrayElement(%s, indexOf(arrayCumSum(%s), %s)), %s)",
		idxInLr, valueArr, lrCardArr, idxInLr, def)
	return
}

// pickMixedFirst returns the high-card parameter of the first
// mixed-membership occurrence (mrhp is parallel to lmr).
func pickMixedFirst(mrhpArr, lmrArr string, membershipId uint64) (expr string) {
	expr = fmt.Sprintf("arrayFirst((p, m) -> m = %d, %s, %s)", membershipId, mrhpArr, lmrArr)
	return
}

// ComposeHistorySql builds the newest-first history SELECT over
// factsTable. limit is clamped to [1, HistoryLimitCap]; zero applies
// the cap. Column order is the ParseHistoryRows contract.
func ComposeHistorySql(factsTable string, limit int) (sql string, err error) {
	if factsTable == "" {
		err = eh.Errorf("queryrunfacts: history needs factsTable")
		return
	}
	if limit <= 0 || limit > HistoryLimitCap {
		limit = HistoryLimitCap
	}
	symbol := func(m uint64) string { return pickLcr(colSymbolValue, ColSymbolLr, colSymbolLrCard, m, "''") }
	str := func(m uint64) string { return pickLcr(colStringValue, colStringLr, colStringLrCard, m, "''") }
	u64 := func(m uint64) string { return pickLcr(colU64Value, colU64Lr, colU64LrCard, m, "0") }
	sql = fmt.Sprintf(`SELECT
  %s AS id,
  toUnixTimestamp(%s) AS ts_sec,
  %s AS query_id,
  %s AS event,
  %s AS kind,
  %s AS lane,
  %s AS app,
  %s AS run_id,
  %s AS duration_ms,
  %s AS read_rows,
  %s AS read_bytes,
  %s AS written_rows,
  %s AS written_bytes,
  %s AS result_rows,
  %s AS result_bytes,
  %s AS memory_peak,
  %s AS normalized_hash,
  %s AS exception_code,
  %s AS exception,
  %s AS query_text
FROM %s
WHERE has(%s, %d)
ORDER BY %s DESC, id
LIMIT %d
FORMAT TabSeparated`,
		ColId,
		ColTs,
		colNaturalKey,
		symbol(vocab.MembQueryRunEventType.GetId().Value()),
		symbol(vocab.MembQueryRunQueryKind.GetId().Value()),
		symbol(vocab.MembQueryRunLane.GetId().Value()),
		pickMixedFirst(colSymbolMrhp, colSymbolLmr, vocab.MembRuntimeApp.GetId().Value()),
		pickMixedFirst(colSymbolMrhp, colSymbolLmr, vocab.MembRuntimeRun.GetId().Value()),
		u64(vocab.MembQueryRunDurationMs.GetId().Value()),
		u64(vocab.MembQueryRunReadRows.GetId().Value()),
		u64(vocab.MembQueryRunReadBytes.GetId().Value()),
		u64(vocab.MembQueryRunWrittenRows.GetId().Value()),
		u64(vocab.MembQueryRunWrittenBytes.GetId().Value()),
		u64(vocab.MembQueryRunResultRows.GetId().Value()),
		u64(vocab.MembQueryRunResultBytes.GetId().Value()),
		u64(vocab.MembQueryRunMemoryPeakBytes.GetId().Value()),
		u64(vocab.MembQueryRunNormalizedHash.GetId().Value()),
		pickLcr(colI64Value, colI64Lr, colI64LrCard, vocab.MembQueryRunExceptionCode.GetId().Value(), "0"),
		str(vocab.MembQueryRunExceptionText.GetId().Value()),
		str(vocab.MembQueryRunQueryText.GetId().Value()),
		factsTable,
		ColSymbolLr, vocab.MembKindQueryRun.GetId().Value(),
		ColTs,
		limit)
	return
}

// ParseHistoryRows decodes the TabSeparated history payload. Column
// order and arity are ComposeHistorySql's contract; a mismatch is an
// error, not a skip, so drift fails loudly.
func ParseHistoryRows(raw []byte) (rows []HistoryRow, err error) {
	rows = []HistoryRow{}
	if len(raw) == 0 {
		return
	}
	for line := range strings.SplitSeq(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != historyRowColumns {
			err = eh.Errorf("queryrunfacts: history row: expected %d columns, got %d (line=%q)", historyRowColumns, len(parts), line)
			return
		}
		var row HistoryRow
		u := func(i int) (v uint64) {
			if err != nil {
				return
			}
			v, perr := strconv.ParseUint(parts[i], 10, 64)
			if perr != nil {
				err = eh.Errorf("queryrunfacts: history row: column %d %q: %w", i, parts[i], perr)
			}
			return v
		}
		row.Id = u(0)
		tsSec := u(1)
		row.DurationMs = u(8)
		row.ReadRows = u(9)
		row.ReadBytes = u(10)
		row.WrittenRows = u(11)
		row.WrittenBytes = u(12)
		row.ResultRows = u(13)
		row.ResultBytes = u(14)
		row.MemoryPeak = u(15)
		row.NormalizedHash = u(16)
		if err != nil {
			return
		}
		excCode, perr := strconv.ParseInt(parts[17], 10, 64)
		if perr != nil {
			err = eh.Errorf("queryrunfacts: history row: exception code %q: %w", parts[17], perr)
			return
		}
		row.Ts = time.Unix(int64(tsSec), 0).UTC()
		row.QueryId = unescapeTabSeparated(parts[2])
		row.Event = unescapeTabSeparated(parts[3])
		row.Kind = unescapeTabSeparated(parts[4])
		row.Lane = unescapeTabSeparated(parts[5])
		row.App = unescapeTabSeparated(parts[6])
		row.RunId = unescapeTabSeparated(parts[7])
		row.ExceptionCode = excCode
		row.Exception = unescapeTabSeparated(parts[18])
		row.QueryText = unescapeTabSeparated(parts[19])
		rows = append(rows, row)
	}
	return
}

// ComposeProfileEventsSql is the per-run drill-down, written as an
// ordinary user-visible query: the ProfileEvents attributes are the
// u64-section entries carrying the mixed ProfileEvent membership, so
// names come from the mrhp/lmr pair and counts from the value entries
// whose attribute carries a mixed tag (lmrcard > 0) — parallel filters
// over the same attribute axis, zipped. Intended for the History
// detail's "profile as query" affordance (ADR-0115 S2); the deep tier
// stays one more query away by design.
func ComposeProfileEventsSql(factsTable string, factId uint64) (sql string, err error) {
	if factsTable == "" {
		err = eh.Errorf("queryrunfacts: profile drill-down needs factsTable")
		return
	}
	sql = fmt.Sprintf(`SELECT
  pe.1 AS event,
  pe.2 AS count
FROM %s
ARRAY JOIN arrayZip(
  arrayFilter((p, m) -> m = %d, %s, %s),
  arrayFilter((v, c) -> c > 0, %s, %s)
) AS pe
WHERE %s = %d
ORDER BY count DESC`,
		factsTable,
		vocab.MembQueryRunProfileEvent.GetId().Value(), colU64Mrhp, colU64Lmr,
		colU64Value, colU64LmrCard,
		ColId, factId)
	return
}

// unescapeTabSeparated reverses ClickHouse's TabSeparated string
// escaping (the chstore recentlogs convention): \\, \t, \n, \0; other
// escapes pass through unchanged.
func unescapeTabSeparated(s string) (out string) {
	if !strings.ContainsRune(s, '\\') {
		out = s
		return
	}
	b := strings.Builder{}
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		switch s[i+1] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '\\':
			b.WriteByte('\\')
		case '0':
			b.WriteByte(0)
		default:
			b.WriteByte(s[i])
			b.WriteByte(s[i+1])
		}
		i++
	}
	out = b.String()
	return
}
