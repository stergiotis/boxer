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

// LifecycleFilter narrows the rows returned by LifecyclesByRun. RunId is
// required — joining without a run anchor produces a global firehose,
// which has no real consumer today. AppId is optional. Limit is
// hard-capped at lifecycleRowsCap regardless of the caller value.
type LifecycleFilter struct {
	RunId string
	AppId app.AppIdT
	Limit uint32
}

// lifecycleRowsCap mirrors recentLogsCap: a generous upper bound that
// keeps a misuse from pulling the full retention window into memory.
const lifecycleRowsCap = uint32(1000)

// LifecyclesByRun returns app-lifecycle rows attributed to filter.RunId,
// ordered chronologically by ts (started events before stopped events,
// in process-time order). When filter.AppId is set, the result is
// further narrowed to that app — useful for "show every open/close of
// play in this run". RunId is required; an empty RunId returns an
// error rather than a global scan.
//
// Returns an empty slice (not nil) when no rows match. The SQL filter
// applies the limit cap regardless of the caller-supplied Limit.
func (inst *Store) LifecyclesByRun(ctx context.Context, filter LifecycleFilter) (rows []factsstore.AppLifecycleRow, err error) {
	if filter.RunId == "" {
		err = eh.Errorf("chstore: LifecyclesByRun requires a non-empty RunId")
		return
	}
	limit := filter.Limit
	if limit == 0 || limit > lifecycleRowsCap {
		limit = lifecycleRowsCap
	}
	sql := composeLifecyclesByRunSql(inst.qualifiedTable(), filter, limit)
	body, qerr := inst.cli.Query(ctx, sql)
	if qerr != nil {
		err = eh.Errorf("chstore: lifecycles by run query: %w", qerr)
		return
	}
	defer body.Close()
	raw, rerr := io.ReadAll(body)
	if rerr != nil {
		err = eh.Errorf("chstore: lifecycles by run read: %w", rerr)
		return
	}
	rows, err = parseLifecyclesByRunRows(raw)
	if err != nil {
		err = eh.Errorf("chstore: lifecycles by run parse: %w", err)
		return
	}
	return
}

// LastHeartbeatForRun returns the timestamp of the most recent
// heartbeat row attributed to runId. Found is false when no
// heartbeats exist for that run (process didn't tick before
// crashing, or never had heartbeat persistence wired). RunId is
// required.
func (inst *Store) LastHeartbeatForRun(ctx context.Context, runId string) (ts time.Time, found bool, err error) {
	if runId == "" {
		err = eh.Errorf("chstore: LastHeartbeatForRun requires a non-empty RunId")
		return
	}
	sql := composeLastHeartbeatSql(inst.qualifiedTable(), runId)
	body, qerr := inst.cli.Query(ctx, sql)
	if qerr != nil {
		err = eh.Errorf("chstore: last heartbeat query: %w", qerr)
		return
	}
	defer body.Close()
	raw, rerr := io.ReadAll(body)
	if rerr != nil {
		err = eh.Errorf("chstore: last heartbeat read: %w", rerr)
		return
	}
	ts, found, err = parseLastHeartbeatRow(raw)
	if err != nil {
		err = eh.Errorf("chstore: last heartbeat parse: %w", err)
		return
	}
	return
}

func composeLastHeartbeatSql(table, runId string) (sql string) {
	const (
		symLR  = "`tv:symbol:lr:lr:u64:2q:0:0:0::data`"
		symLMR = "`tv:symbol:lmr:lmr:u64:2q:0:0:0::data`"
		tsCol  = "`ts:ts:z64:2k:0:0:`"
	)
	whereParts := []string{
		fmt.Sprintf("has(%s, %d)", symLR, vocab.MembKindRuntimeHeartbeat.GetId().Value()),
		fmt.Sprintf("has(%s, %d)", symLMR, vocab.MembRuntimeRun.GetId().Value()),
		runIdPredicate(runId),
	}
	sql = fmt.Sprintf(`
SELECT toUnixTimestamp(%s) AS ts_sec
FROM %s
WHERE %s
ORDER BY %s DESC
LIMIT 1
FORMAT TabSeparated`,
		tsCol, table,
		strings.Join(whereParts, " AND "),
		tsCol)
	return
}

func parseLastHeartbeatRow(raw []byte) (ts time.Time, found bool, err error) {
	if len(raw) == 0 {
		return
	}
	line := strings.TrimRight(string(raw), "\n")
	if line == "" {
		return
	}
	if newlineIdx := strings.IndexByte(line, '\n'); newlineIdx >= 0 {
		line = line[:newlineIdx]
	}
	tsSec, perr := strconv.ParseInt(line, 10, 64)
	if perr != nil {
		err = eh.Errorf("chstore: last heartbeat: parse ts %q: %w", line, perr)
		return
	}
	ts = time.Unix(tsSec, 0).UTC()
	found = true
	return
}

// LookupRunStart fetches the runtime-start row for runId. Found is false
// when no matching row exists (e.g., the runId was minted by a process
// that didn't have facts persistence wired). RunId is required.
func (inst *Store) LookupRunStart(ctx context.Context, runId string) (row factsstore.RuntimeStartRow, found bool, err error) {
	if runId == "" {
		err = eh.Errorf("chstore: LookupRunStart requires a non-empty RunId")
		return
	}
	sql := composeLookupRunStartSql(inst.qualifiedTable(), runId)
	body, qerr := inst.cli.Query(ctx, sql)
	if qerr != nil {
		err = eh.Errorf("chstore: lookup run start query: %w", qerr)
		return
	}
	defer body.Close()
	raw, rerr := io.ReadAll(body)
	if rerr != nil {
		err = eh.Errorf("chstore: lookup run start read: %w", rerr)
		return
	}
	row, found, err = parseLookupRunStartRow(raw)
	if err != nil {
		err = eh.Errorf("chstore: lookup run start parse: %w", err)
		return
	}
	return
}

// columnExprsRun gathers the projections for LookupRunStart. Order
// MUST match parseLookupRunStartRow.
type columnExprsRun struct {
	id           string
	tsSec        string
	hostname     string
	goVersion    string
	vcsRevision  string
	modulePath   string
	vcsBuildInfo string
	pid          string
	vcsModified  string
}

func buildRunColumnExprs() (e columnExprsRun) {
	const (
		symValue    = "`tv:symbol:value:val:s:m:0:24:0::data`"
		symLR       = "`tv:symbol:lr:lr:u64:2q:0:0:0::data`"
		symLRCard   = "`tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::data`"
		strValue    = "`tv:stringArray:value:val:sh:g:0:0:0::data`"
		strLR       = "`tv:stringArray:lr:lr:u64:2q:0:0:0::data`"
		strLRCard   = "`tv:stringArray:lrcard:lrcard:u64:4gw:0:0:0::data`"
		u64Value    = "`tv:u64Array:value:val:u64h:g:0:0:0::data`"
		u64LR       = "`tv:u64Array:lr:lr:u64:2q:0:0:0::data`"
		u64LRCard   = "`tv:u64Array:lrcard:lrcard:u64:4gw:0:0:0::data`"
		boolValue   = "`tv:bool:value:val:b:g:0:0:0::data`"
		boolLR      = "`tv:bool:lr:lr:u64:2q:0:0:0::data`"
		boolLRCard  = "`tv:bool:lrcard:lrcard:u64:4gw:0:0:0::data`"
		idCol       = "`id:id:u64:2k:0:0:`"
		tsCol       = "`ts:ts:z64:2k:0:0:`"
	)
	e.id = idCol
	e.tsSec = fmt.Sprintf("toUnixTimestamp(%s)", tsCol)
	e.hostname = pickLcrString(symValue, symLR, symLRCard, vocab.MembRunHostname.GetId().Value())
	e.goVersion = pickLcrString(symValue, symLR, symLRCard, vocab.MembRunGoVersion.GetId().Value())
	e.vcsRevision = pickLcrString(symValue, symLR, symLRCard, vocab.MembRunVcsRevision.GetId().Value())
	e.modulePath = pickLcrString(symValue, symLR, symLRCard, vocab.MembRunModulePath.GetId().Value())
	e.vcsBuildInfo = pickLcrString(strValue, strLR, strLRCard, vocab.MembRunVcsBuildInfo.GetId().Value())
	e.pid = pickLcrNumeric(u64Value, u64LR, u64LRCard, vocab.MembRunPid.GetId().Value(), "0")
	e.vcsModified = pickLcrNumeric(boolValue, boolLR, boolLRCard, vocab.MembRunVcsModified.GetId().Value(), "false")
	return
}

// columnExprsLifecycle gathers the projections for LifecyclesByRun.
// Order MUST match parseLifecyclesByRunRows.
type columnExprsLifecycle struct {
	id         string
	tsSec      string
	appId      string
	runId      string
	phase      string
	stopReason string
	tileKey    string
}

func buildLifecycleColumnExprs() (e columnExprsLifecycle) {
	const (
		symValue   = "`tv:symbol:value:val:s:m:0:24:0::data`"
		symLR      = "`tv:symbol:lr:lr:u64:2q:0:0:0::data`"
		symLRCard  = "`tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::data`"
		symLMR     = "`tv:symbol:lmr:lmr:u64:2q:0:0:0::data`"
		symMRHP    = "`tv:symbol:mrhp:mrhp:y:g:0:0:0::data`"
		strValue   = "`tv:stringArray:value:val:sh:g:0:0:0::data`"
		strLR      = "`tv:stringArray:lr:lr:u64:2q:0:0:0::data`"
		strLRCard  = "`tv:stringArray:lrcard:lrcard:u64:4gw:0:0:0::data`"
		u64Value   = "`tv:u64Array:value:val:u64h:g:0:0:0::data`"
		u64LR      = "`tv:u64Array:lr:lr:u64:2q:0:0:0::data`"
		u64LRCard  = "`tv:u64Array:lrcard:lrcard:u64:4gw:0:0:0::data`"
		idCol      = "`id:id:u64:2k:0:0:`"
		tsCol      = "`ts:ts:z64:2k:0:0:`"
	)
	e.id = idCol
	e.tsSec = fmt.Sprintf("toUnixTimestamp(%s)", tsCol)
	e.appId = fmt.Sprintf("arrayFirst((p, m) -> m = %d, %s, %s)",
		vocab.MembRuntimeApp.GetId().Value(), symMRHP, symLMR)
	e.runId = fmt.Sprintf("arrayFirst((p, m) -> m = %d, %s, %s)",
		vocab.MembRuntimeRun.GetId().Value(), symMRHP, symLMR)
	e.phase = pickLcrString(symValue, symLR, symLRCard, vocab.MembLifecyclePhase.GetId().Value())
	e.stopReason = pickLcrString(strValue, strLR, strLRCard, vocab.MembLifecycleStopReason.GetId().Value())
	e.tileKey = pickLcrNumeric(u64Value, u64LR, u64LRCard, vocab.MembLifecycleTileKey.GetId().Value(), "0")
	return
}

// pickLcrNumeric is the numeric counterpart to pickLcrString: same
// cumulative-sum lookup, but the not-found default is a caller-chosen
// constant (e.g. "0" for u64, "false" for bool) rather than the empty
// string. Kept separate so the existing pickLcrString call sites stay
// untouched.
func pickLcrNumeric(valueArr, lrArr, lrCardArr string, membershipId uint64, zero string) (expr string) {
	idxInLr := fmt.Sprintf("indexOf(%s, %d)", lrArr, membershipId)
	expr = fmt.Sprintf("if(%s > 0, arrayElement(%s, indexOf(arrayCumSum(%s), %s)), %s)",
		idxInLr, valueArr, lrCardArr, idxInLr, zero)
	return
}

// runIdPredicate is the "single positional arrayElement" predicate the
// ADR amendment names: locate the MembRuntimeRun mixed-membership on
// the symbol-LMR array, read the parallel mrhp entry, compare to the
// requested run_id.
func runIdPredicate(runId string) (expr string) {
	const (
		symLMR  = "`tv:symbol:lmr:lmr:u64:2q:0:0:0::data`"
		symMRHP = "`tv:symbol:mrhp:mrhp:y:g:0:0:0::data`"
	)
	expr = fmt.Sprintf("arrayFirst((p, m) -> m = %d, %s, %s) = %s",
		vocab.MembRuntimeRun.GetId().Value(), symMRHP, symLMR, quoteSqlString(runId))
	return
}

// appIdPredicate is the parallel predicate for MembRuntimeApp; used by
// the optional LifecycleFilter.AppId narrowing.
func appIdPredicate(appId app.AppIdT) (expr string) {
	const (
		symLMR  = "`tv:symbol:lmr:lmr:u64:2q:0:0:0::data`"
		symMRHP = "`tv:symbol:mrhp:mrhp:y:g:0:0:0::data`"
	)
	expr = fmt.Sprintf("arrayFirst((p, m) -> m = %d, %s, %s) = %s",
		vocab.MembRuntimeApp.GetId().Value(), symMRHP, symLMR, quoteSqlString(string(appId)))
	return
}

func composeLookupRunStartSql(table, runId string) (sql string) {
	e := buildRunColumnExprs()
	const (
		symLR  = "`tv:symbol:lr:lr:u64:2q:0:0:0::data`"
		symLMR = "`tv:symbol:lmr:lmr:u64:2q:0:0:0::data`"
	)
	whereParts := []string{
		fmt.Sprintf("has(%s, %d)", symLR, vocab.MembKindRuntimeRun.GetId().Value()),
		fmt.Sprintf("has(%s, %d)", symLMR, vocab.MembRuntimeRun.GetId().Value()),
		runIdPredicate(runId),
	}
	sql = fmt.Sprintf(`
SELECT
  %s AS id,
  %s AS ts_sec,
  %s AS hostname,
  %s AS go_version,
  %s AS vcs_revision,
  %s AS module_path,
  %s AS vcs_build_info,
  %s AS pid,
  %s AS vcs_modified
FROM %s
WHERE %s
ORDER BY %s ASC
LIMIT 1
FORMAT TabSeparated`,
		e.id, e.tsSec, e.hostname, e.goVersion, e.vcsRevision,
		e.modulePath, e.vcsBuildInfo, e.pid, e.vcsModified,
		table,
		strings.Join(whereParts, " AND "),
		e.id)
	return
}

func composeLifecyclesByRunSql(table string, filter LifecycleFilter, limit uint32) (sql string) {
	e := buildLifecycleColumnExprs()
	const (
		symLR  = "`tv:symbol:lr:lr:u64:2q:0:0:0::data`"
		symLMR = "`tv:symbol:lmr:lmr:u64:2q:0:0:0::data`"
		tsCol  = "`ts:ts:z64:2k:0:0:`"
	)
	whereParts := []string{
		fmt.Sprintf("has(%s, %d)", symLR, vocab.MembKindAppLifecycle.GetId().Value()),
		fmt.Sprintf("has(%s, %d)", symLMR, vocab.MembRuntimeRun.GetId().Value()),
		runIdPredicate(filter.RunId),
	}
	if filter.AppId != "" {
		whereParts = append(whereParts,
			fmt.Sprintf("has(%s, %d)", symLMR, vocab.MembRuntimeApp.GetId().Value()),
			appIdPredicate(filter.AppId))
	}
	sql = fmt.Sprintf(`
SELECT
  %s AS id,
  %s AS ts_sec,
  %s AS app_id,
  %s AS run_id,
  %s AS phase,
  %s AS stop_reason,
  %s AS tile_key
FROM %s
WHERE %s
ORDER BY %s ASC, %s ASC
LIMIT %d
FORMAT TabSeparated`,
		e.id, e.tsSec, e.appId, e.runId, e.phase, e.stopReason, e.tileKey,
		table,
		strings.Join(whereParts, " AND "),
		tsCol, e.id,
		limit)
	return
}

func parseLookupRunStartRow(raw []byte) (row factsstore.RuntimeStartRow, found bool, err error) {
	if len(raw) == 0 {
		return
	}
	line := strings.TrimRight(string(raw), "\n")
	if line == "" {
		return
	}
	// LIMIT 1 + a single matching row yields a single line; multiple
	// matches (shouldn't happen — natural key collapses duplicates) take
	// the first.
	if newlineIdx := strings.IndexByte(line, '\n'); newlineIdx >= 0 {
		line = line[:newlineIdx]
	}
	parts := strings.Split(line, "\t")
	if len(parts) != 9 {
		err = eh.Errorf("chstore: lookup run start: expected 9 columns, got %d (line=%q)", len(parts), line)
		return
	}
	_, perr := strconv.ParseUint(parts[0], 10, 64)
	if perr != nil {
		err = eh.Errorf("chstore: lookup run start: parse id %q: %w", parts[0], perr)
		return
	}
	tsSec, perr := strconv.ParseInt(parts[1], 10, 64)
	if perr != nil {
		err = eh.Errorf("chstore: lookup run start: parse ts %q: %w", parts[1], perr)
		return
	}
	pid, perr := strconv.ParseInt(parts[7], 10, 64)
	if perr != nil {
		err = eh.Errorf("chstore: lookup run start: parse pid %q: %w", parts[7], perr)
		return
	}
	row = factsstore.RuntimeStartRow{
		Ts:           time.Unix(tsSec, 0).UTC(),
		Hostname:     unescapeTabSeparated(parts[2]),
		GoVersion:    unescapeTabSeparated(parts[3]),
		VcsRevision:  unescapeTabSeparated(parts[4]),
		ModulePath:   unescapeTabSeparated(parts[5]),
		VcsBuildInfo: unescapeTabSeparated(parts[6]),
		Pid:          int(pid),
		VcsModified:  parts[8] == "true" || parts[8] == "1",
	}
	found = true
	return
}

func parseLifecyclesByRunRows(raw []byte) (rows []factsstore.AppLifecycleRow, err error) {
	rows = []factsstore.AppLifecycleRow{}
	if len(raw) == 0 {
		return
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 7 {
			err = eh.Errorf("chstore: lifecycles by run: expected 7 columns, got %d (line=%q)", len(parts), line)
			return
		}
		_, perr := strconv.ParseUint(parts[0], 10, 64)
		if perr != nil {
			err = eh.Errorf("chstore: lifecycles by run: parse id %q: %w", parts[0], perr)
			return
		}
		tsSec, perr := strconv.ParseInt(parts[1], 10, 64)
		if perr != nil {
			err = eh.Errorf("chstore: lifecycles by run: parse ts %q: %w", parts[1], perr)
			return
		}
		tileKey, perr := strconv.ParseUint(parts[6], 10, 64)
		if perr != nil {
			err = eh.Errorf("chstore: lifecycles by run: parse tile_key %q: %w", parts[6], perr)
			return
		}
		phaseStr := unescapeTabSeparated(parts[4])
		row := factsstore.AppLifecycleRow{
			Ts:         time.Unix(tsSec, 0).UTC(),
			AppId:      app.AppIdT(unescapeTabSeparated(parts[2])),
			RunId:      unescapeTabSeparated(parts[3]),
			Phase:      phaseFromString(phaseStr),
			StopReason: unescapeTabSeparated(parts[5]),
			TileKey:    tileKey,
		}
		rows = append(rows, row)
	}
	return
}

func phaseFromString(s string) (p factsstore.AppLifecyclePhaseE) {
	switch s {
	case "started":
		p = factsstore.AppLifecyclePhaseStarted
	case "stopped":
		p = factsstore.AppLifecyclePhaseStopped
	default:
		p = factsstore.AppLifecyclePhaseUnspecified
	}
	return
}
