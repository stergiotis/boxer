//go:build llm_generated_opus46

package commitdigest

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type IterationHotspot struct {
	Path        string `json:"path"`
	CommitCount int32  `json:"commitCount"`
	IsGenerated bool   `json:"isGenerated,omitempty"`
}

// RevertSignal flags a commit whose subject indicates it reverts earlier work.
// Surfaced so the summarizer can list them under Decisions & Deletions.
type RevertSignal struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

// FollowUpSignal flags a commit whose subject indicates unfinished work:
// wip/fixup/squash/hotfix language. Kind is one of "wip", "fixup", "squash",
// "hotfix". Surfaced so the summarizer can list them under Follow-ups & Risks.
type FollowUpSignal struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Kind    string `json:"kind"`
}

type DigestMetrics struct {
	BoundaryCrossings []BoundaryCrossing `json:"boundaryCrossings,omitempty"`
	IterationHotspots []IterationHotspot `json:"iterationHotspots,omitempty"`
	Reverts           []RevertSignal     `json:"reverts,omitempty"`
	FollowUps         []FollowUpSignal   `json:"followUps,omitempty"`
	TotalCommits      int32              `json:"totalCommits"`
	UniqueAuthors     int32              `json:"uniqueAuthors"`
	// FollowUpIsAuthorBaseline is true when at least half the chunk's commits
	// carry a follow-up signal. In that regime WIP/fixup prefixes are the
	// author's routine commit cadence, not a risk signal — the renderer
	// suppresses the follow-up bullet so the summarizer does not report a
	// meaningless aggregate like "N of M commits are WIP".
	FollowUpIsAuthorBaseline bool `json:"followUpIsAuthorBaseline,omitempty"`
}

type MetricsConfig struct {
	HotspotTopN int32
}

var statLineRe = regexp.MustCompile(`^\s*(\S+)\s*\|`)
var revertSubjectRe = regexp.MustCompile(`(?i)^revert(\s|[:"])`)
var fixupSubjectRe = regexp.MustCompile(`^fixup!\s`)
var squashSubjectRe = regexp.MustCompile(`^squash!\s`)
var wipSubjectRe = regexp.MustCompile(`(?i)\bwip\b`)
var hotfixSubjectRe = regexp.MustCompile(`(?i)\b(hack|quick\s*fix|hot\s*fix|tempfix)\b`)

// generatedBasenameRe matches filenames whose basename carries a generator
// suffix separated from the stem by either `.` or `_` — covering boxer's
// `.out.go` / `.gen.go` convention and the `_out.rs` convention used by
// Rust-side codegen in downstream projects.
var generatedBasenameRe = regexp.MustCompile(`(?:\.|_)(?:out|gen)\.[^./]+$`)

func ComputeMetrics(commits []CommitEntry, config MetricsConfig) (metrics DigestMetrics) {
	metrics.TotalCommits = int32(len(commits))

	authorSet := make(map[string]struct{}, 16)
	fileCounts := make(map[string]int32, 64)

	for _, c := range commits {
		email := extractCommitEmail(c.Author)
		authorSet[email] = struct{}{}

		subject := strings.TrimSpace(c.Subject)
		if revertSubjectRe.MatchString(subject) {
			metrics.Reverts = append(metrics.Reverts, RevertSignal{
				Hash:    c.Hash,
				Subject: subject,
			})
		}
		if kind := followUpKind(subject); kind != "" {
			metrics.FollowUps = append(metrics.FollowUps, FollowUpSignal{
				Hash:    c.Hash,
				Subject: subject,
				Kind:    kind,
			})
		}

		if c.Stat != "" {
			for _, line := range strings.Split(c.Stat, "\n") {
				m := statLineRe.FindStringSubmatch(line)
				if m != nil {
					path := m[1]
					// skip summary line like "3 files changed, ..."
					if !strings.Contains(line, "changed") {
						fileCounts[path]++
					}
				}
			}
		}
	}

	metrics.UniqueAuthors = int32(len(authorSet))

	if metrics.TotalCommits > 0 && int32(len(metrics.FollowUps))*2 >= metrics.TotalCommits {
		metrics.FollowUpIsAuthorBaseline = true
	}

	{ // build hotspots
		type entry struct {
			path  string
			count int32
		}
		entries := make([]entry, 0, len(fileCounts))
		for p, c := range fileCounts {
			entries = append(entries, entry{path: p, count: c})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].count > entries[j].count
		})
		topN := config.HotspotTopN
		if topN <= 0 {
			topN = 10
		}
		if int32(len(entries)) < topN {
			topN = int32(len(entries))
		}
		// only include files touched by more than one commit
		metrics.IterationHotspots = make([]IterationHotspot, 0, topN)
		for _, e := range entries[:topN] {
			if e.count <= 1 {
				break
			}
			metrics.IterationHotspots = append(metrics.IterationHotspots, IterationHotspot{
				Path:        e.path,
				CommitCount: e.count,
				IsGenerated: isGeneratedPath(e.path),
			})
		}
	}
	return
}

// followUpKind returns "fixup" / "squash" / "wip" / "hotfix" if the subject
// carries the corresponding signal, else the empty string. Fixup/squash take
// precedence over wip/hotfix because they are git's literal autosquash
// prefixes and therefore the strongest signal.
func followUpKind(subject string) (kind string) {
	switch {
	case fixupSubjectRe.MatchString(subject):
		kind = "fixup"
	case squashSubjectRe.MatchString(subject):
		kind = "squash"
	case wipSubjectRe.MatchString(subject):
		kind = "wip"
	case hotfixSubjectRe.MatchString(subject):
		kind = "hotfix"
	}
	return
}

func isGeneratedPath(path string) (generated bool) {
	generated = generatedBasenameRe.MatchString(filepath.Base(path))
	return
}

func extractCommitEmail(author string) (email string) {
	start := strings.LastIndexByte(author, '<')
	end := strings.LastIndexByte(author, '>')
	if start >= 0 && end > start {
		email = strings.ToLower(author[start+1 : end])
		return
	}
	email = strings.ToLower(author)
	return
}
