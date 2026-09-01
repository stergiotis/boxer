package chstore

// The launcher's ranking read (ADR-0214 §SD7/§SD8).
//
// ADR-0158 §SD10 deferred ranking because it "needs a launch record in
// boxer.facts". That record has existed since ADR-0026: every window open
// writes an app-lifecycle `started` row carrying the app id and a timestamp.
// What was missing is this — one aggregate across runs. [Store.LifecyclesByRun]
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
	sql := composeAppLaunchStatsSql(inst.qualifiedTable(), halfLife, limit)
	body, qerr := inst.cli.Query(ctx, sql)
	if qerr != nil {
		err = eh.Errorf("chstore: app launch stats query: %w", qerr)
		return
	}
	defer body.Close()
	raw, rerr := io.ReadAll(body)
	if rerr != nil {
		err = eh.Errorf("chstore: app launch stats read: %w", rerr)
		return
	}
	stats, err = parseAppLaunchStats(raw)
	return
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
	const (
		symLR  = "`tv:symbol:lr:lr:u64:1247:::0::data`"
		symLMR = "`tv:symbol:lmr:lmr:u64:1247:::0::data`"
		tsCol  = "`ts:ts:z64:47::0:`"
	)
	whereParts := []string{
		fmt.Sprintf("has(%s, %d)", symLR, vocab.MembKindAppLifecycle.GetId().Value()),
		fmt.Sprintf("has(%s, %d)", symLMR, vocab.MembRuntimeApp.GetId().Value()),
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
LIMIT %d
FORMAT TabSeparated`,
		halfLifeSec,
		e.appId, tsCol,
		table,
		strings.Join(whereParts, " AND "),
		limit)
	return
}

// parseAppLaunchStats reads the four-column TabSeparated result.
//
// A malformed line fails the whole read rather than being skipped: this feeds
// an ordering, and an ordering computed from a silently truncated trail is
// worse than one the caller knows it could not compute.
func parseAppLaunchStats(raw []byte) (stats []factsstore.AppLaunchStat, err error) {
	stats = []factsstore.AppLaunchStat{}
	text := strings.TrimRight(string(raw), "\n")
	if text == "" {
		return
	}
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 4 {
			err = eb.Build().Int("got", len(parts)).Str("line", line).Errorf("chstore: app launch stats: expected 4 columns")
			return
		}
		opens, perr := strconv.ParseUint(parts[1], 10, 64)
		if perr != nil {
			err = eb.Build().Str("opens", parts[1]).Errorf("chstore: app launch stats: parse opens: %w", perr)
			return
		}
		lastTs, terr := strconv.ParseInt(parts[2], 10, 64)
		if terr != nil {
			err = eb.Build().Str("lastTs", parts[2]).Errorf("chstore: app launch stats: parse last_ts: %w", terr)
			return
		}
		score, serr := strconv.ParseFloat(parts[3], 64)
		if serr != nil {
			err = eb.Build().Str("score", parts[3]).Errorf("chstore: app launch stats: parse score: %w", serr)
			return
		}
		stats = append(stats, factsstore.AppLaunchStat{
			AppId:  app.AppIdT(parts[0]),
			Opens:  opens,
			LastTs: time.Unix(lastTs, 0).UTC(),
			Score:  score,
		})
	}
	return
}
