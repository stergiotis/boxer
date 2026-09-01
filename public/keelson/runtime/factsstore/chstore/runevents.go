package chstore

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// runevents.go reads the trail back as rows (ADR-0191 §SD7): one flattened
// record per recorded event, so a consumer can render a run's history without
// holding the membership vocabulary.
//
// It exists because the alternative was worse in a measured way. The
// event-timeline applet started as a SQL buffer doing this extraction
// client-side, and at ~7 KB of statement the pre-execute pass pipeline —
// which hands the statement between passes as text, and so re-parses it once
// per pass — cost 2.4–3.9 s per Run against a server answering in 90 ms.
// BenchmarkPlayPipeline (nanopass_test) keeps that buffer as its fixture.
// Composed here the SQL is built once, in Go, and ships raw.
//
// The extraction is hand-written array arithmetic rather than the LW_GET
// surface AGENTS.md prefers, for the reason runsessions.go gives beside it:
// this store ships SQL directly, with no client-side expansion pass, so the
// LW_GET family is not available to it — and the server-side read-back UDFs
// it would expand into are not something the store may assume is installed.

// runEventsCap bounds one read, mirroring recentLogsCap. A run's trail is
// bounded by its own length, which for a long-lived process with heartbeats
// every 30 s is a few thousand rows a day; the cap keeps a month-long run
// from pulling its whole history into a render path.
const runEventsCap = uint32(20000)

// runEventKind pairs a kind's membership with the label the trail shows.
// The writers' own labels are not usable: they spell the twelve kinds three
// ways ("runtime-run", "columnWidth", "audit"), which is a wire detail rather
// than a vocabulary anyone chose.
type runEventKind struct {
	memb  uint64
	label string
}

// runEventKinds is the set this view renders, in id order. A kind absent
// here is a kind the trail does not show — which is a deliberate, visible
// omission rather than a silent one, since the guard below is built from
// exactly this list.
func runEventKinds() (out []runEventKind) {
	out = []runEventKind{
		{vocab.MembKindGrant.GetId().Value(), "grant"},
		{vocab.MembKindAudit.GetId().Value(), "audit"},
		{vocab.MembKindState.GetId().Value(), "persist"},
		{vocab.MembKindEvent.GetId().Value(), "event"},
		{vocab.MembKindLog.GetId().Value(), "log"},
		{vocab.MembKindRuntimeRun.GetId().Value(), "run start"},
		{vocab.MembKindRuntimeHeartbeat.GetId().Value(), "heartbeat"},
		{vocab.MembKindAppLifecycle.GetId().Value(), "lifecycle"},
		{vocab.MembKindQueryRun.GetId().Value(), "query run"},
		{vocab.MembKindLaunch.GetId().Value(), "launch"},
		{vocab.MembKindWorkingset.GetId().Value(), "workingset"},
		{vocab.MembKindColumnWidth.GetId().Value(), "column width"},
	}
	return
}

var _ factsstore.RunEventReaderI = (*Store)(nil)

// ListRunEvents returns the trail of one run, oldest first: every row naming
// that run, plus — when filter.Since is set — rows naming no run at all that
// landed after it started.
//
// The second half is the compromise ADR-0191 leaves behind rather than
// hides. Grant, audit, log and column-width rows written before that decision
// carry no run id, so the only thing that can place them is their timestamp,
// and a second boxer process overlapping this one lands its rows here too.
// Rows written since carry the run and are selected by name.
func (inst *Store) ListRunEvents(filter factsstore.RunEventFilter) (rows []factsstore.RunEventRow, err error) {
	if filter.RunId == "" {
		err = eh.Errorf("chstore: ListRunEvents requires a non-empty RunId")
		return
	}
	limit := filter.Limit
	if limit == 0 || limit > runEventsCap {
		limit = runEventsCap
	}
	sql := composeRunEventsSql(inst.qualifiedTable(), filter, limit)
	body, qerr := inst.cli.Query(context.Background(), sql)
	if qerr != nil {
		err = eh.Errorf("chstore: run events query: %w", qerr)
		return
	}
	defer body.Close()
	raw, rerr := io.ReadAll(body)
	if rerr != nil {
		err = eh.Errorf("chstore: run events read: %w", rerr)
		return
	}
	rows, err = parseRunEventRows(raw)
	if err != nil {
		err = eh.Errorf("chstore: run events parse: %w", err)
	}
	return
}

func composeRunEventsSql(table string, filter factsstore.RunEventFilter, limit uint32) (sql string) {
	const (
		symValue  = "`tv:symbol:value:val:s:124::I:0::data`"
		symLR     = "`tv:symbol:lr:lr:u64:1247:::0::data`"
		symLMR    = "`tv:symbol:lmr:lmr:u64:1247:::0::data`"
		symMRHP   = "`tv:symbol:mrhp:mrhp:y:4:::0::data`"
		strValue  = "`tv:stringArray:value:val:sh:4::8:0::data`"
		u64Value  = "`tv:u64Array:value:val:u64h:4:::0::data`"
		u64LR     = "`tv:u64Array:lr:lr:u64:1247:::0::data`"
		u64LRCard = "`tv:u64Array:lrcard:lrcard:u64:4E:::0::data`"
		idCol     = "`id:id:u64:47::0:`"
		tsCol     = "`ts:ts:z64:47::0:`"
	)
	kinds := runEventKinds()
	ids := make([]string, 0, len(kinds))
	labels := make([]string, 0, len(kinds))
	for _, k := range kinds {
		ids = append(ids, strconv.FormatUint(k.memb, 10))
		labels = append(labels, quoteSqlString(k.label))
	}
	idArray := "[" + strings.Join(ids, ",") + "]"
	labelArray := "[" + strings.Join(labels, ",") + "]"

	appExpr := fmt.Sprintf("arrayFirst((p, m) -> m = %d, %s, %s)",
		vocab.MembRuntimeApp.GetId().Value(), symMRHP, symLMR)
	runExpr := fmt.Sprintf("arrayFirst((p, m) -> m = %d, %s, %s)",
		vocab.MembRuntimeRun.GetId().Value(), symMRHP, symLMR)
	instanceExpr := pickLcrNumeric(u64Value, u64LR, u64LRCard,
		vocab.MembLifecycleTileKey.GetId().Value(), "0")
	// A row carries exactly one kind, so intersecting the kind lane with the
	// twelve ids yields one id and transform() names it.
	kindExpr := fmt.Sprintf("transform(arrayElement(arrayIntersect(%s, %s), 1), %s, %s, '?')",
		symLR, idArray, idArray, labelArray)
	// What the row says, in written order. Every writer opens the symbol
	// section with its kind label, so element 1 is that; the app and run are
	// already their own columns and drop out. The string section carries the
	// free-text half (log message, stop reason).
	detailExpr := fmt.Sprintf(
		"arrayStringConcat(arrayConcat(arrayFilter(v -> v != %s AND v != %s, arraySlice(%s, 2)), %s), ' · ')",
		appExpr, runExpr, symValue, strValue)

	// The cheap necessary guard first: hasAny over the kind lane prunes the
	// granules holding other vocabularies' rows (sysmetrics, capmap) through
	// the skip index, which indexOf and countEqual never do.
	where := []string{fmt.Sprintf("hasAny(%s, %s)", symLR, idArray)}
	attribution := runExpr + " = " + quoteSqlString(filter.RunId)
	if !filter.Since.IsZero() {
		attribution = fmt.Sprintf("(%s OR (%s = '' AND %s >= toDateTime64(%s, 9, 'UTC')))",
			attribution, runExpr, tsCol,
			quoteSqlString(filter.Since.UTC().Format("2006-01-02 15:04:05.000000000")))
	}
	where = append(where, attribution)

	sql = fmt.Sprintf(`
SELECT
  toString(%s) AS fact_id,
  toUnixTimestamp64Milli(%s) AS ts_ms,
  %s AS kind,
  %s AS app_id,
  %s AS instance_key,
  %s AS run_id,
  %s AS detail
FROM %s
WHERE %s
ORDER BY %s ASC, %s ASC
LIMIT %d
FORMAT TabSeparated`,
		idCol, tsCol, kindExpr, appExpr, instanceExpr, runExpr, detailExpr,
		table, strings.Join(where, " AND "), tsCol, idCol, limit)
	return
}

func parseRunEventRows(raw []byte) (rows []factsstore.RunEventRow, err error) {
	rows = []factsstore.RunEventRow{}
	if len(raw) == 0 {
		return
	}
	for line := range strings.SplitSeq(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 7 {
			err = eb.Build().Int("got", len(parts)).Str("line", line).Errorf("chstore: run events: expected 7 columns")
			return
		}
		tsMs, perr := strconv.ParseInt(parts[1], 10, 64)
		if perr != nil {
			err = eb.Build().Str("ts", parts[1]).Errorf("chstore: run events: parse ts: %w", perr)
			return
		}
		instance, perr := strconv.ParseUint(parts[4], 10, 64)
		if perr != nil {
			err = eb.Build().Str("instanceKey", parts[4]).Errorf("chstore: run events: parse instance_key: %w", perr)
			return
		}
		rows = append(rows, factsstore.RunEventRow{
			Ts:          time.UnixMilli(tsMs).UTC(),
			Kind:        unescapeTabSeparated(parts[2]),
			AppId:       app.AppIdT(unescapeTabSeparated(parts[3])),
			InstanceKey: instance,
			RunId:       unescapeTabSeparated(parts[5]),
			Detail:      unescapeTabSeparated(parts[6]),
			Source:      factsstore.RunEventSourceFacts,
			FactId:      unescapeTabSeparated(parts[0]),
		})
	}
	return
}
