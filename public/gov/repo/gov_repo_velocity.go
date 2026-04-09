//go:build llm_generated_opus46

package repo

import (
	"context"
	"iter"
	"sort"

	"github.com/stergiotis/boxer/public/observability/eh"
)

type VelocityRecord struct {
	Month       string
	CommitCount int
}

type VelocityAnalyzer struct {
	Since string
	Until string
}

func (inst *VelocityAnalyzer) Run(ctx context.Context, git *GitRunner) iter.Seq2[VelocityRecord, error] {
	return func(yield func(VelocityRecord, error) bool) {
		counts := make(map[string]int, 64)
		args := git.buildLogArgs("%ad", inst.Since, inst.Until, "--date=format:%Y-%m")
		for line, err := range git.RunLines(ctx, args...) {
			if err != nil {
				yield(VelocityRecord{}, eh.Errorf("unable to read git log: %w", err))
				return
			}
			if line == "" {
				continue
			}
			counts[line]++
		}

		records := make([]VelocityRecord, 0, len(counts))
		for month, count := range counts {
			records = append(records, VelocityRecord{
				Month:       month,
				CommitCount: count,
			})
		}
		sort.Slice(records, func(i, j int) bool {
			return records[i].Month < records[j].Month
		})

		for _, r := range records {
			if !yield(r, nil) {
				return
			}
		}
	}
}
