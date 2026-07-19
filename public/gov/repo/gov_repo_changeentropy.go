package repo

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// EntropyFact is one (month, file) feature-change line total: the number of
// lines the file's Feature-Introduction commits modified in Month. It is the
// feature-extraction output for change-entropy analysis (Hassan, ICSE 2009) —
// the per-period modified-line distribution whose Shannon entropy the consumer
// computes (this repository does it as SQL in clickhouse-local).
type EntropyFact struct {
	Month         string `json:"month"` // "YYYY-MM" of the author date
	Path          string `json:"path"`
	ModifiedLines int64  `json:"modifiedLines"` // added + deleted, summed over the month's feature commits
}

// ChangeEntropyAnalyzer extracts the per-(month, file) feature-change line churn
// for change-entropy analysis. It walks the non-merge history with --numstat,
// keeps only Feature-Introduction commits (per the injected IsFeatureCommit
// classifier — Hassan's protocol excludes General-Maintenance and
// Fault-Repairing commits from the entropy), and sums each file's added+deleted
// lines per author-month.
//
// The zero value keeps every commit (IsFeatureCommit nil ⇒ all) and every path.
// Rename ("old => new" numstat) and binary ("-" numstat) rows are skipped; the
// file need not survive at HEAD, since entropy measures the modification
// process at the time, not current ownership.
type ChangeEntropyAnalyzer struct {
	// PathFilter limits which files participate; nil accepts all.
	PathFilter func(path string) bool
	// IsFeatureCommit classifies a commit subject as a Feature-Introduction
	// commit. nil keeps every commit (no semantic gating).
	IsFeatureCommit func(subject string) bool
}

// ExtractLineChurn walks history and returns the per-(month, file) feature line
// churn (aggregated, sorted by month then path), plus the number of
// Feature-Introduction commits that contributed lines and the total non-merge
// commit count — the context the entropy is computed against.
func (inst *ChangeEntropyAnalyzer) ExtractLineChurn(ctx context.Context, git *GitRunner) (facts []EntropyFact, featureCommits int, totalCommits int, err error) {
	const headerSep = "\x01"
	const fieldSep = "\x1f"

	type key struct{ month, path string }
	agg := make(map[key]int64, 1024)

	var curMonth string
	var curFeature bool
	var curContributed bool
	finishCommit := func() {
		if curFeature && curContributed {
			featureCommits++
		}
		curContributed = false
	}

	for line, iterErr := range git.RunLines(ctx, "log", "--no-merges", "-M", "--numstat",
		"--date=format:%Y-%m", "--format="+headerSep+"%ad"+fieldSep+"%s") {
		if iterErr != nil {
			err = eh.Errorf("unable to read git log: %w", iterErr)
			return
		}
		if strings.HasPrefix(line, headerSep) {
			finishCommit() // close the previous commit
			totalCommits++
			parts := strings.SplitN(line[len(headerSep):], fieldSep, 2)
			if len(parts) != 2 {
				curMonth, curFeature = "", false
				continue
			}
			curMonth = parts[0]
			curFeature = inst.IsFeatureCommit == nil || inst.IsFeatureCommit(parts[1])
			continue
		}
		if !curFeature || curMonth == "" || line == "" {
			continue
		}
		// numstat row: added \t deleted \t path
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) != 3 || cols[0] == "-" {
			continue // header gap or binary file ("-" columns)
		}
		path := cols[2]
		if strings.Contains(path, " => ") {
			continue // rename row — the "old => new" path syntax is skipped
		}
		if inst.PathFilter != nil && !inst.PathFilter(path) {
			continue
		}
		added, e1 := strconv.Atoi(cols[0])
		deleted, e2 := strconv.Atoi(cols[1])
		if e1 != nil || e2 != nil || added+deleted == 0 {
			continue
		}
		agg[key{curMonth, path}] += int64(added + deleted)
		curContributed = true
	}
	finishCommit() // the last commit has no trailing header to close it

	facts = make([]EntropyFact, 0, len(agg))
	for k, lines := range agg {
		facts = append(facts, EntropyFact{Month: k.month, Path: k.path, ModifiedLines: lines})
	}
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].Month != facts[j].Month {
			return facts[i].Month < facts[j].Month
		}
		return facts[i].Path < facts[j].Path
	})
	return
}
