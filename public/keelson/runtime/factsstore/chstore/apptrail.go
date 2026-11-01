package chstore

// The app center's cross-run reads (ADR-0260 §SD5): window sessions, the
// newest log rows per app, and the bus's requests folded per subject — over
// every process the trail holds. [Store.LifecyclesByRun] and
// [Store.ListRunEvents] refuse to scan without a run anchor, which is right
// for a per-run timeline; these are the across-runs aggregates, each bounded
// by a look-back and a cap so that an introspection table, which sees no
// predicate, can serve them.
//
// The extraction is hand-written array arithmetic for the reason
// runsessions.go gives: this store ships SQL directly, with no expansion
// pass, so the LW_GET family is not available to it.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	busaudit "github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var _ factsstore.AppTrailReaderI = (*Store)(nil)

// appTrailCap bounds every read. Generous against a trail a person reads,
// and there so a long-lived trail cannot pull an unbounded result into a
// render path.
const appTrailCap = uint32(10000)

// Column names of the sections the three reads draw on, beyond the ones
// runsessions.go and recentlogs.go spell.
const (
	atSymLR     = "`tv:symbol:lr:lr:u64:1247:::0::data`"
	atSymValue  = "`tv:symbol:value:val:s:124::I:0::data`"
	atSymLRCard = "`tv:symbol:lrcard:lrcard:u64:4E:::0::data`"
	atU64Value  = "`tv:u64Array:value:val:u64h:4:::0::data`"
	atU64LR     = "`tv:u64Array:lr:lr:u64:1247:::0::data`"
	atU64LRCard = "`tv:u64Array:lrcard:lrcard:u64:4E:::0::data`"
	atStrValue  = "`tv:stringArray:value:val:sh:4::8:0::data`"
	atStrLen    = "`tv:stringArray:len:len:u64:4D:::0::data`"
	atStrLR     = "`tv:stringArray:lr:lr:u64:1247:::0::data`"
	atStrLRCard = "`tv:stringArray:lrcard:lrcard:u64:4E:::0::data`"
	atIdCol     = "`id:id:u64:47::0:`"
	atTsCol     = "`ts:ts:z64:47::0:`"
)

// appTrailExprs are the attribution projections every row kind shares: the
// app and run from the mixed-low-card symbol lane, the window from the u64
// lane (ADR-0191 §SD3).
type appTrailExprs struct {
	appId       string
	runId       string
	instanceKey string
	tsSec       string
}

func buildAppTrailExprs() (e appTrailExprs) {
	e.appId = pickLcrString(atSymValue, atSymLR, atSymLRCard, vocab.MembRuntimeApp.GetId().Value())
	e.runId = pickLcrString(atSymValue, atSymLR, atSymLRCard, vocab.MembRuntimeRun.GetId().Value())
	e.instanceKey = pickLcrNumeric(atU64Value, atU64LR, atU64LRCard, vocab.MembLifecycleTileKey.GetId().Value(), "0")
	e.tsSec = fmt.Sprintf("toUnixTimestamp(%s)", atTsCol)
	return
}

// appTrailWhere is the kind tag, the app gate and the look-back.
func appTrailWhere(kind uint64, since time.Time) string {
	parts := []string{
		fmt.Sprintf("has(%s, %d)", atSymLR, kind),
		fmt.Sprintf("has(%s, %d)", atSymLR, vocab.MembRuntimeApp.GetId().Value()),
	}
	if !since.IsZero() {
		parts = append(parts, fmt.Sprintf("%s >= toDateTime(%d, 'UTC')", atTsCol, since.Unix()))
	}
	return strings.Join(parts, " AND ")
}

func appTrailLimit(limit uint32) uint32 {
	if limit == 0 || limit > appTrailCap {
		return appTrailCap
	}
	return limit
}

// AppRuns implements [factsstore.AppTrailReaderI].
func (inst *Store) AppRuns(ctx context.Context, filter factsstore.AppTrailFilter) (rows []factsstore.AppRunRow, err error) {
	raw, err := inst.queryAll(ctx, "app runs", composeAppRunsSql(inst.qualifiedTable(), filter))
	if err != nil {
		return
	}
	rows, err = parseAppRuns(raw)
	return
}

// composeAppRunsSql pairs each (run, app, instance)'s `started` and
// `stopped` rows. A group missing either side keeps a zero there, which the
// row type documents; a group with neither cannot occur, since every row in
// it is one of the two. Each session also carries the last heartbeat or
// start record of its process, so a session that never closed can be given
// the latest end the trail supports.
//
// Rows that name no run are left out. They were written before ADR-0191
// stamped the run on every row, and the instance key restarts in every
// process, so (app, instance) alone would fold every such session of an app
// into one that spans the whole look-back.
func composeAppRunsSql(table string, filter factsstore.AppTrailFilter) string {
	e := buildAppTrailExprs()
	l := buildLifecycleColumnExprs()
	seenWhere := []string{fmt.Sprintf("hasAny(%s, [%d, %d])", atSymLR,
		vocab.MembKindRuntimeHeartbeat.GetId().Value(), vocab.MembKindRuntimeRun.GetId().Value())}
	if !filter.Since.IsZero() {
		seenWhere = append(seenWhere, fmt.Sprintf("%s >= toDateTime(%d, 'UTC')", atTsCol, filter.Since.Unix()))
	}
	return fmt.Sprintf(`
SELECT s.run_id, s.app_id, s.instance_key, s.started, s.stopped, s.stop_reason, ifNull(h.seen, 0) AS run_seen
FROM (
  SELECT
    run_id,
    app_id,
    instance_key,
    minIf(ts_sec, phase = 'started') AS started,
    maxIf(ts_sec, phase = 'stopped') AS stopped,
    anyIf(stop_reason, phase = 'stopped') AS stop_reason
  FROM (
    SELECT %s AS run_id, %s AS app_id, %s AS instance_key, %s AS phase, %s AS stop_reason, %s AS ts_sec
    FROM %s
    WHERE %s
  )
  WHERE app_id != '' AND run_id != ''
  GROUP BY run_id, app_id, instance_key
  ORDER BY greatest(started, stopped) DESC, app_id ASC, instance_key ASC
  LIMIT %d
) AS s
LEFT JOIN (
  SELECT %s AS run_id, max(%s) AS seen
  FROM %s
  WHERE %s
  GROUP BY run_id
) AS h ON h.run_id = s.run_id
ORDER BY greatest(s.started, s.stopped) DESC, s.app_id ASC, s.instance_key ASC
SETTINGS join_use_nulls = 1
FORMAT TabSeparated`,
		e.runId, e.appId, e.instanceKey, l.phase, l.stopReason, e.tsSec,
		table,
		appTrailWhere(vocab.MembKindAppLifecycle.GetId().Value(), filter.Since),
		appTrailLimit(filter.Limit),
		e.runId, e.tsSec,
		table,
		strings.Join(seenWhere, " AND "))
}

func parseAppRuns(raw []byte) (rows []factsstore.AppRunRow, err error) {
	rows = []factsstore.AppRunRow{}
	err = eachTsvLine(raw, 7, "app runs", func(p []string) (err error) {
		key, err := strconv.ParseUint(p[2], 10, 64)
		if err != nil {
			return
		}
		started, err := parseUnixOrZero(p[3])
		if err != nil {
			return
		}
		stopped, err := parseUnixOrZero(p[4])
		if err != nil {
			return
		}
		seen, err := parseUnixOrZero(p[6])
		if err != nil {
			return
		}
		rows = append(rows, factsstore.AppRunRow{
			RunId: unescapeTabSeparated(p[0]), AppId: app.AppIdT(unescapeTabSeparated(p[1])), InstanceKey: key,
			StartedAt: started, StoppedAt: stopped, StopReason: unescapeTabSeparated(p[5]), RunSeenAt: seen,
		})
		return
	})
	return
}

// AppLogTail implements [factsstore.AppTrailReaderI].
func (inst *Store) AppLogTail(ctx context.Context, filter factsstore.AppTrailFilter, perApp uint32) (rows []factsstore.AppLogRow, err error) {
	if perApp == 0 {
		err = eh.Errorf("chstore: AppLogTail requires a positive perApp")
		return
	}
	raw, err := inst.queryAll(ctx, "app log tail", composeAppLogTailSql(inst.qualifiedTable(), filter, perApp))
	if err != nil {
		return
	}
	rows, err = parseAppLogTail(raw)
	return
}

// composeAppLogTailSql takes the newest perApp rows of each app with
// `LIMIT n BY`, so a chatty app cannot crowd a quiet one out of the tail.
// Fields and stack are not read (ADR-0260 §SD5).
func composeAppLogTailSql(table string, filter factsstore.AppTrailFilter, perApp uint32) string {
	e := buildAppTrailExprs()
	l := buildColumnExprs()
	return fmt.Sprintf(`
SELECT ts_sec, app_id, instance_key, run_id, level, caller, message, err
FROM (
  SELECT %s AS id, %s AS ts_sec, %s AS app_id, %s AS instance_key, %s AS run_id,
         %s AS level, %s AS caller, %s AS message, %s AS err
  FROM %s
  WHERE %s
)
WHERE app_id != ''
ORDER BY ts_sec DESC, id DESC
LIMIT %d BY app_id
LIMIT %d
FORMAT TabSeparated`,
		atIdCol, e.tsSec, e.appId, e.instanceKey, e.runId,
		l.level, l.caller, l.message, l.errStr,
		table,
		appTrailWhere(vocab.MembKindLog.GetId().Value(), filter.Since),
		perApp, appTrailLimit(filter.Limit))
}

func parseAppLogTail(raw []byte) (rows []factsstore.AppLogRow, err error) {
	rows = []factsstore.AppLogRow{}
	err = eachTsvLine(raw, 8, "app log tail", func(p []string) (err error) {
		ts, err := strconv.ParseInt(p[0], 10, 64)
		if err != nil {
			return
		}
		key, err := strconv.ParseUint(p[2], 10, 64)
		if err != nil {
			return
		}
		rows = append(rows, factsstore.AppLogRow{
			Ts: time.Unix(ts, 0).UTC(), AppId: app.AppIdT(unescapeTabSeparated(p[1])), InstanceKey: key,
			RunId: unescapeTabSeparated(p[3]), Level: unescapeTabSeparated(p[4]), Caller: unescapeTabSeparated(p[5]),
			Message: unescapeTabSeparated(p[6]), Error: unescapeTabSeparated(p[7]),
		})
		return
	})
	return
}

// AppAuditSummary implements [factsstore.AppTrailReaderI].
func (inst *Store) AppAuditSummary(ctx context.Context, filter factsstore.AppTrailFilter) (rows []factsstore.AppAuditRow, err error) {
	raw, err := inst.queryAll(ctx, "app audit summary", composeAppAuditSql(inst.qualifiedTable(), filter))
	if err != nil {
		return
	}
	rows, err = parseAppAudit(raw)
	return
}

// composeAppAuditSql folds the bus's requests per (app, subject, result):
// the audit events of the bus domain (ADR-0296's Update of 2026-10-30),
// the subject read from the event's references, the result its outcome,
// the latency from its attributes. The writer records a latency only when it is
// positive, so an absent one reads as zero and the mean is taken over the
// requests that carry one. Rows of the retired audit kind are not read.
func composeAppAuditSql(table string, filter factsstore.AppTrailFilter) string {
	e := buildAppTrailExprs()
	list := func(m uint64) string { return pickLcrList(atStrValue, atStrLen, atStrLR, atStrLRCard, m) }
	where := appTrailWhere(vocab.MembKindAuditEvent.GetId().Value(), filter.Since) +
		" AND " + symbolEquals(vocab.MembAuditEventDomain.GetId().Value(), busaudit.BusDomain) +
		" AND " + symbolEquals(vocab.MembAuditEventAction.GetId().Value(), busaudit.BusActionRequest)
	attr := func(key string) string {
		return fmt.Sprintf("arrayElement(attr_values, indexOf(attr_keys, %s))", quoteSqlString(key))
	}
	return fmt.Sprintf(`
SELECT
  app_id,
  subject,
  result,
  count() AS requests,
  min(ts_sec) AS first_at,
  max(ts_sec) AS last_at,
  if(countIf(latency > 0) = 0, 0, round(avgIf(latency, latency > 0), 3)) AS mean_latency,
  max(latency) AS max_latency
FROM (
  SELECT
    app_id,
    ts_sec,
    arrayElement(ref_values, indexOf(ref_types, %s)) AS subject,
    result,
    toUInt32OrZero(%s) AS latency
  FROM (
    SELECT %s AS app_id, %s AS ts_sec, %s AS result, %s AS ref_types, %s AS ref_values, %s AS attr_keys, %s AS attr_values
    FROM %s
    WHERE %s
  )
)
WHERE app_id != ''
GROUP BY app_id, subject, result
ORDER BY requests DESC, app_id ASC, subject ASC, result ASC
LIMIT %d
FORMAT TabSeparated`,
		quoteSqlString(busaudit.BusRefSubject), attr(busaudit.BusAttrLatencyMs),
		e.appId, e.tsSec, pickLcrString(atSymValue, atSymLR, atSymLRCard, vocab.MembAuditEventOutcome.GetId().Value()),
		list(vocab.MembAuditEventRefTypes.GetId().Value()), list(vocab.MembAuditEventRefValues.GetId().Value()),
		list(vocab.MembAuditEventAttrKeys.GetId().Value()), list(vocab.MembAuditEventAttrValues.GetId().Value()),
		table, where,
		appTrailLimit(filter.Limit))
}

// pickLcrList is the values of the attribute under membershipId on an
// array-valued section, read on the plain channel: the attribute that owns
// the membership's position in the lr lane (what LW_RAGGED_PARENT_IDS
// computes over lrcard), then its run of the flattened value array, located
// by the per-attribute len column (what LW_LIST_BY_TAG_EQUAL does); [] when
// the row has no such attribute.
func pickLcrList(valueArr, lenArr, lrArr, lrCardArr string, membershipId uint64) (expr string) {
	attr := fmt.Sprintf("arrayElement(arrayFlatten(arrayMap((i, c) -> arrayWithConstant(c, i), arrayEnumerate(%[1]s), %[1]s)), indexOf(%[2]s, %[3]d))",
		lrCardArr, lrArr, membershipId)
	return fmt.Sprintf("if(%[1]s > 0, arraySlice(%[2]s, arraySum(arraySlice(%[3]s, 1, %[1]s - 1)) + 1, arrayElement(%[3]s, %[1]s)), [])",
		attr, valueArr, lenArr)
}

func parseAppAudit(raw []byte) (rows []factsstore.AppAuditRow, err error) {
	rows = []factsstore.AppAuditRow{}
	err = eachTsvLine(raw, 8, "app audit summary", func(p []string) (err error) {
		n, err := strconv.ParseUint(p[3], 10, 64)
		if err != nil {
			return
		}
		first, err := parseUnixOrZero(p[4])
		if err != nil {
			return
		}
		last, err := parseUnixOrZero(p[5])
		if err != nil {
			return
		}
		mean, err := strconv.ParseFloat(p[6], 64)
		if err != nil {
			return
		}
		mx, err := strconv.ParseUint(p[7], 10, 32)
		if err != nil {
			return
		}
		rows = append(rows, factsstore.AppAuditRow{
			AppId: app.AppIdT(unescapeTabSeparated(p[0])), Subject: unescapeTabSeparated(p[1]), Result: unescapeTabSeparated(p[2]),
			Requests: n, FirstAt: first, LastAt: last, MeanLatencyMs: mean, MaxLatencyMs: uint32(mx),
		})
		return
	})
	return
}

// queryAll runs sql and reads the whole body.
func (inst *Store) queryAll(ctx context.Context, what string, sql string) (raw []byte, err error) {
	body, err := inst.cli.Query(ctx, sql)
	if err != nil {
		err = eb.Build().Str("read", what).Errorf("chstore: app trail query: %w", err)
		return
	}
	defer func() { _ = body.Close() }()
	raw, err = io.ReadAll(body)
	if err != nil {
		err = eb.Build().Str("read", what).Errorf("chstore: app trail body: %w", err)
	}
	return
}

// eachTsvLine splits a TabSeparated body into lines of exactly cols fields.
// A malformed line fails the whole read, as parseAppLaunchStats does: a
// table silently missing rows is worse than one that says it could not be
// read.
func eachTsvLine(raw []byte, cols int, what string, fn func(parts []string) error) (err error) {
	text := strings.TrimRight(string(raw), "\n")
	if text == "" {
		return
	}
	for line := range strings.SplitSeq(text, "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != cols {
			return eb.Build().Str("read", what).Int("got", len(parts)).Int("want", cols).Str("line", line).Errorf("chstore: app trail: wrong column count")
		}
		if err = fn(parts); err != nil {
			return eb.Build().Str("read", what).Str("line", line).Errorf("chstore: app trail parse: %w", err)
		}
	}
	return
}

// parseUnixOrZero reads unix seconds; 0 is the zero time, not the epoch,
// because the aggregates above write 0 for "no such row".
func parseUnixOrZero(s string) (t time.Time, err error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v == 0 {
		return
	}
	t = time.Unix(v, 0).UTC()
	return
}

// LogRowsFilter selects the log rows [Store.LogRows] returns. Every field
// but Limit is optional; the zero value of each is "no constraint". The
// read is bounded by Since and by Limit, as the other reads here are, so a
// caller that sets neither still gets at most appTrailCap rows.
type LogRowsFilter struct {
	// Since and Until bound the row's timestamp: Since inclusive, Until
	// exclusive.
	Since time.Time
	Until time.Time
	// Levels keeps rows whose level is one of these; empty keeps every
	// row, including level-less ones.
	Levels []string
	// AppContains and MessageContains are case-insensitive substrings of
	// the app id and of the message.
	AppContains     string
	MessageContains string
	// RunId keeps the rows stamped with this run (ADR-0191 §SD3).
	RunId string
	// InstanceKey keeps the rows of this window (ADR-0191 §SD4); 0 keeps
	// every row.
	InstanceKey uint64
	Limit       uint32
}

// LogRowRead is one log row as [Store.LogRows] reads it back: the row the
// writer was handed, less the structured error chain, which the writer does
// not persist (only its flat summary in Error), plus the run the row was
// stamped with.
type LogRowRead struct {
	Row   factsstore.LogRow
	RunId string
}

// LogRows returns the log rows matching filter, newest first, with their
// envelope, their attribution and their typed fields: what the log viewer
// shows of a row. Every filter is applied in the query, so Limit counts
// matching rows.
func (inst *Store) LogRows(ctx context.Context, filter LogRowsFilter) (rows []LogRowRead, err error) {
	raw, err := inst.queryAll(ctx, "log rows", composeLogRowsSql(inst.qualifiedTable(), filter))
	if err != nil {
		return
	}
	rows, err = parseLogRows(raw)
	return
}

// Columns the log-row read draws on beyond the ones above. Each field kind
// has its own section; the field's name rides the mixed channel (lmr, with
// the name as the high-card parameter in mrhp), per encodeLogEntity.
const (
	lrTextValue  = "`tv:textArray:value:val:sh:5::7:0::data`"
	lrTextLen    = "`tv:textArray:len:len:u64:4D:::0::data`"
	lrTextLR     = "`tv:textArray:lr:lr:u64:1247:::0::data`"
	lrTextLRCard = "`tv:textArray:lrcard:lrcard:u64:4E:::0::data`"
	lrU64Len     = "`tv:u64Array:len:len:u64:4D:::0::data`"
)

// logFieldSection is one section a log field may land in: its value
// column, its per-attribute len column (empty for a scalar section), and
// how a value is rendered as text.
type logFieldSection struct {
	kind   string
	name   string
	value  string
	scalar bool
	render func(v string) string
}

var logFieldSections = []logFieldSection{
	{kind: "string", name: "stringArray", value: "`tv:stringArray:value:val:sh:4::8:0::data`", render: func(v string) string { return v }},
	{kind: "int", name: "i64Array", value: "`tv:i64Array:value:val:i64h:4:::0::data`", render: toStringExpr},
	{kind: "uint", name: "u64Array", value: "`tv:u64Array:value:val:u64h:4:::0::data`", render: toStringExpr},
	{kind: "float", name: "f64Array", value: "`tv:f64Array:value:val:f64h:4A:::0::data`", render: toStringExpr},
	{kind: "bool", name: "bool", value: "`tv:bool:value:val:b:4:::0::data`", scalar: true, render: toStringExpr},
	{kind: "bytes", name: "blobArray", value: "`tv:blobArray:value:val:yh:4:::0::data`", render: func(v string) string { return "base64Encode(" + v + ")" }},
	{kind: "time", name: "timeArray", value: "`tv:timeArray:value:val:z64h:4:::0::data`", render: func(v string) string { return "toString(toUnixTimestamp64Nano(" + v + "))" }},
}

func toStringExpr(v string) string { return "toString(" + v + ")" }

// ownerOf maps each position of a membership lane (lr or lmr) to the
// 1-based attribute that carries it, by expanding the per-attribute card
// column: the same arithmetic as LW_RAGGED_PARENT_IDS. Unlike the
// cumulative-sum lookup in pickLcrString, it stays right when one
// attribute carries more than one membership on the lane.
func ownerOf(cardArr string) string {
	return fmt.Sprintf("arrayFlatten(arrayMap((i, c) -> arrayWithConstant(c, i), arrayEnumerate(%[1]s), %[1]s))", cardArr)
}

// pickScalarAttr is the value of the attribute that carries membershipId on
// the plain lane of a scalar section (one value per attribute); zero when
// the row has none.
func pickScalarAttr(valueArr, lrArr, lrCardArr string, membershipId uint64, zero string) string {
	idx := fmt.Sprintf("indexOf(%s, %d)", lrArr, membershipId)
	return fmt.Sprintf("if(%[1]s > 0, arrayElement(%[2]s, arrayElement(%[3]s, %[1]s)), %[4]s)",
		idx, valueArr, ownerOf(lrCardArr), zero)
}

// logFieldsExpr is every MembLogField attribute of a row as [kind, name,
// value-as-text] triples, section by section. Attributes carrying other
// mixed memberships are left out, so slots a writer adds beside the fields
// do not show up as fields.
func logFieldsExpr() string {
	membId := vocab.MembLogField.GetId().Value()
	parts := make([]string, 0, len(logFieldSections))
	for _, s := range logFieldSections {
		lmr := fmt.Sprintf("`tv:%s:lmr:lmr:u64:1247:::0::data`", s.name)
		mrhp := fmt.Sprintf("`tv:%s:mrhp:mrhp:y:4:::0::data`", s.name)
		lmrCard := fmt.Sprintf("`tv:%s:lmrcard:lmrcard:u64:4E:::0::data`", s.name)
		attr := fmt.Sprintf("arrayElement(%s, j)", ownerOf(lmrCard))
		var pos string
		if s.scalar {
			pos = attr
		} else {
			lenArr := fmt.Sprintf("`tv:%s:len:len:u64:4D:::0::data`", s.name)
			pos = fmt.Sprintf("arraySum(arraySlice(%[1]s, 1, %[2]s - 1)) + 1", lenArr, attr)
		}
		value := s.render(fmt.Sprintf("arrayElement(%s, %s)", s.value, pos))
		parts = append(parts, fmt.Sprintf("arrayMap(j -> [%s, toString(arrayElement(%s, j)), %s], arrayFilter(j -> arrayElement(%s, j) = %d, arrayEnumerate(%s)))",
			quoteSqlLiteral(s.kind), mrhp, value, lmr, membId, lmr))
	}
	return "arrayConcat(" + strings.Join(parts, ",\n    ") + ")"
}

// quoteSqlLiteral quotes s as a ClickHouse string literal. Backslash is
// the server's escape character, so it is escaped before the quote; a
// filter typed by a person or passed by an agent cannot close the literal.
func quoteSqlLiteral(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
}

// composeLogRowsSql reads whole log rows newest first. Filters reference
// the projected aliases, so a filter on the level and the projection agree
// on what the level is; the ORDER BY is the table's sorting key, so a
// LIMIT reads in order rather than sorting the look-back.
func composeLogRowsSql(table string, filter LogRowsFilter) string {
	sym := func(m uint64) string { return pickScalarAttr(atSymValue, atSymLR, atSymLRCard, m, "''") }
	str := func(m uint64) string {
		return "arrayElement(" + pickLcrList(atStrValue, atStrLen, atStrLR, atStrLRCard, m) + ", 1)"
	}
	where := []string{fmt.Sprintf("has(%s, %d)", atSymLR, vocab.MembKindLog.GetId().Value())}
	if !filter.Since.IsZero() {
		where = append(where, fmt.Sprintf("%s >= fromUnixTimestamp64Nano(%d, 'UTC')", atTsCol, filter.Since.UnixNano()))
	}
	if !filter.Until.IsZero() {
		where = append(where, fmt.Sprintf("%s < fromUnixTimestamp64Nano(%d, 'UTC')", atTsCol, filter.Until.UnixNano()))
	}
	if len(filter.Levels) > 0 {
		q := make([]string, 0, len(filter.Levels))
		for _, l := range filter.Levels {
			q = append(q, quoteSqlLiteral(l))
		}
		where = append(where, "level IN ("+strings.Join(q, ", ")+")")
	}
	if filter.AppContains != "" {
		where = append(where, "positionCaseInsensitiveUTF8(app_id, "+quoteSqlLiteral(filter.AppContains)+") > 0")
	}
	if filter.MessageContains != "" {
		where = append(where, "positionCaseInsensitiveUTF8(message, "+quoteSqlLiteral(filter.MessageContains)+") > 0")
	}
	if filter.RunId != "" {
		where = append(where, "run_id = "+quoteSqlLiteral(filter.RunId))
	}
	if filter.InstanceKey != 0 {
		where = append(where, fmt.Sprintf("instance_key = %s", quoteSqlLiteral(strconv.FormatUint(filter.InstanceKey, 10))))
	}
	return fmt.Sprintf(`
SELECT
  toString(%s) AS id,
  toString(toUnixTimestamp64Nano(%s)) AS ts_ns,
  %s AS app_id,
  %s AS run_id,
  toString(arrayElement(%s, 1)) AS instance_key,
  %s AS level,
  %s AS caller,
  %s AS service,
  %s AS message,
  %s AS err,
  arrayElement(%s, 1) AS stack,
  %s AS fields
FROM %s
WHERE %s
ORDER BY %s DESC, %s DESC
LIMIT %d
FORMAT JSONEachRow`,
		atIdCol, atTsCol,
		sym(vocab.MembRuntimeApp.GetId().Value()),
		sym(vocab.MembRuntimeRun.GetId().Value()),
		pickLcrList(atU64Value, lrU64Len, atU64LR, atU64LRCard, vocab.MembLifecycleTileKey.GetId().Value()),
		sym(vocab.MembLogLevel.GetId().Value()),
		sym(vocab.MembLogCaller.GetId().Value()),
		sym(vocab.MembLogService.GetId().Value()),
		str(vocab.MembLogMessage.GetId().Value()),
		str(vocab.MembLogError.GetId().Value()),
		pickLcrList(lrTextValue, lrTextLen, lrTextLR, lrTextLRCard, vocab.MembLogStack.GetId().Value()),
		logFieldsExpr(),
		table,
		strings.Join(where, " AND "),
		atTsCol, atIdCol,
		appTrailLimit(filter.Limit))
}

// logRowJson is one line of composeLogRowsSql's output. Integers arrive as
// strings, which the query writes explicitly so the server's 64-bit
// quoting setting does not decide the shape.
type logRowJson struct {
	Id          string     `json:"id"`
	TsNs        string     `json:"ts_ns"`
	AppId       string     `json:"app_id"`
	RunId       string     `json:"run_id"`
	InstanceKey string     `json:"instance_key"`
	Level       string     `json:"level"`
	Caller      string     `json:"caller"`
	Service     string     `json:"service"`
	Message     string     `json:"message"`
	Err         string     `json:"err"`
	Stack       string     `json:"stack"`
	Fields      [][]string `json:"fields"`
}

func parseLogRows(raw []byte) (rows []LogRowRead, err error) {
	rows = []LogRowRead{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		var j logRowJson
		if err = dec.Decode(&j); err == io.EOF {
			err = nil
			return
		} else if err != nil {
			err = eh.Errorf("chstore: log rows: decode: %w", err)
			return
		}
		var r LogRowRead
		if r, err = j.row(); err != nil {
			return
		}
		rows = append(rows, r)
	}
}

func (inst logRowJson) row() (out LogRowRead, err error) {
	ns, err := strconv.ParseInt(inst.TsNs, 10, 64)
	if err != nil {
		err = eb.Build().Str("ts", inst.TsNs).Errorf("chstore: log rows: parse ts: %w", err)
		return
	}
	key, err := strconv.ParseUint(inst.InstanceKey, 10, 64)
	if err != nil {
		err = eb.Build().Str("instance", inst.InstanceKey).Errorf("chstore: log rows: parse instance key: %w", err)
		return
	}
	out = LogRowRead{RunId: inst.RunId, Row: factsstore.LogRow{
		Ts: time.Unix(0, ns).UTC(), AppId: app.AppIdT(inst.AppId), InstanceKey: key,
		Level: inst.Level, Caller: inst.Caller, Service: inst.Service,
		Message: inst.Message, Error: inst.Err, Stack: inst.Stack,
	}}
	for _, f := range inst.Fields {
		if len(f) != 3 {
			err = eb.Build().Int("got", len(f)).Errorf("chstore: log rows: a field is not a [kind, name, value] triple")
			return
		}
		var lf factsstore.LogField
		if lf, err = parseLogField(f[0], f[1], f[2]); err != nil {
			return
		}
		out.Row.Fields = append(out.Row.Fields, lf)
	}
	return
}

// parseLogField turns one rendered field back into its typed form: the
// inverse of the renderers in logFieldSections.
func parseLogField(kind, name, v string) (f factsstore.LogField, err error) {
	f.Name = name
	switch kind {
	case "string":
		f.Kind, f.Str = factsstore.LogFieldKindString, v
	case "int":
		f.Kind = factsstore.LogFieldKindInt
		f.Int, err = strconv.ParseInt(v, 10, 64)
	case "uint":
		f.Kind = factsstore.LogFieldKindUint
		f.Uint, err = strconv.ParseUint(v, 10, 64)
	case "float":
		f.Kind = factsstore.LogFieldKindFloat
		f.Float, err = strconv.ParseFloat(v, 64)
	case "bool":
		f.Kind = factsstore.LogFieldKindBool
		f.Bool, err = strconv.ParseBool(v)
	case "bytes":
		f.Kind = factsstore.LogFieldKindBytes
		f.Bytes, err = base64.StdEncoding.DecodeString(v)
	case "time":
		f.Kind = factsstore.LogFieldKindTime
		var ns int64
		ns, err = strconv.ParseInt(v, 10, 64)
		f.Time = time.Unix(0, ns).UTC()
	default:
		err = eb.Build().Str("kind", kind).Errorf("chstore: log rows: unknown field kind")
	}
	if err != nil {
		err = eb.Build().Str("field", name).Str("kind", kind).Errorf("chstore: log rows: parse field: %w", err)
	}
	return
}
