package chstore

// The app center's cross-run reads (ADR-0260 §SD5): window sessions, the
// newest log rows per app, and audited requests folded per subject — over
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

var _ factsstore.AppTrailReaderI = (*Store)(nil)

// appTrailCap bounds every read. Generous against a trail a person reads,
// and there so a long-lived trail cannot pull an unbounded result into a
// render path.
const appTrailCap = uint32(10000)

// Column names of the sections the three reads draw on, beyond the ones
// runsessions.go and recentlogs.go spell.
const (
	atSymLR     = "`tv:symbol:lr:lr:u64:1247:::0::data`"
	atSymLMR    = "`tv:symbol:lmr:lmr:u64:1247:::0::data`"
	atSymMRHP   = "`tv:symbol:mrhp:mrhp:y:4:::0::data`"
	atSymValue  = "`tv:symbol:value:val:s:124::I:0::data`"
	atSymLRCard = "`tv:symbol:lrcard:lrcard:u64:4E:::0::data`"
	atU64Value  = "`tv:u64Array:value:val:u64h:4:::0::data`"
	atU64LR     = "`tv:u64Array:lr:lr:u64:1247:::0::data`"
	atU64LRCard = "`tv:u64Array:lrcard:lrcard:u64:4E:::0::data`"
	atU32Value  = "`tv:u32Array:value:val:u32h:4:::0::data`"
	atU32LR     = "`tv:u32Array:lr:lr:u64:1247:::0::data`"
	atU32LRCard = "`tv:u32Array:lrcard:lrcard:u64:4E:::0::data`"
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
	e.appId = fmt.Sprintf("arrayFirst((p, m) -> m = %d, %s, %s)",
		vocab.MembRuntimeApp.GetId().Value(), atSymMRHP, atSymLMR)
	e.runId = fmt.Sprintf("arrayFirst((p, m) -> m = %d, %s, %s)",
		vocab.MembRuntimeRun.GetId().Value(), atSymMRHP, atSymLMR)
	e.instanceKey = pickLcrNumeric(atU64Value, atU64LR, atU64LRCard, vocab.MembLifecycleTileKey.GetId().Value(), "0")
	e.tsSec = fmt.Sprintf("toUnixTimestamp(%s)", atTsCol)
	return
}

// appTrailWhere is the kind tag, the app gate and the look-back.
func appTrailWhere(kind uint64, since time.Time) string {
	parts := []string{
		fmt.Sprintf("has(%s, %d)", atSymLR, kind),
		fmt.Sprintf("has(%s, %d)", atSymLMR, vocab.MembRuntimeApp.GetId().Value()),
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
// it is one of the two.
func composeAppRunsSql(table string, filter factsstore.AppTrailFilter) string {
	e := buildAppTrailExprs()
	l := buildLifecycleColumnExprs()
	return fmt.Sprintf(`
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
WHERE app_id != ''
GROUP BY run_id, app_id, instance_key
ORDER BY greatest(started, stopped) DESC, app_id ASC, instance_key ASC
LIMIT %d
FORMAT TabSeparated`,
		e.runId, e.appId, e.instanceKey, l.phase, l.stopReason, e.tsSec,
		table,
		appTrailWhere(vocab.MembKindAppLifecycle.GetId().Value(), filter.Since),
		appTrailLimit(filter.Limit))
}

func parseAppRuns(raw []byte) (rows []factsstore.AppRunRow, err error) {
	rows = []factsstore.AppRunRow{}
	err = eachTsvLine(raw, 6, "app runs", func(p []string) (err error) {
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
		rows = append(rows, factsstore.AppRunRow{
			RunId: unescapeTabSeparated(p[0]), AppId: app.AppIdT(unescapeTabSeparated(p[1])), InstanceKey: key,
			StartedAt: started, StoppedAt: stopped, StopReason: unescapeTabSeparated(p[5]),
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

// composeAppAuditSql folds audit rows per (app, subject, result). The
// writer records a latency only when it is positive, so a zero here is an
// absent value and the mean is taken over the requests that carry one.
func composeAppAuditSql(table string, filter factsstore.AppTrailFilter) string {
	e := buildAppTrailExprs()
	subject := pickLcrString(atSymValue, atSymLR, atSymLRCard, vocab.MembAuditRequestSubject.GetId().Value())
	result := pickLcrString(atSymValue, atSymLR, atSymLRCard, vocab.MembAuditResult.GetId().Value())
	latency := pickLcrNumeric(atU32Value, atU32LR, atU32LRCard, vocab.MembAuditLatencyMs.GetId().Value(), "0")
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
  SELECT %s AS app_id, %s AS subject, %s AS result, %s AS latency, %s AS ts_sec
  FROM %s
  WHERE %s
)
WHERE app_id != ''
GROUP BY app_id, subject, result
ORDER BY requests DESC, app_id ASC, subject ASC, result ASC
LIMIT %d
FORMAT TabSeparated`,
		e.appId, subject, result, latency, e.tsSec,
		table,
		appTrailWhere(vocab.MembKindAudit.GetId().Value(), filter.Since),
		appTrailLimit(filter.Limit))
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
