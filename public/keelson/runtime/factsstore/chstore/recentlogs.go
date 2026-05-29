//go:build llm_generated_opus47

package chstore

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

// LogFilter narrows the rows returned by RecentLogs. Every field is
// optional; zero values mean "no constraint on this dimension". Limit
// is hard-capped server-side at recentLogsCap to keep one query from
// pulling the full retention window.
type LogFilter struct {
	AppId app.AppIdT
	Since time.Time
	Until time.Time
	Level string
	Limit uint32
}

// recentLogsCap is the per-query upper bound. Generous enough for an
// operator scrolling tail logs; small enough that a misuse won't pull
// a GB of rows in one shot. Tune up if real use cases demand it.
const recentLogsCap = uint32(1000)

// RecentLogs returns rows tagged KindLog matching the filter, newest
// first. The envelope columns (level, message, caller, error, stack,
// service, app_id, ts) are reconstructed from the typed membership
// sections via ClickHouse's arrayFirst higher-order function — no
// reliance on the WriteLog positional ordering. The Fields slice is
// not populated by this method; per-field reconstruction is a v2
// feature once a concrete consumer asks for it.
//
// Returns an empty slice (not nil) when no rows match. The SQL filter
// applies the limit cap regardless of the caller-supplied Limit.
func (inst *Store) RecentLogs(ctx context.Context, filter LogFilter) (rows []factsstore.LogRow, err error) {
	limit := filter.Limit
	if limit == 0 || limit > recentLogsCap {
		limit = recentLogsCap
	}
	sql := composeRecentLogsSql(inst.qualifiedTable(), filter, limit)
	body, err := inst.cli.Query(ctx, sql)
	if err != nil {
		err = eh.Errorf("chstore: recent logs query: %w", err)
		return
	}
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		err = eh.Errorf("chstore: recent logs read: %w", err)
		return
	}
	rows, err = parseRecentLogsRows(raw)
	if err != nil {
		err = eh.Errorf("chstore: recent logs parse: %w", err)
		return
	}
	return
}

// recentLogsColumnExprs gathers the eight envelope projections. Kept
// in one place so the column ORDER matches parseRecentLogsRows below
// — adding or reordering an expression here means updating both.
type recentLogsColumnExprs struct {
	id      string
	tsSec    string
	appId   string
	level   string
	caller  string
	service string
	message string
	errStr  string
	stack   string
}

func buildColumnExprs() (e recentLogsColumnExprs) {
	const (
		symLR      = "`tv:symbol:lr:lr:u64:2q:0:0:0::data`"
		symLMR     = "`tv:symbol:lmr:lmr:u64:2q:0:0:0::data`"
		symMRHP    = "`tv:symbol:mrhp:mrhp:y:g:0:0:0::data`"
		symValue   = "`tv:symbol:value:val:s:m:0:24:0::data`"
		symLRCard  = "`tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::data`"
		strLR      = "`tv:stringArray:lr:lr:u64:2q:0:0:0::data`"
		strValue   = "`tv:stringArray:value:val:sh:g:0:0:0::data`"
		strLRCard  = "`tv:stringArray:lrcard:lrcard:u64:4gw:0:0:0::data`"
		textLR     = "`tv:textArray:lr:lr:u64:2q:0:0:0::data`"
		textValue  = "`tv:textArray:value:val:sh:g:0:0:0::data`"
		textLRCard = "`tv:textArray:lrcard:lrcard:u64:4gw:0:0:0::data`"
		idCol      = "`id:id:u64:2k:0:0:`"
		tsCol      = "`ts:ts:z64:2k:0:0:`"
	)
	e.id = idCol
	// `ts` is z64 — emitted by the leeway DDL pipeline as
	// DateTime64(9,'UTC') (nano precision). We coerce to unix-seconds
	// so the Go side can re-hydrate via time.Unix without the
	// timezone-decode ambiguity TabSeparated otherwise has. Sub-second
	// precision is preserved on the wire but discarded here by
	// design — recentlogs renders human-readable timestamps; callers
	// needing per-event ordering should fall back to the `id` column.
	e.tsSec = fmt.Sprintf("toUnixTimestamp(%s)", tsCol)
	// AppId is a Mixed-Low-Card-Ref membership: the mrhp array (high-
	// card parameter) is parallel to lmr (same length, same order), so
	// arrayFirst zips cleanly.
	e.appId = fmt.Sprintf("arrayFirst((p, m) -> m = %d, %s, %s)",
		vocab.MembRuntimeApp.GetId().Value(), symMRHP, symLMR)
	// Envelope LCR fields: value[] is parallel to *card[] (one entry
	// per attribute), but lr[] is a SUBSET array (only LCR attrs). We
	// map an LCR id → position in lr → position in value via cumulative
	// sums on lrcard.
	e.level = pickLcrString(symValue, symLR, symLRCard, vocab.MembLogLevel.GetId().Value())
	e.caller = pickLcrString(symValue, symLR, symLRCard, vocab.MembLogCaller.GetId().Value())
	e.service = pickLcrString(symValue, symLR, symLRCard, vocab.MembLogService.GetId().Value())
	e.message = pickLcrString(strValue, strLR, strLRCard, vocab.MembLogMessage.GetId().Value())
	e.errStr = pickLcrString(strValue, strLR, strLRCard, vocab.MembLogError.GetId().Value())
	e.stack = pickLcrString(textValue, textLR, textLRCard, vocab.MembLogStack.GetId().Value())
	return
}

// pickLcrString emits the expression that returns the value carried by
// the attribute tagged with low-card-ref `membershipId`. The leeway
// encoding is non-trivial: `value` has one entry per attribute, but
// `lr` only contains the LCR membership ids — different sizes (any
// attribute can also be HCR/LMR-tagged). The `lrcard` array is
// parallel to `value` and counts LCR memberships at each position; the
// running prefix-sum of lrcard yields the lr-index for each value
// position. Looking up our membership id in lr gives an lr-index;
// finding that index in the cumulative-sum array yields the value
// position. The whole pipeline is wrapped in `if(indexOf > 0, …, '')`
// because `indexOf(lr, missing) = 0` would otherwise lead arrayElement
// to misbehave on edge cases.
func pickLcrString(valueArr, lrArr, lrCardArr string, membershipId uint64) (expr string) {
	idxInLr := fmt.Sprintf("indexOf(%s, %d)", lrArr, membershipId)
	expr = fmt.Sprintf("if(%s > 0, arrayElement(%s, indexOf(arrayCumSum(%s), %s)), '')",
		idxInLr, valueArr, lrCardArr, idxInLr)
	return
}

func composeRecentLogsSql(table string, filter LogFilter, limit uint32) (sql string) {
	e := buildColumnExprs()
	const (
		symLR  = "`tv:symbol:lr:lr:u64:2q:0:0:0::data`"
		symLMR = "`tv:symbol:lmr:lmr:u64:2q:0:0:0::data`"
		tsCol  = "`ts:ts:z64:2k:0:0:`"
	)
	whereParts := []string{
		fmt.Sprintf("has(%s, %d)", symLR, vocab.MembKindLog.GetId().Value()),
	}
	if filter.AppId != "" {
		// has(lmr, MembRuntimeApp.id) gates rows that carry SOME app
		// mixed-membership; the value comparison restricts to ours.
		whereParts = append(whereParts,
			fmt.Sprintf("has(%s, %d)", symLMR, vocab.MembRuntimeApp.GetId().Value()),
			fmt.Sprintf("(%s) = %s", e.appId, quoteSqlString(string(filter.AppId))))
	}
	if !filter.Since.IsZero() {
		whereParts = append(whereParts,
			fmt.Sprintf("%s >= toDateTime(%d, 'UTC')", tsCol, filter.Since.Unix()))
	}
	if !filter.Until.IsZero() {
		whereParts = append(whereParts,
			fmt.Sprintf("%s < toDateTime(%d, 'UTC')", tsCol, filter.Until.Unix()))
	}
	if filter.Level != "" {
		whereParts = append(whereParts,
			fmt.Sprintf("(%s) = %s", e.level, quoteSqlString(filter.Level)))
	}
	sql = fmt.Sprintf(`
SELECT
  %s AS id,
  %s AS ts_sec,
  %s AS app_id,
  %s AS level,
  %s AS caller,
  %s AS service,
  %s AS message,
  %s AS err,
  %s AS stack
FROM %s
WHERE %s
ORDER BY %s DESC
LIMIT %d
FORMAT TabSeparated`,
		e.id, e.tsSec, e.appId, e.level, e.caller, e.service,
		e.message, e.errStr, e.stack,
		table,
		strings.Join(whereParts, " AND "),
		tsCol,
		limit)
	return
}

// parseRecentLogsRows decodes the TabSeparated payload into LogRow
// instances. The column order MUST match composeRecentLogsSql; tests
// guard the mapping. TabSeparated escaping: ClickHouse uses
// backslash-escapes for tab / newline / backslash inside string
// fields, so we undo those before assigning. Empty string fields land
// as "" on LogRow, which is the documented "absent" sentinel for the
// envelope members.
func parseRecentLogsRows(raw []byte) (rows []factsstore.LogRow, err error) {
	rows = []factsstore.LogRow{}
	if len(raw) == 0 {
		return
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 9 {
			err = eh.Errorf("chstore: recent logs: expected 9 columns, got %d (line=%q)", len(parts), line)
			return
		}
		// parts[0] = id (uint64) — not currently surfaced on LogRow;
		// kept in the SELECT for future ordering / pagination work.
		_, perr := strconv.ParseUint(parts[0], 10, 64)
		if perr != nil {
			err = eh.Errorf("chstore: recent logs: parse id %q: %w", parts[0], perr)
			return
		}
		tsSec, perr := strconv.ParseInt(parts[1], 10, 64)
		if perr != nil {
			err = eh.Errorf("chstore: recent logs: parse ts %q: %w", parts[1], perr)
			return
		}
		row := factsstore.LogRow{
			Ts:      time.Unix(tsSec, 0).UTC(),
			AppId:   app.AppIdT(unescapeTabSeparated(parts[2])),
			Level:   unescapeTabSeparated(parts[3]),
			Caller:  unescapeTabSeparated(parts[4]),
			Service: unescapeTabSeparated(parts[5]),
			Message: unescapeTabSeparated(parts[6]),
			Error:   unescapeTabSeparated(parts[7]),
			Stack:   unescapeTabSeparated(parts[8]),
		}
		rows = append(rows, row)
	}
	return
}

// unescapeTabSeparated reverses ClickHouse's TabSeparated string
// escaping: \\ → backslash, \t → tab, \n → newline, \0 → NUL. Other
// backslash-escapes pass through unchanged so unknown sequences don't
// corrupt the data silently.
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
