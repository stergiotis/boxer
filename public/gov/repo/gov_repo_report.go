//go:build llm_generated_opus46

package repo

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// box inner width (between the border characters)
const boxW = 76

type ReportGenerator struct {
	Since string
	Until string
	TopN  int
}

func (inst *ReportGenerator) since() (s string) {
	s = inst.Since
	if s == "" {
		s = "12 months ago"
	}
	return
}

func (inst *ReportGenerator) topN() (n int) {
	n = inst.TopN
	if n <= 0 {
		n = 10
	}
	return
}

func (inst *ReportGenerator) Generate(ctx context.Context, git *GitRunner, w io.Writer) (err error) {
	since := inst.since()
	until := inst.Until
	topN := inst.topN()

	// ── Banner ──────────────────────────────────────────────────────────
	err = wLine(w, "╔"+strings.Repeat("═", boxW)+"╗")
	if err != nil {
		return
	}
	err = wLine(w, "║"+padRight("", boxW)+"║")
	if err != nil {
		return
	}
	err = wLine(w, "║"+center("R E P O S I T O R Y   H E A L T H   R E P O R T", boxW)+"║")
	if err != nil {
		return
	}
	{
		label := since
		if until != "" {
			label += " .. " + until
		}
		err = wLine(w, "║"+center("━━━  "+label+"  ━━━", boxW)+"║")
		if err != nil {
			return
		}
	}
	err = wLine(w, "║"+padRight("", boxW)+"║")
	if err != nil {
		return
	}
	err = wLine(w, "╚"+strings.Repeat("═", boxW)+"╝")
	if err != nil {
		return
	}
	_, err = fmt.Fprint(w, "\n")
	if err != nil {
		return
	}

	{ // Section: Contributors & Bus Factor
		analyzer := &ContributorAnalyzer{Since: since, Until: until}
		var bf BusFactorResult
		bf, err = analyzer.RunSummary(ctx, git)
		if err != nil {
			err = eh.Errorf("contributor analysis failed: %w", err)
			return
		}
		err = inst.writeContributors(w, bf, topN)
		if err != nil {
			return
		}
	}

	err = writeDivider(w)
	if err != nil {
		return
	}

	{ // Section: Velocity
		analyzer := &VelocityAnalyzer{Since: since, Until: until}
		var records []VelocityRecord
		records, err = collectSeq2(analyzer.Run(ctx, git))
		if err != nil {
			err = eh.Errorf("velocity analysis failed: %w", err)
			return
		}
		err = inst.writeVelocity(w, records)
		if err != nil {
			return
		}
	}

	err = writeDivider(w)
	if err != nil {
		return
	}

	{ // Section: Code Authorship
		analyzer := &AuthorshipAnalyzer{}
		var records []AuthorshipRecord
		records, err = collectSeq2(analyzer.Run(ctx, git))
		if err != nil {
			err = eh.Errorf("authorship analysis failed: %w", err)
			return
		}
		err = inst.writeAuthorship(w, records)
		if err != nil {
			return
		}
	}

	err = writeDivider(w)
	if err != nil {
		return
	}

	{ // Section: High-Churn Files
		analyzer := &ChurnAnalyzer{TopN: topN, Since: since, Until: until}
		var records []ChurnRecord
		records, err = collectSeq2(analyzer.Run(ctx, git))
		if err != nil {
			err = eh.Errorf("churn analysis failed: %w", err)
			return
		}
		err = inst.writeChurn(w, records)
		if err != nil {
			return
		}
	}

	err = writeDivider(w)
	if err != nil {
		return
	}

	{ // Section: Bug Hotspots
		analyzer := &BugHotspotAnalyzer{TopN: topN, Since: since, Until: until}
		var records []BugHotspotRecord
		records, err = collectSeq2(analyzer.Run(ctx, git))
		if err != nil {
			err = eh.Errorf("bug hotspot analysis failed: %w", err)
			return
		}
		err = inst.writeBugHotspots(w, records)
		if err != nil {
			return
		}
	}

	err = writeDivider(w)
	if err != nil {
		return
	}

	{ // Section: Firefighting
		analyzer := &FirefightAnalyzer{Since: since, Until: until}
		var records []FirefightRecord
		records, err = collectSeq2(analyzer.Run(ctx, git))
		if err != nil {
			err = eh.Errorf("firefighting analysis failed: %w", err)
			return
		}
		err = inst.writeFirefighting(w, records)
		if err != nil {
			return
		}
	}

	// ── Footer ──────────────────────────────────────────────────────────
	err = wLine(w, "╰"+strings.Repeat("─", boxW)+"╯")
	if err != nil {
		return
	}

	return
}

func (inst *ReportGenerator) writeContributors(w io.Writer, bf BusFactorResult, topN int) (err error) {
	err = writeSectionHeader(w, "Contributors & Bus Factor")
	if err != nil {
		return
	}

	busIcon := "▼"
	busWord := "RISK"
	if bf.BusFactor >= 3 {
		busIcon = "●"
		busWord = "HEALTHY"
	} else if bf.BusFactor >= 2 {
		busIcon = "◆"
		busWord = "LOW"
	}

	line := fmt.Sprintf("  %s Bus Factor: %d (%s)       Total Commits: %d", busIcon, bf.BusFactor, busWord, bf.TotalCommits)
	err = wRow(w, line)
	if err != nil {
		return
	}
	err = wRow(w, "")
	if err != nil {
		return
	}

	n := topN
	if n > len(bf.Contributors) {
		n = len(bf.Contributors)
	}
	const barW = 30
	for _, c := range bf.Contributors[:n] {
		full := int(math.Round(c.Percentage / 100.0 * barW))
		bar := strings.Repeat("█", full) + strings.Repeat("░", barW-full)
		author := padRight(truncateMid(c.Author, 28), 28)
		line = fmt.Sprintf("  %s  %s %5.1f%%", author, bar, c.Percentage)
		err = wRow(w, line)
		if err != nil {
			return
		}
	}
	return
}

func (inst *ReportGenerator) writeVelocity(w io.Writer, records []VelocityRecord) (err error) {
	err = writeSectionHeader(w, "Commit Velocity")
	if err != nil {
		return
	}

	if len(records) == 0 {
		err = wRow(w, "  (no commits in range)")
		return
	}

	maxCount := 0
	for _, r := range records {
		if r.CommitCount > maxCount {
			maxCount = r.CommitCount
		}
	}
	const barW = 50
	for _, r := range records {
		bar := buildBar(r.CommitCount, maxCount, barW)
		line := fmt.Sprintf("  %s  %4d %s", r.Month, r.CommitCount, bar)
		err = wRow(w, line)
		if err != nil {
			return
		}
	}
	return
}

func (inst *ReportGenerator) writeChurn(w io.Writer, records []ChurnRecord) (err error) {
	err = writeSectionHeader(w, "High-Churn Files")
	if err != nil {
		return
	}

	if len(records) == 0 {
		err = wRow(w, "  (no file changes in range)")
		return
	}

	maxCount := records[0].ChangeCount
	const pathW = 55
	const barW = 12
	for _, r := range records {
		bar := buildBar(r.ChangeCount, maxCount, barW)
		path := padRight(truncateMid(r.FilePath, pathW), pathW)
		line := fmt.Sprintf("  %s %4d %s", path, r.ChangeCount, bar)
		err = wRow(w, line)
		if err != nil {
			return
		}
	}
	return
}

func (inst *ReportGenerator) writeBugHotspots(w io.Writer, records []BugHotspotRecord) (err error) {
	err = writeSectionHeader(w, "Bug Hotspots")
	if err != nil {
		return
	}

	if len(records) == 0 {
		err = wRow(w, "  (no bug-fix commits in range)")
		return
	}

	maxCount := records[0].BugFixCount
	const pathW = 55
	const barW = 12
	for _, r := range records {
		bar := buildBar(r.BugFixCount, maxCount, barW)
		path := padRight(truncateMid(r.FilePath, pathW), pathW)
		line := fmt.Sprintf("  %s %4d %s", path, r.BugFixCount, bar)
		err = wRow(w, line)
		if err != nil {
			return
		}
	}
	return
}

func (inst *ReportGenerator) writeFirefighting(w io.Writer, records []FirefightRecord) (err error) {
	err = writeSectionHeader(w, "Firefighting")
	if err != nil {
		return
	}

	if len(records) == 0 {
		err = wRow(w, "  ● No reverts, hotfixes, or emergency commits found.")
		return
	}

	revertCount := 0
	hotfixCount := 0
	emergencyCount := 0
	for _, r := range records {
		switch r.Kind {
		case FirefightKindRevert:
			revertCount++
		case FirefightKindHotfix:
			hotfixCount++
		case FirefightKindEmergency:
			emergencyCount++
		}
	}

	line := fmt.Sprintf("  ▼ Reverts: %-5d    Hotfixes: %-5d    Emergencies: %d", revertCount, hotfixCount, emergencyCount)
	err = wRow(w, line)
	if err != nil {
		return
	}
	err = wRow(w, "")
	if err != nil {
		return
	}

	n := len(records)
	if n > 5 {
		n = 5
	}
	for _, r := range records[:n] {
		subj := truncateMid(r.Subject, 48)
		line = fmt.Sprintf("    %s  %-9s  %s", r.Date, r.Kind.String(), subj)
		err = wRow(w, line)
		if err != nil {
			return
		}
	}
	if len(records) > 5 {
		line = fmt.Sprintf("    ... and %d more", len(records)-5)
		err = wRow(w, line)
		if err != nil {
			return
		}
	}
	return
}

func (inst *ReportGenerator) writeAuthorship(w io.Writer, records []AuthorshipRecord) (err error) {
	err = writeSectionHeader(w, "Code Authorship (Human ▓▓ vs LLM ██)")
	if err != nil {
		return
	}
	if len(records) == 0 {
		err = wRow(w, "  (no Go files found)")
		return
	}

	// Find max for scaling both sides to the same scale
	maxVal := 0
	for _, r := range records {
		if r.HumanLines > maxVal {
			maxVal = r.HumanLines
		}
		if r.LLMLines > maxVal {
			maxVal = r.LLMLines
		}
	}

	const halfBar = 25
	for _, r := range records {
		humanCells := 0
		llmCells := 0
		if maxVal > 0 {
			humanCells = int(math.Round(float64(r.HumanLines) / float64(maxVal) * halfBar))
			llmCells = int(math.Round(float64(r.LLMLines) / float64(maxVal) * halfBar))
		}
		humanBar := strings.Repeat(" ", halfBar-humanCells) + strings.Repeat("▓", humanCells)
		llmBar := strings.Repeat("█", llmCells) + strings.Repeat(" ", halfBar-llmCells)
		line := fmt.Sprintf("  %s %s│%s", r.Month, humanBar, llmBar)
		err = wRow(w, line)
		if err != nil {
			return
		}
	}

	// Summary line
	if last := records[len(records)-1]; last.HumanLines+last.LLMLines > 0 {
		total := last.HumanLines + last.LLMLines
		pct := 100.0 * float64(last.LLMLines) / float64(total)
		line := fmt.Sprintf("  Latest: %d human, %d LLM (%d files), %.1f%% LLM", last.HumanLines, last.LLMLines, last.LLMFiles, pct)
		err = wRow(w, "")
		if err != nil {
			return
		}
		err = wRow(w, line)
		if err != nil {
			return
		}
	}
	return
}

// ── Box drawing helpers ─────────────────────────────────────────────────────

func writeSectionHeader(w io.Writer, title string) (err error) {
	err = wRow(w, "")
	if err != nil {
		return
	}
	titleLen := utf8.RuneCountInString(title)
	pad := boxW - titleLen - 6 // "  ┄┄ " + title + " ┄..."
	if pad < 0 {
		pad = 0
	}
	err = wRow(w, "  "+strings.Repeat("┄", 2)+" "+title+" "+strings.Repeat("┄", pad))
	if err != nil {
		return
	}
	err = wRow(w, "")
	return
}

func writeDivider(w io.Writer) (err error) {
	_, err = fmt.Fprint(w, "\n")
	return
}

func wRow(w io.Writer, content string) (err error) {
	_, err = fmt.Fprintf(w, "│%s│\n", padRight(content, boxW))
	return
}

func wLine(w io.Writer, s string) (err error) {
	_, err = fmt.Fprintln(w, s)
	return
}

// ── Bar rendering ───────────────────────────────────────────────────────────

// buildBar renders a proportional bar using eighth-block characters for
// sub-character precision: " ▏▎▍▌▋▊▉█"
func buildBar(value int, maxValue int, width int) (bar string) {
	if maxValue <= 0 || width <= 0 {
		bar = strings.Repeat(" ", width)
		return
	}
	// eighths is the filled amount in 1/8th-cell units
	eighths := int(math.Round(float64(value) / float64(maxValue) * float64(width) * 8))
	fullCells := eighths / 8
	remainder := eighths % 8

	// eighth-block elements indexed 0..8 (0 = space, 8 = full block)
	blocks := []rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉', '█'}

	var sb strings.Builder
	sb.Grow(width * 3)
	for range fullCells {
		sb.WriteRune('█')
	}
	used := fullCells
	if used < width {
		sb.WriteRune(blocks[remainder])
		used++
	}
	for range width - used {
		sb.WriteRune(' ')
	}
	bar = sb.String()
	return
}

// ── String helpers ──────────────────────────────────────────────────────────

func collectSeq2[T any](seq func(func(T, error) bool)) (result []T, err error) {
	result = make([]T, 0, 32)
	for v, iterErr := range seq {
		if iterErr != nil {
			err = iterErr
			return
		}
		result = append(result, v)
	}
	return
}

// truncateMid truncates long strings by keeping the start and end,
// replacing the middle with "..". This preserves directory context and
// the filename.
func truncateMid(s string, maxLen int) (r string) {
	runes := []rune(s)
	if len(runes) <= maxLen {
		r = s
		return
	}
	if maxLen < 5 {
		r = string(runes[:maxLen])
		return
	}
	// keep slightly more of the tail (filename is usually at the end)
	headLen := (maxLen - 2) / 3
	tailLen := maxLen - 2 - headLen
	r = string(runes[:headLen]) + ".." + string(runes[len(runes)-tailLen:])
	return
}

func padRight(s string, width int) (r string) {
	runeLen := utf8.RuneCountInString(s)
	if runeLen >= width {
		r = s
		return
	}
	r = s + strings.Repeat(" ", width-runeLen)
	return
}

func center(s string, width int) (r string) {
	runeLen := utf8.RuneCountInString(s)
	if runeLen >= width {
		r = s
		return
	}
	left := (width - runeLen) / 2
	right := width - runeLen - left
	r = strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
	return
}
