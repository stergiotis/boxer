//go:build llm_generated_opus47

// Package llmtag applies //go:build llm_generated_* markers to Go source
// files whose git-blame attribution points to commits authored with an
// LLM Co-Authored-By trailer.
package llmtag

import (
	"bufio"
	"context"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
	DecisionUnknown           DecisionE = 0
	DecisionSkipAlreadyTagged DecisionE = 1
	DecisionSkipHasBuildTag   DecisionE = 2
	DecisionSkipNotEnoughLLM  DecisionE = 3
	DecisionSkipNoLines       DecisionE = 4
	DecisionSkipUncommitted   DecisionE = 5
	DecisionWouldApply        DecisionE = 6
	DecisionApplied           DecisionE = 7
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
	default:
		s = "unknown"
	}
	return
}

// FileReport describes what the Applier decided for one file.
type FileReport struct {
	Path                  string           `json:"path"`
	TotalLines            int32            `json:"totalLines"`
	LLMLines              int32            `json:"llmLines"`
	ModelLines            map[string]int32 `json:"modelLines,omitempty"`
	DominantTag           string           `json:"dominantTag,omitempty"`
	ExistingBuildDirective string          `json:"existingBuildDirective,omitempty"`
	Decision              DecisionE        `json:"-"`
	DecisionLabel         string           `json:"decision"`
}

// Applier analyses a directory of Go source files, attributes lines to
// commits via git blame, and prepends //go:build llm_generated_<tag>
// when the LLM share exceeds Threshold.
//
// Zero value is usable: ApplyOpDryRun, Threshold 0.5, default identities.
type Applier struct {
	ApplyOp         ApplyOpE
	Threshold       float64
	ExtraIdentities []LLMIdentity
}

// Run walks root yielding one FileReport per considered Go file.
func (inst *Applier) Run(ctx context.Context, git *repo.GitRunner, root string) iter.Seq2[FileReport, error] {
	return func(yield func(FileReport, error) bool) {
		identities := slices.Concat(KnownLLMIdentities, inst.ExtraIdentities)

		var llmCommits map[string]string
		{ // Stage: pre-scan commit history for LLM co-authors.
			var err error
			llmCommits, err = scanLLMCommits(ctx, git, identities)
			if err != nil {
				yield(FileReport{}, eh.Errorf("unable to scan commit history: %w", err))
				return
			}
		}

		threshold := inst.Threshold
		if threshold <= 0 {
			threshold = 0.5
		}

		for absPath, walkErr := range walkGoFiles(root) {
			if walkErr != nil {
				if !yield(FileReport{}, eh.Errorf("walk error: %w", walkErr)) {
					return
				}
				continue
			}
			rec, procErr := inst.processFile(ctx, git, absPath, llmCommits, threshold)
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

func (inst *Applier) processFile(ctx context.Context, git *repo.GitRunner, absPath string, llmCommits map[string]string, threshold float64) (rec FileReport, err error) {
	rec.Path = absPath
	rec.ModelLines = make(map[string]int32, 4)

	var existing string
	existing, err = readExistingBuildDirective(absPath)
	if err != nil {
		err = eh.Errorf("unable to read file preamble: %w", err)
		return
	}
	rec.ExistingBuildDirective = existing
	if strings.Contains(existing, "llm_generated") {
		rec.Decision = DecisionSkipAlreadyTagged
		return
	}

	blamePath := absPath
	{ // Stage: resolve blame path relative to git repo root if possible.
		rel, relErr := filepath.Rel(git.RepoPath, absPath)
		if relErr == nil && !strings.HasPrefix(rel, "..") {
			blamePath = rel
		}
	}

	var counts map[string]int32
	var allCommitted bool
	counts, allCommitted, err = blameCounts(ctx, git, blamePath)
	if err != nil {
		err = eh.Errorf("git blame failed: %w", err)
		return
	}
	if !allCommitted {
		rec.Decision = DecisionSkipUncommitted
	}

	var total int32
	var llm int32
	for hash, count := range counts {
		total += count
		tag, ok := llmCommits[hash]
		if !ok {
			continue
		}
		llm += count
		rec.ModelLines[tag] += count
	}
	rec.TotalLines = total
	rec.LLMLines = llm

	if total == 0 {
		if rec.Decision == DecisionUnknown {
			rec.Decision = DecisionSkipNoLines
		}
		return
	}
	if float64(llm)/float64(total) < threshold {
		if rec.Decision == DecisionUnknown {
			rec.Decision = DecisionSkipNotEnoughLLM
		}
		return
	}

	var bestTag string
	var bestCount int32
	for tag, count := range rec.ModelLines {
		if count > bestCount {
			bestTag = tag
			bestCount = count
		}
	}
	rec.DominantTag = bestTag

	if existing != "" {
		rec.Decision = DecisionSkipHasBuildTag
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

// scanLLMCommits returns a map from commit hash to the llm_generated_<tag>
// suffix implied by the first matching Co-Authored-By trailer.
func scanLLMCommits(ctx context.Context, git *repo.GitRunner, identities []LLMIdentity) (out map[string]string, err error) {
	out = make(map[string]string, 512)

	var curHash string
	var curBody strings.Builder
	const recordEnd = "\x03"
	for line, iterErr := range git.RunLines(ctx, "log", "--format=%H%n%B%n"+recordEnd) {
		if iterErr != nil {
			err = eh.Errorf("unable to read git log: %w", iterErr)
			return
		}
		if line == recordEnd {
			tag := detectLLMFromBody(curBody.String(), identities)
			if tag != "" {
				out[curHash] = tag
			}
			curHash = ""
			curBody.Reset()
			continue
		}
		if curHash == "" {
			curHash = line
			continue
		}
		curBody.WriteString(line)
		curBody.WriteByte('\n')
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
