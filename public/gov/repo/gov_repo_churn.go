//go:build llm_generated_opus46

package repo

import (
	"context"
	"iter"
	"sort"

	"github.com/stergiotis/boxer/public/observability/eh"
)

type ChurnRecord struct {
	FilePath    string
	ChangeCount int
}

type ChurnAnalyzer struct {
	TopN  int
	Since string
	Until string
}

func (inst *ChurnAnalyzer) Run(ctx context.Context, git *GitRunner) iter.Seq2[ChurnRecord, error] {
	return func(yield func(ChurnRecord, error) bool) {
		counts := make(map[string]int, 256)
		args := git.buildLogArgs("", inst.Since, inst.Until, "--name-only")
		for line, err := range git.RunLines(ctx, args...) {
			if err != nil {
				yield(ChurnRecord{}, eh.Errorf("unable to read git log: %w", err))
				return
			}
			if line == "" {
				continue
			}
			counts[line]++
		}

		records := make([]ChurnRecord, 0, len(counts))
		for path, count := range counts {
			records = append(records, ChurnRecord{
				FilePath:    path,
				ChangeCount: count,
			})
		}
		sort.Slice(records, func(i, j int) bool {
			return records[i].ChangeCount > records[j].ChangeCount
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
