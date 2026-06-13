// Package llmtag applies //go:build llm_generated_* markers to Go source
// files whose git-blame attribution points to commits authored with an
// LLM Co-Authored-By trailer.
package llmtag

import (
	"bufio"
	"bytes"
	"context"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/gov/repo"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// LLMIdentity names an LLM whose Co-Authored-By trailers should be
// mapped to a llm_generated_<Tag> build tag.
type LLMIdentity struct {
	Display string
	Tag     string
}

// KnownLLMIdentities holds the identities matched by default. Callers may
// extend the list via Applier.ExtraIdentities without mutating the default.
var KnownLLMIdentities = []LLMIdentity{
	{Display: "Claude Opus 4.6", Tag: "opus46"},
	{Display: "Claude Opus 4.7", Tag: "opus47"},
	{Display: "Claude Sonnet 4.6", Tag: "sonnet46"},
	{Display: "Claude Haiku 4.5", Tag: "haiku45"},
	{Display: "Gemini 3 Pro", Tag: "gemini3pro"},
}

// ApplyOpE selects whether the Applier mutates files or only reports.
type ApplyOpE uint8

const (
	ApplyOpDryRun ApplyOpE = 0
	ApplyOpApply  ApplyOpE = 1
)

// DecisionE is the outcome of processing a single file.
type DecisionE uint8

const (
	DecisionUnknown                 DecisionE = 0
	DecisionSkipAlreadyTagged       DecisionE = 1
	DecisionSkipHasBuildTag         DecisionE = 2
	DecisionSkipNotEnoughLLM        DecisionE = 3
	DecisionSkipNoLines             DecisionE = 4
	DecisionSkipUncommitted         DecisionE = 5
	DecisionWouldApply              DecisionE = 6
	DecisionApplied                 DecisionE = 7
	DecisionSkipComplexLLMDirective DecisionE = 8
	DecisionWouldUpdate             DecisionE = 9
	DecisionUpdated                 DecisionE = 10
	DecisionWouldRemove             DecisionE = 11
	DecisionRemoved                 DecisionE = 12
	DecisionSkipUntracked           DecisionE = 13
)

// String returns a stable label for diagnostics.
func (inst DecisionE) String() (s string) {
	switch inst {
	case DecisionSkipAlreadyTagged:
		s = "skip_already_tagged"
	case DecisionSkipHasBuildTag:
		s = "skip_has_unrelated_build_tag"
	case DecisionSkipNotEnoughLLM:
		s = "skip_below_threshold"
	case DecisionSkipNoLines:
		s = "skip_no_lines"
	case DecisionSkipUncommitted:
		s = "skip_uncommitted"
	case DecisionWouldApply:
		s = "would_apply"
	case DecisionApplied:
		s = "applied"
	case DecisionSkipComplexLLMDirective:
		s = "skip_complex_llm_directive"
	case DecisionWouldUpdate:
		s = "would_update_tag"
	case DecisionUpdated:
		s = "updated_tag"
	case DecisionWouldRemove:
		s = "would_remove_tag"
	case DecisionRemoved:
		s = "removed_tag"
	case DecisionSkipUntracked:
		s = "skip_untracked"
	default:
		s = "unknown"
	}
	return
}

// FileReport describes what the Applier decided for one file.
type FileReport struct {
	Path                   string           `json:"path"`
	TotalLines             int32            `json:"totalLines"`
	LLMLines               int32            `json:"llmLines"`
	NonLLMLines            int32            `json:"nonLLMLines"`
	TrustedLLMLines        int32            `json:"trustedLLMLines,omitempty"`
	ModelLines             map[string]int32 `json:"modelLines,omitempty"`
	DominantTag            string           `json:"dominantTag,omitempty"`
	ExistingBuildDirective string           `json:"existingBuildDirective,omitempty"`
	ExistingLLMTag         string           `json:"existingLLMTag,omitempty"`
	Decision               DecisionE        `json:"-"`
	DecisionLabel          string           `json:"decision"`
}

// Applier analyses a directory of Go source files, attributes lines to
// commits via git blame, and maintains //go:build llm_generated_<tag>
// directives so they reflect current attribution.
//
// A file is classified as LLM-generated iff LLM-attributed lines exceed
// BOTH Threshold (fraction of all counted lines) AND MinLLMLines
// (absolute floor). Tagged files whose share has dropped below the
// thresholds get their tag stripped; tagged files whose dominant model
// has changed get their tag rewritten.
//
// Co-Authored-By trailers became reliable only with Claude. Earlier LLMs
// (notably Gemini) authored code with no trailer, so blame on such files
// looks 100% human. To compensate, lines from commits whose author date
// is before TrailerCutoff are attributed to the existing simple
// llm_generated_<tag> directive when the file carries one. Untagged
// files treat pre-cutoff lines as non-LLM (the safe default — we have no
// signal to say otherwise). When TrailerCutoff is the zero time, no
// trust is granted and only trailers matter.
//
// Zero value is usable: ApplyOpDryRun, Threshold 0.5, MinLLMLines 0,
// default identities, no cutoff.
type Applier struct {
	ApplyOp         ApplyOpE
	Threshold       float64
	MinLLMLines     int32
	TrailerCutoff   time.Time
	ExtraIdentities []LLMIdentity
}

// AutoDetectCutoff scans the commit history once and returns the author
// date of the earliest LLM-trailered commit, or the zero time if none
// exist. Useful for logging the cutoff before iteration begins.
func (inst *Applier) AutoDetectCutoff(ctx context.Context, git *repo.GitRunner) (out time.Time, err error) {
	identities := slices.Concat(KnownLLMIdentities, inst.ExtraIdentities)
	var commits map[string]commitInfoT
	commits, err = scanCommits(ctx, git, identities)
	if err != nil {
		err = eh.Errorf("unable to scan commit history: %w", err)
		return
	}
	out = earliestLLMCommitDate(commits)
	return
}

// Run walks root yielding one FileReport per considered Go file.
func (inst *Applier) Run(ctx context.Context, git *repo.GitRunner, root string) iter.Seq2[FileReport, error] {
	return func(yield func(FileReport, error) bool) {
		identities := slices.Concat(KnownLLMIdentities, inst.ExtraIdentities)

		var commits map[string]commitInfoT
		{ // Stage: pre-scan commit history (date + LLM trailer per commit).
			var err error
			commits, err = scanCommits(ctx, git, identities)
			if err != nil {
				yield(FileReport{}, eh.Errorf("unable to scan commit history: %w", err))
				return
			}
		}

		var tracked map[string]struct{}
		{ // Stage: pre-fetch tracked .go files so untracked files can be
			// skipped without invoking blame (which would error on them).
			var err error
			tracked, err = scanTrackedGoFiles(ctx, git)
			if err != nil {
				yield(FileReport{}, eh.Errorf("unable to list tracked files: %w", err))
				return
			}
		}

		threshold := inst.Threshold
		if threshold <= 0 {
			threshold = 0.5
		}
		minLines := inst.MinLLMLines
		if minLines < 0 {
			minLines = 0
		}
		cutoff := inst.TrailerCutoff
		if cutoff.IsZero() {
			cutoff = earliestLLMCommitDate(commits)
		}

		for absPath, walkErr := range walkGoFiles(root) {
			if walkErr != nil {
				if !yield(FileReport{}, eh.Errorf("walk error: %w", walkErr)) {
					return
				}
				continue
			}
			rec, procErr := inst.processFile(ctx, git, absPath, commits, tracked, threshold, minLines, cutoff)
			rec.DecisionLabel = rec.Decision.String()
			if procErr != nil {
				if !yield(rec, eb.Build().Str("path", absPath).Errorf("processing failed: %w", procErr)) {
					return
				}
				continue
			}
			if !yield(rec, nil) {
				return
			}
		}
	}
}

func (inst *Applier) processFile(ctx context.Context, git *repo.GitRunner, absPath string, commits map[string]commitInfoT, tracked map[string]struct{}, threshold float64, minLines int32, cutoff time.Time) (rec FileReport, err error) {
	rec.Path = absPath
	rec.ModelLines = make(map[string]int32, 4)

	var existing string
	existing, err = readExistingBuildDirective(absPath)
	if err != nil {
		err = eh.Errorf("unable to read file preamble: %w", err)
		return
	}
	rec.ExistingBuildDirective = existing

	mentionsLLM := strings.Contains(existing, "llm_generated")
	simpleTag := extractSimpleLLMTag(existing)
	rec.ExistingLLMTag = simpleTag
	complexLLMDirective := mentionsLLM && simpleTag == ""
	hasOtherBuildTag := existing != "" && !mentionsLLM

	blamePath := absPath
	{ // Stage: resolve blame path relative to git repo root if possible.
		rel, relErr := filepath.Rel(git.RepoPath, absPath)
		if relErr == nil && !strings.HasPrefix(rel, "..") {
			blamePath = rel
		}
	}

	if _, ok := tracked[blamePath]; !ok {
		// Untracked file — no blame history to analyse. Skip cleanly so
		// new local files don't error out the run.
		rec.Decision = DecisionSkipUntracked
		return
	}

	var counts map[string]int32
	var allCommitted bool
	counts, allCommitted, err = blameCounts(ctx, git, blamePath)
	if err != nil {
		err = eh.Errorf("git blame failed: %w", err)
		return
	}

	var total, llm, trusted int32
	for hash, count := range counts {
		total += count
		info, known := commits[hash]
		switch {
		case known && info.LLMTag != "":
			llm += count
			rec.ModelLines[info.LLMTag] += count
		case simpleTag != "" && !cutoff.IsZero() && known && info.Date.Before(cutoff):
			// Pre-cutoff trailerless commit on a simple-tagged file.
			// Trust the existing tag as the line's provenance.
			llm += count
			trusted += count
			rec.ModelLines[simpleTag] += count
		}
	}
	rec.TotalLines = total
	rec.LLMLines = llm
	rec.NonLLMLines = total - llm
	rec.TrustedLLMLines = trusted

	var bestTag string
	{ // Stage: pick the model with the most attributed lines.
		var bestCount int32
		for tag, count := range rec.ModelLines {
			if count > bestCount {
				bestTag = tag
				bestCount = count
			}
		}
	}
	rec.DominantTag = bestTag

	var shouldBeTagged bool
	if total > 0 {
		shouldBeTagged = float64(llm)/float64(total) > threshold && llm > minLines
	}

	switch {
	case complexLLMDirective:
		// Manually-crafted directive (e.g. `llm_generated_X || llm_generated_Y`,
		// `!llm_generated_X`, `integration && llm_generated_X`). Don't touch.
		rec.Decision = DecisionSkipComplexLLMDirective
		return

	case simpleTag != "":
		// File carries a simple llm_generated_<tag> directive — re-evaluate.
		if !allCommitted {
			rec.Decision = DecisionSkipUncommitted
			return
		}
		if total == 0 {
			rec.Decision = DecisionSkipNoLines
			return
		}
		if !shouldBeTagged {
			if inst.ApplyOp == ApplyOpDryRun {
				rec.Decision = DecisionWouldRemove
				return
			}
			err = removeBuildTag(absPath, simpleTag)
			if err != nil {
				err = eb.Build().Str("path", absPath).Str("tag", simpleTag).Errorf("unable to remove build tag: %w", err)
				return
			}
			rec.Decision = DecisionRemoved
			return
		}
		if simpleTag != bestTag && bestTag != "" {
			if inst.ApplyOp == ApplyOpDryRun {
				rec.Decision = DecisionWouldUpdate
				return
			}
			err = updateBuildTag(absPath, simpleTag, bestTag)
			if err != nil {
				err = eb.Build().Str("path", absPath).Str("oldTag", simpleTag).Str("newTag", bestTag).Errorf("unable to update build tag: %w", err)
				return
			}
			rec.Decision = DecisionUpdated
			return
		}
		rec.Decision = DecisionSkipAlreadyTagged
		return

	case hasOtherBuildTag:
		// Non-llm directive present. Surface a conflict iff we would have
		// tagged; otherwise stay silent.
		if shouldBeTagged {
			rec.Decision = DecisionSkipHasBuildTag
		} else if total == 0 {
			rec.Decision = DecisionSkipNoLines
		} else {
			rec.Decision = DecisionSkipNotEnoughLLM
		}
		return

	default:
		// Untagged file.
		if total == 0 {
			rec.Decision = DecisionSkipNoLines
			return
		}
		if !shouldBeTagged {
			rec.Decision = DecisionSkipNotEnoughLLM
			return
		}
		if inst.ApplyOp == ApplyOpDryRun {
			rec.Decision = DecisionWouldApply
			return
		}
		err = prependBuildTag(absPath, bestTag)
		if err != nil {
			err = eb.Build().Str("path", absPath).Str("tag", bestTag).Errorf("unable to prepend build tag: %w", err)
			return
		}
		rec.Decision = DecisionApplied
		return
	}
}

// commitInfoT carries the per-commit metadata needed to attribute a blame
// line. LLMTag is the llm_generated_<tag> suffix implied by a matching
// Co-Authored-By trailer (empty when none was found).
type commitInfoT struct {
	Date   time.Time
	LLMTag string
}

// scanCommits returns a map from commit hash to commitInfoT for every
// commit reachable from HEAD. AuthorDate (%aI) is preferred so that the
// cutoff reflects when the work was originally authored, even if commits
// were later rebased or cherry-picked.
func scanCommits(ctx context.Context, git *repo.GitRunner, identities []LLMIdentity) (out map[string]commitInfoT, err error) {
	out = make(map[string]commitInfoT, 1024)

	const recordEnd = "\x03"
	var stage int
	var curHash string
	var curDate time.Time
	var curBody strings.Builder
	for line, iterErr := range git.RunLines(ctx, "log", "--format=%H%n%aI%n%B%n"+recordEnd) {
		if iterErr != nil {
			err = eh.Errorf("unable to read git log: %w", iterErr)
			return
		}
		if line == recordEnd {
			tag := detectLLMFromBody(curBody.String(), identities)
			out[curHash] = commitInfoT{Date: curDate, LLMTag: tag}
			stage = 0
			curHash = ""
			curDate = time.Time{}
			curBody.Reset()
			continue
		}
		switch stage {
		case 0:
			curHash = line
			stage = 1
		case 1:
			d, parseErr := time.Parse(time.RFC3339, line)
			if parseErr == nil {
				curDate = d
			}
			stage = 2
		default:
			curBody.WriteString(line)
			curBody.WriteByte('\n')
		}
	}
	return
}

// scanTrackedGoFiles returns the set of repo-relative paths to tracked
// .go files. Used to skip untracked sources cleanly, since `git blame`
// errors out on them.
func scanTrackedGoFiles(ctx context.Context, git *repo.GitRunner) (out map[string]struct{}, err error) {
	out = make(map[string]struct{}, 1024)
	for line, iterErr := range git.RunLines(ctx, "ls-files", "--", "*.go") {
		if iterErr != nil {
			err = eh.Errorf("ls-files: %w", iterErr)
			return
		}
		if line == "" {
			continue
		}
		out[line] = struct{}{}
	}
	return
}

// earliestLLMCommitDate returns the author date of the earliest commit
// carrying an LLM Co-Authored-By trailer, or the zero time if none exist.
func earliestLLMCommitDate(commits map[string]commitInfoT) (out time.Time) {
	for _, info := range commits {
		if info.LLMTag == "" {
			continue
		}
		if out.IsZero() || info.Date.Before(out) {
			out = info.Date
		}
	}
	return
}

// detectLLMFromBody scans a commit body for Co-Authored-By trailers that
// match a known LLM identity. Returns the last matching tag, so the most
// recent trailer wins when multiple are present.
func detectLLMFromBody(body string, identities []LLMIdentity) (tag string) {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(strings.ToLower(line), "co-authored-by:") {
			continue
		}
		value := strings.TrimSpace(line[len("co-authored-by:"):])
		for _, id := range identities {
			if strings.HasPrefix(value, id.Display) {
				tag = id.Tag
				break
			}
		}
	}
	return
}

// blameCounts runs `git blame --porcelain` and counts content lines per
// commit hash. allCommitted is false when uncommitted lines are present
// (their synthetic zero hash is dropped from the map).
func blameCounts(ctx context.Context, git *repo.GitRunner, path string) (counts map[string]int32, allCommitted bool, err error) {
	counts = make(map[string]int32, 32)
	allCommitted = true

	var curHash string
	for line, iterErr := range git.RunLines(ctx, "blame", "--porcelain", "--", path) {
		if iterErr != nil {
			err = eh.Errorf("blame stream failed: %w", iterErr)
			return
		}
		if len(line) >= 40 && isHexPrefix(line[:40]) && (len(line) == 40 || line[40] == ' ') {
			curHash = line[:40]
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			continue
		}
		if curHash == "" {
			continue
		}
		if isZeroHash(curHash) {
			allCommitted = false
			continue
		}
		counts[curHash]++
	}
	return
}

// readExistingBuildDirective returns the first //go:build or // +build
// directive found in the preamble (before the package clause), or an empty
// string if none is present.
func readExistingBuildDirective(path string) (directive string, err error) {
	f, err := os.Open(path)
	if err != nil {
		err = eh.Errorf("open: %w", err)
		return
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "//go:build ") || strings.HasPrefix(trimmed, "// +build ") {
			directive = trimmed
			return
		}
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		return
	}
	err = scanner.Err()
	if err != nil {
		err = eh.Errorf("scan preamble: %w", err)
	}
	return
}

// prependBuildTag rewrites path with a //go:build llm_generated_<tag>
// header followed by a blank line. Original file mode is preserved.
func prependBuildTag(path string, tag string) (err error) {
	var info os.FileInfo
	info, err = os.Stat(path)
	if err != nil {
		err = eh.Errorf("stat: %w", err)
		return
	}
	var data []byte
	data, err = os.ReadFile(path)
	if err != nil {
		err = eh.Errorf("read: %w", err)
		return
	}
	header := []byte("//go:build llm_generated_" + tag + "\n\n")
	out := make([]byte, 0, len(header)+len(data))
	out = append(out, header...)
	out = append(out, data...)
	err = os.WriteFile(path, out, info.Mode().Perm())
	if err != nil {
		err = eh.Errorf("write: %w", err)
		return
	}
	return
}

// extractSimpleLLMTag returns the suffix of a single positive llm_generated
// build directive (e.g. "opus47" for "//go:build llm_generated_opus47").
// Returns "" for complex expressions, negations, conjunctions/disjunctions,
// or non-llm directives.
func extractSimpleLLMTag(directive string) (tag string) {
	const prefixModern = "//go:build llm_generated_"
	const prefixLegacy = "// +build llm_generated_"
	var rest string
	switch {
	case strings.HasPrefix(directive, prefixModern):
		rest = strings.TrimSpace(directive[len(prefixModern):])
	case strings.HasPrefix(directive, prefixLegacy):
		rest = strings.TrimSpace(directive[len(prefixLegacy):])
	default:
		return
	}
	if !isPlainTagSuffix(rest) {
		return
	}
	tag = rest
	return
}

// isPlainTagSuffix reports whether s is a non-empty alphanumeric identifier
// — i.e. a tag suffix free of whitespace, parentheses, !, &&, ||.
func isPlainTagSuffix(s string) (ok bool) {
	if s == "" {
		return
	}
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return
	}
	ok = true
	return
}

// removeBuildTag strips the simple //go:build llm_generated_<expectedTag>
// directive from the preamble of path, along with one immediately-following
// blank line if present. Original file mode is preserved.
func removeBuildTag(path string, expectedTag string) (err error) {
	var info os.FileInfo
	info, err = os.Stat(path)
	if err != nil {
		err = eh.Errorf("stat: %w", err)
		return
	}
	var data []byte
	data, err = os.ReadFile(path)
	if err != nil {
		err = eh.Errorf("read: %w", err)
		return
	}
	lines := bytes.Split(data, []byte("\n"))
	idx := findSimpleLLMDirectiveLine(lines, expectedTag)
	if idx < 0 {
		err = eb.Build().Str("expectedTag", expectedTag).Errorf("simple llm_generated directive not found in preamble")
		return
	}
	end := idx + 1
	if end < len(lines) && len(bytes.TrimSpace(lines[end])) == 0 {
		end++
	}
	out := make([][]byte, 0, len(lines)-(end-idx))
	out = append(out, lines[:idx]...)
	out = append(out, lines[end:]...)
	err = os.WriteFile(path, bytes.Join(out, []byte("\n")), info.Mode().Perm())
	if err != nil {
		err = eh.Errorf("write: %w", err)
		return
	}
	return
}

// updateBuildTag replaces a simple //go:build llm_generated_<oldTag>
// directive in the preamble of path with the canonical form for newTag.
func updateBuildTag(path string, oldTag string, newTag string) (err error) {
	var info os.FileInfo
	info, err = os.Stat(path)
	if err != nil {
		err = eh.Errorf("stat: %w", err)
		return
	}
	var data []byte
	data, err = os.ReadFile(path)
	if err != nil {
		err = eh.Errorf("read: %w", err)
		return
	}
	lines := bytes.Split(data, []byte("\n"))
	idx := findSimpleLLMDirectiveLine(lines, oldTag)
	if idx < 0 {
		err = eb.Build().Str("oldTag", oldTag).Errorf("simple llm_generated directive not found in preamble")
		return
	}
	lines[idx] = []byte("//go:build llm_generated_" + newTag)
	err = os.WriteFile(path, bytes.Join(lines, []byte("\n")), info.Mode().Perm())
	if err != nil {
		err = eh.Errorf("write: %w", err)
		return
	}
	return
}

// findSimpleLLMDirectiveLine scans the preamble (blank lines and non-build
// // comments are tolerated) for a simple //go:build llm_generated_<tag>
// line whose tag matches expectedTag. Returns the line index, or -1 if it
// is not present before the package clause.
func findSimpleLLMDirectiveLine(lines [][]byte, expectedTag string) (idx int) {
	idx = -1
	for i, raw := range lines {
		s := strings.TrimSpace(string(raw))
		if s == "" {
			continue
		}
		isBuildLine := strings.HasPrefix(s, "//go:build ") || strings.HasPrefix(s, "// +build ")
		if !isBuildLine {
			if strings.HasPrefix(s, "//") {
				continue
			}
			return
		}
		if extractSimpleLLMTag(s) == expectedTag {
			idx = i
			return
		}
		return
	}
	return
}

// walkGoFiles yields absolute paths to non-generated Go source files under
// root, skipping vendor, .git, node_modules and testdata directories.
func walkGoFiles(root string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		stop := false
		walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, entErr error) (err error) {
			if entErr != nil {
				err = entErr
				return
			}
			if d.IsDir() {
				if isSkipDir(d.Name()) {
					err = fs.SkipDir
				}
				return
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".go") {
				return
			}
			if strings.Contains(name, ".gen.") || strings.Contains(name, ".out.") {
				return
			}
			if !yield(path, nil) {
				stop = true
				err = fs.SkipAll
			}
			return
		})
		if walkErr != nil && !stop {
			yield("", eh.Errorf("walk: %w", walkErr))
		}
	}
}

func isSkipDir(name string) (skip bool) {
	switch name {
	case ".git", "vendor", "node_modules", "testdata":
		skip = true
	}
	return
}

func isHexPrefix(s string) (ok bool) {
	ok = true
	for _, c := range s {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		ok = false
		return
	}
	return
}

func isZeroHash(s string) (ok bool) {
	for _, c := range s {
		if c != '0' {
			return
		}
	}
	ok = true
	return
}
