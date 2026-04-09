//go:build llm_generated_opus46

package repo

import (
	"context"
	"iter"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

type ContributorRecord struct {
	Author      string
	CommitCount int
	Percentage  float64
}

type BusFactorResult struct {
	BusFactor    int
	TotalCommits int
	Contributors []ContributorRecord
}

type ContributorAnalyzer struct {
	Since string
	Until string
	TopN  int
}

func (inst *ContributorAnalyzer) Run(ctx context.Context, git *GitRunner) iter.Seq2[ContributorRecord, error] {
	return func(yield func(ContributorRecord, error) bool) {
		records, err := inst.collect(ctx, git)
		if err != nil {
			yield(ContributorRecord{}, err)
			return
		}
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

func (inst *ContributorAnalyzer) RunSummary(ctx context.Context, git *GitRunner) (result BusFactorResult, err error) {
	var records []ContributorRecord
	records, err = inst.collect(ctx, git)
	if err != nil {
		return
	}

	totalCommits := 0
	for _, r := range records {
		totalCommits += r.CommitCount
	}

	busFactor := 0
	cumulative := 0
	threshold := (totalCommits + 1) / 2
	for _, r := range records {
		busFactor++
		cumulative += r.CommitCount
		if cumulative >= threshold {
			break
		}
	}

	result = BusFactorResult{
		BusFactor:    busFactor,
		TotalCommits: totalCommits,
		Contributors: records,
	}
	return
}

func (inst *ContributorAnalyzer) collect(ctx context.Context, git *GitRunner) (records []ContributorRecord, err error) {
	args := make([]string, 0, 8)
	args = append(args, "shortlog", "-sne", "--no-merges")
	if inst.Since != "" {
		args = append(args, "--since="+inst.Since)
	}
	if inst.Until != "" {
		args = append(args, "--until="+inst.Until)
	}
	args = append(args, "HEAD")

	totalCommits := 0
	records = make([]ContributorRecord, 0, 32)
	for line, lineErr := range git.RunLines(ctx, args...) {
		if lineErr != nil {
			err = eh.Errorf("unable to read git shortlog: %w", lineErr)
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// format: "  123\tAuthor Name <email>"
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		var count int
		count, err = strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			err = eh.Errorf("unable to parse commit count %q: %w", parts[0], err)
			return
		}
		totalCommits += count
		records = append(records, ContributorRecord{
			Author:      parts[1],
			CommitCount: count,
		})
	}

	if totalCommits > 0 {
		for i := range records {
			records[i].Percentage = float64(records[i].CommitCount) / float64(totalCommits) * 100.0
		}
	}
	return
}
