//go:build llm_generated_opus46

package repo

import (
	"context"
	"iter"
	"sort"

	"github.com/stergiotis/boxer/public/observability/eh"
)

type BugHotspotRecord struct {
	FilePath    string
	BugFixCount int
}

type BugHotspotAnalyzer struct {
	Since   string
	Until   string
	TopN    int
	Pattern string
}

func (inst *BugHotspotAnalyzer) pattern() (p string) {
	p = inst.Pattern
	if p == "" {
		p = "^(fix|hotfix):"
	}
	return
}

func (inst *BugHotspotAnalyzer) Run(ctx context.Context, git *GitRunner) iter.Seq2[BugHotspotRecord, error] {
	return func(yield func(BugHotspotRecord, error) bool) {
		counts := make(map[string]int, 256)
		args := git.buildLogArgs("", inst.Since, inst.Until, "--name-only", "-i", "-E", "--grep="+inst.pattern())
		for line, err := range git.RunLines(ctx, args...) {
			if err != nil {
				yield(BugHotspotRecord{}, eh.Errorf("unable to read git log: %w", err))
				return
			}
			if line == "" {
				continue
			}
			counts[line]++
		}

		records := make([]BugHotspotRecord, 0, len(counts))
		for path, count := range counts {
			records = append(records, BugHotspotRecord{
				FilePath:    path,
				BugFixCount: count,
			})
		}
		sort.Slice(records, func(i, j int) bool {
			return records[i].BugFixCount > records[j].BugFixCount
		})

		topN := inst.TopN
		if topN <= 0 || topN > len(records) {
			topN = len(records)
		}
		for _, r := range records[:topN] {
			if !yield(r, nil) {
				return
			}
		}
	}
}
