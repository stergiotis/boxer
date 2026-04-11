//go:build llm_generated_opus46

package repo

import (
	"context"
	"iter"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

type AuthorshipRecord struct {
	Month      string
	HumanLines int
	LLMLines   int
	TotalFiles int
	LLMFiles   int
}

type AuthorshipAnalyzer struct{}

func (inst *AuthorshipAnalyzer) Run(ctx context.Context, git *GitRunner) iter.Seq2[AuthorshipRecord, error] {
	return func(yield func(AuthorshipRecord, error) bool) {
		// Step 1: get one commit hash per month (last commit of each month)
		type monthCommit struct {
			month string
			hash  string
		}
		seen := make(map[string]int, 64) // month -> index in commits
		commits := make([]monthCommit, 0, 64)
		for line, err := range git.RunLines(ctx, "log", "--format=%H %ai", "--reverse", "--", "*.go") {
			if err != nil {
				yield(AuthorshipRecord{}, eh.Errorf("unable to read git log: %w", err))
				return
			}
			if len(line) < 48 {
				continue
			}
			hash := line[:40]
			month := line[41:48] // "YYYY-MM"
			if idx, ok := seen[month]; ok {
				commits[idx].hash = hash
			} else {
				seen[month] = len(commits)
				commits = append(commits, monthCommit{month: month, hash: hash})
			}
		}
		sort.Slice(commits, func(i, j int) bool {
			return commits[i].month < commits[j].month
		})

		// Step 2: for each monthly snapshot, count human vs LLM lines
		for _, mc := range commits {
			rec, err := inst.snapshot(ctx, git, mc.hash)
			if err != nil {
				yield(AuthorshipRecord{}, eh.Errorf("snapshot %s (%s) failed: %w", mc.month, mc.hash[:8], err))
				return
			}
			rec.Month = mc.month
			if !yield(rec, nil) {
				return
			}
		}
	}
}

func (inst *AuthorshipAnalyzer) snapshot(ctx context.Context, git *GitRunner, hash string) (rec AuthorshipRecord, err error) {
	// List all files in tree
	var files []string
	for line, iterErr := range git.RunLines(ctx, "ls-tree", "-r", "--name-only", hash) {
		if iterErr != nil {
			err = eh.Errorf("ls-tree failed: %w", iterErr)
			return
		}
		if !strings.HasSuffix(line, ".go") {
			continue
		}
		if strings.Contains(line, ".gen.") || strings.Contains(line, ".out.") || strings.Contains(line, "golay24") {
			continue
		}
		files = append(files, line)
	}

	rec.TotalFiles = len(files)

	// Count lines per file, check first line for LLM tag
	for _, path := range files {
		lineCount := 0
		isLLM := false
		for line, iterErr := range git.RunLines(ctx, "show", hash+":"+path) {
			if iterErr != nil {
				err = eh.Errorf("show %s failed: %w", path, iterErr)
				return
			}
			if lineCount == 0 {
				isLLM = strings.HasPrefix(line, "//go:build llm_generated")
			}
			lineCount++
		}
		if isLLM {
			rec.LLMLines += lineCount
			rec.LLMFiles++
		} else {
			rec.HumanLines += lineCount
		}
	}
	return
}
