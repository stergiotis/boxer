//go:build llm_generated_opus46

package commitdigest

import (
	"regexp"
	"sort"
	"strings"
)

type ForeignCommit struct {
	Hash   string
	Author string
	Date   string
}

type IterationHotspot struct {
	Path        string
	CommitCount int32
}

type DigestMetrics struct {
	ForeignCommits    []ForeignCommit
	IterationHotspots []IterationHotspot
	TotalCommits      int32
	UniqueAuthors     int32
}

type MetricsConfig struct {
	KnownAuthors []string
	HotspotTopN  int32
}

var statLineRe = regexp.MustCompile(`^\s*(\S+)\s*\|`)

func ComputeMetrics(commits []CommitEntry, config MetricsConfig) (metrics DigestMetrics) {
	metrics.TotalCommits = int32(len(commits))

	knownSet := make(map[string]struct{}, len(config.KnownAuthors))
	for _, a := range config.KnownAuthors {
		knownSet[strings.ToLower(strings.TrimSpace(a))] = struct{}{}
	}
	hasKnownAuthors := len(knownSet) > 0

	authorSet := make(map[string]struct{}, 16)
	fileCounts := make(map[string]int32, 64)
	metrics.ForeignCommits = make([]ForeignCommit, 0, 4)

	for _, c := range commits {
		email := extractCommitEmail(c.Author)
		authorSet[email] = struct{}{}

		if hasKnownAuthors {
			if !isKnownAuthor(email, c.Author, knownSet) {
				metrics.ForeignCommits = append(metrics.ForeignCommits, ForeignCommit{
					Hash:   c.Hash,
					Author: c.Author,
					Date:   c.Date,
				})
			}
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
			})
		}
	}
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

func isKnownAuthor(email string, fullAuthor string, knownSet map[string]struct{}) (known bool) {
	if _, ok := knownSet[email]; ok {
		known = true
		return
	}
	lowerAuthor := strings.ToLower(fullAuthor)
	for k := range knownSet {
		if strings.Contains(lowerAuthor, k) {
			known = true
			return
		}
	}
	return
}
