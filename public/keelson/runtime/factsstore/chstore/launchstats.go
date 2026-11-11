package chstore

// The launcher's ranking read (ADR-0214 §SD7/§SD8).
//
// ADR-0158 §SD10 deferred ranking because it "needs a launch record in
// boxer.facts". That record has existed since ADR-0026: every window open
// writes an app-lifecycle `started` row carrying the app id and a timestamp.
// What was missing is this — one aggregate across runs. [Store.ListRunEvents]
// refuses to scan without a run anchor, and that refusal is right for its own
// consumer (a per-run timeline), so this is a second query rather than a
// loosening of the first.
//
// The decay runs server-side. Shipping one row per launch and folding it in Go
// would move the whole trail across the wire every time the launcher opens,
// for a result that is one float per app.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
)

var _ factsstore.AppLaunchHistoryReaderI = (*Store)(nil)

// launchStatsCap bounds the result. Generous against any plausible app corpus
// — one row per app that has ever been opened — and there so a corrupt trail
// cannot pull an unbounded scan into memory.
const launchStatsCap = uint32(4096)

// AppLaunchStats implements [factsstore.AppLaunchHistoryReaderI].
func (inst *Store) AppLaunchStats(ctx context.Context, halfLife time.Duration, limit uint32) (stats []factsstore.AppLaunchStat, err error) {
	if halfLife <= 0 {
		err = eh.Errorf("chstore: AppLaunchStats requires a positive halfLife")
		return
	}
	if limit == 0 || limit > launchStatsCap {
		limit = launchStatsCap
	}
	err = inst.readRows(ctx, "app launch stats",
		composeAppLaunchStatsSql(inst.qualifiedTable(), halfLife, limit), 4, func(r row) {
			stats = append(stats, launchStatOf(r))
		})
	if stats == nil && err == nil {
		stats = []factsstore.AppLaunchStat{}
	}
	return
}

// launchStatOf reads one row of composeAppLaunchStatsSql's result.
func launchStatOf(r row) factsstore.AppLaunchStat {
	return factsstore.AppLaunchStat{
		AppId:  app.AppIdT(r.str(0)),
		Opens:  r.u64(1),
		LastTs: time.Unix(r.i64(2), 0).UTC(),
		Score:  r.f64(3),
	}
}

// composeAppLaunchStatsSql groups the `started` app-lifecycle rows by app and
// folds each app's timestamps into a decayed weight.
//
// The weight is the textbook frecency form: every launch contributes
// 2^(-age/halfLife), so one launch a half-life ago is worth half of one just
// now, and the sum grows with frequency. Computed against the server's clock
// rather than the client's — the rows were written with the server's now(),
// and mixing clocks would make a skewed client see the future.
//
// `phase = 'started'` is the whole filter beyond the kind tag: a close is not
// a launch, and counting both would double every completed session while
// leaving the open one single.
func composeAppLaunchStatsSql(table string, halfLife time.Duration, limit uint32) (sql string) {
	e := buildLifecycleColumnExprs()
	whereParts := []string{
		fmt.Sprintf("has(%s, %d)", hSymLr, vocab.MembKindAppLifecycle.GetId().Value()),
		fmt.Sprintf("has(%s, %d)", hSymLr, vocab.MembRuntimeApp.GetId().Value()),
		fmt.Sprintf("%s = 'started'", e.phase),
	}
	halfLifeSec := halfLife.Seconds()
	sql = fmt.Sprintf(`
SELECT
  app_id,
  count() AS opens,
  max(ts_sec) AS last_ts,
  sum(pow(2, -1 * (toUnixTimestamp(now()) - ts_sec) / %f)) AS score
FROM (
  SELECT
    %s AS app_id,
    toUnixTimestamp(%s) AS ts_sec
  FROM %s
  WHERE %s
)
WHERE app_id != ''
GROUP BY app_id
ORDER BY score DESC, last_ts DESC, app_id ASC
LIMIT %d`,
		halfLifeSec,
		e.appId, hTs,
		table,
		strings.Join(whereParts, " AND "),
		limit)
	return
}
