package appcenter

import (
	"strings"
	"time"

	playlaunch "github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// A section's "Open in play" opens the SQL playground on the introspection
// endpoint with a statement over the table the section reads, filtered to
// the app, and runs it. Play reads the table live and whole: every column,
// not only the ones the page shows, and joins where the page composed in Go
// (the ADR list). The statement is the page's filter with the id inlined,
// since a launch config carries no parameters.
//
// The model-call statement leaves out the prompt and completion, as the page
// does (ADR-0260 §SD3); a reader in play can add them.

// Section keys, shared by the page's headers and playQuery.
const (
	secRuns     = "runs"
	secLogs     = "logs"
	secAudit    = "audit"
	secRun      = "run"
	secCaps     = "caps"
	secState    = "state"
	secCoverage = "coverage"
	secAdrs     = "adrs"
	secJobs     = "jobs"
	secDatasets = "datasets"
	secLlm      = "llm"
	secTasks    = "tasks"
)

// playSections is every section that opens in play, in page order.
var playSections = []string{
	secRuns, secLogs, secAudit, secRun, secCaps, secState, secCoverage,
	secAdrs, secJobs, secDatasets, secLlm, secTasks,
}

// sqlString is s as a ClickHouse string literal.
func sqlString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// playQuery is the statement section key opens play on for appId. adrDir is
// the app's directory in the citation index (packageDir); the ADR section
// has no statement without one. ok is false for a section with none.
func playQuery(key string, appId string, adrDir string) (sql string, ok bool) {
	id := sqlString(appId)
	ok = true
	switch key {
	case secRuns:
		// One bar per window session, drawn on play's Timeline (playTab). No
		// lane column, so sessions of two windows at once stack rather than
		// overdraw.
		sql = sessionTimelineSql("WHERE app_id = " + id)
	case secLogs:
		sql = "SELECT fromUnixTimestamp64Milli(ts_ms) AS ts, level, message, error, caller, run_id, instance_key\n" +
			"FROM keelson('app_logs')\nWHERE app_id = " + id + "\nORDER BY ts_ms DESC"
	case secAudit:
		sql = "SELECT subject, result, requests, mean_latency_ms, max_latency_ms,\n" +
			"  fromUnixTimestamp64Milli(first_ms) AS first, fromUnixTimestamp64Milli(last_ms) AS last\n" +
			"FROM keelson('app_audit')\nWHERE app_id = " + id + "\nORDER BY requests DESC, subject, result"
	case secRun:
		sql = "SELECT fromUnixTimestamp64Milli(ts_ms) AS ts, kind, instance_key, detail, source, run_id\n" +
			"FROM keelson('runtime_events')\nWHERE app_id = " + id + "\nORDER BY ts_ms DESC"
	case secCaps:
		// Declared and held side by side: the manifest's list, and what each
		// open window holds, granted or declared.
		sql = "SELECT 'manifest' AS origin, toUInt64(0) AS instance_key, cap AS capability, '' AS reason\n" +
			"FROM keelson('apps') ARRAY JOIN caps AS cap\nWHERE id = " + id + "\n" +
			"UNION ALL\n" +
			"SELECT if(declared, 'held, declared', 'held, granted') AS origin, instance_key,\n" +
			"  concat(pattern, ' [', direction, ']') AS capability, reason\n" +
			"FROM keelson('client_caps')\nWHERE app_id = " + id + "\n" +
			"ORDER BY origin, instance_key, capability"
	case secState:
		sql = "SELECT kind, key, payload_bytes, detail, written_at, run_id, instance_key\n" +
			"FROM keelson('app_state')\nWHERE app_id = " + id + "\nORDER BY kind, key"
	case secCoverage:
		sql = "SELECT pkg_path,\n" +
			"  round(100 * covered_stmts / nullIf(total_stmts, 0), 1) AS stmts_pct, covered_stmts, total_stmts,\n" +
			"  round(100 * covered_funcs / nullIf(total_funcs, 0), 1) AS funcs_pct, covered_funcs, total_funcs\n" +
			"FROM keelson('coverage_pkgs')\n" +
			"WHERE pkg_path = " + id + " OR startsWith(pkg_path, concat(" + id + ", '/'))\n" +
			"ORDER BY pkg_path"
	case secAdrs:
		if adrDir == "" {
			return "", false
		}
		dir := sqlString(adrDir)
		sql = "SELECT r.num AS num, any(a.title) AS title, any(a.status) AS status,\n" +
			"  count() AS citations, groupUniqArray(r.path) AS files\n" +
			"FROM keelson('coderef') AS r\nLEFT JOIN keelson('adr') AS a ON a.num = r.num\n" +
			"WHERE r.pkg = " + dir + " OR startsWith(r.pkg, concat(" + dir + ", '/'))\n" +
			"GROUP BY r.num\nORDER BY citations DESC, num"
	case secJobs:
		sql = "SELECT *\nFROM keelson('watchbill')\nWHERE owner_app_id = " + id + "\nORDER BY run_after DESC"
	case secDatasets:
		sql = "SELECT *\nFROM keelson('adhoc')\nWHERE publisher = " + id + "\nORDER BY alias"
	case secLlm:
		sql = "SELECT at, purpose, sensitivity, model, endpoint_host, messages, tools, tool_calls,\n" +
			"  input_tokens, output_tokens, elapsed_ms, finish_reason, incomplete, refused, error\n" +
			"FROM keelson('llm_calls')\nWHERE app_id = " + id + "\nORDER BY at DESC"
	case secTasks:
		sql = "SELECT *\nFROM keelson('tasks')\nWHERE owner_app_id = " + id + "\nORDER BY created_at DESC"
	default:
		ok = false
	}
	return
}

// sessionTimelineSql draws keelson('app_runs') as intervals in play's
// Timeline column contract (`_tl_time`, `_tl_time_end`). The SQL applet
// `app-sessions` spells the same shape by hand, with a lane per app; the two
// are kept alike.
//
// A bar ends at the session's close; without one, at its process's last
// heartbeat, and the `ending` column says the end is a lower bound; with
// neither, it is a one-second mark. A session whose start is not in the
// look-back is a one-second mark at its close: drawing it from the
// look-back's edge would invent a session as long as the look-back.
func sessionTimelineSql(where string) string {
	return strings.NewReplacer("{{where}}", where).Replace(`SELECT
  arrayElement(splitByChar('/', app_id), -1) AS app,
  fromUnixTimestamp64Milli(start_ms, 'UTC') AS started,
  fromUnixTimestamp64Milli(end_ms, 'UTC') AS ended,
  intDiv(end_ms - start_ms, 1000) AS seconds,
  ending, run_id, instance_key, app_id,
  fromUnixTimestamp64Milli(start_ms, 'UTC') AS _tl_time,
  fromUnixTimestamp64Milli(end_ms, 'UTC') AS _tl_time_end
FROM (
  SELECT *,
    if(started_ms > 0, started_ms, stopped_ms - 1000) AS start_ms,
    multiIf(stopped_ms > 0, stopped_ms, run_seen_ms > start_ms, run_seen_ms, start_ms + 1000) AS end_ms,
    multiIf(started_ms = 0, concat('no start in the look-back; closed: ', stop_reason),
            stopped_ms > 0, concat('closed: ', stop_reason),
            run_seen_ms > start_ms, 'no close: process last seen',
            'no close: no later sign of the process') AS ending
  FROM keelson('app_runs')
  {{where}}
)
ORDER BY _tl_time ASC`)
}

// playTab is the tab a section opens play on; "" leaves play's own choice.
func playTab(key string) string {
	if key == secRuns {
		return "timeline"
	}
	return ""
}

// openInPlay opens a play window running sql on the introspection endpoint,
// focused on tab when set, off the frame goroutine. A refusal lands as the
// status line's note.
func (inst *App) openInPlay(sql string, tab string) {
	cfg, err := buscodec.Encode(playlaunch.PlayLaunch{
		At:       time.Now().UTC(),
		Sql:      sql,
		AutoRun:  true,
		Endpoint: playlaunch.EndpointIntrospection,
		Tab:      tab,
	})
	if err != nil {
		inst.mu.Lock()
		inst.snap.openNote = "open in play: " + err.Error()
		inst.mu.Unlock()
		return
	}
	inst.request(playlaunch.AppId, playlaunch.Kind, cfg)
}

// request asks the window host to open target, with a launch config when
// kind is set, off the frame goroutine.
func (inst *App) request(target string, kind string, cfg []byte) {
	if inst.bus == nil || inst.appCtx == nil || inst.appCtx.Err() != nil {
		return
	}
	inst.wg.Add(1)
	go func() {
		defer inst.wg.Done()
		_, err := windowhost.RequestOpen(inst.bus, app.AppIdT(target), kind, cfg)
		note := ""
		if err != nil {
			note = "open " + shortApp(target) + ": " + err.Error()
		}
		inst.mu.Lock()
		inst.snap.openNote = note
		inst.mu.Unlock()
	}()
}
