package appcenter

import (
	"sort"
	"strings"
	"time"
)

// The page's derivations: pure functions of the snapshot, so the claims
// ADR-0260 §SD4 makes for each lens are tested without a window.

// shortApp is the last segment of an app id — its import path's leaf, or an
// applet's slug. The full id is on hover.
func shortApp(appId string) (s string) {
	s = appId
	if i := strings.LastIndexByte(s, '/'); i >= 0 && i < len(s)-1 {
		s = s[i+1:]
	}
	return
}

// matchingApps is the indices of the apps whose id, display name or summary
// contains filter, case-insensitively; every app for an empty filter.
func matchingApps(cols *appCols, filter string) (idx []int) {
	f := strings.ToLower(strings.TrimSpace(filter))
	for i, id := range cols.Id {
		if f == "" || strings.Contains(strings.ToLower(id), f) ||
			strings.Contains(strings.ToLower(cols.Display[i]), f) ||
			strings.Contains(strings.ToLower(cols.Summary[i]), f) {
			idx = append(idx, i)
		}
	}
	return
}

// appIndex is the row of appId in cols, or -1.
func appIndex(cols *appCols, appId string) int {
	for i, id := range cols.Id {
		if id == appId {
			return i
		}
	}
	return -1
}

// packageDir is the repository directory of appId as the citation index
// spells it (`coderef.pkg`, e.g. "apps/appstate"), or "" when no indexed
// package lies at or under any tail of the id.
//
// The index is keyed by directory, the app by import path, and the module
// prefix between them is not recorded on either side. The longest tail that
// names an indexed directory — itself, or as the parent of one — wins, so a
// short leaf ("play") cannot match an unrelated directory of the same name
// when the full tail ("apps/play") also matches.
func packageDir(appId string, pkgs []string) (dir string) {
	if appId == "" || strings.HasPrefix(appId, "/") {
		return
	}
	for i := 0; i < len(appId); i++ {
		if i > 0 && appId[i-1] != '/' {
			continue
		}
		tail := appId[i:]
		for _, p := range pkgs {
			if p == tail || strings.HasPrefix(p, tail+"/") {
				return tail
			}
		}
	}
	return
}

// adrRef is one ADR the app's code cites.
type adrRef struct {
	num    int32
	title  string
	status string
	refs   uint64
}

// adrsFor is the ADRs cited in appId's package subtree, most-cited first
// (ADR-0260 §SD4): a lower bound on what governs the app, since code that
// implements a decision without naming it is invisible to the index.
func adrsFor(appId string, refs *coderefCols, adrs *adrCols) (out []adrRef, dir string) {
	dir = packageDir(appId, refs.Pkg)
	if dir == "" {
		return
	}
	counts := make(map[int32]uint64, 8)
	for i, p := range refs.Pkg {
		if p == dir || strings.HasPrefix(p, dir+"/") {
			counts[refs.Num[i]] += refs.Refs[i]
		}
	}
	titles := make(map[int32]int, len(adrs.Num))
	for i, n := range adrs.Num {
		titles[n] = i
	}
	out = make([]adrRef, 0, len(counts))
	for n, c := range counts {
		r := adrRef{num: n, refs: c}
		if i, ok := titles[n]; ok {
			r.title, r.status = adrs.Title[i], adrs.Status[i]
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].refs != out[j].refs {
			return out[i].refs > out[j].refs
		}
		return out[i].num < out[j].num
	})
	return
}

// coverageTotal sums the app's packages. ok is false when there are none.
type coverageTotal struct {
	pkgs         int
	coveredStmts uint64
	totalStmts   uint64
	coveredFuncs uint64
	totalFuncs   uint64
}

func sumCoverage(cols *coveragePkgCols) (t coverageTotal) {
	t.pkgs = len(cols.PkgPath)
	for i := range cols.PkgPath {
		t.coveredStmts += cols.CoveredStmts[i]
		t.totalStmts += cols.TotalStmts[i]
		t.coveredFuncs += cols.CoveredFuncs[i]
		t.totalFuncs += cols.TotalFuncs[i]
	}
	return
}

// percent is covered/total as a percentage, 0 for an empty total.
func percent(covered, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(covered) / float64(total)
}

// coverageActive reports whether the process samples coverage at all: a
// status row with a mode says so, and without one every package reads as
// absent rather than as uncovered.
func coverageActive(cols *coverageStatusCols) bool {
	return len(cols.Mode) > 0 && cols.Mode[0] != ""
}

// kindCount is how many events of one kind the run recorded for the app.
type kindCount struct {
	kind  string
	count int
}

// countKinds tallies the events by kind, most frequent first.
func countKinds(cols *eventCols) (out []kindCount) {
	return countValues(cols.Kind)
}

// countValues tallies values, most frequent first, ties by value.
func countValues(values []string) (out []kindCount) {
	idx := make(map[string]int, 8)
	for _, k := range values {
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, kindCount{kind: k})
		}
		out[i].count++
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].kind < out[j].kind
	})
	return
}

// runSummary folds an app's sessions across processes.
type runSummary struct {
	sessions int
	runs     int
	open     int
	// lastMs is the latest start or stop, 0 when there is none.
	lastMs int64
}

func summarizeRuns(cols *runCols) (s runSummary) {
	s.sessions = len(cols.RunId)
	seen := make(map[string]struct{}, 8)
	for i, r := range cols.RunId {
		seen[r] = struct{}{}
		if cols.StoppedMs[i] == 0 {
			s.open++
		}
		s.lastMs = max(s.lastMs, cols.StartedMs[i], cols.StoppedMs[i])
	}
	s.runs = len(seen)
	return
}

// sessionEnd is where a session's bar ends: its close, else the last time
// its process was seen alive, else nowhere. closed says the end is a close
// rather than a lower bound.
func sessionEnd(startedMs, stoppedMs, seenMs int64) (endMs int64, closed bool) {
	switch {
	case stoppedMs > 0:
		return stoppedMs, true
	case seenMs > startedMs:
		return seenMs, false
	}
	return 0, false
}

// sessionLength is how long a session lasted: exact when it closed, a lower
// bound ("≥") when only its process's last sign of life is known, "" when
// either end is unknown.
func sessionLength(startedMs, stoppedMs, seenMs int64) string {
	endMs, closed := sessionEnd(startedMs, stoppedMs, seenMs)
	if startedMs == 0 || endMs == 0 || endMs < startedMs {
		return ""
	}
	d := (time.Duration(endMs-startedMs) * time.Millisecond).Round(time.Second).String()
	if !closed {
		return "≥ " + d
	}
	return d
}

// llmTotal folds the app's model calls.
type llmTotal struct {
	calls        int
	failed       int
	inputTokens  int64
	outputTokens int64
}

func sumLlm(cols *llmCols) (t llmTotal) {
	t.calls = len(cols.At)
	for i := range cols.At {
		t.inputTokens += cols.InputTokens[i]
		t.outputTokens += cols.OutputTokens[i]
		if cols.Refused[i] || cols.Error[i] != "" {
			t.failed++
		}
	}
	return
}
