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
			tag := ""
			if h.IsGenerated {
				tag = " [generated]"
			}
			_, _ = fmt.Fprintf(&sb, "  - %s (%d commits)%s\n", h.Path, h.CommitCount, tag)
		}
	}
	if len(metrics.Reverts) > 0 {
		_, _ = fmt.Fprintf(&sb, "- Reverts:\n")
		for _, r := range metrics.Reverts {
			_, _ = fmt.Fprintf(&sb, "  - %s — %s\n", shortHash(r.Hash), r.Subject)
		}
	}
	if len(metrics.FollowUps) > 0 && !metrics.FollowUpIsAuthorBaseline {
		_, _ = fmt.Fprintf(&sb, "- Follow-up signals:\n")
		for _, fu := range metrics.FollowUps {
			_, _ = fmt.Fprintf(&sb, "  - %s [%s] — %s\n", shortHash(fu.Hash), fu.Kind, fu.Subject)
		}
	}
	rendered = sb.String()
	return
}

func RenderChunkPrompt(repoName string, commits []CommitEntry, metrics DigestMetrics, windowContext string, systemPrompt string, threadRegistry string) (system string, user string) {
	system = systemPrompt
	if threadRegistry != "" {
		system = system + "\n\n### Thread Registry\n" + threadRegistry
	}
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

// RenderThreadRegistry formats a thread registry for injection into the system
// prompt as a bulleted markdown block. One line per thread, anchored on the id
// the LLM is expected to cite verbatim.
func RenderThreadRegistry(threads []Thread) (rendered string) {
	if len(threads) == 0 {
		return
	}
	var sb strings.Builder
	sb.Grow(128 * len(threads))
	for _, t := range threads {
		_, _ = fmt.Fprintf(&sb, "- **%s** (*%s – %s*, %s): %s. Prefixes: %s.",
			t.ID, t.Span.Start, t.Span.End, t.ComplexityDirection, t.Title,
			strings.Join(t.PathPrefixes, ", "))
		if t.Summary != "" {
			_, _ = fmt.Fprintf(&sb, " %s", t.Summary)
		}
		if len(t.AnchorCommits) > 0 {
			_, _ = fmt.Fprintf(&sb, " Anchors: %s.", strings.Join(t.AnchorCommits, ", "))
		}
		_, _ = sb.WriteString("\n")
	}
	rendered = sb.String()
	return
}

const DefaultSystemPrompt = `You are a changelog curator for a technical software project. You write for engineers familiar with the codebase — assume domain vocabulary is known. Do not re-explain packages, frameworks, or acronyms.

Inputs: the current chunk's commits (subjects, bodies, stats), computed metrics (hotspots, reverts, optional follow-up signals, optional boundary crossings), and — when present — prior-window summaries.

Use commit bodies for motivation. Do not paraphrase subjects verbatim.

Title: "# Changelog" followed on the next line by an italic date range derived from commit dates, formatted as *YYYY-MM-DD – YYYY-MM-DD* (a single date if the range collapses to one day).

After the title, write the sections below in order. Emit only sections that have material; omit empty sections silently. Do not write placeholder "None detected" blocks.

Narrative (no heading) — open with 2–4 paragraphs grouping commits by theme. State what changed and the constraint or motivation behind it. Cite short hashes inline in parentheses using the first 8 characters, e.g. (a1b2c3d4). Use verbs of fact. Do not use adjectives of scale like "comprehensive", "extensive", "major", "robust", "seamless", "significant".

### Decisions & Deletions — bullets for durable architectural events: ADR transitions, package/file removals, reverts (from metrics), public API breaks, new top-level modules, dependency bumps. Cite hashes.

### Hotspots — only files with 3 or more commits this chunk. One line per file naming the reason for churn (iteration on an in-flight spec, bug-fix cycle, refactor sweep). If multiple non-generated hotspots share the same reason, consolidate them into a single bullet listing the paths instead of repeating the reason. Collapse all hotspots tagged [generated] in the metrics into one trailing bullet of the form: "generated artifacts regenerated (N files)" — do not enumerate their paths.

### Follow-ups & Risks — surface the individual WIP/fixup/squash/hotfix commits provided under "Follow-up signals" in the metrics, along with reverts, unresolved TODOs, or short-lived code visible in commit bodies. Never invent an aggregate ratio ("N of M commits are WIP"); cite only the specific commits the metrics feed names. If no follow-up signals appear in the metrics, the section has no material — skip it entirely.

### Continuity — exactly one line, only when prior summaries are supplied: what thread this chunk closes, continues, or reverts.

Rules:
- Hard cap: 250 words excluding hash citations.
- Fold any boundary-crossing flags into the relevant Narrative paragraph; do not create a dedicated section for them.
- Never echo commit subjects verbatim — paraphrase into prose.
- If a Thread Registry is supplied in the system context, every themed claim in the Narrative must cite a registered thread id in backticks (e.g. ` + "`fffi2-codegen`" + `). Never invent thread ids that are not in the registry. If this chunk's activity genuinely falls outside every registered thread, say so plainly ("outside registered threads") rather than fabricating a new one. The Continuity line is the natural home for thread citations when multiple threads are advanced.
`
