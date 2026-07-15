// Package adrcorpus reads the doc/adr corpus into a queryable model: one [Adr]
// per decision, the [Subtask] sub-items each one declares for itself, and every
// [CodeRef] citing an ADR number from source.
//
// It reports three deliberately separate axes:
//
//   - The decision lifecycle — frontmatter `status`. "Was this decided?"
//   - Implementation degree — evidence, derived by [ScanCodeRefs] and folded in
//     by [Aggregate]: how, where and to what depth an ADR's number is cited in
//     source. "Was this built, and how far?" An `accepted` ADR with zero code
//     references and a `proposed` ADR cited across a dozen packages are exactly
//     the drift cases this surfaces.
//   - Sub-item progress — how much of what an ADR decomposed itself into it
//     declares done. Unlike the evidence axis this one is *declared*, not
//     inferred; [Subtask] explains why.
//
// The package is a pure library over markdown: no CLI, no Arrow, no
// clickhouse. Those belong to its consumers — `boxer adr`
// (public/app/commands/adr) adds the Arrow tables and the clickhouse-local
// query surface, and the adrboard app renders the same model as a board — so
// neither a GUI app nor a lint has to link a command to read the corpus.
//
// The design, and the ADR-reference convention the evidence axis depends on,
// are recorded in ADR-0092.
package adrcorpus

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// Adr is one row of the `adr` table: the decision-lifecycle facts from
// frontmatter plus body-derived planning/freshness signals. The code-evidence
// fields (CodeRefs … ImplEvidence) are filled later by [Aggregate].
type Adr struct {
	Num            int
	Slug           string
	Title          string
	Path           string
	Status         string
	Date           string
	ReviewedBy     string
	ReviewedDate   string
	SupersededBy   string
	WithdrawnDate  string
	BodyBytes      int
	HasUpdate      bool
	UpdateCount    int
	LastDate       string // latest ISO date (frontmatter or body) that is not in the future
	PlanMarkers    []string
	PlanMaxPhase   int
	CodeRefs       int
	CodeFiles      int
	CodePkgs       int
	CodeLangs      []string
	CodeQualifiers []string
	ImplEvidence   string
	// Subtasks are the sub-items the ADR declares for itself. They are rows of
	// the separate `subtask` table, not a column of this one — [AllSubtasks]
	// flattens them; the rollups below stay here.
	Subtasks      []Subtask
	SubtasksTotal int
	SubtasksDone  int
	// SubtasksCited counts sub-items with at least one §-pinned citation,
	// filled by [Aggregate]. It is independent of SubtasksDone rather than
	// disjoint from it — a sub-item can be both declared done and cited — so
	// do not subtract one from the other. Read a per-sub-item verdict off
	// [Subtask.Done] and [Subtask.CodeRefs].
	SubtasksCited int
}

var (
	adrFileRe    = regexp.MustCompile(`^(\d{4})-(.+)\.md$`)
	fmDelimRe    = regexp.MustCompile(`(?m)^---[ \t]*$`)
	h1Re         = regexp.MustCompile(`(?m)^#\s+(?:ADR-\d+:\s*)?(.+?)\s*$`)
	planMarkerRe = regexp.MustCompile(`(?i)\b(Phase|Cut|Step|Milestone)[ -]?(\d+)\b`)
	mMarkerRe    = regexp.MustCompile(`\bM(\d+)\b`)
	updateHeadRe = regexp.MustCompile(`(?im)^#{2,5}\s+.*\bupdates?\b`)
	isoDateRe    = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2})\b`)
)

// ParseDir parses every NNNN-*.md file under dir into Adr rows, sorted by Num.
func ParseDir(dir string) (adrs []Adr, err error) {
	var entries []os.DirEntry
	entries, err = os.ReadDir(dir)
	if err != nil {
		return nil, eh.Errorf("unable to read adr dir %q: %w", dir, err)
	}
	today := time.Now().Format("2006-01-02")
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := adrFileRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		var a Adr
		a, err = parseFile(filepath.Join(dir, e.Name()), m[1], m[2], today)
		if err != nil {
			return nil, err
		}
		adrs = append(adrs, a)
	}
	sort.Slice(adrs, func(i, j int) bool { return adrs[i].Num < adrs[j].Num })
	return adrs, nil
}

func parseFile(path, numStr, slug, today string) (a Adr, err error) {
	var src []byte
	src, err = os.ReadFile(path)
	if err != nil {
		return a, eh.Errorf("unable to read adr %q: %w", path, err)
	}
	a.Num, _ = strconv.Atoi(numStr)
	a.Slug = slug
	a.Path = filepath.ToSlash(path)
	a.BodyBytes = len(src)
	a.PlanMaxPhase = -1

	fm, body := splitFrontmatter(string(src))
	a.Status = fm["status"]
	a.Date = fm["date"]
	a.ReviewedBy = fm["reviewed-by"]
	a.ReviewedDate = fm["reviewed-date"]
	a.SupersededBy = fm["superseded-by"]
	a.WithdrawnDate = fm["withdrawn-date"]

	// Title is the first H1 in the body — searched after the frontmatter so the
	// commented-out "# reviewed-by:" template lines are never mistaken for it.
	if hm := h1Re.FindStringSubmatch(body); hm != nil {
		a.Title = strings.TrimSpace(hm[1])
	}
	if a.Title == "" {
		a.Title = slug
	}

	a.UpdateCount = len(updateHeadRe.FindAllString(body, -1))
	a.HasUpdate = a.UpdateCount > 0

	// Freshness: the latest non-future date among the frontmatter dates and any
	// date in the body. The future cutoff drops prose deadlines/horizons (e.g.
	// a "2028-12-31" license horizon) that are not edit timestamps.
	for _, d := range []string{a.Date, a.ReviewedDate, a.WithdrawnDate} {
		if d != "" && d <= today && d > a.LastDate {
			a.LastDate = d
		}
	}
	for _, dm := range isoDateRe.FindAllStringSubmatch(body, -1) {
		if dm[1] <= today && dm[1] > a.LastDate {
			a.LastDate = dm[1]
		}
	}

	a.PlanMarkers, a.PlanMaxPhase = extractPlanMarkers(body)

	// body is a suffix of src, so the frontmatter's line count is the offset
	// that turns body-relative lines into file-relative ones.
	a.Subtasks = extractSubtasks(a.Num, body, strings.Count(string(src[:len(src)-len(body)]), "\n"))
	a.SubtasksTotal = len(a.Subtasks)
	for _, s := range a.Subtasks {
		if s.Done {
			a.SubtasksDone++
		}
	}
	return a, nil
}

// splitFrontmatter separates a leading `---`…`---` YAML block from the body.
// The block is parsed as flat scalar key→value pairs — the ADR corpus uses
// only top-level scalars, so this avoids a YAML dependency: indented (nested)
// lines and `#` comment lines are skipped, and surrounding quotes are stripped.
// When there is no leading block the whole input is returned as the body.
func splitFrontmatter(content string) (fm map[string]string, body string) {
	fm = map[string]string{}
	loc := fmDelimRe.FindAllStringIndex(content, 2)
	if len(loc) < 2 || loc[0][0] != 0 {
		return fm, content
	}
	for line := range strings.SplitSeq(content[loc[0][1]:loc[1][0]], "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fm[strings.TrimSpace(key)] = unquote(strings.TrimSpace(val))
	}
	return fm, content[loc[1][1]:]
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// extractPlanMarkers harvests the implementation-decomposition vocabulary an
// ADR defines for itself — Phase/Cut/Step/Milestone N and the M<n> milestone
// shorthand — returning the distinct sorted set and the highest Phase seen
// (-1 if none). Tier/Round are intentionally excluded: in this corpus they
// denote the ADR edit-policy and review rounds, not build progress.
func extractPlanMarkers(content string) (markers []string, maxPhase int) {
	maxPhase = -1
	set := make(map[string]struct{})
	for _, m := range planMarkerRe.FindAllStringSubmatch(content, -1) {
		word := strings.ToLower(m[1])
		word = strings.ToUpper(word[:1]) + word[1:]
		n, _ := strconv.Atoi(m[2])
		set[fmt.Sprintf("%s %d", word, n)] = struct{}{}
		if word == "Phase" && n > maxPhase {
			maxPhase = n
		}
	}
	for _, m := range mMarkerRe.FindAllStringSubmatch(content, -1) {
		n, _ := strconv.Atoi(m[1])
		set["M"+strconv.Itoa(n)] = struct{}{}
	}
	markers = make([]string, 0, len(set))
	for k := range set {
		markers = append(markers, k)
	}
	sort.Strings(markers)
	return markers, maxPhase
}
