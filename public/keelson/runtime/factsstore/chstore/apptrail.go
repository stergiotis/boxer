package chstore

// The app center's cross-run reads (ADR-0260 §SD5): window sessions, the
// newest log rows per app, and the bus's requests folded per subject — over
// every process the trail holds. [Store.ListRunEvents] refuses to scan without a run anchor, which is right
// for a per-run timeline; these are the across-runs aggregates, each bounded
// by a look-back and a cap so that an introspection table, which sees no
// predicate, can serve them.
//
// Each is authored against the read surface and expanded before it ships
// (authored.go).

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	busaudit "github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/factsread"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var _ factsstore.AppTrailReaderI = (*Store)(nil)
var _ factsstore.LogReaderI = (*Store)(nil)

// appTrailCap bounds every read. Generous against a trail a person reads,
// and there so a long-lived trail cannot pull an unbounded result into a
// render path.
const appTrailCap = uint32(10000)

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
	e.appId = sym(vocab.MembRuntimeApp)
	e.runId = sym(vocab.MembRuntimeRun)
	e.instanceKey = u64(vocab.MembLifecycleTileKey)
	e.tsSec = fmt.Sprintf("toUnixTimestamp(%s)", hTs)
	return
}

// appTrailWhere is the kind tag, the app gate and the look-back.
func appTrailWhere(kind uint64, since time.Time, b binds) string {
	parts := []string{
		fmt.Sprintf("has(%s, %d)", hSymLr, kind),
		fmt.Sprintf("has(%s, %d)", hSymLr, vocab.MembRuntimeApp.GetId().Value()),
	}
	if !since.IsZero() {
		parts = append(parts, fmt.Sprintf("%s >= toDateTime(%s, 'UTC')", hTs, b.i64("since", since.Unix())))
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
	rows = []factsstore.AppRunRow{}
	b := binds{}
	err = inst.readRows(ctx, "app runs", composeAppRunsSql(inst.qualifiedTable(), filter, b), b, 7, func(r row) {
		rows = append(rows, factsstore.AppRunRow{
			RunId: r.str(0), AppId: app.AppIdT(r.str(1)), InstanceKey: r.u64(2),
			StartedAt: r.unix(3), StoppedAt: r.unix(4), StopReason: r.str(5), RunSeenAt: r.unix(6),
		})
	})
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
func composeAppRunsSql(table string, filter factsstore.AppTrailFilter, b binds) string {
	e := buildAppTrailExprs()
	l := buildLifecycleColumnExprs()
	seenWhere := []string{fmt.Sprintf("hasAny(%s, [%d, %d])", hSymLr,
		vocab.MembKindRuntimeHeartbeat.GetId().Value(), vocab.MembKindRuntimeRun.GetId().Value())}
	if !filter.Since.IsZero() {
		seenWhere = append(seenWhere, fmt.Sprintf("%s >= toDateTime(%s, 'UTC')", hTs, b.i64("since", filter.Since.Unix())))
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
SETTINGS join_use_nulls = 1`,
		e.runId, e.appId, e.instanceKey, l.phase, l.stopReason, e.tsSec,
		table,
		appTrailWhere(vocab.MembKindAppLifecycle.GetId().Value(), filter.Since, b),
		appTrailLimit(filter.Limit),
		e.runId, e.tsSec,
		table,
		strings.Join(seenWhere, " AND "))
}

// AppLogTail implements [factsstore.AppTrailReaderI].
func (inst *Store) AppLogTail(ctx context.Context, filter factsstore.AppTrailFilter, perApp uint32) (rows []factsstore.AppLogRow, err error) {
	if perApp == 0 {
		err = eh.Errorf("chstore: AppLogTail requires a positive perApp")
		return
	}
	rows = []factsstore.AppLogRow{}
	b := binds{}
	err = inst.readRows(ctx, "app log tail", composeAppLogTailSql(inst.qualifiedTable(), filter, perApp, b), b, 8, func(r row) {
		rows = append(rows, factsstore.AppLogRow{
			Ts: time.Unix(r.i64(0), 0).UTC(), AppId: app.AppIdT(r.str(1)), InstanceKey: r.u64(2),
			RunId: r.str(3), Level: r.str(4), Caller: r.str(5), Message: r.str(6), Error: r.str(7),
		})
	})
	return
}

// composeAppLogTailSql takes the newest perApp rows of each app with
// `LIMIT n BY`, so a chatty app cannot crowd a quiet one out of the tail.
// Fields and stack are not read (ADR-0260 §SD5).
func composeAppLogTailSql(table string, filter factsstore.AppTrailFilter, perApp uint32, b binds) string {
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
LIMIT %d`,
		hId, e.tsSec, e.appId, e.instanceKey, e.runId,
		l.level, l.caller, l.message, l.errStr,
		table,
		appTrailWhere(vocab.MembKindLog.GetId().Value(), filter.Since, b),
		perApp, appTrailLimit(filter.Limit))
}

// AppAuditSummary implements [factsstore.AppTrailReaderI].
func (inst *Store) AppAuditSummary(ctx context.Context, filter factsstore.AppTrailFilter) (rows []factsstore.AppAuditRow, err error) {
	rows = []factsstore.AppAuditRow{}
	b := binds{}
	err = inst.readRows(ctx, "app audit summary", composeAppAuditSql(inst.qualifiedTable(), filter, b), b, 8, func(r row) {
		rows = append(rows, factsstore.AppAuditRow{
			AppId: app.AppIdT(r.str(0)), Subject: r.str(1), Result: r.str(2),
			Requests: r.u64(3), FirstAt: r.unix(4), LastAt: r.unix(5),
			MeanLatencyMs: r.f64(6), MaxLatencyMs: uint32(r.u64(7)),
		})
	})
	return
}

// composeAppAuditSql folds the bus's requests per (app, subject, result):
// the audit events of the bus domain (ADR-0296's Update of 2026-10-30),
// the subject read from the event's references, the result its outcome,
// the latency from its attributes. The writer records a latency only when it is
// positive, so an absent one reads as zero and the mean is taken over the
// requests that carry one. Rows of the retired audit kind are not read.
func composeAppAuditSql(table string, filter factsstore.AppTrailFilter, b binds) string {
	e := buildAppTrailExprs()
	where := appTrailWhere(vocab.MembKindAuditEvent.GetId().Value(), filter.Since, b) +
		" AND " + sym(vocab.MembAuditEventDomain) + " = " + quoteSqlString(busaudit.BusDomain) +
		" AND " + sym(vocab.MembAuditEventAction) + " = " + quoteSqlString(busaudit.BusActionRequest)
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
LIMIT %d`,
		quoteSqlString(busaudit.BusRefSubject), attr(busaudit.BusAttrLatencyMs),
		e.appId, e.tsSec, sym(vocab.MembAuditEventOutcome),
		strList(vocab.MembAuditEventRefTypes), strList(vocab.MembAuditEventRefValues),
		strList(vocab.MembAuditEventAttrKeys), strList(vocab.MembAuditEventAttrValues),
		table, where,
		appTrailLimit(filter.Limit))
}

// LogRows returns the log rows matching filter, newest first, with their
// envelope, their attribution and their typed fields: what the log viewer
// shows of a row. Every filter is applied in the query, so Limit counts
// matching rows.
func (inst *Store) LogRows(ctx context.Context, filter factsstore.LogRowsFilter) (rows []factsstore.LogRowRead, err error) {
	rows = []factsstore.LogRowRead{}
	var ferr error
	b := binds{}
	err = inst.readRows(ctx, "log rows", composeLogRowsSql(inst.qualifiedTable(), filter, b), b, 11, func(r row) {
		out := factsstore.LogRowRead{RunId: r.str(2), Row: factsstore.LogRow{
			Ts: time.Unix(0, r.i64(0)).UTC(), AppId: app.AppIdT(r.str(1)), InstanceKey: r.u64(3),
			Level: r.str(4), Caller: r.str(5), Service: r.str(6),
			Message: r.str(7), Error: r.str(8), Stack: r.str(9),
		}}
		for _, f := range r.stringLists(10) {
			if len(f) != 3 {
				if ferr == nil {
					ferr = eb.Build().Int("got", len(f)).Errorf("chstore: log rows: a field is not a [kind, name, value] triple")
				}
				return
			}
			lf, perr := parseLogField(f[0], f[1], f[2])
			if perr != nil {
				if ferr == nil {
					ferr = perr
				}
				return
			}
			out.Row.Fields = append(out.Row.Fields, lf)
		}
		rows = append(rows, out)
	})
	if err == nil {
		err = ferr
	}
	return
}

// logFieldSection is one section a log field may land in, and how a value
// of it is rendered as text. Each field kind has its own section; the
// field's name rides the mixed channel as the high-cardinality parameter,
// per encodeLogEntity.
type logFieldSection struct {
	kind   string
	name   string
	scalar bool
	render func(v string) string
}

var logFieldSections = []logFieldSection{
	{kind: "string", name: "stringArray", render: func(v string) string { return v }},
	{kind: "int", name: "i64Array", render: toStringExpr},
	{kind: "uint", name: "u64Array", render: toStringExpr},
	{kind: "float", name: "f64Array", render: toStringExpr},
	{kind: "bool", name: "bool", scalar: true, render: toStringExpr},
	{kind: "bytes", name: "blobArray", render: func(v string) string { return "base64Encode(" + v + ")" }},
	{kind: "time", name: "timeArray", render: func(v string) string { return "toString(toUnixTimestamp64Nano(" + v + "))" }},
}

func toStringExpr(v string) string { return "toString(" + v + ")" }

// logFieldsExpr is every MembLogField attribute of a row as [kind, name,
// value-as-text] triples, section by section. Attributes carrying other
// mixed memberships are left out, so slots a writer adds beside the fields
// do not show up as fields.
func logFieldsExpr() string {
	field := factsread.MembName(vocab.MembLogField)
	parts := make([]string, 0, len(logFieldSections))
	for _, s := range logFieldSections {
		// The two selectors are co-indexed: the positions the membership
		// occupies on the mixed lane, and the attribute each belongs to.
		names := fmt.Sprintf("LW_CO_GATHER(`%s:mrhp`, LW_SEL('%s', '%s', '%s'))", s.name, s.name, field, factsread.MixedChannel)
		attrs := fmt.Sprintf("LW_SEL_ATTRS('%s', '%s', '%s')", s.name, field, factsread.MixedChannel)
		var values string
		if s.scalar {
			values = fmt.Sprintf("LW_CO_GATHER(`%s:value`, %s)", s.name, attrs)
		} else {
			// The value lane is flattened across attributes, so an
			// attribute index is not a value index.
			values = fmt.Sprintf("arrayMap(a -> LW_RAGGED_ELEM(`%[1]s:value`, `%[1]s:len`, a, 1), %[2]s)", s.name, attrs)
		}
		parts = append(parts, fmt.Sprintf("arrayMap((n, v) -> [%s, toString(n), %s], %s, %s)",
			quoteSqlLiteral(s.kind), s.render("v"), names, values))
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
func composeLogRowsSql(table string, filter factsstore.LogRowsFilter, b binds) string {
	where := []string{fmt.Sprintf("has(%s, %d)", hSymLr, vocab.MembKindLog.GetId().Value())}
	if !filter.Since.IsZero() {
		where = append(where, fmt.Sprintf("%s >= fromUnixTimestamp64Nano(%s, 'UTC')", hTs, b.i64("since", filter.Since.UnixNano())))
	}
	if !filter.Until.IsZero() {
		where = append(where, fmt.Sprintf("%s < fromUnixTimestamp64Nano(%s, 'UTC')", hTs, b.i64("until", filter.Until.UnixNano())))
	}
	if len(filter.Levels) > 0 {
		q := make([]string, 0, len(filter.Levels))
		for _, l := range filter.Levels {
			q = append(q, quoteSqlLiteral(l))
		}
		where = append(where, "level IN ("+strings.Join(q, ", ")+")")
	}
	if filter.AppContains != "" {
		where = append(where, "positionCaseInsensitiveUTF8(app_id, "+b.str("app", filter.AppContains)+") > 0")
	}
	if filter.MessageContains != "" {
		where = append(where, "positionCaseInsensitiveUTF8(message, "+b.str("message", filter.MessageContains)+") > 0")
	}
	if filter.RunId != "" {
		where = append(where, "run_id = "+b.str("run", filter.RunId))
	}
	if filter.InstanceKey != 0 {
		where = append(where, "instance_key = "+b.u64("instance", filter.InstanceKey))
	}
	return fmt.Sprintf(`
SELECT
  toUnixTimestamp64Nano(%s) AS ts_ns,
  %s AS app_id,
  %s AS run_id,
  %s AS instance_key,
  %s AS level,
  %s AS caller,
  %s AS service,
  %s AS message,
  %s AS err,
  %s AS stack,
  %s AS fields
FROM %s
WHERE %s
ORDER BY %s DESC, %s DESC
LIMIT %d`,
		hTs,
		sym(vocab.MembRuntimeApp),
		sym(vocab.MembRuntimeRun),
		u64(vocab.MembLifecycleTileKey),
		sym(vocab.MembLogLevel),
		sym(vocab.MembLogCaller),
		sym(vocab.MembLogService),
		str(vocab.MembLogMessage),
		str(vocab.MembLogError),
		text(vocab.MembLogStack),
		logFieldsExpr(),
		table,
		strings.Join(where, " AND "),
		hTs, hId,
		appTrailLimit(filter.Limit))
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
