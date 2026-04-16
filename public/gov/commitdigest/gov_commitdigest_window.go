//go:build llm_generated_opus46

package commitdigest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

type SlidingWindow struct {
	MaxSummaries int32
	Summaries    []string
	Dir          string
	nextIndex    int32
}

func (inst *SlidingWindow) maxSummaries() (k int32) {
	k = inst.MaxSummaries
	if k <= 0 {
		k = 3
	}
	return
}

func (inst *SlidingWindow) LoadFromDir() (err error) {
	if inst.Dir == "" {
		return
	}
	entries, readErr := os.ReadDir(inst.Dir)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return
		}
		err = eb.Build().Str("dir", inst.Dir).Errorf("unable to read summaries directory: %w", readErr)
		return
	}

	names := make([]string, 0, len(entries))
	var maxIndex int32
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "summary_") && strings.HasSuffix(name, ".md") {
			names = append(names, name)
			// extract the numeric index: "summary_0028_2026-04-01_2026-04-12.md" → "0028"
			rest := strings.TrimPrefix(name, "summary_")
			indexStr, _, _ := strings.Cut(rest, "_")
			if strings.HasSuffix(indexStr, ".md") {
				indexStr = strings.TrimSuffix(indexStr, ".md")
			}
			num, parseErr := strconv.ParseInt(indexStr, 10, 32)
			if parseErr == nil && int32(num) >= maxIndex {
				maxIndex = int32(num) + 1
			}
		}
	}
	sort.Strings(names)
	inst.nextIndex = maxIndex

	inst.Summaries = make([]string, 0, len(names))
	for _, name := range names {
		var data []byte
		data, err = os.ReadFile(filepath.Join(inst.Dir, name))
		if err != nil {
			err = eb.Build().Str("file", name).Errorf("unable to read summary file: %w", err)
			return
		}
		content := strings.TrimSpace(string(data))
		if content != "" {
			inst.Summaries = append(inst.Summaries, content)
		}
	}

	// keep only the most recent k summaries
	k := int(inst.maxSummaries())
	if len(inst.Summaries) > k {
		inst.Summaries = inst.Summaries[len(inst.Summaries)-k:]
	}
	return
}

func (inst *SlidingWindow) Push(summary string) {
	inst.Summaries = append(inst.Summaries, summary)
	k := int(inst.maxSummaries())
	if len(inst.Summaries) > k {
		inst.Summaries = inst.Summaries[len(inst.Summaries)-k:]
	}
}

func (inst *SlidingWindow) Persist(chunkIndex int32, commits []CommitEntry) (err error) {
	if inst.Dir == "" {
		return
	}
	err = os.MkdirAll(inst.Dir, 0o755)
	if err != nil {
		err = eb.Build().Str("dir", inst.Dir).Errorf("unable to create summaries directory: %w", err)
		return
	}
	if len(inst.Summaries) == 0 {
		return
	}
	latest := inst.Summaries[len(inst.Summaries)-1]
	globalIndex := inst.nextIndex + chunkIndex

	var name string
	if len(commits) > 0 {
		// commits are newest-first from git log; last entry is oldest, first is newest
		oldest := extractDatePrefix(commits[len(commits)-1].Date)
		newest := extractDatePrefix(commits[0].Date)
		name = fmt.Sprintf("summary_%04d_%s_%s.md", globalIndex, oldest, newest)
	} else {
		name = fmt.Sprintf("summary_%04d.md", globalIndex)
	}

	err = os.WriteFile(filepath.Join(inst.Dir, name), []byte(latest+"\n"), 0o644)
	if err != nil {
		err = eb.Build().Str("file", name).Errorf("unable to write summary file: %w", err)
		return
	}
	return
}

// extractDatePrefix returns the YYYY-MM-DD portion from a git date string like "2026-04-12 23:57:45 +0200".
func extractDatePrefix(gitDate string) (date string) {
	date, _, _ = strings.Cut(gitDate, " ")
	if date == "" {
		date = "unknown"
	}
	return
}

func (inst *SlidingWindow) RenderContext() (context string) {
	if len(inst.Summaries) == 0 {
		return
	}
	var sb strings.Builder
	sb.Grow(512)
	for i, s := range inst.Summaries {
		if i > 0 {
			_, _ = sb.WriteString("\n---\n")
		}
		_, _ = sb.WriteString(s)
	}
	context = sb.String()
	return
}

func (inst *SlidingWindow) ContextTokenCount(counter TokenCounterI) (count int64) {
	ctx := inst.RenderContext()
	if ctx == "" {
		return
	}
	count = counter.CountTokens(ctx)
	return
}
