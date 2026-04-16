//go:build llm_generated_opus46

package commitdigest

import (
	"fmt"
	"strings"
)

func shortHash(hash string) (short string) {
	if len(hash) > 12 {
		short = hash[:12]
	} else {
		short = hash
	}
	return
}

func RenderCommitEntry(commit CommitEntry) (rendered string) {
	var sb strings.Builder
	sb.Grow(256)
	_, _ = fmt.Fprintf(&sb, "## %s — %s (%s)\n%s\n", shortHash(commit.Hash), commit.Subject, commit.Author, commit.Date)
	if commit.Body != "" {
		_, _ = fmt.Fprintf(&sb, "\n%s\n", commit.Body)
	}
	if commit.Stat != "" {
		_, _ = fmt.Fprintf(&sb, "```\n%s\n```\n", commit.Stat)
	}
	rendered = sb.String()
	return
}

func RenderRepoHeader(repoName string) (rendered string) {
	rendered = fmt.Sprintf("# %s\n", repoName)
	return
}

func RenderMetricsSection(metrics DigestMetrics) (rendered string) {
	var sb strings.Builder
	sb.Grow(256)
	_, _ = fmt.Fprintf(&sb, "\n### Metrics\n")
	_, _ = fmt.Fprintf(&sb, "- Total commits: %d\n", metrics.TotalCommits)
	_, _ = fmt.Fprintf(&sb, "- Unique authors: %d\n", metrics.UniqueAuthors)
	if len(metrics.BoundaryCrossings) > 0 {
		_, _ = fmt.Fprintf(&sb, "- Ownership boundary crossings: %d\n", len(metrics.BoundaryCrossings))
		for _, bc := range metrics.BoundaryCrossings {
			_, _ = fmt.Fprintf(&sb, "  - %s modified `%s` (owners: %s)\n",
				bc.Author, bc.File, strings.Join(bc.Owners, ", "))
		}
	}
	if len(metrics.IterationHotspots) > 0 {
		_, _ = fmt.Fprintf(&sb, "- Iteration hotspots:\n")
		for _, h := range metrics.IterationHotspots {
			_, _ = fmt.Fprintf(&sb, "  - %s (%d commits)\n", h.Path, h.CommitCount)
		}
	}
	rendered = sb.String()
	return
}

func RenderChunkPrompt(repoName string, commits []CommitEntry, metrics DigestMetrics, windowContext string, systemPrompt string) (system string, user string) {
	system = systemPrompt
	if windowContext != "" {
		system = system + "\n\n### Prior Summaries\n" + windowContext
	}

	var sb strings.Builder
	sb.Grow(1024)
	_, _ = sb.WriteString(RenderRepoHeader(repoName))
	for _, c := range commits {
		_, _ = sb.WriteString("\n")
		_, _ = sb.WriteString(RenderCommitEntry(c))
	}
	_, _ = sb.WriteString(RenderMetricsSection(metrics))
	user = sb.String()
	return
}

const DefaultSystemPrompt = `You are a changelog summarizer. Given a set of git commits and repository metrics, produce a concise changelog summary.
Focus on:
- What changed and why (group related commits)
- Ownership boundary crossings (files modified by someone outside the usual author group — flag these for review attention)
- Iteration hotspots (files with high churn)
Keep the summary under 300 words. Use markdown formatting. Include the date range in below the title.`
