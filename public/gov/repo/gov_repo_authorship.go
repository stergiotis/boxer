package repo

import (
	"context"
	"iter"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// AuthorshipRecord holds cumulative authored-line counts through the end of
// Month, split by author type (human vs LLM) and code vs test files.
//
// Attribution reads git history directly: a commit's added lines count as LLM
// when it carries a Co-Authored-By trailer naming a known code-generation
// model, otherwise human. This is the provenance source of record since the
// llm_generated_* build tags were retired. Counts are cumulative additions
// (deletions are not subtracted), so the final record approximates current
// authorship volume and the series shows how authorship shifted over the
// project's life.
type AuthorshipRecord struct {
	Month          string
	HumanLines     int
	LLMLines       int
	TotalFiles     int
	LLMFiles       int
	HumanTestLines int
	LLMTestLines   int
	TotalTestFiles int
	LLMTestFiles   int
}

type AuthorshipAnalyzer struct{}

// commitIsLLM reports whether a commit's joined Co-Authored-By trailer values
// name a known code-generation model. Matching the vendor family (rather than
// exact model strings) keeps successive model revisions attributed without a
// registry to maintain — the failure mode the per-model build tags suffered.
func commitIsLLM(coauthors string) bool {
	s := strings.ToLower(coauthors)
	return strings.Contains(s, "claude") || strings.Contains(s, "gemini")
}

// monthAgg accumulates one month's additions plus the count of files first
// seen that month in each category, so a cumulative sweep yields per-month
// running totals for both lines and distinct files.
type monthAgg struct {
	month                    string
	humanLines, llmLines     int
	humanTest, llmTest       int
	newTotalCode, newLLMCode int
	newTotalTest, newLLMTest int
}

func (inst *AuthorshipAnalyzer) Run(ctx context.Context, git *GitRunner) iter.Seq2[AuthorshipRecord, error] {
	return func(yield func(AuthorshipRecord, error) bool) {
		months := make([]*monthAgg, 0, 64)
		idx := make(map[string]int, 64)
		agg := func(m string) *monthAgg {
			if i, ok := idx[m]; ok {
				return months[i]
			}
			a := &monthAgg{month: m}
			idx[m] = len(months)
			months = append(months, a)
			return a
		}

		// Distinct-file tracking, so LLMFiles counts files that ever received
		// an LLM-authored addition (the post-tag analogue of a tagged file).
		seenCode := make(map[string]struct{}, 1024)
		seenCodeLLM := make(map[string]struct{}, 1024)
		seenTest := make(map[string]struct{}, 1024)
		seenTestLLM := make(map[string]struct{}, 1024)

		const sep = "\x01"
		var curMonth string
		var curLLM bool
		// --reverse walks oldest-first so months accrue in order. Each commit
		// header line is prefixed with sep; the lines between headers are the
		// --numstat rows (added \t deleted \t path) for that commit.
		for line, err := range git.RunLines(ctx, "log", "--reverse", "--no-merges",
			"--numstat", "--date=format:%Y-%m",
			"--format="+sep+"%H\t%ad\t%(trailers:key=Co-authored-by,valueonly,separator=\x1f)",
			"--", "*.go") {
			if err != nil {
				yield(AuthorshipRecord{}, eh.Errorf("unable to read git log: %w", err))
				return
			}
			if strings.HasPrefix(line, sep) {
				parts := strings.SplitN(line[len(sep):], "\t", 3)
				if len(parts) < 2 {
					continue
				}
				curMonth = parts[1]
				coauthors := ""
				if len(parts) == 3 {
					coauthors = parts[2]
				}
				curLLM = commitIsLLM(coauthors)
				continue
			}
			if curMonth == "" {
				continue
			}
			cols := strings.SplitN(line, "\t", 3)
			if len(cols) < 3 || cols[0] == "-" {
				continue // header gap or binary file
			}
			path := cols[2]
			if !strings.HasSuffix(path, ".go") || strings.Contains(path, " => ") {
				continue // non-Go or rename row
			}
			if strings.Contains(path, ".gen.") || strings.Contains(path, ".out.") || strings.Contains(path, "golay24") {
				continue // generated
			}
			added, convErr := strconv.Atoi(cols[0])
			if convErr != nil {
				continue
			}
			a := agg(curMonth)
			if strings.HasSuffix(path, "_test.go") {
				if curLLM {
					a.llmTest += added
				} else {
					a.humanTest += added
				}
				if _, ok := seenTest[path]; !ok {
					seenTest[path] = struct{}{}
					a.newTotalTest++
				}
				if curLLM {
					if _, ok := seenTestLLM[path]; !ok {
						seenTestLLM[path] = struct{}{}
						a.newLLMTest++
					}
				}
			} else {
				if curLLM {
					a.llmLines += added
				} else {
					a.humanLines += added
				}
				if _, ok := seenCode[path]; !ok {
					seenCode[path] = struct{}{}
					a.newTotalCode++
				}
				if curLLM {
					if _, ok := seenCodeLLM[path]; !ok {
						seenCodeLLM[path] = struct{}{}
						a.newLLMCode++
					}
				}
			}
		}

		sort.Slice(months, func(i, j int) bool { return months[i].month < months[j].month })

		var rec AuthorshipRecord
		for _, a := range months {
			rec.Month = a.month
			rec.HumanLines += a.humanLines
			rec.LLMLines += a.llmLines
			rec.HumanTestLines += a.humanTest
			rec.LLMTestLines += a.llmTest
			rec.TotalFiles += a.newTotalCode
			rec.LLMFiles += a.newLLMCode
			rec.TotalTestFiles += a.newTotalTest
			rec.LLMTestFiles += a.newLLMTest
			if !yield(rec, nil) {
				return
			}
		}
	}
}
